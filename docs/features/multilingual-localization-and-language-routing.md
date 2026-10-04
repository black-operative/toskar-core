# Toskar Core — Multilingual Localization and Language Routing

## Feature Specification

### Status

**Type:** Product Experience / Internationalization / Model Capability  
**Primary goal:** Make Toskar usable in as many languages as reasonably possible across the application UI, assistant responses, model routing, retrieval, notifications, and connected workflows.

**Primary subsystems:** Desktop/Mobile UI, Huginn, Muninn, Mimir, Gungnir, Norn, Gjallarhorn, Community Model Ratings.

---

## 1. Product Goal

Toskar should support multilingual users at three separate but coordinated layers:

```text
Application language
→ menus, buttons, dialogs, validation, dates, numbers, notifications

Assistant language
→ the language Toskar understands and responds in

Model language capability
→ how well a particular model performs in each language
```

The default experience should be:

```text
Install Toskar
        ↓
Detect system language
        ↓
Localize the UI
        ↓
Prefer responses in that language
        ↓
Route to a model that is strong in that language
        ↓
Fallback gracefully when necessary
```

> **Language should be a user preference and routing signal—not a barrier to using local AI.**

---

## 2. Design Principles

1. Do not invent a custom localization framework.
2. UI localization and model-language support are different concerns.
3. English may be the source locale, but the architecture must not be English-specific.
4. Core APIs should prefer stable codes/data over translated prose.
5. Explicit user language requests always override automatic behavior.
6. Model language ability is a routing signal, not a hard compatibility gate.
7. Missing translations must fall back safely.
8. Translation, memory, retrieval, tools, and notifications should preserve source-language provenance.
9. Normal users should not need to configure any of this.
10. Advanced users may override language and routing behavior.

---

# Part I — Application Internationalization

## 3. Standard i18n Stack

Use established libraries:

```text
Desktop React/Wails
→ i18next + react-i18next

Mobile Expo/React Native
→ i18next + react-i18next
→ expo-localization for system locale detection

Dates / numbers / currency
→ Intl

Advanced message formatting if needed
→ ICU MessageFormat
```

Toskar should not build its own pluralization, locale fallback, or date/number system.

---

## 4. Shared Translation Catalog

Desktop and Mobile should share translation resources where possible.

Suggested layout:

```text
packages/
  i18n/
    locales/
      en/
        common.json
        chat.json
        models.json
        computers.json
        automations.json
        notifications.json
        settings.json
        diagnostics.json
      es/
      de/
      fr/
      pt/
      ja/
      ko/
      zh-CN/
      zh-TW/
```

Use stable semantic keys:

```tsx
t("automations.runNow")
t("models.install")
t("notifications.markRead")
```

Avoid using English strings as keys.

---

## 5. No Hard-Coded UI Strings

Bad:

```tsx
<Button>New automation</Button>
```

Preferred:

```tsx
<Button>{t("automations.new")}</Button>
```

CI/linting should eventually detect obvious hard-coded user-facing strings.

---

## 6. Locale Detection and Fallback

On first launch:

```text
locale = system locale
```

Fallback:

```text
es-MX → es → en
pt-BR → pt → en
```

The UI should never expose a raw translation key to a normal user.

Use BCP 47-style language identifiers such as:

```text
en-US
es-MX
de-DE
ja-JP
```

---

## 7. Language Settings

Recommended normal-user settings:

```text
App language
● System default
○ English
○ Español
○ Deutsch
○ Français
...

Assistant language
● Same as app
○ Auto-detect from conversation
○ English
○ Español
...
```

The UI language and assistant language must remain separable.

---

# Part II — Locale-Aware Formatting

## 8. Dates, Numbers, Currency, and Plurals

Never build locale-sensitive strings manually.

Use `Intl` for:

- dates,
- times,
- relative time,
- numbers,
- currency,
- percentages.

Examples:

```text
en-US → Sep 30, 2026, 8:00 PM
de-DE → 30.09.2026, 20:00

en-US → 1,234.56
de-DE → 1.234,56
```

Do not implement English-only plural logic such as:

```text
count === 1 ? "computer" : "computers"
```

Use library/ICU plural rules.

---

## 9. Right-to-Left Support

