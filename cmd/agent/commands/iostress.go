package commands

import (
	"fmt"
	"time"

	"github.com/danhngo-lx/xk6-disruptor/pkg/agent"
	"github.com/danhngo-lx/xk6-disruptor/pkg/agent/stressors"
	"github.com/danhngo-lx/xk6-disruptor/pkg/runtime"
	"github.com/spf13/cobra"
)

// BuildIOStressCmd returns a cobra command for stressing disk I/O
func BuildIOStressCmd(env runtime.Environment, config *agent.Config) *cobra.Command {
	var duration time.Duration
	var disruption stressors.IODisruption

	cmd := &cobra.Command{
		Use:   "io-stress",
		Short: "disk I/O stressor",
		Long: "Runs parallel workers that continuously write and read a working-set file to create " +
			"sustained I/O pressure. Simulates the 'noisy neighbour' disk scenario where multiple " +
			"workloads share the same storage backend (e.g. a shared storage pool).",
		RunE: func(cmd *cobra.Command, _ []string) error {
			a, err := agent.Start(env, config)
			if err != nil {
				return fmt.Errorf("initializing agent: %w", err)
			}
			defer a.Stop()

			s, err := stressors.NewIOStressor(disruption)
			if err != nil {
				return err
			}

			return s.Apply(cmd.Context(), duration)
		},
	}

	cmd.Flags().DurationVarP(&duration, "duration", "d", 0, "duration of the disruption")
	cmd.Flags().StringVar(&disruption.Path, "path", "/tmp",
		"directory to create the I/O working-set files in")
	cmd.Flags().IntVar(&disruption.Workers, "workers", 0,
		"number of parallel I/O workers (default 4)")
	cmd.Flags().Int64Var(&disruption.BytesPerWorker, "bytes-per-worker", 0,
		"working-set file size per worker in bytes (default 1048576 = 1 MiB)")

	return cmd
}
