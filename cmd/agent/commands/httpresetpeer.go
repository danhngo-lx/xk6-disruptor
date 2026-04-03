package commands

import (
	"fmt"
	"net"
	"time"

	"github.com/danhngo-lx/xk6-disruptor/pkg/agent"
	"github.com/danhngo-lx/xk6-disruptor/pkg/agent/protocol"
	agenthttp "github.com/danhngo-lx/xk6-disruptor/pkg/agent/protocol/http"
	"github.com/danhngo-lx/xk6-disruptor/pkg/iptables"
	"github.com/danhngo-lx/xk6-disruptor/pkg/runtime"
	"github.com/spf13/cobra"
)

// BuildHTTPResetPeerCmd returns a cobra command for the http-reset-peer disruptor.
func BuildHTTPResetPeerCmd(env runtime.Environment, config *agent.Config) *cobra.Command {
	disruption := agenthttp.ResetPeerDisruption{}
	var duration time.Duration
	var port uint
	var targetPort uint
	var upstreamHost string
	transparent := true

	cmd := &cobra.Command{
		Use:   "http-reset-peer",
		Short: "HTTP TCP reset peer injector",
		Long: "Intercepts TCP connections on the target port and abruptly resets them by sending a RST " +
			"packet (SO_LINGER=0). A configurable fraction of connections (toxicity) are reset; the " +
			"remainder are transparently proxied to the upstream. This simulates flaky network " +
			"conditions at the TCP layer. Requires NET_ADMIN capability.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if targetPort == 0 {
				return fmt.Errorf("target port for fault injection is required")
			}

			a, err := agent.Start(env, config)
			if err != nil {
				return fmt.Errorf("initializing agent: %w", err)
			}
			defer a.Stop()

			listenAddress := net.JoinHostPort("", fmt.Sprint(port))
			upstreamAddress := net.JoinHostPort(upstreamHost, fmt.Sprint(targetPort))

			listener, err := net.Listen("tcp", listenAddress)
			if err != nil {
				return fmt.Errorf("setting up listener at %q: %w", listenAddress, err)
			}

			proxy, err := agenthttp.NewResetPeerProxy(listener, upstreamAddress, disruption)
			if err != nil {
				return err
			}

			var redirector protocol.TrafficRedirector
			if transparent {
				tr := &protocol.TrafficRedirectionSpec{
					DestinationPort: targetPort,
					RedirectPort:    port,
				}

				redirector, err = protocol.NewTrafficRedirector(tr, iptables.New(env.Executor()))
				if err != nil {
					return err
				}
			} else {
				redirector = protocol.NoopTrafficRedirector()
			}

			disruptor, err := protocol.NewDisruptor(env.Executor(), proxy, redirector)
			if err != nil {
				return err
			}

			return a.ApplyDisruption(cmd.Context(), disruptor, duration)
		},
	}

	cmd.Flags().DurationVarP(&duration, "duration", "d", 0, "duration of the disruption")
	cmd.Flags().UintVarP(&targetPort, "target", "t", 0, "port of the target application (required)")
	cmd.Flags().UintVarP(&port, "port", "p", 8000, "port the proxy will listen on")
	cmd.Flags().StringVar(&upstreamHost, "upstream-host", "localhost", "upstream host to forward non-reset connections to")
	cmd.Flags().DurationVar(&disruption.ResetTimeout, "reset-timeout", 0,
		"how long to wait after accepting a connection before sending the RST (0 = immediately)")
	cmd.Flags().Float32Var(&disruption.Toxicity, "toxicity", 1.0,
		"fraction of connections to reset (0.0–1.0, default 1.0 = all)")
	cmd.Flags().BoolVar(&transparent, "transparent", true,
		"redirect traffic via iptables (transparent mode); set false when running alongside a service mesh")

	return cmd
}