The architecture should support RTL languages such as:

```text
Arabic
Hebrew
Persian
Urdu
```

Each locale should expose:

```text
direction = ltr | rtl
```

Requirements:

- layout mirroring,
- correct text direction,
- mixed LTR/RTL content,
- code blocks remaining LTR,
- selective icon mirroring,
- correct number rendering.

RTL does not have to be launch-complete in V1, but layout primitives should not make it impossible.

---

# Part III — Core API Language Neutrality

## 10. Stable Error and Status Codes

Core should prefer stable codes over translated UI text.

Preferred:

```json
{
  "code": "MODEL_NOT_FOUND",
  "model_id": "qwen3"
}
```

The client localizes the message.

Core may also provide technical details for logs/Diagnostics, but the stable code should drive normal UI presentation.

---

# Part IV — Assistant Language Policy

## 11. Language Resolution

Huginn should resolve the intended response language in this order:

1. explicit user request,
2. current conversation language,
3. assistant-language setting,
4. app language,
5. system locale,
6. English fallback.

Explicit user requests always win.

Example:

```text
UI language: German
Assistant default: German

User: "Answer this one in English."
→ English response
```

---

## 12. Automatic Language Detection

Huginn may detect the likely language of the current request/conversation.

Detection should preferably happen locally.

Do not require sending user content to an external translation service merely to identify the language.

---

# Part V — Model Language Capabilities

## 13. Language Capability Metadata

Models are not equally strong across languages.

Toskar should track language capability as model metadata.

Conceptual:

```json
{
  "language_capabilities": {
    "en": "excellent",
    "es": "good",
    "de": "good",
    "ja": "fair"
  }
}
```

Recommended normalized levels:

```text
Unknown
Limited
Fair
Good
Excellent
```

Do not imply scientific precision where none exists.

---

## 14. Capability Sources and Confidence

Language capability may come from:

- model cards,
- maintainer metadata,
- benchmark data,
- provider metadata,
- community ratings,
- local evaluation.

Retain provenance and confidence.

Example:

```text
Spanish: Good
Confidence: Medium
Source: model metadata + community ratings
```

---

# Part VI — Language-Aware Model Routing

## 15. Huginn Routing

Language should become one input to automatic model selection.

Routing may consider:

```text
task type
requested language
model language capability
hardware fit
health
speed
context length
tool support
community rating
profile preferences
```

Example:

```text
User asks in Spanish

Model A
Spanish: Excellent
Speed: Good
Fit: Good

Model B
Spanish: Fair
Speed: Excellent
Fit: Excellent
```

For a language-heavy task, Huginn may prefer Model A.

---

## 16. Graceful Fallback

If no strong language match exists:

```text
best language match unavailable
        ↓
best multilingual model available
        ↓
respond in requested language
        ↓
warn only if expected quality is materially reduced
```

Missing capability metadata must not make a model unusable.

---

# Part VII — Tokenization and Context

## 17. Language Token Efficiency

Different languages may consume different numbers of tokens on different tokenizers.

Muninn should use actual tokenizer estimates where practical rather than assuming equal text length means equal context cost.

Future routing may consider language token efficiency for:

- context budgets,
- long conversations,
- model selection,
- hosted-provider cost estimates.

---

# Part VIII — Multilingual Memory

## 18. Cross-Language Memory

Muninn should retrieve memories across languages.

Example:

```text
User in Spanish:
"Mi proyecto usa Go."

Later in English:
"What language does my project use?"

→ Go
```

Memory records may retain:

```text
original text
source language
normalized representation
```

UI may display original text, translated text, or both.

---

# Part IX — Multilingual Retrieval / RAG

## 19. Mimir Cross-Lingual Retrieval

Mimir should support:

```text
query language != document language
```

Example:

```text
Question: Spanish
Document: English
Response: Spanish
```

Where practical, use multilingual embeddings for cross-language semantic retrieval.

Indexed content should retain:

```text
detected language
original language
source metadata
```

---

# Part X — Tools, Speech, and Connectors

## 20. Tool Language Capabilities

Tools/providers should advertise supported languages when relevant.

Examples:

```json
{
  "capability": "speech.transcribe",
  "languages": ["en", "es", "de", "fr"],
  "auto_detect": true
}
```

