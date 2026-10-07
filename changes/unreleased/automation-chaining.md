### Added

- Chain automations: one can run right after another finishes, given its
  result, such as a research automation followed by one that drafts a
  note from it, or only when the first one notifies, such as acting once
  a price drops. Choose "After another automation" under "Runs", or
  `toskarctl automations create --trigger after --after <id>`. Loops are
  refused when saved, and a chain stops after five in a row.
