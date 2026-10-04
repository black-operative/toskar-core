# Troubleshooting

Start with the answer itself. Under each answer, **What I did** lists the model that answered and why, the searches and pages read, the knowledge and memories used, and anything that went wrong. In advanced mode, **Run details** adds timings, tokens, tools, and the computer that ran each part. Diagnostics shows the health of every part of Toskar and what it can do right now.

## The web UI does not load

The daemon serves the UI only when it finds a directory containing `index.html`. Packages and archives include it. From a clone, build it first:

```bash
make start
```

Then open `http://127.0.0.1:7331`. `GET /api/v1/health` still works if the UI was not built.

## Port already in use

The API defaults to `7331` and Bifrost to `7332`. Stop the other process, or change `api_port` and `internal_port` in `config.json` in the data directory and restart. A second daemon needs its own data directory (`-data-dir`) as well.

## Where are the logs?

`toskarctl paths` prints the log directory. The daemon appends JSON to `logs/daemon.log` and to standard output. With the deb or rpm package, the service's output is in `journalctl -u toskar`. llama-server writes its own log next to `daemon.log`.

Remove API keys, tokens, personal data, private URLs, and credentials before you paste a log into an issue.

## A model will not start

- The file must be a GGUF. `POST /api/v1/models/install-from-url` rejects other URLs.
- llama.cpp must be installed (Models offers it, or `POST /api/v1/runtimes/llamacpp/install`). Installing needs a network path to GitHub.
- A model whose `llama-server` exits while loading, for example a damaged file or one that does not fit, fails at once with the reason.
- On Windows the installer uses the CPU llama.cpp build (`bin-win-cpu-x64`). A GPU listed in hardware detection does not by itself select a GPU build.
- Out of memory: close other apps or choose a smaller model. While training uses the computer, the error says so, with about how long training has left.

## Auto chose a model I did not expect

The first line of **What I did** says which model answered and why: a quick question stays on a model that is already loaded, coding goes to a coding model, current information needs a model that can use tools, and a question about your files or knowledge prefers a larger model. A question about what a specialized AI was trained for goes to that AI. Choose a model in the composer instead of Auto to keep one model for the chat.

Embedding and reranker models never answer chats. They help Knowledge search.

## The answer did not use the web

Web look-ups need Internet allowed in the profile. A plain question is answered without tools; ask for a search ("look up…", "search for…") or ask about something current. The same search within 15 minutes, or page within 30 minutes, is answered from the cache. Settings → What left this computer → **Delete run records now** clears it.

## The answer did not use my knowledge or memories

- **Knowledge:** a chat uses the knowledge sources of its profile, and those you attach to it. Knowledge → **Try a question** shows which passages a question finds. Without an embedding model, search matches words, so a question phrased differently may find nothing. Install an embedding model (Nomic Embed Text v1.5 is in the catalog) to search by meaning.
- **Memories:** memory may be off for this chat, or in Settings. API requests use memories only when they ask (`yggdrasil.memory: true`) or their API key is set to always.

## A knowledge source failed

The source shows **Failed** with the reason.

- **A file or folder:** check that it still exists and is a supported type.
- **A scanned PDF:** the first one installs text recognition (about 110 MB from PyPI), which needs a network connection. A copy of Toskar from the Mac App Store reads scanned PDFs only if the app includes text recognition.
- **A database or web API:** the reason names the connection or HTTP error, with credentials removed. Search keeps using the last data that was fetched. Fix the settings and choose **Reindex**.

A scanned PDF attached to a chat is refused: connect it on the Knowledge page instead, which reads scanned pages once.

## An answer says "Nothing was changed"

The answer said it did something, such as committing or sending, but no tool that changes things ran. Ask again, and check that the profile allows the tool. In advanced mode, Tools shows each tool's permission.

## An automation did not notify, or skipped a tool

- History says why a notice was or was not sent, such as a price that was not below the amount.
- A tool the automation was not approved to use is skipped, the run continues, and an **approval** notification names the tool. Edit the automation to approve it.
- An automation that runs out of memory twice in a row is paused, and its notification says why. Choose a smaller model or another time, then resume it.
- Automations wait for a chat to finish, and for 20 seconds after one, before they load a model.

## Training is not available

- Training needs a Mac with Apple Silicon or a computer with an NVIDIA GPU. The plan shows each computer and why it can or cannot train.
- Another computer can train for this one: pair it, and choose it in the plan's Review step.
- A copy of Toskar from the Mac App Store cannot run the Python it would download. It trains on a paired computer, unless the app includes the training environment.
- Training waits for chat, automations, and benchmarks before it starts, and the job shows what it is waiting for.

## Export as a GGUF file failed

The export needs about the base model's size in free disk space, and the revision's adapter and base model on this computer. It also needs `llama-export-lora`, which comes with llama.cpp from the Runtimes installer but not with the Mac App Store app.

## Another computer does not appear

- Discovery defaults to on. Both daemons need to be running.
- Bifrost must be reachable on port 7332. On macOS, allow Local Network for the process.
- mDNS does not cross every Docker or VPN boundary. Set `TOSKAR_STATIC_PEERS` or `static_peers` to `host:7332`.
- After you enable discovery on a daemon that already bound Bifrost to loopback, restart it.

## Pairing fails

Approve the offer on the second computer. Protected Bifrost routes answer `401` until that pairing is stored. Health and pairing routes are the ones that answer before trust exists.

## A connected service or MCP tool source stops working

- **Connected services:** Settings → Connected services → **Check** tests the stored credential. A blank secret field keeps the stored one when you change other values.
- **MCP tool sources:** a source that signs in with a browser says "needs you to sign in again" when its sign-in expires. Open Tools and choose **Sign in**. Tools also shows each source's log.

## API calls return 401 or 403

- **401:** the daemon is not bound to loopback, and the request has no `Authorization: Bearer YOUR_API_KEY` header. `/api/v1`, `/v1`, and `/mcp` all use that check. On the default loopback bind, none requires a key.
- **403:** the request asked for something its key does not allow, such as memory, a tool, or a placement. The message says what was refused. API Access shows each key's permissions.

Do not paste a real key into a bug report, and do not put one in a URL.

## The API says no model is installed

`/v1/chat/completions` does not download a model. Install one, then use `auto`, a profile, or a model id.

## Toskar uses more and more memory

Note how long Toskar has been running and what it was doing (chats, automations, models loading and unloading, tool sources). Then:

1. Export a diagnostic bundle (Diagnostics → **Export diagnostics**). It includes `runtime.json` (goroutines, heap size, collections) and `profiles/goroutines.txt` and `profiles/heap.pb.gz`, which show where memory and background work go. They hold function names, counts, and sizes, not prompts or files.
2. If it keeps growing, export a second bundle an hour later. Two snapshots show what grew.
3. Attach both to an issue.

To look yourself, start the daemon with `TOSKAR_PPROF=127.0.0.1:6060` and run `go tool pprof http://127.0.0.1:6060/debug/pprof/heap`, or open `http://127.0.0.1:6060/debug/pprof/goroutine?debug=1`.

Stopping a model, a tool source, or Toskar itself ends every process it started, including helpers such as the `node` process `npx` runs. If one is left behind, note its command line (`ps -ef | grep llama-server`) in the issue.

## Reset

`POST /api/v1/settings/reset` (Settings → **Reset application**) clears application state. Pass `delete_models=true` only when you also want model files removed. This is local and not reversible.
