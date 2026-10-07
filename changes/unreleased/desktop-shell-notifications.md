### Changed

- The desktop app can post desktop notices itself: with
  `TOSKAR_DESKTOP_NOTIFICATIONS=shell`, which the app sets on the service it
  starts, notices go to it as `notification.desktop` events, so they carry
  Toskar's name and icon, open the app when clicked, and work in the Mac App
  Store edition. Without it the service posts them as before.
