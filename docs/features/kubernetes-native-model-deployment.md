# Toskar Core — Kubernetes-Native Model Deployment

## Feature Specification — V1

### Status

**Type:** Feature  
**Target:** Toskar Core  
**Initial implementation:** Kubernetes backend using standard Kubernetes resources  
**Future extension:** Toskar Operator and CRDs

---

## 1. Goal

Allow Toskar Core to run natively inside Kubernetes and use a Kubernetes cluster as a model execution environment.

Toskar should be able to:

- discover usable compute resources in the cluster,
- understand which nodes can run which models,
- deploy model runtimes to appropriate Kubernetes nodes,
- start, stop, restart, and monitor model workloads,
- route inference requests to deployed model instances,
- expose model health and placement through the normal Toskar APIs,
- integrate Kubernetes resources into Norn's placement decisions.

The user-facing concept should remain simple:

> "Run this model on my Kubernetes cluster."

The user should not need to manually author Deployments, Services, node affinity rules, GPU requests, health probes, or model-runtime configuration for ordinary use.

---

## 2. Product Principle

Kubernetes should become another Toskar execution target.

Today, Norn can reason about Toskar computers and place workloads on them.

With Kubernetes support:

```text
Norn
├── Local computer
├── Paired Toskar computer
└── Kubernetes cluster
    ├── Node A
    ├── Node B
    └── Node C
```

Toskar should not attempt to replace the Kubernetes scheduler.

Norn decides **what placement characteristics are required**.

Kubernetes decides **which exact node receives the Pod** based on those constraints.

---

## 3. Non-Goal: Rebuilding Kubernetes Scheduling

Toskar should not become a competing container scheduler.

Do not implement:

- a custom pod scheduler for V1,
- manual pod-to-node placement when Kubernetes-native affinity can express the intent,
- custom replacement logic for Deployments/ReplicaSets,
- custom container health supervision that duplicates Kubernetes probes.

Instead:

```text
Norn determines intent
        ↓
Toskar translates intent
        ↓
Kubernetes resources/constraints
        ↓
Kubernetes scheduler places workload
```

Examples of placement intent:

- requires NVIDIA GPU,
- requires at least 16 GB VRAM,
- prefer nodes with model already cached,
- avoid nodes under high memory pressure,
- tolerate a specific GPU taint,
- run only on nodes labeled for AI workloads.

---

## 4. Initial Architecture

### Control Plane

Toskar Core runs as a Kubernetes workload.

Suggested V1:

```text
Deployment
└── toskar-core
    ├── API
    ├── Norn
    ├── Heimdall
    ├── Kubernetes backend
    └── model deployment controller
```

Core communicates with the Kubernetes API using an in-cluster ServiceAccount.

### Model Workloads

Each deployed model runs as a Kubernetes-managed workload.

Conceptually:

```text
Toskar Core
      │
      ▼
Kubernetes API
      │
      ├── Deployment / StatefulSet
      │     └── Model runtime Pod
      │
      ├── Service
      │     └── Model endpoint
      │
      ├── ConfigMap / Secret
      │
      └── PVC / cache volume
```

The exact resource type may vary by workload.

---

## 5. Do We Need an Operator?

### V1: No

Do **not** require a Kubernetes Operator for the first implementation.

Core can directly create and manage standard Kubernetes resources.

This keeps the first release smaller and lets Toskar validate:

- resource discovery,
- model placement,
- model startup,
- routing,
- lifecycle management,
- storage,
- health,
- cluster compatibility.

### Later: Probably Yes

An Operator becomes valuable when Toskar wants declarative Kubernetes-native resources such as:

```yaml
apiVersion: toskar.ai/v1alpha1
kind: ModelDeployment
metadata:
  name: qwen-coder
spec:
  model: Qwen/Qwen2.5-Coder-14B
  replicas: 2
  placement:
    strategy: automatic
  runtime:
    type: llama.cpp
```

At that stage the Operator can reconcile desired state continuously.

Potential future CRDs:

- `ModelDeployment`
- `ToskarNode`
- `ModelProfile`
- `ModelGrid`
- `ModelCache`
- `TrainingJob`

The Operator should be a later phase, not a prerequisite for Kubernetes support.

---

## 6. Kubernetes Backend Abstraction

