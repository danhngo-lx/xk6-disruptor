package disruptors

import (
	"fmt"
	"time"

	"github.com/grafana/xk6-disruptor/pkg/types/intstr"
	"github.com/grafana/xk6-disruptor/pkg/utils"

	corev1 "k8s.io/api/core/v1"
)

func buildGrpcFaultCmd(
	targetAddress string,
	fault GrpcFault,
	duration time.Duration,
	options GrpcDisruptionOptions,
) []string {
	cmd := []string{
		"xk6-disruptor-agent",
		"grpc",
		"-d", utils.DurationSeconds(duration),
		"-t", fmt.Sprint(fault.Port),
	}

	// TODO: make port mandatory
	if fault.Port != intstr.NullValue {
		cmd = append(cmd, "-t", fault.Port.Str())
	}

	if fault.AverageDelay > 0 {
		cmd = append(
			cmd,
			"-a",
			utils.DurationMillSeconds(fault.AverageDelay),
			"-v",
			utils.DurationMillSeconds(fault.DelayVariation),
		)
	}

	if fault.ErrorRate > 0 {
		cmd = append(
			cmd,
			"-s",
			fmt.Sprint(fault.StatusCode),
			"-r",
			fmt.Sprint(fault.ErrorRate),
		)
		if fault.StatusMessage != "" {
			cmd = append(cmd, "-m", fault.StatusMessage)
		}
	}

	if len(fault.Exclude) > 0 {
		cmd = append(cmd, "-x", fault.Exclude)
	}

	if options.ProxyPort != 0 {
		cmd = append(cmd, "-p", fmt.Sprint(options.ProxyPort))
	}

	cmd = append(cmd, "--upstream-host", targetAddress)

	return cmd
}

func buildHTTPFaultCmd(
	targetAddress string,
	fault HTTPFault,
	duration time.Duration,
	options HTTPDisruptionOptions,
) []string {
	cmd := []string{
		"xk6-disruptor-agent",
		"http",
		"-d", utils.DurationSeconds(duration),
	}

	// TODO: make port mandatory
	if fault.Port != intstr.NullValue {
		cmd = append(cmd, "-t", fault.Port.Str())
	}

	if fault.AverageDelay > 0 {
		cmd = append(
			cmd,
			"-a",
			utils.DurationMillSeconds(fault.AverageDelay),
			"-v",
			utils.DurationMillSeconds(fault.DelayVariation),
		)
	}

	if fault.ErrorRate > 0 {
		cmd = append(
			cmd,
			"-e",
			fmt.Sprint(fault.ErrorCode),
			"-r",
			fmt.Sprint(fault.ErrorRate),
		)
		if fault.ErrorBody != "" {
			cmd = append(cmd, "-b", fault.ErrorBody)
		}
	}

	if len(fault.Exclude) > 0 {
		cmd = append(cmd, "-x", fault.Exclude)
	}

	if fault.ModifyResponseBody != "" {
		cmd = append(cmd, "--modify-body", fault.ModifyResponseBody)
	}

	for key, value := range fault.ModifyResponseHeaders {
		cmd = append(cmd, "--modify-header", key+"="+value)
	}

	if options.ProxyPort != 0 {
		cmd = append(cmd, "-p", fmt.Sprint(options.ProxyPort))
	}

	if options.NonTransparent {
		cmd = append(cmd, "--transparent=false")
	}

	cmd = append(cmd, "--upstream-host", targetAddress)

	return cmd
}

func buildNetworkFaultCmd(fault NetworkFault, duration time.Duration) []string {
	cmd := []string{
		"xk6-disruptor-agent",
		"network-drop",
		"-d", utils.DurationSeconds(duration),
	}

	if fault.Port != 0 {
		cmd = append(cmd, "-p", fmt.Sprint(fault.Port))
	}

	if fault.Protocol != "" {
		cmd = append(cmd, "-P", fault.Protocol)
	}

	return cmd
}

