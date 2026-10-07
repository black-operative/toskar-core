# Configuration

Toskar Core is configured in three places:

- **`config.json`** in the data directory: where the daemon listens, where it keeps files, and how it finds other computers. It is read at startup.
- **Environment variables** (`TOSKAR_*`) override some `config.json` values. They are meant for containers and scripted nodes. Each also works under its name from before the rename, `YGGDRASIL_*`, such as `YGGDRASIL_API_PORT`; when both are set, `TOSKAR_*` wins.
- **Settings** are stored in the database and changed in the app or through `PATCH /api/v1/settings`. Most take effect at once.

## Data directory

| OS | Default |
| --- | --- |
| macOS | `~/Library/Application Support/Toskar` |
| Windows | `%LOCALAPPDATA%\Toskar` |
| Linux | `$XDG_DATA_HOME/toskar`, or `~/.local/share/toskar` |

An install from before the rename to Toskar keeps its `Yggdrasil` folder (`yggdrasil` on Linux) and its `yggdrasil.db`: when that folder exists and the Toskar one has no `config.json`, it is used as it is, and nothing is moved.

Start the daemon with `-data-dir <path>` to use another directory, such as a second daemon for testing. A `config.json` that does not set `data_dir` belongs to the directory it is in. `toskarctl paths` prints the default paths.

What lives there:

| Path | Contents |
| --- | --- |
| `config.json` | The values below. Written with mode `0600`. |
| `toskar.db` | SQLite: models, profiles, chats, memories, settings, automations, specialized AIs and their examples, knowledge indexes and vectors, run records, notifications, API key hashes, paired computers. |
| `models/` | Installed GGUF model files, and image and video models in `models/images/` and `models/video/`, one folder per model. |
| `runtimes/llamacpp/` | `llama-server` and the other llama.cpp programs. |
| `runtimes/python/` | `uv`, a private Python, and the environments for training (`envs/trainer-mlx`, `envs/trainer-peft`), text recognition (`envs/ocr`), running code (`envs/code`), and speech (`envs/speech`), each installed the first time it is needed. |
| `runtimes/sdcpp/` | stable-diffusion.cpp, one folder per release, installed by image generation setup. |
| `runtimes/speech/` | Downloaded Whisper models (`whisper/`) and Piper voices (`voices/`), Hugging Face's own logs and caches (`hf/`), and each speech job's files (`jobs/`, removed when the job ends). |
| `artifacts/` | Files attached to chats and files the assistant made, one folder per chat. |
| `knowledge/` | Copies of pasted and uploaded knowledge, and `ocr-cache/` (recognized text of scanned PDFs). |
| `training/` | Trained adapters (`adapters/`), exported GGUF files (`exports/`), job working files (`jobs/`, removed when a job ends), and downloaded training weights (`hf-cache/`). |
| `code-runs/` | Each code run's working folder, deleted when the run ends. |
| `browser/` | Each chat's temporary browser profile, deleted when its browser closes. |
| `image-jobs/`, `video-jobs/` | Each image's or clip's working files, deleted when it is made. |
| `logs/` | `daemon.log` (JSON) and llama-server logs. |
| `secrets/` | This computer's identity (`node-<id>.key`, `.pub`), and credentials: `connector-<service>.json`, `mcp-<source>.json`, `knowledge-<source>`, `notify-<destination>`. Directory mode `0700`, files `0600`. |

## `config.json`

