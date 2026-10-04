# Toskar Core — Expanded Tool Platform

## Feature Specification — V1+

### Status

**Type:** Feature / Platform Capability  
**Primary subsystem:** Gungnir  
**Supporting subsystems:** Norn, Heimdall, Bifrost  
**Primary goal:** Expand Toskar from a small set of built-in tools into a general-purpose tool platform that makes local AI feel as capable and convenient as a hosted AI service.

---

## 1. Goal

Toskar should support a broad set of user-facing capabilities without requiring users to understand which runtime, provider, computer, or backend implements them.

Users should be able to ask for tasks such as:

```text
Generate an image.
Transcribe this meeting.
Analyze this spreadsheet.
Create a PDF.
Read this response aloud.
Search my email.
Schedule a meeting.
Open this website and download my invoice.
Make a short video from this image.
```

Toskar should determine:

- which tool is needed,
- whether it is available,
- which provider can perform it,
- which Toskar node should execute it,
- what permissions are required,
- whether approval is needed,
- how progress and results are returned.

> **Users choose what they want done. Toskar figures out how and where to do it.**

---

## 2. Why This Matters

Toskar's core mission is:

> **Make local AI as easy as SaaS AI.**

A capable hosted AI service is not just a chat model. It can work with images, audio, files, code, spreadsheets, documents, email, calendars, websites, video, and external systems.

If local AI requires users to manually configure a separate application for each capability, the local experience remains harder than SaaS. The tool platform should hide that infrastructure complexity.

---

## 3. Existing Baseline

Existing/basic tool families include:

```text
web
files
shell
git
```

These should be represented through the same Gungnir abstraction as all future tools.

---

## 4. Initial 10 Additional Tool Families

| Priority | Tool family | Primary capability |
| --- | --- | --- |
| 1 | Image generation & editing | Generate, edit, upscale, transform images |
| 2 | Speech-to-text | Transcribe audio, meetings, voice notes, video |
| 3 | Code & data sandbox | Execute Python/JavaScript for calculations and analysis |
| 4 | Document creation | Create/edit PDF, DOCX, Markdown, presentations |
| 5 | Spreadsheet & data | Read/write XLSX/CSV, formulas, charts, transformations |
| 6 | Text-to-speech | Read responses aloud and generate narration/audio |
| 7 | Email & calendar | Search/read/draft/send mail and manage schedules |
| 8 | Maps & places | Local search, places, routing, travel/local planning |
| 9 | Browser / computer use | Navigate websites and complete browser workflows |
| 10 | Video generation | Generate/edit/extend short video content |

These are user-facing capability families. Internally, each may expose multiple individual tools.

---

# Part I — Tool Platform Architecture

## 5. Gungnir as the Tool Control Plane

Gungnir should become the source of truth for tool discovery, authorization, invocation, lifecycle, provider selection, and execution.

```text
Model / Agent
     │
     ▼
Gungnir Tool Registry
     │
     ├── schema validation
     ├── permission policy
     ├── provider selection
     ├── execution placement
     ├── timeout
     ├── cancellation
     ├── progress
     └── audit
             │
             ▼
         Tool Provider
```

Gungnir should not care whether a capability is implemented locally, on another Toskar node, by a plugin, by an external API, by a GPU service, or by a sandbox.

---

## 6. Capability-First Tool Names

Tools should be named for what they do, not for the implementation.

Preferred:

```text
image.generate
image.edit
speech.transcribe
speech.synthesize
code.execute
document.create
spreadsheet.analyze
email.search
calendar.create
browser.navigate
video.generate
```

Avoid implementation-specific model-facing contracts such as:

```text
comfyui.invoke
whisper.run
gmail.search
```

Providers implement capabilities:

```text
image.generate
   ├── ComfyUI provider
   ├── local Flux provider
   ├── Stable Diffusion provider
   └── hosted image provider
```

This allows provider changes without changing the model-facing tool contract.

---

## 7. Tool Definition

Every tool should advertise a common descriptor:

```text
id
name
description
version

input_schema
output_schema

permissions_required

execution:
  local
  remote
  either

requirements:
  cpu
  memory
  gpu
  accelerator
  runtime
  network
  filesystem
  credentials

supports:
  streaming
  progress
  cancellation

limits:
  max_runtime
  max_input_size
  max_output_size
```

Example:

