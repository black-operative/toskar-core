# Clustering

Yggdrasil can use more than one computer. Each computer runs `yggdrasil-daemon`. Work is placed per role. One model is not split across machines.

## Discovery

Discovery is enabled by default. The daemon advertises `_localai._tcp` and browses for the same service. Peers show up through `GET /api/v1/nodes` and the Computers page.

The service's port is Bifrost's (7332). Its TXT record carries `node_id`, `name`, `version`, `pairing`, and `api_port`, the port of the API (7331 by default), so an app that finds Yggdrasil on the network, such as the iPhone app, knows where to connect.

When mDNS cannot see peers, set static addresses:

- `static_peers` in `config.json`, as `host:7332`
- or `TOSKAR_STATIC_PEERS=host-a:7332,host-b:7332`

`docker-compose.cluster.yml` uses static peers and `TOSKAR_STUB_INFERENCE=true`. That compose file checks pairing and placement without a real GGUF. It is not a GPU cluster.

If discovery is on at startup and Bifrost is still bound to loopback, the daemon sets the internal host to `0.0.0.0`. Changing discovery later can require a restart. Settings expose that as `discovery_needs_restart`.

## Pairing

Pairing is a consent step between two daemons.

1. On one computer, start pairing against a discovered node (`POST /api/v1/nodes/pair` or the web UI). It shows a 6-digit code.
2. On the other computer, approve the offer with the same code (`POST /api/v1/nodes/{id}/pair/approve`), or enter the code (`POST /api/v1/nodes/pair/claim`).
3. Each side stores the peer identity. Later Bifrost calls that read models or run chat send a bearer token checked against that peer's key.

Both steps are signed with the computers' node keys. The offer is signed by the computer that made it and names the computer it is for. The approving computer signs its answer over the session and the code, so only the computer that was asked, holding the key that was seen when pairing started, can finish it. Each computer stores the other's key and its fingerprint, sha256 of the raw ed25519 key, the same fingerprint join commands show.

Pairing never replaces a paired computer's key. An offer or answer for a paired computer with a different key is refused; remove the computer (`POST /api/v1/nodes/{id}/revoke`) and pair again.

Wrong codes and failed answers are limited by address. A pairing whose answer fails 5 times ends, and 10 wrong codes end every pairing waiting on that computer; start pairing again for a new code. Pending offers are listed only on this computer's API, not on Bifrost.

A bearer token names the computer it is for and is good for one request, for up to 5 minutes. Both computers need this version or later to pair or to work together.

`/internal/v1/health`, `/internal/v1/node`, `/internal/v1/pairing/offer`, `/internal/v1/pairing/complete`, `/internal/v1/pairing/outbound/{code}`, and the join routes answer without that token. Other internal routes reject a missing or invalid token.

Revoke a peer with `POST /api/v1/nodes/{id}/revoke`.

## Joining with one command

For a server or any computer you reach over SSH, without mDNS or a screen, join it with a command made on a computer already in the network.

1. On a computer in the network, run `yggctl join-token create`. It prints the command for the new computer:

   ```text
   yggctl join --server 192.168.1.10:7332 --token ygj_… --fingerprint sha256:…
   ```

2. On the new computer, with Yggdrasil running, run that command. The two computers trust each other from then on, and the new one shows on the Computers page and takes work from Norn like any paired computer.

   If Yggdrasil isn't installed there yet, use the second command `join-token create` prints instead. It installs Yggdrasil, starts it as a service, and joins:

   ```text
   curl -fsSL https://github.com/yeixio/yggdrasil-core/releases/latest/download/install.sh | sh -s -- join --server … --token … --fingerprint …
   ```

   On Linux it installs the release's `.deb` (apt) or `.rpm` (dnf, yum, rpm) and the `yggdrasil` systemd service, using `sudo` when not run as root. On macOS it installs the headless archive in `~/.local/lib/yggdrasil` with a launchd agent, or with `sudo` in `/usr/local/lib/yggdrasil` with a launchd daemon that runs as the person who ran `sudo`, for a Mac nobody is logged in to. Each download is checked against the release's `SHA256SUMS.txt`, and a Yggdrasil that is already installed and running is left as it is. Windows has the same in PowerShell, from `install.ps1`, which installs in `%LOCALAPPDATA%\Programs\Yggdrasil` and starts at sign-in with a scheduled task.

   `TOSKAR_VERSION` installs a particular release instead of the latest. Without `join`, the scripts only install.

The Computers page does the same: **Add by command** makes a command, with tabs for a computer that has Yggdrasil, one to install it on (Linux, macOS), and Windows, a copy button, a countdown, **Revoke**, and the recent commands. It says when the computer has joined.

The token lasts 15 minutes (`--ttl` up to `24h`) and works once. `yggctl join-token list` shows recent tokens, and `yggctl join-token revoke <id>` stops an unused one. Only a proof key derived from the token is stored, and the token is never logged.

How the join stays safe over Bifrost's plain HTTP:
- **The right computer:** the command carries the issuing computer's key fingerprint. Before going on, the new computer checks that computer's signature on a fresh challenge, so a different machine at that address is refused before anything that proves the token is sent.
- **The token stays put:** the new computer proves it holds the token with an HMAC over the challenge and its own public key. The token itself never crosses the network, and a relay can't swap in its own key.
- **One use:** the token is used up before the new computer is trusted, so two computers racing with one token can't both get in. Ten failed attempts from one address within 10 minutes pause joins from it.
- **Records:** each join, refusal, token made, and token revoked is logged and published (`node.join.accepted`, `node.join.rejected`, `join_token.created`, `join_token.revoked`). A join also announces "Computer paired".

