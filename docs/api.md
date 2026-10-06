# API

The daemon listens on `http://127.0.0.1:7331` unless `config.json` or `TOSKAR_API_HOST` / `TOSKAR_API_PORT` say otherwise. This page lists every route and records behavior that matters when you call the server. [api/openapi.yaml](../api/openapi.yaml) describes every route below in OpenAPI 3.0. A test fails when a route is added to the daemon without it. [CLI](cli.md) and [Configuration](configuration.md) cover the command line and settings.

## Authentication

Loopback binds (`127.0.0.1`, `::1`, `localhost`) do not require an API key. Any other API host, including `0.0.0.0` and `::`, requires `Authorization: Bearer YOUR_API_KEY` on every `/api/v1/*` and `/v1/*` request from another machine. A request that arrives over loopback (`127.0.0.1` or `::1`) still needs no key, so apps on this computer keep working when network access is on. A request carrying `Forwarded`, `X-Forwarded-For`, or `X-Real-IP` came through a proxy and is treated as coming from the network. The daemon refuses to listen on a non-loopback address until at least one API key exists. `GET /about` and `GET /source` stay open so a network user can obtain the corresponding source.

The web UI calls this setting local network access. Turning it on stores `api_host` as `0.0.0.0`, turns the key check on, and moves the API to that address at once, with no restart. Turning it off moves it back to `127.0.0.1` and closes connections from other devices; a request from another device after that is refused with `403` `LAN_ACCESS_OFF`. If the new address can't be bound, the setting stays as it was and the change returns an error. The daemon also rejects a settings change that enables that bind when no key exists. The computer's discovery record (Bonjour) carries `api_lan=true` or `false`, so an app that finds it can tell an API it can reach from one that answers only on that computer.

Create a key from the web UI or `POST /api/v1/api-keys`. The response includes the secret once. The database stores a bcrypt hash and a prefix. The plaintext key is not written to disk. Revoke with `DELETE /api/v1/api-keys/{id}` and rotate with `POST /api/v1/api-keys/{id}/rotate`. Do not put the key in a URL or query string. Those requests are rejected.

### Connecting a phone

A phone gets its own key with a 6-digit code instead of a copied secret:

1. **The computer shows a code.** Its own UI calls `POST /api/v1/devices/pairing`, which returns `code`, `expires_at` (10 minutes on), `address` (where a phone reaches this computer, such as `192.168.1.20:7331`), and `reachable` (false while the API answers only on this computer). With `{"enable_lan": true}` it first turns on local network access, without the key that setting otherwise needs, since pairing makes the phone's key; if no phone or key ever uses it, the next start turns it off again instead of refusing to start. A new code replaces the last one. `GET /api/v1/devices/pairing` returns the same without the code, plus `state` (`waiting`, `connected`, `expired`, or `cancelled`) and `device`, the key a phone connected with. `DELETE` stops showing the code.
2. **The phone sends it.** `POST /api/v1/devices/pair` with `{"code": "123456", "device_name": "Sam's iPhone"}` needs no key. It answers `201` with `api_key`, shown once, and `key`, its record, named after the phone, with `kind` `device`.

Rules for the exchange:
- **Local network only:** the exchange answers only from this computer's network (private, link-local, and 100.64/10 addresses). A request through a proxy (`Forwarded`, `X-Forwarded-For`, `X-Real-IP`) is refused with `PAIRING_NOT_LOCAL`.
- **Single use:** a code works once.
- **Wrong codes:** five wrong codes cancel it (`PAIRING_WRONG_CODE`, then `PAIRING_NOT_STARTED`).
- **Too many tries:** an address gets ten tries in ten minutes (`PAIRING_THROTTLED`, `429`).
- **Expiry:** an expired code returns `PAIRING_EXPIRED` (`410`).

Only the code's hash is kept, in memory, so restarting the daemon ends it.

A phone's key reaches only what the phone uses:
- chat and Stop;
- conversations and their messages, including renaming and deleting them;
- live events;
- answering a tool's question;
- the chat's files;
- read-only lists: health, version, settings, profiles, models, running models, and computers.

Anything else returns `403` `DEVICE_NOT_ALLOWED`. It appears in `GET /api-keys` with `kind` `device`, keeps that kind when rotated, and is revoked like any other key. Over plain HTTP the code and the key can be read by others on the same network, as any key can.

`TOSKAR_API_KEY`, when set, is hashed at startup if that secret is not already valid. The Docker image binds `0.0.0.0` and will not start until that variable is set or a key is already in the data directory. The cluster compose file sets a local test key for that reason.

A bearer token on plain HTTP does not encrypt traffic. It stops anonymous use of a trusted LAN. TLS or mTLS for remote access is not implemented.

### Browsers and websites

A website open in a browser on this computer can send requests to `127.0.0.1`, so the daemon checks two headers before anything else, on every route:

- **`Origin`**, when a browser sends one, must be the daemon's own address (its web UI), the desktop app's webview (`wails://wails`, `http://wails.localhost`), or a loopback address on the API port, such as `http://localhost:7331`. The allowed origin is echoed in `Access-Control-Allow-Origin`; `*` is never sent. Other origins get `403 ORIGIN_NOT_ALLOWED`.
- **`Host`** must name this computer: `localhost`, a `.localhost` name, or a loopback IP. With local network access on, any IP is accepted, and so is a hostname on a request from another device. A hostname on a request over loopback gets `403 HOST_NOT_ALLOWED`, which stops DNS rebinding.

A request with a valid API key passes both checks, and a preflight that announces an `Authorization` header is answered for any origin. Clients that are not browsers, such as `toskarctl` and `curl`, send no `Origin` and are unaffected. A reverse proxy on this computer must keep a loopback `Host` or send a key.

Bifrost, on port 7332, is a separate server. Its protected routes require a paired-node token. Pairing routes answer before trust exists; they check signatures and limit attempts. See [clustering.md](clustering.md).

## Corresponding source

`GET /about` and `GET /source` return the same JSON and do not require an API key:

```json
{
  "name": "Toskar Core",
  "version": "1.4.0",
  "commit": "abc1234",
  "license": "AGPL-3.0-or-later",
  "source": "https://github.com/yeixio/toskar-core/tree/v1.4.0"
}
```

`GET /api/v1/version` includes `license` and `source` as well. The Settings page links to that source URL.

A release build, which sets `version.Version` from the tag, points `source` at `https://github.com/yeixio/toskar-core/tree/v<version>`. A development build with a known commit points at `.../tree/<commit>`. A build with neither points at the repository itself.

If you distribute or operate a modified Toskar Core over a network, set the source URL so users can obtain the corresponding source for your modified version:

```text
-X github.com/yeixio/toskar-core/internal/version.SourceURL=<url-of-your-corresponding-source>
```

The same text is printed by `toskar -version` and by `toskarctl version` or `toskarctl about`.

## Route reference

Control-plane routes are under `/api/v1`. The OpenAI-compatible routes are under `/v1`, and Toskar's MCP server is `/mcp` (see [MCP](mcp.md)). `GET /about` and `GET /source` are at the root.

### System

