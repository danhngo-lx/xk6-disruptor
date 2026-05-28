package helpers

import (
	"context"
	"fmt"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
)

// Supported workload kinds for scaling.
const (
	WorkloadKindDeployment  = "Deployment"
	WorkloadKindStatefulSet = "StatefulSet"
)

// WorkloadRef uniquely identifies a workload to be scaled.
type WorkloadRef struct {
	Kind      string
	Namespace string
	Name      string
}

func (r WorkloadRef) String() string {
	return fmt.Sprintf("%s/%s/%s", r.Kind, r.Namespace, r.Name)
}

// WorkloadHelper defines helper methods for scaling Deployments and StatefulSets.
//
// The implementation reads and writes spec.replicas on the workload resource itself
// rather than the /scale subresource, so it works against both the real apiserver
// (assuming RBAC for updating Deployments/StatefulSets) and client-go's fake
// clientset (which does not synthesize the scale subresource).
type WorkloadHelper interface {
	// GetReplicas returns the current desired replica count for the workload.
	GetReplicas(ctx context.Context, ref WorkloadRef) (int32, error)
	// Scale sets the desired replica count.
	Scale(ctx context.Context, ref WorkloadRef, replicas int32) error
	// List returns workload refs of the given kind in the namespace matching the label selector.
	// When labelSelector is empty all workloads of that kind in the namespace are returned.
	List(ctx context.Context, kind, namespace string, labelSelector map[string]string) ([]WorkloadRef, error)
	// Exists reports whether the named workload of the given kind exists in the namespace.
	Exists(ctx context.Context, ref WorkloadRef) (bool, error)
}

type workloadHelper struct {
	client kubernetes.Interface
}

// NewWorkloadHelper returns a WorkloadHelper backed by the given Kubernetes client.
func NewWorkloadHelper(client kubernetes.Interface) WorkloadHelper {
	return &workloadHelper{client: client}
}

func validateKind(kind string) error {
	switch kind {
	case WorkloadKindDeployment, WorkloadKindStatefulSet:
		return nil
	default:
		return fmt.Errorf("unsupported workload kind %q (supported: Deployment, StatefulSet)", kind)
	}
}

func (h *workloadHelper) GetReplicas(ctx context.Context, ref WorkloadRef) (int32, error) {
	if err := validateKind(ref.Kind); err != nil {
		return 0, err
	}

	switch ref.Kind {
	case WorkloadKindDeployment:
		d, err := h.client.AppsV1().Deployments(ref.Namespace).Get(ctx, ref.Name, metav1.GetOptions{})
		if err != nil {
			return 0, fmt.Errorf("getting %s: %w", ref, err)
		}
		if d.Spec.Replicas == nil {
			return 1, nil
		}
		return *d.Spec.Replicas, nil
	case WorkloadKindStatefulSet:
		s, err := h.client.AppsV1().StatefulSets(ref.Namespace).Get(ctx, ref.Name, metav1.GetOptions{})
		if err != nil {
			return 0, fmt.Errorf("getting %s: %w", ref, err)
		}
		if s.Spec.Replicas == nil {
			return 1, nil
		}
		return *s.Spec.Replicas, nil
	}
	return 0, fmt.Errorf("unsupported workload kind %q", ref.Kind)
}

func (h *workloadHelper) Scale(ctx context.Context, ref WorkloadRef, replicas int32) error {
	if err := validateKind(ref.Kind); err != nil {
		return err
	}
	if replicas < 0 {
		replicas = 0
	}

	switch ref.Kind {
	case WorkloadKindDeployment:
		d, err := h.client.AppsV1().Deployments(ref.Namespace).Get(ctx, ref.Name, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("getting %s: %w", ref, err)
		}
		d.Spec.Replicas = &replicas
		if _, err := h.client.AppsV1().Deployments(ref.Namespace).Update(ctx, d, metav1.UpdateOptions{}); err != nil {
			return fmt.Errorf("scaling %s to %d: %w", ref, replicas, err)
		}
		return nil
	case WorkloadKindStatefulSet:
		s, err := h.client.AppsV1().StatefulSets(ref.Namespace).Get(ctx, ref.Name, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("getting %s: %w", ref, err)
		}
		s.Spec.Replicas = &replicas
		if _, err := h.client.AppsV1().StatefulSets(ref.Namespace).Update(ctx, s, metav1.UpdateOptions{}); err != nil {
			return fmt.Errorf("scaling %s to %d: %w", ref, replicas, err)
		}
		return nil
	}
	return fmt.Errorf("unsupported workload kind %q", ref.Kind)
}

func (h *workloadHelper) List(
	ctx context.Context,
	kind, namespace string,
	labelSelector map[string]string,
) ([]WorkloadRef, error) {
	if err := validateKind(kind); err != nil {
		return nil, err
	}

	opts := metav1.ListOptions{LabelSelector: labels.Set(labelSelector).String()}

	switch kind {
	case WorkloadKindDeployment:
		list, err := h.client.AppsV1().Deployments(namespace).List(ctx, opts)
		if err != nil {
			return nil, err
		}
		refs := make([]WorkloadRef, 0, len(list.Items))
		for _, d := range list.Items {
			refs = append(refs, WorkloadRef{Kind: kind, Namespace: d.Namespace, Name: d.Name})
		}
		return refs, nil

	case WorkloadKindStatefulSet:
		list, err := h.client.AppsV1().StatefulSets(namespace).List(ctx, opts)
		if err != nil {
			return nil, err
		}
		refs := make([]WorkloadRef, 0, len(list.Items))
		for _, s := range list.Items {
			refs = append(refs, WorkloadRef{Kind: kind, Namespace: s.Namespace, Name: s.Name})
		}
		return refs, nil
	}
	return nil, nil
}

func (h *workloadHelper) Exists(ctx context.Context, ref WorkloadRef) (bool, error) {
	if err := validateKind(ref.Kind); err != nil {
		return false, err
	}

	var err error
	switch ref.Kind {
	case WorkloadKindDeployment:
		_, err = h.client.AppsV1().Deployments(ref.Namespace).Get(ctx, ref.Name, metav1.GetOptions{})
	case WorkloadKindStatefulSet:
		_, err = h.client.AppsV1().StatefulSets(ref.Namespace).Get(ctx, ref.Name, metav1.GetOptions{})
	}
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}
