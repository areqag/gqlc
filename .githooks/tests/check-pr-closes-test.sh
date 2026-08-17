#!/usr/bin/env bash
# Tests for .github/scripts/check-pr-closes.py
# (bd gqlc-nyo, bd gqlc-w4al, bd gqlc-1ekq, bd gqlc-zzz5).
#
# The checker decides whether a PR may merge without closing the GitHub issue
# its bead mirrors. Most of its exits are passes, so the interesting property
# is not "does it fail on a bad body" but "is there any input it cannot read
# that it passes anyway" — a gate whose inputs go missing quietly is a gate
# that has stopped running while still reporting green. Its body used to
# arrive as an environment variable filled by a grep pipeline whose failure
# was indistinguishable from "no bead on this PR"; it now arrives as a file,
# and the cases below hold both that file and the bd export to fail-closed.
#
# Two ways of passing carry their own sections. A "Refs:" line is the declared
# opt-out for a PR that touches a bead without resolving it, and every
# condition it has to survive is a row here, because the whole point of
# declaring it is that it cannot be a bare assertion. A body-declared bead id
# the export could never carry is a refusal rather than a skip, because the
# skip it used to take demanded nothing of the PR and said nothing in the log.
#
# The workflow half — that anything re-runs when a PR body changes, and that
# what is read is the live body rather than the frozen event payload — is
# pinned in internal/tools/ciguard, against the parsed ci.yml. That the suite
# below is reached from CI at all is pinned there too.
#
# Run via: just test-hooks
set -u

REPO="$(cd "$(dirname "$0")/../.." && pwd)"
SCRIPT="$REPO/.github/scripts/check-pr-closes.py"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

pass=0
fail=0
ok()  { pass=$((pass + 1)); printf 'ok   - %s\n' "$1"; }
bad() { fail=$((fail + 1)); printf 'FAIL - %s: %s\n' "$1" "$2"; }

# A bd export with one bead of each shape the checker branches on.
# gqlc-sub.12 carries no "status" key, which is how the checker sees a record
# bd exported before that field existed; gqlc-done is the only closed one.
EXPORT="$TMP/issues.jsonl"
cat >"$EXPORT" <<'JSONL'
{"id":"gqlc-mirrored","issue_type":"bug","status":"in_progress","external_ref":"https://github.com/areqag/gqlc/issues/617"}
{"id":"gqlc-local","issue_type":"task","external_ref":""}
{"id":"gqlc-umbrella","issue_type":"epic","external_ref":"https://github.com/areqag/gqlc/issues/500"}
{"id":"gqlc-done","issue_type":"bug","status":"closed","external_ref":"https://github.com/areqag/gqlc/issues/700"}
{"id":"gqlc-sub.12","issue_type":"task","external_ref":"https://github.com/areqag/gqlc/issues/712"}
{"id":"gqlc-oddref","issue_type":"task","external_ref":"https://example.com/tracker/9"}
JSONL

# $1=file contents; echoes the path of a body file holding them.
body() {
    local f
    f="$TMP/body.$RANDOM.txt"
    printf '%s' "$1" >"$f"
    printf '%s' "$f"
}

# Leaves the checker's combined output in $OUT and its status in $RC.
# $1=export path, $2=body file path, $3=branch name.
OUT=""
RC=0
run_check() {
    OUT="$(python3 "$SCRIPT" "$1" "$2" "$3" 2>&1)"
    RC=$?
}

# $1=case name, $2=export, $3=body file, $4=branch, $5=expected substring.
# The substring is required as well as the status: a gate that refuses without
# saying which issue number it wanted sends the author back to the workflow
# source to find out, which is how the old failure text ("Add 'Closes #598' to
# the PR body") stranded a PR whose body already said exactly that.
expect_red() {
    run_check "$2" "$3" "$4"
    if [ "$RC" -eq 0 ]; then
        bad "$1" "exited 0: $OUT"
    elif ! printf '%s' "$OUT" | grep -qF "$5"; then
        bad "$1" "refused, but did not say '$5': $OUT"
    else
        ok "$1"
    fi
}

# $1=case name, $2=export, $3=body file, $4=branch.
expect_green() {
    run_check "$2" "$3" "$4"
    if [ "$RC" -eq 0 ]; then
        ok "$1"
    else
        bad "$1" "exited $RC: $OUT"
    fi
}

