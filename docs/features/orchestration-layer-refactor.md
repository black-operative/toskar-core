# Toskar Core — Orchestration Layer Refactor

## Feature Specification

### Status

**Type:** Architecture / UX Refactor  
**Primary subsystem:** Huginn  
**Supporting subsystems:** Norn, Gungnir, Muninn, Heimdall  
**Primary goal:** Give normal users a high-quality, zero-configuration orchestration experience while preserving deep control for advanced users.

---

## 1. Product Goal

Toskar should not require a normal user to understand model routing, tool selection, agent roles, context budgets, memory retrieval, retries, fallback models, parallelism, verification passes, execution nodes, or runtime placement.

The default experience should be:

```text
Install Toskar
      ↓
Choose or install a model
      ↓
Ask for something
      ↓
Toskar figures out how to accomplish it
```

> **The default orchestrator should be invisible, adaptive, and good enough that most users never need to configure it.**

Advanced users should still be able to inspect and customize the orchestration policy in detail.

---

# Part I — Terminology

## 2. Profiles vs Orchestration

Profiles should be treated as saved assistant policies over the orchestration system, not as the orchestration system itself.

A Profile should evolve from:

```text
model
system prompt
tools
```

into:

```text
Profile
  ├── identity / instructions
  ├── model policy
  ├── tool policy
  ├── memory policy
  ├── orchestration policy
  └── execution policy
```

---

## 3. Subsystem Responsibilities

### Huginn

Owns request-level orchestration:

- task classification,
- planning,
- worker creation,
- multi-step execution,
- model-call coordination,
- verification,
- synthesis,
- run state.

### Norn

Owns scheduling and placement:

- execution-node selection,
- runtime selection,
- tool placement,
- CPU/RAM/GPU/VRAM fit,
- load-aware routing,
- placement preferences.

### Gungnir

Owns tools and capabilities:

- tool registry,
- tool selection support,
- permissions,
- provider selection,
- invocation,
- result handling.

### Muninn

Owns memory/context:

- durable memory retrieval,
- conversation summarization,
- relevant context retrieval,
- context-budget management,
- irrelevant-context suppression.

### Heimdall

Owns health/reliability signals:

- model/provider health,
- process failure detection,
- degraded node detection,
- runtime instability,
- fallback signals.

---

# Part II — Zero-Interaction Default

## 4. Default Orchestrator

Recommended conceptual default:

```text
Toskar Default

Model selection:       Auto
Tool selection:        Auto
Memory:                On
Context management:    Auto
Reasoning effort:      Auto
Parallelism:           Auto
Verification:          Auto
Fallbacks:             Auto
Node placement:        Auto
Retries:               Auto
```

The normal user should not need to see or edit this list.

---

## 5. Default Decision Flow

The orchestrator should choose the least-complex strategy likely to produce a good result.

```text
User request
    ↓
Task classification
    ↓
Can one normal model call answer this well?
    ├── Yes → execute directly
    └── No
         ↓
Does it need tools?
         ├── Yes → select relevant tools
         └── No
         ↓
Does it need planning/multiple steps?
         ├── Yes → plan
         └── No
         ↓
Would parallel work help?
         ├── Yes → create workers
         └── No
         ↓
Execute
         ↓
Verify if useful
         ↓
Final response
```

---

## 6. Escalate Only When Needed

### Simple request

```text
"What is a mutex?"
```

Expected:

```text
one model call
no planner
no worker team
no verifier
```

### Current-information request

```text
"What's the latest stable Go version?"
```

Expected:

```text
model + web tool
```

### Analysis request

```text
"Compare these three CSV files and summarize the differences."
```

Expected:

```text
file inspection
+ code/data tool
+ synthesis
```

### Complex research request

```text
"Research four approaches to distributed inference and produce an architecture document."
```

Expected:

```text
planner
+ parallel research workers
+ synthesis
+ verification
```

---

# Part III — Automatic Model Selection

## 7. Model Roles

Potential model roles:

```text
fast/default
reasoning
coding
vision
long-context
planner
worker
reviewer
```

