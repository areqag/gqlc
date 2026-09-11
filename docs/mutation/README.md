# The mutation screen, and the one place it does not hold

A mutation matrix is only as good as its screen — the check that tells a
genuine kill from a mutant that merely stopped compiling. This repo's
standard screen is

    go test -c -o /dev/null <pkg>

run before the mutant's test run. `go build` is not a substitute: it does
not compile `_test.go` files, so a mutation that breaks only a test file
passes `go build` and reddens the run for the wrong reason.

## Where the screen is unsound

**The screen does not see emitted code.** Anything under
`internal/codegen/*/testdata/*.go.txt`, and anything a generator writes
out as a string, is compiled *one module down* — in a child module the
parent test assembles in a `TempDir` and runs with its own `go test`.
`go test -c` compiles the **generator**; it never compiles the
**emission**.

So for any mutation whose effect lands in emitted code, the screen
returns rc=0 whatever happened. It cannot distinguish a mutant that broke
the emitted package from one that changed its behaviour from one that did
nothing at all.

The trap is sharpest when the mutation is inside a Go string literal in
the parent — `render_models.go` writing `"\tif err != nil {\n"`. That
line is parent source, so the instinct is that the parent's compiler
guards it. It does not: the literal compiles no matter what it says.

## Measured

Against `internal/codegen/neo4j`, one mutation at a time, restored
between runs. Both mutants are in the emission, one breaking it and one
merely changing its behaviour:

| mutation | `go test -c` | real run | `[build failed]` | child test names |
|---|---|---|---|---|
| none (baseline) | rc=0 | ok | 0 | 21 `--- PASS` |
| `render_models.go`: emitted gate → `if linusUndefinedSymbol != nil {` | **rc=0** | rc=1 | 1 | **0** |
| `render_queries.go`: emitted pointer gate `!=` → `==` | **rc=0** | rc=1 | 0 | 1 `--- FAIL`, 20 `--- PASS` |

The screen reads rc=0 on all three. It is blind to the difference between
a broken emission and a correct one, which is the entire job it was
brought in to do.

## The sound screen for emitted code

Run the outer harness and read the **child** log:

    go test -count=1 -run '^TestEmittedDecodersRunOnDriverValues$' ./internal/codegen/neo4j

and assert all four:

1. `[build failed]` is **absent** — and `[setup failed]` too. A fixture
   that stops parsing prints `setup`, not `build`; asserting only one
   spelling misses the other.
2. At least one `--- FAIL:` **naming a child test**, with sibling child
   tests still `--- PASS`. That pairing is the tell: the table above
   shows a child compile break printing `[build failed]` with *zero*
   child names, where a genuine kill names `TestAnythingColumnsCarryTheirNull`
   and leaves 20 siblings passing.
3. `no tests to run` count is 0. A `_test.go` declaring no tests exits 0.
4. Anchor the `-run` regex: `-run 'TestFoo/sub'` matches `TestFooMapping`
   by prefix. Write `^TestFoo$/^sub$`.

Anchor sibling-PASS greps on `^[[:space:]]*--- PASS`, never `^ *--- PASS`
— the child log is indented with tabs, and the space-only class reports 0
where the truth is 21.

## Why the matrices built on the bad screen were not wrong anyway

Worth recording so nobody re-audits them from scratch:

- **SURVIVED rows are self-screening.** The harness does `require.NoError`
  on the child `go test` exit status and then compares the pass set
  against the declared tests. A green package entails the child compiled
  and every declared test ran and passed, so a compile break cannot
  masquerade as SURVIVED.
- **KILLED rows need the child-name tell**, and it discriminates cleanly,
  per the table above.

So the screen's blindness costs a matrix nothing on the SURVIVED side and
everything on the KILLED side, where a row can read KILLED because the
artifact stopped parsing.

## Publishing a row so a later reader can re-run it

A published row is evidence only to the extent someone else can reproduce
it. Two things decide that, and both are about how the row is *written*.

**Publish the mutation as a diff, not as a description.** A row that says
"replace the early-out with a panic" leaves the next reader guessing at the
bytes. A row that carries the hunk leaves nothing to guess. Where a diff is
too long to sit in the table, put it beside the table and have the row point
at it.

**An `md5 after` column pins movement, not identity.** That column is
author-self-certified by construction — it is a digest of bytes only the
author saw — so the reproducible claim it makes is `changed` versus `no-op`,
which is the question it was brought in to answer. The digits are not
reproducible from the row's own prose.

Measured (PR #963, round 3): a reviewer re-ran two inherited rows. MR-2,
deleting a named early-out, reproduced to the digit at `b97b97d7`, because
deleting a named block leaves no wording choice. MR-2ctl, replacing the same
site with a panic, reproduced the *behaviour* exactly — 268 panics, 275 FAIL,
the same as the original run — and hashed differently, `027faa18` against
`276fafeb`, purely because the two authors wrote different panic text.

So a reader comparing digits across two honest runs of the same row can read
a mismatch as a contradiction when it is only a difference in wording. Either
publish the literal diff, or say on the table that the hash pins movement
rather than identity. The second costs one sentence and is what the column
means either way.

This is a writing convention and no gate holds it: a gate for it would have
to grade prose. What falsifies a matrix written this way is a reader who
cannot reconstruct the mutated bytes from the row — if that happens, the row
needed its diff.
