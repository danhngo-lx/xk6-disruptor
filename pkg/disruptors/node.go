package disruptors

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/danhngo-lx/xk6-disruptor/pkg/internal/version"
	"github.com/danhngo-lx/xk6-disruptor/pkg/kubernetes"
	"github.com/danhngo-lx/xk6-disruptor/pkg/kubernetes/helpers"
	corev1 "k8s.io/api/core/v1"
)

// NodeDrainFault specifies a node drain operation
type NodeDrainFault struct {
	// SkipDaemonSets controls whether DaemonSet-owned pods are skipped during eviction
	SkipDaemonSets bool `js:"skipDaemonSets"`
	// DeleteLocalData controls whether pods with emptyDir/hostPath volumes are evicted
	DeleteLocalData bool `js:"deleteLocalData"`
	// Timeout is the per-pod eviction timeout
	Timeout time.Duration `js:"timeout"`
}

// NodeTaintFault specifies a taint to add to a node
type NodeTaintFault struct {
	// Key is the taint key
	Key string `js:"key"`
	// Value is the taint value
	Value string `js:"value"`
	// Effect is the taint effect: "NoSchedule", "PreferNoSchedule", or "NoExecute"
	Effect string `js:"effect"`
}

// NodeAttributes defines label-based criteria for selecting nodes
type NodeAttributes struct {
	Labels map[string]string
}

// NodeDisruptorOptions defines options that control NodeDisruptor behaviour
type NodeDisruptorOptions struct {
	// AgentImage overrides the container image used for the privileged helper pod.
	// When empty the image is resolved the same way as PodDisruptor (env var or build default).
	AgentImage string `js:"agentImage"`
	// AgentNamespace is the namespace where privileged helper pods are created.
	// Defaults to "kube-system".
	AgentNamespace string `js:"agentNamespace"`
	// InjectTimeout is the time budget for the helper pod to start before an operation
	// is considered failed. Defaults to 30s.
	InjectTimeout time.Duration `js:"injectTimeout"`
}

// NodeDisruptor defines the methods for injecting chaos at the node level
type NodeDisruptor interface {
	Disruptor
	// Drain cordons the node, evicts all eligible pods, waits for duration, then uncordons
	Drain(ctx context.Context, fault NodeDrainFault, duration time.Duration) error
	// TaintNode adds a taint to the node, waits for duration, then removes it
	TaintNode(ctx context.Context, fault NodeTaintFault, duration time.Duration) error
	// InjectCPUStress runs a CPU stressor at node level for the given duration via a privileged pod
	InjectCPUStress(ctx context.Context, fault CPUStressFault, duration time.Duration) error
	// InjectMemoryStress runs a memory stressor at node level for the given duration via a privileged pod
	InjectMemoryStress(ctx context.Context, fault MemoryStressFault, duration time.Duration) error
	// InjectIOStress runs an IO stressor at node level for the given duration via a privileged pod
	InjectIOStress(ctx context.Context, fault IOStressFault, duration time.Duration) error
	// InjectKubeletServiceKill stops the kubelet service for the given duration then restarts it
	InjectKubeletServiceKill(ctx context.Context, duration time.Duration) error
}

// nodeDisruptor is the concrete implementation of NodeDisruptor
type nodeDisruptor struct {
	nodes   []corev1.Node
	helper  helpers.NodeHelper
	options NodeDisruptorOptions
}

// NewNodeDisruptor creates a new NodeDisruptor targeting nodes identified by name or label selector.
// Exactly one of name or labelSelector must be non-empty.
func NewNodeDisruptor(
	ctx context.Context,
	k8s kubernetes.Kubernetes,
	name string,
	labelSelector map[string]string,
	options NodeDisruptorOptions,
) (NodeDisruptor, error) {
	helper := k8s.NodeHelper()

	var (
		nodes []corev1.Node
		err   error
	)

	switch {
	case name != "" && len(labelSelector) > 0:
		return nil, fmt.Errorf("provide either name or select.labels, not both")
	case name != "":
		node, getErr := helper.Get(ctx, name)
		if getErr != nil {
			return nil, fmt.Errorf("getting node %q: %w", name, getErr)
		}
		nodes = []corev1.Node{node}
	case len(labelSelector) > 0:
		nodes, err = helper.List(ctx, labelSelector)
		if err != nil {
			return nil, fmt.Errorf("listing nodes: %w", err)
		}
		if len(nodes) == 0 {
			return nil, fmt.Errorf("no nodes matched selector %v", labelSelector)
		}
	default:
		return nil, fmt.Errorf("either name or select.labels must be provided")
	}

	return &nodeDisruptor{
		nodes:   nodes,
		helper:  helper,
		options: options,
	}, nil
}

