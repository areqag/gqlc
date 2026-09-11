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

One exception, and it is declared in code rather than only here:
`assembledOnlySentinels` in
`internal/codegen/conformance/conformance_test.go` lists members no
fixture can reach because no query *text* reaches the branch on any
enrolled target. Their witness is a case in `TestAssembledInput`
instead — §5.1's criterion, applied to a whole sentinel rather than to
one construct — and `TestReachableBranchesAreReached` still measures the
branch in the same corpus profile, so the obligation moves rather than
lapses. The list is fail-closed in both directions: an entry outside
`allSentinels` reds the sweep as prose against nothing, and so does an
entry a fixture *does* cover, because a fixture is the better witness
whenever one exists.

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
| `ErrRecordFieldCollision` | A declared record's fields have no legal Go spelling: two field names mangling to one struct field, or a name of underscores alone mangling to none. Apart from `ErrPropertyFieldCollision`, which applies the same mangle one container out, because the two name different things to fix — that one an entity and two of its properties, this one a record TYPE that may be declared far from the property carrying it, where renaming a field changes every position the type reaches. Asked before the carrier question — it stood behind `ErrUnimplementedTypeKind`'s kind walk until `gqlc-x2uy` deleted that sentinel, and is now the first question each site asks — at every site that asks a type map for a property carrier, and asked RECURSIVELY through list elements and record fields: a record is spelled as a Go struct wherever it is reached from. Not a target's answer — the mangle is gqlc's, so the refusal holds on every backend at once. | stage 1 of `gqlc-x9tg7` |
| `ErrUnrepresentableWidth` | A property width the target's type table has no faithful Go carrier for. The refused set is the backend's answer, not the language's: what one target carries another may not. | C3 |
| `ErrUnstorableProperty` | A schema property whose width the target's STORE will not hold, though the type table carries it faithfully. Apart from `ErrUnrepresentableWidth` because the two answer different questions and ask the reader for different edits: an unrepresentable width has no Go type at all, while an unstorable one has one that is exercised — neo4j carries `LIST<LIST<INT16>>` as `[][]int16` and decodes one arriving as a query value, and it is the server that refuses to hold it as a property. Scoped to declared properties for that reason: a column and a parameter are read and bound, never stored. | ADR 0035, `gqlc-v0gk` |
| `ErrUnrepresentableEdgeUnion` | An edge-union leaf two of whose candidates carry one label. An edge value arrives with its label and its properties but never its endpoint types, so the label is the whole of what the dispatch has to tell candidates apart by. | `gqlc-35yu.9`, after C6 |
| `ErrUnrepresentableTemporal` | A temporal expression whose kind the target's type table has no faithful Go carrier for. Apart from `ErrUnrepresentableWidth` because a temporal expression carries no property width at all: the edit it asks for is to a query's `RETURN` clause, not to the schema. **Assembled-only** since bd `gqlc-dy40s`: the only backend that refuses a temporal kind is Apache AGE, and its dialect gate now refuses every temporal spelling on the query text ahead of the carrier question, so no query text an enrolled target parses reaches this sentinel. Not retired — `codegen.Prepare` is portable and the branch is live for any consumer whose type map lacks a kind. | #714, after C6 |
| `ErrExecOnProjection` | A query annotated `:exec` projects at least one column. The columns are contract; discarding them silently is the guess sqlc makes and ADR 0010 D1 refuses. | C4 |
| `ErrCardinalityShapeMismatch` | A query annotated `:one`, `:many` or `:iter` projects no columns, so there is no row type to return. | C4 (`:iter` added by `gqlc-1a5`) |
| `ErrFormatFailure` | `go/format.Source` rejected an emitted file's raw contents. The one member whose fail-site is the FORMATTER rather than a check: every other sentinel here is returned by something that inspected the input and refused it, while this one is returned after the input was accepted and emitted. **Assembled-only**, and unlike `ErrUnrepresentableTemporal` it never had a fixture and cannot have one — an on-disk fixture would have to be a well-formed schema whose correct emission does not parse, which is a bug in this generator rather than a property of the input, so the fixture would freeze a live defect into the corpus and fall due for deletion the day it was repaired. Moved here from §4 by bd `gqlc-9xiz`; §4 records the three falsifications and which one ended the exclusion. | C0 (promoted to the reachable set by `gqlc-9xiz`) |
| `ErrIterOnWrite` | A query annotated `:iter` writes. Streaming hands the consumer a row before the transaction that produced it has committed, so a write the consumer has already acted on may still roll back; ADR 0010 D8 makes `:iter` read-only rather than defining what a rolled-back yielded row means. | `gqlc-1a5` |

## 2. Refusal taxonomy — construct to sentinel

Which sentinel a refusal belongs under, keyed by the construct being
refused. Many-to-one: a sentinel is category-grained and carries every
construct in its catchment. The phase column names the pass in `Prepare`
that owns the check (`internal/codegen/prepare.go`), first offender
winning across every axis.

Every row here is a construct some input reaches. The branches that
carry a sentinel but that nothing can fire are in §3, kept apart so a
contributor does not try to write a fixture for one.

Input arrives two ways and both count, per §5.1. Most rows are reached
by a schema, a query file or a CLI option, and their fixture lives under
`test/data/codegen/invalid`. The rest are reached only by an `Input` a
consumer of this package assembles and hands to `Generate` — a
`Cardinality` outside its constant set, a `schema.Schema` no GQL parse
would produce, a `Validated` shape the resolver did not build. Those
have no on-disk form, so they are reached from
`internal/codegen/conformance/assembled_input_test.go` instead, and
their rows say so. The distinction is worth carrying because it names
the edit each row's refusal asks for: the first kind asks a user to
change a file, the second asks a caller to fix its own code.

**That claim is measured too, and by the same sweep §3 is.** Every
fail-site in the package that is *not* tagged `//gqlc:unreachable` must
be executed by the corpus at least once; a fail-site the corpus never
reaches fails `TestSentinelTaxonomy` naming the file and the line, and
the fix is either a fixture that reaches it or a §3 row saying why none
can. The two assertions are mirrors — §3's sites must measure zero,
§2's must measure non-zero — so a claim in either direction has to be
paid for in coverage. §5.1 gives the rule and the one exemption: a
sentinel outside `allSentinels`, which is to say a §4 row.

That exemption is from *this* section's obligation and from nothing else.
A §4 sentinel owes the opposite measurement instead — every branch
returning it must go unexecuted, exactly as a §3-tagged branch must —
and `TestExcludedBranchesAreUnreached` collects it. Until that test
existed §4 owed nothing at all, which made it a place to park a live
sentinel with no fixture and no red run (`gqlc-4r01`).

