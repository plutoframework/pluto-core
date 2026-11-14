"""Comment-only formatting drift is still drift for full-source snapshots."""

from pathlib import Path
import tempfile
import unittest

from doc_examples import verify


class DocExampleTests(unittest.TestCase):
    def setUp(self):
        directory = tempfile.TemporaryDirectory()
        self.addCleanup(directory.cleanup)
        self.root = Path(directory.name)
        (self.root / "docs").mkdir()
        (self.root / "docs/REFERENCE.md").write_text("# Reference\n")
        self.source = "package example\n\n// A stable operation ID.\n"
        (self.root / "example.go").write_text(self.source)
        self.document(self.source)

    def document(self, snippet, source="example.go"):
        (self.root / "README.md").write_text(
            f"<!-- example-source: {source} -->\n\n```go\n{snippet}```\n"
            "<!-- /example-source -->\n"
        )

    def test_matching_full_source_passes(self):
        self.assertEqual(verify(self.root), 1)

    def test_wrapping_only_snapshot_comment_fails(self):
        self.document(self.source.replace("operation ID.", "operation\n// ID."))
        with self.assertRaisesRegex(ValueError, "Example drift"):
            verify(self.root)

    def test_malformed_or_escaping_marker_fails(self):
        self.document(self.source, "../outside.go")
        with self.assertRaisesRegex(ValueError, "Invalid example source"):
            verify(self.root)
        (self.root / "README.md").write_text(
            "<!-- example-source: example.go -->"
        )
        with self.assertRaisesRegex(ValueError, "Malformed example marker"):
            verify(self.root)


if __name__ == "__main__":
    unittest.main()
