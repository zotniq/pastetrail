#!/usr/bin/env python3
"""Build and package one native OS/architecture with a SHA-256 sidecar."""

import gzip
import hashlib
import io
import json
import os
import re
import subprocess
import sys
import tarfile
import time
import zipfile
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent


def archive_files(destination, files, epoch, windows):
    if windows:
        # ZIP cannot represent timestamps before 1980.
        date = time.gmtime(max(epoch, 315532800))[:6]
        with zipfile.ZipFile(destination, "w", compression=zipfile.ZIP_DEFLATED) as archive:
            for name, data, mode in files:
                info = zipfile.ZipInfo(name, date)
                info.create_system = 3
                info.external_attr = mode << 16
                info.compress_type = zipfile.ZIP_DEFLATED
                archive.writestr(info, data)
    else:
        with destination.open("wb") as output:
            with gzip.GzipFile(filename="", fileobj=output, mode="wb", mtime=epoch) as compressed:
                with tarfile.open(fileobj=compressed, mode="w") as archive:
                    for name, data, mode in files:
                        info = tarfile.TarInfo(name)
                        info.size, info.mode, info.mtime = len(data), mode, epoch
                        archive.addfile(info, io.BytesIO(data))


def main():
    os.chdir(ROOT)
    version = sys.argv[1] if len(sys.argv) > 1 else subprocess.check_output(
        ["git", "describe", "--tags", "--always", "--dirty"], text=True
    ).strip()
    if not re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9._+-]*", version):
        raise ValueError("version must contain only letters, digits, periods, +, -, or _")
    config = json.loads(subprocess.check_output(
        ["go", "env", "-json", "GOOS", "GOARCH", "CGO_ENABLED", "GOROOT"], text=True
    ))
    goos, arch = config["GOOS"], config["GOARCH"]
    if config["CGO_ENABLED"] != "1":
        raise ValueError("release builds require CGO_ENABLED=1; refusing a nonfunctional native artifact")
    if goos not in ("darwin", "linux", "windows") or arch not in ("amd64", "arm64"):
        raise ValueError(f"unsupported release target: {goos}/{arch}")
    binary_name = "pastetrail.exe" if goos == "windows" else "pastetrail"
    name = f"pastetrail_{version}_{goos}_{arch}"
    directory = ROOT / "dist" / name
    directory.mkdir(parents=True, exist_ok=True)
    binary = directory / binary_name
    subprocess.run([
        "go", "build", "-trimpath", "-ldflags", f"-s -w -X main.version={version}",
        "-o", str(binary), "./cmd/pastetrail",
    ], check=True)
    epoch = int(os.environ.get("SOURCE_DATE_EPOCH") or subprocess.check_output(
        ["git", "log", "-1", "--format=%ct"], text=True
    ).strip())
    files = [(binary_name, binary.read_bytes(), 0o755)]
    files.extend((name, (ROOT / name).read_bytes(), 0o644) for name in ("LICENSE", "README.md"))
    files.append(("LICENSE-Go", (Path(config["GOROOT"]) / "LICENSE").read_bytes(), 0o644))
    archive = ROOT / "dist" / (name + (".zip" if goos == "windows" else ".tar.gz"))
    archive_files(archive, files, epoch, goos == "windows")
    digest = hashlib.sha256(archive.read_bytes()).hexdigest()
    archive.with_name(archive.name + ".sha256").write_text(f"{digest}  {archive.name}\n", encoding="ascii")
    print(f"{archive.name}: {archive.stat().st_size:,} bytes; binary {binary.stat().st_size:,} bytes")


if __name__ == "__main__":
    main()