The measurement binds fail-sites rather than rows: a row's *prose* is
still a hand-written claim about which construct reaches the site. What
the fence guarantees is that no site in this section's catchment is
dead, and that no dead site hides in it.

| Sentinel | Refused construct | Phase |
|---|---|---|
| `ErrInvalidPackageName` | A configured package name that is not a valid Go package identifier. | package identifier |
| `ErrInvalidPackageName` | A schema name whose lowercase mangle is not a valid Go package identifier. | package identifier |
| `ErrUnnamedMultiLabelType` | A multi-label node type carrying no explicit `Name`. | Phase Z |
| `ErrUnnamedMultiLabelType` | An edge label shared across endpoint pairs, on an edge type carrying no explicit `Name`. | Phase Z |
| `ErrUnnamedMultiLabelType` | A node type with an empty label set and no explicit `Name`. Assembled: `Schema.Nodes` keyed by the empty `LabelSetKey`. A GQL parse cannot carry one — `schema/gql` refuses it with `ErrUnnamedNodeType` — but `Input.Schema` is a `schema.Schema` a caller fills in directly. | Phase Z |
| `ErrUnnamedMultiLabelType` | A multi-label edge type carrying no explicit `Name`. Assembled: an `EdgeKey` whose `KeyLabels` is a conjunction. A parse cannot carry one — Cypher has no conjunction syntax for edge labels and `schema/gql` refuses the key with `ErrMultiLabelEdgeType`. | Phase Z |
| `ErrUnnamedMultiLabelType` | An edge type with an empty label and no explicit `Name`. Assembled: an `EdgeKey` whose `KeyLabels` is empty; `schema/gql` refuses one with `ErrUnnamedEdgeType`. | Phase Z |
| `ErrInvalidEntityName` | An explicit `NodeType.Name` or `EdgeType.Name` that is not a valid exported Go identifier. | Phase Z |
| `ErrInvalidEntityName` | A node label set, or an edge label, whose mangle is not a valid exported Go identifier. | Phase Z |
| `ErrPropertyFieldCollision` | Two properties on one entity mangling to one struct field. | Phase Z |
| `ErrUnrepresentableWidth` | A schema property whose width the target's type table has no carrier for, whether or not any query projects it. | Phase Z |
| `ErrUnstorableProperty` | A schema property whose width the target's store will not hold, whether or not any query projects it. Today's only instance is a list whose element is a list, on the neo4j targets: the server answers such a write "Collections containing collections can not be stored in properties" (ADR 0035). Asked after the carrier question, so a width that is both unrepresentable and unstorable is reported as unrepresentable — the missing carrier is the nearer obstacle and the one whose edit comes first. | Phase Z |
| `ErrRecordFieldCollision` | A schema property whose type is a record with two field names mangling to one Go struct field, or with a field name of underscores alone, at any depth reachable through list elements and record fields. The first question this site asks, since `gqlc-x2uy` deleted the kind walk that used to stand ahead of it: a record with no Go spelling is reported as a collision rather than as a width the table cannot carry, because the rename is the edit and a struct gqlc cannot name is one no type map was ever going to be asked about. | Phase Z |
| `ErrInvalidCardinality` | A query whose `Cardinality` is the zero value. | batch admission |
| `ErrDuplicateQueryName` | Two queries in one batch under one name. | batch admission |
| `ErrDuplicateSourceFile` | Two queries from distinct source paths sharing a basename. | batch admission |
| `ErrIdentifierCollision` | A query name matching an identifier the emission reserves (`Queries`, `New`, `WithTx`, `DBTX`, `EnsureGraph`, …). | Phase A |
| `ErrExecOnProjection` | `:exec` on a query with one or more projected columns. | Phase A |
| `ErrCardinalityShapeMismatch` | `:one` or `:many` on a query with no projected columns, read or write. | Phase A |
| `ErrIterOnWrite` | `:iter` on a query the resolver classified as a write, whether or not it projects columns. Ordered AHEAD of the zero-column gate, so a zero-column `:iter` write — which satisfies both — is reported here. That ordering is the useful one rather than the arbitrary one: adding a `RETURN` to a streamed write leaves it refused, where re-annotating it `:many` admits it outright, so this is the axis whose edit actually resolves the input. `TestIterAdmission` pins the order, and `invalid/iter_on_write` is the fixture. | Phase A |
| `ErrCardinalityShapeMismatch` | `:iter` on a READ with no projected columns. **Assembled**, and it is the only row in this table whose fixture was attempted and found not to exist: a read query must end in `RETURN`, which projects at least one column, and the one other zero-column read shape — a `CALL` with no `YIELD` — dies at `ErrUnknownProcedure` in the conformance harness, which builds `procsig.NewRegistry(nil)`. Four spellings were measured against the harness on 2026-09-11 (`RETURN *` bare, after a `WITH`, and under a `WHERE`; and `CALL db.ping()`) and none reached this branch. That is four candidates on today's harness rather than a proof that no text reaches it; read it as "no fixture found yet", and a procedure registry arriving in the corpus is the change that would make one the better witness. `TestAssembledInput/cardinality-iter-zero-column-read` is the witness, per §5.1's assembled-input criterion. It was written in the `codegen_test` package (`gqlc-1a5`) and moved here by `gqlc-e3ra`: `corpusPackageOf` folds `internal/codegen_test` onto `internal/codegen`, which `corpusPackages` then drops, so the earlier witness executed this return in a binary the coverage sweep never ran. Nothing was red, because the two `:one`/`:many` write fixtures cover the same return — deleting both reddens `TestReachableBranchesAreReached` on that return with the old witness in place, and leaves it green with this one. | Phase A |
| `ErrOutOfC6Scope` | Query text carrying a backtick, which no Go raw string can hold. | Phase A |
| `ErrOutOfC6Scope` | Query text carrying a carriage return. A Go raw string holds one but DISCARDS it from the literal's VALUE (Go spec, "String literals"), so the emission compiles and the constant is not the text that was parsed and resolved. The one member of this family whose failure is silent, and the reason the family is checked at all (`gqlc-7f9a`). Row added with the two below it, having been owed since. | Phase A |
| `ErrOutOfC6Scope` | Query text carrying a NUL, which Go source may not hold at all — a documented implementation restriction on the language, not a property of the literal — so the emitted file does not parse. Loud rather than silent: nothing ships wrong, and the refusal buys the diagnostic rather than the correctness (`gqlc-32n53`). | Phase A |
| `ErrOutOfC6Scope` | Query text that is not valid UTF-8, which Go source must be, so the emitted file does not parse. Loud on the NUL row's argument. Both are reached only from the positions the openCypher lexer has no reason to scan — inside a line comment, and inside a string literal, where the byte is a value the server itself accepts. Bare, either is a token recognition error the parse raises first, so a fixture for these two must place the byte deliberately (`gqlc-32n53`). | Phase A |
| `ErrAliasRequired` | A column whose text is neither a bare identifier nor a property access. | Phase A |
| `ErrOutOfC6Scope` | A parameter whose resolved type is not `ResolvedProperty`. Post-v1. The gate is a type assertion rather than a switch, so it admits exactly one dynamic type and what reaches it is everything else satisfying `resolver.ResolvedType` — a set no enumeration closes, on exactly the argument the column row below makes. That is the whole statement of what lands here; what follows illustrates it rather than bounding it. The other seven variants in value form reach it — whole node, whole edge, edge union, scalar literal, list, temporal expression, unknown — and so do the nil interface and all eight variants' pointer forms. The property variant is spared in its value form alone: `*resolver.ResolvedProperty` lands here, measured by `TestUnmatchedResolvedTypeRefusesRatherThanFaulting/parameter/typed-nil/ResolvedProperty`, and so does `struct{ resolver.ResolvedProperty }`, measured as `resolved as property:` by the assembled-`Input` suite's `param-non-property` case — so neither the pointer forms nor the embedders here are scoped to the other seven. `TestUnmatchedResolvedTypeRefusesRatherThanFaulting/parameter` measures the four faulting shapes at this site from outside the package; the column row carries the argument and the limit. | Phase A |
| `ErrUnrepresentableEdgeUnion` | An edge-union column two of whose candidates carry the same label. | Phase A |
| `ErrUnrepresentableEdgeUnion` | An edge union reached through a list element chain, two of whose candidates carry the same label. | Phase A |
| `ErrInvalidCardinality` | A query whose `Cardinality` is neither the zero value nor a member of the closed set. Assembled: `Cardinality(7)`. `queryfile.parseCardinality` yields only the three members or refuses the annotation, so no parse produces a fourth. | Phase A |
| `ErrUnrepresentableWidth` | A projected column whose property width the target's type table has no carrier for. Assembled: a `Validated.Columns` entry carrying a width no schema property declares — Phase Z walks the schema, so a column backed by a declared property loses there first. | Phase A |
| `ErrUnrepresentableWidth` | A query parameter whose property width the target's type table has no carrier for. Assembled, on the same argument: the resolver draws a parameter's `ResolvedProperty` from a schema property or from `callProjectionType`, and both yield widths Phase Z has already passed. | Phase A |
| `ErrRecordFieldCollision` | A projected column carrying a record whose fields have no legal Go spelling. Assembled, on the width row's argument. The record is legal as a KIND and illegal only as a SPELLING, which is why the carrier question would answer and the collision is the only thing left to refuse it. | Phase A |
| `ErrRecordFieldCollision` | A query parameter carrying such a record. Assembled, on the same argument. Spelled at both sites because they are separate returns in separate passes, and a check wired into one alone would leave the other emitting a struct that declares one Go field twice. | Phase A |
| `ErrUnrepresentableWidth` | A list element whose leaf carries a property width the target's type table has no carrier for. Assembled: a `ResolvedList` over a `ResolvedProperty`, a shape `resolveType` has no arm for. Phase B repeats the walk over the element it splits off a schema LIST property; that element's width is one Phase Z has already passed. | Phase A |
| `ErrRecordFieldCollision` | A list element whose leaf is a record with no legal Go spelling. Assembled, spelled the same way and on the same argument as the kind row above it. | Phase A |
| `ErrOutOfC6Scope` | A column, or a list element leaf, referencing a node or edge type the schema does not declare. Assembled: the resolver resolves against the same schema Phase Z indexed and commits only declared types. | Phase A |
| `ErrOutOfC6Scope` | An edge union with fewer than two candidates, or one naming a candidate the schema does not declare. Assembled: the resolver collapses a lone candidate to `ResolvedEdge` and commits only declared edges. | Phase A |
| `ErrOutOfC6Scope` | A column whose resolved type matches no arm of the switch over `resolver.ResolvedType`. Assembled: the pointer form of a variant, `&resolver.ResolvedNode{}` — the marker and every variant's `String` take value receivers, so `*ResolvedNode` satisfies the interface while `case resolver.ResolvedNode:` does not match it. The fall-through set is not "the eight and their pointers": `resolver.ResolvedType` is sealed against nothing an out-of-package caller can write, because Go promotes an embedded variant's unexported marker and `struct{ resolver.ResolvedNode }` therefore satisfies the interface from any package and lands here too (§5.1 step 5). The arm names the offending type by asking it, and four witnessed shapes that reach it have no answer to give: the nil interface; each of the eight typed-nil pointer forms, since Go emits a nil check before a value method reached through a pointer, so even the zero-sized `(*resolver.ResolvedUnknown)(nil)` faults where its body dereferences nothing; a struct whose embedded pointer to a variant is nil; and a struct embedding `resolver.ResolvedType` itself, which names no variant at all. The last two are neither a nil interface nor a nil pointer to look at. Four is what is witnessed, not what the set holds: whether a value faults is a fact about the `String()` it ends up dispatching, and the interface is satisfiable by types codegen never sees, so no enumeration here closes the set — a nil pointer to `struct{ resolver.ResolvedNode }` faults and is none of the four, since the promoted value method must dereference it and that struct is not one of the eight variants. Composing is not itself what faults, though: the same struct held by value answers, which is what `TestUnmatchedResolvedTypeKeepsTheWireTagWhereThereIsOne`'s `value-embedder` case pins. This enumeration is **not known to be closed**. All four faulted in the `String()` call inside the `return` until `gqlc-edze`; the render now goes through `ResolvedTypeName`, which asks under a recover and falls back to the dynamic type name whenever no name comes back, so they refuse as `<nil>`, `*resolver.ResolvedNode` and the caller's own type name. That the enumeration is open costs the fix nothing, because the fix enumerates no shape: an unwitnessed one renders by the same route. What it does bound is the panic and, since `gqlc-sv61`, the empty answer: a `String()` returning `""` faults nowhere, so it was taken at its word and the refusal rendered `resolved as ` with nothing after it. An empty name is now treated as no name and falls back with the faults, which is why the condition is the absence of a name rather than the presence of a panic. Emptiness is the whole of that test — a tag of `" "` or `"???"` is still passed through, since this arm can tell whether the type said anything but not whether it was any use. Neither bound covers every way an implementation can decline to return: a `String()` that blocks or calls `runtime.Goexit` still takes the caller with it. `TestUnmatchedResolvedTypeRefusesRatherThanFaulting` measures all four shapes at this arm from outside the package. A non-nil pointer form is what pays this row's coverage. | Phase A |
| `ErrOutOfC6Scope` | A list element whose resolved type matches no arm of `buildListElemPlan`'s switch. Assembled: the same pointer form under a `ResolvedList`, on the same argument — that switch names the same eight arms and its default is open in the same two directions, pointer forms and embedded promotions. It renders through the same `ResolvedTypeName` for the same reason: all eight typed-nil pointer forms faulted in the `String()` call inside the `return` until `gqlc-edze`, and so did a `resolver.ResolvedList{Element: nil}`, whose nil element reaches the same line, a list element whose embedded pointer to a variant is nil, and one embedding `resolver.ResolvedType` itself. The column row above carries the argument, the limit, and the fact that those four shapes are witnessed rather than exhaustive. | Phase A |
| `ErrUnrepresentableTemporal` | A list element whose temporal kind the target's type table has no carrier for. Assembled: a `resolver.Temporal` outside its own constant set, on the same footing as the out-of-set `Cardinality` above. No *query* reaches it, and the reason has moved twice, so it is worth stating as it stands rather than as it was. Phase Z was never involved — Phase Z walks schema property widths, and a `collect(...)` element comes from an expression. `age.rejectUnservedQueries` was the reason next, dropping every list column; since bd `gqlc-p6cb` it judges a list column by its ELEMENT and stands aside on a temporal one, so it closes nothing here (`TestAgeGateAnswersBeforePrepare` measures that). What closes the path today is the same thing that closed the column row's: AGE's dialect gate refuses every temporal constructor on the query TEXT, so no text produces a temporal list element on the one backend that refuses a kind at all. | Phase A |
| `ErrParamNameCollision` | Two parameters of one query mangling to one `Params` field. | Phase B |
| `ErrOutOfC6Scope` | A parameter whose name mangles to no Go field name — `$_`, `$__`, any name of nothing but underscores — in a query binding two or more parameters. Arity-conditional because the emission is: the no-parameter and one-parameter forms take the bare typed argument and derive no identifier from the parameter name at all, so `$_` is served there and stays served. Deferred rather than permanent, which is why it sits here and not under an unrepresentability: a stage spelling `Params` fields positionally would admit it. Before `gqlc-2m2v` the two-or-more form emitted a struct field with no name and a bind expression reading `arg.,`, and `go/format` refused the file as `ErrFormatFailure` — an error naming a template bug, handed to an author whose query was what went wrong. | Phase B |
| `ErrRowFieldCollision` | Two columns of one query deriving one `Row` field. | Phase B |
| `ErrUnrepresentableTemporal` | A projected column whose temporal kind the target's type table has no carrier for. Phase B, not Phase A: Phase A does not ask the type table about temporal kinds, so the refusal lands at the row-field derivation site. **Assembled** since bd `gqlc-dy40s`, and it was a fixture until then: `unrepresentable_temporal_duration_column` projected `duration.between(...)` at `apache-age-pgx-v5`, the last temporal spelling AGE's dialect gate did not hold. Closing that gap refuses the text ahead of the carrier, so the fixture was deleted and the sentinel entered `assembledOnlySentinels`. The case is `column-temporal`, and it differs from the fixture in its argument as well as its form — its kind names no member of the constant block, so it is refused on every backend rather than on the one with no carrier for a duration. | Phase B |
| `ErrIdentifierCollision` | Two exported top-level identifiers colliding across the seven swept sources — the emitter's own package-scope declarations, entity structs, decode helpers, method names, `<Method>Params`, `<Method>Row`, edge-union interfaces. The first source is seeded from the `scopePackage` half of the reserved set: a `NODE TYPE Queries` or an edge-union interface deriving `ReadQuerier` redeclares a name `db.go` or `querier.go` already holds, which the Phase A gate does not see because that one reads a query's name (`gqlc-e6mh`). The `scopeMethod` half — `WithTx`, `Begin`, `Commit`, `Rollback`, `EnsureGraph`, `DropGraph` — stays out: those are methods on `*Queries` or `*Tx` and share no scope with a package-level type. The seeded half is uniform across targets while three of the declarations behind it are not — `DBTX` and `SessionInit` come from the Apache AGE emission alone, and `UUID` from `neo4j-go-v6` alone, so seeding them refuses a name the other targets leave free. A false refusal, taken per D2 Resolved rather than admitting an input under one target and refusing it under another. §6 enumerates all twenty-three rows with the target each is declared by and the target each would actually break. | identifier sweep |
| `ErrFormatFailure` | An emitted file whose raw contents `go/format.Source` rejects. Assembled: a `NamedQuery.Name` that is not a Go identifier — `"Fetch Me"` — which is emitted as a method name and refused by the formatter with every template correct. `Name`'s doc says "must already be a valid exported Go identifier ... Enforced by the queryfile front end; Generate does not re-validate", and that is the pipeline-vs-contract argument §5.1 rejects: `Name` is an exported field of an exported struct, so what the front end enforces does not bound what a caller hands over, and no gate between the envelope and emission looks at it again. The case is `format-failure-query-name`. Not a phase of `Prepare` at all, which is what makes this row unlike every other one here: `Prepare` accepts the input, the emission renders it, and the refusal is the formatter's verdict on bytes this package produced. A reader who wants the check that *should* have caught it is looking for a `Name` validation on the envelope, not for a missing arm in a switch. | `Finalise` (post-emission, `emit.go`) |

