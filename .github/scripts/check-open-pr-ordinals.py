#!/usr/bin/env python3
"""
Re-ask the ordinal question for every open PR when master moves (bd gqlc-4plwf).

Usage: check-open-pr-ordinals.py DIR [DIR ...]
       check-open-pr-ordinals.py --self-test

THE HALF check-doc-ordinals.py CANNOT SEE. That gate reads a TREE, and on a pull
request the tree it reads is the merge ref, which GitHub assembles when the
author last pushed. The collision this file exists for arrives afterwards, from
the OTHER side: master takes the ordinal while the PR sits still. Nothing about
the PR changes, so no `pull_request` event fires, so the merge ref is never
rebuilt and the green check keeps standing for a number that is now taken.

Measured window, 2026-09-02 (bd gqlc-4plwf): between 16:49 and 20:12 a PR held
an ordinal that master had already adopted, and every gate the repository has
was green the whole time.

WHY THIS SHAPE. No event exists for "your base moved" -- except that the base
moves BY a master push, which is an event we already receive. So the master
push re-asks the question for every PR still open and delivers the answer as a
commit status on each PR's head SHA, where its checks list already is. The
collision reads as a red X within minutes, with no author push and no cron.
Alternatives and why they lost are in gqlc-4plwf's design note (Արփինէ).

WHAT IS COMPARED, and this is the subtle part. Per PR, a THREE-DOT compare of
the pushed master against the PR head, taking only files the PR ADDS (status
added / renamed / copied) under an enrolled directory. Not the union of the two
trees: a stale head still carrying an old master file whose ordinal master has
since renumbered-and-reused collides in a union, but is absent from the tree
the squash merge actually produces, so a union would report a collision that
cannot happen. `renamed` is included because a renumber IS a rename and claims
its new ordinal.

The verdict itself is delegated to check-doc-ordinals.py rather than
reimplemented, by materializing a scratch directory of EMPTY files -- master's
filenames plus the PR's additions, deduped by name. That reuses one copy of the
ordinal regex, the collision logic, the mixed-directory refusal and the remedy
text; two copies of a series convention drift silently. Empty files are enough
because that checker's offenders() stats filenames and never opens one.

THIS RUN GOES GREEN ON A COLLISION IT FINDS. The defect belongs to the PR and
is reported on the PR; master is fine, and a collision that actually lands on
master is caught by the unconditional tree check on ci.yml's push arm. What
DOES fail this run is a broken instrument: an API call that errors, or a
compare whose file list hits the cap below.

THE COMPARE FILE CAP IS SILENT, MEASURED NOT ASSUMED. On 2026-09-02 against
this repository, `repos/{owner}/{repo}/compare/{base}...{head}` returned exactly
300 entries in `files` for a range whose `total_commits` was 850, sent NO Link
header, and ignored pagination: `?per_page=100&page=1` still returned 300 and
`page=2` returned 0. So a truncated response is indistinguishable from a
complete one by anything in the response, and there is no page to ask for the
rest. A PR at the cap is therefore refused loudly rather than under-checked --
under-checking here means a green status on a question nobody answered, which
is the exact failure this file exists to remove.
"""

import json
import os
import subprocess
import sys
import tempfile
from pathlib import Path

# The context these statuses are posted under. It is deliberately NOT a required
# check: required demands a green status on every head SHA at every event, so a
# PR opened after the last master push would wait forever on a context nobody
# will post. The acceptance for this bead asks for RED, not BLOCKED.
CONTEXT = "ordinal-recheck"

# GitHub truncates a commit-status description past 140 characters, so a
# description assembled from the checker's own words is cut here rather than by
# the API, which would drop the remedy silently mid-word.
DESCRIPTION_LIMIT = 140

# What the compare endpoint returns when it has stopped telling the truth about
# how many files changed. Measured, with the falsifier, in the docstring above.
COMPARE_FILE_CAP = 300

# A file the PR ADDS under an enrolled directory claims an ordinal. A modified
# or deleted file does not: modifying 0012 leaves 0012 claimed once, and
# deleting it frees the number rather than taking it.
CLAIMING_STATUSES = frozenset({"added", "renamed", "copied"})

CHECKER = Path(__file__).resolve().parent / "check-doc-ordinals.py"


def run(argv, check=True):
    """Run a command, returning it complete. Failure is fatal by default.

    No `|| true` anywhere in this file: an instrument that cannot read its
    input has cleared nothing, and a swallowed API error would leave every PR
    silently unchecked while the run stayed green -- which is the shape of the
    defect this gate exists to catch, reproduced in the gate itself.
    """
    result = subprocess.run(argv, capture_output=True, text=True)
    if check and result.returncode != 0:
        sys.exit(
            f"error: {' '.join(argv[:3])} failed (rc={result.returncode}):\n"
            f"{result.stderr.strip()}"
        )
    return result


