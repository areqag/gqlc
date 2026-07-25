# The config file declares a list of generation targets

`gqlc.yaml` carries a `graph` sequence, one entry per **generation
target**: its own schema, query directory, language axes, and a
`gen.go` block naming the package, the output directory, and the
driver. One `gqlc generate` run produces every target's package. The
wire shape is sqlc's version-2 `sql:` list, key for key, with `graph`
in place of `sql` and `driver` in place of `sql_package`. The format
version stays 1: the old single-target shape stops loading.

## Context

- gqlc is the sqlc analogue for graph query languages, and users
  arrive with sqlc's model of a project: one `sql:` list, one entry per
  generated package, each entry naming its schema, its queries, its
  engine, and a `gen:` block with `package`, `out`, and `sql_package`.
- The motivating project has one schema and many modules, each with its
  own query directory and its own generated package
  (`internal/user/gen`, `internal/order/gen`). Today's flat file
  declares exactly one of everything, so that project needs one config
  file and one invocation per module, and nothing checks that the two
  output directories differ.
- ADR 0012 gave the output directory exclusive ownership: each run
  wipes and rewrites it, guarded by the all-marked tripwire. The CLI
  implements that as one interleaved pass — stat, read, sweep,
  abort-or-wipe, write (CLI-1 §5.1).
- `codegen.Generate` is pure and the CLI owns every write, so the
  ordering of checks against writes is a CLI-side decision, not a
  pipeline one.
- The loader's version seam (config-file-format §5) is
  probe-then-dispatch: each accepted version keeps its decoder, so old
  files load forever. Only version 1 has ever existed, the released
  binary is v0.0.1, and the format has no users outside this repo.

## Decision

**The wire shape mirrors sqlc.** The document is `version` plus
`graph`, a non-empty sequence. Each entry carries `schema`,
`schema_language`, `queries`, `query_language`, an optional `procsig`,
and a required `gen:` mapping whose one key is `go:` — `package`,
`out`, `driver`. `schema` repeats per entry rather than hoisting to
the document root; the language axes stay at entry level beside the
inputs they describe, as sqlc's `engine` does.

**One entry is one generation target**: one parsed schema, one query
directory, one Go package written into one output directory. In
memory, `Config{Targets []Target}`.

**The version stays 1 and this is a breaking change to it.** A file in
the old flat shape no longer loads, and gets a targeted error naming
the shape change rather than the version machinery's messages — it
still declares `version: 1`, truthfully.

**Errors are entry-scoped.** Every message the loader and the pipeline
form about one entry carries a `graph[i]: ` prefix, whatever the entry
count. A target that fails at setup — unreadable schema, procsig
failure, empty query directory, codegen error, unmapped axis — aborts
the whole run at that entry. Query-level diagnostics still accumulate,
now across every target as well as every file, and any diagnostic
anywhere means nothing is written for any target.

**No two targets may share an output directory.** The loader rejects a
document in which one entry's `out` equals or contains another's, on
`filepath.Rel`-compared values.

**The write is two-phase.** The CLI runs the ADR 0012 tripwire's
inspect steps for every target first, then commits every target. A run
that aborts under the tripwire has mutated nothing.

**Each entry parses its own schema.** Ten entries naming one schema
file parse it ten times.

## Considered options

- **Hoist `schema` to the document root**, with the sequence carrying
  only what differs. Rejected: it departs from sqlc for one saved line,
  and a project that genuinely wants two schemas then needs a per-entry
  override — the repeated key with no override rule is the smaller
  format.
- **Ship this as `version: 2` with the v1 decoder migrating old files
  forward.** Rejected: the migration decoder, its test matrix, and the
  two-shape `Save` question are permanent costs, bought to protect
  files that exist only on the authors' disks. Pre-1.0 with no users is
  precisely the window in which the old shape can simply stop loading.
  The probe-then-dispatch seam is untouched and is what a genuine v2
  will use; what is spent here is the grace period, not the mechanism.
- **Accumulate setup failures across targets** and report them
  together. Rejected: every stage-3-to-8 error would need a
  partial-result notion in the pipeline and a "which targets did run"
  question in the CLI, to report a second failure the user reaches
  anyway on the next run.
- **Run today's interleaved write once per target.** Rejected: entry 0
  is wiped and rewritten before entry 2's sweep discovers a
  hand-written file, so an abort leaves a half-generated project — a
  failure mode single-target gqlc does not have, created by the same
  ADR 0012 guard that exists to prevent surprises.
- **Reject only exactly duplicate `out` values.** Rejected in favour of
  rejecting containment too: a nested pair (`internal/db` and
  `internal/db/gen`) generates successfully once and then fails on
  every subsequent run, because the parent's sweep sees the child
  directory it cannot prove marked. Overlap and equality are one check
  and one class of mistake.
- **Parse each distinct schema path once and share the
  `schema.Schema` across the resolvers that name it.** Rejected: it
  puts one value behind N consumers and makes "is `schema.Schema`
  safe to alias?" a question the code has to answer, to save
  milliseconds on a corpus that front-ends in milliseconds. Revisit
  only against a profile.
- **An `init` wizard that loops until the user stops adding targets.**
  Rejected: `init` writes one target and `init --add` appends one, so
  the confirm gate always guards a single, previewable delta.

## Consequences

- **ADR 0012 is amended, not superseded.** Read "the output directory"
  there as "each generation target's output directory": ownership,
  the wipe, and the all-marked tripwire are per target, and the abort
  message names its entry. Its consequence "errors surface before any
  write" now holds across targets only because of the two-phase split
  — with N targets the purity of `codegen.Generate` alone no longer
  buys it, since target 0's write precedes target 2's inspection.
- **A config file written for gqlc v0.0.1 fails.** It declares
  `version: 1`, so the version machinery has nothing to say about it;
  the loader recognises the old top-level keys and says the format
  changed.
- **N entries naming one schema file parse it N times**, and two
  targets that generate identical `models.go` bytes both write them.
- **Output directories are checked lexically at load.** Equality and
  containment between two paths written in the same style are caught;
  an absolute path and a relative one naming the same directory, or two
  paths joined through a symlink, are not — the loader resolves nothing
  and touches no filesystem (config-file-format §4). What a missed pair
  costs is worth naming precisely rather than calling bounded. Two
  targets aliased onto *one* directory leave it holding both packages,
  which does not compile. Two aliased onto a *nested* pair reproduce the
  permanent abort this check exists to prevent, because the parent's
  sweep finds a subdirectory it cannot prove marked. Neither is
  reachable without an alias the loader cannot see — an absolute path
  against a relative one, a symlink, or a case-insensitive filesystem —
  and neither deletes a hand-written file, which the tripwire still
  refuses to do.
- **"Generation target" displaces two existing uses of "target".**
  `classifyTarget`'s target is the config file being classified, and
  config-file-format §1 calls the output directory "the output target".
  Both are renamed as part of this change, and CONTEXT.md gains a
  glossary entry with the usual `_Avoid_` clause; a term this central
  cannot share a word with two neighbours.
- **`gqlc init` writes one target.** Editing a file that declares more
  than one is refused rather than served by a wizard that can express
  only one; appending is `init --add`.
- **A mid-commit I/O error still leaves a partial write.** The
  two-phase split moves every *check* before every mutation; it does
  not make N directory rewrites atomic, and atomic writes remain a
  non-goal.
