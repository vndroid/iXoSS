#!/usr/bin/env python3
"""Check the recorded port, or regenerate its audit against the pinned checkout."""

import argparse
import difflib
from datetime import date
import hashlib
import json
from pathlib import Path
from pathlib import PurePosixPath
import re
import subprocess
import sys


ROOT = Path(__file__).resolve().parents[1]
LOCK = ROOT / "third_party/imgproxy/UPSTREAM.json"
DIFF = LOCK.with_name("PORT.diff")
REVIEWS = LOCK.with_name("REVIEWS.json")
NOTICE_HEADER = (
    b"// Derived from imgproxy (Copyright imgproxy contributors), Apache-2.0.\n"
    b"// Modified by iXoSS, including import paths and/or implementation.\n"
    b"// See LICENSE-imgproxy, NOTICE and third_party/imgproxy/UPSTREAM.json.\n\n"
)
KINDS = ("identical_after_import_rewrite", "modified", "local_only")
STATES = ("ported", "already_present", "not_applicable", "pending_analysis",
          "pending_port", "pending_validation")
DIRECTORIES = (
    "processing", "options", "security", "vips", "imagetype", "imagemeta",
    "auximageprovider", "env", "bufreader", "errctx", "ioutil", "imath", "ensure",
)


def digest(data):
    return hashlib.sha256(data).hexdigest()


def require(condition, message):
    if not condition:
        raise ValueError(message)


def valid_path(value):
    return (isinstance(value, str) and bool(value) and "\\" not in value
            and not PurePosixPath(value).is_absolute()
            and not any(part in ("", ".", "..") for part in value.split("/")))


def valid_hash(value, length=40):
    return isinstance(value, str) and re.fullmatch(r"[0-9a-f]{%d}" % length, value)


def validate(lock, reviews):
    require(lock.get("schema_version") == 2, "unsupported source-lock schema")
    require(reviews.get("schema_version") == 1, "unsupported review schema")
    require(lock.get("repository") == "https://github.com/imgproxy/imgproxy",
            "unexpected upstream repository")
    date.fromisoformat(lock["audit_date"])
    require(valid_hash(lock["baseline"]["commit"]), "invalid baseline SHA")
    require(re.fullmatch(r"v\d+\.\d+\.\d+", lock["baseline"]["version"]),
            "invalid baseline version")
    seen = set()
    for entry in lock["files"]:
        path = entry["path"]
        require(valid_path(path) and path.startswith("internal/"), "invalid local path")
        require(path not in seen, "duplicate path: " + path)
        seen.add(path)
        require(valid_hash(entry["local_sha256"], 64), "invalid local hash: " + path)
        require(entry["comparison"] in KINDS, "invalid comparison: " + path)
        if entry["comparison"] == "local_only":
            require(entry.get("upstream_path") is None and "upstream_sha256" not in entry,
                    "local-only file has an upstream mapping: " + path)
        else:
            require(valid_path(entry["upstream_path"]), "invalid upstream path: " + path)
            require(valid_hash(entry["upstream_sha256"], 64), "invalid upstream hash: " + path)
    require(lock["summary"] == {
        kind: sum(e["comparison"] == kind for e in lock["files"]) for kind in KINDS
    }, "summary does not match file classifications")
    for component in lock["components"]:
        require(component["role"] in ("rewritten", "removed", "replaced"),
                "invalid component role")
        require(component["reason"] and component["upstream_paths"], "empty component")
        for path in component["upstream_paths"] + component["local_paths"]:
            require(valid_path(path), "invalid component path")
    intervals = reviews["intervals"]
    require(bool(intervals), "missing review interval")
    previous = None
    for interval in intervals:
        require(isinstance(interval["complete"], bool), "completion must be boolean")
        require(previous is None or interval["from"] == previous,
                "review intervals have a gap")
        previous = interval["to"]
        for end in ("from", "to"):
            require(valid_hash(interval[end]["commit"]), "invalid review SHA")
            require(re.fullmatch(r"v\d+\.\d+\.\d+", interval[end]["version"]),
                    "invalid review version")
        commits = interval["commits"]
        require(bool(commits) and bool(interval["scope"]), "empty review")
        require(len({c["commit"] for c in commits}) == len(commits),
                "duplicate reviewed commit")
        for commit in commits:
            require(valid_hash(commit["commit"]), "invalid reviewed commit SHA")
            require(commit["status"] in STATES and bool(commit["reason"]),
                    "invalid decision or missing reason")
            for path in commit["upstream_paths"] + commit["local_paths"]:
                require(valid_path(path), "invalid review file path")
            for dependency in commit["dependencies"]:
                require(valid_hash(dependency), "invalid dependency SHA")
            if commit["status"] in ("ported", "already_present"):
                require(bool(commit["evidence"]) and bool(commit["local_paths"]),
                        "adopted patch lacks evidence or local mapping")
                require(valid_hash(commit["local_commit"]), "invalid local patch SHA")
        require(not interval["complete"] or not any(
            c["status"].startswith("pending_") for c in commits
        ), "completed interval contains pending decisions")
    progress = lock["review_progress"]
    require(progress["latest_interval"] == intervals[-1]["to"],
            "review cursor does not match ledger")
    require(progress["complete"] == intervals[-1]["complete"],
            "review completion does not match ledger")
    require(reviews["baseline"] == lock["baseline"]["commit"], "review baseline mismatch")
    require(valid_hash(lock["port_diff_sha256"], 64) and
            digest(DIFF.read_bytes()) == lock["port_diff_sha256"], "PORT.diff hash mismatch")
    decisions = {c["commit"]: c for interval in intervals for c in interval["commits"]}
    for patch in lock["backported_security_commits"]:
        require(valid_hash(patch["commit"]), "invalid backport SHA")
        require(patch["commit"] in decisions and
                decisions[patch["commit"]]["status"] in ("ported", "already_present"),
                "backport lacks completed review evidence")
    for notice in lock["retained_notices"]:
        require(valid_path(notice["path"]), "invalid retained notice path")
        require(valid_hash(notice["sha256"], 64), "invalid retained notice hash")
        require(digest((ROOT / notice["path"]).read_bytes()) == notice["sha256"],
                "retained notice changed: " + notice["path"])
    for prior in reviews["prior_evidence"]:
        require(valid_hash(prior["commit"]) and prior["status"] == "already_present",
                "invalid prior patch evidence")
        require(prior["reason"] and prior["evidence"] and prior["local_paths"],
                "missing prior patch evidence")
        require(valid_hash(prior["local_commit"]), "invalid prior local commit")
        require(all(valid_path(p) for p in prior["local_paths"]), "invalid prior local path")


