package commands

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/danhngo-lx/xk6-disruptor/pkg/agent"
	agentdns "github.com/danhngo-lx/xk6-disruptor/pkg/agent/dns"
	"github.com/danhngo-lx/xk6-disruptor/pkg/iptables"
	"github.com/danhngo-lx/xk6-disruptor/pkg/runtime"
	"github.com/spf13/cobra"
)

const (
	dnsProxyPort  = 5353
	dnsTargetPort = 53
)

// dnsDisruptor wraps the DNS proxy with iptables redirection and implements agent.Disruptor
type dnsDisruptor struct {
	proxy    *agentdns.Proxy
	ipt      iptables.Iptables
	duration time.Duration
}

func (d *dnsDisruptor) Apply(ctx context.Context, duration time.Duration) error {
	// Redirect DNS traffic (port 53) to the proxy (port 5353)
	ruleset := iptables.NewRuleSet(d.ipt)
	//nolint:errcheck
	defer ruleset.Remove()

	rules := []iptables.Rule{
		{
			Table: "nat",
			Chain: "OUTPUT",
			Args:  fmt.Sprintf("-p udp --dport %d -j REDIRECT --to-port %d", dnsTargetPort, dnsProxyPort),
		},
		{
			Table: "nat",
			Chain: "PREROUTING",
			Args:  fmt.Sprintf("-p udp --dport %d -j REDIRECT --to-port %d", dnsTargetPort, dnsProxyPort),
		},
	}

	for _, rule := range rules {
		if err := ruleset.Add(rule); err != nil {
			return fmt.Errorf("setting up DNS redirect: %w", err)
		}
	}

	proxyCtx, cancel := context.WithTimeout(ctx, duration)
	defer cancel()

	return d.proxy.Start(proxyCtx)
}

// BuildDNSCmd returns a cobra command for injecting DNS faults
func BuildDNSCmd(env runtime.Environment, config *agent.Config) *cobra.Command {
	var duration time.Duration
	var errorRate float32
	var upstreamDNS string
	var spoof []string

	cmd := &cobra.Command{
		Use:   "dns",
		Short: "DNS fault injector",
		Long: "Intercepts DNS queries and injects faults: returns NXDOMAIN for a fraction of requests " +
			"or substitutes spoofed IP addresses for specified domains. " +
			"Requires NET_ADMIN capability.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			a, err := agent.Start(env, config)
			if err != nil {
				return fmt.Errorf("initializing agent: %w", err)
			}
			defer a.Stop()

			spoofMap, err := parseSpoofEntries(spoof)
			if err != nil {
				return err
			}

			listenAddr := net.JoinHostPort("127.0.0.1", fmt.Sprint(dnsProxyPort))
			proxy, err := agentdns.NewProxy(agentdns.Config{
				ListenAddr:   listenAddr,
				UpstreamAddr: upstreamDNS,
				ErrorRate:    errorRate,
				Spoof:        spoofMap,
			})
			if err != nil {
				return err
			}

			disruptor := &dnsDisruptor{
				proxy:    proxy,
				ipt:      iptables.New(env.Executor()),
				duration: duration,
			}

			return a.ApplyDisruption(cmd.Context(), disruptor, duration)
		},
	}

	cmd.Flags().DurationVarP(&duration, "duration", "d", 0, "duration of the disruption")
	cmd.Flags().Float32VarP(&errorRate, "error-rate", "r", 0,
		"fraction (0.0-1.0) of DNS queries that will return NXDOMAIN")
	cmd.Flags().StringVar(&upstreamDNS, "upstream", "8.8.8.8:53",
		"upstream DNS server to forward non-faulted queries to")
	cmd.Flags().StringArrayVar(&spoof, "spoof", []string{},
		"domain=ip mapping to spoof (e.g. example.com=1.2.3.4, can be specified multiple times)")

	return cmd
}

// parseSpoofEntries parses "domain=ip" strings into a map with FQDN keys
func parseSpoofEntries(entries []string) (map[string]string, error) {
	result := make(map[string]string, len(entries))

	for _, entry := range entries {
		for i, ch := range entry {
			if ch == '=' {
				domain := entry[:i]
				ip := entry[i+1:]

				if domain == "" || ip == "" {
					return nil, fmt.Errorf("invalid spoof entry %q: expected domain=ip", entry)
				}

				// Ensure FQDN with trailing dot
				if domain[len(domain)-1] != '.' {
					domain += "."
				}

				result[domain] = ip

				goto next
			}
		}

		return nil, fmt.Errorf("invalid spoof entry %q: expected domain=ip", entry)

	next:
	}

	return result, nil
}
