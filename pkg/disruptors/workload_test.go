package disruptors

import (
	"context"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/danhngo-lx/xk6-disruptor/pkg/kubernetes"
)

func TestNewWorkloadDisruptor(t *testing.T) {
	t.Parallel()

	t.Run("succeeds when target exists", func(t *testing.T) {
		t.Parallel()
		client := fake.NewSimpleClientset(newDeployment("api", "ns", nil, 3))
		k8s, _ := kubernetes.NewFakeKubernetes(client)

		_, err := NewWorkloadDisruptor(t.Context(), k8s, WorkloadSelectorSpec{
			Kind:      "Deployment",
			Namespace: "ns",
			Select:    WorkloadAttributes{Name: "api"},
		})
		if err != nil {
			t.Fatalf("NewWorkloadDisruptor: %v", err)
		}
	})

	t.Run("fails when target missing", func(t *testing.T) {
		t.Parallel()
		client := fake.NewSimpleClientset()
		k8s, _ := kubernetes.NewFakeKubernetes(client)

		_, err := NewWorkloadDisruptor(t.Context(), k8s, WorkloadSelectorSpec{
			Kind:      "Deployment",
			Namespace: "ns",
			Select:    WorkloadAttributes{Name: "ghost"},
		})
		if err == nil {
			t.Error("expected error for missing target")
		}
	})

	t.Run("fails for invalid spec", func(t *testing.T) {
		t.Parallel()
		client := fake.NewSimpleClientset()
		k8s, _ := kubernetes.NewFakeKubernetes(client)

		_, err := NewWorkloadDisruptor(t.Context(), k8s, WorkloadSelectorSpec{
			Kind: "DaemonSet",
		})
		if err == nil {
			t.Error("expected error for unsupported kind")
		}
	})
}

func getDeploymentReplicas(t *testing.T, client *fake.Clientset, ns, name string) int32 {
	t.Helper()
	d, err := client.AppsV1().Deployments(ns).Get(context.Background(), name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Get deployment: %v", err)
	}
	if d.Spec.Replicas == nil {
		return 0
	}
	return *d.Spec.Replicas
}

func getStatefulSetReplicas(t *testing.T, client *fake.Clientset, ns, name string) int32 {
	t.Helper()
	s, err := client.AppsV1().StatefulSets(ns).Get(context.Background(), name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Get statefulset: %v", err)
	}
	if s.Spec.Replicas == nil {
		return 0
	}
	return *s.Spec.Replicas
}

func TestWorkloadDisruptor_ScaleReplicas_NoAutoRevert(t *testing.T) {
	t.Parallel()

	client := fake.NewSimpleClientset(newDeployment("api", "ns", nil, 3))
	k8s, _ := kubernetes.NewFakeKubernetes(client)

	d, err := NewWorkloadDisruptor(t.Context(), k8s, WorkloadSelectorSpec{
		Kind:      "Deployment",
		Namespace: "ns",
		Select:    WorkloadAttributes{Name: "api"},
	})
	if err != nil {
		t.Fatalf("NewWorkloadDisruptor: %v", err)
	}

	// scale to 0; should not auto-revert
	err = d.ScaleReplicas(t.Context(), ReplicaChangeFault{Replicas: ptrInt32(0)}, 0)
	if err != nil {
		t.Fatalf("ScaleReplicas: %v", err)
	}

	got := getDeploymentReplicas(t, client, "ns", "api")
	if got != 0 {
		t.Errorf("expected replicas=0, got %d", got)
	}

	// Cleanup restores original
	if err := d.Cleanup(t.Context()); err != nil {
		t.Fatalf("Cleanup: %v", err)
	}
	got = getDeploymentReplicas(t, client, "ns", "api")
	if got != 3 {
		t.Errorf("after Cleanup, expected replicas=3, got %d", got)
	}
}

func TestWorkloadDisruptor_ScaleReplicas_AutoRevert(t *testing.T) {
	t.Parallel()

	client := fake.NewSimpleClientset(newDeployment("api", "ns", nil, 4))
	k8s, _ := kubernetes.NewFakeKubernetes(client)

	d, err := NewWorkloadDisruptor(t.Context(), k8s, WorkloadSelectorSpec{
		Kind:      "Deployment",
		Namespace: "ns",
		Select:    WorkloadAttributes{Name: "api"},
	})
	if err != nil {
		t.Fatalf("NewWorkloadDisruptor: %v", err)
	}

	start := time.Now()
	err = d.ScaleReplicas(t.Context(),
		ReplicaChangeFault{Percentage: ptrInt32(50), AutoRevert: true},
		50*time.Millisecond,
	)
	if err != nil {
		t.Fatalf("ScaleReplicas: %v", err)
	}
	elapsed := time.Since(start)
	if elapsed < 50*time.Millisecond {
		t.Errorf("expected to wait at least 50ms, waited %s", elapsed)
	}

	got := getDeploymentReplicas(t, client, "ns", "api")
	if got != 4 {
		t.Errorf("expected reverted replicas=4, got %d", got)
	}
}

