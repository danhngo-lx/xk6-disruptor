package disruptors

import (
	"context"
	"fmt"
	"time"
)

// ReplicaChangeFault specifies a replica scaling change to apply to one or more
// Deployment / StatefulSet workloads.
//
// Exactly one of Replicas, Delta, or Percentage must be set:
//   - Replicas: absolute target (e.g. 0 scales to zero)
//   - Delta:    relative change (e.g. -2 removes two replicas; floored at 0)
//   - Percentage: percent of current (e.g. 50 halves the count; floor rounding, min 0)
//
// When AutoRevert is true the fault sleeps for Duration after applying the change
// and then restores the original replica count. When AutoRevert is false the change
// is applied and the call returns immediately; the original count is still recorded
// on the disruptor so a later Cleanup() restores it.
type ReplicaChangeFault struct {
	// Replicas is the absolute target replica count. Pointer so zero is distinguishable
	// from unset.
	Replicas *int32 `js:"replicas"`
	// Delta is the relative change in replicas (may be negative).
	Delta *int32 `js:"delta"`
	// Percentage of current replicas to target, expressed as 0-N (e.g. 50 == 50%).
	Percentage *int32 `js:"percentage"`
	// AutoRevert restores the original replica count after Duration when true.
	AutoRevert bool `js:"autoRevert"`
}

// Validate checks that exactly one of Replicas/Delta/Percentage is set and that
// Duration is provided when AutoRevert is true.
func (f ReplicaChangeFault) Validate(duration time.Duration) error {
	set := 0
	if f.Replicas != nil {
		set++
	}
	if f.Delta != nil {
		set++
	}
	if f.Percentage != nil {
		set++
	}
	if set != 1 {
		return fmt.Errorf("exactly one of replicas, delta, or percentage must be set (got %d)", set)
	}

	if f.Percentage != nil && *f.Percentage < 0 {
		return fmt.Errorf("percentage must be >= 0 (got %d)", *f.Percentage)
	}

	if f.AutoRevert && duration <= 0 {
		return fmt.Errorf("duration must be > 0 when autoRevert is true")
	}

	return nil
}

// Resolve computes the target replica count given the current replica count.
// Negative results are clamped to 0.
func (f ReplicaChangeFault) Resolve(current int32) int32 {
	var target int32
	switch {
	case f.Replicas != nil:
		target = *f.Replicas
	case f.Delta != nil:
		target = current + *f.Delta
	case f.Percentage != nil:
		target = int32(int64(current) * int64(*f.Percentage) / 100)
	}
	if target < 0 {
		target = 0
	}
	return target
}

// ReplicaChangeFaultInjector defines the interface for changing replica counts.
type ReplicaChangeFaultInjector interface {
	ScaleReplicas(ctx context.Context, fault ReplicaChangeFault, duration time.Duration) error
}
