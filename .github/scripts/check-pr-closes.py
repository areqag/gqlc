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

A body that carries one of GitHub's closing keywords and names no bead this
checker can resolve fails for the same reason (bd gqlc-mk7v): the claim is
made, GitHub acts on it at merge, and there is nothing left here to hold the
number against. A body that closes nothing passes, because a PR making no
claim has none to check.

Every exit prints. A pass that says nothing is one no reader can tell from
this gate not having run (bd gqlc-mk7v, bd gqlc-63ao). The suite holds that
as a property rather than as a habit: its green helper requires output as
well as a zero status.

This file is approximating another system's markdown parser, so the useful
question about any disagreement with GitHub is which direction it errs in --
and the answer depends on which question is being asked, because two are.

  "Can a reader see this opt-out marker?"  Honouring one nobody can see is
  the expensive answer: the gate annotates the check run to say an issue
  stays open, over a body in which no reader can find it said. Blanking too
  much costs a refusal the author clears by moving one line. So this errs
  towards blanking. visible_prose().

  "Does this body close an issue?"  Missing a keyword GitHub acts on is the
  expensive answer: the PR merges, the issue closes, and no bead held the
  number against it. Blanking too much loses the claim. So this errs towards
  keeping. claimable_prose().

One function cannot err in both directions, and until this commit one did.
That collision is what bd gqlc-ncb8, bd gqlc-xz16 and bd gqlc-tysj each are:
the first two are shapes GitHub hides that were honoured because the cheap
fix over-blanks, which is unsafe for the second question; the third is an
inline code span, which has to be kept for the first question and dropped
for the second. Split, each is answerable in its own direction, and all
three are closed. The shared line-oriented core is prose_only().

A PR that touches a bead without resolving it declares that with a
'Refs: <bead-id> #<issue>' line (bd gqlc-1ekq), starting at the line's
first character and read over what visible_prose leaves of the body --
fenced code blocks, HTML comments a line leaves open, raw <pre> and <code>
blocks wherever their opening tag falls on the line, and the lines inside an
unterminated HTML attribute value, blanked.
Those are the carriers a marker was measured to survive invisibly in,
against GitHub's own renderer; they are not certified to be
every place GitHub hides text.

Which carriers they are came out of a sweep of 85 body shapes put through
POST /markdown. That sweep ran against an earlier prose_only, kept none of
its bodies, and cannot be re-run from anything in this tree, so no result
of it is stated here or below: not how many of the 85 disagreed with the
checker, and not which way any one of them did. It is where the design
came from, not a bound on this commit. What is measured at this commit is
the suite's visibility section, where every body was put to the same
renderer and the row's colour reports what came back.

