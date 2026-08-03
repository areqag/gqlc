# codegen sentinel taxonomy

The live index of the error sentinels `internal/codegen` refuses through:
what each one means, and which construct routes to it.
`TestSentinelTaxonomy` (`internal/codegen/errors_test.go`) holds this
document against `allSentinels` in both directions, so a sentinel added,
renamed or retired in code fails the suite until the tables below say the
same thing, and a row naming a sentinel that no longer exists fails it
too.

The C0–C6 stage specs each carry a snapshot of this taxonomy as it stood
at that stage, and they stay as they are. C2's table names
`ErrOutOfC2Scope` because that is the sentinel C2 shipped, four renames
ago; rewriting it to match today's code would destroy the record without
making anything true. C6 was the last stage, so a sentinel arriving after
it — `ErrUnrepresentableEdgeUnion` is the first — has no stage spec left
to be recorded in. This document is where the current answer lives; the
stage specs are where the historical ones do.

"Stay as they are" is not the same as "read as history". Twelve places
in that series look current and are not, and each now carries a note
pointing here: every stage's sentinel-set section (C0 §9 through C6 §7),
and every construct-to-sentinel table under "Out of scope, routed to the
appropriate sentinel" (C1 §7 through C5 §7 — C5's is the series' last,
C6 has no such table). `ErrOutOfC5Scope` in C5's table is a weak
self-signal and the other nineteen rows carry none, so the note is what
tells a reader who greps for a construct that they have landed in the
past. `TestSentinelTaxonomy` fails if one of those sections loses its
note or a new one arrives without it. The posture and the wording follow
`docs/specs/cli-stage-0.md` §4.4, which took the same line toward C6.

The set is the shared front end's. The backend packages under
`internal/codegen` declare sentinels of their own for shapes only one
target refuses, and those are outside both `allSentinels` and this
document.

---

## 1. Index — the reachable set

One row per member of `allSentinels`, which is the closed set of
sentinels the shared front end may return on a user's input. A backend
returns its own sentinels on top of these, so a `Generate` call can fail
with an error outside this set. Every member is paired with at least one
negative fixture in `test/data/codegen/invalid`, which
`TestSentinelReachability` enforces.

| Sentinel | Meaning | Introduced |
|---|---|---|
| `ErrInvalidPackageName` | The emitted package name — configured, or mangled from the schema name — is not a valid Go package identifier. | C0 |
| `ErrDuplicateSourceFile` | Two queries in one batch come from distinct paths whose basenames collide, so their emitted files would collide too. | C0 |
| `ErrDuplicateQueryName` | Two queries in one batch share a name. The front end reads one file at a time and cannot see the collision; the batch check can. | C0 |
| `ErrInvalidCardinality` | A query carries the zero cardinality, or one outside the closed set. A caller bug the front end never produces. | C0 |
| `ErrOutOfC6Scope` | The input is admissible but carries a construct v1 does not project. Category-grained, and renamed at each stage boundary so a caller branching on it re-inspects at the upgrade. | C6 (renamed from `ErrOutOfC5Scope`) |
| `ErrParamNameCollision` | Two parameters of one query mangle to the same `Params` field. | C1 |
| `ErrRowFieldCollision` | Two columns of one query derive the same `Row` field. | C1 |
| `ErrAliasRequired` | A column's text is neither a bare identifier nor a property access, so no row-field name follows from it deterministically. | C1 |
| `ErrIdentifierCollision` | Two exported top-level identifiers in one emitted package collide, or a query's method name is one the emission reserves. | C1 (C2 and C5 widen the swept set) |
| `ErrInvalidEntityName` | An entity's explicit name, or the name its labels mangle to, is not a valid exported Go identifier. | C2 |
| `ErrUnnamedMultiLabelType` | A schema type whose labels do not name it unambiguously carries no explicit name, and the emission will not guess one. | C2 |
| `ErrPropertyFieldCollision` | Two properties of one entity mangle to the same struct field. | C2 |
| `ErrUnrepresentableWidth` | A property width the target's type table has no faithful Go carrier for. The refused set is the backend's answer, not the language's: what one target carries another may not. | C3 |
| `ErrUnrepresentableEdgeUnion` | An edge-union leaf two of whose candidates carry one label. An edge value arrives with its label and its properties but never its endpoint types, so the label is the whole of what the dispatch has to tell candidates apart by. | `gqlc-35yu.9`, after C6 |
| `ErrUnrepresentableTemporal` | A temporal expression whose kind the target's type table has no faithful Go carrier for. Apart from `ErrUnrepresentableWidth` because a temporal expression carries no property width at all: the edit it asks for is to a query's `RETURN` clause, not to the schema. | #714, after C6 |
| `ErrExecOnProjection` | A query annotated `:exec` projects at least one column. The columns are contract; discarding them silently is the guess sqlc makes and ADR 0010 D1 refuses. | C4 |
| `ErrCardinalityShapeMismatch` | A query annotated `:one` or `:many` projects no columns, so there is no row type to return. | C4 |

