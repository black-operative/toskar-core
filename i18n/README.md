# Translations

Toskar's text in every language: the web UI that the desktop app shows,
the desktop shell's menus, and the iPhone app. The iPhone app copies this
folder when it builds, so all of them share one catalog.

```text
i18n/
  languages.json            languages that can be chosen, with their direction
  glossary.json             the main terms, by the key that holds each one
  same-as-english.json      text meant to read the same as English, such as PDF
  locales/<language>/       one folder per BCP 47 tag, such as en, es, pt-BR
    common.json             navigation, status, and words used everywhere
    chat.json               Chat: the composer, history, progress, and errors
    settings.json           and one file per area of the app
    desktop.json            the desktop app's own menus, tray, and closing screen
    mobile.json             the iPhone app's text
    onboarding.json         the first-run setup
  requests/<language>.json  the words automation requests are read with, such as
                            "every morning at 8" (format: web/src/features/
                            automations/requestWords/types.ts); the daemon and
                            the web app both read them
    memory.json             the Memory page
    models.json             the Models page and what Chat says about a model
    computers.json          the Computers page
    performance.json        the Performance page
    automations.json        Automations and their schedules
    notifications.json      the bell, desktop notices, and email, push, and webhooks
    tools.json              the Tools page and tool sources
    services.json           connected services
    apiAccess.json          the API Access page
    knowledge.json          Knowledge sources, connections, and search
    train.json              training specialized AIs
    profiles.json           AI profiles, their strategies, tools, and orchestrators
    diagnostics.json        the health page, logs, caches, and what Toskar can do
    lore.json               the Norse names' stories, the mascot, and the logo
    errors.json             what each error code from the service means
```

The desktop app's shell copies this folder when it builds and reads
`desktop.json`; the web UI tells it the language, so its menus and tray
match the page.

The iPhone app keeps a copy of `languages.json` and every `mobile.json` in
`mobile/src/i18n/catalog.json` (yeixio/toskar-apps), which its build
refreshes from the matching core. Its App language follows the computer it
is connected to unless it is set on the phone.

English (`en`) is the source. Every other language has the same files and
keys; a key that is missing falls back to English, so a language can ship
before it is complete.

## Languages

Tier 1 of the spec (§27): English, German (`de`), Spanish (`es`), French
(`fr`), Italian (`it`), Brazilian Portuguese (`pt-BR`), Japanese (`ja`),
Korean (`ko`), Simplified Chinese (`zh-Hans`), and Traditional Chinese
(`zh-Hant`). All but English are machine translated (`status: machine`), and
Settings says so under the language picker until a person reviews one and
its status becomes `reviewed`.

A system language finds its catalog by exact tag, by base language (de-AT →
de), by script (zh-TW and zh-HK → zh-Hant, zh-CN → zh-Hans), or by another
region in the same script (pt-PT → pt-BR). Simplified and Traditional
Chinese never stand in for each other.

Two features read what people type, so a language brings more than its
catalog. Automations read a request in the App language or in English, with
each language's words in
`web/src/features/automations/requestWords/<tag>.ts`: schedule words,
weekdays, times of day and 24-hour times (18:30, 18 h, 18時, 오후 6시,
下午6点), intervals, notify conditions, and currency symbols (€, R$, ¥, ₩).
A language's `form.describePlaceholder` and `parse.describeWhen` in
`automations.json` are examples in that language, and a test checks that the
parser reads the example. A new language adds a words file beside the others
and lists it in `requestWords/index.ts`; a test fails until it does. The
import of pasted training examples reads each language's own labels
(`Q:`/`A:`, `P:`/`R:`, `F:`/`A:`, `问：`/`答：`, …): a language's
`material.placeholder` in `train.json` must use labels the importer knows
(`internal/training/dataset.go`), and a test checks that it does.

### Reviewing a translation

