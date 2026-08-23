#!/usr/bin/env python3
"""
Refuse a bead label GitHub cannot mirror (bd gqlc-89vw).

Usage: check-label-lengths.py [jsonl_path]   (default: .beads/issues.jsonl)

MEASURED 2026-08-22. The very first `subject:` label the town filed was
rejected by the GitHub API:

    Failed to create gqlc-xiri in GitHub: Validation Failed
    value: "subject:kingdom/brain/playbooks/citizen-protocol.md"
    resource: Label, field: name, code: invalid

That string is 51 characters. GitHub caps a label name at 50, which leaves 42
characters of path after the `subject:` prefix.

WHY THIS IS A GATE AND NOT A WARNING. The failure is a warning from
.githooks/bd-gh-sync, not an error: the bead is created in bd, the hold
machinery keeps working (it reads bd labels, not GitHub ones), and the only
casualty is the bd<->GitHub 1:1 mirror the town uses for review and closing.
So the scheme degrades silently, and the person who pays is whoever later
wonders why a bead has no External link. A label too long is therefore visible
exactly once, in the scrollback of an unrelated push, and never again.

THE REMEDY THIS NAMES is the one the scheme already permits: label the deepest
ancestor DIRECTORY that fits. That is what gqlc-xiri does today
(subject:kingdom/brain/playbooks, 31 chars). It costs precision -- a directory
subject holds on any PR touching anything beneath it -- and it keeps the label
vocabulary readable, which a hashed or abbreviated tail would not.

The check is over EVERY label, not just `subject:` ones: the 50-char cap is
GitHub's, and it applies to every label the town mints.

Scope note: this runs over the committed export, so it catches an unmirrorable
label at PR time rather than at filing time. Refusing at filing time, inside
.githooks/bd-gh-sync, is the other half and is filed separately -- this gate is
what stops one reaching master in the meantime.
"""

import json
import sys

# GitHub's cap on a label name. Measured, not documented from memory: a
# 51-character name was rejected as `code: invalid` (see module docstring).
MAX_LABEL = 50

PREFIX = "subject:"


def main(argv):
    path = argv[1] if len(argv) > 1 else ".beads/issues.jsonl"

    try:
        with open(path, encoding="utf-8") as fh:
            lines = fh.readlines()
    except OSError as exc:
        # An unreadable export is a failure, not a skip: a gate that cannot
        # read its input has not cleared anything.
        print(f"error: cannot read {path}: {exc}", file=sys.stderr)
        return 1

    offenders = []
    beads = 0
    labels_seen = 0

    for lineno, line in enumerate(lines, start=1):
        if not line.strip():
            continue
        try:
            issue = json.loads(line)
        except json.JSONDecodeError as exc:
            print(f"error: {path}:{lineno} is not JSON: {exc}", file=sys.stderr)
            return 1
        beads += 1
        for label in issue.get("labels") or []:
            labels_seen += 1
            if len(label) > MAX_LABEL:
                offenders.append((issue.get("id", "(no id)"), label))

    if beads == 0:
        # The export always carries beads. Zero means the file moved or the
        # format changed, and a gate that examined nothing must not print a
        # pass -- this repo has shipped a detector that exited 0 on the very
        # condition it was written to catch.
        print(
            f"error: {path} contains no issues, so this gate examined no labels at all.",
            file=sys.stderr,
        )
        return 1

    if offenders:
        print(
            f"error: {len(offenders)} label(s) exceed GitHub's {MAX_LABEL}-character cap "
            "and cannot be mirrored:",
            file=sys.stderr,
        )
        for bead_id, label in offenders:
            print(f"  {bead_id}: {label!r} ({len(label)} chars)", file=sys.stderr)
            if label.startswith(PREFIX):
                budget = MAX_LABEL - len(PREFIX)
                path_part = label[len(PREFIX) :]
                suggestion = path_part
                while len(suggestion) > budget and "/" in suggestion:
                    suggestion = suggestion.rsplit("/", 1)[0]
                if len(suggestion) <= budget and suggestion != path_part:
                    print(
                        f"      use the deepest ancestor directory that fits: "
                        f"{PREFIX}{suggestion} ({len(PREFIX) + len(suggestion)} chars)",
                        file=sys.stderr,
                    )
                else:
                    print(
                        f"      the path budget after '{PREFIX}' is {budget} characters; "
                        "no ancestor directory of this path fits",
                        file=sys.stderr,
                    )
        print(
            "      bd-gh-sync only WARNS on this, so an unmirrored bead looks healthy "
            "on every board (bd gqlc-89vw).",
            file=sys.stderr,
        )
        return 1

    print(
        f"checked {labels_seen} label(s) on {beads} bead(s): "
        f"all within GitHub's {MAX_LABEL}-character cap"
    )
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
