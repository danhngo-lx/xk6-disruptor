// Package api implements a layer between javascript code (via goja)) and the disruptors
// allowing for validations and type conversions when needed
//
// The implementation of the JS API follows the design described in
// https://github.com/danhngo-lx/xk6-disruptor/blob/fix-context-usage/docs/01-development/design-docs/002-js-api-implementation.md
package api

import (
	"context"
	"fmt"
	"reflect"
	"time"

	"github.com/grafana/sobek"
	"github.com/danhngo-lx/xk6-disruptor/pkg/disruptors"
	"github.com/danhngo-lx/xk6-disruptor/pkg/kubernetes"
	"go.k6.io/k6/js/common"
	"go.k6.io/k6/js/modules"
)

// TODO: call directly Convert from API methods
func convertValue(_ *sobek.Runtime, value sobek.Value, target interface{}) error {
	return Convert(value.Export(), target)
}

// buildObject returns the value as a
func buildObject(rt *sobek.Runtime, value interface{}) (*sobek.Object, error) {
	obj := rt.NewObject()

	t := reflect.TypeOf(value)
	v := reflect.ValueOf(value)
	for i := range t.NumMethod() {
		name := t.Method(i).Name
		f := v.MethodByName(name)
		err := obj.Set(toCamelCase(name), f.Interface())
		if err != nil {
			return nil, err
		}
	}

	return obj, nil
}

// jsDisruptor implements the JS interface for Disruptor
type jsDisruptor struct {
	ctx     context.Context // this context controls the object's lifecycle
	rt      *sobek.Runtime
	tracker *tracker
	disruptors.Disruptor
}

// Targets is a proxy method. Validates parameters and delegates to the PodDisruptor method.
// Emits the targets_selected gauge so users get a sanity-check sample of how many
// targets the selector matched.
func (p *jsDisruptor) Targets() sobek.Value {
	targets, err := p.Disruptor.Targets(p.ctx)
	if err != nil {
		common.Throw(p.rt, fmt.Errorf("error getting kubernetes config path: %w", err))
	}

	p.tracker.emitTargetsSelected(p.ctx, len(targets))

	return p.rt.ToValue(targets)
}

// TargetIPs is a proxy method that returns the pod IP addresses of the disruptor's targets.
// Useful when transparent mode is disabled and the load test must send requests directly to the
// proxy port on each pod IP (bypassing the Kubernetes service and any service-mesh iptables rules).
func (p *jsDisruptor) TargetIPs() sobek.Value {
	ips, err := p.Disruptor.TargetIPs(p.ctx)
	if err != nil {
		common.Throw(p.rt, fmt.Errorf("error getting target IPs: %w", err))
	}

	return p.rt.ToValue(ips)
}

// Cleanup stops any running agent processes on the disruptor's target pods. It is safe
// to call even when no agent is running — pods with no active agent are silently skipped.
// Call this in setup() before injecting faults to ensure no stale agent from a previous
// interrupted run is still active.
func (p *jsDisruptor) Cleanup() {
	err := p.Disruptor.Cleanup(p.ctx)
	if err != nil {
		common.Throw(p.rt, fmt.Errorf("error cleaning up agents: %w", err))
	}
}

// jsProtocolFaultInjector implements the JS interface for jsProtocolFaultInjector
type jsProtocolFaultInjector struct {
	ctx     context.Context // this context controls the object's lifecycle
	rt      *sobek.Runtime
	tracker *tracker
	disruptors.ProtocolFaultInjector
}

// injectHTTPFaults is a proxy method. Validates parameters and delegates to the Protocol Disruptor method
func (p *jsProtocolFaultInjector) InjectHTTPFaults(args ...sobek.Value) {
	if len(args) < 2 {
		common.Throw(p.rt, fmt.Errorf("HTTPFault and duration are required"))
	}

	fault := disruptors.HTTPFault{}
	err := convertValue(p.rt, args[0], &fault)
	if err != nil {
		common.Throw(p.rt, fmt.Errorf("invalid fault argument: %w", err))
	}

	var duration time.Duration
	err = convertValue(p.rt, args[1], &duration)
	if err != nil {
		common.Throw(p.rt, fmt.Errorf("invalid duration argument: %w", err))
	}

	opts := disruptors.HTTPDisruptionOptions{}
	if len(args) > 2 {
		err = convertValue(p.rt, args[2], &opts)
		if err != nil {
			common.Throw(p.rt, fmt.Errorf("invalid options argument: %w", err))
		}
	}

	err = p.tracker.track(p.ctx, "http", func() error {
		return p.ProtocolFaultInjector.InjectHTTPFaults(p.ctx, fault, duration, opts)
	})
	if err != nil {
		common.Throw(p.rt, fmt.Errorf("error injecting fault: %w", err))
	}
}

