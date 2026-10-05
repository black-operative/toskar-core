#!/usr/bin/env python3
"""Changelog fragments.

Each pull request describes its change in its own file under
changes/unreleased/ instead of editing CHANGELOG.md, so pull requests do not
conflict over the changelog. A release gathers the fragments into a dated
section of CHANGELOG.md.

site/highlights.json holds the few highlights toskar.ai shows as "New in
1.6" for the latest minor release. A release of a new minor version updates
it in the same pull request; check fails until it does.

A fragment is Markdown with Keep a Changelog section headings and bullets:

    ### Added

    - Push notifications through ntfy, ...

Usage:
    scripts/changelog.py check              validate every fragment, and that
                                            site/highlights.json is for the
                                            latest release
    scripts/changelog.py preview            print the Unreleased section
    scripts/changelog.py release 1.5.0 [--date 2026-10-02]
                                            write the release into CHANGELOG.md
                                            and remove the fragments
"""

from __future__ import annotations

import argparse
import datetime
import json
import pathlib
import re
import sys

ROOT = pathlib.Path(__file__).resolve().parent.parent
FRAGMENTS = ROOT / "changes" / "unreleased"
CHANGELOG = ROOT / "CHANGELOG.md"
HIGHLIGHTS = ROOT / "site" / "highlights.json"

# Keep a Changelog's sections, in the order they are written.
SECTIONS = ["Added", "Changed", "Deprecated", "Removed", "Fixed", "Security"]

HEADING = re.compile(r"^###\s+(.+?)\s*$")
VERSION = re.compile(r"^\d+\.\d+\.\d+(-(alpha|beta|rc)\.\d+)?$")
UNRELEASED = "## [Unreleased]"
# A stable release's heading; the newest is first in CHANGELOG.md.
STABLE_RELEASE = re.compile(r"^## \[(\d+)\.(\d+)\.\d+\]", re.M)
HIGHLIGHT_COUNT = (3, 9)
HIGHLIGHT_TITLE_MAX = 60
HIGHLIGHT_TEXT_MAX = 280


class FragmentError(Exception):
    pass


def fragment_files(directory: pathlib.Path = FRAGMENTS) -> list[pathlib.Path]:
    """Every fragment, in name order (the README is not one)."""
    if not directory.is_dir():
        return []
    return sorted(p for p in directory.glob("*.md") if p.name.lower() != "readme.md")


def parse(text: str, name: str = "fragment") -> dict[str, list[str]]:
    """A fragment's bullets by section. A bullet keeps its continuation lines."""
    sections: dict[str, list[str]] = {}
    current: str | None = None
    for number, raw in enumerate(text.splitlines(), 1):
        line = raw.rstrip()
        m = HEADING.match(line)
        if m:
            current = m.group(1).strip().capitalize()
            if current not in SECTIONS:
                raise FragmentError(f"{name}:{number}: unknown section {m.group(1)!r}; use one of {', '.join(SECTIONS)}")
            sections.setdefault(current, [])
            continue
        if not line.strip():
            continue
        if current is None:
            raise FragmentError(f"{name}:{number}: text before a section heading such as '### Added'")
        if line.startswith("- "):
            sections[current].append(line)
        elif sections[current] and (line.startswith("  ") or line.startswith("\t")):
            sections[current][-1] += "\n" + line
        else:
            raise FragmentError(f"{name}:{number}: each entry is a bullet starting with '- '")
    if not any(sections.values()):
        raise FragmentError(f"{name}: no entries")
    for section, bullets in sections.items():
        if not bullets:
            raise FragmentError(f"{name}: '### {section}' has no entries")
    return sections


def gather(paths: list[pathlib.Path]) -> dict[str, list[str]]:
    merged: dict[str, list[str]] = {s: [] for s in SECTIONS}
    for path in paths:
        for section, bullets in parse(path.read_text(encoding="utf-8"), path.name).items():
            merged[section].extend(bullets)
    return merged


def render(merged: dict[str, list[str]]) -> str:
    parts = []
    for section in SECTIONS:
        if merged.get(section):
            parts.append(f"### {section}\n\n" + "\n".join(merged[section]))
    return "\n\n".join(parts)