// Targets returns the names of targeted nodes
func (d *nodeDisruptor) Targets(_ context.Context) ([]string, error) {
	names := make([]string, len(d.nodes))
	for i, n := range d.nodes {
		names[i] = n.Name
	}
	return names, nil
}

// TargetIPs returns the internal IP addresses of targeted nodes
func (d *nodeDisruptor) TargetIPs(_ context.Context) ([]string, error) {
	ips := make([]string, 0, len(d.nodes))
	for _, n := range d.nodes {
		for _, addr := range n.Status.Addresses {
			if addr.Type == corev1.NodeInternalIP {
				ips = append(ips, addr.Address)
				break
			}
		}
	}
	return ips, nil
}

// Cleanup is a no-op for NodeDisruptor. Privileged pods are deleted inline after each fault.
// For drain/taint, the caller is responsible for calling the disruptor method — it always
// restores the node state before returning.
func (d *nodeDisruptor) Cleanup(_ context.Context) error {
	return nil
}

// agentImage returns the container image to use for privileged helper pods
func (d *nodeDisruptor) agentImage() string {
	if d.options.AgentImage != "" {
		return d.options.AgentImage
	}
	return version.AgentImage()
}

// agentNamespace returns the namespace where helper pods are created
func (d *nodeDisruptor) agentNamespace() string {
	if d.options.AgentNamespace != "" {
		return d.options.AgentNamespace
	}
	return "kube-system"
}

// injectTimeout returns the timeout budget for a helper pod to reach Running
func (d *nodeDisruptor) injectTimeout() time.Duration {
	if d.options.InjectTimeout > 0 {
		return d.options.InjectTimeout
	}
	return 30 * time.Second
}

// runPrivilegedPod creates a privileged pod on nodeName, waits for it to complete, then deletes it.
// args are passed as the container Args (the Command is always "xk6-disruptor-agent").
func (d *nodeDisruptor) runPrivilegedPod(
	ctx context.Context,
	nodeName string,
	podName string,
	args []string,
	duration time.Duration,
) error {
	namespace := d.agentNamespace()
	spec := helpers.PrivilegedPodSpec{
		Name:      podName,
		Namespace: namespace,
		NodeName:  nodeName,
		Image:     d.agentImage(),
		Command:   []string{"xk6-disruptor-agent"},
		Args:      args,
	}

	if _, err := d.helper.CreatePrivilegedPod(ctx, spec); err != nil {
		return fmt.Errorf("creating privileged pod: %w", err)
	}

	defer func() {
		// Always clean up the helper pod, even if the context is already done
		_ = d.helper.DeletePod(context.Background(), namespace, podName) //nolint:contextcheck
	}()

	// Give the pod some extra time beyond the fault duration for scheduling + startup
	timeout := duration + d.injectTimeout() + 30*time.Second
	return d.helper.WaitPodCompleted(ctx, namespace, podName, timeout)
}

// forEachNode runs fn concurrently on all targeted nodes and collects errors
func (d *nodeDisruptor) forEachNode(fn func(node corev1.Node) error) error {
	type result struct {
		node string
		err  error
	}
	results := make(chan result, len(d.nodes))

	for _, node := range d.nodes {
		node := node
		go func() {
			results <- result{node: node.Name, err: fn(node)}
		}()
	}

	var errs []error
	for range d.nodes {
		r := <-results
		if r.err != nil {
			errs = append(errs, fmt.Errorf("node %s: %w", r.node, r.err))
		}
	}
	return errors.Join(errs...)
}

