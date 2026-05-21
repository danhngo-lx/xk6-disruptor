package api

import (
	"context"
	"errors"
	"testing"

	"github.com/grafana/sobek"
	"go.k6.io/k6/js/common"
	"go.k6.io/k6/lib"
	"go.k6.io/k6/metrics"
)

// fakeVU is a minimal modules.VU implementation that satisfies just enough of
// the interface for Track / EmitTargetsSelected. The fields not exercised by
// these methods (Events, InitEnv, Runtime, RegisterCallback) return zero
// values, which is fine because nothing in the metrics code touches them.
type fakeVU struct {
	ctx   context.Context
	state *lib.State
}

func (v *fakeVU) Context() context.Context              { return v.ctx }
func (v *fakeVU) Events() common.Events                 { return common.Events{} }
func (v *fakeVU) InitEnv() *common.InitEnvironment      { return nil }
func (v *fakeVU) State() *lib.State                     { return v.state }
func (v *fakeVU) Runtime() *sobek.Runtime               { return nil }
func (v *fakeVU) RegisterCallback() func(func() error)  { return func(func() error) {} }

// newTestSetup builds a Metrics instance, a fake VU with a populated lib.State,
// and a buffered samples channel that tests can drain to inspect emissions.
func newTestSetup(t *testing.T) (*Metrics, *fakeVU, chan metrics.SampleContainer) {
	t.Helper()
	registry := metrics.NewRegistry()
	m := RegisterMetrics(registry)
	samples := make(chan metrics.SampleContainer, 64)
	state := &lib.State{
		Samples: samples,
		Tags:    lib.NewVUStateTags(registry.RootTagSet()),
	}
	vu := &fakeVU{ctx: t.Context(), state: state}
	return m, vu, samples
}

// drain reads all samples currently buffered in ch and flattens them into a
// single slice for assertions.
func drain(ch chan metrics.SampleContainer) []metrics.Sample {
	var out []metrics.Sample
	for _, container := range metrics.GetBufferedSamples(ch) {
		out = append(out, container.GetSamples()...)
	}
	return out
}

func findByMetric(samples []metrics.Sample, name string) []metrics.Sample {
	var out []metrics.Sample
	for _, s := range samples {
		if s.Metric.Name == name {
			out = append(out, s)
		}
	}
	return out
}

func TestTrack_EmitsStartStopAndDurationOnSuccess(t *testing.T) {
	t.Parallel()

	m, vu, samples := newTestSetup(t)
	target := TargetInfo{Disruptor: "pod", Namespace: "checkout", Name: "app=frontend"}

	err := m.Track(vu.Context(), vu, "http", target, func() error { return nil })
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := drain(samples)

	active := findByMetric(got, "xk6_disruptor_fault_active")
	if len(active) != 2 {
		t.Fatalf("want 2 fault_active samples (start+stop), got %d", len(active))
	}
	if active[0].Value != 1 {
		t.Errorf("first fault_active sample should be 1 (start), got %v", active[0].Value)
	}
	if active[1].Value != 0 {
		t.Errorf("second fault_active sample should be 0 (stop), got %v", active[1].Value)
	}

	for _, s := range active {
		tags := s.Tags.Map()
		if tags["fault_type"] != "http" {
			t.Errorf("fault_type tag = %q, want http", tags["fault_type"])
		}
		if tags["disruptor"] != "pod" {
			t.Errorf("disruptor tag = %q, want pod", tags["disruptor"])
		}
		if tags["target_namespace"] != "checkout" {
			t.Errorf("target_namespace tag = %q, want checkout", tags["target_namespace"])
		}
		if tags["target_name"] != "app=frontend" {
			t.Errorf("target_name tag = %q, want app=frontend", tags["target_name"])
		}
	}

	injected := findByMetric(got, "xk6_disruptor_faults_injected_total")
	if len(injected) != 1 {
		t.Errorf("want 1 faults_injected_total sample, got %d", len(injected))
	}

	duration := findByMetric(got, "xk6_disruptor_fault_duration_seconds")
	if len(duration) != 1 {
		t.Fatalf("want 1 fault_duration_seconds sample, got %d", len(duration))
	}
	if outcome, _ := duration[0].Tags.Get("outcome"); outcome != "success" {
		t.Errorf("duration outcome tag = %q, want success", outcome)
	}

	if len(findByMetric(got, "xk6_disruptor_faults_failed_total")) != 0 {
		t.Errorf("did not expect a faults_failed_total sample on success")
	}
}

func TestTrack_EmitsFailureSampleAndErrorClassOnError(t *testing.T) {
	t.Parallel()

	m, vu, samples := newTestSetup(t)
	target := TargetInfo{Disruptor: "service", Namespace: "ns", Name: "svc"}

	want := errors.New("agent injection timed out waiting for container")
	err := m.Track(vu.Context(), vu, "http", target, func() error { return want })
	if !errors.Is(err, want) {
		t.Fatalf("Track should return the underlying error, got %v", err)
	}

	got := drain(samples)

	failed := findByMetric(got, "xk6_disruptor_faults_failed_total")
	if len(failed) != 1 {
		t.Fatalf("want 1 faults_failed_total sample, got %d", len(failed))
	}
	if cls, _ := failed[0].Tags.Get("error_class"); cls != "timeout" {
		t.Errorf("error_class tag = %q, want timeout (classified from 'timed out')", cls)
	}

	duration := findByMetric(got, "xk6_disruptor_fault_duration_seconds")
	if len(duration) != 1 {
		t.Fatalf("want 1 duration sample, got %d", len(duration))
	}
	if outcome, _ := duration[0].Tags.Get("outcome"); outcome != "error" {
		t.Errorf("duration outcome tag = %q, want error", outcome)
	}

	// fault_active must still return to 0 even when fn errored, so dashboards
	// don't show a stuck-open fault window.
	active := findByMetric(got, "xk6_disruptor_fault_active")
	if len(active) != 2 || active[1].Value != 0 {
		t.Errorf("fault_active must reach 0 after error, got %v", active)
	}
}

func TestTrack_NoStateIsNoop(t *testing.T) {
	t.Parallel()

	registry := metrics.NewRegistry()
	m := RegisterMetrics(registry)
	vu := &fakeVU{ctx: context.Background(), state: nil} // simulates setup()/teardown()

	called := false
	err := m.Track(vu.Context(), vu, "http", TargetInfo{}, func() error {
		called = true
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Error("fn must still run even when there is no VU state")
	}
}

func TestFormatPodSelector(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		include map[string]string
		exclude map[string]string
		want    string
	}{
		{name: "empty", want: "*"},
		{name: "single include", include: map[string]string{"app": "frontend"}, want: "app=frontend"},
		{
			name:    "sorted include",
			include: map[string]string{"tier": "web", "app": "frontend"},
			want:    "app=frontend,tier=web",
		},
		{
			name:    "include and exclude",
			include: map[string]string{"app": "frontend"},
			exclude: map[string]string{"canary": "true"},
			want:    "app=frontend,!canary=true",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := FormatPodSelector(tc.include, tc.exclude)
			if got != tc.want {
				t.Errorf("FormatPodSelector(%v, %v) = %q, want %q", tc.include, tc.exclude, got, tc.want)
			}
		})
	}
}