## 3. Branches no input reaches

Checks that carry a sentinel but that no schema, no query, no CLI option
and no `Input` an out-of-package caller can assemble will fire — §5.1
gives the full rule. Two reasons, and which one applies matters when
reading a fail-site:

- **Shadowed.** An earlier check applies the same predicate to the same
  value, so the earlier one always answers first. The row names the
  winner and the input it wins on. Every row below is one of these.
- **Total.** The branch is the default arm of a switch whose cases
  already name every *inhabitant* of the switched type, so no value of
  that type reaches it. No row carries this, and §5.1 step 5 says why
  none of the switches here can: an interface with one exported
  implementation is open to any package that embeds it, and a named
  integer enum is an integer. Neither has a count to take. A default arm
  that looks total is a shadowed one whose shadow has not been named
  yet.

A branch that *is* reached but faults before its `return` completes is
neither. It is reached, so its row belongs in §2 with the input that
reaches it, and the fault is a bug to file against that site — not a
reason to record the branch as dead. A tag there measures the absence of
a working test, which is the fail-open move this section exists to make
expensive.

An invariant a layer above merely *maintains* is not on that list, and
this is the correction `gqlc-h4ug` made. `Input`, `NamedQuery` and every
`resolver.Resolved*` variant are exported structs with exported fields,
so a caller assembles one without going through the parser or the
resolver at all; "the resolver would never build this" therefore says
nothing about what a branch can be handed. Sixteen rows were measured
from outside the package, found live, and moved to §2 — thirteen resting
on that argument, one (`list-elem-temporal`) on a shadow that turned out
to be a different backend's pre-gate and to cover the query path only,
and two (`column-unknown-variant`, `list-elem-unknown-variant`) on a
totality claim over a sum that has no inhabitant count to take: an
unexported marker method is not a seal, because Go promotes it through
an embedded variant.

