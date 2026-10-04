# Architecture

Yggdrasil Core is one daemon process, `toskar`, plus the web UI it serves. Desktop and mobile applications are separate clients. They are not built from this repository.

```text
client (web UI, desktop, mobile, curl, an app over MCP or the OpenAI API)
        |
        |  HTTP  /api/v1, /v1, /mcp     default 127.0.0.1:7331
        v
  toskar
        |
        +-- request pipeline: classify, route, assemble context, plan, run tools, check
        |     Huginn (routing) · Muninn (memory) · Mimir (knowledge) · artifacts (files)
        +-- tools: built-in, connected services, MCP tool sources
        +-- automations, notifications (Gjallarhorn), training (specialized AIs)
        +-- sharing the computer, run records, what left this computer, caches
        +-- Norn placement
        |
        +-- runtime adapters
        |     +-- llamacpp (local llama-server: chat, embedding, reranking)
        |     +-- external-openai (configured remote server)
        +-- Python environments: training (MLX, PyTorch) and text recognition
        |
        +-- Bifrost  :7332
              discovery, pairing, paired-computer calls, remote training
```

Local state is a SQLite database, model files, runtime binaries, Python environments, logs, and a secrets directory under the OS data path. See [Configuration](configuration.md#data-directory) and [Privacy](privacy.md).

## Packages

The Norse names are what the app and the code call the subsystems.

| Name | Role | Package |
| --- | --- | --- |
| Yggdrasil Core | Daemon, API, web UI | `cmd/daemon`, `internal/app`, `internal/api`, `web/` |
| Huginn | Classifies each request, picks the model (Auto, specialized AIs, fallback), and chooses which tools a turn is offered | `internal/huginn` |
| Muninn | Memories, per-chat and global Memory off, summaries of long conversations | `internal/muninn` |
| Mimir | Connected knowledge: files, folders, uploads, databases, web APIs, scanned PDFs; keyword search plus meaning search with an embedding model | `internal/mimir`, `internal/ocr` |
| Gjallarhorn | Notification center and delivery | `internal/gjallarhorn` |
| Norn | Places chat roles, automations, and training on a computer | `internal/scheduler` |
| Bifrost | Discovery, pairing, the computer-to-computer protocol | `internal/discovery`, `internal/nodes`, `internal/auth` |
| Heimdall | Health checks, diagnostics, the event stream | `internal/events`, `internal/diagnostics`, `internal/models/health` |
| Brokkr | Train Your Own AI: specialized AIs, trainers, export | `internal/training`, `internal/pyenv` |
| Ymir | Model catalog, downloads, fit | `internal/models` |
| Ratatoskr | Chat | `internal/orchestrator`, `internal/app` |

Other packages:

| Package | Role |
| --- | --- |
| `internal/tools` | Tool registry, built-in tools (internet, files, shell, Git, `files.create`), policies |
| `internal/connectors` | GitHub and Home Assistant, with credentials kept out of model context |
| `internal/mcp` | MCP tool sources, and Yggdrasil's own MCP server at `/mcp` |
| `internal/artifacts` | Files attached to chats and files the assistant made |
| `internal/automations` | Scheduled prompts |
| `internal/share` | Who gets the computer when several kinds of work want it |
| `internal/runlog` | A trace of each run |
| `internal/egress`, `internal/retention` | What left this computer, and removing old run records |
| `internal/cache` | Caches, each with a declared policy |
| `internal/inventory` | The capability inventory: what Yggdrasil can do right now |
| `internal/personal` | Personalization of answers |
| `internal/structured` | Checking and repairing JSON from models (tool arguments, `response_format`, automation results) |
| `internal/contextusage` | Splitting a prompt into the parts the context gauge shows |
| `internal/turnopts` | What an API request asks of a turn, within its key's permissions |

## Request path

A chat message, an API request, and an automation run take the same path.

1. **Classify.** Huginn decides what kind of request it is: a quick question, current information, coding, a detailed question, a task on this computer, or a question about what Yggdrasil can do. A capability question is answered from the inventory.
2. **Route.** With Auto, Huginn picks the model: a deployed specialized AI when the message is about what it was trained for, otherwise the largest suitable model that fits. It sets the effort (Fast, Balanced, or Thorough).
3. **Assemble context.** Instructions come first: the profile, the specialized AI's instructions, personalization, and memories that fit the question. Retrieved content is data, not instructions: attached files, connected knowledge, and web look-ups arrive as labelled reference material in the user turn. Older messages are summarized when a conversation passes half the model's window.
4. **Place.** Norn picks the computer. Work that uses local-only memories or knowledge stays on this computer.
5. **Run.** A request with several parts is planned and worked through in parts, side by side when it can be. Each turn is offered only the tools it needs. Policies (Allow, Ask, Deny) are enforced by the registry, not by the model.
6. **Check.** Figures in the answer are checked against the sources and the arithmetic is recomputed, with a correction pass when something is wrong. A claim that something was changed when no tool changed anything is called out.
7. **Record.** The answer keeps its sources, steps, and files. The run is traced, what left the computer is recorded, and events stream to clients on `GET /api/v1/events`.

If the model fails before it answers, the turn runs once more on another model. Stop ends every model call, tool, plan step, and paired computer working on the turn.

## Sharing the computer

Chat comes first, then automations, then background knowledge indexing, then benchmarks, then training (`internal/share`). Lower-priority work waits for higher-priority work instead of competing with it for memory. A chat that arrives during training is still answered.

## Bifrost

Bifrost is the internal HTTP server on port 7332.

- mDNS service type `_localai._tcp` on domain `local.`, with `node_id`, `name`, `version`, `pairing`, and `api_port` in its TXT record
- optional static peers (`TOSKAR_STATIC_PEERS` or `config.json`) when mDNS is not available, including Docker
- pairing offer and approval before a peer is trusted
- certificate-backed bearer tokens on protected routes such as remote chat, model control, and remote training

Discovery is on by default. When it is on and the internal bind address is still loopback, startup rebinds Bifrost to `0.0.0.0` so peers on the LAN can connect. Pairing routes on that port are reachable without a token until a peer is trusted. Protected routes reject unsigned calls. See [Clustering](clustering.md).

## Norn

Norn is a deterministic placer. Given the computers the daemon knows about, which models they have, and a role, it picks a computer and records a `scheduler.placement` event. It does not split one model across computers. Cross-machine work is placement of whole roles: a plan's workers (each part has its own worker slot), the planner and reviewer, automations, and training jobs.

## Strategies

Every profile runs on the request pipeline above (`internal/orchestrator/builtin/simple`). A profile's strategy decides how much of it a request uses:

| Strategy | Behavior |
| --- | --- |
| Auto (default) | Quick questions get one model; requests with several parts are planned and checked, as the effort allows |
| Single model | One model answers, without a plan |
| Planner + workers | Requests with several parts are always worked through in parts |
| Team | A planner splits each request that is not a quick question, workers do the parts, the answering model writes the answer with tools, memory, and knowledge, and a reviewer checks it |

Profiles assign models to roles: primary, fast, coding, planner, worker, and reviewer. Each worker part has its own slot (`worker:1`, `worker:2`, …), so Norn can spread the parts over paired computers, and parts on different computers are written at the same time. Earlier versions had a separate Team orchestrator that ran a coordinator, a worker, and a reviewer without tools; profiles that used it are migrated to the Team strategy. The orchestration controls (effort, planning, workers, verification, tool calls, memory, context share, fallback order, time limit) tune any strategy. See [API](api.md#profiles-and-orchestration).

## Specialized AIs

`internal/training` builds a specialized AI from a base model, a LoRA adapter trained on the user's examples, system instructions, and Mimir knowledge sources. Trainers implement `training.Trainer`. The MLX trainer runs on Apple Silicon and the PEFT trainer on NVIDIA GPUs with CUDA, each in a Python environment the daemon manages under `runtimes/python` (`internal/pyenv`), and both export the adapter as a GGUF LoRA. Each computer runs one training job at a time. Norn can place a job on a paired computer: the examples go over Bifrost (`/internal/v1/training/`), and the adapter comes back to the computer that owns the AI, which evaluates and serves it. A sandboxed App Store build cannot run a downloaded Python, so it uses environments bundled with the app or trains on a paired computer.

llama-server loads every deployed adapter for a base model at scale 0. Each request names the adapter to apply, or none for the base model, so one process serves the base model and each specialized AI built on it. A `sai:<slug>` model id in chat or `/v1/chat/completions` adds the AI's instructions and knowledge and applies its adapter. A revision can also be exported as one standalone GGUF. See [features/train-your-own-ai.md](features/train-your-own-ai.md).

## Runtimes

Runtime adapters implement `pkg/pluginapi.Runtime`: detect, install, start, stop, and health. The process that actually generates tokens is outside the daemon (`llama-server`, or an HTTP server you already run). Embedding and reranker models run in their own `llama-server` processes for knowledge search. See [runtimes.md](runtimes.md).

## Heimdall

The event bus publishes structured events for chat, tasks, models, tools, computers, placement, automations, notifications, and training (see [API](api.md#events)). `GET /api/v1/events` is a server-sent stream of that bus. `GET /api/v1/diagnostics` builds a zip that omits secrets. A model health monitor stops a model that stops responding. There is no telemetry pipeline.

## What is not in this process

- Yggdrasil Desktop and Yggdrasil Mobile
- splitting a single model across machines
- TLS for remote API access
- code signing of release binaries
