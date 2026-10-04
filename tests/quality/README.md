# Quality test set

A fixed set of representative requests, each with the behavior it must have
(spec §64). Routing, retrieval, planning, verification, approvals, and history
all change answers; these cases check that changes keep the behavior people
rely on.

`cases.json` holds the cases. Each has a `message` (and optional `history`
and `setup`: knowledge files and tool policies), what it checks (`what`), and
`expect`:

| Check | Meaning |
| --- | --- |
| `effort` | The run's effort, such as `Fast` |
| `plan`, `min_workers` | Whether it was worked through in parts, and how many |
| `lookup` | Whether the web was looked up first |
| `no_tools_run`, `not_run` | No tool, or none of these tools, ran |
| `approval_for` | These tools asked first (stub only: a real model may not try) |
| `sources` | The answer cites these kinds, such as `knowledge` or `web` |
| `verified`, `notice_matches` | The answer was checked; the notice says this |
| `steps_match` | A "What I did" step matches |
| `prompt_contains` | The model was sent this text (stub only) |
| `answer_matches` | The answer matches; `answer_real_only` skips it for the stub |
| `no_false_claims` | An answer that claims a change nothing made carries a notice |

`stub` scripts what the stub model says, one reply per model call. `stub_only`
cases check scripted behavior a real model may not produce.

## Running it

- **Stub model:** `make quality`, also part of `go test ./...` in CI. It runs
  in-process with the web replaced by fixed pages, so it needs no model and no
  network.
- **Real models:** start a daemon with models installed, then run
  `make quality-real`, setting `TOSKAR_QUALITY_URL` if the daemon is not at
  `http://127.0.0.1:7331`, and `TOSKAR_QUALITY_KEY` if it needs an API key.
  Each case adds a profile and knowledge, which are removed afterwards, and a
  chat, which is kept so a failure can be read.
- **On a schedule:** `.github/workflows/quality.yml` runs the real-model set
  weekly on a self-hosted runner labelled `yggdrasil-models`, when the
  repository variable `QUALITY_DAEMON_URL` is set.

Add a case when a change fixes a behavior, so it stays fixed.
