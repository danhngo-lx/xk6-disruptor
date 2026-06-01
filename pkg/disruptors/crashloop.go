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
// The fault kills all processes in the target container (using kill -9 -1), causing
// Kubernetes to restart it. After enough restarts the pod enters CrashLoopBackOff.
// This approach works even when containers use init systems like tini or dumb-init.
type CrashLoopFault struct {
	// Container is the name of the container whose processes will be killed.
	Container string `js:"container"`
	// Count is the maximum number of times to kill the container.
	// When 0 (default) the container is killed repeatedly for the full duration.
	Count int `js:"count"`
	// RestartTimeout is the maximum time to wait for a container to restart after kill.
	// Defaults to 30 seconds if not specified or set to 0.
	RestartTimeout time.Duration `js:"restartTimeout"`
}
