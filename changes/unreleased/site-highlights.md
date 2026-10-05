### Added

- `site/highlights.json` holds the latest minor release's highlights, which toskar.ai shows as "New in 1.6" instead of a list written into the site. A new minor release updates it in the same pull request: `scripts/changelog.py check` fails until its version matches the latest release in `CHANGELOG.md`.
