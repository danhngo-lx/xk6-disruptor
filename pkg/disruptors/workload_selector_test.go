package disruptors

import (
	"errors"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/danhngo-lx/xk6-disruptor/pkg/kubernetes/helpers"
)

func newDeployment(name, ns string, labels map[string]string, replicas int32) *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
			Labels:    labels,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
		},
	}
}

func newStatefulSet(name, ns string, labels map[string]string, replicas int32) *appsv1.StatefulSet {
	return &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
			Labels:    labels,
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &replicas,
		},
	}
}

func TestNewWorkloadSelector(t *testing.T) {
	t.Parallel()

	helper := helpers.NewWorkloadHelper(fake.NewSimpleClientset())

	testCases := []struct {
		title   string
		spec    WorkloadSelectorSpec
		wantErr bool
	}{
		{
			title:   "deployment by name ok",
			spec:    WorkloadSelectorSpec{Kind: "Deployment", Namespace: "ns", Select: WorkloadAttributes{Name: "app"}},
			wantErr: false,
		},
		{
			title:   "statefulset by labels ok",
			spec:    WorkloadSelectorSpec{Kind: "StatefulSet", Namespace: "ns", Select: WorkloadAttributes{Labels: map[string]string{"app": "db"}}},
			wantErr: false,
		},
		{
			title:   "missing kind",
			spec:    WorkloadSelectorSpec{Namespace: "ns", Select: WorkloadAttributes{Name: "app"}},
			wantErr: true,
		},
		{
			title:   "unsupported kind",
			spec:    WorkloadSelectorSpec{Kind: "DaemonSet", Namespace: "ns", Select: WorkloadAttributes{Name: "app"}},
			wantErr: true,
		},
		{
			title:   "no selector",
			spec:    WorkloadSelectorSpec{Kind: "Deployment", Namespace: "ns"},
			wantErr: true,
		},
		{
			title:   "both name and labels",
			spec:    WorkloadSelectorSpec{Kind: "Deployment", Namespace: "ns", Select: WorkloadAttributes{Name: "app", Labels: map[string]string{"a": "b"}}},
			wantErr: true,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.title, func(t *testing.T) {
			t.Parallel()
			_, err := NewWorkloadSelector(tc.spec, helper)
			if tc.wantErr && err == nil {
				t.Errorf("expected error")
			}
			if !tc.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestWorkloadSelector_Targets(t *testing.T) {
	t.Parallel()

	client := fake.NewSimpleClientset(
		newDeployment("api", "ns", map[string]string{"app": "api"}, 3),
		newDeployment("worker", "ns", map[string]string{"app": "worker"}, 2),
		newDeployment("other", "other-ns", map[string]string{"app": "api"}, 1),
		newStatefulSet("db", "ns", map[string]string{"app": "db"}, 1),
	)
	helper := helpers.NewWorkloadHelper(client)

	t.Run("by name resolves single deployment", func(t *testing.T) {
		t.Parallel()
		sel, err := NewWorkloadSelector(
			WorkloadSelectorSpec{Kind: "Deployment", Namespace: "ns", Select: WorkloadAttributes{Name: "api"}},
			helper,
		)
		if err != nil {
			t.Fatalf("NewWorkloadSelector: %v", err)
		}
		refs, err := sel.Targets(t.Context())
		if err != nil {
			t.Fatalf("Targets: %v", err)
		}
		if len(refs) != 1 || refs[0].Name != "api" || refs[0].Namespace != "ns" {
			t.Errorf("unexpected refs: %+v", refs)
		}
	})

	t.Run("by name missing workload errors", func(t *testing.T) {
		t.Parallel()
		sel, err := NewWorkloadSelector(
			WorkloadSelectorSpec{Kind: "Deployment", Namespace: "ns", Select: WorkloadAttributes{Name: "ghost"}},
			helper,
		)
		if err != nil {
			t.Fatalf("NewWorkloadSelector: %v", err)
		}
		_, err = sel.Targets(t.Context())
		if err == nil {
			t.Error("expected error for missing workload")
		}
	})

	t.Run("by labels filters by namespace", func(t *testing.T) {
		t.Parallel()
		sel, err := NewWorkloadSelector(
			WorkloadSelectorSpec{Kind: "Deployment", Namespace: "ns", Select: WorkloadAttributes{Labels: map[string]string{"app": "api"}}},
			helper,
		)
		if err != nil {
			t.Fatalf("NewWorkloadSelector: %v", err)
		}
		refs, err := sel.Targets(t.Context())
		if err != nil {
			t.Fatalf("Targets: %v", err)
		}
		if len(refs) != 1 || refs[0].Name != "api" {
			t.Errorf("expected one deployment 'api', got %+v", refs)
		}
	})

	t.Run("by labels matches none", func(t *testing.T) {
		t.Parallel()
		sel, err := NewWorkloadSelector(
			WorkloadSelectorSpec{Kind: "Deployment", Namespace: "ns", Select: WorkloadAttributes{Labels: map[string]string{"app": "nope"}}},
			helper,
		)
		if err != nil {
			t.Fatalf("NewWorkloadSelector: %v", err)
		}
		_, err = sel.Targets(t.Context())
		if !errors.Is(err, ErrSelectorNoWorkloads) {
			t.Errorf("expected ErrSelectorNoWorkloads, got %v", err)
		}
	})

	t.Run("statefulset by labels", func(t *testing.T) {
		t.Parallel()
		sel, err := NewWorkloadSelector(
			WorkloadSelectorSpec{Kind: "StatefulSet", Namespace: "ns", Select: WorkloadAttributes{Labels: map[string]string{"app": "db"}}},
			helper,
		)
		if err != nil {
			t.Fatalf("NewWorkloadSelector: %v", err)
		}
		refs, err := sel.Targets(t.Context())
		if err != nil {
			t.Fatalf("Targets: %v", err)
		}
		if len(refs) != 1 || refs[0].Kind != "StatefulSet" {
			t.Errorf("expected StatefulSet, got %+v", refs)
		}
	})
}
