# Apache AGE refuses a construct on the query text, and only with a witness

The `apache-age-pgx-v5` target refuses a batch at generate time when a query's
**text** spells something the pinned AGE image will not accept. Which constructs
those are is decided by a table in which every entry carries a probe — the exact
cypher a live session ran — and the answer the server gave. A sweep refuses an
entry that has neither.

The second half is the decision. The first half was already made and is not
re-argued here.

## Context

Generated code executes the author's original query text verbatim (ADR 0005).
The emitter never rewrites it, so a construct the server's parser or its
function catalogue rejects produces a package that compiles, passes the compile
fence, and fails on **every call** with a server-side error. The author finds
out in production.

Generate-time refusal is this codebase's posture for *this backend cannot
represent that*, stated twice in the record: ADR 0025 rejected a shape because
"it emits a column no decoder can fill at exit 0", and ADR 0027 rejected
per-entity multi-label admission on that same precedent. `gqlc-35yu.14` applied
it to the query text for the first time, refusing the relationship-type
alternation AGE's parser has no production for.

What `.14` left open is what else belongs there. AGE 1.7.0 has no native
temporal type — `agtype`'s value enum has none, `pg_cast` holds no cast to or
from one in either direction, and of the 348 functions in `ag_catalog` exactly
one has a temporal name. So `datetime()`, `date()`, `duration()` and their
siblings look like obvious candidates, and writing them all down would take a
minute.

That minute is the trap. **The hazard here is asymmetric.** A refusal that is
missing costs the author a runtime error they were going to get anyway. A
refusal that is *wrong* costs them a query that would have worked — and ADR 0005
means there is no rewrite, no escape hatch and no flag. A list of names someone
believed is this epic's own recurring defect wearing a different hat: a guard
that is green because nothing it looks at can contradict it.

## Decision

`internal/codegen/age/dialect.go` holds `dialectGaps`, a table whose entries
each carry:

- a **sentinel**, so a caller branches with `errors.Is` on the specific gap;
- a **find**, which reads the offending spellings out of a query text by
  parsing it (`internal/query/cypher`) rather than scanning for characters;
- a **diagnose**, the prose the author meets, which differs per gap because the
  way out differs;
- a **witness**, naming the live test that re-measures the gap;
- **refused probes**, each a query text and the substring of the server's answer;
- **served texts**, which the same session accepted and `find` must stay silent
  on.

`rejectDialectGaps` runs the table ahead of `codegen.Prepare` and reports the
first gap that fires. `rejectUnservedQueries` yields to the whole table rather
than to one construct, so a gap added inherits the yield.

**A name cannot enter the refusal list without a probe.** The undefined-function
gap does not hold a list of names at all: `undefinedFunctions` is *derived* by
parsing the probe texts for the calls they make. There is no literal for a name
to be appended to.

**A probe cannot enter without a live test.**
`TestEveryDialectGapCarriesItsWitness` requires, for every gap: at least one
probe; that `find` reads each probe (so the measurement is of something the gate
actually refuses); that `find` reads none of the served texts (so the gate is
bounded, which is the half that matters given the asymmetry above); that the
named witness is declared in a `live_*_test.go` file; that every probe text,
every recorded answer and every served text appears verbatim in **that witness's
own body**; and that every AGE live recipe in the justfile runs it — a `go test`
invocation, built with `-tags codegen_live`, selected WHOLE by every `-run` and
matched by no `-skip` — with `-count=1`.

"In the body" means in the body's **code**. The reader parses each live file and
renders every test's body back from the tree with `format.Node`, so a comment is
not part of any body. Taking the body as the source bytes between `fn.Body.Pos()`
and `fn.Body.End()` — which is what it did until review mutation M15 — hands the
comments back too, and a probe row commented out then satisfied the sweep while
measuring nothing. That is exactly the growth-on-suspicion this decision exists
to prevent, so it is guarded twice: `TestWitnessBodyIsCodeAndNotCommentary` on
the reader (line comments, a block comment, a doc comment, and a positive
control that real code IS carried) and `TestACommentedProbeReddensTheSweep` on
the reader composed into the sweep, which is the only place the property is
visible — `witnessGaps` takes a bodies map, and a map value carrying a comment is
just a string carrying a comment.

