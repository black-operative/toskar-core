# MCP

Yggdrasil speaks the [Model Context Protocol](https://modelcontextprotocol.io) both ways:

- **Tool sources.** Add an MCP server and the AI can use its tools in every chat and automation.
- **Yggdrasil as a server.** Apps such as Claude Desktop, Claude Code, Cursor, and VS Code can ask your local AI and search your connected knowledge.

Neither needs a config file. Both use the same tool registry, permissions, and credential handling as the built-in tools and connected services.

## Adding tools

Open **Tools → Add tools**. There are four ways in, and each ends the same way: Yggdrasil connects, lists the tools, and says what it found ("ready with 14 tools: 10 that read, and 4 that change things and ask you first").

| Way | What you do |
| --- | --- |
| Gallery | Pick an entry and answer at most one or two questions: a folder, a key, or nothing. Services such as Notion, Linear, Jira and Confluence, and Sentry open a sign-in window instead. |
| Paste | Paste whatever a server's instructions say. That can be JSON for Claude Desktop, Cursor, VS Code, Windsurf, Gemini CLI, LM Studio, or Zed, a fragment of it, one server object, a web address, a command line, or a `claude mcp add` line. Placeholders such as `<your-api-key>` or `YOUR_TOKEN_HERE` become fields to fill in. |
| From your other apps | Servers you already set up in Claude Desktop, Claude Code, Cursor, VS Code, Windsurf, Gemini CLI, or LM Studio on this computer. Yggdrasil reads them from the app again when you add one, so their secrets never pass through the browser. |
| Custom | A command to run, or a web address, with environment variables or headers. |

Nothing is stored if the server cannot be reached. The error says what to do: install Node.js or uv, check a key, or try again because the first start downloads the server. The exception is a service that needs a sign-in. It is kept, marked **Sign in to use it**, so the sign-in can finish it.

`npx mcp-remote <address>` entries, which apps without remote support use to reach a web server, are turned into a direct connection. Yggdrasil's own sign-in is used, and no Node.js process runs.

### The gallery

| Entry | Runs | Needs |
| --- | --- | --- |
| Folders | on this computer (`@modelcontextprotocol/server-filesystem`) | Node.js, the folders to share |
| SQLite database | on this computer (`mcp-server-sqlite`) | uv, the database file |
| PostgreSQL | on this computer (`postgres-mcp`, read-only mode) | uv, the connection address |
| Browser | on this computer (`@playwright/mcp`) | Node.js, Google Chrome |
| Brave Search | on this computer (`@brave/brave-search-mcp-server`) | Node.js, a Brave Search API key |
| Notion, Linear, Jira & Confluence, Sentry | on the web | a sign-in |
| GitHub (all tools) | on the web | a personal access token |
| Context7 library docs, DeepWiki, Microsoft Learn | on the web | nothing (Context7 takes an optional key) |
| Time zones | on this computer (`mcp-server-time`) | uv |
| MCP test server | on this computer (`@modelcontextprotocol/server-everything`) | Node.js |

An entry whose program is missing says so ("Needs uv") and links to the installer.

## Using them

Tool source tools join the catalog as `<source>.<tool>`, such as `linear.list_issues`, with `source` `mcp:<source>`. They then work like connected services:

- **Selection.** Huginn offers a source's tools when a message names it or its subject. Gallery entries come with their subjects, such as tickets and backlog for Linear. You can add your own words. **Offer it in every chat** offers a source with every request.
- **Permissions.** Tools that only read run without asking. Tools that change something ask first. A server's own `readOnlyHint` and `destructiveHint` annotations decide which is which. Without them, names such as `get_…`, `list_…`, and `search_…` read. You can switch **Ask first** for any tool, and turn any tool off.
- **Untrusted data.** What a tool returns is data, not instructions (§58). After a turn reads it, a tool that changes something asks first even if it is allowed. Links in results become the answer's sources.
- **Results.** The model sees the text, small structured data, and links. Images and audio are left out with a note, and long text is cut at 24,000 characters.
- **Resources.** A server that offers resources gets two more read tools, `<source>.list_resources` and `<source>.read_resource`.
- **Prompts.** A server's ready-made prompts are listed on its card. **Start a chat with it** fills one in and opens a chat with it ready to send.
- **Roots.** A server on this computer is told its roots: the folders it was given. The Folders server keeps to them.
- **Asking the AI.** A server may ask the AI to write something as part of a tool (MCP sampling). This is off until you turn on **Let it ask your AI for help**. It then uses the model Auto picks.

## Running them

A server on this computer starts when one of its tools is first needed. It stops after 10 minutes unused, the way models unload. Its tool list is kept, so restarting Yggdrasil starts nothing. A server that changes its tool list while running is listed again.

Yggdrasil looks for `npx`, `uvx`, `docker`, and other programs on `PATH`, and also in the usual install folders: Homebrew, `~/.local/bin`, nvm, Volta, Bun, and Cargo. This means a daemon started by launchd or systemd still finds them. A server gets only the environment it needs to run, such as `HOME`, `PATH`, the locale, and proxy settings, plus the variables you gave it. Anything else in Yggdrasil's environment stays out. Stopping a server also stops the processes it started.

Remote servers use Streamable HTTP. A server that only speaks the older HTTP+SSE transport is detected and used that way. A server that forgets its session is reconnected, and the call runs again.

Each card has a log with the server's own output and log messages, when it started and stopped, and why a call failed.

## Credentials

Keys, tokens, passwords, connection strings with a password, and every header are kept in the `secrets` directory, in `mcp-<source>.json`, never in SQLite. A variable counts as secret by its name (`…KEY`, `…TOKEN`, `…SECRET`, `…PASSWORD`, `DATABASE_URI`, and so on) or by its value (`sk-…`, `ghp_…`, `github_pat_…`, `postgres://user:password@…`). The API shows a stored secret as its last four characters. Results, errors, and logs are scrubbed of every secret value before the model or the page sees them. The model never sees a credential: a tool call carries only the model's arguments.

### Signing in

Services that use MCP's OAuth sign-in are added by signing in in a browser window:

1. Yggdrasil finds the service's sign-in from its protected resource metadata (RFC 9728) and authorization server metadata (RFC 8414). Services from before RFC 9728 use their address's `/authorize` and `/token`.
2. It registers itself as an app (RFC 7591), unless you gave a client ID under Custom.
3. You sign in and choose what Yggdrasil may use. The code is exchanged with PKCE (S256) and a resource indicator (RFC 8707).
4. The browser returns to `/mcp/oauth/callback` on Yggdrasil's own address. A one-time state value, valid for 15 minutes, protects it. The window closes, and the card shows the tools.

Access tokens are refreshed before they expire. **Sign out** forgets the tokens.

Calls to a tool source on the web are recorded under Settings → What left this computer, as connected services are. A tool source on this computer is not sent anything by Yggdrasil, but the server itself may reach its service; Brave Search does.

## Yggdrasil as an MCP server

`/mcp` on the API's address is an MCP server over Streamable HTTP. It offers three tools:

| Tool | What it does |
| --- | --- |
| `ask_local_ai` | Answers a prompt with a local model: Auto, or a model id from `list_local_models`. Connected knowledge is used. Tools only read, and memories are used only when the key always allows them. |
| `list_local_models` | Auto, the installed chat models, and deployed specialized AIs. |
| `search_my_knowledge` | Passages from connected knowledge (Mimir) that match a query. |

**API Access → Use Yggdrasil in other AI apps** gives the settings for Claude Desktop, Claude Code, Cursor, VS Code, and other apps, ready to copy. Apps that start a program, such as Claude Desktop, use `yggctl mcp`. It is a bridge that passes each message to `/mcp`. `TOSKAR_URL` sets the address and `TOSKAR_API_KEY` the key. The settings it gives still use `YGGDRASIL_URL` and `YGGDRASIL_API_KEY`, which every version of `yggctl` reads. If Yggdrasil is not running, the app is told so.

`/mcp` is checked like `/v1`. On this computer a key is optional. When the API is open to the network a key is required, and the key's permissions for knowledge, memory, and tools apply. A request that carries an `Origin` header from any site other than Yggdrasil's own page is refused, so a web page cannot use your local AI through your browser.

## API

| Method | Path | |
| --- | --- | --- |
| GET | `/api/v1/mcp/servers` | Tool sources, with status, tools, and masked values |
| POST | `/api/v1/mcp/servers` | Add one: `{"preset", "values"}`, `{"spec", "values"}`, or `{"import": {"app", "name"}}`. Also `redirect_base` for a sign-in. Returns `server` and, when a sign-in is needed, `sign_in_url` |
| GET, PATCH, PUT, DELETE | `/api/v1/mcp/servers/{id}` | Read; change settings (`enabled`, `allow_sampling`, `always_offer`, `keywords`, `policies`); change how it is reached and check it (`{"spec", "values"}`, blank secrets kept); remove |
| POST | `/api/v1/mcp/servers/{id}/check` | Connect again and list the tools |
| POST | `/api/v1/mcp/servers/{id}/sign-in`, `/sign-out` | Start a sign-in (returns `url`), or forget it |
| GET | `/api/v1/mcp/servers/{id}/logs` | Recent log lines |
| GET, POST | `/api/v1/mcp/servers/{id}/prompts`, `/prompts/{name}` | List prompts, or fill one (`{"arguments"}`, returns `text`) |
| GET | `/api/v1/mcp/servers/{id}/resources` | List resources |
| GET | `/api/v1/mcp/gallery` | Gallery entries, with `missing` programs and `added` sources |
| GET | `/api/v1/mcp/import` | Servers in other apps on this computer, secrets hidden |
| POST | `/api/v1/mcp/parse` | Read pasted text (`{"text"}`) into specs and what each still needs |
| GET | `/api/v1/mcp/share` | The address and command other apps use |
| POST, DELETE | `/mcp` | Yggdrasil's MCP server |
| GET | `/mcp/oauth/callback` | Where a sign-in returns |

Tool sources are stored in the `mcp_servers` table (migration 020) without secret values.

## Not yet

- A server's own forms mid-call (elicitation) are declined.
- Server-to-client messages outside a call's reply stream (the optional GET stream) are not listened to. Tool list changes from remote servers are picked up on **Check**.
- Tool sources run on the computer that added them. They are not placed on paired computers.