Running the command again on a computer that already joined says so and changes nothing. A computer in another network is refused until it runs `yggctl leave`, which tells each paired computer it is leaving and forgets them all. Models, settings, and its identity stay. `yggctl network` shows this computer's address, fingerprint, network, and paired computers.

### Automating joins

Every join command takes `--output json` and exits 0 when joined or already joined, 1 when refused, and 2 for a usage error, so provisioning tools can run them unattended.

- **Make a token where you are:** run `yggctl join-token create --output json` on a computer in the network, over SSH from the provisioning machine if need be, and read `install_command` (or `command`, `token`, `server`, `fingerprint`) from it. Tokens can last up to a day (`--ttl 24h`) for a slow build, and each still works once, so make one per computer. The control API (`POST /api/v1/join-tokens`) does the same from scripts on that computer, or from elsewhere with an API key when the API listens beyond it.
- **Name it:** `--name gpu-box-3` renames the computer as it joins. A name already taken in the network gets `-2`, `-3`, and so on.
- **Right after installing:** `--wait 60s` waits for Yggdrasil to start before joining. `install.sh` waits on its own.

cloud-init, with the token made beforehand:

```yaml
runcmd:
  - curl -fsSL https://github.com/yeixio/yggdrasil-core/releases/latest/download/install.sh | sh -s -- join --server 10.0.0.5:7332 --token ygj_… --fingerprint sha256:… --name worker-01
```

Ansible, making the token on an existing computer for each new one:

```yaml
- name: Make a join token
  ansible.builtin.command: yggctl join-token create --ttl 30m --output json
  delegate_to: studio
  register: token
  changed_when: true
  no_log: true  # the output holds the token

- name: Install Yggdrasil and join
  ansible.builtin.shell: >-
    {{ (token.stdout | from_json).install_command }} --name {{ inventory_hostname }}
  no_log: true
```

The new computer pairs with the computer that made the token. Other computers in the network pair with it separately.

Both computers need Bifrost reachable on the network, which is so while discovery is on (the default). Making a token or joining is refused with `JOIN_NOT_REACHABLE` otherwise.

## Placement

Norn (`internal/scheduler`) scores candidates and picks a node for a role. With the Team strategy, a planner splits the request, each part goes to its own worker slot, and a reviewer checks the answer. With two paired computers and the models installed where those roles need them, the workers land on different machines and write their parts at the same time. The event stream records `scheduler.placement`.

Placement prefers an idle computer that has the model over one that is busy answering or training. A computer that is training is passed over for another with the model, even when it has the model loaded. If it is the only one with the model, it still answers. Each computer's Bifrost health answer says whether it is training, and the Computers page shows a Training badge.

### Tools on other computers

Image generation (`image.generate`, `image.edit`), video (`video.generate`), and speech (`speech.transcribe`, `speech.synthesize`) run on whichever paired computer can run them. A tool counts as available when this computer or any online paired one has a ready provider, so asking for an image on a laptop without image generation uses the workstation that has it.

**Where a call runs:**
- Placement prefers a computer whose GPU does the work. Today that is the macOS build of stable-diffusion.cpp, which uses Metal; the Linux and Windows builds run on the CPU.
- Otherwise this computer is preferred, since nothing has to travel.
- The chat profile's computer policy applies: Remote off keeps calls here, denied computers are skipped, preferred computers are favored, and Prefer local keeps a call here whenever it can run here.
- When a computer cannot be reached, or turns out not to be ready, the next one is tried. The tool's own error, such as a prompt that is too long, is not retried elsewhere.

**How a call runs:**
1. This computer keeps the approval and the audit. It reads the files the call needs from the chat and sends them with the arguments to `POST /internal/v1/tools/run` on the chosen computer. Up to 40 MB travels per call.
2. That computer runs the work after its own chats, returns the result and any files, and keeps nothing.
3. This computer saves the files to the chat. The result names the computer that ran it.

Stop closes the connection, which stops the work on the other computer. Each job sent is recorded in What left this computer.

`GET /internal/v1/tools/providers` is what a computer can run. It is asked at most every 30 seconds per computer, and again after a failed call. A computer whose Yggdrasil predates remote tools answers 404 and is listed as needing a newer Yggdrasil. Diagnostics → **Tools on each computer** shows every computer's providers and their state: Ready (healthy), Installing, Failed, or Not set up (unavailable). `GET /api/v1/tools/providers` returns the same list.

A manual pass is written up in [two-machine-team-demo.md](two-machine-team-demo.md). Continuous integration does not run that pass on physical hardware. The Docker cluster check uses stub inference.

## Ports

| Port | Bind by default | Role |
| --- | --- | --- |
| 7331 | `127.0.0.1` | Web UI, `/api/v1`, `/v1` |
| 7332 | `0.0.0.0` when discovery is on | Bifrost |

Keep 7331 on loopback unless you have read [privacy.md](privacy.md) and [SECURITY.md](../SECURITY.md). A non-loopback bind requires an API key and the daemon will not start without one. Firewall prompts on macOS are about local-network access for discovery, not about publishing the API to the internet.
