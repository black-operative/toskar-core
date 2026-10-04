# Toskar Core Versioning Rules

## Purpose

This document defines when and how Toskar Core version numbers change.

The goals are to make releases predictable, communicate compatibility clearly, keep pre-release numbering consistent, make tags and binaries traceable to exact source revisions, avoid arbitrary version bumps, and keep release automation simple.

Toskar Core uses **Semantic Versioning (SemVer)** with standard pre-release identifiers.

---

## 1. Version Format

Stable releases use:

```text
MAJOR.MINOR.PATCH
```

Examples:

```text
1.2.0
1.2.1
1.3.0
2.0.0
```

Git tags use a leading `v`:

```text
v1.2.0
v1.2.1
v1.3.0
v2.0.0
```

Pre-release versions use:

```text
MAJOR.MINOR.PATCH-PRERELEASE.NUMBER
```

Examples:

```text
1.3.0-alpha.1
1.3.0-alpha.2
1.3.0-beta.1
1.3.0-beta.2
1.3.0-rc.1
```

Git tags:

```text
v1.3.0-alpha.1
v1.3.0-beta.1
v1.3.0-rc.1
```

---

## 2. Semantic Versioning

### MAJOR

Increment `MAJOR` when a stable release introduces incompatible changes to a supported public interface.

Examples:

- removing or renaming a previously supported API endpoint,
- changing an API response in a way existing clients cannot reasonably tolerate,
- changing authentication behavior in a way that requires client changes,
- removing a supported configuration key without compatibility handling,
- changing a stable plugin/runtime interface incompatibly,
- changing persisted data in a way that requires an explicit migration and cannot preserve compatibility,
- removing a documented command or flag that existing automation may depend on.

Example:

```text
1.8.4 → 2.0.0
```

### MINOR

Increment `MINOR` when adding backward-compatible functionality.

Examples:

- adding a new API endpoint,
- adding a new runtime adapter,
- adding Kubernetes support,
- adding persistent memory,
- adding new CLI commands,
- adding optional configuration,
- adding support for new hardware,
- adding an additional orchestration capability.

Example:

```text
1.2.3 → 1.3.0
```

When `MINOR` changes, reset `PATCH` to zero.

### PATCH

Increment `PATCH` for backward-compatible fixes.

Examples:

- crash fixes,
- memory leaks,
- incorrect placement decisions,
- packaging fixes,
- model download fixes,
- security fixes that do not intentionally change the public API contract,
- documentation corrections shipped with a maintenance release,
- platform compatibility fixes.

Example:

```text
1.3.0 → 1.3.1
```

---

## 3. Public Compatibility Surface

Versioning decisions should be based on the interfaces users and integrations can reasonably depend on.

For Toskar Core this includes, where documented as supported:

- `/api/v1` routes,
- OpenAI-compatible `/v1` routes,
- CLI commands and flags,
- configuration files and environment variables,
- persisted database/state formats,
- plugin/runtime adapter interfaces,
- Bifrost protocol behavior,
- externally consumed event formats,
- package names and supported installation paths.

Internal Go packages are not automatically considered stable public APIs unless explicitly documented as such.

A refactor of internal implementation does not require a major release merely because internal function signatures changed.

---

## 4. Breaking Changes

A change should be treated as breaking when a reasonable existing user or integration must change in order to continue working.

Typical breaking changes:

```text
DELETE /api/v1/foo
→ breaking

rename JSON field "node_id" to "node"
→ breaking unless compatibility is retained

remove environment variable YGGDRASIL_API_HOST
→ breaking

change config syntax without automatic migration
→ breaking
```

Not every behavior change is automatically breaking.

Examples that are normally compatible:

- adding an optional JSON field,
- adding a new endpoint,
- adding a new enum value when consumers are expected to tolerate unknown values,
- improving scheduling decisions,
- fixing behavior that contradicted documented behavior,
- adding stricter validation for invalid input that was never documented as supported.

When uncertain, prefer compatibility or document the change prominently.

---

## 5. Pre-1.0 Development

If Toskar Core is below `1.0.0`, compatibility expectations may be looser, but version changes should still communicate intent.

