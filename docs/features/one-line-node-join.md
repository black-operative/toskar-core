# Toskar Core — One-Line Node Join

## Feature Specification — V1

### Status

**Type:** Feature  
**Target:** Toskar Core / Bifrost  
**Primary goal:** Make adding a computer to an existing Toskar network as easy as joining a K3s node to a cluster.

---

## 1. Goal

A user should be able to add a new computer to an existing Toskar network with a single command.

Typical workflow:

1. Open Toskar on an existing trusted computer.
2. Generate a temporary join command.
3. SSH into the new computer.
4. Paste the command.
5. The new computer installs/configures Core if needed, securely joins the Toskar network, and becomes available to Norn.

The experience should feel like:

```bash
curl -sfL https://github.com/yeixio/yggdrasil-core/releases/latest/download/install.sh | sh -s -- join \
  --server https://192.168.1.10:7332 \
  --token ygj_abc123...
```

or, if Toskar Core is already installed:

```bash
sudo toskar join \
  --server https://192.168.1.10:7332 \
  --token ygj_abc123...
```

The exact syntax may evolve, but the product requirement is:

> **Joining a new machine should require one copy/paste command and no manual certificate, peer, or config-file work.**

---

## 2. Why This Matters

Toskar's central product goal is to make local AI feel as easy as SaaS AI.

Today, multi-computer local AI can require users to understand:

- discovery,
- IP addresses,
- ports,
- TLS certificates,
- node identities,
- runtime setup,
- firewall rules,
- peer configuration.

That complexity directly conflicts with Toskar's mission.

A one-line join flow turns:

```text
"Configure another AI node"
```

into:

```text
"Paste this command on the other computer."
```

That should become the preferred setup path for:

- headless Linux servers,
- remote workstations,
- homelab machines,
- cloud VMs,
- machines where the user is already connected over SSH,
- systems where mDNS discovery is unavailable or inconvenient.

---

## 3. Relationship to Existing Bifrost Pairing

Bifrost already owns node discovery, pairing, trust, certificates, and node-to-node communication.

This feature should not create a second trust system.

Instead, the join flow should become a **non-interactive bootstrap path into Bifrost**.

Conceptually:

```text
Existing Toskar Network
        │
        │ generate join token
        ▼
Temporary Join Credential
        │
        │ copy/paste
        ▼
New Node
        │
        │ bootstrap request
        ▼
Bifrost Trust Establishment
        │
        ▼
Normal Paired Toskar Node
```

Once joining is complete, the temporary token is no longer used.

All normal communication should use the same Bifrost identity/certificate mechanism as any other paired node.

---

## 4. Primary User Experience

### Existing node

From CLI:

```bash
toskarctl join-token create
```

Example output:

```text
Join a computer to this Toskar network:

curl -sfL https://github.com/yeixio/yggdrasil-core/releases/latest/download/install.sh | sh -s -- join \
  --server https://192.168.1.10:7332 \
  --token ygj_7PH4W8J2K6...

This token expires in 15 minutes.
It can be used once.
```

Or, if Core is already installed on the remote machine:

```text
sudo toskar join \
  --server https://192.168.1.10:7332 \
  --token ygj_7PH4W8J2K6...
```

### Desktop/Web UI

A future UI flow could be:

```text
Computers
   ↓
Add Computer
   ↓
Join by command
```

Display:

```text
Run this command on the computer you want to add:

[ copy command ]

Expires in 14:32
One-time use
```

Optional controls:

```text
[Generate New Command]
[Revoke]
```

---

## 5. Design Principles

The join system should follow these rules:

1. **One command for the normal case.**
2. **No manual certificate exchange.**
3. **No permanent shared cluster password.**
4. **Join credentials are short-lived.**
5. **Join credentials should be one-time use by default.**
6. **A captured token should have limited value.**
7. **Joining establishes normal Bifrost trust; it does not bypass it.**
8. **The user should be able to revoke unused join tokens.**
9. **The joining node should verify the server identity before establishing durable trust.**
10. **The command should work well over SSH and on headless machines.**

---

## 6. Join Token

Introduce a temporary bootstrap credential:

```text
Toskar Join Token
```

Example format:

```text
ygj_<random-secret>
```

