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
label at PR time, which is the LAST of the three chances to catch one. The two
earlier refusals are named below; this gate is what stops a label reaching
master when both of them are bypassed.

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
        # job knows where else it will bite. bd-gh-sync REFUSES the push (it
        # stopped merely warning in #1330); claude-pre-bash refuses the command
        # that mints the label. So a label reaching this gate means both earlier
        # refusals were bypassed -- most often because the label was written
        # somewhere neither of them can read, such as `bd create --file`.
        print(
            "      .githooks/claude-pre-bash refuses this at creation time and "
            "bd-gh-sync refuses the push, so reaching CI means it was minted "
            "somewhere neither reads (bd gqlc-89vw, gqlc-uy7j).",
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