// InjectHTTPResetPeerFaults is a proxy method. Validates parameters and delegates to the Protocol Disruptor method.
// Signature: injectHTTPResetPeerFaults(fault, duration, options?)
func (p *jsProtocolFaultInjector) InjectHTTPResetPeerFaults(args ...sobek.Value) {
	if len(args) < 2 {
		common.Throw(p.rt, fmt.Errorf("HTTPResetPeerFault and duration are required"))
	}

	fault := disruptors.HTTPResetPeerFault{}
	err := convertValue(p.rt, args[0], &fault)
	if err != nil {
		common.Throw(p.rt, fmt.Errorf("invalid fault argument: %w", err))
	}

	var duration time.Duration
	err = convertValue(p.rt, args[1], &duration)
	if err != nil {
		common.Throw(p.rt, fmt.Errorf("invalid duration argument: %w", err))
	}

	opts := disruptors.HTTPDisruptionOptions{}
	if len(args) > 2 {
		err = convertValue(p.rt, args[2], &opts)
		if err != nil {
			common.Throw(p.rt, fmt.Errorf("invalid options argument: %w", err))
		}
	}

	err = p.tracker.track(p.ctx, "http_reset_peer", func() error {
		return p.ProtocolFaultInjector.InjectHTTPResetPeerFaults(p.ctx, fault, duration, opts)
	})
	if err != nil {
		common.Throw(p.rt, fmt.Errorf("error injecting fault: %w", err))
	}
}

// InjectGrpcFaults is a proxy method. Validates parameters and delegates to the PodDisruptor method
func (p *jsProtocolFaultInjector) InjectGrpcFaults(args ...sobek.Value) {
	if len(args) < 2 {
		common.Throw(p.rt, fmt.Errorf("GrpcFault and duration are required"))
	}

	fault := disruptors.GrpcFault{}
	err := convertValue(p.rt, args[0], &fault)
	if err != nil {
		common.Throw(p.rt, fmt.Errorf("invalid fault argument: %w", err))
	}

	var duration time.Duration
	err = convertValue(p.rt, args[1], &duration)
	if err != nil {
		common.Throw(p.rt, fmt.Errorf("invalid duration argument: %w", err))
	}

	opts := disruptors.GrpcDisruptionOptions{}
	if len(args) > 2 {
		err = convertValue(p.rt, args[2], &opts)
		if err != nil {
			common.Throw(p.rt, fmt.Errorf("invalid options argument: %w", err))
		}
	}

	err = p.tracker.track(p.ctx, "grpc", func() error {
		return p.ProtocolFaultInjector.InjectGrpcFaults(p.ctx, fault, duration, opts)
	})
	if err != nil {
		common.Throw(p.rt, fmt.Errorf("error injecting fault: %w", err))
	}
}

// jsPodFaultInjector implements methods for injecting faults into Pods
type jsPodFaultInjector struct {
	ctx     context.Context
	rt      *sobek.Runtime
	tracker *tracker
	disruptors.PodFaultInjector
}

// TerminatePods is a proxy method. Validates parameters and delegates to the Pod Fault Injector method
func (p *jsPodFaultInjector) TerminatePods(args ...sobek.Value) {
	if len(args) == 0 {
		common.Throw(p.rt, fmt.Errorf("PodTermination fault is required"))
	}

	fault := disruptors.PodTerminationFault{}
	err := convertValue(p.rt, args[0], &fault)
	if err != nil {
		common.Throw(p.rt, fmt.Errorf("invalid fault argument: %w", err))
	}

	err = p.tracker.track(p.ctx, "terminate", func() error {
		// TODO: return list of pods terminated
		_, e := p.PodFaultInjector.TerminatePods(p.ctx, fault)
		return e
	})
	if err != nil {
		common.Throw(p.rt, fmt.Errorf("error injecting fault: %w", err))
	}
}

// jsDNSFaultInjector implements the JS interface for injecting DNS faults
type jsDNSFaultInjector struct {
	ctx     context.Context
	rt      *sobek.Runtime
	tracker *tracker
	disruptors.DNSFaultInjector
}

// InjectDNSFaults is a proxy method. Validates parameters and delegates to the DNS fault injector method
func (p *jsDNSFaultInjector) InjectDNSFaults(args ...sobek.Value) {
	if len(args) < 2 {
		common.Throw(p.rt, fmt.Errorf("DNSFault and duration are required"))
	}

	fault := disruptors.DNSFault{}
	err := convertValue(p.rt, args[0], &fault)
	if err != nil {
		common.Throw(p.rt, fmt.Errorf("invalid fault argument: %w", err))
	}

	var duration time.Duration
	err = convertValue(p.rt, args[1], &duration)
	if err != nil {
		common.Throw(p.rt, fmt.Errorf("invalid duration argument: %w", err))
	}

	err = p.tracker.track(p.ctx, "dns", func() error {
		return p.DNSFaultInjector.InjectDNSFaults(p.ctx, fault, duration)
	})
	if err != nil {
		common.Throw(p.rt, fmt.Errorf("error injecting fault: %w", err))
	}
}

// jsNetworkShapingFaultInjector implements the JS interface for injecting network shaping faults
type jsNetworkShapingFaultInjector struct {
	ctx     context.Context
	rt      *sobek.Runtime
	tracker *tracker
	disruptors.NetworkShapingFaultInjector
}

