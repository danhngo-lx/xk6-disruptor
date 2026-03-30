package disruptors

import (
	"context"
	"time"
)

// DNSFaultInjector defines the interface for injecting DNS faults
type DNSFaultInjector interface {
	InjectDNSFaults(ctx context.Context, fault DNSFault, duration time.Duration) error
}

// DNSFault specifies faults to be injected into DNS queries
type DNSFault struct {
	// ErrorRate is the fraction (0.0-1.0) of DNS queries that will return NXDOMAIN
	ErrorRate float32 `js:"errorRate"`
	// Spoof maps domain names to IP addresses to return instead of real DNS results
	Spoof map[string]string `js:"spoof"`
	// UpstreamDNS is the upstream DNS server for non-faulted queries (default: 8.8.8.8:53)
	UpstreamDNS string `js:"upstreamDNS"`
}