def validate_history(upstream, reviews):
    def git(*args):
        return subprocess.check_output(["git", "-C", str(upstream), *args], text=True).strip()
    for interval in reviews["intervals"]:
        for end in ("from", "to"):
            require(git("rev-parse", interval[end]["version"] + "^{commit}") ==
                    interval[end]["commit"], "review tag/SHA mismatch")
        start, finish = interval["from"]["commit"], interval["to"]["commit"]
        subprocess.check_call(["git", "-C", str(upstream), "merge-base", "--is-ancestor",
                               start, finish])
        actual = git("rev-list", "--reverse", start + ".." + finish).splitlines()
        require(actual == [c["commit"] for c in interval["commits"]],
                "review ledger omits or reorders upstream commits")
        for commit in interval["commits"]:
            paths = git("diff-tree", "--root", "-m", "--no-commit-id", "--name-only",
                        "-r", commit["commit"]).splitlines()
            require(sorted(set(paths)) == sorted(commit["upstream_paths"]),
                    "review paths differ from upstream: " + commit["commit"])


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
    reviews = json.loads(REVIEWS.read_text())
    validate(lock, reviews)
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
        tag = subprocess.check_output(
            ["git", "-C", str(upstream), "rev-parse", lock["baseline"]["version"] + "^{commit}"],
            text=True,
        ).strip()
        require(tag == revision, "baseline tag does not match SHA")
        validate_history(upstream, reviews)
        for notice in lock["retained_notices"]:
            require(digest((upstream / notice["upstream_path"]).read_bytes()) == notice["sha256"],
                    "retained notice differs from upstream")
    recorded = {entry["path"]: entry for entry in lock["files"]}
    entries = []
    errors = []
    diff = []
    for path in sources():
        relative = path.relative_to(ROOT).as_posix()
        data = path.read_bytes()
        old = recorded.get(relative, {})
        upstream_path = old.get("upstream_path", path.relative_to(ROOT / "internal").as_posix())
        entry = {"path": relative, "local_sha256": digest(data), "upstream_path": upstream_path}
        if old.get("comparison") != "local_only":
            require(data.startswith(NOTICE_HEADER), "missing modification notice: " + relative)
        if args.upstream:
            reference = upstream / upstream_path if upstream_path else None
            normalized = data.removeprefix(NOTICE_HEADER).replace(
                b"github.com/vndroid/iXoSS/internal/",
                b"github.com/imgproxy/imgproxy/v4/",
            )
            reference_data = reference.read_bytes() if reference and reference.is_file() else b""
            if reference and reference.is_file():
                entry.update(
                    upstream_sha256=digest(reference_data),
                    comparison="identical_after_import_rewrite"
                    if normalized == reference_data else "modified",
                )
            else:
                entry["comparison"] = "local_only"
                entry["upstream_path"] = None
            diff.extend(difflib.unified_diff(
                reference_data.decode().splitlines(keepends=True),
                normalized.decode().splitlines(keepends=True),
                fromfile="upstream/" + upstream_path
                if reference and reference.is_file() else "/dev/null",
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
        lock["audit_date"] = date.today().isoformat()
        lock["files"] = entries
        lock["port_diff_sha256"] = digest("".join(diff).encode())
        lock["summary"] = {
            kind: sum(entry["comparison"] == kind for entry in entries)
            for kind in ("identical_after_import_rewrite", "modified", "local_only")
        }
        LOCK.write_text(json.dumps(lock, indent=2) + "\n")
        DIFF.write_text("".join(diff))
    print("imgproxy port audit OK: " + lock["baseline"]["version"])
    return 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except (ValueError, KeyError, TypeError, OSError, subprocess.CalledProcessError) as error:
        print("Port audit failed: " + str(error), file=sys.stderr)
        sys.exit(1)
