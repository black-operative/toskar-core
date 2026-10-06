### Added

- A phone can connect with a 6-digit code instead of a copied API key: the
  computer shows the code (`POST /api/v1/devices/pairing`), and the phone
  sends it from the local network (`POST /api/v1/devices/pair`) to get a key
  of its own, named after it. A phone's key reaches only chat, its
  conversations, and the lists the phone reads.
