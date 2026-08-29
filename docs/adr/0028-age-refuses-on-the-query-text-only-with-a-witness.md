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
own body**; and that each recipe `ageLiveRecipes` names runs it —
**one** `go test` invocation that is itself built with `-tags codegen_live`,
selected WHOLE by every `-run` it carries and matched by no `-skip` it carries.
Every `go test` in such a recipe must also carry `-count=1`. That list is
hand-written rather than derived, and it is held by two guards, because a list
goes wrong in two directions and each guard catches one.
`witnessGaps` complains once per *entry* of the map `readRecipes` returns, not
once per name, so a name **dropped** is a recipe nobody checks: its length is
pinned in `TestEveryDialectGapCarriesItsWitness` (review mutation R1 — one name
left was green before that pin). A name **repeated** leaves that length
unchanged and collapses the map by one
entry, which the pin cannot see, so `recipeBodies` complains about it instead
(review mutation D1 — two names, one distinct — which survived the pin; and
D2CONSEQ, which gutted `test-codegen-live`'s `-run` down to a non-witness with
that duplicate in place and was complained about by nothing). The empty list
fails on `recipeBodies`' own vacuity complaint (R2). The list is not every live
recipe in the file and not what CI invokes:
the live neo4j recipe (`test-codegen-live-neo4j`) must *not* run these witnesses,
so a derived "every live recipe" rule would be false, and nothing here reads
`.github/workflows` to check that CI calls either name.

**One invocation, not one body.** Those flags were read from the whole recipe
body until round 4, while the `go test` was located separately, so a
`-tags codegen_live` on any *other* command satisfied the tag requirement for a
`go test` carrying none. Putting this justfile's own fence idiom
(`go vet -tags codegen_live ./...`) on the line above and dropping the tag off
the test restored review mutation T1 with one added line: measured, the recipe
then printed `[no test files]` for all 154 packages under `test/data/codegen`
and exited 0, starting no container and running no witness, while the sweep
stayed green (review mutations POOLTAG2 and POOLTAG). The `-count=1` check had
the same shape and two spellings of the same silence: the bytes `-count=1` left
standing inside an `-ldflags` value (CNT1), and a real `-count=1` on a different
`go test` in the same body (CNT2). `goTestInvocations` now cuts a body at the
boundaries a command actually ends on — a line, `&&`, `||`, `;`, `|` — and every
flag is read from the command that would run it.

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
which is the same defect one artefact out: an AGE recipe already carries a
`-skip`, so a witness name in the body is as likely to be there to remove the
test as to select it, and a name in a comment is not on the command line at all.
The three surviving shapes were: appending the witness name to the `-skip` the
AGE recipe already has, one token on one line; moving the name from `-run` to
`-skip`; and deleting it from an AGE recipe's `-run` while leaving it in a justfile
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

The approximations that remain, named with the direction each fails in. Within
one invocation every `-run` must select the witness, where `go test` honours only
the last: **complaint**. Any `-skip` alternative whose first element matches the
witness counts as skipping it, where `go test` would drop only a subtest:
**complaint**, and deliberately the same direction as the `-run` rule. Within one
invocation every `-tags` value must carry `codegen_live`, where `go test` honours
the last: **complaint**. The pattern split is naive where `go test`'s is
bracket-aware, and a pattern whose pieces do not compile is read as neither
selected nor safely skipped: **complaint**. An operator without spaces around it
(`a&&go test`) leaves no command starting at `go`, so the invocation is not seen
at all: **complaint**.

The ones known to fail toward **silence** are below. That list has grown in every
review round so far, each time because someone mutated a sentence claiming it was
already complete, so it is written as what has been found and not as what exists.

- **Expansion.** The reader is not a shell, so a recipe writing
  `SKIP='…|W' && go test … -skip "$SKIP"` hands it the literal `$SKIP`, which
  compiles as a regexp matching no test name; for `-skip` that reads as a witness
  which is not skipped (review mutation V4b). Closing it means being a shell.