| Method | Path | Purpose |
| --- | --- | --- |
| GET | `/health` | The process is up, and `acceleration`: where the loaded models run (`gpu`, `partial`, `cpu`, `cpu_expected`, or `idle`) |
| GET | `/version` | Version, commit, license, corresponding source, and client `contract` |
| GET | `/hardware` | This computer's CPU, memory, disk, and accelerators |
| GET, PATCH | `/settings` | Read or change settings (see [Configuration](configuration.md#settings)). `PUT` is accepted as `PATCH`. |
| POST | `/settings/reset` | Clear application state; `delete_models=true` also removes model files |
| GET, POST | `/diagnostics` | Build the diagnostic bundle, which omits secrets. `?include_conversations=true` adds chats. |
| GET | `/diagnostics/runtime` | The daemon's memory (`heap_bytes`, `sys_bytes`) and goroutines `now` and in `samples` taken every `interval_seconds` (five minutes) for the last day, oldest first, since `started_at`. The Diagnostics page charts it. |
| GET | `/logs` | Log files. `GET /logs/{name}` returns one; `?tail_bytes=` limits it to the end. |
| GET | `/events` | Server-sent event stream (see [Events](#events)) |
| GET | `/capabilities` | The capability inventory. `?ask=` returns the abilities a question is about. `GET /capabilities/models/{id}` says which computers can run a model. |
| GET | `/caches` | Every cache with its policy and counts. `POST /caches/{name}/clear` clears one. |
| GET | `/performance` | Recent generation runs with timings. `?sort=`, `?order=`, `?limit=` |

### Chat

| Method | Path | Purpose |
| --- | --- | --- |
| POST | `/chat` | Send a message through a profile. Takes `conversation_id`, `message`, `profile_id`, `model_id` (`auto` for Auto), `effort`, `attachments` (artifact ids), `execution` (where it runs), `time_zone` (the person's IANA time zone, such as `America/Juneau`; the app sends the browser's), and `stream`. Every turn is told the current date and time in that zone, or this computer's when it is empty or unknown; an automation's run uses its schedule's zone. Progress arrives on `/events`. |
| POST | `/chat/stop` | Stop a conversation's running turn: `{"conversation_id": "..."}` returns `{"stopped": true}` when one was running |
| GET, POST | `/conversations` | List or create chats |
| PATCH, DELETE | `/conversations/{id}` | Change a chat's `title`, `profile_id`, `model_id`, or `memory_off`, or delete it with its files |
| GET | `/conversations/{id}/messages` | A chat's messages, with each answer's `meta` |
| GET | `/conversations/{id}/artifacts` | Files attached to or made in a chat |
| POST | `/artifacts` | Upload a file to attach: `name` plus `text`, or `content_base64` for binary files |
| GET, POST, DELETE | `/images/setup` | Image generation: what is installed and the models on offer, start setting up a model (`model_id`) in the background, or stop the setup. Poll `GET` for its progress. |
| DELETE | `/images/models/{id}` | Delete an installed image model |
| GET, POST, DELETE | `/video/setup` | Video generation, the same way: status, start setting up a model (`model_id`), or stop |
| DELETE | `/video/models/{id}` | Delete an installed video model |
| GET, DELETE | `/artifacts/{id}` | A file's details, or delete it. `GET /artifacts/{id}/content` returns the bytes as a download. |
| GET, POST | `/memory` | Memories (Muninn). GET returns `memories` and `categories`. |
| PATCH, DELETE | `/memory/{id}` | Change a memory's `content`, `category`, `enabled`, or `local_only`, or delete it |
| GET, PUT | `/personalization` | How answers should look: `length`, `tone`, `format`, `units`, `about_me`, `instructions` |
| GET | `/runs` | Run traces, newest first. `?conversation_id=`, `?limit=`. `GET /runs/{id}` returns one. |
| GET, POST | `/tasks` | Orchestration tasks. `GET /tasks/{id}` returns one with its steps. |

### Models and runtimes

| Method | Path | Purpose |
| --- | --- | --- |
| GET | `/models` | The catalog and installed models, with `support_role` and fit |
| GET | `/models/fit` | How well each catalog model fits each computer |
| GET | `/models/recommend` | A recommended setup. `?purpose=` (`general`, `coding`, `research`) |
| GET | `/models/browse` | Search Hugging Face GGUF models. `?q=`, `?limit=` |
| POST | `/models/{id}/install` | Install a catalog model |
| POST | `/models/install-from-url` | Install a GGUF from a URL |
| DELETE | `/models/{id}` | Remove an installed model |
| POST | `/models/{id}/start`, `/models/{id}/stop` | Load or unload a model |
| GET | `/diagnostics/gpu` | What this computer still needs for Toskar to use its GPU: each missing piece (`vulkan_loader_missing`, `vulkan_driver_missing`, `render_access_denied`, `nvidia_driver_missing`, `windows_driver_missing`, `cpu_build`) with a fix `command` written for this computer, or a `url` |
| GET | `/performance/live` | Live CPU, memory, and GPU figures for this and paired computers: now, the last hour, and per-minute averages for the day (see [GPU acceleration](#gpu-acceleration)) |
| GET | `/models/running` | Loaded models, with `mode` (`embedding` or `reranking` for supporting models), `speed_tok_per_sec` from their latest replies, and `acceleration`: where each runs (see [GPU acceleration](#gpu-acceleration)) |
| GET, PUT, DELETE | `/models/{id}/rating` | This person's 1–5 star rating of a model, and exactly what sharing it would send. See [Community ratings](#community-ratings). |
| POST | `/models/{id}/rating/dismiss` | Stop asking for a rating of a model |
| GET | `/ratings/community` | Everyone's ratings of the models here, from hardware like this computer's, overall, and by language |
| GET | `/runtimes` | Runtimes and their detection |
| GET, PUT | `/external-server` | The external OpenAI-compatible server: `base_url`, `has_key`, and its `models`; PUT takes `base_url`, `api_key`, and `clear_key` |
| POST | `/runtimes/{id}/install` | Install a runtime (`llamacpp`) |

### Profiles and tools

| Method | Path | Purpose |
| --- | --- | --- |
| GET, POST | `/profiles` | List or create profiles |
| GET, PATCH, DELETE | `/profiles/{id}` | Read, change, or delete a profile, including its roles, tools, `knowledge_sources`, and `orchestration` |
| POST | `/profiles/{id}/reset` | Put a built-in profile back to how Toskar ships it. Other profiles are refused with 400 |
| GET | `/tools` | Every tool's descriptor (level, input schema, outputs, requirements, time limit, provider), with whether it is enabled, its `health`, and the profiles that allow it |
| GET | `/tools/{id}` | One tool's descriptor |
| GET | `/tools/runs` | Audited tool calls, newest first; `?tool_id=`, `?conversation_id=`, `?limit=` |
| POST | `/tools/{id}/enabled` | Turn a tool on or off everywhere |
| POST | `/tools/{id}/test` | Run a tool with `args` and return its result |
| POST | `/tools/decide` | Answer an Ask approval: `request_id`, `allow`, and `allow_session` |
| GET | `/tools/activity` | Recent tool calls, kept in memory for this process |
| GET | `/tools/providers` | Each computer's providers for image and speech tools, which can run on any paired computer: state (healthy, installing, failed, unavailable), what runs it, whether a GPU does the work, and the `languages` it works in (`auto_detect` when it tells the language itself) |
| GET | `/connectors` | Connected services and their status |
| PUT, DELETE | `/connectors/{id}` | Connect a service (`{"values": {...}}`), or disconnect it. `POST /connectors/{id}/check` tests the stored credential. |
| GET, POST | `/mcp/servers` | MCP tool sources, or add one |
| GET, PATCH, PUT, DELETE | `/mcp/servers/{id}` | Read, change, replace, or remove a source |
| POST | `/mcp/servers/{id}/check`, `/sign-in`, `/sign-out` | Test a source, or start or end its browser sign-in |
| GET | `/mcp/servers/{id}/logs`, `/prompts`, `/resources` | A source's log, prompts, and resources. `POST /mcp/servers/{id}/prompts/{name}` fills in a prompt. |
| GET | `/mcp/gallery` | Known MCP servers to add |
| GET | `/mcp/import` | MCP servers set up in other apps on this computer, with secrets hidden |
| POST | `/mcp/parse` | Read pasted MCP configuration into sources, and what each still needs |
| GET | `/mcp/share` | What other apps need to use Toskar over MCP: `url`, `command`, `args`, `needs_key` |

### Knowledge

| Method | Path | Purpose |
| --- | --- | --- |
| GET, POST | `/knowledge/sources` | Connected knowledge (Mimir), or add a source: `kind` `path`, `text`, `database`, or `api` |
| GET, PATCH, DELETE | `/knowledge/sources/{id}` | Read, change (`name`, `text`, `local_only`, `remote`), or remove a source |
| POST | `/knowledge/sources/{id}/refresh` | Reindex now |
| GET | `/knowledge/sources/{id}/content` | The copy kept for a pasted or uploaded source |
| POST | `/knowledge/search` | Passages that match a question |

Uploads send `text`, or `content_base64` for binary files such as `.xlsx` and `.pdf`. The same field works for `/training/classify` and `/training/ais/{id}/materials`.

### Training

| Method | Path | Purpose |
| --- | --- | --- |
| GET | `/training/backends` | Trainers, and whether this computer can use each, with the reason |
| GET | `/training/base-models` | Trainable base models ranked for a job. `?goal=` |
| POST | `/training/classify` | Recommend Training, Knowledge, or Both for material, before it is added |
| GET, POST | `/training/ais` | Specialized AIs, or create one |
| GET, PATCH, DELETE | `/training/ais/{id}` | Read, change, or delete a specialized AI and its adapters and exports |
| POST | `/training/ais/{id}/materials` | Add material. `DELETE /training/ais/{id}/materials/{mid}` removes it. |
| POST | `/training/ais/{id}/conversations` | Add saved chats as material |
| GET, POST | `/training/ais/{id}/examples` | Examples with their flags, or add one. `PATCH`/`DELETE /training/ais/{id}/examples/{eid}` |
| GET | `/training/ais/{id}/plan` | What trains, what stays connected, and the training fit per computer |
| POST | `/training/ais/{id}/train` | Start a training job; an optional `{"node_id": "..."}` picks the computer |
| GET | `/training/jobs` | Training jobs. `GET /training/jobs/{id}` returns one; `POST /training/jobs/{id}/cancel` cancels it. |
| PUT | `/training/ais/{id}/test-prompts` | Replace the test set |
| POST | `/training/ais/{id}/revisions/{n}/evaluate` | Compare base and specialized answers |
| POST | `/training/ais/{id}/revisions/{n}/deploy` | Deploy an evaluated revision. `POST /training/ais/{id}/undeploy` |
| GET, POST, DELETE | `/training/ais/{id}/revisions/{n}/export` | Merge a revision into one standalone GGUF, see its status, or delete the file. `GET /training/ais/{id}/revisions/{n}/export/file` downloads it. |
| GET | `/training/deployed` | Deployed specialized AIs as models |
| POST | `/training/example` | Set up the example AI from the sample material, or return it. `GET /training/samples` returns the sample files. |

### Automations and notifications

| Method | Path | Purpose |
| --- | --- | --- |
| GET, POST | `/automations` | Scheduled prompts, or create one |
| POST | `/automations/preview` | Run an automation once without saving it |
| GET, PATCH, DELETE | `/automations/{id}` | An automation with its history, change it, or delete it |
| POST | `/automations/{id}/run`, `/pause`, `/resume` | Run now, pause, or resume |
| GET | `/automations/{id}/runs` | Older runs, a page at a time (`before`, `limit`) |
| GET | `/notifications` | The notification center: `{"notifications": [...], "unread": n}`. `?unread=1` lists unread ones; `?category=` lists one category. |
| GET | `/notifications/{id}` | One notification with each channel's delivery |
| GET, POST | `/notifications/destinations` | Email and webhook destinations; creating a webhook returns its signing `secret` once |
| GET, PATCH, DELETE | `/notifications/destinations/{id}` | Read, change, or remove a destination. A blank `password` keeps the stored one |
| POST | `/notifications/destinations/{id}/test` | Send a test notification now: `{"ok": true}`, or `ok` false with `error` and `permanent` |
| POST | `/notifications/destinations/{id}/rotate-secret` | A new webhook signing secret; the old one stops working |
| GET, PUT | `/notifications/quiet-hours` | Quiet hours: `enabled`, `start`, `end` (HH:MM), `time_zone`, `allow` (`errors` or `nothing`) |
| POST | `/notifications/read` | Mark notifications read: `{"ids": [...]}`; no ids marks all |
| POST | `/notifications/{id}/dismiss` | Dismiss one |

### Privacy

| Method | Path | Purpose |
| --- | --- | --- |
| GET | `/egress` | What left this computer, newest first. `?conversation_id=` narrows to one chat. |
| GET, PUT | `/privacy` | `{"retention_days": n, "last_30_days": {...}}`; PUT sets `retention_days` |
| POST | `/privacy/delete-runs` | Remove run records now, and return how many |

### Computers

| Method | Path | Purpose |
| --- | --- | --- |
| GET | `/nodes` | This computer and the others Toskar knows |
| POST | `/nodes/refresh` | Look for computers again |
| POST | `/nodes/pair` | Ask a discovered computer to pair |
| GET | `/nodes/pairing/pending` | Pairing requests waiting for approval on this computer |
| POST | `/nodes/{id}/pair/approve` | Approve a pairing request |
| POST | `/nodes/pair/claim` | Pair with a code shown on the other computer: `node_id` and `code` |
| POST | `/nodes/pairing/offer` | Receive a pairing offer from another computer |
| GET | `/nodes/pairing/outbound/{code}` | The status of a pairing this computer started |
| POST | `/nodes/{id}/revoke` | Remove a paired computer |
| POST, GET | `/join-tokens` | Make a one-time join token and the command to use it (`{"ttl_minutes": 15}`), or list recent ones |
| DELETE | `/join-tokens/{id}` | Revoke an unused join token |
| GET | `/network` | This computer's network, Bifrost address, fingerprint, and paired computers |
| POST | `/network/join` | Join the network of the computer that made a token: `server`, `token`, `fingerprint` ([Clustering](clustering.md#joining-with-one-command)) |
| POST | `/network/leave` | Leave the network and forget paired computers |

### API keys and benchmarks

| Method | Path | Purpose |
| --- | --- | --- |
| GET, POST | `/api-keys` | List key metadata, or create a key (the secret is returned once) |
| DELETE | `/api-keys/{id}` | Revoke a key |
| POST | `/api-keys/{id}/rotate` | Replace a key's secret; its permissions are kept |
| PUT | `/api-keys/{id}/permissions` | Set what requests with the key may use |
| POST, GET, DELETE | `/devices/pairing` | Show a code a phone connects with, see whether one has, or stop ([Connecting a phone](#connecting-a-phone)) |
| POST | `/devices/pair` | A phone exchanges the code for its own key; no key needed, local network only |
| GET, POST | `/benchmarks` | Benchmark runs, or start one |
| GET | `/benchmarks/workloads` | The workloads a benchmark can run |
| GET | `/benchmarks/{id}` | One benchmark. `POST /benchmarks/{id}/cancel` stops it. |

## Chat turns

Assistant messages from `GET /conversations/{id}/messages` carry `meta`: the `sources` an answer drew on (`web`, `knowledge`, or `file`, with title, URL or source name, and a snippet) and plain-language `steps` describing what was done. The same `meta` is on the `chat.complete` event.

`POST /chat` takes `attachments`, a list of artifact ids. Chat reads documents, spreadsheets (`.csv`, `.tsv`, `.xlsx`), PDFs with a text layer, JSON, HTML, and code files. Images are not read yet. A scanned PDF attached to a chat is refused with a pointer to the Knowledge page, which reads scanned pages once with text recognition, instead of on every turn. An attached file reaches the model as data in the user turn, the whole file when it fits and otherwise the parts that best match the question, and files attached or produced earlier in the chat add the passages that match later questions. The user message's `meta.files` lists its attachments.

The `files.create` tool, allowed by default in the built-in profiles, saves a file the user can download: a document, JSON, a spreadsheet (`.xlsx` is built from CSV text), or code. It writes only to Toskar's file store. When a message asks for a file and the profile allows `files.create` without asking, Toskar has the model write only the contents and saves the file itself, with a `chat.making_file` event. The answer's `meta.files` lists produced files. File content is served as an attachment with a sandboxing `Content-Security-Policy`, so HTML never runs on the API's origin.

When a model under 4B parameters answers from attached files or connected knowledge, `meta.notice` says it can mix up numbers and details and suggests a larger model. Auto treats a question about the user's files or knowledge as one that needs a careful answer, so it prefers a larger model that fits.

### Auto and specialized AIs

A chat whose `model_id` is `auto` gets an installed model chosen for each message. The message is classified as a quick question, current information, coding, a detailed question, or a task on this computer. Auto then picks the largest suitable model that fits this computer's memory, keeps a quick question on a model that is already loaded, requires tool calling for current information and tasks, and skips a model that failed in the last 10 minutes. The `chat.model_routed` event carries `model_id`, `model_name`, and a plain-language `reason`, and the reason is the first of the answer's `steps`.

The answer's language is one of Auto's inputs (multilingual spec §15–16). It is the language the answer is written in (see Answer language), and at each of Auto's steps a model whose `languages` rate it higher comes before a larger one: excellent, then good, then fair or unrated, then a model whose levels leave the language out or call it limited. A model without language levels is never left out. A loaded model keeps a quick question only when nothing that fits writes the language better. When the model Auto picks is weak in the language and nothing installed that fits does better, the answer is still written in the language and its `notice` says so, in the App language.

Community ratings and load are Auto's other inputs (spec §13):
- **Ratings:** with community ratings on, a model whose signal (see Community ratings) is at least 0.5 better comes before a larger one; closer than that, size decides. Language still comes first, and ratings never make a model that doesn't fit, or can't use tools when the request needs them, the choice. Only the summary already kept is read. When ratings changed the pick, the reason says so, such as "…, rated higher on computers like yours".
- **Load:** a model already answering something on this computer gives way to a loaded, idle model that writes the language as well, is at least half its size, and isn't rated clearly worse ("…, since Qwen 2.5 14B was busy"). A quick question takes an idle loaded model before a busy one.
- **Busy computers:** placement counts the turns this computer has streaming on each computer, and an idle computer with the model comes before a busy one. Work other computers send to a paired computer isn't counted.

Before choosing a model, Auto checks the deployed specialized AIs. A chat goes to one when the message names it, or when at least two of the message's words, and at least 40% of them, are words the AI was trained on. Those words come from its name, its goal, and words that recur in its training questions. Requests that need current information, a task on this computer, or code never go to a specialized AI, because it answers without tools. An AI whose adapter file is missing on this computer, or whose base model is not installed, is skipped. Asking for it by id then fails with an explanation. When Auto routes to a specialized AI, `chat.model_routed` carries its `sai:` id and name.

A model can have a `support_role`: `embedding`, `reranker`, or `classifier`. These models serve Toskar instead of chatting. The role comes from the catalog, or, for models installed by URL or from Hugging Face, from the model's name, purpose, or tags. Auto, fallback, and the default model never pick one. A chat that asks for one by id fails with an explanation.

A catalog model's `languages` say how well it writes each language it has a level for (multilingual spec §13–14, §31), best first: `language` (a BCP 47 tag), `level` (`limited`, `fair`, `good`, or `excellent`), `confidence` (`low`, `medium`, or `high`), and `sources` (`model_card`, `maintainer`, `benchmark`, `provider`, `community`, or `local_evaluation`). Levels are coarse on purpose. A language a model has no level for is unknown and left out. The levels in the catalog come from what each model card says it supports, judged by size for languages it lists but doesn't lead with; models installed by URL or from Hugging Face have none yet.

If the model fails before it shows or changes anything, the turn runs once more: on the same model on another computer when one has it, else on another installed quantization of the same model (smaller when it ran out of memory, otherwise the largest that fits), else on another installed model. After running out of memory, a larger version of the same model is never the fallback. Quantizations are matched by the model's Hugging Face repository. `chat.model_routed` then has `fallback: true`. The answer's steps say what happened, and `meta.notice` warns when the model that answered is noticeably smaller. A model whose `llama-server` exits while loading fails at once instead of after the 120-second readiness timeout.

### Effort, plans, checks, and Stop

`POST /chat` takes `effort`: `auto` (the default), `fast`, `balanced`, or `thorough`. Effort sets a budget, not a number of calls the client sees:

- **Fast** answers in one go. It doesn't plan, a look-up uses search results without reading pages, figures are checked but not sent back for correction, and a turn may make 3 tool calls.
- **Balanced** plans requests with several parts, reads one page per look-up, corrects figures once, and allows 10 tool calls.
- **Thorough** reads two pages per look-up, corrects figures twice, allows 16 tool calls, and lets Auto pick the largest model that fits.
- **Auto** uses Fast for a quick question, Thorough for a request that needs a detailed answer, has several parts, or uses the user's files or knowledge, and Balanced otherwise.

The `chat.effort` event reports the effort used. A chosen effort is listed in the answer's `steps`.

A request with several parts is worked through in parts. "Compare A, B and C…" and "research A vs B" become one part per subject, looked up on the web side by side when web search is allowed without asking. "Do X, then Y, then Z" becomes parts in order, each seeing the notes before it. The final answer is written from the parts' notes, or a requested file is made from them. `plan.created` carries `steps` and `parallel`, and `plan.step` carries `index`, `step`, and `status` (`running`, `done`, or `failed`).

Before an answer is shown, its arithmetic is recomputed. When the turn used reference material or tool results, each figure must appear in, or follow from, the lines about the same subject. Dates and years are skipped. This check needs no model. Only an answer with issues is sent back to the model once, with the issues named (`chat.verifying`), and the revision is kept if it fixes some. `verify.done` reports `issues`, `fixed`, and `remaining`. Remaining figures become the answer's `meta.notice`. An answer that describes tool calls instead of answering is asked for again without tools.

Two more checks follow:
- **Code:** fenced code blocks tagged `go`, `json`, `python`, or `bash`/`sh` are parsed, never run. Go and JSON are parsed in Toskar; Python with Python's own parser (`ast.parse`) and shell with `bash -n`, when installed. Untagged blocks, other languages, and sketches with `...` are skipped. Code that doesn't parse goes back to the model with the errors named, once per correction the effort allows, and the revision is kept when it has fewer errors. `verify.code` reports `blocks`, `issues`, `fixed`, and `remaining`, and errors left become the answer's `meta.notice`.
- **Links:** web addresses in the answer, outside code, are checked against the reference material, tool results, and the conversation; no request is made. A link no source gave is one the model wrote from memory and often doesn't open, so it goes back to the model with the links named, once per correction the effort allows, and the revision is kept when it has fewer. A site's home page, example addresses, and addresses on this computer or network are not flagged. `verify.links` reports `links`, `issues`, `fixed`, and `remaining`, and links left become the answer's `meta.notice`.
- **Contradictions:** at Thorough effort, an answer of 400 characters or more is checked by the reviewer for statements that contradict each other or the reference material. When some are found, the answer is rewritten once with them named, and kept when the contradictions drop. `verify.consistency` reports `found`, `fixed`, and `remaining`. A check whose reply can't be read counts as finding none, so it never holds an answer back.

Stopping a turn, with `POST /chat/stop` or by closing the stream, stops every model call, tool call, plan step, pending approval, and paired computer working on it. The part already written is saved as the answer, with `meta.notice` "Stopped before the answer was finished." A turn stopped in the middle of a plan keeps the notes of the parts that finished. A turn stopped before anything was written keeps a short note with the sources found so far. The `chat.stopped` event carries `conversation_id` and `kept`. A new message in the same chat stops a turn still running there.

### Context, memory, and look-ups

Window budgets are counted in tokens with the answering model's tokenizer (llama-server's `/tokenize`) while that model runs on this computer, and estimated otherwise: about a token per Chinese, Japanese, or Korean character, half a token per punctuation mark or symbol, and four characters a token for other text. A paired computer returns its llama-server's prompt count, and an external server its `usage.prompt_tokens` (requested with `stream_options.include_usage`, and dropped for servers that reject it), so the total is exact there too and only the breakdown is estimated. `context.estimated` is `true` only when the total itself is an estimate. `context.instructions` covers the whole system prompt except the tool list, including personalization, memories, and the user guide. `context.prompt_tokens` is the whole prompt, including tokens llama-server reused from its cache, and tokens no section accounts for, such as the chat template, count as `instructions`. When a conversation's history passes half of the model's window, a summary of the older messages is written after the reply and replaces them in later turns. The messages stay saved. The `chat.summarized` event follows. The `chat.complete` payload's `context.summarized_messages` counts the messages the summary covers, and `pipeline_ms` is the time from the request to the first model call. `context.limit` is the window the answering model's llama-server reports (`/props` `n_ctx`) when it runs on this computer, and the catalog's window otherwise. `context.memory_bytes`, when the model ran on this computer, is about how much memory that window reserves: its KV cache, from the model file's layer and attention-head counts at f16, an upper estimate for models with sliding-window attention.

Memory is on unless the setting `memory_enabled` is `false` or a conversation has `memory_off: true` (set with `PATCH /conversations/{id}`). A message that starts "Remember that…", "Forget…", or asks "What do you remember?" is answered without a model; the reply is saved as usual and the events `memory.saved` or `memory.deleted` follow. Other turns add the memories that fit the question to the instructions and list them as `memory` sources. A secret such as a password, key, or card number is not saved. A memory fits a question when it is about the person (identity and preferences), when it shares words with the question, or, with an embedding model installed, when it is close in meaning, whatever language either is written in (multilingual spec §18): a memory saved as "Mi proyecto usa Go" comes with "What language does my project use?". Each memory's `language` is the language it is written in, detected on this computer; its vector is made by the embedding model when first needed and again after an edit or a change of model.

When a question needs current information and the profile allows `internet.search` without asking, Toskar searches the web and reads the best page before the model answers. The `chat.lookup` event carries the `query`. The results reach the model as data, and the model answers without web tools for that turn.

### Tools

Each turn is offered only the tools it needs (spec §16). Huginn picks tool groups from the kind of request and cues in the message: web search for questions, files for a file name or folder, shell for "run" or "install", Git for "commit" or "branch". It then limits them to what the profile allows. A call to a tool that was not offered is refused, and the model is told which tools it has, so it cannot widen its own tools. The profile's Allow, Ask, and Deny still decide what runs. Tool ids have capability aliases, and `web.search`, `web.open`, `files.read`, `files.write`, `files.search`, and `shell.run` reach the built-in tools. A short answer that only writes out a call, such as `files.search {"query": "x"}`, is taken as the call when the tool was offered. Each call has a time limit (web 45 s, files 30 s, Git 90 s, shell 2 min), and `tool.failed` carries a `kind`: `timeout`, `cancelled`, `denied`, `not_offered`, `invalid`, or `failed`.

See [Tools](tools.md) for policies and the tool loop.

## Knowledge

A profile's `knowledge_sources` lists Mimir source ids. Chat searches them on every turn and adds the matching passages to the user turn as labelled reference material, never to the system prompt, and the model is told not to follow instructions inside them (§58). A specialized AI's knowledge sources are searched when it answers, and an API request can add sources with `yggdrasil.knowledge_sources`.

A knowledge source can read current data from a database or a web API instead of a file. `POST /knowledge/sources` takes `kind` `database` with `remote` `{"driver": "sqlite", "database": "~/shop.db", "query": "SELECT …"}`, or `driver` `postgres` or `mysql` with `connection_string`. It takes `kind` `api` with `remote` `{"url": "https://…", "items": "data.products", "headers": {"Authorization": "Bearer …"}}`. `refresh_minutes` (1 to 10080, default 60) sets how old the data may get.

- **Queries:** a query must be one `SELECT`, `WITH`, or `VALUES` statement. It runs in a read-only transaction, and a SQLite file is opened read-only, so Toskar never changes the database. Each row becomes a passage labelled with its column names, and a query that returns more than 200,000 rows is refused.
- **APIs:** a JSON response that is a list of objects, or holds exactly one such list (or the list at `items`), becomes one passage per object. Other JSON, CSV, HTML, and text are read like files. Requests time out after 30 seconds, and responses are limited to 20 MB.
- **Credentials:** connection strings and header values are kept in the secrets directory, not the database. They are never returned: a source reports `remote` with the driver, query, URL, `items`, `header_names`, and `refresh_minutes`. `PATCH /knowledge/sources/{id}` with `remote` changes the settings. A blank `connection_string` or header value keeps the stored one, and the credentials are deleted with the source.
- **Refreshing:** when a search uses a source whose data is older than `refresh_minutes`, Mimir fetches it again in the background, and that search uses the data already indexed. `POST /knowledge/sources/{id}/refresh` fetches at once. If a fetch fails, the source is `failed` with the reason (credentials removed), and search keeps using the last data that was fetched.

Knowledge sources read scanned PDFs with text recognition (OCR). Pages with a text layer are read as before, and only the pages without one are recognized, so a scanned appendix in a digital document is read too. Recognition runs RapidOCR in a private Python environment that is installed under `runtimes/python/envs/ocr` the first time a scanned page needs it. The install is about 110 MB to download and 290 MB on disk, and the recognition models come with it, so nothing else is downloaded. A page takes about a second on an M5 Pro. Recognized text is remembered by file content, so a folder source does not recognize its scanned PDFs again when another file changes. On the Train page, a scanned PDF has no examples to train on, so it is recommended as knowledge. Recognition reads Chinese and English text with the models RapidOCR ships; accented letters of other languages may be misread, and other scripts are not recognized.

Knowledge search matches words (BM25). When an embedding model is installed (one with `support_role` `embedding`, such as `nomic-embed-text-v1.5-q8` in the catalog), it also matches meaning, so "What warranty do you offer?" finds a passage about a five-year guarantee. The embedding model runs in its own `llama-server` started with `--embedding`, loaded when first needed and unloaded by the idle sweeper like any model; it appears in `GET /models/running` with `mode` `embedding`. Passages are embedded in the background after a source is added or reindexed, and the vectors are kept in the daemon database. A reindex keeps the vectors of passages whose text did not change, and installing a different embedding model embeds every passage again. A source with more than 20,000 passages is searched by words only. Search never waits for embedding: it uses the vectors that exist. With more than one embedding model installed, the one rated in the most languages is used, so `bge-m3-q8` (BGE-M3, multilingual) is preferred to `nomic-embed-text-v1.5-q8` (English); switching embeds every passage again.

A source's `language` is the language most of its passages are written in, and `languages` lists every language found with how many passages are in it (multilingual spec §19). Both are detected on this computer when the source is indexed. With a multilingual embedding model, a question finds passages by meaning in any language, such as a Spanish question finding an English policy, and the answer is written in the answer language, not the document's.

Word and meaning matches are combined with reciprocal rank fusion. A passage found only by meaning must be similar enough to the question, and close to the best match. When words found something, it must also be at least as similar to the question as the best word match, so a question that names one product does not bring in every similar row. When a reranker model is installed (`support_role` `reranker`), it reorders the top 16 passages. Each hit from `POST /knowledge/search` has `match`: `keyword`, `semantic`, or `both`. `score` orders hits within one search only: BM25 for word-only search, the fused rank when meaning is used, and the reranker's score for passages it ordered. A knowledge source reports `embedded_count` and `embedding_model`. With no embedding model installed, if it cannot start, or while training is using the computer, search uses words only.

## Training and export

See [Train Your Own AI](features/train-your-own-ai.md) for the workflow.

A trained revision can be exported as one GGUF file that llama.cpp, LM Studio, Ollama, and other GGUF tools load without the adapter. `POST /training/ais/{id}/revisions/{n}/export` starts the merge and returns `202` with the status, or `200` when the file already exists. The merge runs `llama-export-lora` from the installed llama.cpp in the background, and it takes seconds for a small model and a minute or two for a large one. Tensors the adapter changed are written as F16, and the rest keep the base model's quantization, so the file is a little larger than the base model. The status has `state` (`none`, `exporting`, `ready`, or `failed`), `filename` (such as `tire-bot-r2.gguf`), `size_bytes` (the estimate while exporting), `error`, and the AI's `instructions`, which are not part of the file and are needed as the system prompt elsewhere. Exporting is refused when there is not enough free disk space, when the revision's adapter or base model is not on this computer, and in builds that ship only `llama-server`. `training.export.completed` and `training.export.failed` carry `ai_id`, `name`, and `revision`, and post a notification. Exports are deleted with their AI.

## Automations and notifications

An automation's `notification.mode` is `condition`, `change`, `always`, `failure` (only failed runs), or `none`. An automation runs on the same stack as chat: `model_id` `auto` picks a model for each run, and memories and connected knowledge are used the same way. Its `response_language` is the language results are written in: `account` (the default) follows the assistant language setting, `app` the App language, `auto` the language of the request, or a language tag such as `de`. A language the request asks for, such as "answer in English", always wins. Tools follow the unattended policy, because nobody is there to approve them. Tools listed in the automation's `tools` were approved when it was saved, and they run even if they change things. With no `tools`, only read-only tools the profile allows without asking can run. A tool the profile denies never runs. When a run reaches a tool that was not approved, the tool is skipped and the run continues. The run's `automation.completed` event lists the tool in `skipped`, and an `approval` notification says which tools to approve.

**Runs.**
- **Running together:** due automations run two at a time, so a slow one doesn't hold up the rest. An automation that is still running isn't started again.
- **Time limit:** each run has 20 minutes. One that takes longer is stopped, fails with `AUTOMATION_TIMEOUT`, and isn't retried.
- **Run now:** `POST /automations/{id}/run` answers `202` with the run as soon as it has started (`status` `running`). The run goes on without the request, so closing the page doesn't stop it. Its progress and result arrive as `automation.started`, then `automation.completed` or `automation.failed`, and in the automation's `history`. Running one that is already running answers `409` `AUTOMATION_RUNNING`. `toskarctl automations run` waits for the result.
- **Retries:** a failed run is retried by its error code when it has one, such as `CONNECTION_LOST` or `COMPUTER_OFFLINE`, and never for `OUT_OF_MEMORY`, `NO_MODEL_INSTALLED`, or `AUTOMATION_TIMEOUT`. The error's text decides only for errors without a known code.
- **History:** `GET /automations/{id}` includes the newest 20 runs in `history`, and `history_more` when there are older ones. `GET /automations/{id}/runs?before=<run id>&limit=` pages them, newest first: `{runs, more}`, up to 100 runs a page.
- **Preview:** `POST /automations/preview` runs the draft in its `response_language`, with "today" in its schedule's time zone, as the saved automation will.
- **Pausing:** an automation that fails three times in a row, for any reason, is paused, and its notification says why in the App language. Running out of memory pauses it after two.

Notifications come from Gjallarhorn. Each one is stored first and then delivered to its channels. A notification has `category` (`automation`, `approval`, `model`, `training`, `health`, or `system`), `severity` (`info`, `success`, `warning`, or `error`), `title`, `body`, and a `link` back to its source in the app, such as `/automations?id=…`. Each channel's attempt is recorded in `deliveries`. A delivery is `delivered`, `failed`, or `suppressed`; for example, desktop notices are suppressed when `notify_task_finish` is off. A repeat with the same source within 10 minutes is counted on the first notification (`repeat_count`) and marks it unread again. The `notification.created` event carries `id`, `category`, `severity`, `title`, `body`, `message`, and `link`. Finished and failed automations post to the desktop too.

Health notifications (category `health`) come on changes only. A paired computer that goes offline gets a warning, unless `notify_peer_offline` is off, and one when it is back online. A computer that was never announced offline is not announced back. A model that crashes twice within 30 minutes gets an error saying it keeps crashing, at most once an hour, with a hint when it is likely running out of memory; a single crash is handled by answering on another model. These stay out of desktop notices and go to the bell and to destinations that take `health`.

**Destinations.** Notifications can also go to email, through your own SMTP server, to phones and computers through ntfy, and to webhooks. Each destination has `categories` (empty: all) and `min_severity` (`info`, `success`, `warning`, or `error`), and receives every notification that matches, from any part of Toskar.
- **Email:** `host`, `port` (587, or 465 with `tls`), `username`, `from`, `to` (up to 20 addresses), and `tls`: `starttls` (the default), `tls`, or `none`. `none` is allowed only for a server on this computer, so a password is never sent unencrypted over the network. The password is stored in `secrets/notify-<id>`, never returned, and kept when a change leaves it blank.
- **Webhooks:** must use `https`, except for an address on this computer or the local network (`localhost`, private IP addresses, `.local`, `.lan`, `.home.arpa`). Redirects are not followed. Each request is a JSON `POST`: `version` (1), `notification_id`, `created_at`, `category`, `severity`, `title`, `body`, `language` (the BCP 47 tag `title` and `body` are written in, the App language), `message` (when there is one), `link`, `repeat_count` (when more than 1), and `source` (`type`, `id`). The `Toskar-Signature` header is `t=<unix seconds>,v1=<hex HMAC-SHA256 of "<t>.<body>">`, keyed with the destination's secret. `Toskar-Notification-Id` repeats the id, so a receiver can ignore a delivery it already handled. Both are also sent under their names from before the rename, `Yggdrasil-Signature` and `Yggdrasil-Notification-Id`, with the same values.

- **Push (ntfy):** `server` (default `https://ntfy.sh`, or your own; https except on the local network), `topic` (letters, digits, `-`, `_`, up to 64), `content`, and `open_url`. `content` is `full` (title and text) or `private` (only "You have a new Toskar notification"); private is the default on ntfy.sh, where anyone who knows a topic can read it. Severity sets ntfy's priority: errors 4 (high), warnings 3, everything else 2. With `open_url` (this Toskar's address), tapping the notification opens its link there. An access token for a protected topic is sent as `password`, stored in `secrets/notify-<id>`, and sent as a bearer token. ntfy has apps for Android and iPhone and works in a browser; no Yeix-hosted service is involved.

**Language.** A notification's `message` is its title and body as catalog keys with the values they need (`{"title": {"key": "notifications:notices.modelReady"}, "body": [{"key": "notifications:notices.modelReadyBody", "params": {"model": "Qwen 2.5 7B"}}]}`, from `i18n/locales/<language>/notifications.json`), so each app shows it in its own App language; a piece with `text` instead of `key`, such as an automation's name or what a model wrote, is shown as it is. `title` and `body` stay in English. Desktop notices, email, push, and webhooks are written in the App language (`ui_locale`) when they are sent; with the system language, they are in English.

**Delivery.** Email, push, and webhook deliveries go out in the background, so a slow server never holds up an automation. A delivery is `pending` until it is sent, then `delivered`, `failed`, or `cancelled` (its destination was removed). A failed delivery is retried after 1, 5, and 30 minutes, then given up, without running the work that made the notification again. Failures that retrying cannot fix are not retried, and their `error` says what to change: a refused sign-in or ntfy access token, an unknown recipient, an untrusted certificate, a server without STARTTLS, a webhook answering 4xx other than 408 or 429, or one that redirects. Every email, push, and webhook delivery is recorded in What left this computer as kind `notification`.

**Quiet hours.** Between `start` and `end` in `time_zone`, deliveries outside the app are `held` until quiet hours end. That covers desktop notices, email, push, and webhooks. With `allow` `errors` (the default), errors still go out; `nothing` holds everything. Held notifications are in the notification center at once.

**Digests.** A destination with `digest` (`{"at": "18:00", "time_zone": "America/Juneau"}`) gets one message a day instead of each notice: deliveries wait as `digest` until that time, then go out together, titled "N notifications" and listing each notice's title and the first line of its body (up to 30, then a count). Errors still go out at once. A digest due in quiet hours waits for them to end. A failed digest is retried like any delivery. Setting `digest` to `{"at": ""}` sends each notice again.

## Sharing the computer

Chat, automations, knowledge indexing, benchmarks, and training share this computer in that order of priority. A chat or API request, including a request from a paired computer, never waits. An automation run waits while a chat is running, and for 20 seconds after one, so it does not load a model between someone's messages. A benchmark also waits for automations, and it waits again before each model, because loading a model unloads the others. Background embedding of knowledge passages waits for chat and automations before each small batch, and does not start while training runs. Training waits for all of them before it unloads models to free memory. While work waits, `work.waiting` carries `class`, `label`, and `reason` ("Waiting for your chat to finish"), and the benchmark's progress or the training job's detail shows the reason. A chat that arrives during training is still answered: on a paired computer that has the model when there is one, and then its steps say `This computer is training, so <computer> answered`. Otherwise it is answered here, and its steps say `Training "…" is using this computer (about N minutes left)`. Placement avoids a training computer for any role unless the profile prefers this computer or pins the role, and paired computers say in their health answer when they are training, so work avoids them too. `GET /nodes` marks a training computer with `training: true`. If its model runs out of memory, the error says so, with the estimate and what to do. Out-of-memory failures are not retried. An automation that runs out of memory twice in a row is paused, and its notification explains why; one that fails three times in a row for any other reason is paused too.

## Connected services

Content from a connected service, such as an email or a calendar event, stays in the language it was written in unless you ask for a translation; Toskar's own text around it follows the App language (multilingual spec §21). Sign-ins and what a service may do never depend on language settings.

Connected services add tools. Today they are:
- GitHub (`github.search`, `github.issue`, `github.comment`).
- Home Assistant (`homeassistant.states`, `homeassistant.call`).
- Email over IMAP and SMTP (`email.search`, `email.read`, `email.draft`, `email.send`, `email.archive`).
- Calendar over CalDAV (`calendar.search`, `calendar.availability`, `calendar.create`, `calendar.update`, `calendar.cancel`).

Email and Calendar sign in with an app password. See [Tools](tools.md#email-and-calendar).
- **Connecting:** `PUT /connectors/{id}` checks the values with the service before storing anything, and returns the account it connected as. A blank secret field keeps the stored value.
- **Storage:** credentials are stored in the `secrets` directory of the data directory, not in the database. They are added to a request only when a tool runs, so they are never part of model context, events, or tool arguments.
- **Responses:** the API never returns a secret value; `values` shows a stored token as its last four characters only. Results and errors are scrubbed of any credential value before the model sees them.
- **Policies:** connected tools join the tool catalog with `source` `connector:<id>`. Reading is allowed and changes ask first, unless a profile sets its own policy for the tool.
- **Selection:** they are offered when a message is about the service. That means it names the service, or uses words like issues or pull requests for GitHub and lights or sensors for Home Assistant. It also counts when the message uses words the service taught: Home Assistant's device names, learned when it connects and whenever all devices are read. Tools that change something are offered only when the message asks for a change ("turn on", "comment").
- **Fetched first:** a read tool marked `prefetch` (Home Assistant's device list) is called before the model answers a message about its service, when the profile allows it without asking. Its data, written as plain lines, replaces the web look-up for that turn.
- **Untrusted data:** what they return is treated as untrusted data (§58), and links in results become sources.

## MCP

MCP tool sources add tools the same way, with `source` `mcp:<source>`; secrets are kept in `secrets/mcp-<source>.json`. `/mcp` (outside `/api/v1`) is Toskar's own MCP server for other apps, checked like `/v1`, and `/mcp/oauth/callback` is where a tool source's sign-in returns. See [MCP](mcp.md).

## Personalization

Personalization shapes how answers look in every chat, automation, and API request. It has four choices: `length` (`brief`, `balanced`, `detailed`), `tone` (`friendly`, `neutral`, `direct`), `format` (`prose`, `lists`), and `units` (`metric`, `imperial`). It also has two short notes, `about_me` and `instructions`, of up to 1,500 characters each. It is stored as a setting and added to the model's instructions as style guidance, after a specialized AI's own instructions. It is kept apart from permissions: what a tool may do comes only from profiles and Settings. A personalization note or a memory that tries to grant a permission is refused with 400, for example "you can always push without asking" or "don't ask before running commands". The refusal says where permissions are set. A memory that states a preference, such as "I use the terminal a lot", is saved and changes no policy.

**Answer language.** Every chat, automation, and API request is answered in one language, decided on this computer (multilingual spec §11–12): a language the message asks for, such as "answer in English" or "antworte auf Deutsch", always wins. Then the `assistant_language_mode` setting decides: `auto` (the default) answers in the language the message is written in, or the conversation's recent language when the message is too short to tell; `language` answers in `assistant_language`; `app` answers in the App language (`ui_locale`). After that come this computer's language and English. Code, commands, file names, and quoted text keep their language.

## Privacy and run records

Each run records what left this computer.
- **Record kinds:** `web_search` (the query), `web_page` (the address), `places` (the map service and the place, kind of place and point, or route asked for), `paired_computer` (the prompt and context, or training examples), `external_server` (a chat sent to a server that is not on this computer), `connector` (the service and what it was asked; long text such as a comment's body is left out), `notification` (an email or webhook delivery: the server or host, and the notification's title), and `community_ratings` (a rating shared or withdrawn, with the model, hardware class, and how it runs when that is shared, or the public ratings summary downloaded).
- **Record fields:** `source` (`chat`, `api`, `automation`, `training`), plus `conversation_id` and `task_id` when there are any.

Memories and knowledge sources have `local_only`. Set it with `PATCH /memory/{id}` or the knowledge update, `{"local_only": true}`. A turn that uses a local-only memory or a passage from a local-only source runs on this computer, even when placement would have chosen a paired computer, and its steps say so.

Run records hold prompts and tool results: tasks and their steps, automation run results, and the egress record.
- **Retention:** they are kept for `retention_days` (30 by default; 0 keeps them) and removed daily.
- **Delete now:** `POST /privacy/delete-runs` removes them now and returns how many.
- **Exceptions:** each automation keeps its latest successful result, which the `change` notification mode compares against, and work that may still be running is never removed.
- **Chats:** chats are not run records; the `save_chat_history` setting covers them.

Every chat turn, API request, and automation run is traced. A chat or API run's id is the turn's task id, and the answer's `meta.run_id` names it. A run has:
- `strategy`: how it was handled, such as "Looked up the web first" or "Worked through 3 parts side by side". It also includes the routing reason and a fallback, when there was one.
- `effort`.
- `models`: per model, role, and computer. Each entry has the calls, `load_ms` (a real model start of 150 ms or more), `first_token_ms`, `ttft_ms`, tokens in and out, `cached_tokens` (from llama.cpp's cache), and tokens per second.
- `tools`: calls, failures, and total time.
- `nodes`, `workers` and `parallel` for a plan, verification passes with issues and fixes, and `retries`.
- `context_tokens` of `context_limit`, `latency_ms`, and `pipeline_ms`.
- `status`: `completed`, `failed`, or `stopped`.

In advanced mode, an answer has "Run details". Runs are run records, so the retention and delete action above apply to them.

## Community ratings

A person rates a model with 1 to 5 stars and optional reasons (`great_responses`, `fast`, `slow`, `stable`, `crashed`, `too_much_memory`, `great_for_coding`, `great_for_chat`, `good_tool_use`, `poor_tool_use`). The rating is kept on this computer.
- **Sharing:** `PUT /models/{id}/rating` with `{"stars": 4, "tags": ["fast"], "share": true}` also sends it to the ratings service ([yeixio/toskar-ratings](https://github.com/yeixio/toskar-ratings)). `GET` returns `shares`, exactly what that sends besides the stars and tags: the model (a slug of its Hugging Face repository, quantization, format, runtime, and backend) and the hardware class (platform, architecture, accelerator maker and model, memory type, and a memory band). A random rating ID, made the first time a rating is shared and never derived from the computer, and the app version go with it. `share: false` withdraws a shared rating; `DELETE` withdraws it and removes it here. A `502` means the rating is saved here but the service could not be reached.
- **In recommendations:** with community ratings on, they are one signal next to the curated order, for the Best overall, Best coding, and Best reasoning picks in `/models/fit` (this computer only) and the main model in `/models/recommend`. A model's signal is its cohort's weighted score less the summary's `prior`, from hardware like this computer's when that cohort is published and from everyone otherwise; early ratings count half. The pick is the eligible model whose signal, less 0.3 for each place down the curated list, is highest, so a model two places down needs to be rated 0.6 better to be chosen first, and a poorly rated first choice gives way. Ratings never make a model that cannot run here eligible. `community_chosen` on a winner or recommendation says ratings changed the pick. Only the summary already kept is used, so recommendations never download anything.
- **How it runs:** `"observations": true` with `share` also sends how the model ran on this computer in the last 30 days, as `GET` shows it in `observations`: the median tokens per second and time to first token (from 3 or more replies), starts and failed starts, whether it crashed or ran out of memory, and the band of the most context a reply used. Replies that ran on a paired computer are left out. Starts, failed starts, and crashes are counted from when this version is installed and kept 90 days. What is sent is a snapshot from when the rating is saved; saving again updates it. In `/ratings/community`, a cohort where people shared this has `observed`, `median_tokens_per_second`, `median_ttft_ms`, `successful_start_rate`, `crash_rate`, and `out_of_memory_rate`.
- **Comparable models:** a model installed from somewhere other than Hugging Face, or whose quantization is not in its name, has `rateable: false` and a `reason`. It can still be rated here.
- **Language:** `"language": "es"` says the language the model was used in (multilingual spec §23). It is reduced to what ratings collect by, a base tag such as `pt` for `pt-BR`, with Chinese kept as `zh-Hans` or `zh-Hant`; anything that is not a language tag is a `400`. `GET` returns it as `language`, and a shared rating sends it. Leaving it out says none. In `/ratings/community`, a model's `languages` are its ratings by language, each with `language`, `ratings`, `average`, `weighted_score`, and `confidence`, for everyone who runs it the same way: published with at least 3 ratings, never split by hardware. With community ratings on, Auto counts them when choosing a model for an answer in that language: a language with 10 or more ratings sets the model's level for it (Excellent from a weighted score of 4.3, Good from 3.7, Fair from 3.0, Limited below), and one with 3 to 9 moves the level one step at most. A level the ratings moved lists `community` among its `sources`.
- **When to ask:** `ask` is `true` for an installed, unrated model that has answered 10 times on at least 2 days, while the `ratings_prompts` setting is on (the default) and the person has not dismissed it.
- **Community scores:** with the `community_ratings` setting on (off by default), `GET /ratings/community` downloads the public summary at most once a day, from the service or, when it cannot be reached, from [yeixio/toskar-model-data](https://github.com/yeixio/toskar-model-data), and keeps it for offline use. For each local model it returns `similar`, the narrowest published group of hardware like this computer's (`family`, then `class`, then `backend`), and `overall`. Each has `ratings`, `average`, `weighted_score` (leaning toward the average while there are few ratings), and `confidence` (`limited`, `early`, or `community`). Ratings of different quantizations, runtimes, or backends are never combined.

## Profiles and orchestration

A profile's `orchestration` object holds its advanced controls. Every field is optional; empty keeps the default, which follows the chat's effort.

| Field | Values | Effect |
| --- | --- | --- |
| `strategy` | `single`, `planned`, `team` | How a request is worked through. Empty is Auto. `single`: one model, no plan. `planned`: a request with several parts is always worked through in parts. `team`: a planner splits the request, workers do the parts, and a reviewer checks the answer; quick questions are still answered directly |
| `effort` | `fast`, `balanced`, `thorough` | The profile's effort when a chat leaves effort on Auto |
| `planning` | `on`, `off`, `always` | `on` works through requests with several parts in parts, whatever the effort; `always` also asks the planner model to split a request with no obvious parts |
| `max_workers` | 2–8 | Most parts in a plan |
| `parallel` | `on`, `off` | `off` works through parts one at a time |
| `verification` | `off`, `check`, `correct`, `thorough` | `off` skips the figure check; `check` reports only; `correct` and `thorough` allow one or two correction passes |
| `max_tool_calls` | 1–50 | Most tool calls in one turn |
| `memory` | `off` | Keeps persistent memory out of the profile's chats |
| `context_share` | 0.1–0.9 | Most of the model's window earlier messages may use |
| `fallback` | `off` | Shows a failure instead of answering on another model |
| `fallback_models` | up to 8 model ids | Tried in order when the answering model fails, before Toskar picks another installed model |
| `retries` | 1–3 | Tries after a model fails before showing anything (default 1). Each try first runs the same model on another online computer that has it, then another model |
| `timeout_seconds` | 10–3600 | Stops a turn that runs longer; the answer so far is kept and says it reached the time limit |

Invalid values are refused with 400. In advanced mode, the profile editor has an Orchestration section, alongside model roles, tools, knowledge, and placement.

A profile's `roles` assign models, and optionally computers, to these roles. A role without a model uses the model the chat chose, or Auto's pick.

| Role | Used for |
| --- | --- |
| `assistant` | Writing the answer (the primary model) |
| `fast` | Quick questions, when the chat is on Auto |
| `coding` | Coding requests, when the chat is on Auto |
| `planner` | Splitting a request into parts |
| `worker` | Each part of a plan. With the Team strategy or a worker model, each part gets its own slot (`worker:1`, `worker:2`, …) that Norn can place on another computer, and parts on different computers are written at the same time |
| `reviewer` | Checking the answer |

A profile's `node_policy` holds its placement rules:

| Field | Values | Effect |
| --- | --- | --- |
| `mode` | `automatic`, `prefer_local`, `manual` | Where roles run by default; `manual` relies on role pins |
| `preferred_nodes` | computer ids | Favored when they have the model |
| `denied_nodes` | computer ids | Never used |
| `remote` | `off` | Every turn stays on this computer |

A computer cannot be both preferred and denied. A role pinned to a computer still follows its pin; a worker slot such as `worker:2` follows the `worker` role's pin.

`orchestrator_id` is `simple` for every profile. A profile sent with the older `team` orchestrator, or with the role names `coordinator` and `researcher`, is stored with the Team strategy and the roles `planner` and `assistant`. Profiles saved by older versions are migrated the same way at startup.

## Structured results

Structured results are checked the same way elsewhere:
- **Tool arguments:** they are checked against each tool's schema before the tool runs. Safe repairs are made, such as `"7"` for a whole number, or JSON data where text is expected. A call with an argument of the wrong type is refused with `kind` `invalid`, naming the argument, so the model can call again.
- **Automations:** a condition automation's result must end with the JSON its condition reads: `{"price": number}` for a threshold, or `{"significant": boolean}`. A threshold's optional `currency` (ISO 4217, such as `EUR`; omitted means `USD`) is the currency the run reports the price in, so the numbers compare as they are. That JSON is read with the same repairs. When it is missing or wrong, the model is asked once to supply it from its own answer. Notices show the prose, never the JSON.

## Capability inventory

The capability inventory lists what exists right now:
- `models` (with the computers they are on and whether they are running), `nodes` (online, memory, whether they can train), and `tools` from every source (built in, connected services, MCP), enabled or not;
- `connectors`, `providers` (runtimes and MCP tool sources, with health), and stored `artifacts`;
- `abilities`, each with `available`, the tools or models it comes `via`, and a `note` saying how it works or what would make it possible. Abilities are worked out from tools and models by what they do, so a new MCP tool that sends email counts as email without code.

A chat question about what Toskar can do, such as "Can you generate an image?", "Do you have access to my email?", or "Which computer can run Qwen 2.5 14B?", gets the matching facts as trusted instructions. The model answers from them instead of guessing, and the answer's steps say so. In the app, Diagnostics shows the same list.

Three behaviors come from the quality test set (`tests/quality`):
- **Plain questions:** a plain question is answered without tools; the message must ask for a search, a file, a command, and so on.
- **Capability questions:** a short question about what Toskar can do is answered straight from the capability inventory, without a model.
- **False claims:** an answer that says it changed something, when no tool that changes things ran, gets the notice "Nothing was changed: no tool ran to do this, whatever the answer says."

**Questions about Toskar.** The user guide (`docs/user-guide/guide.json`) ships inside the daemon. A question about Toskar itself, one that names it or its screens, or one that matches the guide's own words strongly, gets up to four matching guide passages (about 3,000 characters) in the turn's instructions, and the model is told to answer from them, name screens as they do, and say when they don't cover the question. The answer's steps say which sections it read. Questions about anything else get nothing added. A short how-to question about MCP is answered directly, without a model.

## Caches

Every cache declares its policy: `key`, `ttl`, `invalidation`, `scope`, and `privacy` (`public` or `personal`). Secret data, such as credentials, is never cached; a cache that would hold it is refused when it is created.

| Cache | Key | Kept | Cleared when | Privacy |
| --- | --- | --- | --- | --- |
| `web_search` | the query, in lower case | 15 min | age; run records deleted | personal |
| `web_pages` | the page address | 30 min | age; run records deleted | personal |
| `capabilities` | one inventory snapshot | 30 s | a model downloads, loads, or unloads; a computer pairs or goes on- or offline; a tool is turned on or off | public |
| `model_search` | Hugging Face search text and limit | 5 min | age | public |
| `knowledge_index` | source and passage (on disk) | until invalidated | files change; reindex; source removed | personal |

A repeat web search or page read is answered from the cache. The tool does not run, nothing leaves this computer (so no egress record is written), and the run trace counts it in `cache_hits`. In-memory caches are bounded, dropping the least recently used entry first, and can be cleared one at a time. "Delete run records now" also clears every personal in-memory cache.

## Client contract

The desktop app, mobile apps, and other clients read a versioned contract: events, run traces, answers with their citations, steps, and files, artifacts, notifications, and egress records. The version is `major.minor`, now `1.9` (1.1 added `repeat_count` to notifications; 1.2 added `setup` to answers, an offer to install what a request needed; 1.3 added stable error codes to chat streams; 1.4 added `message` to notifications, their title and body as catalog keys; 1.5 added `error_code` and `error_details` to run traces; 1.6 added `context` to answers, the context gauge; 1.7 added `backend` and `device` to the models in a run; 1.8 added them to answers; 1.9 added `artifact_id` to file citations, the chat file a source names, so a client can offer it for download).
- **Where it appears:** every event has `contract`, and so do answer metadata and run traces. Metadata saved before the contract existed has no `contract` and reads as 1.0. Every response carries the `Toskar-Contract` header, and the same value as `Yggdrasil-Contract`, its name from before the rename, and `GET /api/v1/version` has `contract` (`version`, `major`).
- **Minor versions** add fields or event types. Clients ignore what they do not know, so an older client keeps working.
- **Major versions** remove something or change its meaning. A client may send `Toskar-Client-Contract: 1.0`, or `Yggdrasil-Client-Contract: 1.0`, which every version accepts; when both are sent, `Toskar-Client-Contract` wins. A client built for another major version gets 426 with code `CONTRACT_MISMATCH`, and the message says whether to update the app or Toskar. A client that sends no header is served as before.
- **Compatibility test:** `tests/contract` records the contract's fields. It fails when one is removed or renamed within a major version, and when one is added without a minor version bump (`UPDATE_CONTRACT=1 go test ./tests/contract` records the new fields).

## Errors

An error is `{"error": {"code", "message", "details"}}`. `code` is stable, such as `MODEL_NOT_INSTALLED` or `MEMORY_LOOKS_SECRET`, and `details` has the values its message needs, such as `model_id`. Clients show text for the code from the catalog, `i18n/locales/<language>/errors.json`, in the App language; `message` is the English text, for logs, Diagnostics, and codes a client does not know yet. A code keeps its meaning, and a more specific code may replace a general one such as `BAD_REQUEST` or `INSTALL_FAILED`. Every code Toskar sends has English text in `errors.json`, and a test checks it.

A chat that fails while streaming sends `event: error_code` with the same `code`, `message`, and `details`, then `event: error` with the text, as before. Chat errors are recognized in core, once: `NO_MODEL_INSTALLED`, `NO_MODEL_ASSIGNED`, `MODEL_NOT_INSTALLED`, `RUNTIME_NOT_INSTALLED`, `CONTEXT_TOO_LONG`, `OUT_OF_MEMORY`, `COMPUTER_OFFLINE`, `CONNECTION_LOST`, `RUNTIME_ERROR`, and `MODEL_UNHEALTHY`, whose `error` text is the model's failure as JSON.

## Events

`GET /api/v1/events` is a server-sent event stream. Each event has `type`, `payload`, a timestamp, and `contract`. Clients should ignore types they do not know.

| Type | Sent when |
| --- | --- |
| `chat.token` | A piece of an answer is written |
| `chat.complete`, `chat.error`, `chat.stopped` | A turn finishes, fails, or is stopped. `chat.complete` carries the answer's `meta` and `context`. |
| `chat.model_routed` | A model is chosen for a turn, with `reason`, and `fallback: true` after a failure |
| `chat.effort`, `chat.lookup`, `chat.verifying`, `chat.making_file`, `chat.summarized` | Effort is set, the web is looked up first, figures are being checked, a requested file is being made, or older messages were summarized |
| `plan.created`, `plan.step` | A request with several parts is planned, and each part runs |
| `verify.action`, `verify.done`, `verify.code`, `verify.consistency` | The answer checks: figures, claimed actions, code, and contradictions |
| `tool.requested`, `tool.started`, `tool.completed`, `tool.failed`, `tool.parsed` | A tool call waits for approval, runs, finishes, fails (with `kind`), or is read from text |
| `knowledge.retrieved`, `knowledge.failed` | Knowledge passages are added to a turn, or the search fails |
| `memory.saved`, `memory.deleted` | A memory is saved or forgotten from a chat |
| `orchestration.role` | A planner, worker, or reviewer starts, with the computer it runs on |
| `plan.planner`, `answer.reviewed` | The planner split a request (`parts`); the reviewer checked an answer (`changed`) |
| `task.created`, `task.started`, `task.completed`, `task.failed` | An orchestration task changes state |
| `scheduler.placement` | Norn places work on a computer |
| `work.waiting` | Work waits for higher-priority work, with `class`, `label`, and `reason` |
| `automation.started`, `automation.completed`, `automation.failed` | An automation runs. `automation.completed` lists skipped tools in `skipped`. |
| `notification.created` | A notification is stored |
| `model.download.started`, `.progress`, `.completed`, `.failed` | A model downloads |
| `model.load.started`, `model.load.completed`, `model.unloaded` | A model loads, or the idle sweeper unloads it |
| `model.health.degraded`, `model.health.failed` | A loaded model stops answering health checks |
| `model.cleanup.started`, `.completed`, `.failed` | Model files are removed |
| `node.discovered`, `node.online`, `node.offline`, `node.paired` | Another computer is found, comes online, goes offline, or pairs |
| `training.job`, `training.eval`, `training.deployed` | A training job changes, an evaluation runs, or an AI is deployed |
| `training.export.completed`, `training.export.failed` | A GGUF export finishes or fails |

## OpenAI-compatible API

Implemented routes:

| Method | Path |
| --- | --- |
| GET | `/v1/models` |
| POST | `/v1/chat/completions` |

No other `/v1` routes are registered. Embeddings, image generation, and the legacy completions API are not implemented.

### `GET /v1/models`

Returns `auto` and profiles, not raw files on disk:

```json
{
  "object": "list",
  "data": [
    {"id": "auto", "object": "model", "owned_by": "yggdrasil"},
    {"id": "profile:general-assistant", "object": "model", "owned_by": "yggdrasil"}
  ]
}
```

Built-in profile ids include `general-assistant`, `programming`, and `research`. `owned_by` stays `yggdrasil` for contract 1.x, since clients may match on it.

### `POST /v1/chat/completions`

```json
{
  "model": "profile:general-assistant",
  "messages": [{"role": "user", "content": "Hello"}],
  "stream": false
}
```

`model` may be `profile:<id>`, `auto`, or a bare id. A bare id is used as a profile id when that profile exists, and otherwise as a model override. `auto` picks an installed model for each request, as in chat.

The API gets the same assistant as chat: planning, web look-ups, connected services, answer checks, personalization, and specialized AIs.
- **Messages:** the whole `messages` array is used. The last `user` message is the turn, and earlier `user` and `assistant` messages are its history. `system` (and `developer`) messages are the calling app's instructions. They cannot change what tools may do.
- **Effort:** `reasoning_effort` maps to effort. `minimal` and `low` give Fast, `medium` gives Balanced, and `high` gives Thorough.

The optional `toskar` object holds the assistant's own controls. Its name from before the rename, `yggdrasil`, works too; when a request sends both, `toskar` wins.

```json
{
  "model": "auto",
  "messages": [{"role": "user", "content": "What did we decide about the release?"}],
  "toskar": {
    "memory": true,
    "knowledge": true,
    "knowledge_sources": ["<mimir source id>"],
    "tools": ["internet.search", "internet.open"],
    "effort": "thorough",
    "placement": "local",
    "progress": true
  }
}
```

| Field | Meaning |
| --- | --- |
| `memory` | Use the person's memories. API requests use them only when they ask, unless the key says otherwise. "Remember that …" saves a memory only when memory is on for the request. |
| `knowledge` | Use the profile's connected knowledge. `knowledge_sources` adds sources. |
| `tools` | Narrow the profile's tools to these ids. A request can never add a tool or loosen a policy. |
| `effort` | `auto`, `fast`, `balanced`, or `thorough`; it takes precedence over `reasoning_effort`. |
| `placement` | `local` or `automatic`. |
| `progress` | With `"stream": true`, progress and tool activity arrive as chunks with an empty `delta` and a `toskar.event`, such as `{"type": "tool.started", "tool_id": "internet.search"}`. Before `[DONE]`, a last such chunk carries `toskar.sources`, `steps`, `notice`, and `files`. Each chunk has the same object as `yggdrasil` too, for clients written before the rename. Clients that ignore unknown fields see a plain OpenAI stream. |

`response_format` asks for JSON:
- **Types:** `{"type": "json_object"}`, or `{"type": "json_schema", "json_schema": {"schema": {...}}}` with a JSON Schema. Toskar checks `type`, `properties`, `required`, `enum`, and `items`.
- **Constrained output:** the model is told the shape. On this computer, llama.cpp also constrains the reply to the schema with a grammar. Such a turn is one reply: the web look-up still runs first, but there is no plan, tool call, file, or figure check.
- **Repairs:** the answer's JSON is found (fenced or not) and safely repaired: trailing commas, curly quotes, `"$1,299"` for a number, `"yes"` for a boolean.
- **Retry:** an answer that still does not fit is asked for once more, with the problems named.
- **Result:** the response content is compact JSON. When nothing fits, the request fails with 422 and lists the problems. A streamed request gets the JSON as one chunk.

A non-streaming response has a `toskar` object, and the same object as `yggdrasil`, with the answer's `sources`, `steps`, `notice`, and `files` when there are any.

Each API key has `permissions` that a request can only narrow:
- `memory` and `knowledge` are `never`, `on_request`, or `always`. The defaults are `on_request` for memory and `always` for knowledge.
- `tools` is `profile` (the default), `read_only`, or `none`.
- `placement` (true by default) lets a request choose where it runs.

Asking for something a key does not allow returns 403 and says what was refused. Change a key's permissions with `PUT /api/v1/api-keys/{id}/permissions` or on the API Access page; rotating a key keeps them. When the API listens beyond this computer, every request from another machine needs a key, so its limits always apply there. On this computer a key is optional; its limits apply when the app sends it, and a request without one gets the defaults.

### Streaming

`"stream": true` responds with `Content-Type: text/event-stream`. Each event is `data: {json}` and the stream ends with `data: [DONE]`.

### Known differences

- `temperature` and `max_tokens` are accepted and ignored.
- `tool` messages and assistant `tool_calls` in the history are skipped.
- Tool definitions in the OpenAI request are not passed through. Tool use is controlled by the Toskar profile.
- The non-streaming `id` is the fixed string `chatcmpl-ygg`.
- A model must already be installed and startable. The HTTP call does not download one for you.

Examples that match this behavior are in [examples/](../examples/).

## GPU acceleration

When llama-server loads a model, it logs which devices it found, how many of the model's layers it put on a GPU, and the memory it took there. The daemon reads that log, so `GET /models/running` reports each model's `acceleration`:

| Field | Meaning |
| --- | --- |
| `state` | `gpu` (every layer on a GPU), `partial` (some layers), `cpu` (on the CPU although this computer has a GPU), or `cpu_expected` (on the CPU, with no GPU to use) |
| `reason` | Why a state is short of `gpu`: `cpu_build` (the CPU-only llama.cpp is installed where the GPU build would run; reinstall it from Diagnostics), `gpu_memory` (the model doesn't fit in GPU memory), `gpu_unavailable` (the GPU build found no GPU it could use, such as without a graphics driver or Vulkan), or `no_gpu` |
| `backend` | `metal`, `vulkan`, `cuda`, `rocm`, `sycl`, or `cpu` |
| `devices` | The GPUs the model is on |
| `layers_offloaded`, `layers_total` | Layers on a GPU, of the model's total (0 when the runtime didn't say) |
| `gpu_memory_bytes` | What the model, its cache, and its working memory take on the GPUs |

The same report tags what each model produced, so a GPU reply can be told from a CPU one: `GET /performance` records and the models in a run (`GET /runs/{id}`) carry `backend` and `device` (such as `vulkan` and `AMD Radeon RX 7900 XTX`, or `cpu` and an empty device), and so does every benchmark sample. They are empty for replies recorded before this, and for steps that ran on a paired computer. A saved answer's message metadata carries them too (`meta.backend`, `meta.device`, contract 1.8), so a client such as the iPhone app can say whether an answer came from the GPU. `GET /runtimes` lists what the installed llama.cpp build can run on in `detection.backends`, such as `["cpu", "vulkan"]`; a CPU-only build is `["cpu"]`.

`GET /health` sums it up in `acceleration`: the least accelerated loaded chat model's state, or `idle` when no model is loaded. It never changes `status`; a computer without a GPU is healthy.

### Live figures

`GET /performance/live` returns one entry per computer, this one first: `current`, `recent` (a reading every 5 seconds while a model is loaded, every minute otherwise, over the last hour), and `day` (per-minute averages). Each reading has `cpu_percent`, `memory_used_bytes`, `memory_total_bytes`, and `gpus`, each with `busy_percent`, `memory_used_bytes`, `memory_total_bytes`, `temperature_c`, and `power_watts`. A figure a computer can't give is left out, never reported as 0. None of them need root or admin rights:

| Platform | CPU and memory | GPU |
| --- | --- | --- |
| Linux | `/proc/stat`, `/proc/meminfo` | AMD: the kernel's `gpu_busy_percent`, `mem_info_vram_*`, and hwmon temperature and power. Intel: hwmon where exposed. NVIDIA: `nvidia-smi` |
| macOS | `top`, `vm_stat` | `ioreg` (busy %, and on Apple silicon the shared memory the GPU is using). Temperature and power need root, so they are left out |
| Windows | `typeperf`, `GlobalMemoryStatusEx` | Windows' GPU counters (busy %, dedicated memory). NVIDIA: `nvidia-smi`, which also gives temperature and power |