// InjectNetworkShapingFaults is a proxy method. Validates parameters and delegates to the network shaping fault injector
func (p *jsNetworkShapingFaultInjector) InjectNetworkShapingFaults(args ...sobek.Value) {
	if len(args) < 2 {
		common.Throw(p.rt, fmt.Errorf("NetworkShapingFault and duration are required"))
	}

	fault := disruptors.NetworkShapingFault{}
	err := convertValue(p.rt, args[0], &fault)
	if err != nil {
		common.Throw(p.rt, fmt.Errorf("invalid fault argument: %w", err))
	}

	var duration time.Duration
	err = convertValue(p.rt, args[1], &duration)
	if err != nil {
		common.Throw(p.rt, fmt.Errorf("invalid duration argument: %w", err))
	}

	err = p.tracker.track(p.ctx, "network_shaping", func() error {
		return p.NetworkShapingFaultInjector.InjectNetworkShapingFaults(p.ctx, fault, duration)
	})
	if err != nil {
		common.Throw(p.rt, fmt.Errorf("error injecting fault: %w", err))
	}
}

// jsNetworkPartitionFaultInjector implements the JS interface for injecting network partition faults
type jsNetworkPartitionFaultInjector struct {
	ctx     context.Context
	rt      *sobek.Runtime
	tracker *tracker
	disruptors.NetworkPartitionFaultInjector
}

// InjectNetworkPartition is a proxy method. Validates parameters and delegates to the network partition fault injector
func (p *jsNetworkPartitionFaultInjector) InjectNetworkPartition(args ...sobek.Value) {
	if len(args) < 2 {
		common.Throw(p.rt, fmt.Errorf("NetworkPartitionFault and duration are required"))
	}

	fault := disruptors.NetworkPartitionFault{}
	err := convertValue(p.rt, args[0], &fault)
	if err != nil {
		common.Throw(p.rt, fmt.Errorf("invalid fault argument: %w", err))
	}

	var duration time.Duration
	err = convertValue(p.rt, args[1], &duration)
	if err != nil {
		common.Throw(p.rt, fmt.Errorf("invalid duration argument: %w", err))
	}

	err = p.tracker.track(p.ctx, "network_partition", func() error {
		return p.NetworkPartitionFaultInjector.InjectNetworkPartition(p.ctx, fault, duration)
	})
	if err != nil {
		common.Throw(p.rt, fmt.Errorf("error injecting fault: %w", err))
	}
}

// jsCPUStressFaultInjector implements the JS interface for injecting CPU stress faults
type jsCPUStressFaultInjector struct {
	ctx     context.Context
	rt      *sobek.Runtime
	tracker *tracker
	disruptors.CPUStressFaultInjector
}

// InjectCPUStress is a proxy method. Validates parameters and delegates to the CPU stress fault injector method
func (p *jsCPUStressFaultInjector) InjectCPUStress(args ...sobek.Value) {
	if len(args) < 2 {
		common.Throw(p.rt, fmt.Errorf("CPUStressFault and duration are required"))
	}

	fault := disruptors.CPUStressFault{}
	err := convertValue(p.rt, args[0], &fault)
	if err != nil {
		common.Throw(p.rt, fmt.Errorf("invalid fault argument: %w", err))
	}

	var duration time.Duration
	err = convertValue(p.rt, args[1], &duration)
	if err != nil {
		common.Throw(p.rt, fmt.Errorf("invalid duration argument: %w", err))
	}

	err = p.tracker.track(p.ctx, "cpu_stress", func() error {
		return p.CPUStressFaultInjector.InjectCPUStress(p.ctx, fault, duration)
	})
	if err != nil {
		common.Throw(p.rt, fmt.Errorf("error injecting fault: %w", err))
	}
}

// jsMemoryStressFaultInjector implements the JS interface for injecting memory stress faults
type jsMemoryStressFaultInjector struct {
	ctx     context.Context
	rt      *sobek.Runtime
	tracker *tracker
	disruptors.MemoryStressFaultInjector
}

// InjectMemoryStress is a proxy method. Validates parameters and delegates to the memory stress fault injector method
func (p *jsMemoryStressFaultInjector) InjectMemoryStress(args ...sobek.Value) {
	if len(args) < 2 {
		common.Throw(p.rt, fmt.Errorf("MemoryStressFault and duration are required"))
	}

	fault := disruptors.MemoryStressFault{}
	err := convertValue(p.rt, args[0], &fault)
	if err != nil {
		common.Throw(p.rt, fmt.Errorf("invalid fault argument: %w", err))
	}

	var duration time.Duration
	err = convertValue(p.rt, args[1], &duration)
	if err != nil {
		common.Throw(p.rt, fmt.Errorf("invalid duration argument: %w", err))
	}

	err = p.tracker.track(p.ctx, "memory_stress", func() error {
		return p.MemoryStressFaultInjector.InjectMemoryStress(p.ctx, fault, duration)
	})
	if err != nil {
		common.Throw(p.rt, fmt.Errorf("error injecting fault: %w", err))
	}
}

