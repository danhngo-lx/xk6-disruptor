package disruptors

import (
	"context"
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"

	"github.com/danhngo-lx/xk6-disruptor/pkg/kubernetes"
	"github.com/danhngo-lx/xk6-disruptor/pkg/kubernetes/helpers"
)

// ── shared machinery ──────────────────────────────────────────────────────────
//
// Every Istio fault below targets a single named, namespaced Istio custom resource
// through the generic dynamic client (no typed Istio clientset is used — see
// pkg/kubernetes/helpers/istio.go for the GVR constants). Every fault follows the
// same snapshot → patch → wait → restore shape, implemented once in istioTarget.

// istioTarget resolves and mutates a single named Istio custom resource.
type istioTarget struct {
	client    dynamic.ResourceInterface
	kind      string
	namespace string
	name      string
}

func newIstioTarget(
	ctx context.Context,
	k8s kubernetes.Kubernetes,
	gvr schema.GroupVersionResource,
	kind, namespace, name string,
) (*istioTarget, error) {
	if namespace == "" {
		namespace = "default"
	}
	if name == "" {
		return nil, fmt.Errorf("name is required")
	}

	client := k8s.DynamicClient().Resource(gvr).Namespace(namespace)
	if _, err := client.Get(ctx, name, metav1.GetOptions{}); err != nil {
		return nil, fmt.Errorf("getting %s %s/%s: %w", kind, namespace, name, err)
	}

	return &istioTarget{client: client, kind: kind, namespace: namespace, name: name}, nil
}

// Targets returns the single target as "<Kind>/<namespace>/<name>".
func (t *istioTarget) Targets(_ context.Context) ([]string, error) {
	return []string{fmt.Sprintf("%s/%s/%s", t.kind, t.namespace, t.name)}, nil
}

// TargetIPs is not meaningful for an Istio custom resource. Returns an empty slice.
func (t *istioTarget) TargetIPs(_ context.Context) ([]string, error) {
	return []string{}, nil
}

// Cleanup is a no-op: applyPatchForDuration always restores the original object
// before returning, mirroring NodeDisruptor's Drain/TaintNode.
func (t *istioTarget) Cleanup(_ context.Context) error {
	return nil
}

// applyPatchForDuration snapshots the current object, applies mutate to a copy,
// updates it, waits for duration (or context cancellation), then restores the
// original object.
func (t *istioTarget) applyPatchForDuration(
	ctx context.Context,
	duration time.Duration,
	mutate func(obj *unstructured.Unstructured) error,
) error {
	current, err := t.client.Get(ctx, t.name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("getting %s: %w", t.kind, err)
	}
	restore := current.DeepCopy()

	patched := current.DeepCopy()
	if err := mutate(patched); err != nil {
		return err
	}
	if _, err := t.client.Update(ctx, patched, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("patching %s: %w", t.kind, err)
	}

	waitOrCancel(ctx, duration)

	if latest, err := t.client.Get(ctx, t.name, metav1.GetOptions{}); err == nil {
		restore.SetResourceVersion(latest.GetResourceVersion())
	}
	if _, err := t.client.Update(context.Background(), restore, metav1.UpdateOptions{}); err != nil { //nolint:contextcheck
		return fmt.Errorf("restoring %s: %w", t.kind, err)
	}
	return nil
}

// ── VirtualService faults ─────────────────────────────────────────────────────

// VirtualServiceFaultInjectionFault injects Istio's native HTTP delay/abort fault
// injection into one spec.http[] route of a VirtualService.
type VirtualServiceFaultInjectionFault struct {
	// HTTPRouteIndex selects which spec.http[] entry to patch (default 0).
	HTTPRouteIndex int `js:"httpRouteIndex"`
	// DelayMillis is the fixed delay to inject, in milliseconds. 0 disables delay.
	DelayMillis int64 `js:"delayMillis"`
	// DelayPercent is the fraction (0-100) of requests delayed. Defaults to 100 when DelayMillis is set.
	DelayPercent float64 `js:"delayPercent"`
	// AbortHTTPStatus is the HTTP status code to abort with. 0 disables abort.
	AbortHTTPStatus int64 `js:"abortHTTPStatus"`
	// AbortPercent is the fraction (0-100) of requests aborted. Defaults to 100 when AbortHTTPStatus is set.
	AbortPercent float64 `js:"abortPercent"`
}

