#!/usr/bin/env python3
"""
Allocate the next free ordinal in a hand-numbered series (bd gqlc-dawo1).

Usage: next-doc-ordinal.py DIR [DIR ...]
       next-doc-ordinal.py --self-test

THE SIBLING GATE READS A TREE; THIS READS THE REMOTE. check-doc-ordinals.py
(bd gqlc-lsll5) refuses two documents claiming one ordinal, but it can only see
a collision once both documents are in ONE tree -- at rebase or at merge. By
then the number is spent, the document is written, and the fix is a rename plus
every citation aimed at it. The collision was CREATED earlier, at the moment an
author picked "the next free number" by reading master.

MEASURED 2026-09-01, and the repository was carrying a live instance
while this was written. master's highest ADR ordinal was 0037, so the naive read
offered 0038 -- and 0038 already existed on the branch of PR #2126, open at the
time and merged the next day. The PR number is cited rather than the branch name
this script actually matched on, because a merged PR's branch is eventually
deleted and a citation nobody can resolve reads later as a claim nobody checked.
An author allocating that day from master alone would have collided, as PR #2105
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
import tempfile
from contextlib import redirect_stderr, redirect_stdout
from io import StringIO
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


def _stub_remotes(taken):
    """Stand in for ordinals_on_remotes with a canned map, for --self-test."""

    def fake(directory):
        return {ordinal: list(sources) for ordinal, sources in taken.items()}

    return fake


def self_test():
    """Drive the allocation core without touching the network.

    Run by `just gates` and by ci.yml's tidy job on every PR, because this
    repository has no Python test runner and a tool nothing exercises is one
    nobody has watched fail (bd gqlc-c30cl). The fetch is the one part these
    rows do NOT drive: the tool refuses without a network by design, so a row
    that fetched would be a flaky red. The stub stands in for the remote layer
    only; the merge of tree and remotes, the offer arithmetic and the
    naive-read comparison all run for real.
    """
    failed = False

    shape_rows = [
        ("a four-digit ordinal with a slug claims its number", "0038-x.md", "0038"),
        ("the first ordinal is well-formed", "0001-a.md", "0001"),
        ("two digits are not an ordinal", "38-x.md", None),
        ("five digits are not an ordinal", "00038-x.md", None),
        ("a bare number with no slug claims nothing", "0038.md", None),
        ("a non-markdown suffix claims nothing", "0038-x.txt", None),
    ]
    for name, filename, want in shape_rows:
        match = ORDINAL.match(filename)
        got = match.group(1) if match else None
        if got != want:
            failed = True
            print(
                f"self-test FAILED: {name}\n  wanted {want!r}, got {got!r}",
                file=sys.stderr,
            )
            continue
        print(f"self-test ok: {name}")

    with tempfile.TemporaryDirectory() as tmp:
        series = Path(tmp)
        (series / "0001-first.md").write_text("a")
        (series / "0002-second.md").write_text("b")
        (series / "notes.md").write_text("not a numbered document")
        (series / "sub").mkdir()
        (series / "sub" / "0003-nested.md").write_text("outside the series")
        got = ordinals_in_tree(series)
        name = "the tree read takes numbered files and nothing else"
        if sorted(got) != ["0001", "0002"]:
            failed = True
            print(
                f"self-test FAILED: {name}\n  wanted ['0001', '0002'], "
                f"got {sorted(got)!r}",
                file=sys.stderr,
            )
        else:
            print(f"self-test ok: {name}")
        name = "a missing directory reads as empty, not an error"
        if ordinals_in_tree(series / "no-such-series") != {}:
            failed = True
            print(f"self-test FAILED: {name}", file=sys.stderr)
        else:
            print(f"self-test ok: {name}")

    alloc_rows = [
        (
            "the measured window: a branch holds an ordinal above the tree max",
            ["0001-on-master.md", "0002-on-master.md"],
            {
                "0001": ["origin/master: docs/adr/0001-on-master.md"],
                "0002": ["origin/master: docs/adr/0002-on-master.md"],
                "0003": ["origin/some-branch: docs/adr/0003-in-flight.md"],
            },
            "0004",
        ),
        (
            "green control: the remotes agree with the tree",
            ["0001-on-master.md", "0002-on-master.md"],
            {
                "0001": ["origin/master: docs/adr/0001-on-master.md"],
                "0002": ["origin/master: docs/adr/0002-on-master.md"],
            },
            "0003",
        ),
        (
            "a branch behind the tree does not move the offer",
            ["0001-a.md", "0002-b.md", "0003-c.md"],
            {
                "0001": ["origin/master: docs/adr/0001-a.md"],
                "0002": ["origin/master: docs/adr/0002-b.md"],
            },
            "0004",
        ),
    ]
    real_remotes = ordinals_on_remotes
    try:
        for name, tree_files, remote_taken, want in alloc_rows:
            globals()["ordinals_on_remotes"] = _stub_remotes(remote_taken)
            with tempfile.TemporaryDirectory() as tmp:
                series = Path(tmp)
                for filename in tree_files:
                    (series / filename).write_text("x")
                got, _ = next_free(series)
            if got != want:
                failed = True
                print(
                    f"self-test FAILED: {name}\n  wanted {want!r}, got {got!r}",
                    file=sys.stderr,
                )
                continue
            print(f"self-test ok: {name}")

        name = "the report names the number the naive master-only read would take"
        globals()["ordinals_on_remotes"] = _stub_remotes(
            {
                "0001": ["origin/master: docs/adr/0001-a.md"],
                "0002": ["origin/master: docs/adr/0002-b.md"],
                "0003": ["origin/some-branch: docs/adr/0003-in-flight.md"],
            }
        )
        with tempfile.TemporaryDirectory() as tmp:
            series = Path(tmp)
            (series / "0001-a.md").write_text("x")
            (series / "0002-b.md").write_text("x")
            out = StringIO()
            with redirect_stdout(out):
                got = report(series)
        if got != "0004" or "would have offered 0003" not in out.getvalue():
            failed = True
            print(
                f"self-test FAILED: {name}\n  got {got!r}: {out.getvalue()!r}",
                file=sys.stderr,
            )
        else:
            print(f"self-test ok: {name}")

        name = "no divergence line when master already agrees"
        globals()["ordinals_on_remotes"] = _stub_remotes(
            {
                "0001": ["origin/master: docs/adr/0001-a.md"],
                "0002": ["origin/master: docs/adr/0002-b.md"],
            }
        )
        with tempfile.TemporaryDirectory() as tmp:
            series = Path(tmp)
            (series / "0001-a.md").write_text("x")
            (series / "0002-b.md").write_text("x")
            out = StringIO()
            with redirect_stdout(out):
                got = report(series)
        if got != "0003" or "would have offered" in out.getvalue():
            failed = True
            print(
                f"self-test FAILED: {name}\n  got {got!r}: {out.getvalue()!r}",
                file=sys.stderr,
            )
        else:
            print(f"self-test ok: {name}")

        name = "a directory with no series anywhere is refused, not numbered"
        globals()["ordinals_on_remotes"] = _stub_remotes({})
        with tempfile.TemporaryDirectory() as tmp:
            out, err = StringIO(), StringIO()
            with redirect_stdout(out), redirect_stderr(err):
                got = report(Path(tmp))
        if got is not None:
            failed = True
            print(
                f"self-test FAILED: {name}\n  wanted None, got {got!r}",
                file=sys.stderr,
            )
        else:
            print(f"self-test ok: {name}")
    finally:
        globals()["ordinals_on_remotes"] = real_remotes

    if failed:
        print(
            "error: the allocation core does not behave as designed, so no "
            "number it offers can be trusted.",
            file=sys.stderr,
        )
        return 1
    return 0


def main(argv):
    if argv[1:] == ["--self-test"]:
        return self_test()
    directories = argv[1:]
    if not directories:
        print(f"usage: {argv[0]} DIR [DIR ...]", file=sys.stderr)
        print(f"       {argv[0]} --self-test", file=sys.stderr)
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