// Drain cordons the node, evicts eligible pods, waits for duration, then uncordons
func (d *nodeDisruptor) Drain(ctx context.Context, fault NodeDrainFault, duration time.Duration) error {
	return d.forEachNode(func(node corev1.Node) error {
		if err := d.helper.Cordon(ctx, node.Name); err != nil {
			return fmt.Errorf("cordoning node: %w", err)
		}

		evictOptions := helpers.NodeEvictOptions{
			SkipDaemonSets:  fault.SkipDaemonSets,
			DeleteLocalData: fault.DeleteLocalData,
			Timeout:         fault.Timeout,
		}
		if err := d.helper.EvictPods(ctx, node.Name, evictOptions); err != nil {
			return fmt.Errorf("evicting pods: %w", err)
		}

		timer := time.NewTimer(duration)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-ctx.Done():
		}

		return d.helper.Uncordon(ctx, node.Name)
	})
}

// TaintNode adds a taint to the node, waits for duration, then removes the taint
func (d *nodeDisruptor) TaintNode(ctx context.Context, fault NodeTaintFault, duration time.Duration) error {
	effect := corev1.TaintEffect(fault.Effect)
	if effect == "" {
		effect = corev1.TaintEffectNoSchedule
	}

	taint := corev1.Taint{
		Key:    fault.Key,
		Value:  fault.Value,
		Effect: effect,
	}

	return d.forEachNode(func(node corev1.Node) error {
		if err := d.helper.AddTaint(ctx, node.Name, taint); err != nil {
			return fmt.Errorf("adding taint: %w", err)
		}

		timer := time.NewTimer(duration)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-ctx.Done():
		}

		return d.helper.RemoveTaint(ctx, node.Name, fault.Key)
	})
}

// InjectCPUStress runs an in-node CPU stressor via a privileged helper pod
func (d *nodeDisruptor) InjectCPUStress(ctx context.Context, fault CPUStressFault, duration time.Duration) error {
	return d.forEachNode(func(node corev1.Node) error {
		fullCmd := buildCPUStressCmd(fault, duration)
		podName := fmt.Sprintf("xk6-cpu-stress-%s", node.Name)
		return d.runPrivilegedPod(ctx, node.Name, podName, fullCmd[1:], duration)
	})
}

// InjectMemoryStress runs an in-node memory stressor via a privileged helper pod
func (d *nodeDisruptor) InjectMemoryStress(ctx context.Context, fault MemoryStressFault, duration time.Duration) error {
	return d.forEachNode(func(node corev1.Node) error {
		fullCmd := buildMemoryStressCmd(fault, duration)
		podName := fmt.Sprintf("xk6-mem-stress-%s", node.Name)
		return d.runPrivilegedPod(ctx, node.Name, podName, fullCmd[1:], duration)
	})
}

// InjectIOStress runs an in-node IO stressor via a privileged helper pod
func (d *nodeDisruptor) InjectIOStress(ctx context.Context, fault IOStressFault, duration time.Duration) error {
	return d.forEachNode(func(node corev1.Node) error {
		fullCmd := buildIOStressCmd(fault, duration)
		podName := fmt.Sprintf("xk6-io-stress-%s", node.Name)
		return d.runPrivilegedPod(ctx, node.Name, podName, fullCmd[1:], duration)
	})
}

// InjectKubeletServiceKill stops the kubelet service for the given duration then restarts it.
// It runs a privileged pod (hostPID=true) that uses nsenter to reach the host systemd.
// The stop, wait, and start all happen inside a single long-running exec so no second
// connection to the pod is needed after the kubelet stops.
func (d *nodeDisruptor) InjectKubeletServiceKill(ctx context.Context, duration time.Duration) error {
	return d.forEachNode(func(node corev1.Node) error {
		args := []string{"kubelet-kill", "--duration", duration.String()}
		podName := fmt.Sprintf("xk6-kubelet-kill-%s", node.Name)
		return d.runPrivilegedPod(ctx, node.Name, podName, args, duration)
	})
}
