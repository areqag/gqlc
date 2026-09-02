# An undeclared relationship type is warned about, not refused

`MATCH (:Person)-[r:AUTHORED|LIKES|FLAGGED]->(p:Post)` against a schema that
declares AUTHORED and LIKES but not FLAGGED now prints, on stderr:

    warning: relationship type "FLAGGED" is not declared in the schema; edge "r"
    was narrowed to its declared types and no decoder is generated for
    "FLAGGED", so a row of that type fails at runtime. Fix the spelling if it is
    misspelled, or declare it if the graph has drifted ahead of the schema.

The generate still succeeds and still writes the same code it wrote before.
Previously it said nothing at all.

This is ADR 0030's posture applied to a second construct: when the compiler
cannot honour what the author declared, it must say so rather than quietly
narrow. It is `gqlc-1dmu`.

## What was silent

The resolver narrows an edge binding's candidate set to the probes the schema
declares (`edgeCandidates`, ADR 0005/0022), and leaves the query TEXT verbatim.
So a relationship type no declaration mentions is dropped from the candidate
set, no decoder arm is emitted for it, and the server is still asked for it.
Nothing anywhere reported the drop.

Two very different situations produce that shape, and gqlc cannot tell them
apart:

- **Drift.** The graph grew a relationship type ahead of the GQL schema. The
  sealed interface correctly has no member for it, and a row of that type fails
  at runtime with a clear error rather than decoding into the wrong arm. This is
  the `edge_union_undeclared_relationship_type` fixture, and it is the only
  fixture whose emitted `default:` arm a live server can reach.
- **A typo.** The author wrote `ACTED_INN`. There is no runtime failure to read,
  because no row of that type will ever arrive; the query simply matches less
  than the author believes.

## Decision

Warn. Do not refuse.

`ValidatedQuery` gains a `Warnings []string` lane; `resolve` populates it on the
success path only; `pipeline.Result` gains a `Warnings` lane carrying them out
with the same `graph[i]: <path>: query <Name>: ` prefix Diagnostics carries; the
CLI prints them to stderr before the diagnostics and does not count them in the
summary error.

### Why not refuse, given ADR 0030

ADR 0030 rejects because a decided precedence **can be wrong silently** and a
rejection cannot. That asymmetry is not present here.

A refusal here would be wrong LOUDLY and unrecoverably for the drift case: an
author whose graph legitimately carries a type their schema has not caught up
with would have no way to compile the query at all, short of editing a schema
they may not own. gqlc's own corpus depends on that case — refusing deletes the
only live reach into the edge-union `default:` arm.

And unlike ADR 0030's construct, the narrowing here is not a discarded
declaration whose loss is invisible. The generated code is well-typed over the
declared types either way; the question is purely whether the author is told.
Telling them is the whole fix, and it costs the drift case nothing.

The posture ADR 0030 states — silent-wrong is worse than loud-wrong — is
satisfied by making it loud. Refusal is a stronger remedy than the defect needs.

### Why "declared nowhere" and not "not in the candidate set"

The detector fires only on a relationship type that appears in NO edge
declaration in the schema. A type that is declared but is dropped from THIS
edge's candidate set by its endpoints does not warn.

`(:Person)-[r:AUTHORED|REPORTED]->(:Post)` with `REPORTED` declared as
`(:Post)-[:REPORTED]->(:Person)` loses REPORTED to endpoint narrowing. That is
ADR 0022 working: an author who spells an alternation to cover both directions
wants exactly that, and it happens on ordinary correct queries. Warning there
would fire constantly and bury the signal this exists for. `gqlc-he8v` records
the endpoint-narrowing question as a separate one, deliberately not answered
here.

ADR 0038 has since answered it, and moved this boundary a little: the narrowed
case is still not this detector's, but a drop whose type is declared ONLY in the
reversed orientation — and which survives nowhere else in the query — now warns
in a second lane. The broad "not in the candidate set" test stated above remains
rejected, by argument here and by measurement there.

`TestWrongOrientationDropIsWarnedAbout` is the row that holds the line, and it
replaced `TestEndpointNarrowedButDeclaredTypeIsSilent` when the boundary moved.
Broadening THIS detector to candidate-set membership still turns it red, because
it asserts the drop is reported by the wrong-orientation producer and not by this
one.

### Why warnings are not diagnostics

`pipeline.Diagnostics` is fatal-accumulation: every entry appended to it
discards every target's batch (§6.2) and fails the run. A warning routed there
would refuse the compile, which is the decision this ADR declines to make. So
`Warnings` is a second, parallel lane rather than a severity on the first.

It is deliberately small. There is no severity enum, no source position and no
suppression flag. A general diagnostics framework can be built when there is a
second caller to generalise over.

At this ADR the lane was one lane of strings with one producer. ADR 0038 added
the second, and the ruling below fired: the lane is now `[]Warning` where
`Warning` is `{Producer, Text string}`, and no larger.

Two properties of the lane that are asserted rather than assumed:

- **Warnings ride out on the failing branch too.** The all-or-nothing rule is
  about batches, not advice, and a misspelled relationship type is a plausible
  cause of the refusal printed under it. `TestRunKeepsWarningsAlongsideDiagnostics`.
- **Warnings are not counted by the summary error.** `generate: 1 error`, not 2.
  Folding them in would make the exit status report a failure that did not
  happen. `TestGenerateWarningsPrintBeforeDiagnosticsAndAreNotCounted`.

### Why the resolver, and only on success

The narrowing is the resolver's, so the report is the resolver's. It runs at
`resolve`'s final return, so a query that fails resolution gets one verdict — the
error — and never both an error and a warning about the same edge. An edge whose
every named type is undeclared has an empty candidate set and is already refused
with `ErrUnknownEdge`; `TestWhollyUndeclaredEdgeIsRefusedNotWarned` pins that it
does not also warn.

Deduplication is query-wide by type name, in first-appearance order: one
misspelling repeated across three MATCH clauses is one mistake.

## Consequences

- `ValidatedQuery.Warnings` is `json:"warnings,omitempty"`. Every corpus golden
  of a query that warns about nothing is byte-identical; 12798 of the resolver
  sweep's 12880 cells did not move.
- 82 sweep cells did move — the cross-product cells where a query is resolved
  against a foreign schema that does not declare its relationship types. That is
  the manifest recording, per cell, that the compiler now says something it did
  not say. Regenerated with the sanctioned `-update` run.
- `test/data/codegen/valid/edge_union_undeclared_relationship_type` is unchanged
  and still enrolled: same goldens, same `default:` arm, same live reach. Only
  the stderr of a generate that produces it is different.
- `runTarget` and `frontEndWalk` grow a third return. No behaviour on the
  existing two moved; the `Targets`/`Diagnostics` invariant in the package doc is
  untouched and `TestRunKeepsWarningsAlongsideDiagnostics` re-asserts it.

## What would change this

- **If the warning proves noisy in practice** — the likeliest source being a
  project that deliberately queries ahead of its schema in many places — the
  next step is a per-project suppression, not a narrower detector. The detector
  is already at its narrowest defensible setting.
- **If a second warning producer appears**, the string lane should become a
  small struct with a producer tag before it acquires a third.

## Provenance

Decided and implemented under `gqlc-1dmu`, which had been deferred once on the
grounds that no non-fatal channel existed. It did not; this ADR builds the
smallest one that carries this warning and nothing else.
