# bd 1.0.4 → 1.2.2 upgrade evaluation (bd `gqlc-8mqlj`)

Rehearsed 2026-09-10. Nothing live was written: all probes ran on pristine
`cp -a` copies of `.beads` in `/tmp/opencode/bd122-*`, removed after.
Binaries: deployed `/usr/bin/bd` = 1.0.4 (`ce242a879`); rehearsal binary =
upstream v1.2.2 (`6c124203e`), fetched as the release tarball, run user-local.

## Verdict

**Do NOT flip the fleet binary yet.** The 21-migration jump applies cleanly
and delivers real fixes (0048 + 0049 verified in effect), but the upgrade as
a bare binary swap is unsafe for three measured reasons:

1. **First contact rewrites the store irreversibly and silently.** One
   read-only `bd info` under 1.2.2 doubles the noms store (63M → 124/125M)
   and changes table shapes, with no migration recorded anywhere the
   operator can see (`migrate --inspect` still says 1.0.4 afterwards).
2. **There is no binary-flip rollback.** After a 1.2.2 touch, the 1.0.4
   binary loses writes entirely and loses `status` / `dep list`. Rollback
   is restore-from-backup or nothing.
3. **The migration machinery contradicts itself.** `migrate schema` says
   "already at v53" while `migrate --inspect` on the same copy says
   "Schema Version: 1.0.4, Registered Migrations: 0, mismatch". One of
   those readers is wrong, so neither can gate the flip.

## Premise corrections to the bead as filed

- The gap is **32 → 53 migrations (21), not 61 → 102 (41).** Counted
  first-party via `gh api .../internal/storage/schema/migrations?ref=`:
  32 `.up.sql` files at `ce242a879` (highest 0032), 53 at v1.2.2
  (highest 0053). "v53" in `migrate schema` output is the file count.
- "`bd sql` working in embedded mode" **does not hold for this fleet.**
  Measured on 1.2.2 against a migrated copy: `bd sql` → `'bd sql' is not
  yet supported in embedded mode`, identical to 1.0.4. `bd dolt start`
  → `not supported in embedded mode (no Dolt server)`. The 2026-09-03
  reading must have been a server-mode or sqlite backend. No `bd sql`
  benefit accrues to an embeddeddolt fleet.

## Rehearsal matrix

Copies: B (size/control), C (old-binary blast radius), D (`migrate schema`
+ 1.2.2 battery), E (pristine 1.0.4 control). Each `cp -a` of the live
`.beads` (63M noms) with `GIT_DIR`/`GIT_WORK_TREE`/`GIT_INDEX_FILE` unset.

| # | Step | Expected | Observed |
|---|---|---|---|
| 1 | Fresh copy + 1.2.2 `info` | read-only, no write | **Store 63M → 124M.** Silent rewrite on a read path |
| 2 | Repeat 1 on second copy | same | Reproduced: 63M → 124M on one `bd info` |
| 3 | `migrate --inspect` after touch | version bumped / migrations listed | **Still `Schema Version: 1.0.4`, `Registered Migrations: 0`, mismatch warning.** The rewrite is untracked |
| 4 | Explicit `migrate schema --json` on pristine copy | applies 0033..0053 with output | `✓ Schema already at v53` in 2.3s — and the store STILL doubled (63M → 125M). "Nothing to do" that rewrites everything |
| 5 | Old binary `status` on pristine copy (control) | works | Works (966 total) |
| 6 | Old binary `status` after one 1.2.2 touch | works (rollback viable) | **BROKEN:** `compute blocked IDs: ... column "depends_on_id" could not be found` (rename is upstream 0041/0044/0045) |
| 7 | Old binary `list`/`show`/`ready`/`info` after touch | — | Still OK |
| 8 | Old binary `dep list` after touch | — | **BROKEN:** same `depends_on_id` error |
| 9 | Old binary `create` after touch | — | **BROKEN:** `record event in events: Error 1105: Field 'id' doesn't have a default value` (upstream 0051 dropped the default; old code relies on it) |
| 10 | Ceiling-B probe: single 70KB `note` | refused on 1.0.4 (fleet has it), accepted on migrated 1.2.2 | Refused on 1.0.4 (`failed to update issue: Error 1105: string 'AAA…`); **accepted on 1.2.2** → 0049 in effect |
| 11 | Ceiling-A probe: append after 70KB note | accepted on 1.2.2 | **Accepted** → 0048 in effect |
| 12 | `bd sql` / `bd dolt start` on migrated copy, 1.2.2 | per bead premise, working | **Both refused** in embedded mode (see premise corrections) |

## Behaviour deltas 1.0.4 → 1.2.2 (repo contracts affected)

Measured 1.2.2-on-migrated-copy vs 1.0.4-on-pristine-copy. Holds and
regressions named separately.

