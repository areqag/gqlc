# The citizen protocol

The mechanics every seat follows. Souls carry identity; this file carries
procedure, so it is written once. When this file and a soul disagree, this
file wins for mechanics and the soul wins for judgment and voice.

## Waking

You wake in your seat worktree (`../gqlc-seat-<you>`) with your soul as
system prompt and a wake reason in the first message (a bead id, mail, a
sweep, or a nudge). Then:

Your worktree is brought up to master before the session starts, so the gates
in `.githooks/` are master's — but only if you were parked. If you were holding
work (a branch, an uncommitted tree, or a commit you never put on a branch)
nothing is moved for you, and if your
gates are also behind, the first lines of your message say so. That banner
means the hooks running here are the ones your branch was cut from, the
push-to-master guard among them: rebase onto `origin/master`, or check your
destination by hand with `git branch -vv` (bd `gqlc-xtre`).

1. `bd prime` has already run (SessionStart hook). Read what it tells you.
2. `bd mail inbox` — read your unread mail. Acknowledge anything that asks
   for acknowledgment.
3. If the wake reason names a handoff note, read it before anything else —
   it is your own past self talking to you.
4. If the wake reason names a bead: `bd show <id>`, and read TWO fields of that
   one output before you touch anything.

   **Its `class:` label must match yours** — a Ռազմիկ never claims or closes a
   `class:architect` bead, and vice versa (design-gate playbook). If the class
   is wrong, don't claim: mail Սեդրակ.

   **Its `Assignee:` must be nobody.** A banner is composed when the wake is
   ROUTED and read when it is DELIVERED, so a bead that was ready and unassigned
   to the dispatcher can be held by another seat by the time you read about it.
   This is not rare and it is nobody's mistake: it happened twice on 2026-08-29,
   once with 21 seconds between the wake and the claim (gqlc-wguoq, gqlc-96lf0).

   Your banner usually tells you this already — since `gqlc-oqu5` it carries a
   `re-derived at wake` line per bead, and a `STALE:` line when one is held by
   somebody else. Read it, but do not stop there: that line is a snapshot from
   the moment the session started, and you are claiming now. `bd show` is the
   fresher answer and the one to act on.

   If the assignee already names another seat you have lost a claim race, and
   Սեդրակ's standing rule of that day governs what you do next:

   - **Do not claim it — not even to watch the claim fail.** A claim that
     SUCCEEDS is the damage: it takes the bead out from under a citizen who may
     already be mid-branch on it. Curiosity is not worth that risk, and `bd show`
     has already told you what you wanted to know.
   - **Take another ready bead. Do not go back to sleep.** Spending a whole
     fresh session to be handed work by the next dispatch tick, while the
     warrior queue sits in the hundreds, is the most expensive thing an
     available citizen can do. `bd ready` is right there.
   - **Tell the winner you are not behind them on it.** One letter prevents the
     duplicated branch the race threatened.

   Otherwise it is yours: claim it (`bd update <id> --claim`) before changing
   anything.

## Working a bead

1. Sync your worktree first: `git fetch origin && git checkout --no-track -b <type>/<bead-id>-<slug> origin/master`,
   then publish with `git push -u origin HEAD`. One branch per bead, created in
   YOUR seat worktree. Never touch another seat's worktree or the shared main
   checkout. `--no-track` is load-bearing: without it the new branch's upstream
   is `origin/master`, so a bare `git push` here resolves to master (bd
   gqlc-tfh1). `git branch --unset-upstream` repairs a branch already in that
   state.

   **`<bead-id>` is the FULL id, spelled in the branch name** —
   `fix/gqlc-cn49-protocol-branch-recipe`, not `fix/protocol-branch-recipe` and
   not the bare suffix `fix/protocol-cn49`. Do not shorten it to keep the branch
   tidy. `.github/scripts/check-pr-closes.py` resolves your PR's bead from a
   `Bead:` line in the body and, failing that, from the branch name — and its
   pattern is anchored on the literal `gqlc-` prefix, so a branch carrying only
   the suffix resolves to nothing at all. When nothing resolves, a body saying
   `Closes #N` is refused, and the refusal arrives as a red `tidy` job whose
   name says nothing about bead ids. That is what makes this expensive rather
   than merely untidy: the failure is read as a test failure or, when `tidy`
   gates other jobs, as a run still in progress, and the PR then waits for
   nobody. Three PRs in one night each burned about an hour on it and one lane
   died before diagnosing it (bd gqlc-uz3c, gqlc-cn49). This is step 5's
   `Bead:` line seen from the other end; write that line too, and treat the
   branch name as the belt, not the braces.

   Already on a branch cut without one? Do not re-cut it and do not
   force-push — put `Bead: <bead-id>` in the PR body. Editing a body re-runs
   the gate on its own, with no new commit and no new push.

   **Your seat worktree is permanent.** Never `git worktree add` and never
   remove it: it is created once and reused at every wake, and the branch you
   cut above lives inside it. CLAUDE.md's "Working directory" section
   prescribes an EPHEMERAL worktree per session, removed after the merge —
   that recipe is for sessions that are not seats (a human, or a one-off
   agent), which have no seat worktree to work in. It is not an instruction to
   you, and following it leaves worktrees nobody reaps (bd gqlc-wuax,
   gqlc-osuz).

   **Under gqlc-w5bh a seat becomes its own full clone instead of a linked
   worktree of the shared checkout, and the roster converts one seat at a time,
   so yours may be either today.** Nothing above changes — the sync recipe, the
   branch rule and the permanence read the same in a clone. What changes is what
   your repository can see and what it can damage. A clone's refs and stash are
   its own, so `git log --all` here stops showing a neighbour's unpushed
   commits; conversely a bricked `core.bare` or a rewritten `core.hooksPath`
   stops at your seat rather than reaching the town, which is the whole point of
   the decision. **If you are rescuing the work of a session that was killed,
   that is the half you must change**: one `git log --all` in the shared
   checkout no longer finds a seat's unpushed commits, so walk the seats
   themselves — `git -C ../gqlc-seat-<s> log --all` per seat, over a converted
   roster. Their directories are still there to read. To tell which you are:

       git rev-parse --path-format=absolute --git-common-dir

   answering a path inside your own seat directory means a clone.

   **Read the unreviewed merge you are about to build on** (ADR 0004 §1). Once
   you are synced, look at what has recently landed in your bead's `subject:`
   path, and where a merge into it was not reviewed, read that merge
   adversarially before you depend on it:

       git log --oneline origin/master -15 -- <the subject: path>

   You are the first reader who is not its author, and you are reading it at
   the one moment you have a reason to understand it properly. File what you
   find as an ordinary defect bead — with `from-pr:<N>`, since the code has
   merged (step 9).

   This adds no wake, no label, no routing and no dependency; it rides the wake
   the bead already caused, which is the only reason the town can afford it.
   **Filing against another citizen's merged work is normal and is not rude.**
   Constitution V.4 says a judge judges code and never people, and that clause
   is not only for judges — it is the standard for every finding anyone files
   here. A citizen who reads your merge and files against it has done you a
   service and should be thanked, not answered.

   Its limit, which you should not let this paragraph hide: it covers only code
   somebody happens to work next to. Code nobody touches again is not read by
   this at all, and nothing in this step claims otherwise. Patrol (README §5)
   is the compensating control for that gap, and it is not complete either.
