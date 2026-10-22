#!/usr/bin/env python3
"""Enforce strictly greater than 95% Go statement coverage, including the CLI."""

import sys
from pathlib import Path


def coverage(profile):
    covered = total = 0
    lines = profile.splitlines()
    if not lines or lines[0] not in ("mode: set", "mode: count", "mode: atomic"):
        raise ValueError("invalid Go coverage profile")
    for line in lines[1:]:
        _, statements, count = line.rsplit(" ", 2)
        statements, count = int(statements), int(count)
        if statements < 0 or count < 0:
            raise ValueError("negative coverage count")
        total += statements
        if count:
            covered += statements
    if not total:
        raise ValueError("coverage profile contains no statements")
    return covered, total


def main():
    covered, total = coverage(Path(sys.argv[1] if len(sys.argv) > 1 else "coverage.out").read_text())
    print(f"Go statement coverage: {covered / total:.2%} ({covered}/{total})")
    return 0 if covered * 100 > total * 95 else 1


if __name__ == "__main__":
    sys.exit(main())
