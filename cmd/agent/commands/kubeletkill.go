package commands

import (
	"fmt"
	"os/exec"
	"time"

	"github.com/danhngo-lx/xk6-disruptor/pkg/agent"
	"github.com/danhngo-lx/xk6-disruptor/pkg/runtime"
	"github.com/spf13/cobra"
)

// BuildKubeletKillCmd returns a cobra command that stops and restarts the kubelet service.
// It must run inside a privileged pod with hostPID=true so that nsenter can reach PID 1
// on the host and access the host's systemd mount namespace.
//
// The stop, sleep, and start all happen within a single process invocation so that k6
// never needs to re-connect to the pod after the kubelet has stopped (which would break
// the exec SPDY channel).
func BuildKubeletKillCmd(env runtime.Environment, config *agent.Config) *cobra.Command {
	var duration time.Duration

	cmd := &cobra.Command{
		Use:   "kubelet-kill",
		Short: "kubelet service kill",
		Long: "Stops the kubelet systemd service on the host node for a given duration, " +
			"then restarts it. Must run in a privileged container with hostPID=true. " +
			"Uses nsenter to access the host's systemd mount namespace via PID 1.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			a, err := agent.Start(env, config)
			if err != nil {
				return fmt.Errorf("initializing agent: %w", err)
			}
			defer a.Stop()

			if err := nsenterSystemctl(cmd, "stop", "kubelet"); err != nil {
				return fmt.Errorf("stopping kubelet: %w", err)
			}

			timer := time.NewTimer(duration)
			defer timer.Stop()
			select {
			case <-timer.C:
			case <-cmd.Context().Done():
			}

			if err := nsenterSystemctl(cmd, "start", "kubelet"); err != nil {
				return fmt.Errorf("restarting kubelet: %w", err)
			}

			return nil
		},
	}

	cmd.Flags().DurationVarP(&duration, "duration", "d", 0, "duration to keep the kubelet stopped")

	return cmd
}

// nsenterSystemctl runs "systemctl <action> <unit>" inside the host's systemd mount namespace
// by entering PID 1's mount namespace via nsenter.
func nsenterSystemctl(cmd *cobra.Command, action, unit string) error {
	out, err := exec.CommandContext(
		cmd.Context(),
		"nsenter", "--target", "1", "--mount", "--",
		"systemctl", action, unit,
	).CombinedOutput()
	if err != nil {
		return fmt.Errorf("nsenter systemctl %s %s: %w\noutput: %s", action, unit, err, string(out))
	}
	return nil
}