They are listed because §2 claims to carry every construct in each
sentinel's catchment, and a reader matching a fail-site in `prepare.go`
against §2 would otherwise find nothing. They are listed separately
because §5 step 3 asks for a negative fixture per sentinel, and a
sentinel whose every branch is here would have no fixture to offer.

Each branch is named rather than described: the site name in column two
is the `//gqlc:unreachable <site>` tag above that branch's `return`, and
the tags and this table are held equal in both directions. The site name
replaces §2's phase column here because several of these branches are
reached from more than one phase — `list-elem-unknown-variant` hangs off
both Phase A's probe and Phase B's plan build — so a single phase name
would be false.

**The claims below are measured, but the measurement is weaker than the
claim.** `TestSentinelTaxonomy` runs the fixture conformance suite, the
assembled-`Input` suite, the two backend suites and the CLI under
coverage of `internal/codegen`, and fails if any tagged branch executes;
§5.1 gives the rule and the reason the sweep excludes this package's own
tests. What that establishes is that *no test binary outside this
package reaches the branch today* — which is not *no input reaches it*,
and the gap is satisfiable by omission: a row whose reaching input nobody
has thought to write down measures exactly the zero a genuinely dead
branch measures. The sweep is a tripwire on these rows, not a proof of
them.

So the "why" column does the work the fence cannot: name the facts the
claim rests on, so a reader who changes one knows what they are changing,
and so a reviewer has an argument to attack rather than a number. If a
fact moves and the branch goes live, the coverage assertion goes red and
names the row — that half the fence does hold.