At this commit every disagreement rowed in that section is the cheap
direction -- this blanking refusing a marker GitHub renders, which the
author resolves by moving the line. The three that went the other way are
the three beads above, and each is now a red row naming what closed it.
That is a statement about the bodies in the suite and not a census: a shape
nobody has put to the renderer is rowed nowhere.
prose_only's docstring enumerates the shapes and each is a row, and the
suite's visibility section counts them again in its own words. Nothing in
this repository checks the two against each other -- so changing the set
means finding every sentence in both whose number depends on it. The
declaration is then checked
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
from typing import NoReturn

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
# BEAD_IN_BODY, and only over what visible_prose() leaves: an honoured marker
# makes this gate state on the check run that an issue stays open, so the
# carriers that were measured to hide the spelling where GitHub renders
# nothing are blanked before this pattern runs. Blanked, not proved absent --
# what the suite establishes is that every body measured in it that GitHub
# hides the marker in is refused, not that no such shape remains.
# Leading whitespace is rejected because four spaces is
# markdown's indented-code-block spelling and this file's own subject matter
# is the marker. group(2) is the rest of the line, which is where the issue
# number has to be.
REFS_IN_BODY = re.compile(r"(?im)^Refs:[ \t]*(\S*gqlc-\S*)([^\n]*)")
# The avowal for a closing keyword this PR's bead does not mirror. Line
# anchored over visible_prose()'s body, on REFS_IN_BODY's terms and for its
# reasons: honouring a marker a reader cannot find is the expensive way to be
# wrong about one, and here it is expensive in this file's own currency,
# because what an unfindable avowal buys is a closing keyword reaching merge
# unheld. It buys nothing on its own: the demand for the bead's own number
# runs before extra_closes() is ever called, so a body whose only closing
# line is avowed still fails that demand. All it does is convert an extra
# from an accident into an assertion, which is the same trade the 'Refs:'
# marker makes for the number it declines to close.
ALSO_CLOSES = re.compile(r"(?im)^Also-closes:[ \t]*([^\n]*)")
# A code fence: up to three spaces of indentation, then a run of three or
# more backticks or tildes, then the info string. Four spaces is markdown's
# indented-code spelling rather than a fence, and a tab indents by four, so
# neither opens one -- both measured against GitHub's renderer, see the
# suite's fence section. group(1) is the run, group(2) the info string; which
# of the two a line is depends on the state prose_only() is in.
FENCE = re.compile(r"^ {0,3}(`{3,}|~{3,})([^\n]*)$")
# A raw <pre> or <code> block, which GitHub renders as code. <script>,
# <style> and <textarea> are the other tags markdown groups with <pre>, and
# are deliberately not blanked: GitHub's sanitiser escapes the tag rather
# than honouring it, so '&lt;script&gt;' and the text below it both come back
# as prose a reader sees. Measured through POST /markdown for all three tags;
# the <script> one is rowed in the suite. group(1) is the tag, so the closer
# has to be that same tag. The '^ {0,3}' is markdown's bound on where an HTML
# block starts -- at four the line is an indented code block instead -- and
# the trailing '[\s>]|$' is what keeps '<pretend>' from reading as a <pre>.
# Both bounds are rows.
HTML_OPEN = re.compile(r"^ {0,3}<(pre|code)(?:[\s>]|$)", re.I)
# The same tag read anywhere on the line, which is where GitHub's sanitiser
# reads it: markdown starts an HTML *block* only at the line's start, but an
# inline '<pre>' part-way along a paragraph still opens the element in the
# assembled output and the lines below it land inside (bd gqlc-xz16). Used
# only by visible_prose(), never by claimable_prose(): a body that writes
# '<pre>' in a sentence -- the bodies on this file's own PRs do -- has every
# line below that sentence blanked, which costs a refusal on the visibility
# question and would cost a lost claim on the other one.
HTML_OPEN_ANY = re.compile(r"<(pre|code)(?:[\s>/]|$)", re.I)
# markdown's indented code block, which is what a four-space '<pre>' is:
# GitHub renders the tag as the block's text and the line below it as prose,
# measured. HTML_OPEN's '^ {0,3}' already excludes it; HTML_OPEN_ANY does not,
# so visible_prose() has to.
INDENTED_CODE = re.compile(r"^(?: {4}|\t)")
# An inline code span: a run of backticks, the shortest text reaching a run of
# the same length, and no blank line in between -- a code span is inline
# syntax and cannot cross from one block to the next, so an unpaired backtick
# in one paragraph and another three paragraphs down span nothing. Without
# that bound the blanking would be the fail-open direction on the question it
# is used for: a stray backtick would swallow a real closing keyword.
#
# Read only by claimable_prose(), and the asymmetry is the point. GitHub does
# not act on a closing keyword inside a span -- measured on PR #901, whose
# body carries 'Closes #617' and 'Closes: #617' in spans and nothing else
# naming 617, and whose closingIssuesReferences lists 862 and 883 only, all
# three issues being closed so that is not the reason (bd gqlc-tysj). But it
# does *render* one, as visible monospace, so the opt-out marker inside a span
# is a marker a reader sees and visible_prose() leaves it standing.
CODE_SPAN = re.compile(r"(?<!`)(`+)(?!`)(?:(?!\n[ \t]*\n).)+?(?<!`)\1(?!`)", re.S)
# A complete comment on one line. Blanked out before a closing tag is looked
# for, because a closing tag inside a comment does not end the block a reader
# sees: markdown's line scanner does stop the HTML block on the line that
# spells '</pre>', but the sanitiser then drops the comment, which leaves the
# element open, and the marker below it lands inside it. Measured, both
# spellings rowed: '<pre><!-- </pre> -->' with a marker under it, and '<pre>'
# followed by '<!-- </pre> -->', each render the marker inside the <pre>.
# Non-greedy so two comments on a line are two runs. Never applied to text
# holding an unterminated '<!--' -- both call sites have already truncated
# there or established there is none -- so it cannot reach across lines.
COMMENT_RUN = re.compile(r"<!--.*?-->")
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
# open when it does not.
#
# Two call sites read it, and they read different bodies. check_opt_out()
# reads the raw one, which is the marker's rule inverted: a 'Closes #N'
# quoted inside a fence refuses an opt-out although GitHub will not act on
# it. Same direction as the rest of this pattern, and rowed as such. main()'s
# no-bead check reads claimable_prose() instead -- there no opt-out is being
# honoured, so the refusal buys nothing against a body that only quotes the
# spelling, and a body on this file is where such a quote turns up: PR #901's
# carries closing-keyword matches inside carriers claimable_prose() blanks,
# measured at this commit against its live body. Its inline spans naming
# #617 are among them, and #617 is absent from its closingIssuesReferences
# while #862 and #883, written as ordinary lines, are listed -- all three
# issues closed, so that is not the reason (bd gqlc-tysj). Both sites refuse
# on a
# hit and fall through to a pass on a miss, so a spelling added to this
# pattern can turn a pass into a refusal at either and never the reverse;
# that is the asymmetry, and it is the pattern's, not one call site's.
GH_CLOSES = re.compile(
    r"(?i)\b(?:close[sd]?|fix(?:es|ed)?|resolve[sd]?)\b:?\s*"
    r"(?:https?://github\.com/[\w.-]+/[\w.-]+/issues/"
    r"|(?:[\w.-]+/[\w.-]+)?#"
    r"|GH-)(\d+)"
)
ISSUE_N = re.compile(r"/issues/(\d+)$")
HASH_N = re.compile(r"#(\d+)")


