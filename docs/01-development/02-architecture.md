# Architecture

The xk6-disruptor consists of two main components: a k6 extension and the xk6-disruptor-agent. The xk6-disruptor-agent is a command line tool that can inject disruptions in the target system where it runs. The xk6-disruptor extension provides an API for injecting faults into a target system using the xk6-disruptor as a backend tool. The xk6-disruptor extension will install the agent in the target and send commands in order to inject the desired faults.

The xk6-disruptor-agent is provided as a Docker image built from `images/agent/Dockerfile`. See [Agent Image Configuration](#agent-image-configuration) for how to configure which image is used.

The agent offers a series of commands that inject different types of disruptions. It can run standalone, as a CLI application to facilitate debugging.

## Disruptors

Disruptors are the top-level objects exposed to k6 scripts. Currently two disruptors are available: `PodDisruptor` and `ServiceDisruptor`. Both are backed by the same ephemeral agent container (`xk6-agent`) injected into target pods.

All disruptors expose the following utility methods:

| Method | Returns | Description |
|---|---|---|
| `targets()` | `string[]` | Names of the pods currently selected by the disruptor |
| `targetIPs()` | `string[]` | IP addresses of the pods currently selected by the disruptor |

### PodDisruptor

Targets pods directly, selected by namespace and label selectors.

**Constructor:** `new PodDisruptor(config)`

All fields are passed in a single configuration object:

| Field | Type | Required | Description |
|---|---|---|---|
| `namespace` | `string` | Yes | Kubernetes namespace to target |
| `select` | `{ labels: Record<string, string> }` | Yes | Label selector for target pods |
| `exclude` | `{ labels: Record<string, string> }` | No | Label selector to exclude pods |
| `injectTimeout` | `string \| number` | No | How long to wait for the agent container to start (default `"30s"`). Negative disables waiting. |
| `agentImage` | `string` | No | Full container image reference for the agent. See [Agent Image Configuration](#agent-image-configuration). |

```js
const disruptor = new PodDisruptor({
  namespace: 'my-ns',
  select: { labels: { 'app.kubernetes.io/name': 'my-app' } },
  agentImage: 'ghcr.io/myorg/xk6-disruptor-agent:latest',  // optional
});
```

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

**Constructor:** `new ServiceDisruptor(service, namespace, options?)`

| Argument | Type | Required | Description |
|---|---|---|---|
| `service` | `string` | Yes | Name of the Kubernetes Service |
| `namespace` | `string` | Yes | Namespace the Service lives in |
| `options` | `object` | No | `{ injectTimeout?, agentImage? }` — same semantics as `PodDisruptor` |

```js
const disruptor = new ServiceDisruptor('my-service', 'my-ns', {
  agentImage: 'ghcr.io/myorg/xk6-disruptor-agent:latest',  // optional
});
```

## HTTP Fault Options

`injectHTTPFaults` accepts an options object (`HTTPDisruptionOptions`) in addition to the fault parameters:

| Option | Type | Default | Description |
|---|---|---|---|
| `proxyPort` | `number` | `8000` | Port the agent proxy listens on inside the pod |
| `nonTransparent` | `boolean` | `false` | When `true`, skips iptables setup. Load traffic must be sent directly to `pod-IP:proxyPort`. See [Service Mesh Compatibility](#service-mesh-compatibility). |

## Service Mesh Compatibility

The disruptor uses iptables rules inside each target pod to transparently redirect traffic through the fault-injection proxy. Service meshes such as **Istio** also install iptables rules (via `istio-init`) during pod startup. Because Istio's rules redirect all inbound TCP to the Envoy sidecar first, the disruptor's rules are never reached and the proxy receives no traffic.

### Recommended fix: exclude the target port from Istio interception

Add the `traffic.sidecar.istio.io/excludeInboundPorts` annotation to the target Deployment's pod template. This tells `istio-init` to leave the specified port alone so the disruptor's iptables rules take effect:

```yaml
spec:
  template:
    metadata:
      annotations:
        traffic.sidecar.istio.io/excludeInboundPorts: "9998"  # replace with your app port
```

Or apply it without editing the manifest directly:

```bash
kubectl patch deployment <name> -n <namespace> \
  --type='json' \
  -p='[{"op":"add","path":"/spec/template/metadata/annotations","value":{"traffic.sidecar.istio.io/excludeInboundPorts":"<port>"}}]'
```

This triggers a rolling restart. Traffic on the excluded port flows directly into the pod network namespace, bypassing Envoy. mTLS and Istio telemetry are disabled for that port only.

### Alternative: non-transparent mode

If you cannot modify the Deployment, set `nonTransparent: true` in `HTTPDisruptionOptions`. The agent starts the proxy without installing iptables rules. The k6 load test must then send requests directly to `pod-IP:proxyPort` (obtained from `disruptor.targetIPs()`) instead of the Kubernetes service address:

```js
export function setup() {
  const disruptor = new PodDisruptor({ namespace: 'my-ns', select: { labels: { app: 'my-app' } } });
  return { podIPs: disruptor.targetIPs() };
}

export function runLoad(data) {
  const ip = data.podIPs[Math.floor(Math.random() * data.podIPs.length)];
  http.get(`http://${ip}:8000/my-path`);  // 8000 is the default proxyPort
}

export function runDisrupt() {
  const disruptor = new PodDisruptor({ namespace: 'my-ns', select: { labels: { app: 'my-app' } } });
  disruptor.injectHTTPFaults(
    { averageDelay: '100ms', errorRate: 0.1, errorCode: 500, port: 9998, nonTransparent: true },
    '60s',
  );
}
```

This approach bypasses the service address, so it is only suitable when the k6 script directly controls the load generation target.

## Agent Image Configuration

The agent container image is resolved in the following priority order (first match wins):

| Priority | Method | Example |
|---|---|---|
| 1 (highest) | `agentImage` option in the k6 script | `agentImage: 'ghcr.io/myorg/xk6-disruptor-agent:v1.2.3'` |
| 2 | `XK6_DISRUPTOR_AGENT_IMAGE` environment variable | `XK6_DISRUPTOR_AGENT_IMAGE=ghcr.io/myorg/xk6-disruptor-agent:latest k6 run script.js` |
| 3 | Build-time ldflags | `-ldflags "-X .../version.agentImageRepo=ghcr.io/myorg/xk6-disruptor-agent"` |
| 4 (default) | Compiled-in default | `ghcr.io/danhngo-lx/xk6-disruptor-agent:<version or latest>` |

### Building the agent image locally

The agent Dockerfile at `images/agent/Dockerfile` uses a multi-stage build and compiles the agent binary from source. Run from the **repository root**:

```bash
docker build -t ghcr.io/danhngo-lx/xk6-disruptor-agent:latest -f images/agent/Dockerfile .
```

To push to a registry:

```bash
echo $GITHUB_TOKEN | docker login ghcr.io -u <username> --password-stdin

docker build -t ghcr.io/danhngo-lx/xk6-disruptor-agent:latest -f images/agent/Dockerfile .
docker push ghcr.io/danhngo-lx/xk6-disruptor-agent:latest
```

### Specifying the image in a k6 script

```js
const disruptor = new PodDisruptor({
  namespace: 'my-ns',
  select: { labels: { app: 'my-app' } },
  agentImage: 'ghcr.io/danhngo-lx/xk6-disruptor-agent:latest',
});
```

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

