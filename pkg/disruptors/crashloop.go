package disruptors

import (
	"context"
	"time"
)

// CrashLoopFaultInjector defines the interface for injecting crash loop faults into pods
type CrashLoopFaultInjector interface {
	InjectCrashLoopFault(ctx context.Context, fault CrashLoopFault, duration time.Duration) error
}

// CrashLoopFault specifies a crash loop fault to be injected into a pod's container.
// The fault repeatedly kills the target container's main process (PID 1), causing
// Kubernetes to restart it. After enough restarts the pod enters CrashLoopBackOff.
type CrashLoopFault struct {
	// Container is the name of the container whose main process will be killed.
	Container string `js:"container"`
	// Count is the maximum number of times to kill the container.
	// When 0 (default) the container is killed repeatedly for the full duration.
	Count int `js:"count"`
}