For `0.x.y` releases:

```text
0.MINOR.PATCH
```

Recommended interpretation:

- `MINOR` may contain breaking changes,
- `PATCH` should remain backward-compatible whenever practical.

Example:

```text
0.8.2 → 0.9.0
```

may include a breaking API adjustment.

However, pre-1.0 status should not be used as an excuse for arbitrary breakage. If a public interface is already being used, prefer migration paths and release notes even before 1.0.

---

## 6. Pre-Release Stages

Toskar uses three standard pre-release stages:

```text
alpha
beta
rc
```

### Alpha

Use `alpha` when major functionality is incomplete, unstable, or still changing rapidly.

Example:

```text
v1.4.0-alpha.1
```

Appropriate when:

- architecture is still moving,
- APIs may change,
- major functionality is incomplete,
- compatibility has not stabilized,
- releases are primarily for developers and early testers.

Alpha numbering starts at `alpha.1` and increments monotonically.

### Beta

Use `beta` when planned release functionality is substantially present and the focus has shifted toward testing, compatibility, performance, reliability, packaging, documentation, and bug fixing.

Example:

```text
v1.4.0-beta.1
```

A beta may still receive feature changes, but large architectural changes should become increasingly uncommon.

Beta numbering starts independently at `beta.1`.

Example:

```text
v1.4.0-alpha.4
v1.4.0-beta.1
```

### Release Candidate

Use `rc` when the code is believed to be suitable for the stable release unless a release-blocking problem is found.

Example:

```text
v1.4.0-rc.1
```

During RC:

- avoid new features,
- fix release blockers,
- verify packaging,
- verify upgrade behavior,
- verify installation instructions,
- verify release automation,
- finalize documentation.

If an RC requires a fix:

```text
v1.4.0-rc.1
v1.4.0-rc.2
```

Do not modify or replace the original RC tag.

---

## 7. Pre-Release Progression

The normal progression is:

```text
v1.4.0-alpha.1
v1.4.0-alpha.2
v1.4.0-beta.1
v1.4.0-beta.2
v1.4.0-rc.1
v1.4.0
```

Not every release needs every stage.

Examples:

Small feature release:

```text
v1.5.0-beta.1
v1.5.0
```

Small patch release:

```text
v1.5.1
```

High-risk patch:

```text
v1.5.1-rc.1
v1.5.1
```

Use the amount of pre-release testing appropriate to the risk.

---

## 8. Choosing the Next Version

Determine the next version from the largest compatibility impact included in the release.

If the current stable release is:

```text
v1.4.2
```

and the next release contains only fixes:

```text
v1.4.3
```

If it contains backward-compatible features:

```text
v1.5.0
```

If it contains an intentional incompatible public-interface change:

```text
v2.0.0
```

The release version is determined by the most significant included change.

---

## 9. Decide the Target Version Before Pre-Releases

Once a pre-release series begins, choose the intended stable version first.

Correct:

```text
v1.6.0-beta.1
v1.6.0-beta.2
v1.6.0
```

If release scope changes enough that the target version must change, begin a new pre-release series under the correct version.

Example:

```text
v1.6.0-beta.2
```

then a breaking stable-interface change becomes necessary:

```text
v2.0.0-beta.1
```

---

## 10. Pre-Release Numbering

Each pre-release phase has its own monotonically increasing integer.

Correct:

```text
v1.7.0-alpha.1
v1.7.0-alpha.2
v1.7.0-beta.1
v1.7.0-beta.2
v1.7.0-rc.1
```

Never reuse a pre-release number.

If `beta.1` is bad, fix it and publish `beta.2`.

Do not delete and recreate `beta.1`.

---

## 11. Stable Promotion

A stable release is a new immutable tag.

Example:

```text
v1.8.0-rc.2
v1.8.0
```

If no source change occurs between RC and stable, both tags may point to the same commit.

That is acceptable.

If documentation or release metadata changes before stable, merge those changes normally and tag the resulting commit.

---

## 12. Patch Release Rules

Patch releases should be narrowly scoped.

A patch release should normally contain:

- bug fixes,
- security fixes,
- regression fixes,
- packaging corrections,
- minor compatibility improvements,
- safe documentation corrections.

