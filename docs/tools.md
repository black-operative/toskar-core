# Tools

Toskar has one tool registry. Chat, profiles, and the Tools screen all use it. Built-in tools, connected services, and MCP tool sources all run through it. There is no second execution path.

## Architecture

`internal/tools/catalog.go` lists every built-in tool: id, capability, JSON argument shape, default permission, and whether it is read-only.

The registry in `internal/tools/registry.go` runs tools. `allow` runs immediately. `ask` waits for the in-chat approval. `deny`, or a tool missing from the profile, never runs. A globally disabled tool is rejected before a prompt.

Internet search and page open live in `internal/tools/internet`. `SearchProvider` and `PageFetcher` can be replaced without changing the chat loop. The default search provider reads DuckDuckGo HTML results. Opening a page returns title and text, with scripts, navigation, and very long pages trimmed. `internet.open` reads only the public internet: an address on this computer or the local network (loopback, private ranges such as 192.168.x.x and fc00::/7, link-local such as 169.254.169.254 and fe80::/10, carrier-grade NAT 100.64.0.0/10, unspecified, and multicast) is refused with "this address is on your own network or computer, which web reads cannot open". Every address a name resolves to is checked, and the check runs again on each connection, so a redirect or a name that changes its answer cannot reach the daemon's own API, a router, or a cloud metadata service. The check lives in `internal/netguard`. Page reads do not use a proxy from the environment.

The simple orchestrator waits for a model generation, classifies it, and only then shows text. A generation can contain assistant text, one tool call, or both. Tool calls, tool results, function-call syntax, and known model control tokens never become the assistant message. Ordinary JSON and code samples stay in the answer. The call is executed, the result goes back to the model, and the loop continues up to the turn's tool-call budget: 3 calls on Fast, 10 on Balanced, and 16 on Thorough, or a profile's own `max_tool_calls` (1–50). Invalid tool JSON is not executed. The model may try again twice, then the turn ends with whatever readable text remains. A tool failure is a short status such as "Web search failed," not a stack trace. "Used N tools" expands the calls without raw arguments.

## Which tools a turn is offered

Each turn is offered only the tools it needs. Huginn picks tool groups from the kind of request and from cues in the message: web search for current questions, files for a file name or folder, the shell for "run" or "install", Git for "commit" or "branch", a connected service when the message is about it. A plain question gets no tools. The groups are then limited to what the profile allows.

A call to a tool that was not offered is refused, and the model is told which tools it has, so it cannot widen its own tools. Tool ids have capability aliases: `web.search`, `web.open`, `files.read`, `files.write`, `files.search`, and `shell.run` reach the built-in tools. A short answer that only writes out a call, such as `files.search {"query": "x"}`, is taken as the call when the tool was offered.

Arguments are checked against the tool's schema before it runs. Safe repairs are made, such as `"7"` for a whole number. A call with an argument of the wrong type is refused, naming the argument, so the model can call again.

Each call has a time limit: web 45 s, files 30 s, Git 90 s, the shell 2 minutes. A failed call reports a `kind`: `timeout`, `cancelled`, `denied`, `not_offered`, `invalid`, or `failed`.

When a question needs current information and the profile allows web search without asking, Toskar searches and reads the best page before the model answers, so a small model answers from the page instead of choosing a tool. A repeat search within 15 minutes, or page within 30 minutes, is answered from the cache.

## Making files

`files.create`, allowed by default in the built-in profiles, saves a file the user can download. It writes only to Toskar's file store, never to your folders. When a message asks for a file, Toskar has the model write only the contents and saves the file itself.

| Asked for | Written from | Notes |
| --- | --- | --- |
| Word document (`.docx`) | Markdown | Headings, bold, italic, code, lists, tables, code blocks, quotes, and rules |
| PDF (`.pdf`) | Markdown | A4, the same elements, page numbers on longer documents. It uses the PDF standard fonts, so characters outside Western European text show as `?` |
| Spreadsheet (`.xlsx`) | CSV text | A line `## Sheet: Name` starts another sheet; a cell starting with `=` is a formula, such as `=SUM(B2:B9)`; numbers are stored as numbers |
| Markdown, text, HTML, JSON, CSV, code | Itself | |

