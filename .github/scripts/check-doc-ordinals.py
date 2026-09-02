#!/usr/bin/env python3
"""
Refuse two documents claiming one ordinal in a hand-numbered series (bd gqlc-lsll5).

Usage: check-doc-ordinals.py DIR [DIR ...]

MEASURED 2026-09-02 by Ար, publishing gqlc-awtb. PR #2105 was cut when 0036 was
the next free ADR number and took it. While it was in flight, PR #2093 merged
docs/adr/0036-age-admits-non-zoned-temporal-list-elements-at-every-depth.md. The
branch then rebased onto master CLEANLY, passed `just gates` (all 11 arms) and
passed CI (7 arms SUCCESS, tidy among them) with TWO FILES NAMED 0036 in
docs/adr/. Every gate the repo had was green on a tree carrying a duplicate.

WHY NOTHING SAW IT. Two documents claiming one ordinal differ in FILENAME, so
git compares them as two unrelated additions and has nothing to conflict over. A
clean rebase is therefore not weak evidence that the number is still free -- it
is NO evidence, because nothing in the merge ever compares the numbers. And no
tool in the tree parsed an ordinal out of a filename, so there was no later
check either.

WHY IT IS WORSE THAN UNTIDY. These documents are cited BY NUMBER, from source
comments, from bead notes and from designs. Two documents under one number makes
every one of those citations ambiguous, and the ambiguity is silent at the point
of reading: whoever follows "see ADR 0036" gets whichever file they happen to
open. That is how the collision above was found -- by accident, one bead later,
by a warrior whose design cited "ADR 0036" meaning the other document.

WHAT THIS DOES NOT CATCH, and it is the half that bites first. This reads a
TREE, so it sees a collision only once both documents are in one. It cannot warn
the author who is about to allocate a number that an UNMERGED branch already
holds -- which is the moment the collision is actually created, and the moment it
is free to fix. Measured again on 2026-09-01: ADR 0037 existed only on
`fix/gqlc-awtb-narrow-width-refuses` and a "next free number" read of master
would have taken it a second time. The remedy printed below therefore names
remote branches rather than the working tree, and the allocation-time half is
filed separately.

WHICH SERIES ARE ENROLLED is decided by the call sites, not here, because "is
this an ordinal series" is not a property this file can infer. Enrolled today:
docs/adr and kingdom/brain/decisions -- both hand-allocated, both cited by
number, both colliding in exactly the way above.

kingdom/brain/postmortems is deliberately NOT enrolled, and stating that is the
point of this paragraph: it is DATE-prefixed, and its dates are meant to repeat
(two documents dated 2026-08-21 and two dated 2026-08-22 coexist correctly
today). Enrolling it would refuse a tree that is right. An omission nobody wrote
down reads later as an oversight, and the next person re-derives it.

A DIRECTORY WHOSE FILES DO NOT ALL CARRY AN ORDINAL IS A FAILURE, not a skip.
Both enrolled series are 100%% conforming (47 of 47 files on the day this
landed), so there is no legitimate mixed directory to accommodate -- and a gate
that quietly ignores what it cannot parse is a gate that passes over exactly the
files a rename would hide from it.
"""

import re
import sys
from collections import defaultdict
from pathlib import Path

# Four digits, a hyphen, a slug, `.md`. The width is the series' own convention
# rather than a limit this file imposes: every enrolled document uses four.
ORDINAL = re.compile(r"^(\d{4})-.+\.md$")

REMEDY = (
    "Renumber one of them to the next free ordinal -- and pick it by reading the "
    "REMOTE BRANCHES, not this tree. A number free on master can already be taken "
    "by a branch in flight, and a clean rebase will not tell you: two files "
    "claiming one ordinal differ in name, so git has nothing to conflict over. "
    "`git ls-remote --heads origin` then grep the series across those branches, "
    "or `gh pr list --state open --json files` and look for the directory."
)


def offenders(directory):
    """Return (by_ordinal, unparsed, total) for one enrolled directory.

    by_ordinal maps an ordinal to every filename claiming it, so a caller can
    report all colliding names rather than just the second one found.
    """
    by_ordinal = defaultdict(list)
    unparsed = []
    total = 0
    for path in sorted(directory.iterdir()):
        if not path.is_file():
            continue
        total += 1
        match = ORDINAL.match(path.name)
        if match is None:
            unparsed.append(path.name)
            continue
        by_ordinal[match.group(1)].append(path.name)
    return by_ordinal, unparsed, total


def check(directory):
    """Report on one directory, as (error lines, documents seen).

    No error lines means clean. The count rides back with them so a clean run
    can say what it passed over without walking the directory a second time.
    """
    errors = []
    if not directory.is_dir():
        # Not a skip: a gate that cannot read its input has cleared nothing, and
        # a directory renamed out from under this call site would otherwise make
        # the gate silently stop checking the series it exists for.
        return [f"error: {directory} is not a directory, so nothing was checked."], 0

    by_ordinal, unparsed, total = offenders(directory)

    if total == 0:
        return [
            f"error: {directory} contains no files, so this gate checked nothing."
        ], 0

    if unparsed:
        errors.append(
            f"error: {len(unparsed)} file(s) in {directory} carry no NNNN- ordinal, "
            "so this gate cannot tell whether they collide:"
        )
        errors.extend(f"  {name}" for name in unparsed)
        errors.append(
            "      Every file in an enrolled series must be numbered. If this "
            "directory is not an ordinal series, remove it from the call sites "
            "in justfile and .github/workflows/ci.yml rather than loosening "
            "this check."
        )

    for ordinal, names in sorted(by_ordinal.items()):
        if len(names) > 1:
            errors.append(
                f"error: {len(names)} documents in {directory} claim ordinal {ordinal}:"
            )
            errors.extend(f"  {name}" for name in names)
            errors.append(f"      {REMEDY}")

    return errors, total


def main(argv):
    directories = argv[1:]
    if not directories:
        print(f"usage: {argv[0]} DIR [DIR ...]", file=sys.stderr)
        return 2

    failed = False
    checked = []
    for name in directories:
        errors, total = check(Path(name))
        if errors:
            failed = True
            for line in errors:
                print(line, file=sys.stderr)
            continue
        checked.append((name, total))

    if failed:
        return 1

    # The counts are printed rather than a bare "ok" for the reason the empty
    # directory is refused above: a pass has to say what it passed over.
    for name, total in checked:
        print(f"checked {total} document(s) in {name}: every ordinal is unique")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
