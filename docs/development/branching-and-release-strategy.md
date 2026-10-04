# Toskar Core Branching and Release Strategy

## Purpose

This document defines the recommended Git branching, pull request, branch protection, release, backport, and security release process for Toskar Core.

The goal is to keep development simple, keep `main` healthy, make releases reproducible, and establish predictable contribution rules for maintainers and external contributors.

Toskar Core uses a **trunk-based development model** with short-lived branches, pull requests into `main`, tag-driven releases, and maintenance branches only when older stable releases need continued support.

---

## 1. Branching Model

### Permanent branches

Toskar Core should keep the number of permanent branches small.

Initially, the only permanent branch should be:

```text
main
```

`main` represents the current development version of Toskar Core.

Every commit on `main` should:

- build successfully,
- pass required CI checks,
- be reasonably releasable,
- avoid known broken intermediate states.

Do **not** maintain a permanent `develop` branch.

Additional long-lived branches should only be created when a released version needs maintenance while `main` continues toward a newer release.

Example:

```text
main                         ← current development
│
├── feature/23-memory
├── fix/42-runtime-cleanup
├── docs/kubernetes-guide
│
├── tag: v1.3.0-beta.1
├── tag: v1.3.0-beta.2
└── tag: v1.3.0

release/1.2                  ← only if 1.2 still needs maintenance
    └── tag: v1.2.1
```

---

## 2. Short-Lived Branches

All normal work should happen on short-lived branches.

Recommended naming patterns:

```text
feature/23-persistent-memory
feature/26-kubernetes
fix/41-runtime-crash
fix/57-windows-path
docs/api-auth
refactor/norn-placement
test/bifrost-reconnect
```

When an issue exists, include the issue number where practical.

Branch naming should remain a convention rather than a hard enforcement rule. External contributors may submit branches from forks using other names.

---

## 3. Normal Development Flow

The standard development workflow is:

```text
Issue
  ↓
Short-lived branch
  ↓
Commits
  ↓
Pull Request → main
  ↓
CI
  ↓
Review
  ↓
Approval
  ↓
Squash merge
  ↓
Delete branch
```

Even maintainers should normally use this workflow.

Direct pushes to `main` should be treated as an emergency-only bypass.

---

## 4. Pull Requests

All normal changes should enter `main` through a pull request.

A pull request should:

- describe what changes,
- explain why the change is needed,
- reference related issues,
- identify compatibility or migration concerns,
- include or update tests where appropriate,
- update documentation when behavior changes,
- pass required CI before merge.

Large umbrella features should be broken into smaller implementation issues and PRs.

Example:

```text
#23 Persistent Memory

#31 Add memory persistence schema
#32 Add memory repository API
#33 Add context assembler
#34 Add memory enable/disable API
#35 Add memory provenance
#36 Add summarization policy
```

Each issue should produce a reasonably reviewable PR rather than one massive feature branch.

---

## 5. Merge Strategy

### Default: Squash and Merge

Toskar Core should use **Squash and merge** as the normal merge strategy.

Example contributor history:

```text
fix thing
fix test
oops
rename
lint
final fix
```

becomes one repository commit:

```text
feat(memory): add persistent memory store (#31)
```

Benefits include:

- clean repository history,
- easier changelog generation,
- easier reverts,
- easier `git bisect`,
- one logical commit per PR.

Normal merge commits should be disabled unless there is a future reason to preserve branch topology.

Rebase merging may also be disabled to keep contribution behavior consistent.

---

## 6. Pull Request Titles

Because squash merging commonly uses the PR title as the final commit message, PR titles should be meaningful.

Recommended style:

```text
feat: add Kubernetes compute backend
fix: recover from failed llama.cpp process
docs: document Bifrost pairing
perf: reduce model discovery allocations
refactor: separate Norn placement policy
test: add API authentication integration coverage
```

A Conventional Commits-style format is recommended but does not need to become a hard blocker immediately.

Common prefixes:

```text
feat:
fix:
docs:
perf:
refactor:
test:
build:
ci:
chore:
security:
```

---

## 7. Protecting `main`

`main` should be protected with a GitHub ruleset.

Recommended protections:

