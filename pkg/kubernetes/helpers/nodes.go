package helpers

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
)

// NodeEvictOptions defines options for evicting pods from a node
type NodeEvictOptions struct {
	// SkipDaemonSets controls whether DaemonSet-owned pods are skipped during eviction
	SkipDaemonSets bool
	// DeleteLocalData controls whether pods with local storage (emptyDir/hostPath) are evicted
	DeleteLocalData bool
	// Timeout is the per-pod eviction timeout; defaults to 300s when zero
	Timeout time.Duration
}

// PrivilegedPodSpec defines the spec for creating a privileged helper pod on a specific node
type PrivilegedPodSpec struct {
	// Name is the pod name
	Name string
	// Namespace is the pod namespace
	Namespace string
	// NodeName is the node the pod must be scheduled on
	NodeName string
	// Image is the container image to run
	Image string
	// Command overrides the image's ENTRYPOINT (argv[0])
	Command []string
	// Args are the arguments passed to the command
	Args []string
}

// NodeHelper defines helper methods for managing Kubernetes nodes
type NodeHelper interface {
	// List returns nodes matching the given label selector
	List(ctx context.Context, labelSelector map[string]string) ([]corev1.Node, error)
	// Get returns a specific node by name
	Get(ctx context.Context, name string) (corev1.Node, error)
	// Cordon marks the node as unschedulable
	Cordon(ctx context.Context, name string) error
	// Uncordon marks the node as schedulable
	Uncordon(ctx context.Context, name string) error
	// AddTaint adds a taint to the node; no-ops if the taint already exists
	AddTaint(ctx context.Context, name string, taint corev1.Taint) error
	// RemoveTaint removes all taints whose key matches taintKey
	RemoveTaint(ctx context.Context, name string, taintKey string) error
	// EvictPods evicts all eligible pods from the given node
	EvictPods(ctx context.Context, nodeName string, options NodeEvictOptions) error
	// WaitForReady blocks until the node becomes Ready or ctx/timeout expires
	WaitForReady(ctx context.Context, name string, timeout time.Duration) error
	// CreatePrivilegedPod creates a privileged pod on the specified node and returns it
	CreatePrivilegedPod(ctx context.Context, spec PrivilegedPodSpec) (corev1.Pod, error)
	// DeletePod deletes a pod; silently ignores not-found errors
	DeletePod(ctx context.Context, namespace, name string) error
	// WaitPodCompleted blocks until the pod reaches Succeeded or Failed phase
	WaitPodCompleted(ctx context.Context, namespace, name string, timeout time.Duration) error
}

type nodeHelper struct {
	client kubernetes.Interface
}

// NewNodeHelper returns a NodeHelper backed by the given Kubernetes client
func NewNodeHelper(client kubernetes.Interface) NodeHelper {
	return &nodeHelper{client: client}
}

func (h *nodeHelper) List(ctx context.Context, labelSelector map[string]string) ([]corev1.Node, error) {
	sel := labels.Set(labelSelector).String()
	list, err := h.client.CoreV1().Nodes().List(ctx, metav1.ListOptions{LabelSelector: sel})
	if err != nil {
		return nil, err
	}
	return list.Items, nil
}

func (h *nodeHelper) Get(ctx context.Context, name string) (corev1.Node, error) {
	node, err := h.client.CoreV1().Nodes().Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return corev1.Node{}, err
	}
	return *node, nil
}

func (h *nodeHelper) Cordon(ctx context.Context, name string) error {
	patch := []byte(`{"spec":{"unschedulable":true}}`)
	_, err := h.client.CoreV1().Nodes().Patch(ctx, name, types.MergePatchType, patch, metav1.PatchOptions{})
	return err
}

func (h *nodeHelper) Uncordon(ctx context.Context, name string) error {
	patch := []byte(`{"spec":{"unschedulable":false}}`)
	_, err := h.client.CoreV1().Nodes().Patch(ctx, name, types.MergePatchType, patch, metav1.PatchOptions{})
	return err
}

func (h *nodeHelper) AddTaint(ctx context.Context, name string, taint corev1.Taint) error {
	node, err := h.client.CoreV1().Nodes().Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return err
	}

	for _, t := range node.Spec.Taints {
		if t.Key == taint.Key && t.Effect == taint.Effect {
			return nil // already present
		}
	}

	node.Spec.Taints = append(node.Spec.Taints, taint)
	_, err = h.client.CoreV1().Nodes().Update(ctx, node, metav1.UpdateOptions{})
	return err
}

func (h *nodeHelper) RemoveTaint(ctx context.Context, name string, taintKey string) error {
	node, err := h.client.CoreV1().Nodes().Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return err
	}

	filtered := node.Spec.Taints[:0]
	for _, t := range node.Spec.Taints {
		if t.Key != taintKey {
			filtered = append(filtered, t)
		}
	}
	node.Spec.Taints = filtered

	_, err = h.client.CoreV1().Nodes().Update(ctx, node, metav1.UpdateOptions{})
	return err
}

