# Design

How the web UI looks and behaves: the tokens it is built from, the components and patterns to reuse, and the rules new UI follows. The desktop app shows this same UI. The source of truth is `web/src/index.css` (tokens and component classes), `web/src/lib/realms.ts` (page realms), and `web/src/lib/designSystem.ts` (accent use). This page explains them; when they disagree, fix this page.

## Principles

- **Plain first, brand second.** A label says what something does. The Norse names are flavour: they head pages, sit in tooltips, and tell stories, but a sentence never depends on knowing them.
- **Never imply loss.** A page that is loading or failed to load never shows its empty state. "You have none" appears only once the data has loaded.
- **Safe by default.** Destructive or permission-granting dialogs start on the safe choice, and Escape picks it.
- **Everything works from the keyboard, at phone width, in both themes, in all 10 languages.** CI checks the first three; the i18n checks cover the fourth.

## Color

Colors are tokens on `:root`, switched by `data-theme` (`light` or `dark`) on `<html>`. Components use the Tailwind names (`text-ink-muted`, `bg-surface`), never raw hex. Dark is the primary brand surface; light mirrors it in cool gray-blue daylight.

### Surfaces and text

| Token | Dark | Light | Use |
| --- | --- | --- | --- |
| `canvas` | `#0E131A` | `#F0F4F8` | Page background |
| `surface` | `#111821` | `#F8FBFD` | Cards, panels, inputs |
| `raised` | `#18222E` | `#E2E9F0` | Hover, selected rows, secondary buttons |
| `sidebar` | `#080B10` | `#E4EAF0` | App chrome |
| `line` | `#273341` | `#C4D0DC` | Borders and dividers |
| `ink` | `#EEF2F6` | `#121C26` | Primary text |
| `ink-muted` | `#9AA7B5` | `#47576A` | Secondary text, descriptions |
| `ink-faint` | `#7E8B98` | `#5D6875` | Labels, captions, placeholders |

Every text token meets **4.5:1** (WCAG AA) on every surface it is used on, the sidebar included. `ink-faint` is the floor: nothing smaller or fainter is used for text.

### Actions and status

| Token | Dark | Light | Use |
| --- | --- | --- | --- |
| `primary` | `#4FD1C5` | `#105D5B` | Primary buttons, focus rings, active nav |
| `accent` | `#C6A15B` | `#695023` | Recommendations, "best" badges |
| `success` | `#5FBF8F` | `#1F5E48` | Ready, done |
| `warning` | `#D8A657` | `#6E4E1D` | Needs attention |
| `danger` | `#D96868` | `#913232` | Errors, destructive actions |
| `info` | `#6FA8DC` | `#325778` | Neutral notices |

In light, these also pass 4.5:1 on their own 15–20% tint, because status chips use them that way (`bg-success/15 text-success`).

### Realm accents

Each subsystem has an accent (`ygg`, `bifrost`, `norn`, `huginn`, `muninn`, `mimir`, `heimdall`, `gungnir`, `ratatoskr`). Use them for **chips, icons, dots, and realm kickers only, never for page fills or long text**. Dark mode uses the brand colors; light mode swaps in deeper text-safe values (for example Mimir `#C6A15B` becomes `#7F632C`).

### Changing a color

Keep the hue and move lightness to the nearest value that passes 4.5:1 on every surface the token sits on, in both themes. The accessibility check in CI fails if it doesn't.

## Type

| Role | Class or font | Notes |
| --- | --- | --- |
| Display | `font-display` (Outfit) | Page titles, section titles, card names |
| Body | `font-sans` (Figtree) | Everything else |
| Code | `font-mono` (IBM Plex Mono) | Code, IDs, logs, commands |
| Page title | `.page-title` | 28px, 32px from `sm`; one per page, an `h1` |
| Page subtitle | `.page-subtitle` | 15px, `ink-muted` |
| Section title | `.section-title` | 18px semibold; an `h2` |
| Label | `.label-caps` | 10px uppercase, tracked, `ink-faint`; sidebar groups and small headings |

