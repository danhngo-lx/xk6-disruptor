package network

import (
	"context"
	"fmt"
	"time"

	"github.com/grafana/xk6-disruptor/pkg/iptables"
)

// PartitionDirection defines which traffic direction to block
type PartitionDirection string

const (
	// PartitionIngress blocks incoming traffic from the specified hosts
	PartitionIngress PartitionDirection = "ingress"
	// PartitionEgress blocks outgoing traffic to the specified hosts
	PartitionEgress PartitionDirection = "egress"
	// PartitionBoth blocks both incoming and outgoing traffic to/from the specified hosts
	PartitionBoth PartitionDirection = "both"
)

// PartitionConfig defines the configuration for a network partition disruption
type PartitionConfig struct {
	// Direction of traffic to block: ingress, egress, or both
	Direction PartitionDirection
	// Hosts is a list of CIDRs or IP addresses to block traffic to/from
	Hosts []string
}

// PartitionDisruptor blocks traffic between the pod and a set of specified hosts using iptables
type PartitionDisruptor struct {
	Iptables iptables.Iptables
	Config   PartitionConfig
}

// Apply applies the network partition for the given duration
func (d PartitionDisruptor) Apply(ctx context.Context, duration time.Duration) error {
	if len(d.Config.Hosts) == 0 {
		return fmt.Errorf("at least one host must be specified")
	}

	if duration < time.Second {
		return ErrDurationTooShort
	}

	ruleset := iptables.NewRuleSet(d.Iptables)
	//nolint:errcheck
	defer ruleset.Remove()

	for _, host := range d.Config.Hosts {
		switch d.Config.Direction {
		case PartitionIngress:
			if err := ruleset.Add(iptables.Rule{
				Table: "filter",
				Chain: "INPUT",
				Args:  fmt.Sprintf("-s %s -j DROP", host),
			}); err != nil {
				return err
			}

		case PartitionEgress:
			if err := ruleset.Add(iptables.Rule{
				Table: "filter",
				Chain: "OUTPUT",
				Args:  fmt.Sprintf("-d %s -j DROP", host),
			}); err != nil {
				return err
			}

		case PartitionBoth, "":
			if err := ruleset.Add(iptables.Rule{
				Table: "filter",
				Chain: "INPUT",
				Args:  fmt.Sprintf("-s %s -j DROP", host),
			}); err != nil {
				return err
			}

			if err := ruleset.Add(iptables.Rule{
				Table: "filter",
				Chain: "OUTPUT",
				Args:  fmt.Sprintf("-d %s -j DROP", host),
			}); err != nil {
				return err
			}

		default:
			return fmt.Errorf("invalid direction %q: must be ingress, egress, or both", d.Config.Direction)
		}
	}

	ctx, cancel := context.WithTimeout(ctx, duration)
	defer cancel()

	select {
	case <-time.After(duration):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