| Sentinel | Fail-site | Branch | Why it cannot fire |
|---|---|---|---|
| `ErrOutOfC6Scope` | `param-type-invariant` | A parameter type Phase B admits only in its representable form. | Shadowed. `Prepare` runs `phaseAAdmit` unconditionally before `phaseBDerive`, and Phase A's parameter loop applies the same `ResolvedProperty` type assertion to the same `q.Validated.Parameters` slice, refusing with the same sentinel. The two are the same assertion on the same value, so whatever fails one fails the other and Phase A answers first — the argument needs no enumeration of what can fail it, which is as well, since `resolver.ResolvedType` is open (§5.1 step 5) and no enumeration exists. Measured: an assembled `Input` carrying a `ResolvedScalar` parameter returns Phase A's message, and so do one carrying a `*resolver.ResolvedProperty` and one carrying a `struct{ resolver.ResolvedProperty }`. A nil parameter type reaches Phase A's site too and is refused there; that is a fact about Phase A's fail-site, not a second reason this one is dead. That assertion is not the only thing holding this branch dead, and the difference is what a reader editing Phase A needs: a failed assertion leaves `prop` at its zero value, and Phase A's width check `tm.Property(prop.Type)` refuses that zero value as well, so deleting Phase A's assertion alone does NOT wake this branch. Measured by deleting Phase A's parameter gates and running the §3 fence: with the assertion gone the fence stays green, and with the width check gone as well it reds naming this row. The check standing between them does not participate — the fence reds with it still in place, because a zero `PropertyType` carries none of the prefixes `Kind` tests for and so reads as `KindScalar`, which that check admits. Measured over `unimplementedTypeKind`, which occupied that position until `gqlc-x2uy` deleted it; `recordFieldLegality` stands there now and admits the zero value for the same reason, so the measurement carries over and the shadow has one fewer thing standing in it than when it was taken. So the two gates are not interchangeable and the shadow is not the assertion's alone: the width check is what keeps this branch unreached, and the assertion is what decides WHICH sentinel a non-property parameter meets. Deleting the assertion and keeping the width check reds `out_of_c6_scope_non_property_parameter` in the conformance corpus instead, which then refuses through `ErrUnrepresentableWidth` over an empty width rather than through this row's sentinel. |
| `ErrAliasRequired` | `row-field-alias` | A column whose text yields no row-field name at derivation. | Shadowed. Phase A calls `rowFieldName` over the same `q.Validated.Columns` slice and refuses with the same sentinel and the same message. The two calls take one argument, `col.Name`, and `rowFieldName` is a pure function of it, so no column can be admitted by the first call and refused by the second. |
| `ErrOutOfC6Scope` | `column-type-invariant` | A column type matching no arm of Phase B's plan switch. | Shadowed, exactly. `Prepare` runs `phaseAAdmit` unconditionally before `phaseBDerive`, and Phase A's column switch names the same eight variants Phase B's does over the same `q.Validated.Columns` slice — so whatever falls through Phase B's default falls through Phase A's, and Phase A answers first. The shadow rests on the two arm lists being equal, not on the set that falls through them being small — which matters, because that set is unbounded: `resolver.ResolvedType` is sealed against nothing an out-of-package caller can write (§5.1 step 5), so pointer forms, embedded promotions and anything else a caller invents all fall through both switches alike. Measured, each landing on Phase A's message and the §2 `column-unknown-variant` row: `*resolver.ResolvedNode`, and `struct{ resolver.ResolvedNode }` declared outside `internal/resolver`. A nil column and all eight typed-nil pointer forms reach that same Phase A site and are refused there. This branch goes live only if Phase B's switch loses an arm Phase A's keeps, which is the deletion it fences. |

## 4. Declared and deliberately unreachable

Sentinels the package declares but keeps out of `allSentinels`, so no
negative fixture is required of them.

What is required of them instead is that the claim be true.
`TestExcludedBranchesAreUnreached` measures every branch returning a
sentinel in this table against the corpus coverage profile and fails if
one of them executes — the same measurement §3's tagged branches get, off
the same profile. Until it existed this table was the fence's one
exemption that bought silence: a §4 row is skipped by
`TestUnreachedBranchesAreUnreached`, which owns tagged branches, and
exempted by name from `TestReachableBranchesAreReached`, so a live
sentinel could be retired out of `allSentinels` and parked here with
nothing to say otherwise (`gqlc-4r01`).

The first thing that measurement found was a false row in this table.

| Sentinel | Why it is out of the reachable set |
|---|---|

**The table is empty, and that is a result rather than an oversight.**
The section's machinery stays because the exemption it grants is real
and a future sentinel may earn it; what is gone is the only row that
ever claimed it.

That row was `ErrFormatFailure`, and it took three falsifications to
retire. The first two were template bugs an ordinary user schema
reached. `gqlc-2m2v`: a query binding two or more parameters, one of
them `$_`, emitted a `Params` struct field with no name and a bind
expression reading `arg.,`, refused by `go/format` on all three targets.
`gqlc-9xiz`: a declared property of type `LIST<RECORD>` or
`LIST<LIST<RECORD>>` on `apache-age-pgx-v5`, where the decode helper's
name was derived from the carrier `map[string]any` and the brackets
survived into `agtypeProperty(props, "rows", agtypeListOfNullableMap[string]any)`,
which parses as an index expression inside an argument list.