"Runs it" means the recipe's command line **selects** it. That check was
`strings.Contains` over the recipe body until review mutations L18, L19 and L20,
which is the same defect one artefact out: both AGE recipes already carry a
`-skip`, so a witness name in the body is as likely to be there to remove the
test as to select it, and a name in a comment is not on the command line at all.
The three surviving shapes were: appending the witness name to the `-skip` the
AGE recipe already has, one token on one line; moving the name from `-run` to
`-skip`; and deleting it from both recipes' `-run` while leaving it in a justfile
comment — M15's move on the artefact the `format.Node` fix does not reach.
`recipeBodies` now strips shell comments as it reads a recipe body, and
`recipeRuns` parses the `-run`, `-skip` and `-tags` values as separate patterns.

Selection is a **regexp** question and not string equality. `go test` splits a
pattern on top-level `|` into alternatives first and each alternative on `/` into
elements, then matches element *i* against the *i*'th part of a test's name
unanchored (`testing/match.go`: `splitRegexp`, `alternationMatch.matches`). So
`-run 'TestAGERefuses'` does reach the witness, and appending `|W` to
`-skip 'TestLiveSmoke/neo4j'` drops `W` outright — the appended text is a second
alternative, not a second element. Measured against go1.26.5 rather than read off
the flag documentation, because the two readings differ and the wrong one makes
L18 look harmless.

**A `-run` has to select the witness WHOLE.** `go test` reads the elements after
the first as a subtest filter, and both AGE witnesses run every probe row as a
subtest, so `-run '…|TestAGERefusesTheFunctionsItDoesNotDefine/toTimestamp'` runs
one probe of five and `-run '…/x'` runs none at all. Both print `--- PASS` for
the top-level and exit 0, and in the second case `[no tests to run]` does not
appear either, because the smoke battery in the first alternative did run
something. This was the shape of review
mutations MA and MB, and the first version of `recipeRuns` accepted both while
refusing the strictly smaller loss expressed as a `-skip` carve-out (MC). So an
alternative counts as selecting the witness only when it carries no further
elements. Both AGE recipes' `-run` alternatives are bare names, so the rule costs
nothing here.

The approximations that remain, named with the direction each fails in. Every
`-run` must select the witness, where `go test` honours only the last:
**complaint**. Any `-skip` alternative whose first element matches the witness
counts as skipping it, where `go test` would drop only a subtest: **complaint**,
and deliberately the same direction as the `-run` rule. Every `-tags` value must
carry `codegen_live`, where `go test` honours the last: **complaint**. The
pattern split is naive where `go test`'s is bracket-aware, and a pattern whose
pieces do not compile is read as neither selected nor safely skipped:
**complaint**. The one that fails toward **silence** is expansion — the reader is
not a shell, so a recipe writing `SKIP='…|W' && go test … -skip "$SKIP"` hands it
the literal `$SKIP`, which compiles as a regexp matching no test name; for `-run`
that reads as a witness which does not run, and for `-skip` it reads as a witness
which is not skipped (review mutation V4b). Closing that means being a shell.

**A check that finds nothing must not answer yes.** Every flag loop above is over
a slice that may be empty, and an empty slice of `-run` values genuinely does mean
"selects everything" — which made "there are no flags here" indistinguishable from
"the flags select the witness". Three mutations reached that state and stayed
green: a recipe body that shells out to a script and never invokes `go test` at
all (V1 — this justfile already has recipes that run `bash .githooks/tests/*.sh`);
a quoted `#` in an `-ldflags` value, which the comment strip took as a comment and
which carried the `-run` away with it while leaving `-count=1` standing (V3); and
dropping `-tags codegen_live`, which compiles none of the live battery, so `go
test` prints `[no test files]` for every package under `test/data/codegen` (T1).
`recipeRuns` now requires a `go test` invocation and the live tag before it reads
any pattern, and refuses a command line whose quoting does not close; the comment
strip cuts only at a `#` that starts a word outside quotes.

**What the sweep still does not check: where the package argument points.** It
reads the command line and nothing else, so the `cd test/data/codegen` in front
of it and the `./...` after it are unexamined. Narrowing the path to `./valid/...`
leaves the `-run` selecting a test that is no longer in the package set, and every
complaint here stays silent (review mutation T2) — the same class as L18, one
argument further along, and left open rather than closed. It is left open because
it is not a question about flags: answering it means resolving a package pattern
against the file system, relative to a directory named by a shell builtin.

`TestRecipeRunsOnlyWhatTheCommandLineSelects` drives the reader and the selector
composed, over justfile source the test writes, with a row for each surviving
mutation above and for both directions of the unanchored match. L18 and L19 have
their own rows there. **L20 does not**, despite two rows that were named for it
until round 3: a witness named only in a comment introduces no `-run`, so those
rows hold whether comments are stripped or not, and they are really the `-run`
half. The comment property itself — text that is spelled is not text that runs —
is carried by the two `-count=1` rows of
`TestRecipeReaderComplainsOnEachBrokenRecipe`, which are the rows that die when
`stripRecipeComment` is made the identity.

