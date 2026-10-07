### Changed

- The computer adds what an automation's condition needs, such as the price
  in the threshold's currency, when each run starts, so automations made
  with `toskarctl`, chat, or the API check their conditions the same way as
  ones made on the page. A saved prompt is only the task.
- A run's history says why it did or didn't notify as the computer decided
  it, instead of the page guessing from the result. Runs record it as
  `notify_detail` and `notify_values`.