Neither of those moved the row, and the argument for keeping it was
better than it looks: both were repaired at the template, so the claim
was true again each time, and a *negative fixture* for either would have
been a well-formed schema whose correct emission does not parse — a live
defect frozen into the corpus, due for deletion the day it was repaired.

What that argument missed is that a fixture is not the only witness §5
step 3 accepts. **A construct with no on-disk form takes a case in
`assembled_input_test.go` instead**, and one exists here with nothing
broken at all: a `NamedQuery.Name` that is not a Go identifier is
emitted as a method name and refused by the formatter. `Name` is
documented as "Enforced by the queryfile front end; Generate does not
re-validate" — the pipeline-vs-contract argument §5.1 rejects, since
`Name` is an exported field of an exported struct. Under §5.1's own
criterion the sentinel was reachable the whole time, and the §4 fence
passed only because nobody had written the case. `gqlc-9xiz` promoted
it: §1 and §2 rows above, `assembledOnlySentinels` for the fixture
exemption, and the case `format-failure-query-name` as the witness.

Two lessons worth keeping, since the row is gone and cannot carry them:

- **The exemption this section grants is self-exempting while it is
  believed.** A §4 row makes the fence require *zero* coverage, so the
  test that would prove the row wrong is the one thing the row forbids
  anyone to write. That is why `gqlc-tn96`'s tripwire — asserting the
  `LIST<RECORD>` defect still stood — reddened the fence instead of
  passing. Note which way that cut: the refusal is what forced
  `gqlc-9xiz` to repair the defect rather than document it. The fence
  works; the trap is reading a green §4 as evidence rather than as an
  untested claim.
- **"No fixture is possible" is not "no witness is possible."** Reach
  for the assembled route before concluding a sentinel belongs here.

## 5. Adding, renaming or retiring a sentinel

The fence names the document and the section on failure, so the loop is
short. In full:

1. Declare the sentinel in `internal/codegen/errors.go` with a doc
   comment saying what it refuses. Declare it on its own line: the fence
   pairs each name with the message beside it.
2. Add it to `allSentinels` if a user's input can reach it, and add a row
   to §1 above. Otherwise leave it out of the slice and add a row to §4
   saying why — and note that §4 is measured, not asserted: every branch
   returning that sentinel has to go unexecuted when the corpus runs.
3. Add a §2 row per construct that routes to it, and a negative fixture
   under `test/data/codegen/invalid` for at least one of them, which is
   what `TestSentinelReachability` asks for. A construct with no on-disk
   form — one an `Input` carries rather than a file — takes a case in
   `internal/codegen/conformance/assembled_input_test.go` instead, and
   its row says so. A branch no input can reach goes in §3, tagged and
   fenced per §5.1 — if every construct you can name for a new sentinel
   belongs in §3, the sentinel does not belong in `allSentinels`.

   Where *every* construct routing to a live sentinel has lost its
   on-disk form — the sentinel is still reachable through the package's
   API, but a gate ahead of it now shadows every query text that used to
   arrive — add it to `assembledOnlySentinels` and delete the fixture in
   the same change. Not a later one: until the gate lands, the fixture's
   query still reaches the branch and still passes, so the corpus reds on
   the deletion alone. That is the fail-closed property working, and it
   means the deletion and the gate are one change whether or not you
   wanted them to be.
4. A rename is the same three edits with the old name removed; a
   retirement drops the rows, the slice entry and the fixture together.

The stage specs are not part of this loop. They record what each stage
shipped and take no edit when the set moves.

### 5.1 Deciding whether a branch is §2 or §3

Reachable means **an out-of-package caller reaches it** — a schema, a
query file or a CLI option arriving through `Generate`, or an `Input` a
consumer of this package hands to `Generate` directly. That second
clause is not a loophole, it is the reason `allSentinels` is a closed
set at all: `Input` and `NamedQuery` are exported, so the checks that
guard their fields are this package's contract with its callers rather
than internal invariants, and `internal/cli/pipeline` is not the only
caller such a contract can have. It is what makes the zero-`Cardinality`
row in §2 legitimate — a caller reaches it by leaving a field unset — and
the same clause carries the sixteen rows `gqlc-h4ug` moved out of §3,
each reached by an `Input` no parse produces but any caller can build.
A row of that kind names its reaching value in the "assembled" clause of
its §2 entry, and its coverage is paid by
`internal/codegen/conformance/assembled_input_test.go` rather than by a
fixture on disk, because it has no on-disk form.

It does not mean "some test executes the line", and it does not mean
"the resolver would never build this". The first classifies by
test-presence: a test inside `package codegen` can call an unexported
function with a hand-built `resolver` value and reach a branch no input
can, which is what put three unreachable branches into §2 before
`gqlc-h4ug`, including one whose twin — the identical default arm over
the same sum, one level up — sat in §3 the whole time because it happened
to have no unit test. The second classifies by upstream habit:
`resolver.ValidatedQuery`, `resolver.Column` and all eight `Resolved*`
variants are exported structs with exported fields, so what the resolver
does or does not construct constrains the pipeline and not the contract.
Thirteen §3 rows argued that way, and all thirteen fired the first time
anyone assembled the value by hand. A fourteenth argued a shadow that
held for queries and not for assembled `Input`s, which is the same
mistake read from the other side. Two more argued totality over a sum
they believed closed, which is that mistake one level down:
`resolver.ResolvedType`'s unexported marker is a fact about who may
implement it *from scratch*, and the question is again what a caller can
hand over — which, since Go promotes an embedded variant's unexported
methods, is anything at all. Two rounds of review were spent shrinking
that count from eight to sixteen before anyone asked whether it was a
count. It is not; §5.1 step 5 carries the rule that replaced it.

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
`conformance_test.go`, so it would drop out of the sweep and take every
negative fixture with it — leaving a fence that measured the backends
and the CLI, found every tagged branch unreached, and passed. The sweep
therefore also asserts that the conformance package is in the set it
derived.

The measurement runs in both directions, and the second direction is
what makes §2 a claim rather than an assertion. A tagged branch the
corpus reaches fails, because the tag says nothing reaches it. An
untagged branch the corpus misses fails too, because §2 says every
construct in it is one some input reaches, and a fail-site no fixture
touches is a claim with nothing behind it — either the fixture is
missing or the branch belongs in §3. Both directions name the file and
the line, so neither leaves the reader to work out which branch moved.

