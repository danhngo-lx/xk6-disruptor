package disruptors

import (
	"context"
	"fmt"
	"time"

	"github.com/danhngo-lx/xk6-disruptor/pkg/kubernetes"
	"github.com/danhngo-lx/xk6-disruptor/pkg/kubernetes/helpers"
)

// FillerWorkloadFault specifies a throwaway filler Deployment sized to consume a
// node pool's allocatable capacity, forcing genuine Pending pods / cluster-autoscaler
// pressure. This complements node CPU/memory stress (which simulates noisy-neighbour
// resource pressure, not unschedulable capacity).
type FillerWorkloadFault struct {
	// Replicas is the number of filler pods to create (required, > 0).
	Replicas int32 `js:"replicas"`
	// CPUPerReplica is the CPU request per replica in Kubernetes quantity format
	// (e.g. "500m"). At least one of CPUPerReplica/MemoryPerReplica must be set.
	CPUPerReplica string `js:"cpuPerReplica"`
	// MemoryPerReplica is the memory request per replica in Kubernetes quantity
	// format (e.g. "512Mi"). At least one of CPUPerReplica/MemoryPerReplica must be set.
	MemoryPerReplica string `js:"memoryPerReplica"`
	// NodeSelector constrains the filler pods to a specific node pool
	// (e.g. {"agentpool": "myPool"}).
	NodeSelector map[string]string `js:"nodeSelector"`
	// PriorityClassName controls preemption behaviour; leave empty to use the cluster default.
	PriorityClassName string `js:"priorityClassName"`
}

// Validate checks that the fault has enough information to create a filler workload.
func (f FillerWorkloadFault) Validate() error {
	if f.Replicas <= 0 {
		return fmt.Errorf("replicas must be > 0")
	}
	if f.CPUPerReplica == "" && f.MemoryPerReplica == "" {
		return fmt.Errorf("at least one of cpuPerReplica or memoryPerReplica must be set")
	}
	return nil
}

// FillerWorkloadFaultInjector defines the interface for injecting a filler-workload fault.
type FillerWorkloadFaultInjector interface {
	InjectFillerWorkload(ctx context.Context, fault FillerWorkloadFault, duration time.Duration) error
}

// NodePoolDisruptor defines the methods for injecting node-pool capacity faults.
// It is a namespaced, pure-API disruptor: it creates and deletes a Deployment,
// it never execs into a target pod.
type NodePoolDisruptor interface {
	Disruptor
	FillerWorkloadFaultInjector
}

// nodePoolDisruptor is the concrete implementation of NodePoolDisruptor.
type nodePoolDisruptor struct {
	helper    helpers.WorkloadHelper
	namespace string
	name      string
}

// NewNodePoolDisruptor creates a NodePoolDisruptor that manages a filler Deployment
// named "xk6-disruptor-filler" in the given namespace (default "default").
func NewNodePoolDisruptor(
	_ context.Context,
	k8s kubernetes.Kubernetes,
	namespace string,
) (NodePoolDisruptor, error) {
	if namespace == "" {
		namespace = "default"
	}

	return &nodePoolDisruptor{
		helper:    k8s.WorkloadHelper(),
		namespace: namespace,
		name:      "xk6-disruptor-filler",
	}, nil
}

// Targets returns the filler Deployment's ref, "Deployment/<namespace>/<name>".
func (d *nodePoolDisruptor) Targets(_ context.Context) ([]string, error) {
	return []string{fmt.Sprintf("Deployment/%s/%s", d.namespace, d.name)}, nil
}

// TargetIPs is not meaningful for a node-pool disruptor. Returns an empty slice.
func (d *nodePoolDisruptor) TargetIPs(_ context.Context) ([]string, error) {
	return []string{}, nil
}

// Cleanup deletes the filler Deployment if it exists. Safe to call even if no
// filler is currently running.
func (d *nodePoolDisruptor) Cleanup(ctx context.Context) error {
	return d.helper.DeleteFiller(ctx, d.namespace, d.name)
}

// InjectFillerWorkload creates the filler Deployment, waits for duration (or context
// cancellation), then deletes it.
func (d *nodePoolDisruptor) InjectFillerWorkload(
	ctx context.Context,
	fault FillerWorkloadFault,
	duration time.Duration,
) error {
	if err := fault.Validate(); err != nil {
		return err
	}

	spec := helpers.FillerSpec{
		Name:              d.name,
		Namespace:         d.namespace,
		Replicas:          fault.Replicas,
		CPU:               fault.CPUPerReplica,
		Memory:            fault.MemoryPerReplica,
		NodeSelector:      fault.NodeSelector,
		PriorityClassName: fault.PriorityClassName,
	}
	if err := d.helper.CreateFiller(ctx, spec); err != nil {
		return err
	}

	waitOrCancel(ctx, duration)

	// Always clean up using a background context so an already-cancelled ctx does
	// not leak the filler Deployment. The caller can still call Cleanup() as a safety net.
	return d.helper.DeleteFiller(context.Background(), d.namespace, d.name) //nolint:contextcheck
}