def verdict(directory, master_names, added_names):
    """Decide one series for one PR, as (ok, description).

    The decision core, kept free of the network so --self-test can drive it
    with a reconstructed window. `directory` is named only so the delegated
    checker's messages read as they do everywhere else; nothing is read from it.
    """
    names = sorted(set(master_names) | set(added_names))
    with tempfile.TemporaryDirectory() as scratch:
        series = Path(scratch) / Path(directory).name
        series.mkdir()
        for name in names:
            # Empty is sufficient: the delegated checker stats filenames and
            # never opens a file. Asserted by its own docstring and by reading
            # offenders(); if that ever stops being true this materialization
            # is what has to change, not the call site.
            (series / name).touch()
        result = run([sys.executable, str(CHECKER), str(series)], check=False)

    if result.returncode == 0:
        return True, ""

    # The checker's first error line already names the ordinal and the count.
    # Reusing its sentence keeps one wording for one defect wherever it is
    # reported; a description written here would drift from the one the author
    # sees in CI.
    first = next(
        (line for line in result.stderr.splitlines() if line.startswith("error:")),
        "",
    )
    detail = first[len("error:") :].strip().rstrip(":")
    detail = detail.replace(str(series), directory)
    return False, detail[:DESCRIPTION_LIMIT]


def claimed_by(head_sha, files, directories):
    """The enrolled files a compare's entries claim, as {directory: [name, ...]}.

    The second decision core, kept free of the network for the reason verdict()
    is: which entry claims an ordinal is a judgement, and a test that can only
    see the final verdict cannot see it being made. Refuses at the cap here
    rather than in the caller because the cap is a property of this list.
    """
    if len(files) >= COMPARE_FILE_CAP:
        sys.exit(
            f"error: the compare for {head_sha} returned {len(files)} files, at or "
            f"over the {COMPARE_FILE_CAP} the API caps this response at. It sends no "
            "Link header and does not paginate, so the rest cannot be fetched and a "
            "truncated list is indistinguishable from a complete one. Refusing to "
            "post a verdict on a question that was not fully asked."
        )

    claimed = {directory: [] for directory in directories}
    for entry in files:
        if entry.get("status") not in CLAIMING_STATUSES:
            continue
        path = Path(entry["filename"])
        for directory in directories:
            # Equality, not a prefix test: an enrolled series is one flat
            # directory, and a document in a subdirectory of it is not part of
            # the numbering these two gates share.
            if path.parent == Path(directory):
                claimed[directory].append(path.name)
    return claimed


def added_under(repo, base_sha, head_sha, directories):
    """The enrolled files this PR adds, as {directory: [basename, ...]}."""
    compare = json.loads(
        run(
            [
                "gh",
                "api",
                f"repos/{repo}/compare/{base_sha}...{head_sha}",
            ]
        ).stdout
    )
    return claimed_by(head_sha, compare.get("files", []), directories)


def post_status(repo, head_sha, state, description, target_url):
    run(
        [
            "gh",
            "api",
            "-X",
            "POST",
            f"repos/{repo}/statuses/{head_sha}",
            "-f",
            f"state={state}",
            "-f",
            f"context={CONTEXT}",
            "-f",
            f"description={description}",
            "-f",
            f"target_url={target_url}",
        ]
    )


def check_open_prs(directories):
    repo = os.environ["GITHUB_REPOSITORY"]
    base_sha = os.environ["GITHUB_SHA"]
    target_url = (
        f"{os.environ['GITHUB_SERVER_URL']}/{repo}/actions/runs"
        f"/{os.environ['GITHUB_RUN_ID']}"
    )

    master = {
        directory: [path.name for path in Path(directory).iterdir() if path.is_file()]
        for directory in directories
    }

    prs = json.loads(
        run(
            [
                "gh",
                "pr",
                "list",
                "--state",
                "open",
                "--limit",
                "1000",
                "--json",
                "number,headRefOid",
            ]
        ).stdout
    )

    for pr in prs:
        head = pr["headRefOid"]
        added = added_under(repo, base_sha, head, directories)
        if not any(added.values()):
            # Silent by design. Most PRs touch no enrolled series, and a
            # success status on every one of them would put a context on every
            # PR in the town to say nothing happened.
            print(f"PR #{pr['number']}: adds no enrolled document, no status posted")
            continue

        failures = []
        for directory in directories:
            if not added[directory]:
                continue
            ok, description = verdict(directory, master[directory], added[directory])
            if not ok:
                failures.append(description)

        if failures:
            post_status(repo, head, "failure", failures[0], target_url)
            print(f"PR #{pr['number']}: FAILURE posted on {head}: {failures[0]}")
        else:
            description = f"no ordinal taken by base @{base_sha[:7]}"
            post_status(repo, head, "success", description, target_url)
            print(f"PR #{pr['number']}: success posted on {head}")
    return 0


