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

| Method | Status | Description |
|---|---|---|
| `injectHTTPFaults` | ✅ Stable | HTTP proxy: delay, error code, body/header modification |
| `injectGrpcFaults` | ✅ Stable | gRPC proxy: delay and status code injection |
| `injectNetworkFaults` | ✅ Stable | iptables: drop ingress packets by port/protocol |
| `terminatePods` | ✅ Stable | Delete a subset of target pods |
| `injectCrashLoopFault` | ✅ Stable | Repeatedly kill a container's PID 1 to drive it into CrashLoopBackOff |
| `injectHTTPResetPeerFaults` | ⚠️ Experimental | TCP proxy: abruptly RST connections to simulate flaky/lossy network |
| `injectNetworkShapingFaults` | ⚠️ Experimental | tc netem: delay, jitter, loss, corruption, duplication, rate limit |
| `injectNetworkPartition` | ⚠️ Experimental | iptables: block traffic to/from specific CIDRs |
| `injectCPUStress` | ⚠️ Experimental | Consume a target % of CPU across N cores |
| `injectMemoryStress` | ⚠️ Experimental | Allocate and hold a given amount of memory |
| `injectDNSFaults` | ⚠️ Experimental | DNS proxy: NXDOMAIN injection and domain spoofing |

> ⚠️ Experimental faults are code-complete but have not been validated end-to-end in a live cluster. They require the custom agent image built from this fork.

---

### `injectHTTPFaults(fault, duration, options?)`

Injects faults at the HTTP layer by running a transparent reverse proxy inside the pod. All HTTP traffic on the target port is redirected through it via iptables (unless `nonTransparent` is set).

**`HTTPFault` fields** (1st argument):

| Field | Type | Default | Description |
|---|---|---|---|
| `port` | `number \| string` | `80` | Target port (number or named port, e.g. `8080` or `"http"`) |
| `averageDelay` | `number` (ms) | `0` | Average delay added to every request in milliseconds |
| `delayVariation` | `number` (ms) | `0` | Variation around `averageDelay` (uniform distribution). Only used when `averageDelay > 0`. |
| `errorRate` | `number` | `0` | Fraction of requests that return an error (0.0–1.0) |
| `errorCode` | `number` | `0` | HTTP status code returned for errored requests (e.g. `500`, `503`) |
| `errorBody` | `string` | `""` | Response body returned for errored requests |
| `exclude` | `string` | `""` | Comma-separated list of URL paths to exclude from disruption (e.g. `"/health,/ready"`) |
| `modifyResponseBody` | `string` | `""` | Replaces the upstream response body with this string when non-empty |
| `modifyResponseHeaders` | `Record<string, string>` | `{}` | Adds or overwrites headers in the upstream response |

