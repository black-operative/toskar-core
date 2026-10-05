### Changed

- CI can run on self-hosted runners: the repository variables `CI_RUNS_ON` (pull request and push checks) and `TRUSTED_RUNS_ON` (releases, screenshots, and the small-model quality job) pick the runner, and GitHub's runners stay the default. The installer test always runs on a GitHub runner, since it installs system-wide.
