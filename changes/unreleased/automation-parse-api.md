### Added

- `POST /api/v1/automations/parse` reads a request such as "every morning at
  8, tell me if the price is below $500" into an automation's name, task,
  schedule, and notification, in any of the App's languages or English. It
  uses the same words as the Automations page, now kept in
  `i18n/requests/`, so the page, `toskarctl`, and chat can read requests the
  same way.
