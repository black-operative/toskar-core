# Two-machine Team demo — test plan

Manual acceptance for a two-computer demo: one Chat turn runs a **Team** pipeline across two paired computers, and the UI shows which machine ran each role.

See [Clustering](clustering.md) for how discovery, pairing, and placement work.

## Goal

With desktop **A** and laptop **B** on the same LAN:

1. Pair through the GUI (no IP/port entry).
2. Run Programming (**Team**) from Chat on A.
3. See the **planner**, each **worker**, and the **reviewer** land on a computer.
4. Get **one** final answer in the same conversation.

## Prerequisites

- Toskar installed (or running from source) on both machines.
- Discovery enabled on both.
- At least one GGUF model installed where roles need it:
  - **Same model on both** + pin worker → B, **or**
  - Model A only on desktop, Model B only on laptop (pin roles to those models/nodes).
- Advanced mode on (Profiles → Edit roles), for pinning.

## Steps

### 1. Pair A and B

1. Open Toskar on A and B.
2. Enable discovery if prompted.
3. On A, find B in the cluster/nodes UI and start pairing.
4. On B, approve the incoming offer.
5. Confirm both show each other as paired and online.

**Pass:** No manual IP or port configuration.

### 2. Install model(s)

**Option A — one model on both machines (fastest)**

1. Install the same model on A and B (or “Install on all computers” from A).
2. Chat profile = **Programming (Team)**. Pinning is optional: automatic placement prefers spreading Team roles across online computers that have the model.
3. (Optional) Advanced → **Profiles** → **Programming** → pin **worker** → **B** for a forced layout.

**Option B — different models (plan scenario)**

1. Install Model A on the desktop only.
2. Install Model B on the laptop only.
3. Programming roles: planner → Model A (+ desktop if pinned), worker → Model B (+ laptop), reviewer → Model A.

**Pass:** Roles show the intended model and node pin (or Automatic).

### 3. Chat with Team on A

1. On A, open **Chat**.
2. New conversation (or select one).
3. Set **Profile** to **Programming (Team)**.
4. Pick an installed model (fills empty role model IDs).
5. Send a short prompt (e.g. “Explain what a mutex is in two short paragraphs.”).

**Pass during the turn:**

- Status updates mention roles / computer names (e.g. “Worker on …”).
- A left-border **timeline** lists:
  - Planner on \<name\>
  - Worker on \<name\>
  - Reviewer on \<name\>
- Worker’s computer is **B** when pinned (or when Model B is only on B).
- One assistant bubble with the **final** answer (not three mashed role dumps).

### 3b. Performance tab (after the turn)

1. Open **Performance** → **Chat history**.
2. Find the turn you just ran.

**Pass:**

- **Computers** shows both machine names with a **Cross-machine** badge.
- Expanding the row lists the planner, each worker, and the reviewer with computer, TTFT, total time, and eval tok/s per role.
- Summary **Cross-machine** count increased.

## 4. Negative checks (quick)

| Check | Expected |
|-------|----------|
| Chat profile = General Assistant (simple) | No team timeline; single-node style reply |
| Unpin worker → Automatic | Still completes; placement may stay on one machine |
| Revoke B while pinned to B | Fail with a clear offline/placement error naming that computer |

## Out of scope for this plan

- Code signing / notarized installers
- Tool `ask` prompts during Team turns
- Persisted task step history UI
- WAN / NAT pairing

## Automated coverage (CI)

`make test-cluster` runs the same placement story headless↔headless in Docker:

1. Pair A and B (plus C for discovery/auth checks).
2. Enable `TOSKAR_STUB_INFERENCE` so nodes seed a fake `stub-team` model (no GGUF download).
3. Pin Programming roles: planner/reviewer → A, worker → B.
4. `POST /api/v1/chat` on A and assert:
   - `orchestration.role` events place **worker on B**
   - one non-empty final answer

This exercises Bifrost remote chat + Team placement. It does **not** replace the manual UI timeline check above.

## Related code

- Chat path: `internal/app/chat.go` (`withChatModel`, role steps)
- Team strategy: `internal/orchestrator/builtin/simple/team.go` (planner, reviewer) and `plan.go` (worker slots)
- Timeline events: `orchestration.role` (SSE)
- UI: `web/src/features/chat/ChatPage.tsx`
- Cluster driver: `cmd/clustercheck` + `docker-compose.cluster.yml`
- Checklist comment: `internal/nodes/server_test.go`