Add Kubernetes as a placement/runtime backend rather than scattering Kubernetes API calls throughout Norn.

Conceptual interface:

```go
type ComputeBackend interface {
    DiscoverResources(ctx context.Context) ([]ComputeResource, error)
    ValidatePlacement(ctx context.Context, req PlacementRequest) error
    DeployModel(ctx context.Context, spec ModelDeploymentSpec) (DeploymentRef, error)
    StopModel(ctx context.Context, ref DeploymentRef) error
    DeleteModel(ctx context.Context, ref DeploymentRef) error
    GetHealth(ctx context.Context, ref DeploymentRef) (HealthStatus, error)
    GetEndpoint(ctx context.Context, ref DeploymentRef) (Endpoint, error)
}
```

Possible implementations:

```text
ComputeBackend
├── LocalNodeBackend
├── ToskarNodeBackend
└── KubernetesBackend
```

Norn should operate on common placement concepts wherever practical.

---

## 7. Resource Discovery

Toskar must understand the cluster before it can recommend or deploy models.

V1 should discover:

- Kubernetes nodes,
- allocatable CPU,
- allocatable memory,
- GPU resources exposed by device plugins,
- node labels,
- taints,
- current scheduling availability,
- architecture,
- operating system,
- known accelerator type where available,
- model-runtime compatibility metadata where available.

Potential normalized representation:

```text
Cluster Node
Name: gpu-node-02
CPU: 32 cores
Memory: 128 GB
GPU: NVIDIA RTX 4090
GPU count: 1
GPU memory: 24 GB
Labels:
  yggdrasil.ai/workload=model
Status: Ready
```

Do not depend exclusively on free-form node labels for hardware capability.

Where Kubernetes does not expose enough hardware metadata directly, Toskar may introduce an optional node inventory component later.

---

## 8. Optional Node Agent

V1 should avoid requiring a privileged node agent unless necessary.

However, an optional Toskar node agent may become useful for:

- detailed GPU inventory,
- VRAM information not exposed through standard resources,
- runtime capability detection,
- local model cache status,
- disk availability,
- driver/runtime versions,
- temperature/health telemetry,
- high-quality performance estimates.

If introduced, deploy it as a DaemonSet.

```text
DaemonSet
└── yggdrasil-node-agent
    └── one Pod per eligible node
```

The node agent should report capability data to Core but should not replace Kubernetes scheduling.

---

## 9. Model Deployment Lifecycle

Normal lifecycle:

```text
Requested
   ↓
Planning
   ↓
Creating resources
   ↓
Pulling runtime/model
   ↓
Starting
   ↓
Ready
   ↓
Running
   ↓
Stopping
   ↓
Stopped
```

Failure states:

```text
Pending
ImagePullError
ModelDownloadError
Unschedulable
RuntimeFailed
OOMKilled
ProbeFailed
CrashLoopBackOff
Unknown
```

Toskar should translate Kubernetes-specific states into clear user-facing model states.

Example:

```text
Kubernetes:
0/4 nodes are available: insufficient nvidia.com/gpu

Toskar:
No cluster node currently has enough available GPU capacity for this model.
```

Advanced mode may expose the raw Kubernetes reason.

---

## 10. Workload Types

### Deployment

Use for stateless model servers where:

- Pod identity does not matter,
- model state is external or reproducible,
- replicas may be supported.

### StatefulSet

Use only when stable identity/storage association is required.

Do not default to StatefulSet unnecessarily.

### Job

Use for finite tasks such as:

- future model conversion,
- benchmarking,
- one-time preprocessing,
- future training/customization jobs.

### DaemonSet

Use only for node-local components such as an optional inventory/agent service.

---

## 11. Model Runtime Pod

A model Pod should contain one primary runtime process.

Examples may include:

- llama.cpp server,
- vLLM,
- Ollama-compatible worker,
- future runtime adapters.

Toskar should generate runtime-specific configuration from its existing runtime abstraction.

Conceptually:

```text
ModelDeploymentSpec
├── model
├── quantization
├── runtime
├── context length
├── resource request
├── placement requirements
├── storage/cache
└── environment
```

Do not make Kubernetes-specific concepts leak unnecessarily into normal model configuration.

---

## 12. Resource Requests and Limits

Toskar should generate reasonable Kubernetes resource requests.

Examples:

```yaml
resources:
  requests:
    cpu: "4"
    memory: "12Gi"
    nvidia.com/gpu: "1"
  limits:
    nvidia.com/gpu: "1"
```

CPU and memory limits require care because overly strict limits can destabilize inference workloads.

V1 should prefer safe requests and conservative limit behavior.

GPU requests should use the resource names exposed by the cluster's configured device plugin.

Do not assume every cluster uses NVIDIA.

---

## 13. GPU and Accelerator Support

Design for multiple accelerator ecosystems.

Potential resource environments include:

- NVIDIA device plugin,
- AMD GPU device plugins,
- Intel GPU resources,
- CPU-only workers,
- future vendor-neutral device interfaces.

Toskar should normalize accelerator capability internally.

Example:

```text
Accelerator
vendor: NVIDIA
model: RTX 4090
memory: 24 GB
count: 1
resourceName: nvidia.com/gpu
```

Avoid hard-coding NVIDIA semantics throughout Core.

---

## 14. Placement Strategy

Norn should derive Kubernetes scheduling constraints.

Possible signals:

- required accelerator vendor/type,
- minimum estimated model memory,
- available system RAM,
- model cache locality,
- node health,
- affinity preferences,
- taints/tolerations,
- user-defined labels,
- current Toskar workload pressure.

Generated Kubernetes constructs may include:

- `nodeSelector`,
- node affinity,
- pod affinity/anti-affinity,
- tolerations,
- resource requests.

User-facing options should remain simple.

Example:

```text
Run on:
● Automatic
○ Kubernetes cluster
○ Specific cluster node
```

Advanced mode may expose placement rules.

---

## 15. Model Fit in Kubernetes

Existing Toskar fit logic should be extended to cluster resources.

Potential results:

```text
Qwen 7B
Good fit — 3 cluster nodes available

Qwen 14B
Good fit — 1 cluster node available

Llama 70B
Doesn't fit on a single cluster node
Grid research required
```

Do not treat aggregate cluster memory as automatically available to one model.

Until Model Grid exists, a single model instance must fit on one Kubernetes execution node unless the selected runtime already supports distributed execution.

---

## 16. Model Storage and Cache

Model download size makes storage architecture important.

Support multiple strategies over time.

### V1 Options

#### Node-local cache

Models are cached on the Kubernetes node.

Advantages:

- fast startup after first use,
- simple runtime access.

Disadvantages:

- cache duplication,
- Pod rescheduling can move to a node without the model.

#### Shared PersistentVolume

Models live on shared storage.

Advantages:

- avoids repeated downloads,
- any compatible node can access the model.

Disadvantages:

- shared storage throughput may become a bottleneck.

### Future

Possible Toskar-aware model cache:

```text
Model requested
      ↓
Norn knows node B already has model
      ↓
Prefer node B
```

Model cache locality should eventually influence placement.

---

## 17. Model Downloading

The cluster must not require each user to manually preload models.

Toskar should support automated model acquisition.

Possible V1 patterns:

- init container downloads the model,
- model cache volume populated before runtime starts,
- Toskar cache service,
- preexisting mounted model directory.

Credentials for private model repositories must use Kubernetes Secrets.

Never embed credentials directly in generated Pod specs or ConfigMaps.

---

## 18. Networking

Model workloads need internal endpoints.

Use Kubernetes Services for normal internal routing.

Example:

```text
Toskar Core
     │
     ▼
ClusterIP Service
     │
     ▼
Model Pod
```

Core should route model requests internally without exposing every runtime publicly.

### External Exposure

Toskar Core itself may be exposed using:

- ClusterIP,
- LoadBalancer,
- Ingress,
- Gateway API,
- port-forward for development.

V1 Helm defaults should be conservative.

Default:

```text
Core Service: ClusterIP
External access: disabled
```

Do not automatically expose model runtime Pods directly to the Internet.

---

## 19. API Authentication

Existing Toskar authentication rules still apply.

If Core is reachable beyond loopback or outside a trusted local boundary:

- authentication is required,
- secrets must use Kubernetes Secrets,
- API keys must never be committed to manifests,
- TLS should be supported for externally exposed deployments.

Kubernetes network placement does not replace application authentication.

---

## 20. Kubernetes RBAC

Core must use least-privilege RBAC.

Create a dedicated ServiceAccount.

Grant only the operations required for enabled features.

