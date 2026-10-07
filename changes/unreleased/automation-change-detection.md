### Fixed

- "Notify on change" notifies when something actually changed, not when the
  model words the same facts differently: it compares the values the
  automation tracks and the pages it read, and otherwise asks the model
  whether anything meaningful changed. The notification says what changed.
- "Notify when available" works in every language: the result now carries
  an availability flag instead of being read for English phrases.
