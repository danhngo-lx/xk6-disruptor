package commands

import (
	"fmt"
	"time"

	"github.com/danhngo-lx/xk6-disruptor/pkg/agent"
	"github.com/danhngo-lx/xk6-disruptor/pkg/agent/network"
	"github.com/danhngo-lx/xk6-disruptor/pkg/iptables"
	"github.com/danhngo-lx/xk6-disruptor/pkg/runtime"
	"github.com/spf13/cobra"
)

// BuildNetworkPartitionCmd returns a cobra command for partitioning network traffic using iptables
func BuildNetworkPartitionCmd(env runtime.Environment, config *agent.Config) *cobra.Command {
	var duration time.Duration
	var direction string
	var hosts []string

	cmd := &cobra.Command{
		Use:   "network-partition",
		Short: "network partition (experimental)",
		Long: "Blocks traffic between the pod and a set of specified hosts (CIDRs or IPs) using iptables. " +
			"Requires NET_ADMIN capability.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			a, err := agent.Start(env, config)
			if err != nil {
				return fmt.Errorf("initializing agent: %w", err)
			}
			defer a.Stop()

			disruptor := network.PartitionDisruptor{
				Iptables: iptables.New(env.Executor()),
				Config: network.PartitionConfig{
					Direction: network.PartitionDirection(direction),
					Hosts:     hosts,
				},
			}

			return a.ApplyDisruption(cmd.Context(), disruptor, duration)
		},
	}

	cmd.Flags().DurationVarP(&duration, "duration", "d", 0, "duration of the disruption")
	cmd.Flags().StringVar(&direction, "direction", "both",
		"direction of traffic to block: ingress, egress, or both")
	cmd.Flags().StringArrayVar(&hosts, "host", []string{},
		"CIDR or IP address to block (can be specified multiple times)")

	return cmd
}