- require pull requests before merging,
- require at least **1 approving review**,
- dismiss stale approvals when new commits are pushed,
- require review conversations to be resolved,
- require required CI checks to pass,
- block force pushes,
- block branch deletion,
- restrict direct pushes,
- require linear history when squash merging is standard,
- apply rules to repository administrators,
- retain a tightly controlled emergency bypass.

The goal is that the normal path for everyone is:

```text
branch → PR → checks → review → merge
```

---

## 8. Required CI Checks

The exact required job names should match the repository's GitHub Actions workflows.

Typical required checks for Toskar Core should include the equivalent of:

```text
gofmt
go vet
golangci-lint
go test ./...
cross-compile
frontend lint
frontend typecheck
frontend tests
frontend production build
security checks
CLA check
```

Only checks that actually exist and are stable should be marked required.

A flaky required check blocks development and should be fixed rather than routinely bypassed.

---

## 9. Merge Queue

GitHub's merge queue is recommended once contributor activity increases.

Without a merge queue:

```text
PR A passes against main
PR B passes against the same main

PR A merges
PR B is now merging against a different main than it tested against
```

A merge queue retests the actual merge candidate.

For a small project this is optional at first, but it becomes useful as concurrent contributions increase.

---

## 10. Release Philosophy

Toskar Core releases should be **tag-driven**.

The Git tag is the source of truth for the exact source revision that produced a release.

Example tags:

```text
v1.3.0-beta.1
v1.3.0-beta.2
v1.3.0-rc.1
v1.3.0
v1.3.1
```

Official release artifacts should be built by CI from the tagged commit.

Avoid building official release binaries locally and manually uploading them.

---

## 11. Release Flow

Recommended release flow:

```text
Normal PRs
   ↓
main
   ↓
Release preparation PR
   ↓
CI + review
   ↓
Merge to main
   ↓
Create release tag
   ↓
GitHub Actions release workflow
   ↓
Artifacts
Checksums
Packages
GitHub Release
Package repositories
Homebrew updates
Other publishing steps
```

---

## 12. Release Preparation PR

Before a significant release, create a small release preparation PR.

Example:

```text
release: prepare v1.3.0-beta.1
```

That PR may include:

- the `CHANGELOG.md` section written by `scripts/changelog.py release` from `changes/unreleased/`,
- release notes source,
- documentation changes,
- compatibility notes,
- release metadata,
- version metadata if the project requires it.

Avoid scattering manually maintained version numbers throughout the repository.

If build-time version injection already exists, prefer that mechanism.

---

## 13. Beta Releases

Beta releases do not require dedicated branches.

Example:

```text
main
 ├── feature A
 ├── feature B
 ├── fixes
 │
 ├── v1.3.0-beta.1
 │
 ├── more fixes
 │
 ├── v1.3.0-beta.2
 │
 ├── more fixes
 │
 └── v1.3.0
```

The tags identify the release snapshots.

This keeps the branching model simple during active development.

---

## 14. Stable Releases

A stable release follows the same tag-driven process.

Example:

```text
main
  ↓
release preparation PR
  ↓
merge
  ↓
v1.3.0
  ↓
release automation
```

Once a stable version is published, its tag must never move.

If `v1.3.0` contains a bug, release:

```text
v1.3.1
```

Do not repoint `v1.3.0` to another commit.

---

## 15. Protecting Release Tags

Create a GitHub tag ruleset for:

```text
v*
```

Recommended policy:

- only maintainers or release automation can create release tags,
- published tags cannot be moved,
- published tags cannot be deleted except through an explicit emergency process,
- automation credentials used for release publishing should be narrowly scoped.

Release tags are part of Toskar's reproducibility and supply-chain trust.

---

## 16. When to Create `release/*` Branches

Do not create release branches until they are needed.

A release branch becomes useful when:

1. a stable version is still supported, and
2. `main` has moved on to development that should not be included in a patch release.

Example:

```text
v1.3.0 released

main
└── development toward 1.4

release/1.3
└── maintenance for the 1.3 series
```

Create the maintenance branch from the stable release tag:

```bash
git switch -c release/1.3 v1.3.0
```

---

## 17. Protecting `release/*`

Once release branches exist, protect:

```text
release/*
```

Recommended requirements:

- pull request required,
- required CI checks,
- at least one approval,
- resolved review conversations,
- no force pushes,
- no branch deletion,
- restricted direct pushes,
- maintainer-controlled merging.