def release(changelog: str, version: str, date: str, body: str) -> str:
    """CHANGELOG.md with a new version section under Unreleased."""
    if not VERSION.match(version):
        raise FragmentError(f"{version!r} is not a version such as 1.5.0 or 1.5.0-beta.1")
    if f"## [{version}]" in changelog:
        raise FragmentError(f"CHANGELOG.md already has {version}")
    if UNRELEASED not in changelog:
        raise FragmentError(f"CHANGELOG.md has no '{UNRELEASED}' heading")
    if not body.strip():
        raise FragmentError("there are no changes to release")
    head, rest = changelog.split(UNRELEASED, 1)
    # Whatever sits under Unreleased (the pointer to the fragments) stays there.
    m = re.search(r"^## \[", rest, re.M)
    pointer, older = (rest[: m.start()], rest[m.start():]) if m else (rest, "")
    section = f"## [{version}] - {date}\n\n{body.strip()}\n\n"
    return head + UNRELEASED + pointer.rstrip() + "\n\n" + section + older


def latest_minor(changelog: str) -> str | None:
    """The newest stable release's minor version in CHANGELOG.md, such as 1.6."""
    m = STABLE_RELEASE.search(changelog)
    return f"{m.group(1)}.{m.group(2)}" if m else None


def check_highlights(changelog: str, highlights_text: str | None) -> None:
    """site/highlights.json is valid and describes the latest minor release."""
    minor = latest_minor(changelog)
    if minor is None:
        return
    name = "site/highlights.json"
    if highlights_text is None:
        raise FragmentError(f"{name} is missing; write the highlights of {minor}")
    try:
        data = json.loads(highlights_text)
    except json.JSONDecodeError as err:
        raise FragmentError(f"{name} is not valid JSON: {err}") from err
    if not isinstance(data, dict) or data.get("schemaVersion") != 1:
        raise FragmentError(f"{name} needs \"schemaVersion\": 1")
    if data.get("version") != minor:
        raise FragmentError(
            f"{name} is for {data.get('version')!r}, but the latest release in CHANGELOG.md is {minor}: "
            f"write the {minor} highlights that toskar.ai shows as \"New in {minor}\""
        )
    items = data.get("items")
    low, high = HIGHLIGHT_COUNT
    if not isinstance(items, list) or not low <= len(items) <= high:
        raise FragmentError(f"{name} needs {low} to {high} items")
    for number, item in enumerate(items, 1):
        if not isinstance(item, dict):
            raise FragmentError(f"{name}: item {number} is not an object")
        for key in ("realm", "title", "text"):
            if not isinstance(item.get(key), str) or not item[key].strip():
                raise FragmentError(f"{name}: item {number} needs a {key}")
        if len(item["title"]) > HIGHLIGHT_TITLE_MAX:
            raise FragmentError(f"{name}: item {number}'s title is over {HIGHLIGHT_TITLE_MAX} characters")
        if len(item["text"]) > HIGHLIGHT_TEXT_MAX:
            raise FragmentError(f"{name}: item {number}'s text is over {HIGHLIGHT_TEXT_MAX} characters")


def read_highlights() -> str | None:
    return HIGHLIGHTS.read_text(encoding="utf-8") if HIGHLIGHTS.is_file() else None


def main(argv: list[str]) -> int:
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    sub = parser.add_subparsers(dest="command", required=True)
    sub.add_parser("check")
    sub.add_parser("preview")
    rel = sub.add_parser("release")
    rel.add_argument("version")
    rel.add_argument("--date", default=datetime.date.today().isoformat())
    args = parser.parse_args(argv)

    paths = fragment_files()
    try:
        merged = gather(paths)
        if args.command == "check":
            check_highlights(CHANGELOG.read_text(encoding="utf-8"), read_highlights())
            print(f"{len(paths)} changelog fragment(s) are valid, and site/highlights.json is current")
        elif args.command == "preview":
            print(render(merged) or "No unreleased changes.")
        elif args.command == "release":
            text = release(CHANGELOG.read_text(encoding="utf-8"), args.version, args.date, render(merged))
            CHANGELOG.write_text(text, encoding="utf-8")
            for path in paths:
                path.unlink()
            print(f"Wrote {args.version} to CHANGELOG.md from {len(paths)} fragment(s)")
            try:
                check_highlights(text, read_highlights())
            except FragmentError as err:
                print(f"next: {err}. CI fails until it is updated.", file=sys.stderr)
    except FragmentError as err:
        print(f"error: {err}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
