### Changed

- The Linux packages are now `toskar` (deb and rpm), and the service is `toskar.service`, which also answers to `yggdrasil.service`. `apt-get upgrade` and `dnf upgrade` move an existing `yggdrasil` install over, keeping its data, its system user, and its service running; a small transitional `yggdrasil` deb makes that work with apt and can be removed afterwards. The service's log is in `journalctl -u toskar`.
- The Homebrew formula is now `toskar`, and `brew upgrade` moves an existing `yggdrasil` install to it.