```json
{
  "id": "image.generate",
  "name": "Generate Image",
  "version": 1,
  "permissions_required": ["image.generate"],
  "execution": "either",
  "requirements": {
    "gpu": true
  },
  "supports": {
    "progress": true,
    "cancellation": true
  }
}
```

---

## 8. Tool Registry API

Core should expose:

```text
GET /api/v1/tools
GET /api/v1/tools/{id}
```

Profiles should be able to declare capability policy:

```yaml
allowed_tools:
  - web.search
  - image.generate
  - speech.transcribe
  - code.execute
```

---

## 9. Tool Provider Interface

Providers should implement a stable internal interface:

```text
ToolProvider
  ListTools()
  DescribeTool()
  Invoke()
  Cancel()
  Health()
```

Possible future extensions:

```text
Install()
Update()
Capabilities()
EstimateRequirements()
EstimateCost()
```

Provider classes may include:

- local providers,
- remote Toskar providers,
- plugins/connectors,
- hosted API providers.

---

# Part II — Permissions and Safety

## 10. Tool Permission Model

Recommended user-facing permission levels:

### Level 1 — Low-risk local capability

Examples:

```text
calculator
image generation
speech transcription of user-provided audio
text-to-speech
```

May run automatically when enabled.

### Level 2 — Read external data

Examples:

```text
web search
read selected files
search email
read calendar
database SELECT
```

May be persistently granted per profile/integration.

### Level 3 — Change external state

Examples:

```text
send email
create calendar event
edit file
Git commit
submit web form
```

Should require explicit permission and may require per-action approval.

### Level 4 — High-impact execution

Examples:

```text
shell command
delete files
database write
deploy infrastructure
purchase
destructive remote action
```

Require explicit approval unless an advanced trusted policy has been deliberately configured.

---

## 11. Simple Permission UX

Normal users should see capability groups, not schemas:

```text
Assistant capabilities

Internet              On
Files                 On
Images                On
Audio                 On
Code & Data           On
Documents             On
Email                 Off
Calendar              Off
Browser Control       Off

Advanced >
```

Advanced configuration may expose individual tool IDs, providers, nodes, read/write boundaries, approval policy, and timeouts.

---

## 12. Unattended Tool Execution

Automations must never silently gain permissions beyond those explicitly granted to the automation/profile.

Example:

```text
Daily inbox summary

Allowed:
  email.search
  email.read

Not allowed:
  email.send
  email.delete
```

---

## 13. Tool Audit

Gungnir should record:

```text
tool.requested
tool.permission_required
tool.approved
tool.started
tool.progress
tool.completed
tool.failed
tool.cancelled
```

Useful fields:

```text
tool_id
provider
execution_node
profile
duration
status
error class
```

Sensitive payloads should not be logged by default.

---

# Part III — Distributed Tool Execution

## 14. Tools as Schedulable Workloads

Tools should be eligible for Norn placement just like models.

Example requirement:

```text
image.generate

GPU: required
VRAM: recommended 12 GB
runtime: diffusion
network: optional
```

Norn can choose the best node:

```text
MacBook
  24 GB unified memory
  running chat model

Linux Workstation
  RX 7900 XTX
  idle

Decision:
run image.generate on Linux Workstation
```

The user sees only the result.

---

## 15. One Toskar, Many Machines

```text
User
  │ "Generate an image."
  ▼
Chat model
  ▼
Gungnir
  ▼
Norn
  ├── laptop unsuitable/busy
  └── workstation GPU available
          ▼
    image provider runs
          ▼
       result
```

The Toskar network should feel like **one AI computer**.

Norn may consider:

- CPU/RAM,
- GPU/VRAM,
- accelerator type,
- runtime availability,
- cache state,
- current load,
- expected duration,
- network latency,
- provider health,
- user placement policy.

---

## 16. Provider Health

Heimdall should expose provider states:

```text
Healthy
Degraded
Unavailable
Installing
Updating
Failed
```

Multiple providers may implement the same capability, allowing fallback.

---

# Part IV — Initial Tool Families

## 17. Image Generation and Editing

Suggested tools:

```text
image.generate
image.edit
image.upscale
image.remove_background
```

Potential providers:

```text
ComfyUI
Flux
Stable Diffusion
future local image runtimes
configured hosted providers
```

If no image provider exists:

```text
Image generation isn't set up yet.

Recommended:
Flux Schnell
Best node: Gaming PC
Download: 12.4 GB

[Set up image generation]
```