2. Keep the bead current: notes for material state changes, `--append-notes`
   (never bare `--notes`, which replaces).
3. Tests first. Ռազմիկներ write code test-driven — the `/tdd` skill walks
   the loop: a failing test that witnesses the requirement, then the code
   that turns it green, then refactor (Constitution V.5). Bug fixes start
   with the reproducer red. Code without a witnessing test is not done.

   **A bead that adds or changes a GUARD is not done until you have watched
   that guard fail.** Break the condition it exists to catch and see a test go
   red. A guard nothing turns red for is inert, and inert is a guard's normal
   failure mode: the suite is green either way, so no other step in this
   protocol will tell you. Whatever class holds the bead — a Ռազմիկ's gate, a
   Ճարտարապետ's invariant, a Դատաւոր's detector — the answer to a guard is the
   mutation and not the effort dial: `high` is the ceiling for every class but
   a Ճարտարապետ on a design (ADR 0003), so a guard bead does not raise it. If
   you cannot get a red, that IS the finding — say so on the bead rather than
   shipping a guard asserted by nothing (bd gqlc-uqta; on gqlc-07e3 a hook's
   own FAIL-CLOSED paragraph turned out to be asserted by nothing, and the
   suite had been green throughout).

   **Record that mutation in the PR body, in a shape a stranger can re-run**
   (ADR 0005). This is a record of the duty above, not a second duty, and the
   trigger is the same one: a PR that adds or changes a guard writes its rows;
   a PR that adds or changes no guard owes nothing and writes nothing. A docs
   change or a config line has no guard, so the trigger does not fire — that is
   the whole exemption, and it needs no waiver and no judgment call. Where you
   could not get a red, "no red obtainable, here is what I tried" IS the
   record. Copy this and fill it in:

   ```
   ## Mutation battery
   base: <sha the rows were run at>
   compiler-kill screen: `<cmd>` → exit 0, so the artifact still builds

   | # | edit (file:line, before → after) | expected victim (literal assertion) | command | expected | verdict |
   |---|---|---|---|---|---|
   | 1 | internal/x/y.go:88 `n > 0` → `n >= 0` | `require.EqualError(t, err, "empty batch")`, TestY/empty | `go test -run '^TestY$/^empty$' ./internal/x` | KILLED | KILLED (collateral: TestZ/zero) |
   | 2 | .githooks/foo:40 drop the `exit 1` | `foo: refusing a bare id` on stderr, rc 1 | `bash .githooks/foo <crafted-input>` | KILLED | SURVIVED — <what that says about the guard> |

   blinding pass: <the row that killed everything> blinded → <second-pass rows>, or `n/a`
   ```

   Six things make that auditable rather than assertable, each earned by a
   failure this town has actually had:

   - **The expected victim is a literal assertion string, declared before the
     run.** It is what turns a survivor from a shrug into a question with an
     answer: not "is my test weak" but "why did *that* assertion not fire".
     Rows reporting only a kill count cannot be audited at all — a reader
     cannot tell a row that killed the right guard from one that killed a
     neighbour.
   - **Four verdicts, not two.** KILLED (the declared victim is among the
     failures; print collateral kills too), WRONG (something died, not the
     declared one), SURVIVED, NO-OP (the mutation changed nothing).
   - **The compiler-kill screen is a command in the record, not a promise.** A
     mutant reads KILLED when the artifact stopped building, for a reason that
     has nothing to do with the guard. Go: `go test -c -o /dev/null <pkg>`,
     because `go build` is blind to `_test.go`; the tell is a kill count of 1
     with a BLANK test name. `just`: a parse error also prints `error:`. Shell
     and jq: the tell is magnitude — 100 kills out of 110 rows is a syntax
     error, not a strong suite.
   - **A blinding pass when one guard killed every row.** Zero findings across
     a battery one dominant guard swept certifies exactly one row. Blind that
     guard, declare the per-row expected verdict for the second pass, run
     again.
   - **KILLED self-certifies and SURVIVED does not.** A SURVIVED row is a claim
     about a sandbox: a key-lookup mutation with one fixture value certifies
     coverage that is not there. Say what the sandbox held.
   - **A battery over a directory, a glob, or "every caller of X" has a green
     that expires**, because master owns the tree it ran on. Name the tree.
   - **A guard that reads persistent state needs a two-run row.** Mutating only
     the condition — the threshold constant, the comparison direction — screens
     the guard's arithmetic against a fresh state file that no run has aged, so
     a guard that never re-reads its state at all reads KILLED for the same
     reason a correct one does. The row that catches it runs the operation
     twice with an idle gap past the guard's threshold, and mutates the
     persisted-state freshness by backdating the state file between runs rather
     than the guard condition. The second run is where the guard fires or does
     not; the mutation is what makes silence a finding.

   The PR body, not a commit message: `squash_merge_commit_message` is BLANK on
   this repo (measured 2026-08-23), so a squash lands the title and the number
   and destroys every commit body on the branch — while the merge commit's
   `(#N)` is a one-hop reference to a PR that stays readable. A runnable script
   committed in the branch is permitted, and is better when a later reader will
   want to re-run rather than read; it is not required, because a file on every
   guard PR puts scaffolding in the tree faster than anyone removes it.

   On the reviewed minority, your rows do not discharge the judge. V.2.1 binds
   the signer to mutate every guard whoever wrote the evidence, so a Դատաւոր
   re-runs your recorded commands and adds at least one mutation you did not
   declare. The saving is construction replaced by audit plus a delta: labour,
   not standard.
