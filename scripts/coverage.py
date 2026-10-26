#!/usr/bin/env python3
"""Report Go statement coverage, including the CLI."""

import sys
from pathlib import Path


def coverage(profile):
    blocks = {}
    lines = profile.splitlines()
    if not lines or lines[0] not in ("mode: set", "mode: count", "mode: atomic"):
        raise ValueError("invalid Go coverage profile")
    for line in lines[1:]:
        location, statements, count = line.rsplit(" ", 2)
        statements, count = int(statements), int(count)
        if statements < 0 or count < 0:
            raise ValueError("negative coverage count")
        if location in blocks:
            previous_statements, previous_count = blocks[location]
            if statements != previous_statements:
                raise ValueError("inconsistent statement count for coverage block")
            count += previous_count
        blocks[location] = (statements, count)
    total = sum(statements for statements, _ in blocks.values())
    covered = sum(statements for statements, count in blocks.values() if count)
    if not total:
        raise ValueError("coverage profile contains no statements")
    return covered, total


def main():
    covered, total = coverage(Path(sys.argv[1] if len(sys.argv) > 1 else "coverage.out").read_text())
    print(f"Go statement coverage: {covered / total:.2%} ({covered}/{total})")
    return 0


if __name__ == "__main__":
    sys.exit(main())
