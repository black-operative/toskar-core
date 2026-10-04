# Command line

Yggdrasil Core installs two programs:

- **`yggdrasil-daemon`** runs Yggdrasil: the API, the web UI, models, schedules, and the connection to other computers.
- **`yggctl`** talks to a running daemon from a terminal or a script.

## `yggdrasil-daemon`

```bash
yggdrasil-daemon                      # use the default data directory
yggdrasil-daemon -data-dir ~/ygg-test # use another one
```

| Flag | Meaning |
| --- | --- |
| `-data-dir <path>` | Data directory. Defaults to the one for your OS (see [Configuration](configuration.md#data-directory)). |
| `-version` | Print the version, license, and corresponding source, then exit |
| `-python-envs` | Print, as JSON, the Python environments a sandboxed app bundles, then exit |

The daemon runs in the foreground and logs JSON to standard output and to `logs/daemon.log`. Stop it with Ctrl+C.

How it is started depends on how it was installed:

- **deb and rpm packages:** they install a systemd service, `yggdrasil.service`, and enable and start it. It runs as the system user `yggdrasil`, so its data directory is `/var/lib/yggdrasil/.local/share/toskar`, or `/var/lib/yggdrasil/.local/share/yggdrasil` for an install from before the rename. Use `sudo systemctl status yggdrasil`, `restart`, and `stop`. `journalctl -u yggdrasil` shows its log. Removing the package stops and disables the service.
- **Homebrew and the headless archives:** they install the programs only. Run `yggdrasil-daemon` from a terminal or your own service manager.

## `yggctl`

`yggctl` reaches the daemon at `TOSKAR_URL`, or `http://127.0.0.1:7331` when that is not set. When the daemon listens beyond this computer, set `TOSKAR_API_KEY` too. It is sent as the bearer token.

| Command | What it does |
| --- | --- |
| `yggctl version` (or `about`) | Version, license, and corresponding source |
| `yggctl paths` | Default data, model, runtime, log, and database paths |
| `yggctl automations …` | Manage scheduled automations (below) |
| `yggctl mcp` | Let an app that starts MCP servers as programs, such as Claude Desktop, use Yggdrasil (below) |
| `yggctl join-token create [--ttl 15m]` | Make a one-time join token and print the command that adds a computer to this network ([Clustering](clustering.md#joining-with-one-command)) |
| `yggctl join-token list`, `revoke <id>` | List recent join tokens, or stop an unused one |
| `yggctl join --server … --token … --fingerprint … [--name …] [--wait 60s]` | Join this computer to the network that made the command, optionally renaming it, and waiting for Yggdrasil to start. Exits 0 when joined or already joined, 1 when refused, 2 for a usage error |
| `yggctl network` | This computer's address, fingerprint, network, and paired computers |
| `yggctl leave` | Leave the network: tell each paired computer, then forget them all |
| `yggctl completion <bash\|zsh\|fish>` | Print a shell completion script |

### Automations

```bash
yggctl automations list
yggctl automations get AUTOMATION_ID
yggctl automations create --name "Morning price" --prompt "Check the price of …" \
  --profile general-assistant --model auto --schedule daily --at 08:00 --zone America/Los_Angeles \
  --notify condition --condition-kind threshold --condition-op below --condition-value 500
yggctl automations update AUTOMATION_ID --at 07:30
yggctl automations run AUTOMATION_ID
yggctl automations pause AUTOMATION_ID
yggctl automations resume AUTOMATION_ID
yggctl automations delete AUTOMATION_ID
```

`create` needs `--name`, `--prompt`, `--profile`, and `--model`. `--model auto` lets Auto pick a model for each run. `update` changes only the flags you pass.

| Flag | Values |
| --- | --- |
| `--schedule` | `once`, `daily`, `weekly`, or `interval` |
| `--at` | `HH:MM` for daily and weekly; an RFC 3339 time for `once` |
| `--every` | A duration for `interval`, such as `6h` or `30m` |
| `--weekday` | `0`–`6` for weekly; Sunday is `0` |
| `--zone` | An IANA time zone, such as `Europe/Berlin` (default `UTC`) |
| `--notify` | `always` (default), `condition`, `change`, `failure`, or `none` |
| `--condition-kind` | With `--notify condition`: `threshold`, `available`, or `significant` |
| `--condition-op`, `--condition-value` | With `threshold`: `below` or `above`, and the amount |
| `--tool` | A tool the automation may use unattended. Repeat it for more tools. Tools listed here were approved by you and run even if they change things. |
| `--disabled` | Create the automation paused |

### MCP bridge

Apps such as Claude Desktop start MCP servers as programs. `yggctl mcp` is that program: it reads MCP messages on standard input, sends each to the daemon's `/mcp` endpoint, and writes the replies to standard output. API Access → **Use Yggdrasil in other AI apps** shows the exact configuration to paste for Claude Desktop, Claude Code, Cursor, VS Code, and other apps. See [MCP](mcp.md).

### Shell completion

```bash
yggctl completion zsh > "${fpath[1]}/_yggctl"   # zsh
yggctl completion bash > /etc/bash_completion.d/yggctl
yggctl completion fish > ~/.config/fish/completions/yggctl.fish
```

Homebrew and the deb and rpm packages install the completion scripts for you.
