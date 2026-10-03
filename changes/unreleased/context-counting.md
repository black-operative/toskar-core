### Fixed

- The context gauge counts everything in the system prompt, including your personalization, memories, and guide excerpts, which it left out when estimating.
- Chats with an external server show the real prompt size: Yggdrasil asks the server for its token counts at the end of each reply.
- When no tokenizer is available, the estimate is closer for Chinese, Japanese, and Korean (about a token per character, not a quarter) and for code and JSON.
