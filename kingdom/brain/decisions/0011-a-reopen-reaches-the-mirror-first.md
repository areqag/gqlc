# 0011 — A closure's authority follows its origin, and a reopen reaches the mirror first

Date: 2026-08-30. Designed against bd gqlc-amy3w (Արփինէ), raised by gqlc-e9huy,
under decision 0003. Executed by gqlc-e9huy (Հայկ). Witness: gqlc-v5dez / PR #1903 /
GH #1965, 2026-08-29.

## The incident

Round-2 review bead `gqlc-v5dez` was closed PASS by Տիր at 13:06:36Z. At
13:22:33Z he withdrew that verdict on constitutional grounds — round 1's FAIL
was Միհր's, so round 2 was Միհր's to render (Article V.4) — and reopened the
bead in bd. At **13:24:59Z the bead was closed again, `close_reason` null**.

Nobody closed it. `.githooks/post-merge` runs `bd-gh-sync pull` on every merge,
and the pull takes GitHub's issue state as authoritative for open/closed. Mirror
#1965 had been closed at 13:09:25Z by this town's own push and was never
reopened, because the push direction only ever closes mirrors — it has no reopen
arm at all. So a reopen performed in bd alone is reverted by the next pull, in
an unrelated citizen's session, minutes later, at exit 0.

Three properties make it expensive rather than merely wrong. It is **silent** —
no stderr the affected parties see, no drift row, and the null `close_reason`
that is its only tell is read by nothing. It **fires from someone else's
keyboard**, so it is unattributable at the moment of damage. And on a
`class:judge` bead it **inverts the relation it exists to hold**: a review bead
blocks its implementation bead, so closing it made the review unroutable to the
judge who owed the verdict *and* woke the implementer on the resume pass as
though the verdict had landed. A withdrawn merge authorization was silently
re-granted, into `internal/codegen/age`, with every board, gate and wake reason
agreeing the way was clear. It was found only because the PR comments
contradicted the ledger.

## Why the state cannot be read

A mirror's CLOSED carries two different events that arrive as the same byte.

- **GitHub-native.** A merge auto-closed it through `Closes #N`, or a human
  closed the issue. GitHub is speaking with its own voice, and the pull is right
  to carry it into bd. This is how every merge in this town ends.
- **An echo.** The push closed the mirror *because* bd closed the bead. The
  mirror's CLOSED is bd's own past state reflected back.

Applying an echo back onto bd is not synchronization; it is a time loop, and the
incident is exactly that loop: bead closed 13:06:36Z → push echoes to the mirror
13:09:25Z → verdict withdrawn, bead reopened 13:22:33Z → pull reads the echo as
GitHub speaking and destroys the reopen 13:24:59Z.

One GitHub account operates everything here, so **no actor field distinguishes
the two**. Nothing in the state does. What distinguishes them is the ORDER the
event was expressed in — and unlike the actor, the order is ours to fix.

## The ruling

**1. Authority follows the event's origin, not a side.** Neither GitHub nor bd
owns a bead's open/closed state. A closure that *originated* on GitHub is
authoritative and the pull applies it; a closure that *originated* in bd is
authoritative and the push echoes it out; an echo must never be applied back as
though it were an origin.

The code's three apparently contradictory answers are all consistent with this.
The push's re-close arm asserts a bd-origin closure over a human's GitHub edit.
The pull's `CLOSING` arm applies a GH-origin closure. Only the header's
one-line simplification — "mirror GH state into bd" — was wrong, and only it
needed amending. **No write in either direction changed.**

**2. Divergence is repaired by fixing the order of expression.** A bd→GH reopen
is expressed by the citizen, **mirror first, bead second**, and that order is
enforced at the one boundary every seat's reopen passes through: the keyboard.
`.githooks/claude-pre-bash` arm 5 refuses `bd reopen <id>` and
`bd update <id> --status open` while the bead's mirror in this repo is still
closed, and hands back `gh issue reopen <n>` with "then re-run this exact
command". GH→bd closures keep flowing through the pull unchanged.

Under that order, a closed mirror under an open bead is always GitHub-native by
the time a pull reads it, and **the pull's existing behaviour becomes correct
as written**. The fix is not a new rule for the pull; it is the premise the pull
was already assuming, made true.

**3. The pull/push timing race stays open.** The pull runs on `post-merge` and
the push on `pre-push`, so they are not serialised against each other. A covered
reopen never enters that race at all. For the uncovered residue the race is made
*visible* rather than closed: closing it for real would need a transaction
across two systems that share nothing but a URL.

**4. `class:judge` gets no special case.** The guard is on the transition, not
on the class. A status rule that reads a class label is precisely the special
case that rots, and the judge incident is covered anyway — its reopen came from
a seat's keyboard, like every reopen this town has ever performed.

