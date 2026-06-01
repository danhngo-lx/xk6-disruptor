// Package disruptors implements an API for disrupting targets
package disruptors

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/danhngo-lx/xk6-disruptor/pkg/kubernetes"
	"github.com/danhngo-lx/xk6-disruptor/pkg/kubernetes/helpers"
	"github.com/danhngo-lx/xk6-disruptor/pkg/types/intstr"
	"github.com/danhngo-lx/xk6-disruptor/pkg/utils"
	corev1 "k8s.io/api/core/v1"
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
	CrashLoopFaultInjector
	DiskFillFaultInjector
	IOStressFaultInjector
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

// Cleanup stops any running agent process on each target pod. It execs the cleanup
// command directly into the existing xk6-agent ephemeral container without injecting
// a new one. If no agent is running on a pod the error is silently ignored.
func (d *podDisruptor) Cleanup(ctx context.Context) error {
	targets, err := d.selector.Targets(ctx)
	if err != nil {
		return err
	}

	cleanupCmd := buildCleanupCmd()
	for _, pod := range targets {
		// best-effort: ignore errors (container not found = no agent running)
		_, _, _ = d.helper.Exec(ctx, pod.Name, "xk6-agent", cleanupCmd, []byte{})
	}

	return nil
}

// InjectCrashLoopFault repeatedly kills all processes in the specified container
// in each target pod (using kill -9 -1), causing Kubernetes to restart it. After
// enough restarts the pod enters CrashLoopBackOff. The fault runs for the given
// duration or until fault.Count crashes have been triggered (whichever comes first).
func (d *podDisruptor) InjectCrashLoopFault(ctx context.Context, fault CrashLoopFault, duration time.Duration) error {
	targets, err := d.selector.Targets(ctx)
	if err != nil {
		return err
	}

	if len(targets) == 0 {
		return fmt.Errorf("no target pods found matching selector")
	}

	type result struct {
		pod     string
		crashes int
		err     error
	}
	results := make(chan result, len(targets))
	deadline := time.Now().Add(duration)

	for _, pod := range targets {
		go func(pod corev1.Pod) {
			crashes, err := d.crashLoopPod(ctx, pod, fault, deadline)
			results <- result{pod: pod.Name, crashes: crashes, err: err}
		}(pod)
	}

	var errs []error
	totalCrashes := 0
	for range targets {
		r := <-results
		totalCrashes += r.crashes
		if r.err != nil {
			errs = append(errs, fmt.Errorf("pod %s: %w", r.pod, r.err))
		}
	}

	if totalCrashes == 0 && len(errs) == 0 {
		return fmt.Errorf("crash loop fault had no effect: failed to kill any container processes. " +
			"Ensure the target container has the 'kill' command available and processes are killable")
	}

	return errors.Join(errs...)
}

// crashLoopPod repeatedly kills all processes in the target container in a single pod.
// Returns the number of verified container restarts and any error encountered.
func (d *podDisruptor) crashLoopPod(ctx context.Context, pod corev1.Pod, fault CrashLoopFault, deadline time.Time) (int, error) {
	crashes := 0
	consecutiveFailures := 0
	const maxConsecutiveFailures = 5

	for {
		if ctx.Err() != nil {
			return crashes, nil
		}
		if time.Now().After(deadline) {
			return crashes, nil
		}
		if fault.Count > 0 && crashes >= fault.Count {
			return crashes, nil
		}

		// Wait for container to be running before attempting to kill it
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return crashes, nil
		}
		waitTimeout := 30 * time.Second
		if remaining < waitTimeout {
			waitTimeout = remaining
		}
		running, err := d.helper.WaitContainerRunning(ctx, pod.Name, fault.Container, waitTimeout)
		if err != nil {
			consecutiveFailures++
			if consecutiveFailures >= maxConsecutiveFailures {
				return crashes, fmt.Errorf("failed waiting for container to be running after %d attempts: %w", consecutiveFailures, err)
			}
			continue
		}
		if !running {
			// Timed out waiting for container to be running
			consecutiveFailures++
			if consecutiveFailures >= maxConsecutiveFailures {
				return crashes, fmt.Errorf("container not running after %d attempts - container may be stuck in CrashLoopBackOff or terminated", consecutiveFailures)
			}
			continue
		}

		// Get current restart count before killing
		restartCountBefore, err := d.helper.GetContainerRestartCount(ctx, pod.Name, fault.Container)
		if err != nil {
			// Container not found or other error - wait and retry
			consecutiveFailures++
			if consecutiveFailures >= maxConsecutiveFailures {
				return crashes, fmt.Errorf("failed to get container restart count after %d attempts: %w", consecutiveFailures, err)
			}
			continue
		}

		_, stderr, err := d.helper.Exec(ctx, pod.Name, fault.Container, []string{"kill", "-9", "-1"}, []byte{})
		if err != nil {
			consecutiveFailures++
			if consecutiveFailures >= maxConsecutiveFailures {
				errMsg := string(stderr)
				if errMsg == "" {
					errMsg = err.Error()
				}
				return crashes, fmt.Errorf("failed to kill process after %d attempts (last error: %s). "+
					"Container may not have 'kill' command or process may be unkillable", consecutiveFailures, errMsg)
			}
			// container may be mid-restart — will wait at top of loop
			continue
		}

		// Wait for the container to actually restart (restart count to increase)
		remaining = time.Until(deadline)
		if remaining <= 0 {
			return crashes, nil
		}
		waitTimeout = 30 * time.Second
		if remaining < waitTimeout {
			waitTimeout = remaining
		}

		newCount, restarted, err := d.helper.WaitContainerRestart(ctx, pod.Name, fault.Container, restartCountBefore, waitTimeout)
		if err != nil {
			consecutiveFailures++
			if consecutiveFailures >= maxConsecutiveFailures {
				return crashes, fmt.Errorf("error waiting for container restart: %w", err)
			}
			continue
		}

		if restarted {
			consecutiveFailures = 0
			crashes++
			_ = newCount // restart count updated
		} else {
			// Kill succeeded but container didn't restart
			consecutiveFailures++
			if consecutiveFailures >= maxConsecutiveFailures {
				return crashes, fmt.Errorf("kill command succeeded but container did not restart after %d attempts. "+
					"The container runtime may be blocking signals or the container may be configured to ignore them", consecutiveFailures)
			}
		}
	}
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

// InjectHTTPResetPeerFaults intercepts TCP connections on the target port and abruptly resets them.
func (d *podDisruptor) InjectHTTPResetPeerFaults(
	ctx context.Context,
	fault HTTPResetPeerFault,
	duration time.Duration,
	options HTTPDisruptionOptions,
) error {
	if fault.Port.IsNull() || fault.Port.IsZero() {
		fault.Port = DefaultTargetPort
	}

	command := PodHTTPResetPeerFaultCommand{
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

// InjectDiskFill fills disk space in the target pods by writing a large file
func (d *podDisruptor) InjectDiskFill(
	ctx context.Context,
	fault DiskFillFault,
	duration time.Duration,
) error {
	command := PodDiskFillFaultCommand{
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

	return NewPodController(targets).Visit(ctx, visitor)
}

// InjectIOStress runs parallel I/O workers to create sustained disk I/O pressure
func (d *podDisruptor) InjectIOStress(
	ctx context.Context,
	fault IOStressFault,
	duration time.Duration,
) error {
	command := PodIOStressFaultCommand{
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

	return NewPodController(targets).Visit(ctx, visitor)
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
