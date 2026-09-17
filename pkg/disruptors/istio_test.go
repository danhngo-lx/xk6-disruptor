package disruptors

import (
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/danhngo-lx/xk6-disruptor/pkg/kubernetes"
	"github.com/danhngo-lx/xk6-disruptor/pkg/kubernetes/helpers"
)

func newUnstructuredVirtualService(namespace, name string) *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "networking.istio.io/v1",
			"kind":       "VirtualService",
			"metadata": map[string]any{
				"name":      name,
				"namespace": namespace,
			},
			"spec": map[string]any{
				"hosts": []any{"my-service"},
				"http": []any{
					map[string]any{
						"route": []any{
							map[string]any{
								"destination": map[string]any{
									"host": "my-service",
								},
							},
						},
					},
				},
			},
		},
	}
}

func newUnstructuredDestinationRule(namespace, name string) *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "networking.istio.io/v1",
			"kind":       "DestinationRule",
			"metadata": map[string]any{
				"name":      name,
				"namespace": namespace,
			},
			"spec": map[string]any{
				"host": "my-service",
			},
		},
	}
}

func newUnstructuredPeerAuthentication(namespace, name string) *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "security.istio.io/v1",
			"kind":       "PeerAuthentication",
			"metadata": map[string]any{
				"name":      name,
				"namespace": namespace,
			},
			"spec": map[string]any{},
		},
	}
}

func newUnstructuredAuthorizationPolicy(namespace, name string) *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "security.istio.io/v1",
			"kind":       "AuthorizationPolicy",
			"metadata": map[string]any{
				"name":      name,
				"namespace": namespace,
			},
			"spec": map[string]any{
				"action": "ALLOW",
			},
		},
	}
}

func TestVirtualServiceDisruptor_InjectFaultInjection(t *testing.T) {
	t.Parallel()

	obj := newUnstructuredVirtualService("ns", "my-vs")
	k8s, err := kubernetes.NewFakeKubernetesWithDynamicObjects(fake.NewSimpleClientset(), obj)
	if err != nil {
		t.Fatalf("NewFakeKubernetesWithDynamicObjects: %v", err)
	}

	d, err := NewVirtualServiceDisruptor(t.Context(), k8s, "ns", "my-vs")
	if err != nil {
		t.Fatalf("NewVirtualServiceDisruptor: %v", err)
	}

	fault := VirtualServiceFaultInjectionFault{DelayMillis: 500, AbortHTTPStatus: 503}
	if err := d.InjectFaultInjection(t.Context(), fault, 10*time.Millisecond); err != nil {
		t.Fatalf("InjectFaultInjection: %v", err)
	}

	restored, err := k8s.DynamicClient().Resource(helpers.VirtualServiceGVR).
		Namespace("ns").Get(t.Context(), "my-vs", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("getting restored VirtualService: %v", err)
	}

	if _, found, _ := unstructured.NestedMap(restored.Object, "spec", "http"); found {
		t.Error("expected spec.http[0] to have no fault field after restore")
	}
	routes, _, err := unstructured.NestedSlice(restored.Object, "spec", "http")
	if err != nil {
		t.Fatalf("NestedSlice: %v", err)
	}
	route, ok := routes[0].(map[string]any)
	if !ok {
		t.Fatalf("unexpected route shape: %T", routes[0])
	}
	if _, hasFault := route["fault"]; hasFault {
		t.Error("expected fault key to be removed after restore")
	}
}

func TestVirtualServiceDisruptor_InjectMisroute(t *testing.T) {
	t.Parallel()

	obj := newUnstructuredVirtualService("ns", "my-vs")
	k8s, err := kubernetes.NewFakeKubernetesWithDynamicObjects(fake.NewSimpleClientset(), obj)
	if err != nil {
		t.Fatalf("NewFakeKubernetesWithDynamicObjects: %v", err)
	}

	d, err := NewVirtualServiceDisruptor(t.Context(), k8s, "ns", "my-vs")
	if err != nil {
		t.Fatalf("NewVirtualServiceDisruptor: %v", err)
	}

	fault := VirtualServiceMisrouteFault{Host: "nonexistent-service", Subset: "canary"}
	if err := d.InjectMisroute(t.Context(), fault, 10*time.Millisecond); err != nil {
		t.Fatalf("InjectMisroute: %v", err)
	}

	restored, err := k8s.DynamicClient().Resource(helpers.VirtualServiceGVR).
		Namespace("ns").Get(t.Context(), "my-vs", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("getting restored VirtualService: %v", err)
	}

	routes, _, _ := unstructured.NestedSlice(restored.Object, "spec", "http")
	route := routes[0].(map[string]any)
	destinations, _, _ := unstructured.NestedSlice(route, "route")
	dest := destinations[0].(map[string]any)
	destination, _, _ := unstructured.NestedMap(dest, "destination")
	if destination["host"] != "my-service" {
		t.Errorf("expected destination host restored to %q, got %q", "my-service", destination["host"])
	}
	if _, hasSubset := destination["subset"]; hasSubset {
		t.Error("expected no subset after restore")
	}
}

