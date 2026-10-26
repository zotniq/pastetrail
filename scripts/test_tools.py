import hashlib
import contextlib
import io
import tarfile
import tempfile
import unittest
from unittest.mock import patch
import zipfile
from pathlib import Path

from coverage import coverage, main as coverage_main
from package import archive_files


class CoverageTests(unittest.TestCase):
    def test_weighted_statements(self):
        self.assertEqual(coverage("mode: atomic\na.go:1.1,2.1 96 1\nb.go:1.1,2.1 4 0\n"), (96, 100))
        self.assertEqual(coverage("mode: set\na.go:1.1,2.1 1 20\n"), (1, 1))

    def test_invalid_and_empty(self):
        for text in ("", "mode: invalid", "mode: count", "mode: set\nbad", "mode: set\na.go 1 -1"):
            with self.subTest(text=text), self.assertRaises(ValueError):
                coverage(text)

    def test_merge_blocks_from_multiple_test_packages(self):
        profile = "mode: atomic\na.go:1.1,2.1 96 0\nb.go:1.1,2.1 4 0\na.go:1.1,2.1 96 1\n"
        self.assertEqual(coverage(profile), (96, 100))
        with self.assertRaises(ValueError):
            coverage(profile + "a.go:1.1,2.1 95 1\n")

    def test_report_without_threshold(self):
        for covered in (95, 96):
            profile = f"mode: count\na.go  {covered} 1\nb.go  {100-covered} 0\n"
            with patch("sys.argv", ["coverage.py"]), patch("pathlib.Path.read_text", return_value=profile):
                with contextlib.redirect_stdout(io.StringIO()):
                    self.assertEqual(coverage_main(), 0)


class ArchiveTests(unittest.TestCase):
    def test_archive_contents_permissions_and_repeatability(self):
        files = [("pastetrail", b"fixture binary", 0o755), ("LICENSE", b"fixture license", 0o644)]
        with tempfile.TemporaryDirectory() as directory:
            for windows in (False, True):
                destination = Path(directory) / ("fixture.zip" if windows else "fixture.tar.gz")
                archive_files(destination, files, 1700000000, windows)
                first = hashlib.sha256(destination.read_bytes()).digest()
                archive_files(destination, files, 1700000000, windows)
                self.assertEqual(first, hashlib.sha256(destination.read_bytes()).digest())
                if windows:
                    with zipfile.ZipFile(destination) as archive:
                        self.assertEqual(archive.read("pastetrail"), b"fixture binary")
                        self.assertEqual(archive.getinfo("pastetrail").external_attr >> 16, 0o755)
                        self.assertEqual(archive.namelist(), ["pastetrail", "LICENSE"])
                else:
                    with tarfile.open(destination) as archive:
                        self.assertEqual(archive.extractfile("pastetrail").read(), b"fixture binary")
                        self.assertEqual(archive.getmember("pastetrail").mode, 0o755)
                        self.assertEqual(archive.getnames(), ["pastetrail", "LICENSE"])


if __name__ == "__main__":
    unittest.main()