- **The package argument** — see below (review mutation T2). It has two
  two-invocation spellings, both the same silence with a line in front, and both
  a `go test` carrying the tag and the right `-run` over `./valid/...`, which
  holds no live test: alongside an *untagged* one over `./...` (POOLRUN2), and
  alongside a tagged one over `./...` whose `-run` drops the witness (P2).
  Binding the flags to their own invocation does not reach either, because the
  invocation that satisfies every flag check is the one whose *path* runs
  nothing.

  P2 is a capability this round **lost**, and the loss is deliberate. Until the
  per-invocation rewrite, `recipeRuns` pooled the `-run` values of the whole
  body and required every one of them to select the witness, so a second
  `go test` whose `-run` dropped it refused the recipe. That refused P2 — for
  the wrong reason, since the same pooling refused a legitimate two-battery
  split, which is now the row "a second go test that runs it runs it". Measured
  both sides: at `00e2c456` P2 fails `TestEveryDialectGapCarriesItsWitness`;
  after the rewrite it passes. Recovering it means reading where `./valid/...`
  points, which is T2 itself and is left open, so it is written down here
  instead.
- **Whether a command line is reached.** `goTestInvocations` returns the `go
  test` commands a body *contains* in command position, which is a superset of
  what it runs, and the two callers take that in opposite directions. The
  `-count=1` rule is over EVERY invocation, so an unreached command only adds a
  requirement: complaint. `recipeRuns` is satisfied by SOME invocation, so an
  unreached command can answer for the body: silence. Measured — `go test -run
  'TestLiveSmoke' … || go test -run '<the full set>' …` in the live recipe keeps
  the sweep green while the witness invocation runs only on the first one's
  failure (P3). Adjacent and also unread: `… || true` appended to the real
  recipe (P4) leaves the witness running and the recipe gating on nothing, which
  costs gating rather than running and so is outside what "runs it" claims at
  all. `|| true` is one of this justfile's own idioms.
- **Shell this reader does not model**: backslash escapes, command substitution,
  heredocs and here-strings. Each can put a comment cut, or a command boundary,
  where `sh` would not, and what a wrong cut costs is a `-run` — of which the
  absence selects everything. This justfile uses all four, in recipes this reader
  never reads, so the bound is not the file; it is that `recipeBodies` reads only
  the recipes `ageLiveRecipes` names, and those use none of them.
  `TestTheRecipesThisReaderParsesStayInsideTheShellItModels` holds that, because
  it is true of the single-line recipes it names today rather than a law about
  justfiles.
  (Counts and line numbers for those constructs stood in this paragraph and in
  two comments until round 5. They were accurate and nothing checked them, so
  one edit above the first line number would have made three places wrong at
  once; the shapes are what the argument needs. The recipe count went the same
  way in round 6 — see the pin note below.)

**A check that finds nothing must not answer yes.** Every flag loop above is over
a slice that may be empty, and an empty slice of `-run` values genuinely does mean
"selects everything" — which made "there are no flags here" indistinguishable from
"the flags select the witness". Three mutations reached that state and stayed
green: a recipe body that shells out to a script and never invokes `go test` at
all (V1 — when this was written the justfile already had recipes that ran
`bash .githooks/tests/*.sh`; PR #1595 has since deleted them, so the shape V1
imitates is no longer present in the tree, bd `gqlc-u2nim`);
a quoted `#` in an `-ldflags` value, which the comment strip took as a comment and
which carried the `-run` away with it while leaving `-count=1` standing (V3); and
dropping `-tags codegen_live`, which compiles none of the live battery, so `go
test` prints `[no test files]` for every package under `test/data/codegen` (T1).
`recipeRuns` now reads flags only off a `go test` invocation and requires the
live tag on that invocation before it reads any pattern, and refuses a command
line whose quoting does not close; the comment strip cuts only at a `#` that
starts a word outside quotes. The `go test` has to be in command position —
starting a line or following `&&`, `||`, `;` or `|` — because the first version
of that check searched every argument and would have counted a witness "run" by
`echo go test -run W`.

Round 4 found the same question unanswered one level down: the loops did not find
nothing, they found a flag on a command that was not the test run, which is the
same "yes" from a check that was looking at the wrong thing. POOLTAG2, POOLTAG,
CNT1 and CNT2 are those, and their rows are in
`TestRecipeRunsOnlyWhatTheCommandLineSelects` and
`TestRecipeReaderComplainsOnEachBrokenRecipe` respectively. `-count=1` is
required of **every** `go test` in a live recipe rather than of some, because
`recipeBodies` is not told which witness matters and "some invocation is
uncached" would leave the one that runs the witness free to report on a cache.
A body running no `go test` at all is vacuously silent there, deliberately:
"this recipe runs no test" is `recipeRuns`' answer and the sweep asks it of every
gap.

