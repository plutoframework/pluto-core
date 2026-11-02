"""Check that complete documentation snippets match their owning Go source."""

from pathlib import Path
import re

PATTERN = re.compile(
    r"<!-- example-source: ([^\n]+) -->\s*```go\n(.*?)\n```"
    r"\s*<!-- /example-source -->",
    re.S,
)


def verify(root):
    root = root.resolve()
    count = 0
    for document in (root / "README.md", root / "docs/REFERENCE.md"):
        text = document.read_text()
        examples = PATTERN.findall(text)
        if len(examples) != text.count("<!-- example-source:"):
            raise ValueError(f"Malformed example marker: {document}")
        for relative, snippet in examples:
            source = (root / relative).resolve()
            if not source.is_relative_to(root) or not source.is_file():
                raise ValueError(
                    f"Invalid example source: {document}: {relative}"
                )
            if snippet.strip() != source.read_text().strip():
                raise ValueError(f"Example drift: {document}: {relative}")
            count += 1
    return count


if __name__ == "__main__":
    count = verify(Path(__file__).resolve().parents[1])
    print(f"PASS: {count} source-owned documentation examples")