The user should not need to choose backend details unless they want to.

---

## 18. Speech-to-Text

Suggested tool:

```text
speech.transcribe
```

Use cases:

- meetings,
- voice notes,
- interviews,
- uploaded videos,
- recordings.

Potential providers:

```text
whisper.cpp
faster-whisper
MLX Whisper
other local ASR runtime
```

---

## 19. Text-to-Speech

Suggested tool:

```text
speech.synthesize
```

Use cases:

- read responses aloud,
- narration,
- accessibility,
- generated audio files,
- mobile voice output.

Potential providers:

```text
Piper
Kokoro
other local TTS
configured hosted provider
```

---

## 20. Code and Data Sandbox

Suggested tool:

```text
code.execute
```

Initial runtimes:

```text
Python
JavaScript
```

Use cases:

- calculations,
- CSV analysis,
- statistics,
- plots,
- transformations,
- file generation,
- lightweight programming.

This must not simply call unrestricted shell execution.

Recommended sandbox controls:

```text
CPU limit
memory limit
execution timeout
filesystem boundary
network off by default
temporary working directory
maximum output size
```

---

## 21. Document Creation

Suggested capabilities:

```text
document.create
document.edit
pdf.create
pdf.extract
presentation.create
```

User-facing category:

> **Documents**

Example requests:

```text
Create a PDF report.
Turn this analysis into a Word document.
Make a presentation.
Edit this document.
Extract text from this PDF.
```

---

## 22. Spreadsheet and Data Tools

Suggested tools:

```text
spreadsheet.create
spreadsheet.read
spreadsheet.edit
spreadsheet.analyze
spreadsheet.chart
```

Formats:

```text
XLSX
CSV
TSV
```

Prefer structured workbook APIs rather than GUI automation.

---

## 23. Email

Suggested tools:

```text
email.search
email.read
email.draft
email.send
email.archive
```

Example policy:

```text
Search email     Allow
Read email       Allow
Draft email      Allow
Send email       Ask
Delete email     Deny
```

Provider architecture should allow Gmail and future providers without changing capability IDs.

---

## 24. Calendar

Suggested tools:

```text
calendar.search
calendar.availability
calendar.create
calendar.update
calendar.cancel
```

Read/write permissions should remain distinct.

---

## 25. Maps and Places

Suggested tools:

```text
places.search
places.details
maps.route
maps.distance
```

Use cases include businesses, restaurants, travel, routing, nearby places, and itinerary planning.

---

## 26. Browser / Computer Use

Suggested browser capabilities:

```text
browser.open
browser.navigate
browser.click
browser.type
browser.extract
browser.download
browser.upload
```

This should be implemented only after the permission framework is mature.

Browser execution should use an isolated session, with explicit confirmation for sensitive actions.

---

## 27. Video Generation

Suggested tools:

```text
video.generate
video.edit
video.extend
```

Video workloads require strong support for:

- GPU placement,
- VRAM estimation,
- long-running progress,
- disk management,
- cancellation.

Norn placement is especially valuable here.

---

# Part V — Capability Installation

## 28. Automatic Capability Discovery

Gungnir should know:

```text
what tools are installed
what providers are healthy
which nodes can execute them
what dependencies are missing
```

Example:

```text
image.generate
Available: No

Possible setup:
Gaming PC
RX 7900 XTX
Flux provider
```

---

## 29. Guided Installation

If a requested capability is unavailable:

```text
User:
Make me an image of a Viking tree.

Toskar:
Image generation isn't installed yet.

I found a compatible setup:

Provider: Flux Schnell
Computer: Gaming PC
Download: 12.4 GB

[Install]
```

Long-term flow:

```text
User intent
   ↓
detect missing capability
   ↓
find suitable node/provider
   ↓
ask permission
   ↓
install/configure
   ↓
complete original task
```

---

# Part VI — User Experience

## 30. Simple Mode

Normal users should interact with capability groups:

```text
Capabilities

Internet
Files
Images
Audio
Code & Data
Documents
Email
Calendar
Browser
Video
```

Advanced configuration should exist but should not be required for ordinary use.

---

## 31. Advanced Mode

Advanced users may inspect:

- tool IDs,
- providers,
- versions,
- permissions,
- execution node preferences,
- resource requirements,
- timeouts,
- approval rules,
- provider health.

---

