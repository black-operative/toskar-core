### Changed

- Release archives are named `toskar-<version>-<os>-<arch>-headless.tar.gz`. The install scripts put Toskar in `~/.local/lib/toskar` or `/usr/local/lib/toskar` on macOS, run by the launchd service `ai.toskar.toskar`, and in `%LOCALAPPDATA%\Programs\Toskar` on Windows, started by the scheduled task Toskar. Installing over a setup from before the rename removes its old service or task, so only one copy runs, and leaves the old install folder as a link to the new one. The scripts can still install a release from before the rename.
