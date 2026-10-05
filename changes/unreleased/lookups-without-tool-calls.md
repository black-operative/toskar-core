### Fixed

- A model that can't call tools, such as Gemma 2, gets current answers again. Choosing one turned off every tool, including the web, places, and service look-ups Toskar runs itself before the model answers, so it answered from memory ("I don't have access to real-time information"). Those look-ups now run for every model; a model that can't call tools just isn't asked to.
