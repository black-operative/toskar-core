### Fixed

- A page that can't load its data says so, with Try again and a link to Diagnostics, instead of looking empty. Before, a failed request showed "No computers", "No model installed", or "No tool sources yet", as if they were gone, or kept saying Loading. This covers Chat, Automations, Models, Train, Knowledge, Memory, Computers, Performance, Profiles, and Tools.
- The sidebar's status no longer says "No model" or "No computers" before it knows; while those are loading or can't be read, it doesn't claim either. A failed request is retried once instead of three times, so its error shows in about a second.
