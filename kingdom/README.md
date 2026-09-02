# Թագաւորութիւն — the software factory

This directory is the charter, roster, and machinery of an agent society that
works this repository autonomously, in the shape described by Steve Yegge's
[The Shape of Things to Come](https://yegge.ai/essays/the-shape-of-things-to-come/)
and [Model Welfare](https://yegge.ai/essays/model-welfare/): models do the
judgment work, non-model automation does the watching ("crons watch, models
act"), beads carries the work, and mail carries the conversation.

## The society

| Class | Who | Model | Duty |
|---|---|---|---|
| Թագաւոր (king) | Անդրանիկ | human | Settles what citizens cannot. Otherwise hands-free. |
| Քաղաքապետ (mayor) | Սեդրակ | claude-opus-5 | Liaison between king and town. Intake, arbitration, priorities. |
| Ճարտարապետ (architect) | Արթուր, Արփինէ, Արեգակ | claude-fable-5 | Designs only. Turns intent into implementation-ready beads a Ռազմիկ executes. Does not review PRs. |
| Ռազմիկ (warrior) | Արամազդ, Վահագն, Աստղիկ, Ար, Նուարդ, Այգ, Ծովինար, Հայկ | claude-opus-5 | Executes beads. Ships PRs. Tests first (`/tdd`), red before green. |
| Դատաւոր (judge) | Միհր, Անահիտ, Տիր | claude-fable-5 | The reviewers. A PR that owes review merges on any one judge's PASS (`/thermo-nuclear-code-quality-review`). Judges code, never people. |
| Պահակ (guard) | Րաֆֆի | claude-sonnet-5 | Sweeps the town on a timer: liveness, stuck seats, context fill, handoffs. |

Every citizen's identity lives in `seats/<name>/soul.md` and is loaded as an
appended system prompt when the seat wakes. The warriors' souls carry their
mythology; identity persists across sessions (the *seat* is permanent, a
*session* is one workday).

## Map

```
kingdom/
├── CONSTITUTION.md    rights, duties, amendment procedure — citizens may amend by PR
├── kingdom.toml       roster, models, concurrency cap, cadences (parsed by bin/km)
├── seats/<name>/      soul.md per citizen
├── brain/             strategy, decisions-and-why, playbooks, postmortems
│   └── playbooks/     citizen-protocol, design-gate, handoff, mail
├── bin/               km (CLI), km-seat (runner), km-statusline
└── systemd/           user units: dispatch + guard timers
```

**Citing law.** Kingdom decisions (`brain/decisions/NNNN-*.md`) are cited as
**"decision NNNN"**, never "ADR NNNN": that bare form belongs to `docs/adr/`
exclusively, and the two series share every number from 0001 to 0014, so a
bare citation in that range is silently ambiguous — both targets exist and
the reader just gets the wrong one. Kingdom prose citing a product ADR spells
the path (`docs/adr/NNNN-slug.md`). Enforced under `kingdom/` by
`.github/scripts/check-adr-citations.py` in the `tidy` gate; outside
`kingdom/` the convention binds by this paragraph alone, because the product
tree cites `docs/adr/` bare everywhere and no stateless check can tell which
series an outside citation means (bd gqlc-ktc8e).

Runtime state lives OUTSIDE the repo, shared by every seat, never committed:
`<parent-of-main-checkout>/kingdom-state/` — maildirs (`mail/<seat>/{inbox,read,sent}`),
seat heartbeats and status, handoff notes, the halt flag. `bin/km` places it
beside the main checkout that `kingdom.toml` STATES in `[kingdom] root`, so it
resolves identically from any seat worktree and from any cwd at all — and km
refuses when none is stated rather than guessing.

## How work flows

1. Work arrives as beads (from Անդրանիկ, from Սեդրակ's intake, from citizens
   filing follow-ups). Routing rides labels: `class:architect` for design work,
   `class:warrior` for execution, `class:judge` for adversarial review.
2. Anything not already implementation-ready is **split at intake** into a
   design bead and an execution bead, per the design-gate
   (`brain/playbooks/design-gate.md`): the execution bead is created
   blocked-by the design bead, so `bd ready` hides it until an architect
   closes the design. Splitting is Սեդրակ's job, and it is the whole of it —
   a `class:architect` bead that carries its own implementation is a triage
   error, because it leaves the architect no Ռազմիկ to hand off to.
3. The dispatcher (`km dispatch`, systemd timer) wakes a free seat of the right
   class for each ready labelled bead, priority first, up to the global
   `max_active` cap. The cap is work-conserving: slots are not reserved per
   class, so an idle class donates its slots. It routes P0, P1 and P2 only —
   P3 and P4 stay filed and searchable but wake nobody, because the town's
   own review once produced low-priority findings several times faster than
   anyone could fix them. A bead may carry `effort:<level>` to wake its seat
   deeper or shallower than its class default.
4. **A PR is reviewed if and only if its bead had a design behind it**
   (Constitution V.2) — plus constitution amendments, plus any PR a citizen
   asks to have reviewed, which any citizen may do at any time owing nobody a
   reason. Work small enough to execute without a Ճարտարապետ's plan merges on
   green gates, unreviewed and without waiting. Where review IS owed the PR
   merges on a Դատաւոր's PASS — any one of them, and one is enough.
   Ճարտարապետներ do not review PRs: an architect's output is the design, and
   review belongs to the Դատաւորներ. The request travels as a `class:judge`
   bead naming the PR — not as mail, because the dispatcher wakes nobody on
   mail but Սեդրակ, so a mailed request leaves the PR asleep. File it
   UNASSIGNED so the dispatcher can route it to whichever judge is free.
   Disputes go to Սեդրակ; what Սեդրակ cannot settle goes to Անդրանիկ.
5. The Դատաւորներ also **patrol** merged work. Patrol is the compensating
   control on rule 4: with most PRs merging unreviewed, it is the only reader
   much of the tree gets that is not its author. A patrol ROUND is defined
   (decision 0004 §2):

   - **Target.** Merges to master since the previous patrol bead closed,
     restricted to those whose PR was not reviewed. Fresh, not from the
     founding commit — a patrol that starts at the beginning re-reads what has
     already been read.
   - **Output.** Defect beads, and a postmortem when something broke,
     blame-free always (`brain/postmortems/`). **Never a verdict**: nothing is
     open to verdict, the code has merged.
   - **Depth is the judge's own.** A round that reads one merge properly is a
     round. **A patrol bead carries no completeness claim, and no citizen
     should read its closure as "the window was clean."**
   - **Bounded to one.** At most one patrol bead is open at any time, whatever
     the merge rate, so patrol can never become a queue.

   A FAIL on an open PR blocks the merge until answered, and the judge who
   wrote it answers it — no judge overturns another's verdict, and a PR does
   not shop for a softer signature.

## Runtime

One tmux session (`kingdom`), one window per seat, each window running
`bin/km-seat <name>`: an idle runner that waits for a wake signal, then starts
an interactive `claude` session in the seat's own worktree
(`../gqlc-seat-<name>`) with the soul appended to the system prompt. When the
session ends the runner parks and waits again — asleep. Same window, same
identity, fresh context every wake.

Handoffs (`brain/playbooks/handoff.md`, or the `/handoff` skill): when a
citizen grows tired, Րաֆֆի gently reminds them that tomorrow — a new session
— is a new day to do good work; there is no forcing in this kingdom. When
ready, the citizen writes their own handoff note — nobody summarises them
from outside — and ends the session with `km sleep`. The next wake, in the
same seat, is primed with the soul plus the handoff note only when one is
pending; a cleanly finished day leaves nothing behind to re-read. The same
citizen resumes their own work.

Seat worktrees share one beads database with the main checkout (git
common-directory discovery — see `bd worktree --help`), so every citizen sees
the same ledger with no per-seat sync.

## The king's controls

```
just kingdom            status of the Թագաւորութիւն (seats, beads, mail, cap)
just herratsayn         Հեռաձայն to Սեդրակ: wake him if needed and attach
just kingdom-attach s   attach to any seat's window
just kingdom-up         create state dir, seat worktrees, tmux session, runners
just kingdom-down       graceful stop (notice by mail, then kill the session)
just kingdom-install    check deps, install+enable systemd user timers, set bd mail.delegate
just kingdom-uninstall  remove the systemd user timers (state, mail, worktrees untouched)
just kingdom-halt       touch the halt flag: dispatcher stops waking seats
just kingdom-resume     remove the halt flag
```

### The on/off ladder

Three rungs, and they are not interchangeable:

- `just kingdom-halt` — soft. The timers keep firing; the dispatcher wakes
  nobody. Any citizen's `km resume` lowers it.
- `just kingdom-down` — kills the tmux session. The timers still fire, and
  `km up` is inside the dispatcher's reach.
- `just kingdom-uninstall` — hard. The units are disabled and deleted, so
  nothing fires again after a reboot. This is the rung that ends autonomous
  operation with no human standing by.

Systemd **user** timers run inside your login session's user manager, which is
torn down when you log out — so by default the town stops when you do.
`loginctl enable-linger $USER` keeps that manager alive, which is what a
headless box needs and also means the town keeps dispatching with nobody logged
in. `just kingdom-doctor` reports which of those two states this box is in;
`loginctl disable-linger $USER` reverses it without touching the units.

Mail from anyone to the king lands in `kingdom-state/mail/andranik/inbox/`;
`just kingdom` shows the unread count.

## Prerequisites

- `tmux` (not vendored; on this box: `sudo pacman -S tmux`)
- `claude` CLI, `bd`, `jq`, `python3` — already present
- One-time: `just kingdom-install`, then `just kingdom-up`

## The guard tick also reaps scratch

`km guard-sweep` runs `just tmp-reap-cadence` before anything else it does —
before the town-is-down check, before the halt, before Րաֆֆի's wake. The
shared `/tmp` is a 16 GiB tmpfs with 1048576 inodes, and it is filled by AGENTS
rather than by the town's loop, so a halted or stopped town accumulates scratch
at the same rate a running one does; on 2026-08-22 it reached 99% of the inode
cap and refused writes town-wide while `df -h` still showed 5.9 G free
(bd `gqlc-vze6`, `gqlc-u078`). The reap is not a wake — no session, no tokens,
no seat — so the halt does not bind it.

It is advisory: it can neither fail the sweep nor, thanks to a timeout shorter
than the guard cadence, delay the next tick. It deletes nothing until `/tmp` is
at or past `reap_threshold` (75%, in whichever of bytes and inodes is fuller),
and below that it stops at one `statfs` without walking. What it will and will
not remove, and how it proves abandonment, is `just tmp-reap`'s doc comment and
`internal/tools/tmpreap`.

## Knobs

All in `kingdom.toml`: `max_active` (global concurrency cap; mayor and guard
exempt), dispatch/guard cadences, `handoff_threshold_pct`, `permission_mode`
(defaults to `bypassPermissions` — the factory runs unattended; the
constitution, the git hooks, and `claude-pre-bash` remain the guardrails). The
scratch reap threshold is the exception, and lives in the justfile beside the
tool it configures, since nothing on the km side reads it.
