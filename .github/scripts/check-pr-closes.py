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

Exits 0 (pass) or 1 (fail with diagnostic).
"""
import json
import re
import sys

# 'Bead: gqlc-xyz' anywhere in the body. \S+ rather than a bead-id character
# class because that is what the shell grep this replaced matched, and a
# narrower class would newly reject ids whose alphabet grows.
BEAD_IN_BODY = re.compile(r"(?i)Bead:\s*(gqlc-\S+)")
# A branch name carries the id with no marker, so the alphabet has to be
# spelled out: '\S+' would swallow the rest of 'fix/gqlc-w4al-body-edits'.
BEAD_IN_BRANCH = re.compile(r"(?i)(gqlc-[a-z0-9.]+)")
CLOSES = re.compile(r"(?i)(?:closes|fixes|resolves)\s+#(\d+)")
ISSUE_N = re.compile(r"/issues/(\d+)$")


def read_body(path):
    """The live PR body. An unreadable file fails the gate rather than
    yielding '' -- an empty body reads as 'no Closes present', so a fetch
    that silently produced nothing would pass every PR."""
    try:
        with open(path, encoding="utf-8") as f:
            return f.read()
    except OSError as e:
        print(f"ERROR: cannot read the PR body from {path!r}: {e}")
        print("       The body is fetched from the API before this runs; a")
        print("       missing file means that fetch did not land, and this")
        print("       gate refuses to pass on a body it never saw.")
        sys.exit(1)


def load_bead(jsonl_path, bead_id):
    """The bead's export record, or None when the export does not carry it.
    A malformed line is skipped rather than fatal -- one bad line must not
    hide the bead being looked for -- but an export that cannot be opened at
    all fails, because 'no export' would otherwise pass every PR."""
    try:
        f = open(jsonl_path, encoding="utf-8")
    except OSError as e:
        print(f"ERROR: cannot read the bd export at {jsonl_path!r}: {e}")
        print("       Without it no PR can be checked, so this fails rather")
        print("       than passing every PR unexamined.")
        sys.exit(1)
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


def main():
    if len(sys.argv) < 4:
        print("Usage: check-pr-closes.py <jsonl_path> <body_file> <branch>")
        sys.exit(1)

    jsonl_path, body_file, branch = sys.argv[1], sys.argv[2], sys.argv[3]
    pr_body = read_body(body_file)

    m = BEAD_IN_BODY.search(pr_body) or BEAD_IN_BRANCH.search(branch)
    bead_id = m.group(1) if m else ""

    if not bead_id:
        sys.exit(0)  # No bead on this PR -> pass

    bead = load_bead(jsonl_path, bead_id)
    if bead is None:
        # Unknown bead - PASS (don't block on stale export)
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

    # Scan PR body for Closes/Fixes/Resolves #M
    # Case-insensitive; match the keyword, optional whitespace, #number
    found = CLOSES.findall(pr_body)

    if not found:
        print(f"ERROR: PR body is missing 'Closes #{expected_n}'")
        print(f"       Bead {bead_id} maps to GitHub issue #{expected_n}.")
        print(
            f"       Add 'Closes #{expected_n}' to the PR body so the issue "
            "closes on merge."
        )
        print("       Editing the body re-runs this check on its own; you do")
        print("       not need to push a commit or reopen the PR.")
        sys.exit(1)

    if expected_n not in found:
        wrong = [n for n in found if n != expected_n]
        print(
            f"ERROR: PR body closes #{', #'.join(wrong)} but bead {bead_id} "
            f"maps to #{expected_n}."
        )
        print(f"       Replace 'Closes #{wrong[0]}' with 'Closes #{expected_n}'.")
        print("       Closing the wrong issue is worse than closing none.")
        sys.exit(1)

    # Correct number present
    print(f"[check-pr-closes] {bead_id} -> Closes #{expected_n} (ok)")
    sys.exit(0)


if __name__ == "__main__":
    main()
