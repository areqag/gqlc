# `bd prime` creates a ledger at a BEADS_DIR holding none

Sibling of [bd-ledger-queries.md](bd-ledger-queries.md) and
[bd-ledger-writes.md](bd-ledger-writes.md). Like those, this is a record of the
**deployed** tool's behaviour, not of anything this repository can patch: `bd`
is `/usr/bin/bd`, a symlink into `/usr/lib/node_modules/@beads/bd`, and its
source is not vendored here. Confirmed 2026-09-10.

Measured first-party 2026-09-10 against **bd 1.0.4 (`ce242a879`)**, in throwaway
`/tmp` directories with cwd outside any git repository, so nothing below could
reach the live ledger even by discovery. The behaviour was first reported on
2026-09-01/02 from `gqlc-zpjuc` and is filed as bd `gqlc-5smcw`.

## The defect in one line

Every other bd verb **refuses** a `BEADS_DIR` that holds config but no database.
`bd prime` **creates** one, says nothing about having done so, and from that
moment every consumer sharing the path gets confident, successful, empty
answers about a fleet that is not theirs.

## The reproduction

A directory holding only the two config files a `.beads` directory normally
carries, and no database:

    $ bdir=$(mktemp -d)/beads-no-db && mkdir -p "$bdir"
    $ printf '# Beads Configuration File\n' > "$bdir/config.yaml"
    $ printf '{"database":"dolt","backend":"dolt","dolt_mode":"embedded",
               "dolt_database":"probe","project_id":"0000..."}\n' > "$bdir/metadata.json"
    $ cd "$(mktemp -d)"                 # outside any git repository
    $ ls -A "$bdir"
    config.yaml  metadata.json

**Step 1 — every ordinary verb refuses, and creates nothing.**

    $ BEADS_DIR="$bdir" bd stats
    Error: no beads database found
    Hint: run 'bd where' to inspect the resolved workspace, or 'bd init' to create
          a new database, or set BEADS_DIR to point to your .beads directory
    $ ls -A "$bdir"
    config.yaml  metadata.json

This is the correct answer and the useful one. It names the condition and it
offers `bd init` — the explicit, opt-in creation verb that already exists.

**Step 2 — `bd prime` creates the database instead.**

    $ BEADS_DIR="$bdir" bd prime; echo "rc=$?"
    rc=0
    $ ls -A "$bdir"
    config.yaml  embeddeddolt  metadata.json

`embeddeddolt` is new. The exit status is 0. The **entire** stderr of that run
was one unrelated warning about the directory's permissions — no line of it
mentions a database, a creation, or `bd init`:

    Warning: /tmp/.../beads-no-db has permissions 0755 (recommended: 0700).

**Step 3 — the same command that refused in step 1 now answers.**

    $ BEADS_DIR="$bdir" bd stats; echo "rc=$?"
    📊 Issue Database Status
    Summary:
      Total Issues:           0
      Open:                   0
      ...
    rc=0

    $ BEADS_DIR="$bdir" bd ready --json
    []
    $ BEADS_DIR="$bdir" bd list --all --json -n 0
    []
    $ BEADS_DIR="$bdir" bd show gqlc-o22k; echo "rc=$?"   # a bead that certainly exists
    rc=1                                                  # and NOTHING on stdout

The three aggregate queries are the hazard: they succeed, and each reports a
healthy, empty project. That is the failure mode — **a successful command** —
and it is byte-for-byte indistinguishable from a genuinely new project with no
issues in it. `bd show` is the one verb that does signal, and only by exit
status: it prints nothing at all on stdout, so a call site that reads its output
and ignores `$?` sees the same empty string it would see for a real absence.
This is why `bd show <id> --json | jq '.[0]'` is a safe existence test only when
the status is also checked.

## Why `prime` specifically

`prime` is the amplifier, and the distinction is in the *verb*, not the cwd.
Measured 2026-09-02 across both arms, `BEADS_DIR` pointed at a config-only
directory:

| cwd | `bd prime` | other verbs |
| --- | --- | --- |
| outside any repo | rc=0, **created** `embeddeddolt` | `Error: no beads database found` |
| inside a beads repo | rc=0, **created** `embeddeddolt` (+ backup) | ignore `BEADS_DIR`, fall back to discovery, answer correctly |

So a wrong `BEADS_DIR` on its own is **latent** — every ordinary verb is inert
against it, either refusing or quietly answering from the real ledger. `prime`
is what arms it, permanently, for every consumer of that path.

That matters here because `bd prime` is not a verb anyone chooses to run: it is
wired into the `SessionStart` hook in `.claude/settings.json`, so **any** agent
session inherits the arming before it runs a single query of its own. This is
the mechanism by which one wrong environment variable put ten agents to work
against an empty database for four hours (`gqlc-zpjuc`).

Related and separately recorded: a `BEADS_DIR` that does not exist at all
silently falls back to discovery, so a working `bd` query is **not** evidence
that your environment points anywhere in particular (memory
`workflow-absent-beads-dir-silently-falls-back`).

## What is wanted upstream

Not fixable here. The fix belongs in bd, and the shape is fail-closed with the
bootstrap path preserved — which bd already has, because `bd init` exists and is
the explicit opt-in:

1. `bd prime` should **refuse** a `BEADS_DIR` whose directory exists and holds
   config but no database, with the message `bd stats` already gives, naming
   `bd init` as the remedy. Creation stays available; it stops being implicit.
2. Failing that, it should say on **stderr** that it created a database and
   where. A single line converts a silent, permanent wrong answer into one a
   reader can act on.

Recommendation 1 does not break first-time setup, and that was checked before
it was written down rather than assumed — a fail-closed guard that forbids its
own bootstrap path bricks a fresh checkout, and that is invisible from a
workspace which is already initialised. Measured 2026-09-10 in an empty `/tmp`
directory: `bd init --prefix probe` exits 0, creates `embeddeddolt` beside the
config, installs hooks, and the resulting ledger then accepts `bd create` and
answers `bd list --all --json -n 0` with the created bead. So the explicit path
is whole, and `prime` refusing an implicit creation takes nothing away from it.

No upstream issue covering this existed on `gastownhall/beads` as of 2026-09-10.

What *can* be done here is to guard the call site rather than the tool: the
`SessionStart` hook is the only reason `bd prime` runs unattended, and it can
refuse the config-only directory itself. That is bd `gqlc-q2jb`, and it is
deliberately not part of the change that added this document —
`.claude/settings.json` belongs to a different lane.

## How to tell whether you are in this state

`bd prime` leaves no trace in its own output, so ask the filesystem and the
board rather than the tool's exit status:

    $ bd where                       # what workspace did bd actually resolve?
    $ ls -A "${BEADS_DIR:-.beads}"   # is there an `embeddeddolt` beside the config?
    $ bd show gqlc-o22k              # a bead known to exist: found, or not?

The last one is the cheapest positive control, and it is the one to reach for
first when a board reads empty or a bead you filed has "vanished" — an empty
`bd ready` is the symptom of this defect and of a finished fleet alike.