func buildDNSFaultCmd(fault DNSFault, duration time.Duration) []string {
	cmd := []string{
		"xk6-disruptor-agent",
		"dns",
		"-d", utils.DurationSeconds(duration),
	}

	if fault.ErrorRate > 0 {
		cmd = append(cmd, "-r", fmt.Sprint(fault.ErrorRate))
	}

	if fault.UpstreamDNS != "" {
		cmd = append(cmd, "--upstream", fault.UpstreamDNS)
	}

	for domain, ip := range fault.Spoof {
		cmd = append(cmd, "--spoof", domain+"="+ip)
	}

	return cmd
}

func buildNetworkShapingCmd(fault NetworkShapingFault, duration time.Duration) []string {
	cmd := []string{
		"xk6-disruptor-agent",
		"network-shape",
		"-d", utils.DurationSeconds(duration),
	}

	if fault.Interface != "" {
		cmd = append(cmd, "--interface", fault.Interface)
	}

	if fault.Delay > 0 {
		cmd = append(cmd, "--delay", utils.DurationMillSeconds(fault.Delay))
	}

	if fault.Jitter > 0 {
		cmd = append(cmd, "--jitter", utils.DurationMillSeconds(fault.Jitter))
	}

	if fault.Loss > 0 {
		cmd = append(cmd, "--loss", fmt.Sprint(fault.Loss))
	}

	if fault.Corrupt > 0 {
		cmd = append(cmd, "--corrupt", fmt.Sprint(fault.Corrupt))
	}

	if fault.Duplicate > 0 {
		cmd = append(cmd, "--duplicate", fmt.Sprint(fault.Duplicate))
	}

	if fault.Rate != "" {
		cmd = append(cmd, "--rate", fault.Rate)
	}

	return cmd
}

func buildNetworkPartitionCmd(fault NetworkPartitionFault, duration time.Duration) []string {
	cmd := []string{
		"xk6-disruptor-agent",
		"network-partition",
		"-d", utils.DurationSeconds(duration),
		"--direction", fault.Direction,
	}

	for _, host := range fault.Hosts {
		cmd = append(cmd, "--host", host)
	}

	return cmd
}

func buildCPUStressCmd(fault CPUStressFault, duration time.Duration) []string {
	return []string{
		"xk6-disruptor-agent",
		"stress",
		"-d", utils.DurationSeconds(duration),
		"-l", fmt.Sprint(fault.Load),
		"-c", fmt.Sprint(fault.CPUs),
	}
}

func buildMemoryStressCmd(fault MemoryStressFault, duration time.Duration) []string {
	return []string{
		"xk6-disruptor-agent",
		"memory-stress",
		"-d", utils.DurationSeconds(duration),
		"--bytes", fmt.Sprint(fault.Bytes),
	}
}

func buildCleanupCmd() []string {
	return []string{"xk6-disruptor-agent", "cleanup"}
}

// PodHTTPFaultCommand implements the PodVisitCommands interface for injecting
// HttpFaults in a Pod
type PodHTTPFaultCommand struct {
	fault    HTTPFault
	duration time.Duration
	options  HTTPDisruptionOptions
}

// Commands return the command for injecting a HttpFault in a Pod
func (c PodHTTPFaultCommand) Commands(pod corev1.Pod) (VisitCommands, error) {
	if utils.HasHostNetwork(pod) {
		return VisitCommands{}, fmt.Errorf("fault cannot be safely injected because pod %q uses hostNetwork", pod.Name)
	}

	// find the container port for fault injection
	port, err := utils.FindPort(c.fault.Port, pod)
	if err != nil {
		return VisitCommands{}, err
	}
	podFault := c.fault
	podFault.Port = port

	targetAddress, err := utils.PodIP(pod)
	if err != nil {
		return VisitCommands{}, err
	}

	return VisitCommands{
		Exec:    buildHTTPFaultCmd(targetAddress, podFault, c.duration, c.options),
		Cleanup: buildCleanupCmd(),
	}, nil
}

// PodGrpcFaultCommand implements the PodVisitCommands interface for injecting GrpcFaults in a Pod
type PodGrpcFaultCommand struct {
	fault    GrpcFault
	duration time.Duration
	options  GrpcDisruptionOptions
}

