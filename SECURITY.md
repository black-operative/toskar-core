# Security policy

## Supported versions

Security fixes are considered for the latest tagged release. Older tags, including alphas, are not maintained as a separate support line. Builds from `main` are development snapshots.

| Version | Supported |
| --- | --- |
| Latest tag | Yes |
| Older tags | No |

## Reporting a Vulnerability

Please do not report security vulnerabilities through public GitHub issues.

Use GitHub's private vulnerability reporting feature for this repository:

1. Open the repository's **Security and quality** tab.
2. Select **Report a vulnerability**.
3. Provide as much detail as possible, including:
   - affected version;
   - reproduction steps;
   - expected and observed behavior;
   - potential impact;
   - relevant logs or configuration;
   - suggested remediation, if known.

Please remove API keys, credentials, tokens, personal data, and other secrets before submitting logs.

We will review reports as promptly as reasonably possible and may contact you through the private advisory to request additional information or coordinate a fix.

Please allow a reasonable period for investigation and remediation before publicly disclosing the vulnerability.

There is no bug-bounty program in this repository.

## Sensitive areas

These parts of the system deserve extra care in review:

- **API authentication.** Loopback does not require a key. Any other bind requires `Authorization: Bearer` on `/api/v1` and `/v1` from other machines (loopback requests without proxy forwarding headers still need none), and the daemon refuses to listen there until a key exists. Keys are stored as bcrypt hashes. A bearer token on plain HTTP does not encrypt traffic.
- **Bifrost pairing.** Port 7332 is reachable on the LAN when discovery is on. Pairing routes are unauthenticated until a peer is trusted. Later node calls use certificate-backed tokens.
- **Network exposure.** Defaults keep the control API on loopback and advertise the node on the local network.
- **Tool execution.** Profiles can allow a model to run shell commands, write files, or use git. Defaults for terminal, file writes, and git are `ask`.
- **Secrets.** API keys and node identity material live under the data directory `secrets/` path. Diagnostic bundles are written to exclude them. Logs may still contain operational detail.
- **Remote nodes.** A paired computer can be asked to run a role. Pair only machines you trust, and revoke peers you no longer use.