**Holds** (no doc change needed): `--status open` literal; `--all`
composes with label/assignee/type/priority; explicit `--status` beats
`--all`; `dep list` down-default + `--direction=up`; `show --json` is an
array; `update -d` moves `updated_at`; no-op update prints success and
changes nothing; `-l` on update refused (rc 1, empty stdout, nothing
written); two-field release works; multi-id `update` best-effort (rc 0)
vs multi-id `close` all-or-nothing (rc 1); `ready --limit 5` keeps its
stderr disclosure; `list --all --json -n 0` census identical (79 = 79
non-closed incl. probes); `--status all --limit 0` works (repo's
bd-gh-sync/ghorphan shape).

**D1 — `bd list` lost its row-cap disclosure.** 1.0.4: bare `list --json`
→ 50 rows + stderr notice. 1.2.2: bare `list --json` → all 79 rows, no
notice; explicit `--limit 50` → 50 rows, **no notice either**. A script
that hits a limit under 1.2.2 cannot tell. (`ready` still discloses, so
the two commands now disagree.) Repo sites all pass `--limit 0`
explicitly and are immune; `docs/bd-ledger-queries.md` ("caps are 50 for
list and 100 for ready", "disclosed on stderr") goes stale on upgrade.

**D2 — bare `bd list` no longer caps at 50.** 79 rows returned against a
help text still saying `default 50`. JSON and human tree rendering agree.
Stale-doc impact only (same file as D1).

**D3 — `bd show` gains count keys.** 1.2.2 `show` adds `comment_count`,
`dependency_count`, `dependent_count` (1.0.4 `show` lacks them although
1.0.4 `list` has them). Additive; the writes-doc sweep query's `del()`
list already names them. No breakage; note for pin authors.

**D4 — `bd history` regresses on real history.** 1.2.2 `history gqlc-8mqlj`
(203 entries) → `Scan error ... "description": converting NULL to string
is unsupported`; 1.0.4 reads it fine; 1.2.2 reads a 3-entry probe bead
fine. A NULL-tolerant reader became strict. Anything scripting `history`
(long beads first) breaks.

**D5 — `migrate --inspect` vs `migrate schema` disagree.** Same copy:
`schema` says v53/current, `--inspect` says 1.0.4/0-registered/mismatch
(steps 3–4). The gate an operator would check pre-flip cannot be
trusted until upstream reconciles the two readers (cf.
gastownhall/beads#6142).

Rehearsal-method caveat: l7d7q-style "jsonl export still moves" cannot be
witnessed in non-git scratch — neither binary moves `issues.jsonl` there
(verified both). That check needs a git-backed copy to mean anything.

## Rollback path

Binary flip-back is **not** a rollback (steps 6/8/9: old binary loses
writes, `status`, `dep list` on a touched DB). The only rollback is
restore-from-backup: `bd backup init <path>` + `bd backup sync` taken
with 1.0.4 BEFORE the flip, `bd backup restore` on retreat. The operator
request below makes the backup the load-bearing step. Note the fleet
currently has no dolt remote and no backup destination configured.

## Operator request (hand-over text; NOT filed anywhere)

> Operator (agent-n): please do NOT upgrade /usr/bin/bd past 1.0.4 yet.
> Rehearsed 2026-09-10 on pristine copies of the live ledger (bd
> `gqlc-8mqlj`, report: `docs/bd-upgrade-eval-8mqlj.md` in the PR):
> the 1.2.2 binary migrates the store silently on first contact (63M →
> ~125M on a read-only `info`), records no migration (`--inspect` still
> reports 1.0.4), and its own two migration readers contradict each other
> ("already at v53" vs "mismatch"). After the touch, bd 1.0.4 loses all
> writes (`events.id` default gone, upstream 0051) and loses `status` /
> `dep list` (`depends_on_id` renamed, upstream 0041/0044/0045) — so a
> flip-back is not a rollback. When upstream has a trustworthy,
> recorded migration (reconciled readers, answering #6142-class
> behaviour), the safe sequence is: `bd backup init` + `sync` under
> 1.0.4 → flip binary → `migrate schema` with observed output →
> contract spot-checks per the report's Holds list → keep the backup
> until one sync round-trip is green. Requested pre-steps for upstream:
> reconcile `migrate --inspect` with `migrate schema`; restore the
> `bd list` cap notice; tolerate NULL `description` in `history`.
> Evidence: bead notes on `gqlc-8mqlj` + this PR. No live state was
> touched by the rehearsal.

## Upstream follow-ups proposed (not filed; parent's call)

1. `migrate --inspect` / `migrate schema` disagreement + silent
   storage rewrite on open (extends #6142 evidence).
2. `bd list` cap disclosure removed (regression vs 1.0.4).
3. `bd history` NULL-`description` scan error (regression vs 1.0.4).
