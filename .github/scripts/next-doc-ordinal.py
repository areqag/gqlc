#!/usr/bin/env python3
"""
Allocate the next free ordinal in a hand-numbered series (bd gqlc-dawo1).

Usage: next-doc-ordinal.py DIR [DIR ...]

THE SIBLING GATE READS A TREE; THIS READS THE REMOTE. check-doc-ordinals.py
(bd gqlc-lsll5) refuses two documents claiming one ordinal, but it can only see
a collision once both documents are in ONE tree -- at rebase or at merge. By
then the number is spent, the document is written, and the fix is a rename plus
every citation aimed at it. The collision was CREATED earlier, at the moment an
author picked "the next free number" by reading master.

MEASURED 2026-09-01 by Այգ, and the repository was carrying a live instance
while this was written. master's highest ADR ordinal was 0037, so the naive read
offered 0038 -- and 0038 already existed on
origin/feat/gqlc-7aw2j-wrong-orientation-drop-warning, an open PR. An author
allocating that day from master alone would have collided, exactly as PR #2105
collided with PR #2093 over 0036 the day before. Both reads of master were
correct when they were made. That is the whole defect: master is not the
allocation namespace, and nothing told anybody so.

WHY IT FETCHES, AND WHY IT REFUSES RATHER THAN DEGRADE. A remote-tracking ref
is only as fresh as the last fetch, so answering from stale refs reproduces the
very bug -- a number that looks free because the evidence was old. This script
therefore fetches, and if it cannot, it EXITS NONZERO with no number. Printing
the master-only answer with a caveat was considered and rejected: the caveat is
advice, the number is what gets used, and a tool that hands you the wrong number
politely is worse than one that hands you nothing. There is no bootstrap path
that needs an ordinal without a network -- an ADR that cannot be pushed does not
need a number yet.

WHY REMOTE BRANCHES AND NOT `gh pr list`. A branch holds its number from the
moment the file is committed, which is before a PR exists and stays true if the
PR is never opened. Open PRs are a SUBSET of remote branches here (origin is the
only remote and there are no fork PRs), so scanning branches is both cheaper --
one fetch, no API -- and strictly wider. If this repository ever takes fork PRs,
that assumption breaks and this docstring is where to correct it.

WHAT IT STILL CANNOT SEE, stated because an unstated limit reads as a guarantee:
a number written in a working tree that has never been pushed, on any machine
including this one before its first push. Two authors who both allocate offline
and push later still collide, and check-doc-ordinals.py is what catches them.
This narrows the window from "until merge" to "until push"; it does not close
it.
"""

import re
import subprocess
import sys
from pathlib import Path

# Four digits, a hyphen, a slug, `.md` -- the same shape check-doc-ordinals.py
# enforces, deliberately duplicated rather than imported: these two run in
# different contexts (a gate in CI, a tool on a laptop) and a shared module
# would make the gate depend on a file whose absence it could not report.
ORDINAL = re.compile(r"^(\d{4})-.+\.md$")

ORDINAL_WIDTH = 4


def git(*args, check=True):
    """Run git, returning stdout. Raises CalledProcessError when check and rc."""
    return subprocess.run(
        ["git", *args], capture_output=True, text=True, check=check
    ).stdout


def fetch_or_die():
    """Refresh remote-tracking refs, or exit having printed no number.

    The refusal is the point: see the module docstring. An answer from stale
    refs is the defect this script exists to prevent, wearing its name.
    """
    try:
        subprocess.run(
            ["git", "fetch", "--quiet", "origin"],
            capture_output=True,
            text=True,
            check=True,
        )
    except subprocess.CalledProcessError as exc:
        print(
            "error: could not fetch origin, so the remote's ordinals are unknown "
            "and no number can be offered.\n"
            "      This script refuses rather than falling back to what this "
            "tree alone knows: a number that looks free because the evidence "
            "was stale is the exact defect it exists to prevent.\n"
            f"      git said: {(exc.stderr or '').strip()}",
            file=sys.stderr,
        )
        return False
    except FileNotFoundError:
        print("error: git not found on PATH.", file=sys.stderr)
        return False
    return True


def ordinals_in_tree(directory):
    """Ordinals of files in the working tree, as {ordinal: [source, ...]}."""
    found = {}
    if not directory.is_dir():
        return found
    for path in sorted(directory.iterdir()):
        if not path.is_file():
            continue
        match = ORDINAL.match(path.name)
        if match:
            found.setdefault(match.group(1), []).append(f"this tree: {path.name}")
    return found


def ordinals_on_remotes(directory):
    """Ordinals held by any origin branch, as {ordinal: [source, ...]}."""
    found = {}
    refs = git(
        "for-each-ref", "--format=%(refname:short)", "refs/remotes/origin"
    ).split()
    for ref in refs:
        listing = git(
            "ls-tree", "-r", "--name-only", ref, "--", str(directory), check=False
        )
        for line in listing.splitlines():
            match = ORDINAL.match(line.rsplit("/", 1)[-1])
            if match:
                found.setdefault(match.group(1), []).append(f"{ref}: {line}")
    return found


def next_free(directory):
    """Report (next ordinal, taken map) for one series."""
    taken = ordinals_in_tree(directory)
    for ordinal, sources in ordinals_on_remotes(directory).items():
        taken.setdefault(ordinal, []).extend(sources)
    highest = max((int(o) for o in taken), default=0)
    return f"{highest + 1:0{ORDINAL_WIDTH}d}", taken


def report(directory):
    """Print one series' allocation, and what the naive read would have said."""
    ordinal, taken = next_free(directory)
    if not taken:
        print(
            f"error: no numbered documents found in {directory}, on this tree or "
            "any origin branch, so this is not a series this script can allocate "
            "in.",
            file=sys.stderr,
        )
        return None

    print(f"{directory}: next free ordinal is {ordinal}")

    # The comparison is the argument for using this at all: when it prints
    # nothing the tool looks pointless, and when it prints something it has just
    # prevented the collision it exists for.
    on_master = {
        o
        for o, sources in taken.items()
        if any(s.startswith("origin/master:") for s in sources)
    }
    naive = f"{max((int(o) for o in on_master), default=0) + 1:0{ORDINAL_WIDTH}d}"
    if naive != ordinal:
        print(
            f"  reading origin/master alone would have offered {naive}, which is "
            "already taken:"
        )
        for source in sorted(taken.get(naive, [])):
            print(f"    {source}")
    return ordinal


def main(argv):
    directories = argv[1:]
    if not directories:
        print(f"usage: {argv[0]} DIR [DIR ...]", file=sys.stderr)
        return 2

    if not fetch_or_die():
        return 1

    failed = False
    for name in directories:
        if report(Path(name)) is None:
            failed = True
    return 1 if failed else 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
