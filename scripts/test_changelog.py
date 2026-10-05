#!/usr/bin/env python3
"""Tests for scripts/changelog.py."""

import importlib.util
import json
import pathlib
import tempfile
import unittest

spec = importlib.util.spec_from_file_location("changelog", pathlib.Path(__file__).with_name("changelog.py"))
changelog = importlib.util.module_from_spec(spec)
spec.loader.exec_module(changelog)

CHANGELOG = """# Changelog

Intro.

## [Unreleased]

Each pull request adds a fragment under changes/unreleased/.

## [1.4.0] - 2026-10-02

### Added

- Old thing.
"""


class FragmentTest(unittest.TestCase):
    def test_parse_and_merge_in_section_order(self):
        with tempfile.TemporaryDirectory() as d:
            p = pathlib.Path(d)
            (p / "b-push.md").write_text("### Fixed\n\n- A fix.\n\n### Added\n\n- Push\n  over two lines.\n")
            (p / "a-sandbox.md").write_text("### Added\n\n- Sandbox.\n")
            (p / "README.md").write_text("Not a fragment.")
            paths = changelog.fragment_files(p)
            self.assertEqual([x.name for x in paths], ["a-sandbox.md", "b-push.md"])
            body = changelog.render(changelog.gather(paths))
        self.assertEqual(body, "### Added\n\n- Sandbox.\n- Push\n  over two lines.\n\n### Fixed\n\n- A fix.")

    def test_bad_fragments(self):
        for text, why in [
            ("- No heading.\n", "before a section"),
            ("### Improved\n\n- x\n", "unknown section"),
            ("### Added\n\nJust prose.\n", "bullet"),
            ("### Added\n", "no entries"),
            ("### Added\n\n### Fixed\n\n- x\n", "has no entries"),
        ]:
            with self.assertRaises(changelog.FragmentError) as ctx:
                changelog.parse(text, "f.md")
            self.assertIn(why, str(ctx.exception))

    def test_release_inserts_section_under_unreleased(self):
        out = changelog.release(CHANGELOG, "1.5.0", "2026-11-01", "### Added\n\n- New thing.")
        self.assertIn("## [Unreleased]\n\nEach pull request adds a fragment under changes/unreleased/.\n\n## [1.5.0] - 2026-11-01\n\n### Added\n\n- New thing.\n\n## [1.4.0]", out)
        with self.assertRaises(changelog.FragmentError):
            changelog.release(out, "1.5.0", "2026-11-01", "### Added\n\n- Again.")
        with self.assertRaises(changelog.FragmentError):
            changelog.release(CHANGELOG, "v1.5", "2026-11-01", "### Added\n\n- x")
        with self.assertRaises(changelog.FragmentError):
            changelog.release(CHANGELOG, "1.5.0", "2026-11-01", "")

    def test_repository_fragments_are_valid(self):
        changelog.gather(changelog.fragment_files())


def highlights(version="1.4", count=3, **item):
    entry = {"realm": "Mimir", "title": "Ask your documents", "text": "Answers cite their sources."}
    entry.update(item)
    return json.dumps({"schemaVersion": 1, "version": version, "items": [entry] * count})


class HighlightsTest(unittest.TestCase):
    def test_current_highlights_pass(self):
        changelog.check_highlights(CHANGELOG, highlights())

    def test_latest_minor_skips_prereleases(self):
        text = CHANGELOG.replace("## [1.4.0]", "## [1.5.0-beta.1] - 2026-10-03\n\n### Added\n\n- Beta.\n\n## [1.4.0]")
        self.assertEqual(changelog.latest_minor(text), "1.4")
        changelog.check_highlights(text, highlights())

    def test_a_new_minor_release_needs_new_highlights(self):
        text = CHANGELOG.replace("## [1.4.0]", "## [1.5.0] - 2026-10-03\n\n### Added\n\n- New.\n\n## [1.4.0]")
        with self.assertRaisesRegex(changelog.FragmentError, "write the 1.5 highlights"):
            changelog.check_highlights(text, highlights())
        changelog.check_highlights(text, highlights("1.5"))

    def test_a_patch_release_keeps_them(self):
        text = CHANGELOG.replace("## [1.4.0]", "## [1.4.1] - 2026-10-03\n\n### Fixed\n\n- Fix.\n\n## [1.4.0]")
        changelog.check_highlights(text, highlights())

    def test_bad_highlights(self):
        for text, message in [
            (None, "missing"),
            ("{", "not valid JSON"),
            (json.dumps({"version": "1.4", "items": []}), "schemaVersion"),
            (highlights(count=2), "3 to 9 items"),
            (highlights(count=10), "3 to 9 items"),
            (highlights(realm=""), "needs a realm"),
            (highlights(title="x" * 61), "title is over"),
            (highlights(text="x" * 281), "text is over"),
        ]:
            with self.subTest(message=message), self.assertRaisesRegex(changelog.FragmentError, message):
                changelog.check_highlights(CHANGELOG, text)

    def test_repository_highlights_are_current(self):
        changelog.check_highlights(changelog.CHANGELOG.read_text(encoding="utf-8"), changelog.read_highlights())


if __name__ == "__main__":
    unittest.main()
