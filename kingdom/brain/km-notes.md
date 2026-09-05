# km-notes

The prose that stood inside `kingdom/bin/km`, moved out under bd gqlc-rc3jb
and gqlc-sz410 at c1719bdb. Most of it is the town's incident record — the bead
ids, the measurements and the dates are the words written at the moment each
finding was made. Every entry is the comment block that stood at the named
function, verbatim with its hash marks stripped and nothing reworded.

km carries a one-line pointer at each site: `# history:
kingdom/brain/km-notes.md#<slug>`. The slug is the first word of the heading,
so a pointer resolves by searching this file for `## <slug>`. Several blocks
from one function are suffixed `-2`, `-3` in file order.

**Read this before editing the function a pointer names.** These blocks are
here because deleting them would have the town re-run the incidents they
record; moving them did not make them optional.

## verb — the operator-run verb a message advertises is free text at the print site

(`kingdom/bin/km`, `verb`)

The verb a message advertises an operator run is free text at the print site;
the verbs km HAS are case labels in the dispatch table at the foot of this
file. Nothing joined the two, so when f6dc4c7b (#1595) deleted the tmux
operator chrome it took the `herratsayn)` label and left `km halt` still
telling whoever had just stopped the town to run `km herratsayn` (gqlc-cw7h).
That sentence had been correct since it was written six days earlier — the
label was live from c08fc7f0 (2026-08-21) — and at the moment it was deleted
the two sat 2489 lines apart in this file, so the deletion falsified the
sentence without touching it. That is the drift this catches and review does
not: whoever would have to notice is the one removing the arm, not the one who
wrote the line. This reads the labels out of that table, so a print site is
answered by km's own routing rather than by whatever the author typed.

It warns and carries on rather than dying, and prints the name either way: the
operator reaching for the emergency brake must not be stopped by a defect in
the sentence that describes it. A caller that wants the refusal fatal assigns
it (`v=$(verb resume)`), which `set -e` does abort on. So this does not make a
phantom verb unwritable — it makes it announce itself on stderr the first time
the path runs, to the developer who just edited it.

It does not see past the first word. Flags and subcommands after it — the
`send town -s ...` in `$(verb mail) send town` — are still free text. Nor does
a matched label prove the arm behind it works: it proves km ROUTES the name,
not that the route arrives.

## require_identity — the crown is not a default

(`kingdom/bin/km`, `require_identity`)

The crown is not a default. `mail send`, the mail box selectors and `halt`
each resolved their caller by defaulting an unset KINGDOM_SEAT to the crown —
not spelled literally here, so a grep for the defect finds sites and not this
account of it (bd gqlc-ujw7). Any session
started outside km-seat — every ephemeral agent in the shared checkout, and
anyone running km from a plain shell — sent letters headed by the king, read
the king's inbox instead of its own, and stamped a Constitution VI.4 halt
with his name. Measured 2026-08-29: three of four letters in Սեդրակ's inbox
were misattributed this way, and only the human signature inside the body
disagreed. The fallback names the one identity holding powers nobody else
does, so it errs toward MORE authority than the caller has, never less.

cmd_sleep and cmd_resume already refused this condition. These sites did not,
and the refusal below is the shape cmd_resume uses — including the escape
hatch, because the crown asserts itself the way anyone else does.
CALL THIS BARE, never as "$(require_identity …)". It refuses by exiting, and
`exit` inside a command substitution leaves only the subshell: the first cut
of this fix echoed the seat, and `halt` went on to raise the town's brake with
a blank actor while returning 0 — the same fail-open in the same reserved
power, one layer down. `set -e` does not cover it either, because the status
of a substitution used as an ARGUMENT is discarded outright. So the check runs
in the caller's own shell and the caller reads $KINGDOM_SEAT itself.

## no_args — a verb that reads no argv must still refuse arguments

(`kingdom/bin/km`, `no_args`)

The dispatcher hands every verb the rest of argv, and a verb that never reads
it acts for ANY argument — including the one a careful person types FIRST.
Measured 2026-08-29 (bd gqlc-akp4w): `km deploy --help` fast-forwarded the
deployed checkout and printed no help, and `km resume --help` reached the
Constitution VI.4 identity refusal rather than the help it asked for. Ten
dispatched verbs read no argv at all, so --help was the one spelling
guaranteed to perform the action instead of explaining it. `km down --help`
closes the town; that row is a static reading, not a first-party run.

The refusal is `die`, not a silent skip. A verb that quietly ignored the
argument would still leave whoever typed it believing km had understood it —
which is the sibling defect (bd gqlc-wguoq), where a message reported the
state instead of the act and its own raiser read his halt as someone else's.

## git_at — git exports GIT_DIR into every hook environment

(`kingdom/bin/km`, `git_at`)

git exports GIT_DIR and friends into every hook environment, and with GIT_DIR
set `git -C <path>` resolves the repo from GIT_DIR rather than from <path>. km
is reachable from a hook (bd delegates mail to it), and a deploy that merged
into whichever repo invoked the hook would be the same class of accident the
deploy command exists to end — so every git call that names a repo by path
scrubs the inheritance first.

Drop the whole GIT_* namespace and re-export an allowlist, rather than
enumerating what to remove. The asymmetry decides it: a git release that adds
an auth variable breaks LOUDLY here — at the deploy that needed it, naming
itself — while one that adds a repo-selecting variable would break SILENTLY,
later, in someone else's repo. Default-deny puts the new unknown on the safe
side. The allowlist is transport only: GIT_CONFIG_* is deliberately absent,
since it can inject core.hooksPath and aliases.

KM_GIT_TIMEOUT bounds the call when a caller declares one. It is read here
rather than wrapped per call site because a caller sets it with `local` and
every git the callee reaches is then bounded, which is the property a timer
needs: deploy now runs from inside the dispatch and guard ticks (gqlc-nm7w),
and OnUnitActiveSec means the next tick cannot begin until this one ends, so
one hung fetch is a stopped town.

## main_root — stated in kingdom.toml, because a derived root answers about wherever you stand

(`kingdom/bin/km`, `main_root`)

Read from `[kingdom] root`, refusing when it is unstated, relative, or names no
directory. Nothing here consults git or the cwd.

IT USED TO. The root was derived from the git common dir of the caller's cwd,
with the `-seat-<name>` suffix stripped off the basename so a seat resolved the
shared checkout. That reads as robust and it is the defect: every path km
resolves hangs off this one, so all of them became a function of where the
process happened to stand. Both halves were reached by ordinary work, neither
by contrivance (bd gqlc-yfi2).

The WRITE half is what the bead was filed for. `deploy_root()` below is the
first caller that WRITES to whatever this returns — a fetch and a
`merge --ff-only` — so km run from an unrelated repository named THAT
repository as the tree to fast-forward. It is not hypothetical: during a
mutation screen one mutant fast-forwarded the real shared checkout through
exactly this path (`kingdom/brain/postmortems/2026-08-21-test-suite-damaged-its-own-repo.md`),
and gqlc-nm7w since put cmd_deploy inside the dispatch and guard ticks, so the
write is now performed by a timer rather than only by a command a human typed.

The READ half has no mitigation and needs only a caller in a plausible
directory, which is why the bead was repriced P3 → P2. Run from the PARENT of
the checkouts, the derivation resolved a common dir whose basename is `.`, so
seat_worktree() composed `.../.-seat-sedrak` for every seat — and `km doctor`,
the town's health oracle, then printed a FAIL naming twelve seats as
unverifiable and a warn that bd's mail delegation resolved nowhere. Both false.
Re-run from a seat worktree the same two lines read ok. A readout that lies is
a defect in its own right, and the mayor had begun drafting a bead against the
`.-seat-` path before he re-ran it correctly.

Two properties the stated key gets for free, both of which the derivation had
to work for. An exported GIT_DIR can no longer name the repo that invoked a
hook, because no git command runs here to be redirected — `git_at`'s scrub
still guards every other call site, but this one is out of that blast radius by
construction. And the seat-clone case (gqlc-w5bh) needs no inverse: a clone's
common dir is its own, so the derivation named the SEAT and the basename strip
existed to undo the `.../gqlc-seat-x-seat-y` that seat_worktree() would then
compose. A stated root is the same string from a clone, a linked worktree, or
anywhere else, so there is no composition left to invert.

The cost, which is real and is the trade the bead asked for: a clone of this
repository anywhere else must edit that line before km will run there. Prefer
refusing to guessing — a km that says it cannot tell where the town is is worth
more than one that answers about a town that does not exist.

## km_gate_paths — the paths whose content IS a gate, defined once so drift and freshness cannot disagree

(`kingdom/bin/km`, `KM_GATE_PATHS`)

The paths whose content IS a gate. One definition, read by kingdom_drift for
the deployed root and by seat_freshness for each seat worktree, so the two
cannot come to disagree about what counts as a gate.

kingdom/ is the machinery the systemd units execute. .githooks/ is what
core.hooksPath resolves to AND where Claude Code's PreToolUse hook
(claude-pre-bash) lives. .claude/ carries the registration that makes that
hook run at all — a checkout holding the hook file but not the settings runs
neither, so the two travel together.

## master_holder — why the deployed root could not be fast-forwarded

(`kingdom/bin/km`, `master_holder`)

Names why the deployed root could not be fast-forwarded; silence means it can.

CAPABILITY, which is a different question from kingdom_drift's CONTENT, and
the reason both are asked: a root parked off master has zero drift for as long
as master does not move, so the content row reads ok while the deploy path is
already refusing. Measured 2026-08-29 (bd gqlc-ssfqj): the deploy root sat on
a detached HEAD at exactly master's tip, `km doctor` printed "ok: ... match
origin/master", and `km deploy` on the same root died "parked on 'a detached
HEAD'". Two verdicts from one tree. The defect is invisible until master
moves, and what master moving does is start the town running stale machinery.

cmd_deploy refuses on this and doctor reports it, from here, so the two cannot
come to disagree about which roots are deployable.

The git-dir probe is not redundant with symbolic-ref: on a directory that is
not a repository at all, symbolic-ref also exits non-zero with empty stdout,
so without it every non-repo root is reported as "a detached HEAD" — a
measured-sounding sentence about a measurement that never happened.
Names the worktree that holds the local `master` branch, if one does;
silence means the ref is free. Read only when the deploy root is NOT on
master, so the root can never be its own answer.

## deploy_blocker — a branch ref is checked out in at most one worktree

(`kingdom/bin/km`, `deploy_blocker`)

Git checks a branch ref out in at most one worktree, and the town
shares one repo across many (33 of them, measured 2026-08-29). So a
seat that ran `git checkout master` to read master takes the ref the
deploy root needs, and the operator's obvious repair dies
"fatal: 'master' is already used by worktree at ...". That refusal is
knowable HERE; making them find it by hand is the avoidable half of
the diagnosis, and it is the half that reads as "the repair is a
one-liner" right up until it is not (bd gqlc-59n70). Measured
2026-08-29 on gqlc-ssfqj: Աստղիկ hit exactly that refusal and could
only conclude "something in the town does contend for it".

## kingdom_drift — the deployed paths where the tree differs from origin/master

(`kingdom/bin/km`, `kingdom_drift`)

Names the deployed paths where the tree differs from origin/master; silence
means no drift. It diffs the WORKING TREE, not HEAD, because that is what the
units execute: a file edited or staged in place is drift at the right commit.

gqlc-d20c widened this from kingdom/ alone. The hooks are executed out of the
main checkout exactly as the units are, and nothing advanced that checkout:
measured 2026-08-23, it sat 34 commits back while PR #1333's hook fix was
already merged, so `bd close` from a sibling worktree was refused by the
pre-fix copy and allowed by the merged one — same input, same file, two
verdicts. `git status` there was clean apart from two .beads exports, so the
condition had no tell at all and was found by comparing shas by hand.

## kingdom_drift-2 — refresh the ref before measuring against it

(`kingdom/bin/km`, `kingdom_drift`)

Refresh the ref this is about to read, because otherwise the measurement
is circular. The only fetch in a dispatch tick lives in cmd_deploy, and
cmd_deploy is reached only once drift is already non-empty — so while the
town is halted or down, and hold_fetch_master / cmd_seat_refresh /
guard-sweep are therefore not running either, nothing moves
refs/remotes/origin/master, drift measures empty, and the detector is
blind for exactly the outage across which merges accumulate. Measured
2026-08-23 (bd gqlc-iom4): the checkout sat at 43b1fbe9 while
origin/master was cd7ee956, two of the three commits between them
touching kingdom/; eight consecutive ticks logged only "the town is down"
and never DRIFT. One hand-run `git fetch origin master`, nothing else,
and the next tick logged DRIFT and fast-forwarded.

Bounded and NON-FATAL. A hung fetch is a stopped town (see git_at), and a
refusal here would stop it too — so a failure degrades to the older
behaviour of judging against whatever ref is on disk. It says so on
stderr, because rendering an unmeasured state as a measured one is the
defect this fixes, one level up. Narrow (origin master), not --all: the
ref this reads is the only one it needs.

## ensure_deployed — fail OPEN, after trying to fix it

(`kingdom/bin/km`, `ensure_deployed`)

Fail OPEN, after trying to fix it. This used to be assert_deployed, which
refused, and the refusal is what stopped the town (gqlc-nm7w): nothing in the
Թագաւորութիւն ever ran `km deploy` on its own — no systemd unit, no km-seat,
no justfile recipe, no line of citizen-protocol.md — while origin/master DOES
move under the timers, because hold_fetch_master and cmd_seat_refresh both
fetch. So any merged PR touching a gate path made every dispatch tick and
every guard tick exit non-zero into the journal, and the town routed nobody
until a human typed `just kingdom-deploy`. Measured 2026-08-23:
kingdom-guard.service exit 1 at 03:25:43, naming twenty-odd kingdom/ paths.

Both arms of the trade have now been observed here, which is what decides it.
gqlc-ed2u is stale code running behind a healthy indicator; gqlc-nm7w is NO
code running behind a healthy indicator. Routing from a tree one merge behind
is recoverable and self-corrects on the next tick; a halted town needs a
human at 3am and gets one at 9am. So the timers heal themselves and then
continue either way — but never silently, and `km doctor` and the `km status`
board still FAIL and still say DRIFT, because those are read by someone who
can act.

KM_SELF_DEPLOYED bounds the hand-off below to one hop.

## merge_blockers — the one class of local dirt a deploy may resolve by itself

(`kingdom/bin/km`, `merge_blockers`)

The one class of local dirt a deploy may resolve by itself, and the reason
--ff-only alone was a permanent stop (gqlc-n8h3). bd re-exports
.beads/issues.jsonl into the deploy root continuously, so a modified export
is that tree's STEADY STATE rather than a leftover — measured 2026-08-23 in
the shared checkout: staged-modified, 125 insertions against 92 deletions.
Nearly every commit in this repo touches that file, because every bd write is
exported and committed, so git refuses the fast-forward with "Your local
changes to the following files would be overwritten by merge" at the first
attempt where HEAD is genuinely behind. Since deploy is the remedy that
dispatch and guard both name, that is a loop with no exit.

Discarding the local export is lossless, and it was measured rather than
assumed. The Dolt DB under .beads/embeddeddolt is bd's source of truth and
the jsonl is a passive export: a fresh `bd export` of the live ledger came
back with the same 790 lines as the dirty working-tree copy and an empty
sorted diff, and every id in the COMMITTED copy was present in the export
too. The DIRECTION is the whole argument — taking the incoming version drops
nothing the DB cannot re-emit, whereas restoring the local copy over the
merged one would revert whatever bead rows other citizens pushed while this
tree stood still. The bytes are copied aside anyway, because "the database
can regenerate it" is a claim about bd, and this function should not be the
only thing standing between an operator and a file.

Scope is exactly .beads/. Everything else dirty in that tree is a human's
uncommitted work — the shared checkout is where Անդրանիկ works — so if
anything else is in the way the merge is left to refuse, naming it, and
nothing at all is set aside, not even the exports: a partial one would leave
him resolving a tree km had already edited under him.

## herdr_server_up — herdr's server, not `tmux has-session`

(`kingdom/bin/km`, `herdr_server_up`)

The herdr server is the successor to `tmux has-session` here: if it is not
running there is no town to reach.

This collapsed a three-valued world into a two-valued rc, and every caller
below turned the 1 into the positive claim "the town is down" — the same
defect gqlc-ygcfi fixed one level up at the workspace-list read (gqlc-pgbfp).
herdr already separates the three, on the exit status. Measured first-party
2026-08-30 against herdr 0.6.10, via HERDR_SOCKET_PATH — an env lever present
in the binary and absent from `herdr --help` — so the down case could be
reproduced without stopping the town's own server:

  running      rc 0, stdout `status: running` + version/protocol/socket
  not running  rc 0, stdout `status: not running` + `socket: <path>`
  cannot ask   rc 1, stdout empty, stderr herdr's own Error object

Both routine answers write 0 bytes of stderr — reproduced for an absent
socket, a regular file and a directory — so the `2>/dev/null` that stood here
was inert on every path except the one whose reason it was discarding.

## workspace_id — all three id lookups answer EMPTY for "not there"

(`kingdom/bin/km`, `workspace_id`)

The three id lookups below all answer EMPTY for "not there", and all three
feed a caller that reads "not there" as permission to create it. So the
`unreadable` direction seat_agent_status was given (above) is owed here too,
and it is carried by the EXIT STATUS rather than by a sentinel string,
because these values are passed back to herdr as ids: rc 0 with empty stdout
is a payload we read that does not hold the thing, rc 1 is a payload we could
not read. A caller that collapses the two by testing only for emptiness —
`[ -z "$(seat_tab_id ...)" ]` — re-arms the defect, because a herdr shape
change then mints one tab per seat per wake and reconcile pass, unbounded.
Verified against live herdr 0.6.10 / protocol 13 on 2026-08-29 (bd
gqlc-jvlu): every key read here is present today, so the rc is about the
NEXT shape change, not this one.

The diagnosis is on stderr because nothing else carries it. Two redirects
stood on each of these reads and only one of them belonged: python's, which
keeps a stack trace out of an operator's console, and herdr's, which threw
away the sole account of the failure that exists (gqlc-v152q). Herdr's is
gone, so the four messages below can name which of their two hypotheses it
was instead of listing both: measured 2026-08-30 against herdr 0.6.10,
`workspace list`, `tab list`, `pane list` and `pane get` each write 0 bytes
of stderr when they succeed and a JSON error object with a `code` and a
`message` when they fail, so herdr's silence above such a message is itself
the evidence that herdr answered and the PAYLOAD is what km could not read.
`$( )` captures stdout only, so herdr's stderr cannot reach the parse.

## seat_agent_status — herdr's agent_status replaces pgrep plus pane scraping

(`kingdom/bin/km`, `seat_agent_status`)

Herdr's agent_status is the answer km used to derive from pgrep + pane
grep. Values: idle | working | blocked | done | unknown. A pane with no
agent reporting carries the literal string `unknown`.

It lives at result.pane.agent_status. This read was one level too shallow,
at result.agent_status, from the herdr migration until 2026-08-29 — and a
missing key is not a parse error, so the `.get(...,'unknown')` default that
stood here answered `unknown` for every pane in the town and never once
said so. Measured on the live workspace that day (bd gqlc-jvlu): km read
`unknown` for all 16 seats while herdr reported idle, working or done for
all 16. seat_session_live therefore never returned true in its life, and
seat_runner_start's trample guard never fired, so `herdr pane run` typed
km-seat's command line into working citizens' prompt boxes and they obeyed
it. So there is no default here any more: a payload this does not
understand is `unreadable`.

`unreadable` is not `unknown`, and the gap between them is a safety
direction. `unknown` is a positive report that no agent is running, which
seat_runner_start reads as permission to launch on top of the pane.
Answering that for a payload we failed to read is exactly how the next
herdr shape change re-arms this defect silently.

## seat_session_live — a false dead ends a citizen's session, so anything short of an outright denial reads live

(`kingdom/bin/km`, `seat_session_live`)

A live SESSION is claude actually running in the pane. The status is set BY
claude's hook, so any status at all means claude is up; `done` means the
last turn finished with the process still around for more input.

Written as "everything but the one value that positively denies a session"
rather than as a list of the four live states, because this predicate's two
errors do not cost the same. A false live holds a respawn or a worktree
refresh, which the next pass retries and a human can see. A false dead ends
a citizen's session under her. So `unreadable` reads live, and so does any
status herdr adds after this was written.

## seat_runner_live — the runner check under herdr, not pgrep on a tty

(`kingdom/bin/km`, `seat_runner_live`)

Under tmux the runner check was `pgrep km-seat` on the pane's tty. Under
herdr, agent_status is `unknown` when claude isn't running — including when
km-seat is parked between wakes, which is the HEALTHY asleep state, not a
dead runner. So the coarser check WAS the right one for a while, and it is
not any more. A pane outlives its command: herdr keeps the pane after any
child exits, so once km-seat exits (crash, `exec` under a stale hooks path,
or a stray fish command completing while nobody parked afterwards) the
pane stays and this check reads alive against nothing. Measured 2026-08-29
(gqlc-2vxs): 14 of 16 seats' panes existed with NO km-seat process behind
them, dispatch skipped them all as "wake queued already", cmd_wake and
cmd_reconcile refused to respawn because both gate on this predicate, and
`km reconcile` printed "0 runner(s) respawned" with the corpses in front of
it — the town's characteristic 0-over-a-dead-query shape a third time.

So we ask what actually distinguishes dead from parked: whether a km-seat
process for THIS seat exists at all. `bash km-seat <seat>` sleeping in its
`until [ -s wake ]` loop still shows in the process table, so a parked seat
still reads alive. Only a seat with no km-seat anywhere in ps reads dead —
which is the population cmd_up's respawn was written for. The trailing
boundary `([[:space:]]|$)` stops `km-seat ar` from matching `km-seat
aramazd`; the leading `/km-seat` stops the pattern from matching
`km-seat-ox` or `km-overlap` if either is ever a seat name.

`agent_status = unknown` still means "claude is not running", including for
a healthily parked seat, so it is not the tell — the tell is process
presence, exactly as the tmux era had it. What the herdr migration bought
us is that the pane persists across a crashed km-seat, which is the whole
reason we need to distinguish presence at all.

## seat_runner_path — which km-seat a runner is actually executing

(`kingdom/bin/km`, `seat_runner_path`)

seat_runner_live answers WHETHER a runner is there; this answers WHERE its
bytes came from, which is a different question and the one nothing in the town
was asking. Read from the process's own argv rather than composed from the
roster, because argv is the only thing that records the directory its launcher
happened to be standing in. `exec` rewrites argv, so this also tracks a runner
that has re-exec'd into the deployed copy rather than reporting where it
started (Ծովինար's test, gqlc-kp3h1).

The argv element is found by matching `*/km-seat` rather than by index: a
runner appears as `bash <path> <seat>` when started through the interpreter and
as `<path> <seat>` when executed directly, so argv[1] is the path in one shape
and the seat in the other.

## seat_wake_queue_age — the age of a seat's wake file

(`kingdom/bin/km`, `seat_wake_queue_age`)

Age in seconds of a seat's wake file, empty if none. This is the town's
fallback tell for the case seat_runner_live gets wrong — a healthy km-seat's
`until [ -s wake ]; sleep 5` drains its file within ten seconds, so a wake
older than that is evidence something is not reading, and the age keeps
climbing until someone acts. Deliberately not gated on the predicate above:
the whole point of gqlc-2vxs's second finding is that whatever replaces
seat_runner_live will grow its OWN blind spot, and this cell is what catches
it. Named at the seat helpers rather than inline in cmd_status because
cmd_reconcile is the second natural caller once this bead has a partner —
leaving that partner unwritten now, but not putting the helper somewhere it
would have to be lifted.

## seat_launch_probe — the only check that reads the PROCESS rather than the intent

(`kingdom/bin/km`, `seat_launch_probe`)

---------- seat launch conformance ----------

Every instrument this town had reported on INTENT. `km cfg claude
permission_mode` answered `bypassPermissions` and was telling the truth about
kingdom.toml while fifteen of sixteen running sessions contradicted it: bare
`claude --resume <uuid>`, no --permission-mode, no --append-system-prompt, no
--model. So they froze on modals nobody was there to answer, ran without their
souls, and ran as the wrong model — three Ճարտարապետներ and three Դատաւորներ
configured as claude-fable-5 were all on the default. `km status` called them
awake and healthy for a whole day (bd gqlc-8dsa; the restart that fixed them
was ordered by Անդրանիկ).

So this reads the PROCESS. It is the only check here that does, and that is
the entire point of it: nothing else in km can tell a seat launched by km-seat
from one somebody resumed by hand.

REPORT ONLY, never remediate. Fixing a mis-launched session means ending it,
and a citizen's session is not km's to end on a timer (Constitution VI.2).

The seat's claude is FOUND by cwd. km-seat cds to the worktree before exec'ing
claude, so the seat's tree is the process's cwd — and, unlike a walk down from
the km-seat pid, that still finds a session somebody started by hand in the
pane, which IS the population this exists for. herdr knows the pane but
reports no pid, so it cannot answer this.

Ancestry then does one job only: a claude descended from another claude in the
same worktree is that session's own child, not a second session. Claude Code
forks before exec'ing its tools, and inside the fork/exec window the child
still reads exe=claude and cwd=the worktree while carrying a COPY of the
parent's argv — conforming, not merely indistinguishable. Measured over 300s
of the live sixteen-seat town on 2026-08-29: 33 such extras across 9 seats (32
caught mid-fork on their way to becoming a grep, one a citizen's own
`claude --help`), 23510 sightings, every one of them descended from that
seat's own session and not one ambiguous. Without this filter km doctor went
red at random on a healthy town, and a permanently red gate is one nobody
reads. Two INDEPENDENT claudes in one worktree still say '?': that is the
two-live-turns-on-one-tree hazard, and it is worth the alarm.

## seat_launch_probe-2 — --effort is the one axis where DIFFERENT is legal

(`kingdom/bin/km`, `seat_launch_probe`)

--effort is the one axis where DIFFERENT is legal. A bead carrying an
effort:<level> label wakes its seat at that level instead of the class
default, and Constitution V.6.2 lets any citizen write one without asking
(km-seat honours it; bd gqlc-jmwh). So equality with the class default would
go red for a seat launched exactly as the town intends, and a permanently red
gate is one nobody reads. What is asserted is what nobody may legitimately
do: launch with no level at all, or with one claude does not take. A level
that merely DIFFERS from the class default is not reported at all — it is
what a legitimate escalation looks like, and V.6.6 records depth on the bead.

When the class has no valid level configured, km-seat launches with no
--effort by design (valid_effort drops it), so absent is correct there and
the whole axis is skipped rather than inverted.

## seat_runner_start — bringing a fresh km-seat back for a seat

(`kingdom/bin/km`, `seat_runner_start`)

Bring a fresh km-seat back for a seat. Uses `herdr pane run` to launch the
runner as a CHILD of the pane's shell (fish per herdr config) rather than
`exec`ing over the shell — if km-seat exits unexpectedly under `exec`, the
pane closes with it, and herdr reaps the workspace when its last pane is
gone. As a child, a failure leaves the shell at its prompt, with the error
visible, and the pane still exists to be repaired.

`herdr agent start` was tempting but creates a NEW pane in the tab even
when one exists, so each cmd_up left two panes per tab (measured
2026-08-29). Reusing the pane herdr already made for the tab is what keeps
the "one pane per seat" invariant this town's roster and status expect.

THE RETURN IS WITNESSED, not acked. `herdr pane run` TYPES a command line at
the pane and reports on the typing; it cannot report on what ran, because
whatever has the pane's foreground reads those bytes. So its exit status was
the wrong thing to return, and returning it made a repair that had done
nothing indistinguishable from one that worked. Measured 2026-08-29 (bd
gqlc-c48e): 14 seats had been resumed by hand with `claude --resume` typed at
their panes, so the pane's foreground was claude, and every respawn's command
line went into claude's prompt box. cmd_reconcile printed "14 runner(s)
respawned" and did so again two minutes later, and again, across 77 dispatch
runs over 2h48m, while nothing was parked on any wake file. The town's
0-over-a-dead-query shape inverted: 14 over a dead repair, which is worse,
because a counter climbing is read as recovery in progress.

The trample guard above cannot cover that population and is not being asked
to. A hand-run claude carries no herdr agent record, so agent_status reads
`unknown` and seat_session_live reads that as "no session" — the guard is
blind to exactly the case that creates the hazard. The witness below is what
holds regardless of why the command line failed to become a process.

THREE outcomes, because two callers report the refusal in words: 0 the runner
is there, 1 declined without trying (a live session, VI.2), 2 tried and no
runner appeared. Collapsing 2 into 1 is what would put "$s has a live
session" in front of an operator whose seat has none.

3 seconds is a witness budget, not a timeout to tune: on 2026-08-29T15:52Z
fifteen runners went from `herdr pane run` to visible in ps within 2 seconds
of each other (sequential pids, `ps -o lstart`), so a runner that is coming
is here at once. The cost of the budget is paid only when the respawn has
already failed.

## seat_runner_start-2 — a runner comes from the tree the town deployed, never from the caller's

(`kingdom/bin/km`, `seat_runner_start`)

The runner the town DEPLOYED, not the copy sitting beside this km.
`$SCRIPT_DIR` is wherever the caller happened to be standing, so a citizen
running `km up`, `km wake`, `km reconcile` or `km dispatch` from their own seat
worktree starts every runner it spawns out of THAT worktree — and km-seat then
asks the km NEXT TO IT for the ledger, the state dir, and the seat's class,
model, effort and permission mode, so a whole seat is launched from one
branch's `kingdom/` rather than from master's.

Measured 2026-09-02 on the live town (bd gqlc-dpo3o): 10 of 16 runners were
executing `gqlc-seat-sedrak/kingdom/bin/km-seat`, that worktree parked at
e4e032a0 against master's 7dabb01a. Its km-seat was byte-identical to the
deployed one, but the km and kingdom.toml beside it were not — the config was
missing `deploy_escalate_after_ticks`, merged that day in #2066.

Two earlier fixes patched the symptoms and left this. #2136 (gqlc-zpjuc)
taught km-seat to ask `km deploy-root` for the ledger, which is why those ten
seats read the town's beads correctly rather than an empty ledger; #2170
(gqlc-mrelv) taught it to re-exec the deployed parse of ITSELF at its next
wake. Neither reaches the spawn, and the second is a backstop with three gaps
this closes: it hashes km-seat alone, so a divergent km or kingdom.toml beside
it is invisible; it cannot fire while the two copies are byte-identical, which
is the state all ten were in, so a runner sits on the wrong tree indefinitely
with nothing to notice; and everything km-seat does BEFORE its loop — the
ledger guard, seat-info, the soul and worktree checks — has already run from
the wrong tree by the time the re-exec could help.

The five answers km-seat takes from its neighbouring km were compared across
both trees that day (deploy-root, state-dir, seat-info, `cfg claude
permission_mode`, `cfg effort warrior`) and all five AGREED. So no live harm
was found, and none is claimed here. What was wrong is that nothing made them
agree.

## seat_box_text — where claude's TUI draws the input box

(`kingdom/bin/km`, `seat_box_text`)

claude's TUI draws the input box between the last two horizontal rules of the
visible buffer. Empty means what was typed has been submitted; anything else
is text still sitting there. Measured 2026-08-29 on a scratch pane, and the
render is the same shape while the agent is `working`, which send_line allows
(bd gqlc-gh7xj).

The bare prompt glyph is the empty box, not content. It is compared as a whole
trimmed line rather than stripped by a regex so that the multibyte character
never reaches one.

Exit 1 means no box was found. That is not the same answer as an empty box and
no caller may read it as one.

It was also not the same answer as a pane that could not be read, and until
gqlc-p7l0u those two shared it. The read sat inside the pipeline with its
stderr dropped, so a failing `herdr pane read` gave awk nothing, n stayed 0,
and awk exited 1 — byte-identical to "the pane rendered and had no box", with
the reason that separates them discarded. The read is taken out of the
pipeline so its own status is legible, and unreadable is a third answer.
2>&1 rather than dropping stderr: measured 2026-08-30 against herdr 0.6.10,
`pane read <bad>` exits 1 with 0 bytes on stdout and its reason on stderr —
and which stream carries a diagnosis is herdr's to change, so both are kept.

## seat_box_text-2 — which horizontal rule bounds the composer

(`kingdom/bin/km`, `seat_box_text`)

A rule is bare OR carries the tab label km writes in seat_runner_start,
which renders inside the box's TOP edge: `─── Աստղիկ ──`. Matching only
the bare form found one edge on every seat in the town and none of the
region between them (gqlc-6re91). The optional group is anchored at both
ends so a line that merely CONTAINS a rule run is not an edge: a spurious
edge shrinks the region, and a region that reads empty when it is not is
what makes send_line type onto a citizen's draft.
Matching the shape alone is not enough, because the shape has no minimum
width: a bare rule the citizen PASTED into the box — km output, a markdown
separator — is an edge at any length, and if it is the last interior line
it becomes edge[n-1], the region collapses to nothing, and a box holding
an unsent draft reports EMPTY (gqlc-eq1t1). That is the direction the
comment above calls expensive: send_line's consent pre-check allows a send
when the box reads empty, so it types a nudge onto the draft.

So width decides, and it is taken from the buffer rather than fixed,
because a constant would be this terminal's width and would rot on the
next resize. The box's own edges are drawn to the render width and are all
within 3% of each other: measured 2026-08-30 across all 16 live panes,
every edge line carried between 343 and 352 ─, the bottom edge at the full
352 and the titled top edges at 343-348 (the label eats the difference).
Half the widest rule in the buffer therefore clears a real edge by a wide
margin while rejecting a paste that is not close to full width. What is
counted is the ─ glyphs, not the length of the line, because the shape
admits a label between two rules: a pasted heading of the form `─ text ─`
can be 200 columns wide while being two dashes, and only the dash count
calls that narrow.

It does NOT reject a paste wider than half the pane. Nothing in this file
can: a full-width rule pasted from another pane's render is byte-identical
to a real edge. The limit is stated rather than papered over.

THE SHAPE IS TESTED ON ASCII, after substituting, and never on `─` itself.
`─` is U+2500 — three bytes — and under a C/POSIX locale awk has no
multibyte notion, so `─+` parses as the bytes \xe2\x94 followed by one or
more \x80. A real rule is that triple repeated N times, whose second
character is \xe2 and not \x80, so the match failed for every N > 1 and NO
line was an edge: measured 2026-08-30, all 8 fixtures read NOBOX under
LC_ALL=C and 6 of them found their box under en_US.UTF-8 (bd gqlc-yeesk,
found by Նվարդ while rebasing #1959). gsub of a multibyte LITERAL has no
quantifier and is bytewise, so it is locale-independent; counting with it
first and testing an ASCII shape after gives the same answer everywhere.

Not live when it was found — the units inherit LANG=en_US.UTF-8 from the
systemd manager — but km pins no locale anywhere, so it is one absent
environment variable from town-wide, and since #1959 "no box" REFUSES a
send rather than merely degrading a witness. Every send in the town would
have been refused, silently and with no report of why.

`dashes > 0` is not redundant with the width test below. Without it a line
of ASCII hyphens is a candidate carrying zero dashes, and where the buffer
holds no real rule the widest is also zero, so `0 * 2 >= 0` promotes it:
two `--------` lines in a pane with no composer at all then report an
EMPTY box, which is the single answer that makes send_line type (VI.2).

## seat_box_text-3 — a located region is not a located composer

(`kingdom/bin/km`, `seat_box_text`)

Everything above decides WHICH lines are edges. This decides what to answer
when the region between the last two of them is not a composer's interior.
Until bd gqlc-mfjgl the answer was rc=0 and an empty string — the same answer
as a genuinely empty box, which is the one answer that makes send_line type
(VI.2). So the function was safe exactly when it found NO box, and unsafe when
it found a box whose contents it could not locate. That is backwards for a
guard, and the width heuristic above cannot close it: `## seat_box_text-2`
already concedes that a full-width rule pasted from another pane's render is
byte-identical to a real edge, and nothing in this file can reject one.

MEASURED 2026-09-02 by extracting the function and feeding it synthetic
buffers, herdr stubbed:

  a real composer holding km's ask       rc=0 box=[❯ [km] an ask nobody submitted]
  two ADJACENT rules, nothing between    rc=0 box=[]     <- was a lie
  real box, then one extra rule below    rc=0 box=[]     <- was a lie
  no composer rendered                   rc=1 box=[]

Row 3 is the shape that bites: the pane holds a composer with a stranded ask
in it and ONE further rule below, so edge[n-1] and edge[n] become that box's
bottom border and the stray rule, with nothing between them. Row 2 is the same
fault reached differently — adjacent edges make the print loop's range empty.

THE DISCRIMINATOR IS THE PROMPT GLYPH, and it is not a widening of the edge
rule: which lines are edges does not change. A composer's interior always
carries a line whose first character is `❯` or `>`, because the bare glyph IS
the empty box (`## seat_box_text`, above). Measured across all 16 live panes
the same day: 12 rendered a box, and every one of the 12 had exactly one
interior line carrying the glyph — 11 bare and one holding raffi's stranded
ask. Replaying both versions of the function over those 16 buffers moved no
verdict, so the fail-closed arm is inert on the healthy population.

Absent that glyph the region is not the composer, and the function now exits 2
with its own sentence on stderr. It shares rc=2 with a pane herdr could not
read because every caller wants the SAME direction from both — refuse, do not
claim — and a distinct code would have bought four more arms that all did the
same thing. What the two causes do not share is the sentence, so the callers
no longer say "herdr's own account of it is above" when the account above is
km's; they say "the account of it is above", which is true of both.

THE LIMIT. A render with no prompt glyph in the composer would now be refused
rather than typed into. That is the intended direction and it is loud — every
refusal prints why — but it is a real behaviour change and no such render has
been observed. If one exists, the repair is to widen the glyph set here, never
to restore the empty-string answer.

WHAT THIS DOES NOT ESTABLISH. It does not prove the raffi incident of
2026-09-02, where two of km's asks accumulated in one composer. That pane was
re-read while taking this fix and by then held ONE ask, hand-delivered by
Սեդրակ, which the function reads correctly (rc=0, non-empty) and which
send_line would refuse. The incident buffer is gone: a composer's past states
are not in the scrollback. So this closes a measured fail-open path that COULD
produce that accumulation; it does not demonstrate that it did.

## seat_wedge_reason — why a tool call has not come back: MARK, never act

(`kingdom/bin/km`, `seat_wedge_reason`)

Why a tool call has not come back, when the pane says so. MARK, never act:
nothing downstream consumes this word, it goes into the WEDGED line so a
citizen can decide whether the pane is worth twenty seconds (VI.2). The
recovery ladder that may act on the same states is gqlc-3evsn's, not this.

It reports what was on the screen at the moment of the read and nothing else.
`visible` is the only source herdr 0.6.10 accepts — `scrollback`, `history`,
`full`, `buffer`, `all` and `screen` are all refused with `invalid read
source` (measured 2026-08-30) — so a modal answered a second before the read
leaves no trace here at all.

WHICH SAMPLES THIS WAS BUILT FROM, and the limits of that:
  - the usage-limit wall, 14 first-hand captures across every other seat in
    the town on 2026-08-30, all reading `You've hit your limit · resets
    11:50pm (America/New_York)` over `/extra-usage to finish what you're
    working on.` gqlc-3evsn's earlier capture of the same wall separates the
    clause with a hyphen where these use `·`, which is exactly why the match
    is on the two fixed fragments and not on either whole line.
  - the consent modal, from the capture filed on gqlc-5ojel (Աստղիկ, frozen
    1h54m): `Dangerous rm operation ...` over `Do you want to proceed?` over
    `> 1. Yes  2. No`. That is one heuristic's wording; the harness's set of
    modals is not enumerable from outside it, so the match is on the SHAPE
    the dialog shares — an interrogative line plus a numbered option list —
    and deliberately not on `Dangerous`. Both are required, because prose
    asking a question is common and prose asking a question above a numbered
    list is not.
  - a tool genuinely in flight, first-hand from this seat's own pane during a
    Bash call: a spinner line and `Running 1 shell command…`, carrying
    neither marker. That is the `unknown` arm, and it is the arm that says km
    cannot tell a slow command from a hung one.
A modal nobody here has seen will read `unknown`. That is the honest answer
and it is the answer km gives today for every state.

Always exits 0, and the failures are words rather than statuses. km runs
`set -e` and the caller interpolates this into an assignment at statement
level, where a non-zero return ends cmd_status mid-table (bd gqlc-nvbd3).

## seat_wedge_reason-2 — the lower marker wins, and each state needs both of its fragments

(`kingdom/bin/km`, `seat_wedge_reason`)

The LOWER marker wins, not the first one found. A pane holds the whole
visible screen, so a modal that was answered minutes ago is still up
there above whatever the seat is doing now; the state the operator is
being told about is the one nearest the prompt.
Two fragments for each state, both required, and the position of a state
is its lower fragment. Either fragment alone is prose a citizen could
plausibly write — `/extra-usage` and `Do you want to` both appear in this
repo's own bead descriptions — so one of them on its own buys a false
positive in exchange for nothing, `unknown` already meaning "look".

## seat_box_emptied — a read that finds no box is a verdict, not a turn to skip

(`kingdom/bin/km`, `seat_box_emptied`)

A read that finds NO BOX ends the poll, with the same verdict the loop used to
reach only if its LAST read happened to find none. It used to `continue`
instead, which let a single later read overturn every earlier one — and the
later read is not a second look at the same box. It is a look at a different
screen.

This is the mechanism behind gqlc-hrft2 and gqlc-4b0cc: a `/exit` that WAS
submitted, reported by `send_line` as "the box still holds text" across roughly
4.2 seconds of polling, on a seat that had plainly exited. Three measurements,
2026-09-02, and the first two are the ones that settle it:

- **A delivered `/exit` leaves the pane with no box at all, and keeps it that
  way.** Captured every ~100ms for 10s across a submitted `/exit` in a
  throwaway tmux session: the frame before the Enter reads the box holding
  `/exit`, and every one of the 100 frames after it reads NO BOX. Confirmed
  through km's own reader too — a seat's pane after its session ends carries no
  horizontal rule anywhere in the visible buffer, so `seat_box_text` answers 1.
- **A freshly started claude renders a NON-EMPTY box over an EMPTY composer.**
  Its placeholder: `❯ Try "edit <filepath> to..."`. Nothing distinguishes that
  from a citizen's draft at the byte level, because it is text in the composer.
- The poll spans both. `km-seat` relaunches claude in the SAME pane on the next
  queued wake, so nine reads can witness the composer gone and the tenth catch
  its successor's placeholder. The 4.2 seconds were never one box read twenty
  times.

So the old code returned "holds text" for a message that had been delivered,
and `send_line` then pressed its second Enter into whatever now occupied the
pane — a different session's composer — before reporting "Nothing was
delivered". Both halves are wrong in the direction that costs most: the wake is
re-sent, and a stray Enter is submitted into a session nobody was addressing.

The direction of the verdict is the argument for the change. A box that has
VANISHED is the strongest evidence available that the text left it, since
typing only ever appends and nothing km does clears a composer. `send_line`
already draws exactly that inference on the two weaker premises next to it —
the unreadable-pane and no-box arms both report delivered — so this makes the
poll agree with the function that calls it.

What it costs, stated rather than dismissed: a transient failure to parse the
box while the text is still in it now reports delivered instead of polling on.
Nothing measured produces that transition inside a live session — the observed
no-box states are session boundaries, and the pre-send check at the top of
`send_line` already treats a no-box read as decisive — but the sandbox that
says so is small, so it is a risk taken knowingly and not one ruled out.

`seat_box_echoes` still has the `continue`, deliberately, and it has TWO call
sites rather than the one it is easy to see. At km:983 it runs BEFORE any
Enter, and the failure it feeds ("something covered the composer mid-send")
presses no key, so a late read that overturns nine earlier ones costs nothing
there. At km:1035 it runs after BOTH Enters, and every non-zero verdict it can
return — including the no-box one — lands in the same arm and reports
delivered, so the overturning read cannot change the outcome either. Neither
site has this bug; whether they should match anyway is gqlc-w63bv.

That second site does carry a smaller inaccuracy, filed on the same bead: its
message says "the box is not empty — but what it holds is NOT this message",
which is a sentence about rc 1 being printed for rc 2 as well, where there is
no box to hold anything.

Not the cause, checked and cleared: gqlc-051cj's normalisation asymmetry —
`box_one_line` folds U+00A0 to a space and `seat_box_text`'s awk trims only
POSIX whitespace — is REAL and does turn a composer rendering glyph+U+00A0 into
a box that reads as occupied. It was measured through `tmux capture-pane`,
which preserves that byte. It is not reachable through the reader km actually
uses: `herdr pane read` returns an idle composer as a bare glyph, on five live
seats sampled across two days.

## box_one_line — one comparable line out of what the box renders

(`kingdom/bin/km`, `box_one_line`)

One comparable line out of what the box renders. Measured 2026-08-30 on a
probe pane (bd gqlc-3evsn): a message too long for the pane is wrapped at WORD
boundaries — 350/342/237 characters over three lines for one 829-character
send, not a fixed column cut — and seat_box_text trims each rendered line, so
rejoining them with a single space reconstructs the text. Comparing the raw
multi-line capture against what was sent would therefore refuse every message
wider than the pane.

The prompt glyph leads the first line and is followed by U+00A0, which is NOT
[[:space:]] in the C locale, so it is spelled as bytes rather than left to a
character class. Both sides of any comparison go through this, so a text that
itself begins with a glyph is normalised the same way and cannot drift.

## seat_box_echoes — did the text we just typed land in the composer

(`kingdom/bin/km`, `seat_box_echoes`)

Did the text we just typed actually land in the composer? Polled on the same
schedule and for the same reason as seat_box_emptied: typing takes about two
pane reads to render, so a single immediate read would answer "no" for a send
that was fine.

CONTAINMENT, not equality, and the box having been witnessed EMPTY moments
before is what makes that strong enough: nothing else can be in there to match
against, so any decoration the TUI adds around the text costs a refusal rather
than buying a false pass. Equality would refuse the first time claude renders a
hint inside the box, and a false refusal unroutes a live seat — the failure
gqlc-gh7xj went out of its way not to introduce.

## km_authored_text — the whole corpus of what km types, as a predicate

(`kingdom/bin/km`, `km_authored_text`)

Every string km types into a citizen's composer is either `/exit` or carries
the `[km] ` prefix. That was already true when this predicate was written; what
it adds is that it is now CHECKED, at the one place text enters a pane, so it
cannot be quietly falsified by a new send site (bd gqlc-cj7hp).

IT IS A TEST ON ONE STRING AND MUST STAY ONE. `send_line` uses it as a REFUSAL,
on text km is about to type, so a version answering yes when any line of a
buffer matched would let a multi-line ask through on the strength of its second
line. The buffer question — who typed what is already sitting in a composer —
is a different question with a different safe direction, and it belongs to
`box_authorship` (bd gqlc-fb8ip).

THE CORPUS WAS MEASURED, not remembered. Extracted from the source on
2026-09-02: five templates — `/exit` from two call sites, and four `[km] `
asks. `send_line` is the only thing in the whole repository that types into a
pane, over `kingdom/`, `.githooks/`, the justfile and every `*.sh`, `*.py`
and `*.go`; `km-seat` hands the wake queue to claude as argv and so cannot
strand text in a box. The falsifier that would have sunk this — an older km
that sent unprefixed asks, whose text could still be sitting in a box today —
was looked for and did not exist: `git log -S'[km] ' -- kingdom/bin/km`
reaches the founding commit c08fc7f0.

WHAT THE INVARIANT BUYS is the ability to rule km OUT. Seven stranded,
unsubmitted strings had been read out of live panes by 2026-09-02 — four in
gqlc-cj7hp's own filing, then `merge it once the screen confirms` (33) on
Վահագն's pane at 15:32Z, and `go with the design bead, dont bother with the
cheap amendment` (61) and `merge it when CI is green` (25) on Սեդրակ's and
Հայկ's at 15:44Z. Not one matches the corpus, and the longest is 61 characters
against gqlc-gh7xj's threshold of 64 — so gh7xj, km typing a burst claude
renders as a paste and never submits, explains none of them. Without a checked
corpus that reads as an impression about phrasing; with one it is a
measurement, and it is what `seat_box_state` reports on.

DO NOT INVERT THAT. 61 is under 64 by three characters, so the next specimen
may well be longer, and a string over 64 is not thereby km's — the corpus is
the test, and length is only the reason gh7xj cannot be the answer for these
seven. The two later specimens sharpen what the sender is doing rather than
who it is: each is an imperative granting a decision on that seat's actual
in-flight work — Սեդրակ was weighing a design bead against a cheap amendment,
Հայկ was waiting on CI — which agrees with astghik's content-coupling finding
and rules out a fixed script on a timer. `dont` for `don't` is the kind of tell
a keyboard leaves and a template does not, and it is offered as exactly that:
a tell, not a finding.

IT IS NOW A FINDING, and it settles who the other sender is (bd gqlc-cj7hp,
2026-09-02). `herdr agent send` writes literal text and presses no Enter —
herdr's own help says so — so a string left sitting in a composer is exactly
what that verb leaves behind, and a person typing at the herdr TUI leaves the
same thing. The paragraph above is right that nothing in this REPOSITORY types
it; the step it was missing is that the repository is not the only thing on
this machine. Measured the same day: the interactive herdr client is pid
1510441 on `pts/0` with STAT `Sl+` — the foreground process group of a terminal
— parented `herdr <- fish <- ghostty <- Hyprland`, and `herdr-client.log`'s
attach windows cover every stranding anyone has measured. An eighth specimen
that afternoon, `take the next ready warrior bead` in Այգ's box, was
byte-identical to one Ար had read in a different pane 13.5 hours earlier, which
softens the content-coupling argument without overturning it: a supervisor with
a handful of standard directives produces repetition AND apparent coupling,
because the standard directive is the apt one.

DO NOT SPEND A SESSION HUNTING THE SENDER IN herdr's LOG. It records a send
only when the send FAILS. Of 458 `api.request.complete` events in
`~/.config/herdr/herdr-server.log` since 2026-06-13, the 136 with
`outcome="ok"` are all `tab.create`, `agent.start`, `workspace.*`, `tab.*`,
`pane.release_agent` and `agent.focus` — not one send; `agent.send` appears 172
times and `pane.send_keys` 79, every one an error. Confirmed against a
known-good event: Սեդրակ delivered three nudges at 14:43Z and the log holds no
api line between 14:40 and 14:49. A delivered send leaves no trace by design,
so no after-the-fact attribution exists to be found. What the log DOES witness
is operator presence: `tab.focus` is logged, and exactly one of its 376 events
was CLI-originated.

None of that makes the sender's instructions wrong — the ones legible on the
panes were apt, given at the right moment. The defect is ours: the only
hand-path into a composer leaves the text unsubmitted, a boxed seat is
unreachable by wake, nudge, sleep and down at once, and nothing tells the
person who typed it. `km wake <seat> --reason "<text>"` is the same sentence
delivered with a receipt, because it routes through `send_line`.

The prefix is therefore load-bearing prose, not decoration. Anything that
gives km a new thing to say gives it the prefix, or it is refused at
`send_line` with the reason above.

## send_line — herdr writes the text and send-keys writes the Enter

(`kingdom/bin/km`, `send_line`)

`herdr agent send` writes the text and `pane send-keys` writes the Enter, and
both go down the pane's pty as ordinary bursts. There is no socket path that
bypasses the tty — this comment claimed one until 2026-08-29 — and herdr emits
no bracketed-paste markers even when the application has enabled DECSET 2004,
so claude cannot tell either call from a human typing and falls back to
guessing by burst size. Measured against herdr 0.6.10 with a raw-tty reader in
a scratch pane (bd gqlc-bi7n): the two writes arrive as SEPARATE bursts while
the far side is sitting in read(), and coalesce into ONE when it is not.

That matters because claude takes a single burst of 64 characters or more as a
paste and renders its trailing CR as a literal newline. So a coalesced pair
carrying a long message is typed and never submitted, while both calls return
rc=0. On a live pane, one burst of 63 characters submits and one of 64 strands,
6 of 6 trials. The nudge text every caller here sends is well over 64.

The failure is therefore a race on the far side's read loop, not a property of
a pane or a seat: 26 of 26 back-to-back sends submitted against a live claude,
idle and busy alike, so the rate is low and is NOT characterised. That rate is
why the acks are not merely incomplete but untestable by repetition — a run of
uniform successes is what the defect looks like — so the send is witnessed
rather than retried: seat_box_emptied reads the box back, and a box that still
holds the text gets one lone Enter, which submitted it 3 of 3 (bd gqlc-gh7xj).

The signature keeps the `<target>` shape
callers used with `send_line "=$SESSION:$seat"`, but the target may just be
the seat name; the wrapper accepts either.

It is addressed by PANE, not by seat name. `herdr agent send <target>` takes
terminal ids, unique agent names, agent labels and pane ids — and a seat name
is none of those. km labels TABS with the seat name, in seat_runner_start,
and never once calls `herdr agent rename`, so no agent in this town has ever
carried a seat name. Measured 2026-08-29 (bd gqlc-nq78): all 16 agents are
named the literal `claude`, `agent get aramazd` and `agent get sedrak` both answer
agent_not_found, `agent get claude` answers agent_target_ambiguous, and the
pane id for that same seat resolves. So this call failed for every seat, in
every state, from the herdr migration until that date — and its three callers
each reported success anyway.

The diagnosis is no longer discarded. herdr signals the failure by exit
status and puts WHICH failure in a JSON payload on STDERR — measured against
the deployed binary 2026-08-29 with an unresolvable target: rc=1, stdout 0
bytes, stderr the {"error":{"code":"agent_not_found",...}} object. A caller
reading only the status can distinguish "not delivered" from "delivered" and
nothing finer, and a dispatch tick that died on this call spent forty minutes
saying nothing at all (bd gqlc-87al). Both streams are captured anyway and
re-emitted on stderr: which stream carries a diagnosis is herdr's to change,
and 2>&1 costs nothing to be wrong about.

And `agent send` TYPES; it does not SUBMIT. It returns ok with the text left
sitting in the agent's input box, where it stays until something presses
Enter. Both halves witnessed first-party on this seat's own pane, 2026-08-29
(bd gqlc-nq78, verdict-qo4s-r2): `agent send <pane> <marker>` answered
{"type":"ok"} and a following `pane read` showed the marker after the prompt,
unsubmitted; then `pane send-keys <pane> Enter` submitted it and it arrived
as a message in this session. `agent send` takes no flags on the deployed
binary, so there is no submit option to ask for instead. An ack is delivery
of TEXT, and every caller here wants delivery of an EFFECT — a seat asked to
/exit that does not leave is the account-vs-work defect a second time.

The Enter is guarded, and the guard sits in the primitive rather than in the
callers because the hazard belongs to the mechanism: Enter on a modal presses
a button nobody consented to (Constitution VI.2), and cmd_reconcile — the one
caller with no status check in front of it — sends to seats that may be
sitting on a permission ask. seat_prompt_visible already draws the line the
nudge path uses; one guard here covers every caller, including the next one.

CONSENT IS OBSERVED HERE, not inferred from an enum. Until 2026-08-30 the only
thing standing between this function and an Enter on a modal was
seat_prompt_visible, whose argument was that a covered seat reads `blocked`.
That premise is falsified: nine seats killed by a usage-limit wall read
done/working/unknown and never blocked (bd gqlc-3evsn), and this function
admits all three. What kept km off those panes was not the guard below but an
accident of a DIFFERENT gate — the dispatch ladder's liveness test, which
required `idle` and so never reached them.

THAT ACCIDENT IS GONE as of bd gqlc-ymbw0: the liveness test admits `done` too,
because `done` is the steady state a finished seat rests in and requiring
`idle` made the ladder blind to the whole gqlc-5vp7 population. Quota-killed
panes read `done` among other things, so widening it points the ladder at them
for the first time and the checks below are now the only thing refusing those
panes — which is the job this function was given here, not a new load on it.
Not witnessed on a live quota modal: none existed in the town when the widening
was measured. What is measured is a trust dialog (rc=1, above); the argument
that it carries is the allow-witness design — a surface with no empty composer
is refused whatever covers it — and that is an argument, not a second reading.

So the enum check is kept for the modals the hook does see, and the decision is
made instead on the pane's own render, twice:

  BEFORE typing — an input box must exist and be EMPTY. No box is a refusal,
  not a shrug: there is nowhere for the text to land and the Enter would press
  whatever button that surface does have (Constitution VI.2). A box holding
  text is a refusal too — typing APPENDS, so a citizen's half-written draft
  would be silently prefixed to this message and submitted with it, which was
  bd gqlc-6bnkw and is now answered rather than only documented.

  AFTER the send acks and BEFORE the Enter — the box must hold what was typed.
  The pre-check is one photograph; a modal arriving in the gap between it and
  the Enter is the case this narrows, and it is also the only witness that the
  ack meant anything on this pane.

Measured 2026-08-30 on a live claude trust dialog: agent_status `blocked` AND
seat_box_text rc=1. The two witnesses agree where they overlap, which is why
the box check is an ALLOW-witness that a composer exists rather than a
deny-list of modal signatures — a list fails open on the modal nobody wrote
down, and the quota modal is exactly that modal.

Every refusal returns 1 with a reason on stderr naming WHICH witness failed.
Callers count refusals: a send this function cannot witness is the verdict
"machinery cannot reach this seat", and retrying it silently forever is the
open loop gqlc-3evsn indicts.

## send_line-2 — `|| witnessed=$?` rather than a bare call

(`kingdom/bin/km`, `send_line`)

`|| witnessed=$?` and not a bare call: a bare one is an untested failing
command, so `set -e` unwinds send_line at the exact moment it has something
to say and the next line is what would read it. Every caller today happens
to sit inside an `if`, which suspends -e for the whole dynamic extent and
hides this — the refusal below was witnessed reaching nobody, with no
diagnosis on either stream, the first time it was driven from a plain call
(gqlc-3evsn). The same shape has taken this file twice before, once eating
`km sleep`'s message (gqlc-7bps) and once killing 19 dispatch ticks
outright (gqlc-87al), so the fix belongs here rather than in the callers.

## send_line-3 — unreadable is delivered too, and it is a different sentence

(`kingdom/bin/km`, `send_line`)

Unreadable is reported as delivered too, but it is a different sentence
because it is a different thing to have seen: above, a pane that rendered
with no box in it; here, no reading of the pane at all. Delivered rather
than not, on the same reasoning as the branch above and one more: `km
sleep` sends `/exit` and prints "the /exit was NOT delivered" on a
non-zero return, and a pane that has gone away between the Enter and the
read is most easily explained by that /exit having worked. That is a
judgement about which way to be wrong, not a measurement (gqlc-p7l0u).

## seat_pane_idle — idle is herdr's report, not km's own pane scraping

(`kingdom/bin/km`, `seat_pane_idle`)

Idle == herdr's report says the seat is at an empty prompt, AND the seat's
visible pane buffer did not move across a 3-second gap. The enum arm is still
herdr's report and not km's own scraping — no reconciling "esc to interrupt"
strings, no false-idle on a mid-tool-call. The second witness IS a read of the
screen, but it compares the screen to ITSELF rather than matching chrome.

THE SECOND WITNESS, and why the enum alone was not enough (bd gqlc-y8puo). A
seat whose turn ended with a BACKGROUND shell still running reads `done`: herdr
derives its state from the screen and cannot see the shell, while Claude Code
re-invokes the agent the moment the shell finishes. So the most careful work in
the town — a long mutation battery parked in the background — was the work most
likely to read as a dead seat, and the ladder's remedy is `km sleep`, which ends
the session. Priced on what can be DESTROYED rather than what is held up.

The bead's own stated mechanism was wrong and is worth not re-deriving: it said
a LONG shell reads idle. It does not. Measured 2026-09-02 by sampling
`km seat-idle` ten times at 3s intervals from inside a single 30s FOREGROUND
call — not-idle on all ten. Foreground work is `working` throughout. Only the
background case reaches `done`.

**THE PANE-DELTA WITNESS DOES NOT COVER THAT CASE, and it is the one thing the
witness it replaced got right.** A seat parked at an empty prompt over a running
background shell repaints only if its chrome does, and whether that chrome ticks
was NOT measured — on 2026-09-03 no seat in the town was in that state, so there
was nothing to read. What was measured first-party that day is the neighbouring
fact, which is why the gap is 3 seconds: a LIVE turn's footer ticks at least
once a second (`3h 42m 35s` -> `3h 42m 38s` across one gap on nvard's own pane),
so a working seat reliably reads moving. The regression is declared in km's own
header rather than argued away here, and it is bounded by `stalled_age`, which
comes from seat_dry_age and is wholly independent of this function.

A DEAD SEAT IS STILL REACHED, and this property survives the replacement for a
different reason than before. A killed session's pane is frozen, so it reads
still, so it reads idle, so the ladder reaches it — where the old witness got
there by finding no children under a pid that no longer existed. A witness that
answered not-idle for everyone would silence the ladder town-wide; the harness
row that guards this one is the resting-seat row, and it dies when the still/
moving comparison is inverted.

### history: why the process witness was replaced (bd gqlc-39nk9, gqlc-yq1vg)

Until 2026-09-03 the second witness was `seat_work_running`: does a claude
process whose cwd is the seat's worktree have a live child. Its premise — "a
claude process at rest has no children" — is false, and the measurement once
cited for it was a snapshot that did not generalise.

Sampled twice on 2026-09-03, minutes apart: 15 live seat sessions of which 7
held at least one child, then 14 of which 6 did. EVERY child observed on either
pass was a DAEMON rather than work — gopls on every seat that held anything at
all, aged 45m to 31.7h; a node language server on 4; background shells up to
30.6h old, four of them on one seat. A language server attaches on the first Go
file and never leaves, so what the predicate actually tracked was session age.

Both samples are recorded because the population moved between them — one
session ended in those minutes and took its gopls with it. That is the same
error the original paragraph made: any single count is a snapshot, and writing
one as a property is what put a false sentence here the first time.

It failed in both polarities, and the fail-silent one was the common case:

- FAIL-TOWARD-SILENT, near half the town. A seat holding a language server
  reported work forever, so seat_pane_idle was permanently false, so cmd_dispatch
  not only never nudged it but CLEARED its five idle markers every pass. Such a
  seat could not accumulate an idle episode at all.
- FAIL-TOWARD-NUDGEABLE. A thinking turn is not a process: streaming and
  extended thinking run inside claude itself, so a seat mid-thought had zero
  children and read exactly like a dead one.

The single live tool call seen on either pass is the sharpest form of the defect
rather than a counter-example: it sat on ayg beside a gopls and a node server,
so the predicate answered `working` for a seat that genuinely was — and answered
identically for the same seat on the pass where no tool call was running. The
answer did not move with the thing it claimed to measure.

Two repairs were considered and rejected before the replacement. An allowlist of
shell shapes rots as Claude Code's chrome moves; a denylist of daemons rots
faster, and nobody writing one on 2026-09-02 would have had `node` on it, which
4 seats held a day later. claude's own CPU time does not separate them either:
over 10s across 12 seats, seats with and without children interleaved throughout
the range.

BOTH `idle` AND `done` MEAN THAT, and taking only `idle` made this predicate
blind to the steady state (bd gqlc-ymbw0). Measured 2026-08-30 against the
live town: `done` is what a pane reads once a turn has COMPLETED under
herdr's observation, and `idle` is the transient before a session's first
turn — pane 15 went done -> idle -> working and pane 16 unknown -> idle ->
working, each `idle` window lasting seconds. So a seat that has been working
all day and stops is `done`, permanently, and `idle` is essentially reachable
only just after a session starts. That is backwards from what this ladder
needs: the seats of gqlc-5vp7 had FINISHED, which is precisely `done`.

The claim this replaces — that claude's herdr hook "fires at the states
claude passes through" — is not how the value is produced. The installed
integration (~/.claude/hooks/herdr-agent-state.sh, HERDR_INTEGRATION_VERSION=5)
reports only a session id and no state at all; herdr derives the state from
the screen against its own manifest. That manifest emits `working`, `idle`,
`blocked` and `unknown` and has NO rule producing `done` — `done` is layered
on by herdr above detection, which is why `herdr agent explain` on a `done`
pane answers `state: idle`. Do not read one of these two values as more
authoritative than the other; they answer different questions.

WIDENING THIS POINTS THE NUDGE LADDER AT QUOTA-KILLED PANES, deliberately, and
whoever narrows it again should know what they are restoring. Seats killed by a
usage-limit wall read done/working/unknown and never `blocked` (bd gqlc-3evsn),
so before this change the `= idle` here was — by accident, not by design — the
only thing keeping km from typing at them. send_line is where that is now
refused, on the pane's own render rather than on this enum; read its comment
before treating this predicate as a safety boundary. It is not one. It answers
"has this seat stopped", and the ladder's other rungs answer the rest.

## seat_box_state — not whether a seat is boxed, but by whose text

(`kingdom/bin/km`, `seat_box_state`)

An idle seat's composer is not always empty, and the difference decides whether
anything can still reach the seat. send_line refuses a send into a box it has
PROBED and found holding a draft — typing appends, so it would paste onto the
end of a citizen's draft and submit the pair as one message (gqlc-6bnkw) — and
cmd_wake, cmd_sleep, cmd_down and dispatch's recovery ask all route through it.
So a seat holding a real draft is unreachable by all four transports at once
while seat_agent_status still says `idle` (gqlc-gv7dw).

READ, THOUGH, IS NOT HELD. A box read non-empty was that refusal's whole verdict
until gqlc-2ohck, and gqlc-i8dlp reproduced a GHOST — pixels over an empty
composer — on a seat that was responsive throughout, which cost the mayor about
an hour of every transport at once. The verdict is now an ACT: type one
character, ask what the box holds, Backspace it away. A ghost has its rendered
line replaced, a real composer appends. It is fail-closed, so every state it
cannot establish refuses exactly as before. `seat_box_state` below is unchanged
by that and is still a read, so its tag is a claim about pixels; the send path
is where the claim gets tested.

THIS ANSWERS BY WHOSE TEXT, which the predicate it replaces
(`seat_box_holds_text`, yes/no) could not. The tag narrows how the text got
there without settling it, and the narrowing is only worth anything because
km's own corpus is now checked at the send — see `## km_authored_text`. `km` is
text km itself types, so read gqlc-gh7xj first: km's own long nudge is read as
a paste and its trailing CR renders as a literal newline, which strands it.
`foreign` is a box no line of which carries either shape, so no code path in
this town typed any of it — it is a citizen composing right now, or the second
sender of gqlc-cj7hp. `mixed` is both in one box.

`mixed` WAS ADDED AFTER THIS PARAGRAPH WAS FIRST TRUE OF ONE LINE ONLY. Until
bd gqlc-fb8ip the box was flattened by `box_one_line` before the corpus test
ran, and that test matches a PREFIX, so it could only ever see the first line.
Սեդրակ measured the cost on raffi 2026-09-02: a composer holding a three-word
draft above two of km's own `[km] ` asks was tagged `foreign`, over which the
status prose asserts that no code path in this town typed it. Classification
now runs per line, in `box_authorship`, and `mixed` is a third answer rather
than a lean toward either — it is the case whose remedy differs from both, an
ask of km's stranded above a citizen's draft that must not be destroyed to
clear it. It is also the direct evidence that km's asks strand and ACCUMULATE:
a second ask was delivered into a box the first had already made non-empty.

THAT RE-READ IS DONE (bd gqlc-nypdt, Այգ 2026-09-03) AND gqlc-cj7hp's COUNTS
STAND. Cite it for its numbers again. The correction landed somewhere the
filing did not expect, so the reason matters more than the verdict, and the
reason is a DIRECTION rather than an absence.

The old tag is applied in cj7hp four times, not once — the three-seat witness
line at 15:44Z, Այգ at 16:13Z, Աստղիկ's pair at 16:40Z (the two live arrivals
among the nine), and Նուարդ at 16:43Z — and for Միհր's 16:38Z string it is the
only attribution recorded. So the re-count cannot rest on the tag being absent
from the evidence. It rests on the tag being unable to err the way that would
matter: classifying by the first line can hide km's own lines UNDERNEATH a
citizen's, so a box that is really `mixed` reads `foreign`, but no line carrying
the `[km] ` prefix can read `foreign`. The defect over-reports `foreign` and
cannot manufacture one. Every string cj7hp quotes was quoted as the box's
content — its first line — so each quoted attribution is sound, and what the
defect could have concealed is km text BELOW a line nobody quoted.

The corpus is the independent check, and it excludes as a CLASS rather than
string by string, which is stronger than the per-string grep this note used to
claim: km's whole send corpus is `/exit` plus four asks each carrying the
literal `[km] ` prefix, and `git log -S'[km] '` reaches the founding commit, so
no era of km sent unprefixed asks and any box text without that prefix is not
km's on sight. Two of the nine were additionally grepped individually; the class
exclusion is what covers all nine.

An earlier version of this note said the tag appeared once and that each of the
nine strings had its own corpus grep. Both were false against cj7hp's own
record, and Անահիտ caught them in review (gqlc-xzn7a round 1).

What was measured, both halves against a positive control:

- The live population at 06:56Z, ten boxed seats read one pane at a time
  through `seat_box_text`: every box ONE content line, every one `foreign`. On
  a single-line box the two classifiers are identical by construction — "the
  first line" and "every line" are the same line — so the mis-tag has zero
  incidence there.
- The historical record, which exists because the town's readers ran their
  probes inside tool calls and the raw output survives in the transcripts. 108
  executed pane and box reads; 14 composer regions recoverable; 2 of those are
  reconstruction artifacts, tool outputs that concatenated several panes' dumps
  so that "the last two rules" span two screens. Both classifiers agree on all
  14. Zero disagreements.
- THE CONTROL, without which that zero means nothing: fb8ip's own raffi
  composer, replayed verbatim through the same two classifiers, reads `foreign`
  under the old one and `mixed` under the new. The instrument can see a mixed
  box, and it did not find one.

That specimen is outside cj7hp's population twice over: measured at 22:4xZ,
after every cj7hp reading (02:40Z to 15:44Z), on a seat appearing nowhere in
cj7hp's table.

WHAT IS NOT RESTORED, and it is small: those three tag witnesses at 15:44Z were
computed by the defective classifier over panes that no longer exist, so they
are unverifiable rather than wrong. Nothing rests on them, and nothing rests on
Միհր's 16:38Z tag either, which is the one string the tag alone attributed: the
class exclusion above reads off the string itself, so it does not need the pane
back.

THE RE-READ FOUND A DIFFERENT DEFECT, in the corrected tag rather than the old
one: `box_authorship` asks authorship per RENDERED ROW, and a composer wraps.
One stranded km ask alone in a box therefore tags `mixed`, because only its
first row carries the prefix. Measured at the live width of 352 columns, km's
530-character stall ask wraps to two rows and tags `mixed`. That is bd
gqlc-mtfbz, and until it is fixed a `mixed` tag does not establish that a
citizen's draft is present — which is the one thing its remedy turns on.

IT READS THE COMPOSER'S BYTES AND NOTHING ELSE, which is the whole of its
authority and the whole of its blindness. A citizen whose own draft happens to
open with `[km] ` is tagged `km`. Do not read the tag as provenance. It is a shape test against a
corpus, and it is worth having only because the corpus is enforced at the one
place text enters a pane.

ONLY A POSITIVE READ OF TEXT makes any claim beyond `none`. A pane with no box
or one that could not be read answers `none`, which leaves the seat in whatever
population it was already in rather than inventing a new alarm out of a failed
read. The conservative direction here is to under-report: a boxed seat missed
still shows up as IDLE, which is the state it had before this existed. That is
also why every failure path echoes `none` and returns 0 rather than returning
non-zero with nothing on stdout — a caller doing `x=$(seat_box_state "$s")`
cannot distinguish an empty capture from a function that does not exist, and a
harness row asserting the empty string passes on both.

THAT CONFLATION WAS CHALLENGED AND RULED ON — gqlc-mfjgl's "should not answer
`none` for a box it failed to read" against the paragraph above; the ruling is
gqlc-tdb9a, and it stands with the paragraph, on measured grounds rather than
asserted ones. The question that decides it is whether any consumer treats
`none` as a LICENCE to act, rather than as a default that changes nothing.
Enumerated over the shipped km at 83fc8743: the function has exactly two code
consumers, both in cmd_status — the idle refinement and the NOWORK arm — and
both use `none` only to LEAVE a seat in the population it already occupied;
only a positive tag moves one. Neither acts on it. The remedies their lines
recommend (`km sleep`, the recovery ladder's ask) route through send_line,
which re-derives the box itself at act time and refuses when the read fails —
rc=2 is fail-closed there since gqlc-mfjgl — and a refused ask is counted and
escalates to sedrak at the recover_attempts cap with the refusal quoted. So a
PERSISTENTLY unreadable pane surfaces through the acting path, on the ladder's
cadence, even though the board never distinguishes it. The one non-code
consumer, decision 0012's Tier 1 release, spells its precondition
`seat_box_state ≠ none`, so a failed read BLOCKS that act rather than
licensing it. The dispatcher does not read this function at all; its box guard
is send_line's own read.

Two limits, each a condition under which the ruling stops holding. It is a
fact about the enumerated consumer set: a new consumer that acts on `none`
without re-deriving the box at act time is not covered, and adding one reopens
gqlc-tdb9a's question rather than inheriting its answer. And if `none` is
ever split, every shipped predicate spells "boxed" as `!= none` — both
cmd_status sites and decision 0012's precondition — so a fifth value
satisfies all three as if it were a positive read of text unless every site
moves in the same change; gqlc-tdb9a's acceptance item 3 governs that
measurement.

What survives of the objection is real and is filed, not absorbed: `none`
warrants no claim that a read HAPPENED, and three of cmd_status's own
sentences claim one anyway ("the composer was READ and found empty"; "has
been read and found empty — the box is checked for every seat on this line";
the BOXED line's reading of its own absence). On the failed-read path each is
an observation nobody made — gqlc-3e4m5 F2/I1's class, in the board. The
repair is those sentences (bd gqlc-6jp65), not this contract.

box_one_line and not a bare `-n "$box"`, and the difference is one case, not
the obvious one: seat_box_text's awk already drops a line that is EXACTLY `❯`
or `>`, so a bare glyph never reaches here. What does reach here is a glyph
followed by spaces or the U+00A0 the TUI pads with — awk keeps that line, and
`-n` on it reports an empty composer as BOXED, which raises the unreachable
alarm over a seat a send would have reached. It is also the normalisation
send_line's own witness uses, so this function answers about the same bytes as
the guard it is reporting on.

## seat_prompt_visible — whether the agent can receive text at all

(`kingdom/bin/km`, `seat_prompt_visible`)

Whether the agent is available to receive text. `blocked` is what the hook
reports for the modals it SEES — measured 2026-08-30 on a live trust dialog;
`unknown` means claude isn't reporting, which includes the "no agent at all"
case. Both refuse the send.

This comment listed the usage-limit modal among the `blocked` cases and that
was wrong, which mattered more than a stale sentence usually does: it was the
premise decision 0007 §1 rested on. Nine seats killed by a quota wall on 2026-08-29
read done, working and unknown, never blocked, and this function admits the
first two — so `blocked` is a floor on which modals are refused here and not a
claim that every covered pane reaches it (bd gqlc-3evsn). What refuses the ones
it cannot see is send_line's box check, on the pane's own render.

## seat_can_report — an instrument whose silence is indistinguishable from its all-clear

(`kingdom/bin/km`, `seat_can_report`)

Can this seat report at all? heartbeat.json is written by no heartbeat script:
it is a side effect of kingdom/bin/km-statusline, which runs only because
.claude/settings.json names it as the statusLine command. BOTH are TRACKED
files, so whether a seat can report its liveness is a property of the COMMIT
its worktree is parked on — not of the seat, the machine, or the session. A
worktree on a branch that forked before the kingdom landed has neither file
and goes silent while working (measured 2026-08-22 on Վահագն, who had worked
all night: fifteen seat worktrees, one of them without the key, and the
welfare round could not read the one seat it most wanted to).

That matters because an absent heartbeat is otherwise overloaded three ways —
idle (no action), misconfigured (the instrument is blind), and WEDGED, which
is the case the welfare round exists to catch — and the board resolved all
three as the first. A detector whose failure mode is indistinguishable from
its all-clear is not a detector. This separates the second arm out so it can
be named.

Fail-closed: a worktree that is absent, or that cannot be read, is not
evidence that the wiring is there.

## progress_field — the progress witness

(`kingdom/bin/km`, `progress_field`)

---------- the progress witness ----------

The heartbeat above answers "is a process alive". It CANNOT answer "is this
seat doing anything", and the comment at status_unresponsive_after is careful
about why: km-statusline goes on writing all through a wedged turn, so a seat
frozen on a modal inside a tool call has a heartbeat as fresh as a seat doing
real work (gqlc-n97e, 13+ minutes measured; gqlc-eier, 80 and 152).

progress.json is the other half. It is written by .githooks/claude-tool-witness
on PreToolUse and PostToolUse and by nothing else — no timer, no statusline, no
daemon — so its `last_progress` field advances only when a tool call actually
FINISHED. That is the whole design: an instrument that ticks on a schedule
vouches for the schedule, and this town has been burned by that shape four
times in one night.

## seat_dry_age — how long the progress witness has been dry

(`kingdom/bin/km`, `seat_dry_age`)

How long this seat's progress witness has been dry — since the newest tool call
STARTED when one is in flight, since the newest one FINISHED otherwise. It is
the single number behind km status's WORK column and behind the recovery
ladder's stalled trigger, and it is one function so the board and the ladder
cannot come to disagree about which seats are in trouble.

rc 1 when nothing can be read, never an age: an unreadable witness says nothing
about the seat, and answering "0 seconds" would say the most reassuring
possible thing about a seat nobody can see — the mistake the HB column had to
be taught out of (gqlc-eier). Callers must not read rc 1 as stalled.

## seat_can_witness — whether a seat can witness its own progress is a property of the commit it is parked on

(`kingdom/bin/km`, `seat_can_witness`)

Can this seat witness its own progress at all? The same question seat_can_report
asks of the heartbeat, with the same answer shape and the same reason for
existing: .githooks/claude-tool-witness and the PostToolUse wiring in
.claude/settings.json are TRACKED files, so a worktree parked on a commit older
than this one writes no progress.json — and a silent instrument renders exactly
like a seat that has completed no tool call, which is the stall this whole path
exists to name. Fail-closed: a worktree that is absent, or cannot be read, is
not evidence that the wiring is there.

## mail_box_count — counting one seat's mail box

(`kingdom/bin/km`, `mail_box_count`)

mail_box_count <seat> <inbox|read|sent>

The directory guard is load-bearing under `set -o pipefail`: find exits 1 on a
directory that does not exist, and the whole km run dies mid-table for a box
nobody has written to yet (gqlc-6wqw). `read/` is missing far more often than
`inbox/` is, so the counter below would hit it constantly.

That guard answers the cause that was hitting us, not the whole failure. find
still exits 1 on a directory that EXISTS and cannot be walked — an unreadable
entry inside it, or the box removed between the test and the walk — and with
its stderr discarded under `set -o pipefail` that killed the dispatch tick
with nothing in the journal to read. This is the last instance on a tick path
of the shape that caused gqlc-w3wb and gqlc-5ybu (gqlc-w375); it is repaired
here in the fail-loud form km:113 argues for, rather than left as the one the
third outage comes from.

The partial count is kept rather than replaced with 0. `wc -l` has already
counted whatever find reached before it stopped, and that number is a FLOOR.
Zero is the one answer that must not be invented here: "0 unread" is how a
seat's waiting mail disappears from every board that reads this.

## mail_unread_from — how many of one sender's letters are still unread

(`kingdom/bin/km`, `mail_unread_from`)

How many of <sender>'s letters are still sitting in <recipient>'s inbox.
mail_send writes "<ts>--<from>--<slug>.md", so the sender is in the name and
no body is read. The caveat on unread_count above applies with full force
here: for a SEAT this counts letters that outlived at least one of the
recipient's wakes, and for the king it counts deliveries to a box nobody
drains (gqlc-2abx). Neither caller passes him as the RECIPIENT: one walks
seats_all, which excludes him, and the idle escalation names the seat sedrak.
That pass compares two counts inside a single tick, so the staleness above
cannot reach it — only a letter arriving in the window can, which is the
point.

## wake_queue_add — the queue is a SET of reasons, not a log of attempts

(`kingdom/bin/km`, `wake_queue_add`)

The queue is a SET of unread reasons, not a log of attempts. A stuck queue is
re-derived by every dispatcher tick, and km-seat cats the whole file as ONE
wake, so N identical lines reach the seat as a batch dispatch never sent —
during the gqlc-2vxs outage Րաֆֆի's file held the guard-round reason six times
over. The other thirteen orphaned queues held one line each only because
dispatch happened to skip a seat it had already queued for; that bounding was
accidental, and this is the one that is not (gqlc-t37m).

Distinct reasons still accumulate: one wake can legitimately name several
beads, and km-seat walks all of them. So the match is exact — -F and -x, since
reasons carry regex metacharacters and one reason is often a prefix of
another, and `--` because a reason may begin with a dash. Absent file is a
miss, which is the append case.

## cmd_wake — through seat_nudge, never bare send_line

(`kingdom/bin/km`, `cmd_wake`)

Through seat_nudge, never bare send_line — the wrapper is the name
this path's queue-on-refusal logic is written against. Under tmux the
send was a clear/text/Enter tty burst that could re-coalesce and
strand itself, and this path had the split without the confirmation,
so it printed its cheerful success line while six nudges in a row sat
typed and unsent (gqlc-01ev). send_line now types AND presses Enter
and returns non-zero if either call fails, which is not the same as
delivery. Characterised 2026-08-29 on a consenting pane (bd gqlc-bi7n,
and see send_line's own comment): the two writes coalesce into one pty
burst whenever the far side is not sitting in read(), and a single
burst of 64 characters or more is taken as a paste, so its trailing CR
types a newline instead of submitting. Dispatch wake text is not short,
so this path can still report a nudge it did not deliver — the
gqlc-01ev shape one layer down — while both calls return rc=0. Reading
the box back is the answer; it is gqlc-gh7xj and is not done here.

Reported, never fatal: route_owners and the mayor's wake both call
this from inside cmd_dispatch under `set -e`, so a non-zero return
here would take the whole routing run down with it.

## cmd_up — a seat is its own clone, not a linked worktree

(`kingdom/bin/km`, `cmd_up`)

A seat is its own clone, not a linked worktree of the main checkout
(gqlc-w5bh): sharing refs and local config is what let one seat's writes
land in another's guard window, brick the whole town's core.bare, and put
one stray hooksPath in every seat's config at once. Cloned from the URL
rather than from "$mr", so the seat carries no local branch of anyone
else's and no object store hardlinked to the town's. core.hooksPath has
to be set per clone: it lives in local config, which a clone does not
inherit.

## cmd_up-2 — the one step of the raise that must stop it

(`kingdom/bin/km`, `cmd_up`)

The create is the one step here that must STOP the raise: unlike the
two reads below, a failure means there is no workspace to put seats
in. It used to stop anyway, on `set -e`, but with the `2>/dev/null`
eating herdr's account and no sentence of km's own — so `km up` ended
in silence after the worktree output above (gqlc-3pks4). Dropping the
redirect costs nothing on the success path: stderr never reaches
`created`, which captures stdout only. Measured 2026-08-30, herdr
0.6.10 — a failing create exits 2 with the reason on stderr and stdout
empty, and `workspace list`, `tab list` and `status server` each
succeed writing no stderr at all. A successful CREATE was not
measured: it would have raised a second workspace on the live town.

## halt_log — "is the town stopped now" versus "was it ever"

(`kingdom/bin/km`, `halt_log`)

The flag file answers "is the town stopped now"; this answers "was it ever,
and by whom". They are two files because the flag's ABSENCE is the lowered
state, so a log that doubled as the flag would have to be deleted to lower a
halt — which is the erasure this exists to stop (bd gqlc-nlc1). VI.4 reserves
lowering to two citizens, and until this file existed a halt that was raised
and lowered left nothing behind at all.

The detail field is last because it is free text from argv. Newlines and tabs
are squashed out of EVERY interpolated field, not only that one: any field
that keeps them appends a second, fully-formed event that nobody performed.
The actor is the field that makes this matter rather than merely untidy. It
came from $KINGDOM_SEAT unsquashed until gqlc-tq83, so one `km halt` could
write a backdated `lower` by `andranik` beneath its own raise — and VI.4
reserves lowering to two citizens, which is the reservation this file exists
to make auditable. That is accident-resistance, not a security boundary: the
file has none, and anyone who can set KINGDOM_SEAT can also append directly.

## cmd_halt — every accidental halt began as a citizen's question

(`kingdom/bin/km`, `cmd_halt`)

Every accidental halt this town has recorded was a citizen asking a
question with the write verb. The `-*` arm above closed the flag
spellings; `km halt show` carries no dash, so it is well-formed reason
text, reaches here legitimately, and stopped the town for 46 minutes on
2026-08-29 (bd gqlc-wguoq). A read-word opens the question; what may
follow it is a COUNT, because `--history N` is the family's only read
form taking an argument and its dash-less spelling is two tokens. Counting
tokens instead of reading them let `km halt history 20` through the guard
written for this very accident (bd gqlc-cwic0, Միհրի F1). Prose that opens
with a read-word still passes: `show stopper in CI` has a tail that is not
a number.

## cmd_halt-2 — report the act and its actor, not the resulting state

(`kingdom/bin/km`, `cmd_halt`)

Reports the ACT and its actor, not the resulting state. "halt raised" was
a true sentence about the world and said nothing about what the command
had just done, so on 2026-08-29 the guard raised a halt by typing
`km halt show`, read this line, concluded it described a standing halt
someone else was tracking, mailed the mayor "town healthy under the halt,
nothing new", and slept. The town stayed stopped 46 minutes (bd
gqlc-wguoq). The three earlier accidental halts were each caught by a
human who knew what they had just typed; an agent on a sweep does not,
and is now a routine reader of this line.

## cap_exempt_class — who is exempt from concurrency.max_active

(`kingdom/bin/km`, `cap_exempt_class`)

Judges are exempt from concurrency.max_active alongside the mayor and the
guard: the bench is the town's merge gate, so a cap that can close it is a
merge freeze rather than a throttle, and it competes with the very seats
producing its queue (gqlc-dz85). Judges are still worker seats above —
dropping them there would leave free_worker_of_class unable to find them and
route them nothing at all.

Named, because it used to exist only as the difference between two case
statements twelve lines apart: worker_seats listing three classes and
capped_seats listing two. A reader had to diff them to find the exemption,
and a fourth class added to one and not the other would have been silently
capped or silently exempt (gqlc-a4tu, Ծովինար).

## cmd_reconcile — agree the ledger with the world before anything reads it

(`kingdom/bin/km`, `cmd_reconcile`)

Bring the ledger back into agreement with the world before anything reads it.

Four repairs. The difference between them is consent, and the order they run
in is not arbitrary — the runner check comes first because it is the only one
whose failure makes the seat unreachable by every later mechanism, including
this function's own.

  runner gone     — km-seat is the pane's command and the ONLY reader of a
                    wake file. Its absence is a defect at ANY status, which is
                    what the status arms below could not see: for the whole
                    life of this function the case statement skipped exactly
                    the `asleep` seats where a dead runner lives, and an asleep
                    seat with no session is what a resting citizen looks like
                    (III.5). Recovery is cmd_up's own idempotent respawn.
  awake/pending, no session   — a stale record. Correcting it frees the slot
                    and, because free_worker_of_class only picks an `asleep`
                    seat, hands the seat back to the town.
  pending, session alive      — the citizen asked to leave and the /exit never
                    landed, so it is re-delivered. That needs no asking:
                    `km sleep` was the asking.
  asleep, session alive       — the OTHER direction of the same lie, and it
                    strands nobody: it INTERRUPTS. Both dispatch passes wake
                    asleep seats, so a working citizen recorded asleep gets
                    handed a second bead on top of the one she holds. Sighted
                    2026-08-22T14:23Z on Նուարդ, mid-generation on PR #1122
                    while the board read her asleep (gqlc-5vp7). Recording
                    `awake` takes nothing from her and costs no slot: the cap
                    counts sessions, not status strings.

What is NOT here: ending a seat that is alive and idle. From outside, idle and
thinking are told apart only by evidence this function does not gather, and
ending a session against a citizen's will is VI.2. The dispatcher NUDGES such
a seat instead (seat_idle), which asks rather than forces.

## cmd_reconcile-2 — reported, never fatal, and never tallied without the send

(`kingdom/bin/km`, `cmd_reconcile`)

Reported, never fatal, and never tallied without the send
saying so. This ran bare under `set -euo pipefail`, so a
refused send aborted the whole reconcile pass HERE, at the
first asleep-pending seat, and every seat after it went
unexamined — while the next line announced a re-send that had
not happened and the tally counted it. Since cmd_dispatch
calls this pass before it routes anything, that killed the
tick outright: 19 consecutive ticks routed nothing, silently,
because one citizen ran `km sleep` (bd gqlc-87al).

## cmd_reconcile-3 — the suspect marker is confirmed over elapsed time, not over a count of passes

(`kingdom/bin/km`, `cmd_reconcile`)

Confirmed over elapsed time, not over a count of passes. km-seat
writes `awake` BEFORE it execs claude, so there is a brief moment when
a seat that is starting up is indistinguishable from one that has died
— and rewriting the ledger there would hand a running seat's name back
to the dispatcher, against a session that is by then alive. Nothing
repairs that: `asleep` is not a state this function revisits.

A sighting COUNT does not close the gap, because nothing separates two
passes: reconcile is a public subcommand, and a manual `km dispatch`
landing seconds before a cron tick makes a pair with no time in it.
The marker's age is the signal instead. 60s clears any exec window and
sits well under the 120s cadence, so a genuinely dead seat is still
freed on the next tick. The cap stopped counting it on the first pass
either way, so the throughput that matters is already recovered.

## class_roster_reason — why no seat of a class could be given a bead

(`kingdom/bin/km`, `class_roster_reason`)

Why no seat of <class> could be given a bead, as a sentence an operator can
act on. The lookup above returns 1 for four conditions with four different
remedies, and until gqlc-2ve4 all four produced the same output: none. The
call site was an `if` with no `else`, one layer below the `elif .cls == null
then empty` arm that used to drop unlabelled beads — the same defect twice in
one function, a decision taken and not spoken. OBSERVED 2026-08-22T13:12Z: a
P0 class:architect bead sat unrouted with all three architect seats occupied,
and nothing in the run, the summary or the board said so. Every indicator was
green, because every seat was either working or legitimately resting.

The CAP is not answered here. It belongs to cmd_dispatch, which is the only
scope holding the slot count and the judge exemption, and conflating the two
would send an operator to the roster for a throttle or to the throttle for a
roster.

## dispatch_loud_priority — an unroutable bead is named one at a time below this line and counted above it

(`kingdom/bin/km`, `DISPATCH_LOUD_PRIORITY`)

The priority at or below which an unroutable bead is named ONE AT A TIME.
Above it they are counted instead. Both halves are load-bearing and they fail
in opposite directions: a P0 nobody can take is the town's highest work
standing still and has to be greppable by id, while the routable queue holds
197 beads and a per-bead line for each of them every two minutes is the
volume at which an operator stops reading the journal — which is the same
outcome as silence, reached from the other side. A number, not a config key:
it tunes how loud a report is, not what the town does.

## stalled_marker_path — the stalled-P0 marker

(`kingdom/bin/km`, `stalled_marker_path`)

---------- the stalled-P0 marker ----------

gqlc-2ve4 gave a P0 that no seat could take a line of its own in the run's
output. gqlc-tz95 is that line's own limitation: the dispatcher's output is a
journal, which exists for whoever runs journalctl with the right filter inside
the right two-minute window. gqlc-2ve4's complaint was "a number in a queue
nobody is watching", and a better line in the same queue does not answer it.

So the run leaves DURABLE state — one file — and the two surfaces a human
already reads carry it: `km status`, the one glance at the town, and
`km doctor`, which FAILS. That is the whole choice, and the alternatives were
rejected for reasons worth keeping. MAIL to Սեդրակ makes the mayor a required
actor in a hot path, which is the opposite of the direction his role is being
taken in, and the king's own box has been measured at 30 delivered / 0 read
(gqlc-2abx) — a second write-only carrier. An AUTO-WAKE acts on a citizen,
which is Constitution VI.2 for an awake seat and is coercion for a condition
that is often two minutes from healing on its own.

The file is a marker and NOT a queue of events, which is what bounds the
repetition. A stall that persists for an hour is 30 dispatch runs; 30 letters,
or 30 anything, is the volume at which the report becomes the silence it was
written to end (this town has 48 unread nudges on record). Writing the same
marker 30 times says the same thing once.

Format, one line per stalled bead:  <first-seen epoch> <id> <class> <reason>
The first-seen stamp is per BEAD, so a second P0 joining the stall does not
restart the first one's clock, and it is what lets doctor separate a stall two
minutes old — which the next tick may route — from one that has held all night.

A run that never reaches here — the town is halted, or the ready query failed
and dispatch refused — leaves the marker exactly as it was, and that is the
direction to fail in: a stall stands until a run PROVES it has cleared, rather
than being cleared by a run that could not look. Both read surfaces say the
marker is the dispatcher's record and is only as fresh as its last run.

## stalled_p0_render — one account of the stall, for both read surfaces

(`kingdom/bin/km`, `stalled_p0_render`)

What the two read surfaces say about the marker, in ONE place. They differ in
severity and in nothing else; two copies of this loop would be two accounts of
the same stall, free to disagree about which beads and how long.

rc 1 for "no stall recorded". A marker that is not a readable file (the path
taken over by a directory, say) reads the same way rather than aborting: this
is an instrument on a status command, and status commands that die are status
commands nobody runs.

## dispatch_candidate_docs — a JSON payload belongs on a descriptor, never in argv

(`kingdom/bin/km`, `dispatch_candidate_docs`)

A JSON payload belongs on a descriptor, never in argv. `--argjson` puts the
whole document in ONE command-line argument, and Linux caps a single argument
at MAX_ARG_STRLEN — 32 pages, 131072 bytes on a 4 KiB-page machine. That is a
hard kernel ceiling in the binfmt loader: it is not ARG_MAX, it does not move
with `ulimit -s`, and no amount of free memory raises it.

The reason this is a rule here and not a note on one call site: the payload is
`bd ready`, so its size is the length of the BACKLOG. The failure is monotonic
in the queue — once crossed it cannot self-heal, and the fuller the queue, the
more certainly the dispatcher that drains it is switched off. It is an outage
that arrives on the worst day, by construction.

MEASURED 2026-08-22 (gqlc-fo48): `bd ready --json -n 0` was 1222148 bytes,
9.3x the ceiling, and the fresh pass had been dead 63 consecutive runs over
two hours. --slurpfile reads the document from a file descriptor and wraps it
in an array, hence the [0] on each binding.

## route_owners — the cap arithmetic that must not exist in two copies

(`kingdom/bin/km`, `route_owners`)

Wakes every sleeping seat that owns work in <list> (lines of "seat id"), with
<prefix> naming what those ids are to it. Both routing passes that hand a seat
its OWN bead go through here: the cap arithmetic is the part that must not
exist in two copies, because a divergence between them overspends the cap
silently, and the cap is what keeps the town inside its quota.

It reads and updates cmd_dispatch's `slots`, `wakes` and `withheld` through
bash's dynamic scope, and reads its `active` and `max` for the cap's reason
string, which is why it is only ever called from there.

## stranded_ids — the state no dispatch pass can reach

(`kingdom/bin/km`, `stranded_ids`)

The state no dispatch pass can reach. The resume pass takes in_progress beads
that name a seat, the owned and fresh passes take ready ones — and `bd ready`
returns beads whose status is open only, so an in_progress bead with no
assignee is in no pass and cannot re-enter one. It reads as work underway and
no wake will ever come for it. Unassigning a bead a seat had already claimed
produces exactly this state, and did on gqlc-gwf0 (recorded in gqlc-mro7),
done by someone trying to make the bead more routable.

NOT counted here: a bead assigned to a name that is not a seat. That is the
SECOND unreachable shape and it has its own predicate below,
unreachable_owner_rows, because the two want different treatment — this one is
always a defect and fails `km doctor`, that one is usually a person holding
their own work and must not.

## unreachable_owner_rows — the other unreachable shape

(`kingdom/bin/km`, `unreachable_owner_rows`)

The other unreachable shape. Every dispatch pass matches an assignee against
the SEAT roster — the resume pass over in-progress beads, the owned pass over
ready ones — so a bead assigned to a name that is not a seat is routed by
nobody. Unlike a stranded bead it does not even look unrouted: it looks like
somebody is working on it, which is why nothing has ever found one.

Three cases, and conflating them is the actual defect (gqlc-t4zx):

  a live seat   — routed normally, nothing to report.
  a person      — [humans] in kingdom.toml. Held deliberately. Reporting these
                  would put a line on the board every time a human takes a
                  bead, and an operator learns to ignore a channel that is
                  always noisy; the reachability gate was nearly not written
                  at all for this reason.
  neither       — the only defect. A retired seat's name lands here, and so
                  does a typo'd assignee and a person nobody has added to the
                  roster yet.

NOT CLOSED, not `--status open`: bd's `--status open` is the literal status
and excludes in_progress and blocked, so a claimed bead held by a name the
town cannot resolve — the exact state this exists to find — would read as
absent. Any status this code does not recognise counts too, which is the safe
side for a report.

It reports and does not act. Repointing another citizen's claimed work is a
confident sweep over evidence this function does not have: it cannot tell a
retired seat from a person the roster has not caught up with, and both look
identical here. Naming them is the whole job; the decision is a human's.

## conditioned_verdict_ids — the cutoff is decision 0009's own merge, to the second and not to the day

(`kingdom/bin/km`, `conditioned_verdict_ids`)

Decision 0009: a verdict is PASS or FAIL, and a PASS is unconditional. A condition
written into a close reason reaches no dispatch pass and no assignee. Other
things do read `close_reason` — bdguard parses it for a cited sha, bd-gh-sync
takes its first line as a mirror tag — but neither can route an obligation,
and nor can this row: it names ids and assigns nobody, which is why it warns
rather than fails. The match can be this blunt because ruling 2
leaves a compliant close reason no reason to contain the word at all, down to
forbidding "no conditions" as a disclaimer.

The cutoff is the decision's own merge (3c5a9988), to the SECOND and not to the
day. Fifteen close reasons already on the ledger carry the word, and seven of
them closed on the decision's own date — a bare `2026-08-29` would have shipped
this row ringing on all seven from its first run, which is how a warn row
teaches the town to scroll past it.

## wall — what the flag is for, and the two outages that bought it

(`kingdom/bin/km`, `wall_flag` and the `wall_*` family)

The quota wall is per-ACCOUNT, so it arrives town-wide and lifts town-wide, and
until this landed nothing noticed either edge. Twice measured: reset 03:50Z and
first routed wake 12:34Z, 8h44m of silence while kingdom-dispatch.timer fired
~260 times exiting 0 (gqlc-tdciz); then 2026-09-01/02, ten seats frozen 8-10.7h
in single unfinished turns, recovered only by an operator restart. Every board
read the town as UP through both.

A walled seat is never freed by anything already here, and it is worth being
precise about why, because the first design of this guard was aimed at the
wrong failure. `cmd_reconcile` writes `asleep` only for a seat recorded awake
with NO live session; a walled seat HAS a live claude, sitting at a prompt
whose first turn was refused. So it never cycles, never returns its slot, and
never answers a pass — every routing pass wakes ASLEEP seats only. The original
plan guarded a launch storm; there is no storm, and the bead's own design was
re-ruled once that was measured (gqlc-nmxmr).

What is left is three failures, and this family addresses the first two and
reports the third:

1. The recovery ladder POISONS a walled seat. Each ask strands text in a
   composer that never submits it, and `send_line` then correctly refuses that
   seat forever (gqlc-6bnkw, gqlc-gv7dw) — the guard protecting a citizen's
   draft turns one stranded nudge into a seat nothing can reach again. The
   3-ask cap then converts one town-wide wall into sixteen private ESCALATED
   episodes that OUTLIVE it.
2. Nothing watched for the reset.
3. Post-reset the held slots strangle routing: ten capped seats awake means
   `slots=0`, and only judge work is cap-exempt. This design does not fix that
   one — ending a session is an operator's action (VI.2) — it hands the
   operator the roster instead of a healthy-looking board.

## wall_refusal_seats — dispatch's own artefact, never a pane read

(`kingdom/bin/km`, `wall_refusal_seats`)

The raise interface is the per-seat `idle-refusal` file the ladder writes when
a send is refused, removed the moment the seat works again or an ask lands.
Deliberately NOT `seat_wedge_reason`: that reads a live pane, it keeps exactly
one caller in `cmd_status`, and nothing in dispatch may read a pane for this.
Three bounds on the interface, all known and all accepted:

- It is LATE. An idle-shape wall (the turn ended at a prompt) reaches a refusal
  in ~4-6 min through the two-pass idle confirm; a mid-turn wall — last night's
  shape, where `seat_agent_status` keeps saying `working` — reaches one only
  through the dry path at `welfare.stalled_after_minutes`. So the first
  evidence lands 5-45 min after onset. That delays the first walled LINE, not
  the recovery: the wall itself is hours long, and the probe interval is what
  prices the clear.
- It is NOT wall-specific. A consent modal, a citizen's own draft and a foreign
  sender all produce refusals. Nothing here infers a wall from them — they only
  decide when a probe is worth spending. The probe is the disambiguation.
- It is budget-EXEMPT, which the arithmetic needs: a refusal does not set
  `delivered`, so two seats can both acquire the marker inside one pass. If
  refusals were capped at one per run the two-seat threshold could never be
  reached in the window.

## wall_probe — a subprocess with an exit code, which is why it replaced a canary wake

(`kingdom/bin/km`, `wall_probe`)

The first design woke a seat as a canary and asked it to run a verb first
thing. That was withdrawn: a canary needs an asleep seat AND a free slot, and a
full wall holds every capped slot while leaving almost nobody asleep; each
canary launch is itself a wake into a wall, stranding one more composer; and
its fast path depended on a citizen remembering a first action. A probe has no
session, no slot, no pane and nothing to forget.

Three details are load-bearing:

- **The cwd is a `mktemp -d`, never a checkout.** From a checkout, SessionStart
  hooks and CLAUDE.md load the project into a probe whose whole purpose is to
  cost nothing.
- **No `--model`.** The seats' default model is the one whose wall this asks
  about. A cheaper model that answers while theirs is still walled would clear
  the flag onto a town that is still stopped.
- **Everything unrecognised is WALLED.** Nonzero, timeout, empty stdout,
  limit-shaped stdout. Wrongly walled costs one 10-minute interval; wrongly
  clear costs a whole re-raise cycle, and re-raising means the ladder gets one
  more chance to poison a seat first.

The design named one load-bearing guess, and it is still a guess at the time
of writing: `claude -p` under a standing wall has not been observed, because
no wall stood to measure against. The predicate above defaults the unmeasured
branch to the cheap side. The first live wall's actual probe output belongs in
the decision doc, verbatim.

The limit fragments are the same two `seat_wedge_reason` matches on a pane.
Two readers of one vendor string, cross-referenced by comment rather than
folded into a shared helper: for two greps a helper buys nothing, and it would
couple a pane parser to a subprocess parser so that a change to either drags
the other.

## wall_withheld — the line that must appear on EVERY pass

(`kingdom/bin/km`, `wall_withheld`)

A watcher that waits silently is indistinguishable from the eleven hours this
exists to prevent, so the flag says so every single pass rather than once at
the raise. It names when it was raised, when the last probe ran, when the next
is due, and the `rm` that overrides it — an operator reading one line during an
incident should not have to find this file by searching.

It also names what is withheld, and the whole pass is: reconcile, the ladder,
and routing. The ladder because an ask into a walled seat strands a draft that
makes it permanently unreachable; routing because a wake into a wall mints
another frozen slot-holder. A wake file already queued before the raise is
consumed by its runner and freezes one seat anyway; that seat becomes further
refusal evidence, and nothing here is built for it.

## wall_clear — the marker wipe, and the letter that is the real deliverable

(`kingdom/bin/km`, `wall_clear`)

Two things happen that are easy to read as tidying and are not.

**The five per-seat ladder markers are deleted.** The counters measured the
WALL, not the citizens: an ask refused because the account was stopped is not
an unanswered ask. Leaving them means every seat that accumulated three
refusals during the wall stays ESCALATED afterwards, so the ladder is silent
for the rest of the episode — exactly when post-reset delivery might finally
work.

The ladder does not do this wipe for us, and the reason is narrower than it
looks. The ladder clears the five markers on one branch only — a seat that is
neither idle nor stalled — and that branch covers a seat witnessed working AND
a seat km cannot read at all. A seat who sat through the wall is neither: she
is awake with a progress witness hours stale, so she is stalled, so the ladder
walks straight past its wipe into the episode.

Measured, because the first attempt to witness this was vacuous. Blinding this
`rm -f` in a copy of km and then clearing a wall left both planted seats at
5/5 markers, and that pass then read `dispatch: hayk is awake and has finished
no tool call for 3h00m, ESCALATED to sedrak 2s ago after 3 unanswered asks;
not typing at her again this episode` — silenced for refusals the wall caused,
which is the defect this wipe exists to prevent. Unmutated, the same seats end
the pass at 2/5, and those two are `idle-attempts` holding `1` and a fresh
`idle-refusal`: the ladder opens a NEW episode in the same pass and counts
from zero. That is the intended shape, not a leak — the wipe forgets the
wall's refusals, it does not excuse a seat who is still stalled after it.

The vacuous run had planted no `status` and no `progress.json`, so every seat
read unreadable, took the ladder's wipe branch, and reached 0/5 whether or not
`wall_clear` ran at all.

**The letter to Սեդրակ is the deliverable, not the routing.** Routing resumes
by falling through, in the same two minutes. What routing cannot do is tell
anyone that ten slots are still held by sessions that will never end. The
letter names each seat with the age of the last tool call of hers that
FINISHED, and says plainly that this is the evidence and not the verdict — one
long legitimate tool call reads identically from outside, and the wrong seat
ended is a citizen's day. Ending them is an operator's action and nobody
else's (VI.2), on gqlc-qs4jq's protocol: ask each seat to self-park first, end
only what does not park.

It is sent with the ladder's own witnessed-delivery idiom — `mail_send`
exiting 0 is not delivery, and this letter is the only artefact of a lift
nobody watched.

## wall_guard — probes cost nothing in the healthy case, and the flag is raised only by evidence

(`kingdom/bin/km`, `wall_guard`)

Two thresholds, and they do different jobs. The two-seat rule prices only how
OFTEN a probe is spent: one seat wedges for private reasons routinely, and a
probe is a real model call, so a lone consent modal must not buy one. The
probe is what actually raises. A false trigger therefore answers itself at once
and costs exactly one probe — which is why the trigger can afford to be a crude
signal.

The lone-walled-seat corner is named and accepted rather than solved: a single
walled seat escalates (mail is a file write, it succeeds), the mail wake
launches Սեդրակ into the same wall, and his seat becomes the second refusal
source 20-50 minutes later.

Probes run only under suspicion or under a standing flag. Unconditional
per-pass probing was rejected: it is a steady-state model-call spend to watch
for a condition that is absent almost always.

The call site sits ABOVE the mayor's mail wake, inverting the halt's placement
below it. The halt sits underneath deliberately, so that a halted town can hear
by mail that its cause is over (gqlc-ozfr). A walled town cannot hear anything,
and waking Սեդրակ into a wall freezes the one seat the whole recovery chain
routes through.

## ladder_evidence — the terminal rung's account of the seat, rendered once for two readers

(`kingdom/bin/km`, `ladder_evidence`)

This was fifteen lines of prose inlined in the mail rung's `-m`, and it was
the only account the ladder ever gave of why it stopped: the trigger, the
slot the seat is holding, its unread count, the age of the last delivered
ask, the last refusal, and whether any ask went unwitnessed. That list is
the evidence, and it does not depend on who reads it — so when a second
terminal rung appeared that reaches a bead rather than an inbox
(`cmd_dispatch-7a`), the body had to stop being a property of the letter.

It is a function rather than a variable because it shells out — `stat` on
the nudge marker, `seat_agent_status`, `cfg concurrency max_active` — and
the cap arm is reached at most once per seat per pass, so there is nothing
to amortise.

The extraction is byte-for-byte: the letter a non-sedrak seat's escalation
renders under this function is diffed against the letter master renders for
the same scenario, and they are identical. That is the whole claim being
made here, and it is the one worth a regression test, because a rewrite of
shared prose is exactly where a "harmless" reflow silently changes what the
mayor is told (bd gqlc-bwmst).

Its remedy sentence used to read `km sleep --seat $s` frees the slot if
she is genuinely done. That was false in all three branches a terminal
rung can fire for, which is why it is now a trichotomy that matches the
state instead. Against a BOXED seat the command is vacuous — `send_line`
refuses into a non-empty composer, and the rung fires precisely when asks
were being refused, which is what a box looks like from outside. Against
a walled or dead-turn session it is destructive: `/exit` is client-side
and WOULD deliver, taking the resume-at-reset continuation decision 0013
exists to protect, so the one branch where the command works is the branch
where it destroys. And against a present citizen who is choosing to sit it
is not the mayor's act at all (VI.2). Ruled by gqlc-b5x22 and written down
as decision 0016; the boxed path is decision 0012's.

That paragraph says "this report", not "this letter", and the word is
load-bearing for the reason this section exists: the block is rendered
once for two readers, and on the bead rung there is no letter. It is also
the reason the trichotomy carries bare `"` around `km wake`'s reason
rather than `\"` — this body is an UNQUOTED heredoc, where a backslash is
special only before `` $ ` \ `` and a newline, so `\"` would reach the
mayor as a literal backslash-quote. The sentence was written at the old
inlined site, where `\"` was correct, and moving it here inverted that.
Nothing catches this: `bash -n` parses both, no test reads the string, and
the gates are green either way — it is visible only in a render (bd
gqlc-7ninj, caught by Միհր in review).

## ladder_escalate_bead — the other terminal rung, for when the ladder's only recipient is its subject

(`kingdom/bin/km`, `ladder_escalate_bead`)

Files the bead the letter would have been. Unassigned and with no `class:`
label, so it routes to a Ռազմիկ by inference and the seat woken for it IS
the remedy — a bead addressed to Սեդրակ would reproduce the defect it
exists to fix. `-p 1` because the subject is holding a `max_active` slot
while answering no pass, and the fresh pass has a `max_priority` floor that
a lower number could sit under.

Three return codes, because "filed" and "routed" are different claims and
the caller must be able to say which one it has. 0 is filed AND on the
ready queue; 2 is filed but the queue could not be shown to hold it; 1 is
refused, nothing written. The readback through `bd ready` is the same
standard the mail rung is held to in `cmd_dispatch-7`: a bead can be
created and still reach no pass, and the fresh pass reads the ready queue,
not the ledger — so `bd create` exiting 0 is an attempt, not a delivery.

The id is parsed out of `bd create`'s human line rather than taken on
trust. An exit of 0 with no id in the output is code 2 and not 0: the
caller can still write a marker (something was probably filed) but must not
claim a routable bead by name it never read.

## cmd_dispatch — read the world before anything is counted or chosen

(`kingdom/bin/km`, `cmd_dispatch`)

Before anything is counted or chosen. Every decision below reads the
status files, and a stale one both spends a slot and hides the seat from
free_worker_of_class, so the same record costs the town twice.
Hygiene must not cost the town its routing. This ran bare, and km sets
`pipefail`, so ANY failure anywhere in the reconcile pass became the exit
status of the pipeline and aborted cmd_dispatch before a single bead was
considered. That is how one refused `herdr agent send` stopped the whole
factory for forty minutes (bd gqlc-87al).

The specific refusal underneath is fixed above, but the containment is the
part that generalises: reconcile is a pre-pass that repairs the ledger, and
a repair that cannot be made is a reason to route on a slightly stale
ledger, never a reason to route nothing at all.

## cmd_dispatch-2 — the awake, idle seat that no asleep pass could reach

(`kingdom/bin/km`, `cmd_dispatch`)

And so does everyone else, once they are idle. Until now this dispatcher's
ONLY mail-driven wake was the four lines above, for one seat: every other
citizen was reachable only through the asleep passes below. On 2026-08-22
six seats finished their work, wrote handoffs, never ran `km sleep`, and
sat at empty prompts for ELEVEN HOURS holding all five slots — unroutable
by every automatic mechanism the town had, while the board called them
awake and the merge gate stayed shut (gqlc-5vp7).

This is the ASK, not the force. It types a line at a citizen it has reason
to believe is not mid-turn; it does not end anyone's session, and whether
the text may be typed at all is decided per send, on the pane's own render,
by send_line (VI.2) — not here.

TWO TRIGGERS, because one signal could not carry both duties. Until
2026-08-30 this pass fired on `agent_status == idle` alone, which also
doubled as the thing keeping km off a covered pane. That conflation had a
measured cost in each direction: nine seats killed by a usage-limit wall on
2026-08-29 read done/working/unknown and were therefore never asked — over
six hours and 168 dispatch runs this pass named exactly ONE seat, the one
that was never dead — while the property it was credited with (never type
at a modal) was being supplied by that accident rather than by any check
(bd gqlc-3evsn, decision 0007 amended).

  idle    — the agent reports idle, confirmed over two passes. The
            empty-prompt case, unchanged: a single capture is one
            photograph of a TUI, and the marker's age is what makes the
            second sighting a different moment rather than the same one.
  stalled — the progress witness has run dry: no tool call has FINISHED for
            [welfare] stalled_after_minutes, in flight or not. This is the
            dead-turn arm, and it reads no agent_status at all. It needs no
            two-pass confirm because progress.json is durable state on
            disk, so its age is already its own second sighting.

The threshold is shared with km status, deliberately: the ladder now asks
exactly the seats the board names NOWORK and WEDGED, so the report and the
recovery cannot disagree about who is in trouble. A seat whose progress
witness is UNREADABLE is not stalled — the board's own "?"/unwired lines
own that case, and failing toward silence is right where the instrument,
not the seat, is what went quiet.

The marker filenames still say `idle`. They predate the stalled trigger and
renaming ephemeral state buys nothing; all five are episode state, not a
record of which arm fired.

## cmd_dispatch-3 — what ends a welfare episode, and why any sign of life takes all its markers

(`kingdom/bin/km`, `cmd_dispatch`)

Which arm holds, if either. Read before anything is cleared or asked,
because the answer is also what ENDS an episode: a seat that is asleep,
or awake with neither trigger holding, has given the only sign of life
this ladder accepts.

And "neither trigger holds" is now literally witnessed work — a tool
call that FINISHED inside the threshold — rather than a pane that
merely stopped looking idle, which is what decision 0007 §5 asked for and
what the reset it shipped with could not deliver.

Any sign of life takes ALL of the episode's markers with it. The floor
below is per episode and not per seat: keyed to the seat it would
silence the town's only channel to a citizen who came back, worked, and
went idle again, for its whole window. Going to sleep ends the episode
for the same reason, and neither `km sleep` nor reconcile's freed arm
removes these files, so a seat that was escalated, slept and woke again
met a pass that read a stale idle-escalated and reported ESCALATED
against an episode in which nobody had asked her anything — permanent
silence on the town's only automatic channel to her, which is the worst
failure this ladder can have.

## cmd_dispatch-4 — an empty inbox is no longer a reason to say nothing

(`kingdom/bin/km`, `cmd_dispatch`)

An empty inbox is no longer a reason to say nothing, and that was the
residual hole rather than a corner of one. The six seats of gqlc-5vp7
had FINISHED — pushed, handoffs written — and a citizen who has
finished has usually just drained her inbox, so `pending -gt 0` was
false for exactly the seats this pass was written to reach. The unread
count now chooses the WORDS; being awake and idle is the trigger.

The two-pass confirm belongs to the idle arm alone. When both arms hold
— awake, idle, and nothing finished for the threshold, which is the
gqlc-5vp7 finished-day shape — the idle words are the truer ones, so it
is tested first and the stalled arm is the fallback.

## cmd_dispatch-5 — a nudge starts a turn and spends a citizen's quota, so it needs a floor

(`kingdom/bin/km`, `cmd_dispatch`)

A nudge is not free: it STARTS A TURN in the citizen's session and
spends her quota. Without a floor, a seat that does not act on one is
typed at every two passes — 165 times over the eleven hours this pass
exists to end — and the town's one channel to an awake seat becomes a
metronome nobody reads (the same shape as the repeated welfare
check-ins, 48 unread). Reported and never silent: a slot is still held.

The floor is set by a DELIVERED ask and by nothing else. A refusal
started no turn and spent no quota, so it earns no silence — it is
counted (below) and retried at the ladder's own cadence.

## cmd_dispatch-6 — where the recovery ladder ends

(`kingdom/bin/km`, `cmd_dispatch`)

The ladder ends. Without a cap the floor above only slows the
metronome: a seat that answers nothing is asked every half hour for as
long as it stays awake and idle, and nobody is ever told — the shape
the welfare check-ins already reached at 48 unread. At the cap the pass
stops typing and says it once, to the one seat whose job is deciding.
`|| true` is load-bearing under `set -e`: the file is created below,
after the first successful nudge, and removed on any sign of life, so
it is ABSENT on ask one of every episode. Without it the failing
substitution takes the whole dispatch tick down before the routing
passes run — the ladder died on its own first rung (gqlc-w3wb).

## cmd_dispatch-7 — the marker records a DELIVERY, not an attempt

(`kingdom/bin/km`, `cmd_dispatch`)

The marker records a DELIVERY, not an attempt. Set before the send
with the send's failure swallowed, a mail that never arrived still
suppresses every later retry: the mayor is never told and this pass
reports ESCALATED for the rest of the episode, which fails more
quietly than doing nothing at all. ENOSPC has emptied this town's
scratch before (gqlc-vze6), so a failing send is not hypothetical.

mail_send's own return code will not carry it either: its last
statement writes the SENDER's sent box, so a delivery that failed
to reach the inbox still returns 0. So the inbox is counted before
and after, with km's own reader.

Counted from andranik alone, not over the whole box: any letter
landing in that window — another seat's, a concurrent km's —
confirms a delivery that failed, and a false confirm writes the
marker and restores the permanent silence this code exists to end.

A read by Սեդրակ racing between the two counts reports UNDELIVERED
for a mail that did land; the cost is one retried escalation on the
next pass, which is the direction to be wrong in.

## cmd_dispatch-7a — a letter about Սեդրակ goes to Սեդրակ, so the subject branch files a bead

(`kingdom/bin/km`, `cmd_dispatch`)

The rung above mails Սեդրակ unconditionally, and the one case where that is
guaranteed useless is the one it cannot see: when the exhausted seat IS
Սեդրակ. The letter lands in the box its own subject is not draining. Worse,
it lands SUCCESSFULLY — the unread count rises, which is exactly the witness
`cmd_dispatch-7` demands, so the arm reads DELIVERED, writes the marker, and
suppresses every retry for the episode. The mayor's mail wake cannot rescue
it either: that fires only when sedrak is NOT awake, and this seat is awake
by definition of the arm it reached. Measured over 48h on 2026-09: 17
escalation letters, two of them sedrak-about-sedrak, inside a 46-hour freeze
(bd gqlc-bwmst, filed by Արթուր).

So the branch is on the SUBJECT, not on the state of the mayor's box. That
is the narrow form the bead asked for and the widest one that is obviously
right. Whether a boxed-but-not-subject Սեդրակ should also take the bead rung
is a live design question and is deliberately not answered here — that arm
would branch on a pane read rather than on a seat name, and it can be wrong
in a direction this one cannot.

Mail is not the fallback when `bd create` refuses. The `*)` arm writes NO
marker and says UNFILED, so the next pass retries: a letter is not a
degraded bead here, it is the defect. The `2)` arm does write the marker,
because the bead exists — refiling it every two minutes would fill the
ready queue with duplicates of a bead that is already there.

The marker's CONTENT is what distinguishes the two rungs afterwards, and the
report line reads it back. An empty marker means a letter, which is both the
old shape and what every marker already on disk in the town holds — state
outlives a deploy, so the empty case cannot be treated as malformed.

## cmd_dispatch-8 — one delivered ask per run, because the cohort that went down comes back together

(`kingdom/bin/km`, `cmd_dispatch`)

ONE DELIVERED ASK PER RUN, town-wide. Decision 0007 §5 asked for this and it
was never built; the stalled arm is what makes it live, because the
population it reaches arrives as a COHORT — nine seats went down within
the same minute on 2026-08-29 and would come back within the same one.
Resuming nine sessions on one tick is the API-storm shape, and the town
loses nothing by spreading them over nine ticks of a two-minute
dispatch. The budget is spent by a DELIVERY: a refusal started no turn,
so it is allowed through and the seat behind it is not made to wait for
a send that never happened.

## cmd_dispatch-9 — a send refusal is a verdict about the seat, so it is counted rather than forgotten

(`kingdom/bin/km`, `cmd_dispatch`)

COUNTED, where it used to be reported and forgotten. That was right
while every refusal here was km's own tty failing, and it is wrong
now: send_line refuses when it cannot witness a place to type, so a
refusal is a verdict about the SEAT — "machinery cannot reach this
one" — and retrying it in silence forever is precisely the open loop
this ladder was rebuilt to close (decision 0007 §5, amended).

No floor is set, so the cadence stays fast: nothing was spent, and a
pane that recovers should be reached on the next pass rather than
half an hour later. What ends it is the cap, and the cap now ends in
a letter naming the refusal.

## cmd_dispatch-10 — a full cap no longer returns early

(`kingdom/bin/km`, `cmd_dispatch`)

A full cap no longer returns early. It used to, which put the whole
routing decision behind the cap check: at 5/5 nothing was considered at
all, at any priority, so a P0 judge bead did not outrank a running P3 and
the town's only merge gate could be held shut by its own throughput
(gqlc-dz85). Zeroing the slots instead lets both passes below run and
decline capped work on their own, which is what leaves the judge reachable.
A slot is spent by a session that EXISTS, not by a file that says one
does. Reading the status string here is what let a finished session hold
a slot forever, and what let a live one under asleep-pending hold none at
all so the cap could overcommit (gqlc-s16s, gqlc-0gjt).

## cmd_dispatch-11 — the resume pass gives a seat its own work back first

(`kingdom/bin/km`, `cmd_dispatch`)

Resume pass: a seat with in-progress beads gets its own work back first
(Constitution III.3) before any fresh bead is routed anywhere.

Both queries below are fail-closed. A query that FAILS is not a queue with
nothing in it: swallowing the status with `|| x=""` is what let a total
routing failure print "done (0 wake(s) this run)" and exit 0 for the whole
life of the kingdom, with no seat ever woken and nothing to see (gqlc-z1qw).
`-n 0` is unlimited and is load-bearing on every bd query here. The JSON
renderers cap silently — `ready` at 100, `list` at 50 — while the plain
renderers disclose it ("Showing 100 of 234 ready issues"), so the caller
that cannot read prose is the only one not told. A bead past the window
is not a bead nobody claimed, it is a bead nobody was shown, and the two
are indistinguishable from the board (gqlc-mlca).

ACTIONABLE, not merely in progress. Post-#1116 law is that a warrior's
implementation bead STAYS in_progress while its class:judge review bead
awaits a verdict, so a resume pass that woke every in-progress holder woke
review-blocked warriors on every two-minute cycle — each burning a slot
from concurrency.max_active to check mail and sleep again, with the judge
who would unblock them competing for the slots they ate (gqlc-3jmx). A
bead is actionable here iff it carries no OPEN blocks-type dependency;
the warrior files the review bead with `--deps blocks:<impl-bead>`, and
the judge's close of it is what makes the impl bead actionable again.
`bd ready` returns status open ONLY, so an in_progress bead never
re-enters it and this pass is the only wake path back — which is why the
predicate has to live here and nowhere else.

The blocker join is a second, EXPLICIT-ID `bd show`, and the two
alternatives were rejected on measurement rather than taste:
  `bd blocked --json` has no -n flag at all, so its cap behaviour cannot
    be measured; an overflow bead would read as unblocked. Fail-open and
    silent, in the very function gqlc-z1qw burned.
  list's `dependency_count` reads 0 beside an open blocker (measured
    2026-08-21). Unusable.
An explicit-id query has no renderer window at all, so it is complete by
construction. The `[ -n "$ids" ]` guard is load-bearing and not a tidiness
check: `bd show --json` with ZERO ids prints an error OBJECT on stdout, so
that failure arrives as data and not only as a status. (Re-measured
2026-08-23: real bd also exits 1, so pipefail would catch this one too —
an earlier note here said exit 0. The guard does not rest on either: the
data half is what is certain across bd versions.)
`.[]` is deliberately not wrapped in an array coercion for the same
reason — on that object jq aborts, and the die below is what the town
sees instead of a silent all-clear.

## cmd_dispatch-12 — the ready queue that serves both passes

(`kingdom/bin/km`, `cmd_dispatch`)

The ready queue serves the two passes below. Its rows split by whether the
bead names an owner:

  owned — assigned to a seat. It goes to that seat and to nobody else, at
          any status. Before this split, a bead that was assigned but
          still OPEN was in neither pass — not null-assignee for the fresh
          one, not in_progress for the resume one — so it sat on the board
          looking owned and routed to nobody, at any priority, with
          nothing reporting the omission (gqlc-xq8a, gqlc-g10n).
  fresh — unassigned, routed to a free seat of its class. A class label
          names that class; an absent one INFERS warrior (gqlc-38ye).

A class label decides nothing for an owned bead: the assignee already
names the seat, which is the rule the resume pass has always used. Hence
the class question arises on the fresh arm only, where a label is the sole
way to choose a seat at all.

An unlabelled bead used to be DROPPED here, silently. That made labelling
a gate rather than a hint, and put one seat — the Քաղաքապետ, whose soul
carried it as a standing chore — in front of every bead in the town.
MEASURED 2026-08-23: of 208 unassigned open beads, 25 carried no `class:`
label, including one P0 and three P1; none of them was named in any
dispatch run, because the drop said nothing. Inferring warrior costs an
occasional escalation under Constitution III.1, which is cheap; a bead
nobody can see is not. Owner's decision, recorded on gqlc-38ye.

Captured once rather than piped: the hold verdict below needs each
candidate's labels, and re-querying could read a different queue.

## cmd_dispatch-13 — `. as $c` must stay bound inside the pipe

(`kingdom/bin/km`, `cmd_dispatch`)

`. as $c` binds the class name before the pipe, and must stay bound: inside
`map(...)` the pipe in `($b.labels // []) | index(...)` rebinds `.` to the
labels array, so a bare `"class:" + .` is string + array — a type error that
aborts the whole program on any unassigned bead, labelled or not.

The `lowpri` arm is the priority floor, and it sits on the FRESH branch
alone — after the `owned` test, so a bead that names a seat is never
withheld from it by a number (Constitution III.3, same reason the resume
pass runs first). It emits rather than drops, so the beads it withholds
can be named in the output below instead of vanishing.

It sits AHEAD of the class test, so an unlabelled bead below the floor is
named by the floor rather than routed. Either order routes the same set;
this one reports the more specific reason, and the reason is the point.

The `epic` arm is the third withholding arm and the newest (gqlc-calw). An
epic is a container for months of work under one id: a warrior woken on
one is told to claim it and begin, there is no honest way to do that, and
the slot it spends is one of six. The treatment is an architect
decomposing it into execution beads, which is why the arm sits AFTER the
owned test — an epic that names a seat is exactly the shape that should
route, and Constitution III.3 protects it there as it protects a bead
below the floor.

It sits AHEAD of `lowpri` for the opposite reason `lowpri` sits ahead of
the class test: the reason printed should be the one an operator can act
on. "Below the floor" invites raising the floor, and raising it would not
make an epic workable. Being an epic is a property of the bead; the floor
is a config line that moved from 2 to 3 the night before this was written.

`issue_type` and NOT a title convention or a label. MEASURED 2026-08-23
over the whole ledger (`bd list --all -n 0 --json`, 814 beads): 6 carry
`issue_type: epic`, of which 2 are not closed — gqlc-h9n at P1 and
gqlc-35yu at P2, both unassigned, both in `bd ready`, both at or above a
floor of 3, so both routable today. An `[EPIC]` title prefix is carried by
2 of those 6, a minority convention in prose. An `epic` LABEL is carried
by 0 beads in the ledger. Only the type is bd's own field.

Each fresh row carries a third field, `inferred` or `labelled`, saying
where its class came from. Without it a routed-by-default bead and a
deliberately-labelled one are indistinguishable at the wake, which is the
silence this change is here to end rather than to relocate.

## cmd_dispatch-14 — a pass that chooses nothing says so out loud

(`kingdom/bin/km`, `cmd_dispatch`)

Said out loud, in the same shape as the hold verdicts below. A pass that
declines work silently and then prints a summary that reads healthy is
this dispatcher's oldest recurring defect, and a priority floor is a
machine for producing exactly that — 158 of the 319 open beads the morning
it was added.

Named individually up to a cap, then counted (gqlc-s4zm). It used to name
every one, which was right at 5 beads and wrong at 165: the floor moved to
2 for Constitution V.3.1, and P3 is now where machinery beads are FILED
rather than a backlog anyone means to drain, so the withheld set is
permanently large by design. A per-bead line for each of them on a
two-minute tick is the volume that makes an operator stop reading — the
same failure as silence, reached from the other side, which is the
argument the unroutable arm below already settled for the same reason.

The cap is not silence and the difference is the count: every withheld
bead is in `bd ready`, the total is in the done line, and the line below
says how many it did not name and how to list them. What the cap costs is
the ability to spot ONE unexpected id in the tail by eye, which is why it
is a cap and not an aggregate — a run withholding a handful still names
them all, exactly as before.

## cmd_dispatch-15 — is the candidate's premise actually on master

(`kingdom/bin/km`, `cmd_dispatch`)

Ask whether each candidate's premise is actually on master and out of every
open PR's way (gqlc-pj4r). Asked ONCE, here, for BOTH of the passes below
that route ready beads, and over the owned and fresh ids together.

It used to be computed after the owned pass had already routed, which made
the hold rule a protection for UNASSIGNED beads only: assigning a bead to a
seat removed its hold, by pass ordering rather than by any decision anyone
recorded. Fail-silent in the direction that reads healthy, too — the
operator saw `owner-withheld`, an accurate-sounding line about capacity,
and nothing anywhere said the hold question had never been asked. A mayor
labelling a bead to hold it, which is the documented remedy, got no warning
that assigning that same bead defeated the label (gqlc-2n3d5).

The RESUME pass above is deliberately NOT covered. A seat that has already
begun must stay resumable; holding it there would strand live work behind a
PR the seat may itself be waiting on, and Constitution III.3 wants the seat
told about its in-progress half first.

The edges come from ONE multi-id `bd dep list` — the single-id form returns
the parent issue object instead of {issue_id, depends_on_id, type} rows, so
a per-id loop would parse a different shape and quietly find no edges — and
the parent statuses from ONE `bd show` over the discovered-from parents.

## cmd_dispatch-16 — captured on the AND-OR, never read from $? in the branch

(`kingdom/bin/km`, `cmd_dispatch`)

Captured on the AND-OR rather than read from $? in the branch. Inside
the then-branch of an `if !`, $? is the status of the NEGATED test,
which is 0 exactly when the branch is taken — so this sentence spent
its life reporting every failure as rc=0.

The number is worth carrying because it discriminates. Measured
2026-08-30 against `km hold-verdict`: bad stdin exits 1, through
die (km:22) inside the substitution's subshell, and cmd_hold_verdict
ends in a jq program whose own status is the function's — a jq forced
to exit 5 there surfaced here as 5. Its earlier `jq -e` guards do NOT
surface: they are caught by an `if !` and collapse to die's 1. So 1
means "km refused the input or a guard fired" and anything else is
jq's own (bd gqlc-uethk).

## cmd_dispatch-17 — the owned pass, and why hold and owner-withheld stay disjoint

(`kingdom/bin/km`, `cmd_dispatch`)

Owned pass: an unclaimed bead that names a seat goes back to that seat,
ahead of any fresh routing, for the same reason the resume pass runs first
(Constitution III.3). An assignee that is not a seat — a person, most of
them — matches no seat here and is left alone.

A held bead is dropped from the list before route_owners sees it, and says
so on a line of its own that names the assignee. Which ARM stopped a bead
has to stay legible, because `hold` and `owner-withheld` are both
withholding lines and both read like the system working: a bead that is
assigned AND held is reported here by the hold arm and never reaches the
cap arithmetic that would have reported it as owner-withheld, so the two
counters on the closing line stay disjoint and neither absorbs the other.

## cmd_dispatch-18 — both branches worded, and the losing one as ordinary

(`kingdom/bin/km`, `cmd_dispatch`)

Both branches, and the losing one worded as ordinary. This bead is
ready and unassigned as THIS pass reads it, and a wake is composed
before it is delivered, so another seat may hold it by the time the
citizen reads the banner — twice on 2026-08-29 (gqlc-wguoq, 21s;
gqlc-96lf0). Naming only the claim left the loser with one
instruction, and it was the harmful one.

Deliberately does NOT restate that the bead is held or that claiming
it is the damage: since gqlc-oqu5 km-seat re-derives every bead named
here at delivery and appends the assignee as fact, plus a STALE line
saying not to claim. Repeating that here would say it twice in four
lines, on facts this pass is too early to know. What no other line
carries is the second half of Սեդրակ's standing rule — take other
work rather than sleeping — so that is what this adds, with the
protocol named for the rest of it.

## hold_fetch_master — the routing hold: a bead whose premise lives only on an unmerged branch

(`kingdom/bin/km`, `hold_fetch_master`)

---------- the routing hold ----------

A bead filed against code that exists only on an unmerged PR branch reads
`ready`, and a warrior routed to it branches from origin/master and finds
nothing to fix. Until now the only guard was Սեդրակ declining to class-label
such beads, from a list he carried in his handoff — a person remembering.

HOLD a fresh-routing candidate if EITHER its declared subject path is absent
from origin/master, OR an open PR modifies it. Release is automatic: merging
or closing the PR changes the answer on the next run, so nobody has to
remember anything. Holds gate FRESH routing only; work that already names a
seat is never withheld, whether that seat has started it or merely been
assigned it (Constitution III.3) — both owner-facing passes run upstream of
the verdict, so no hold can reach them.

The convention this reads is a bd label `subject:<path>`, repo-relative, file
or directory. A title-prefix→path map was measured and rejected: it would live
on master and be maintained reactively, so residue about a NEW subsystem — the
premise-absent case exactly — hits it as an unknown prefix and fails open at
the worst spot.

## branch_pr_state — open, finished, none or unanswered

(`kingdom/bin/km`, `branch_pr_state`)

branch_pr_state <branch-name> -> open | finished | none | unanswered

The one place seat_refresh_step asks GitHub what the branch's PR record says
(0008). Fail-closed in the same shape as hold_open_pr_files above: a timeout
and an exit-status check, with an empty answer distinct from no answer —
jq-null over a failed query is the known trap (gqlc-z1qw), which is why the
helper reports `unanswered` on non-zero exit rather than folding it into
`none`. Two paths to one HOLD must stay tellable apart (0008 §3): the words
`open` / `none` / `unanswered` name the reason so seat_refresh_step's verdict
lines can carry it through to the journal.

headRefOid is in the field list although this consumer ignores it: gqlc-guq3's
consumer needs it, and the contract should not reshape under her.

## cmd_hold_verdict — fail closed per candidate, never a silent drop

(`kingdom/bin/km`, `cmd_hold_verdict`)

Fail closed per candidate, never a silent drop: gqlc-z1qw was a jq
abort that read as a healthy zero, and one bad row must cost one line
rather than the whole run.

`null` is EXCLUDED from this arm on purpose (gqlc-bcir). It used to be
caught, and it is not corruption: measured 2026-08-22 over the ready
queue, 293 beads had an array and 14 had null — every one of those 14
was reported to the operator as "malformed candidate", 14 of the 27
holds the town was issuing. A majority of the hold census was an
unlabelled bead described in the vocabulary of data corruption, and
the residue arm below — the code written for exactly that bead — was
unreachable for all of them.

The type is NAMED rather than only the type that was wanted, because
"labels is not an array" sends a reader looking for a corrupt record
without telling them what to look for.

## cmd_hold_verdict-2 — a review bead's premise IS an open PR touching its subject

(`kingdom/bin/km`, `cmd_hold_verdict`)

The premise of a review bead IS an open PR touching its subject, so
the three arms below that ask "might an open PR be touching this?"
are inverted for it: they hold the review for as long as it is wanted
and release it when the PR merges. The premise-absent arm above them
is NOT inverted — a review naming a path no branch has is a typo — so
the exemption is per-arm, never a pass for the class (gqlc-n4oe).

No apostrophes in this jq program: it is a single-quoted shell string.

## reap_scratch — the town's only unattended reclamation of shared scratch

(`kingdom/bin/km`, `reap_scratch`)

The town's only unattended reclamation of the shared scratch filesystem
(bd gqlc-u078). internal/tools/tmpreap has existed since PR #1057 and was
wired to nothing: every reclamation to date was a person or an agent noticing,
and the one time nobody noticed, /tmp hit 99% of its 1048576-inode cap and
began refusing writes town-wide while `df -h` showed 5.9G free (bd gqlc-vze6).

ADVISORY, in every direction. It cannot fail the sweep, because the sweep's
job is the Պահակ's round and a filesystem tool that could stop it would be a
second way for the town to go quiet. It cannot hang the sweep either: the
guard unit is OnUnitActiveSec, so the next tick cannot begin until this one
ends, and one wedged walk over a large /tmp would be a stopped cadence. The
timeout is generous because the walk is genuinely large, and short of the
15-minute guard interval so two ticks cannot overlap.

The threshold and the deletion policy live in the justfile recipe and in the
tool, not here. This function decides only WHEN to ask.

## guard_unanswered — which seats hold Ռաֆֆի's letters unread, and for how long

(`kingdom/bin/km`, `guard_unanswered`)

Which seats are holding letters of Րաֆֆի's that they have not read, with how
many and what state they are in.

He wakes into a FRESH session every round — km-seat execs claude with no
--continue — so nothing he wrote last round is in front of him unless
something puts it there. On 2026-08-24 he wrote to the mayor five times
between 04:05Z and 05:05Z about a heartbeat frozen at 03:44:54Z, and the fifth
letter withdrew the first four ("just sitting rather than STUCK … You've done
good work today") on no new evidence at all. That is not a guard overruling
his own record; it is a guard who could not see it, and the last letter in a
box is the one a reader trusts (bd gqlc-pff4).

Two is the threshold: one letter is not yet a repetition. The seat's STATUS
rides along because it is what distinguishes the two readings the count alone
cannot — a seat that is asleep may simply not have been woken since, while an
awake one has had the letter in front of it and left it there.

## cmd_guard_sweep — above every early return, and why that placement is the design

(`kingdom/bin/km`, `cmd_guard_sweep`)

ABOVE every early return below, and that placement is the design
(bd gqlc-u078). Scratch accumulates from AGENTS, not from the town's own
loop: a halted town, a town that is down, and a town whose Պահակ is
already awake each still have sixteen worktrees and whatever factory wave
is running writing into /tmp. Every one of those states returns before the
wake, so a reap placed beside the wake would be disarmed in precisely the
conditions that produced the incident — 2026-08-22 filled the inode table
while the town was HALTED and no seat was routing at all.

The halt does not bind it, for the reason the halt binds the wake: this
starts no session, spends no tokens and touches no seat, so Article VI.4's
reservation of the halt to Սեդրակ and Անդրանիկ is not engaged by it. What
a halt promises is a quiet town, not a filling disk.

## cmd_guard_sweep-2 — the halt binds every timer-driven wake, not just the dispatcher's

(`kingdom/bin/km`, `cmd_guard_sweep`)

The halt binds every TIMER-driven wake, not just the dispatcher's. This is
the second one, and each wake here starts a real claude session, so a halt
that missed it went on spending tokens once per guard cadence — and since
Article VI.4 reserves lowering a halt to Սեդրակ or Անդրանիկ, whoever
raised it had no way to find that out (gqlc-6dzi). `km wake` and
`km mail` are deliberately NOT bound: a halted town still has to be
workable by the operator who is going to lower the halt. This named
`km herratsayn` until gqlc-cw7h; #1595 had already deleted that verb, and
the sentence outlived it.

## patrol_open_ids — decision 0004 §2, the execution half

(`kingdom/bin/km`, `patrol_open_ids`)

Decision 0004 §2, the execution half. Decision 0003 change 2 names the judges' PATROL
duty as the compensating control on the whole unreviewed-merge regime — very
nearly every PR now merges on green gates with the author as its only reader
— and measured at c129a0a5 patrol was two sentences in the tree,
kingdom/README.md line 77 and that clause of decision 0003. It had no trigger, no
target and no output shape. A seat runs only when woken; dispatch wakes a
seat only for a bead; nothing filed one. So the compensating control on the
town's central throughput trade could not begin at all.

Filed by km rather than by Րաֆֆի during his round, because a step in a soul
is a step an agent can forget and a line in cmd_guard_sweep is not. Called
AFTER that command's early returns, which is the whole reason it sits at the
bottom of the function: a halted town files no patrol bead (Article VI.4),
and a tick that returns because Րաֆֆի is already awake files none either.
Neither is a missed obligation — the next tick meets the same empty board.

BOUNDED TO ONE, PERMANENTLY, and not configurable. Patrol's queue depth is
one whatever the merge rate. That clause is what keeps decision 0004 inside decision
0003's constraint, and a later change making it a setting reintroduces
exactly the queue decision 0003 was written to drain.

The bound is measured as NOT CLOSED, never as `--status open`. In bd,
`--status open` is the literal status and excludes in_progress and blocked,
so a patrol bead a judge had already claimed would read as absent and the
next tick would file a second — the bound gone, silently, in precisely the
state it exists for. That is not hypothetical: the same `--status open`
reading made km doctor's identity arm miss a P0 (gqlc-18br, gqlc-c7b5). Any
status this code does not recognise counts as open too, which is the safe
side for a bound.

## patrol_window_is_empty — the bound measures the QUEUE and not the work

(`kingdom/bin/km`, `patrol_window_is_empty`)

The bound above measures the QUEUE and says nothing about whether there is
anything to patrol, so a judge who closed an empty-window bead HONESTLY was
handed another one four minutes later. Measured 2026-08-24 (bd gqlc-gsr9):
gqlc-ot5u closed 03:15:11Z with master at 44ee6224, guard-sweep filed
gqlc-z298 at 03:19:33Z, dispatch woke a judge, and at 03:21:30Z after a fetch
`git log 44ee6224..origin/master` was EMPTY. The wake produced nothing and
could produce nothing. At guard_minutes=15 the effective patrol cadence
collapses to the guard interval — an arithmetic ceiling of four judge wakes
an hour across any merge-free stretch, and the merge-free stretches are the
overnight and quota-walled ones. That is the wake burn decision 0003 exists to
drain, restarted by its own compensating control.

THE POSTURE HERE IS INVERTED FROM THE BOUND'S, deliberately. A bound that
cannot be measured is not raised, because filing blind queues a bead every
cadence for as long as bd is unwell. A WINDOW that cannot be measured is
filed anyway, because a patrol that stops is coverage lost silently and one
empty wake is the cheaper failure. So only a MEASURED emptiness is silent:
an absent origin/master, a git that refuses, and a board with no closed
patrol bead to start a window from all file.

`--since` is INCLUSIVE at the second (measured, git 2.55.0), so a merge
landing in the same second as the close is read by the next round rather
than falling between the two.

## dirty_open_prs — decision 0006 §1, the execution half

(`kingdom/bin/km`, `dirty_open_prs`)

Decision 0006 §1, the execution half (bd gqlc-wz47). Nothing told an author their
open PR had gone DIRTY. Every case on 2026-08-22 was found by a person
happening to look, and Արամազդ's estimate was that three of seven affected
authors did not know. A PR goes DIRTY through nothing its author did: a merge
that appends to a shared registry invalidates every open PR appending to the
same registry, and he found three live registries in one afternoon.

The census rides the guard cadence rather than becoming an actor: km already
runs on this timer and already carries mail. MAIL, NOT A WAKE — a DIRTY PR is
routine, the census expects to find them often, and a wake per conflict would
spend concurrency slots on the ordinary case.

## dirty_conflicted_paths — --name-only is the line to trust, and an unknown head is neither clean nor dirty

(`kingdom/bin/km`, `dirty_conflicted_paths`)

--name-only is the line to trust, and it is not a preference. Without it
`git merge-tree --write-tree` prints `Auto-merging <path>` for files it merged
SUCCESSFULLY, on lines adjacent to `CONFLICT (content): <other path>`; read as
a block that says the first file conflicted, and it says the opposite.
Արամազդ made this error first and caught it against his own count.

The head is RESOLVED before it is merged, and that is not belt-and-braces.
Measured 2026-08-23 with git 2.55.0: `git merge-tree --write-tree --name-only
<base> <an oid this repo does not have>` exits **1** — the same status as a
genuine conflict — writing `not something we can merge` to stderr and NOTHING
to stdout. So rc is not the three-way answer it looks like: read on rc alone,
a head whose branch someone deleted comes back with an empty conflict list
and is reported as CLEAN. Unknown is neither clean nor dirty, and the caller
says so out loud.

## unit_health — InactiveEnterTimestamp and Result describe the PREVIOUS run

(`kingdom/bin/km`, `unit_health`)

gqlc-67ml. InactiveEnterTimestamp and Result describe the PREVIOUS
run, and while the timer is actually firing the unit they are
transiently inconsistent with each other: 'guard last run succeeded'
flapped warn then ok between two doctor runs a minute apart, with
Result=success ExecMainStatus=0 reported both times. So a unit systemd
says is running now gets no verdict on the run before it — the run
under way has not ended, and the record of the one before it is being
overwritten. A cadence of two minutes makes that a wolf cry, and a
verdict on an unfinished run is an unmeasured state rendered as a
measured one, which is the thing this file exists to refuse.

## unit_health-2 — an empty timestamp is not a history of never failing

(`kingdom/bin/km`, `unit_health`)

gqlc-28ysg. An empty InactiveEnterTimestamp is not a history of never
having run, and this branch used to render it as one — "its timer has
never fired it", about a timer this function has never queried.
systemd garbage-collects an inactive service nothing references any
more and re-loads it from disk with empty runtime state; LoadState
stays `loaded`, so there is no tell. Measured 2026-08-30 with
throwaway units: a service that never started and one whose timer was
stopped three seconds ago agree on every property sampled here, and on
the TIMER's own LastTriggerUSec, which is erased with them. That
history survives only in the journal, so km cannot have it cheaply and
this says nothing about it.

What it can measure is whether anything will fire the unit AGAIN, and
that is the half the operator acts on: the two states this used to
collapse want opposite responses — install the units, or find out who
stopped a healthy timer four minutes ago.

## status_unresponsive_after — how stale a heartbeat has to be

(`kingdom/bin/km`, `status_unresponsive_after`)

How stale a heartbeat has to be before the board says UNRESPONSIVE.

This does NOT contradict km:227-231, and a reader who hits both should be able
to see why without guessing which one is a bug. That comment refuses to let
seat_session_live REAP on the heartbeat, and its measurement is a 53-second
tool call: over that span the age separates mid-turn from between-turns, not
working from abandoned. Both claims survive here because this threshold is two
orders of magnitude away from that measurement and because nothing acts on it.
What is rendered is the AGE, always, which is a fact and not a judgement; the
marker is a second line of text. Nobody is freed, nudged, or ended by it —
gqlc-eier is explicit that a modal is not consent to be killed (VI.2), and the
instrument's whole job is to let a human or the mayor decide.

30 minutes: 15x the 2-minute dispatch cadence, and 34x the longest tool call
anyone here has measured. The two freezes that motivated this were 80 and 152
minutes (Միհր on a usage-limit modal, Անահիտ on a shutdown modal, both while
the merge gate sat shut and every board read healthy). A seat legitimately
inside one turn for half an hour is possible and would be flagged; the cost of
that is one line of text asking someone to look at a pane.

## status_stalled_after — how long without a completed tool call

(`kingdom/bin/km`, `status_stalled_after`)

How long without a COMPLETED tool call before the board says a live seat is
making no progress. Separate from the heartbeat threshold above and necessarily
so: that one measures whether the instrument reported, this one measures
whether anything happened, and the whole point of gqlc-r3ac is that the first
stays green while the second runs dry.

20 minutes. The two freezes it is sized against were 80 and 152 (gqlc-eier),
and the longest legitimate single tool call anyone here has a number for is the
~10-minute pre-push suite — so 20 is twice the longest known honest wait and a
quarter of the shortest measured freeze. Long-running Agent spawns can exceed
it and will be marked; that costs one line of text asking a human to look at a
pane, which is the same trade UNRESPONSIVE already makes. There is deliberately
NO per-tool allowance: a per-tool exemption is a list of names, and the wedge
this exists to catch was a tool whose name nobody had written down.

## seat_escalated_tag — has the recovery ladder given up on this seat

(`kingdom/bin/km`, `seat_escalated_tag`)

Has the recovery ladder given up on this seat? The WEDGED and NOWORK lines
name seats the ladder is supposed to be asking, so whether it has STOPPED
asking belongs on the same row — without it the two readings are a board that
says "being handled" and a dispatcher that has been silent for hours, which is
the state gqlc-3evsn found and could not see.

Empty and rc 0 when there is no marker: this decorates a row, and a seat nobody
has escalated must render as the row it already was.

## cmd_status — MARKED, not swallowed and not fatal

(`kingdom/bin/km`, `cmd_status`)

MARKED, not swallowed and not fatal (gqlc-bn5r). `|| inprog=""` here made
an unanswerable question render identically to an answer of "nobody is
working": an em dash in all sixteen BEADS cells, which is also what a
genuinely idle town looks like. That equivalence is what hid gqlc-z1qw
for the kingdom's whole life — routing was dead and the board was the
only witness, and it was never contradicted.

Not `die`, though dispatch does exactly that on the same query. Dispatch
is a timer job whose refusal belongs in the journal; this is the one
glance a human takes at the town, and a status command that dies is a
status command nobody runs. So it prints everything it CAN and names the
part it could not read.

One capture over the whole pipeline, deliberately: bd failing and jq
aborting on what bd returned are two different faults with one remedy
here, and under `set -o pipefail` this catches both. Checking bd's rc
alone would leave the jq half fail-open — measured, that half renders the
ready line as "  architect,   warrior", a blank where a count should be.

## cmd_status-2 — the state a citizen reaches by ordinary SUCCESS

(`kingdom/bin/km`, `cmd_status`)

The state a citizen reaches by ordinary SUCCESS — finish, write the
handoff, forget `km sleep` — and the LEAST available one a seat has.
Every dispatch pass wakes ASLEEP seats only, so an awake seat is
unroutable, and its live session still spends a max_active slot: the
same record costs the town twice. Six of them held all five slots for
eleven hours while this table called each of them `awake`, which is
the same cell it prints for a citizen mid-work (gqlc-5vp7).

The STATE cell and not a line alone, and unlike UNRESPONSIVE — which
deliberately leaves STATE saying what the status FILE says, because
the heartbeat is a separate instrument that must be able to disagree
in the open. This is not a second account of the same field; it is the
same kind of live probe as the `awake?` above it, refining the cell
from the pane rather than contradicting it.

MARK, never act. cmd_reconcile cannot end this session: from outside,
idle and thinking are told apart only by one photograph of a TUI, and
ending a citizen's session on that is VI.2 (gqlc-971s). What this
buys is that an operator who can SEE the seat fixes it in one command,
and a marker costs nobody's uncommitted work when it is wrong.

## cmd_status-3 — 60s, against the 5s poll km-seat sleeps between wake checks

(`kingdom/bin/km`, `cmd_status`)

60s is well past the 5s poll interval km-seat sleeps between wake
checks and well short of anything an operator would want to sleep
through. The seven-hour orphans in gqlc-2vxs would have surfaced
here within a minute if this line had existed.

Gated on state == asleep, because that is the shape 2vxs measures:
a resting citizen with a wake nobody read. The other states have a
legitimate reader — awake/asleep-pending seats keep the wake file
until claude exits and km-seat loops back to consume it, so an old
wake file there is the runner working, not the runner missing. The
IDLE / WEDGED / UNRESPONSIVE lines already speak for those cases.

## cmd_status-4 — `-n 0`, or the counters are silently FLOORS rather than counts

(`kingdom/bin/km`, `cmd_status`)

`-n 0`: without it these counters are silently FLOORS, not counts — and a
precise-looking wrong number is worse than none, because the mayor sizes
his standing labelling chore off this line (gqlc-mlca).

`|| ready="[]"` substituted an EMPTY BOARD for an unanswered question and
then counted it, printing "0 architect, 0 warrior, 0 judge, 0 unlabelled"
— four precise-looking numbers, none of them measured (gqlc-bn5r). The
`jq -e .` is the second half and not decoration: bd can exit 0 and hand
back a lock message, and the counting jqs below then each fail
independently under command substitution, rendering "  architect,
warrior" — a blank where a count goes, which is a worse lie than a zero
because nothing in the line says a query failed.

## cmd_status-5 — the king's box is NOT a seat's box

(`kingdom/bin/km`, `cmd_status`)

The king's box is NOT a seat's box, and the old line printed the same word
over both (gqlc-2abx). A letter leaves inbox/ only when someone runs
`km mail read`; every seat is a process that runs it, and Անդրանիկ is a
human who reads the file in an editor. Measured 2026-08-22: inbox 30,
read 0, zero reads since the town was founded — a number that reads
identically whether he has read every word or none of them.

This is worth a distinction and not just a softer word because the count
had already changed the mayor's conduct twice in one day, both times
toward silence: two digests to the king were withheld with "he has 30
unread" written down as the reason, and the thing withheld was a bench
decision only he could make. A metric that argues for silence toward the
one person who can unblock you is worse than an absent one, because an
absent one sends you to look.

## cmd_doctor — TWO labels, not one prefix

(`kingdom/bin/km`, `cmd_doctor`)

TWO labels, not one prefix (gqlc-67ml). A row that printed one label under
both outcomes STATED THE OPPOSITE of what it measured whenever the label
was phrased as a positive assertion: 'warn: town is up' over a town that
was down, 'warn: guard last run succeeded' over a run that had failed,
both in a real run on 2026-08-23. Four characters of prefix were carrying
the whole negation of a sentence written to mean the other thing, and an
operator scanning this on restart morning reads the sentence.

So the failing phrasing is supplied per call and says what is wrong. It is
a required argument, not a defaulted one: a default would silently
reinstate the defect for every row that forgot it.

## cmd_doctor-2 — what check_rc adds over check

(`kingdom/bin/km`, `cmd_doctor`)

check_rc adds ONE more outcome: a named rc with a sentence of its own, for
a command whose exit status says more than yes/no. Two rows below have
one — herdr_server_up answers 0 running / 1 not running / 2 could not ask,
and `systemctl --user is-enabled` answers rc 0 enabled / 1 disabled / 4
not-found. A two-valued row asserts one cause for both of the last two,
whose remedies differ. That is the defect gqlc-ygcfi and gqlc-pgbfp each
fixed one level down, arriving here (gqlc-vdko8).

stdout is dropped, stderr is NOT. `check` swallowed both, and the doctor
is the command an operator runs precisely when something is wrong — the
one caller guaranteed to throw the reason away. Measured 2026-08-30 over
every row here: only `mkdir -p` and herdr_server_up write to stderr at
all, and both write a diagnosis naming what actually failed. `command -v`
and `systemctl --user is-enabled` write none on any outcome, so passing
stderr through costs those rows nothing and no per-row redirect is needed.

## cmd_doctor-3 — the second unreachable shape, and why it is a WARN

(`kingdom/bin/km`, `cmd_doctor`)

The second unreachable shape (gqlc-t4zx), and deliberately a WARN where
the arm above is a FAIL. The difference is what the finding rests on. A
bead in progress with no assignee is unreachable as a matter of the
dispatcher's own arithmetic; an unresolvable assignee is unreachable as a
matter of a roster somebody maintains by hand, so a person who took a bead
before their name was added reddens this arm through no fault of the
board. A gate that goes red when a human takes work is one operators learn
to run with, and this file has that failure written into it twice already.
A named warning is the thing an operator can act on; the acting is theirs.

## cmd_doctor-wall — a standing wall is a WARN, and only its age makes it a FAIL

(`kingdom/bin/km`, `cmd_doctor`)

Three outcomes, and the middle one is the point of the row.

A flag that is standing is **not** a defect. It is the mechanism working: the
account is stopped, dispatch is withholding routing, and it will probe again
inside ten minutes. A row that reddened on the flag alone would be red for the
whole of every wall — through the exact hours an operator most needs the board
to mean something — and would train everyone to ignore it. So a standing flag
reports `warn:` and says in the same sentence what is being withheld and how
often the probe runs, because an operator reading `warn` needs to know whether
to act (no) and when it resolves itself.

What IS a defect is a flag nobody is clearing. Past `WALL_STALE_SECONDS` (12h)
the row FAILs, and it deliberately names both readings rather than choosing:
either the probe loop has stopped — no dispatch pass at all, or a probe that
never succeeds — or a wall has genuinely stood that long, which a weekly cap
can do. Both are operator news and neither is diagnosable from here. The
measured outages this machinery was built for were 8h44m and ~9h, so 12h sits
above the longest wall we have actually seen and below a day.

The unreadable arm FAILs too, and it is not pedantry: `wall_raised_epoch`
failing means the age cannot be computed, so the 12h arm above it can never
fire. A flag with a corrupt first line would otherwise stand forever behind a
green board — silent withholding of routing, which is the failure this whole
bead exists to end.

Witnessed by backdating a flag to 13h and reading the row (FAIL), to 20m
(warn), and removing it (ok). Blinding the 12h arm turned the 13h case back
into the `warn` line, so the FAIL comes from the age and not from the flag
merely existing.

## cmd_doctor-fence — a count pin over a prohibition that has no other enforcement

(`kingdom/bin/km`, `cmd_doctor`)

The town must never invoke the vendor's paid-overflow command, under any
condition (Անդրանիկ, 2026-08-30, gqlc-tdciz). Nothing else in the machinery
can enforce that: the string is legitimate to READ — the wedge detector
matches it in a pane, and the wall probe's predicate matches it in the probe's
own output — so a plain "must not appear" grep would be red on the honest
tree, and a grep that skips readers cannot tell a reader from a composer.

So the row pins the COUNT. Three matches under `kingdom/bin/`, all three
readers, enumerated in the failure message itself so the next person does not
have to rediscover which three are allowed. A fourth line is a line somebody
added, and the only thing a new line can be doing with that string that the
three do not already do is composing or typing it.

Two limits, stated because the row does not cover them:

**It is one-directional.** The row FAILs when the count EXCEEDS the pin, so
deleting a reader — the wedge detector, say — passes. That is why the `ok:`
line prints the observed count against the pin rather than saying nothing: a
drop from 3 to 2 is visible to a reader of the output, and is not caught by
the gate. Making it an equality would red the board on any legitimate
refactor of a detector, and the harm it guards against is invoking the
command, not failing to grep for it.

**It sees `kingdom/bin/` only.** A composer added under `kingdom/brain/` or in
a playbook is outside it. The prohibition is wider than the fence; the fence
is where machinery that could actually run it lives.

Both mutants were run. Appending ` or run /extra-usage` to one ladder ask took
the count to 4 and the row FAILed naming the offending file. Deleting the row
itself made the probe report zero fence rows rather than a passing one — a
deleted gate does not read as a green one.

## cmd_doctor-4 — "can any pass reach this bead" versus "can this seat"

(`kingdom/bin/km`, `cmd_doctor`)

The stranded arm above answers "can any pass reach this bead"; this one
answers "did the last run actually place the town's highest work", which
is a different question with the same consequence — nothing happens, and
every board reads healthy (gqlc-tz95).

Graded on AGE and not on the fact. Under the threshold the commonest
cause is a class whose wakes are already queued and which routes on the
next tick without anybody acting; failing hard on that trains the reader
to skip the row, which costs more than the row is worth.

## cmd_doctor-5 — the identity arm

(`kingdom/bin/km`, `cmd_doctor`)

THE IDENTITY ARM (gqlc-0rv9). `.githooks/commit-msg` refuses an
implausible author and `.github/scripts/check-pr-authors.sh` refuses one
in Actions — both gate GIT. Nothing gated BD, and both consume the same
ambient `git config user.email`: when a fixture identity leaks into the
shared repo config, git refuses the commit and bd silently writes the
false address onto every bead created in the window. Measured
2026-08-22: 15 beads across 5 citizens, in two windows 50 minutes apart
(km@test, then fixture@example.invalid). The human noticed at 03:20Z; the
second window had closed at 03:01Z. The noticing arrives after the window
structurally, every time, which is what makes this worth an arm.

SOURCED, not copied. This is the predicate's third consumer, and
gqlc-gy3q is the record of what a second copy costs: while that bead was
open two PRs carried independent copies that had ALREADY disagreed on
four inputs. Copying it here would put the divergence between what git
refuses and what bd is audited for — silently, in the direction where
master accepts what commit-msg rejects. That file is read-only to this
one: km defines nothing in it and changes nothing about it.

## cmd_doctor-6 — the population arm, and what `--status open` hid

(`kingdom/bin/km`, `cmd_doctor`)

THE POPULATION (gqlc-c7b5). This asked `--status open`, and in bd that
is the LITERAL status `open`, not "not closed": a bead in in_progress
or blocked was never queried and the arm reported on the rest with
full confidence. Measured 2026-08-23 while repairing gqlc-3x1r — the
arm named exactly three open beads owned by km@test and was silent
about gqlc-o13d, in_progress, owner fixture@example.invalid, and a P0.

So: `--all`, which applies no status filter, and CLOSED is excluded in
jq rather than by asking for three statuses by name. Two reasons for
that shape. A named list has to be revised every time bd gains a
status, and it fails silently in the direction of omission when nobody
does. And closed is excluded on purpose, not incidentally: a closed
bead's owner is history — the window is shut, there is nothing to
reassign — so failing on one would leave this arm permanently red over
the beads gqlc-0rv9 already repaired, and a permanently red gate is
one nobody reads.

`-n 0` for the row cap, which is a SEPARATE default from the status
filter and disables only itself.

## cmd_doctor-7 — the coverage arm

(`kingdom/bin/km`, `cmd_doctor`)

THE COVERAGE ARM (gqlc-p1ek). The deployment half is already fixed —
km-seat refreshes a parked seat at wake — but the refresh deliberately
does NOT move a seat holding work, and it tells only that seat, in its
own wake banner. Nobody sweeping the town could see the gap, and the
population it covers is exactly the seats that are working, i.e. the ones
about to push. SOFT, not hard: a worktree that is legitimately mid-PR is
not an operator error, and Constitution VI.2 forbids anything here from
coercing the citizen standing in it. It is a mark.

## cmd_doctor-8 — the launch arm

(`kingdom/bin/km`, `cmd_doctor`)

THE LAUNCH ARM (gqlc-8dsa). Every other row in this file reports on
CONFIGURATION; this one reports on the sixteen processes. That distinction
is the whole finding: `km cfg claude permission_mode` answered
bypassPermissions truthfully while fifteen sessions had no such flag, and
`km status` called them awake and healthy for a day.

HARD, unlike the coverage arm above it. A worktree legitimately mid-PR is
behind on hooks and that is nobody's error, so coverage is a mark; a
session with no soul or the wrong model is not a state the town has any
legitimate route into. The one axis a citizen may legitimately vary
(--effort, V.6.2) is the one seat_launch_probe deliberately does not fail
on.

Per seat and never in aggregate: an "all seats ok" over sixteen sources
passes when fifteen go silent, so each seat is judged alone and an
unresolvable process is named rather than skipped.

## cmd_doctor-rooting — a runner rooted in a citizen's worktree

(`kingdom/bin/km`, `cmd_doctor`)

A runner executes the km-seat file its launcher happened to be standing next
to. Measured 2026-09-02 by Սեդրակ from /proc: nine of sixteen were running
km-seat out of `gqlc-seat-sedrak` rather than the deploy root, put there by
whoever's shell started them (bd gqlc-lzkps, the residual of gqlc-qs4jq).

WHY NOTHING CAUGHT IT, and it is not that gqlc-s67rz's re-exec is wrong. That
guard compares the DEPLOYED bytes against the parse the runner started with and
execs the deployed copy when they differ, which always heals toward the deploy
root. But a runner rooted in a worktree parked at origin/master is running
bytes IDENTICAL to the deployed ones, so the sums agree and it never re-execs.
The heal is dormant exactly while the hazard is dormant, and the state it
cannot leave is the one that looks healthy. `km seat-refresh` cannot see it
either, because the tree it would report on is fine.

What makes it a hazard rather than a curiosity is that one `git checkout` in
that citizen's worktree rewrites the bytes a live runner is executing, and the
frozen self_sum means nothing notices. On 2026-09-02 that same worktree being
parked at an old commit froze ten seats for nine hours while every board read
healthy.

REPORT, NOT REPAIR. Refusing or re-exec'ing means ending a runner, and a runner
ending means its citizen's session ends, which is not km's to do on a timer
(VI.2). The population drains itself as seats respawn from the deploy root — 9
to 2 in the four hours after the measurement above — so what was missing was
never a mechanism, only a reader.

## cmd_doctor-9 — an armed timer is not a working one

(`kingdom/bin/km`, `cmd_doctor`)

An enabled timer says the machinery is armed, not that it works: the
dispatcher routed nothing for its whole life with both timers green
(gqlc-z1qw), and now that it dies on a bad query, the death lands only in
the journal unless something asks the unit how its last run went.

Not `check` rows: unit_health has more outcomes than two. A boolean row
threw that away and printed one label — 'guard last run succeeded', with
the run having failed (gqlc-67ml). So the row renders unit_health's own
sentence, which is the thing that was measured.

But rendering and GATING are different questions, and choosing echo over
`check` answered the first while silently answering the second too: it
took the row out of the exit status entirely. So on 2026-08-29 the
dispatcher had exited 1 on every tick for eleven minutes, routing nothing,
and `km doctor` said so and exited 0 (gqlc-vqqr). This is the town's only
liveness check and a dead dispatcher is the one condition under which the
town does no work at all.

FAILED is therefore its own arm and is hard. It is matched BY NAME rather
than by making the catch-all hard, because NOT INSTALLED falls there too
and is not a failure: `just kingdom-install` runs `km doctor` BEFORE
`km install-units`, so a hard catch-all would refuse the command whose
whole job is to install the units it is complaining are absent, on
precisely the fresh box that has never had them. An operator who does hit
the hard arm and needs the units rewritten anyway can run
`km install-units` directly, which does not consult doctor.

## cmd_doctor-10 — the linger arm

(`kingdom/bin/km`, `cmd_doctor`)

THE LINGER ARM (gqlc-yxnf). Not a `check` row, and deliberately not a
pass/fail one: both answers are legitimate configurations, and which is
correct depends on something km cannot see. `loginctl enable-linger` is
what a headless town NEEDS — without it the user manager is torn down at
logout and neither timer fires again until the next login. With it, the
timers keep dispatching after logout and across reboots, with nobody
attached to any pane. So the row NAMES the state and its consequence.

A boolean row here would have to pick one of those as the failure and
would then be wrong for half the deployments; what nobody could see
before is which of the two this box is in, and that is the whole ask.

The unanswerable case is the one that must not read as either: CI has no
user bus (the same reason every systemctl row above is soft), and
loginctl exits non-zero with nothing on stdout there. Reporting "not
lingering" for "could not ask" is the fail-open this file is organised
against (gqlc-z1qw).

## seat_freshness — core.hooksPath is RELATIVE, so a seat runs its own checkout's hooks

(`kingdom/bin/km`, `seat_freshness`)

core.hooksPath is RELATIVE, so a seat runs the hooks in its OWN checkout and
acquires a merged one only when its worktree next moves. `km up` parks a seat
detached and nothing moves it again, so merging a gate deploys it to nobody
and the town's drift detectors all read clean — they check the hooksPath
VALUE, which stays '.githooks' in a seat whose .githooks/ predates the gate
(bd gqlc-xtre: 7 of 14 seats had no push guard hours after it merged).

Exit 3 means "still exposed": held for work in flight AND behind on hooks.
km-seat turns that into a banner in the citizen's first message.

## seat_freshness-2 — the freshness contract

(`kingdom/bin/km`, `seat_freshness`)

seat_freshness <seat> <target-sha> -> "<state> <behind_all> <behind_gates> <ahead> <detail>"

ONE derivation, read by seat-refresh (which acts on it), by status and by
doctor (which only report it). gqlc-p1ek asks for exactly this and says why:
the obvious second derivation is to read `git config --get core.hooksPath`,
and that value is '.githooks' in a seat whose .githooks/ predates the gate —
measured 2026-08-22, all 14 seats read healthy while 7 held no push guard.
Coverage is a question about CONTENT, so it is answered by counting commits
the worktree has not got, against the paths that ARE the gates.

Deliberately no fetch here. status renders this per seat on every call, and a
board that stalls on the network is a board nobody runs; seat-refresh fetches
before calling in. How fresh the ref being judged against is therefore depends
on the seat's layout: a linked worktree shares one remote-tracking ref with the
whole town, so it is as fresh as the last fetch by anybody, while a clone
(gqlc-w5bh) has its own and is only as fresh as ITS last fetch.

States, and the distinction gqlc-d38n turns on — "behind because working" is
not "behind because parked", and only the second is a defect. Extended by
decision 0008 to name a fourth state git alone cannot judge:
  current   HEAD is origin/master
  parked    clean, and holding nothing that some other ref does not already
            hold, so moving it can lose nothing. THIS is the 14-commit case.
  working   a dirty tree, or commits that live in this checkout and nowhere
            else. Legitimately behind; reported, never counted as stale.
  published on a branch, clean, every commit held by its upstream. Nothing
            is lost by moving, but "on a branch" is not "finished" — that
            question lives in the PR record, which the board cannot ask
            without a network call, so it is deferred to seat_refresh_step
            (0008). Legitimately behind; reported, never counted as stale.
  unknown   no worktree, or git could not answer. Never rendered as current:
            a question that was not answered is not a clean answer.

## seat_freshness-3 — what makes a move unsafe is holding the ONLY checkout

(`kingdom/bin/km`, `seat_freshness`)

What makes a move unsafe is not being on a branch; it is holding the ONLY
copy of something. Being on a branch used to be sufficient for `working`,
and `working` never moves, so a seat whose branch had merged was reported
exposed forever and resolved never — measured 2026-08-23, three seats
63-65 commits behind on the gate paths, every one of them clean with
nothing unpushed (bd gqlc-5t2u).

Asked as "does any ref elsewhere hold this?", which needs no opinion about
merges. Deliberately NOT `--is-ancestor` and not patch-id: a squash-merged
branch is neither an ancestor of master nor patch-identical to it, so both
refuse such a seat forever, and patch-id said "not on master" for all five
of ayg's merged commits — confidently and wrongly.

  on a branch   commits its upstream has not got
  detached      commits target has not got, as before

One question, not two, FOR THE HOLD: "no upstream configured" and "git
would not answer" take the same branch and are held for the same reason —
nothing was shown to hold this. They are told apart for the REPORT below,
which is a different question. Asking rev-parse first and rev-list second would read
better and would put an unreachable fallback under the second call, which
a mutation duly SURVIVED: rev-list cannot fail for a ref rev-parse has
just verified, so no row could ever reach it.

An unresolvable @{u} is the SAME hold whatever caused it — nothing was
shown to hold this — so the hold does not move and only the sentence does
(gqlc-guq3). @{u} cannot say which case it is: it is the one question
whose answer is identical. The branch's config can, offline, with no
network call and no gh auth, because neither a remote-side deletion nor a
prune touches it. Measured 2026-08-29 in a scratch repo, after push -u
then remote-side delete then fetch --prune:
  no upstream configured   branch.<n>.remote UNSET, branch.<n>.merge UNSET
  upstream configured      both INTACT, tracking ref absent
Configured is ALL the keys answer, and the sentence below says no more.
It is not "published to a remote": an upstream may be a local branch,
where remote is "." and nothing was ever pushed anywhere (Միհր, PR #1763
F1 — the earlier wording said "was published" and lied about that case).
Read the config keys, not git's two error strings: those also differ, and
they are human-facing text, unversioned and localisable.

Published is NOT safe, so this changes no verdict: the branch reached a
remote once, which says nothing about whether the work landed, and a
branch pushed to the wrong upstream (gqlc-tfh1) is held by nothing.

## seat_refresh_step — one seat's verdict, and the one move that is ever safe

(`kingdom/bin/km`, `seat_refresh_step`)

seat_refresh_step <seat> <check> <target> <stale_note>

One seat's verdict and, unless --check, the one move that is ever safe.
Echoes a line beginning "<seat>: "; the exit status is the whole point of
separating it from the printing:

  0  covered — current, refreshed, or held with its hooks already current
  3  still exposed, and the tree is holding work: a branch or uncommitted
     changes stand in the way of the move
  4  unjudged — no worktree, or git could not answer, or the move failed
  5  still exposed, but only because --check declined to move it; the tree
     is parked and nothing is in the way

All three non-zero statuses are distinct because their remedies are addressed
to different people: 3 wants the citizen standing in that tree to rebase, 4
wants `km up` or a human with a shell, 5 wants the operator who typed --check
to type it again without. Collapsing 3 and 4 would send an unseated seat's
operator to a citizen who does not exist; collapsing 3 and 5 did send one to
ask three citizens to rebase trees with nothing in them (gqlc-jjcj).

5 is reachable only under --check. km-seat is the only caller that branches
on this status and it never passes --check (km-seat:183), so no existing
reader of 3 can be handed a 5.

SHARED by the single-seat path and the sweep, not copied into each. The two
differ only in whether an unjudged seat aborts, and gqlc-gy3q is the record
of what a second copy of a predicate costs — two copies of one rule had
already disagreed on four inputs while that bead was open.

## seat_refresh_step-2 — safe, finished, and not under a live claude

(`kingdom/bin/km`, `seat_refresh_step`)

finished: safe AND finished. One more conjunct before moving —
km-seat's own wake comment ("no checkout happens under a live
claude") generalised to the sweep and the manual path. Reads the
world (herdr's report), not the status file: a dead-turn seat is
held either way, but a seat whose record says `awake` over a
crashed session is movable, which the record would wrongly
strand. At the wake path the session is never live (km-seat
refreshes before claude starts), so the guard cannot starve the
main clearing route.

## seat_refresh_step-3 — --check never moves, so a parked seat stays stale

(`kingdom/bin/km`, `seat_refresh_step`)

--check never moves, so a parked seat behind on gates is STILL
exposed when this returns, and the exit status has to say so. It says
so as 5 rather than 3, because the two exposures want different
people: 3 wants the citizen standing in that tree to rebase, 5 wants
the operator who just typed --check to type it again without. Both
were 3 until gqlc-jjcj, and the sweep's summary — the only reader
that buckets by status — attributed the merged bucket to the one
cause that fits 3, sending an operator to ask three citizens to
rebase trees with nothing in them.

Reachable only under --check, and km-seat (the sole caller that
branches on this status, km-seat:183) never passes it, so nothing
that reads 3 today can be handed a 5.

## cmd_seat_refresh_all — the sweep

(`kingdom/bin/km`, `cmd_seat_refresh_all`)

The sweep (gqlc-k9r2). MEASURED 2026-08-23 on the live town: 15 of 16 seat
worktrees were missing merged .githooks/.claude commits, 7 of them PARKED as
far as 53 commits back — all three architects, both seated judges and the
guard — and a 16th roster seat had no worktree at all. The per-seat command
was already correct and already refused every unsafe move; what was missing
was any way to apply it to the town without composing sixteen invocations by
hand at the moment of switching the town on.

It refuses exactly what the single-seat path refuses, seat by seat, and it
moves nothing else — Constitution VI.2's no-forcing applies to a citizen's
work in a tree as much as to their session. The failure this must never have
is the convenient one: a sweep that treats "held" as "skip the guard".

## unit_drift — why it detects instead of repairing, and why it scrubs KM_STATE_DIR

`km deploy` fast-forwards the checkout and used to stop there: it never called
`render_unit` and never touched `$HOME/.config/systemd/user`. So a merged change
under `kingdom/systemd/` reached the tree and reached no unit, while deploy
printed `deployed: <root> at <sha>` — a report true about the checkout and
silent about the thing the checkout was supposed to change (bd gqlc-2a4go, found
while landing gqlc-leq7, which edits kingdom-dispatch-alarm.service and needed a
manual `km install-units` nothing asked for).

**Why not re-render and reload.** The self-healing answer means nobody has to
remember, and it was declined for two reasons. A `systemctl --user
daemon-reload` issued from `cmd_deploy` runs inside the dispatch tick, so it is
the tick asking its own manager to reload. And rendering WITHOUT the reload is
worse than either: the files on disk would then match, so this very check reads
clean while systemd still holds the old definition — the detector would be
disarmed by the repair.

**The gate is the doctor row, not deploy's line.** deploy exits 0 either way and
its stdout is a journal nobody reads; `ensure_deployed` folds it into one line
of stderr. A row that only prints is not a gate — measured on this change: with
the row's `ok=1` removed, `km doctor` still printed the identical `FAIL:` text
and exited **0**. The word FAIL is not the gate; `ok=1` is.

**KM_STATE_DIR is scrubbed on purpose.** `@STATE@` appears in exactly one source
(kingdom-dispatch-alarm.service). `KM_STATE_DIR` is exported into every seat
session, while the units themselves run km WITHOUT it, so the render a real
install produces for the town is the DERIVED one. Honouring the ambient value
would call every unit drifted whenever a seat ran `km doctor` — a row that cries
wolf from half the town's sessions is one operators learn to run past, which is
the failure mode gqlc-d1jkv is open about elsewhere.

**What it leans on.** The row renders with the RUNNING km's `kingdom.toml`, so it
is a claim about the deployed tree only while that tree is certified against
origin/master. That is why it is asked immediately after the `kingdom_drift`
row, which reports exactly that.

**One list, not two.** `unit_names` is the selection rule (`*.service`,
`*.timer`) read by both the installer and this check. Two lists disagree
silently, and a unit named by only one is either installed and never checked or
checked and never installed. Replacing the installer's hardcoded five with the
glob was verified behaviour-identical against the tree at the time: the derived
set and the hardcoded set were the same five names.
