package disruptors

import "context"

// Disruptor defines the generic interface implemented by all disruptors
type Disruptor interface {
	// Targets returns the names of the targets for the disruptor
	Targets(ctx context.Context) ([]string, error)
	// TargetIPs returns the IP addresses of the targets for the disruptor.
	// Pods that have not yet been assigned an IP are silently omitted.
	TargetIPs(ctx context.Context) ([]string, error)
	// Cleanup stops any running agent processes on the target pods and restores
	// any resources (e.g. iptables rules) they installed. Safe to call even if
	// no agent is running — it is a no-op in that case.
	Cleanup(ctx context.Context) error
}
