package commands

import (
	"fmt"
	"time"

	"github.com/danhngo-lx/xk6-disruptor/pkg/agent"
	"github.com/danhngo-lx/xk6-disruptor/pkg/agent/network"
	"github.com/danhngo-lx/xk6-disruptor/pkg/runtime"
	"github.com/spf13/cobra"
)

// BuildNetworkShapeCmd returns a cobra command for shaping network traffic using tc netem
func BuildNetworkShapeCmd(env runtime.Environment, config *agent.Config) *cobra.Command {
	var duration time.Duration
	shapingConfig := network.ShapingConfig{}

	cmd := &cobra.Command{
		Use:   "network-shape",
		Short: "network traffic shaper",
		Long: "Shapes network traffic using tc netem. Supports delay, jitter, packet loss, corruption, " +
			"duplication and rate limiting. Requires NET_ADMIN capability and iproute2.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			a, err := agent.Start(env, config)
			if err != nil {
				return fmt.Errorf("initializing agent: %w", err)
			}
			defer a.Stop()

			shaper, err := network.NewShaper(env.Executor(), shapingConfig)
			if err != nil {
				return err
			}

			return a.ApplyDisruption(cmd.Context(), shaper, duration)
		},
	}

	cmd.Flags().DurationVarP(&duration, "duration", "d", 0, "duration of the disruption")
	cmd.Flags().StringVar(&shapingConfig.Interface, "interface", "eth0", "network interface to shape")
	cmd.Flags().DurationVar(&shapingConfig.Delay, "delay", 0, "average packet delay (e.g. 100ms)")
	cmd.Flags().DurationVar(&shapingConfig.Jitter, "jitter", 0, "delay variation / jitter (e.g. 10ms)")
	cmd.Flags().Float32Var(&shapingConfig.Loss, "loss", 0, "fraction of packets to drop (0.0-1.0)")
	cmd.Flags().Float32Var(&shapingConfig.Corrupt, "corrupt", 0, "fraction of packets to corrupt (0.0-1.0)")
	cmd.Flags().Float32Var(&shapingConfig.Duplicate, "duplicate", 0, "fraction of packets to duplicate (0.0-1.0)")
	cmd.Flags().StringVar(&shapingConfig.Rate, "rate", "", "bandwidth rate limit (e.g. 1mbit, 100kbit)")

	return cmd
}
