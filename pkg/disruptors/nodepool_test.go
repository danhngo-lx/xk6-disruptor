package disruptors

import (
	"testing"
	"time"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/danhngo-lx/xk6-disruptor/pkg/kubernetes"
	"github.com/danhngo-lx/xk6-disruptor/pkg/kubernetes/helpers"
)

func TestNodePoolDisruptor_InjectFillerWorkload(t *testing.T) {
	t.Parallel()

	client := fake.NewSimpleClientset()
	k8s, _ := kubernetes.NewFakeKubernetes(client)

	d, err := NewNodePoolDisruptor(t.Context(), k8s, "ns")
	if err != nil {
		t.Fatalf("NewNodePoolDisruptor: %v", err)
	}

	fault := FillerWorkloadFault{
		Replicas:         3,
		CPUPerReplica:    "500m",
		MemoryPerReplica: "512Mi",
		NodeSelector:     map[string]string{"agentpool": "myPool"},
	}

	if err := d.InjectFillerWorkload(t.Context(), fault, 10*time.Millisecond); err != nil {
		t.Fatalf("InjectFillerWorkload: %v", err)
	}

	_, err = client.AppsV1().Deployments("ns").Get(t.Context(), "xk6-disruptor-filler", metav1.GetOptions{})
	if !k8serrors.IsNotFound(err) {
		t.Errorf("expected filler deployment to be deleted after duration, got err=%v", err)
	}
}

func TestNodePoolDisruptor_Cleanup(t *testing.T) {
	t.Parallel()

	client := fake.NewSimpleClientset()
	k8s, _ := kubernetes.NewFakeKubernetes(client)

	d, err := NewNodePoolDisruptor(t.Context(), k8s, "ns")
	if err != nil {
		t.Fatalf("NewNodePoolDisruptor: %v", err)
	}

	// Cleanup with nothing to clean up is a safe no-op.
	if err := d.Cleanup(t.Context()); err != nil {
		t.Errorf("Cleanup on empty state should be a no-op: %v", err)
	}

	// Create the filler out-of-band (bypassing the duration wait) and confirm
	// Cleanup deletes it.
	fault := FillerWorkloadFault{Replicas: 1, CPUPerReplica: "100m"}
	nd, ok := d.(*nodePoolDisruptor)
	if !ok {
		t.Fatalf("expected *nodePoolDisruptor, got %T", d)
	}
	if err := nd.helper.CreateFiller(t.Context(), helperFillerSpec(nd, fault)); err != nil {
		t.Fatalf("CreateFiller: %v", err)
	}

	if err := d.Cleanup(t.Context()); err != nil {
		t.Fatalf("Cleanup: %v", err)
	}

	_, err = client.AppsV1().Deployments("ns").Get(t.Context(), "xk6-disruptor-filler", metav1.GetOptions{})
	if !k8serrors.IsNotFound(err) {
		t.Errorf("expected filler deployment to be deleted, got err=%v", err)
	}
}

func helperFillerSpec(nd *nodePoolDisruptor, fault FillerWorkloadFault) helpers.FillerSpec {
	return helpers.FillerSpec{
		Name:      nd.name,
		Namespace: nd.namespace,
		Replicas:  fault.Replicas,
		CPU:       fault.CPUPerReplica,
		Memory:    fault.MemoryPerReplica,
	}
}

func TestFillerWorkloadFault_Validate(t *testing.T) {
	t.Parallel()

	if err := (FillerWorkloadFault{}).Validate(); err == nil {
		t.Error("expected error for zero replicas")
	}
	if err := (FillerWorkloadFault{Replicas: 1}).Validate(); err == nil {
		t.Error("expected error when no cpu/memory set")
	}
	if err := (FillerWorkloadFault{Replicas: 1, CPUPerReplica: "1"}).Validate(); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}
