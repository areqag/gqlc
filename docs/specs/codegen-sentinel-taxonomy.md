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
sentinel outside `allSentinels`, which is to say a §4 row, since a
sentinel documented as deliberately unreachable has no coverage to owe.

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
| `ErrInvalidCardinality` | A query whose `Cardinality` is the zero value. | batch admission |
| `ErrDuplicateQueryName` | Two queries in one batch under one name. | batch admission |
| `ErrDuplicateSourceFile` | Two queries from distinct source paths sharing a basename. | batch admission |
| `ErrIdentifierCollision` | A query name matching an identifier the emission reserves (`Queries`, `New`, `WithTx`, `DBTX`, `EnsureGraph`, …). | Phase A |
| `ErrExecOnProjection` | `:exec` on a query with one or more projected columns. | Phase A |
| `ErrCardinalityShapeMismatch` | `:one` or `:many` on a query with no projected columns, read or write. | Phase A |
| `ErrOutOfC6Scope` | Query text carrying a backtick, which no Go raw string can hold. | Phase A |
| `ErrAliasRequired` | A column whose text is neither a bare identifier nor a property access. | Phase A |
| `ErrOutOfC6Scope` | A non-property parameter — whole node, whole edge, scalar literal, list, temporal expression, or unknown. Post-v1. | Phase A |
| `ErrUnrepresentableEdgeUnion` | An edge-union column two of whose candidates carry the same label. | Phase A |
| `ErrUnrepresentableEdgeUnion` | An edge union reached through a list element chain, two of whose candidates carry the same label. | Phase A |
| `ErrInvalidCardinality` | A query whose `Cardinality` is neither the zero value nor a member of the closed set. Assembled: `Cardinality(7)`. `queryfile.parseCardinality` yields only the three members or refuses the annotation, so no parse produces a fourth. | Phase A |
| `ErrUnrepresentableWidth` | A projected column whose property width the target's type table has no carrier for. Assembled: a `Validated.Columns` entry carrying a width no schema property declares — Phase Z walks the schema, so a column backed by a declared property loses there first. | Phase A |
| `ErrUnrepresentableWidth` | A query parameter whose property width the target's type table has no carrier for. Assembled, on the same argument: the resolver draws a parameter's `ResolvedProperty` from a schema property or from `callProjectionType`, and both yield widths Phase Z has already passed. | Phase A |
| `ErrUnrepresentableWidth` | A list element whose leaf carries a property width the target's type table has no carrier for. Assembled: a `ResolvedList` over a `ResolvedProperty`, a shape `resolveType` has no arm for. Phase B repeats the walk over the element it splits off a schema LIST property; that element's width is one Phase Z has already passed. | Phase A |
| `ErrOutOfC6Scope` | A column, or a list element leaf, referencing a node or edge type the schema does not declare. Assembled: the resolver resolves against the same schema Phase Z indexed and commits only declared types. | Phase A |
| `ErrOutOfC6Scope` | An edge union with fewer than two candidates, or one naming a candidate the schema does not declare. Assembled: the resolver collapses a lone candidate to `ResolvedEdge` and commits only declared edges. | Phase A |
| `ErrOutOfC6Scope` | A column whose resolved type matches no arm of the switch over `resolver.ResolvedType`. Assembled: the pointer form of a variant, `&resolver.ResolvedNode{}` — the marker and every variant's `String` take value receivers, so `*ResolvedNode` satisfies the interface while `case resolver.ResolvedNode:` does not match it. The fall-through set is not "the eight and their pointers": `resolver.ResolvedType` is sealed against nothing an out-of-package caller can write, because Go promotes an embedded variant's unexported marker and `struct{ resolver.ResolvedNode }` therefore satisfies the interface from any package and lands here too (§5.1 step 5). Two shapes reach this arm and fault instead of refusing (`gqlc-edze`): the nil interface, and every one of the eight typed-nil pointer forms — Go emits a nil check before a value method reached through a pointer, so even the zero-sized `(*resolver.ResolvedUnknown)(nil)` panics in the `String()` call inside the `return`. A non-nil pointer form is what pays this row's coverage. | Phase A |
| `ErrOutOfC6Scope` | A list element whose resolved type matches no arm of `buildListElemPlan`'s switch. Assembled: the same pointer form under a `ResolvedList`, on the same argument — that switch names the same eight arms and its default is open in the same two directions, pointer forms and embedded promotions. It carries the same faults: all eight typed-nil pointer forms panic in the `String()` call inside the `return`, and so does a `resolver.ResolvedList{Element: nil}`, whose nil element reaches the same line and dereferences there (`gqlc-edze`). | Phase A |
| `ErrUnrepresentableTemporal` | A list element whose temporal kind the target's type table has no carrier for. Assembled: a `resolver.Temporal` outside its own constant set, on the same footing as the out-of-set `Cardinality` above. No *query* reaches it, and would need a backend that both refuses a kind and serves list columns: only Apache AGE refuses a kind, and `age.rejectUnservedQueries` drops a query carrying a list column three lines before `age.generate` calls `codegen.Prepare`. Phase Z is not what stops it — Phase Z walks schema property widths, and a `collect(...)` element comes from an expression. | Phase A |
| `ErrParamNameCollision` | Two parameters of one query mangling to one `Params` field. | Phase B |
| `ErrRowFieldCollision` | Two columns of one query deriving one `Row` field. | Phase B |
| `ErrUnrepresentableTemporal` | A projected column whose temporal kind the target's type table has no carrier for. Phase B, not Phase A: Phase A does not ask the type table about temporal kinds, so the refusal lands at the row-field derivation site. | Phase B |
| `ErrIdentifierCollision` | Two exported top-level identifiers colliding across the six swept sources — entity structs, decode helpers, method names, `<Method>Params`, `<Method>Row`, edge-union interfaces. | identifier sweep |

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
| `ErrOutOfC6Scope` | `param-type-invariant` | A parameter type Phase B admits only in its representable form. | Shadowed. `Prepare` runs `phaseAAdmit` unconditionally before `phaseBDerive`, and Phase A's parameter loop applies the same `ResolvedProperty` type assertion to the same `q.Validated.Parameters` slice, refusing with the same sentinel. The two are the same assertion on the same value, so whatever fails one fails the other and Phase A answers first — the argument needs no enumeration of what can fail it, which is as well, since `resolver.ResolvedType` is open (§5.1 step 5) and no enumeration exists. Measured: an assembled `Input` carrying a `ResolvedScalar` parameter returns Phase A's message, and so do one carrying a `*resolver.ResolvedProperty` and one carrying a `struct{ resolver.ResolvedProperty }`. A nil parameter type reaches Phase A's site too and panics there (`gqlc-edze`); that is a fact about Phase A's fail-site, not a second reason this one is dead. |
| `ErrAliasRequired` | `row-field-alias` | A column whose text yields no row-field name at derivation. | Shadowed. Phase A calls `rowFieldName` over the same `q.Validated.Columns` slice and refuses with the same sentinel and the same message. The two calls take one argument, `col.Name`, and `rowFieldName` is a pure function of it, so no column can be admitted by the first call and refused by the second. |
| `ErrOutOfC6Scope` | `column-type-invariant` | A column type matching no arm of Phase B's plan switch. | Shadowed, exactly. `Prepare` runs `phaseAAdmit` unconditionally before `phaseBDerive`, and Phase A's column switch names the same eight variants Phase B's does over the same `q.Validated.Columns` slice — so whatever falls through Phase B's default falls through Phase A's, and Phase A answers first. The shadow rests on the two arm lists being equal, not on the set that falls through them being small — which matters, because that set is unbounded: `resolver.ResolvedType` is sealed against nothing an out-of-package caller can write (§5.1 step 5), so pointer forms, embedded promotions and anything else a caller invents all fall through both switches alike. Measured, each landing on Phase A's message and the §2 `column-unknown-variant` row: `*resolver.ResolvedNode`, and `struct{ resolver.ResolvedNode }` declared outside `internal/resolver`. A nil column and all eight typed-nil pointer forms reach that same Phase A site and panic there (`gqlc-edze`). This branch goes live only if Phase B's switch loses an arm Phase A's keeps, which is the deletion it fences. |

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
   what `TestSentinelReachability` asks for. A construct with no on-disk
   form — one an `Input` carries rather than a file — takes a case in
   `internal/codegen/conformance/assembled_input_test.go` instead, and
   its row says so. A branch no input can reach goes in §3, tagged and
   fenced per §5.1 — if every construct you can name for a new sentinel
   belongs in §3, the sentinel does not belong in `allSentinels`.
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
`conformance_test.go`, so it would drop out of the sweep and take all
twenty-nine negative fixtures with it — leaving a fence that measured
the backends and the CLI, found every tagged branch unreached, and
passed. The sweep therefore also asserts that the conformance package is
in the set it derived.

The measurement runs in both directions, and the second direction is
what makes §2 a claim rather than an assertion. A tagged branch the
corpus reaches fails, because the tag says nothing reaches it. An
untagged branch the corpus misses fails too, because §2 says every
construct in it is one some input reaches, and a fail-site no fixture
touches is a claim with nothing behind it — either the fixture is
missing or the branch belongs in §3. Both directions name the file and
the line, so neither leaves the reader to work out which branch moved.

The one exemption is a sentinel outside `allSentinels`, which is to say
one with a §4 row: `ErrFormatFailure` is documented as unreachable at
the sentinel level, so its fail-sites owe no coverage. The fence takes
that exemption from §4 rather than from a list of its own, so widening
it means writing the row that says why.

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

   satisfies the interface in one line, from any package, and no
   `case resolver.ResolvedNode:` arm matches it. The pointer forms are
   the same hole a size smaller: every variant's marker and `String`
   take value receivers, so `*resolver.ResolvedNode` satisfies the
   interface too and no value arm matches it either. Both reach the
   default. There is no number to write down — the set is open for as
   long as one implementation is exported, and that is the condition
   the seal cannot touch.

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