// jsCrashLoopFaultInjector implements the JS interface for injecting crash loop faults
type jsCrashLoopFaultInjector struct {
	ctx     context.Context
	rt      *sobek.Runtime
	tracker *tracker
	disruptors.CrashLoopFaultInjector
}

// InjectCrashLoopFault is a proxy method. Validates parameters and delegates to the CrashLoop fault injector
func (p *jsCrashLoopFaultInjector) InjectCrashLoopFault(args ...sobek.Value) {
	if len(args) < 2 {
		common.Throw(p.rt, fmt.Errorf("CrashLoopFault and duration are required"))
	}

	fault := disruptors.CrashLoopFault{}
	err := convertValue(p.rt, args[0], &fault)
	if err != nil {
		common.Throw(p.rt, fmt.Errorf("invalid fault argument: %w", err))
	}

	if fault.Container == "" {
		common.Throw(p.rt, fmt.Errorf("CrashLoopFault.container is required"))
	}

	var duration time.Duration
	err = convertValue(p.rt, args[1], &duration)
	if err != nil {
		common.Throw(p.rt, fmt.Errorf("invalid duration argument: %w", err))
	}

	err = p.tracker.track(p.ctx, "crash_loop", func() error {
		return p.CrashLoopFaultInjector.InjectCrashLoopFault(p.ctx, fault, duration)
	})
	if err != nil {
		common.Throw(p.rt, fmt.Errorf("error injecting fault: %w", err))
	}
}

// jsDiskFillFaultInjector implements the JS interface for injecting disk fill faults
type jsDiskFillFaultInjector struct {
	ctx     context.Context
	rt      *sobek.Runtime
	tracker *tracker
	disruptors.DiskFillFaultInjector
}

// InjectDiskFill is a proxy method. Validates parameters and delegates to the DiskFill fault injector
func (p *jsDiskFillFaultInjector) InjectDiskFill(args ...sobek.Value) {
	if len(args) < 2 {
		common.Throw(p.rt, fmt.Errorf("DiskFillFault and duration are required"))
	}

	fault := disruptors.DiskFillFault{}
	err := convertValue(p.rt, args[0], &fault)
	if err != nil {
		common.Throw(p.rt, fmt.Errorf("invalid fault argument: %w", err))
	}

	if fault.Bytes <= 0 {
		common.Throw(p.rt, fmt.Errorf("DiskFillFault.bytes must be greater than zero"))
	}

	var duration time.Duration
	err = convertValue(p.rt, args[1], &duration)
	if err != nil {
		common.Throw(p.rt, fmt.Errorf("invalid duration argument: %w", err))
	}

	err = p.tracker.track(p.ctx, "disk_fill", func() error {
		return p.DiskFillFaultInjector.InjectDiskFill(p.ctx, fault, duration)
	})
	if err != nil {
		common.Throw(p.rt, fmt.Errorf("error injecting fault: %w", err))
	}
}

// jsIOStressFaultInjector implements the JS interface for injecting I/O stress faults
type jsIOStressFaultInjector struct {
	ctx     context.Context
	rt      *sobek.Runtime
	tracker *tracker
	disruptors.IOStressFaultInjector
}

// InjectIOStress is a proxy method. Validates parameters and delegates to the IOStress fault injector
func (p *jsIOStressFaultInjector) InjectIOStress(args ...sobek.Value) {
	if len(args) < 2 {
		common.Throw(p.rt, fmt.Errorf("IOStressFault and duration are required"))
	}

	fault := disruptors.IOStressFault{}
	err := convertValue(p.rt, args[0], &fault)
	if err != nil {
		common.Throw(p.rt, fmt.Errorf("invalid fault argument: %w", err))
	}

	var duration time.Duration
	err = convertValue(p.rt, args[1], &duration)
	if err != nil {
		common.Throw(p.rt, fmt.Errorf("invalid duration argument: %w", err))
	}

	err = p.tracker.track(p.ctx, "io_stress", func() error {
		return p.IOStressFaultInjector.InjectIOStress(p.ctx, fault, duration)
	})
	if err != nil {
		common.Throw(p.rt, fmt.Errorf("error injecting fault: %w", err))
	}
}

type jsPodDisruptor struct {
	jsDisruptor
	jsProtocolFaultInjector
	jsPodFaultInjector
	jsNetworkFaultInjector
	jsNetworkShapingFaultInjector
	jsNetworkPartitionFaultInjector
	jsCPUStressFaultInjector
	jsMemoryStressFaultInjector
	jsDNSFaultInjector
	jsCrashLoopFaultInjector
	jsDiskFillFaultInjector
	jsIOStressFaultInjector
}

