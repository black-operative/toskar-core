### Added

- Automations can run when another service calls their webhook link: pick
  "When another service calls its link (webhook)" under "Runs", then make
  the link on the automation's page. It's shown once, only a hash of it is
  kept, and making a new one stops the old one. The request's body, up to
  64 KB, is given to the task as data. `toskarctl automations hook <id>`
  makes a link from the command line.
- A "Only when started" schedule, for an automation that runs only from
  Run now or its webhook.

### Security

- A run started by what a trigger delivered (a webhook's body, a changed
  page, new feed posts, or changed files) can't use tools that change
  things outside Toskar, even ones approved for that automation: someone
  else wrote what it read. Those tools are skipped and reported, as
  unapproved tools are.
