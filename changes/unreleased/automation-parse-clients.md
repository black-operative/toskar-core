### Changed

- The Automations page reads a request on the computer, so the page,
  `toskarctl`, and chat read requests the same way. When its words can't
  find a schedule, such as "first thing on weekdays", a model reads the
  request, and the form says so, so you can check it before saving.

### Added

- `toskarctl automations parse "<request>"` prints what Toskar understood
  from a request, and `toskarctl automations create --request "<request>"`
  creates the automation from it.