func TestWorkloadDisruptor_ScaleReplicas_Delta(t *testing.T) {
	t.Parallel()

	client := fake.NewSimpleClientset(newDeployment("api", "ns", nil, 5))
	k8s, _ := kubernetes.NewFakeKubernetes(client)

	d, err := NewWorkloadDisruptor(t.Context(), k8s, WorkloadSelectorSpec{
		Kind:      "Deployment",
		Namespace: "ns",
		Select:    WorkloadAttributes{Name: "api"},
	})
	if err != nil {
		t.Fatalf("NewWorkloadDisruptor: %v", err)
	}

	err = d.ScaleReplicas(t.Context(), ReplicaChangeFault{Delta: ptrInt32(-100)}, 0)
	if err != nil {
		t.Fatalf("ScaleReplicas: %v", err)
	}

	got := getDeploymentReplicas(t, client, "ns", "api")
	if got != 0 {
		t.Errorf("expected delta clamped to 0, got %d", got)
	}
}

func TestWorkloadDisruptor_ScaleReplicas_LabelSelectorMultipleTargets(t *testing.T) {
	t.Parallel()

	client := fake.NewSimpleClientset(
		newDeployment("a", "ns", map[string]string{"team": "alpha"}, 3),
		newDeployment("b", "ns", map[string]string{"team": "alpha"}, 2),
		newDeployment("c", "ns", map[string]string{"team": "beta"}, 4),
	)
	k8s, _ := kubernetes.NewFakeKubernetes(client)

	d, err := NewWorkloadDisruptor(t.Context(), k8s, WorkloadSelectorSpec{
		Kind:      "Deployment",
		Namespace: "ns",
		Select:    WorkloadAttributes{Labels: map[string]string{"team": "alpha"}},
	})
	if err != nil {
		t.Fatalf("NewWorkloadDisruptor: %v", err)
	}

	if err := d.ScaleReplicas(t.Context(), ReplicaChangeFault{Replicas: ptrInt32(0)}, 0); err != nil {
		t.Fatalf("ScaleReplicas: %v", err)
	}

	if r := getDeploymentReplicas(t, client, "ns", "a"); r != 0 {
		t.Errorf("a: expected 0, got %d", r)
	}
	if r := getDeploymentReplicas(t, client, "ns", "b"); r != 0 {
		t.Errorf("b: expected 0, got %d", r)
	}
	if r := getDeploymentReplicas(t, client, "ns", "c"); r != 4 {
		t.Errorf("c (not selected): expected 4, got %d", r)
	}

	if err := d.Cleanup(t.Context()); err != nil {
		t.Fatalf("Cleanup: %v", err)
	}
	if r := getDeploymentReplicas(t, client, "ns", "a"); r != 3 {
		t.Errorf("a after cleanup: expected 3, got %d", r)
	}
	if r := getDeploymentReplicas(t, client, "ns", "b"); r != 2 {
		t.Errorf("b after cleanup: expected 2, got %d", r)
	}
}

func TestWorkloadDisruptor_StatefulSet(t *testing.T) {
	t.Parallel()

	client := fake.NewSimpleClientset(newStatefulSet("db", "ns", nil, 2))
	k8s, _ := kubernetes.NewFakeKubernetes(client)

	d, err := NewWorkloadDisruptor(t.Context(), k8s, WorkloadSelectorSpec{
		Kind:      "StatefulSet",
		Namespace: "ns",
		Select:    WorkloadAttributes{Name: "db"},
	})
	if err != nil {
		t.Fatalf("NewWorkloadDisruptor: %v", err)
	}

	if err := d.ScaleReplicas(t.Context(), ReplicaChangeFault{Delta: ptrInt32(1)}, 0); err != nil {
		t.Fatalf("ScaleReplicas: %v", err)
	}

	if r := getStatefulSetReplicas(t, client, "ns", "db"); r != 3 {
		t.Errorf("expected replicas=3, got %d", r)
	}
}