The one exemption is a sentinel outside `allSentinels`, which is to say
one with a §4 row: it is documented as unreachable at the sentinel
level, so its fail-sites owe no coverage. The fence takes that exemption
from §4 rather than from a list of its own, so widening it means writing
the row that says why. **No sentinel claims it today** — §4's table is
empty since `gqlc-9xiz` promoted `ErrFormatFailure`, which had been its
only row. Read that as raising the bar on a new one rather than as the
exemption being unavailable: §4 asks for zero coverage, and the case
that would falsify a row is the one thing the row forbids, so a §4 claim
is only as good as the routes its author thought to try.

Only §3 asserts *zero*, and only §3 needs a tag. That is the asymmetry
that survives: the fail-open move — silencing a live refusal by tagging
it — costs an edit to a table whose every row has to argue itself,
while the fail-closed move costs a fixture.

To classify a branch:

1. Write the fixture you think reaches it, under
   `test/data/codegen/invalid`, and run it. Instrument the fail-site if
   the sentinel alone does not tell you which branch answered — several
   branches share a sentinel, and two of them share a message.
2. If no fixture reaches it, assemble the `Input` that does and add a
   case to `assembled_input_test.go`. Reach for the smallest value that
   fires it: an out-of-set `Cardinality`, a `Schema` map keyed by hand, a
   `Validated` shape the resolver has no arm for. Do not stop at "the
   resolver would never build this" — that is a fact about the pipeline,
   and the question is about the contract.
3. If either fires the branch, it is a §2 row, and the test you wrote is
   the coverage step 3 above asks for.
4. If an earlier check answers instead, the branch is **shadowed**. Say
   which check wins, on what input, and why nothing can slip past the
   first and be caught by the second.
5. If the branch is a default arm whose switch already names every
   member a `const` block or a marker interface appears to close, it
   *looks* **total**. Do not count. Those two shapes are the only ones
   switched on in this package, and neither has a bounded inhabitant
   set, so the count cannot come out and the answer is always step 4 or
   a new §2 row.

   An **interface is not sealed by an unexported marker method.** The
   marker stops another package writing an implementation from
   scratch. It stops nothing else, because Go promotes an embedded
   type's unexported methods — so

   ```go
   type mine struct{ resolver.ResolvedNode }   // outside internal/resolver
   var _ resolver.ResolvedType = mine{}        // compiles
   ```

   satisfies the interface in one line, from any package in this module,
   and no `case resolver.ResolvedNode:` arm matches it. The `internal/`
   boundary bounds *where* such a type can be written, not how many
   there are, and it does not shrink the problem: the switch is in
   `internal/codegen`, so anything that can reach the switch can also
   write the type that escapes it. The pointer forms are the same hole a
   size smaller: every variant's marker and `String` take value
   receivers, so `*resolver.ResolvedNode` satisfies the interface too
   and no value arm matches it either. Both reach the
   default. There is no number to write down — the set is open for as
   long as one implementation is exported, and that is the condition
   the seal cannot touch.

   What that leaves the default arms is a posture rather than a
   consequence, and `gqlc-edze` picked one: they **refuse** what reaches
   them, and their rows stay in §2 permanently. The alternative on the
   table was to normalise pointer forms back to their value forms at
   `Prepare`'s boundary so the switches saw eight shapes only. It closes
   nothing — an embedder is neither a value form nor a pointer form, so
   it arrives at the default either way — and it would have bought a
   narrower default arm sitting in front of the same open set. Refusing
   costs the arm the ability to name its offender by asking it, which is
   why the render goes through `ResolvedTypeName` rather than
   `t.String()`; the two §2 rows carry that.

   A **named integer enum is an integer.** A `const` block names six
   `resolver.Temporal`s and a `resolver.Temporal` holds whatever an
   `int` holds; an out-of-package caller writes `resolver.Temporal(6)`
   or `codegen.Cardinality(7)` with no conversion the compiler
   objects to. §2 carries a row for each, reached exactly that way.

   So step 5 has no outcome of its own. Fall back to 4 and name the
   check that answers first, or write the case that reaches the branch
   and give it a §2 row.
6. If the branch runs but faults before its `return` completes, it is
   not a §3 row at all. It is reached, so it belongs in §2 with the input
   that reaches it, and the fault is a bug to file against that site.
   Tagging it records the absence of a working test as the absence of an
   input, which is the fail-open move this section is built to price.
7. For 4 and 5: tag the `return` with `//gqlc:unreachable <site>`, add
   a §3 row naming the same site, and write the "why" so it names the
   facts it depends on rather than asserting a conclusion. The fence
   holds tags and rows equal in both directions and holds both against
   the coverage measurement, so a branch tagged without the row, a row
   naming no tag, and a tag over a branch the corpus reaches are three
   separate failures with three separate messages.

## 6. The reserved identifier set

§2's identifier-sweep row seeds source 0 from a fixed set: the exported
names a generated package declares because the emitter fixes them,
whatever the batch contains. A query or a schema element deriving one of
them fails with `ErrIdentifierCollision`.
`TestReservedSetSectionMatchesTheReservedRows` holds this table against
`reservedIdentifiers` in both directions, and holds the *Scope* and
*Declared by* cells against the columns the corpus measures, so this is
the set rather than a summary of it. *Breaks on* is derived from those
two rather than measured: it is *Declared by* for a package-scope row
and `no target` for a method-scope one.

The last two columns answer different questions. *Declared by* is which
targets emit the declaration, read off the committed goldens by
`TestReservedScopeMatchesTheEmittedGoldens`. *Breaks on* is where a
schema element taking that name would emit a package that does not
compile. They part at method scope: a method occupies no package block
whatever its receiver, so an entity struct of the same name compiles on
every target, and those six rows are defensive against entity names on
all of them. Against query names most of them are not — see below the
table.

Both columns take the target as their axis, not the batch. `ErrNoRows`
and `ErrMultipleResults` are emitted, and so break, only for a batch
carrying at least one `:one` query; they read *every target* because
every target emits them for such a batch.

| Identifier | Scope | Declared by | Breaks on |
|---|---|---|---|
| `Queries` | `scopePackage` | every target | every target |
| `New` | `scopePackage` | every target | every target |
| `Querier` | `scopePackage` | every target | every target |
| `ReadQuerier` | `scopePackage` | every target | every target |
| `WriteQuerier` | `scopePackage` | every target | every target |
| `ErrNoRows` | `scopePackage` | every target | every target |
| `ErrMultipleResults` | `scopePackage` | every target | every target |
| `DBTX` | `scopePackage` | `apache-age-pgx-v5` | `apache-age-pgx-v5` |
| `SessionInit` | `scopePackage` | `apache-age-pgx-v5` | `apache-age-pgx-v5` |
| `Tx` | `scopePackage` | every target | every target |
| `ErrTxDone` | `scopePackage` | every target | every target |
| `WithTx` | `scopeMethod` | every target | no target |
| `Begin` | `scopeMethod` | every target | no target |
| `Commit` | `scopeMethod` | every target | no target |
| `Rollback` | `scopeMethod` | every target | no target |
| `EnsureGraph` | `scopeMethod` | `apache-age-pgx-v5` | no target |
| `DropGraph` | `scopeMethod` | `apache-age-pgx-v5` | no target |
| `Date` | `scopePackage` | every target | every target |
| `Time` | `scopePackage` | every target | every target |
| `LocalTime` | `scopePackage` | every target | every target |
| `LocalDateTime` | `scopePackage` | every target | every target |
| `Duration` | `scopePackage` | every target | every target |
| `UUID` | `scopePackage` | `neo4j-go-v6` | `neo4j-go-v6` |

