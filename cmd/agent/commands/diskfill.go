package commands

import (
	"fmt"
	"time"

	"github.com/danhngo-lx/xk6-disruptor/pkg/agent"
	"github.com/danhngo-lx/xk6-disruptor/pkg/agent/stressors"
	"github.com/danhngo-lx/xk6-disruptor/pkg/runtime"
	"github.com/spf13/cobra"
)

// BuildDiskFillCmd returns a cobra command for filling disk space
func BuildDiskFillCmd(env runtime.Environment, config *agent.Config) *cobra.Command {
	var duration time.Duration
	var disruption stressors.DiskFillDisruption

	cmd := &cobra.Command{
		Use:   "disk-fill",
		Short: "disk fill stressor",
		Long: "Writes a large file to the target path to consume disk/ephemeral storage quota. " +
			"The file is held for the specified duration and deleted on cleanup. " +
			"If the written amount exceeds the pod's ephemeral-storage limit, the kubelet will evict the pod.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			a, err := agent.Start(env, config)
			if err != nil {
				return fmt.Errorf("initializing agent: %w", err)
			}
			defer a.Stop()

			s, err := stressors.NewDiskFillStressor(disruption)
			if err != nil {
				return err
			}

			return s.Apply(cmd.Context(), duration)
		},
	}

	cmd.Flags().DurationVarP(&duration, "duration", "d", 0, "duration of the disruption")
	cmd.Flags().Int64Var(&disruption.Bytes, "bytes", 0,
		"number of bytes to write (required)")
	cmd.Flags().StringVar(&disruption.Path, "path", "/tmp",
		"directory to write the fill file into")
	cmd.Flags().Int64Var(&disruption.BlockSize, "block-size", 0,
		"write block size in bytes (default 262144 = 256 KiB)")

	return cmd
}
