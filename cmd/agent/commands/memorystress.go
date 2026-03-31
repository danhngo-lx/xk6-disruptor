package commands

import (
	"fmt"
	"time"

	"github.com/danhngo-lx/xk6-disruptor/pkg/agent"
	"github.com/danhngo-lx/xk6-disruptor/pkg/agent/stressors"
	"github.com/danhngo-lx/xk6-disruptor/pkg/runtime"
	"github.com/spf13/cobra"
)

// BuildMemoryStressCmd returns a cobra command with the specification of the memory-stress command
func BuildMemoryStressCmd(env runtime.Environment, config *agent.Config) *cobra.Command {
	var duration time.Duration
	var disruption stressors.MemoryDisruption

	cmd := &cobra.Command{
		Use:   "memory-stress",
		Short: "memory pressure stressor",
		Long:  "Allocates and holds memory to simulate memory pressure",
		RunE: func(cmd *cobra.Command, _ []string) error {
			agent, err := agent.Start(env, config)
			if err != nil {
				return fmt.Errorf("initializing agent: %w", err)
			}
			defer agent.Stop()

			s, err := stressors.NewMemoryStressor(disruption)
			if err != nil {
				return err
			}

			return s.Apply(cmd.Context(), duration)
		},
	}

	cmd.Flags().DurationVarP(&duration, "duration", "d", 0, "duration of the disruption")
	cmd.Flags().Int64Var(&disruption.Bytes, "bytes", 0, "number of bytes to allocate and hold")

	return cmd
}