The set is refused uniformly, so a row is over-broad on a target where
neither an entity nor a query taking that name would collide. *Breaks
on* answers the entity half of that, and the six method-scope rows are
defensive there. On the query half **none of the six is a collision**,
and the first auditor who checks will find that all six compile. That is
not the gate violating its own charter; it is what the reservations are
for.

Since `gqlc-f4hf` a query method is emitted on the unexported core,
`func (q *queries) <Name>`, and the fixed methods sit on `*Queries` or on
`*Tx` — both of which embed that core. Different receiver base types, so
a query named `Commit` redeclares nothing. Measured against a package
declaring `func (q *queries) Commit` beside `func (tx *Tx) Commit`: it
builds, and `go vet` is clean.

What the emission does instead is **succeed and land on the wrong
handle**, silently, in one of two mirror-image ways. A query taking a
name fixed on `*Queries` — `WithTx`, `Begin`, `EnsureGraph`, `DropGraph`
— is shadowed at depth 0 on the root handle and promotes into `*Tx`
unshadowed. A query taking a name fixed on `*Tx` — `Commit`, `Rollback`
— is the mirror: promoted unshadowed onto `*Queries`, shadowed on `*Tx`.

Either way loses something. The first re-opens what the embedded-core
shape exists to close: `tx.Begin(ctx)` compiles again and runs a user
query inside the open transaction, and on `apache-age-pgx-v5`
`tx.EnsureGraph(ctx)` reaches graph lifecycle from a transaction
(`docs/specs/codegen-tx-embedded-querier.md` §6). The second makes the
author's own query callable on `q` and unreachable on `tx`. In both the
compiler says nothing, and neither does vet, so the author
learns which they got at a call site or not at all — whereas the remedy
costs one rename under a clear diagnostic. So the reservation is
package-wide and receiver-blind by decision, not by oversight
(`docs/specs/codegen-tx-embedded-querier.md` §5, superseding the
call-site-ambiguity grounds ruled on `gqlc-3d0l` for the shipped
accessor shape, `docs/specs/codegen-tx-object.md` §9.1).
`reservedIdentifiers` records this at its declaration. Every
`scopePackage` row stands on the collision ground instead, the five
temporal carriers and `UUID` included: on a target whose
emission declares `temporal.go` or `uuid.go` the carrier is a
package-level type, and an entity or query of the same name redeclares
it; none of the six is ever refused for call-site shape.

The §2 sweep seeds source 0 with the `scopePackage` subset alone, so a
method-scope name is not among the identifiers it compares; Phase A's
membership check is what stands between such a query and the
redeclaration, and `TestReservedIdentifiersAreUniformAcrossBackends`
requires it for all twenty-three rows.

On `neo4j-go-v6` the over-broad rows are six of the twenty-three: `DBTX`
and `SessionInit`, which neo4j never declares; `EnsureGraph` and
`DropGraph`, which only `apache-age-pgx-v5` declares — on that target a
query of either name collides, on neo4j neither name is taken on either
half; and `Commit` and `Rollback`, which are over-broad on
`apache-age-pgx-v5` as well, for the receiver reason above rather than
because the target leaves the name free. The five temporal carriers are
not among the six: the neo4j emission declares every one of them, so an
entity or query of any of those names collides there. Neither is `UUID`,
for the same reason and on this target alone. `WithTx` and
`Begin` are not among the six either: every target declares them on
`*Queries`, so refusing a query of either name is the collision rather
than a false refusal.

On `neo4j-go-v5` the over-broad rows are those six and `UUID`, seven of
the twenty-three. That row is the only one whose over-breadth splits two
MAJORS of one backend rather than two backends: `dbtype.UUID` landed in
neo4j-go-driver v6.2.0 and v5.28.4 has no counterpart, so `uuid.go` is
emitted on one of the two and the name is free on the other. It is the
sharpest case of the uniformity cost this section is about, and it is
taken on the same D2 Resolved grounds as `DBTX` — a name that generated
under `neo4j-go-v5` and was refused under `neo4j-go-v6` would make the
reserved set a function of the driver version an author happened to pin.

On `apache-age-pgx-v5` the over-broad rows are three of the twenty-three:
the same `Commit` and `Rollback`, over-broad for the receiver reason
above rather than because the target leaves the name free, and `UUID`,
which this backend refuses as a width and so never declares as a carrier.
The five temporal
carriers sat here too until that backend admitted `DATE`, `LOCAL TIME`
and `DURATION`, and then zoned `TIME`; it now emits `temporal.go` on the
same trigger every target does, so the *Declared by* cells the corpus
measures moved on their own. `LocalDateTime` is the carrier that still
shows what reserves a name is the emission and not the admission: nothing
on this target reaches it, since `internal/graph` has no LOCALDATETIME
property width at all and `typeMap.Temporal` refuses every expression
kind, and it is declared anyway, because `temporal.go` declares the five
together whichever width triggered the file. `UUID` does not ride that
rule, and the difference is the file boundary rather than a decision:
`uuid.go` has one trigger of its own, so a target reaching no UUID emits
no carrier — where a target reaching one temporal width of five emits all
five.

Six, seven and three count the target axis alone. The batch axis moves
them: on a batch with no `:one` query nothing declares `ErrNoRows` or
`ErrMultipleResults`; on a batch whose surface names no temporal width
nothing emits `temporal.go` and so nothing declares the five carriers;
and on a batch whose surface names no UUID nothing emits `uuid.go`.
Every target gains two over-broad rows on a batch of the first shape and
five more again on one of the second — and `neo4j-go-v6` gains the
`UUID` row on one of the third, which the other two targets already
count. Fourteen for a batch of all three shapes under either neo4j major,
ten for an AGE-only one. `NODE TYPE DBTX` is refused
on a name neo4j leaves free, and `NODE TYPE UUID` on one that
`neo4j-go-v5` and `apache-age-pgx-v5` leave free — both taken per D2
Resolved, one uniform set rather than a name that generates under one
target and is refused under another.