Release branches should be more conservative than `main`.

Allowed changes should generally be limited to:

- bug fixes,
- security fixes,
- compatibility fixes,
- critical documentation,
- required release metadata.

New feature development belongs on `main`.

---

## 18. Backport Procedure

Whenever practical, fix a bug on `main` first.

Recommended flow:

```text
Bug
 ↓
Fix PR → main
 ↓
Merge
 ↓
Backport to release/X.Y
 ↓
Backport PR
 ↓
Patch release
```

Example:

```text
main
  │
  └── fix: prevent runtime deadlock
                │
                └──────────► release/1.3
                                  │
                                  └── v1.3.1
```

This prevents a bug from being fixed only in an old release while remaining broken in future versions.

If `main` has diverged enough that the implementation must differ, create separate fixes referencing the same issue.

---

## 19. Patch Release Procedure

Example patch release:

```text
release/1.3
   ↓
backport PR
   ↓
CI + review
   ↓
merge
   ↓
release preparation if needed
   ↓
v1.3.1
   ↓
release automation
```

Do not merge unfinished features from `main` into the release branch simply to obtain a fix.

---

## 20. Security Release Procedure

Serious vulnerabilities may require a private fix before public disclosure.

Recommended process:

```text
Private vulnerability report
        ↓
GitHub private security fork/advisory
        ↓
Private fix
        ↓
Review + testing
        ↓
Prepare patched releases
        ↓
Publish fixes
        ↓
Create release tags
        ↓
Publish security advisory
```

After disclosure:

- ensure the fix exists on `main`,
- backport to every supported stable branch,
- release patched versions,
- publish the advisory and upgrade guidance.

Do not require security reporters to open public issues.

Follow `SECURITY.md` for vulnerability reporting.

---

## 21. Emergency Bypass

Maintainers should retain a controlled emergency bypass for situations such as:

- repairing broken release automation,
- fixing a severe repository configuration problem,
- responding to an active security incident.

The bypass should not become a normal convenience path.

After an emergency direct change:

- document what happened,
- ensure CI runs,
- create follow-up review where appropriate,
- restore normal protections immediately.

---

## 22. CODEOWNERS

Use `CODEOWNERS` for areas where maintainer review is especially important.

Example:

```text
/.github/workflows/      @gopherstein
/packaging/              @gopherstein
/internal/security/      @gopherstein
/internal/bifrost/       @gopherstein
/internal/norn/          @gopherstein
/SECURITY.md             @gopherstein
/go.mod                  @gopherstein
/go.sum                  @gopherstein
```

Adjust paths to match the actual repository.

Sensitive areas commonly include:

- CI and release workflows,
- authentication and authorization,
- network security,
- model/runtime download verification,
- dependency changes,
- package publishing,
- release infrastructure.

As the maintainer team grows, ownership should be distributed rather than permanently centralized.

---

## 23. Release Environment Protection

Publishing jobs should use a protected GitHub Environment, for example:

```text
release
```

Store release-specific credentials there instead of broadly accessible repository secrets where possible.

The release environment may protect operations such as:

- publishing packages,
- updating package repositories,
- updating Homebrew metadata,
- publishing container images,
- signing artifacts,
- creating or publishing GitHub releases.

For sensitive publishing steps, consider requiring maintainer approval before a workflow may enter the environment.

This reduces the impact of an ordinary CI workflow compromise.

---

## 24. Contributor Workflow

External contributors should generally follow:

```text
Fork repository
   ↓
Create branch
   ↓
Make focused changes
   ↓
Open PR against main
   ↓
CLA / contribution checks
   ↓
CI
   ↓
Review
   ↓
Squash merge
```

Contributors should not need write access to the primary repository.

Maintainers should avoid asking contributors to rebase repeatedly unless the PR actually conflicts or requires retesting.

---

## 25. Branches That Should Be Protected

### Protected

```text
main
release/*          # when maintenance branches exist
v* tags            # release tags
```

### Not normally protected

```text
feature/*
fix/*
docs/*
refactor/*
test/*
contributor fork branches
```

Short-lived branches exist to facilitate development and should remain easy to update.

---

## 26. Recommended GitHub Settings

### Repository merge settings

