# Privacy

Yggdrasil Core does not send usage telemetry by default.

A search of this repository found no analytics or crash-reporting client (no Sentry, PostHog, Mixpanel, Amplitude, Google Analytics, or a metrics upload). The `internal/tools/plausible.go` file checks whether a tool call looks like a real path or command. It is not the Plausible analytics product. Metrics stored by the daemon stay in the local SQLite database.

If that changes, this document should name what is collected, where it is sent, the default, and how to turn it off.

## What is stored on this computer

Everything Yggdrasil keeps is in the data directory. [Configuration](configuration.md#data-directory) lists every path. In short:

| Path | Contents |
| --- | --- |
| `yggdrasil.db` | The audit of tool calls (what each was about, never file contents), chats and their messages, summaries of long conversations, memories, profiles, settings, model start and crash counts for community ratings (kept 90 days), automations and their results, specialized AIs and their examples, knowledge indexes and vectors, run records (traces, task history, what left this computer), notifications, API key hashes, paired computers, and join token records (a proof key and when each was used, never the token). |
| `artifacts/` | Files attached to chats and files the assistant made, one folder per chat. Deleting a chat deletes its files. |
| `knowledge/` | Copies of content you pasted or uploaded as knowledge, and the recognized text of scanned PDFs. Linked files, folders, databases, and web APIs stay where they are. |
| `training/` | Trained adapters, exported GGUF files, and downloaded training weights. |
| `models/`, `runtimes/` | Model files, llama.cpp, and the Python environments for training and text recognition. |
| `logs/` | JSON logs. They are not sent anywhere. |
| `secrets/` | This computer's identity, and credentials for connected services, MCP tool sources, database and API knowledge, and notification destinations (SMTP passwords, ntfy access tokens, and webhook signing secrets). Directory mode `0700`, files `0600`. API keys are stored as bcrypt hashes in `yggdrasil.db`, never as plaintext. |

What you can see and remove:

- **Chats:** saved while `save_chat_history` is on (the default). Delete a chat from the chat list.
- **Memories:** listed on the Memory page, where you can edit, pause, mark **This computer only**, or delete them, or turn memory off. A secret such as a password, key, or card number is never saved as a memory.
- **Run records:** prompts and tool results of past runs. They are kept for 30 days by default and removed daily. Settings → **What left this computer** sets **Keep run records for** (7, 30, or 90 days, or **Keep them**) and has **Delete run records now**. Deleting them also clears cached web searches and pages. Chats are not run records and are not deleted with them.
- **Knowledge, files, specialized AIs:** each has a delete action. Deleting a specialized AI deletes its adapters and exports.

The diagnostic bundle is written to omit secrets, private keys, and API key material. Its `runtime.json` and `profiles/` describe the daemon's memory and goroutines: function names, counts, and sizes, with no prompts, files, or credentials. Do not assume a log file has been redacted. Remove tokens and personal text before you paste a log into an issue.

## Encryption

Yggdrasil does not encrypt its own files, and encryption is not required to use it.

- **At rest:** `yggdrasil.db`, `artifacts/`, `knowledge/`, and the other files above are plain files on disk. `secrets/` is readable only by your user account (`0700`/`0600`), and API keys are bcrypt hashes. Turn on full-disk encryption to keep everything encrypted while the computer is off or locked: FileVault on macOS, BitLocker or Device Encryption on Windows, and LUKS on Linux.
- **To the internet:** web search, page reads, model and runtime downloads, community ratings, and the built-in services use HTTPS. Addresses you enter, such as an external server, a webhook, an MCP server, or your own map services, are encrypted when they use `https://`. Email requires TLS or STARTTLS except for a mail server on this computer.
- **Between your computers:** Bifrost (port 7332) is plain HTTP. Pairing uses a signed handshake and each request carries a token from a paired node, so other machines can't join or call it, but a chat placed on a paired computer, and its answer, cross the network unencrypted.
- **Local network access:** the API on port 7331 is plain HTTP with a bearer key. See [API exposure](#api-exposure).

Use paired computers and local network access on a network you trust. Settings → **Your data stays private** states the same in the app.

## What can leave the machine

Nothing leaves because the daemon started. Traffic is sent only when a feature that talks to the network is used:

| Action | Where data goes |
| --- | --- |
| Install or update llama.cpp | `api.github.com` and the GitHub release download for `ggml-org/llama.cpp` |
| Search or install a Hugging Face model | Hugging Face Hub |
| Install a model from a URL | The host in that URL |
| Places and directions (`places.search`, `places.details`, `maps.route`, `maps.distance`) | OpenStreetMap's public services, or your own (see `places_*` in configuration): Nominatim with the place or address asked about, Overpass with the kind of place and the point searched around, and the router with the two points. They run only when the profile allows the Internet capability. Each request is recorded; a repeat within the hour is answered from memory and sends nothing. |
| Browser (`browser.*`) | The sites you or the assistant open, and the resources their pages load, from an isolated browser with a fresh profile. Nothing on this computer or the local network can be reached. Each page opened and each download is recorded. Clicking, typing, and downloading ask first. |
| Web search and page reads (`internet.search`, `internet.open`) | DuckDuckGo's public HTML search, then the pages opened. They run only when the profile allows them. Only public addresses are opened: a page on this computer or the local network, such as the daemon's own API or a router's admin page, is refused, even when another page redirects to it. A repeat within 15 minutes (searches) or 30 minutes (pages) is answered from the cache and sends nothing. |
| Connected services (GitHub, Home Assistant, Email, Calendar) | That service, with the credential you stored, when one of its tools runs. Email and Calendar talk only to the mail and CalDAV servers you entered. |
| MCP tool sources | That server, when one of its tools runs |
| Database and web API knowledge | The database or URL you connected, when it is refreshed |
| First training run | The `astral-sh/uv` GitHub release, Python builds uv fetches, and pinned packages from PyPI |
| Training a base model the first time | Hugging Face Hub, for the base model's training weights. Your examples are not uploaded. |
| Training on a paired computer | That computer receives the training examples and the AI's instructions over Bifrost, and returns the adapter. It deletes its copy when the job ends. |
| First code run (`code.execute`) | PyPI, for numpy, pandas, matplotlib, and openpyxl. The code itself has no network. |
| First use of speech (`speech.transcribe`, `speech.synthesize`, Read aloud) | PyPI, for faster-whisper and Piper, then Hugging Face for each Whisper model and Piper voice the first time it is used. The audio and text stay on this computer. |
| Image and video generation setup | GitHub, for the pinned stable-diffusion.cpp release (and, for video, the TAEHV decoder), and Hugging Face, for the model's files. Only when you set it up. Prompts, images, and clips stay on this computer. |
| First scanned PDF in Knowledge | PyPI, for the text-recognition packages (about 110 MB). The PDF itself is read on this computer. |
| A chat placed on a paired computer | That computer receives the prompt and context over Bifrost |
| An image or speech tool run on a paired computer | That computer receives the tool's arguments, such as the prompt, and the chat file it needs, such as the image to edit or the audio to transcribe, over Bifrost. It returns the result and keeps nothing. Each job is recorded. |
| Email, push, and webhook notifications | Your SMTP server, the ntfy server (ntfy.sh or your own), or the webhook address, with each notification's title and text; push to ntfy.sh sends only a generic notice unless you choose full content. Only for destinations you add, and only for the categories and severities you choose |
| Community ratings | Only when you choose. With **Show community ratings** on (off by default), the public ratings summary is downloaded once a day from the ratings service, or from GitHub when it cannot be reached, while you browse models; nothing about you is sent. A rating is sent only when you tick **Share this rating with the community**: the stars and reasons, the model, the computer's class (such as Apple M4 Max with 32–64 GB of unified memory), a random rating ID, and the app version. With **Include how it runs here** also ticked, the model's median speed and time to first token on this computer, how many of its starts worked, whether it crashed or ran out of memory, and how much context it used; the dialog shows the numbers first. Never prompts, chats, file names, the computer's name, your user name, or an address; the service does not store IP addresses. Each download, rating shared, and rating withdrawn is recorded. |
| External server (`external-openai`) | Only for a chat where you choose one of its models: the prompt and conversation, with the API key you stored, to the base URL you set. Auto, fallback, offline profiles, and chats using memories or knowledge marked This computer only never send anything there. Listing its models sends the API key to `/v1/models`. Each chat is recorded. |
| Bifrost discovery and pairing | Other computers on the local network, or the static peers you listed |
| Joining a network (`yggctl join`) | The computer that made the join command: this computer's name, ID, public key, and Bifrost address. The token itself is never sent; only a proof made from it. Leaving tells each paired computer. Both are recorded. |
| LAN API | Any client that can reach port 7331 after you turn on local network access |

## What left this computer

Each run records what it sent off this computer: the query of each web search, the address of each page read, the prompt and context sent to a paired computer (or the training examples), a chat sent to an external server, and what a connected service was asked. Long text, such as the body of a comment posted to GitHub, is left out of the record.

Settings → **What left this computer** lists the records, newest first, and counts them for the last 30 days. `GET /api/v1/egress` returns the same list. The records are run records, so the retention period and **Delete run records now** apply to them.

## Keeping data on this computer

Memories and knowledge sources can be marked **This computer only**. A turn that uses one runs on this computer, even when placement would have chosen a paired computer, and the answer's steps say so.

Text recognition for scanned PDFs, meaning search with an embedding model, and training on Apple Silicon run on this computer. Training on a paired computer sends the examples there, and the plan shows which computer before you start.

## Personalization and permissions

Personalization (Settings → **Personalization**) shapes how answers look: length, tone, format, units, and two short notes about you. It never grants a permission. A personalization note or a memory that tries to, such as "you can always push without asking", is refused. What a tool may do comes only from profiles and Settings.

## LAN behavior

- Port 7331 stays on `127.0.0.1` until local network access is turned on (API Access → **Local network access**).
- Discovery defaults to on. Bifrost (port 7332) is then bound on all interfaces so peers can connect.
- mDNS advertises the node on the local link.
- Pairing routes on port 7332 answer before a peer is trusted. Other Bifrost routes require a token from a paired node.

## API exposure

On the default loopback bind, the control API and the OpenAI-compatible API do not require a key. That is safe only while the port is not reachable from other machines.

Any other bind requires `Authorization: Bearer YOUR_API_KEY` on `/api/v1/*` and `/v1/*`. The daemon will not start that listener unless a key exists. Enabling local network access stores `0.0.0.0` and refuses the change when no key exists. The new socket applies the next time the daemon starts. The key check follows the configured host, so it applies as soon as the setting is saved.

The Docker image listens on `0.0.0.0`. Set `TOSKAR_API_KEY` or the process exits before it accepts connections. See [API](api.md).

An API key on plain HTTP does not encrypt traffic. It limits who can call the API. TLS or mTLS for remote access is not implemented.

## Logging

Logs are JSON on stdout and in `logs/daemon.log`. They include startup fields such as the node id, data directory, and listen addresses. They are not shipped anywhere by this code.

## Secrets

API keys use the prefix `ygg_`, and join tokens `ygj_`. Both stay after the rename to Toskar: existing keys and paired computers rely on them, and secret scanners already recognize them. The database stores a bcrypt hash and a prefix. The secret is returned once when the key is created or rotated and is not written to disk. Node certificates and pairing material live under `secrets/`. Do not commit a data directory, and do not attach `secrets/` to a bug report.