The capability names `document.create`, `pdf.create`, and `spreadsheet.create` reach `files.create` in that format, so a name without an extension still becomes that kind of file.

`spreadsheet.analyze` summarizes a spreadsheet (`.xlsx`, `.csv`, or `.tsv`) attached to or made in the chat, by name or id. For each sheet it returns the number of rows and the first five. For each column it returns the type (number, date, text, or empty), how many cells are filled, and, for numbers, the minimum, maximum, average, and total; for other columns, how many distinct values there are and the most common ones. It reads up to 10 sheets, 50 columns, and 200,000 rows, so an answer can use a whole file that would not fit in the prompt. It is offered when a message mentions a spreadsheet, CSV, Excel, a workbook, or a sheet.

## Browser

The browser tools use web pages that need JavaScript or interaction, in an isolated browser. For simply reading a page, `internet.open` is faster. They are the Browser capability, and are not in offline profiles.

| Tool | What it does | Default |
| --- | --- | --- |
| `browser.open` | Opens an address and returns the page's text and its links, buttons, and fields, each with a `ref` | Allow |
| `browser.extract` | Reads more of the open page: its whole text, its links, or its tables | Allow |
| `browser.screenshot` | Attaches a picture of what the page shows | Allow |
| `browser.click` | Clicks a link or button by `ref` and returns the page after | Ask |
| `browser.type` | Types into a field by `ref`, and with `submit` presses Enter | Ask |
| `browser.download` | Brings a file the page links to into the chat, up to 25 MB, keeping the page's sign-in | Ask |
| `browser.close` | Closes the chat's browser and forgets its pages | Allow |

- **The browser:** a Chromium-based browser already on this computer (Google Chrome, Microsoft Edge, Chromium, or Brave), run headless. Nothing is downloaded. Without one, the tools say what to install. The Mac App Store build cannot start one.
- **Isolation:** each chat has its own browser with a fresh, temporary profile. It never uses your own profile, sign-ins, saved passwords, or extensions, and it saves nothing to your computer. A chat's browser closes after 10 idle minutes, with `browser.close`, or when Toskar stops, and its profile is deleted. At most three run at once.
- **This computer and the local network are off limits:** every request a page makes, including redirects and resources, is checked as it happens. Requests for loopback, private, link-local, and other non-public addresses, and for names such as `localhost` or `*.local`, are refused. Downloads check again at connect time.
- **Sensitive fields:** password, payment, and one-time-code fields are marked on the page and refused for typing; the person enters those themselves. Pages are treated as data, not instructions, so a turn that used them asks before changing anything.
- **Privacy:** each page opened, including where a click or a search leads, and each download is recorded in What left this computer.

## Places and directions

