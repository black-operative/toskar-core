# Contributing

Toskar Core is the headless control plane. Changes should fit that role: runtimes, models, hardware detection, pairing, placement, health, and the HTTP API. Desktop and mobile clients live in other repositories, but every app's text is translated here (see [Translations](#translations)).

## Contributor License Agreement

Contributors retain copyright ownership of their contributions.

By submitting a contribution to Toskar Core, you agree to the [Toskar Contributor License Agreement](CLA.md), which grants YEIXIO LLC the rights necessary to use, modify, distribute, sublicense, and relicense contributed code, including as part of commercial offerings.

This allows Toskar Core to remain open source under the AGPL while preserving future commercial licensing options.

Pull requests include a checkbox for that agreement. Check it before requesting review.

## Development prerequisites

Go 1.26.3+, Node.js 22, and pnpm 9. Details, commands, and CI are in [docs/development.md](docs/development.md).

## Repository setup

```bash
git clone https://github.com/yeixio/toskar-core.git
cd toskar-core
make start
```

## Build

```bash
make daemon
```

`make package-headless` builds a headless package for the machine you are on. The release script is `scripts/build/package-core-release.sh`.

## Run

```bash
make start
```

Open `http://127.0.0.1:7331`. `make help` lists the other targets.

## Test

```bash
make test
cd web && pnpm test
```

## Lint

```bash
make lint
make vet
```

`make lint` runs golangci-lint v2.14.0 and `pnpm lint` in `web/`. CI runs the same checks. ESLint warnings are reported and do not fail the job.

## Formatting

```bash
make fmt
```

## Submitting an issue

Use the forms in `.github/ISSUE_TEMPLATE/`. Blank issues are disabled. Pick the form that matches the report: bug, hardware, performance, runtime, feature, or documentation. Strip secrets from logs. [SUPPORT.md](SUPPORT.md) says which form to use.

## Repository Labels

GitHub issue labels are defined in `.github/labels.yml`.

Maintainers can synchronize them with:

    ./scripts/sync-github-labels.sh

Running the script requires the GitHub CLI and permission to manage
labels in the repository.

Do not automatically run this workflow on contributor pull requests,
because repository label changes require elevated GitHub permissions.

## Submitting a pull request

Use the pull request template. Check "I agree to the Toskar Contributor License Agreement." Link an issue when there is one. Describe how you tested and on which platform. Keep the change focused.

This repository does not include an `AGENTS.md`. Follow the style of the package you are editing. See [docs/development.md](docs/development.md).

## Runtime adapter contributions

Read [docs/runtimes.md](docs/runtimes.md). An adapter implements `pkg/pluginapi.Runtime` and needs tests for detection and lifecycle. Open a runtime request first if the backend is new, so scope and licensing can be discussed before a large patch.

## Hardware compatibility reports

Use the hardware issue form. Say whether it works, works with limitations, does not work, or you are not sure. Include OS, CPU, GPU, memory, Toskar version, runtime, and models. This surface is large, and a careful report is a real contribution.

## Documentation contributions

User-facing behavior belongs in `docs/user-guide/guide.json` when it changes the public guide, and in `docs/` when it explains the daemon. Run the doc checks in [docs/user-guide/README.md](docs/user-guide/README.md) if you touch the guide.

## Translations

Every app's text is in one catalog in this repository, `i18n/locales/<language>/`: the web UI, the desktop app's menus, and the iPhone app's screens. English is the source, and the other languages started as machine translations. A person who reads a language well can make it better than any machine. [i18n/README.md](i18n/README.md) has the catalog's rules.

Translations go through the same steps as code: a pull request, a review, CI, and then a release.

1. **Find the text.** Search `i18n/locales/<language>/` for the words you see, then change the same key in that file. Don't change the key or the English text unless the English is what's wrong.
2. **Keep the terms consistent.** `python3 scripts/i18n.py glossary <language>` prints the words your language uses for Toskar's main terms, such as Model, Auto, and Knowledge. Use the same word everywhere. To change a term, change it in every file in one pull request.
3. **Keep what the app fills in.** `{{placeholders}}` stay exactly as in English, though they can move within the sentence. Plural keys need every form your language uses: Spanish, French, Italian, and Portuguese add `_many`; Japanese, Korean, and Chinese have only `_other`.
4. **See what's left.** `python3 scripts/i18n.py status <language> --keys` lists keys that are missing (shown in English until translated) and text that is still the same as English. Text that is meant to stay that way, such as `PDF`, `Git commit`, or a word your language shares with English, is listed in `i18n/same-as-english.json` and isn't counted. Add a key there when you keep its text on purpose, and remove it when you translate one.
5. **Check.** Run `pnpm test` in `web/`. It checks every language against English: valid JSON, no duplicate keys, the same placeholders, and every plural form.
6. **Open a pull request.** Say which language, and how you checked it: in the app, or by reading the files. If you reviewed a whole language, set its `status` in `i18n/languages.json` to `reviewed`. Settings then stops saying it was machine translated.

A fix in one language is reviewed by someone who reads it, when one is around. Otherwise a maintainer checks that the change is consistent with the glossary and passes CI. A change ships in the next release of core. The desktop and iPhone apps pick up the catalog the next time they're built.

**A new language** starts as an issue on the [translation form](.github/ISSUE_TEMPLATE/translation.yml), so the work can be shared. It follows "Adding a language" in [i18n/README.md](i18n/README.md). Before it's complete, a language can ship as `partial`, because missing keys fall back to English.

If you'd rather not edit files, report the text on the translation form, with a screenshot if you can.

## Security reports

Do not file a public issue for a vulnerability. Use [SECURITY.md](SECURITY.md).

## Good first contributions

- hardware testing on a machine you already have
- reviewing the translation in a language you read (see [Translations](#translations))
- runtime support notes, including Windows GPU builds versus the CPU archive the installer selects
- documentation and examples
- platform packaging (Windows archives, confirming the Homebrew formula on the default branch)
- tests around runtime asset selection and API examples
- bug fixes with a reproduction

## Looking for something to work on?

Start with issues labeled [good first issue](https://github.com/yeixio/toskar-core/issues?q=is%3Aissue+is%3Aopen+label%3A%22good+first+issue%22), [help wanted](https://github.com/yeixio/toskar-core/issues?q=is%3Aissue+is%3Aopen+label%3A%22help+wanted%22), [hardware](https://github.com/yeixio/toskar-core/issues?q=is%3Aissue+is%3Aopen+label%3Ahardware), or [documentation](https://github.com/yeixio/toskar-core/issues?q=is%3Aissue+is%3Aopen+label%3Adocumentation).

Hardware reports are genuine contributions. Toskar has a large heterogeneous hardware surface, and CI does not generate tokens on those GPUs. A report that names the machine, the model, and the result is more useful than a guess.

Suggested starter issues are listed in [.github/SEED_ISSUES.md](.github/SEED_ISSUES.md). Those are drafts. They are not already filed on GitHub.
