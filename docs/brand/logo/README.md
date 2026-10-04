# The Toskar mark

Yggdrasil, the world tree, drawn as circuit traces: a 100-unit grid, 45° geometry only, and round pads with a clearance ring. Colors come from `web/src/index.css`.

| File | Use | Replaces |
| --- | --- | --- |
| `toskar-mark.svg` | Primary mark on dark surfaces: boot splash, sidebar, README | `web/public/yggdrasil-mark.png`, `yggdrasil-logo.png` |
| `toskar-mark-light.svg` | The same mark on light surfaces | — |
| `toskar-mark-mono.svg` | One color, using `currentColor` | — |
| `toskar-mark-small.svg`, `-small-light.svg` | 32 px and below | — |
| `toskar-icon.svg`, `-maskable.svg` | Sources for the app icons (#0B0F14 tile) | — |
| `favicon.svg` | Browser tab. It switches colors with `prefers-color-scheme`. | New. Add `<link rel="icon" href="/favicon.svg" type="image/svg+xml">` before the PNG links |
| `png/favicon.ico`, `favicon-16x16.png`, `favicon-32x32.png` | Fallback favicons in a mid-tone teal | `web/public`, same names |
| `png/apple-touch-icon.png`, `icon-192.png`, `icon-512.png`, `icon-maskable-192.png`, `icon-maskable-512.png` | Home screen and PWA | `web/public`, same names |
| `png/toskar-logo.png` (1024), `png/toskar-mark.png` (512) | Where SVG can't be used, and `docs/brand/toskar-logo.png` | `docs/brand/yggdrasil-logo.png`, `web/public` |

## Rules

- Use the small version at 32 px and below.
- Don't add outlines, glows or gradients. The clearance ring separates the pads from the traces on any background.
- Leave clear space of at least one pad's width (about 8% of the mark's width) on every side.

The files were named `yggdrasil-*` before the rename to Toskar (#237). The mark itself did not change.