Round 5 found it one level further out again, in the *tables* rather than in the
loops. Two lists carry claims nothing else re-states — `ageLiveRecipes`, and the
shell constructs `TestTheRecipesThisReaderParsesStayInsideTheShellItModels`
refuses in a recipe body — and neither was guarded against being shorter. Only
`ageLiveRecipes` had its empty case covered, on `recipeBodies`' vacuity
complaint (R2); the construct list was guarded by nothing at all, and the
argument standing where its guard belonged said a `require.Len` there "could not
be made to fail". All four construct rows deleted left this package green at
exit 0 with that test printing `--- PASS` over an assertion it no longer made
(review mutation A9), and deleting one row was the same silence one shape at a
time (A9b); one name dropped from `ageLiveRecipes` left a live recipe checked by
nobody (R1). Both counts are now pinned where the claim is made.

A count over a list catches a row that **leaves**. It does not catch a row that
is wrong, and it does not catch a row that is **duplicated**: a list holding one
name twice keeps its length and still loses a recipe from the
sweep, which is review mutation D1 against the round-5 pin. The two lists close
that direction in different places. The construct texts are counted as a *set*,
so four rows naming three shapes fails on the pin itself
(a `require.Len` over a map built from the texts, which a duplicate shortens;
measured as mutation CDUP). `ageLiveRecipes` is counted as a list, so its
duplicate is
caught one layer down, by `recipeBodies` — the reader every justfile consumer is
funnelled through by `readRecipes`, so D1 now reddens `TestEveryDialectGapCarriesItsWitness`
and `TestTheRecipesThisReaderParsesStayInsideTheShellItModels` together, and the
complaint is shown failing by a row of `TestRecipeReaderComplainsOnEachBrokenRecipe`
rather than assumed. Neither guard catches a row that is *wrong*. For
`ageLiveRecipes` the sweep catches that downstream anyway — a name no recipe in
the justfile carries is `recipeBodies`' missing-recipe complaint, and a name
whose recipe runs no witness is `witnessGaps`' own (measured as D2CTRL, which
gutted `test-codegen-live`'s `-run` with the list left alone). For the construct
list a row naming a shape the reader *does* model is caught by nothing, and is
left open.

**The list's length is written once, at the pin.** It was restated in prose
through this document and `dialect_test.go`, and mutation GROW —
`ageLiveRecipes` 2 names/2 distinct to 3 names/3 distinct, the pin literal moved
to 3, and a third justfile recipe running the same witnesses — left the package
green at exit 0 while those restatements went false, and nothing failed. That is
the shape `c3ca7f04` removed from this file when it deleted three copied justfile
line numbers and a copied count of `$(` occurrences: a number restated somewhere
the compiler cannot see has nothing that fails when it rots, so the second copy
is a defect on the day it is written. The cardinal is not load-bearing here in
any case — "two guards" and "two directions" are properties of the *shape* of a
list guard, invariant to how many names the list holds, and their agreeing with
the name count today is a coincidence rather than a link. The round-5 note above
records the same defect in the same paragraph that had been written to condemn
it, which is why the sweep for this one went wider than the pin's own sentences.
What remains, here and in `dialect_test.go`, is the arithmetic of the mutations
themselves — D1's "two names, one distinct", and GROW's before-and-after three
sentences above. Those are facts about experiments that were run, not
restatements of the list's cardinality, and they do not go false when the list
grows. That sweep was for the recipe cardinal and was not run over the construct
list's, which is why the paragraph above quoted the shell-shape pin complete with
its literal until round 8; the same rule applies to it and the literal is gone.

That sweep also turned up a claim that was not rotting but simply false, made
here and in two `dialect_test.go` comments: that **both** AGE recipes carry a
`-skip`. `test-codegen-live` carries none — only `test-codegen-live-age` does —
and the sentence had stood through six review rounds because a cardinal reads as
checked. One `-skip` is all the argument needs, since one is enough to make
`strings.Contains` over a recipe body unsound, so it is now written as "an AGE
recipe already carries a `-skip`". The complement grep found it; the universals
grep structurally could not.

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
is carried by **one** row: "a trailing comment does not carry `-count=1` either"
in `TestRecipeReaderComplainsOnEachBrokenRecipe`, which is the row that dies when
`stripRecipeComment` is made the identity (mutation G15). Its whole-line sibling,
"a `-count=1` only a comment spells", survives that mutation now and did not
before: a comment on its own line is no longer a command whose fields anyone
reads, because reading flags per invocation drops it before the comment strip
would have. Only a comment sharing a line with the `go test` still reaches the
flags.