def refuse(headline, *detail) -> NoReturn:
    """Print a refusal and exit non-zero. Detail lines are indented under the
    headline so a CI log shows one message rather than several.

    Annotated NoReturn because callers rely on it: several 'refuse(...)'
    calls below are followed by code that would be reading an unbound name
    or a None if control came back. An inline sys.exit carries that for a
    type checker, a call behind it does not. Take the annotation off and
    that is what a checker reports: load_bead()'s 'f' possibly unbound,
    opt_out_number()'s 'hit' still an Optional where '.group' is called on
    it, and main()'s 'pr_body' a 'str | None' where findall() wants a str.
    Put it back and they go. No count is kept here, because it moves with
    the checker and the mode it runs in -- and pyright run from the
    repository root reports nothing at all on this file either way, its
    defaults excluding dot-directories; a copy outside .github/ is one run
    that does measure it."""
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


def comments_blanked(text):
    """`text` with every complete comment replaced by as many spaces.

    Length-preserving, and that is the point rather than tidiness. Deleting
    the comments instead joins whatever stood either side of one, so
    '<pre>x</p<!-- c -->re>' yields a '</pre>' the line never carried: the
    block closes here, the marker below it goes back into prose, and the gate
    annotates a check run to say an issue stays open. GitHub does the
    opposite -- its HTML-block scanner looks for a literal '</pre>' on the
    line and finds none, so the block runs on and the marker renders inside
    the <pre>. Measured against POST /markdown, both spellings rowed; it was
    live on this branch between 520b01c3 and this commit.

    Spaces cannot make that mistake. The callers look for '</pre' and
    '</code', neither of which holds a space, so a hit in the result lies
    wholly outside every blanked run and is a hit at the same offset in
    `text`.
    """
    return COMMENT_RUN.sub(lambda m: " " * len(m.group(0)), text)


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


def open_attr_quote(text):
    """The quote character an HTML attribute value is left open in at the end
    of `text`, or None.

    This is inline-HTML reading rather than the line model the rest of this
    function family is, and it is here for one shape: a marker on its own line
    inside an unterminated attribute value renders as nothing at all, because
    the lines below the open quote are part of the value (bd gqlc-ncb8).
    '<a href="', the marker, '">z</a>' comes back from GitHub as '<p>z</p>'.

    Two deliberate narrowings, both rowed. A value opens only at a quote whose
    preceding non-space character is '=', so an apostrophe in a sentence that
    happens to follow a '<' and a word -- "a <b isn't c" -- opens nothing; and
    a tag left unterminated with no quote open at all returns None, because
    the '<' plus a word is a far commoner thing to write in prose than an
    attribute list is to break across a line. Both are holes in the
    fail-closed direction and neither is a hole this repository has met.
    """
    i, n = 0, len(text)
    while i < n:
        if text[i] != "<":
            i += 1
            continue
        j = i + 1
        if j < n and text[j] == "/":
            j += 1
        if j >= n or not text[j].isalpha():
            i += 1
            continue
        while j < n and text[j] != ">":
            if text[j] in "\"'" and text[:j].rstrip().endswith("="):
                end = text.find(text[j], j + 1)
                if end < 0:
                    return text[j]
                j = end + 1
            else:
                j += 1
        if j >= n:
            return None
        i = j + 1
    return None


def blank_span(text):
    """`text` with every character but a newline replaced by a space.

    Length- and line-preserving for the same reason comments_blanked() is:
    what is blanked has to leave the offsets of everything around it alone.
    """
    return re.sub(r"[^\n]", " ", text)


def visible_prose(pr_body):
    """The body a reader of the PR can see, for the question "is this opt-out
    marker visible".

    Honouring a marker nobody can see is the expensive failure on that
    question: the gate annotates the check run to say an issue stays open,
    over a body in which no reader can find it said. Blanking too much costs
    a refusal the author clears by moving one line. So this errs towards
    blanking, and the three shapes bd gqlc-ncb8 and bd gqlc-xz16 were filed
    over are closed by erring that way rather than by modelling GitHub more
    closely.
    """
    return prose_only(pr_body, strict=True)