// Validate checks that the fault specifies at least a delay or an abort.
func (f VirtualServiceFaultInjectionFault) Validate() error {
	if f.DelayMillis <= 0 && f.AbortHTTPStatus <= 0 {
		return fmt.Errorf("at least one of delayMillis or abortHTTPStatus must be set")
	}
	return nil
}

// VirtualServiceMisrouteFault overrides a spec.http[] route's destination to a
// (typically bad) host/subset, simulating a route-config regression.
type VirtualServiceMisrouteFault struct {
	// HTTPRouteIndex selects which spec.http[] entry to patch (default 0).
	HTTPRouteIndex int `js:"httpRouteIndex"`
	// RouteIndex selects which route[] destination within the selected http[] entry (default 0).
	RouteIndex int `js:"routeIndex"`
	// Host overrides the destination host (required).
	Host string `js:"host"`
	// Subset overrides the destination subset. Empty leaves it unchanged.
	Subset string `js:"subset"`
}

// Validate checks that a destination host was provided.
func (f VirtualServiceMisrouteFault) Validate() error {
	if f.Host == "" {
		return fmt.Errorf("host is required")
	}
	return nil
}

// VirtualServiceFaultInjector defines the interface for injecting delay/abort faults.
type VirtualServiceFaultInjector interface {
	InjectFaultInjection(ctx context.Context, fault VirtualServiceFaultInjectionFault, duration time.Duration) error
}

// VirtualServiceMisrouteInjector defines the interface for injecting route misconfigurations.
type VirtualServiceMisrouteInjector interface {
	InjectMisroute(ctx context.Context, fault VirtualServiceMisrouteFault, duration time.Duration) error
}

// VirtualServiceDisruptor defines the methods for injecting chaos into a single VirtualService.
type VirtualServiceDisruptor interface {
	Disruptor
	VirtualServiceFaultInjector
	VirtualServiceMisrouteInjector
}

type virtualServiceDisruptor struct{ *istioTarget }

// NewVirtualServiceDisruptor creates a VirtualServiceDisruptor targeting the named VirtualService.
func NewVirtualServiceDisruptor(
	ctx context.Context, k8s kubernetes.Kubernetes, namespace, name string,
) (VirtualServiceDisruptor, error) {
	t, err := newIstioTarget(ctx, k8s, helpers.VirtualServiceGVR, "VirtualService", namespace, name)
	if err != nil {
		return nil, err
	}
	return &virtualServiceDisruptor{t}, nil
}

func httpRouteAt(obj *unstructured.Unstructured, idx int) (map[string]any, []any, error) {
	routes, found, err := unstructured.NestedSlice(obj.Object, "spec", "http")
	if err != nil {
		return nil, nil, err
	}
	if !found || idx < 0 || idx >= len(routes) {
		return nil, nil, fmt.Errorf("virtualservice has no spec.http[%d] entry", idx)
	}
	route, ok := routes[idx].(map[string]any)
	if !ok {
		return nil, nil, fmt.Errorf("unexpected spec.http[%d] shape", idx)
	}
	return route, routes, nil
}

// InjectFaultInjection patches spec.http[fault.HTTPRouteIndex].fault with a delay
// and/or abort, waits for duration, then restores the VirtualService.
func (d *virtualServiceDisruptor) InjectFaultInjection(
	ctx context.Context, fault VirtualServiceFaultInjectionFault, duration time.Duration,
) error {
	if err := fault.Validate(); err != nil {
		return err
	}

	return d.applyPatchForDuration(ctx, duration, func(obj *unstructured.Unstructured) error {
		route, routes, err := httpRouteAt(obj, fault.HTTPRouteIndex)
		if err != nil {
			return err
		}

		faultSpec := map[string]any{}
		if fault.DelayMillis > 0 {
			percent := fault.DelayPercent
			if percent <= 0 {
				percent = 100
			}
			faultSpec["delay"] = map[string]any{
				"fixedDelay": fmt.Sprintf("%dms", fault.DelayMillis),
				"percentage": map[string]any{"value": percent},
			}
		}
		if fault.AbortHTTPStatus > 0 {
			percent := fault.AbortPercent
			if percent <= 0 {
				percent = 100
			}
			faultSpec["abort"] = map[string]any{
				"httpStatus": fault.AbortHTTPStatus,
				"percentage": map[string]any{"value": percent},
			}
		}

		route["fault"] = faultSpec
		routes[fault.HTTPRouteIndex] = route
		return unstructured.SetNestedSlice(obj.Object, routes, "spec", "http")
	})
}