# $1=case name, $2=export, $3=body file, $4=branch, $5=expected substring.
# For the passes that have to leave a trace: a pass whose output could be
# empty is one nobody can tell from the gate not having run.
expect_green_saying() {
    run_check "$2" "$3" "$4"
    if [ "$RC" -ne 0 ]; then
        bad "$1" "exited $RC: $OUT"
    elif ! printf '%s' "$OUT" | grep -qF "$5"; then
        bad "$1" "passed, but did not say '$5': $OUT"
    else
        ok "$1"
    fi
}

# --- the gate's reason for existing -----------------------------------------

expect_green "a body closing the bead's own issue passes" \
    "$EXPORT" "$(body 'Bead: gqlc-mirrored

Closes #617')" "some/branch"

expect_red "a body with no Closes line is refused, naming the issue" \
    "$EXPORT" "$(body 'Bead: gqlc-mirrored')" "some/branch" \
    "missing 'Closes #617'"

expect_red "a body closing the wrong issue is refused, naming both numbers" \
    "$EXPORT" "$(body 'Bead: gqlc-mirrored

Closes #123')" "some/branch" \
    "closes #123 but bead gqlc-mirrored maps to #617"

# The escape from the state this bead was filed over: the failure has to say
# that editing the body is enough. Pushing a commit and reopening the PR were
# the only two ways out, and the message named neither.
expect_red "the refusal says an edit alone re-runs the check" \
    "$EXPORT" "$(body 'Bead: gqlc-mirrored')" "some/branch" \
    "Editing the body re-runs this check on its own"

# The other way out of the same refusal. An author whose PR does not resolve
# its bead has to be told the declared spelling, because the escapes left
# otherwise are undeclared ones: rename the branch so it carries no id, or
# write a 'Bead:' line naming a well-formed id the export does not carry —
# the row above admits that second one deliberately. Both pass this gate in
# silence, and both pass it for the PRs that should have closed something too
# (bd gqlc-1ekq, bd gqlc-oh30).
expect_red "the refusal spells the opt-out, with this bead's own number" \
    "$EXPORT" "$(body 'Bead: gqlc-mirrored')" "some/branch" \
    "'Refs: gqlc-mirrored #617'"

# --- inputs the checker cannot read are refusals, not passes -----------------
# Both of these used to be, or would naturally be, a silent pass. An absent
# body reads as "no Closes line present" only if you already know a body was
# fetched; an absent export reads as "bead not found", which the checker
# passes on purpose so a stale export cannot block a PR.

expect_red "a body file that is not there is refused" \
    "$EXPORT" "$TMP/no-such-body.txt" "some/branch" \
    "cannot read the PR body"

expect_red "an export that is not there is refused" \
    "$TMP/no-such-export.jsonl" "$(body 'Bead: gqlc-mirrored

Closes #617')" "some/branch" \
    "cannot read the bd export"

# An empty body is a body: it was fetched and it says nothing. The branch
# still names the bead, so silence must be refused rather than skipped —
# which is the shape of the fetch failure the step's `bash -e` catches, held
# here from the other side in case it ever does not.
expect_red "an empty body over a bead-bearing branch is refused" \
    "$EXPORT" "$(body '')" "fix/gqlc-mirrored-thing" \
    "missing 'Closes #617'"

# --- which bead the PR is about ---------------------------------------------

expect_red "the bead id is taken from the branch when the body has no Bead line" \
    "$EXPORT" "$(body 'no marker here')" "fix/gqlc-mirrored-thing" \
    "missing 'Closes #617'"

# The body wins, so a branch named after one bead can carry a PR about
# another. If precedence inverted, a body that named its bead explicitly
# would be overruled by whatever the branch name happened to contain.
expect_green "a Bead line in the body overrides the branch name" \
    "$EXPORT" "$(body 'Bead: gqlc-local')" "fix/gqlc-mirrored-thing"

expect_green "a PR naming no bead at all passes" \
    "$EXPORT" "$(body 'no marker here')" "some/branch"

# --- the sanctioned passes --------------------------------------------------
# Each is a way through the gate, so each is a way past it. A row per exit
# records which passes are meant; nothing here fails when a new pass exit is
# added, so that stays a reading obligation on the diff rather than an
# asserted one. The declared opt-out is the fourth, and has a section of its
# own below.

