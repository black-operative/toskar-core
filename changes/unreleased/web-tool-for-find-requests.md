### Changed

- Chat looks things up on the web before answering when a message asks it to find something: a recommendation, where to buy, what something costs, reviews, a result or a release, or "check" and "find" for me. Before, these were answered from the model's memory, which for a small model often meant naming products that don't fit or telling you to search yourself.
- A follow-up such as "Can you provide a link to that bike?" is searched by what it refers to: the model writes the search from the conversation, so it names the bike.
- An answer that still tells you to search, check websites, or pretends to browse is looked up and written again from what the web says. Small talk such as "Hi! How are you?" is never looked up.
