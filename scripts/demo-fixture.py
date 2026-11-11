#!/usr/bin/env python3
"""Builds the demo machine's project fixture (apps/ios/Togo/Tutorial/Resources/demo-project.json).

The demo replays a recorded OpenCode session in togo-demo-status: the "status" page before the
session is that repository's main branch, and the working tree it left is its demo/incidents
branch. This script rebuilds main as a short history by "Jamie" in a scratch repository (no real
names, emails or paths), lays the session's working tree over it, and records what git reports:
the log, each commit's diffs, the working tree's diffs and status, and every file's text.

    scripts/demo-fixture.py ../togo-demo-status > apps/ios/Togo/Tutorial/Resources/demo-project.json
"""
import json
import os
import subprocess
import sys
import tempfile

# main's files, committed in this order. package-lock.json is left out: nobody reviews it.
HISTORY = [
    ("Start the status page", 4 * 86400, [".gitignore", "README.md", "package.json", "tsconfig.json", "src/server.ts", "src/app.ts"]),
    ("Read services from data/services.json", 3 * 86400, ["data/services.json", "src/services.ts"]),
    ("Draw 30 days of uptime under each service", 2 * 86400, ["src/uptime.ts", "src/render.ts", "public/style.css"]),
    ("Test the page and the uptime bars", 26 * 3600, ["test/app.test.ts", "test/uptime.test.ts", "opencode.json"]),
]
ROOT = "/Users/jamie/status"
AUTHOR = "Jamie"
EMAIL = "jamie@example.com"


def git(cwd, *args, env=None):
    return subprocess.run(["git", *args], cwd=cwd, check=True, capture_output=True, text=True, env=env).stdout


def show(repo, ref, path):
    return git(repo, "show", f"{ref}:{path}")


def main():
    source = os.path.abspath(sys.argv[1])
    changed = git(source, "diff", "--name-status", "main", "demo/incidents").split("\n")
    changed = [line.split("\t") for line in changed if line]
    with tempfile.TemporaryDirectory() as scratch:
        git(scratch, "init", "-q", "-b", "main")
        git(scratch, "config", "user.name", AUTHOR)
        git(scratch, "config", "user.email", EMAIL)
        head = {}
        commits = []
        base = 1_762_000_000
        for subject, age, paths in HISTORY:
            for path in paths:
                text = show(source, "main", path)
                head[path] = text
                os.makedirs(os.path.dirname(os.path.join(scratch, path)) or scratch, exist_ok=True)
                with open(os.path.join(scratch, path), "w") as f:
                    f.write(text)
            git(scratch, "add", *paths)
            stamp = f"{base - age} +0000"
            env = dict(os.environ, GIT_AUTHOR_DATE=stamp, GIT_COMMITTER_DATE=stamp)
            git(scratch, "commit", "-q", "-m", subject, env=env)
            sha = git(scratch, "rev-parse", "HEAD").strip()
            diffs = {p: git(scratch, "show", "--format=", sha, "--", p) for p in paths}
            commits.append({"hash": sha, "short": sha[:7], "subject": subject, "age": age, "paths": paths, "diffs": diffs})
        for i, commit in enumerate(commits):
            commit["parents"] = [commits[i - 1]["hash"]] if i else []
        commits.reverse()

        work, status = {}, []
        for kind, path in changed:
            text = show(source, "demo/incidents", path)
            work[path] = text
            with open(os.path.join(scratch, path), "w") as f:
                f.write(text)
            status.append({"path": path, "index": "?" if kind == "A" else ".", "worktree": "?" if kind == "A" else "M"})
        diffs = {s["path"]: git(scratch, "diff", "--", s["path"]) for s in status if s["worktree"] == "M"}

    for text in list(head.values()) + list(work.values()):
        assert "/Users/" not in text and "jamie" not in text.lower()
    json.dump({
        "root": ROOT, "branch": "main", "author": AUTHOR,
        "head": head, "work": work, "status": status, "diffs": diffs, "commits": commits,
    }, sys.stdout, indent=1, sort_keys=True, ensure_ascii=False)
    sys.stdout.write("\n")


if __name__ == "__main__":
    main()