## 2. Refusal taxonomy — construct to sentinel

Which sentinel a refusal belongs under, keyed by the construct being
refused. Many-to-one: a sentinel is category-grained and carries every
construct in its catchment. The phase column names the pass in `Prepare`
that owns the check (`internal/codegen/prepare.go`), first offender
winning across every axis.

Every row here is a construct some input reaches. The branches that
carry a sentinel but that nothing can fire are in §3, kept apart so a
contributor does not try to write a fixture for one.

| Sentinel | Refused construct | Phase |
|---|---|---|
| `ErrInvalidPackageName` | A configured package name that is not a valid Go package identifier. | package identifier |
| `ErrInvalidPackageName` | A schema name whose lowercase mangle is not a valid Go package identifier. | package identifier |
| `ErrUnnamedMultiLabelType` | A multi-label node type, or one with an empty label set, carrying no explicit `Name`. | Phase Z |
| `ErrUnnamedMultiLabelType` | A multi-label edge type, or one with an empty label, carrying no explicit `Name`. | Phase Z |
| `ErrUnnamedMultiLabelType` | An edge label shared across endpoint pairs, on an edge type carrying no explicit `Name`. | Phase Z |
| `ErrInvalidEntityName` | An explicit `NodeType.Name` or `EdgeType.Name` that is not a valid exported Go identifier. | Phase Z |
| `ErrInvalidEntityName` | A node label set, or an edge label, whose mangle is not a valid exported Go identifier. | Phase Z |
| `ErrPropertyFieldCollision` | Two properties on one entity mangling to one struct field. | Phase Z |
| `ErrUnrepresentableWidth` | A schema property whose width the target's type table has no carrier for, whether or not any query projects it. | Phase Z |
| `ErrInvalidCardinality` | A query whose `Cardinality` is the zero value. | batch admission |
| `ErrDuplicateQueryName` | Two queries in one batch under one name. | batch admission |
| `ErrDuplicateSourceFile` | Two queries from distinct source paths sharing a basename. | batch admission |
| `ErrIdentifierCollision` | A query name matching an identifier the emission reserves (`Queries`, `New`, `WithTx`, `DBTX`, `EnsureGraph`, …). | Phase A |
| `ErrInvalidCardinality` | A query whose `Cardinality` is outside the closed set. | Phase A |
| `ErrExecOnProjection` | `:exec` on a query with one or more projected columns. | Phase A |
| `ErrCardinalityShapeMismatch` | `:one` or `:many` on a query with no projected columns, read or write. | Phase A |
| `ErrOutOfC6Scope` | Query text carrying a backtick, which no Go raw string can hold. | Phase A |
| `ErrAliasRequired` | A column whose text is neither a bare identifier nor a property access. | Phase A |
| `ErrOutOfC6Scope` | A non-property parameter — whole node, whole edge, scalar literal, list, temporal expression, or unknown. Post-v1. | Phase A |
| `ErrUnrepresentableEdgeUnion` | An edge-union column two of whose candidates carry the same label. | Phase A |
| `ErrUnrepresentableEdgeUnion` | An edge union reached through a list element chain, two of whose candidates carry the same label. | Phase A |
| `ErrParamNameCollision` | Two parameters of one query mangling to one `Params` field. | Phase B |
| `ErrRowFieldCollision` | Two columns of one query deriving one `Row` field. | Phase B |
| `ErrIdentifierCollision` | Two exported top-level identifiers colliding across the six swept sources — entity structs, decode helpers, method names, `<Method>Params`, `<Method>Row`, edge-union interfaces. | identifier sweep |