**What the sweep does not check: that the answer is asserted.** It requires the
recorded answer to appear in the witness's body, not to be the subject of an
assertion. Gutting `TestAGERefusesTheFunctionsItDoesNotDefine`'s
`require.Contains(t, pgErr.Message, tc.wantMessage, …)` to `_ = pgErr` leaves the
`wantMessage:` literals standing as real code, so the sweep stays green over a
witness that runs five statements and asserts nothing about the answers (review
mutation M18, reproduced and still open at the time of writing). This is a
weaker hole than M15 — it removes the assertion and leaves the measurement, where
M15 removed the measurement — and closing it needs the sweep to read the
witness's control flow rather than its text, which is a different tool from the
one this decision builds. It is stated here rather than fixed.

Neither hole is inherited. `f4fb1a19`, the commit this work branches from, has no
`internal/codegen/age/dialect.go` and no witness sweep anywhere in the tree
(`git grep -l 'witnessGaps\|readLiveWitnessBodies' f4fb1a19 -- '*.go'` finds
nothing). The binding is introduced entirely here, so a gap in it is this
decision's own; what is true is only the weaker thing, that master bound nothing
and therefore nothing regresses.

The per-witness scoping is the difference between a probe that is re-measured
and a probe that is merely spelled somewhere. A text carried by the neo4j
battery is run against neo4j; a text under an AGE test the `-run` allowlists do
not name is run against nothing. A sweep reading every live file at once tells
neither from the real thing, and the recipe check does not cover for it, because
that check reads the witness a gap *declares* rather than where its text sits.

**The sweep can fail, and that is tested.** `witnessGaps` is a pure function
returning complaints, and `TestWitnessSweepFailsOnEachBrokenBinding` cuts each
binding in turn against a template it first asserts passes, requiring the
specific complaint back — including the row that hands it an empty table,
because every other complaint is raised inside the loop and a loop over nothing
runs no body.

## What is refused today

Two gaps carrying eight refused probes and four served texts — twelve query
texts, counted off `dialectGaps` itself: three refused and two served for the
alternation, five refused and two served for the undefined function.

1. **Relationship-type alternation** (`ErrRelationshipTypeAlternation`, from
   `gqlc-35yu.14`): `-[r:A|B]->` in any of three spellings, answered
   `syntax error at or near "|"`, SQLSTATE 42601.

2. **Undefined function** (`ErrUndefinedFunction`, this decision):
   `datetime()`, `date()`, `localdatetime()`, `duration({days:1})` and
   `toTimestamp('2024-01-01')`, each answered `function <name> does not exist`.
   Every one was run by hand against
   `apache/age@sha256:4241e2d8…` (PostgreSQL 18.1, AGE 1.7.0) during the spike
   `gqlc-35yu.5`, and each probe is the byte sequence that session ran —
   `duration({days:1})` carries no space after the colon for that reason, and
   the refusal is of the *name*, so the argument spelling is not what is being
   measured.

   `timestamp()` is a **served** text from the same session; it returns epoch
   milliseconds as an integer. The other served text — `p.datetime`, the
   property lookup a scan for `datetime(` would have taken for a call — was
   **not** in that session, and neither were the alternation gap's two. They
   are first measured by their witness. The asymmetry is deliberate and only
   runs one way: a served text asserts the gate must *not* fire, so one that is
   wrong about the server reddens the live run, where a refused probe that is
   wrong would refuse an author with no way round it.

Matching is case-insensitive, which is what openCypher function resolution is;
the name is quoted back in the author's own case, because that is what they have
to find in their file. A namespaced call is a different name
(`Cypher.g4 §oC_FunctionName`) and is not refused.

That last claim has two spellings and only one of them can fail, which is worth
writing down because the obvious one is the safe one. Drop the namespace guard
and `duration.between` reports `between`, a name no probe put in the catalogue,
so nothing is refused either way. `com.example.datetime()` reports `datetime`,
which is in it — and the author is refused a call to a function they defined, on
the strength of a probe that measured a different name. Both spellings are
pinned, at the unit level and at the CLI seam.

## Suspected and unverified

Everything below is suspected on the same grounds as the five refusals above and
**has no witness**, so none of it is refused. Docker is not available to the
author of this decision; verifying any of it needs the `live-smoke-age` CI job.
That work is bd `gqlc-osf1`.