Headings go in order: one `h1` per page, then `h2` for sections and cards directly under it (empty states included), then `h3`. Don't pick a heading level for its size; style it with a class.

## Spacing, shape, depth

- **Page gutter:** `px-page-x`, 2rem, 1rem below `md`. Pages scroll inside `.page-scroll`, never the window.
- **Section gap:** `--spacing-section`, 1.5rem.
- **Radius:** `rounded-lg` for controls, `rounded-xl` for cards and panels (`--radius-panel`), `rounded-full` for avatars and dots.
- **Shadow:** `var(--shadow-panel)` for cards and popovers. No other shadows.
- **Touch targets:** at least 24×24px everywhere, and 44×44px for the phone-width menu button and other primary touch controls.

## Components

Use these classes before writing new styles.

| Class | What it is |
| --- | --- |
| `.card` | A panel: surface, xl radius, panel shadow, 20px padding |
| `.card-outline` | A bordered card without a shadow, for calls to action such as "No tool sources yet. Add…" |
| `.btn-primary` | The one main action in a view |
| `.btn-secondary` | Other actions |
| `.btn-danger` | Destructive actions; never the default |
| `.field` | Text inputs and selects; focus is a primary border and ring |
| `.selectable`, `.selectable-active` | Choice cards (onboarding purposes, profiles); add `aria-pressed` or radio semantics |
| `.status-chip` | Small status pill, with a tinted background and a dot |
| `.realm-kicker` | The small Norse name above a page title (`RealmKicker` component) |
| `.empty-hero` | Layout for empty, error, and "page failed" states |
| `.composer`, `.composer-options` | The chat box and its Options panel |

React components to reuse:

| Component | Where | Use |
| --- | --- | --- |
| `EmptyState` | `components/ui` | A list with nothing in it yet: title, description, next action, optional mascot |
| `LoadError` | `components/ui` | A query that failed: "couldn't load this", Try again, Diagnostics, details |
| `PageErrorBoundary` | `components/layout` | Wraps every page; a crash shows Try again, and the sidebar keeps working |
| `Skeleton` | `components/ui` | Content loading: placeholder rows, cards, or chat bubbles in the shape of what's coming, with a screen-reader label |
| `LoadingSpinner` | `components/ui` | Work in progress (searching, detecting hardware, estimating), with a label that says what is happening |
| `Toggle` | `components/ui` | On/off settings, with the switch role |
| `LoreButton` | `components/ui` | A realm name or mascot that opens its story |

## Page structure

```text
RealmKicker (YMIR)          ← Norse name, accent color, opens its lore
h1 Page title               ← plain name, the same as the sidebar label
Subtitle                    ← one or two sentences: what this page is for
Sections (h2)               ← cards or groups
```

The window title follows the page ("Models · Toskar"); `AppLayout` sets it from the sidebar label. New pages add their path to `pageTitles` there and to `realms.ts`.

## States

Every view that loads data has four states, decided in this order:

1. **Failed, with nothing cached** → `LoadError` with the query's `error` and `refetch`. Never the empty state.
2. **Loading** → `Skeleton` in the content's shape (`rows`, `cards`, or `chat`) for lists and pages; `LoadingSpinner` for work in progress, such as a search. Never the empty state, and never a claim the page can't make yet ("Checking hardware…", not "Hardware details unavailable").
3. **Loaded and empty** → `EmptyState` with a next action.
4. **Loaded** → the content.

```tsx
{query.isLoading && <Skeleton label={t('page.loading')} />}
{query.isError && !query.data && (
  <LoadError error={query.error} onRetry={() => void query.refetch()} retrying={query.isFetching} />
)}
{!query.isLoading && !query.isError && items.length === 0 && <EmptyState … />}
```

Status indicators follow the same rule. The sidebar says "No model" only when the model list loaded and nothing is installed. Queries retry once, so a failure shows in about a second.

## Interaction patterns

