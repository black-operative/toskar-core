### Fixed

- Read aloud in the Mac App Store app reads answers with your Mac's own
  voices, instead of trying to install speech and showing a sandbox error.
  The daemon now recognizes the App Sandbox from its folder too, so speech,
  text recognition, the code environment, image generation, training and the
  browser no longer try to download what the sandbox can't run.
- When speech can't be set up, Read aloud says so in plain words, without
  file paths.