**What the sweep did not check: that the answer is asserted.** As shipped it
required the recorded answer to appear in the witness's body, not to be the
subject of an assertion. Gutting `TestAGERefusesTheFunctionsItDoesNotDefine`'s
`require.Contains(t, pgErr.Message, tc.wantMessage, …)` to `_ = pgErr` left the
`wantMessage:` literals standing as real code, so the sweep stayed green over a
witness that ran five statements and asserted nothing about the answers (review
mutation M18). This was a weaker hole than M15 — it removes the assertion and
leaves the measurement, where M15 removed the measurement — and it was stated
here rather than fixed.

**CLOSED (bd gqlc-35yu.17).** The answer half of the sweep now reads
`assertedText` rather than the whole body: every assertion call's arguments,
plus the values bound to any name those arguments read. The prediction above —
that closing it needed the sweep to read the witness's *control flow*, a
different tool from the one this decision builds — was wrong, and wrong in the
direction that mattered, since it is what made the hole look expensive enough to
leave open. One hop over the same AST this decision already parses is enough,
because both shapes a witness here uses are name-mediated: a literal handed
straight to `require.Contains`, and a table column reached as `tc.wantMessage`.
A range variable is deliberately not resolved back to the table it ranges over —
that would pull every unasserted column back in and make the narrowing a slower
spelling of the body. `TestAnUnassertedAnswerReddensTheSweep` is M18 at the unit
level and `TestAssertedTextIsWhatAnAssertionReads` guards the reader itself.
What the narrowing costs: a witness asserting through the testify suite form or
a hand-rolled helper reports its answers unasserted, which is a false red.

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

Three gaps, each carrying its own refused probes and served texts — read off
`dialectGaps` itself rather than restated here, so adding a probe moves the
totals without touching this sentence. The alternation gap holds the
relationship-type alternation spellings `.14` measured; the undefined-function
gap holds the temporal constructor names a live session was measured on; the
spatial gap holds the one constructor a later live run measured. Each carries
served texts alongside. The numbers belong at the pin, not in prose.

1. **Relationship-type alternation** (`ErrRelationshipTypeAlternation`, from
   `gqlc-35yu.14`): `-[r:A|B]->` in any of three spellings, answered
   `syntax error at or near "|"`, SQLSTATE 42601.

2. **Undefined function** (`ErrUndefinedFunction`, this decision):
   `datetime()`, `date()`, `localdatetime()`, `duration({days:1})`,
   `toTimestamp('2024-01-01')`, `time()` and `localtime()`, each answered
   `function <name> does not exist` under SQLSTATE 42883. All but the last two
   were run by hand against
   `apache/age@sha256:4241e2d8…` (PostgreSQL 18.1, AGE 1.7.0) during the spike
   `gqlc-35yu.5`, and each probe is the byte sequence that session ran —
   `duration({days:1})` carries no space after the colon for that reason, and
   the refusal is of the *name*, so the argument spelling is not what is being
   measured.

   `time()` and `localtime()` came later, from `codegen-live` run
   `33268424367` against the same pinned digest (bd `gqlc-osf1`), which is also
   where the SQLSTATE above was first read: the spike captured the message and
   not the code, so until that run the live test pinned the message alone. With
   those two the set of temporal constructors openCypher spells is closed by
   measurement rather than left short on suspicion — and closing it is what put
   the carrier refusal out of reach of a bare constructor, which is the
   consequence recorded below.

   `timestamp()` is a **served** text from the same session; it returns epoch
   milliseconds as an integer. The other served text — `p.datetime`, the
   property lookup a scan for `datetime(` would have taken for a call — was
   **not** in that session, and neither were the alternation gap's two. They
   are first measured by their witness. The asymmetry is deliberate and only
   runs one way: a served text asserts the gate must *not* fire, so one that is
   wrong about the server reddens the live run, where a refused probe that is
   wrong would refuse an author with no way round it.

