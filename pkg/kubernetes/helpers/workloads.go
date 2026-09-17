package helpers

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
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
	// CreateFiller creates (or replaces) a throwaway low-priority Deployment sized to
	// consume node capacity, used to simulate node-pool/cluster-autoscaler exhaustion.
	CreateFiller(ctx context.Context, spec FillerSpec) error
	// DeleteFiller deletes the filler Deployment; silently ignores not-found errors.
	DeleteFiller(ctx context.Context, namespace, name string) error
}

// FillerSpec defines a throwaway filler Deployment used to consume node allocatable
// capacity (CPU/memory) in order to simulate node-pool/cluster-autoscaler exhaustion.
type FillerSpec struct {
	// Name is the Deployment name.
	Name string
	// Namespace is the Deployment namespace.
	Namespace string
	// Replicas is the number of filler pods to create.
	Replicas int32
	// CPU is the CPU request per replica, in Kubernetes quantity format (e.g. "500m").
	CPU string
	// Memory is the memory request per replica, in Kubernetes quantity format (e.g. "512Mi").
	Memory string
	// NodeSelector constrains the filler pods to a specific node pool.
	NodeSelector map[string]string
	// PriorityClassName controls preemption behaviour; leave empty to use the cluster default.
	PriorityClassName string
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

// fillerImage is a minimal container that idles forever, used to occupy node capacity
// without doing real work.
const fillerImage = "registry.k8s.io/pause:3.9"

func (h *workloadHelper) CreateFiller(ctx context.Context, spec FillerSpec) error {
	requests := corev1.ResourceList{}
	if spec.CPU != "" {
		qty, err := resource.ParseQuantity(spec.CPU)
		if err != nil {
			return fmt.Errorf("parsing cpu quantity %q: %w", spec.CPU, err)
		}
		requests[corev1.ResourceCPU] = qty
	}
	if spec.Memory != "" {
		qty, err := resource.ParseQuantity(spec.Memory)
		if err != nil {
			return fmt.Errorf("parsing memory quantity %q: %w", spec.Memory, err)
		}
		requests[corev1.ResourceMemory] = qty
	}

	labels := map[string]string{"app.kubernetes.io/managed-by": "xk6-disruptor-filler"}
	replicas := spec.Replicas
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      spec.Name,
			Namespace: spec.Namespace,
			Labels:    labels,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: labels},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{
					NodeSelector:      spec.NodeSelector,
					PriorityClassName: spec.PriorityClassName,
					Containers: []corev1.Container{
						{
							Name:  "filler",
							Image: fillerImage,
							Resources: corev1.ResourceRequirements{
								Requests: requests,
							},
						},
					},
				},
			},
		},
	}

	_, err := h.client.AppsV1().Deployments(spec.Namespace).Create(ctx, deployment, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("creating filler deployment %s/%s: %w", spec.Namespace, spec.Name, err)
	}
	return nil
}

func (h *workloadHelper) DeleteFiller(ctx context.Context, namespace, name string) error {
	err := h.client.AppsV1().Deployments(namespace).Delete(ctx, name, metav1.DeleteOptions{})
	if err != nil && !k8serrors.IsNotFound(err) {
		return fmt.Errorf("deleting filler deployment %s/%s: %w", namespace, name, err)
	}
	return nil
}
