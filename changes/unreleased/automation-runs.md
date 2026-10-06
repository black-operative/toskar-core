### Fixed

- Automations run two at a time, so a slow one no longer holds up the
  others, and a run that takes more than 20 minutes is stopped instead of
  hanging.
- Run now starts the run and returns right away; closing the page no longer
  stops it, and its result appears when it finishes.
- An automation that fails three times in a row, for any reason, is paused
  with a notification saying why, in the App language. Retries go by the
  kind of error rather than its English wording.