// InjectMisroute patches spec.http[fault.HTTPRouteIndex].route[fault.RouteIndex].destination
// to point at fault.Host/fault.Subset, waits for duration, then restores the VirtualService.
func (d *virtualServiceDisruptor) InjectMisroute(
	ctx context.Context, fault VirtualServiceMisrouteFault, duration time.Duration,
) error {
	if err := fault.Validate(); err != nil {
		return err
	}

	return d.applyPatchForDuration(ctx, duration, func(obj *unstructured.Unstructured) error {
		route, routes, err := httpRouteAt(obj, fault.HTTPRouteIndex)
		if err != nil {
			return err
		}

		destinations, found, err := unstructured.NestedSlice(route, "route")
		if err != nil {
			return err
		}
		if !found || fault.RouteIndex < 0 || fault.RouteIndex >= len(destinations) {
			return fmt.Errorf("virtualservice has no spec.http[%d].route[%d] entry", fault.HTTPRouteIndex, fault.RouteIndex)
		}
		dest, ok := destinations[fault.RouteIndex].(map[string]any)
		if !ok {
			return fmt.Errorf("unexpected spec.http[%d].route[%d] shape", fault.HTTPRouteIndex, fault.RouteIndex)
		}

		destination, _, err := unstructured.NestedMap(dest, "destination")
		if err != nil {
			return err
		}
		if destination == nil {
			destination = map[string]any{}
		}
		destination["host"] = fault.Host
		if fault.Subset != "" {
			destination["subset"] = fault.Subset
		}
		dest["destination"] = destination
		destinations[fault.RouteIndex] = dest
		route["route"] = destinations
		routes[fault.HTTPRouteIndex] = route
		return unstructured.SetNestedSlice(obj.Object, routes, "spec", "http")
	})
}

// ── DestinationRule faults ────────────────────────────────────────────────────

// DestinationRuleTLSFault overrides spec.trafficPolicy.tls.mode, e.g. to mismatch
// the peer's expected mTLS mode and cause connection failures.
type DestinationRuleTLSFault struct {
	// Mode is the TLS mode to set (e.g. "DISABLE", "SIMPLE", "MUTUAL", "ISTIO_MUTUAL").
	Mode string `js:"mode"`
}

// Validate checks that a TLS mode was provided.
func (f DestinationRuleTLSFault) Validate() error {
	if f.Mode == "" {
		return fmt.Errorf("mode is required")
	}
	return nil
}

// DestinationRuleTLSFaultInjector defines the interface for injecting DestinationRule TLS faults.
type DestinationRuleTLSFaultInjector interface {
	InjectTLSFault(ctx context.Context, fault DestinationRuleTLSFault, duration time.Duration) error
}

// DestinationRuleDisruptor defines the methods for injecting chaos into a single DestinationRule.
type DestinationRuleDisruptor interface {
	Disruptor
	DestinationRuleTLSFaultInjector
}

type destinationRuleDisruptor struct{ *istioTarget }

// NewDestinationRuleDisruptor creates a DestinationRuleDisruptor targeting the named DestinationRule.
func NewDestinationRuleDisruptor(
	ctx context.Context, k8s kubernetes.Kubernetes, namespace, name string,
) (DestinationRuleDisruptor, error) {
	t, err := newIstioTarget(ctx, k8s, helpers.DestinationRuleGVR, "DestinationRule", namespace, name)
	if err != nil {
		return nil, err
	}
	return &destinationRuleDisruptor{t}, nil
}

// InjectTLSFault patches spec.trafficPolicy.tls.mode, waits for duration, then restores it.
func (d *destinationRuleDisruptor) InjectTLSFault(
	ctx context.Context, fault DestinationRuleTLSFault, duration time.Duration,
) error {
	if err := fault.Validate(); err != nil {
		return err
	}

	return d.applyPatchForDuration(ctx, duration, func(obj *unstructured.Unstructured) error {
		return unstructured.SetNestedField(obj.Object, fault.Mode, "spec", "trafficPolicy", "tls", "mode")
	})
}

// ── PeerAuthentication faults ─────────────────────────────────────────────────

// PeerAuthenticationMTLSFault overrides spec.mtls.mode, e.g. forcing STRICT to break
// callers that expect plaintext or permissive mTLS.
type PeerAuthenticationMTLSFault struct {
	// Mode is the mTLS mode to set (e.g. "STRICT", "PERMISSIVE", "DISABLE").
	Mode string `js:"mode"`
}