**`HTTPDisruptionOptions` fields** (optional 3rd argument — see [HTTP Fault Options](#http-fault-options)):

| Field | Type | Default | Description |
|---|---|---|---|
| `proxyPort` | `number` | `8000` | Port the proxy listens on inside the pod |
| `nonTransparent` | `boolean` | `false` | Skip iptables setup; load traffic must go to `pod-IP:proxyPort` directly |

```js
disruptor.injectHTTPFaults(
  {
    port: 8080,
    averageDelay: 150,      // add 150ms ± 30ms to every request
    delayVariation: 30,
    errorRate: 0.1,         // additionally fail 10% of requests
    errorCode: 503,
    exclude: "/health,/ready",
  },
  "60s",
);
```

---

### `injectGrpcFaults(fault, duration, options?)`

Injects faults at the gRPC layer using a transparent proxy. Works the same way as HTTP faults but speaks the gRPC wire protocol.

**`GrpcFault` fields** (1st argument):

| Field | Type | Default | Description |
|---|---|---|---|
| `port` | `number \| string` | `80` | Target port |
| `averageDelay` | `number` (ms) | `0` | Average delay added to every RPC call in milliseconds |
| `delayVariation` | `number` (ms) | `0` | Variation around `averageDelay` |
| `errorRate` | `number` | `0` | Fraction of calls that return a gRPC error (0.0–1.0) |
| `statusCode` | `number` | `0` | [gRPC status code](https://grpc.github.io/grpc/core/md_doc_statuscodes.html) returned for errored calls (e.g. `14` = `UNAVAILABLE`) |
| `statusMessage` | `string` | `""` | Message attached to the error status |
| `exclude` | `string` | `""` | Comma-separated list of gRPC service names to exclude |

```js
disruptor.injectGrpcFaults(
  {
    port: 9090,
    averageDelay: 200,
    errorRate: 0.05,
    statusCode: 14,         // UNAVAILABLE
    statusMessage: "injected fault",
  },
  "60s",
);
```

---

### `injectNetworkFaults(fault, duration)`

Drops ingress packets matching a port and/or protocol filter using iptables. Useful for simulating a completely unresponsive service port or protocol.

**`NetworkFault` fields** (1st argument):

| Field | Type | Default | Description |
|---|---|---|---|
| `port` | `number` | `0` | Target port. `0` means all ports. |
| `protocol` | `string` | `""` | Protocol to filter: `"tcp"`, `"udp"`, `"icmp"`, or `""` for all. |

```js
// Drop all incoming TCP traffic on port 8080
disruptor.injectNetworkFaults(
  { port: 8080, protocol: "tcp" },
  "60s",
);

// Drop all incoming traffic (any port, any protocol)
disruptor.injectNetworkFaults({}, "60s");
```

---

### `terminatePods(fault)`

Deletes a subset of the disruptor's target pods. Kubernetes will restart them according to the Deployment/StatefulSet controller.

**`PodTerminationFault` fields** (1st argument):

| Field | Type | Default | Description |
|---|---|---|---|
| `count` | `number \| string` | — | Number of pods to terminate, or a percentage string (e.g. `"50%"`). |
| `timeout` | `number` (ms) | `10000` | How long to wait for each pod to terminate before returning an error. |

```js
// Terminate 1 pod and wait up to 30s for it to stop
disruptor.terminatePods({ count: 1, timeout: 30000 });

// Terminate 50% of target pods
disruptor.terminatePods({ count: "50%" });
```

---

### `injectCrashLoopFault(fault, duration)`

Does **not** use the ephemeral agent container. Executes `kill -9 1` directly into the target container via the Kubernetes exec API, causing the container to exit. Kubernetes restarts it with exponential backoff (10s → 20s → 40s → 80s → 160s → 300s). After ~5–6 kills the pod enters `CrashLoopBackOff`.

**`CrashLoopFault` fields** (1st argument):

| Field | Type | Default | Description |
|---|---|---|---|
| `container` | `string` | — | Name of the container whose PID 1 will be killed |
| `count` | `number` | `0` | Maximum number of kills. `0` means kill repeatedly for the full duration. |

```js
disruptor.injectCrashLoopFault(
  { container: "my-app", count: 6 },
  "300s",
);
```

**Recovery:** No manual cleanup needed. The pod manifest is not changed. Once the disruptor stops, Kubernetes will eventually restart the container successfully after the backoff timer expires (up to 5 minutes after the last kill).

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

## Experimental Fault Types

The following faults are code-complete but have not been validated end-to-end in a live cluster. They require the custom agent image (see [Agent Image Configuration](#agent-image-configuration)). All require the agent to run with `NET_ADMIN` capability.

---

### HTTP Reset Peer — `injectHTTPResetPeerFaults(fault, duration, options?)`

Intercepts TCP connections on the target port and abruptly closes them by sending a TCP RST packet (`SO_LINGER=0`). This simulates a flaky or lossy network at the **TCP layer**, distinct from `injectHTTPFaults` which operates at the HTTP application layer and returns well-formed HTTP error responses.

A configurable fraction of connections (`toxicity`) are reset; the remainder are transparently proxied to the upstream unchanged. This matches the behaviour of [LitmusChaos's `pod-http-reset-peer`](https://litmuschaos.github.io/litmus/experiments/categories/pods/pod-http-reset-peer/) experiment.

**`HTTPResetPeerFault` fields** (1st argument):

| Field | Type | Default | Description |
|---|---|---|---|
| `port` | `number \| string` | `80` | Target port to intercept |
| `resetTimeout` | `number` (ms) | `0` | How long to wait after accepting a connection before sending the RST. `0` = reset immediately. |
| `toxicity` | `number` | `1.0` | Fraction of connections to reset (0.0–1.0). Connections not selected are transparently forwarded. |

**`HTTPDisruptionOptions` fields** (optional 3rd argument — same as `injectHTTPFaults`):

| Field | Type | Default | Description |
|---|---|---|---|
| `proxyPort` | `number` | `8000` | Port the TCP proxy listens on inside the pod |
| `nonTransparent` | `boolean` | `false` | Skip iptables setup; load traffic must go to `pod-IP:proxyPort` directly |

```js
// Reset all connections on port 8080 immediately
disruptor.injectHTTPResetPeerFaults(
  { port: 8080 },
  "60s",
);

// Reset 30% of connections, waiting 500ms after accepting before sending RST
// (allows the client to send its request before the reset, mimicking a mid-flight failure)
disruptor.injectHTTPResetPeerFaults(
  { port: 8080, resetTimeout: 500, toxicity: 0.3 },
  "60s",
);
```

**How it differs from `injectHTTPFaults` with an error code:**

| | `injectHTTPFaults` (errorCode) | `injectHTTPResetPeerFaults` |
|---|---|---|
| Layer | HTTP (application) | TCP (transport) |
| Client sees | A valid HTTP response (e.g. 503) | `connection reset by peer` / `ECONNRESET` |
| Partial reads | No — response is complete | Yes — RST can interrupt mid-stream |
| Useful for | Testing retry logic on HTTP errors | Testing resilience to network-level disruptions |

**Requirements:** `iptables` must be available in the agent image and the agent needs `NET_ADMIN` capability.

---

### Network Shaping — `injectNetworkShapingFaults`

Applies Linux `tc netem` rules to the pod's network interface to simulate degraded network conditions. All fields of `NetworkShapingFault` are optional but at least one must be set. The rules are removed automatically when the fault duration ends or the agent stops.

> **Note:** If a previous test run was interrupted, a stale qdisc may remain. Call `disruptor.cleanup()` before re-running to avoid a `tc qdisc replace` conflict.

**`NetworkShapingFault` fields:**

| Field | Type | Default | Description |
|---|---|---|---|
| `interface` | `string` | `"eth0"` | Network interface to shape |
| `delay` | `number` (ms) | `0` | Average packet delay in milliseconds |
| `jitter` | `number` (ms) | `0` | Delay variation (jitter) in milliseconds. Only used when `delay > 0`. |
| `loss` | `number` | `0` | Fraction of packets to drop (0.0–1.0) |
| `corrupt` | `number` | `0` | Fraction of packets to corrupt (0.0–1.0) |
| `duplicate` | `number` | `0` | Fraction of packets to duplicate (0.0–1.0) |
| `rate` | `string` | `""` | Bandwidth rate limit, e.g. `"1mbit"`, `"100kbit"` |

```js
// Add 200ms delay with 20ms jitter and 1% packet loss
disruptor.injectNetworkShapingFaults(
  { delay: 200, jitter: 20, loss: 0.01 },
  "60s",
);

// Rate-limit to 1 Mbit/s
disruptor.injectNetworkShapingFaults(
  { rate: "1mbit" },
  "60s",
);
```

**Requirements:** `iproute2` (`tc`) must be present in the agent image. The `images/agent/Dockerfile` installs it.

---

### Network Partition — `injectNetworkPartition`

Blocks traffic between the pod and a set of specified hosts (CIDRs or IPs) using `iptables DROP` rules. Rules are removed automatically when the fault ends.

**`NetworkPartitionFault` fields:**

| Field | Type | Required | Description |
|---|---|---|---|
| `hosts` | `string[]` | Yes | List of CIDRs or IPs to block (e.g. `["10.0.0.1", "192.168.0.0/24"]`) |
| `direction` | `string` | No | `"ingress"` (block inbound), `"egress"` (block outbound), or `"both"` (default) |

```js
// Block all traffic to/from a specific pod IP
disruptor.injectNetworkPartition(
  { hosts: ["10.0.1.42"], direction: "both" },
  "60s",
);

// Block outbound traffic to a whole subnet
disruptor.injectNetworkPartition(
  { hosts: ["10.100.0.0/16"], direction: "egress" },
  "60s",
);
```

**Requirements:** `iptables` must be present in the agent image and the agent needs `NET_ADMIN` capability.

---

### CPU Stress — `injectCPUStress`

Spawns goroutines that perform busy-loop SHA1 hashing to consume a target percentage of CPU on each of the specified cores. The load is applied using a precise duty-cycle algorithm (busy-then-sleep per time slice).

**`CPUStressFault` fields:**

| Field | Type | Required | Description |
|---|---|---|---|
| `load` | `number` | Yes | Target CPU load percentage per core (1–100) |
| `cpus` | `number` | Yes | Number of CPUs (cores) to stress |

```js
// Stress 2 CPUs at 80% load for 60 seconds
disruptor.injectCPUStress(
  { load: 80, cpus: 2 },
  "60s",
);
```

**Note:** The pod must have enough CPU quota in its resource limits for the stress to be visible. Requesting 80% of 1 CPU on a pod with a 0.5 CPU limit will be throttled by the container runtime.

---

### Memory Stress — `injectMemoryStress`

Allocates a fixed number of bytes and touches every page to force physical memory allocation (not just virtual). The memory is held for the full duration, then released.

**`MemoryStressFault` fields:**

| Field | Type | Required | Description |
|---|---|---|---|
| `bytes` | `number` | Yes | Number of bytes to allocate and hold |

```js
// Hold 256 MiB of memory for 60 seconds
disruptor.injectMemoryStress(
  { bytes: 256 * 1024 * 1024 },
  "60s",
);
```

**Warning:** If `bytes` exceeds the pod's memory limit, the pod (and the agent container) will be OOM-killed by the kernel. Size the allocation to stay below `resources.limits.memory` to control the blast radius.

---

### DNS Faults — `injectDNSFaults`

Starts an embedded DNS proxy inside the pod (listening on `127.0.0.1:5353`) and redirects all DNS traffic (UDP port 53) to it via iptables. The proxy can return `NXDOMAIN` for a random fraction of queries or substitute spoofed IPs for specific domains. Non-faulted queries are forwarded to the upstream DNS server unchanged.

**`DNSFault` fields:**

| Field | Type | Default | Description |
|---|---|---|---|
| `errorRate` | `number` | `0` | Fraction (0.0–1.0) of DNS queries that return `NXDOMAIN` |
| `spoof` | `Record<string, string>` | `{}` | Map of `domain → IP` to return fake IPs for specific hostnames |
| `upstreamDNS` | `string` | `"8.8.8.8:53"` | Upstream DNS server for non-faulted queries. Set to your cluster DNS (e.g. `"kube-dns.kube-system:53"`) for in-cluster usage. |

```js
// Make 30% of DNS queries fail with NXDOMAIN,
// and point "payments.internal" to a honeypot IP
disruptor.injectDNSFaults(
  {
    errorRate: 0.3,
    spoof: { "payments.internal": "192.0.2.1" },
    upstreamDNS: "10.96.0.10:53",  // kube-dns
  },
  "60s",
);
```

**Important:** In-cluster environments (including Kubernetes pods) use the cluster DNS resolver (e.g. `10.96.0.10:53`), not `8.8.8.8`. Set `upstreamDNS` to the `kube-dns`/`CoreDNS` service IP to ensure non-faulted queries resolve correctly:

```bash
kubectl get svc kube-dns -n kube-system -o jsonpath='{.spec.clusterIP}'
```

**Requirements:** `iptables` must be available in the agent image.

---

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
| `http-reset-peer` | TCP proxy that abruptly resets connections (RST) with configurable toxicity and timeout |
| `grpc` | Transparent gRPC proxy with delay and status code injection |
| `network-drop` | Drop packets matching a port/protocol filter via iptables |
| `network-shape` | Shape traffic with `tc netem` (delay, jitter, loss, corruption, duplication, rate) |
| `network-partition` | Block traffic to/from specific CIDRs via iptables |
| `tcp-drop` | Reset a fraction of TCP connections via NFQUEUE |
| `stress` | Stress CPU at a target load percentage across N cores |
| `memory-stress` | Allocate and hold a given number of bytes of memory |
| `dns` | Intercept DNS queries; return NXDOMAIN or spoofed IPs via an embedded DNS proxy |
| `cleanup` | Terminate the running agent and clean up any installed resources |

