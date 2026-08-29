#!/usr/bin/env python3
"""Stamp deterministic mtimes: tracked files get a pure function of their
blob hash, directories containing them get a constant.

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
import posixpath
import subprocess

out = subprocess.run(
    ["git", "ls-files", "-sz"], check=True, capture_output=True
).stdout

count = 0
dirs = {"."}
for entry in out.split(b"\0"):
    if not entry:
        continue
    meta, path = entry.split(b"\t", 1)
    sha = meta.split()[1]
    mtime = 1_500_000_000 + int(sha[:7], 16)
    os.utime(path, (mtime, mtime), follow_symlinks=False)
    count += 1
    d = posixpath.dirname(path.decode())
    while d and d not in dirs:
        dirs.add(d)
        d = posixpath.dirname(d)

# Directories too: hashOpen on a directory records the directory's OWN stat
# before walking its entries, and directories are not tracked by git, so a
# fresh clone mints new mtimes for all of them. Probed on
# internal/codegen/age: touching only the package dir and its testdata dir,
# no file changed, forces a full re-run. A constant is enough — the entry
# list is hashed per-entry anyway, so the dir mtime carries no signal and
# only needs to be identical across checkouts.
DIR_MTIME = 1_500_000_000
for d in sorted(dirs):
    os.utime(d, (DIR_MTIME, DIR_MTIME))

print(f"stable-mtimes: stamped {count} tracked files, {len(dirs)} directories")