def self_test_claimed_by():
    """Drive the status filter: which compare entry claims an ordinal.

    Split out from the verdict rows because a mutation narrowing
    CLAIMING_STATUSES survives every one of them -- the verdict never sees a
    file the filter dropped, so it cannot report the collision it hides.
    """
    enrolled = ["docs/adr", "kingdom/brain/decisions"]
    doc = "0012-something.md"
    rows = [
        ("an added document claims its ordinal", "added", f"docs/adr/{doc}", True),
        ("a renumber arrives as a RENAME and claims", "renamed", f"docs/adr/{doc}", True),
        ("a copy claims the ordinal it lands on", "copied", f"docs/adr/{doc}", True),
        ("modifying 0012 leaves it claimed once, not twice", "modified", f"docs/adr/{doc}", False),
        ("deleting 0012 frees the number, it does not take it", "removed", f"docs/adr/{doc}", False),
        (
            "a date-prefixed series is not enrolled, so it claims nothing",
            "added",
            f"kingdom/brain/postmortems/{doc}",
            False,
        ),
        (
            "a document one level below an enrolled directory is outside its numbering",
            "added",
            f"docs/adr/sub/{doc}",
            False,
        ),
    ]

    failed = False
    for name, status, filename, want_claimed in rows:
        claimed = claimed_by("headsha", [{"status": status, "filename": filename}], enrolled)
        got_claimed = any(claimed.values())
        if got_claimed != want_claimed:
            failed = True
            print(
                f"self-test FAILED: {name}\n"
                f"  wanted claimed={want_claimed}, got {claimed!r}",
                file=sys.stderr,
            )
            continue
        print(f"self-test ok: {name}")

    # The cap refusal, driven rather than trusted. It is the one place this file
    # chooses to fail loudly instead of answering, so a mutation that deletes it
    # buys a green status on a question that was never fully asked -- the exact
    # shape of the defect this gate exists to remove.
    name = "a compare at the API's file cap is refused, not under-checked"
    at_cap = [
        {"status": "added", "filename": f"docs/adr/{i:04d}-x.md"}
        for i in range(COMPARE_FILE_CAP)
    ]
    try:
        claimed_by("headsha", at_cap, ["docs/adr"])
    except SystemExit as refusal:
        print(f"self-test ok: {name}")
        if str(COMPARE_FILE_CAP) not in str(refusal):
            failed = True
            print(
                f"self-test FAILED: {name}\n"
                f"  the refusal does not name the cap: {refusal!s:.120}",
                file=sys.stderr,
            )
    else:
        failed = True
        print(
            f"self-test FAILED: {name}\n"
            f"  {COMPARE_FILE_CAP} entries were answered rather than refused",
            file=sys.stderr,
        )

    return failed


def self_test():
    """Drive the decision cores over the window this gate was built from.

    Run by `just gates`, by ci.yml's tidy job on every PR, and by
    ordinal-recheck.yml before any PR is examined, because this repository has
    no Python test runner and a gate whose logic nothing exercises is a gate
    nobody has watched fail. The tidy step is the one that MATTERS for a break:
    the other two red only after the breaking change has merged. The rows are
    the ones the design named as owed, plus the status-filter rows a mutation
    battery showed were owed and missing.
    """
    taken = "0012-release-of-an-unreachable-seat.md"
    rows = [
        (
            "the reconstructed window: master took the ordinal after the last push",
            [taken],
            ["0012-conduct-of-a-seat-that-cannot-reach-the-remote.md"],
            False,
        ),
        (
            "green control: the PR's ordinal is free",
            [taken],
            ["0014-something-else-entirely.md"],
            True,
        ),
        (
            "a renumber arrives as a RENAME claiming a taken ordinal",
            [taken, "0013-an-unrelated-decision.md"],
            ["0012-renumbered-into-a-collision.md"],
            False,
        ),
        (
            "the PR re-adds a file master already has under that exact name",
            [taken],
            [taken],
            True,
        ),
    ]

    failed = self_test_claimed_by()
    for name, master_names, added_names, want_ok in rows:
        got_ok, description = verdict("docs/adr", master_names, added_names)
        if got_ok != want_ok:
            failed = True
            print(
                f"self-test FAILED: {name}\n"
                f"  wanted ok={want_ok}, got ok={got_ok} ({description!r})",
                file=sys.stderr,
            )
            continue
        if not want_ok and "0012" not in description:
            failed = True
            print(
                f"self-test FAILED: {name}\n"
                f"  the description does not name the colliding ordinal: {description!r}",
                file=sys.stderr,
            )
            continue
        print(f"self-test ok: {name}")

    if failed:
        print(
            "error: the decision core does not behave as designed, so no verdict "
            "it posts can be trusted.",
            file=sys.stderr,
        )
        return 1
    return 0


def main(argv):
    args = argv[1:]
    if args == ["--self-test"]:
        return self_test()
    if not args or any(arg.startswith("-") for arg in args):
        print(f"usage: {argv[0]} DIR [DIR ...]", file=sys.stderr)
        print(f"       {argv[0]} --self-test", file=sys.stderr)
        return 2

    for directory in args:
        if not Path(directory).is_dir():
            # The same refusal check-doc-ordinals.py makes, for the same
            # reason: a series renamed out from under this call site would
            # otherwise make the gate quietly stop checking it.
            print(
                f"error: {directory} is not a directory, so nothing was checked.",
                file=sys.stderr,
            )
            return 1

    return check_open_prs(args)


if __name__ == "__main__":
    sys.exit(main(sys.argv))