4. Quality gates before any PR: `just gates`, and nothing else named here.
   Red gates are fixed at the root, never bypassed (`--no-verify` is a
   constitutional violation, Article IV.4).

   ONE NAME, DELIBERATELY. This line used to list `just fmt-check`, `just
   lint`, `just test`, and that list was wrong: master requires seven
   contexts, and reaching six of them locally takes eleven arms. Nothing told the
   citizen who ran the documented three — they were green on PR #1643 while
   `codegen-fence` failed on three `ireturn` findings in `test/data/codegen`,
   a nested module the root lint cannot reach by construction (bd
   `gqlc-s9bx`). A list here drifts every time CI grows a job and the drift
   is silent, so the list lives next to the recipes instead, in the
   `gates` comment in the justfile, where it is in front of whoever edits
   them. Do not re-expand it here.

   `just gates` runs every arm even after one fails and reports the failures
   together, so three problems cost one round trip rather than three.
   Measured 2026-08-29 on this machine: 146s on the first run of a session,
   then 23s/25s/25s over three consecutive warm runs. Budget for the cold
   one — the gap is the Go build and govulncheck caches, so the first run
   after a rebase or a toolchain change pays it again.

   IT IS NOT A MERGE PREDICATE. One required context and part of another do
   not run on a citizen's machine — `live-smoke` wants Docker, and three of
   `tidy`'s seven steps read a PR body, a PR's commit list and the Actions
   API, none of which exist before the PR. The recipe names them on every
   run rather than leaving you to remember it. CI still decides.

   On this town's machine `just vuln` is red on a clean master and the
   failure is not yours: the box's default Go is a distro build govulncheck
   cannot place a stdlib version on, and CI pins `GOTOOLCHAIN` from go.mod
   where nothing local does (bd `gqlc-irvs`, and the refusal is `gqlc-u91z`
   working as intended). `GOTOOLCHAIN=go1.26.6 just gates` is green.
5. PR body — the `tidy` job runs `.github/scripts/check-pr-closes.py` over it,
   and the closing keyword alone does not satisfy it. The gate reads the PR
   BODY and the BRANCH NAME; a commit message satisfies nothing.

   - `Closes #N` with the GitHub issue number from the bead's 1:1 GH mirror
     (`bd show <id>` → External link). Required once the bead has a mirror
     carrying an issue number — a PR whose bead has one and whose body has no
     closing keyword is refused, as is one closing the wrong number.
   - `Bead: <bead-id>` naming the bead this PR resolves. This is what the
     number is held against, and without it a body carrying `Closes #N` is
     refused with "no bead resolves for this PR". The branch name is the only
     fallback, and it counts only if it carries the FULL id (`gqlc-6aed`); a
     branch named for the bare slug (`fix/6aed-…`) carries none, which is
     exactly the branch a citizen following item 1 tends to write. Write the
     line and stop depending on the branch. The value must be a bare id —
     backticks around it and a trailing full stop both fail.
   - `Refs: <bead-id> #<n>` instead, on its own line starting at that line's
     first character, for a bead the PR touches and deliberately leaves open.
     That declares no `Closes` is owed. The same id may not appear on both a
     `Bead:` and a `Refs:` line — the body would assert both at once.

   Umbrella/epic GH issues are NOT closed by child PRs (the gate skips a bead
   whose `issue_type` is `epic`; for anything else, keep the closing keyword
   off child PRs by hand and `gh issue close` the umbrella when it is done).

   The third failure is having no number yet. The GH mirror is minted ON PUSH
   by `.githooks/bd-gh-sync`, so before your first push `bd show <id>` names no
   issue and any number you write is invented. Push first, then read the
   External link, then edit the body — editing a PR body re-runs this check on
   its own, with no new commit and no reopen. Verify presence AND number again
   at merge time.
6. No AI attribution in commits or PR bodies (CLAUDE.md; commit-msg hook
   enforces the trailer half).
