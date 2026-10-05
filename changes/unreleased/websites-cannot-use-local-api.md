### Security

- Websites open in a browser on this computer can no longer use or read the Toskar API on `127.0.0.1`. The daemon now answers browsers only from its own web UI, the desktop app, and loopback addresses on its own port, or when the request carries an API key, and no longer sends `Access-Control-Allow-Origin: *`. It also refuses addresses that are not this computer's over loopback, which blocks DNS rebinding. toskarctl, the iPhone app, and other devices with a key work as before.
