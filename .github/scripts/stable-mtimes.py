#!/usr/bin/env python3
"""Give every tracked file an mtime that is a pure function of its blob hash.

Go's test cache records stat info (including mtime) for every file a test
opens, so a test result only replays when those mtimes match the cached run.
actions/checkout clones fresh each run, stamping every file with checkout
time — which is why testdata-reading packages never printed `(cached)` on CI
while an untouched local checkout replays them fine (bd gqlc-84y6, probed:
`touch` alone on testdata, content unchanged, forces a full re-run).

Deriving the mtime from the blob hash rather than from `git log` is what
makes this work under the shallow (depth-1) checkouts CI uses: the last
commit that touched a file is not in a shallow history, but the blob hash is
in the index. Content unchanged across pushes -> same mtime -> the test
cache key survives commits that did not touch the files the test reads.

The offset keeps every derived mtime in the past (1_500_000_000 is 2017-07;
7 hex digits add at most ~268M seconds, landing no later than 2026-01).
Future mtimes are what some tools warn or misbehave on; past ones are inert.
"""

import os
import subprocess

out = subprocess.run(
    ["git", "ls-files", "-sz"], check=True, capture_output=True
).stdout

count = 0
for entry in out.split(b"\0"):
    if not entry:
        continue
    meta, path = entry.split(b"\t", 1)
    sha = meta.split()[1]
    mtime = 1_500_000_000 + int(sha[:7], 16)
    os.utime(path, (mtime, mtime), follow_symlinks=False)
    count += 1

print(f"stable-mtimes: stamped {count} tracked files")
