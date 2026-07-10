# Stage 14 spec — Cypher query parser: CALL + procedure signature registry

The implementation brief for Stage 14 of the Cypher implementation of
`query.Parser`. Fourteenth model evolution after Stage 13 per ADR 0004
(test-first, evolving until feature-complete), under the curation
discipline of ADR 0003 and the type-interface boundary of ADR 0005.
Stage 14 is the ninth (and final before Stage 14) stage of the ADR-0007
expansion beyond the read core. It wires the CALL clause — standalone
CALL, `CALL … YIELD`, and in-query CALL — and introduces the **procedure
signature registry** as a second compile-time input alongside the
schema.

CALL is untypeable without knowing the procedure's argument and result
types: `CALL foo(42)` on its own tells the parser nothing about `foo`'s
input arity or its output columns. The TCK acknowledges this with a
background step (`there exists a procedure test.my.proc(in ::
INTEGER?) :: (out :: STRING?)`) that declares the signature per
scenario. Stage 14 turns that declaration into a first-class parser
input: `cypher.New(WithRegistry(reg))` supplies a set of typed
signatures (`name → (in-types, out-types)`); an unknown procedure at
generation time is an error by design (`ErrUnknownProcedure`), the
symmetric analogue of a MATCH against an unknown label.

Stage 14 replaces the last of `ErrUnsupportedClause`. The sentinel
retires entirely: no clause in the read/write surface routes through it
after Stage 14. Two new sentinels join the canonical set —
`ErrUnknownProcedure` (registry miss, incl. unknown YIELD field) and
`ErrProcedureArity` (statically wrong arg count on explicit
invocation). Three bucket-3 error families ride the skiplist
categorically: wrong argument type, missing implicit-invocation
parameter, and aggregate-in-argument.

Stage 14 introduces one new Binding-sum variant (`CallBinding` — the
per-YIELD-column binding, mirroring `UnwindBinding` shape-wise), no new
`Effect` variant (CALL is a Read statement kind), and one new package
(`internal/procsig`) that carries the registry API and a local
`TypeToken` sum decoupled from `query.Type`.

This document is a **delta** against Stages 0–13 (referenced
individually where relevant); everything not stated here carries over
verbatim. Sections appear here only where Stage 14 changes something.