| Pattern | Use | Behavior |
| --- | --- | --- |
| Modal dialog or drawer | `useDialog(open, onClose)` from `lib/useDialog` | Focus moves in (`data-autofocus`, `initialFocus`, or the first control), Tab stays inside, Escape closes, focus returns to the opener. Container gets `role="dialog"`, `aria-modal`, and `aria-labelledby`. |
| Safe default | `data-autofocus` on Cancel or Deny | Enter alone never deletes or grants; Escape on a permission request denies it |
| Tab list, radio group | `rovingKeyDown` on the container | One Tab stop (`tabIndex` 0 on the chosen item, -1 on the rest); arrows, Home, and End move and choose; RTL aware |
| Pop-up menu | `useMenu(openId, close)` and `rovingKeyDown` | Focus goes to the first item; arrows move; Escape closes back to the button; Tab closes |
| Popover (Options, notifications) | Local state | `aria-expanded` and `aria-controls` on the button; Escape and outside click close; Escape returns focus |
| Phone-width sidebar | `AppLayout` | Below `md`, a drawer opened from the top bar; the page behind is `inert` while it is open |

Focus is always visible: the global `:focus-visible` ring is primary teal, and anything that removes its outline (the chat box, the chat-list search) shows focus another way.

## Layout and breakpoints

- **Phone (<768px, `md`):** the sidebar becomes a drawer; the gutter is 1rem; every page fits 390px wide with no horizontal scroll outside code and tables. In a conversation the chat box starts at one line and grows with the text, up to 30% of the screen.
- **Store screenshots:** the phone captures (`data-form=phone`) show this same layout; capture-only CSS hides nothing but the transient jump-to-latest button.
- **Tablet and up:** the sidebar is always shown.
- **RTL:** use logical properties (`ms-`, `me-`, `ps-`, `start-`, `end-`); drawers slide from the inline start (`--ygg-inline-sign`).
- **Reduced motion:** animations and slides turn off under `prefers-reduced-motion`.

## Accessibility

Target: **WCAG 2.2 AA**.

CI enforces it. `node scripts/screenshots/a11y.mjs` runs axe-core in Chromium over every page and onboarding, in both themes at 1440 and 390px, and fails on any violation or page error. See [Development](development.md#test).

Beyond what axe can see:

- Every control has a visible label or an `aria-label`; a placeholder is not a label.
- Selected state is announced: `aria-selected` (tabs), `aria-checked` (radios), `aria-pressed` (toggle buttons and choice cards), `aria-current="page"` (nav).
- Live updates that matter use `role="status"` or `role="alert"`.

## Writing

- **Plain words for what things do.** "What this computer has for running AI models", not "hardware detected for local inference". Admin pages may use the terms their audience searches for (API key, endpoint, MCP, LoRA, GGUF).
- **Realm names are flavour.** They appear as kickers, in tooltips ("Bifrost · Computer discovery and networking"), and in lore, not as the subject of an explanation.
- **Say what happened and what to do.** Errors name the problem and the next step; raw server text goes under Details.
- **Every string is in all 10 catalogs** (`i18n/locales/<language>/`), with each language's plural forms. Nothing user-facing is written into the code.

## Brand

- **Identity:** winter night, fjord water, aurora, and old Norse metalwork. Dark is home.
- **Realms:** each page has a Norse figure and Elder Futhark rune (`lib/realms.ts`, stories in `lore.json`); see the [glossary](glossary.md).
- **Ratatoskr:** the mascot appears on empty and welcome states, at most once per view. Its states and rig are in [docs/brand/mascot](brand/mascot/README.md).

## Checklist for new UI

- [ ] Uses tokens and the classes above; no raw colors.
- [ ] Loading, failed, empty, and loaded states, in that order.
- [ ] Works from the keyboard: dialogs use `useDialog`, groups use `rovingKeyDown`, menus use `useMenu`, focus is visible.
- [ ] Fits 390px wide and works in both themes.
- [ ] Headings in order; every control labeled.
- [ ] Strings in all 10 languages.
- [ ] `node scripts/screenshots/a11y.mjs` passes.