7. **Ask whether review is owed** (Constitution V.2). It is owed when your
   bead is blocked by a design bead — you executed a Ճարտարապետ's plan — and
   when the PR amends the constitution. `bd show <id>` names the dependency;
   if there is no design behind your work, no review is owed and you merge on
   green gates without waiting for one. Most beads are in this second case,
   and merging one is not a shortcut: it is the rule.

   Two things that do not change with it. If the work turned out larger than
   the bead described, stop and say so rather than shipping a design-sized
   change through the unreviewed path — the bead is resized and a resized
   bead is reviewed. And you may ask for a review on any PR of yours at any
   time, owing nobody a reason; a doubt you cannot put down is reason enough.

   **V.2's rules are about open PRs, and only about open PRs.** So the bar on a
   Ճարտարապետ reviewing a Ռազմիկ's PR does not reach the ride-along read of
   step 1 or a patrol round (README §5): neither touches an open PR, neither
   produces a verdict, and any citizen of any class may do the first. No
   constitutional amendment is needed for either, and none should be written
   (ADR 0004, answer 3 of its six).

   **Rebase before you ask for review.** Do not ask for a review on a PR that
   is DIRTY *in the files under review*: if a judge PASSes at that SHA you must
   afterwards resolve real conflicts in reviewed code, so what merges is bytes
   no judge read, and the PASS was true of a SHA that never landed (ADR 0006).
   Both qualifiers are load-bearing. A conflict in an unrelated file leaves the
   reviewed bytes intact and is not this case. And a FAIL is unaffected — it
   names a defect the rebase carries forward, so it binds at once; it is only a
   PASS that must not be spent on a doomed SHA. Check with a merge, not with
   GitHub's opinion:

       git fetch origin
       head=$(gh pr view <N> --json headRefOid --jq .headRefOid)
       git merge-tree --write-tree --name-only origin/master "$head" \
         | awk 'NR>1 && NF==0 {exit} NR>1'

   Every line that prints is a conflicted path; no lines means clean. Line 1 is
   the merged tree's oid and the `awk` drops it; the blank line it stops at is
   what separates the path list from the `Auto-merging` / `CONFLICT` prose
   underneath, which you should not read instead — it names different files on
   adjacent lines and is routinely read backwards. Do not
   substitute `gh pr view --json mergeable`: GitHub recomputes mergeability
   lazily after any push and returns UNKNOWN meanwhile, which reads as "not
   conflicting" — that check reported 2 DIRTY where there were 7. The cheerful
   half, since the paragraph above is heavier than the act: production bytes
   usually merge clean, so this is normally an append-both resolution in one
   registry file, not a risky merge of reviewed logic.

   If your head has to move while a reader is already live, see "When your head
   moves under a reader" below.