// Validate checks that an mTLS mode was provided.
func (f PeerAuthenticationMTLSFault) Validate() error {
	if f.Mode == "" {
		return fmt.Errorf("mode is required")
	}
	return nil
}

// PeerAuthenticationMTLSFaultInjector defines the interface for injecting PeerAuthentication mTLS faults.
type PeerAuthenticationMTLSFaultInjector interface {
	InjectMTLSFault(ctx context.Context, fault PeerAuthenticationMTLSFault, duration time.Duration) error
}

// PeerAuthenticationDisruptor defines the methods for injecting chaos into a single PeerAuthentication.
type PeerAuthenticationDisruptor interface {
	Disruptor
	PeerAuthenticationMTLSFaultInjector
}

type peerAuthenticationDisruptor struct{ *istioTarget }

// NewPeerAuthenticationDisruptor creates a PeerAuthenticationDisruptor targeting the named PeerAuthentication.
func NewPeerAuthenticationDisruptor(
	ctx context.Context, k8s kubernetes.Kubernetes, namespace, name string,
) (PeerAuthenticationDisruptor, error) {
	t, err := newIstioTarget(ctx, k8s, helpers.PeerAuthenticationGVR, "PeerAuthentication", namespace, name)
	if err != nil {
		return nil, err
	}
	return &peerAuthenticationDisruptor{t}, nil
}

// InjectMTLSFault patches spec.mtls.mode, waits for duration, then restores it.
func (d *peerAuthenticationDisruptor) InjectMTLSFault(
	ctx context.Context, fault PeerAuthenticationMTLSFault, duration time.Duration,
) error {
	if err := fault.Validate(); err != nil {
		return err
	}

	return d.applyPatchForDuration(ctx, duration, func(obj *unstructured.Unstructured) error {
		return unstructured.SetNestedField(obj.Object, fault.Mode, "spec", "mtls", "mode")
	})
}

// ── AuthorizationPolicy faults ────────────────────────────────────────────────

// AuthorizationPolicyDenyFault overrides an AuthorizationPolicy to deny all traffic
// it applies to, simulating an authz misconfiguration (403 storm).
type AuthorizationPolicyDenyFault struct {
	// DenyAll must be true; reserved for future extension (e.g. partial deny rules).
	DenyAll bool `js:"denyAll"`
}

// Validate checks that DenyAll was explicitly requested.
func (f AuthorizationPolicyDenyFault) Validate() error {
	if !f.DenyAll {
		return fmt.Errorf("denyAll must be true")
	}
	return nil
}

// AuthorizationPolicyDenyFaultInjector defines the interface for injecting AuthorizationPolicy deny faults.
type AuthorizationPolicyDenyFaultInjector interface {
	InjectDenyFault(ctx context.Context, fault AuthorizationPolicyDenyFault, duration time.Duration) error
}

// AuthorizationPolicyDisruptor defines the methods for injecting chaos into a single AuthorizationPolicy.
type AuthorizationPolicyDisruptor interface {
	Disruptor
	AuthorizationPolicyDenyFaultInjector
}

type authorizationPolicyDisruptor struct{ *istioTarget }

// NewAuthorizationPolicyDisruptor creates an AuthorizationPolicyDisruptor targeting the named AuthorizationPolicy.
func NewAuthorizationPolicyDisruptor(
	ctx context.Context, k8s kubernetes.Kubernetes, namespace, name string,
) (AuthorizationPolicyDisruptor, error) {
	t, err := newIstioTarget(ctx, k8s, helpers.AuthorizationPolicyGVR, "AuthorizationPolicy", namespace, name)
	if err != nil {
		return nil, err
	}
	return &authorizationPolicyDisruptor{t}, nil
}

// InjectDenyFault sets spec.action to DENY with a single match-all rule, waits for
// duration, then restores the original policy.
func (d *authorizationPolicyDisruptor) InjectDenyFault(
	ctx context.Context, fault AuthorizationPolicyDenyFault, duration time.Duration,
) error {
	if err := fault.Validate(); err != nil {
		return err
	}

	return d.applyPatchForDuration(ctx, duration, func(obj *unstructured.Unstructured) error {
		if err := unstructured.SetNestedField(obj.Object, "DENY", "spec", "action"); err != nil {
			return err
		}
		// A single empty rule object matches every request.
		rules := []any{map[string]any{}}
		return unstructured.SetNestedSlice(obj.Object, rules, "spec", "rules")
	})
}
