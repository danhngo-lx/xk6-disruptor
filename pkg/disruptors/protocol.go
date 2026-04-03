package disruptors

import (
	"context"
	"time"

	"github.com/danhngo-lx/xk6-disruptor/pkg/types/intstr"
)

// ProtocolFaultInjector defines the methods for injecting protocol faults
type ProtocolFaultInjector interface {
	// InjectHTTPFault injects faults in the HTTP requests sent to the disruptor's targets
	// for the specified duration
	InjectHTTPFaults(ctx context.Context, fault HTTPFault, duration time.Duration, options HTTPDisruptionOptions) error
	// InjectGrpcFault injects faults in the grpc requests sent to the disruptor's targets
	// for the specified duration
	InjectGrpcFaults(ctx context.Context, fault GrpcFault, duration time.Duration, options GrpcDisruptionOptions) error
	// InjectHTTPResetPeerFaults intercepts TCP connections on the target port and abruptly
	// resets them (SO_LINGER=0) to simulate flaky network conditions at the TCP layer.
	InjectHTTPResetPeerFaults(ctx context.Context, fault HTTPResetPeerFault, duration time.Duration, options HTTPDisruptionOptions) error
}

// HTTPDisruptionOptions defines options for the injection of HTTP faults in a target pod
type HTTPDisruptionOptions struct {
	// Port used by the agent for listening
	ProxyPort uint `js:"proxyPort"`
	// NonTransparent disables iptables-based traffic redirection when set to true.
	// By default (false) the agent installs iptables rules that transparently redirect
	// traffic on the target port through the proxy. When set to true no iptables rules are
	// installed and the load test must send requests directly to ProxyPort on the pod IP.
	// This is the recommended option when a service mesh such as Istio is present and its
	// own iptables rules conflict with the disruptor's.
	NonTransparent bool `js:"nonTransparent"`
}

// GrpcDisruptionOptions defines options for the injection of grpc faults in a target pod
type GrpcDisruptionOptions struct {
	// Port used by the agent for listening
	ProxyPort uint `js:"proxyPort"`
}

// HTTPFault specifies a fault to be injected in http requests
type HTTPFault struct {
	// port the disruptions will be applied to
	Port intstr.IntOrString
	// Average delay introduced to requests
	AverageDelay time.Duration `js:"averageDelay"`
	// Variation in the delay (with respect of the average delay)
	DelayVariation time.Duration `js:"delayVariation"`
	// Fraction (in the range 0.0 to 1.0) of requests that will return an error
	ErrorRate float32 `js:"errorRate"`
	// Error code to be returned by requests selected in the error rate
	ErrorCode uint `js:"errorCode"`
	// Body to be returned when an error is injected
	ErrorBody string `js:"errorBody"`
	// Comma-separated list of url paths to be excluded from disruptions
	Exclude string
	// ModifyResponseBody replaces the upstream response body with this string when non-empty
	ModifyResponseBody string `js:"modifyResponseBody"`
	// ModifyResponseHeaders adds or replaces headers in the upstream response
	ModifyResponseHeaders map[string]string `js:"modifyResponseHeaders"`
}

// HTTPResetPeerFault specifies a TCP reset-peer fault to be injected.
// Instead of returning an HTTP error response, the proxy abruptly closes the TCP
// connection by sending a RST packet after an optional delay.
type HTTPResetPeerFault struct {
	// Port is the target port to intercept.
	Port intstr.IntOrString
	// ResetTimeout is how long to wait after accepting a connection before sending the RST.
	// Zero means reset immediately.
	ResetTimeout time.Duration `js:"resetTimeout"`
	// Toxicity is the fraction of connections (0.0–1.0) to reset.
	// Connections not selected are transparently proxied to the upstream.
	// Defaults to 1.0 (all connections are reset).
	Toxicity float32 `js:"toxicity"`
}

// GrpcFault specifies a fault to be injected in grpc requests
type GrpcFault struct {
	// port the disruptions will be applied to
	Port intstr.IntOrString
	// Average delay introduced to requests
	AverageDelay time.Duration `js:"averageDelay"`
	// Variation in the delay (with respect of the average delay)
	DelayVariation time.Duration `js:"delayVariation"`
	// Fraction (in the range 0.0 to 1.0) of requests that will return an error
	ErrorRate float32 `js:"errorRate"`
	// Status code to be returned by requests selected to return an error
	StatusCode int32 `js:"statusCode"`
	// Status message to be returned in requests selected to return an error
	StatusMessage string `js:"statusMessage"`
	// List of grpc services to be excluded from disruptions
	Exclude string `js:"exclude"`
}