Relevant tool families:

- speech-to-text,
- text-to-speech,
- translation,
- OCR,
- document generation,
- hosted AI providers.

Gungnir should route to a compatible provider automatically.

---

## 21. Connected Content

Connected-service content should remain in its original language unless the user requests translation.

Example:

```text
Email body: original language
Toskar UI around it: localized
```

Credentials and connector permissions remain independent of language preferences.

---

# Part XI — Notifications and Automations

## 22. Gjallarhorn Localization

System notifications should use translation keys rather than hard-coded English.

Example:

```text
notification.automation.failed
```

Scheduled AI results should use the automation's configured response language.

Possible modes:

```text
Same as account
Same as app
Auto
Explicit language
```

---

# Part XII — Community Model Ratings

## 23. Language-Specific Ratings

Community model ratings may optionally capture language context.

Example:

```text
How well did this model work for you in Spanish?
★★★★★
```

Future model cards may show language-specific aggregates when sample sizes are sufficient:

```text
Overall   4.6
Spanish   4.8
German    4.2
Japanese  4.1
```

These should contribute to Huginn's language-aware recommendations.

---

# Part XIII — Translation Workflow

## 24. Source Locale

Use English as the initial canonical source locale:

```text
en
```

Track translation state:

```text
complete
partial
machine translated
human reviewed
community reviewed
```

A locale may ship before 100% completion if fallback is reliable.

---

## 25. Community Translation

A future community workflow may use normal repository contributions:

```text
translation resources
        ↓
pull request
        ↓
review
        ↓
CI validation
        ↓
release
```

A translation-management service can be evaluated later; it should not be required for the core architecture.

---

## 26. Pseudo-Localization

Add a development pseudo-locale such as:

```text
en-XA
```

Example:

```text
Models
→ [!! Móóódéééls !!]
```

Use it to detect:

- hard-coded strings,
- clipped controls,
- layout assumptions,
- insufficient expansion space.

---

# Part XIV — Initial Language Strategy

## 27. Tiered Rollout

A practical initial target set:

### Tier 1

```text
English
Spanish
French
German
Portuguese
Italian
Japanese
Korean
Simplified Chinese
Traditional Chinese
```

### Tier 2

Potential expansion:

```text
Dutch
Polish
Swedish
Norwegian
Danish
Finnish
Czech
Turkish
Ukrainian
Russian
Indonesian
Vietnamese
Thai
Hindi
```

### Tier 3 / RTL Expansion

```text
Arabic
Hebrew
Persian
Urdu
additional languages based on demand
```

This is product-priority guidance, not a technical whitelist.

---

# Part XV — Testing and CI

## 28. Localization Validation

CI should eventually verify:

- translation JSON syntax,
- required source keys,
- placeholder consistency,
- plural/ICU syntax,
- duplicate keys,
- fallback behavior.

---

## 29. Layout Testing

Test representative languages:

```text
German → long labels
Japanese / Chinese → non-Latin dense layouts
Arabic / Hebrew → RTL
Pseudo-locale → expansion/clipping
```

Also test:

- large accessibility text,
- mixed translated prose + code,
- long automation names,
- notifications,
- error messages.

---

# Part XVI — Data/API Concepts

## 30. User Language Preferences

Conceptual:

```json
{
  "ui_locale": "es-MX",
  "assistant_language": "es",
  "assistant_language_mode": "same_as_ui"
}
```

---

## 31. Model Language Capability

Conceptual:

```json
{
  "model_id": "example-model",
  "languages": [
    {
      "language": "en",
      "level": "excellent",
      "confidence": "high"
    },
    {
      "language": "es",
      "level": "good",
      "confidence": "medium"
    }
  ]
}
```

---

# Part XVII — Related Toskar Features

## 32. Integration Points

This feature should coordinate with:

```text
ai-experience-platform.md
orchestration-layer-refactor.md
persistent-memory-and-cross-model-context.md
expanded-tool-platform.md
community-model-ratings.md
gjallarhorn-notification-system.md
scheduler-and-automations.md
```

Language is cross-cutting infrastructure rather than a separate isolated subsystem.

---

# Part XVIII — Implementation Phases

## 33. L0 — i18n Foundation

Implement:

- shared localization package,
- English source locale,
- locale detection,
- fallback rules,
- language settings,
- Intl formatting.

---

## 34. L1 — UI String Migration

Externalize user-facing Desktop/Mobile strings.

Prioritize:

```text
navigation
settings
chat
models
computers
automations
notifications
errors
```

---

## 35. L2 — Formatting and RTL Foundation

Implement:

- pluralization,
- dates/times,
- numbers/currency,
- relative time,
- RTL-aware layout primitives,
- pseudo-localization.

---

## 36. L3 — Assistant Language Policy

Add:

- assistant-language preference,
- language detection,
- per-request explicit override,
- scheduled-task response language.

---

## 37. L4 — Model Language Metadata

Add:

- language capability schema,
- provenance,
- confidence,
- model-catalog display.

---

## 38. L5 — Language-Aware Routing

Huginn incorporates language into model routing while still considering:

```text
task capability
hardware
health
speed
context
tools
```

---

## 39. L6 — Multilingual Mimir

Add:

- document-language metadata,
- multilingual embeddings where supported,
- cross-language retrieval.

---

## 40. L7 — Tool / Speech Language Metadata

Add supported-language metadata and routing for:

- STT,
- TTS,
- translation,
- document/OCR providers.

---

## 41. L8 — Community Language Quality

Extend community model ratings with optional language-specific feedback.

---

## 42. L9 — Translation Expansion

Add:

- more locale packs,
- community contribution workflow,
- optional translation-platform integration.

---

# Part XIX — V1 Scope

## 43. Include

V1 should include:

- standard i18n library integration,
- shared Desktop/Mobile translation resources,
- system-locale detection,
- language selector,
- English fallback,
- locale-aware formatting,
- pluralization,
- assistant-language preference,
- Huginn language policy,
- basic model language metadata,
- language-aware model routing,
- localized Gjallarhorn templates,
- pseudo-localization,
- CI translation validation.

---

## 44. Not Required for V1

Can follow later:

- perfect translation coverage for dozens of languages,
- automatic translation of all user content,
- external translation SaaS dependency,
- downloadable language packs,
- language-specific model fine-tuning,
- automatic translation of email/files,
- detailed tokenizer benchmarks for every language/model pair.

---

# Part XX — Acceptance Criteria

## 45. Application Localization

Complete when:

1. User-facing UI strings are externalized.
2. Desktop and Mobile share translation resources where practical.
3. System locale is detected automatically.
4. Users can override the app language.
5. Missing translations fall back safely.
6. Dates, times, numbers, currency, and plurals are locale-aware.
7. RTL support is architecturally possible.
8. Translation resources are validated in CI.
9. At least one non-English locale works end-to-end.

---

## 46. Assistant Language

Complete when:

1. Assistant language can default to app language.
2. Users can choose a different assistant language.
3. Explicit user language requests override defaults.
4. Huginn can detect likely conversation language.
5. Language preference persists across restarts/model changes.
6. Scheduled tasks preserve their configured response language.

---

## 47. Model Routing

Complete when:

1. Models can advertise language capabilities.
2. Capability data retains provenance/confidence.
3. Huginn uses language as a routing signal.
4. Hardware, task fit, health, context, and speed remain routing inputs.
5. Unknown language metadata does not block execution.
6. Fallback is graceful and bounded.

---

## 48. Multilingual Knowledge

Complete when:

1. Mimir records document/source language.
2. Multilingual content can be indexed.
3. Cross-language retrieval works where supported.
4. Source language remains available for provenance/citations.

---

# Part XXI — Product Outcome

Target experience:

```text
User installs Toskar on a German Mac
        ↓
UI opens in German
        ↓
User asks in German
        ↓
Huginn resolves German
        ↓
selects the strongest compatible model
        ↓
Muninn retrieves relevant context
        ↓
Mimir can retrieve English or German knowledge
        ↓
Toskar answers naturally in German
        ↓
dates, numbers, notifications, and UI stay localized
```

And mixed-language workflows should also work:

```text
UI: Spanish
Assistant: English
Documents: French
Model: multilingual
```

The user should not need to manually translate anything or understand which model/runtime makes that possible.

> **Toskar should make multilingual local AI feel automatic.**
