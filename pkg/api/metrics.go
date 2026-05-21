package api

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"go.k6.io/k6/js/modules"
	"go.k6.io/k6/lib"
	"go.k6.io/k6/metrics"
)

// Metrics holds the k6 metrics registered by the xk6-disruptor extension.
// One instance is registered per test run (shared across VUs) and referenced
// by every disruptor created by the module.
type Metrics struct {
	FaultActive     *metrics.Metric
	FaultsInjected  *metrics.Metric
	FaultsFailed    *metrics.Metric
	FaultDuration   *metrics.Metric
	TargetsSelected *metrics.Metric
}

// RegisterMetrics creates the disruptor metrics and registers them in r.
func RegisterMetrics(r *metrics.Registry) *Metrics {
	return &Metrics{
		FaultActive:     r.MustNewMetric("xk6_disruptor_fault_active", metrics.Gauge),
		FaultsInjected:  r.MustNewMetric("xk6_disruptor_faults_injected_total", metrics.Counter),
		FaultsFailed:    r.MustNewMetric("xk6_disruptor_faults_failed_total", metrics.Counter),
		FaultDuration:   r.MustNewMetric("xk6_disruptor_fault_duration_seconds", metrics.Trend),
		TargetsSelected: r.MustNewMetric("xk6_disruptor_targets_selected", metrics.Gauge),
	}
}

// TargetInfo identifies the entity a fault is injected against. It is used to
// tag every emitted sample so dashboards / annotations can distinguish faults
// by disruptor type, namespace, and target name.
type TargetInfo struct {
	Disruptor string // "pod", "service" or "node"
	Namespace string // k8s namespace (empty for cluster-scoped node faults)
	Name      string // service name, node name, or serialized pod label selector
}

// Track wraps a fault-injection call with metric emission:
//   - fault_active gauge goes to 1 before fn runs and back to 0 after
//   - faults_injected_total counter increments at start
//   - fault_duration_seconds trend records the wall-clock duration of fn
//   - faults_failed_total counter increments on error with an error_class tag
//
// When the VU has no state (e.g. inside setup()/teardown()), Track skips
// metric emission and just runs fn.
func (m *Metrics) Track(
	ctx context.Context,
	vu modules.VU,
	faultType string,
	target TargetInfo,
	fn func() error,
) error {
	if m == nil || vu == nil {
		return fn()
	}
	state := vu.State()
	if state == nil {
		return fn()
	}

	base := buildTags(state, faultType, target)
	start := time.Now()

	pushSample(ctx, state.Samples, m.FaultActive, base, 1)
	pushSample(ctx, state.Samples, m.FaultsInjected, base, 1)

	err := fn()

	pushSample(ctx, state.Samples, m.FaultActive, base, 0)

	outcome := "success"
	if err != nil {
		outcome = "error"
		errTags := base.Clone()
		errTags.SetTag("error_class", classifyError(err))
		pushSample(ctx, state.Samples, m.FaultsFailed, errTags, 1)
	}

	durTags := base.Clone()
	durTags.SetTag("outcome", outcome)
	pushSample(ctx, state.Samples, m.FaultDuration, durTags, time.Since(start).Seconds())

	return err
}

// EmitTargetsSelected emits the targets_selected gauge. Safe to call when the
// VU has no state (no-op in that case).
func (m *Metrics) EmitTargetsSelected(ctx context.Context, vu modules.VU, target TargetInfo, count int) {
	if m == nil || vu == nil {
		return
	}
	state := vu.State()
	if state == nil {
		return
	}
	base := buildTags(state, "", target)
	pushSample(ctx, state.Samples, m.TargetsSelected, base, float64(count))
}

func buildTags(state *lib.State, faultType string, target TargetInfo) metrics.TagsAndMeta {
	ctm := state.Tags.GetCurrentValues()
	if faultType != "" {
		ctm.SetTag("fault_type", faultType)
	}
	ctm.SetTag("disruptor", target.Disruptor)
	ctm.SetTag("target_namespace", target.Namespace)
	ctm.SetTag("target_name", target.Name)
	return ctm
}

func pushSample(
	ctx context.Context,
	out chan<- metrics.SampleContainer,
	metric *metrics.Metric,
	tm metrics.TagsAndMeta,
	value float64,
) {
	metrics.PushIfNotDone(ctx, out, metrics.Sample{
		TimeSeries: metrics.TimeSeries{Metric: metric, Tags: tm.Tags},
		Time:       time.Now(),
		Value:      value,
		Metadata:   tm.Metadata,
	})
}

// classifyError maps a disruptor error to a coarse error_class tag used on the
// faults_failed_total counter. The classification is intentionally shallow —
// it's a faceting hint for dashboards, not a structured error type.
func classifyError(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	if errors.Is(err, context.Canceled) {
		return "canceled"
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "timeout"), strings.Contains(msg, "timed out"):
		return "timeout"
	case strings.Contains(msg, "exec"):
		return "exec_failed"
	case strings.Contains(msg, "inject"):
		return "inject_failed"
	default:
		return "other"
	}
}

// FormatPodSelector serializes a PodDisruptor's label selectors into a stable
// string suitable for the target_name tag. Inclusion labels are written as
// key=value and exclusion labels as !key=value; keys are sorted for stability.
// Returns "*" when no labels are configured.
func FormatPodSelector(includeLabels, excludeLabels map[string]string) string {
	parts := make([]string, 0, len(includeLabels)+len(excludeLabels))
	for _, k := range sortedKeys(includeLabels) {
		parts = append(parts, k+"="+includeLabels[k])
	}
	for _, k := range sortedKeys(excludeLabels) {
		parts = append(parts, "!"+k+"="+excludeLabels[k])
	}
	if len(parts) == 0 {
		return "*"
	}
	return strings.Join(parts, ",")
}

// tracker bundles per-disruptor metric context (VU, Metrics, TargetInfo) so
// every sub-injector composed into a JS disruptor object can emit samples
// without each method having to re-assemble the tags. A nil tracker (or one
// with no metrics) is treated as a no-op, which is what tests pass.
type tracker struct {
	vu      modules.VU
	metrics *Metrics
	target  TargetInfo
}

func newTracker(vu modules.VU, m *Metrics, target TargetInfo) *tracker {
	return &tracker{vu: vu, metrics: m, target: target}
}

// track wraps fn with start/stop emission for the given fault type.
func (t *tracker) track(ctx context.Context, faultType string, fn func() error) error {
	if t == nil || t.metrics == nil {
		return fn()
	}
	return t.metrics.Track(ctx, t.vu, faultType, t.target, fn)
}

// emitTargetsSelected reports how many targets the disruptor's selector matched.
func (t *tracker) emitTargetsSelected(ctx context.Context, count int) {
	if t == nil || t.metrics == nil {
		return
	}
	t.metrics.EmitTargetsSelected(ctx, t.vu, t.target, count)
}

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
