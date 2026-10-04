### Changed

- The OpenAI-compatible API takes the assistant's controls in a `toskar` object. The `yggdrasil` object still works, and `toskar` wins when a request sends both. Responses and streamed chunks carry the answer's sources, steps, and progress under both names.
