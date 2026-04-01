// Package disruptors implements an API for disrupting targets
package disruptors

import (
	"context"
	"time"

	"github.com/danhngo-lx/xk6-disruptor/pkg/kubernetes"
	"github.com/danhngo-lx/xk6-disruptor/pkg/kubernetes/helpers"
	"github.com/danhngo-lx/xk6-disruptor/pkg/types/intstr"
	"github.com/danhngo-lx/xk6-disruptor/pkg/utils"
)

// DefaultTargetPort defines the default value for a target HTTP
var DefaultTargetPort = intstr.FromInt32(80) //nolint:gochecknoglobals

// PodDisruptor defines the types of faults that can be injected in a Pod
type PodDisruptor interface {
	Disruptor
	ProtocolFaultInjector
	PodFaultInjector
	NetworkFaultInjector
	NetworkShapingFaultInjector
	NetworkPartitionFaultInjector
	CPUStressFaultInjector
	MemoryStressFaultInjector
	DNSFaultInjector
}

// PodDisruptorOptions defines options that controls the PodDisruptor's behavior
type PodDisruptorOptions struct {
	// timeout when waiting agent to be injected in seconds. A zero value forces default.
	// A Negative value forces no waiting.
	InjectTimeout time.Duration `js:"injectTimeout"`
	// AgentImage overrides the container image used for the injected agent ephemeral container.
	// When empty, the image is resolved from the XK6_DISRUPTOR_AGENT_IMAGE environment variable
	// or the build-time default.
	AgentImage string `js:"agentImage"`
}

// podDisruptor is an instance of a PodDisruptor that uses a PodController to interact with target pods
type podDisruptor struct {
	helper   helpers.PodHelper
	selector *PodSelector
	options  PodDisruptorOptions
}

// PodSelectorSpec defines the criteria for selecting a pod for disruption
type PodSelectorSpec struct {
	Namespace string
	// Select Pods that match these PodAttributes
	Select PodAttributes
	// Select Pods that match these PodAttributes
	Exclude PodAttributes
}

// PodAttributes defines the attributes a Pod must match for being selected/excluded
type PodAttributes struct {
	Labels map[string]string
}

// NewPodDisruptor creates a new instance of a PodDisruptor that acts on the pods
// that match the given PodSelector
func NewPodDisruptor(
	_ context.Context,
	k8s kubernetes.Kubernetes,
	spec PodSelectorSpec,
	options PodDisruptorOptions,
) (PodDisruptor, error) {
	// ensure selector and controller use default namespace if none specified
	namespace := spec.NamespaceOrDefault()

	helper := k8s.PodHelper(namespace)

	selector, err := NewPodSelector(spec, helper)
	if err != nil {
		return nil, err
	}

	return &podDisruptor{
		helper:   helper,
		options:  options,
		selector: selector,
	}, nil
}

func (d *podDisruptor) Targets(ctx context.Context) ([]string, error) {
	targets, err := d.selector.Targets(ctx)
	if err != nil {
		return nil, err
	}

	return utils.PodNames(targets), nil
}

func (d *podDisruptor) TargetIPs(ctx context.Context) ([]string, error) {
	targets, err := d.selector.Targets(ctx)
	if err != nil {
		return nil, err
	}

	return utils.PodIPs(targets), nil
}

// InjectHTTPFaults injects faults in the http requests sent to the disruptor's targets
func (d *podDisruptor) InjectHTTPFaults(
	ctx context.Context,
	fault HTTPFault,
	duration time.Duration,
	options HTTPDisruptionOptions,
) error {
	// Handle default port mapping
	// TODO: make port mandatory instead of using a default
	if fault.Port.IsNull() || fault.Port.IsZero() {
		fault.Port = DefaultTargetPort
	}

	command := PodHTTPFaultCommand{
		fault:    fault,
		duration: duration,
		options:  options,
	}

	visitor := NewPodAgentVisitor(
		d.helper,
		PodAgentVisitorOptions{Timeout: d.options.InjectTimeout, AgentImage: d.options.AgentImage},
		command,
	)

	targets, err := d.selector.Targets(ctx)
	if err != nil {
		return err
	}

	controller := NewPodController(targets)

	return controller.Visit(ctx, visitor)
}

// InjectGrpcFaults injects faults in the grpc requests sent to the disruptor's targets
func (d *podDisruptor) InjectGrpcFaults(
	ctx context.Context,
	fault GrpcFault,
	duration time.Duration,
	options GrpcDisruptionOptions,
) error {
	command := PodGrpcFaultCommand{
		fault:    fault,
		duration: duration,
		options:  options,
	}

	visitor := NewPodAgentVisitor(
		d.helper,
		PodAgentVisitorOptions{Timeout: d.options.InjectTimeout, AgentImage: d.options.AgentImage},
		command,
	)

	targets, err := d.selector.Targets(ctx)
	if err != nil {
		return err
	}

	controller := NewPodController(targets)

	return controller.Visit(ctx, visitor)
}