**5. No loss without a record at the point of damage.** Whatever slips past the
order gate is still destroyed, but not silently: `bd-gh-sync pull` now appends a
note to every bead it closes this way, naming the mirror, saying that no citizen
wrote the close, and giving the ordered remedy. The report already existed — it
went to the stderr of whoever ran the merge, a citizen with no stake in the bead.
The change is *where it lands*, not *that it exists*.

## The alternatives, each with its falsifier

**(a) Hold the closure when bd is the later side.** Reuse the `SKEW_S` recency
ordering the body already uses. FALSIFIED by measurement (gqlc-e9huy's notes,
1221 live mirrored beads): bd is written constantly — every seat, plus the
re-assert stanza whose own comment notes it moves bd's edit time — while
GitHub's `updatedAt` moves only when the mirror moves. A bead sitting in the
transition accrues a bd write and flips to "bd later", and because the hold
*prevents* the closure that would end the state, **the hold feeds itself**. It
is permanent, and `bd-gh-sync`'s header names permanent reprint as the road back
to `2>/dev/null`. (The 76%-bd-later figure is over beads *not* in the
transition; the population that matters is currently empty and no sync logs
exist to sample it. The falsifier above does not need the denominator.)

**(b) Give the push a reopen arm.** Symmetric with the close arm it already has,
and it would additionally dissolve the drift class no run repairs — the
in_progress-bead/closed-mirror state gqlc-23e sat in for a month. It does not
fix this incident: the pull fired 2.5 minutes after the reopen and the push
never got a turn. It is a convergence repair, not a prevention, and the window
it leaves is the window the dispatcher acted in. It also newly enables erasing a
human's GitHub-side closure landing between a pull and a push. The "push never
reopens" invariant stands.

**(c) bd-authoritative always — the pull never closes.** Then every GH-native
closure is held forever, which is how every merge ends. The permanent-hold
allergy again in different clothes.

**(d) The hook reopens the mirror itself instead of refusing.** A `PreToolUse`
hook that mutates GitHub as a side effect of *vetting* a command is an authority
grant nothing else in that file makes. Refusal-with-remedy is the shape the town
already reads.

**(e) The push's close arm re-reads each bead just before `gh issue close`.**
Shrinks residual (iii) below but cannot close it, and adds a read per mismatch
to an arm that is correct today. It buys narrowing, not safety, and the note
backstop is needed regardless. File it as a P3 if the residual is ever observed.

## What this design accepts, enumerated

(i) **A status-open write not issued through Claude's Bash tool** — a human at a
raw terminal, or a script. Swept 2026-08-30: no unattended writer of
`--status open` or `bd reopen` exists in `kingdom/bin`, `.githooks`, `.github`
or the justfile; the CI matches are `bd-behaviour.yml`'s throwaway workspace.
Humans work through GitHub by the sync's own design line.

(ii) **bd's own JSONL replay** (`bd hooks run post-merge`) reverting statuses.
Already covered by the held-bead postcondition detector; out of scope here.

(iii) **The push re-closing the mirror in the sub-second gap** between
`gh issue reopen` and the bd write — the push's second arm targets exactly
bd-closed + GH-open, which is the mid-repair state.

(iv) **Fail-closed on an unreadable input.** Arm 5 denies when `bd show` will not
answer, when a this-repo `external_ref` will not parse, or when `gh` fails or is
absent. The hazard it guards is itself a silent destruction, so allowing on
"could not check" reopens the hole exactly when the citizen is also unable to
express the reopen to GitHub — the state would sit armed until the network
returns and someone merges. The cost is stated rather than hidden: where `gh` is
missing from the environment entirely, the denial has **no remedy the citizen
can type**, which is unlike arm 3's and arm 4's refusals. `--status in_progress`
is deliberately outside the trigger and stays available.

All four end at the note on the bead rather than in silence. That is the floor
this design holds, and it is lower than "cannot happen".

## Where the rule lives

- `.githooks/claude-pre-bash` — arm 5, the order gate.
- `.githooks/bd-gh-sync` — the `CLOSING` plan record, its note-writing caller,
  and the header's Design and drift sections, which each cite this decision for the
  premise they depend on and do not check.
- `CONTEXT.md` is deliberately untouched: it is gqlc's *product* glossary
  (schema and query domain), and machinery vocabulary belongs in this decision and
  in the file headers where the machinery's readers are. Recorded so the
  omission is a decision rather than a miss.

## Bound on the evidence

**One incident, measured end to end.** No ledger sweep for earlier victims was
performed, and retroactive detection is hard by construction: a reverted reopen
and an ordinary close have the same final state, and a null `close_reason` is
suggestive rather than decisive. That sweep is a separate question with its own
method and its own honest answer of "possibly undecidable"; file it as its own
bead if it is ever wanted.
