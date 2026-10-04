# Examples

These call a daemon on `http://127.0.0.1:7331`. Start one first (`./bin/toskar`) and install a model before `chat` requests. `GET /v1/models` works before a model is loaded. It lists profiles.

With the default loopback bind, no API key is required. If the daemon listens beyond loopback, set `YGG_API_KEY` to the key from the web UI. The samples send `Authorization: Bearer` only when that variable is set. Never commit a real key, and do not put the key in a URL.

```bash
export YGG_API_KEY=YOUR_API_KEY   # required when the API is not on loopback
```

| | |
| --- | --- |
| [curl/models.sh](curl/models.sh) | `GET /v1/models` |
| [curl/chat.sh](curl/chat.sh) | `POST /v1/chat/completions` |
| [python/chat.py](python/chat.py) | same calls, Python 3 standard library |
| [javascript/chat.mjs](javascript/chat.mjs) | same calls, Node.js 18+ `fetch` |

Known limits of this API are in [docs/api.md](../docs/api.md).
