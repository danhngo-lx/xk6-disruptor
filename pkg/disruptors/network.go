package disruptors

import (
	"context"
	"time"
)

// NetworkFaultInjector defines the interface for injecting network faults
type NetworkFaultInjector interface {
	InjectNetworkFaults(ctx context.Context, fault NetworkFault, duration time.Duration) error
}

// NetworkFault specifies a network fault to be injected
type NetworkFault struct {
	// Port to target for network disruption (0 means all ports)
	Port uint `js:"port"`
	// Protocol to target for network disruption (tcp, udp, icmp, or empty for all)
	Protocol string `js:"protocol"`
}

// NetworkShapingFaultInjector defines the interface for injecting network shaping faults
type NetworkShapingFaultInjector interface {
	InjectNetworkShapingFaults(ctx context.Context, fault NetworkShapingFault, duration time.Duration) error
}

// NetworkShapingFault specifies traffic shaping parameters to be applied via tc netem
type NetworkShapingFault struct {
	// Network interface to apply shaping on (default: eth0)
	Interface string `js:"interface"`
	// Average packet delay
	Delay time.Duration `js:"delay"`
	// Delay variation (jitter)
	Jitter time.Duration `js:"jitter"`
	// Fraction of packets to drop (0.0-1.0)
	Loss float32 `js:"loss"`
	// Fraction of packets to corrupt (0.0-1.0)
	Corrupt float32 `js:"corrupt"`
	// Fraction of packets to duplicate (0.0-1.0)
	Duplicate float32 `js:"duplicate"`
	// Bandwidth rate limit (e.g. "1mbit", "100kbit")
	Rate string `js:"rate"`
}

// NetworkPartitionFaultInjector defines the interface for injecting network partition faults
type NetworkPartitionFaultInjector interface {
	InjectNetworkPartition(ctx context.Context, fault NetworkPartitionFault, duration time.Duration) error
}

// NetworkPartitionFault specifies a network partition to be applied between the target pod and a set of hosts
type NetworkPartitionFault struct {
	// Direction of traffic to block: ingress, egress, or both
	Direction string `js:"direction"`
	// Hosts is a list of CIDRs or IP addresses to block traffic to/from
	Hosts []string `js:"hosts"`
}
