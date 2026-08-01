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
| `ErrUnrepresentableWidth` | A query parameter whose property width the target's type table has no carrier for. | Phase A |
| `ErrOutOfC6Scope` | A non-property parameter — whole node, whole edge, scalar literal, list, temporal expression, or unknown. Post-v1. | Phase A |
| `ErrUnrepresentableEdgeUnion` | An edge-union column two of whose candidates carry the same label. | Phase A |
| `ErrUnrepresentableEdgeUnion` | An edge union reached through a list element chain, two of whose candidates carry the same label. | Phase A |
| `ErrUnrepresentableWidth` | A list element chain whose leaf is a property width the target's type table has no carrier for. | Phase A |
| `ErrOutOfC6Scope` | A list element chain whose leaf is a resolved type the element arms do not cover. | Phase A |
| `ErrParamNameCollision` | Two parameters of one query mangling to one `Params` field. | Phase B |
| `ErrRowFieldCollision` | Two columns of one query deriving one `Row` field. | Phase B |
| `ErrIdentifierCollision` | Two exported top-level identifiers colliding across the six swept sources — entity structs, decode helpers, method names, `<Method>Params`, `<Method>Row`, edge-union interfaces. | identifier sweep |

## 3. Branches no input reaches

Checks that carry a sentinel but that no schema and no query can fire.
Two reasons, and the difference matters when reading a fail-site: a
*shadowed* branch applies a predicate an earlier phase already applied to
the same input, so the earlier phase always wins; a *defensive* branch
holds an invariant an upstream layer maintains, so reaching it takes a
synthetic seam or a regression above.

They are listed because §2 claims to carry every construct in each
sentinel's catchment, and a reader matching a fail-site in `prepare.go`
against §2 would otherwise find nothing. They are listed separately
because §5 asks for a negative fixture per §2 row, and for these no
fixture can be written.

| Sentinel | Branch | Phase |
|---|---|---|
| `ErrUnrepresentableWidth` | A projected column whose property width the target's type table has no carrier for. Shadowed: the width belongs to a schema property, and Phase Z walks every property of every type first. | Phase A |
| `ErrOutOfC6Scope` | A column, or a list element leaf, referencing a node or edge type the schema does not declare. Defensive: the resolver commits only declared types. | Phase A |
| `ErrOutOfC6Scope` | A column whose resolved type matches no arm of the closed sum. Defensive: a deletion fence over the sum's membership. | Phase A |
| `ErrOutOfC6Scope` | An edge union with fewer than two candidates, or one naming a candidate the schema does not declare. Defensive: the resolver collapses a lone candidate to `ResolvedEdge` and commits only declared edges. | Phase A |
| `ErrAliasRequired` | A column whose text yields no row-field name at derivation. Shadowed: Phase A applies `rowFieldName` to the same columns first, and returns the same message. | Phase B |
| `ErrOutOfC6Scope` | A parameter or column type Phase A admits only in its representable form. Defensive: a Phase A regression fails loudly here rather than emitting a field nothing can fill. | Phase B |

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
   goes in §3 instead and carries no fixture — if every construct you can
   name for a new sentinel belongs in §3, the sentinel does not belong in
   `allSentinels`.
4. A rename is the same three edits with the old name removed; a
   retirement drops the rows, the slice entry and the fixture together.

The stage specs are not part of this loop. They record what each stage
shipped and take no edit when the set moves.
