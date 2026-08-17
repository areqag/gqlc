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
'Refs: <bead-id> #<issue>' line (bd gqlc-1ekq), starting at the line's
first character and read over what prose_only leaves of the body --
fenced code blocks, HTML comments and <pre> blocks blanked. Those three
are the carriers a marker was measured to survive in, against GitHub's
own renderer; they are not certified to be every place GitHub hides
text. Where the two disagree over the 58 shapes measured, this blanks a
marker GitHub renders (three of them, rowed in the suite) rather than
honouring one GitHub hides (none). The declaration is then checked
rather than taken: the number has to be the one the bead mirrors, the
export has to not already show the bead closed, and the body has to carry
none of the closing keywords and reference forms GitHub documents for that
number -- see GH_CLOSES, which is deliberately wider than the CLOSES line
this gate demands elsewhere. An honoured declaration prints a ::warning::
annotation, which GitHub attaches to the check run itself.

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
# Unanchored, and read over the raw body rather than prose_only's, so prose,
# code blocks and HTML comments all reach it; '\s*' spans newlines. Three
# consequences, all rowed below: a sentence that writes an id after the word
# 'bead:' is read as a declaration; so is one hidden where GitHub renders
# nothing; and one naming a well-formed id the export does not carry takes
# the skip and leaves this gate demanding nothing on any branch (bd
# gqlc-oh30). Unlike the marker, none of the three is a pass this gate can
# grant on its own: a 'Bead:' line the export can match ends in the demand
# for 'Closes #N'. The exception is that skip, which is what oh30 records.
BEAD_IN_BODY = re.compile(r"(?i)Bead:\s*(\S*gqlc-\S*)")
# The opt-out marker. Read at the first character of a line, unlike
# BEAD_IN_BODY, and only over what prose_only() leaves: an honoured marker
# makes this gate state on the check run that an issue stays open, so a body
# that merely shows the spelling, or hides it where GitHub renders nothing,
# must not reach it. Leading whitespace is rejected because four spaces is
# markdown's indented-code-block spelling and this file's own subject matter
# is the marker. group(2) is the rest of the line, which is where the issue
# number has to be.
REFS_IN_BODY = re.compile(r"(?im)^Refs:[ \t]*(\S*gqlc-\S*)([^\n]*)")
# A code fence: up to three spaces of indentation, then a run of three or
# more backticks or tildes, then the info string. Four spaces is markdown's
# indented-code spelling rather than a fence, and a tab indents by four, so
# neither opens one -- both measured against GitHub's renderer, see the
# suite's fence section. group(1) is the run, group(2) the info string; which
# of the two a line is depends on the state prose_only() is in.
FENCE = re.compile(r"^ {0,3}(`{3,}|~{3,})([^\n]*)$")
# A raw <pre> block, which GitHub renders as code. <script>, <style> and
# <textarea> are the tags markdown groups with <pre>, and are deliberately
# not blanked: GitHub's sanitiser drops the tag and keeps the text, so a
# marker inside one is visible to a reader (measured, rowed in the suite).
PRE_OPEN = re.compile(r"^ {0,3}<pre(?:[\s>]|$)", re.I)
PRE_CLOSE = re.compile(r"</pre", re.I)
# A branch name carries the id with no marker, so the alphabet has to be
# spelled out: '\S+' would swallow the rest of 'fix/gqlc-w4al-body-edits'.
BEAD_IN_BRANCH = re.compile(r"(?i)(gqlc-[a-z0-9.]+)")
# The shape a bead id has, sub-beads ('gqlc-h9n.22') included. Measured over
# .beads/issues.jsonl at master 0c214d20 (2026-08-17): 438 ids, every one
# matched, 86 of them sub-beads. The export grows every session and CI reads
# the merge commit's copy, so that is a count on one day and not a property
# of the file; the number is here to say what the sample was, not what the
# repo has. Applied only to ids the body declares: a branch name is
# incidental, but a declaration the export cannot match is a gate with
# nothing left to demand.
BEAD_ID = re.compile(r"gqlc-[a-z0-9]+(?:\.[0-9]+)*")
CLOSES = re.compile(r"(?i)(?:closes|fixes|resolves)\s+#(\d+)")
# What GitHub might act on, which is wider than what CLOSES demands. The nine
# keywords its docs list (close/closes/closed, fix/fixes/fixed,
# resolve/resolves/resolved), an optional colon after the keyword, and the
# reference forms it autolinks: '#N', 'GH-N', 'OWNER/REPO#N' and a full issue
# URL. Asked only as "would GitHub close this number at merge", never as
# "did the author write the line this gate demanded". They stay separate
# patterns because the two questions fail in opposite directions: a hit here
# refuses, a miss in CLOSES refuses. So widening this one cannot newly pass a
# PR, whereas teaching CLOSES the same list would newly pass PRs refused
# today ('Fixed #617' would satisfy the demand for 'Closes #617'), which is a
# loosening and not this bead's. For the same reason the cross-repo and GH-N
# spellings are matched without checking which repository they name: a
# spelling matched here that GitHub would ignore costs a refusal the author
# can resolve, and one missed costs this gate affirming that an issue stays
# open when it does not. Read over the raw body, not prose_only's, which is
# the marker's rule inverted: a 'Closes #N' quoted inside a fence refuses an
# opt-out although GitHub will not act on it. Same direction as the rest of
# this pattern, and rowed as such.
GH_CLOSES = re.compile(
    r"(?i)\b(?:close[sd]?|fix(?:es|ed)?|resolve[sd]?)\b:?\s*"
    r"(?:https?://github\.com/[\w.-]+/[\w.-]+/issues/"
    r"|(?:[\w.-]+/[\w.-]+)?#"
    r"|GH-)(\d+)"
)
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