Avoid adding substantial new features to a patch release.

Example:

Current:

```text
v1.8.0
```

Bug fixes only:

```text
v1.8.1
```

Add Kubernetes model deployment:

```text
v1.9.0
```

not:

```text
v1.8.1
```

---

## 13. Security Releases

Security fixes follow normal versioning based on compatibility impact.

Example:

Current:

```text
v1.9.2
```

Compatible vulnerability fix:

```text
v1.9.3
```

If the only safe remediation requires an incompatible public-interface change, the change may require a major release.

Security release notes should identify affected versions, fixed versions, upgrade urgency, and any required mitigation.

Sensitive details may be withheld until coordinated disclosure.

---

## 14. Release Branch Versioning

Maintenance branches use the version line they maintain.

Example:

```text
main
  → future v1.10.0

release/1.9
  → v1.9.3
  → v1.9.4
```

Do not release `v1.10.x` from `release/1.9`.

Do not release an older `v1.9.x` maintenance patch from `main` if `main` already contains unreleased `1.10` features.

---

## 15. Release Tags

Official releases use tags of the form:

```text
vMAJOR.MINOR.PATCH
```

or:

```text
vMAJOR.MINOR.PATCH-alpha.N
vMAJOR.MINOR.PATCH-beta.N
vMAJOR.MINOR.PATCH-rc.N
```

Release tags are immutable.

Never:

- move a published tag,
- force-update a release tag,
- delete and recreate a release tag to change its contents.

If a release is wrong, publish another release.

---

## 16. GitHub Release Names

Use the version as the release name.

Examples:

```text
Toskar Core v1.10.0
Toskar Core v1.10.1
Toskar Core v1.11.0-beta.1
```

GitHub pre-release status should be enabled for alpha, beta, and RC releases.

Stable releases should not be marked pre-release.

---

## 17. Version Source of Truth

The Git tag is the source of truth for official release versions.

Avoid manually maintaining the same version in many files.

Preferred build behavior:

```text
git tag
   ↓
release workflow
   ↓
build injects:
version
commit SHA
build metadata
```

A development build may report a clearly non-release identifier such as:

```text
0.1.0-dev
```

Official binaries should report at least version and source commit.

---

## 18. Build Metadata

SemVer supports build metadata:

```text
1.10.0+abcdef1
```

Toskar may use build metadata internally or in development binaries, but official public release tags should remain simple.

Preferred public tag:

```text
v1.10.0
```

Preferred binary diagnostics:

```text
Version: 1.10.0
Commit: abcdef123456
```

---

## 19. Development Versions

Development builds must not accidentally identify themselves as an official release.

Acceptable examples:

```text
0.1.0-dev
1.11.0-dev
1.11.0-beta.2-dev
```

A dirty working tree may optionally be shown diagnostically.

Do not publish an official release artifact from a dirty working tree.

---

## 20. Changelog Rules

Each stable release should have clear release notes or a changelog entry.

Pre-releases should also document meaningful changes when they are intended for external testing.

Recommended categories:

```text
Added
Changed
Fixed
Performance
Security
Deprecated
Removed
Breaking Changes
```

Breaking changes should be impossible to miss.

Migration instructions should accompany breaking changes whenever practical.

---

## 21. Deprecation Policy

Prefer deprecation before removal for stable public interfaces.

Typical sequence:

```text
v1.8.0
Feature is supported

v1.9.0
Feature is deprecated
Warning/documentation added

v2.0.0
Feature may be removed
```

Emergency security issues may justify faster removal.

---

## 22. Versioning API Changes

Compatible API addition:

```text
Add GET /api/v1/automations
```

Impact:

```text
MINOR
```

Compatible response extension:

```json
{
  "id": "node-1",
  "status": "ready",
  "accelerator": "apple"
}
```

where `accelerator` is newly added and consumers are expected to ignore unknown fields.

Impact:

```text
MINOR
```

Incompatible field rename:

```json
{
  "node_id": "node-1"
}
```

becomes:

```json
{
  "node": "node-1"
}
```

without compatibility handling.

Impact:

```text
MAJOR
```

for a stable supported API.