Read it in the app, fix wording in `locales/<tag>/*.json`, and set the
language's `status` to `reviewed` (or `community`) in `languages.json` in
the same pull request. The steps for contributors are in
[CONTRIBUTING.md](../CONTRIBUTING.md#translations).

Keep the glossary consistent: the same English term gets the same
translation everywhere. `glossary.json` lists Toskar's main terms by the
catalog key that holds each one, so a language's glossary is its own text:

```bash
python3 scripts/i18n.py glossary de
```

To see what a language still lacks, run `python3 scripts/i18n.py status`.
It lists keys that are missing and text that is the same as English, with
`--keys` for the keys themselves. Text that is meant to read the same, such
as `PDF`, `Git commit`, or German `Name`, is listed in
`same-as-english.json` (under `all` for every language, or under the
language) and isn't counted. When you keep such text on purpose, add its key
there; when you translate a listed key, remove it. CI checks that the
glossary and that list name keys English has, and that a language's listed
text still matches English.

## Keys

- The product is Toskar, written the same in every language, as are the Norse
  names. Yggdrasil is only the world tree, in the lore's stories; it was the
  product's name before the rename, and some keys, such as `lore:inYggdrasil`,
  still carry it.
- Keys are stable names, never the English text: `nav.models`, not
  `"Models"`. Changing the English wording keeps the key.
- Placeholders use `{{name}}`, and must match English in every language.
- Plurals use i18next suffixes: `key_one`, `key_other`, and `key_zero`,
  `key_two`, `key_few`, or `key_many` where a language needs them. English
  has `_one` and `_other`.

## Errors

The service sends errors with a stable code, such as `MODEL_NOT_INSTALLED`,
and the values the message needs, such as `{{model_id}}`. `errors.json` has
the text for each code; the web UI shows it in the App language and keeps the
service's English text for Diagnostics. A Go test fails when core sends a
code that `errors.json` lacks, or when `errors.json` has a code nothing sends.

`{{detail}}` is the service's own text, usually English and technical: an
entry that is only `{{detail}}` passes it on, because the page around it
already says what failed. Keep those as `{{detail}}`.

## Numbers, dates, and sizes

Numbers, dates, times, sizes, prices, and percentages are formatted with
`Intl` in the App language's region (`web/src/i18n/format.ts`), never by
hand: German writes 1.234,56, 30.09.2026, and 1,5 GB. Pass them into text
already formatted, as `{{size}}`, `{{when}}`, or `{{percent}}`; don't write
units or `%` into the catalog. A number passed as itself, such as
`{{count}}`, is formatted for the locale on the way in, except labels such
as `{{port}}` and `{{version}}`. Plural forms still choose by the number.

## No hard-coded text

Text people read comes from the catalog, never a literal in JSX.
`pnpm lint` in `web/` fails on text in JSX and on literals in attributes
people read or hear, such as `title`, `placeholder`, `label`, and
`aria-label` (§5 of the
[spec](../docs/features/multilingual-localization-and-language-routing.md)).
It leaves alone what is not prose: ids, routes, class names, URLs, acronyms,
and the Norse names, which are the same in every language.

An example of what to type, such as a URL or a command, is not translated.
Mark it on the line before, with the reason:

```tsx
// eslint-disable-next-line i18next/no-literal-string -- an example of what to type, not prose
placeholder="npx -y @scope/some-mcp-server"
```

The check reads JSX only. Text in plain `.ts` modules, such as labels for a
list of choices, goes through `i18n.t('namespace:key')` too.

## Adding a language

1. Add it to `languages.json` with its native name, `dir` (`ltr` or `rtl`),
   and `status`: `machine`, `partial`, `reviewed`, or `community`.
2. Copy `locales/en` to `locales/<tag>` and translate the values. Plural
   keys take every form the language uses (`Intl.PluralRules`): Spanish,
   French, Italian, and Portuguese add `_many`; Japanese, Korean, and Chinese
   have only `_other`.
3. Hermes on iOS has no `Intl.PluralRules`, so the iPhone app loads plural
   rules for each language: add the language's line to
   `mobile/src/i18n/plurals.ts` in yeixio/toskar-apps.
4. Copy `requests/en.json` to `requests/<tag>.json` and translate the words,
   so automation requests can be written in the language. Add its cases to
   `web/src/features/automations/parseRequest.test.ts` and
   `internal/automations/request_test.go`.
5. Run `pnpm test` in `web/`. It checks every file against English: valid
   JSON, no duplicate keys, no keys English lacks, the same placeholders,
   and every plural form the language uses.

## Pseudo-locales

`en-XA` is generated from English, not stored: accented, padded about 30%,
and bracketed, such as `[!! Mööödéééls !!]`. Choose it in Settings in
advanced mode to find text that is not translated or does not fit.

`ar-XB` lays the page out right to left with each English word reversed, as
Android's pseudo-locale of that name does: `sledoM`. Choose it in Settings
in advanced mode to find layout that does not mirror. Text that still reads
left to right is not from the catalog.

## Right to left

A language's `dir` in `languages.json` sets `dir` on the page, and the web UI
mirrors from there:

- Use logical sides in class names: `ms-`/`me-`, `ps-`/`pe-`, `start-`/`end-`,
  `text-start`/`text-end`, `border-s`/`border-e`, `rounded-s`/`rounded-e`.
  A test fails on `ml-`, `pr-`, `left-`, `text-right`, and the like.
  Centering (`left-1/2` with `-translate-x-1/2`) stays as it is.
- An arrow or chevron that points along the reading direction mirrors with
  `inline-block rtl:-scale-x-100`. Don't write such arrows into the catalog;
  steps in a row are joined with `formatSequence`, which points the arrow the
  way the language reads. Menu paths in sentences, such as Settings →
  Developer, are written by the translator with the arrow their language uses.
- Code, commands, paths, and logs (`pre`, `code`, `.font-mono`) stay left to
  right in every language.
- Text people write, such as chat messages and memories, takes `dir="auto"`,
  so Arabic reads right to left in an English UI and English left to right
  in an Arabic one.