## 3. Branches no input reaches

Checks that carry a sentinel but that no schema, no query and no CLI
option can fire. Two reasons, and the difference matters when reading a
fail-site: a *shadowed* branch applies a predicate an earlier check
already applied to the same input, so the earlier one always wins; a
*defensive* branch holds an invariant an upstream layer maintains, so
reaching it takes a synthetic seam or a regression above.

They are listed because §2 claims to carry every construct in each
sentinel's catchment, and a reader matching a fail-site in `prepare.go`
against §2 would otherwise find nothing. They are listed separately
because §5 step 3 asks for a negative fixture per sentinel, and a
sentinel whose every branch is here would have no fixture to offer.

Each branch is named rather than described: the site name in column two
is the `//gqlc:unreachable <site>` tag above that branch's `return`, and
the tags and this table are held equal in both directions. The site name
replaces §2's phase column here because several of these branches are
reached from more than one phase — `list-elem-width` hangs off both Phase
A's probe and Phase B's plan build — so a single phase name would be
false.

**The claims below are measured, not asserted.** `TestSentinelTaxonomy`
runs the fixture conformance suite, the two backend suites and the CLI
under coverage of `internal/codegen` and fails if any tagged branch
executes; §5.1 gives the rule and the reason the sweep excludes this
package's own tests.
The "why" column therefore has one job the fence cannot do: name the
facts the claim rests on, so a reader who changes one of them knows what
they are changing. It does not have to be trusted — if a fact moves and
the branch becomes live, the coverage assertion goes red and names the
row.

| Sentinel | Fail-site | Branch | Why it cannot fire |
|---|---|---|---|
| `ErrUnrepresentableWidth` | `column-width` | A projected column whose property width the target's type table has no carrier for. | Shadowed by Phase Z. A column's `ResolvedProperty.Type` is a schema property's type, and `phaseZAdmit` runs `tm.Property` over every property of every node and edge type before Phase A, first offender winning. |
| `ErrUnrepresentableWidth` | `param-width` | A query parameter whose property width the target's type table has no carrier for. | Shadowed by Phase Z, on the same argument, plus one fact: a parameter's `ResolvedProperty` comes either from a schema property lookup (`resolver/scope.go`, `resolver/resolve.go`) or from `callProjectionType`, which yields only INT / FLOAT / STRING — widths both type tables carry. `unify` never synthesises a third source. |
| `ErrUnrepresentableWidth` | `list-elem-width` | A list element whose leaf is a property width the target's type table has no carrier for. | Shadowed, and the argument rests on three facts. (1) No resolver-built `ResolvedList` bottoms out in a `ResolvedProperty`: `resolveType` has no such arm, and the variable-length-edge arm yields `ResolvedEdge` / `ResolvedEdgeUnion`. (2) The one `ResolvedProperty` element that exists is the one Phase B splits off a schema LIST property, and `neo4j`'s `Property(LIST<X>)` succeeds exactly when `Property(X)` does. (3) `age` refuses every LIST outright, so an AGE batch loses at Phase Z. Widening either backend's LIST admission is what would make this live. |
| `ErrOutOfC6Scope` | `column-unknown-node`, `column-unknown-edge`, `list-elem-unknown-node`, `list-elem-unknown-edge` | A column, or a list element leaf, referencing a node or edge type the schema does not declare. | Defensive: the resolver resolves against the same schema Phase Z indexed and commits only declared types. |
| `ErrOutOfC6Scope` | `column-unknown-variant`, `list-elem-unknown-variant` | A column, or a list element, whose resolved type matches no arm of the closed sum. | Defensive: a deletion fence over the membership of `resolver.ResolvedType`, which is sealed by an unexported method and has eight implementations, all eight of which both switches handle. The two sites are the same construct at two depths. |
| `ErrOutOfC6Scope` | `edge-union-arity`, `edge-union-undeclared` | An edge union with fewer than two candidates, or one naming a candidate the schema does not declare. | Defensive: the resolver collapses a lone candidate to `ResolvedEdge` and commits only declared edges. |
| `ErrAliasRequired` | `row-field-alias` | A column whose text yields no row-field name at derivation. | Shadowed: Phase A applies `rowFieldName` to the same columns first, and returns the same message. |
| `ErrOutOfC6Scope` | `param-type-invariant`, `column-type-invariant` | A parameter or column type Phase A admits only in its representable form. | Defensive: a Phase A regression fails loudly here rather than emitting a field nothing can fill. |