## 32. Chat Presentation

Tool execution should be visible but compact:

```text
Generating image…
✓ Image created
```

```text
Analyzing spreadsheet…
✓ 1,482 rows analyzed
```

```text
Searching email…
✓ 6 relevant messages found
```

Technical execution details can be expandable.

---

## 33. Progress and Cancellation

Long-running tools should support structured progress where available:

```text
Installing video model…
42%

Generating video…
Frame 38 / 120
```

Long-running tools should also support cancellation where technically possible.

---

## 34. Results and Artifacts

The common result model should support:

```text
text
structured JSON
images
audio
video
files
tables
links
multiple artifacts
```

Conceptual result:

```text
ToolResult
  status
  text
  structured_data
  artifacts[]
  warnings[]
  metadata
```

---

# Part VII — Plugins and Ecosystem

## 35. Plugin-Based Expansion

Once the common contract is stable, external providers/plugins should be able to add capabilities.

Candidates:

```text
Home Assistant
GitHub
Linear
Jira
Slack
Microsoft Teams
Discord
Notion
Google Drive
Dropbox
Databases
Photos
CRM systems
```

Potential plugin metadata:

```text
publisher
version
permissions
network access
filesystem access
capabilities
signature/trust status
```

Plugins must not silently inherit Core privileges.

---

# Part VIII — Delivery Plan

## 36. Phase T0 — Common Tool Contract

Implement:

- common descriptor,
- input/output schemas,
- capability IDs,
- progress,
- cancellation,
- artifact results,
- normalized errors.

Success condition: existing web/files/shell/Git tools can use the same abstraction.

---

## 37. Phase T1 — Registry and Policy

Implement:

- Gungnir registry,
- `/api/v1/tools`,
- profile tool allowlists,
- permission levels,
- approval workflow,
- audit events.

---

## 38. Phase T2 — Distributed Execution

Integrate Gungnir with Norn:

- resource declarations,
- execution-node selection,
- remote invocation,
- cancellation,
- provider health,
- result return.

Success condition: a tool requested on one node can transparently execute on another.

---

## 39. Phase T3 — Image Tools

Implement:

```text
image.generate
image.edit
```

Then optionally:

```text
image.upscale
image.remove_background
```

Support at least one local provider and guided setup.

---

## 40. Phase T4 — Speech

Implement:

```text
speech.transcribe
speech.synthesize
```

Local providers first.

---

## 41. Phase T5 — Code and Data Sandbox

Implement:

```text
code.execute
```

with Python, JavaScript, ephemeral isolation, resource limits, and network disabled by default.

---

## 42. Phase T6 — Documents and Spreadsheets

Implement:

```text
document.create
document.edit
pdf.create
spreadsheet.create
spreadsheet.edit
spreadsheet.analyze
presentation.create
```

---

## 43. Phase T7 — Email, Calendar, and Places

Implement provider-backed:

```text
email.*
calendar.*
places.*
maps.*
```

with strict read/write permission separation.

---

## 44. Phase T8 — Browser / Computer Use

Implement an isolated browser environment with:

```text
navigate
click
type
extract
download
```

High-impact actions remain approval-gated.

---

## 45. Phase T9 — Video

Implement:

```text
video.generate
```

then:

```text
video.edit
video.extend
```

with Norn placement and progress reporting.

---

## 46. Phase T10 — External Tool Provider SDK

Publish a supported provider interface/SDK covering:

- capability registration,
- health,
- invocation,
- cancellation,
- permissions,
- schema validation,
- resource requirements.

---

# Part IX — Recommended Priority

## 47. Implementation Order

1. **Image generation/editing**
2. **Speech-to-text**
3. **Code/data sandbox**
4. **Document/PDF creation**
5. **Spreadsheet manipulation**
6. **Text-to-speech**
7. **Email/calendar**
8. **Maps/places**
9. **Browser/computer use**
10. **Video generation**

This sequence delivers substantial visible user value before taking on the highest-risk browser and video workloads.

---

# Part X — Security Requirements

## 48. General Rules

Every tool implementation must:

1. Validate model-produced arguments.
2. Treat model output as untrusted input.
3. Enforce permissions outside the model.
4. Restrict execution to declared capabilities.
5. Support timeout/cancellation where applicable.
6. Avoid logging secrets.
7. Keep credentials outside normal model context.
8. Prefer read-only access by default.
9. Require stronger approval for destructive/write actions.
10. Preserve auditability.

