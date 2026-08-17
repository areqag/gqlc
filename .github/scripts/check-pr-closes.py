#!/usr/bin/env python3
"""
Check that a PR body carries 'Closes #N' when its bead has a GH mirror.

Usage: check-pr-closes.py <jsonl_path> <body_file> <branch>

The body arrives as a file rather than as the workflow event payload
(bd gqlc-w4al). A payload body is frozen at the moment the event fired, so
re-running the run that opened a PR re-asserts the body the PR had then --
which makes 'pass at open, then edit the line out, then re-run the old run'
a green path to merge. The caller fetches the live body from the API into
this file; what the checker reads is therefore the PR's present state.

Every input this checker cannot read is a failure, not a skip: an
unreadable body file and an unreadable export both fail. Silence here would
be indistinguishable from 'no Closes needed', which is the whole defect
class the gate exists to close.

A PR that touches a bead without resolving it declares that with a
'Refs: <bead-id> #<issue>' line (bd gqlc-1ekq). The declaration is checked
rather than taken: the number has to be the one the bead mirrors, the
export has to not already show the bead closed, and the body has to carry
no closing keyword for that number. An honoured declaration prints a
::warning:: annotation, which GitHub attaches to the check run itself.

Exits 0 (pass) or 1 (fail with diagnostic).
"""
import json
import re
import sys

# 'Bead: gqlc-xyz' anywhere in the body. The value is the whole whitespace-
# delimited token around the 'gqlc-', not just the part that looks like an
# id: a token that carries backticks or a trailing full stop is a
# declaration too, and one that has to be named rather than left to match no
# bead. BEAD_ID below is what separates the two. A token with no 'gqlc-' in
# it is not a declaration at all, so 'Bead: none' still matches nothing.
BEAD_IN_BODY = re.compile(r"(?i)Bead:\s*(\S*gqlc-\S*)")
# The opt-out marker. Anchored to the start of a line, unlike BEAD_IN_BODY:
# this pattern's verdict is a pass, so a body that quotes the marker inside a
# sentence must not reach it. group(2) is the rest of the line, which is
# where the issue number has to be.
REFS_IN_BODY = re.compile(r"(?im)^[ \t]*Refs:[ \t]*(\S*gqlc-\S*)([^\n]*)")
# A branch name carries the id with no marker, so the alphabet has to be
# spelled out: '\S+' would swallow the rest of 'fix/gqlc-w4al-body-edits'.
BEAD_IN_BRANCH = re.compile(r"(?i)(gqlc-[a-z0-9.]+)")
# The shape every one of the 310 ids in .beads/issues.jsonl has, sub-beads
# ('gqlc-h9n.22') included. Applied only to ids the body declares: a branch
# name is incidental, but a declaration the export cannot match is a gate
# with nothing left to demand.
BEAD_ID = re.compile(r"gqlc-[a-z0-9]+(?:\.[0-9]+)*")
CLOSES = re.compile(r"(?i)(?:closes|fixes|resolves)\s+#(\d+)")
ISSUE_N = re.compile(r"/issues/(\d+)$")
HASH_N = re.compile(r"#(\d+)")


def refuse(headline, *detail):
    """Print a refusal and exit non-zero. Detail lines are indented under the
    headline so a CI log shows one message rather than several."""
    print(f"ERROR: {headline}")
    for line in detail:
        print(f"       {line}")
    sys.exit(1)


def read_body(path):
    """The live PR body. An unreadable file fails the gate rather than
    yielding '' -- an empty body reads as 'no Closes present', so a fetch
    that silently produced nothing would pass every PR."""
    try:
        with open(path, encoding="utf-8") as f:
            return f.read()
    except OSError as e:
        refuse(
            f"cannot read the PR body from {path!r}: {e}",
            "The body is fetched from the API before this runs; a",
            "missing file means that fetch did not land, and this",
            "gate refuses to pass on a body it never saw.",
        )


def load_bead(jsonl_path, bead_id):
    """The bead's export record, or None when the export does not carry it.
    A malformed line is skipped rather than fatal -- one bad line must not
    hide the bead being looked for -- but an export that cannot be opened at
    all fails, because 'no export' would otherwise pass every PR."""
    try:
        f = open(jsonl_path, encoding="utf-8")
    except OSError as e:
        refuse(
            f"cannot read the bd export at {jsonl_path!r}: {e}",
            "Without it no PR can be checked, so this fails rather",
            "than passing every PR unexamined.",
        )
    with f:
        for line in f:
            line = line.strip()
            if not line:
                continue
            try:
                d = json.loads(line)
            except ValueError:
                continue
            if d.get("id") == bead_id:
                return d
    return None


def declared_bead(pr_body, branch):
    """Which bead the PR is about, and whether it declares it unresolved.

    Returns (bead_id, from_body, refs_match). A 'Bead:' line is the
    resolving declaration and outranks everything; a 'Refs:' line names a
    bead the PR touches and leaves open; the branch name is the fallback.
    """
    named = BEAD_IN_BODY.search(pr_body)
    refs = REFS_IN_BODY.search(pr_body)

    for label, m in (("Bead:", named), ("Refs:", refs)):
        if m and not BEAD_ID.fullmatch(m.group(1)):
            refuse(
                f"the PR body's '{label}' line declares {m.group(1)!r}, which "
                "is not a bead id.",
                "Ids look like 'gqlc-w4al' or 'gqlc-h9n.22'. Backticks around",
                "the id and a trailing full stop both land here, and an id no",
                "bead has leaves this gate with nothing to demand.",
            )

    if named and refs and named.group(1).lower() == refs.group(1).lower():
        refuse(
            f"the PR body puts {named.group(1)} on a 'Bead:' line and on a "
            "'Refs:' line.",
            "'Bead:' is the bead this PR resolves, 'Refs:' one it leaves open,",
            "so the body asserts both. Keep whichever is true.",
        )

    if named:
        return named.group(1), True, None
    if refs:
        return refs.group(1), True, refs
    in_branch = BEAD_IN_BRANCH.search(branch)
    return (in_branch.group(1) if in_branch else ""), False, None