// buildJsPodDisruptor builds a goja object that implements the PodDisruptor API
func buildJsPodDisruptor(
	ctx context.Context,
	rt *sobek.Runtime,
	disruptor disruptors.PodDisruptor,
	tr *tracker,
) (*sobek.Object, error) {
	d := &jsPodDisruptor{
		jsDisruptor: jsDisruptor{
			ctx:       ctx,
			rt:        rt,
			tracker:   tr,
			Disruptor: disruptor,
		},
		jsProtocolFaultInjector: jsProtocolFaultInjector{
			ctx:                   ctx,
			rt:                    rt,
			tracker:               tr,
			ProtocolFaultInjector: disruptor,
		},
		jsPodFaultInjector: jsPodFaultInjector{
			ctx:              ctx,
			rt:               rt,
			tracker:          tr,
			PodFaultInjector: disruptor,
		},
		jsNetworkFaultInjector: jsNetworkFaultInjector{
			ctx:                  ctx,
			rt:                   rt,
			tracker:              tr,
			NetworkFaultInjector: disruptor,
		},
		jsNetworkShapingFaultInjector: jsNetworkShapingFaultInjector{
			ctx:                         ctx,
			rt:                          rt,
			tracker:                     tr,
			NetworkShapingFaultInjector: disruptor,
		},
		jsNetworkPartitionFaultInjector: jsNetworkPartitionFaultInjector{
			ctx:                           ctx,
			rt:                            rt,
			tracker:                       tr,
			NetworkPartitionFaultInjector: disruptor,
		},
		jsCPUStressFaultInjector: jsCPUStressFaultInjector{
			ctx:                    ctx,
			rt:                     rt,
			tracker:                tr,
			CPUStressFaultInjector: disruptor,
		},
		jsMemoryStressFaultInjector: jsMemoryStressFaultInjector{
			ctx:                       ctx,
			rt:                        rt,
			tracker:                   tr,
			MemoryStressFaultInjector: disruptor,
		},
		jsDNSFaultInjector: jsDNSFaultInjector{
			ctx:              ctx,
			rt:               rt,
			tracker:          tr,
			DNSFaultInjector: disruptor,
		},
		jsCrashLoopFaultInjector: jsCrashLoopFaultInjector{
			ctx:                    ctx,
			rt:                     rt,
			tracker:                tr,
			CrashLoopFaultInjector: disruptor,
		},
		jsDiskFillFaultInjector: jsDiskFillFaultInjector{
			ctx:                   ctx,
			rt:                    rt,
			tracker:               tr,
			DiskFillFaultInjector: disruptor,
		},
		jsIOStressFaultInjector: jsIOStressFaultInjector{
			ctx:                   ctx,
			rt:                    rt,
			tracker:               tr,
			IOStressFaultInjector: disruptor,
		},
	}

	return buildObject(rt, d)
}

// jsNetworkFaultInjector implements methods for injecting network faults
type jsNetworkFaultInjector struct {
	ctx     context.Context
	rt      *sobek.Runtime
	tracker *tracker
	disruptors.NetworkFaultInjector
}

// InjectNetworkFaults is a proxy method. Validates parameters and delegates to the Network Fault Injector method
func (p *jsNetworkFaultInjector) InjectNetworkFaults(args ...sobek.Value) {
	if len(args) < 2 {
		common.Throw(p.rt, fmt.Errorf("NetworkFault and duration are required"))
	}

	fault := disruptors.NetworkFault{}
	err := convertValue(p.rt, args[0], &fault)
	if err != nil {
		common.Throw(p.rt, fmt.Errorf("invalid fault argument: %w", err))
	}

	var duration time.Duration
	err = convertValue(p.rt, args[1], &duration)
	if err != nil {
		common.Throw(p.rt, fmt.Errorf("invalid duration argument: %w", err))
	}

	err = p.tracker.track(p.ctx, "network", func() error {
		return p.NetworkFaultInjector.InjectNetworkFaults(p.ctx, fault, duration)
	})
	if err != nil {
		common.Throw(p.rt, fmt.Errorf("error injecting fault: %w", err))
	}
}

type jsServiceDisruptor struct {
	jsDisruptor
	jsProtocolFaultInjector
	jsPodFaultInjector
}

// buildJsServiceDisruptor builds a goja object that implements the ServiceDisruptor API
func buildJsServiceDisruptor(
	ctx context.Context,
	rt *sobek.Runtime,
	disruptor disruptors.ServiceDisruptor,
	tr *tracker,
) (*sobek.Object, error) {
	d := &jsServiceDisruptor{
		jsDisruptor: jsDisruptor{
			ctx:       ctx,
			rt:        rt,
			tracker:   tr,
			Disruptor: disruptor,
		},
		jsProtocolFaultInjector: jsProtocolFaultInjector{
			ctx:                   ctx,
			rt:                    rt,
			tracker:               tr,
			ProtocolFaultInjector: disruptor,
		},
		jsPodFaultInjector: jsPodFaultInjector{
			ctx:              ctx,
			rt:               rt,
			tracker:          tr,
			PodFaultInjector: disruptor,
		},
	}

	return buildObject(rt, d)
}

// podDisruptorArg is a combined struct used to parse the PodDisruptor constructor's first argument.
// It merges PodSelectorSpec and PodDisruptorOptions so that callers can pass all fields in a
// single object (the preferred style) or keep using the legacy two-argument form.
type podDisruptorArg struct {
	// PodSelectorSpec fields
	Namespace string                    `js:"namespace"`
	Select    disruptors.PodAttributes  `js:"select"`
	Exclude   disruptors.PodAttributes  `js:"exclude"`
	// PodDisruptorOptions fields
	InjectTimeout time.Duration `js:"injectTimeout"`
	AgentImage    string        `js:"agentImage"`
}

