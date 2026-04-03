package http

import (
	"context"
	"errors"
	"io"
	"math/rand"
	"net"
	"time"

	"github.com/danhngo-lx/xk6-disruptor/pkg/agent/protocol"
)

// ResetPeerDisruption specifies disruptions applied by the reset peer proxy
type ResetPeerDisruption struct {
	// ResetTimeout is how long to wait after accepting a connection before sending the TCP RST.
	// Zero means reset immediately without reading any data.
	ResetTimeout time.Duration
	// Toxicity is the fraction of connections (0.0–1.0) that will be reset.
	// Connections not selected for reset are transparently proxied to the upstream.
	Toxicity float32
}

// resetPeerProxy is a TCP-level proxy that abruptly resets a configurable fraction
// of incoming connections by setting SO_LINGER=0 before closing them.
// This simulates a lossy or flaky network connection at the TCP layer, distinct from
// HTTP-level error injection which sends well-formed HTTP error responses.
type resetPeerProxy struct {
	listener    net.Listener
	upstream    string // "host:port"
	disruption  ResetPeerDisruption
	metrics     *protocol.MetricMap
	stop        chan struct{}
}

// NewResetPeerProxy creates a new TCP reset-peer proxy.
// listener is where the proxy accepts connections.
// upstream is the "host:port" to forward non-reset connections to.
func NewResetPeerProxy(listener net.Listener, upstream string, d ResetPeerDisruption) (protocol.Proxy, error) {
	if d.Toxicity < 0 || d.Toxicity > 1 {
		return nil, errors.New("toxicity must be in the range [0.0, 1.0]")
	}

	if d.Toxicity == 0 {
		d.Toxicity = 1.0
	}

	metrics := protocol.NewMetricMap(
		protocol.MetricRequests,
		protocol.MetricRequestsDisrupted,
		protocol.MetricRequestsExcluded,
	)

	return &resetPeerProxy{
		listener:   listener,
		upstream:   upstream,
		disruption: d,
		metrics:    metrics,
		stop:       make(chan struct{}),
	}, nil
}

// Start begins accepting connections. Blocks until Stop/Force is called.
func (p *resetPeerProxy) Start() error {
	for {
		conn, err := p.listener.Accept()
		if err != nil {
			select {
			case <-p.stop:
				return nil
			default:
				return err
			}
		}

		go p.handle(conn)
	}
}

// Stop gracefully stops the proxy by closing the listener.
func (p *resetPeerProxy) Stop() error {
	close(p.stop)
	return p.listener.Close()
}

// Force is identical to Stop for this proxy (no in-flight requests to drain).
func (p *resetPeerProxy) Force() error {
	return p.Stop()
}

// Metrics returns runtime counters.
func (p *resetPeerProxy) Metrics() map[string]uint {
	return p.metrics.Map()
}

func (p *resetPeerProxy) handle(conn net.Conn) {
	p.metrics.Inc(protocol.MetricRequests)

	// Decide whether to reset this connection.
	if rand.Float32() <= p.disruption.Toxicity { //nolint:gosec
		p.metrics.Inc(protocol.MetricRequestsDisrupted)
		p.resetConn(conn)
		return
	}

	// Pass-through: forward the connection to the upstream transparently.
	p.metrics.Inc(protocol.MetricRequestsExcluded)
	p.forwardConn(conn)
}

// resetConn waits ResetTimeout then sends a TCP RST by setting SO_LINGER=0.
func (p *resetPeerProxy) resetConn(conn net.Conn) {
	defer func() {
		if tcpConn, ok := conn.(*net.TCPConn); ok {
			// Setting Linger(0) causes the OS to send a RST instead of a FIN
			// when the socket is closed, aborting the TCP connection abruptly.
			_ = tcpConn.SetLinger(0)
		}
		_ = conn.Close()
	}()

	if p.disruption.ResetTimeout > 0 {
		time.Sleep(p.disruption.ResetTimeout)
	}
}

// forwardConn copies data bidirectionally between the accepted connection and the upstream.
func (p *resetPeerProxy) forwardConn(client net.Conn) {
	defer client.Close()

	upstream, err := net.DialTimeout("tcp", p.upstream, 5*time.Second)
	if err != nil {
		return
	}
	defer upstream.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		_, _ = io.Copy(upstream, client)
		cancel()
	}()

	go func() {
		_, _ = io.Copy(client, upstream)
		cancel()
	}()

	<-ctx.Done()
}