Likely V1 resources:

- Pods
- Deployments
- ReplicaSets read access where needed
- Services
- ConfigMaps
- Secrets only where strictly necessary
- PersistentVolumeClaims
- Events
- Nodes read-only
- Jobs if supported

Avoid:

- cluster-admin,
- unrestricted Secrets access,
- wildcard API permissions.

Split namespace-scoped and cluster-scoped permissions where practical.

Node discovery may require cluster-level read permission on Nodes.

Model workloads should ideally live in a dedicated namespace.

Suggested default:

```text
Namespace: yggdrasil
```

---

## 21. Namespace Strategy

V1 should support a configured namespace.

Default:

```text
yggdrasil
```

Possible future support:

- multiple namespaces,
- namespace per team,
- namespace per environment,
- read-only observation across namespaces.

Do not require multi-namespace orchestration for V1.

---

## 22. Health Monitoring

Heimdall should translate Kubernetes health into Toskar health.

Signals include:

- Pod phase,
- container state,
- restart count,
- readiness probes,
- liveness probes,
- Kubernetes events,
- OOMKilled,
- CrashLoopBackOff,
- ImagePullBackOff,
- FailedScheduling.

Model runtime health probes should remain authoritative for whether inference is usable.

Kubernetes Pod Running does not necessarily mean the model is ready.

---

## 23. Readiness and Liveness

Generated model workloads should include appropriate probes.

### Readiness

Answers:

> Can this Pod serve inference now?

Do not mark ready before:

- runtime started,
- model loaded,
- health endpoint passes.

### Liveness

Answers:

> Is the runtime process still functioning?

Use conservative thresholds because loading large models can take significant time.

### Startup Probe

Strongly consider startup probes for large models so Kubernetes does not kill a healthy Pod simply because model loading is slow.

---

## 24. Failure and Recovery

Kubernetes already provides restart/reconciliation behavior.

Toskar should use it rather than fighting it.

Examples:

### Runtime crashes

Kubernetes restarts container.

Heimdall observes restarts.

Repeated restart failures become a Toskar model failure.

### Node disappears

Kubernetes marks node unavailable and reschedules where permitted.

Toskar observes state and reports that the model is recovering.

### Model cannot fit after rescheduling

Toskar marks deployment degraded/failed and explains the placement issue.

Do not create an independent infinite restart loop inside Core.

---

## 25. Replicas and Scaling

V1 may begin with:

```text
replicas: 1
```

Future support:

- multiple replicas for throughput,
- request balancing,
- horizontal autoscaling,
- queue-length-based scaling,
- GPU-utilization-based scaling,
- scale-to-zero.

Scale-to-zero is especially relevant for local/private AI because idle models consume substantial memory.

A future policy may be:

```text
Keep loaded: 10 minutes
No requests
      ↓
Scale to zero
      ↓
New request
      ↓
Scale back to one
```

This should integrate with Toskar's existing model lifecycle concepts.

---

## 26. Routing

Norn should treat each healthy Kubernetes model deployment as an inference target.

Conceptually:

```text
Request
   ↓
Norn
   ├── local model
   ├── paired computer model
   └── Kubernetes model
```

Routing signals may include:

- model identity,
- profile requirements,
- current load,
- estimated latency,
- location,
- cluster health,
- cost/power policies,
- user preference.

---

## 27. Multi-Cluster Support

Not required for V1.

Design APIs so a future Toskar installation can understand:

```text
Clusters
├── home-lab
├── office
└── gpu-cluster
```

Each cluster should eventually have:

- identity,
- credentials,
- capabilities,
- health,
- placement policy.

Initial in-cluster Core only needs one local Kubernetes cluster.

---

## 28. Helm Chart

Kubernetes support should ship with an official Helm chart.

Suggested structure:

```text
charts/
└── toskar-core/
    ├── Chart.yaml
    ├── values.yaml
    └── templates/
```

Helm should install:

- namespace optionally,
- Core Deployment,
- Service,
- ServiceAccount,
- RBAC,
- optional PVC,
- optional node agent DaemonSet later.

Example:

```bash
helm install toskar ./charts/toskar-core
```

Future:

```bash
helm repo add yggdrasil ...
helm install toskar toskar/toskar-core
```

Do not publish chart installation commands until the chart actually exists and is tested.

