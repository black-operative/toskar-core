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
- **Browsers on this computer.** Websites can send requests to `127.0.0.1`. Without an API key, the daemon answers a browser only from its own web UI, the desktop app, or a loopback origin on the API port, and only when the `Host` header names this computer (a loopback name or IP, or with network access on, any IP), which blocks DNS rebinding. It never sends `Access-Control-Allow-Origin: *`.
- **Bifrost pairing.** Port 7332 is reachable on the LAN when discovery is on. Pairing routes are unauthenticated until a peer is trusted. Later node calls use certificate-backed tokens.
- **Automation webhooks.** `POST /hooks/{token}` starts the automation the token belongs to, with no API key: the token is its only proof. Tokens are 32 random bytes, shown once when made, and stored only as SHA-256. They're kept out of the request log. Making a new link stops the old one, and changing the trigger away from a webhook drops it. An unknown token always answers `404`. One link takes a call at most every 10 seconds, bodies over 64 KB are refused, and the browser checks above apply, so a website can't call it from someone's browser. The body reaches the run as data, wrapped and labelled as not instructions. A run started by what any trigger delivered (a webhook's body, a page, feed posts, or files) can't use tools that change things outside Toskar, even ones approved for that automation; reading and creating files in Toskar's store still work. It's reachable from other devices only with network access on.
- **Network exposure.** Defaults keep the control API on loopback and advertise the node on the local network.
- **Tool execution.** Profiles can allow a model to run shell commands, write files, or use git. Defaults for terminal, file writes, and git are `ask`.
- **Secrets.** API keys and node identity material live under the data directory `secrets/` path. Diagnostic bundles are written to exclude them. Logs may still contain operational detail.
- **Remote nodes.** A paired computer can be asked to run a role. Pair only machines you trust, and revoke peers you no longer use.
