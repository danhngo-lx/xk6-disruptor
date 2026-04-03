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

It offers an [API](https://k6.io/docs/javascript-api/xk6-disruptor/api) for creating disruptors that target one specific type of component (e.g., Pods) and is capable of injecting different kinds of faults. Disruptors exist for [Pods](https://k6.io/docs/javascript-api/xk6-disruptor/api/poddisruptor) and [Services](https://k6.io/docs/javascript-api/xk6-disruptor/api/servicedisruptor).

### Supported fault types

| Fault | API method | Description | Status |
|---|---|---|---|
| HTTP faults | `injectHTTPFaults` | Delay, error code injection, response body/header modification | ✅ Stable |
| gRPC faults | `injectGrpcFaults` | Delay and gRPC status code injection | ✅ Stable |
| Network drop | `injectNetworkFaults` | Drop ingress packets by port/protocol via iptables | ✅ Stable |
| Pod termination | `terminatePods` | Terminate a random subset of target pods | ✅ Stable |
| Crash loop | `injectCrashLoopFault` | Repeatedly kill a container's PID 1 to drive the pod into CrashLoopBackOff | ✅ Stable |
| Network shaping | `injectNetworkShapingFaults` | Packet delay, jitter, loss, corruption, duplication, rate limiting via `tc netem` | ⚠️ Experimental |
| Network partition | `injectNetworkPartition` | Block traffic to/from specific CIDRs or IPs | ⚠️ Experimental |
| CPU stress | `injectCPUStress` | Consume a percentage of CPU across N cores | ⚠️ Experimental |
| Memory pressure | `injectMemoryStress` | Allocate and hold a given amount of memory | ⚠️ Experimental |
| DNS faults | `injectDNSFaults` | Return NXDOMAIN for a fraction of queries or spoof domains to fake IPs | ⚠️ Experimental |

> ⚠️ **Experimental** faults are code-complete and follow the same implementation patterns as stable faults, but have not yet been validated end-to-end in a live cluster. They require the custom agent image to be built from this fork. Use with caution and report issues.

See the [architecture guide](docs/01-development/02-architecture.md#experimental-fault-types) for detailed usage, field references, and examples for each experimental fault.

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

## Use cases

The main use case for xk6-disruptor is to test the resiliency of an application of diverse types of disruptions by reproducing their effects without reproducing their root causes. For example, inject delays in the HTTP requests an application makes to a service without having to stress or interfere with the infrastructure (network, nodes) on which the service runs or affect other workloads in unexpected ways.

In this way, xk6-disruptor make reliability tests repeatable and predictable while limiting their blast radius. These are essential characteristics to incorporate these tests in the test suits of applications deployed on shared infrastructures such as staging environments.

## Learn more

Check the [get started guide](https://k6.io/docs/javascript-api/xk6-disruptor/get-started) for instructions on how to install and use `xk6-disruptor`.

The [examples](https://k6.io/docs/javascript-api/xk6-disruptor/examples/) section in the documentation presents examples of using xk6-disruptor for injecting faults in different scenarios.

If you encounter any bugs or unexpected behavior, please search the [currently open GitHub issues](https://github.com/grafana/xk6-disruptor/issues) first, and create a new one if it doesn't exist yet.

The [Roadmap](/ROADMAP.md) presents the project's goals for the coming months regarding new functionalities and enhancements.

If you are interested in contributing with the development of this project, check the [contributing guide](/docs/01-development/01-contributing.md)



