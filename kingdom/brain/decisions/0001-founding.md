# 0001 — Founding decisions

Date: 2026-08-21. Decided by Անդրանիկ with Fable, at the founding.

The choices that shaped the kingdom, and what was rejected:

1. **Persistent tmux seats** over on-demand headless spawns or one
   team-under-the-mayor session. Seats survive between visits, the king can
   attach to any window, and a handoff cycles the session *in place* — same
   window, fresh context. A team under Սեդրակ dies with his session and
   bottlenecks on his context; headless spawns leave nothing to attach to.
2. **File-based mail + `bd mail` delegation** over Claude Code SendMessage or
   an MCP mail server. SendMessage only routes inside one live session tree —
   our seats are independent processes. Files deliver to sleeping seats,
   survive restarts, are greppable by guard and king, and non-agents (timers,
   hooks) can send them. Cost accepted: poll-based delivery, mitigated by
   wake-time reads and guard nudges.
3. **Same seat resumes its handoffs** (Constitution III.3) over
   any-citizen-of-class pools. Self-authored notes read best to their author;
   identity and bead ownership stay coherent. Release/reassignment exists for
   the exceptions.
4. **Global work-conserving concurrency cap** (`max_active = 5`) over
   per-class quotas or everyone-awake. Per-class quotas idle slots while a
   deep queue waits; everyone-awake burns to the quota wall mid-bead. Mayor
   and guard are exempt (rarely busy / seconds-long sweeps).
5. **Seat worktrees, not clones.** `bd` shares one beads database across
   worktrees via git common-directory discovery; separate clones would
   re-open the multi-clone sync races (bd gqlc-hv5u).
6. **Design-gate rides bd dependencies** — no new routing machinery. An
   execution bead blocked-by its design bead is invisible to `bd ready`
   until the design closes; the dispatcher cannot jump the gate.
7. **Models**: Ճարտարապետ = claude-fable-5 (design/review), Ռազմիկ and
   Քաղաքապետ = claude-opus-5 (execution/coordination), Պահակ =
   claude-sonnet-5 (sweeps are short; upgrade if his judgment proves thin).

---

**Superseded in part, 2026-08-22, by Անդրանիկ.** Item 7's parenthetical
"(design/review)" no longer describes the Ճարտարապետ role: architects design
and do not review. Review belongs to the Դատաւոր, and a design is not
reviewed at all. Constitution V.1–V.2 carry the current rule. The rest of
this record stands.
