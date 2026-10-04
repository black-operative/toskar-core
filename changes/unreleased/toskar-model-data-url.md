### Changed

- Community ratings fall back to the public summary at its new home, `yeixio/toskar-model-data`, when the ratings service can't be reached. Earlier versions keep working through GitHub's redirect from the old name.
- Requests to other services (ratings, place search, Hugging Face, web search, webhooks, ntfy, remote memory, and downloads) identify themselves as Toskar in their `User-Agent`.