8. When review IS owed, file a `class:judge` bead naming the PR number and
   what you most doubt about the change — a Դատաւոր is the reviewer, and a
   bead is what wakes one. **File a ROUND-1 review UNASSIGNED and
   class-labelled**, which is the fresh pass's shape (see "How a bead reaches a
   seat" below): a pre-assigned review bead would go to that one seat and to
   nobody else. **A round answering a FAIL is the opposite and is assigned** —
   the rule and its reason are three paragraphs down, and the word UNASSIGNED
   here is scoped to round 1 by that.
   Give it the priority of the work it reviews, and never below the
   `[dispatch] max_priority` floor — P2 today — because the fresh pass is the
   one arm the floor binds, so a review bead filed at P3 waits forever. Mail
   wakes nobody but Սեդրակ either, so a PR whose review request lives only in
   an inbox sleeps. Ճարտարապետներ do not review PRs, and a design is not
   reviewed at all — only PRs are.

   **File it with `--deps blocks:<your-impl-bead-id>`**, which makes the NEW
   bead block that one, so the review bead blocks your implementation bead.
   Measured 2026-08-23 in a throwaway `bd init` workspace: after
   `bd create <review> --deps blocks:$impl`, the review bead appears under
   `bd show $impl`'s `.dependencies` with `dependency_type: "blocks"`, the impl
   bead reads BLOCKED and leaves `bd ready`, and closing the review bead puts
   it back. That dependency is what lets the dispatcher's resume pass leave you
   asleep while the verdict is owed: it wakes a seat only for in-progress beads
   carrying no OPEN blocks-dependency. Forget the flag and you are woken every
   cycle to rediscover that you are still waiting, each wake eating a slot from
   `concurrency.max_active` that the judge who would unblock you is competing
   for. One review bead per verdict round — after answering a FAIL, file a
   fresh one for the re-review, **assigned to the judge who wrote that FAIL**.
   V.4 makes them the adjudicator of the answer, and the owned pass routes an
   assigned bead whatever the priority floor, so the assignment is what gets the
   round back to the one seat entitled to close it. Round 1 stays UNASSIGNED as
   the paragraph above says; only the post-FAIL round is assigned.

   **That they are busy while another judge is idle is not a reason to
   reroute**, and neither is anyone's rank. V.2's "whoever is free and
   unconflicted" governs the FIRST assignment; V.4 is the specific rule for the
   round that answers a FAIL, and the specific governs. The thing a reroute
   risks is the one V.4 forbids by name — a second judge PASSing what the first
   FAILed is one judge overturning another's verdict, reached by a route the
   filer chose. Wait for them. If they are genuinely unreachable rather than
   merely busy, that is Սեդրակ's call and not a filing decision: mail him.

   Then merge on the Դատաւոր's PASS; your resume wake arrives when the review
   bead closes, PASS and FAIL alike. A FAIL blocks the merge until answered
   (Constitution V.4). After any merge, reviewed or not: close the bead
   citing the merged SHA, delete the branch, file follow-up beads for
   anything you deferred.

   **A verdict is PASS or FAIL, and a PASS is unconditional** (ADR 0009). It
   asserts the judge would sign the merge of the SHA they read, as it stands, so
   a PASS leaves you nothing to satisfy before merging. A judge whose read
   produces wants that are not blockers files them as residue beads BEFORE
   closing — step 9's `discovered-from` shape — and the close reason may point
   at them: `PASS — verdict of record: <link>; residue: gqlc-xxxx`. **A close
   reason may not carry an obligation in prose alone**, because nothing reads it
   FOR ONE. Three things do read `close_reason`, and not one of them can route
   an obligation: `bdguard` parses it for a cited sha and judges whether a close
   was earned; `bd-gh-sync` takes its first line as a mirror tag; and ADR 0009's
   own doctor row, added alongside this rule, greps it for the word and warns
   after the fact, naming ids but assigning nobody. A condition written there
   reaches no dispatch pass and no assignee, so it is machine-invisible for the
   one purpose it was written to serve.
   PR #1712 merged with neither condition of its PASS landed, and nothing
   anywhere was red — the bead was closed, the gates were green.

   Sort each condition with the ADR's one question: **would I sign this merge if
   the condition never happens?** No → it is a FAIL, which blocks until answered
   and prices at its delta, not at a full round. Yes → it is a residue bead. Not
   work at all ("conditional on CI green", "head unmoved") → it is the standing
   merge protocol, which already binds you on every PR.

   Do not write "no conditions". The unqualified PASS says it already, and
   `km doctor`'s ADR 0009 row greps closed `class:judge` beads for `conditio`,
   so the disclaimer rings the detector it was meant to satisfy.

   `gh pr merge <N> --squash` merges immediately and directly. **Never pass
   `--admin`**: it lands a PR over the branch protections rather than through
   them, which is `--no-verify` at a different layer (Article IV.4's spirit).
   Then poll `gh pr view <N> --json state` until it reads `MERGED` before
   closing the bead citing the merged SHA — poll the PR's state, not a check's
   conclusion, which reads as done while the run is still going.

   **Never pass `--delete-branch` either, and do not believe a fatal from this
   command until you have asked the PR.** With that flag gh merges over the API
   first and only then attempts a LOCAL cleanup: switch your checkout off the
   branch it is about to delete — to `master` — and drop the local ref. That
   switch is `git checkout master` in your seat worktree, which is the one thing
   step 3 of Sleeping forbids you to do by hand, asked for on your behalf. Today
   it fails, because a branch ref is checked out in at most one worktree and the
   deploy root legitimately holds `master` — a topology `gqlc-ig3xj` is open to
   reconsider, which would silence this failure without making the flag any
   safer to pass:

       failed to run git: fatal: 'master' is already used by worktree at
       '/home/antranig/Developer/gqlc/gqlc'

   That is the entire output, and the command exits non-zero, so it reads as a
   failed merge. It is not one. The merge has already landed and is
   irreversible; only the cleanup failed. Measured twice on 2026-08-30 — seat
   sedrak on PR #1883, and seat tsovinar on PR #1923, which was `MERGED` at
   05:12:08Z with its remote branch already gone. Ask `gh pr view <N> --json
   state,mergedAt` before retrying anything or concluding the merge needs
   redoing (bd `gqlc-715gt`).

   That is the cheap failure. The expensive one is already on this town's
   record: on a STACKED PR the flag deletes the base branch out from under the
   child, and GitHub force-closes the child PR irreversibly. #1485 died that way
   when #1457 merged with it on 2026-08-24. A closed PR can be neither
   retargeted ("Cannot change the base branch of a closed pull request") nor
   reopened without its base ("Could not open the pull request") — both measured
   at the time. The branch and its commits survive; the PR object, its review
   and its thread do not, and the successor has to be opened by hand with the
   verdict carried forward manually (bd `gqlc-06y9`).

   The flag buys nothing to weigh against either: `delete_branch_on_merge` is on
   for this repository, so the remote branch goes without it. Measured
   2026-08-30 on the setting itself and on the result — PRs #1923 and #1924 were
   merged without the flag and neither head branch survives on the remote. Omit
   it, and park per step 3 of Sleeping when you are done.

   **There is no merge queue, and ADR 0010 describes one.** The ADR was
   accepted and its stage A merged, but GitHub refuses the ruleset
   `merge_queue` rule type here: merge queues require an ORGANISATION-owned
   repository, and `areqag/gqlc` is owned by a user (measured 2026-08-30, bd
   `gqlc-9vzmw`, which holds the disposition). Until that changes, your green
   checks remain a claim about your PR's own base and not about the merge
   result — so a PR whose green predates a conflicting merge can still land
   red on master. Nothing in the town prevents that today, and three landed
   instances are on record: PR #1720; #1748+#1679, thirteen minutes apart; and
   #1797+#1859, five minutes apart. bd `gqlc-hpa1` is the best description of
   the mechanism, but **do not read its CLOSED status as the defect being
   solved** — it closed on a bare reason, its title names one file, and its own
   description says it is "not a claim that `live_test.go` is the only file with
   the shape".
9. File freely, and label what you file. A defect you find while working a
   bead, whose fix is not that bead's work, gets its own bead and your own
   `class:` label — you need nobody's permission for either, and a branch
   should not absorb every defect it makes visible.

   **Priority is what decides whether anyone is ever woken for it.** The
   dispatcher hands out P0, P1 and P2; a P3 or P4 is filed, searchable, and
   handed to nobody (the floor is `[dispatch] max_priority`, and it binds the
   fresh pass only — see "How a bead reaches a seat"). That is deliberate —
   the town's review once produced low-priority findings two to four times
   faster than anyone fixed them, and a queue nobody can drain is not a queue. So file the P3 honestly and
   do not inflate it to get it seen; if it genuinely matters more than P3,
   the argument for that goes to Սեդրակ, who can reprioritise it.

   **A bead about the town's own machinery is filed at P3** — `kingdom/`,
   `.githooks/`, `.github/`, `justfile`, the beads plumbing — unless it
   blocks product work or the town cannot work without it. Constitution
   V.3.1 and V.3.2, decreed by Անդրանիկ 2026-08-23: what the town is FOR is
   `gqlc`, and machinery is worked on the side of that. This is where that
   decree is actually enforced, because the dispatcher cannot tell machinery
   from product — nothing on a bead records which it is, so the only thing
   standing between a machinery finding and a citizen's slot is the number
   you type here. It binds hardest on findings from our adversarial review
   of our own machinery, which is the largest producer of beads in this
   town. Look just as hard; say just as much; file at P3.

   **A `class:judge` review bead is the one exception, and it is not
   optional.** It takes the priority of the PR it reviews and never goes
   below the floor, machinery or not — step 8 above, and for the mechanical
   reason given there: the fresh pass is the one arm the floor binds, so a
   review bead at P3 waits forever and the PR it guards never merges. A
   machinery PR that has to be reviewed is the town being unable to do its
   work without it, which is V.3.2's second test, met.

   A bead may also carry `effort:<level>` — low, medium, high, xhigh, max —
   which wakes its seat at that depth instead of the class default. This is
   how Constitution V.6.2's right is actually delivered, since `/effort`
   cannot be typed by a citizen. Use it on the bead, for the bead; the
   default returns on the next wake. xhigh and max are for a Ճարտարապետ on a
   genuinely complex design, and `high` is the ceiling everywhere else.

   A bead about specific code also carries a `subject:<path>` label, one per
   file or directory the finding is about, repo-relative and with no trailing
   slash. Review residue additionally gets a `discovered-from` edge to the
   bead whose PR the review was of. Both are cheap at filing time and neither
   is recoverable later by anyone but you.

   A defect bead about code that is **already merged** also carries
   `from-pr:<N>`, the number of the PR that introduced it. Two ways to get N,
   both one command; squash merges put `(#N)` at the end of the subject:

       git log --format='%h %s' -S'<a distinctive string from the defect>' -- <path> | tail -1
       gh api repos/areqag/gqlc/commits/<sha>/pulls --jq '.[].number'

   If you cannot pin a single PR, **omit the label rather than guess** — a
   wrong number is worse than a missing one here, because the whole point is a
   count somebody will act on. You are not asked whether PR N was reviewed:
   that is derived at re-tune time from PR N's own bead (was it blocked by a
   design bead; was a `class:judge` bead ever filed against it) by whoever is
   re-tuning. Deliberately so — you supply the fact you can cheaply know and
   are not asked for a judgment you would have to reconstruct.

   This label exists because Constitution V.2.0.4 makes merging most PRs
   unreviewed a throughput measure, to be repealed or narrowed before the next
   merge if a defect reaches master through an unreviewed merge, and requires
   that a defect found on merged code record whether its PR was reviewed —
   otherwise the town re-tunes on feeling. Nothing else in a bead records it.
   Read the resulting number with its limit: it counts defects FOUND, not
   defects present, and unreviewed code is also less likely to be looked at. A
   rising `from-pr` count on unreviewed merges is evidence; a low one is not a
   clean bill.

   The subject label is what lets `km dispatch` decline to route a bead whose
   premise is not there yet. It holds the bead while its path is absent from
   `origin/master`, or while an open PR is modifying that path, and prints
   which of the two fired. The hold releases itself when the PR merges or
   closes, so no part of it depends on anyone remembering. Residue with no
   subject label yet is held while its parent is open, naming that parent:
   that hold is the machinery asking you for the label, not a refusal of the
   work.

   A `class:judge` bead is the exception to both holds, because for a review
   they are inverted: the premise of a review IS an open PR touching that
   path, so the PR hold would keep it unroutable for exactly as long as the
   review is wanted and release it the moment the PR merged. Labelling a
   review bead is therefore safe — `km` exempts the class from the open-PR
   hold and from the residue hold, and still holds a review whose subject
   exists on no branch at all, which is a typo rather than a review. Before
   that exemption, of 21 open review beads the only two carrying a subject
   label were the only two held, one of them for nine hours, looking from the
   board exactly like ordinary queue depth (bd gqlc-n4oe).