expect_green "a bead with no GitHub mirror needs no Closes" \
    "$EXPORT" "$(body 'Bead: gqlc-local')" "some/branch"

expect_green "an epic is never closed by a child PR" \
    "$EXPORT" "$(body 'Bead: gqlc-umbrella')" "some/branch"

# The export trails the ledger by whole sessions here — beads are exported in
# their own chore commits after the PR that opened them merges — so a
# well-formed id the export has never heard of is the normal state of a PR
# about a bead created today, not a typo.
expect_green "a bead the export does not carry does not block the PR" \
    "$EXPORT" "$(body 'Bead: gqlc-notinexport')" "some/branch"

# How wide that pass is, pinned rather than left to be discovered: the id need
# not name a real bead, so one line overrides a branch that does name a
# mirrored one and the demand disappears from any PR. Held open on purpose —
# the export trails the ledger, so an absent id is the normal state of a PR
# about a bead created today — and filed as bd gqlc-oh30 for the day that
# trade stops being worth it.
expect_green "an absent id on a Bead line overrides a mirrored branch" \
    "$EXPORT" "$(body 'Bead: gqlc-notinexport')" "fix/gqlc-mirrored-thing"

# --- shapes the body is allowed to take -------------------------------------

expect_green "GitHub's other closing keywords count" \
    "$EXPORT" "$(body 'Bead: gqlc-mirrored

fixes #617')" "some/branch"

expect_green "a second Closes line does not hide the right one" \
    "$EXPORT" "$(body 'Bead: gqlc-mirrored

Closes #617
Closes #900')" "some/branch"

# One unparseable line ahead of the bead used to abort the whole scan, and an
# aborted scan is a bead "not in the export", which passes. A malformed
# export would therefore have waved every PR through.
BROKEN="$TMP/broken.jsonl"
{ printf 'not json at all\n'; cat "$EXPORT"; } >"$BROKEN"
expect_red "a malformed line ahead of the bead does not hide it" \
    "$BROKEN" "$(body 'Bead: gqlc-mirrored')" "some/branch" \
    "missing 'Closes #617'"

# --- a declared id the export could never carry (bd gqlc-zzz5) ---------------
# The value on a Bead:/Refs: line is the whole token around the "gqlc-", so a
# token nothing could match is seen and named. The two spellings below both
# used to reach "bead not in export - skipping", which demands nothing; the
# backticked one printed no line at all.

# Built rather than written literally: shellcheck reads a backtick inside a
# quoted argument as a command substitution (SC2016) whichever quotes it is in.
BT="$(printf '\140')"
FENCE3="$BT$BT$BT"
FENCE4="$FENCE3$BT"
TILDE3='~~~'
TILDE4='~~~~'

expect_red "a backticked id on a Bead line is refused, naming the token" \
    "$EXPORT" "$(body "Bead: ${BT}gqlc-mirrored${BT}")" "some/branch" \
    "'${BT}gqlc-mirrored${BT}', which is not a bead id"

expect_red "an id with a trailing full stop is refused, naming the token" \
    "$EXPORT" "$(body 'Bead: gqlc-mirrored.')" "some/branch" \
    "'gqlc-mirrored.', which is not a bead id"

expect_red "a backticked id on a Refs line is refused too" \
    "$EXPORT" "$(body "Refs: ${BT}gqlc-mirrored${BT} #617")" "some/branch" \
    "'Refs:' declaration reads '${BT}gqlc-mirrored${BT}'"

# 'Bead:' is read anywhere in the body, so the rule reaches prose: a sentence
# that writes an id in backticks after that word is a malformed declaration.
# That is a refusal on a body which passed before this rule, in the direction
# the rule was asked for, and the refusal names the token it read. Measured
# over the repo's last 120 PR bodies: none carries such a token, so no PR
# that passes today is refused by it.
expect_red "a backticked id after 'bead:' in prose is refused too" \
    "$EXPORT" "$(body "Fixed the parser.

Filed the follow-up bead: ${BT}gqlc-abc${BT}.")" "some/branch" \
    "which is not a bead id"