---

## 23. Versioning Configuration Changes

Adding a new optional configuration value:

```text
MINOR
```

Removing or renaming a documented stable configuration value:

```text
MAJOR
```

unless compatibility or automatic migration preserves existing behavior.

Fixing a configuration value that never worked as documented:

```text
PATCH
```

with a release note if users may notice the behavior change.

---

## 24. Versioning Database Changes

Database migrations do not automatically require a major release.

A migration can ship in a minor or patch release if:

- it is automatic,
- existing supported data is preserved,
- upgrade behavior is tested,
- downgrade expectations are documented where relevant.

A database change becomes breaking when users must manually discard, rewrite, or migrate supported state and compatibility cannot be preserved.

---

## 25. Versioning Bifrost / Node Protocol Changes

Changes to Bifrost should consider mixed-version clusters.

Preferred behavior:

- negotiate capability/version where practical,
- preserve compatibility across reasonable adjacent versions,
- reject incompatible peers clearly,
- document compatibility windows.

A protocol change that makes supported existing nodes unable to communicate may require a major release once the protocol is considered stable.

During beta, incompatible protocol changes may occur more frequently, but they should still be documented.

---

## 26. Versioning Experimental Features

Features explicitly documented as experimental may evolve without the same compatibility guarantees as stable interfaces.

Examples may include:

- early Grid experiments,
- research-only distributed inference,
- preview Kubernetes APIs,
- unstable plugin interfaces.

Experimental status must be clear in documentation.

Do not label widely used stable behavior experimental merely to avoid proper versioning.

---

## 27. Documentation-Only Changes

Documentation changes alone normally do not require a release.

Examples:

- typo corrections,
- additional examples,
- contributor documentation,
- architecture notes.

If package metadata or distributed documentation inside an artifact must change, a patch release may be appropriate.

---

## 28. Packaging Changes

Packaging corrections generally count as patches when they repair installation or distribution without changing product functionality.

Examples:

```text
broken .deb dependency metadata
incorrect Homebrew checksum
missing Windows archive
wrong RPM install path
```

Typical impact:

```text
PATCH
```

Adding an entirely new supported distribution method may be included in a minor release.

Example:

```text
first official Kubernetes Helm chart
```

would normally be a `MINOR` feature.

---

## 29. Release Decision Examples

Current stable:

```text
v1.4.2
```

Fix llama.cpp cleanup after crash:

```text
v1.4.3
```

Add persistent memory:

```text
v1.5.0
```

Add scheduler and automations:

```text
v1.6.0
```

Add Kubernetes deployment support:

```text
v1.7.0
```

Remove a stable API endpoint:

```text
v2.0.0
```

Fix a security vulnerability without changing public contracts:

```text
v1.4.3
```

Change an undocumented internal Go interface:

```text
No version impact by itself
```

---

## 30. Pre-Release Decision Example

Target:

```text
v1.6.0
```

Possible progression:

```text
v1.6.0-alpha.1
v1.6.0-alpha.2
v1.6.0-beta.1
v1.6.0-beta.2
v1.6.0-rc.1
v1.6.0
```

If a blocker is found in RC:

```text
v1.6.0-rc.2
```

Do not overwrite `rc.1`.

---

## 31. Release Preparation Procedure

Before tagging a release:

1. Determine the correct next version.
2. Confirm whether the release is alpha, beta, RC, or stable.
3. Create a release preparation PR if required.
4. Update release notes/changelog.
5. Document breaking changes and migrations.
6. Verify CI is green.
7. Verify package/release workflow changes.
8. Merge the release preparation PR.
9. Identify the exact release commit.
10. Create the immutable tag.
11. Allow release automation to build from that tag.
12. Verify artifacts and reported version information.

The branching process is documented separately in:

```text
docs/development/branching-and-release-strategy.md
```

---

## 32. Rules for Changing an Already-Planned Version

Do not casually change the version target after publishing pre-releases.

A target may change when scope genuinely changes.

If a compatible additional feature is added during a minor beta series, the target may remain the same minor release.

If a deliberate breaking stable-interface change is added during a `1.x` beta series, move the target to a `2.0.0` pre-release series.