---

## 29. Configuration

Configuration should be available through:

- Helm values,
- environment variables,
- mounted config file where useful.

Important settings may include:

```text
namespace
model storage class
cache strategy
API bind/exposure
authentication
default runtime
node selectors
tolerations
resource defaults
optional node agent
```

Avoid requiring users to manually edit generated Kubernetes resources.

---

## 30. Observability

Toskar should expose Kubernetes model status through existing diagnostics.

Potential future Prometheus metrics:

- model deployments,
- healthy replicas,
- deployment failures,
- model startup duration,
- model load duration,
- inference requests,
- queue depth,
- tokens/sec,
- node placement,
- cache hit/miss,
- Pod restarts.

Do not make a full observability stack mandatory for V1.

---

## 31. Existing Kubernetes Ecosystem

Toskar should coexist with common Kubernetes GPU/container infrastructure.

Potential integrations to consider:

- NVIDIA device plugin / GPU Operator,
- AMD GPU device plugins,
- Intel device plugins,
- Kubernetes Gateway API,
- standard CSI storage,
- Prometheus,
- cert-manager.

Do not duplicate functionality that established Kubernetes components already provide.

---

## 32. Operator Phase

After direct Kubernetes support is stable, evaluate a Toskar Operator.

### Why an Operator may be valuable

Operators are useful when desired Toskar state should live in the Kubernetes API.

Example:

```yaml
kind: ModelDeployment
spec:
  model: ...
```

Then:

```text
CRD desired state
      ↓
Operator
      ↓
Deployment / Service / Storage
      ↓
Continuous reconciliation
```

Potential benefits:

- GitOps,
- declarative model management,
- Kubernetes-native workflows,
- automatic reconciliation,
- easier integration with Argo CD / Flux,
- custom status objects,
- versioned model rollouts.

### Why not start there

An Operator introduces:

- CRD lifecycle,
- API versioning,
- controller reconciliation complexity,
- upgrade/migration concerns,
- more Kubernetes-specific code.

Toskar should first prove that model deployment works well using standard Kubernetes resources.

---

## 33. Future CRDs

Potential future objects:

### ModelDeployment

```text
Desired model runtime deployment.
```

### ModelProfile

```text
Reusable model/runtime configuration.
```

### ModelCache

```text
Desired model availability on cluster nodes.
```

### TrainingJob

```text
Future specialized-model training workload.
```

### ModelGrid

```text
Future distributed single-model execution across multiple nodes.
```

The `ModelGrid` concept could eventually connect directly to Toskar's distributed-inference research.

---

## 34. Relationship to Toskar Grid

Kubernetes integration and Grid are different features.

### Kubernetes model deployment

```text
One model instance
      ↓
One Kubernetes node
```

### Model Grid

```text
One model instance
      ↓
Multiple nodes together
```

Kubernetes support can become an important foundation for Grid, but V1 Kubernetes support should not require distributed single-model inference.

A future Kubernetes-backed Model Grid might use:

- vLLM distributed execution,
- Ray,
- pipeline parallelism,
- tensor parallelism,
- other runtime-specific mechanisms.

---

## 35. User Experience

Normal Toskar UI could eventually show:

```text
Computers & Compute

This Mac
Ready

Gaming PC
Ready

Kubernetes
8 nodes · 4 GPU nodes
Ready
```

Model installation:

```text
Qwen Coder 14B

Run on:
● Automatic
○ This computer
○ Kubernetes

Kubernetes:
3 compatible nodes available

[Deploy]
```

After deployment:

```text
Qwen Coder 14B

Running on Kubernetes
Node: gpu-worker-03
Runtime: llama.cpp
Status: Healthy

[Open Chat] [Stop]
```

Advanced details can expose:

- namespace,
- Pod,
- Service,
- node,
- resources,
- restart count,
- raw events.

---

## 36. CLI Experience

Potential commands:

```text
yggdrasilctl kubernetes status
yggdrasilctl kubernetes nodes
yggdrasilctl model deploy MODEL --target kubernetes
yggdrasilctl model deployments
yggdrasilctl model stop DEPLOYMENT
```

Exact commands should follow the existing CLI structure.

Do not introduce a separate `kubectl-yggdrasil` tool for V1 unless a compelling need emerges.

---

## 37. Security Requirements

V1 must satisfy:

- least-privilege ServiceAccount,
- no cluster-admin requirement,
- no credentials in ConfigMaps,
- API authentication for exposed Core endpoints,
- no model runtime Service externally exposed by default,
- explicit opt-in for external ingress,
- namespace-scoped workload management where possible,
- safe log handling,
- no secret values surfaced in diagnostics.

---

## 38. V1 Scope

Include:

- Toskar Core running inside Kubernetes.
- Official Helm chart.
- Kubernetes API integration.
- Node/resource discovery.
- GPU/resource-aware model placement.
- Standard Deployment-based model workloads.
- ClusterIP model Services.
- Model start/stop/delete lifecycle.
- Model health and Kubernetes failure translation.
- Placement constraints using normal Kubernetes primitives.
- Model storage/cache strategy.
- Secure ServiceAccount/RBAC.
- Integration with Norn.
- Integration with Heimdall.
- CLI/API representation of Kubernetes model deployments.

---

## 39. Explicitly Out of Scope for V1

Do not require:

- a Kubernetes Operator,
- CRDs,
- distributed single-model inference,
- multi-cluster federation,
- custom Kubernetes scheduler,
- Internet/WAN cluster management,
- automatic GPU driver installation,
- automatic storage-system installation,
- autoscaling,
- scale-to-zero,
- distributed training,
- arbitrary third-party cluster provisioning,
- replacement for Kubernetes-native monitoring.

---

## 40. Suggested Delivery Phases

### Phase K0 — Research Spike

Prove that Core can:

1. run in Kubernetes,
2. authenticate to the in-cluster Kubernetes API,
3. list nodes,
4. create a model Deployment,
5. create a Service,
6. detect readiness,
7. send inference traffic,
8. delete the workload.

### Phase K1 — Kubernetes Backend

Implement:

- resource discovery,
- deployment lifecycle,
- health translation,
- Norn integration.

### Phase K2 — Helm

Create official Helm packaging with:

- ServiceAccount,
- RBAC,
- Deployment,
- Service,
- configuration.

### Phase K3 — Hardware Awareness

Improve:

- GPU discovery,
- VRAM estimation,
- node capability metadata,
- optional node agent if required.

### Phase K4 — Storage and Cache

Add:

- persistent model cache,
- cache-aware placement,
- configurable storage classes.

### Phase K5 — Production Hardening

Add:

- recovery testing,
- RBAC audit,
- upgrade behavior,
- multi-replica Core considerations,
- compatibility matrix.

### Phase K6 — Operator Evaluation

Only after K0-K5 are stable, decide whether CRDs/Operator improve the product enough to justify the additional complexity.

---

## 41. Acceptance Criteria

The initial Kubernetes feature is successful when:

1. Toskar Core can run as a Kubernetes workload.
2. Core can authenticate using a dedicated ServiceAccount.
3. Core does not require cluster-admin.
4. Core can discover Kubernetes nodes and relevant allocatable resources.
5. Norn can identify Kubernetes as a valid placement target.
6. Toskar can deploy a supported model runtime through the Kubernetes API.
7. Kubernetes schedules the model using generated resource requirements and placement constraints.
8. Toskar can detect when the model becomes ready.
9. Inference can be routed through the model's internal Service.
10. Toskar can stop and delete a deployed model cleanly.
11. OOMKilled, CrashLoopBackOff, FailedScheduling, and similar states are translated into useful Toskar errors.
12. Model workloads are not publicly exposed by default.
13. Secrets are stored using Kubernetes Secrets where required.
14. Helm can install Core and its RBAC/configuration into a clean supported cluster.
15. Existing non-Kubernetes Toskar execution continues to work unchanged.

---

## 42. Product Outcome

The feature succeeds when a Kubernetes user can install Toskar Core into a cluster and then manage AI models through the same Toskar concepts used everywhere else.

The user should be able to say:

> "Deploy this model to my cluster."

Norn should determine what the model requires, translate that into Kubernetes placement intent, let Kubernetes schedule it, and then expose the resulting model as a normal Toskar inference target.

The long-term result is a unified control plane:

```text
Toskar Core
      │
      ▼
Norn
├── local computer
├── paired computers
├── Kubernetes clusters
└── future Model Grids
```

Kubernetes should feel like a natural extension of Toskar's existing compute model, not a separate product.