Four tools answer questions about places with [OpenStreetMap](https://www.openstreetmap.org/) data. They are part of the Internet capability, level 2, allowed by default, and not in offline profiles. They are offered when a message asks for something near somewhere, directions, how far, or opening hours.

| Tool | What it does | Service |
| --- | --- | --- |
| `places.search` | A kind of place near somewhere, such as `{"query": "coffee", "near": "Juneau, AK"}`, nearest first within 2 km, then 10 km if there is nothing closer (`radius_m` up to 25 km). Otherwise places and addresses by name. Up to 10 results with addresses, distances, opening hours, phone, website, and an id. | Overpass for kinds of places, Nominatim for names |
| `places.details` | One place by its id from `places.search`, such as `node/3430732134` | Nominatim |
| `maps.route` | Turn-by-turn directions, driving (default), walking, or cycling, up to 40 steps | Nominatim for the places, then the router |
| `maps.distance` | Straight-line and route distance and minutes; when there is no route, the straight line still answers | Nominatim, then the router |

- **Places:** `near`, `from`, and `to` are names, addresses, or `lat,lon`. The user's location is not known; the model uses the place named, or where About me in Personalization says they live.
- **Distances** come in meters, kilometers, and miles, so answers follow the units chosen in Personalization.
- **Attribution:** results carry their attribution, "© OpenStreetMap contributors (ODbL)", and an openstreetmap.org link for each place.
- **Fair use:** requests identify Toskar, Nominatim requests are spaced at least a second apart as its usage policy asks, and a repeat within the hour is answered from memory (the Places and routes cache). For heavy use, point `places_geocoder_url`, `places_overpass_url`, and `places_router_url` at your own servers.
- **Privacy:** each request is recorded in What left this computer as Maps and places.

Recognized kinds include cafes, restaurants, fast food, bars, pharmacies, hospitals, clinics, fuel and charging stations, parking, ATMs, banks, post offices, libraries, toilets, supermarkets, bakeries, hardware stores, bookshops, hotels, museums, viewpoints, campsites, parks, playgrounds, and gyms. Anything else is searched by name.

## Running code

`code.execute` runs Python the assistant writes, for calculations, data analysis, and charts. It asks first by default (level 4), and the Run code capability turns it on or off in a profile. It is offered when a message asks to calculate, analyze, chart, or run code.

It runs only inside the operating system's sandbox. There is no fallback: where there is no sandbox, the tool is unavailable and says why.

| System | Sandbox | What the code can do |
| --- | --- | --- |
| macOS | `sandbox-exec` | No network. Reads the system and the Python environment, but not users' folders (`/Users`, `/Volumes`) apart from its own working folder. Writes only to its working folder. |
| Linux | bubblewrap (`bwrap`, install it with your package manager) | New namespaces with no network. The system folders and Python environment are read-only, the working folder is the only writable place, and a private `/tmp`. |
| Windows, or Linux without bubblewrap | none | The tool is unavailable |

- **Environment:** Python 3.12 with numpy, pandas, matplotlib, and openpyxl, in a managed environment (`runtimes/python/envs/code`) installed from PyPI the first time code runs.
- **Files in:** `files` lists files from the chat by name, such as `["sales.xlsx"]`. They are copied into the working folder for the code to read.
- **Files out:** files the code saves there (`.png`, `.jpg`, `.svg`, `.pdf`, `.csv`, `.tsv`, `.xlsx`, `.json`, `.txt`, `.md`, `.html`; up to 10, 25 MB each) are attached to the answer. Others are listed as skipped.
- **Limits:** 90 seconds per run and 32 KB each of printed output and errors. A memory limit of 4 GB of address space applies on Linux. Each run gets a new working folder, which is deleted afterwards.

## Speech

Speech runs on this computer: audio and text are not sent anywhere. Both tools are allowed by default and are level 1, and the Speech capability turns them on or off in a profile. Offline profiles keep them.

- **`speech.transcribe`** writes down what an audio file in the chat says, with Whisper (faster-whisper). `file` is the file's name or id; `quality` is `fast` (the default, Whisper base, about 145 MB) or `accurate` (Whisper small, about 480 MB); `language` is a code such as `de`, or empty to detect it. It returns the text, the language, the length, and timed segments. It is offered when a message attaches audio or mentions transcribing, a recording, a voice note, or a podcast.
- **`speech.synthesize`** reads text aloud with Piper and attaches a `.wav` file to the answer. `text` is up to 5,000 characters; `voice` is a Piper voice name such as `de_DE-thorsten-medium`; without one, the voice is the one for the text's language, detected on this computer, or for `language` (a code such as `de`) when given, and English (`en_US-lessac-medium`) for text too short to tell. Read aloud has voices for Arabic, Chinese, Czech, Danish, Dutch, English, Finnish, French, German, Greek, Hindi, Hungarian, Indonesian, Italian, Korean, Norwegian, Persian, Polish, Portuguese, Russian, Spanish, Swedish, Turkish, Ukrainian, and Vietnamese; text in another language, such as Japanese, is refused with `SPEECH_NO_VOICE` rather than read in the wrong voice. `name` names the file. It is offered when a message asks to read something aloud, narrate it, or turn text into speech.
- **Languages:** `GET /api/v1/tools/providers` lists the `languages` each speech provider works in, and `auto_detect` for Whisper, which tells the spoken language itself (99 languages). A call goes to a paired computer whose provider works in its language when this one's doesn't (multilingual spec §20).
- **Read aloud:** answers in the app have a Read aloud button, which calls `POST /api/v1/speech` and plays the result. The audio is kept in the chat like any other file.
- **Audio files:** chats accept `.wav`, `.mp3`, `.m4a`, `.aac`, `.ogg`, `.flac`, and `.webm` (up to 25 MB). They play in the chat, and the assistant is told it can transcribe them.
- **Environment:** faster-whisper and Piper in a managed environment (`runtimes/python/envs/speech`), installed from PyPI the first time speech is used. Whisper models and Piper voices are downloaded from Hugging Face the first time each is used and kept in `runtimes/speech/`.
- **Limits:** one speech job at a time, 15 minutes each, which allows for the first installation and download.

## Images

`image.generate` makes an image from a description, and `image.edit` changes a PNG or JPEG image in the chat from an instruction, such as "make it night" or "remove the car". Both run on this computer with [stable-diffusion.cpp](https://github.com/leejet/stable-diffusion.cpp), so the prompt and the images are not sent anywhere. They are allowed by default and are level 1, and the Images capability turns them on or off in a profile. Offline profiles keep them. They are offered when a message asks to draw, paint, or make a picture, logo, or illustration, or to edit or change an image. An attached image is offered for editing whatever the message says.

**Setup:** image generation needs a one-time setup, on the Tools page, from a chat, or with `POST /api/v1/images/setup`. Until then the tools are unavailable and say how to set them up. Asking for an image in a chat offers the setup there, and makes the image once it is ready (see [guided installation](capabilities.md#guided-installation)). Setup downloads two things:
- stable-diffusion.cpp release `master-929-3f8527a` from GitHub, 17–35 MB.
- A model from Hugging Face.

Each download is pinned to a revision and checked against its SHA-256. A stopped setup resumes where it left off. The recommended model depends on this computer's memory.

| Model | Download | For | What it does |
| --- | --- | --- | --- |
| FLUX.2 [klein] 4B | 5.2 GB | 16 GB of memory | Makes and edits images in four steps |
| FLUX.2 [klein] 4B, high quality | 8.8 GB | 24 GB of memory or more | The same, with more detail |

Each is the [FLUX.2 [klein] 4B](https://huggingface.co/black-forest-labs/FLUX.2-klein-4B) diffusion model (GGUF, Q4_0 or Q8_0), the Qwen3 4B text encoder (GGUF), and the FLUX.2 small decoder VAE. All three are Apache 2.0 and need no account to download.

- **Other computers:** a paired computer with image generation set up makes images for this one, preferring one whose GPU does the work ([tools on other computers](clustering.md#tools-on-other-computers)).
- **Platforms:** macOS on Apple silicon (Metal), and Linux x86-64 and Windows x64 (CPU builds, which are much slower). Elsewhere, and in the Mac App Store build, which cannot run a program it downloads, the tools are unavailable and say why.
- **Generating:** `prompt` is up to 2,000 characters. `width` and `height` default to 1024 and are rounded to multiples of 64, between 256 and 1536 and up to about 1.5 megapixels. `seed` repeats an image, and each result includes its seed. Images are saved as PNG files attached to the answer.
- **Editing:** `file` is an image in the chat by name or id. The result keeps the image's shape, at up to 1024 pixels on the longer side, and is attached as a new file, so the original is kept.
- **Limits:** one image at a time, 20 minutes each.

## Video

`video.generate` makes a short clip on this computer, from a description or from an image in the chat brought to life. It uses the same stable-diffusion.cpp as images, with its own setup. It is allowed by default and level 1, the Video capability turns it on or off in a profile, and offline profiles keep it. It is offered when a message asks for a video, clip, or animation, or to bring an image to life.

**Setup:** on the Tools page under Video generation, from a chat when a request needs it, or with `POST /api/v1/video/setup`. Setup installs stable-diffusion.cpp if image generation hasn't, then downloads the model from Hugging Face, pinned to revisions and checked against their SHA-256.

| Model | Download | For | What it does |
| --- | --- | --- | --- |
| Wan 2.2 TI2V 5B | 8.5 GB | 16 GB of memory | A clip from a description, or from an image |

The model is Wan 2.2 TI2V 5B (GGUF, Q4_K_M) with the umt5-xxl text encoder (GGUF) and the Wan 2.2 VAE, all Apache 2.0. It also includes TAEHV (`taew2_2`, MIT, 23 MB from GitHub), a small decoder that turns the clip into frames in about 2 seconds; the full VAE's decode takes most of an hour on a 24 GB Mac. None of the files needs an account to download.

- **Clips:** `prompt` describes the subject, motion, camera, and style. `seconds` is 1 to 5 (default 2), made at 16 frames a second. The size is 832×480, or the image's shape (480×832 for a tall one, 640×640 for a square one); `width` and `height` change it, within about 832×480 pixels, in multiples of 32. `seed` repeats a clip, and each result includes it.
- **From an image:** `image` is a PNG or JPEG in the chat, by name or id. It becomes the first frame.
- **Output:** clips are WebM (VP8), attached to the answer, where they play.
- **Other computers:** a paired computer with video set up makes clips for this one, preferring one whose GPU does the work. A laptop can ask a workstation.
- **Time:** a 2-second clip takes about 3½ minutes on an M5 Pro with 24 GB. One clip runs at a time, for up to 75 minutes. Stop ends it.

## Descriptors and levels

Every tool, whatever it comes from, has a descriptor (`GET /api/v1/tools/{id}`, and each entry of `GET /api/v1/tools`):
- **What it is:** its `version` and `input_schema` (a JSON Schema of the arguments), and its `outputs` (text, file, image, or audio).
- **Where and how it runs:** `execution` (local, remote, or either), its `requirements` (network, files on this computer, a stored credential, a runtime, a GPU), and `supports` (progress, and cancel, which every tool has because Stop ends its calls).
- **Limits and source:** `timeout_seconds` and `provider` (`builtin`, `connector:<service>`, or `mcp:<source>`).
- **State:** `health`: `ok`, `off` (turned off on the Tools page), or `unavailable` (no provider is running it, such as an MCP source that is down).

Its permission `level` describes what it can affect:

| Level | Meaning | Examples |
| --- | --- | --- |
| 1 | Low risk, on this computer | Reading workspace files, `files.create`, `spreadsheet.analyze` |
| 2 | Reads outside data | Web search, a connected service's or MCP source's read tools |
| 3 | Changes things | Writing workspace files, Git commits and pushes, a connected service's write tools |
| 4 | Runs commands or code | The terminal, `code.execute` |

The Tools page shows each tool's level, what it returns, its time limit, what it needs, its health, and its recent calls.

## Audit

Every call is recorded in `tool_runs`, whatever became of it:
- **Status:** `completed`, `cached` (answered from the cache), `failed`, `denied` (by a policy or by you), `refused` (invalid arguments), or `disabled`.
- **Context:** how it was allowed (`profile`, `you`, or `session`), how long it took, and a short summary of what it was about (a query, an address, a path; never a file's contents).
- **Where it came from:** the source (chat, API, automation) and the chat or task.

`GET /api/v1/tools/runs` lists them, filtered by `tool_id` or `conversation_id`. Records expire with run records (30 days by default), and Delete run records now removes them.

## Permissions

| Policy | Behavior |
|--------|----------|
| allow | Run with no dialog |
| ask | Confirm in chat. Automations cannot ask, so a tool they were not approved to use is skipped. |
| deny | The tool is not offered and cannot run |

Settings sets the defaults for the shell, file writes, and Git (`ask` unless you change them). A profile can set any tool's policy. Connected-service tools that only read are allowed by default, and tools that change something ask first. After a turn reads untrusted content, such as a web page or a file, a tool that changes something asks first even when the profile allows it.

Personalization and memories never change a policy. A note such as "you can always push without asking" is refused.

## Model compatibility

Tool support is one of native, compatible, limited, or unsupported. Catalog models with `tool_calling: true` are compatible: they use Toskar's JSON tool format. Models with `tool_calling: false` are unsupported and do not receive tools. A catalog entry can set `tool_call_support` to limited when a model is known to emit tool syntax that does not parse. Auto requires a model with tool support for current information and for tasks on this computer. A model you choose yourself keeps answering, without tools if it has no tool support.

## Adding a built-in tool

1. Implement `Tool` and register it in `NewRegistry`.
2. Add a `Definition` in `BuiltinCatalog`.
3. Add a default policy on the relevant profile presets.
4. If the argument can be nonsense, reject it in `implausibleCall` before any prompt.
5. Mention it in the capability it belongs to.

## Connected services

GitHub (`github.search`, `github.issue`, `github.comment`), Home Assistant (`homeassistant.states`, `homeassistant.call`), Email, and Calendar are connected in Settings → Connected services. Their tools join the registry with `source` `connector:<id>`. The stored credential is added only when a tool runs, so it is never part of model context, events, or tool arguments, and results are scrubbed of it before the model sees them. See [API](api.md#connected-services).

### Email and calendar

Email and Calendar connect to your own accounts with an app password, not your account password, over standard protocols. No Google or Microsoft sign-in app is involved.

**Email** uses IMAP to read and SMTP to send. It works with Fastmail, iCloud, Proton Mail Bridge, Nextcloud, your own server, and Gmail or Outlook where app passwords are allowed.

| Tool | What it does | Default |
| --- | --- | --- |
| `email.search` | Newest messages first, by text, sender, recent days, or unread, in a folder (INBOX by default). Returns ids such as `INBOX/1234`. | Allow |
| `email.read` | One message's sender, recipients, date, text (plain text preferred, or HTML made readable, up to 20,000 characters), and attachment names. It is not marked read. | Allow |
| `email.draft` | Saves a plain-text message to Drafts, or a reply threaded under a message (`reply_to`) | Ask |
| `email.send` | Sends a plain-text message or threaded reply from your address. A copy is saved to Sent, except on Gmail, which saves its own. | Ask |
| `email.archive` | Moves a message to the Archive folder | Ask |

- **Encryption:** port 993 (IMAP) and 465 (SMTP) use TLS from the start, and other ports must offer STARTTLS. A server on this computer, such as Proton Mail Bridge, may use its own certificate or no encryption.
- **Folders:** Drafts, Sent, and Archive are found by their special-use flags, then by their usual names.
- **No deleting:** nothing is deleted. On a server without MOVE, archiving copies the message and marks the original deleted, without expunging the folder.

**Calendar** uses CalDAV: Fastmail (`https://caldav.fastmail.com`), iCloud (`https://caldav.icloud.com`), Nextcloud (`https://your-server/remote.php/dav`), Radicale, and others. The address can be the server, where calendars are found through the account's calendar home, or one calendar.

| Tool | What it does | Default |
| --- | --- | --- |
| `calendar.search` | Events, soonest first, in a range (default the next week), by text or calendar. Repeating events are listed by occurrence: the server expands them. | Allow |
| `calendar.availability` | Busy times, and free times within working hours (default 09:00 to 17:00) at least 30 minutes long and still ahead. Events marked free don't count. | Allow |
| `calendar.create` | Adds an event with a start and an end or length, or all day | Ask |
| `calendar.update` | Changes an event's title, time, location, or description, keeping attendees, alarms, and everything else. Moving the start keeps its length. Repeating events can't be changed here. | Ask |
| `calendar.cancel` | Removes an event. Invited people are not notified. | Ask |

- **Version checks:** changes send the event's ETag, so an event changed elsewhere since it was read is not overwritten; the tool says to look it up again.
- **Times** are in this computer's time zone unless given with an offset.

Both services join the connected-service rules. The app password is stored apart from the database and added only when a tool runs, and results are scrubbed of it. Messages and invitations are treated as data, not instructions, so a turn that read them asks before changing anything. Each call is recorded in What left this computer. They are offered when a message is about mail (inbox, email, messages from…) or the calendar (meeting, schedule, free, tomorrow…), and the tools that change something only when the message asks for a change.

## MCP tool sources

MCP servers add tools to the same registry. They are listed on the Tools screen with `source` `mcp:<source>`, and they follow the same policies. Toskar is also an MCP server for other apps. See [MCP](mcp.md).

## Diagnostics

Tool start, complete, failure, rejected parse, and protocol sanitation records stay in memory for the process and appear under Diagnostics → Tool activity. Summaries include the query or URL, not file contents or secrets. Automatic model changes are recorded as `chat.model_routed`.