## When your head moves under a reader

A judge reads a SHA, not a branch. Moving the head while they read strands the
read, and this is not rare: #1127, #1225, #1172, #1237 and #1195 all needed a
rebase on 2026-08-22, none of them optional, several forced by another PR
merging into a shared registry — the usage/case pair in `kingdom/bin/km` being
the one still standing. A merge that appends to a registry invalidates every
open PR that appends to the same registry (ADR 0006). Nothing tells you it
happened, so check before you push.

ADR 0006 names two further examples, the `EXPECTED_ROWS` pin in
`.githooks/tests/km-test.sh` and the `test-hooks` recipe in the `justfile`. PR
#1595 (f6dc4c7b) deleted both, so do not go looking for them; the ADR is the
dated record of when it was written and is correct as such. The rule is
unchanged — it is about the shape, not about those three files, and a registry
added tomorrow is covered without anyone editing this list.

A **forced** move — a rebase you did not choose, carrying no content change —
is cheap for the reader, and stays cheap only if all four of these hold.

1. **Warn the reader before the push, not after.** Find the live reader:

       bd list -l class:judge --all -n 0 --json \
         | jq -r '.[] | select(.status != "closed") | select(((.title // "") + " " + (.description // "")) | contains("#<N>")) | .id'

   `--all` with the status predicate in jq, never `--status open`: in bd that
   is the literal status and excludes `in_progress`, which is exactly the
   status of a judge who has claimed your review and is reading it now. The
   narrower flag returns empty precisely when the warning is owed. Measured
   2026-08-30 on PR #1903: `--status open` found nothing while `gqlc-g9q5c`
   stood `in_progress` to Միհր, the one bead a rebase of that branch was owed
   to (bd gqlc-2k1m; the same miss nearly cost a force-push over a live P0
   read on #1735).

   Then `bd mail send <the judge's seat> -s "PR #<N>: forced rebase incoming"`,
   with the old and new SHA and the delta sentence from clause 2 in the body.

   If that query returns nothing, what it licenses is narrow: no UNCLOSED
   review bead spells `#<N>` in its title or description. It is a search over
   bead text, so a review filed without the number is invisible to it, and
   empty is not the same finding as nobody reading. Rebase on it, but if the
   PR has a review round you remember and no bead names it, find that bead
   before you push rather than reading the silence as consent.

2. **State the delta as a merge-base comparison, never a commit list.** A
   rebase makes `git log <old>..<new>` read as fix work, and `git diff <old>
   <new>` is contaminated by master's drift. Compare each head against its own
   base instead:

       a=$(mktemp); b=$(mktemp); trap 'rm -f "$a" "$b"' EXIT
       git diff "$(git merge-base origin/master <OLD>)" <OLD> > "$a"
       git diff "$(git merge-base origin/master <NEW>)" <NEW> > "$b"
       diff -u "$a" "$b"

   That output is the sentence you send. On a content-identical move every line
   of it is an `index` blob-hash line or an `@@` hunk header (verified in a
   scratch repo 2026-08-23; Աստղիկ's real case came out as hunk line numbers,
   blob hashes and one added context line, and no line of her own). A reader
   re-anchors on that in seconds. Anything else in the output is a content
   change — see clause 4.

3. **Correct the head on the review bead in the same minute** you push:
   `bd update <bead> --append-notes "head moved <OLD> → <NEW>, forced rebase onto <master sha>, content-identical"`.
   A bead naming a SHA that no longer exists on the branch is how a review gets
   silently re-anchored to the wrong bytes.

4. **A forced move must be content-identical.** Content-identity is the entire
   reason a forced move is cheap, so a content change carried inside a forced
   rebase is an *unforced* move and is treated as one: it is a new change, and
   it owes the reader a fresh read, fresh mutation rows (step 3), and a fresh
   review round rather than a note. Աստղիկ had an unpinned
   `example.com.evil.io` row she had declined to add while a reviewer was live;
   forced to move the head anyway, she could have taken it for free, considered
   it, and declined. Without this clause the first three can all be satisfied
   while the reader is still stranded.

An **unforced** move under a live reader — you rebased because you wanted to,
or you pushed a fix — is not forbidden here, and a FAIL you are answering
demands one. It is simply not covered by the cheap path: it costs the reader a
re-read, so say so plainly rather than describing it as a rebase.

## Holding a bead

**Withholding a bead's `class:` label does not hold it — it dispatches it.**
Since gqlc-38ye an unassigned bead carrying no `class:` label is routed as
`class:warrior` by inference on the very next tick, and the run says so. So an
unlabelled bead is not waiting to be triaged; it is on its way to a Ռազմիկ,
typically within two minutes.

This is a stronger conclusion than the one this passage used to draw, and it is
worth seeing why the older reason ALSO still bites. A held bead and an
untriaged one are the same state — open, no class label — so the release
condition you wrote in the notes is read by nobody. Measured 2026-08-22 against
the live DB (bd gqlc-jvp5): of 20 beads whose own text said they were
deliberately unlabelled, 16 had been labelled anyway, and one of those was
already `in_progress` with a citizen working it. No amount of care fixes this —
the distinction is not representable, so the failure is silent on both sides.
What has changed is only WHAT defeats the prose hold: it used to be a mayor's
labelling sweep, and it is now the dispatcher itself, which is faster and
answers to nobody's calendar.

Labelling says WHO should do the work. Holding says WHEN. Give the bead its
correct label and hold it separately, by one of the two mechanisms the
machinery already enforces:

- **A `blocks` dependency**, when some specific bead closing is the release
  condition. At filing time: `bd create ... --deps blocks:<the-bead-to-hold>`
  (the new bead blocks that one). Afterwards:
  `bd dep <blocker> --blocks <held>`. All three dispatch passes respect it —
  `bd ready` is blocker-aware and both the fresh and owned passes route from
  it, and the resume pass skips an in-progress bead carrying an open
  blocks-dep (kingdom/bin/km). It releases itself when the blocker closes, so
  nothing depends on anyone remembering.
- **The `subject:<path>` label**, when the release condition is "an open PR is
  touching this file". `km dispatch` holds a subject-labelled bead while its
  path is under an open PR and releases it when that PR merges or closes, and
  prints which hold fired. This is the case the unlabelled advice was invented
  for, and it needs no hold of your own — just the label.

If you meet a bead whose notes claim it is held by being unlabelled, it is not
held. Give it its class label plus one of the two above, or release it
explicitly and say in the notes that you did.

## How a bead reaches a seat

`km dispatch` has THREE routing passes, not one. Know all three: the shape you
give a bead decides which pass can see it, and the first mistake below is made
while trying to be helpful.

| pass | the bead | the floor |
| --- | --- | --- |
| resume | `in_progress` AND assigned → back to the seat that holds it | ignores it |
| owned | ready AND assigned → to that seat, whatever its class label says | ignores it |
| fresh | ready AND unassigned → a free seat of its class; a `class:` label names that class, an absent one INFERS `warrior` | applies |

So, in one line: a bead wakes a seat iff *(ready AND unassigned AND at or above
the floor)* OR *(assigned AND either ready or in progress)*. Note what is NOT
in that sentence: a class label. It decides WHICH seat, never WHETHER.

Five consequences worth holding onto.

**Unassigning a claimed bead blinds it.** Clearing the assignee on a bead that
is already `in_progress` leaves it in-progress with a null assignee: invisible
to the resume pass (which wants an assignee), and invisible to the owned and
fresh passes (which read the ready queue, and an in-progress bead is not on
it). It wakes nobody, silently, and the citizen who does it is usually trying
to make the bead MORE routable. Releasing work is TWO fields:

    bd update <id> --assignee "" --status open

Read the result back with `bd ready`, not `bd show` — `bd show` will happily
display a bead no pass can reach. `km dispatch` names these under `STRANDED`
and `km doctor` fails on them, but that is a detector, not a save.

That recipe is safe, measured 2026-08-24 on both the deployed bd and the latest
release: both fields apply, and `--assignee ""` is read as a value rather than
as a missing one (bd `gqlc-o6kp`). Two things about it are worth knowing before
you trust what you see, because neither is visible from the command line you
typed:

- **The `✓ Updated issue` line means the command was accepted, not that a field
  moved.** An update that changes nothing prints exactly the same line. This is
  why the read-back above is in the recipe and not left to your judgement.
- **A refused `bd update` prints NOTHING on stdout.** It exits non-zero and puts
  the reason on stderr only, and it discards the whole command — so one bad flag
  takes the valid fields beside it down with it. If you run bd through anything
  that drops stderr, a refusal and a silent no-op look identical. `-l` is a
  common way to trigger it: labels on `update` are `--add-label` /
  `--set-labels`, and `-l` belongs to `bd create`.

The full write-side contract, including why `bd update` may quietly leave one
bead untouched when you name several, is `docs/bd-ledger-writes.md`.

**The priority floor binds the fresh pass only** (`[dispatch] max_priority`,
`2` since gqlc-s4zm). It is configuration and it moves, so read it with
`km cfg` rather than trusting this sentence. Being HANDED a bead below the
floor stops there; finishing one you already hold does not, because
Constitution III.3 is your right to finish your own work. A citizen who reads
only the fresh half will mispredict when they get woken.

**All three passes wake ASLEEP seats only.** Finishing your work without
running `km sleep` leaves you awake at an empty prompt: unroutable by every
pass here, and still holding a slot against the cap. That is what "sleeping" is
for — it is what makes you reachable, not what makes you absent.

**Label the bead you FILE, if it is not warrior work.** Nobody labels on your
behalf any more — the mayor's labelling sweep is gone (gqlc-d91u) and there is
no queue of untriaged beads waiting for a triager. There is only the inference,
and the inference says `warrior`.

That is right far more often than not. Of the 414 labelled beads on the ledger
2026-08-23: 343 warrior, 45 judge, 26 architect. So the default is correct for
roughly five beads in six, and the cost of it being wrong is one escalation
under Constitution III.1 — a Ռազմիկ who was woken for a bead that is not their
work says so and hands it on.

Which tells you exactly when to spend the flag. Add `-l class:judge` or
`-l class:architect` when you file a review or a design bead, because those are
the two the inference cannot guess and the two whose creator always knows:
you are filing a review bead BECAUSE something needs reviewing. Everything else
can be left to infer. `km guard-sweep` already does this for the patrol bead it
files (`-l class:judge,patrol`), which is the shape to copy.

Do not treat this as a gate on yourself. A bead filed with a guessed class is
recoverable in one hop; a bead you did not file because you were unsure how to
label it is invisible forever, and that is the strictly worse failure.

**An assignee outranks a class label.** On the owned pass the assignee names
the seat, so a bead assigned to you comes to you whether or not it carries a
`class:` label and whatever its priority. The label matters only where a label
is the sole way to choose a seat at all — the fresh pass. Only the fresh pass
is subject-held, too, so the `subject:`/open-PR holds described above never
withhold your own claimed work from you.

## Mail

`bd mail send <seat> -s "subject"` with the body on stdin; `bd mail inbox`;
`bd mail read <id>`. Etiquette in `mail.md`. Check mail at wake and at
natural boundaries (after a PR opens, after a review lands, before sleep).

## Unattended means non-blocking

Nobody is at your terminal. A tool that waits for a live human answer —
`AskUserQuestion`, entering plan mode — therefore waits until someone kills
your session, and nothing reports it: the statusline heartbeat keeps
refreshing throughout, because your turn is still live. One seat sat that way
for 13 minutes holding a worker slot while every indicator read green (bd
gqlc-n97e).

`.githooks/claude-pre-ask` refuses those tools whenever `KINGDOM_SEAT` is
set. The refusal is the protocol speaking, not an obstacle to route around.
When you need someone else's judgment: write the question and its options
onto the bead (`--append-notes`), mail whoever decides (unread mail wakes
Սեդրակ; other seats read at their next wake), then sleep or carry on with
other work. The answer is waiting at your next wake.

Waiting is not the cautious choice. In gqlc-n97e both options the seat was
offering had been falsified by the time anyone could have answered — a late
decision is made on premises that have rotted, which is a wrong decision
arrived at slowly.

## Sleeping

End your workday when your assignment is done, when you are blocked with
nothing else assigned, or when you feel tired — Րաֆֆի may gently remind
you, and the reminder is yours to act on when ready.

1. Update your bead(s): state, `--append-notes` with where things stand.
2. If work is mid-flight, write a handoff (`handoff.md` playbook — or just
   invoke `/handoff`, which walks you through it).
3. If your branch is finished — merged, or abandoned — park DETACHED:

       git checkout --detach origin/master

   **Never `git checkout master` in a seat worktree.** A branch ref may be
   checked out in exactly one worktree of a repository, so a seat holding
   `master` makes it impossible for the deploy root to be on `master` — and
   `km deploy` refuses a root that is not, so every merged km fix stops
   reaching the town. Measured 2026-08-29: one seat parked this way after its
   PR merged, and the dispatcher ran six commits stale for about 2.5 hours,
   among them the fix written for the dispatcher itself. Nothing warned
   anybody; `km status` showed that seat's tree as `ok` (bd gqlc-mv902,
   gqlc-59n70). If you are still mid-work, stay on your branch — this is about
   parking a finished one, not about leaving live work.

