package disruptors

import (
	"context"
	"errors"
	"fmt"

	"github.com/danhngo-lx/xk6-disruptor/pkg/kubernetes/helpers"
)

// ErrSelectorNoWorkloads is returned when a WorkloadSelector matches no workloads.
var ErrSelectorNoWorkloads = errors.New("no workloads found matching selector")

// WorkloadAttributes defines name/label criteria for selecting workloads.
type WorkloadAttributes struct {
	// Name selects a single workload by exact name.
	Name string `js:"name"`
	// Labels selects workloads matching all label key/value pairs.
	Labels map[string]string `js:"labels"`
}

// WorkloadSelectorSpec defines criteria for selecting Deployments or StatefulSets.
type WorkloadSelectorSpec struct {
	// Kind is the workload kind: "Deployment" or "StatefulSet".
	Kind string `js:"kind"`
	// Namespace scopes the selection (defaults to "default" when empty).
	Namespace string `js:"namespace"`
	// Select narrows the search by name or labels.
	Select WorkloadAttributes `js:"select"`
}

// NamespaceOrDefault returns the configured namespace or "default".
func (s WorkloadSelectorSpec) NamespaceOrDefault() string {
	if s.Namespace != "" {
		return s.Namespace
	}
	return "default"
}

// WorkloadSelector resolves a WorkloadSelectorSpec to concrete workload refs.
type WorkloadSelector struct {
	spec   WorkloadSelectorSpec
	helper helpers.WorkloadHelper
}

// NewWorkloadSelector validates the spec and returns a WorkloadSelector.
func NewWorkloadSelector(spec WorkloadSelectorSpec, helper helpers.WorkloadHelper) (*WorkloadSelector, error) {
	switch spec.Kind {
	case helpers.WorkloadKindDeployment, helpers.WorkloadKindStatefulSet:
	case "":
		return nil, fmt.Errorf("kind is required (Deployment or StatefulSet)")
	default:
		return nil, fmt.Errorf("unsupported kind %q (supported: Deployment, StatefulSet)", spec.Kind)
	}

	if spec.Select.Name == "" && len(spec.Select.Labels) == 0 {
		return nil, fmt.Errorf("select.name or select.labels must be provided")
	}
	if spec.Select.Name != "" && len(spec.Select.Labels) > 0 {
		return nil, fmt.Errorf("provide either select.name or select.labels, not both")
	}

	return &WorkloadSelector{spec: spec, helper: helper}, nil
}

// Targets returns the workload refs matching the selector.
func (s *WorkloadSelector) Targets(ctx context.Context) ([]helpers.WorkloadRef, error) {
	namespace := s.spec.NamespaceOrDefault()

	if s.spec.Select.Name != "" {
		ref := helpers.WorkloadRef{Kind: s.spec.Kind, Namespace: namespace, Name: s.spec.Select.Name}
		exists, err := s.helper.Exists(ctx, ref)
		if err != nil {
			return nil, fmt.Errorf("looking up %s: %w", ref, err)
		}
		if !exists {
			return nil, fmt.Errorf("%s: %w", ref, ErrSelectorNoWorkloads)
		}
		return []helpers.WorkloadRef{ref}, nil
	}

	refs, err := s.helper.List(ctx, s.spec.Kind, namespace, s.spec.Select.Labels)
	if err != nil {
		return nil, fmt.Errorf("listing %s in %s: %w", s.spec.Kind, namespace, err)
	}
	if len(refs) == 0 {
		return nil, fmt.Errorf("%s in ns %q with labels %v: %w",
			s.spec.Kind, namespace, s.spec.Select.Labels, ErrSelectorNoWorkloads)
	}
	return refs, nil
}
