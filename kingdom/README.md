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
| Ճարտարապետ (architect) | Արթուր, Արփինէ, Արեգակ | claude-fable-5 | Designs. Turns intent into implementation-ready beads. Reviews warriors' PRs. |
| Ռազմիկ (warrior) | Արամազդ, Վահագն, Աստղիկ, Ար, Նուարդ, Այգ, Ծովինար, Հայկ | claude-opus-5 | Executes beads. Ships PRs. Tests first (`/tdd`), red before green. |
| Դատաւոր (judge) | Միհր | claude-fable-5 | Adversarial code-quality review (`/thermo-nuclear-code-quality-review`). Judges code, never people. |
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

Runtime state lives OUTSIDE the repo, shared by every seat, never committed:
`<parent-of-main-checkout>/kingdom-state/` — maildirs (`mail/<seat>/{inbox,read,sent}`),
seat heartbeats and status, handoff notes, the halt flag. `bin/km` derives the
path from `git rev-parse --git-common-dir`, so it resolves identically from any
seat worktree.

## How work flows

1. Work arrives as beads (from Անդրանիկ, from Սեդրակ's intake, from citizens
   filing follow-ups). Routing rides labels: `class:architect` for design work,
   `class:warrior` for execution, `class:judge` for adversarial review.
2. Work that needs a design first follows the design-gate
   (`brain/playbooks/design-gate.md`): the execution bead is created
   blocked-by the design bead, so `bd ready` hides it until an architect
   closes the design.
3. The dispatcher (`km dispatch`, systemd timer) wakes a free seat of the right
   class for each ready labelled bead, priority first, up to the global
   `max_active` cap. The cap is work-conserving: slots are not reserved per
   class, so an idle class donates its slots.
4. A warrior's PR needs a Ճարտարապետ review PASS to merge; the review request
   travels by mail. Disputes go to Սեդրակ; what Սեդրակ cannot settle goes to
   Անդրանիկ.
5. Միհր, the Դատաւոր, adds a second, adversarial line of defence: review
   beads (`class:judge`) route the riskiest changes to him, and he patrols
   merged work on his own judgment. His FAIL on an open PR blocks the merge;
   his findings on merged code become defect beads — and postmortems when
   something broke, blame-free always (`brain/postmortems/`).

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
just kingdom-halt       touch the halt flag: dispatcher stops waking seats
just kingdom-resume     remove the halt flag
```

Mail from anyone to the king lands in `kingdom-state/mail/andranik/inbox/`;
`just kingdom` shows the unread count.

## Prerequisites

- `tmux` (not vendored; on this box: `sudo pacman -S tmux`)
- `claude` CLI, `bd`, `jq`, `python3` — already present
- One-time: `just kingdom-install`, then `just kingdom-up`

## Knobs

All in `kingdom.toml`: `max_active` (global concurrency cap; mayor and guard
exempt), dispatch/guard cadences, `handoff_threshold_pct`, `permission_mode`
(defaults to `bypassPermissions` — the factory runs unattended; the
constitution, the git hooks, and `claude-pre-bash` remain the guardrails).
