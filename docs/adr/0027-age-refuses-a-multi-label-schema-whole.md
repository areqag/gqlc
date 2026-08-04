# Apache AGE refuses a multi-label schema whole, not per entity

A schema that keys a node or edge type on more than one label has no
representation on the `apache-age-pgx-v5` target. The backend refuses the
**whole batch** at generate time — every query in it, including the queries
that project only types it represents perfectly well — rather than omitting the
type, or emitting it and narrowing the refusal to the queries that project it.

The diagnostic carries the reasoning, because a generate-time error is where an
author meets this decision and is the only place they are guaranteed to look.

## Context

`agtype` renders a vertex or an edge with a single `label` field, and AGE's
Cypher parser has no production for a second: `CREATE (x:A:B {n: 1})` is a
*syntax error*, not a value AGE evaluates and rejects. Verified against
apache/age 1.7.0 (bd `gqlc-35yu.15`).

So a type keyed on two labels describes an element no graph reachable through
this backend ever holds. Nothing can write one, and no value matching the type
can arrive to be decoded.

That much was already true and already refused. What was open is the **scope**
of the refusal, and whether the reason for it lived anywhere an author would
find it. The sibling bead `gqlc-35yu.13` was expected to build a per-entity
admission in the shared prepare phase that this decision would consume; it took
the other route — it *served* the LIST and ANY property widths it had been
refusing — so no such mechanism exists and none is proposed here.

The asymmetry with `.13` is the whole point: AGE can genuinely store a list, so
there was something to serve. AGE's parser has no syntax for a second label, so
there is nothing to serve. ADR 0005 closes the remaining escape — generated code
executes the author's original query text verbatim, so the emitter cannot
quietly rewrite `:Person:Employee` into a synthesised composite label, and a
graph written that way would be legible to nothing but itself.

## Decision

`wireEntities` walks the schema's whole entity table and fails the batch if any
table contains a multi-label type. The diagnostic names every offender and each
one's labels, not just the first. The refusal is not narrowed to the columns the
batch projects.

## Considered options

**Omit the struct on AGE and emit it elsewhere.** Rejected: it breaks
`TestBackendInvariantSurface`, which is the property the multi-backend design
rests on — the Go a caller writes against does not vary by backend. Measured,
not assumed: enrolling `test/data/codegen/valid/entity_multi_label_named` in
both targets with the entity dropped on AGE reddens that test with
`entity_multi_label_named declares a different Go surface under neo4j-go-v5
than under apache-age-pgx-v5`, the AGE surface holding three declarations to
neo4j's four.

**Emit the struct on AGE and refuse only the queries that project it.**
Rejected, and rejected on evidence rather than on principle: built, it keeps
`TestBackendInvariantSurface` green — the comparison is over declarations, and
`decode<Entity>` is an unexported receiver-less function the comparison skips —
and emits, at exit 0, a `PersonEmployee` struct whose decoder reads

    if label != "Employee&Person" {

`Employee&Person` is gqlc's own `LabelSetKey` join spelling. No AGE vertex
carries it, and no AGE statement can stamp it. The generated package would
therefore declare a type that compiles, that a caller can construct, and that
no query can ever fill — with a label check nothing could satisfy — and would
say nothing about it.

That last clause no longer holds, and this paragraph is kept as the record of
why the shape was rejected rather than of what would happen today. The residual
it named — filed as `gqlc-05tl` — is closed by
`TestEmittedDecodersGuardOnlyOnStampableLabels`
(`internal/codegen/conformance/decoder_reachability_test.go`), which runs every
registered backend over every valid fixture and refuses any emitted `decode<T>`
whose label guard is a string no value on the decoded entity's own axis can
carry — a node type's decoder is held to the node key labels the schema
declares, an edge type's to the relationship types, and never to the union of
the two.
It reddens on this shape naming `decodePersonEmployee` and the join spelling.
The rejection above stands on its own terms: a gate that catches the emission is
not a reason to prefer emitting it.

That is the same disposition ADR 0025 rejected for temporal kinds: "it emits a
column no decoder can fill at exit 0. Generate-time refusal is this codebase's
posture for *this backend cannot represent that*." Taking it here would make the
posture depend on which limitation you had hit.

**Move the check into the shared prepare phase behind a `TypeMap`-style
channel**, as `TypeMap.Temporal` answers carriage per kind (ADR 0025).
Rejected. ADR 0025's channel exists because the temporal enum *splits*: the
spike found faithful encodings for some kinds and none for calendar durations,
so the answer has to vary per member and grow one arm at a time. Label-set
arity does not split. It is one label or it is unrepresentable, permanently,
because the constraint is in AGE's grammar rather than in its value space. A
refusal channel whose answer can never vary is surface that reads as a
capability — the inverse of ADR 0025's reason for leaving `TypeMap.Scalar`
total.

**Refuse only when the batch is empty of queries, or only per projecting
query.** Rejected: Phase Z names an entity for *every* type the schema
declares, whatever the batch projects, and the emitted surface is
backend-invariant. A type no query touches is still a type the generated
package has to declare, so scoping the gate to the batch's columns would only
move the unfillable declaration from a refused batch into a shipped one.

## Consequences

- A schema carrying one multi-label type refuses AGE generation for the whole
  batch. This is a real cost and it is stated in the diagnostic rather than
  left to be discovered: the message names every refused type and the labels it
  is keyed on, states that AGE stamps exactly one label and that its parser has
  no syntax for a second, gives the witness statement that does not parse,
  counts the queries the refusal takes with it, and names the two ways out —
  give each type a single key label, or generate that schema against a neo4j
  target.

- `test/data/codegen/valid/entity_multi_label_named` stays enrolled for
  `neo4j-go-v5` only. `TestRejectsMultiLabelSchema` now asserts both halves of
  that — that the manifest omits this backend, and that this backend refuses
  the schema — so the omission is a recorded verdict rather than a gap a reader
  takes for an oversight.

- **The refusal has no negative fixture in the conformance corpus, and cannot
  have one today.** `invalid/*/manifest.json` names its sentinel through
  `sentinelByName`, whose lanes are `codegen.`, `queryfile.` and `cypher.`;
  the conformance suite resolves targets through the composed registry and
  imports no single backend, so `age.ErrUnsupportedSchema` is unnameable there
  and `TestInvalid` would fail such a fixture on the lookup. The same gap
  applies to `age.ErrUnsupportedQuery`, and is pre-existing. Closing it means
  giving the registry a way to publish a backend's sentinels; that is `gqlc-rv0h`,
  not this decision.

- The `EntityEdge` arm of the multi-label check is unreachable from GQL input:
  the grammar admits no `&` in an edge's key label set, so
  `-[:WORKED_FOR&FOR]->` fails to parse before codegen sees it. The arm is kept
  because `codegen.Entity` can carry one and a grammar change would reach it
  silently otherwise.