Tracking: bead `gqlc-im1` (GitHub #41). Lands as one graphite branch
(`stage-14-call`) with separated commits (spec + docs → procsig
package → cypher API + WithRegistry option → parser red → parser
green → TCK dir unlock + goldens + skiplist), independently mergeable
as a whole: `just test` is green if this branch lands on `master`
alone (AGENTS.md stacked-branch invariant).

---

## 1. Deliverables

### 1.1 `procsig` package — registry API and local `TypeToken` sum (§3.1)

New package `internal/procsig`, dependency-independent of both `query`
and `schema`. It carries:

- `type TypeToken int` — a local sum for the signature vocabulary the
  TCK corpus uses: `TokenInteger`, `TokenFloat`, `TokenString`,
  `TokenNumber` (an assignable-from marker; NUMBER accepts
  INTEGER-or-FLOAT at the argument-typing site — Call3 exercises it).
  The corpus census attests exactly these four (INTEGER?×47,
  STRING?×30, NUMBER?×4, FLOAT?×2, zero BOOLEAN) — no gold-plating
  (Q2). Any token appearing in a registered signature that is not in
  the sum is a registry-construction error.
- `type Param struct{ Name string; Token TypeToken; Nullable bool }`
  — one signature input, in declaration order.
- `type Result struct{ Name string; Token TypeToken; Nullable bool }`
  — one signature output column, in declaration order.
- `type Signature struct{ Name string; Params []Param; Results []Result }`
  — one fully-qualified procedure signature (`Name` is the dotted
  form: `test.my.proc`).
- `type Registry struct{ ... }` — an immutable, keyed-by-Name lookup:
  `func (Registry) Lookup(name string) (Signature, bool)`.
- `func NewRegistry(sigs []Signature) (Registry, error)` — validates:
  every signature name is non-empty and fully-qualified (at least one
  dot); no duplicate names; every param and result name is non-empty
  and unique inside its own list (a signature cannot declare two
  params `in`, or two results `out`); every token is a member of
  `TypeToken`. Returns an error on any violation; the zero `Registry`
  is a valid, empty registry (all lookups miss).

**Why a separate package.** ADR 0007 §I keeps `query` closed to
signature-only vocabulary: adding a `Number` token, or any
signature-specific vocabulary, into `query.Type` would enlarge
query.Type. `procsig` decouples
signature vocabulary from result-column vocabulary: NUMBER is a
signature-time marker (assignable-from INTEGER-or-FLOAT), it is NOT a
runtime result type, and it must never appear on the wire as a column
type. Keeping it in `procsig` and bridging (§3.2) preserves the
type-interface ADR 0008 record. Q2 fold-in.

**Why not import `query.Type` directly.** A `procsig` that named
`query.Type` values as its signature vocabulary would either: (a) force
`query.Type` to grow a `TypeNumber` variant (query.Type enlargement,
rejected by Q3 ruling), or (b) omit NUMBER and silently coerce the
registry to INTEGER-or-FLOAT (silent info drop; a future stage that
wants to enforce NUMBER-vs-INTEGER at the fail-site would have nothing
to check against). The local sum keeps signature vocabulary honest
without touching `query`.

**Why NUMBER is a first-class token, not a coercion.** Call3 [1]/[2]
call `test.my.proc(in :: NUMBER?)` with an INTEGER value; [3]/[4] with
a FLOAT. Both accept — that IS the semantics of NUMBER (assignable
from either). Modelling NUMBER as a first-class token lets the argument
type check say "42 is INTEGER, NUMBER accepts INTEGER, accept"; a naive
"NUMBER means INTEGER" or "NUMBER means FLOAT" coercion would reject
one of the two. See §4.5 (arg-type check) for the exact rule.

**On-disk format: OUT of Stage 14.** The registry is an in-memory API
in Stage 14: `NewRegistry([]Signature{...})` from Go, populated by the
godog step from parsed feature-table rows. A textual file format (a
`.procsig` or YAML surface for a real CLI user) is intentionally
deferred. It is filed as a follow-up bead at close-out; the Stage 14
close-out (`gqlc-cta`) can proceed on the in-memory API alone, since
codegen consumes `procsig.Registry` values directly.

### 1.2 `CallBinding` — new Binding-sum variant (§3.2)

`Binding` gains a fifth variant, `CallBinding`, carrying:

- `Variable() string` — the YIELD alias (or, if no `AS`, the source
  result field's name; §4.2).
- `Procedure() string` — the fully-qualified procedure name from the
  registry lookup (e.g. `test.my.proc`).
- `SourceField() string` — the signature-declared result column name
  this binding draws from (e.g. `out`).
- `ResultType() query.Type` — the bridged Stage-6 result type (§3.2)
  for the declared token: `TokenInteger` → `TypeInt`, `TokenFloat` →
  `TypeFloat`, `TokenString` → `TypeString`, `TokenNumber` →
  `TypeUnknown` (the wire type — signature-time NUMBER carries no
  honest result-column type; codegen later consults the
  registry directly).
- `Nullable() bool` — the signature's `?` on the source result field
  (Call1 [5]: `label :: STRING?` → `Nullable: true`).
- `Kind() BindingKind` — returns a new `BindingCall` constant.

`NewCallBinding(variable, procedure, sourceField string, resultType
Type, nullable bool)` is the smart constructor. It rejects the empty
variable, the empty procedure, the empty sourceField, and a nil
resultType (normalised to `TypeUnknown`, mirroring `NewUnwindBinding`'s
convention).

**Why a Binding-sum variant, not a per-YIELD-item field on a new
Effect.** CALL YIELD is a **reading** clause: `Query.StatementKind`
stays `StatementRead`, `Part.Effects` stays empty. Each YIELD column
introduces a value into the current part's scope for RETURN or a
downstream WITH to reference; that is precisely what `Binding` models
across the four existing variants. A `CallEffect` would smuggle a Read
shape into the write-side sum and require every downstream
`Part.Effects` walker to filter it out. The Binding path composes with
Stage 6's `refType` (a bare `RETURN out` on a CallBinding types via
the existing binding-ref classifier — §4.4) and Stage 8/9's kind-
conflict sweep (a same-name entity or unwind binding preceding a
CallBinding is a kind conflict — §4.7).

**Why not one CallBinding per procedure call carrying all YIELD
columns.** Each YIELD column has its own type and its own AS-alias-
overridable name. Mirroring `UnwindBinding` (one binding = one
variable) keeps the model uniform: `Ref{n}` on a call-yielded `n`
resolves the same way as `Ref{n}` on an unwind-introduced `n`, and the
`buildPart` referential-integrity sweep does not need to peek inside a
composite binding to know what variables it introduces.

**Why not a fifth kind on the existing entity/path/unwind Binding
interface, no new kind constant.** `BindingKind` gains `BindingCall`
so a downstream consumer switching on `Kind()` can distinguish the
call-yielded binding from an unwind-yielded scalar. Collapsing them
under `BindingUnwind` would silently hide the origin (was this from
UNWIND `[1,2,3]` or from CALL YIELD?) — a category the wire encoding
must carry (§1.3).

### 1.3 Wire encoding — `CallBinding` and `BindingKind` extension (§3.3)

`CallBinding.MarshalJSON` renders (in field order):

- `"kind": "call"` (from `BindingKind.String()` — the single-source
  discriminator convention).
- `"variable": <string>`
- `"procedure": <string>` — fully-qualified name
- `"sourceField": <string>` — the signature-declared result column
- `"resultType": <Type>` — always emitted, using `Type`'s tagged-union
  wire shape (`TypeInt` etc.). For `TokenNumber` the field is
  `TypeUnknown` (a signature-time NUMBER carries no honest column
  type at Stage 14).
- `"nullable": <bool>` — always emitted (matches every other
  Binding variant's shape).

`BindingKind` gains one enum entry `BindingCall` (int-backed, next
after `BindingUnwind`), and `String()` gains a `"call"` case. No
other Binding variant's wire shape changes.

**Why `sourceField` is on the wire even when it equals `variable`.**
A bare `YIELD out` binds a variable named `out` whose sourceField is
also `out`; a `YIELD out AS x` binds `x` whose sourceField is `out`.
The distinction is load-bearing at codegen: the generated method reads
column `out` from the driver's result and returns it under the caller-
visible name `x`. Collapsing them at Stage-14 wire (emitting only
`variable` when it happens to equal `sourceField`) would let a rename
silently unshadow; keeping both fields always keeps the wire
unambiguous.

### 1.4 Procedure registry API on `cypher.New` (§3.4)

`cypher.New` becomes `func New(opts ...Option) query.Parser`. The
option surface (functional options — Q6 ruling (b)):

```go
type Option func(*config)
func WithRegistry(r procsig.Registry) Option
```

`cypher.New()` with no options is equivalent to
`cypher.New(WithRegistry(procsig.Registry{}))` — an empty registry.
`Parse`'s signature is untouched: `Parse(r io.Reader) (query.Query,
error)`. The registry lives on the `parser` value between construction
and Parse, and every scenario builds its own parser (the godog step
`there exists a procedure` populates the per-scenario registry, then
the executing-query step calls `cypher.New(WithRegistry(reg)).Parse`).

**Why functional options and not a required `Parse(reader, registry)`
signature.** Ruling Q6(b). A registry-less caller (any Stage-0..13
consumer) keeps calling `cypher.New()` and gets exactly the pre-Stage-
14 behaviour for every query that does not contain CALL. A query that
DOES contain CALL against an empty registry fails at
`ErrUnknownProcedure` on the first CALL (§4.1). The
backwards-compatibility posture is honest: no CALL in Stage-0..13, so
no lookup is possible, so the sentinel is unreachable in the existing
call-sites; only new call-sites (Stage 14 tests, and future
downstream) opt into the registry.

**Why Parse's signature does not grow the registry parameter.** The
registry is a compile-time input, and — like the schema — it is
supplied to the constructor, not the per-parse call. Also,
`query.Parser` is an interface (`internal/query/parser.go`): widening
its `Parse` signature would break the interface's Stage-0 contract
that mirrors `schema.Parser`. Keeping the option on the constructor
preserves the interface `query.Query` surface.

### 1.5 Retirement of `ErrUnsupportedClause`

The sentinel retires entirely. Q1 ruling (c): the clause IS supported
after Stage 14; repurposing the sentinel for the registry-miss case
would lie about the category. Fail-site inventory (before this stage):

- `EnterOC_InQueryCall` — replaced by pattern collection (§4.1).
- `EnterOC_StandaloneCall` — replaced by pattern collection (§4.1).

Both current `mustReject` pins targeting `ErrUnsupportedClause`
(standalone CALL and in-query CALL from Stage 13) retire (§1.8). The
sentinel is deleted from `errors.go`, `allSentinels`, and the errors
documentation.

### 1.6 New sentinels — `ErrUnknownProcedure` and `ErrProcedureArity`

Two new sentinels join the canonical set:

- `ErrUnknownProcedure` — the registry has no signature named `<n>`
  (or the registry is nil / empty), OR a YIELD clause references a
  result field the signature does not declare. One sentinel covers
  both category-siblings (Q1 ruling); the wrapped message distinguishes
  the case:
  - `unknown procedure: <name>` (procedure-name miss)
  - `unknown procedure result field: <field> on <procedure>` (YIELD
    references a field the signature does not declare)
  Fail-sites: `collectCall` at registry lookup (procedure miss) and
  YIELD-field enumeration (field miss). Both fire before the CALL's
  bindings enter `curPart.bindings`, so a failed lookup leaves the
  part unchanged.

- `ErrProcedureArity` — an explicit invocation (`CALL foo(a, b, c)`
  where the signature declares two params). Statically provable from
  the registry, so accepting-and-deferring would drop a fact the
  parser can honestly detect. Fires only on explicit invocations
  (parens present); implicit invocations (`CALL foo`) pass arguments
  by parameter binding at runtime, so their arity is uncountable at
  parse time (Q4 ruling). Fail-site: `collectCall` after signature
  lookup, before argument mining. Wrapped message: `procedure arity
  mismatch: <name> expects <expected> arguments, got <actual>`.

**Why one `ErrUnknownProcedure` sentinel covers both name and field
misses.** The category is "registry miss" — a name the registry does
not carry, whether the name is the procedure or a field on the
procedure's result signature. A per-fail-site sentinel would multiply
the canonical set without expanding the caller's decision surface: a
caller who wants to hand back "unknown procedure or unknown field"
diagnostics reads the wrapped message either way. Keeping one sentinel
matches the Stage-12/13 convention (`ErrVariableKindConflict` covers
node-vs-edge and node-vs-path — one category, one sentinel).

**Why `ErrProcedureArity` is its own sentinel, not folded into
`ErrUnknownProcedure`.** The two are different categories: unknown
procedure means "no signature to check against", arity mismatch means
"signature present, this call is wrong shape". Codegen callers may
want distinct diagnostics (procedure-not-found vs. wrong-argcount-in-
call), and the wrapped-message distinction alone would force callers
to string-parse. Q4 ruling.

**YIELD-shadows-scope and intra-YIELD collision** (Call1 [15], Call5
[5]/[6]) reuse `ErrVariableKindConflict`. The precedent is
Stage-9 same-kind collision (build.go:114-121, unwind-vs-unwind and
path-vs-unwind fire it), so the operative semantic is "binding
collision in scope," not strictly node-vs-edge. Q4 ruling. The check
must cover:
- Intra-YIELD (Call5 [5]/[6]): `YIELD a, b AS a` and `YIELD a AS c, b
  AS c`. Two YIELD items binding the same variable name.
- Imported-scope (Call1 [15]): `WITH 'Hi' AS label CALL test.labels()
  YIELD label`. The imported name from the preceding WITH's exported
  set collides with a new CallBinding's variable.

The sentinel's wrapped message text does not change at Stage 14 — the
existing "variable used as both node and edge" text is a stringer
label, not the sentinel's identity. Q4 ruling.

### 1.7 Corpus wiring

`clauses/call` joins `readCoreDirs` (52 pickles). Every skiplist
entry Stage 14 adds is enumerated per-scenario in §5, with its
bucket rationale.

**Grammar-only rejection wiring.** Call2 [4] (`CALL test.my.proc
YIELD out RETURN out` — in-query implicit invocation, forbidden by
grammar §140) and Call5 [7] (`CALL test.my.proc('Stefan', 1) YIELD *`
— in-query YIELD *, forbidden by grammar §140) both carry
`@skipGrammarCheck` in the TCK. They ride `mustRejectGrammar`
(Stage 13 precedent, parser_test.go:2051) as pinned parse errors;
the corpus scenarios themselves are passing rejections — the ANTLR-
level syntax error surfaces via the listener's `SyntaxError` sink,
producing a non-nil error and the zero `Query`, which `assertRejected`
accepts.

### 1.8 Layer-2 pins

New `mustParse` cases exercising the Stage-14 shapes. Every entry is
verbatim from the TCK unless marked `AUTHORED:` per the parser_test.go
layer-2 rule.

**Verbatim mustParse pins (10):**

- **Standalone CALL, no args no yields** (`Call1 [1]`): `CALL
  test.doNothing()` — with `test.doNothing() :: ()` registered. Part
  has zero CallBindings, empty Returns, `StatementRead`, empty
  Effects. This is the projection-less Read shape Stage 12 opened
  the door for (write clauses with no RETURN); CALL takes it for the
  no-yields case.
- **Standalone CALL implicit (no parens), no args no yields** (`Call1
  [2]`): `CALL test.doNothing` — implicit invocation shape. Zero
  CallBindings, zero refs, same Read/empty-effects posture as [1].
- **In-query CALL, no args no yields** (`Call1 [3]`): `MATCH (n) CALL
  test.doNothing() RETURN n` — one NodeBinding `n`, zero CallBindings,
  one RefProjection `n`, `StatementRead`.
- **Standalone CALL, no args, YIELD-expanded** (`Call1 [5]`): `CALL
  test.labels()` — with `test.labels() :: (label :: STRING?)`
  registered. One CallBinding `label` (procedure `test.labels`,
  sourceField `label`, resultType `TypeString`, nullable `true`).
  Part.Returns holds one RefProjection `label` (signature declaration
  order — §4.3 standalone expansion), `ReturnsAll: false`,
  `StatementRead`.
- **In-query CALL YIELD** (`Call1 [6]`): `CALL test.labels() YIELD
  label RETURN label` — same CallBinding as [5], but Returns is the
  RETURN clause's RefProjection on `label` (RETURN drives the returns
  slice; the YIELD expansion posture applies only to standalone
  without YIELD).
- **In-query CALL with explicit args YIELD** (`Call2 [1]`): `CALL
  test.my.proc('Stefan', 1) YIELD city, country_code RETURN city,
  country_code` — signature two params (STRING?, INTEGER?), two
  results (STRING?, INTEGER?). Two CallBindings (`city`, `country_
  code`); two RefProjections at RETURN. No refs on the CALL part
  (both args are literals).
- **Standalone CALL with explicit args** (`Call2 [2]`): `CALL
  test.my.proc('Stefan', 1)` — same signature. Two CallBindings,
  Part.Returns expanded to two RefProjections in signature order.
- **Standalone CALL YIELD * with args** (`Call5 [8]`): `CALL
  test.my.proc('Stefan', 1) YIELD *` — the YIELD * variant of the
  standalone. Two CallBindings, Part.Returns two RefProjections in
  signature order (`ReturnsAll: true` per §4.3 — a `YIELD *`
  standalone expands like a no-YIELD standalone: implicit `RETURN *`
  over the CallBindings).
- **CALL with NUMBER accepts INTEGER** (`Call3 [1]`): `CALL
  test.my.proc(42)` — signature `(in :: NUMBER?) :: (out :: STRING?)`.
  One CallBinding `out` (resultType `TypeString`, nullable `true`).
  Arg-type check accepts INTEGER against NUMBER (§4.5).
- **CALL followed by WITH followed by CALL** (`Call6 [1]`): `CALL
  test.labels() YIELD label WITH count(*) AS c CALL test.labels()
  YIELD label RETURN *`. Two-part Query. Part 1: one CallBinding
  `label`, WITH exports `c` (from `count(*)`) — the CallBinding
  `label` is NOT exported past the WITH's explicit projection.
  Part 2: one CallBinding `label` (fresh — this part's own
  CALL YIELD), Returns is `RETURN *` (`ReturnsAll: true`) over
  scope `{c, label}`. Pins the two-CALL-across-WITH shape:
  the second CALL's YIELD introduces its own binding; the first
  CALL's `label` binding does NOT leak past the WITH (a WITH
  export list is authoritative; CallBinding participates in the
  Stage-4 export rule verbatim).

**AUTHORED mustParse pins (5):**

- **AUTHORED: CALL inside EXISTS does not populate an outer
  CallBinding** — `MATCH (n) WHERE exists { CALL test.labels()
  YIELD label RETURN label } RETURN n`, with `test.labels() ::
  (label :: STRING?)` registered. Pins the EXISTS suppression on
  CALL: outer Query has one NodeBinding `n`, zero CallBindings,
  one RefProjection `n`, `StatementRead`. The inner CALL is
  suppressed at `subqueryDepth > 0` (§4.6). Same suppression
  posture Stage 11 documents for MATCH-in-EXISTS and Stage 12
  documents for the write set; Stage 14 pins it for the CALL
  variant explicitly because the CallBinding is Read-side (Stage
  12's writeSeen non-flip pin does not exercise the Read-side
  binding suppression).
- **AUTHORED: bound-var CALL argument regression lock** — `MATCH
  (n) CALL test.labels(n.name) YIELD label RETURN label`, with
  `test.labels(in :: STRING?) :: (label :: STRING?)` registered.
  Pins the arg-mining path for a bound variable reference (`n` is
  a bound NodeBinding from the preceding MATCH; `n.name` is a
  property lookup on it). Passes as a regression lock: the CALL
  part's refs list carries `n` (via arg-side `typeExpressionMining`
  — §4.4); `buildPart` finds `n` in scope; one CallBinding `label`;
  RETURN over `label`. The matched pair (§1.8, kill-probe below)
  exercises the unbound side.
- **AUTHORED: unbound-var CALL argument kill-probe** — `CALL
  test.labels(m.name) YIELD label RETURN label`, with `test.labels
  (in :: STRING?) :: (label :: STRING?)` registered (i.e. no
  preceding MATCH — `m` is unbound). This is the MUSTREJECT variant
  (moves to `mustReject`, not `mustParse`); listed here for pairing
  clarity — see mustReject §1.8 below.
- **AUTHORED: standalone-CALL Returns expansion is deterministic
  signature-declaration order** — `CALL test.my.proc(42)`, with
  `test.my.proc(in :: NUMBER?) :: (a :: INTEGER?, b :: STRING?)`
  registered (corpus-attested tokens only — Q2). Pins the ordering
  rule (§4.3): Part.Returns is two RefProjections in the order
  `a, b` — the signature's Results order, not any hash/map order.
  `ReturnsAll: false` (no YIELD present; the expansion is inferred).
  CallBindings match the same order.
- **AUTHORED: YIELD…WHERE arg-mining probe** — `CALL test.labels()
  YIELD label WHERE label = $needle RETURN label`, with
  `test.labels() :: (label :: STRING?)` registered. Pins that a
  grammar-legal YIELD-WHERE (`oC_YieldItems → … ( SP? oC_Where )?`,
  §140 of the grammar) parses: one CallBinding `label`, the WHERE
  clause's parameter `$needle` is mined into the query-wide
  `Parameters` slice with an ExprUse of `TypeUnknown` (parameter
  uses inside WHERE type as the enclosing comparison result,
  Stage 6 §4). Corpus is silent on this shape — the pin is a Q5
  authored kill-probe. If implementation reveals an obstruction
  the spec is amended and the pin re-classified per Q5 escalation;
  the spec does not silently reject.

**Verbatim mustReject pins (2):**

- **CALL unknown procedure standalone** (`Call1 [13]`): `CALL
  test.my.proc` — no signature registered for `test.my.proc`.
  Fail-site: `collectCall` at registry lookup (procedure-name miss).
  Sentinel: `ErrUnknownProcedure`. Wrapped message: `unknown
  procedure: test.my.proc`.
- **CALL unknown procedure in-query** (`Call1 [14]`): `CALL
  test.my.proc() YIELD out RETURN out` — no signature registered.
  Fail-site: same. Sentinel: same. Same wrapped message.

**Verbatim mustReject pins for `ErrProcedureArity` (2):**

- **Explicit CALL too-few args standalone** (`Call1 [7]`): `CALL
  test.my.proc('Dobby')` — signature `(name :: STRING?, in ::
  INTEGER?)` (arity 2), call has arity 1. Sentinel:
  `ErrProcedureArity`. Wrapped message: `procedure arity mismatch:
  test.my.proc expects 2 arguments, got 1`.
- **Explicit CALL too-many args standalone** (`Call1 [9]`): `CALL
  test.my.proc(1, 2, 3, 4)` — signature `(in :: INTEGER?)` (arity
  1), call has arity 4. Sentinel: `ErrProcedureArity`. Wrapped
  message: `procedure arity mismatch: test.my.proc expects 1
  arguments, got 4`.

Note Call1 [8]/[10] are in-query variants of [7]/[9]; they are
passing rejections through the same sentinel but do not need their
own Layer-2 pins — the four scenarios exercise one fail-site (the
arity check runs before explicit-vs-in-query branches diverge). The
two standalone pins are sufficient.

**Verbatim mustReject pins for `ErrVariableKindConflict` (3):**

- **YIELD shadows imported scope** (`Call1 [15]`): `WITH 'Hi' AS
  label CALL test.labels() YIELD label RETURN *`. Sentinel:
  `ErrVariableKindConflict`. Fail-site: `buildPart` (§4.7) — the
  new CallBinding `label` collides with the imported name `label`
  from the preceding WITH's exported set.
- **YIELD intra rename collision (`b AS a` with `a` already
  yielded)** (`Call5 [5]`): `CALL test.my.proc(null) YIELD a, b AS
  a RETURN a`. Sentinel: `ErrVariableKindConflict`. Fail-site:
  intra-YIELD collision check inside `collectCall` (§4.2).
- **YIELD intra rename collision (both AS to the same name)**
  (`Call5 [6]`): `CALL test.my.proc(null) YIELD a AS c, b AS c
  RETURN c`. Same sentinel, same fail-site.

**Verbatim mustReject pin for `ErrUnboundVariable` (1):**

- **In-query CALL RETURN out with no YIELD** (`Call1 [12]`): `CALL
  test.my.proc(1) RETURN out` — signature `(in :: INTEGER?) ::
  (out :: INTEGER?)`. `RETURN out` references a name no scope
  carries (the in-query CALL introduces NO CallBinding without a
  YIELD; §4.2). Falls out of the existing `ErrUnboundVariable`
  sweep. Wrapped message: `unbound variable: out`.

  **Why the in-query CALL introduces NO CallBinding without a
  YIELD.** The grammar (`oC_InQueryCall`, §140) makes YIELD
  optional; when absent, the CALL's results are dropped (semantics
  parallel to a MATCH whose RETURN does not project any of its
  bindings — the values exist but nothing consumes them). Standalone
  CALL without YIELD, by contrast, has an implicit YIELD * — the
  standalone-CALL Returns expansion (§4.3) inspects the CallBindings.

**AUTHORED mustReject pins (2):**

- **AUTHORED: unbound-var CALL argument kill-probe** — `CALL
  test.labels(m.name) YIELD label RETURN label`, with `test.labels
  (in :: STRING?) :: (label :: STRING?)` registered. Fail-site:
  arg-side `typeExpressionMining` records `m` on `curPart.refs`
  (§4.4); `buildPart` referential-integrity sweep raises
  `ErrUnboundVariable` because `m` is not in scope. Matched pair
  with the mustParse bound-var regression lock (§1.8 above). This
  pin FAILS in RED (§1.8 RED expectations): today's parser
  rejects the whole CALL with `ErrUnsupportedClause` at
  `EnterOC_StandaloneCall`; RED-phase red-lit expectation flips to
  `ErrUnsupportedClause`, and the pin flips to `ErrUnboundVariable`
  after GREEN.
- **AUTHORED: YIELD-field unknown against signature kill-probe** —
  `CALL test.labels() YIELD nofield RETURN nofield`, with
  `test.labels() :: (label :: STRING?)` registered. Fail-site:
  YIELD-field enumeration in `collectCall`. Sentinel:
  `ErrUnknownProcedure`. Wrapped message: `unknown procedure result
  field: nofield on test.labels`. Pins the ONE-sentinel-covers-both
  ruling (Q1): registry-miss and unknown-field-miss share
  `ErrUnknownProcedure`, with the wrapped message doing the sub-
  category disambiguation.

**Exact count.**

mustParse: 10 verbatim + 4 authored = **14 new mustParse pins**.
The 4 authored pins are:

(a) CALL inside EXISTS does not populate an outer CallBinding.
(b) Bound-var CALL argument regression lock (`MATCH (n) CALL
    test.labels(n.name) ...`).
(c) Standalone-CALL Returns expansion is deterministic signature-
    declaration order.
(d) YIELD…WHERE arg-mining probe (Q5 authored kill-probe).

The "AUTHORED: unbound-var CALL argument kill-probe" listed above
is a mustReject entry (not mustParse); it is the matched-pair
counterpart to (b).

mustReject: 8 verbatim + 2 authored = **10 new mustReject pins**.
The 8 verbatim pins are 2 unknown-procedure + 2 arity + 3 kind-
conflict + 1 unbound-var (Call1[12]). The 2 authored pins are
the unbound-var CALL argument kill-probe and the YIELD-field
unknown against signature kill-probe.

mustReject retired: 2 (the Stage-13 standalone-CALL and in-query-
CALL `ErrUnsupportedClause` pins).

Sentinel arithmetic (§1.9): 6 → 7 net (–1 ErrUnsupportedClause,
+2 new — ErrUnknownProcedure and ErrProcedureArity).

`count`s update summary (Stage 13 → Stage 14):

- `mustParse`: 98 → 112 (net +14).
- `mustReject`: 17 → 25 (net +8: +10 new − 2 retired).
- `mustRejectGrammar`: 1 → 3 (+2: Call2[4] and Call5[7]).
- Sentinels (`allSentinels`): 6 → 7 (–1 ErrUnsupportedClause,
  +2 ErrUnknownProcedure and ErrProcedureArity).
- `readCoreDirs`: +1 (`clauses/call`).
- `skiplist`: 85 → 89 (+4 entries — see §5).

RED-phase posture, exact:

- **TestMustParse**: fails on exactly all 14 new pins in RED (with
  `unsupported clause: CALL` from the existing rejection in
  `EnterOC_InQueryCall` / `EnterOC_StandaloneCall`). Passes after
  GREEN.
- **TestMustReject**:
  - **Passes in RED** for the 2 verbatim unknown-proc pins, 2 arity
    pins, 3 kind-conflict pins, and 1 unbound-var pin (Call1[12]) —
    these currently reject via `ErrUnsupportedClause` (a different
    sentinel than the target). Wait: the target sentinel is
    `ErrUnknownProcedure` (etc.); today's rejection uses
    `ErrUnsupportedClause`. That is a WRONG sentinel: `TestMustReject`
    asserts `errors.Is(err, want)`, and `ErrUnsupportedClause`'s
    identity is not `ErrUnknownProcedure`'s identity, so the assertion
    FAILS. Correction: **all 8 verbatim mustReject pins fail in RED**
    (wrong sentinel) and pass in GREEN.
  - **Fails in RED** for the 2 authored mustReject pins (`unbound-var
    arg kill-probe` and `unknown-field kill-probe`) for the same
    reason. Passes after GREEN.
  Total mustReject RED-phase failures: 10 pins.
- **TestMustRejectGrammar**: passes in RED for both Call2[4] and
  Call5[7] — grammar rejects fire at the ANTLR level before the
  listener runs, so their behaviour is independent of the CALL-handler
  changes. The pins document the grammar-level rejection posture
  going forward.
- **TestSentinelReachability**: fails in RED after the sentinel-set
  edit (§1.6). Passes in GREEN when the new sentinels appear in a
  `mustReject` pin and the retired one is removed from `allSentinels`.
- **TestReadCoreAcceptance**: 3381 passed / 434 pending base → after
  Stage 14 GREEN, +48 passing scenarios from clauses/call (52
  pickles − 4 skiplisted per §5) = 3429 passed. Pending grows by 4
  new skiplist entries: 434 → 438 → actually 434 + 4 = 438. Total
  scenarios: 3815 + 52 = 3867 (3429 passed + 438 pending).

### 1.9 Docs inline

- This spec.
- `docs/adr/0007-parser-scope-full-opencypher-surface.md` gains
  a Stage-14 amendment note under §"Procedure signatures as a
  compile-time input artifact": the concrete registry package is
  `internal/procsig`; the `TypeToken` sum decouples signature
  vocabulary from `query.Type`; the on-disk format is deferred to a
  follow-up bead (§8 bead file).
- `docs/adr/0003-curated-dialect-agnostic-query-model.md` gains a
  Stage-14 amendment note: `Binding` sum widens from four to five
  variants (`CallBinding` joins); `BindingKind` gains
  `BindingCall`; `Query.StatementKind` axis is unchanged (CALL is
  Read).
- `internal/query/query.go` package doc gets a one-line Stage-14
  update: five-variant Binding sum.
- `internal/query/cypher/errors.go` retires `ErrUnsupportedClause`;
  adds `ErrUnknownProcedure` and `ErrProcedureArity` with per-
  sentinel doc paragraphs matching the existing convention.
- `CONTRIBUTING.md` — no change needed; the stage cadence documented
  there does not enumerate stages.

---

## 2. Why one atomic cycle

Stage 14 is atomic because its three model pieces (`procsig`,
`CallBinding` on `query`, and the `WithRegistry` option on `cypher`)
are mutually dependent: a `CallBinding` cannot exist without a
resolved signature (procsig lookup), the option surface cannot exist
without procsig, and the parser cannot produce a CallBinding without
having consulted the option's registry. Splitting into three PRs
would leave two intermediate states where CALL either rejects (no
registry to consult) or produces malformed CallBindings (no
resultType).

The corpus wiring (`clauses/call` into `readCoreDirs`) rides the
same PR because the mustParse/mustReject pins are what drive the
listener changes (test-first per ADR 0004); a PR that adds the model
without the pins would be model-first and violate the ADR-0004
cadence.

The skiplist entries (§5) ride the same PR because they are the
bucket-3 counterparts of the pinned tag: they are corpus scenarios
whose negative outcome the parser accepts categorically (wrong-arg-
type, missing-implicit-param, aggregate-in-arg), and separating them
would leave the acceptance suite red on scenarios the ADR
authorises the parser to accept.

The sentinel retirement (`ErrUnsupportedClause`) and the two new
sentinels are on the same PR because the sentinel-reachability sweep
would fail if the retirement lands without the additions (and vice
versa).

---

## 3. Model shape

### 3.1 `procsig` package layout

```go
// internal/procsig/procsig.go

package procsig

// TypeToken is a signature-time type marker. The sum is closed and
// decoupled from query.Type: signature vocabulary is a compile-time
// input, not a wire-visible result type.
type TypeToken int

const (
    TokenInteger TypeToken = iota
    TokenFloat
    TokenString
    TokenNumber // assignable-from INTEGER or FLOAT
)

func (t TypeToken) String() string { /* "INTEGER" etc. */ }

type Param struct {
    Name     string
    Token    TypeToken
    Nullable bool
}

type Result struct {
    Name     string
    Token    TypeToken
    Nullable bool
}

type Signature struct {
    Name    string   // fully-qualified: "test.my.proc"
    Params  []Param
    Results []Result
}

type Registry struct {
    byName map[string]Signature
}

func NewRegistry(sigs []Signature) (Registry, error) { /* … */ }

func (r Registry) Lookup(name string) (Signature, bool)
```

**Why the sum is int-backed with a stringer.** Matches the
`BindingKind` / `AggregateFunc` / `UnionKind` convention in the
`query` package: one source of truth for the token name (the
`String()` method), used verbatim in error messages so the wire and
the diagnostic never drift.

**Why `Param.Name` and `Result.Name` are on the struct.** Positional
matching alone would suffice for arg-count checks, but YIELD names
refer to result columns by their declared name (Call5 [3] outline
exercises `YIELD a, b` and `YIELD b, a` against a `(a, b)` signature
— the ordering of YIELD items is by name, not by position). And the
in-scope binding after `YIELD out` is named `out`, so the model must
carry the name to produce the binding.

**Why `NewRegistry` validates duplicate names in Params / Results.**
A signature with two params both named `in` (`(in :: INTEGER?, in ::
STRING?)`) could never be reached by name from a YIELD or by
parameter binding — the second would shadow the first with no
mechanism to distinguish them. The TCK corpus has no such shape;
validating at construction makes registry misuse impossible-by-
construction. Same for Results.

### 3.2 The token → `query.Type` bridge

Bridge function inside the `cypher` package (not `procsig` — the
bridge is one-way and used only at CallBinding construction):

```go
// internal/query/cypher/procbridge.go

func typeForToken(tok procsig.TypeToken) query.Type {
    switch tok {
    case procsig.TokenInteger: return query.TypeInt{}
    case procsig.TokenFloat:   return query.TypeFloat{}
    case procsig.TokenString:  return query.TypeString{}
    case procsig.TokenNumber:  return query.TypeUnknown{}
    default:
        return query.TypeUnknown{}
    }
}
```

`TokenNumber` → `TypeUnknown` is the wire-honest translation (Q3
ruling): NUMBER is a signature-time marker with no honest result-
column identity. After Stage 14 codegen consults the registry directly
(the Signature has a `TokenNumber` in its Results if the procedure
returns a NUMBER column), so no information is lost — only the wire
Type is TypeUnknown.

The default branch is a belt-and-braces guard against a future
Signature vocabulary widening reaching this bridge without a token
addition here; it is unreachable via a validated Registry (§3.1
`NewRegistry` rejects unknown tokens). If reached, the CallBinding's
resultType is TypeUnknown — the same honest "cannot tell" fallback
the Stage-6 typer uses elsewhere.

### 3.3 Listener state additions

`listener` struct (§3.3 of Stage 13's listener) gains one field:

```go
registry procsig.Registry // per-parse; empty registry is valid, all lookups miss
```

Populated by `newListener` from the `parser`'s `config.registry` (set
by `WithRegistry`), so the field is per-parse (a Registry is a
value, so shared reads are safe).

`rawPart` gains one field (mirror of `unwindBindings`):

```go
callBindings []query.CallBinding // Stage 14: CallBindings from CALL YIELD in this part, walk order
```

`build.go`'s `buildPart` appends `callBindings` to the model
`bindings` slice after `unwindBindings` (§4.7 ordering rule).

### 3.4 Public API on `cypher`

`internal/query/cypher/parser.go` grows:

```go
type Option func(*config)

type config struct {
    registry procsig.Registry
}

func WithRegistry(r procsig.Registry) Option {
    return func(c *config) { c.registry = r }
}

type parser struct {
    cfg config
}

func New(opts ...Option) query.Parser {
    var c config
    for _, o := range opts {
        o(&c)
    }
    return parser{cfg: c}
}

func (p parser) Parse(r io.Reader) (query.Query, error) {
    // ... existing lex/tree/walk setup, but newListener now takes p.cfg.registry
    l := newListener(ts, p.cfg.registry)
    // ... rest identical
}
```

`newListener` signature grows to `newListener(ts, registry)` and
carries the registry onto `l.registry` for `collectCall` to consult.

---

## 4. Parser widening

### 4.1 `EnterOC_StandaloneCall` and `EnterOC_InQueryCall` collect

Both replace their `ErrUnsupportedClause` rejection with a call to
`collectCall`. The two entry points differ only in the shape their
listener sees:

- `EnterOC_StandaloneCall` — passes `standalone=true` and the child
  invocation node (explicit or implicit).
- `EnterOC_InQueryCall` — passes `standalone=false` and the child
  explicit invocation node (grammar disallows implicit in-query per
  §140).

Both are suppressed at `subqueryDepth > 0` (§4.6).

### 4.2 `collectCall` — the shared collection path

Steps (in walk order):

1. **Procedure name extraction.** Walk `oC_ProcedureName` = `oC_
   Namespace oC_SymbolicName`. Concatenate the namespace tokens with
   `.` and append the symbolic name: `test.my.proc`. This is the
   registry key.

2. **Registry lookup.** `sig, ok := l.registry.Lookup(name)`. If not
   ok: `l.fail(fmt.Errorf("%w: %s", ErrUnknownProcedure, name))` and
   return. The failure fires before any bindings enter `curPart`, so
   a failed lookup leaves the part unchanged. Note: an empty (or
   nil-init'd) registry always misses — a parse that reaches a CALL
   without `WithRegistry` fails here.

3. **Argument mining.** Walk each `oC_Expression` under the
   explicit invocation via `typeExpressionMining` (the same Stage-6
   pass that types WHERE / projection expressions). This records
   parameter uses onto `l.params` and variable refs onto
   `curPart.refs` — both existing paths. The argument's mined type
   is discarded at Stage 14 (the arg-type check §4.5 rides the
   registry's declared param token, not the mined argument type at
   runtime).

4. **Arity check.** ONLY for explicit invocation (parens present):
   compare `len(argExprs)` to `len(sig.Params)`. If unequal:
   `l.fail(fmt.Errorf("%w: %s expects %d arguments, got %d",
   ErrProcedureArity, name, len(sig.Params), len(argExprs)))` and
   return. Implicit invocation skips the check (args come from
   parameter binding at runtime).

5. **Aggregate-in-argument check.** Bucket-3 skiplisted (§5) —
   Call1 [16] rides the skiplist. `collectCall` does NOT reject
   aggregate-in-argument at Stage 14; the arg-mining walk records
   the aggregate's parameter uses (if any) and the engine raises
   `InvalidAggregation` on the original text.

6. **YIELD binding collection.** If YIELD is present, iterate
   `oC_YieldItems`:
   - Each `oC_YieldItem` = `( oC_ProcedureResultField SP AS SP )?
     oC_Variable`. The variable is the AS alias (or the bare
     variable if no `AS` — grammar allows both, and the parser sees
     the source field via the optional field child; a bare variable
     means the source field name equals the variable).
   - For each item, resolve the source field against `sig.Results`.
     If no result declares the source field: `l.fail(fmt.Errorf("%w:
     %s on %s", ErrUnknownProcedure, sourceField, name))` and
     return.
   - **Intra-YIELD collision check.** Before recording the
     CallBinding, check the variable against the set of names this
     CALL's YIELD list has already recorded. If duplicate:
     `l.fail(fmt.Errorf("%w: %s", ErrVariableKindConflict,
     variable))` and return. Call5 [5]/[6] exercise both flavours
     (`YIELD a, b AS a`; `YIELD a AS c, b AS c`).
   - Build the CallBinding: `query.NewCallBinding(variable, name,
     sourceField, typeForToken(result.Token), result.Nullable)`.
     Append to `curPart.callBindings`.
   - If the YIELD clause has a trailing WHERE (`oC_YieldItems →
     … ( SP? oC_Where )?`), run `typeExpressionMining` on it —
     parameters and refs mine as usual. Corpus is silent; the
     authored kill-probe (§1.8) exercises the shape.

7. **YIELD * expansion (standalone only).** If `YIELD *` is
   present in a standalone CALL, iterate `sig.Results` in
   declaration order and produce one CallBinding per result
   (variable == sourceField == Result.Name); no aliasing possible.
   Call5 [8] verbatim.

8. **No-YIELD standalone: implicit YIELD expansion.** If the CALL
   is standalone AND no YIELD is present, iterate `sig.Results` in
   declaration order and produce one CallBinding per result (as
   with YIELD *). Call1 [1], [2], [5], Call2 [2], Call3 [1], Call4
   [1] all take this path — the standalone-with-no-YIELD scenarios
   expand implicitly (see §4.3 for the Returns population).

9. **No-YIELD in-query: no CallBinding.** If the CALL is in-query
   AND no YIELD is present, no CallBinding is produced. The
   grammar allows this shape (§140: `oC_InQueryCall` YIELD is
   optional); the values exist at runtime but nothing in-query
   consumes them. Call1 [3]/[4] exercise this shape (there is a
   `RETURN n` that references a MATCH-bound variable, not any
   yielded name — that RETURN parses via the standard MATCH
   binding lookup). Call1 [12]'s `RETURN out` fails via
   `ErrUnboundVariable` because there is no scope for `out`.

### 4.3 Standalone-CALL `Part.Returns` expansion

Standalone CALL that reaches `build.go` with CallBindings but no
explicit RETURN (i.e., no downstream part consumes the results —
the CALL is the whole query) needs `Part.Returns` populated so the
Query has a `Returns` list. The listener sets a flag
`curPart.callStandalone = true` when the collection path runs on
a standalone CALL (as opposed to in-query), and `buildPart` uses
it: at build time, if `callStandalone` is set and no explicit RETURN
appeared, produce one `RefProjection` per CallBinding in the order
they entered `curPart.callBindings` (signature-declaration order,
per §4.2 steps 7/8).

For `YIELD *`, additionally set `curPart.returnsAll = true` so the
Query.Returns semantics match the grammar (a `YIELD *` is a
declarative "all columns"). For no-YIELD standalone, `returnsAll`
is also true — the implicit expansion is the same as `YIELD *`
against the same signature. Call5 [8] (`YIELD *`) and Call1 [5]
(no YIELD) share the standalone-expansion path.

Ordering is deterministic: `sig.Results` order (Q5 ruling). Signature
declaration order is the wire-visible source of truth; a map-iteration
order would be a nondeterministic bug.

### 4.4 CallBinding participation in `refType`

`refType` (Stage 6 §3.2 — the ref-to-Type classifier) gains a case
for CallBinding: a bare `RETURN out` on a CallBinding named `out`
types as the CallBinding's `ResultType()`. This matches the
UnwindBinding participation in `refType` (Stage 9 §3.2 — a bare
ref on an UNWIND-introduced variable types as the binding's
`ElementType()`).

**Why through `refType`, not a new classifier.** `refType` already
resolves a variable to its binding via a per-part lookup; extending
the switch with a `BindingCall` case is the same shape widening
Stage 9 did for `BindingUnwind`. A property lookup on a CallBinding
(`RETURN out.foo`) types as `TypeUnknown` (no schema for procedure
result columns; the value shape is opaque at parse time — same
posture as a property on an UnwindBinding).

### 4.5 Argument-type check — bucket-3 (skiplisted)

Call2 [5]/[6] (`CALL test.my.proc(true)` against `(in :: INTEGER?)`)
are the wrong-arg-type scenarios. Stage 14 does NOT check argument
types against the registry's declared param tokens. The rationale is
Q4 ruling (bucket-3): the mined argument type at parse time is
best-effort (a `$param` mines to `TypeUnknown`; a `n.prop` mines to
`TypeUnknown` because property types live in the schema), so a
parser-time reject would either over-reject (falsely rejecting a
`$param` the engine would accept) or fire only on literals (a
half-check that gives false confidence).

`clauses/call/Call2 [5]` and `[6]` join the skiplist as bucket-3
enumerated entries (§5). A future stage can move the check up to
Stage-14-relative Layer 1 by widening `typeExpressionMining` to
distinguish "literal-typed" from "runtime-typed" mined types; that
widening is out of scope here.

NUMBER acceptance (Call3 [1]-[6]) is the accept-path counterpart: no
check runs, so a NUMBER-typed param accepts INTEGER, FLOAT, and
literally anything else the arg-mining pass tolerates. The Call3
pins in §1.8 exercise the accept-path structurally (the CallBinding
is produced correctly); the arg-vs-param type dance itself is not
modelled.

### 4.6 EXISTS-subquery suppression

`EnterOC_StandaloneCall` and `EnterOC_InQueryCall` both early-return
under `subqueryDepth > 0`, matching the Stage-11 pattern for MATCH /
WITH / RETURN / UNWIND / CREATE / MERGE / DELETE / SET / REMOVE. This
means a CALL inside an `EXISTS { ... }` produces no CallBinding on
the outer part and does not populate `curPart.callBindings`.
Consequences:
- The outer query's model does not carry the inner CALL.
- No CallBinding leaks past the subquery boundary (Stage 11
  §1.2's invariant preserved).
- Since CallBinding is Read-side, no `writeSeen` flip is at stake
  here (Stage 13's writeSeen non-flip is orthogonal).
- The `AUTHORED: CALL inside EXISTS` pin in §1.8 exercises this
  suppression on a non-registered-inside-scope procedure — the
  outer registry does not need to know about the inner CALL's
  procedure because the inner CALL is never inspected. Pin uses a
  procedure name that IS registered (`test.labels`) to keep the
  authored input honest to the runtime path (an engine executing
  the original text would look up the procedure); registration is
  free at Layer 2.

### 4.7 `buildPart` — CallBinding integration

`buildPart` (build.go:76) grows in two ways:

1. **Scope population.** After the existing `scope[b.variable]` for
   entity bindings, path bindings, and unwind bindings, add a
   fourth loop for CallBindings:
   ```go
   for _, cb := range rp.callBindings {
       if _, ok := rp.byVar[cb.Variable()]; ok {
           return query.Part{}, nil, fmt.Errorf("%w: %s", ErrVariableKindConflict, cb.Variable())
       }
       if pathByVar[cb.Variable()] {
           return query.Part{}, nil, fmt.Errorf("%w: %s", ErrVariableKindConflict, cb.Variable())
       }
       if unwindByVar[cb.Variable()] {
           return query.Part{}, nil, fmt.Errorf("%w: %s", ErrVariableKindConflict, cb.Variable())
       }
       // Imported-scope collision: Call1 [15]
       if imported[cb.Variable()] {
           return query.Part{}, nil, fmt.Errorf("%w: %s", ErrVariableKindConflict, cb.Variable())
       }
       scope[cb.Variable()] = true
   }
   ```
   The four-way kind sweep matches the Stage-9 unwind sweep's
   belt-and-braces posture: `collectCall` catches the intra-YIELD
   collision (§4.2 step 6); the four sweeps here are the symmetric
   backstop for cross-clause collisions (a CALL after a MATCH that
   introduced the same name is caught here). Call1 [15]'s
   imported-scope collision is the fourth check.

2. **Bindings append.** After the existing appends for entity /
   path / unwind bindings, append the CallBindings:
   ```go
   for _, cb := range rp.callBindings {
       bindings = append(bindings, cb)
   }
   ```

3. **Standalone-CALL Returns expansion.** After the existing
   `NewPart` construction, if the part has `callStandalone` set
   and `rp.returns == nil`, populate `rp.returns` from the
   CallBindings before calling `NewPart` — this way `NewPart`'s
   "at least one of bindings/projection/effects" invariant is
   trivially satisfied (there are CallBindings), and the RETURN
   list is populated per §4.3.

Ordering in the final `Part.Bindings` slice (mirroring the Stage 9
comment on §4.7 ordering):

- Entity bindings first (rp.bindings)
- Path bindings next (rp.pathBindings) — order collectPattern
  recorded
- Unwind bindings next (rp.unwindBindings) — walk order
- CallBindings last — walk order (which for standalone YIELD *
  and no-YIELD is signature declaration order per §4.2 steps 7/8)

No downstream shape depends on the position of CallBindings within
the slice; the discipline is deterministic ordering only.

### 4.8 `nameBoundAsUnwind` (and equivalent) interaction with CALL

The Stage-9 helper `nameBoundAsUnwind` is a lookup consulted by
Stage-9 kind-conflict checks; §4.7 already handles the CallBinding
side of the cross-kind check. No new helper is needed for
`nameBoundAsCall` at Stage 14 — the two callsites needing to check
would both be in `buildPart` (§4.7) and `collectCall`'s intra-YIELD
sweep (§4.2 step 6), and both use direct maps rather than a helper.

---

## 5. Corpus and bucket-3 skiplist

`clauses/call` (52 pickles) joins `readCoreDirs`. Of the 52
pickles: 48 land as passing scenarios (parse-green or passing
rejection); 4 join `skiplist` as bucket-3 accept-and-defer per
ADR 0007 §6.

**Passing scenarios by disposition (48):**

- **Parse-green — 36 pickles, of which 34 mint goldens.** Call1
  [1]-[6], Call2 [1]-[3], Call3 [1]-[6], Call4 [1]-[2], Call5
  [1]-[4]×(1+1+2+11)=15, Call5 [8], Call6 [1]-[3]. Total: 6 + 3 +
  6 + 2 + 15 + 1 + 3 = 36. Recount from pickle enumeration:
  Call1[1..6]=6, Call2[1..3]=3, Call3[1..6]=6, Call4[1..2]=2,
  Call5[1]=1, Call5[2]=1, Call5[3] outline=2 rows, Call5[4]
  outline=11 rows, Call5[8]=1, Call6[1..3]=3. Total =
  6+3+6+2+1+1+2+11+1+3 = 36 parse-green. Call1 [1] and Call1 [2]
  parse green but mint NO golden: both expect "the result should
  be empty" (the only two such pickles in clauses/call), and the
  harness's noSideEffects path snapshots only when StatementKind
  == StatementWrite — CALL leaves the statement read-kind, so no
  golden file is written. Golden inventory: 3135 → 3169 (+34).
- **Passing rejection (bucket-1) — 10 pickles.** Call1 [7]/[8]/
  [9]/[10] (arity, both stand/in-query pairs — 4 pickles);
  Call1 [12] (RETURN out unbound); Call1 [13]/[14] (unknown proc,
  stand/in-query — 2 pickles); Call1 [15] (shadow imported);
  Call5 [5]/[6] (intra-YIELD collision — 2 pickles). Total: 4 + 1
  + 2 + 1 + 2 = 10.
- **Passing rejection (grammar-level) — 2 pickles.** Call2 [4]
  (in-query implicit) and Call5 [7] (in-query YIELD *). Both
  `@skipGrammarCheck` in the TCK; ANTLR-level syntax error rides
  the SyntaxError sink.

Passing total: 36 + 10 + 2 = 48. ✓

**Skiplist entries added (4):**

- **`[5] Standalone call to procedure should fail if input type is
  wrong`** (Call2 [5]) — wrong-arg-type bucket-3. Rationale:
  `SyntaxError:InvalidArgumentType` is a runtime type check the
  parser accepts (§4.5); the engine on re-executed original text
  raises it.
- **`[6] In-query call to procedure should fail if input type is
  wrong`** (Call2 [6]) — same rationale, in-query variant.
- **`[11] Standalone call to procedure should fail if implicit
  argument is missing`** (Call1 [11]) — `ParameterMissing:
  MissingParameter`. Rationale: implicit invocation binds args
  from parameters at runtime; the parser has no static way to
  detect a missing named parameter (there is no `$name` in the
  query text — the binding is implicit-by-signature-name). Bucket-3
  accept-and-defer.
- **`[16] In-query procedure call should fail if one of the
  argument expressions uses an aggregation function`** (Call1
  [16]) — `SyntaxError:InvalidAggregation`. Rationale: same family
  as `[15] Fail on aggregation in WHERE` (already skiplisted): per-
  position aggregate legality is a semantic rule the type-interface
  boundary does not carry (ADR 0007).

Grand total skiplist growth: 85 → 89 (+4).

**Passing rejections that DO NOT need skiplist entries:**

- Call1 [7]/[8]/[9]/[10] (arity) — bucket-1, passing via
  `ErrProcedureArity`.
- Call1 [12] — bucket-1, passing via `ErrUnboundVariable` (existing
  sweep).
- Call1 [13]/[14] — bucket-1, passing via `ErrUnknownProcedure`.
- Call1 [15] — bucket-1, passing via `ErrVariableKindConflict`.
- Call5 [5]/[6] — bucket-1, passing via `ErrVariableKindConflict`
  (intra-YIELD check).
- Call2 [4] and Call5 [7] — grammar-level parse errors (both
  `@skipGrammarCheck` in TCK).

**TCK error class ↔ sentinel table:**

| TCK error class | Sentinel (Stage 14) | Fail-site |
|---|---|---|
| `ProcedureError:ProcedureNotFound` | `ErrUnknownProcedure` | `collectCall` step 2 |
| `SyntaxError:InvalidNumberOfArguments` | `ErrProcedureArity` | `collectCall` step 4 |
| `SyntaxError:UndefinedVariable` (Call1 [12]) | `ErrUnboundVariable` | `buildPart` refs sweep |
| `SyntaxError:VariableAlreadyBound` (Call1 [15]) | `ErrVariableKindConflict` | `buildPart` scope sweep |
| `SyntaxError:VariableAlreadyBound` (Call5 [5]/[6]) | `ErrVariableKindConflict` | `collectCall` step 6 |
| `SyntaxError:UnexpectedSyntax` (Call5 [7]) | (grammar) | ANTLR SyntaxError sink |
| `SyntaxError:InvalidArgumentPassingMode` (Call2 [4]) | (grammar) | ANTLR SyntaxError sink |
| `SyntaxError:InvalidArgumentType` (Call2 [5]/[6]) | (skiplisted) | bucket-3 |
| `ParameterMissing:MissingParameter` (Call1 [11]) | (skiplisted) | bucket-3 |
| `SyntaxError:InvalidAggregation` (Call1 [16]) | (skiplisted) | bucket-3 |

**Sentinel-set delta (Stage 13 → Stage 14):**

| Sentinel | Stage 13 | Stage 14 |
|---|---|---|
| `ErrUnsupportedClause` | ✓ | RETIRED |
| `ErrUnsupportedParameter` | ✓ | ✓ |
| `ErrUnboundVariable` | ✓ | ✓ (new fail-site: Call1 [12]) |
| `ErrVariableKindConflict` | ✓ | ✓ (new fail-sites: §4.2 step 6, §4.7 imported) |
| `ErrPatternInProjection` | ✓ | ✓ |
| `ErrNestedPropertyTarget` | ✓ | ✓ |
| `ErrUnknownProcedure` | — | ADDED |
| `ErrProcedureArity` | — | ADDED |
| **Total** | **6** | **7** |

---

## 6. Definition of done for Stage 14

- Spec landed in `docs/specs/cypher-query-parser-stage-14.md`.
- `internal/procsig` package created with `TypeToken`, `Param`,
  `Result`, `Signature`, `Registry`, `NewRegistry`, `Lookup`.
- `query.CallBinding` type added with smart constructor, all
  accessors, and `MarshalJSON`. `BindingKind` gains `BindingCall`.
- `cypher.New(opts ...Option)` API in place, with `WithRegistry`.
- `EnterOC_StandaloneCall` / `EnterOC_InQueryCall` replaced with
  `collectCall`. `ErrUnsupportedClause` retired from `errors.go`
  and `allSentinels`. `ErrUnknownProcedure` and
  `ErrProcedureArity` added.
- `refType` extended for `BindingCall`.
- `buildPart` grows the four-way CallBinding scope sweep (byVar,
  pathByVar, unwindByVar, imported) and the CallBindings append.
- `readCoreDirs` gains `clauses/call`.
- Layer-2 pins added per §1.8 (14 mustParse + 10 mustReject + 2
  mustRejectGrammar; retire 2 old CALL `ErrUnsupportedClause`
  mustReject pins).
- Skiplist grows by 4 (Call2 [5]/[6], Call1 [11], Call1 [16]).
- Godog step definition for `there exists a procedure`: parses the
  signature grammar (permissively — `(a :: TOKEN?, ...) :: (b ::
  TOKEN?, ...)`, tolerating optional whitespace and the `) :`
  variant vs `):` variant seen in Call5 [3]) and populates the
  per-scenario `procsig.Registry`, which the executing-query step
  passes to `cypher.New(WithRegistry(reg))`. The step then no-ops
  the data table (it is example data, not an input).
- `TestSentinelReachability`, `TestMustParse`, `TestMustReject`,
  `TestMustRejectGrammar`, `TestSkiplistOrphans`,
  `TestGoldenOrphans`, `TestReadCoreAcceptance`,
  `TestNoUndefinedSteps` all green.
- `just test` green on the branch tip alone (AGENTS.md stacked-
  branch invariant).
- ADR 0007 amended per §1.9.
- ADR 0003 amended per §1.9.
- Godoc updated per §1.9.
- One follow-up bead filed for the on-disk registry format (§8).

---

## 7. Commit inventory (single branch `stage-14-call`)

1. **spec + docs**: this document; ADR 0003 / ADR 0007 amendment
   notes.
2. **procsig package**: `internal/procsig/procsig.go` with
   `TypeToken`, `Param`, `Result`, `Signature`, `Registry`, and
   `NewRegistry` + tests. Includes a small `procsig_test.go`
   covering NewRegistry validation (empty name, unqualified name,
   duplicate names, duplicate param/result names, unknown token)
   and Lookup hit/miss.
3. **query.CallBinding**: `internal/query/query.go` gains the new
   Binding variant, `BindingKind` extension, wire encoding. Tests
   in `internal/query/query_test.go` cover the smart constructor
   (empty variable/procedure/sourceField reject; nil resultType
   normalises to TypeUnknown) and JSON round-trip.
4. **cypher.New with options**: `internal/query/cypher/parser.go`
   gains `Option`, `config`, `WithRegistry`, and the new `New`
   signature. Every existing `cypher.New()` call-site is
   untouched.
5. **parser RED**: Layer-2 mustParse and mustReject pins added
   (§1.8). At this commit, RED failures match §1.8 RED
   expectations. `allSentinels` updated (retire
   `ErrUnsupportedClause`, add the two new sentinels).
   `errors.go` retires and adds sentinels. `TestSentinelReachability`
   fails until the mustReject pins for the new sentinels land in
   the same commit — this commit lands them together so
   reachability is satisfied at the end of the commit.
6. **parser GREEN**: `EnterOC_StandaloneCall` /
   `EnterOC_InQueryCall` replaced with `collectCall`. `procbridge.go`
   added. `refType` extended for `BindingCall`. `buildPart` grows
   the four-way sweep and CallBindings append. Standalone-CALL
   Returns expansion (§4.3) lands.
7. **TCK dir unlock + skiplist + goldens**: `readCoreDirs` gains
   `clauses/call`. Skiplist gains the 4 entries per §5. `godog`
   step definition `there exists a procedure` added. Golden
   snapshots for the 36 parse-green scenarios generated and
   committed.

Each commit compiles and `just test` runs — the commits are
separately reviewable but not independently mergeable (RED expects
GREEN to follow within the same PR, per AGENTS.md).

**Grammar-only rejection pin commit ordering.** The two
`mustRejectGrammar` entries (Call2 [4], Call5 [7]) land in commit 5
(parser RED) alongside the other mustReject pins; they pass at RED
because grammar rejection is orthogonal to CALL-handler changes.

**Sentinel retirement is atomic within commit 5.** Removing
`ErrUnsupportedClause` from `errors.go` and from `allSentinels`
happens in the same commit, and the retirement of the 2 stage-13
mustReject pins that pointed at it happens in the same commit.
`TestSentinelReachability` requires the covered set to equal
`allSentinels`; a partial commit that retired the sentinel from
`allSentinels` but left the pins pointing at it would fail
`TestMustReject` (the sentinel is no longer a canonical member).

---

## 8. Weakest points recorded honestly (per ADR 0004)

- **Arg-type NUMBER acceptance is untested by rejection.** Stage 14
  accepts NUMBER-vs-INTEGER-vs-FLOAT pinlessly at parse time (§4.5
  skiplists the wrong-type reject). A stubbed registry mislabelling
  a NUMBER param as INTEGER would not fail any Stage-14 pin. Q3
  ruling accepts this: NUMBER's assignable-from semantics live in
  codegen (the registry is the source of truth), and
  the Stage-14 pin (`AUTHORED: standalone-CALL Returns expansion`)
  exercises the accept-path through the CallBinding shape. A future
  stage that promotes arg-type checks to Layer 1 would tighten
  this.

- **YIELD-WHERE is authored, not corpus-verified.** The Q5-authored
  pin (§1.8) exercises a grammar-legal but corpus-silent shape. If
  implementation encounters an obstruction (e.g., `oC_Where` under
  YIELD collides with the WHERE-under-MATCH walk), the pin flags it
  early — but the corpus itself does not exercise the shape, so a
  future TCK addition could reveal a divergence. Q5 escalation path
  applies: if implementation reveals obstruction, spec is amended
  before rejection.

- **On-disk registry format deferred.** The registry is an in-
  memory API at Stage 14. A real CLI user would eventually need a
  file surface (YAML, JSON, `.procsig`, etc.). Follow-up bead
  filed at close-out: Stage 14 does not gate on this — codegen
  consumes `procsig.Registry` values directly, and the initial
  Stage 14 cut can proceed with the in-memory-only surface.

- **Aggregate-in-arg is bucket-3 without a widening path.**
  Call1 [16] (`CALL foo(count(n))`) skiplists as bucket-3 — the
  parser accepts the argument as a rich expression via the Stage-6
  typer. A future stage could reject at parse time (aggregate
  legality by position is a grammar-adjacent rule the parser has
  the information to enforce), but the widening is out of scope
  here.

- **CallBinding does not carry the procedure's arg list.** The
  wire model records the YIELD-side (each CallBinding is one
  output column), not the argument list. Codegen later reads
  the CallBinding's `procedure` field, looks it up in the registry
  (registry is a codegen-time input alongside the wire model), and
  reconstructs the argument list from the original text (ADR 0005).
  A future stage could persist the argument shape on a per-CALL
  node in the model, but Stage 14's boundary is "the registry is
  the arg-list truth; the model is the result-column truth" —
  matching the pattern registry-for-inputs, schema-for-labels used
  elsewhere.

- **`ErrUnknownProcedure` conflates two sub-categories.** The
  sentinel identity covers both "procedure name miss" and "YIELD
  field miss on a known procedure"; the wrapped message
  distinguishes. A caller doing `errors.Is(err,
  ErrUnknownProcedure)` gets no free sub-category. Q1 ruling
  accepts this trade-off (one canonical sentinel; sub-category via
  wrapped message).

- **`Signature.Params` positional matching does not enforce
  parameter-name-vs-implicit-invocation coupling.** Implicit
  invocation (`CALL foo`) binds args from `$name` parameters at
  runtime; the parser's parameter-mining pass records `$name`
  onto `l.params` regardless of whether the procedure declares a
  matching `Param.Name`. A `CALL foo` with the signature `(bar ::
  INTEGER?)` and a `$baz` parameter in the query would silently
  parse — the engine would raise MissingParameter on `bar`. This
  is the same rationale as skiplisting Call1 [11] (§5): implicit-
  arg binding is a runtime concern.

---

## Appendix A. Pickle enumeration (clauses/call — 52 pickles)

| Feature | Scenario | Kind | Disposition |
|---|---|---|---|
| Call1 [1] | Standalone doNothing() | parse-green | no golden (read-kind, "result should be empty") |
| Call1 [2] | Standalone doNothing (implicit) | parse-green | no golden (read-kind, "result should be empty") |
| Call1 [3] | In-query doNothing() | parse-green | goldened |
| Call1 [4] | In-query doNothing() consumes 3 rows | parse-green | goldened |
| Call1 [5] | Standalone STRING labels() | parse-green | goldened |
| Call1 [6] | In-query STRING labels() YIELD | parse-green | goldened |
| Call1 [7] | Explicit too-few args (standalone) | reject | `ErrProcedureArity` |
| Call1 [8] | Explicit too-few args (in-query) | reject | `ErrProcedureArity` |
| Call1 [9] | Explicit too-many args (standalone) | reject | `ErrProcedureArity` |
| Call1 [10] | Explicit too-many args (in-query) | reject | `ErrProcedureArity` |
| Call1 [11] | Implicit missing parameter | (bucket-3) | skiplist |
| Call1 [12] | In-query RETURN out with no YIELD | reject | `ErrUnboundVariable` |
| Call1 [13] | Standalone unknown proc | reject | `ErrUnknownProcedure` |
| Call1 [14] | In-query unknown proc | reject | `ErrUnknownProcedure` |
| Call1 [15] | YIELD shadows imported | reject | `ErrVariableKindConflict` |
| Call1 [16] | Aggregate in arg | (bucket-3) | skiplist |
| Call2 [1] | In-query explicit args | parse-green | goldened |
| Call2 [2] | Standalone explicit args | parse-green | goldened |
| Call2 [3] | Standalone implicit args | parse-green | goldened |
| Call2 [4] | In-query implicit args | reject | `mustRejectGrammar` (`@skipGrammarCheck`) |
| Call2 [5] | Wrong arg type standalone | (bucket-3) | skiplist |
| Call2 [6] | Wrong arg type in-query | (bucket-3) | skiplist |
| Call3 [1] | NUMBER accepts INTEGER standalone | parse-green | goldened |
| Call3 [2] | NUMBER accepts INTEGER in-query | parse-green | goldened |
| Call3 [3] | NUMBER accepts FLOAT standalone | parse-green | goldened |
| Call3 [4] | NUMBER accepts FLOAT in-query | parse-green | goldened |
| Call3 [5] | FLOAT accepts INTEGER standalone | parse-green | goldened |
| Call3 [6] | FLOAT accepts INTEGER in-query | parse-green | goldened |
| Call4 [1] | null arg standalone | parse-green | goldened |
| Call4 [2] | null arg in-query | parse-green | goldened |
| Call5 [1] | Explicit projection | parse-green | goldened |
| Call5 [2] | Explicit projection RETURN * | parse-green | goldened |
| Call5 [3] × 2 | YIELD ordering irrelevant (outline: `a, b`; `b, a`) | parse-green | goldened |
| Call5 [4] × 11 | Rename outputs (outline: 11 rows) | parse-green | goldened |
| Call5 [5] | Rename collides bound | reject | `ErrVariableKindConflict` |
| Call5 [6] | Rename all to same | reject | `ErrVariableKindConflict` |
| Call5 [7] | In-query YIELD * (grammar) | reject | `mustRejectGrammar` (`@skipGrammarCheck`) |
| Call5 [8] | Standalone YIELD * | parse-green | goldened |
| Call6 [1] | Two CALLs across WITH | parse-green | goldened |
| Call6 [2] | CALL YIELD projected via WITH | parse-green | goldened |
| Call6 [3] | CALL YIELD renamed via WITH AS | parse-green | goldened |

Total: 6 + 10 + 6 + 6 + 2 + 4 + 11 + 4 + 3 = actually let me
recount by literal roll-up: Call1 = 16 pickles; Call2 = 6; Call3 =
6; Call4 = 2; Call5 base = 8 scenarios but outlines expand: [1] +
[2] + [3]×2 + [4]×11 + [5] + [6] + [7] + [8] = 1 + 1 + 2 + 11 + 1
+ 1 + 1 + 1 = 19 pickles; Call6 = 3. Grand total = 16 + 6 + 6 +
2 + 19 + 3 = **52 pickles**. Matches Layer-1 count.

Passing disposition roll-up:
- parse-green + goldened: 36 pickles
- bucket-1 reject via `ErrUnknownProcedure`: 2 (Call1 [13]/[14])
- bucket-1 reject via `ErrProcedureArity`: 4 (Call1 [7]-[10])
- bucket-1 reject via `ErrVariableKindConflict`: 3 (Call1 [15],
  Call5 [5]/[6])
- bucket-1 reject via `ErrUnboundVariable`: 1 (Call1 [12])
- `mustRejectGrammar` (bucket-1 grammar-level): 2 (Call2 [4],
  Call5 [7])
- bucket-3 skiplist: 4 (Call1 [11], Call1 [16], Call2 [5]/[6])

Total: 36 + 2 + 4 + 3 + 1 + 2 + 4 = **52**. ✓
