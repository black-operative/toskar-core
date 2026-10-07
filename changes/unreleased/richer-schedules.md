### Added

- Automations can run on several weekdays, such as weekdays only, at
  several times a day, monthly on a day of the month (a shorter month uses
  its last day), or on a cron expression. The API takes them as
  `weekdays`, `times`, `month_day`, and `cron`, and so does `toskarctl
  automations create --schedule monthly --day 1` or `--weekday weekdays
  --at 08:00,17:00` or `--schedule cron --cron "0 9 * * 1-5"`. The model
  that reads requests the words can't can now give these schedules too.