def opt_out_number(refs, bead_id):
    """The issue number the 'Refs:' line declines to close."""
    hit = HASH_N.search(refs.group(2))
    if not hit:
        refuse(
            f"the 'Refs: {bead_id}' line names no issue number.",
            "An opt-out has to say which GitHub issue it is leaving open:",
            f"    Refs: {bead_id} #<issue>",
            "The number is held against the bead's own mirror, so it cannot",
            "be copied from another PR.",
        )
    return hit.group(1)


def check_opt_out(pr_body, bead, bead_id, marker_n, expected_n):
    """Everything an opt-out has to survive before it is honoured."""
    if marker_n != expected_n:
        refuse(
            f"the 'Refs: {bead_id}' line names #{marker_n}, but {bead_id} "
            f"mirrors #{expected_n}.",
            f"Write 'Refs: {bead_id} #{expected_n}'.",
        )

    status = bead.get("status") or "unknown"
    if status == "closed":
        refuse(
            f"the PR body leaves {bead_id} open, but the export at this "
            "commit closes it.",
            f".beads/issues.jsonl has {bead_id} at status {status!r}. A PR "
            "that",
            f"closes the bead resolves it, so #{expected_n} has to close with",
            f"it: use 'Closes #{expected_n}'.",
        )

    if expected_n in CLOSES.findall(pr_body):
        refuse(
            f"the PR body leaves {bead_id} open and also carries a closing "
            f"keyword for #{expected_n}.",
            "GitHub acts on the keyword at merge whatever this check says, so",
            f"#{expected_n} would not stay open. Drop one of the two.",
        )


def main():
    if len(sys.argv) < 4:
        print("Usage: check-pr-closes.py <jsonl_path> <body_file> <branch>")
        sys.exit(1)

    jsonl_path, body_file, branch = sys.argv[1], sys.argv[2], sys.argv[3]
    pr_body = read_body(body_file)

    bead_id, from_body, refs = declared_bead(pr_body, branch)
    if not bead_id:
        sys.exit(0)  # No bead on this PR -> pass

    if refs is not None:
        in_branch = BEAD_IN_BRANCH.search(branch)
        if in_branch and in_branch.group(1).lower() != bead_id.lower():
            refuse(
                f"the branch is named after {in_branch.group(1)}, but the "
                f"'Refs:' line declares {bead_id}.",
                "An opt-out names the bead the PR is about; this one would",
                f"leave {in_branch.group(1)} unexamined while excusing another",
                "bead.",
            )
        marker_n = opt_out_number(refs, bead_id)

    bead = load_bead(jsonl_path, bead_id)
    if bead is None:
        # Unknown bead - PASS (don't block on stale export). Reached only by
        # a well-formed id: the export trails the ledger by whole sessions
        # here, so a bead created today is legitimately absent from it.
        print(f"[check-pr-closes] bead {bead_id!r} not in export - skipping")
        sys.exit(0)

    ext = bead.get("external_ref") or ""
    if not ext:
        sys.exit(0)  # No GH mirror -> pass

    if bead.get("issue_type") == "epic":
        print(
            f"[check-pr-closes] {bead_id} is an epic - skipping "
            "(umbrella must not be closed)"
        )
        sys.exit(0)

    m = ISSUE_N.search(ext)
    if not m:
        sys.exit(0)  # Can't parse -> pass
    expected_n = m.group(1)

    if refs is not None:
        check_opt_out(pr_body, bead, bead_id, marker_n, expected_n)
        print(
            f"::warning title=check-pr-closes opt-out::PR declares "
            f"'Refs: {bead_id} #{expected_n}', so issue #{expected_n} stays "
            f"open at merge. Bead status in the export: "
            f"{bead.get('status') or 'unknown'}."
        )
        print(
            f"[check-pr-closes] {bead_id} -> Refs #{expected_n}, "
            "no Closes demanded"
        )
        sys.exit(0)

    # Scan PR body for Closes/Fixes/Resolves #M
    # Case-insensitive; match the keyword, optional whitespace, #number
    found = CLOSES.findall(pr_body)

    if not found:
        refuse(
            f"PR body is missing 'Closes #{expected_n}'",
            f"Bead {bead_id} maps to GitHub issue #{expected_n}.",
            f"Add 'Closes #{expected_n}' to the PR body so the issue closes "
            "on merge.",
            "Editing the body re-runs this check on its own; you do",
            "not need to push a commit or reopen the PR.",
            f"If this PR does not resolve {bead_id}, declare that instead "
            "with a",
            f"line reading 'Refs: {bead_id} #{expected_n}'. That pass is",
            "reported as a warning annotation on this check.",
        )

    if expected_n not in found:
        wrong = [n for n in found if n != expected_n]
        refuse(
            f"PR body closes #{', #'.join(wrong)} but bead {bead_id} "
            f"maps to #{expected_n}.",
            f"Replace 'Closes #{wrong[0]}' with 'Closes #{expected_n}'.",
            "Closing the wrong issue is worse than closing none.",
        )

    # Correct number present
    print(f"[check-pr-closes] {bead_id} -> Closes #{expected_n} (ok)")
    sys.exit(0)


if __name__ == "__main__":
    main()
