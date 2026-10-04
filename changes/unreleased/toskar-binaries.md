### Changed

- The programs are now `toskar` (the daemon) and `toskarctl` (the command line). `yggdrasil-daemon` and `yggctl` are installed as links to them, so launchd agents, systemd units, scripts, and MCP settings that run the old names keep working; on Windows, the install script adds them. Shell completion works for both names. The join command a computer prints still says `yggctl join`, so it works on a computer with an older version.
