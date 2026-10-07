### Added

- Automations can run when a web page changes or a feed has new posts,
  instead of every time. The schedule says how often to check; a check is
  a quick fetch with no model, and the task runs only when something
  changed, told what it was: the lines that came and went, or the new
  posts. Choose it under "Runs" in the form, or with `toskarctl
  automations create --trigger page|feed --trigger-url <url>`.
