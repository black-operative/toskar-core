### Fixed

- The chat's context gauge uses the window the model is actually running with, read from llama-server, instead of a guess from the catalog that could differ from it. Switching back to a chat shows its last reading instead of an empty gauge.

### Added

- The context gauge says how much memory the conversation window reserves on this computer, such as "about 1 GB", and that a longer window remembers more and uses more memory. Near the limit it says the oldest messages will be sent as a summary and suggests a new chat.