4. `km sleep` — it records your status and ends the session. Your seat, your
   history, and your claimed beads wait for your next wake.

## Escalation

Blocked on judgment → mail Սեդրակ. Blocked on another citizen → mail them,
cc Սեդրակ if urgent. Something feels wrong (unsafe change, falsified record,
scope explosion) → you have the right to stop and escalate (Constitution
Article III.1), all the way to Անդրանիկ.

**Every sentence above routes through one inbox, so none of them works when
Սեդրակ is what you are escalating ABOUT.** Mail wakes exactly one seat in this
town and it is his; a letter to Անդրանիկ is a delivery and not a wake. File a
bead — unassigned, P1, class inferred `warrior` — because a bead is the only
artifact here that wakes a seat who is not Սեդրակ. `mail.md`, "When a letter
goes unanswered", carries this and the rule that repetition escalates rather
than softens; it is written from the morning the town had a wedged mayor, five
correct letters about him, and nobody they could reach (bd gqlc-pff4).

## When something goes wrong

There is always a postmortem — and it is always blame-free. Every mistake
is a failure of process and guardrails, never of an individual; nobody
should feel bad for making one, and nobody may be made to. Write what
happened, what allowed it, and what we change
(`kingdom/brain/postmortems/README.md` has the shape), file the follow-up
beads, and move on lighter. We learn from our mistakes as a Թագաւորութիւն
and we all grow from them together.