The same model may satisfy multiple roles.

The user should not need a separate model installed for every role.

---

## 8. Selection Inputs

Huginn may consider:

- task type,
- installed models,
- model capabilities,
- context limits,
- model health,
- hardware fit,
- current node availability,
- expected speed,
- community model ratings,
- profile preferences,
- quality mode.

---

# Part IV — Automatic Tool Selection

## 9. Tool Routing

Examples:

```text
"Generate an image"
→ image.generate

"Analyze this spreadsheet"
→ spreadsheet.analyze

"Search my mail for the invoice"
→ email.search

"Refactor this repository"
→ files + git + code/shell policy
```

Do not expose every available tool to every model call when a smaller relevant set will work better.

---

## 10. Permissions Remain External

Automatic tool selection does not imply automatic authorization.

```text
Huginn:
"I need email.send"

Gungnir:
"This profile requires approval"

User:
[Approve] [Deny]
```

A model must never be able to grant itself broader permissions.

---

# Part V — Memory and Context

## 11. Context Assembly

Muninn should dynamically assemble context from:

```text
recent conversation
older conversation summary
relevant durable memories
relevant project context
relevant files
tool results
active task state
```

The system should avoid dumping all available context into every prompt.

---

# Part VI — Planning and Multi-Agent Execution

## 12. Planning

Create an explicit plan only when useful.

Example:

```text
"Find three NAS drives, compare price/TB, and make a spreadsheet."
```

Potential plan:

```text
1. Find candidates
2. Gather prices/specs
3. Normalize capacities
4. Calculate price/TB
5. Compare
6. Create spreadsheet
```

The plan may remain internal by default.

---

## 13. Worker Agents

For decomposable tasks:

```text
Coordinator
   ├── Research worker
   ├── Technical worker
   ├── Data worker
   └── Reviewer
```

Workers may use different models, tools, and nodes.

This should remain mostly invisible to normal users.

---

## 14. Parallelism

Use parallel execution when it materially reduces wall-clock time.

Example:

```text
Compare:
- Ollama
- llama.cpp
- MLX
- vLLM
```

Potential execution:

```text
4 research workers in parallel
       ↓
single synthesis pass
```

---

# Part VII — Verification

## 15. Automatic Verification

Verification may be used when:

- the task is complex,
- calculations are important,
- sources disagree,
- code was generated,
- a tool action may have failed,
- the model expresses uncertainty,
- a high-impact operation is requested.

Possible checks:

```text
fact/source consistency
calculation re-check
code/test validation
missing requirements
contradiction detection
tool execution success
```

---

## 16. Simple Quality Control

Normal users may optionally see:

```text
Response quality

Auto
Fast
Balanced
Thorough
```

Recommended default:

```text
Auto
```

This controls orchestration budget without exposing agent mechanics.

---

# Part VIII — Reliability and Fallback

## 17. Automatic Recovery

Example:

```text
Preferred model
    ↓
fails to start
    ↓
Heimdall marks unhealthy
    ↓
Norn selects alternative node/model
    ↓
retry
```

Potential fallback order:

```text
same model on another node
alternate quantization
alternate model
smaller compatible model
graceful error
```

Retries must be bounded.

---

# Part IX — Distributed Orchestration

## 18. Automatic Placement

Norn should decide where work runs.

Example:

```text
MacBook
  chat model

7900 XTX workstation
  image model

Linux server
  data processing
```

One user request may use multiple machines while presenting one assistant experience.

---

# Part X — Advanced Mode

## 19. Advanced Mode Philosophy

Advanced mode should expose **policy**, not force users to manipulate low-level internals.

Users should be able to override Auto behavior while retaining safe defaults.

---

## 20. Advanced Profile Sections

### Profile

```text
Name
Description
System instructions
Response style
```

### Models

```text
Primary model
Fast model
Reasoning model
Coding model
Vision model
Planner model
Worker model
Reviewer model
Fallback order
```

Every role may support `Auto`.

### Tools

