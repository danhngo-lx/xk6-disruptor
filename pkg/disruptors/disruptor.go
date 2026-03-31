package disruptors

import "context"

// Disruptor defines the generic interface implemented by all disruptors
type Disruptor interface {
	// Targets returns the names of the targets for the disruptor
	Targets(ctx context.Context) ([]string, error)
	// TargetIPs returns the IP addresses of the targets for the disruptor.
	// Pods that have not yet been assigned an IP are silently omitted.
	TargetIPs(ctx context.Context) ([]string, error)
}