# The other half of the same rule, and the one that says it is about
# well-formedness rather than presence: sub-bead ids carry a dot, and
# refusing them would refuse a large minority of the repo's beads. Counted
# over .beads/issues.jsonl at master 0c214d20 (2026-08-17): 86 of 438. The
# export grows every session and CI reads the merge commit's copy, so the
# pair is what the sample was on a day, not what the repo has now.
expect_green "a sub-bead id with a dot is a bead id" \
    "$EXPORT" "$(body 'Bead: gqlc-sub.12

Closes #712')" "some/branch"

# A line with no bead id on it is not a declaration, so it cannot be a
# malformed one either.
expect_green "a Bead line naming no id at all is not a declaration" \
    "$EXPORT" "$(body 'Bead: none yet')" "some/branch"

# --- the declared opt-out (bd gqlc-1ekq) -------------------------------------
# A PR that changes a bead's status without resolving it says so on a Refs:
# line. Every row from here down is a condition that declaration has to
# survive, because a marker nothing checks is the branch-name dodge with extra
# steps: it would pass this gate on a PR that does resolve its bead, and leave
# no trace that it had.

expect_green_saying "an opt-out passes and says which issue stays open" \
    "$EXPORT" "$(body 'Refs: gqlc-mirrored #617 (reopens it)')" "some/branch" \
    "issue #617 stays open at merge"

# The pass has to be visible on the check run, not only in its log. Four
# other rows above pass without resolving anything — no bead named, no
# mirror, an epic, a bead the export does not carry — and none of them says
# so anywhere GitHub renders. This is the one the gate reports as a warning,
# and GitHub renders a ::warning:: line as an annotation against the run.
expect_green_saying "an honoured opt-out is annotated, not just logged" \
    "$EXPORT" "$(body 'Refs: gqlc-mirrored #617')" "some/branch" \
    "::warning title=check-pr-closes opt-out::"

# The witness this was filed over: PR #609 reopened gqlc-4np (GH #503) from a
# branch that happened not to carry the id. Named the way this repo names
# branches, the gate would have demanded Closes #503 on the PR whose purpose
# was that #503 be open.
expect_green_saying "an opt-out holds over a branch named after the bead" \
    "$EXPORT" "$(body 'Refs: gqlc-mirrored #617')" "fix/gqlc-mirrored-reopen" \
    "no Closes demanded"

expect_green_saying "an opt-out works on a record with no status field" \
    "$EXPORT" "$(body 'Refs: gqlc-sub.12 #712')" "some/branch" \
    "status in the export: unknown"

# Two exits used to read a marker, hold its number against nothing, and print
# nothing: rc=0 with no output, which is also what this gate looks like when
# it has not run. The pass is right in both -- there is no issue to leave
# open -- so what these rows hold is that it is legible. This is the
# complaint the section above makes about the four other silent passes,
# applied to the two on the marker's own path.
expect_green_saying "an opt-out on a bead with no mirror says so" \
    "$EXPORT" "$(body 'Refs: gqlc-local #99999')" "some/branch" \
    "gqlc-local has no GitHub mirror"

expect_green_saying "an opt-out on a mirror with no issue number says so" \
    "$EXPORT" "$(body 'Refs: gqlc-oddref #99999')" "some/branch" \
    "which names no issue number"

expect_red "an opt-out that names no issue number is refused" \
    "$EXPORT" "$(body 'Refs: gqlc-mirrored')" "some/branch" \
    "line names no issue number"

# The number is what stops the marker being copied from another PR: an author
# who writes it has read this gate's own refusal, which names it.
expect_red "an opt-out naming the wrong issue is refused, naming both" \
    "$EXPORT" "$(body 'Refs: gqlc-mirrored #123')" "some/branch" \
    "names #123, but gqlc-mirrored mirrors #617"

# The contradiction the export can see. A PR whose own .beads/issues.jsonl
# closes the bead has resolved it, whatever the body says.
expect_red "an opt-out on a bead the export closes is refused" \
    "$EXPORT" "$(body 'Refs: gqlc-done #700')" "some/branch" \
    "but the export at this commit closes it"

# The contradiction the body can see. GitHub acts on a closing keyword at
# merge whether or not this gate agreed, so the issue would not stay open.
expect_red "an opt-out beside a Closes for the same issue is refused" \
    "$EXPORT" "$(body 'Refs: gqlc-mirrored #617