---

## 49. Credentials

Do not expose the following to normal model context:

```text
OAuth refresh tokens
API secrets
passwords
private keys
```

The model should receive the minimum information required to invoke a capability.

---

## 50. Shell Separation

Purpose-built tools must remain distinct from unrestricted shell execution.

For example:

```text
spreadsheet.analyze
```

should not simply turn into arbitrary model-generated shell commands.

Narrow tools provide better safety, reliability, portability, and auditability.

---

# Part XI — Example API/Data Model

## 51. Example Tool Descriptor

```json
{
  "id": "speech.transcribe",
  "name": "Transcribe Audio",
  "description": "Convert an audio or video recording to text.",
  "version": 1,
  "permissions_required": ["speech.transcribe"],
  "execution": "either",
  "requirements": {
    "network": false,
    "gpu": "optional"
  },
  "supports": {
    "streaming": false,
    "progress": true,
    "cancellation": true
  }
}
```

---

## 52. Tool Run Model

Potential persisted state:

```text
ToolRun
  id
  tool_id
  provider_id
  execution_node
  profile_id
  status
  started_at
  completed_at
  progress
  result_artifacts
  error
```

Status values:

```text
Pending
AwaitingApproval
Scheduled
Running
Completed
Failed
Cancelled
TimedOut
```

---

# Part XII — V1 Scope

## 53. Foundational Scope

The initial platform should include:

- common tool descriptor,
- capability-first IDs,
- Gungnir registry,
- profile tool policy,
- permission classes,
- schema validation,
- execution timeout,
- cancellation,
- progress events,
- artifact-based results,
- provider abstraction,
- provider health,
- Norn placement hooks,
- audit events,
- support for existing tools through the common model.

Initial expanded capabilities should target:

```text
image.generate
image.edit
speech.transcribe
speech.synthesize
code.execute
document.create
pdf.create
spreadsheet.create
spreadsheet.analyze
```

---

## 54. Explicitly Out of Scope for Initial Release

Do not require:

- every provider to be local,
- every provider to support every OS,
- full browser automation immediately,
- arbitrary desktop GUI control,
- autonomous purchases,
- unrestricted shell access through generic tools,
- mandatory cloud accounts,
- mandatory hosted APIs,
- a visual workflow builder,
- a full plugin marketplace.

---

# Part XIII — Acceptance Criteria

## 55. Platform Acceptance Criteria

The platform is ready when:

1. Tools are registered through a common Gungnir abstraction.
2. Existing and new tools are discoverable through the registry.
3. Tools expose structured input/output schemas.
4. Permission checks occur outside model reasoning.
5. Profiles can allow or deny capabilities.
6. Long-running tools can report status.
7. Tools can be cancelled where supported.
8. Results can include files/images/audio/video, not only text.
9. Providers can declare resource requirements.
10. Norn can choose a remote execution node.
11. Provider failure is visible through Heimdall.
12. Multiple providers can implement the same capability.
13. Provider implementation names are unnecessary in normal prompts.
14. High-impact actions require appropriate approval.
15. Tool executions generate audit events.
16. Missing capabilities can be detected cleanly.
17. Provider installation can be added without redesigning the registry.
18. Core remains useful without hosted providers.

---

## 56. Product Acceptance Criteria

The feature succeeds when a normal user can ask:

```text
Generate an image.
Transcribe this audio.
Analyze this spreadsheet.
Create a PDF.
Read this aloud.
```

and Toskar can complete those requests without requiring the user to understand:

- tool schemas,
- model APIs,
- runtime backends,
- GPU placement,
- provider names,
- remote execution,
- dependency installation details.

---

# Part XIV — Product Outcome

The long-term architecture should look like:

```text
                User Intent
                    │
                    ▼
                 Model
                    │
                    ▼
                 Gungnir
            capability request
                    │
          ┌─────────┴─────────┐
          ▼                   ▼
    Permission Policy        Norn
                               │
                     choose execution node
                               │
                               ▼
                         Tool Provider
                               │
                               ▼
                            Result
```

Models provide interchangeable intelligence.

Tools provide interchangeable capabilities.

Norn determines where workloads execute.

Gungnir determines what capabilities exist and whether they are allowed.

> **The Toskar network should behave like one capable AI system, regardless of which computer, runtime, provider, or plugin actually performs the work.**