func TestDestinationRuleDisruptor_InjectTLSFault(t *testing.T) {
	t.Parallel()

	obj := newUnstructuredDestinationRule("ns", "my-dr")
	k8s, err := kubernetes.NewFakeKubernetesWithDynamicObjects(fake.NewSimpleClientset(), obj)
	if err != nil {
		t.Fatalf("NewFakeKubernetesWithDynamicObjects: %v", err)
	}

	d, err := NewDestinationRuleDisruptor(t.Context(), k8s, "ns", "my-dr")
	if err != nil {
		t.Fatalf("NewDestinationRuleDisruptor: %v", err)
	}

	if err := d.InjectTLSFault(t.Context(), DestinationRuleTLSFault{Mode: "DISABLE"}, 10*time.Millisecond); err != nil {
		t.Fatalf("InjectTLSFault: %v", err)
	}

	restored, err := k8s.DynamicClient().Resource(helpers.DestinationRuleGVR).
		Namespace("ns").Get(t.Context(), "my-dr", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("getting restored DestinationRule: %v", err)
	}
	if _, found, _ := unstructured.NestedString(restored.Object, "spec", "trafficPolicy", "tls", "mode"); found {
		t.Error("expected trafficPolicy.tls.mode to be absent after restore")
	}
}

func TestPeerAuthenticationDisruptor_InjectMTLSFault(t *testing.T) {
	t.Parallel()

	obj := newUnstructuredPeerAuthentication("ns", "my-pa")
	k8s, err := kubernetes.NewFakeKubernetesWithDynamicObjects(fake.NewSimpleClientset(), obj)
	if err != nil {
		t.Fatalf("NewFakeKubernetesWithDynamicObjects: %v", err)
	}

	d, err := NewPeerAuthenticationDisruptor(t.Context(), k8s, "ns", "my-pa")
	if err != nil {
		t.Fatalf("NewPeerAuthenticationDisruptor: %v", err)
	}

	if err := d.InjectMTLSFault(t.Context(), PeerAuthenticationMTLSFault{Mode: "STRICT"}, 10*time.Millisecond); err != nil {
		t.Fatalf("InjectMTLSFault: %v", err)
	}

	restored, err := k8s.DynamicClient().Resource(helpers.PeerAuthenticationGVR).
		Namespace("ns").Get(t.Context(), "my-pa", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("getting restored PeerAuthentication: %v", err)
	}
	if _, found, _ := unstructured.NestedString(restored.Object, "spec", "mtls", "mode"); found {
		t.Error("expected mtls.mode to be absent after restore")
	}
}

func TestAuthorizationPolicyDisruptor_InjectDenyFault(t *testing.T) {
	t.Parallel()

	obj := newUnstructuredAuthorizationPolicy("ns", "my-authz")
	k8s, err := kubernetes.NewFakeKubernetesWithDynamicObjects(fake.NewSimpleClientset(), obj)
	if err != nil {
		t.Fatalf("NewFakeKubernetesWithDynamicObjects: %v", err)
	}

	d, err := NewAuthorizationPolicyDisruptor(t.Context(), k8s, "ns", "my-authz")
	if err != nil {
		t.Fatalf("NewAuthorizationPolicyDisruptor: %v", err)
	}

	if err := d.InjectDenyFault(t.Context(), AuthorizationPolicyDenyFault{DenyAll: true}, 10*time.Millisecond); err != nil {
		t.Fatalf("InjectDenyFault: %v", err)
	}

	restored, err := k8s.DynamicClient().Resource(helpers.AuthorizationPolicyGVR).
		Namespace("ns").Get(t.Context(), "my-authz", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("getting restored AuthorizationPolicy: %v", err)
	}
	action, _, _ := unstructured.NestedString(restored.Object, "spec", "action")
	if action != "ALLOW" {
		t.Errorf("expected action restored to ALLOW, got %q", action)
	}
}
