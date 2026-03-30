// Package dns implements a DNS proxy that can inject faults into DNS responses
package dns

import (
	"context"
	"fmt"
	"math/rand"
	"net"
	"time"

	"github.com/miekg/dns"
)

// Config defines the configuration for the DNS proxy
type Config struct {
	// ListenAddr is the address the proxy listens on (e.g. "127.0.0.1:5353")
	ListenAddr string
	// UpstreamAddr is the upstream DNS server to forward non-faulted queries to (e.g. "8.8.8.8:53")
	UpstreamAddr string
	// ErrorRate is the fraction (0.0-1.0) of queries that will return NXDOMAIN
	ErrorRate float32
	// Spoof maps domain names (with trailing dot) to IP addresses to return instead of real results
	// Example: {"example.com.": "1.2.3.4"}
	Spoof map[string]string
}

// Proxy is a DNS proxy that intercepts queries and applies fault injection
type Proxy struct {
	config Config
	server *dns.Server
}

// NewProxy creates and configures a new DNS proxy
func NewProxy(config Config) (*Proxy, error) {
	if config.ListenAddr == "" {
		return nil, fmt.Errorf("listen address is required")
	}

	if config.UpstreamAddr == "" {
		return nil, fmt.Errorf("upstream DNS address is required")
	}

	if config.ErrorRate < 0 || config.ErrorRate > 1 {
		return nil, fmt.Errorf("error rate must be in the range [0.0, 1.0]")
	}

	return &Proxy{config: config}, nil
}

// Start starts the DNS proxy and blocks until the context is cancelled
func (p *Proxy) Start(ctx context.Context) error {
	mux := dns.NewServeMux()
	mux.HandleFunc(".", p.handleQuery)

	p.server = &dns.Server{
		Addr:    p.config.ListenAddr,
		Net:     "udp",
		Handler: mux,
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- p.server.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		_ = p.server.Shutdown()
		return nil
	case err := <-errCh:
		return err
	}
}

func (p *Proxy) handleQuery(w dns.ResponseWriter, r *dns.Msg) {
	if len(r.Question) == 0 {
		dns.HandleFailed(w, r)
		return
	}

	qname := r.Question[0].Name

	// Check for spoofed domain
	if ip, ok := p.config.Spoof[qname]; ok {
		p.respondWithSpoof(w, r, qname, ip)
		return
	}

	// Inject NXDOMAIN at the configured error rate
	if p.config.ErrorRate > 0 && rand.Float32() <= p.config.ErrorRate { //nolint:gosec
		p.respondWithNXDomain(w, r)
		return
	}

	// Forward to upstream
	p.forwardQuery(w, r)
}

func (p *Proxy) respondWithSpoof(w dns.ResponseWriter, r *dns.Msg, name, ipStr string) {
	m := new(dns.Msg)
	m.SetReply(r)
	m.Authoritative = true

	ip := net.ParseIP(ipStr)
	if ip == nil {
		dns.HandleFailed(w, r)
		return
	}

	if ip4 := ip.To4(); ip4 != nil {
		m.Answer = append(m.Answer, &dns.A{
			Hdr: dns.RR_Header{
				Name:   name,
				Rrtype: dns.TypeA,
				Class:  dns.ClassINET,
				Ttl:    60,
			},
			A: ip4,
		})
	} else {
		m.Answer = append(m.Answer, &dns.AAAA{
			Hdr: dns.RR_Header{
				Name:   name,
				Rrtype: dns.TypeAAAA,
				Class:  dns.ClassINET,
				Ttl:    60,
			},
			AAAA: ip,
		})
	}

	_ = w.WriteMsg(m)
}

func (p *Proxy) respondWithNXDomain(w dns.ResponseWriter, r *dns.Msg) {
	m := new(dns.Msg)
	m.SetReply(r)
	m.SetRcode(r, dns.RcodeNameError)
	_ = w.WriteMsg(m)
}

func (p *Proxy) forwardQuery(w dns.ResponseWriter, r *dns.Msg) {
	client := &dns.Client{Timeout: 5 * time.Second}

	resp, _, err := client.Exchange(r, p.config.UpstreamAddr)
	if err != nil {
		dns.HandleFailed(w, r)
		return
	}

	_ = w.WriteMsg(resp)
}