Version numbers communicate compatibility impact, not the number of features.

---

## 33. Do Not Encode Dates Into Normal Versions

Use SemVer rather than calendar versions for Toskar Core releases.

Preferred:

```text
v1.8.0
```

Not:

```text
v2026.09.28
```

Release dates belong in release metadata and changelogs.

---

## 34. Do Not Skip Versions Without Reason

Avoid arbitrary jumps such as:

```text
v1.4.0 → v1.9.0
```

unless there is a concrete release/history reason.

Do not promote a normal feature release to `v2.0.0` merely because it is important.

`MAJOR` communicates compatibility breakage, not marketing significance.

---

## 35. Rules for 1.0.0

`1.0.0` means Toskar Core is declaring a stable public compatibility baseline.

Before `1.0.0`, identify which interfaces are considered stable, such as:

- OpenAI-compatible API behavior,
- core `/api/v1` contracts,
- configuration,
- release/install layout,
- relevant persisted state,
- supported node protocol expectations.

Do not choose `1.0.0` only because the application feels finished.

It should represent a deliberate compatibility commitment.

---

## 36. Support Policy and Versioning

Versioning and support policy are related but different.

A release being valid does not mean it is supported forever.

Example:

```text
v1.7.x   supported
v1.8.x   supported
v1.6.x   unsupported
```

If Toskar later adopts an explicit support window, document it separately.

---

## 37. Release Notes Minimum

Each externally published release should state:

- version,
- release type,
- major changes,
- fixes,
- known issues where important,
- breaking changes,
- migration instructions where required,
- upgrade/install guidance.

For pre-releases, clearly state whether the release is Alpha, Beta, or Release Candidate.

Do not describe a beta as stable.

---

## 38. Summary Decision Table

| Change | Version impact |
| --- | --- |
| Backward-compatible bug fix | PATCH |
| Security fix with compatible behavior | PATCH |
| Packaging correction | PATCH |
| New backward-compatible feature | MINOR |
| New runtime adapter | MINOR |
| New hardware/platform support | MINOR |
| New API endpoint | MINOR |
| Remove stable endpoint | MAJOR |
| Incompatible stable API field change | MAJOR |
| Remove stable config option | MAJOR |
| Internal refactor only | No version impact by itself |
| Documentation-only change | No release required |
| Experimental interface change | Depends on documented stability |
| Breaking change before 1.0 | Usually MINOR |

---

## 39. Summary Pre-Release Table

| Stage | Meaning |
| --- | --- |
| `alpha.N` | Incomplete, unstable, architecture/API may still change |
| `beta.N` | Feature-complete or close; testing, reliability, compatibility work |
| `rc.N` | Believed ready for stable release; release blockers only |
| no suffix | Stable release |

---

## 40. Core Rules

1. Use Semantic Versioning.
2. Stable tags use `vMAJOR.MINOR.PATCH`.
3. Pre-releases use `alpha.N`, `beta.N`, or `rc.N`.
4. `PATCH` is for compatible fixes.
5. `MINOR` is for compatible new functionality.
6. `MAJOR` is for incompatible stable-interface changes.
7. Pre-release numbers never get reused.
8. Release tags are immutable.
9. Official artifacts are built from the exact tagged commit.
10. Never replace a bad release; publish the next version.
11. Choose the intended stable version before starting its pre-release series.
12. Document breaking changes and migrations prominently.
13. Prefer deprecation before removal.
14. Keep version information centralized and injected by release automation where practical.
15. Version numbers communicate compatibility, not marketing importance.

---

## 41. Quick Reference

Current release:

```text
v1.4.2
```

Bug fix:

```text
v1.4.3
```

New compatible feature:

```text
v1.5.0
```

Breaking stable-interface change:

```text
v2.0.0
```

Feature beta:

```text
v1.5.0-beta.1
```

Next beta:

```text
v1.5.0-beta.2
```

Release candidate:

```text
v1.5.0-rc.1
```

Stable:

```text
v1.5.0
```

Bad stable release:

```text
Do not move v1.5.0.
Fix the problem and release v1.5.1.
```

This policy should remain boring, predictable, and automation-friendly.