The token should be:

- cryptographically random,
- high entropy,
- short-lived,
- one-time use by default,
- stored hashed on the issuing node,
- revocable,
- scoped to node enrollment only.

The raw token should be shown only when created.

Toskar should never log the full token.

---

## 7. Token Record

Conceptual persisted record:

```text
JoinToken
  id
  token_hash
  created_at
  expires_at
  created_by
  max_uses
  uses
  revoked_at
  allowed_roles
  allowed_labels
  metadata
```

V1 may keep the record simpler:

```text
id
token_hash
created_at
expires_at
used_at
revoked_at
```

Default policy:

```text
Lifetime: 15 minutes
Uses: 1
```

Advanced options can come later.

---

## 8. Token Scope

A join token should authorize only:

> "Enroll one new Toskar node into this network."

It should **not** authorize:

- arbitrary API access,
- model execution,
- administrative API access,
- secret retrieval,
- joining additional nodes after use,
- modifying cluster-wide configuration.

After successful enrollment, Bifrost-issued credentials become the durable identity.

---

## 9. Bootstrap Protocol

Conceptual flow:

```text
Existing Node                          Joining Node

Create join token
      │
      ├──────── user copies token ─────────►
      │
      │
      │                     POST /bifrost/join
      │◄────────────────────────────────────
      │                     token
      │                     node public key
      │                     node metadata
      │
Validate token
Validate expiration
Validate unused
      │
Create/approve node identity
Issue trust material
      │
      ├────────────────────────────────────►
      │                     certificate /
      │                     network identity
      │                     peer metadata
      │
Mark token consumed
      │
      │◄──────── authenticated reconnect ───
      │
Node becomes normal Bifrost peer
```

The exact endpoint names are implementation details.

---

## 10. Node Identity

The joining node should generate its own key material locally.

Private keys should never be generated by the server and transmitted to the joining node.

Preferred model:

```text
Joining node:
generate private/public keypair
        ↓
send public identity during bootstrap
        ↓
existing trusted node signs/approves identity
        ↓
joining node stores resulting trust material
```

This limits the amount of sensitive material traveling over the network.

---

## 11. Server Verification

The bootstrap flow must defend against connecting to the wrong machine.

A join command should include or obtain enough information to verify the intended server.

Possible mechanisms:

### Option A — TLS certificate validation

If the Bifrost endpoint already has a certificate trusted by the joining machine, normal TLS validation is sufficient.

### Option B — Server fingerprint

The generated command may include a server identity fingerprint:

```bash
toskar join \
  --server https://192.168.1.10:7332 \
  --token ygj_... \
  --fingerprint sha256:ABCD...
```

The joining node validates the server before sending the token.

### Option C — Token-bound server identity

The join token record can be cryptographically bound to the issuing network/server identity.

V1 should select one explicit verification mechanism rather than relying on "trust first thing at this IP" without warning.

---

## 12. Join Command Generation

The issuing node should generate the full command.

Example:

```bash
toskarctl join-token create --print-command
```

Potential output:

```bash
curl -sfL https://github.com/yeixio/yggdrasil-core/releases/latest/download/install.sh | \
  sudo sh -s -- join \
  --server https://10.0.0.5:7332 \
  --token ygj_7PH4W8J2K6 \
  --fingerprint sha256:34D2...
```

If the runtime is already installed:

```bash
sudo toskar join \
  --server https://10.0.0.5:7332 \
  --token ygj_7PH4W8J2K6 \
  --fingerprint sha256:34D2...
```

The user should not have to manually discover:

- server ID,
- peer certificate,
- local node ID,
- Bifrost port,
- CA path,
- config paths.

---

## 13. Installer + Join

The best user experience eventually combines installation and joining.

Example:

```bash
curl -sfL https://github.com/yeixio/yggdrasil-core/releases/latest/download/install.sh | \
  sudo sh -s -- join \
  --server https://10.0.0.5:7332 \
  --token ygj_...
```

Installer responsibilities:

1. Detect OS/architecture.
2. Install the correct Core package.
3. Install/register the service.
4. Run the join command.
5. Start or restart Core.
6. Confirm successful enrollment.
7. Print the resulting node name/status.

