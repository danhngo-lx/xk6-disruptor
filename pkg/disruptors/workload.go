package disruptors

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/danhngo-lx/xk6-disruptor/pkg/kubernetes"
	"github.com/danhngo-lx/xk6-disruptor/pkg/kubernetes/helpers"
)

// WorkloadDisruptor defines the methods for injecting chaos at the workload level
// (Deployment / StatefulSet replica scaling).
type WorkloadDisruptor interface {
	Disruptor
	ReplicaChangeFaultInjector
}

// workloadDisruptor is the concrete implementation of WorkloadDisruptor.
type workloadDisruptor struct {
	selector *WorkloadSelector
	helper   helpers.WorkloadHelper

	mu        sync.Mutex
	originals map[helpers.WorkloadRef]int32
}

// NewWorkloadDisruptor creates a WorkloadDisruptor that targets Deployments or StatefulSets.
func NewWorkloadDisruptor(
	ctx context.Context,
	k8s kubernetes.Kubernetes,
	spec WorkloadSelectorSpec,
) (WorkloadDisruptor, error) {
	helper := k8s.WorkloadHelper()
	selector, err := NewWorkloadSelector(spec, helper)
	if err != nil {
		return nil, err
	}

	// Eagerly resolve to fail fast if the selector matches nothing.
	if _, err := selector.Targets(ctx); err != nil {
		return nil, err
	}

	return &workloadDisruptor{
		selector:  selector,
		helper:    helper,
		originals: make(map[helpers.WorkloadRef]int32),
	}, nil
}

// Targets returns the names of the targeted workloads as "Kind/namespace/name".
func (d *workloadDisruptor) Targets(ctx context.Context) ([]string, error) {
	refs, err := d.selector.Targets(ctx)
	if err != nil {
		return nil, err
	}
	names := make([]string, len(refs))
	for i, r := range refs {
		names[i] = r.String()
	}
	return names, nil
}

// TargetIPs is not meaningful for workloads. Returns an empty slice.
func (d *workloadDisruptor) TargetIPs(_ context.Context) ([]string, error) {
	return []string{}, nil
}

// Cleanup restores every workload this disruptor has scaled back to its original
// replica count. Safe to call multiple times; restored workloads are cleared from
// the recorded originals.
func (d *workloadDisruptor) Cleanup(ctx context.Context) error {
	return d.revertAll(ctx)
}

// ScaleReplicas applies the replica change to each targeted workload. When
// fault.AutoRevert is true the call sleeps for duration then restores each
// workload to its original replica count before returning.
func (d *workloadDisruptor) ScaleReplicas(
	ctx context.Context,
	fault ReplicaChangeFault,
	duration time.Duration,
) error {
	if err := fault.Validate(duration); err != nil {
		return err
	}

	refs, err := d.selector.Targets(ctx)
	if err != nil {
		return err
	}

	type result struct {
		ref helpers.WorkloadRef
		err error
	}
	results := make(chan result, len(refs))

	for _, ref := range refs {
		ref := ref
		go func() {
			results <- result{ref: ref, err: d.applyScale(ctx, ref, fault)}
		}()
	}

	var errs []error
	for range refs {
		r := <-results
		if r.err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", r.ref, r.err))
		}
	}
	if err := errors.Join(errs...); err != nil {
		return err
	}

	if !fault.AutoRevert {
		return nil
	}

	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-ctx.Done():
	}

	// Restore using a background context so an already-cancelled ctx does not
	// prevent revert. The caller can still call Cleanup() as a safety net.
	return d.revertAll(context.Background()) //nolint:contextcheck
}

// applyScale records the original replica count (if not already recorded) and
// patches the workload to the resolved target count.
func (d *workloadDisruptor) applyScale(
	ctx context.Context,
	ref helpers.WorkloadRef,
	fault ReplicaChangeFault,
) error {
	current, err := d.helper.GetReplicas(ctx, ref)
	if err != nil {
		return fmt.Errorf("reading replicas: %w", err)
	}

	d.mu.Lock()
	if _, recorded := d.originals[ref]; !recorded {
		d.originals[ref] = current
	}
	d.mu.Unlock()

	target := fault.Resolve(current)
	if target == current {
		return nil
	}
	return d.helper.Scale(ctx, ref, target)
}

// revertAll restores every recorded workload to its original replica count.
func (d *workloadDisruptor) revertAll(ctx context.Context) error {
	d.mu.Lock()
	pending := make(map[helpers.WorkloadRef]int32, len(d.originals))
	for k, v := range d.originals {
		pending[k] = v
	}
	d.mu.Unlock()

	var errs []error
	for ref, replicas := range pending {
		if err := d.helper.Scale(ctx, ref, replicas); err != nil {
			errs = append(errs, fmt.Errorf("restoring %s to %d: %w", ref, replicas, err))
			continue
		}
		d.mu.Lock()
		delete(d.originals, ref)
		d.mu.Unlock()
	}
	return errors.Join(errs...)
}
