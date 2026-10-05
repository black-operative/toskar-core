### Changed

- Turning on local network access no longer asks apps on this computer for an API key. Requests over loopback (`127.0.0.1` or `::1`) work as before, so the desktop app, `toskarctl`, and a browser on this computer keep working. Other devices still need a key. A request carrying proxy forwarding headers (`Forwarded`, `X-Forwarded-For`, `X-Real-IP`) counts as coming from the network.
