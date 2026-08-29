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
label at PR time -- the LAST chance to catch one, and the only refusal in the
chain that withholds anything from master. The earlier ones are named below and
neither stops a commit: the creation-time one refuses the command that would
mint the label, and bd-gh-sync withholds the bead's MIRROR while the push
carrying the label proceeds.

HOW MANY EARLIER REFUSALS THERE WERE DEPENDS ON THE BEAD, and the difference is
the whole reason to name the chain rather than count it. bd-gh-sync screens only
the beads it is about to mirror: its selection opens with `if
b.get("external_ref"): continue`, so a bead that already HAS its mirror is never
screened and never NOTEd at push time. This gate has no such filter and screens
every bead in the export. So for a label added to an existing bead -- `bd label
add` on a mirrored bead, the common case, since mirrors are the norm -- there is
no push-time notice at all, and no pass in bd-gh-sync offers that bead's labels
to GitHub again, so no 422 is coming either. For that bead this is not the last
of several warnings; it is the only one that is ever printed.

THIS MODULE OWNS THE CAP AND THE REMEDY for all three gates, which is why
MAX_LABEL, PREFIX, shorter() and remedy() are module level and why loading this
file runs nothing (the work is under `if __name__ == "__main__"`). The other two
import it by path:

  - .githooks/bd-gh-sync, at push time, refusing to offer the bead to GitHub;
  - .githooks/claude-pre-bash, at CREATION time, refusing the `bd create` /
    `bd update` / `bd label add` that would mint the label (bd gqlc-uy7j).

The remedy sentence was written three times before that -- once here, once in
bd-gh-sync's selection, and nearly a third time in the creation-time arm. Two
spellings of one number disagree exactly when somebody raises one of them, and
the whole point of refusing early is that the citizen meets the SAME words at
the keyboard as in CI.
"""

import json
import sys

# GitHub's cap on a label name. Measured, not documented from memory: a
# 51-character name was rejected as `code: invalid` (see module docstring).
MAX_LABEL = 50

PREFIX = "subject:"


def shorter(label, cap=MAX_LABEL, prefix=PREFIX):
    """The deepest ancestor DIRECTORY of a `subject:` label that fits, or None.

    None covers three different situations on purpose, all of which mean "this
    function has no suggestion to offer": the label does not carry the prefix,
    no ancestor is short enough, and the label already fits (`s != tail` is
    false, so a label needing no shortening is not handed back as advice).
    """
    if not label.startswith(prefix):
        return None
    budget = cap - len(prefix)
    tail = label[len(prefix):]
    s = tail
    while len(s) > budget and "/" in s:
        s = s.rsplit("/", 1)[0]
    return prefix + s if len(s) <= budget and s != tail else None


def remedy(label, cap=MAX_LABEL, prefix=PREFIX):
    """One sentence telling the author what to type instead, or "" when there
    is nothing useful to say (a label that is not a `subject:` path)."""
    fits = shorter(label, cap, prefix)
    if fits:
        return "Use the deepest ancestor directory that fits: %s (%d characters)." % (
            fits,
            len(fits),
        )
    # `prefix and` is load-bearing: str.startswith("") is True for everything,
    # so an empty prefix would otherwise promise a path budget to every label.
    if prefix and label.startswith(prefix):
        return (
            "The budget after '%s' is %d characters, and no ancestor directory "
            "of this path fits." % (prefix, cap - len(prefix))
        )
    return ""


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
            advice = remedy(label)
            if advice:
                print(f"      {advice}", file=sys.stderr)
        # What the other two gates do about this, stated so a reader of a red CI
        # job looks in the right place. bd-gh-sync does not refuse the push:
        # .githooks/pre-push discards its exit status, and what it withholds is
        # the bead's MIRROR. It screens the ledger `bd list` returns rather than
        # the command line, so no spelling hides a label from it -- but it
        # screens only beads it is about to mirror, skipping any that already
        # carries an external_ref, so a label put on a mirrored bead is not seen
        # there at all. TWO earlier refusals can therefore have been absent, and
        # the message says which for which: the push-time NOTE, absent for every
        # already-mirrored bead, and the creation-time one, which reads only
        # commands issued through Claude Code's bash hook (bd gqlc-uy7j).
        print(
            "      What came before this depends on whether the bead already had "
            "a GitHub mirror. If it did NOT, bd-gh-sync withheld the MIRROR "
            "rather than the push, so the label reached master with the bead "
            "unmirrored, and where hooks were live it named this label in a NOTE "
            "at push time. If it DID, bd-gh-sync skipped the bead -- it screens "
            "only beads it is about to mirror -- so there was no NOTE to find, "
            "and no pass in it offers a mirrored bead's labels to GitHub again, "
            "so this message is the only notice the label gets. Either way the "
            "creation-time refusal in .githooks/claude-pre-bash reads only "
            "commands issued through Claude Code's bash hook, so a plain "
            "terminal, disabled hooks, or a spelling it does not parse arrives "
            "here (bd gqlc-89vw, gqlc-uy7j).",
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