- **`time()`, `localtime()` and `point(…)`.** Never run against any AGE image by
  anyone in this repo's record. `time()` and `localtime()` are almost certainly
  undefined; `point()` is a separate question, since it is not temporal and the
  present diagnostic's prose ("defines no temporal constructor at all") would be
  false of it — it would be a third gap, not a name added to the second.

- **`duration.between(a, b)`.** Namespaced, so a different name from `duration`
  and not covered by that probe. `cypher.UnqualifiedFunctionCalls` drops
  namespaced calls by design, so refusing it needs a second scanner as well as a
  witness.

- **The SQLSTATE of every undefined-function refusal.** The spike recorded the
  server's *message* and not its code, so the live test asserts the message
  alone — unlike the alternation gap, which pins 42601. PostgreSQL's
  `undefined_function` is 42883 and AGE plausibly reports through the same
  channel; plausibly is not measured.

- **The first run of `TestAGERefusesTheFunctionsItDoesNotDefine`.** The AGE live
  half is nightly-and-manual (`codegen-live.yml`, job `live-smoke-age`, skipped
  on pull requests), so these five refusals ship verified by a hand-run spike and
  not yet by CI. The lag is one cycle and the subject is an image pinned by
  digest, which no pull request can alter except by editing that digest.

`localtime()` being unwitnessed is load-bearing, not incidental. It is what
`test/data/codegen/invalid/unrepresentable_temporal_localtime_column` and
`TestTemporalProjectionNamesThisBackend` now use, precisely because an
unwitnessed constructor reaches the carrier question this gate would otherwise
answer first. Give `localtime()` a witness and both have to move.

## Considered options

**Document the gap and refuse nothing.** Rejected against the record: the
posture for "this backend cannot represent that" is generate-time refusal (ADR
0025, ADR 0027), and a comment does not stop a shipped package whose every call
fails.

**Write down every temporal constructor openCypher defines.** Rejected. It is
the guess this decision exists to prevent, and it would have refused
`timestamp()` — the one call that works — on the same reasoning that refuses
`datetime()`.

**Scan the query text for `datetime(`.** Rejected on the same grounds `.14`
rejected scanning for `|`: a property lookup (`p.datetime`), a label
(`(d:date)`), a variable, a string literal and a comment all spell the name, and
a *procedure* invocation spells the name and the parenthesis both while being
`oC_ExplicitProcedureInvocation`, resolved against a different catalogue. The
grammar knows the difference and this repo has a parser.

**Answer through the query model instead of the text.** Impossible, and this is
the load-bearing reason the check is where it is. Predicate structure is dropped
from the model by design (ADR 0003), so `WHERE p.at < datetime()` leaves no
column, parameter or binding carrying the call; a write clause projects nothing
and still ships its whole text; and a call the resolver types is typed by its
*result*, which says nothing about whether the server has the function.

**Put the gap table in the fixture corpus, next to the live tests.**
Rejected: `test/data/codegen` is a separate Go module whose purpose is proving
generated code compiles standalone, and giving it a dependency on
`internal/codegen/age` inverts that. The binding is made source-level instead —
the sweep parses the live files with `go/parser` and renders each test's body
back from the tree, which is what the two sides have to agree on anyway. Parsed
rather than read as bytes for the reason above, and parsed rather than scanned
for `func <name>(` for the same reason the gate it audits parses: a scan cannot
tell a declaration from a string literal spelling one, and it can find a body's
end only by assuming what the formatter puts in column zero. `go/parser` reads a
build-tagged file without honouring the tag, which is what lets a binary built
without `codegen_live` read one.

**Refuse the whole batch, or only the offending query.** The batch, as `.14`
does: a generated package accounts for every query in its batch, and one with no
valid emission is one it cannot account for.

## Consequences

- A projected `date()` is now answered by this gate rather than by
  `codegen.ErrUnrepresentableTemporal`. That reordering is deliberate and is the
  same argument `.14` made: no projection of `date()` will ever parse on this
  server, whatever carrier AGE later grows, so the text has to be rewritten
  before any column question can be put to it. It is pinned by the answer it
  produces, not left to the reading order of `generate.go`.

- `test/data/codegen/invalid/unrepresentable_temporal_date_column` is renamed to
  `..._localtime_column` and its query switched to `localtime()`, because
  `date()` no longer reaches the sentinel the manifest names.

- The refusal list is **short on purpose** and will stay short until someone
  runs a container. That is the intended failure mode: this table under-refuses
  rather than over-refuses, and the sweep is what keeps it that way.

- Adding a construct costs a live measurement. This is a real friction and it is
  the point. The alternative is a table that grows on suspicion, which is a
  guess with a test suite around it.
