### Fixed

- Going to another page and back to Chat reopens the chat you were in, instead of starting a new one, unless you were away for more than 30 minutes. The context gauge keeps its reading when you come back, and after a restart it shows the reading saved with the chat's latest answer.

### Changed

- The client contract is 1.6: an answer's metadata can carry `context`, how full the model's window was for that answer, so clients can show the context gauge when a chat is opened again.