Example completion:

```text
✓ Toskar Core installed
✓ Connected to 10.0.0.5
✓ Node identity created
✓ Joined Toskar network "home"

Node:
  name: gpu-box
  id:   node_9a12...
  status: Ready
```

---

## 14. Existing Installation Join

If Core is already installed, users should not need the installer.

Provide a native CLI command.

Recommended shape:

```bash
toskarctl join \
  --server https://10.0.0.5:7332 \
  --token ygj_...
```

or:

```bash
toskar join ...
```

Prefer `toskarctl join` if `toskarctl` becomes the normal administrative CLI.

The CLI can communicate with the local daemon or perform bootstrap directly depending on implementation.

---

## 15. Headless Operation

This feature is specifically important for headless nodes.

It must work without:

- browser access,
- GUI confirmation on the joining node,
- mDNS,
- interactive prompts in the normal case.

The command should return a non-zero exit status on failure and produce useful terminal output.

---

## 16. Idempotency

Running the join command on a node that is already part of the same Toskar network should not corrupt its identity.

Expected result:

```text
This computer is already joined to "home".
Node ID: node_9a12...
```

If the node belongs to a different Toskar network, do not silently replace trust.

Example:

```text
This computer is already paired with another Toskar network.

Use:
  toskarctl leave

before joining a different network.
```

Advanced force/reset behavior may exist, but it should be explicit.

---

## 17. Leave / Reset

Joining needs a corresponding clean removal path.

Potential command:

```bash
toskarctl leave
```

Expected behavior:

- notify the current network if reachable,
- revoke/remove local Bifrost trust,
- clear cluster membership configuration,
- preserve local models by default,
- preserve local Core installation,
- stop participating in remote scheduling.

Optional destructive behavior may be separate:

```bash
toskarctl reset --identity
```

Do not delete downloaded models simply because the node leaves a team.

---

## 18. Node Naming

The joining node should automatically propose a useful name.

Default sources:

- hostname,
- OS device name.

Example:

```text
mikes-gpu-box
```

The join command may optionally specify:

```bash
--name gpu-box
```

Naming conflicts should be handled clearly.

Example:

```text
gpu-box-2
```

or fail and ask for a unique name, depending on the existing node model.

---

## 19. Labels and Roles

A useful advanced feature is allowing the issuing side to preassign metadata.

Example:

```bash
toskarctl join-token create \
  --label location=garage \
  --label purpose=inference
```

The token can carry allowed bootstrap metadata.

The joining user should not be able to use token parameters to grant itself privileges beyond the token's scope.

Future examples:

```text
role=worker
gpu=true
environment=home
```

V1 does not require arbitrary role-based enrollment.

---

## 20. Discovery vs Join Command

The existing local discovery/pairing UX should remain.

Two valid onboarding paths:

### Nearby interactive pairing

```text
Computers
  ↓
Find Computers
  ↓
Select computer
  ↓
Confirm pairing
```

Best for:

- normal desktop users,
- nearby machines,
- LAN environments with mDNS.

### One-line join

```text
Generate command
  ↓
SSH to remote computer
  ↓
Paste command
```

Best for:

- servers,
- headless machines,
- remote nodes,
- networks without mDNS,
- infrastructure automation.

They should converge on the same Bifrost trust model.

---

## 21. Static Peer Configuration

A successful join should automatically establish enough peer information that the new node can reconnect after restart.

The user should not need to manually edit static peer configuration.

Possible behavior:

```text
bootstrap server
      ↓
learn trusted network peers
      ↓
persist trusted controller/peer information
      ↓
normal Bifrost reconnection
```

Static peers may still be supported as an advanced/manual mechanism.

---

## 22. Controller / Network Identity

The one-line join feature makes it useful to formalize the concept of a Toskar network identity.

Conceptually:

```text
Toskar Network
  id
  name
  trust root / network identity
  known nodes
```

This does not necessarily require introducing a centralized permanent controller.

However, the command issuer must have authority to approve a new member.

V1 can use the node on which the join token was generated as the bootstrap authority.

---

## 23. Multi-Controller Future

Future Toskar networks may have more than one trusted node capable of issuing joins.

Potential behavior:

```text
Network
├── controller A
├── controller B
└── workers
```

Join tokens should therefore eventually belong to the **network**, not conceptually to one physical machine.

This is not required for the first implementation.

---

## 24. Revocation

Users must be able to revoke unused tokens.

CLI:

```bash
toskarctl join-token list
toskarctl join-token revoke <id>
```

Possible output:

```text
ID          CREATED       EXPIRES       STATUS
jt_1234     2 min ago     13 min        active
jt_5678     1 hour ago    expired       expired
jt_9012     yesterday     —             used
```

Raw secrets should not be displayed after creation.

---

## 25. Auditing

Enrollment events should be recorded.

Example:

```text
2026-09-28T18:32:11Z node.join.accepted
network=home
node=gpu-box
node_id=node_9a12
join_token_id=jt_1234
source=10.0.0.22
```

Never log:

- the full join token,
- private keys,
- reusable authentication secrets.

Useful audit events:

```text
join_token.created
join_token.revoked
join_token.expired
join.accepted
join.rejected
node.left
node.revoked
```

---

## 26. Security Against Token Theft

A join token is effectively a temporary invitation to the Toskar network.

Mitigations:

- short expiration,
- one use,
- high entropy,
- hashed storage,
- TLS/bootstrap server verification,
- automatic revocation after use,
- no logging,
- explicit manual revoke,
- rate limiting join attempts.

Future hardening may include:

- source CIDR restrictions,
- token-bound server identity,
- user approval before final enrollment,
- TPM/Secure Enclave device identity,
- token generated for a specific expected node.

---

## 27. Rate Limiting

The join endpoint should be rate-limited.

Repeated invalid join attempts should not allow high-speed token guessing.

Possible limits:

```text
per source IP
per token ID/prefix
global bootstrap endpoint
```

Join errors should avoid revealing unnecessary information about whether a token ID exists.

---

## 28. Error Experience

Failures must be understandable from SSH.

Examples:

### Expired token

```text
Join failed: this join token has expired.

Generate a new join command from an existing Toskar computer.
```

### Used token

```text
Join failed: this join token has already been used.
```

### Wrong server identity

```text
Join stopped: the server identity did not match the expected fingerprint.

Expected: sha256:34D2...
Received: sha256:91AA...

No credentials were sent.
```

### Server unreachable

```text
Could not reach Toskar at 10.0.0.5:7332.

Check:
- the server address
- firewall access
- Bifrost availability
```

---

## 29. Service Installation

For supported operating systems, the installer should register Core as a normal background service.

Potential targets:

```text
Linux      systemd
macOS      launchd
Windows    Windows Service
```

The join feature should not require users to manually keep the daemon running in a terminal.

---

## 30. Supported Platforms

V1 should target the same platforms Core supports where the installation mechanism exists:

```text
Linux amd64
Linux arm64
macOS Apple Silicon
macOS Intel
Windows amd64
```

The shell one-liner is naturally strongest on Unix-like systems.

Windows should have an equivalent PowerShell one-liner.

Example conceptual form:

```powershell
irm https://github.com/yeixio/yggdrasil-core/releases/latest/download/install.ps1 | iex
```

The final implementation should not copy insecure command patterns merely for visual similarity; arguments and execution flow must be designed carefully.

---

## 31. Automation-Friendly Mode

The join feature should also work for infrastructure automation.

Examples:

- cloud-init,
- Ansible,
- Terraform provisioners,
- shell scripts,
- CI-created test nodes,
- lab provisioning.

The command should therefore support:

- non-interactive execution,
- deterministic exit codes,
- structured output eventually,
- no hidden GUI dependency.

Potential future option:

```bash
toskarctl join ... --output json
```

---

## 32. Kubernetes Relationship

Kubernetes-native Toskar deployment is a separate feature.

However, one-line join could be useful for:

- bare-metal nodes outside Kubernetes,
- edge workers,
- hybrid clusters,
- automatically enrolling machines before installing K3s/Kubernetes,
- Toskar Grid experiments.

Do not make Kubernetes a dependency of the join flow.

---

## 33. Grid Relationship

The future Toskar Grid depends on easy node enrollment.

A future experience might be:

```text
Machine A
Toskar Core installed

Generate join command
      ↓

Machine B
paste command

Machine C
paste command

      ↓

Toskar Team
3 computers

      ↓

Norn can route work
and eventually create Model Grids
```

The join feature is therefore foundational infrastructure for larger multi-node features.

---

## 34. CLI Proposal

Potential commands:

```bash
# Generate join credential
toskarctl join-token create

# Generate copy/paste command
toskarctl join-token create --print-command

# List active/recent tokens
toskarctl join-token list

# Revoke token
toskarctl join-token revoke jt_1234

# Join this machine
yggctl join --server <address> --token <token>

# Inspect membership
toskarctl network status

# Leave current network
toskarctl leave
```

Exact CLI naming should follow the existing Toskar CLI conventions before implementation.

---

## 35. API Proposal

Potential control API:

```text
POST   /api/v1/join-tokens
GET    /api/v1/join-tokens
DELETE /api/v1/join-tokens/{id}
```

Bootstrap endpoint:

```text
POST /bifrost/join
```

The bootstrap endpoint must have its own restricted authentication semantics because the joining node is not yet a normal trusted peer.

Do not expose normal administrative API capabilities with a join token.

---

## 36. V1 Scope

Include:

- temporary secure join tokens,
- one-time use by default,
- expiration,
- hashed token storage,
- CLI token generation,
- copy/paste join command,
- headless join flow,
- automatic Bifrost identity establishment,
- automatic persistent peer/network configuration,
- server identity verification,
- useful error messages,
- token revocation,
- basic enrollment audit events,
- idempotent already-joined behavior,
- leave-network command,
- installer integration where supported.

---

## 37. Explicitly Out of Scope for V1

Do not require:

- public Internet relay,
- cloud account,
- central hosted Toskar service,
- Kubernetes,
- multi-controller consensus,
- role-based enrollment policies,
- unattended WAN bootstrap,
- TPM/Secure Enclave identity,
- bulk provisioning UI,
- permanent reusable cluster passwords,
- automatic port forwarding,
- automatic firewall modification beyond package/service requirements.

---

## 38. Suggested Implementation Phases

### Phase J0 — Bootstrap Spike

Prove:

1. Existing node creates short-lived token.
2. New node sends token + public identity.
3. Existing node validates token.
4. Trust material is issued.
5. Token becomes unusable.
6. New node reconnects with normal Bifrost authentication.

### Phase J1 — CLI

Implement:

```text
join-token create
join
join-token revoke
leave
```

### Phase J2 — Installer Integration

Support:

```text
install + join
```

from a single command.

### Phase J3 — UI

Add:

```text
Computers → Add Computer → Join by Command
```

with copy button and expiry countdown.

### Phase J4 — Automation Hardening

Add:

- structured output,
- provisioning-friendly flags,
- better audit trail,
- optional labels/metadata.

---

## 39. Acceptance Criteria

The feature is complete when:

1. A user can generate a join command from an existing Toskar network.
2. The command contains everything needed for the normal join case.
3. The user can SSH into a new supported machine and paste the command.
4. No manual certificate copying is required.
5. No manual editing of peer configuration is required.
6. The token expires automatically.
7. The token is one-time use by default.
8. The server stores only a secure representation of the token.
9. The raw token is never written to normal logs.
10. The joining node verifies the intended bootstrap server.
11. The joining node generates and retains its own private identity key.
12. Successful bootstrap results in normal Bifrost trust.
13. The temporary token has no use after enrollment.
14. The node reconnects after restart without repeating the join process.
15. The node becomes visible to Norn as a normal compute target.
16. Re-running the command on an already joined node is safe.
17. A node cannot silently replace membership in another Toskar network.
18. Unused join tokens can be revoked.
19. Failures return useful terminal messages and non-zero exit codes.
20. Joining works without mDNS or a GUI on the remote node.

---

## 40. Product Outcome

The feature succeeds when adding a server to Toskar feels like adding a K3s worker:

```text
SSH to computer
        ↓
Paste one command
        ↓
Computer appears in Toskar
        ↓
Done
```

Users should not have to understand Bifrost certificates, node identities, peer configuration, or discovery internals.

The implementation may involve substantial trust and networking logic.

The user experience should be one command.
