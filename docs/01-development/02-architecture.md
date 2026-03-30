# Architecture

The xk6-disruptor consists of two main components: a k6 extension and the xk6-disruptor-agent. The xk6-disruptor-agent is a command line tool that can inject disruptions in the target system where it runs. The xk6-disruptor extension provides an API for injecting faults into a target system using the xk6-disruptor as a backend tool. The xk6-disruptor extension will install the agent in the target and send commands in order to inject the desired faults.

The xk6-disruptor-agent is provided as an Docker image that can be pulled from the [xk6-disruptor repository](https://github.com/grafana/xk6-disruptor/pkgs/container/xk6-disruptor-agent) as or [build locally](./01-contributing.md#building-the-xk6-disruptor-agent-image).

The agent offers a series of commands that inject different types of disruptions. It can run standalone, as a CLI application to facilitate debugging.

## Disruptors

Disruptors are the top-level objects exposed to k6 scripts. Currently two disruptors are available: `PodDisruptor` and `ServiceDisruptor`. Both are backed by the same ephemeral agent container (`xk6-agent`) injected into target pods.

### PodDisruptor

Targets pods directly, selected by namespace and label selectors.

Supported fault types:

| Method | Fault | Description |
|---|---|---|
| `injectHTTPFaults` | `HTTPFault` | Delay, error code injection, path exclusions, response body/header modification |
| `injectGrpcFaults` | `GrpcFault` | Delay, gRPC status code injection, service exclusions |
| `injectNetworkFaults` | `NetworkFault` | Drop all or filtered (port/protocol) ingress packets via iptables |
| `injectNetworkShapingFaults` | `NetworkShapingFault` | Packet delay/jitter, loss %, corruption, duplication, and rate limiting via `tc netem` |
| `injectNetworkPartition` | `NetworkPartitionFault` | Block ingress/egress/both traffic to specific CIDRs or IPs via iptables |
| `injectCPUStress` | `CPUStressFault` | Consume a target percentage of CPU across N cores |
| `injectMemoryStress` | `MemoryStressFault` | Allocate and hold a specified number of bytes of memory |
| `injectDNSFaults` | `DNSFault` | Return NXDOMAIN for a fraction of DNS queries or spoof specific domains to fake IPs |
| `terminatePods` | `PodTerminationFault` | Terminate a random subset of target pods |

### ServiceDisruptor

Resolves a Kubernetes `Service` to its backing pods and applies faults to them. Supports the same HTTP, gRPC, and pod termination faults as `PodDisruptor`, but does not expose network-level or resource stress faults.

## Agent Commands

The `xk6-disruptor-agent` binary exposes the following subcommands, each corresponding to one fault type:

| Command | Description |
|---|---|
| `http` | Transparent HTTP reverse proxy with delay, error injection, and body/header modification |
| `grpc` | Transparent gRPC proxy with delay and status code injection |
| `network-drop` | Drop packets matching a port/protocol filter via iptables |
| `network-shape` | Shape traffic with `tc netem` (delay, jitter, loss, corruption, duplication, rate) |
| `network-partition` | Block traffic to/from specific CIDRs via iptables |
| `tcp-drop` | Reset a fraction of TCP connections via NFQUEUE |
| `stress` | Stress CPU at a target load percentage across N cores |
| `memory-stress` | Allocate and hold a given number of bytes of memory |
| `dns` | Intercept DNS queries; return NXDOMAIN or spoofed IPs via an embedded DNS proxy |
| `cleanup` | Terminate the running agent and clean up any installed resources |