Closes #617')" "some/branch" \
    "also carries a closing keyword for #617"

# The same contradiction in every spelling GitHub documents. A spelling
# check 4 does not know is an opt-out honoured over a body that closes the
# issue anyway: GitHub acts on the keyword at merge while a required check
# prints an annotation saying the issue stays open, which is the gate
# asserting the opposite of what happens. The nine keywords are GitHub's
# list; the four reference forms are the ones it autolinks.
for kw in Close Closes Closed Fix Fixes Fixed Resolve Resolves Resolved; do
    expect_red "an opt-out beside '$kw #617' is refused" \
        "$EXPORT" "$(body "Refs: gqlc-mirrored #617

$kw #617")" "some/branch" \
        "also carries a closing keyword for #617"
done

for ref in 'Closes: #617' 'CLOSES: #617' 'Closes areqag/gqlc#617' \
    'Closes GH-617' 'Closes https://github.com/areqag/gqlc/issues/617'; do
    expect_red "an opt-out beside '$ref' is refused" \
        "$EXPORT" "$(body "Refs: gqlc-mirrored #617

$ref")" "some/branch" \
        "also carries a closing keyword for #617"
done

# The wider list is check 4's alone. What the gate demands is still
# 'Closes'/'Fixes'/'Resolves' with a bare '#N', so a body carrying one of the
# other spellings and no marker is refused exactly as it was before check 4
# learned them. Teaching the demand the wider list would refuse nothing new
# and would newly pass PRs it refuses today, which is a loosening; the two
# questions fail in opposite directions, so they stay two patterns.
expect_red "a keyword only check 4 knows does not satisfy the demand" \
    "$EXPORT" "$(body 'Bead: gqlc-mirrored

Fixed #617')" "some/branch" \
    "missing 'Closes #617'"

# The number is held, not just the keyword: an opt-out for one issue beside a
# Closes for a different one is two true statements.
expect_green_saying "an opt-out beside a Closes for another issue passes" \
    "$EXPORT" "$(body 'Refs: gqlc-mirrored #617

Closes #999')" "some/branch" \
    "issue #617 stays open at merge"

expect_red "naming one bead on both a Bead and a Refs line is refused" \
    "$EXPORT" "$(body 'Bead: gqlc-mirrored

Refs: gqlc-mirrored #617')" "some/branch" \
    "on a 'Bead:' line and on a 'Refs:' line"

# An opt-out excuses the bead the PR is about, not a different one. Without
# this a branch named after a mirrored bead could be waved through by a Refs:
# line naming any other bead in the export.
expect_red "an opt-out naming a bead the branch does not is refused" \
    "$EXPORT" "$(body 'Refs: gqlc-sub.12 #712')" "fix/gqlc-mirrored-thing" \
    "the branch is named after gqlc-mirrored"

# The redirect refusal above needs a bead to compare against, so it is scoped
# to branches that carry an id. A Bead: line covers the same ground by a
# different route -- it outranks the marker, so a lifted one is never read and
# the demand for the Bead: line's own bead stands ("a Refs for another bead
# does not excuse the Bead line", at the foot of this file).
# When neither names a bead the marker names it, and a marker lifted verbatim
# from another PR is taken. That is not a bypass -- with no bead named there was no Closes to
# demand (gqlc-0pb8), and the pass only announces that an unrelated issue
# stays open, which it would have anyway -- but it does bound how far check 1
# reaches, so it is pinned rather than left to be inferred from the rows above
# that happen to use a bead-less branch.
expect_green_saying "an opt-out on a branch naming no bead names the bead" \
    "$EXPORT" "$(body 'Refs: gqlc-mirrored #617')" "chore/no-id-in-this-name" \
    "gqlc-mirrored -> Refs #617, no Closes demanded"

# The marker is a line, not a word. Prose about the opt-out — this file's own
# subject matter, and every PR body that documents it — must not be one.
expect_red "a Refs: quoted inside a sentence does not opt out" \
    "$EXPORT" "$(body 'an opt-out is a Refs: gqlc-mirrored #617 line')" \
    "fix/gqlc-mirrored-thing" \
    "missing 'Closes #617'"

