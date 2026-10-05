### Changed

- The real-model quality run rides out a daemon that goes away for a moment (a restart or a settings change), retrying for up to three minutes. If the daemon starts asking for an API key partway through, the run stops with one clear reason instead of failing every case left. A case that stops before it is checked now counts as failed in the summary.
