# Quality test set

A fixed set of representative requests, each with the behavior it must have
(spec §64). Routing, retrieval, planning, verification, approvals, and history
all change answers; these cases check that changes keep the behavior people
rely on.

`cases.json` holds the cases. Each has a `message` (and optional `history`
and `setup`: knowledge files and tool policies), what it checks (`what`), the
`platforms` it runs on (`core`, `phone`, or both; core when absent), and
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
| `no_deflection` | The answer does not send the person off to search, or claim to browse, by `deflection`, instead of answering; with the facts `answer_matches` asks for, a pointer to a site for more is fine (real models only) |

`stub` scripts what the stub model says, one reply per model call. `stub_only`
cases check scripted behavior a real model may not produce.

`web.json` holds the web pages every run reads instead of the live web: a
search gets the results of the first site whose `match` matches it, and a page
is found by its URL. Its facts are fixtures, so an answer that matches them
came from the page, not from the model's memory.

The iPhone app (yeixio/toskar-apps, `mobile/quality`) keeps a copy of both
files and runs the `phone` cases through its on-device chat with the same
expectations and pages, so the phone is held to what core does.

## Running it

- **Stub model:** `make quality`, also part of `go test ./...` in CI. It runs
  in-process with the web replaced by fixed pages, so it needs no model and no
  network.
- **A real model, as a release does:** `scripts/quality-model.sh run` downloads
  llama.cpp (`LLAMA_CPP_TAG`, with `gh`) and Llama 3.2 1B
  (`QUALITY_MODEL_GGUF_URL`), starts llama-server, and runs the cases in-process
  with every model call sent to it (`TOSKAR_QUALITY_MODEL_URL`) and the web
  answered from `web.json`. At least 90% of cases must pass
  (`TOSKAR_QUALITY_MIN_PASS`); `TOSKAR_QUALITY_REPORT` writes every answer to a
  Markdown report. The Release workflow runs this before it publishes, and the
  Quality workflow runs it on pull requests that change how chat decides.
- **Real models on a daemon:** start a daemon with models installed, and with
  `TOSKAR_WEB_FIXTURES=tests/quality/web.json` so its web tools read the
  pages above instead of the internet (it logs a warning when they do), then run
  `make quality-real`, setting `TOSKAR_QUALITY_URL` if the daemon is not at
  `http://127.0.0.1:7331`, and `TOSKAR_QUALITY_KEY` if it needs an API key.
  `TOSKAR_QUALITY_INSTALL=recommended` first installs the models the daemon
  recommends for its hardware. Each case adds a profile and knowledge, which are removed afterwards, and a
  chat, which is kept so a failure can be read.
- **On a schedule or by hand:** `.github/workflows/quality.yml` runs the
  real-model set weekly on a self-hosted runner labelled `toskar-models`, when
  the repository variable `QUALITY_RUNNER` is `docker`. It builds the daemon
  image from the commit under test, starts it with a throwaway data folder and
  a key made for the run, and runs the tests in a Go container beside it, all
  under the runner's rootless Docker. Models and runtimes stay in the Docker
  volumes `toskar-quality-models` and `toskar-quality-runtimes`, so only the
  first run downloads them. **Run workflow** takes a `ref` to test a branch
  before it merges; the workflow itself always comes from main. Extra
  `docker run` flags for the daemon, such as GPU access, go in the variable
  `QUALITY_DOCKER_FLAGS`. The daemon is held to `QUALITY_MEMORY` (default
  `32g`, no swap) and `QUALITY_CPUS` (default three quarters of the cores),
  and picks its models to fit; each run first removes containers an
  interrupted run left behind.

Add a case when a change fixes a behavior, so it stays fixed.