# --- what a marker has to be visible in (bd gqlc-1ekq) ----------------------
# Neither is the marker shown in a code block, which is how this repo quotes
# it. Four leading spaces is markdown's indented-code-block spelling, and a
# fence leaves the line at column zero, so anchoring alone catches only one of
# the two. Both bodies below opt out on the checker as it stood before these
# rows: the branch names the bead, so the demand has to come back.
expect_red "a Refs: indented into a code block does not opt out" \
    "$EXPORT" "$(body 'Bead-free body.

    Refs: gqlc-mirrored #617')" "fix/gqlc-mirrored-thing" \
    "missing 'Closes #617'"

expect_red "a Refs: inside a fenced block does not opt out" \
    "$EXPORT" "$(body "Bead-free body.

${FENCE3}
Refs: gqlc-mirrored #617
${FENCE3}")" "fix/gqlc-mirrored-thing" \
    "missing 'Closes #617'"

# The fence has to close again, or a body with any code block in it could
# never opt out below that block.
expect_green_saying "a marker below a closed code block is still a declaration" \
    "$EXPORT" "$(body "${FENCE3}
quoted output, not a declaration
${FENCE3}

Refs: gqlc-mirrored #617")" "some/branch" \
    "issue #617 stays open at merge"

# The three rows above were once the whole of it, over a fence model that was
# a toggle: any ``` or ~~~ line flipped the state, which is not how a fence
# closes. Every body in this section was put to GitHub's own renderer
# (POST /markdown, mode gfm, a read-only call) and the row's colour reports
# what came back — red where GitHub puts the marker inside <pre><code> or
# drops it from the output entirely, green where GitHub renders it as prose.
# 22 bodies, and one row is red over a body GitHub renders: the declared
# divergence, last in this section. Put the toggle back in place of
# prose_only and 14 rows fail — 12 red ones that pass, honouring a marker no
# reader can see, and 2 green ones that lose their annotation. Nesting a
# fence in a longer one is not an exotic spelling: it is the ordinary way to
# show a fence, and showing this marker is what the beads queued against this
# file are for.
expect_red "a fence nested in a longer one does not close it" \
    "$EXPORT" "$(body "${FENCE4}
${FENCE3}
Refs: gqlc-mirrored #617
${FENCE3}
${FENCE4}")" "fix/gqlc-mirrored-thing" \
    "missing 'Closes #617'"

expect_red "a tilde line does not close a backtick fence" \
    "$EXPORT" "$(body "${FENCE3}
some code
${TILDE3}
Refs: gqlc-mirrored #617
${FENCE3}")" "fix/gqlc-mirrored-thing" \
    "missing 'Closes #617'"

expect_red "a backtick line does not close a tilde fence" \
    "$EXPORT" "$(body "${TILDE3}
${FENCE3}
Refs: gqlc-mirrored #617
${FENCE3}
${TILDE3}")" "fix/gqlc-mirrored-thing" \
    "missing 'Closes #617'"

expect_red "a fence nested in a longer tilde one does not close it" \
    "$EXPORT" "$(body "${TILDE4}
${TILDE3}
Refs: gqlc-mirrored #617
${TILDE3}
${TILDE4}")" "fix/gqlc-mirrored-thing" \
    "missing 'Closes #617'"

# A closer carries nothing after the run. An info string makes the line
# another opener as far as GitHub is concerned, which is how a fence gets
# shown with its language on it.
expect_red "a run with an info string does not close a fence" \
    "$EXPORT" "$(body "${FENCE3}
${FENCE3}js
Refs: gqlc-mirrored #617
${FENCE3}
${FENCE3}")" "fix/gqlc-mirrored-thing" \
    "missing 'Closes #617'"

# Four spaces is an indented code block, not a fence, in either direction:
# a closer indented that far does not close, and an opener indented that far
# does not open. The second row is the one that costs something -- it honours
# a marker -- so it is the one that says the rule is not "blank everything".
expect_red "a closer indented four spaces does not close a fence" \
    "$EXPORT" "$(body "${FENCE3}
some code
    ${FENCE3}
Refs: gqlc-mirrored #617
${FENCE3}")" "fix/gqlc-mirrored-thing" \
    "missing 'Closes #617'"

expect_green_saying "an opener indented four spaces opens no fence" \
    "$EXPORT" "$(body "Bead-free body.

    ${FENCE3}
Refs: gqlc-mirrored #617")" "some/branch" \
    "issue #617 stays open at merge"