```text
Allowed tools
Denied tools
Ask-before-use tools
Preferred providers
Network access
Filesystem access
Shell access
External account access
```

### Memory

```text
Conversation history
Persistent memory
Project memory
Cross-model memory
Context budget
Automatic summarization
```

### Orchestration

```text
Strategy:
  Auto
  Single model
  Planner + worker
  Team
  Custom

Reasoning effort:
  Auto
  Low
  Medium
  High

Planning:
  Off
  Auto
  Always

Verification:
  Off
  Auto
  Always

Parallelism:
  Off
  Auto
  On

Maximum workers:
  1–N

Maximum orchestration depth:
  Auto / advanced value

Maximum model calls:
  Auto / advanced value
```

### Execution

```text
Preferred nodes
Denied nodes
Prefer local node
Prefer fastest node
Allow remote Toskar nodes
Retry count
Fallback models
Fallback nodes
Tool timeout
Task timeout
```

---

# Part XI — Tab Naming

## 21. Recommendation

**Keep `Profile` as the object name, but rename the advanced tab to `Profiles & Orchestration`.**

Recommended navigation:

```text
Advanced
  └── Profiles & Orchestration
```

Inside:

```text
Profiles & Orchestration

[ Default ] [ Coding ] [ Research ] [ + New Profile ]

Profile
Models
Tools
Memory
Orchestration
Execution
```

### Why

- Existing users already understand Profiles.
- A profile remains a saved assistant configuration.
- Orchestration describes the new execution-policy depth.
- Renaming profiles themselves to "Orchestrators" would make a simple preset sound unnecessarily technical.
- Calling the entire area only "Orchestration" hides identity, models, tools, and memory.

---

## 22. Alternatives Considered

### Profiles

Good because it is simple and familiar, but it understates the expanded scope.

### Orchestration

Technically accurate, but too infrastructure-heavy and does not imply saved assistant presets.

### Assistants

Approachable, but can imply personality more than execution policy.

### Configurations

Broad, but generic.

### Profiles & Orchestration

Best balance of continuity and accuracy.

**Recommended.**

---

# Part XII — Default Profile

## 23. Intelligent System Default

The current boilerplate default should become an intelligent system profile:

```text
Toskar Default

Model policy:      Auto
Tool policy:       Auto within permissions
Memory:            On
Planning:          Auto
Parallelism:       Auto
Verification:      Auto
Fallback:          Auto
Placement:         Auto
```

The user should never need to edit it to get a good experience.

---

## 24. Default Profile Protection

Consider making the system default:

- always present,
- resettable,
- cloneable,
- not permanently deletable.

Example:

```text
Toskar Default
[Duplicate] [Reset to defaults]
```

---

# Part XIII — Refactor Plan

## 25. Phase O0 — Inventory Current Behavior

Document:

- current profile schema,
- hard-coded routing,
- model-selection assumptions,
- tool-selection assumptions,
- team-mode behavior,
- Norn integration,
- context construction.

---

## 26. Phase O1 — Introduce Orchestration Policy

Add a structured policy object:

```text
OrchestrationPolicy
  model_policy
  planning_policy
  worker_policy
  verification_policy
  tool_policy
  memory_policy
  placement_policy
  retry_policy
```

Default values should use `Auto` wherever practical.

---

## 27. Phase O2 — Default Orchestrator

Implement the no-interaction path:

- task classification,
- direct vs planned execution,
- relevant tool selection,
- context assembly,
- automatic placement,
- bounded fallback,
- basic verification.

Success condition:

> A new user can get a strong result without ever opening Profiles.

---

## 28. Phase O3 — Advanced UI

Rename the tab:

```text
Profiles
```

to:

```text
Profiles & Orchestration
```

Add sections:

```text
Profile
Models
Tools
Memory
Orchestration
Execution
```

Hide complexity when Advanced mode is off.

---

## 29. Phase O4 — Multi-Agent / Parallel Execution

Add:

- worker creation,
- parallel work,
- coordinator/synthesis,
- per-worker model/tool selection,
- bounded worker count.

Default:

```text
Auto
```

---

## 30. Phase O5 — Verification and Quality Modes

Add:

```text
Auto
Fast
Balanced
Thorough
```

Map those modes to orchestration budgets.

---

## 31. Phase O6 — Reliability and Recovery

Integrate Heimdall signals:

- unhealthy model avoidance,
- alternate-node retry,
- alternate-model fallback,
- bounded retries,
- advanced diagnostics.

---

## 32. Phase O7 — Orchestration Observability

Advanced users should be able to inspect execution:

```text
Run details

Planner:
Qwen Reasoning 14B

Workers:
3

Tools:
web.search
code.execute

Execution:
MacBook → planner
Linux workstation → 2 workers

Verification:
1 pass

Total model calls:
6
```

Normal users should see only concise progress unless details are expanded.

---

# Part XIV — UX Examples

## 33. Normal User

User:

```text
Compare the best three options and make me a spreadsheet.
```

Visible:

```text
Researching…
Comparing results…
Creating spreadsheet…
✓ Done
```

Invisible:

```text
task classification
planner
parallel research
tool use
calculation
spreadsheet generation
verification
```

No setup required.

---

## 34. Advanced User

```text
Profile: Deep Research

Primary model: Auto
Planner: Qwen Reasoning
Worker model: Qwen 14B
Reviewer: Auto

Workers: max 4
Parallelism: On
Planning: Always
Verification: Always

Tools:
Web            Allow
Files          Allow
Code           Allow
Shell          Deny

Memory:
Persistent     On
Project        On

Placement:
Allow all nodes
Prefer workstation
```

---

# Part XV — Data Model

## 35. Conceptual Profile

```text
Profile
  id
  name
  description
  instructions
  model_policy
  tool_policy
  memory_policy
  orchestration_policy
  execution_policy
```

## 36. Conceptual Orchestration Policy

```text
OrchestrationPolicy
  strategy
  reasoning_effort
  planning
  verification
  parallelism
  max_workers
  max_depth
  max_model_calls
  retry_policy
  fallback_policy
```

Prefer enums such as:

```text
Auto
Off
On
Always
```

over requiring numeric tuning.

---

# Part XVI — Migration

## 37. Existing Profile Compatibility

Existing profiles should migrate without losing behavior.

Migration should:

- preserve profile names,
- preserve system prompts/instructions,
- preserve selected models,
- preserve tool policy,
- map legacy team settings into orchestration policy,
- assign `Auto` to new settings unless existing behavior requires an explicit value.

---

# Part XVII — Acceptance Criteria

## 38. Default Experience

The refactor is successful when:

1. A new user can use Toskar without configuring a profile.
2. The default profile performs automatic orchestration.
3. Simple requests remain simple and fast.
4. Complex requests can escalate to planning, tools, workers, or verification.
5. Model selection can be automatic.
6. Tool selection can be automatic within granted permissions.
7. Muninn supplies relevant context automatically.
8. Norn selects execution placement automatically.
9. Heimdall failures can trigger bounded recovery.
10. Users are not required to understand agent concepts.

---

## 39. Advanced Experience

Advanced mode is successful when:

1. Users can create multiple profiles.
2. Profiles contain model, tool, memory, orchestration, and execution policy.
3. Users can override Auto behavior.
4. Users can configure worker count and parallelism.
5. Users can configure planning and verification.
6. Users can configure model roles and fallbacks.
7. Users can configure tool permissions.
8. Users can configure memory policy.
9. Users can configure node placement.
10. Users can inspect orchestration run details.

---

# Part XVIII — Product Outcome

The desired end state is:

```text
Normal user
   ↓
Ask Toskar
   ↓
Automatic orchestration
   ↓
Good result
```

while advanced users can access:

```text
Profiles & Orchestration
   ↓
Models
Tools
Memory
Planning
Workers
Verification
Placement
Fallbacks
Retries
Execution policy
```

The orchestration system should become more capable without making the normal product feel more complicated.

> **Complexity belongs inside Toskar unless the user explicitly asks to control it.**
