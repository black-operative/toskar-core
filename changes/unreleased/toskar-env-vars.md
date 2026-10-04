### Changed

- Environment variables have new names starting with `TOSKAR_`, such as `TOSKAR_API_PORT`, `TOSKAR_API_KEY`, and `TOSKAR_URL`. The `YGGDRASIL_` names keep working, so existing Docker, systemd, launchd, and MCP settings need no change. When both are set, the `TOSKAR_` name wins. The install scripts accept both names.
