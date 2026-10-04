# Norse subsystem glossary

Toskar names its subsystems after figures from Norse myth. This glossary maps each name to what it does in Core, where it lives in the code, and the mythic figure.

| Name | In Toskar | Code path | Myth |
| --- | --- | --- | --- |
| **Huginn** | Classifies each request, picks the model (Auto, specialized AIs, fallback), and chooses which tools a turn is offered | `internal/huginn` | One of Odin's ravens; thought and observation |
| **Muninn** | Memories, per-chat and global Memory off, summaries of long conversations | `internal/muninn` | Odin's other raven; memory |
| **Mimir** | Connected knowledge: files, folders, uploads, databases, web APIs, scanned PDFs; keyword and meaning search | `internal/mimir`, `internal/ocr` | The wise being whose well holds knowledge |
| **Gjallarhorn** | Notification center and delivery | `internal/gjallarhorn` | Heimdall's horn that sounds across the realms |
| **Norn** | Places chat roles, automations, and training on a computer that can run them | `internal/scheduler` | The Norns who decide fate |
| **Bifrost** | Discovery, pairing, and computer-to-computer protocol (port 7332) | `internal/discovery`, `internal/nodes`, `internal/auth` | The rainbow bridge between realms |
| **Heimdall** | Health checks, diagnostics, and the event stream | `internal/events`, `internal/diagnostics`, `internal/models/health` | The watchman who guards the bridge |
| **Gungnir** | Tools page: built-in tools, connected services, and MCP tool sources | `internal/tools`, `web` tools routes | Odin's spear that never misses |
| **Brokkr** | Train Your Own AI: specialized AIs, trainers, export | `internal/training`, `internal/pyenv` | The dwarf smith who forged divine gifts |
| **Ymir** | Model catalog, downloads, and fit | `internal/models` | The primordial giant from whom the world was shaped |
| **Ratatoskr** | Chat | `internal/orchestrator`, `internal/app` | The squirrel that carries messages up and down Yggdrasil |

UI pages also use Norse names for related screens (see `web/src/lib/realms.ts`): for example Sleipnir for performance, Odin for profiles, Valgrind for API access, and Forseti for settings.

For package layout and the request path, see [Architecture](architecture.md).
