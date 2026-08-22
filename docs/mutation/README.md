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
