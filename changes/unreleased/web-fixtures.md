### Changed

- `TOSKAR_WEB_FIXTURES`, for quality runs only, makes the daemon answer web search, places, and page reads from a file of fixed pages (`tests/quality/web.json`) instead of the internet, and log a warning that it does. The self-hosted real-model run uses it, so its answers are checked against the same pages as every other run.
