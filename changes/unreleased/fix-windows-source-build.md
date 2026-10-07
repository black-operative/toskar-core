### Fixed

- On Windows, a build from source now shows the web UI. The page used to stay
  blank because the scripts and styles under `/assets/` were answered with the
  start page instead of the files.
- On Windows, `make start` from PowerShell or cmd now builds `bin/toskar.exe`
  and runs it, and no longer prints "The system cannot find the path
  specified." when it looks up the git commit.
