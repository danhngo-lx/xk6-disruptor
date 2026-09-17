package helpers

import "k8s.io/apimachinery/pkg/runtime/schema"

// GroupVersionResource constants for the Istio CRDs targeted by Istio fault types.
// These are hardcoded (rather than discovered) because the disruptor talks to them
// through a generic dynamic client, not a typed Istio clientset — if the cluster's
// Istio CRD version ever changes, this is the one place to update.
var (
	// VirtualServiceGVR identifies networking.istio.io VirtualService resources.
	VirtualServiceGVR = schema.GroupVersionResource{
		Group: "networking.istio.io", Version: "v1", Resource: "virtualservices",
	}
	// DestinationRuleGVR identifies networking.istio.io DestinationRule resources.
	DestinationRuleGVR = schema.GroupVersionResource{
		Group: "networking.istio.io", Version: "v1", Resource: "destinationrules",
	}
	// PeerAuthenticationGVR identifies security.istio.io PeerAuthentication resources.
	PeerAuthenticationGVR = schema.GroupVersionResource{
		Group: "security.istio.io", Version: "v1", Resource: "peerauthentications",
	}
	// AuthorizationPolicyGVR identifies security.istio.io AuthorizationPolicy resources.
	AuthorizationPolicyGVR = schema.GroupVersionResource{
		Group: "security.istio.io", Version: "v1", Resource: "authorizationpolicies",
	}
)