3. **Undefined spatial constructor** (`ErrUndefinedSpatialFunction`, bd
   `gqlc-l8e2n`): `point({x: 1, y: 2})`, answered `function point does not
   exist`, SQLSTATE 42883. Measured by the `live-smoke-age` job of `codegen-live`
   run `33268424367` — the same run that closed item 2's set, dispatched by bd
   `gqlc-osf1` against the same digest-pinned image. `p.point` is its served
   text, and it has no served *call*: nothing spatial on that image answers.

   It is a gap of its own rather than a name added to the one above, for the
   reason that bullet already gave — the temporal diagnostic's prose is false of
   a spatial name — and because the two remedies differ. A temporal refusal can
   send the author to a backend that models the type; this project models no
   spatial type on any target, so the spatial refusal names the only action that
   is certainly available: store the coordinates as ordinary properties and
   compute the geometry in Go. A sentinel here is chosen by the fix, not by the
   SQLSTATE, which is why one 42883 answers to two.

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

## Measured but not refused

The list that stood here was of constructs suspected and never run. All of them
have now been run, on `codegen-live` run `33268424367` against the pinned digest
(bd `gqlc-osf1`), and two of them were refused by the server while still being
refused by no gap here. `point({x: 1, y: 2})` was one, and is one no longer: it
is item 3 above as of bd `gqlc-l8e2n`, a gap of its own for the reason this
section gave. One is left, and it is not an oversight:

- **`duration.between(null, null)`.** Refused under a different error *class*:
  SQLSTATE 3F000, `schema "duration" does not exist`. Postgres reads the
  openCypher namespace as a schema qualifier and fails on the qualifier before
  looking for a function, so the answer names no function at all — which is
  precisely what the gap's own guard
  (`TestEveryRefusedFunctionNameIsNamedByItsProbeAnswer`) requires of a row in
  it. It cannot join this gap even with a namespaced scanner, and needs its own:
  bd `gqlc-dy40s`.

It is held to that answer by `TestAGEAnswersTheConstructsNoGapRefuses`, so the
bead inherits a witness rather than a suspicion, and the pinned image changing
its answer reds the nightly rather than surfacing when someone finally builds
the gap.

The AGE live half remains nightly-and-manual (`codegen-live.yml`, job
`live-smoke-age`, skipped on pull requests), so a refusal here ships verified by
a dispatched run rather than by the pull request that adds it.
`codegen-live.yml` accepts `workflow_dispatch`, which is what lets the arm be
witnessed off the nightly clock at all; Docker being unavailable locally is a
limit on iteration, not on witnessing. The lag is one cycle, against an image
pinned by digest that no pull request can alter except by editing that digest.

### What closing the temporal set cost

`localtime()` being unwitnessed used to be load-bearing: the corpus's
unrepresentable-temporal fixture and `TestTemporalProjectionNamesThisBackend`
both called it, precisely because an unwitnessed constructor reaches the carrier
question this gate would otherwise answer first. Giving it a witness moved both,
and the move is worth recording because the room it left is small.

Every route to a temporal column runs through a temporal constructor
(`internal/query/cypher/shape.go §temporalConstructorType`; an aggregate over a
temporal *property* types as unknown at that layer, so it does not reach one).
With the bare set closed, the only spelling that still reaches
`codegen.ErrUnrepresentableTemporal` on this backend is the namespaced
`duration.between`. Both moved onto it, and
`test/data/codegen/invalid/unrepresentable_temporal_duration_column` is now the
only fixture in the corpus naming that sentinel — which
`TestSentinelReachability` requires one of.

So building `gqlc-dy40s` closes the last route, and whoever takes it owes that
question an answer before deleting the fixture: a sentinel this backend can no
longer reach from any query text is either a sentinel to retire or a gate whose
ordering should change.

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
  same argument `.14` made: against the pinned image (AGE 1.7.0 on PostgreSQL
  18.1, `apache/age@sha256:4241e2d8…`) the probe `RETURN date()` was answered
  `function date does not exist`, so a projection of `date()` does not parse
  and the text has to be rewritten before any column question can be put to it.
  A later AGE that grows a `date` function would move that measurement, and the
  reordering would need re-examining on that image; the argument does not
  extend past what this repo can witness. It is pinned by the answer it
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