// NewPodDisruptor creates an instance of a PodDisruptor
// The context passed to this constructor is expected to control the lifecycle of the PodDisruptor
func NewPodDisruptor(
	ctx context.Context,
	rt *sobek.Runtime,
	c sobek.ConstructorCall,
	k8s kubernetes.Kubernetes,
	vu modules.VU,
	m *Metrics,
) (*sobek.Object, error) {
	if c.Argument(0).Equals(sobek.Null()) {
		return nil, fmt.Errorf("PodDisruptor constructor expects a non null PodSelector argument")
	}

	arg := podDisruptorArg{}
	err := convertValue(rt, c.Argument(0), &arg)
	if err != nil {
		return nil, fmt.Errorf("invalid PodSelector: %w", err)
	}

	selector := disruptors.PodSelectorSpec{
		Namespace: arg.Namespace,
		Select:    arg.Select,
		Exclude:   arg.Exclude,
	}

	options := disruptors.PodDisruptorOptions{
		InjectTimeout: arg.InjectTimeout,
		AgentImage:    arg.AgentImage,
	}

	disruptor, err := disruptors.NewPodDisruptor(ctx, k8s, selector, options)
	if err != nil {
		return nil, fmt.Errorf("error creating PodDisruptor: %w", err)
	}

	tr := newTracker(vu, m, TargetInfo{
		Disruptor: "pod",
		Namespace: selector.NamespaceOrDefault(),
		Name:      FormatPodSelector(arg.Select.Labels, arg.Exclude.Labels),
	})

	obj, err := buildJsPodDisruptor(ctx, rt, disruptor, tr)
	if err != nil {
		return nil, fmt.Errorf("error creating PodDisruptor: %w", err)
	}

	return obj, nil
}

// NewServiceDisruptor creates an instance of a ServiceDisruptor and returns it as a goja object
// The context passed to this constructor is expected to control the lifecycle of the ServiceDisruptor
func NewServiceDisruptor(
	ctx context.Context,
	rt *sobek.Runtime,
	c sobek.ConstructorCall,
	k8s kubernetes.Kubernetes,
	vu modules.VU,
	m *Metrics,
) (*sobek.Object, error) {
	if len(c.Arguments) < 2 {
		return nil, fmt.Errorf("ServiceDisruptor constructor requires service and namespace parameters")
	}

	var service string
	err := convertValue(rt, c.Argument(0), &service)
	if err != nil {
		return nil, fmt.Errorf("invalid service name argument for ServiceDisruptor constructor: %w", err)
	}

	var namespace string
	err = convertValue(rt, c.Argument(1), &namespace)
	if err != nil {
		return nil, fmt.Errorf("invalid namespace argument for ServiceDisruptor constructor: %w", err)
	}

	options := disruptors.ServiceDisruptorOptions{}
	// options argument is optional
	if len(c.Arguments) > 2 {
		err = convertValue(rt, c.Argument(2), &options)
		if err != nil {
			return nil, fmt.Errorf("invalid ServiceDisruptorOptions: %w", err)
		}
	}

	disruptor, err := disruptors.NewServiceDisruptor(ctx, k8s, service, namespace, options)
	if err != nil {
		return nil, fmt.Errorf("error creating ServiceDisruptor: %w", err)
	}

	tr := newTracker(vu, m, TargetInfo{
		Disruptor: "service",
		Namespace: namespace,
		Name:      service,
	})

	obj, err := buildJsServiceDisruptor(ctx, rt, disruptor, tr)
	if err != nil {
		return nil, fmt.Errorf("error creating ServiceDisruptor: %w", err)
	}

	return obj, nil
}

// ── NodeDisruptor JS API ─────────────────────────────────────────────────────

// jsNodeDrainFaultInjector wraps the NodeDisruptor Drain method for JS
type jsNodeDrainFaultInjector struct {
	ctx     context.Context
	rt      *sobek.Runtime
	tracker *tracker
	disruptors.NodeDisruptor
}

func (p *jsNodeDrainFaultInjector) Drain(args ...sobek.Value) {
	if len(args) < 2 {
		common.Throw(p.rt, fmt.Errorf("NodeDrainFault and duration are required"))
	}

	fault := disruptors.NodeDrainFault{}
	if err := convertValue(p.rt, args[0], &fault); err != nil {
		common.Throw(p.rt, fmt.Errorf("invalid fault argument: %w", err))
	}

	var duration time.Duration
	if err := convertValue(p.rt, args[1], &duration); err != nil {
		common.Throw(p.rt, fmt.Errorf("invalid duration argument: %w", err))
	}

	err := p.tracker.track(p.ctx, "drain", func() error {
		return p.NodeDisruptor.Drain(p.ctx, fault, duration)
	})
	if err != nil {
		common.Throw(p.rt, fmt.Errorf("error draining node: %w", err))
	}
}

