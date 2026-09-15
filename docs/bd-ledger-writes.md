# Writing to the bd ledger

Sibling of [bd-ledger-queries.md](bd-ledger-queries.md), which covers the read
side. `bd` is an external tool (`/usr/bin/bd`, a node shim over a native binary);
its source is not in this repository and none of this can be fixed by patching
it. What follows is the write-side contract as measured against the deployed
binary.

Except where a section says otherwise, the rows below were taken first-party on
2026-08-24 against **bd 1.0.4 (`ce242a879`)**, in throwaway `bd init` workspaces
— never against the live ledger. Binary unchanged on this host since 2026-05-09,
so it is the same binary everyone here has been running. bd `gqlc-o6kp`.

The one exception is the record-size ceiling, measured 2026-09-03 against the
same binary but on a **probe bead in the live ledger**, created and deleted for
it. It is called out here rather than left to the section because "never against
the live ledger" is the kind of blanket assurance that should not quietly stop
being true.

The pre-image census of 2026-09-05 is also live-ledger work, but read-only: it
reconstructs `gqlc-jffyz`'s freeze-time record from its current one rather than
provoking a fresh refusal, precisely so that no bead had to be written to in
order to measure the thing that punishes writes.

The second exception is larger and is named here for the same reason. On
2026-09-14 the ceiling was **removed from the live ledger** by a schema
migration — a DDL write against the shared fleet DB, authorised by the owner,
taken with a full `cp -a` backup as the rollback point. Every row of that repair
was rehearsed on a throwaway copy first and every claim below that it falsifies
has been corrected in place rather than deleted. See
[Applying the fix elsewhere](#applying-the-fix-elsewhere).

## The confirmation line means "accepted", not "changed"

    $ bd update <id> --status blocked --assignee somebody   # already both
    ✓ Updated issue: <id> — <title>
    rc=0, and not one field moved.

`✓ Updated issue` reports that the command was accepted, not that a row changed.
There is no separate rendering for an update that was a no-op. **A success line
is not evidence of a write**, so anyone who watches for one and stops there
has checked nothing. Read the field back.

## A refused flag or value discards the whole update — loudly

Every rejection path measured behaves identically: exit 1, **stdout empty**, the
reason on stderr, and **no field written, including the fields that were valid**.

| Command (single id) | rc | stdout | wrote |
| --- | --- | --- | --- |
| `--status open` | 0 | `✓ Updated issue` | status |
| `--assignee ""` | 0 | `✓ Updated issue` | assignee |
| `--assignee "" --status open` | 0 | `✓ Updated issue` | both |
| `--status open --set-labels <label>` | 0 | `✓ Updated issue` | both |
| `-l <label>` | 1 | *empty* | nothing |
| `--status open -l <label>` | 1 | *empty* | nothing |
| `--status open --frobnicate x` | 1 | *empty* | nothing |
| `--status bogus` | 1 | *empty* | nothing |
| `--assignee "" --status bogus` | 1 | *empty* | nothing |
| `--priority 9` | 1 | *empty* | nothing |
| `--status open --priority 9` | 1 | *empty* | nothing |

Two consequences worth stating separately.

**Multi-flag updates are not poisoned as a class.** Rows 3 and 4 apply both
fields. The documented two-field release —

    bd update <id> --assignee "" --status open

— is safe, and `--assignee ""` is parsed as a value, not as a missing one.

**One bad flag costs the whole command, not just itself.** Validation runs before
the write, so a valid `--status` sitting beside an invalid `--priority` is
discarded with it. That is the right behaviour — a half-applied release is worse
than a refused one — but it means a typo silently costs every intent on the line
if nobody reads the exit status.

### `bd update` has no `-l`

Labels on `bd update` are `--add-label`, `--remove-label` and `--set-labels`.
`-l, --labels` is **`bd create`'s** flag. Carrying the habit across from `create`
costs the entire update, with this on stderr and nothing on stdout:

    Error: unknown shorthand flag: 'l' in -l

## Where the silence actually comes from

Put the two halves together. A refused write produces **empty stdout, exit 1, and
a reason on stderr only**. Discard stderr — which this repository's scripted call
sites do, and which agent transcripts routinely do — and ignore the exit status,
and a refused write is indistinguishable from a silent no-op. The tool was loud;
the channel it was loud on was closed.

This is the same failure the query side has with the row cap notice: bd discloses
on stderr, and stderr is where nobody is listening.

## `bd update` is per-id best-effort; `bd close` is all-or-nothing

Both accept several ids. They disagree about what a bad one means, and the
disagreement is in the exit status, which is the thing scripts read:

    $ bd update <good-id> <nonexistent-id> --status open
    ✓ Updated issue: <good-id> — <title>          # stdout
    Error resolving <nonexistent-id>: ...          # stderr
    rc=0                                           # <-- reports success
    good-id was updated; nonexistent-id was not.

    $ bd close <good-id> <nonexistent-id>
    Error: resolving ID <nonexistent-id>: ...      # stderr
    rc=1
    good-id was NOT closed. Nothing was written.

So `bd update` is the one shape measured here where **a partially failed write
exits 0**. Order does not matter — the bad id first or second gives the same
result. Name one bead per `bd update` whose success you intend to check by exit
status, or check each bead's state afterwards rather than the command's.

## A bead that grows past ~65535 bytes stops accepting writes

> **Retired on this ledger, 2026-09-14** (bd `gqlc-l7d7q`, `gqlc-3q77o`). All
> thirteen columns involved are `LONGTEXT` now, so **nothing below is a reason to
> ration what you write into a bead.** The section is kept because two things in
> it outlive the fix: it is still exactly true of any ledger that has *not* had
> the migration applied — which includes a fresh clone, since deployed bd
> v1.0.4 (`ce242a879678`) does not carry upstream `0048`/`0049` at all — and the
> measuring technique below is still how you find out which ledger you are on.
> The witness for the repair is `gqlc-jffyz` itself: frozen and un-updatable by
> anyone for eleven days, it took an `--append-notes` at `rc=0` on 2026-09-14.
> What was applied, and how `0049` silently under-applies, is under
> [Applying the fix elsewhere](#applying-the-fix-elsewhere).

Measured 2026-09-03 against the same bd 1.0.4, on a throwaway bead `gqlc-zniam`
created and deleted for it. The live casualty, `gqlc-jffyz`, was not written to:
reproducing the state on a probe costs nothing and testing it on a bead holding
real handoff notes is the damage itself. bd `gqlc-a8j2i`.

Every write records an event, and the events row carries **the whole bead
serialised as JSON, in a column named `old_value`** — the pre-image, not a
delta. The column is a MySQL/Dolt `TEXT`, so once a bead's serialised record
passes that type's 65535-byte ceiling the event can no longer be written and
`bd update` fails:

    Error updating <id>: failed to record event: record event in events:
    Error 1105: string '{"id":"<id>","title":...}' is too large for column 'old_value'

Three consequences, each measured rather than reasoned from the message:

- **The size of the write is irrelevant.** Five bytes of note are refused for
  the same reason fifty thousand are: what is too large is the pre-image, which
  is the same either way.
- **Shrinking does not recover it.** `bd update --notes` with a short body is
  refused too. It cannot be otherwise: the event must record the oversized *old*
  value before the smaller new one exists. There is no path back under the
  ceiling through `bd update`.
- **`bd close` still works** — it does not go through that path — but it is a
  one-way door. A closed oversized bead cannot be reopened, and neither
  `--status`, `--priority` nor `--append-notes` will move afterwards. So the
  bead is closable but never again editable. *(This door is what the 2026-09-14
  migration opened: the cap on `events.old_value` was the whole enforcement, so
  widening it released every bead already behind it. `gqlc-jffyz` was closed and
  oversized on both counts and now accepts writes again.)*

**Reads are entirely unaffected**, which is why nothing shows it. `bd show`,
`bd list` and `bd ready` all answer normally and the bead looks healthy on every
board. The only symptom is on the stderr of a write, which is the failure mode
the rest of this document is about.

### Measuring how close a bead is

The pre-image is **the whole bead record with `dependencies`, `dependents` and
`parent` removed** — not a fixed list of keys. bd omits null fields, so the key
set varies per bead: an open bead carries no `closed_at` or `close_reason`, a
closed one carries both, and `close_reason` in this ledger is often several
paragraphs. Counting a fixed subset is what made the previous version of this
section read low, by up to 15310 bytes (`gqlc-h9n.7`: 23636 actual, 8326
reported — 65% low).

Three properties decide the number, and each was worth hundreds to thousands of
bytes when it was left out:

- It is **compact** — no whitespace between tokens.
- It carries Go `encoding/json`'s **HTML escaping**: the three characters `<`,
  `>` and `&` are each rewritten to their six-byte `\uXXXX` JSON escape form —
  the exact three strings are in the query below. 1139 of 1656 beads carry at
  least one, and the largest
  such delta on the ledger is 1050 bytes (`gqlc-35yu.5`).
- The ceiling is on **bytes**. jq's `length` on a string counts codepoints, so
  against any non-ASCII prose in a bead it reads low, and it reads low by more
  the further outside Latin-1 that prose sits. Use `utf8bytelength`.

<!-- Validated byte-for-byte against the refusal dump; see the paragraph below
     before changing any of the three transforms. -->

    bd list --all --json -n 0 | jq -r '
      .[] | del(.dependencies, .dependency_count, .dependent_count,
                .comment_count, .parent)
      | [ (tojson | gsub("<";"\\u003c") | gsub(">";"\\u003e")
                  | gsub("&";"\\u0026") | utf8bytelength), .id ]
      | @tsv' | sort -rn | head

**Validated against the only first-party measurement of a pre-image there is,
which is the refusal's own dump.** `gqlc-jffyz` froze at a dumped 66492 bytes.
It has since been closed, which added `closed_at` and `close_reason` and
shortened `status` from `in_progress` to `closed`; reconstructing that
freeze-time record from today's reproduces **66492 exactly**, and **65917** with
the HTML escaping alone removed — both figures matching an independent
derivation on bd `gqlc-budsa`. The escaping is therefore live and load-bearing
at 575 bytes on that bead, and the whole question at the ceiling is decided in
the last few hundred. One bead is the whole validation set, because it is the
only one whose dump was recorded.

The 65535 constant is still inferred from a bracket rather than read out of the
engine: acceptance at a computed payload of roughly 65500 and refusal at 65545.

**The direction of the error is not fixed, so no shortcut is safe in one
direction.** `bd show --json | jq -c '.[0]'` inlines `dependencies` and
`dependents` as whole objects and so reads *high* for a bead that has them — by
75438 bytes on `gqlc-3evsn`. But 1132 of 1656 beads have no dependencies,
dependents or parent at all, and for every one of those the same command reads
*low*, by exactly the escaping delta. Reading high wastes a bead; reading low
tells an author to keep appending to a bead that is already dead, and the
appends are what is lost. Do not trade one for the other — measure the
pre-image.

**Ledger-wide on 2026-09-05: 1656 beads, two over the line** — `gqlc-jffyz` at
68146 and `gqlc-2m9r8` at 66962. The next largest is 44095 (`gqlc-3evsn`), so
nothing sits in the 45k–65k approach and this is still not a fleet-wide cliff.
The previous census here — 1533 beads, one over, next largest 42151 — was taken
with the under-reading query above; its 42151 is `gqlc-3evsn` measured that way,
and reproduces today, so that figure was not stale but understated. Both
instruments happen to name the same beads today, and that agreement is thin
rather than reassuring: the old query put `gqlc-jffyz` at 65540 against a 65535
ceiling, five bytes of margin on a bead 2611 bytes over.

The distribution only grows in one direction, because notes append and nothing
prunes them, so the beads that approach the ceiling are the ones worked over
many sessions — which is exactly the population whose notes hold the handoff. It
fires first where it costs most.

`--append-notes` is still the right flag (a bare `--notes` replaces, silently
losing the trail). The tension that used to sit here — the discipline that makes
notes valuable being the one that eventually freezes the bead — is resolved on
this ledger rather than merely deferred: `LONGTEXT` is a 4GB type, so appending
is no longer a slow way of spending a bead.

## Applying the fix elsewhere

Three files, applied in one window on 2026-09-14. The first two are upstream's
own bytes, taken from `gastownhall/beads` at `f56632adcfab`:

    internal/storage/schema/migrations/0048_widen_event_value_columns.up.sql
    internal/storage/schema/migrations/0049_longtext_large_content_columns.up.sql

`0048` widens `events.old_value` / `new_value`; `0049` widens the eleven
large-content columns on `issues`, `wisps` and `comments`. **Neither exists at
the deployed bd commit** `ce242a879678` (v1.0.4), whose migrations are the 17 Go
files under `internal/storage/dolt/migrations/` — so these are upstream bytes
that our bd would never run on its own, and they have to be applied by hand
against the embedded dolt directory with a matching CLI (dolt v1.86.4, tag
commit `73fcb4a868c2`, which is exactly the dolt bd 1.0.4 embeds).

**`0049` silently under-applies on dolt 1.86.4, and this is the trap.** It exits
`0`, prints no error, and moves only **8 of its 11 columns**. The three it skips
are its single-column `PREPARE`/`EXECUTE` blocks:

    issues.close_reason    wisps.close_reason    comments.text

Its two multi-column blocks apply correctly. **The guards are not the cause** —
measured directly, the guard subquery for `issues.close_reason` returns `1` and
`SET @t = (SELECT ...)` reads `1` back, so the guard asks for the fix and the
fix does not happen. The root cause inside dolt's prepared-statement handling
was not chased down; it is not needed for the repair. The three are restated
unguarded, each carrying the nullability and `DEFAULT` read off
`INFORMATION_SCHEMA` **before** the change, because `MODIFY` replaces the whole
column definition and dropping a `DEFAULT` regresses inserts that omit it:

    ALTER TABLE issues   MODIFY COLUMN close_reason LONGTEXT DEFAULT '';
    ALTER TABLE wisps    MODIFY COLUMN close_reason LONGTEXT DEFAULT '';
    ALTER TABLE comments MODIFY COLUMN `text`       LONGTEXT NOT NULL;

**`rc=0` is not evidence that a DDL applied.** Nothing else reports this failure
— no stderr, no non-zero exit, and bd keeps working half-migrated — so the only
verdict is a read-back, which must return zero rows when you are done:

    SELECT TABLE_NAME, COLUMN_NAME FROM INFORMATION_SCHEMA.COLUMNS
    WHERE TABLE_SCHEMA = DATABASE() AND COLUMN_TYPE = 'text'
      AND ( (TABLE_NAME = 'events' AND COLUMN_NAME IN ('old_value','new_value'))
         OR (TABLE_NAME IN ('issues','wisps') AND COLUMN_NAME IN
             ('notes','description','design','acceptance_criteria','close_reason'))
         OR (TABLE_NAME = 'comments' AND COLUMN_NAME = 'text') );

Two notes on rehearsing it, both of which cost time here. The control this
repair was originally specified against — `bd update gqlc-jffyz` must be refused
— **was no longer executable**, because `gqlc-jffyz` had since been closed; a
fresh frozen bead has to be manufactured on the copy instead, and the cheapest
way is `--notes` of 60000 bytes plus `--description` of 20000, which puts the
record over the line without any single column exceeding it. And `BEADS_DIR`
pointing at a copy is not by itself proof that writes land there: create a bead
on the copy and confirm it is **absent from live**, with both probes run from a
directory where bd resolves a database. Run from somewhere it resolves nothing
and the probe returns `no beads database found`, which reads exactly like a
clean pass.

## Rules for a scripted write

1. **Check the exit status.** It is load-bearing on every rejection path.
2. **Do not discard stderr on a write.** Capture it and print it on the failure
   branch; a bare `2>/dev/null` throws away the only account of what refused.
3. **One id per `bd update`** you intend to verify by exit status.
4. **Read back the field that matters**, and for routability read it back with
   `bd ready` — `bd show` will display a bead that `bd ready` will not return.
5. **Never treat `✓` as a write.**
6. **On a long-lived bead, `--append-notes` keeps working** — the record-size
   ceiling that used to make this rule the opposite was removed from this ledger
   on 2026-09-14. On any ledger that has not had that migration applied, the old
   rule stands unchanged: past the ceiling every update is refused for good. The
   section above says how to tell the two apart, and it is a schema read rather
   than a judgement call.

## What is gated, and what is not

`.github/workflows/bd-behaviour.yml` runs these claims against the **latest
released** bd, in its own job, so this repository learns that a future bd has
changed its mind before it upgrades. That workflow is an ALARM, not a merge gate,
for the reasons its own header gives — the claims are about a binary this repository
neither owns nor vendors.

Nothing gates the write call sites themselves, and that is a deliberate
omission rather than an oversight. The query side has a sweep because it has
eleven call sites; when this was taken, the write side had **one**.

## Audit of this repository's write call sites

Taken 2026-08-24 with the same scanner shape the query sweep uses (comments cut,
quoted spans blanked, command position required).

The site the 2026-08-24 and 2026-08-29 (bd `gqlc-u2nim`) readings both named has
since been deleted with the script that held it, so neither reading can be
reproduced. Every other match at that time was fixture text in a hook test under
`.githooks/tests/` — `bd close` command strings handed to a hook as data and
never executed — and PR #1595 deleted that directory too. Re-grepped for this
document's own removal, one scripted write survives:

| Site | Call | Verdict |
| --- | --- | --- |
| `.githooks/bd-gh-sync` 1313 | `bd update --append-notes ... >/dev/null 2>&1 \|\| _notes_failed=...` | correct — exit status is the whole evidence an append needs, per the rule above, and the count it feeds is reported |

That re-grep is a grep and not the scanner: the scanner shape described above
cuts comments and blanks quoted spans, and no such tool survives in this tree to
re-run. So the table is a dated reading with nothing re-deriving it.

The exposure this document addresses is therefore mostly in the **recipes people
type by hand**, not in the scripts — "Rules for a scripted write" above is the
whole of the discipline, and it applies to a recipe typed once just as much.

## What could not be reproduced

`gqlc-o6kp` was filed on the observation that

    bd update <id> --assignee "" --status open -l <label>

printed `✓ Updated issue:` and changed nothing, and it asked whether an
unhonoured flag discards its siblings while still reporting success. Half of
that reproduces and half does not, and the half that does not matters to anyone
reading the bead later.

The discard is real and total. The success line is not: that argv exits 1 with
empty stdout on the deployed binary, and on the latest release, and there is only
one `bd` on this host with an unchanged mtime, so it is not a version difference.
A `✓` and that argv cannot come from the same invocation.

What the bead recorded faithfully is the **experience** — and the experience is
fully explained without any success line being printed by that command, once you
know that the refusal writes nothing to stdout and speaks only on stderr. The
bead's own step 2 records `-l` alone as having "printed NO output at all", which
is exactly what a stderr-suppressed rc=1 looks like from the inside.

That is why the rules above are about the exit status and stderr rather than
about a flag: the reported defect was not bd being quiet, it was bd being loud in
a place nobody was reading.
