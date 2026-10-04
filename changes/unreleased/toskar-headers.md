### Changed

- API responses carry a `Toskar-Contract` header beside `Yggdrasil-Contract`, and clients may send `Toskar-Client-Contract` or `Yggdrasil-Client-Contract`. Webhooks are signed in `Toskar-Signature` and `Yggdrasil-Signature`, and carry `Toskar-Notification-Id` and `Yggdrasil-Notification-Id`, with the same values, so existing receivers keep working.