def comment_opens_at(line):
    """Where an HTML comment opens on this line without closing again, or
    None. Read anywhere on the line, not only at its first character: a
    comment opened mid-line runs on to the next line too (measured)."""
    i = 0
    while True:
        start = line.find("<!--", i)
        if start < 0:
            return None
        end = line.find("-->", start + 4)
        if end < 0:
            return start
        i = end + 3


def prose_only(pr_body):
    """The body with everything GitHub does not render as prose blanked out,
    line count preserved: fenced code blocks, HTML comments and <pre> blocks.

    Fences follow the rule GitHub's renderer follows rather than a toggle on
    every ``` and ~~~ line. A fence closes only on a run of the same
    character, at least as long as the one that opened it, with nothing but
    whitespace after it; a backtick fence whose info string carries a
    backtick opens nothing. Put the toggle back in place of this function
    and the suite fails 14 rows -- 12 bodies whose marker it honours though
    GitHub renders it inside <pre><code> or not at all, and 2 it blanks that
    GitHub renders as prose. Among the 12 is the ordinary idiom for showing
    a fence, which is to nest it in a longer one; showing this marker is
    what this file is about, so that is the realistic body rather than the
    exotic one. An unclosed fence, comment or <pre> swallows the rest.

    Not a markdown parser, and where it diverges it blanks rather than
    keeps: a fence indented one to three spaces into a list item is blanked
    here although GitHub renders a column-zero line below it as prose. Over-
    blanking costs a refusal the author resolves by moving the line out from
    under the block; under-blanking is this gate annotating a check run to
    say an issue stays open, over a body in which no reader can see it said.
    """
    out = []
    state = None  # None | ("fence", char, run length) | "comment" | "pre"
    for line in pr_body.split("\n"):
        if state is None:
            m = FENCE.match(line)
            if m and not (m.group(1)[0] == "`" and "`" in m.group(2)):
                state = ("fence", m.group(1)[0], len(m.group(1)))
                out.append("")
                continue
            cut = comment_opens_at(line)
            if cut is not None:
                state = "comment"
                out.append(line[:cut])
                continue
            if PRE_OPEN.match(line):
                # '<pre>x</pre>' on one line closes on that line, so the
                # lines below it are prose again.
                state = None if PRE_CLOSE.search(line) else "pre"
                out.append("")
                continue
            out.append(line)
            continue

        out.append("")
        if state == "comment":
            # The whole closing line goes, including anything after the
            # '-->': that line is part of the comment's block, and a marker
            # cannot be at its first character anyway.
            if "-->" in line:
                state = None
        elif state == "pre":
            if PRE_CLOSE.search(line):
                state = None
        else:
            m = FENCE.match(line)
            if (
                m
                and m.group(1)[0] == state[1]
                and len(m.group(1)) >= state[2]
                and not m.group(2).strip()
            ):
                state = None
    return "\n".join(out)


def declared_bead(pr_body, branch):
    """Which bead the PR is about, and whether it declares it unresolved.

    Returns (bead_id, from_body, refs_match). A 'Bead:' line is the
    resolving declaration and outranks everything; a 'Refs:' line names a
    bead the PR touches and leaves open; the branch name is the fallback.
    """
    named = BEAD_IN_BODY.search(pr_body)
    refs = REFS_IN_BODY.search(prose_only(pr_body))

    for label, m in (("Bead:", named), ("Refs:", refs)):
        if m and not BEAD_ID.fullmatch(m.group(1)):
            refuse(
                f"the PR body's '{label}' declaration reads {m.group(1)!r}, "
                "which is not a bead id.",
                "Ids look like 'gqlc-w4al' or 'gqlc-h9n.22'. Backticks around",
                "the id and a trailing full stop both land here, and an id no",
                "bead has leaves this gate with nothing to demand.",
                "'Bead:' is read anywhere in the body, so a sentence that",
                "writes an id after that word lands here too.",
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
            "An opt-out has to say which GitHub issue it is leaving open, on",
            f"a line whose first character is the 'R' of 'Refs: {bead_id} "
            "#<issue>'.",
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

    if expected_n in GH_CLOSES.findall(pr_body):
        refuse(
            f"the PR body leaves {bead_id} open and also carries a closing "
            f"keyword for #{expected_n}.",
            "A closing keyword is GitHub's to act on at merge, not this",
            "check's, so the body would be asserting both that "
            f"#{expected_n}",
            "stays open and that it closes. Drop one of the two.",
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
        if refs is not None:
            # A marker on a bead with no mirror was the one exit that read a
            # declaration, held its number against nothing, and said so
            # nowhere: rc=0 and no output at all, which is what a gate that
            # never ran also looks like. The pass is right -- there is no
            # issue to leave open -- so what changes is only that it is
            # legible.
            print(
                f"[check-pr-closes] {bead_id} has no GitHub mirror, so the "
                f"'Refs: {bead_id} #{marker_n}' line holds nothing"
            )
        sys.exit(0)  # No GH mirror -> pass

    if bead.get("issue_type") == "epic":
        print(
            f"[check-pr-closes] {bead_id} is an epic - skipping "
            "(umbrella must not be closed)"
        )
        sys.exit(0)

    m = ISSUE_N.search(ext)
    if not m:
        if refs is not None:
            print(
                f"[check-pr-closes] {bead_id} mirrors {ext!r}, which names no "
                f"issue number, so the 'Refs: {bead_id} #{marker_n}' line "
                "holds nothing"
            )
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
            f"line reading 'Refs: {bead_id} #{expected_n}' and starting at "
            "that",
            "line's first character. That pass is reported as a warning",
            "annotation on this check.",
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