## 4. Declared and deliberately unreachable

Sentinels the package declares but keeps out of `allSentinels`, so no
negative fixture is required of them.

| Sentinel | Why it is out of the reachable set |
|---|---|
| `ErrFormatFailure` | `go/format.Source` rejected an emitted file. A well-formed emission cannot fail formatting, so firing this takes a template bug or synthetic corruption; a fixture for it would buy a test seam rather than coverage. |

## 5. Adding, renaming or retiring a sentinel

The fence names the document and the section on failure, so the loop is
short. In full:

1. Declare the sentinel in `internal/codegen/errors.go` with a doc
   comment saying what it refuses. Declare it on its own line: the fence
   pairs each name with the message beside it.
2. Add it to `allSentinels` if a user's input can reach it, and add a row
   to §1 above. Otherwise leave it out of the slice and add a row to §4
   saying why.
3. Add a §2 row per construct that routes to it, and a negative fixture
   under `test/data/codegen/invalid` for at least one of them, which is
   what `TestSentinelReachability` asks for. A branch no input can reach
   goes in §3 instead, tagged and fenced per §5.1 — if every construct
   you can name for a new sentinel belongs in §3, the sentinel does not
   belong in `allSentinels`.
4. A rename is the same three edits with the old name removed; a
   retirement drops the rows, the slice entry and the fixture together.

The stage specs are not part of this loop. They record what each stage
shipped and take no edit when the set moves.

### 5.1 Deciding whether a branch is §2 or §3

Reachable means **a user's input reaches it** — a schema, a query file,
or a CLI option, arriving through `Generate`. It does not mean "some
test executes the line". A test inside `package codegen` can call an
unexported function with a hand-built `resolver` value and reach a
branch no parser can produce; classifying by test-presence is what put
three unreachable branches into §2 before `gqlc-h4ug`, including one
whose twin — the identical default arm over the same sealed sum, one
level up — sat in §3 the whole time because it happened to have no unit
test.

So the measurement is taken from outside the package.
`TestSentinelTaxonomy` asks `go list -test` which packages' test
binaries link `internal/codegen`, drops `internal/codegen` itself, runs
`go test` over the rest under coverage of `internal/codegen`, and
asserts every tagged branch goes unexecuted. An out-of-package caller
reaches this package through its exported surface, which is the surface
a user's input arrives on — so that set is derived rather than listed,
and a new consumer joins it by existing.

`-test` is the load-bearing flag and its absence does not show in the
result. Plain `go list` reports only what a package's non-test build
imports, and `internal/codegen/conformance` holds nothing but
`conformance_test.go`, so it would drop out of the sweep and take all
twenty-nine negative fixtures with it — leaving a fence that measured
the backends and the CLI, found every tagged branch unreached, and
passed. The sweep therefore also asserts that the conformance package is
in the set it derived.

The measurement runs in one direction only. A tagged branch the corpus
reaches fails; an untagged branch no fixture happens to cover does not,
because "no fixture covers it yet" and "no input can reach it" are
different claims and a tag asserts the second. That asymmetry is
deliberate — it leaves a corpus gap to be found by other means, but it
makes the fail-open direction, silencing a live refusal by tagging it,
the one the fence cannot be talked into.

To classify a branch:

1. Write the fixture you think reaches it, under
   `test/data/codegen/invalid`, and run it. Instrument the fail-site if
   the sentinel alone does not tell you which branch answered — several
   branches share a sentinel, and two of them share a message.
2. If the fixture fires the branch, it is a §2 row and that fixture is
   the one step 3 above asks for.
3. If an earlier check answers instead, the branch is **shadowed**. Say
   which check wins and on what input.
4. If no input can be constructed at all, the branch is **defensive**.
   Say which upstream invariant would have to break first.
5. For 3 and 4: tag the `return` with `//gqlc:unreachable <site>`, add a
   §3 row naming the same site, and write the "why" so it names the
   facts it depends on rather than asserting a conclusion. The fence
   holds tags and rows equal in both directions and holds both against
   the coverage measurement, so a branch tagged without the row, a row
   naming no tag, and a tag over a branch the corpus reaches are three
   separate failures with three separate messages.