Recommended:

- Enable **Squash merging**
- Disable **Merge commits**
- Disable **Rebase merging** unless maintainers specifically want it
- Automatically delete head branches after merge
- Enable auto-merge if desired
- Enable merge queue when contribution volume warrants it

### `main` ruleset

Recommended:

- require pull request,
- require 1 approval,
- dismiss stale approvals,
- require resolved conversations,
- require status checks,
- block force push,
- block deletion,
- require linear history,
- apply to administrators,
- tightly limit bypass.

### `release/*` ruleset

Recommended:

- require pull request,
- require 1 approval,
- require status checks,
- block force push,
- block deletion,
- require resolved conversations,
- restrict write/merge permissions to maintainers.

### Tag ruleset

Pattern:

```text
v*
```

Recommended:

- restrict creation,
- prevent modification,
- prevent deletion except emergency administration.

---

## 27. Recommended Release Versioning

Use semantic versioning where practical:

```text
MAJOR.MINOR.PATCH
```

Examples:

```text
v1.2.0
v1.2.1
v1.3.0
v2.0.0
```

Pre-release versions:

```text
v1.3.0-alpha.1
v1.3.0-beta.1
v1.3.0-rc.1
```

General interpretation:

```text
PATCH  backward-compatible bug fixes
MINOR  backward-compatible features
MAJOR  incompatible changes
```

During beta development, exact stability promises may still evolve, but tag naming should remain predictable.

---

## 28. Release Checklist

Before creating a release tag:

- all required CI is green,
- release preparation PR is merged,
- release notes/changelog are current,
- known migration concerns are documented,
- compatibility documentation is current,
- security-sensitive changes have appropriate review,
- package/release workflow changes have been tested,
- the intended release commit is clearly identified.

After tagging:

- verify the release workflow completed,
- verify artifacts and checksums,
- verify package metadata,
- verify install instructions,
- verify the GitHub Release points to the intended tag,
- verify the released binary reports the expected version/commit,
- do not move the tag if a problem is discovered.

If the release is bad, fix forward with a new release.

---

## 29. Example End-to-End Feature

```text
GitHub Issue #26
Feature: Kubernetes-native model deployment

        ↓

feature/26-kubernetes-backend

        ↓

Pull Request
feat: add Kubernetes compute backend

        ↓

Required CI

        ↓

Maintainer review

        ↓

Squash merge

        ↓

main

        ↓

future release preparation PR

        ↓

v1.3.0-beta.1

        ↓

GitHub Actions creates release artifacts
```

---

## 30. Example End-to-End Patch

```text
Bug discovered in v1.3.0

        ↓

fix/88-runtime-deadlock

        ↓

PR → main

        ↓

merge

        ↓

backport commit to release/1.3

        ↓

PR → release/1.3

        ↓

merge

        ↓

tag v1.3.1

        ↓

release automation
```

---

## 31. Guiding Principles

The branching and release process should remain simpler than the software itself.

The most important rules are:

1. `main` stays healthy.
2. Normal changes enter through pull requests.
3. Short-lived branches are preferred.
4. Squash merging keeps history clean.
5. Official releases come from immutable Git tags.
6. Release artifacts are built by automation from the tagged commit.
7. Maintenance branches exist only when they are actually needed.
8. Fix bugs on `main` first, then backport where practical.
9. Security fixes may use a private release process.
10. CI, release infrastructure, and security-sensitive code receive stronger review.
11. Direct pushes and bypasses are emergency tools, not normal workflow.
12. Do not add branching ceremony without a concrete need.

---

## 32. Recommended Strategy Summary

For Toskar Core today:

```text
Permanent:
  main

Short-lived:
  feature/*
  fix/*
  docs/*
  refactor/*
  test/*

Future maintenance:
  release/X.Y

Release identifiers:
  vX.Y.Z
  vX.Y.Z-beta.N
  vX.Y.Z-rc.N
```

Development:

```text
issue → branch → PR → CI → review → squash merge → main
```

Release:

```text
main → release preparation PR → merge → immutable tag → automated release
```

Maintenance:

```text
fix → main → backport PR → release/X.Y → patch tag
```

This model provides a clean contributor experience, predictable releases, and strong repository protections without introducing unnecessary GitFlow-style complexity.
