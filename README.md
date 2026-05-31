> [!WARNING]
> **⚠️ This repository is archived and no longer maintained by Grafana Labs.**
> 
> Grafana Labs is no longer actively maintaining this project. The repository is now read-only, and no further updates, bug fixes, or feature requests will be addressed.
> 
> **You are welcome to fork this repository** if you would like to continue development or maintain your own version.

# xk6-disruptor

</br>
</br>

<p align="center">
  <img src="assets/logo.png" alt="xk6-disruptor" width="300"></a>
  <br>
  "Like Unit Testing, but for <strong>Reliability</strong>"
  <br>
</p>
<p align="center">  
</div>


xk6-disruptor is an extension adds fault injection capabilities to [Grafana k6](https://github.com/grafana/k6). It implements the ideas of the Chaos Engineering discipline and enables Grafana k6 users to test their system's reliability under turbulent conditions.

<blockquote>
⚠️ <strong>Important</strong>
xk6-disruptor is in the alpha stage, undergoing active development. We do not guarantee API compatibility between releases - your k6 scripts may need to be updated on each release until this extension reaches v1.0 release.
</blockquote>

## Why xk6-disruptor?

xk6-disruptor is purposely designed and built to provide the best experience for developers trying to make their systems more reliable:

- Everything as code.
  - No need to learn a new DSL.
  - Developers can use their usual development IDE
  - Facilitate test reuse and sharing

- Fast to adopt with no day-two surprises.
  - No need to deploy and maintain a fleet of agents or operators.
- Easy to extend and integrate with other [types of tests](https://k6.io/docs/test-types/introduction/).
  - No need to try to glue multiple tools together to get the job done.

Also, this project has been built to be a good citizen in the Grafana k6 ecosystem by:

- Working well with other extensions.
- Working well with k6's core concepts and features.

You can check this out in the following example:

```js
export default function () {
    // Create a new pod disruptor with a selector 
    // that matches pods from the "default" namespace with the label "app=my-app"
    const disruptor = new PodDisruptor({
        namespace: "default",
        select: { labels: { app: "my-app" } },
    });

    // Disrupt the targets by injecting HTTP faults into them for 30 seconds
    const fault = {
        averageDelay: 500,
        errorRate: 0.1,
        errorCode: 500
    }
    disruptor.injectHTTPFaults(fault, "30s")
}
```

## Features

The project, at this time, is intended to test systems running in Kubernetes. Other platforms are not supported at this time.

It offers an API for creating disruptors that target one specific type of component (e.g., Pods, Nodes) and is capable of injecting different kinds of faults. Disruptors exist for Pods, Services, Nodes, and Workloads (Deployments / StatefulSets).

### Pod / Service fault types

| Fault | API method | Description | Status |
|---|---|---|---|
| HTTP faults | `injectHTTPFaults` | Delay, error code injection, response body/header modification | ✅ Stable |
| gRPC faults | `injectGrpcFaults` | Delay and gRPC status code injection | ✅ Stable |
| Network drop | `injectNetworkFaults` | Drop ingress packets by port/protocol via iptables | ✅ Stable |
| Pod termination | `terminatePods` | Terminate a random subset of target pods | ✅ Stable |
| Crash loop | `injectCrashLoopFault` | Repeatedly kill all processes in a container to drive the pod into CrashLoopBackOff | ✅ Stable |
| TCP reset peer | `injectHTTPResetPeerFaults` | Abruptly RST TCP connections to simulate flaky/lossy network at the transport layer | ⚠️ Experimental |
| Network shaping | `injectNetworkShapingFaults` | Packet delay, jitter, loss, corruption, duplication, rate limiting via `tc netem` | ⚠️ Experimental |
| Network partition | `injectNetworkPartition` | Block traffic to/from specific CIDRs or IPs | ⚠️ Experimental |
| CPU stress | `injectCPUStress` | Consume a percentage of CPU across N cores | ⚠️ Experimental |
| Memory pressure | `injectMemoryStress` | Allocate and hold a given amount of memory | ⚠️ Experimental |
| DNS faults | `injectDNSFaults` | Return NXDOMAIN for a fraction of queries or spoof domains to fake IPs | ⚠️ Experimental |
| Disk fill | `injectDiskFill` | Write a large file to exhaust ephemeral storage quota (can trigger pod eviction) | ⚠️ Experimental |
| IO stress | `injectIOStress` | Parallel write/read workers to saturate I/O throughput ("noisy neighbour") | ⚠️ Experimental |

### Node fault types (`NodeDisruptor`)

| Fault | API method | Description | Status |
|---|---|---|---|
| Node drain | `drain` | Cordon + evict all eligible pods, then uncordon after duration | ⚠️ Experimental |
| Node taint | `taintNode` | Add a taint to the node, remove it after duration | ⚠️ Experimental |
| Node CPU stress | `injectCPUStress` | Run a CPU stressor at node level via a privileged pod | ⚠️ Experimental |
| Node memory stress | `injectMemoryStress` | Run a memory stressor at node level via a privileged pod | ⚠️ Experimental |
| Node IO stress | `injectIOStress` | Run an IO stressor at node level via a privileged pod | ⚠️ Experimental |
| Kubelet service kill | `injectKubeletServiceKill` | Stop the kubelet service for a duration then restart it via nsenter | ⚠️ Experimental |

### Workload fault types (`WorkloadDisruptor`)

| Fault | API method | Description | Status |
|---|---|---|---|
| Replica change | `scaleReplicas` | Scale a Deployment / StatefulSet up or down (absolute, delta, or percentage), optionally auto-revert after a duration | ⚠️ Experimental |

> ⚠️ **Experimental** faults are code-complete and follow the same implementation patterns as stable faults, but have not yet been validated end-to-end in a live cluster. They require the custom agent image to be built from this fork. Use with caution and report issues.

See the [architecture guide](docs/01-development/02-architecture.md#experimental-fault-types) for detailed usage, field references, and examples for each experimental fault.

### TCP Reset Peer

Intercepts TCP connections on the target port and sends a RST packet (`SO_LINGER=0`), causing clients to receive `connection reset by peer`. Unlike `injectHTTPFaults`, this operates at the TCP layer — the client never sees an HTTP response.

```js
// Reset 30% of connections after a 500ms delay (mid-flight reset)
disruptor.injectHTTPResetPeerFaults(
  { port: 8080, resetTimeout: 500, toxicity: 0.3 },
  "60s",
);
```

Fields: `port` (target port), `resetTimeout` (ms to wait before RST, default `0`), `toxicity` (fraction 0–1 of connections to reset, default `1.0`). Accepts the same optional 3rd options argument as `injectHTTPFaults` (`proxyPort`, `nonTransparent`).

### Network Shaping

Applies `tc netem` rules to a pod's network interface. All parameters are optional but at least one must be set.

```js
disruptor.injectNetworkShapingFaults(
  { delay: 200, jitter: 20, loss: 0.01 },  // 200ms ± 20ms, 1% loss
  "60s",
);
```

Fields: `interface` (default `"eth0"`), `delay` (ms), `jitter` (ms), `loss`, `corrupt`, `duplicate` (all fractions 0–1), `rate` (string, e.g. `"1mbit"`).

### Network Partition

Blocks traffic between the pod and specified CIDRs/IPs via `iptables DROP`.

```js
disruptor.injectNetworkPartition(
  { hosts: ["10.0.1.42", "192.168.0.0/24"], direction: "egress" },
  "60s",
);
```

Fields: `hosts` (required, array of CIDRs or IPs), `direction` (`"ingress"`, `"egress"`, or `"both"` — default `"both"`).

### CPU Stress

Consumes a target percentage of CPU across N cores using a precise duty-cycle algorithm.

```js
disruptor.injectCPUStress(
  { load: 80, cpus: 2 },  // 80% on 2 cores
  "60s",
);
```

Fields: `load` (required, 1–100), `cpus` (required, number of cores to stress).

### Memory Stress

Allocates and holds a fixed amount of memory, touching every page to force physical allocation.

```js
disruptor.injectMemoryStress(
  { bytes: 256 * 1024 * 1024 },  // 256 MiB
  "60s",
);
```

Fields: `bytes` (required). **Warning:** if `bytes` exceeds the pod's memory limit the pod will be OOM-killed.

### DNS Faults

Starts an in-pod DNS proxy and redirects all UDP port 53 traffic to it. Non-faulted queries are forwarded to the configured upstream.

```js
disruptor.injectDNSFaults(
  {
    errorRate: 0.3,                         // 30% of queries return NXDOMAIN
    spoof: { "payments.internal": "192.0.2.1" },
    upstreamDNS: "10.96.0.10:53",          // use kube-dns, not 8.8.8.8
  },
  "60s",
);
```

Fields: `errorRate` (0–1), `spoof` (domain → IP map), `upstreamDNS` (default `"8.8.8.8:53"` — **change this to your cluster DNS** for in-cluster use).

### Disk Fill

Writes a large file inside the pod to consume ephemeral storage quota. The file is deleted automatically when the fault ends. If `bytes` exceeds `resources.limits.ephemeral-storage`, the kubelet evicts the pod.

```js
// Fill 500 MiB of ephemeral storage
disruptor.injectDiskFill(
  { bytes: 500 * 1024 * 1024 },
  "60s",
);
```

Fields: `bytes` (required), `path` (default `"/tmp"`), `blockSize` (default 262144 = 256 KiB). Note: ephemeral-storage limits must be set in the pod spec for eviction to trigger.

### IO Stress

Runs N parallel workers that continuously write and read back a fixed-size file to create sustained I/O pressure. Does not fill up the disk — goal is throughput/IOPS saturation.

```js
// 4 workers × 10 MiB working set = 40 MiB per write/read cycle
disruptor.injectIOStress(
  { path: "/data", workers: 4, bytesPerWorker: 10 * 1024 * 1024 },
  "60s",
);
```

Fields: `path` (default `"/tmp"`), `workers` (default `4`), `bytesPerWorker` (default 1 MiB). Set `path` to a PVC mount to target a specific volume.

## NodeDisruptor

`NodeDisruptor` targets Kubernetes nodes rather than pods. It supports two categories of operation:

- **API-only** (`drain`, `taintNode`): implemented directly via the Kubernetes node API — no agent injection.
- **Privileged pod** (`injectCPUStress`, `injectMemoryStress`, `injectIOStress`, `injectKubeletServiceKill`): creates a temporary privileged pod (`hostPID=true`) on the target node using the agent image, runs the stressor, then deletes the pod.

### Constructor

```js
import { NodeDisruptor } from "k6/x/disruptor";

// Target a specific node by name
const disruptor = new NodeDisruptor({ name: "worker-node-1" });

// Or target nodes by label selector
const disruptor = new NodeDisruptor({
  select: { labels: { "node-role.kubernetes.io/worker": "" } },
  agentNamespace: "kube-system",   // namespace for privileged helper pods (default: kube-system)
  agentImage: "myregistry/xk6-disruptor-agent:v1.0",  // optional image override
});
```

### Node Drain

Cordons the node, evicts all eligible pods, waits for `duration`, then uncordons.

```js
disruptor.drain(
  { skipDaemonSets: true, deleteLocalData: false },
  "120s",
);
```

Fields: `skipDaemonSets` (default `false`), `deleteLocalData` (default `false`), `timeout` (per-pod eviction timeout, default `300s`).

### Node Taint

Adds a taint to the node, waits for `duration`, then removes it.

```js
disruptor.taintNode(
  { key: "chaos", value: "true", effect: "NoSchedule" },
  "60s",
);
```

Fields: `key` (required), `value`, `effect` (`"NoSchedule"` | `"PreferNoSchedule"` | `"NoExecute"`, default `"NoSchedule"`).

### Node CPU Stress

Runs the agent as a privileged pod on the node to stress CPU at node level.

```js
disruptor.injectCPUStress({ load: 90, cpus: 4 }, "60s");
```

### Node Memory Stress

Runs the agent as a privileged pod on the node to allocate memory at node level.

```js
disruptor.injectMemoryStress({ bytes: 2 * 1024 * 1024 * 1024 }, "60s"); // 2 GiB
```

### Node IO Stress

Runs the agent as a privileged pod on the node with sustained I/O workers.

```js
disruptor.injectIOStress(
  { path: "/var/lib/kubelet", workers: 4, bytesPerWorker: 50 * 1024 * 1024 },
  "60s",
);
```

### Kubelet Service Kill

Stops the kubelet systemd service on the node for `duration` then restarts it. Uses `nsenter` to enter the host's systemd mount namespace (requires `hostPID=true`, which is set automatically on the helper pod).

```js
disruptor.injectKubeletServiceKill("30s");
```

After the kubelet stops the node becomes `NotReady` (after the node monitor grace period, default ~40 s). Active pods continue running but new scheduling and exec operations are blocked. The kubelet is automatically restarted before the method returns.

### RBAC requirements for NodeDisruptor

The service account running k6 needs the following permissions in addition to what `PodDisruptor` requires:

```yaml
- apiGroups: [""]
  resources: ["nodes"]
  verbs: ["get", "list", "watch", "patch", "update"]
- apiGroups: [""]
  resources: ["pods"]
  verbs: ["get", "list", "watch", "create", "delete"]
- apiGroups: ["policy"]
  resources: ["evictions"]
  verbs: ["create"]
```

For `drain` and eviction operations the service account also needs `pods/eviction` in all namespaces.

## WorkloadDisruptor

`WorkloadDisruptor` targets Kubernetes workload objects (Deployments and StatefulSets) and changes their desired replica count to inject chaos at the workload level. Unlike `PodDisruptor` and `NodeDisruptor`, it requires no agent image and no privileged pod — all operations are pure Kubernetes API calls (Get + Update on `spec.replicas`).

Typical scenarios:

- Scale a Deployment to `0` to simulate a full outage of a dependency.
- Reduce replicas by a delta (e.g. `-2`) to simulate partial capacity loss.
- Halve replicas (`percentage: 50`) to simulate a rolling failure across a fleet.

### Constructor

```js
import { WorkloadDisruptor } from "k6/x/disruptor";

// Target a single Deployment by name
const disruptor = new WorkloadDisruptor({
  kind: "Deployment",
  namespace: "default",
  select: { name: "my-app" },
});

// Or target multiple workloads by label selector
const disruptor = new WorkloadDisruptor({
  kind: "Deployment",
  namespace: "default",
  select: { labels: { team: "platform" } },
});
```

Constructor argument fields:

| Field | Type | Required | Description |
|---|---|---|---|
| `kind` | `string` | Yes | Workload kind: `"Deployment"` or `"StatefulSet"`. |
| `namespace` | `string` | No | Namespace to scope the lookup. Defaults to `"default"`. |
| `select.name` | `string` | One-of | Target a single workload by exact name. |
| `select.labels` | `object` | One-of | Match-all label map. Targets every workload of `kind` in `namespace` matching every label. |

Exactly one of `select.name` or `select.labels` must be set.

### `scaleReplicas(fault, duration?)`

Applies a replica change to every selected workload concurrently. The original replica count of each workload is recorded on the disruptor; calling `cleanup()` later restores those originals.

Fault fields (exactly **one** of `replicas`, `delta`, `percentage` must be set):

| Field | Type | Description |
|---|---|---|
| `replicas` | `int32` | Absolute target replica count. `0` scales the workload to zero. |
| `delta` | `int32` | Relative change. Negative values reduce; the result is clamped to `0` if it would go below zero. |
| `percentage` | `int32` | Percent of current replicas (floor rounding; `50` halves, `0` scales to zero, `200` doubles). Must be `>= 0`. |
| `autoRevert` | `bool` | Defaults to `false`. When `true`, the method blocks for `duration`, then restores each workload to its original replica count before returning. When `false`, the change is applied and the call returns immediately; replicas remain changed until `cleanup()` is called. |

`duration` is required only when `autoRevert: true`.

```js
// Scale to zero and leave there until cleanup()
disruptor.scaleReplicas({ replicas: 0 });

// Remove 2 replicas for 30 s then auto-restore
disruptor.scaleReplicas({ delta: -2, autoRevert: true }, "30s");

// Halve replicas for 1 minute then auto-restore
disruptor.scaleReplicas({ percentage: 50, autoRevert: true }, "1m");

// Always call cleanup() (safe even when autoRevert was true; no-op for already-restored workloads)
disruptor.cleanup();
```

### Targets

`disruptor.targets()` returns the list of resolved workload refs as strings of the form `Kind/namespace/name`, e.g. `Deployment/default/my-app`. This also emits the `xk6_disruptor_targets_selected` metric.

`disruptor.targetIPs()` always returns an empty array — pod IPs are not meaningful for a workload-level disruptor; resolve them via `PodDisruptor` or `Service.spec.selector` instead.

### RBAC requirements for WorkloadDisruptor

The service account running k6 needs permission to read **and update** the workload resources (not just the `/scale` subresource — the implementation writes `spec.replicas` directly so it works against both the real apiserver and client-go's fake clientset):

```yaml
- apiGroups: ["apps"]
  resources: ["deployments", "statefulsets"]
  verbs: ["get", "list", "update"]
```

### Caveats

- **HPA conflicts:** if a HorizontalPodAutoscaler manages the target workload, your `scaleReplicas` change will fight the HPA. The HPA will re-set replicas to its computed value within ~15 seconds. For deterministic results, scale-disable the HPA before the test (`kubectl autoscale ... --min=N --max=N`) or temporarily delete it.
- **`autoRevert: false`:** when `autoRevert` is false, replicas remain at the changed value until `cleanup()` is called. Always pair the disruptor with a `teardown()` that calls `cleanup()` to avoid leaking scale state.
- **Single workload kind per disruptor:** one `WorkloadDisruptor` instance targets a single `kind` (Deployment **or** StatefulSet). Create two disruptors if you need both.

## Agent image configuration

xk6-disruptor injects an ephemeral container (`xk6-disruptor-agent`) into target pods to apply faults. The container image used can be configured at three levels:

| Level | How |
|---|---|
| Per script | `agentImage: 'ghcr.io/myorg/xk6-disruptor-agent:latest'` in disruptor options |
| Per run | `XK6_DISRUPTOR_AGENT_IMAGE=ghcr.io/myorg/xk6-disruptor-agent:latest k6 run script.js` |
| Build time | `-ldflags "-X .../version.agentImageRepo=ghcr.io/myorg/xk6-disruptor-agent"` when building k6 |

Build the agent image locally from the repo root:

```bash
docker build -t ghcr.io/danhngo-lx/xk6-disruptor-agent:latest -f images/agent/Dockerfile .
```

See the [architecture guide](docs/01-development/02-architecture.md#agent-image-configuration) for full details.

## Service mesh compatibility

When running inside an **Istio**-enabled namespace, Istio's `istio-init` container installs iptables rules that intercept all inbound traffic before xk6-disruptor can redirect it. The recommended fix is to exclude the target app port from Istio's interception via a Deployment annotation:

```bash
kubectl patch deployment <name> -n <namespace> \
  --type='json' \
  -p='[{"op":"add","path":"/spec/template/metadata/annotations","value":{"traffic.sidecar.istio.io/excludeInboundPorts":"<port>"}}]'
```

If you cannot modify the Deployment, use `nonTransparent: true` in `HTTPDisruptionOptions` and target pod IPs directly (obtained via `disruptor.targetIPs()`). See the [architecture guide](docs/01-development/02-architecture.md#service-mesh-compatibility) for details.

## Metrics

xk6-disruptor emits k6 metrics for every fault injection so you can correlate disruptor activity with k6 HTTP/gRPC results on the same Grafana dashboard, Prometheus alert, or InfluxDB query — without instrumenting anything yourself.

### Emitted metrics

Every metric carries the same four base tags:

| Tag | Values |
|---|---|
| `fault_type` | `http`, `http_reset_peer`, `grpc`, `terminate`, `network`, `network_shaping`, `network_partition`, `cpu_stress`, `memory_stress`, `io_stress`, `dns`, `crash_loop`, `disk_fill`, `drain`, `taint`, `kubelet_kill`, `replica_change` |
| `disruptor` | `pod`, `service`, `node`, `workload` |
| `target_namespace` | Kubernetes namespace the disruptor targets (may be empty for cluster-scoped node faults) |
| `target_name` | Service name, node name, or serialized pod selector (e.g. `app=frontend,!canary=true`) |

| Metric | Type | When emitted |
|---|---|---|
| `xk6_disruptor_fault_active` | Gauge | `1` immediately before the underlying call runs; back to `0` when it returns (success **or** error). |
| `xk6_disruptor_faults_injected_total` | Counter | `+1` per fault-injection call at start. |
| `xk6_disruptor_faults_failed_total` | Counter | `+1` when an injection call returns an error. Adds `error_class` tag (`timeout`, `canceled`, `exec_failed`, `inject_failed`, `other`). |
| `xk6_disruptor_fault_duration_seconds` | Trend | Wall-clock duration of each call. Adds `outcome` tag (`success` / `error`). |
| `xk6_disruptor_targets_selected` | Gauge | Number of targets matched by the selector. Emitted when `disruptor.targets()` is called from JS. |

### Grafana annotations

`xk6_disruptor_fault_active` is designed to drive Grafana annotations or shaded "fault window" regions on dashboards.

**Shaded region while a fault is active** (Prometheus annotation query):

```promql
xk6_disruptor_fault_active == 1
```

**Vertical markers at each fault start** (annotation query for discrete events):

```promql
changes(xk6_disruptor_faults_injected_total[$__rate_interval]) > 0
```

In a k6 run, export to Prometheus / InfluxDB / etc. via the standard k6 output flag — e.g. `k6 run --out experimental-prometheus-rw=… script.js` — and the disruptor metrics flow alongside `http_req_duration` and friends.

### Caveats

- **`setup()` / `teardown()`**: faults injected from these phases run normally but emit no metrics, because k6 does not expose VU state outside the default function.
- **Concurrent injections with identical tags**: if two goroutines inject the same `fault_type` against the same target at the same time, the gauge drops to `0` as soon as the first one finishes even though the second is still active. In practice this is rare; a per-injection ID tag could disambiguate at the cost of higher cardinality.

## Use cases

The main use case for xk6-disruptor is to test the resiliency of an application of diverse types of disruptions by reproducing their effects without reproducing their root causes. For example, inject delays in the HTTP requests an application makes to a service without having to stress or interfere with the infrastructure (network, nodes) on which the service runs or affect other workloads in unexpected ways.

In this way, xk6-disruptor make reliability tests repeatable and predictable while limiting their blast radius. These are essential characteristics to incorporate these tests in the test suits of applications deployed on shared infrastructures such as staging environments.

## Learn more

Check the [get started guide](https://k6.io/docs/javascript-api/xk6-disruptor/get-started) for instructions on how to install and use `xk6-disruptor`.

The [examples](https://k6.io/docs/javascript-api/xk6-disruptor/examples/) section in the documentation presents examples of using xk6-disruptor for injecting faults in different scenarios.

If you encounter any bugs or unexpected behavior, please search the [currently open GitHub issues](https://github.com/grafana/xk6-disruptor/issues) first, and create a new one if it doesn't exist yet.

The [Roadmap](/ROADMAP.md) presents the project's goals for the coming months regarding new functionalities and enhancements.

If you are interested in contributing with the development of this project, check the [contributing guide](/docs/01-development/01-contributing.md)