// jsNodeTaintFaultInjector wraps the NodeDisruptor TaintNode method for JS
type jsNodeTaintFaultInjector struct {
	ctx     context.Context
	rt      *sobek.Runtime
	tracker *tracker
	disruptors.NodeDisruptor
}

func (p *jsNodeTaintFaultInjector) TaintNode(args ...sobek.Value) {
	if len(args) < 2 {
		common.Throw(p.rt, fmt.Errorf("NodeTaintFault and duration are required"))
	}

	fault := disruptors.NodeTaintFault{}
	if err := convertValue(p.rt, args[0], &fault); err != nil {
		common.Throw(p.rt, fmt.Errorf("invalid fault argument: %w", err))
	}

	var duration time.Duration
	if err := convertValue(p.rt, args[1], &duration); err != nil {
		common.Throw(p.rt, fmt.Errorf("invalid duration argument: %w", err))
	}

	err := p.tracker.track(p.ctx, "taint", func() error {
		return p.NodeDisruptor.TaintNode(p.ctx, fault, duration)
	})
	if err != nil {
		common.Throw(p.rt, fmt.Errorf("error tainting node: %w", err))
	}
}

// jsNodeCPUStressFaultInjector wraps the NodeDisruptor InjectCPUStress method for JS
type jsNodeCPUStressFaultInjector struct {
	ctx     context.Context
	rt      *sobek.Runtime
	tracker *tracker
	disruptors.NodeDisruptor
}

func (p *jsNodeCPUStressFaultInjector) InjectCPUStress(args ...sobek.Value) {
	if len(args) < 2 {
		common.Throw(p.rt, fmt.Errorf("CPUStressFault and duration are required"))
	}

	fault := disruptors.CPUStressFault{}
	if err := convertValue(p.rt, args[0], &fault); err != nil {
		common.Throw(p.rt, fmt.Errorf("invalid fault argument: %w", err))
	}

	var duration time.Duration
	if err := convertValue(p.rt, args[1], &duration); err != nil {
		common.Throw(p.rt, fmt.Errorf("invalid duration argument: %w", err))
	}

	err := p.tracker.track(p.ctx, "cpu_stress", func() error {
		return p.NodeDisruptor.InjectCPUStress(p.ctx, fault, duration)
	})
	if err != nil {
		common.Throw(p.rt, fmt.Errorf("error injecting fault: %w", err))
	}
}

// jsNodeMemoryStressFaultInjector wraps the NodeDisruptor InjectMemoryStress method for JS
type jsNodeMemoryStressFaultInjector struct {
	ctx     context.Context
	rt      *sobek.Runtime
	tracker *tracker
	disruptors.NodeDisruptor
}

func (p *jsNodeMemoryStressFaultInjector) InjectMemoryStress(args ...sobek.Value) {
	if len(args) < 2 {
		common.Throw(p.rt, fmt.Errorf("MemoryStressFault and duration are required"))
	}

	fault := disruptors.MemoryStressFault{}
	if err := convertValue(p.rt, args[0], &fault); err != nil {
		common.Throw(p.rt, fmt.Errorf("invalid fault argument: %w", err))
	}

	var duration time.Duration
	if err := convertValue(p.rt, args[1], &duration); err != nil {
		common.Throw(p.rt, fmt.Errorf("invalid duration argument: %w", err))
	}

	err := p.tracker.track(p.ctx, "memory_stress", func() error {
		return p.NodeDisruptor.InjectMemoryStress(p.ctx, fault, duration)
	})
	if err != nil {
		common.Throw(p.rt, fmt.Errorf("error injecting fault: %w", err))
	}
}

// jsNodeIOStressFaultInjector wraps the NodeDisruptor InjectIOStress method for JS
type jsNodeIOStressFaultInjector struct {
	ctx     context.Context
	rt      *sobek.Runtime
	tracker *tracker
	disruptors.NodeDisruptor
}

func (p *jsNodeIOStressFaultInjector) InjectIOStress(args ...sobek.Value) {
	if len(args) < 2 {
		common.Throw(p.rt, fmt.Errorf("IOStressFault and duration are required"))
	}

	fault := disruptors.IOStressFault{}
	if err := convertValue(p.rt, args[0], &fault); err != nil {
		common.Throw(p.rt, fmt.Errorf("invalid fault argument: %w", err))
	}

	var duration time.Duration
	if err := convertValue(p.rt, args[1], &duration); err != nil {
		common.Throw(p.rt, fmt.Errorf("invalid duration argument: %w", err))
	}

	err := p.tracker.track(p.ctx, "io_stress", func() error {
		return p.NodeDisruptor.InjectIOStress(p.ctx, fault, duration)
	})
	if err != nil {
		common.Throw(p.rt, fmt.Errorf("error injecting fault: %w", err))
	}
}

// jsNodeKubeletKillInjector wraps the NodeDisruptor InjectKubeletServiceKill method for JS
type jsNodeKubeletKillInjector struct {
	ctx     context.Context
	rt      *sobek.Runtime
	tracker *tracker
	disruptors.NodeDisruptor
}