// InjectNetworkShapingFaults applies tc netem-based traffic shaping to the target pods
func (d *podDisruptor) InjectNetworkShapingFaults(
	ctx context.Context,
	fault NetworkShapingFault,
	duration time.Duration,
) error {
	command := PodNetworkShapingFaultCommand{
		fault:    fault,
		duration: duration,
	}

	visitor := NewPodAgentVisitor(
		d.helper,
		PodAgentVisitorOptions{Timeout: d.options.InjectTimeout, AgentImage: d.options.AgentImage},
		command,
	)

	targets, err := d.selector.Targets(ctx)
	if err != nil {
		return err
	}

	controller := NewPodController(targets)

	return controller.Visit(ctx, visitor)
}

// InjectNetworkPartition blocks traffic between the target pods and the specified hosts
func (d *podDisruptor) InjectNetworkPartition(
	ctx context.Context,
	fault NetworkPartitionFault,
	duration time.Duration,
) error {
	command := PodNetworkPartitionFaultCommand{
		fault:    fault,
		duration: duration,
	}

	visitor := NewPodAgentVisitor(
		d.helper,
		PodAgentVisitorOptions{Timeout: d.options.InjectTimeout, AgentImage: d.options.AgentImage},
		command,
	)

	targets, err := d.selector.Targets(ctx)
	if err != nil {
		return err
	}

	controller := NewPodController(targets)

	return controller.Visit(ctx, visitor)
}

// InjectCPUStress injects CPU stress in the target pods
func (d *podDisruptor) InjectCPUStress(
	ctx context.Context,
	fault CPUStressFault,
	duration time.Duration,
) error {
	command := PodCPUStressFaultCommand{
		fault:    fault,
		duration: duration,
	}

	visitor := NewPodAgentVisitor(
		d.helper,
		PodAgentVisitorOptions{Timeout: d.options.InjectTimeout, AgentImage: d.options.AgentImage},
		command,
	)

	targets, err := d.selector.Targets(ctx)
	if err != nil {
		return err
	}

	controller := NewPodController(targets)

	return controller.Visit(ctx, visitor)
}

// InjectMemoryStress injects memory pressure in the target pods
func (d *podDisruptor) InjectMemoryStress(
	ctx context.Context,
	fault MemoryStressFault,
	duration time.Duration,
) error {
	command := PodMemoryStressFaultCommand{
		fault:    fault,
		duration: duration,
	}

	visitor := NewPodAgentVisitor(
		d.helper,
		PodAgentVisitorOptions{Timeout: d.options.InjectTimeout, AgentImage: d.options.AgentImage},
		command,
	)

	targets, err := d.selector.Targets(ctx)
	if err != nil {
		return err
	}

	controller := NewPodController(targets)

	return controller.Visit(ctx, visitor)
}

// InjectDNSFaults injects DNS faults in the target pods
func (d *podDisruptor) InjectDNSFaults(
	ctx context.Context,
	fault DNSFault,
	duration time.Duration,
) error {
	command := PodDNSFaultCommand{
		fault:    fault,
		duration: duration,
	}

	visitor := NewPodAgentVisitor(
		d.helper,
		PodAgentVisitorOptions{Timeout: d.options.InjectTimeout, AgentImage: d.options.AgentImage},
		command,
	)

	targets, err := d.selector.Targets(ctx)
	if err != nil {
		return err
	}

	controller := NewPodController(targets)

	return controller.Visit(ctx, visitor)
}

// TerminatePods terminates a subset of the target pods of the disruptor
func (d *podDisruptor) TerminatePods(
	ctx context.Context,
	fault PodTerminationFault,
) ([]string, error) {
	targets, err := d.selector.Targets(ctx)
	if err != nil {
		return nil, err
	}

	targets, err = utils.Sample(targets, fault.Count)
	if err != nil {
		return nil, err
	}

	controller := NewPodController(targets)

	visitor := PodTerminationVisitor{helper: d.helper, timeout: fault.Timeout}

	return utils.PodNames(targets), controller.Visit(ctx, visitor)
}

// InjectNetworkFaults injects network faults in the target pods
func (d *podDisruptor) InjectNetworkFaults(
	ctx context.Context,
	fault NetworkFault,
	duration time.Duration,
) error {
	command := PodNetworkFaultCommand{
		fault:    fault,
		duration: duration,
	}

	visitor := NewPodAgentVisitor(
		d.helper,
		PodAgentVisitorOptions{Timeout: d.options.InjectTimeout, AgentImage: d.options.AgentImage},
		command,
	)

	targets, err := d.selector.Targets(ctx)
	if err != nil {
		return err
	}

	controller := NewPodController(targets)

	return controller.Visit(ctx, visitor)
}
