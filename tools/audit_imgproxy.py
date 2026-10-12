#!/usr/bin/env python3
"""Check the recorded port, or regenerate its audit against the pinned checkout."""

import argparse
import difflib
import hashlib
import json
from pathlib import Path
import subprocess
import sys


ROOT = Path(__file__).resolve().parents[1]
LOCK = ROOT / "third_party/imgproxy/UPSTREAM.json"
DIFF = LOCK.with_name("PORT.diff")
DIRECTORIES = (
    "processing", "options", "security", "vips", "imagetype", "imagemeta",
    "auximageprovider", "env", "bufreader", "errctx", "ioutil", "imath", "ensure",
)


def digest(data):
    return hashlib.sha256(data).hexdigest()


def sources():
    return sorted(
        path for directory in DIRECTORIES
        for path in (ROOT / "internal" / directory).rglob("*")
        if path.is_file() and path.suffix in (".go", ".c", ".h")
        and not path.name.endswith("_test.go")
    )


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--upstream", type=Path, help="clean pinned imgproxy checkout")
    parser.add_argument("--write", action="store_true", help="refresh the file audit")
    args = parser.parse_args()
    if args.write and not args.upstream:
        parser.error("--write requires --upstream")
    lock = json.loads(LOCK.read_text())
    if args.upstream:
        upstream = args.upstream.resolve()
        revision = subprocess.check_output(
            ["git", "-C", str(upstream), "rev-parse", "HEAD"], text=True
        ).strip()
        if revision != lock["baseline"]["commit"]:
            parser.error("upstream HEAD does not match the pinned baseline commit")
        if subprocess.check_output(
            ["git", "-C", str(upstream), "status", "--porcelain"], text=True
        ).strip():
            parser.error("upstream checkout must be clean")
    recorded = {entry["path"]: entry for entry in lock["files"]}
    entries = []
    errors = []
    diff = []
    for path in sources():
        relative = path.relative_to(ROOT).as_posix()
        data = path.read_bytes()
        entry = {"path": relative, "local_sha256": digest(data)}
        if args.upstream:
            reference = upstream / path.relative_to(ROOT / "internal")
            normalized = data.replace(
                b"github.com/vndroid/iXoSS/internal/",
                b"github.com/imgproxy/imgproxy/v4/",
            )
            reference_data = reference.read_bytes() if reference.is_file() else b""
            if reference.is_file():
                entry.update(
                    upstream_sha256=digest(reference_data),
                    comparison="identical_after_import_rewrite"
                    if normalized == reference_data else "modified",
                )
            else:
                entry["comparison"] = "local_only"
            diff.extend(difflib.unified_diff(
                reference_data.decode().splitlines(keepends=True),
                normalized.decode().splitlines(keepends=True),
                fromfile="upstream/" + path.relative_to(ROOT / "internal").as_posix()
                if reference.is_file() else "/dev/null",
                tofile=relative,
                n=0,
            ))
        else:
            entry.update({key: value for key, value in recorded.get(relative, {}).items()
                          if key != "local_sha256"})
        entries.append(entry)
        if not args.write and recorded.get(relative) != entry:
            errors.append(relative)
    if not args.write:
        errors.extend(sorted(set(recorded) - {entry["path"] for entry in entries}))
        if args.upstream and DIFF.read_text() != "".join(diff):
            errors.append("third_party/imgproxy/PORT.diff")
        if errors:
            print("Port audit needs review: " + ", ".join(errors), file=sys.stderr)
            return 1
    else:
        lock["files"] = entries
        lock["summary"] = {
            kind: sum(entry["comparison"] == kind for entry in entries)
            for kind in ("identical_after_import_rewrite", "modified", "local_only")
        }
        LOCK.write_text(json.dumps(lock, indent=2) + "\n")
        DIFF.write_text("".join(diff))
    print("imgproxy port audit OK: " + lock["baseline"]["version"])
    return 0


if __name__ == "__main__":
    sys.exit(main())