func (p *jsNodeKubeletKillInjector) InjectKubeletServiceKill(args ...sobek.Value) {
	if len(args) < 1 {
		common.Throw(p.rt, fmt.Errorf("duration is required"))
	}

	var duration time.Duration
	if err := convertValue(p.rt, args[0], &duration); err != nil {
		common.Throw(p.rt, fmt.Errorf("invalid duration argument: %w", err))
	}

	err := p.tracker.track(p.ctx, "kubelet_kill", func() error {
		return p.NodeDisruptor.InjectKubeletServiceKill(p.ctx, duration)
	})
	if err != nil {
		common.Throw(p.rt, fmt.Errorf("error injecting fault: %w", err))
	}
}

// jsNodeDisruptor combines all node-level JS injector wrappers
type jsNodeDisruptor struct {
	jsDisruptor
	jsNodeDrainFaultInjector
	jsNodeTaintFaultInjector
	jsNodeCPUStressFaultInjector
	jsNodeMemoryStressFaultInjector
	jsNodeIOStressFaultInjector
	jsNodeKubeletKillInjector
}

// buildJsNodeDisruptor builds a goja object that implements the NodeDisruptor API
func buildJsNodeDisruptor(
	ctx context.Context,
	rt *sobek.Runtime,
	disruptor disruptors.NodeDisruptor,
	tr *tracker,
) (*sobek.Object, error) {
	d := &jsNodeDisruptor{
		jsDisruptor: jsDisruptor{
			ctx:       ctx,
			rt:        rt,
			tracker:   tr,
			Disruptor: disruptor,
		},
		jsNodeDrainFaultInjector: jsNodeDrainFaultInjector{
			ctx:           ctx,
			rt:            rt,
			tracker:       tr,
			NodeDisruptor: disruptor,
		},
		jsNodeTaintFaultInjector: jsNodeTaintFaultInjector{
			ctx:           ctx,
			rt:            rt,
			tracker:       tr,
			NodeDisruptor: disruptor,
		},
		jsNodeCPUStressFaultInjector: jsNodeCPUStressFaultInjector{
			ctx:           ctx,
			rt:            rt,
			tracker:       tr,
			NodeDisruptor: disruptor,
		},
		jsNodeMemoryStressFaultInjector: jsNodeMemoryStressFaultInjector{
			ctx:           ctx,
			rt:            rt,
			tracker:       tr,
			NodeDisruptor: disruptor,
		},
		jsNodeIOStressFaultInjector: jsNodeIOStressFaultInjector{
			ctx:           ctx,
			rt:            rt,
			tracker:       tr,
			NodeDisruptor: disruptor,
		},
		jsNodeKubeletKillInjector: jsNodeKubeletKillInjector{
			ctx:           ctx,
			rt:            rt,
			tracker:       tr,
			NodeDisruptor: disruptor,
		},
	}

	return buildObject(rt, d)
}

// nodeDisruptorArg is the combined struct used to parse the NodeDisruptor constructor argument
type nodeDisruptorArg struct {
	Name           string                    `js:"name"`
	Select         disruptors.NodeAttributes `js:"select"`
	AgentImage     string                    `js:"agentImage"`
	AgentNamespace string                    `js:"agentNamespace"`
	InjectTimeout  time.Duration             `js:"injectTimeout"`
}

// NewNodeDisruptor creates an instance of a NodeDisruptor and returns it as a goja object
func NewNodeDisruptor(
	ctx context.Context,
	rt *sobek.Runtime,
	c sobek.ConstructorCall,
	k8s kubernetes.Kubernetes,
	vu modules.VU,
	m *Metrics,
) (*sobek.Object, error) {
	if c.Argument(0).Equals(sobek.Null()) {
		return nil, fmt.Errorf("NodeDisruptor constructor expects a non-null argument")
	}

	arg := nodeDisruptorArg{}
	if err := convertValue(rt, c.Argument(0), &arg); err != nil {
		return nil, fmt.Errorf("invalid NodeDisruptor argument: %w", err)
	}

	options := disruptors.NodeDisruptorOptions{
		AgentImage:     arg.AgentImage,
		AgentNamespace: arg.AgentNamespace,
		InjectTimeout:  arg.InjectTimeout,
	}

	disruptor, err := disruptors.NewNodeDisruptor(ctx, k8s, arg.Name, arg.Select.Labels, options)
	if err != nil {
		return nil, fmt.Errorf("error creating NodeDisruptor: %w", err)
	}

	targetName := arg.Name
	if targetName == "" {
		targetName = FormatPodSelector(arg.Select.Labels, nil)
	}
	tr := newTracker(vu, m, TargetInfo{
		Disruptor: "node",
		Namespace: arg.AgentNamespace,
		Name:      targetName,
	})

	obj, err := buildJsNodeDisruptor(ctx, rt, disruptor, tr)
	if err != nil {
		return nil, fmt.Errorf("error creating NodeDisruptor: %w", err)
	}

	return obj, nil
}