def claimable_prose(pr_body):
    """The body GitHub's autolinker acts on, for the question "does this body
    close an issue".

    The opposite direction from visible_prose(), and that is why they are two
    functions rather than one. Missing a closing keyword GitHub acts on is the
    expensive failure here -- the PR merges, the issue closes, and no bead
    held the number against it -- while keeping one GitHub ignores costs a
    refusal the author clears by rewording. So this blanks only carriers
    GitHub was measured not to act on, and none of visible_prose()'s
    pessimism: an inline '<pre>' in a sentence blanks the rest of the body
    there, and doing that here would drop the claim.
    """
    return CODE_SPAN.sub(lambda m: blank_span(m.group(0)), prose_only(pr_body))


def prose_only(pr_body, strict=False):
    """The body with its code and comment carriers blanked out, line count
    preserved. Do not call this directly: call visible_prose() or
    claimable_prose(), which are the two questions this file asks of a body
    and which err in opposite directions. This is their shared body, and
    'strict' is which of the two is asking.

    Blanked either way: fenced code blocks, HTML comments a line leaves
    open, and raw <pre> and <code> blocks whose opening tag starts a line,
    indented no more than three spaces -- which is markdown's own bound on
    where an HTML block may start, four spaces being an indented code block
    instead. Both ends of that bound are rows.

    Blanked only under 'strict', because each costs a lost claim on the
    other question and only a movable refusal on this one: a <pre> or <code>
    opening tag wherever it falls on the line rather than only at its start,
    the lines inside an unterminated HTML attribute value, and the rest of
    the body below a raw block that opens and closes on one line and then
    opens a comment. Not everything GitHub declines to render as prose --
    what was measured to diverge from it on the marker, in either direction,
    is the paragraphs from 'Not a markdown parser' down, and each shape they
    name is a row.

    Fences follow the rule GitHub's renderer follows rather than a toggle on
    every ``` and ~~~ line. A fence closes only on a run of the same
    character, at least as long as the one that opened it, with nothing but
    whitespace after it; a backtick fence whose info string carries a
    backtick opens nothing. Put the toggle back in place of this function --
    4446b7fc's outside_fences and the FENCE it read, both verbatim -- and
    the suite goes red in three directions: markers the toggle honours and
    this blanks, markers this honours that the toggle blanks and GitHub
    renders as prose, and, because main()'s no-bead check reads this
    function too, a closing keyword inside an HTML comment refused as a
    claim. The first of those says where the two functions differ and
    nothing about what a reader sees: some of its rows are markers the
    renderer puts in a code element or drops from the output, and some are
    divergences the 'Not a markdown parser' paragraph rows, where the
    renderer shows the marker and the toggle is the one agreeing with it.
    How many rows that is stays out of this docstring:
    it moved on this branch, and the row that moved it was added to the
    no-bead section rather than to the visibility one, so the number tracks
    the suite's size and not this function's behaviour. Naming the stand-in
    is what makes it re-countable instead.
    Among the first of the three is the ordinary idiom
    for showing a fence, which is to nest it in a longer one; showing this
    marker is what this file is about, so that is the realistic body rather
    than the exotic one. An unclosed fence, comment or HTML block swallows
    the rest.

    A comment complete on one line is left standing, which is a decision
    and not an oversight. What the comment state blanks is the span from a
    '<!--' its line does not close to the '-->' that does, because that is
    the span a marker can hide in: the lines under an unterminated comment
    are inside it. A one-line comment puts no line in that state, and a
    marker written after one on the same line is not at its line's first
    character, which is where the marker pattern anchors. What does turn on
    it is main()'s no-bead check, which reads this function: a body whose
    only closing keyword sits inside '<!-- Closes #N -->' is read as making
    the claim and refused for naming no bead, while the same keyword in a
    comment opened and closed on separate lines is blanked and passes.
    Levelling the two means blanking the one-line spelling as well, and
    that is the direction not taken here, because it turns a refusal into a
    pass on a premise this tree cannot establish: whether GitHub acts on a
    closing keyword inside a comment at merge is not measured anywhere in
    it, and no PR body in this repository carried the shape to measure it
    on when this was written. The two errors are not equal either. The
    refusal names both ways out and says an edit alone re-runs the check;
    the pass loses the claim, and loses it on this path and not on the
    other two, because here there is no bead to hold a number against. Of
    the three sites reading a closing keyword out of the body, the no-bead
    check and main()'s demand for 'Closes #N' both read claimable_prose()
    and so both see a one-line comment's keyword and neither sees one a
    line lower. check_opt_out() reads the raw body deliberately: it is
    asking a third question -- does this body contradict itself, opting
    #N out while also carrying a keyword for it -- and the safe direction
    there is to refuse, because the cost of refusing is an edit and the
    cost of passing is a body asserting both. Both comment spellings are
    rows in the suite's no-bead section.

    Not a markdown parser. Measured against GitHub's own renderer
    (POST /markdown, mode gfm), visible_prose() blanks six bodies the
    renderer does show the marker in: a fence and a <pre> each indented one
    to three spaces into a list item, a <code> opened against a paragraph it
    cannot interrupt, a <code> sharing its line with the comment that opens
    on it, a <code> whose only closing tag on its line is inside a comment
    comments_blanked() blanks, and a <code> opened mid-line with no blank
    line above it -- the last three are not a complete tag on a line of
    their own, so GitHub keeps them inline as visible monospace. (A seventh
    row in that direction, a marker sharing a block's closing line, is the
    marker pattern's line anchor rather than this function; it is rowed
    where it says so.) That is the cheap direction on this question: it
    costs a refusal the author resolves by moving the line out from under
    the block, where the other direction is this gate annotating a check run
    to say an issue stays open, over a body in which no reader can see it
    said. Every one of the seven is red, and each says so where it stands.

    At this commit no measured divergence goes the other way on the
    visibility question -- no body in that section renders the marker
    nowhere and is honoured anyway. Three did until this commit, and closing
    them is what bd gqlc-ncb8 and bd gqlc-xz16 were: a marker on its own
    line inside an unterminated HTML attribute ('<a href="', the line,
    '">z</a>'), which renders as nothing at all because attribute values are
    inline syntax; a raw block whose opening tag has text before it on its
    line ('x <pre>'), which opens the element for GitHub's sanitiser though
    it starts no markdown HTML block; and a block that closes on its own
    opening line and then opens a comment, where the '<!--' is emitted raw
    with nothing to close it and the sanitiser swallows the rest of the
    body. All three are red rows now, each naming what closed it. That is a
    statement about the rows in the suite, not a proof that no such shape
    remains.

    Two shapes reach the pattern over bodies GitHub does render, and are
    rowed green rather than blanked: an inline code span, which GitHub shows
    as visible monospace, and a <details> block, which GitHub collapses
    rather than hides. The code span is where the two questions collide --
    claimable_prose() blanks it and this does not, because a keyword a
    reader can see is not a keyword GitHub acts on; measured on PR #901,
    whose body carries 'Closes #617' only inside spans and whose
    closingIssuesReferences does not list 617. Every shape named in these
    paragraphs is a row in the suite's visibility section.
    """
    out = []
    # None | ("comment", enclosing) | ("fence", char, run length)
    #      | ("html", tag name)
    # A comment carries the state it interrupted. One opened inside a raw
    # <pre>/<code> block ends at its own '-->' and leaves that block open, so
    # a comment state that dropped the enclosing tuple exited the block early
    # and put the lines below the comment back into prose.
    state = None
    prev_blank = True
    for line in pr_body.split("\n"):
        was_blank, prev_blank = prev_blank, not line.strip()
        if state is None:
            m = FENCE.match(line)
            if m and not (m.group(1)[0] == "`" and "`" in m.group(2)):
                state = ("fence", m.group(1)[0], len(m.group(1)))
                out.append("")
                continue
            cut = comment_opens_at(line)
            # A raw block and a comment can open on one line: '<pre><!--'.
            # The opener is read over the part of the line before the comment
            # and the comment then carries the block, the same way one opened
            # a line lower does. Testing the comment first and stopping there
            # is what let '<pre><!--', '-->', a marker and '</pre>' put the
            # marker back in prose while GitHub still renders it inside the
            # <pre>; measured. Everything after the '<!--' is inside the
            # comment, the closing tag included, so the one-line-block test
            # reads the same part of the line the opener does.
            head = line if cut is None else line[:cut]
            if strict and not (was_blank and INDENTED_CODE.match(line)):
                m = HTML_OPEN_ANY.search(head)
            else:
                m = HTML_OPEN.match(head)
            # '<pre>x</pre>' on one line closes on that line, so the lines
            # below it are prose again -- but only a closing tag left standing
            # once the comments are blanked counts.
            opened = None
            if (
                m
                and f"</{m.group(1).lower()}"
                not in comments_blanked(head).lower()
            ):
                opened = ("html", m.group(1).lower())
            if cut is not None:
                if strict and m and opened is None:
                    # The block markdown opened on this line also ends on it,
                    # so the '<!--' after the closing tag is emitted raw with
                    # nothing to close it: the '-->' below is markdown text by
                    # then and comes back escaped, and the sanitiser's comment
                    # runs to the end of the body. Measured -- GitHub renders
                    # '<pre></pre><!--', '-->', a marker as an empty <pre> and
                    # nothing else (bd gqlc-xz16).
                    state = ("swallow",)
                    out.append(line[:cut])
                    continue
                state = ("comment", opened)
                out.append(line[:cut])
                continue
            if m:
                state = opened
                out.append("")
                continue
            if strict:
                quote = open_attr_quote(head)
                if quote is not None:
                    state = ("attr", quote)
                    out.append("")
                    continue
            out.append(line)
            continue

        out.append("")
        if state[0] == "swallow":
            continue
        if state[0] == "attr":
            # The value ends at its matching quote, and the line carrying that
            # quote is inside it -- so what is released is the line after.
            # Measured: '<a href="', 'x">z</a>', a marker renders the marker
            # as prose.
            if state[1] in line:
                state = None
        elif state[0] == "comment":
            # The whole closing line goes, including anything after the
            # '-->': the part up to it is inside the comment, and the part
            # after it is not at the line's first character, which the
            # marker pattern anchors to. A marker can start this line --
            # 'Refs: <id> #<n> -->' -- but it is inside the comment there
            # and GitHub hides it as well; measured. What resumes is
            # whatever the comment interrupted -- unless the rest of that
            # line carries the enclosing block's own closing tag, which
            # GitHub does close the block on (measured, rowed).
            end = line.find("-->")
            if end >= 0:
                enclosing = state[1]
                if (
                    enclosing is not None
                    and f"</{enclosing[1]}" in line[end + 3:].lower()
                ):
                    enclosing = None
                state = enclosing
        elif state[0] == "html":
            # A comment opened inside a raw HTML block is live, unlike one
            # inside a fence, where GitHub escapes it: the block is passed
            # through as HTML, so the sanitiser swallows from '<!--' to the
            # next '-->' -- past the closing tag, and to the end of the body
            # if it never comes. Measured: '<pre>', '<!--', '</pre>', a
            # blank line and a marker renders as an empty <pre> and nothing
            # else, so the comment is checked before the closing tag.
            cut = comment_opens_at(line)
            if cut is not None:
                if strict and f"</{state[1]}" in comments_blanked(
                    line[:cut]
                ).lower():
                    # The block ends before the comment opens, so this is the
                    # multi-line spelling of the shape above: markdown's HTML
                    # block is over and the '-->' below is escaped.
                    state = ("swallow",)
                else:
                    state = ("comment", state)
            elif f"</{state[1]}" in comments_blanked(line).lower():
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

    Returns (bead_id, refs_match). A 'Bead:' line is the resolving
    declaration and outranks everything; a 'Refs:' line names a bead the PR
    touches and leaves open; the branch name is the fallback.
    """
    named = BEAD_IN_BODY.search(pr_body)
    refs = REFS_IN_BODY.search(visible_prose(pr_body))

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
        return named.group(1), None
    if refs:
        return refs.group(1), refs
    in_branch = BEAD_IN_BRANCH.search(branch)
    return (in_branch.group(1) if in_branch else ""), None


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


def unverified_tail(refs, bead_id, marker_n):
    """How a pass that checked nothing ends, in the words of the declaration
    the PR made.

    A 'Refs:' line names the number the marker held, so the wording can name
    it back. A 'Bead:' line carries no number of its own and main() leaves
    marker_n at None on that path, so the same sentence there would read
    "#None" -- which is why PR #901 fixed one of the two forms and left the
    other silent rather than moving one line (bd gqlc-63ao).
    """
    if refs is not None:
        return f"the 'Refs: {bead_id} #{marker_n}' line holds nothing"
    return "no 'Closes' line was demanded and none was checked"


def extra_closes(pr_body, expected_n):
    """Numbers GitHub would close at merge that this PR's bead does not mirror.

    The demand above is a membership test -- is the bead's own number among
    the body's closing keywords -- and a membership test is satisfied by one
    member. Every other number in the body went unread, which is the whole of
    bd gqlc-7i3g: at merge GitHub acts on those too, and this gate had
    nothing left to hold them against, which is the harm its own docstring
    names.

    Asked as GH_CLOSES' question ("would GitHub close this number") rather
    than CLOSES' ("did the author write the line this gate demanded"),
    because what makes an extra harmful is the merge-time action, not the
    spelling. So 'Fixed GH-900' is an extra although the demand would not
    have recognised it.

    The two scans below read two different bodies, and which one each reads
    follows from which way it is expensive to be wrong.

    The extras scan reads claimable_prose(), for the reason main()'s no-bead
    check reads it: a hit here refuses, and a PR *about* this gate quotes
    closing lines as examples -- gqlc-7i3g's own description does. A quoted
    extra is not one GitHub acts on, so refusing it would be a false red on
    exactly the PRs that touch this file.

    The avowal scan reads visible_prose(), which is the opposite trade and
    the right one for a marker the author writes. An avowal subtracts a
    refusal, so honouring one no reader can find waves an unheld closing
    keyword through to merge -- the harm this file exists for -- while
    blanking one a reader can see costs a refusal cleared by moving the line.
    That is REFS_IN_BODY's argument, and the avowal is on its terms.

    Deduplicated, because the same number written twice is one assertion and
    not an extra.

    An 'Also-closes:' line subtracts the numbers it names, because a bare
    refusal here would be wrong on PRs that exist. Measured 2026-08-22 over
    all 25 open PRs, asking GitHub itself via closingIssuesReferences rather
    than reading the bodies: 3 carry two closing references, and 2 of the 3
    are deliberate -- #1199 closes #1123 and #1157, #1237 closes #1125 and
    #1218. Both would have been blocked with no remedy but splitting the PR,
    and the remedy bd gqlc-7i3g proposed instead -- resolve every extra to a
    bead in the export -- is not available: of the 6 numbers those 3 PRs
    close, the export mirrors 0, including the one extra that is beyond
    argument (#1194's own 'Closes #1159'). The third of the three is the
    accident this check is for, and the same measurement narrows what it
    was: GitHub links only #1159 from #1194, so its 'Վահագն closed #1184'
    line is narration GitHub does not act on. The avowal is what separates
    those two populations, and nothing in the export can.
    """
    seen = dict.fromkeys(GH_CLOSES.findall(claimable_prose(pr_body)))
    avowed = set()
    for line in ALSO_CLOSES.findall(visible_prose(pr_body)):
        avowed.update(HASH_N.findall(line))
    return [n for n in seen if n != expected_n and n not in avowed]


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

    # The same hole as the demand's, and louder on this path: the exit below
    # prints "no Closes demanded" over a body carrying a keyword GitHub acts
    # on. The check above is the self-contradiction only -- the bead's OWN
    # number -- so before this every other number passed an opt-out unread.
    extra = extra_closes(pr_body, expected_n)
    if extra:
        refuse(
            f"the PR body leaves {bead_id} open but closes "
            f"#{', #'.join(extra)}.",
            "An opt-out declares that this PR resolves no bead, so no "
            "closing",
            "keyword is demanded of it and none is checked. A keyword for "
            "another",
            "issue is still GitHub's to act on at merge, and this gate has",
            "nothing to hold that number against.",
            "Drop the keyword: if that issue is done, close it by hand; if "
            "it is not,",
            "give it its own bead and PR. Or drop the 'Refs:' line and "
            "resolve the",
            "bead this PR is about.",
            "If this PR really does resolve it, say so: a line reading",
            "'Also-closes: #<issue>' avows the extra and this check honours "
            "it.",
            "Editing the body re-runs this check on its own; you do",
            "not need to push a commit or reopen the PR.",
        )


def main():
    if len(sys.argv) < 4:
        print("Usage: check-pr-closes.py <jsonl_path> <body_file> <branch>")
        sys.exit(1)

    jsonl_path, body_file, branch = sys.argv[1], sys.argv[2], sys.argv[3]
    pr_body = read_body(body_file)

    bead_id, refs = declared_bead(pr_body, branch)
    if not bead_id:
        # Nothing resolved: no 'Bead:' or 'Refs:' line naming a bead id, and
        # no id in the branch name. This exit used to be sys.exit(0) with no
        # output, so a body with any number of closing lines on a
        # descriptively named branch was passed without one of them being
        # read (bd gqlc-mk7v; measured silent on PRs #946 and #963).
        #
        # A closing keyword is GitHub's to act on at merge, so a body that
        # carries one is asserting that an issue closes. That assertion is
        # what this gate exists to hold against a bead, and with no bead
        # there is nothing to hold it against, so it is refused. A body that
        # closes nothing asserts nothing, and a chore or docs PR is entitled
        # to that, so it passes -- audibly, because the whole complaint here
        # is that a silent pass reads like a gate that never ran.
        claimed = list(
            dict.fromkeys(GH_CLOSES.findall(claimable_prose(pr_body)))
        )
        if claimed:
            refuse(
                f"the PR body closes #{', #'.join(claimed)}, but no bead "
                "resolves for this PR.",
                "A closing keyword is GitHub's to act on at merge, so the "
                "body",
                "asserts that an issue closes while leaving this check "
                "nothing to",
                "hold the number against: no 'Bead:' or 'Refs:' line names a "
                "bead",
                "id, and the branch name carries none either.",
                "Add a line reading 'Bead: <bead-id>' for the bead this PR "
                "resolves,",
                "or drop the closing keyword if it resolves none.",
                "Editing the body re-runs this check on its own; you do",
                "not need to push a commit or reopen the PR.",
            )
        print(
            "[check-pr-closes] no bead named by the body or the branch, and "
            "no closing keyword in the body's prose - nothing to check"
        )
        sys.exit(0)

    # unverified_tail() takes this name at the exit for an export record
    # carrying no external_ref value and at the exit for a mirror that
    # names no issue number, and neither of those calls sits under a
    # 'refs is not None' branch: Python evaluates the argument whether or
    # not the callee goes on to use it. So the binding is required rather
    # than defensive. Delete this line and a body declaring its bead with
    # a 'Bead:' line, over a record carrying no external_ref value, raises
    # UnboundLocalError and exits 1 where it should print and pass; the
    # suite has rows on both of those exits in that declaration form and
    # that edit reds them. check_opt_out()'s read of it, further down,
    # does sit under such a branch.
    marker_n = None
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
        # What the export establishes here is that its record for this bead
        # carries no external_ref value. That is not the same proposition as
        # "the bead has no GitHub mirror", and this comment used to say it
        # was ("the pass is right -- there is no issue to leave open").
        # Measured over master's export at c4081ee4: 2 of its 459 records
        # carry no external_ref, and the live ledger mirrors both of them
        # (gqlc-l45j on #933, gqlc-8inj on #934). So on that export the two
        # propositions coincided nowhere, and the exit passed PR #951 -- body
        # line 'Closes #933' -- on the ground that #933 did not exist.
        #
        # It stays a pass anyway, because refusing here is a demand no author
        # can meet: the export is a committed file that lands in its own
        # chore commit after the PR merges, so the bead a PR is about can be
        # missing its mirror in the copy CI reads and nothing the author
        # writes in the body or the branch name changes that. A 'Refs:' line
        # reaches this same exit, so it is not an escape either. What changes
        # is that the pass now says it verified nothing, on both declaration
        # forms rather than on 'Refs:' alone (bd gqlc-63ao).
        print(
            f"[check-pr-closes] the export's record for {bead_id} carries no "
            "external_ref value, so "
            + unverified_tail(refs, bead_id, marker_n)
        )
        sys.exit(0)

    if bead.get("issue_type") == "epic":
        print(
            f"[check-pr-closes] {bead_id} is an epic - skipping "
            "(umbrella must not be closed)"
        )
        sys.exit(0)

    m = ISSUE_N.search(ext)
    if not m:
        # The twin of the exit above, and silent on the same declaration
        # form for the same reason: a mirror this cannot parse leaves no
        # number to demand, which is a pass, and the pass has to say so
        # whichever line declared the bead.
        print(
            f"[check-pr-closes] {bead_id} mirrors {ext!r}, which names no "
            "issue number, so "
            + unverified_tail(refs, bead_id, marker_n)
        )
        sys.exit(0)
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
    found = CLOSES.findall(claimable_prose(pr_body))

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
        # Two answers, not one. The wording this replaced was "Replace
        # 'Closes #<wrong>' with 'Closes #<right>'", which assumes the
        # replacement is the fix and never mentions that leaving the issue
        # open is a declarable answer -- so an author who decides the gate is
        # wrong writes the opt-out and quotes the rejected instruction back
        # into the body to explain why. That explanation used to be an extra
        # claim, because the demand read the raw body; it no longer is, since
        # the demand reads claimable_prose() and a backticked keyword is
        # blanked. The imperative is dropped anyway: what a refusal spells
        # out is what gets pasted, so it spells out the two answers rather
        # than one line to copy (bd gqlc-tysj).
        refuse(
            f"PR body closes #{', #'.join(wrong)} but bead {bead_id} "
            f"maps to #{expected_n}.",
            "Closing the wrong issue is worse than closing none.",
            f"If this PR resolves {bead_id}, point a closing keyword at "
            f"#{expected_n}.",
            f"If it leaves {bead_id} open, declare that with a line reading",
            f"'Refs: {bead_id} #{expected_n}' and starting at that line's "
            "first character.",
            "Editing the body re-runs this check on its own; you do",
            "not need to push a commit or reopen the PR.",
        )

    # The bead's own number is present. That is a membership test, and it is
    # satisfied by one member, so until bd gqlc-7i3g every other closing
    # keyword in the body reached merge unexamined.
    extra = extra_closes(pr_body, expected_n)
    if extra:
        refuse(
            f"the PR body also closes #{', #'.join(extra)}, which "
            f"{bead_id} does not mirror.",
            f"{bead_id} mirrors #{expected_n}, the body closes it, and that "
            "much is",
            "what this gate demanded. The numbers above are the ones it "
            "demanded",
            "nothing of: a closing keyword is GitHub's to act on at merge, "
            "so each",
            "of them closes an issue with nothing left here to hold the "
            "number",
            "against.",
            "Drop the keyword for any issue this PR does not resolve.",
            "If that issue is done, close it by hand; if it is not, give it "
            "its own",
            "bead and PR.",
            "If this PR really does resolve it, say so: a line reading",
            "'Also-closes: #<issue>' avows the extra and this check honours "
            "it.",
            "Editing the body re-runs this check on its own; you do",
            "not need to push a commit or reopen the PR.",
        )

    # Correct number present, and it is the only one the body closes
    print(f"[check-pr-closes] {bead_id} -> Closes #{expected_n} (ok)")
    sys.exit(0)


if __name__ == "__main__":
    main()