# A backtick fence's info string may not carry a backtick, so this line is a
# paragraph and the fence below it is the one that opens. Under the toggle
# the two lines cancelled and the marker was read as live.
expect_red "a backtick in the info string opens no fence" \
    "$EXPORT" "$(body "${FENCE3}a${BT}b
${FENCE3}
Refs: gqlc-mirrored #617
${FENCE3}")" "fix/gqlc-mirrored-thing" \
    "missing 'Closes #617'"

# A body pasted from a Windows editor is still a body.
expect_red "a nested fence in a CRLF body does not opt out" \
    "$EXPORT" "$(body "$(printf '%s\r\n%s\r\n%s\r\n%s\r\n%s' \
        "${FENCE4}" "${FENCE3}" 'Refs: gqlc-mirrored #617' \
        "${FENCE3}" "${FENCE4}")")" "fix/gqlc-mirrored-thing" \
    "missing 'Closes #617'"

# The second carrier, and the one that needs no nesting: GitHub renders an
# HTML comment as nothing at all, so a marker inside one is a declaration no
# reader of the PR can see and only a checker reading raw bytes can. This
# repo has met the shape before (bd gqlc-jnsk, a graded site inside an HTML
# comment). An unclosed comment swallows the rest of the body, as an unclosed
# fence does.
expect_red "a Refs: inside an HTML comment does not opt out" \
    "$EXPORT" "$(body 'Bead-free body.

<!--
Refs: gqlc-mirrored #617
-->')" "fix/gqlc-mirrored-thing" \
    "missing 'Closes #617'"

expect_red "an unclosed HTML comment swallows the marker below it" \
    "$EXPORT" "$(body 'Bead-free body.

<!--
Refs: gqlc-mirrored #617')" "fix/gqlc-mirrored-thing" \
    "missing 'Closes #617'"

# A comment may open part-way along a line and still run on to the next one,
# so the opener is looked for anywhere, not only at the first character.
expect_red "a comment opened part-way along a line still hides the marker" \
    "$EXPORT" "$(body 'Bead-free body. <!-- x --> <!--
Refs: gqlc-mirrored #617
-->')" "fix/gqlc-mirrored-thing" \
    "missing 'Closes #617'"

expect_green_saying "a marker below a closed HTML comment is a declaration" \
    "$EXPORT" "$(body '<!--
a note nobody sees
-->

Refs: gqlc-mirrored #617')" "some/branch" \
    "issue #617 stays open at merge"

# The third carrier. <pre> and <code> are markdown's raw-HTML spellings of a
# code block and GitHub renders them as one. <script>, <style> and <textarea>
# are the tags markdown groups with <pre>, and are deliberately left alone:
# GitHub's sanitiser drops the tag and keeps the text, so the marker below is
# visible to a reader and is honoured. Blanking it would cost a refusal for
# no gain.
expect_red "a Refs: inside a <pre> block does not opt out" \
    "$EXPORT" "$(body 'Bead-free body.

<pre>
Refs: gqlc-mirrored #617
</pre>')" "fix/gqlc-mirrored-thing" \
    "missing 'Closes #617'"

expect_green_saying "a marker below a closed <pre> block is a declaration" \
    "$EXPORT" "$(body '<pre>
quoted output
</pre>

Refs: gqlc-mirrored #617')" "some/branch" \
    "issue #617 stays open at merge"

expect_green_saying "a marker below a one-line <pre> block is a declaration" \
    "$EXPORT" "$(body '<pre>quoted output</pre>

Refs: gqlc-mirrored #617')" "some/branch" \
    "issue #617 stays open at merge"

expect_green_saying "a marker inside a <script> block is a declaration" \
    "$EXPORT" "$(body '<script>
Refs: gqlc-mirrored #617
</script>')" "some/branch" \
    "issue #617 stays open at merge"

expect_red "a Refs: inside a <code> block does not opt out" \
    "$EXPORT" "$(body 'Bead-free body.

<code>
Refs: gqlc-mirrored #617
</code>')" "fix/gqlc-mirrored-thing" \
    "missing 'Closes #617'"

# The closer has to be the tag that opened, so a '</code>' does not end a
# <pre> block and the marker below stays blanked.
expect_red "a </code> does not close a <pre> block" \
    "$EXPORT" "$(body 'Bead-free body.

