#!/usr/bin/env python3
"""List every commit in a pinned release interval, with candidate local mappings.

This produces evidence for human review; it never applies patches or decides
whether a change is safe, irrelevant or already implemented.
"""
import argparse
import json
from pathlib import Path
import subprocess
import sys

from audit_imgproxy import LOCK


def report(upstream, start, finish, lock):
    def git(*args):
        return subprocess.check_output(["git", "-C", str(upstream), *args], text=True).strip()
    start = git("rev-parse", "--verify", start + "^{commit}")
    finish = git("rev-parse", "--verify", finish + "^{commit}")
    subprocess.check_call(["git", "-C", str(upstream), "merge-base", "--is-ancestor",
                           start, finish])
    commits = []
    for sha in git("rev-list", "--reverse", start + ".." + finish).splitlines():
        paths = sorted(set(git("diff-tree", "--root", "-m", "--no-commit-id",
                               "--name-only", "-r", sha).splitlines()))
        candidates = set()
        components = []
        for path in paths:
            candidates.update(e["path"] for e in lock["files"]
                              if e["upstream_path"] == path)
            for component in lock["components"]:
                if any(path == p or path.startswith(p + "/")
                       for p in component["upstream_paths"]):
                    candidates.update(component["local_paths"])
                    if component not in components:
                        components.append(component)
        commits.append({"commit": sha, "subject": git("show", "-s", "--format=%s", sha),
                        "upstream_paths": paths, "candidate_local_paths": sorted(candidates),
                        "components": components, "status": "pending_analysis"})
    return {"from_commit": start, "to_commit": finish, "commits": commits}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--upstream", required=True, type=Path)
    parser.add_argument("--from-ref", required=True)
    parser.add_argument("--to-ref", required=True)
    args = parser.parse_args()
    if args.from_ref.startswith("-") or args.to_ref.startswith("-"):
        parser.error("refs must not start with '-'")
    print(json.dumps(report(args.upstream, args.from_ref, args.to_ref,
                            json.loads(LOCK.read_text())), indent=2))


if __name__ == "__main__":
    try:
        main()
    except (OSError, ValueError, subprocess.CalledProcessError) as error:
        print("Upstream comparison failed: " + str(error), file=sys.stderr)
        sys.exit(1)