// Commands return the command for injecting a GrpcFault in a Pod
func (c PodGrpcFaultCommand) Commands(pod corev1.Pod) (VisitCommands, error) {
	if utils.HasHostNetwork(pod) {
		return VisitCommands{}, fmt.Errorf("fault cannot be safely injected because pod %q uses hostNetwork", pod.Name)
	}

	// find the container port for fault injection
	port, err := utils.FindPort(c.fault.Port, pod)
	if err != nil {
		return VisitCommands{}, err
	}
	podFault := c.fault
	podFault.Port = port

	targetAddress, err := utils.PodIP(pod)
	if err != nil {
		return VisitCommands{}, err
	}

	return VisitCommands{
		Exec:    buildGrpcFaultCmd(targetAddress, c.fault, c.duration, c.options),
		Cleanup: buildCleanupCmd(),
	}, nil
}

// PodNetworkFaultCommand implements the PodVisitCommands interface for injecting NetworkFaults in a Pod
type PodNetworkFaultCommand struct {
	fault    NetworkFault
	duration time.Duration
}

// Commands return the command for injecting a NetworkFault in a Pod
func (c PodNetworkFaultCommand) Commands(_ corev1.Pod) (VisitCommands, error) {
	return VisitCommands{
		Exec:    buildNetworkFaultCmd(c.fault, c.duration),
		Cleanup: buildCleanupCmd(),
	}, nil
}

// PodNetworkShapingFaultCommand implements the PodVisitCommands interface for injecting NetworkShapingFaults in a Pod
type PodNetworkShapingFaultCommand struct {
	fault    NetworkShapingFault
	duration time.Duration
}

// Commands return the command for injecting a NetworkShapingFault in a Pod
func (c PodNetworkShapingFaultCommand) Commands(_ corev1.Pod) (VisitCommands, error) {
	return VisitCommands{
		Exec:    buildNetworkShapingCmd(c.fault, c.duration),
		Cleanup: buildCleanupCmd(),
	}, nil
}

// PodNetworkPartitionFaultCommand implements the PodVisitCommands interface for injecting NetworkPartitionFaults in a Pod
type PodNetworkPartitionFaultCommand struct {
	fault    NetworkPartitionFault
	duration time.Duration
}

// Commands return the command for injecting a NetworkPartitionFault in a Pod
func (c PodNetworkPartitionFaultCommand) Commands(_ corev1.Pod) (VisitCommands, error) {
	if c.fault.Direction == "" {
		return VisitCommands{}, fmt.Errorf("direction is required for network partition fault")
	}

	if len(c.fault.Hosts) == 0 {
		return VisitCommands{}, fmt.Errorf("at least one host is required for network partition fault")
	}

	return VisitCommands{
		Exec:    buildNetworkPartitionCmd(c.fault, c.duration),
		Cleanup: buildCleanupCmd(),
	}, nil
}

// PodCPUStressFaultCommand implements the PodVisitCommands interface for injecting CPUStressFaults in a Pod
type PodCPUStressFaultCommand struct {
	fault    CPUStressFault
	duration time.Duration
}

// Commands return the command for injecting a CPUStressFault in a Pod
func (c PodCPUStressFaultCommand) Commands(_ corev1.Pod) (VisitCommands, error) {
	return VisitCommands{
		Exec:    buildCPUStressCmd(c.fault, c.duration),
		Cleanup: buildCleanupCmd(),
	}, nil
}

// PodMemoryStressFaultCommand implements the PodVisitCommands interface for injecting MemoryStressFaults in a Pod
type PodMemoryStressFaultCommand struct {
	fault    MemoryStressFault
	duration time.Duration
}

// Commands return the command for injecting a MemoryStressFault in a Pod
func (c PodMemoryStressFaultCommand) Commands(_ corev1.Pod) (VisitCommands, error) {
	return VisitCommands{
		Exec:    buildMemoryStressCmd(c.fault, c.duration),
		Cleanup: buildCleanupCmd(),
	}, nil
}

// PodDNSFaultCommand implements the PodVisitCommands interface for injecting DNSFaults in a Pod
type PodDNSFaultCommand struct {
	fault    DNSFault
	duration time.Duration
}

// Commands return the command for injecting a DNSFault in a Pod
func (c PodDNSFaultCommand) Commands(_ corev1.Pod) (VisitCommands, error) {
	return VisitCommands{
		Exec:    buildDNSFaultCmd(c.fault, c.duration),
		Cleanup: buildCleanupCmd(),
	}, nil
}