func (h *nodeHelper) EvictPods(ctx context.Context, nodeName string, options NodeEvictOptions) error {
	pods, err := h.client.CoreV1().Pods("").List(ctx, metav1.ListOptions{
		FieldSelector: fmt.Sprintf("spec.nodeName=%s", nodeName),
	})
	if err != nil {
		return err
	}

	var evictErrs []error
	for i := range pods.Items {
		pod := &pods.Items[i]

		if options.SkipDaemonSets {
			isDaemonSet := false
			for _, ref := range pod.OwnerReferences {
				if ref.Kind == "DaemonSet" {
					isDaemonSet = true
					break
				}
			}
			if isDaemonSet {
				continue
			}
		}

		if !options.DeleteLocalData {
			hasLocal := false
			for _, vol := range pod.Spec.Volumes {
				if vol.EmptyDir != nil || vol.HostPath != nil {
					hasLocal = true
					break
				}
			}
			if hasLocal {
				continue
			}
		}

		eviction := &policyv1.Eviction{
			ObjectMeta: metav1.ObjectMeta{
				Name:      pod.Name,
				Namespace: pod.Namespace,
			},
		}
		if err := h.client.PolicyV1().Evictions(pod.Namespace).Evict(ctx, eviction); err != nil && !k8serrors.IsNotFound(err) {
			evictErrs = append(evictErrs, fmt.Errorf("evicting pod %s/%s: %w", pod.Namespace, pod.Name, err))
		}
	}

	if len(evictErrs) > 0 {
		return fmt.Errorf("errors evicting pods from node %s: %v", nodeName, evictErrs)
	}
	return nil
}

func (h *nodeHelper) WaitForReady(ctx context.Context, name string, timeout time.Duration) error {
	secs := int64(timeout.Seconds())
	watcher, err := h.client.CoreV1().Nodes().Watch(ctx, metav1.ListOptions{
		FieldSelector:  fmt.Sprintf("metadata.name=%s", name),
		TimeoutSeconds: &secs,
	})
	if err != nil {
		return err
	}
	defer watcher.Stop()

	for {
		select {
		case event, ok := <-watcher.ResultChan():
			if !ok {
				return fmt.Errorf("watch channel closed while waiting for node %s to become Ready", name)
			}
			if event.Type == watch.Modified || event.Type == watch.Added {
				node, ok := event.Object.(*corev1.Node)
				if !ok {
					continue
				}
				for _, cond := range node.Status.Conditions {
					if cond.Type == corev1.NodeReady && cond.Status == corev1.ConditionTrue {
						return nil
					}
				}
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (h *nodeHelper) CreatePrivilegedPod(ctx context.Context, spec PrivilegedPodSpec) (corev1.Pod, error) {
	privileged := true
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      spec.Name,
			Namespace: spec.Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": "xk6-disruptor",
			},
		},
		Spec: corev1.PodSpec{
			NodeName:      spec.NodeName,
			HostPID:       true,
			RestartPolicy: corev1.RestartPolicyNever,
			Containers: []corev1.Container{
				{
					Name:    "xk6-agent",
					Image:   spec.Image,
					Command: spec.Command,
					Args:    spec.Args,
					SecurityContext: &corev1.SecurityContext{
						Privileged: &privileged,
					},
				},
			},
		},
	}

	created, err := h.client.CoreV1().Pods(spec.Namespace).Create(ctx, pod, metav1.CreateOptions{})
	if err != nil {
		return corev1.Pod{}, err
	}
	return *created, nil
}

func (h *nodeHelper) DeletePod(ctx context.Context, namespace, name string) error {
	err := h.client.CoreV1().Pods(namespace).Delete(ctx, name, metav1.DeleteOptions{})
	if k8serrors.IsNotFound(err) {
		return nil
	}
	return err
}

func (h *nodeHelper) WaitPodCompleted(ctx context.Context, namespace, name string, timeout time.Duration) error {
	secs := int64(timeout.Seconds())
	watcher, err := h.client.CoreV1().Pods(namespace).Watch(ctx, metav1.ListOptions{
		FieldSelector:  fmt.Sprintf("metadata.name=%s", name),
		TimeoutSeconds: &secs,
	})
	if err != nil {
		return err
	}
	defer watcher.Stop()

	for {
		select {
		case event, ok := <-watcher.ResultChan():
			if !ok {
				return fmt.Errorf("watch channel closed while waiting for pod %s/%s to complete", namespace, name)
			}
			pod, ok := event.Object.(*corev1.Pod)
			if !ok {
				continue
			}
			switch pod.Status.Phase {
			case corev1.PodSucceeded:
				return nil
			case corev1.PodFailed:
				return fmt.Errorf("privileged pod %s/%s failed", namespace, name)
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}