<pre>
</code>
Refs: gqlc-mirrored #617
</pre>')" "fix/gqlc-mirrored-thing" \
    "missing 'Closes #617'"

# What this does NOT blank, stated as a row rather than left to be found. A
# code span opens and closes with backtick runs shorter than a fence, and can
# span lines; GitHub renders the marker inside one as <code>, which is
# visible monospace text rather than something a reader cannot see. Blanking
# it needs inline parsing, and the invisibility this section exists for is
# absent, so the marker is honoured and the limit is pinned here.
expect_green_saying "a marker inside a multi-line code span is a declaration" \
    "$EXPORT" "$(body 'Bead-free body.

'"${BT}${BT}"'
Refs: gqlc-mirrored #617
'"${BT}${BT}")" "some/branch" \
    "issue #617 stays open at merge"

# Which of the three is open decides what the others mean, so the state is
# one machine and not three passes. A fence line inside a comment opens no
# fence, and a comment opener inside a fence opens no comment; both bodies
# below leave the marker in prose, where GitHub also leaves it.
# A comment opener inside a raw HTML block is the one crossing that goes the
# other way: the block is passed through as HTML, so GitHub's sanitiser
# swallows from the '<!--' past '</pre>' and to the end of the body. Measured
# first-party: that body renders as an empty <pre> and nothing else.
expect_red "a comment opened inside a <pre> block hides the marker below it" \
    "$EXPORT" "$(body '<pre>
<!--
</pre>

Refs: gqlc-mirrored #617')" "fix/gqlc-mirrored-thing" \
    "missing 'Closes #617'"

expect_green_saying "a fence line inside a comment opens no fence" \
    "$EXPORT" "$(body "<!--
${FENCE3}
-->

Refs: gqlc-mirrored #617")" "some/branch" \
    "issue #617 stays open at merge"

expect_green_saying "a comment opener inside a fence opens no comment" \
    "$EXPORT" "$(body "${FENCE3}
<!--
${FENCE3}

Refs: gqlc-mirrored #617")" "some/branch" \
    "issue #617 stays open at merge"

# Where this disagrees with GitHub, and the direction it disagrees in. A
# fence indented one to three spaces into a list item is a fence here and a
# fence inside the list item there, so a column-zero line below it is prose
# to GitHub and blanked by this. The author moves the line out from under the
# block and the refusal names what to write; the other direction would be
# this gate annotating a check run over a declaration nobody can see. Swept
# 85 body shapes through GitHub's renderer and this checker together -- 58
# of them, then 35 more picked to break the result the first 58 gave, 8
# shared: seven disagreements, this one among them. None goes the other way
# except the code span rowed above, which GitHub renders as visible
# monospace rather than hiding. 85 shapes is not every shape, so what the
# sweep bounds is the direction, not the count.
expect_red "a fence indented into a list item blanks the marker below it" \
    "$EXPORT" "$(body "- item

  ${FENCE3}
Refs: gqlc-mirrored #617
  ${FENCE3}")" "fix/gqlc-mirrored-thing" \
    "missing 'Closes #617'"

# The inverse asymmetry, in the same function. Check 4 reads the raw body, so
# a closing keyword quoted in a fence refuses an opt-out although GitHub will
# not act on it. That is the direction GH_CLOSES is argued in throughout -- a
# spelling matched here costs a refusal the author resolves, one missed costs
# the gate affirming that an issue stays open when it does not -- but the two
# halves of the same function reading two different bodies is worth a row
# rather than an inference.
expect_red "a Closes quoted in a fence still refuses an opt-out" \
    "$EXPORT" "$(body "Refs: gqlc-mirrored #617

${FENCE3}
Closes #617
${FENCE3}")" "some/branch" \
    "also carries a closing keyword for #617"

# A Bead: line is the resolving declaration, so it outranks a Refs: line for
# another bead rather than being excused by it.
expect_red "a Refs for another bead does not excuse the Bead line" \
    "$EXPORT" "$(body 'Bead: gqlc-mirrored

Refs: gqlc-sub.12 #712')" "some/branch" \
    "missing 'Closes #617'"

printf -- '---\n%d passed, %d failed\n' "$pass" "$fail"
[ "$fail" -eq 0 ]