| Key | Default | Meaning |
| --- | --- | --- |
| `data_dir` | the directory the file is in | Root of the paths below |
| `db_path` | `<data_dir>/toskar.db` (or an existing `yggdrasil.db`) | SQLite database |
| `models_dir` | `<data_dir>/models` | Model files |
| `runtimes_dir` | `<data_dir>/runtimes` | Runtime binaries and Python environments |
| `logs_dir` | `<data_dir>/logs` | Log files |
| `web_ui_dir` | empty | A built web UI to serve instead of the bundled one, such as `web/dist` in a clone |
| `web_ui_enabled` | `true` | Serve the web UI at `/` |
| `api_host` | `127.0.0.1` | Address of the API and web UI. Any address other than loopback requires an API key (see [API](api.md#authentication)). |
| `api_port` | `7331` | Port of the API and web UI |
| `lan_api_enabled` | `false` | Set by **Local network access** on the API Access page. Turning it on sets `api_host` to `0.0.0.0` and needs an existing API key. |
| `internal_host` | `127.0.0.1` | Address of Bifrost, the computer-to-computer server. Discovery sets it to `0.0.0.0`. |
| `internal_port` | `7332` | Port of Bifrost |
| `discovery_enabled` | `true` | Advertise and find other computers on the local network over mDNS |
| `static_peers` | none | `host:7332` addresses to try when mDNS cannot see them, such as across Docker networks or a VPN |
| `advertise_host` | the first non-loopback IPv4 address | The address paired computers use to reach this one |
| `places_geocoder_url` | `https://nominatim.openstreetmap.org` | The Nominatim service places and routes use to find places and addresses |
| `places_overpass_url` | `https://overpass-api.de/api/interpreter` | The Overpass service for kinds of places near somewhere |
| `places_router_url` | `https://routing.openstreetmap.de` | The OSRM service for routes. Your own OSRM server is used as `/route/v1/{driving,foot,bike}/…`. |
| `external_openai_url` | none | An OpenAI-compatible server (OpenAI, vLLM, Ollama elsewhere…) whose models can be chosen for a chat. Its API key is `secrets/external-openai.key`. Set both on the Computers page, under Network & access → External server (advanced mode) or with `PUT /api/v1/external-server`. |
| `ratings_url` | `https://ratings.toskar.ai` | The community ratings service that shared ratings go to and the daily summary comes from |
| `ratings_summary_url` | the summary in `yeixio/toskar-model-data` | The public ratings summary used when the service cannot be reached |
| `node_name` | the host name | The name other computers see |
| `node_id` | generated | This computer's id. Do not copy it between computers. |

Restart the daemon after editing the file by hand. A changed `api_host` or `internal_host` also applies only after a restart, because the daemon keeps the socket it bound at startup.

## Environment variables

| Variable | Overrides | Notes |
| --- | --- | --- |
| `TOSKAR_API_HOST` | `api_host` | `0.0.0.0` makes the API reachable from other computers. The daemon will not start until a key exists or `TOSKAR_API_KEY` is set. |
| `TOSKAR_API_PORT` | `api_port` | |
| `TOSKAR_API_KEY` | | Stored, hashed, as an API key at startup if it is not already valid. |
| `TOSKAR_INTERNAL_HOST` | `internal_host` | |
| `TOSKAR_INTERNAL_PORT` | `internal_port` | |
| `TOSKAR_DISCOVERY_ENABLED` | `discovery_enabled` | `1`, `true`, `yes`, or `on` turn it on; anything else turns it off. |
| `TOSKAR_STATIC_PEERS` | `static_peers` | Comma-separated `host:port` list |
| `TOSKAR_ADVERTISE_HOST` | `advertise_host` | |
| `TOSKAR_NODE_ID` | `node_id` | For containers with a fixed identity |
| `TOSKAR_NODE_NAME` | `node_name` | |
| `TOSKAR_WEB_UI_DIR` | `web_ui_dir` | |
| `TOSKAR_WEB_UI_ENABLED` | `web_ui_enabled` | |
| `TOSKAR_STUB_INFERENCE` | | Answers with a stub model instead of llama.cpp. For tests and the Docker cluster check only. |
| `TOSKAR_PPROF` | | Serves Go's live profiles at `http://<address>/debug/pprof/`, such as `127.0.0.1:6060`, for diagnosing memory or CPU use. Only a loopback address is accepted. Off when unset. |
| `TOSKAR_DESKTOP_NOTIFICATIONS` | | `shell` hands desktop notices to the desktop app as `notification.desktop` events instead of posting them from the daemon. The desktop app sets it on the daemon it starts, so its notices carry its name and icon and open the app when clicked. |
| `TOSKAR_UPDATE_CHECK` | | `off` stops the daily look for a newer version, whatever the setting says. The desktop app sets it on the service it starts, because it updates with the app. |
| `TOSKAR_SANDBOXED` | | `1` behaves as if the daemon ran in the macOS App Sandbox (see [Runtimes](runtimes.md#python-environments-in-sandboxed-builds)). For testing. |

`toskarctl` reads two more: `TOSKAR_URL` (the daemon address, default `http://127.0.0.1:7331`) and `TOSKAR_API_KEY` (sent as the bearer token). See [CLI](cli.md).

## Settings

Settings are stored in `toskar.db`. The app's Settings page shows them, and `GET /api/v1/settings` returns them together with the read-only paths, addresses, and node identity above.

| Setting | Default | Meaning |
| --- | --- | --- |
| `ui_locale` | empty | **App language**, as a BCP 47 tag such as `es` or `pt-BR`. Empty follows each device's system language. The web UI, the desktop app, and the iPhone app read it, so they agree. `en-XA` is the pseudo-locale for testing translations. |
| `assistant_language_mode` | `auto` | **Assistant language**: the language answers are written in. `auto` answers in the language you write in, or the conversation's for a short message; `app` answers in the App language; `language` answers in `assistant_language`. A language asked for in a message, such as "answer in English", always wins. Detection runs on this computer. |
| `assistant_language` | empty | The language answers are written in with `assistant_language_mode` `language`, as a BCP 47 tag such as `de`. |
| `advanced_mode` | `false` | Shows Profiles, Tools, and API Access, per-tool permissions, and run details |
| `model_lifecycle` | `automatic` | `automatic` unloads models that sit idle; `manual` leaves them loaded until you stop them |
| `idle_unload_minutes` | `15` | Minutes before an idle model is unloaded; `0` never unloads |
| `keep_running_in_background` | `false` | Keep the daemon running when the desktop window closes. Saving a schedule turns it on. |
| `launch_at_login` | `false` | Start the desktop app at sign-in |
| `default_profile_id` | empty | Profile for new chats |
| `default_execution` | `automatic` | **Run chats on**: `automatic` (Norn picks a computer), `local` (this computer), or `ask` (ask each time) |
| `download_behavior` | `ask` | `ask` before downloading a model, or `automatic` |
| `model_storage_limit_gb` | `0` | Most disk space models may use; `0` is unlimited |
| `save_chat_history` | `true` | Keep chats after they end |
| `save_task_history` | `true` | Keep task history |
| `memory_enabled` | `true` | Persistent memory for chats. Each chat can still turn it off. |
| `notify_task_finish` | `true` | Desktop notices for finished automations and tasks |
| `notify_peer_offline` | `true` | A notice when a paired computer goes offline |
| `update_check` | `true` | Look at toskar.ai once a day for a newer version, and say so in Settings. Builds that update themselves (App Store, the desktop app) don't check. |
| `community_ratings` | `false` | Show community model ratings, downloading the public summary once a day while models are browsed |
| `ratings_prompts` | `true` | Ask for a rating after a model has been used a while |
| `notification_quiet_hours` | off, 22:00–07:00 | Quiet hours, as JSON; set them in Settings → Email, push, and webhooks or with `PUT /api/v1/notifications/quiet-hours` |
| `tool_terminal`, `tool_file_writes`, `tool_git` | `ask` | Default policy for the shell, file writes, and Git: `deny`, `ask`, `allow`, or `allow-for-session` |
| `node_name`, `lan_api_enabled`, `discovery_enabled`, `web_ui_enabled` | see `config.json` | Writable through settings; stored in `config.json` |

Personalization (`GET/PUT /api/v1/personalization`) and run-record retention (`GET/PUT /api/v1/privacy`) are stored as settings too. See [API](api.md).

`POST /api/v1/settings/reset` clears application state. With `delete_models=true` it also removes model files. It cannot be undone.

## Daemon flags

| Flag | Meaning |
| --- | --- |
| `-data-dir <path>` | Use another data directory |
| `-version` | Print the version, license, and corresponding source, then exit |
| `-python-envs` | Print, as JSON, the Python environments a sandboxed app bundles (see [Runtimes](runtimes.md#python-environments-in-sandboxed-builds)), then exit |
