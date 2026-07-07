# Model unfreeze — per-position CALL-arg records

The implementation brief for cycle **gqlc-0ig** of the model-unfreeze
campaign: an additive `Args []CallArg` axis on `query.CallBinding`,
populated parser-side at `collectCall`'s existing argument-mining
loop, closing the R7 §7.1.1 CALL-arg-attribution gap
(`docs/specs/resolver-stage-r7.md §7.1.1`) without touching the
frozen wire shape's zero-value semantics. The axis carries the
mined argument's `query.Type` plus an implicit position (slice
index); the procedure signature match (including the
NUMBER-assignable-from-INTEGER-or-FLOAT rule from ADR 0007) stays
resolver-side, consulting the per-parse `procsig.Registry` the
resolver already holds (`internal/resolver/resolve.go:21`).

This brief is the **contract for the whole gqlc-0ig cycle**: it spans
the spec PR (this file), the unfreeze PR (model + parser +
ADR 0008 amendment + parser-test rebaseline; resolver untouched
and green), and the resolver-widening PR (a new argument-site
assignability walk over `CallBinding.Args()`; new discriminating
invalid fixture + preserved byte-identity over every pre-existing
resolver golden). Both code PRs land under ADR 0008's post-freeze
revision protocol (§Post-freeze revision protocol) — additive-only,
dated amendment, golden rebaseline whose diff shows only the new
surface.

The three other unfreeze beads (`gqlc-hk0` — CLOSED, `gqlc-fvo` —
CLOSED, `gqlc-ay9`, `gqlc-5xg`), the cycle-2 residual beads
(`gqlc-4w5`, `gqlc-qcc`), and codegen are **out of scope** — this
campaign closes 0ig alone. See §9 for the non-goals table.

> **Layering divergence from the bead text — flagged for
> team-lead / linus review.** The bead text (gqlc-0ig) proposes
> carrying the "param name/token from the matched procsig.Signature
> emitted at parse". §3.2 argues that per-signature vocabulary
> (`procsig.TypeToken`) belongs resolver-side, not on the wire:
> R7's parser-authoritative unknown-procedure precedent
> (`docs/specs/resolver-stage-r7.md §4.4`) puts the sig-name check
> at parse but the sig-vocabulary check at resolve. This spec
> commits to Args-carry-mined-type-only; the resolver looks up the
> matched param position from `procsig.Registry.Lookup(procedure).
> Params[i]`. If team-lead / linus prefer the bead text's
> parser-emits-token shape instead, §3.2 lists the exact edits.

---

## 1. Deliverables

Spec cycle (Cycle 1) — this PR:

- `docs/specs/unfreeze-0ig-call-args.md` — this file.

Unfreeze cycle (Cycle 2, follow-up PR):

- `internal/query/query.go` — one additive field `args []CallArg`
  on `CallBinding` (last field, per the tail-append convention hk0
  and fvo established at
  `internal/query/query.go:748` and `:790`; verified against master
  `62923d8`); one new type `CallArg` (a package-level struct with
  one `Type` field, `Type() query.Type` accessor); one new
  constructor `NewCallBindingWithArgs(variable, procedure,
  sourceField string, resultType Type, nullable bool, args []CallArg)
  (CallBinding, error)` — the existing `NewCallBinding` is preserved
  verbatim and forwards through the new constructor with `args=nil`;
  one new accessor `Args() []CallArg`. §4.1.
- `internal/query/type_test.go` — new `TestNewCallArg` and
  `TestCallArgMarshalJSON` tests pinning the sub-type's zero and
  non-zero shape; a widened `TestNewCallBindingWithArgs` pinning
  the true-side wiring (non-nil args → key present in JSON).
  The pre-existing `TestNewCallBinding` and
  `TestCallBindingMarshalJSON` (§3.1.4) stay verbatim — under
  omit-when-zero-length their pinned JSON strings match the widened
  marshaller bit-for-bit. §5.
- `internal/query/cypher/call.go:47-66` — `collectCall`'s existing
  argument-mining loop is widened to accumulate one `CallArg` per
  argument expression (using the already-mined `t` from
  `typeExpressionMining`) and thread the resulting slice through
  `collectYieldItems` / `expandAllResults` so each minted
  `CallBinding` carries the same args slice. The two existing
  callees gain an `args []query.CallArg` parameter; the parameter-use
  and ref emission (lines 54-66) stays verbatim. §4.2.
- `internal/query/cypher/parser_test.go` — the 15 CallBinding pins
  (§4.3 enumerates each) that today use `NewCallBinding("var",
  "proc", "field", ...)` stay VERBATIM under `require.Equal` for
  the pins whose CALLs have zero arguments (Go struct zero-value
  equality — a `CallBinding{…, args: nil}` equals a
  `CallBinding{…}` bit-for-bit under `reflect.DeepEqual`; a nil
  slice equals a nil slice). Pins whose CALLs have ≥1 argument
  rebaseline — the pin's `NewCallBinding(...)` becomes
  `NewCallBindingWithArgs(..., []query.CallArg{...})` with mined
  types. §4.3 pins each rebaseline.
- `docs/adr/0008-query-model-freeze-resolver-api.md` — one dated
  amendment note (top of the file, ADR 0003 stage-note convention)
  recording the CALL-arg axis's adoption. The "Known deferred
  additions" list carries a new closed-out entry for `CallBinding.
  Args`. Verbatim text pinned in §7.
- Parser goldens (`*.golden.json` in
  `internal/query/cypher/testdata/golden/`) rebaseline **for 28
  fixtures** — under omit-when-zero-length and the branch-base
  corpus's CALL distribution (§4.4.1), 28 of the 32 CallBinding-
  carrying goldens gain an `args` key (each embedded on every
  CallBinding of the CALL clause). The 4 no-arg-CALL goldens
  are byte-identical. §4.4 pins the fence with per-scenario
  witnessing.

Resolver-widening cycle (Cycle 3, follow-up PR after the unfreeze PR
merges):

- `internal/resolver/resolve.go` — a new argument-site assignability
  walk. Per each `query.CallBinding` at Phase A1 (the switch arm at
  `internal/resolver/resolve.go:262-285`), look up the procedure
  signature via the resolver-held `procsig.Registry`, walk the
  binding's `Args()` slice, and check each mined type against the
  positionally-corresponding `sig.Params[i].Token` under the
  assignability rule (STRING param → TypeString-only; INTEGER →
  TypeInt; FLOAT → TypeFloat; NUMBER → TypeInt or TypeFloat, per
  ADR 0007 Stage-14 note lines 172-174). A mismatch fires the
  existing `ErrOutOfR0Scope` sentinel with a widened message set
  (a new sentinel is rejected in §8.3 for R7-precedent reasons).
  §8.1.
- One new resolver invalid fixture:
  `call_arg_wrong_type_at_arg_site.cypher` (§8.4). At branch base
  this fixture is silently admitted (R7 §4.8 records the resolver
  has no arg-side check today); post-widening it fires the widened
  `ErrOutOfR0Scope`. This is the sole wire-observable resolver
  behaviour change; every pre-existing resolver-valid golden is
  byte-identical (§8.5).
- `docs/specs/resolver-stage-r7.md §7.1.1` — the
  "CALL-arg attribution" deferral prose retires; a "0ig cycle
  closes the gap" successor prose lands (§8.6, pinned verbatim).

Nothing downstream of the resolver is built — the resolver's widening
lands the arg-site assignability semantics; codegen consumes them
under a future ADR.

---

## 2. Frame — what changes and what stays

### 2.1 The gap in one paragraph

The parser's Stage 14 `collectCall` at
`internal/query/cypher/call.go:47-66` mines every CALL argument
expression via `typeExpressionMining` and immediately DISCARDS the
mined `query.Type` (comment at `:47-53`: "The mined argument type
is discarded — the arg-type check against sig.Params is bucket-3
(spec §4.5), so today's job is only to route param and ref info onto
the part's state so referential integrity and parameter tables stay
honest"). The bucket-3 skiplisting cite is
`docs/specs/cypher-query-parser-stage-14.md:971-990`. The mined
type is then used only to construct an `ExprUse{t,
ExprInProjection}` for any parameter tree the expression walks over
(line 64). The frozen wire — `query.CallBinding` at
`internal/query/query.go:843-849` — carries **zero information about
the arguments**: only the yielded column's variable, procedure name,
source field, result type, and nullable bit.

Consequence for R7's ADR 0009 delivery: the ADR line _"argument
assignability including NUMBER assignable-from INTEGER-or-FLOAT
(ADR 0007 Stage-14 note)"_ has NO application site in the resolver.
R7 shipped YIELD typing, gate lift, and parser-authoritative
unknown-procedure (`docs/specs/resolver-stage-r7.md §7.1.1` records
the deferral honestly, closing the R7 cycle without the widening).
The 0ig cycle records the per-argument axis the check would consult.

### 2.2 The one wire axis — additive, omit-when-zero-length

**One field on one sum variant**:

- `CallBinding` gains `args []CallArg` — a slice of positional
  argument records, mined at the same site the current parser
  already visits (`typeExpressionMining` at `call.go:55`).

Each `CallArg` carries one field:

- `Type() query.Type` — the mined `query.Type` from
  `typeExpressionMining`. `TypeInt{}` for `42`, `TypeFloat{}` for
  `42.3`, `TypeString{}` for `'Stefan'`, `TypeUnknown{}` for
  `$param` / `n.name` / `null` and for anything the walk cannot
  type (Stage 6 §4 fallback).

No `Name`, no `Token`, no procedure-side vocabulary. §3.2 argues the
divergence from the bead text's parser-emits-sig-token proposal
against R7's parser-authoritative-unknown-procedure precedent.

The JSON encoding is **omit-when-zero-length**
(`,omitempty` on the slice), following the campaign convention
established by hk0 (omit-when-false) and fvo (omit-when-zero-int).
The convention: a Use / Binding whose additive axis carries the
axis's zero value emits the axis-absent form; a rebaseline of a
future cycle diff-highlights only the axes' actual wire presence,
not their schema surface. §4.1.3.1 accounts the branch-base corpus
under this encoding: 28 of the 32 CallBinding-carrying goldens
rebaseline (each gains a non-nil `args` key on every CALL in the
scenario); 4 stay byte-identical.

### 2.3 One accessor, one constructor, one sub-type

`CallBinding` gains:

- `NewCallBindingWithArgs(variable, procedure, sourceField string,
  resultType Type, nullable bool, args []CallArg) (CallBinding, error)`
  — the widened constructor.
- `NewCallBinding(variable, procedure, sourceField string,
  resultType Type, nullable bool) (CallBinding, error)` — preserved
  verbatim, forwards through `NewCallBindingWithArgs` with
  `args=nil`.
- `Args() []CallArg` — the accessor.

`CallArg` gains:

- `NewCallArg(t Type) CallArg` — the constructor.
- `Type() Type` — the accessor.

No new `Kind()` on `CallArg` (it is not a sum variant — the sum is
`Binding`; `CallArg` is a plain product record). Sealed-sum status
is unchanged on every existing interface: `Binding` stays at five
variants, `Use` at three, `Type` at seventeen. **No new sealed sum
is opened by this cycle.**

### 2.4 The resolver-widening semantics — arg-site assignability

The widening lives in the resolver's Phase A1 CallBinding arm at
`internal/resolver/resolve.go:262-285`. Per each CallBinding, look
up the procedure signature via `r procsig.Registry`; walk the
binding's `Args()` slice; check each mined `Type()` against
`sig.Params[i].Token` under the assignability rule (ADR 0007
Stage-14 note lines 172-174):

| Signature `sig.Params[i].Token` | Accepts mined `CallArg.Type()` |
|---|---|
| `procsig.TokenInteger` | `TypeInt` or `TypeUnknown` |
| `procsig.TokenFloat` | `TypeFloat` or `TypeUnknown` |
| `procsig.TokenString` | `TypeString` or `TypeUnknown` |
| `procsig.TokenNumber` | `TypeInt`, `TypeFloat`, or `TypeUnknown` |

`TypeUnknown` is always accepted (a `$param`, a property projection,
or a `null` argument mines to Unknown; rejecting these at
resolver-time would over-reject exactly the shapes the current
bucket-3 skiplist accepts). §4.5 of Stage 14's spec quotes the
Q4 ruling verbatim: _"the mined argument type at parse time is
best-effort … so a parser-time reject would either over-reject
(falsely rejecting a $param the engine would accept) or fire only
on literals (a half-check that gives false confidence)"_. At
resolve time the check IS honest for literals (the mined type is
authoritative), and TypeUnknown accepts silently — the same
posture. §8.1 details.

**Sentinel discipline** (§8.3) — no new sentinel. The mismatch fires
`ErrOutOfR0Scope` with a widened message set, following R7's
sentinel-non-addition precedent
(`docs/specs/resolver-stage-r7.md §5.1`: _"R7 introduces no new
sentinel. The R6 close-out committed to one per stage as a hard
maximum; zero is a floor R7 hits naturally"_). The 0ig arm is a
YIELD-adjacent widening; the R7 posture carries.

### 2.5 What this cycle does NOT touch

The wire surface, sum arities, and semantic contracts unchanged by
this cycle:

- **`Use` remains sealed at three variants** with the fvo `part`
  axis. No new position (e.g., `ExprInCallArg`) — the CALL-arg
  parameter Uses that R7 §4.8 records as `ExprUse{miningType,
  ExprInProjection}` STAY `ExprInProjection`; the arg-site
  assignability check consults `CallBinding.Args()`, not the
  Use's position discriminator. §3.4 argues the divergence from
  R7 §7.1.1's "Two minimal frozen-model changes" list (which
  proposes both `ExprInCallArg` AND a `Calls` axis on `Part`).
- **`Binding` remains sealed at five variants** — CallBinding gains
  a field, not a new sibling.
- **`Type` remains sealed at seventeen variants** — the mined
  arg types reuse the existing frozen Type sum.
- **`procsig.TypeToken`** stays a signature-only vocabulary
  (ADR 0007 Stage-14 note lines 172-174) — it never appears on
  the query wire. The resolver bridges `procsig.TokenNumber` to
  `query.TypeUnknown` when needed (existing `typeForToken` at
  `internal/query/cypher/call.go:234-247`; verified against
  master `62923d8`).
- **Parser fail-sites for CALL** stay at
  `ErrUnknownProcedure`, `ErrProcedureArity`,
  `ErrVariableKindConflict` (spec §4.2 of Stage-14) — no
  new parser sentinel. The arg-type check is deliberately still
  deferred to the resolver (bucket-3 posture preserved at parse
  time; the RESOLVER widening picks it up).

---

## 3. Mining — what the frozen model records today

### 3.1 `CallBinding` — the frozen shape

Verbatim from `internal/query/query.go:820-932`
(read fresh against master `62923d8`):

```go
// CallBinding is a query variable bound to one YIELD result column of a
// CALL clause (Stage 14 spec §1.2 / §3.2). Each YIELD item mints one
// CallBinding, mirroring UnwindBinding's "one variable per binding"
// shape: the variable is the AS-alias (or the bare source-field name
// when no AS is present); the procedure is the fully-qualified name
// looked up in the procedure registry (procsig.Registry); sourceField
// is the signature-declared result column this binding draws from
// (kept even when it equals the variable, so a YIELD out AS x is
// unambiguous at codegen). The resultType is the bridged Stage-6
// type (TypeInt / TypeFloat / TypeString / TypeUnknown — the last for
// a NUMBER signature token, whose runtime column type post-freeze
// codegen reads from the registry directly). Nullable mirrors the
// signature's trailing `?` verbatim.
type CallBinding struct {
    variable    string // always non-empty
    procedure   string // fully-qualified name; always non-empty
    sourceField string // signature-declared result column; always non-empty
    resultType  Type   // bridged Stage-6 result type; TypeUnknown for NUMBER
    nullable    bool   // signature's trailing '?'
}
```

`NewCallBinding` at `:857-877` rejects empty `variable` / `procedure`
/ `sourceField`; normalises `nil` `resultType` to `TypeUnknown{}`.

Accessors at `:882-908`:

| Method | Return |
|---|---|
| `Variable() string` | `:882` |
| `Procedure() string` | `:886` |
| `SourceField() string` | `:893` |
| `ResultType() Type` | `:900` |
| `Kind() BindingKind` | `:903` |
| `Nullable() bool` | `:907` |
| `isBinding()` | `:909` |

MarshalJSON at `:916-932` (**every field always-emitted**, per the
pre-freeze frozen wire posture):

```go
func (b CallBinding) MarshalJSON() ([]byte, error) {
    return json.Marshal(struct {
        Kind        string `json:"kind"`
        Variable    string `json:"variable"`
        Procedure   string `json:"procedure"`
        SourceField string `json:"sourceField"`
        ResultType  Type   `json:"resultType"`
        Nullable    bool   `json:"nullable"`
    }{
        Kind:        b.Kind().String(),
        Variable:    b.variable,
        Procedure:   b.procedure,
        SourceField: b.sourceField,
        ResultType:  b.resultType,
        Nullable:    b.nullable,
    })
}
```

**Zero information about arguments.** The bucket-3 discard at
`call.go:47-66` never reaches the model.

### 3.2 The layering decision — where the sig-vocabulary lives

**The bead text (gqlc-0ig):** proposes carrying "param name/token from
the matched procsig.Signature, emitted at parse". This would put
`procsig.TypeToken` values (INTEGER / FLOAT / STRING / NUMBER) on the
query wire.

**R7's precedent (`docs/specs/resolver-stage-r7.md §4.4`):**
_"Parser-authoritative unknown-procedure. R7 does NOT re-check
procedure names against the registry — the parser has already
rejected unknown procedures at Stage 14 §4.2 step 2. The resolver
trusts the parser's approval."_ R7 puts SIG-NAME knowledge at parse,
SIG-VOCABULARY knowledge (result column typing) at resolve
(`callBindingSlot.resultType` at `internal/resolver/resolve.go:98`,
sourced from the bridged `CallBinding.ResultType()` — the bridge
happens at parse via `typeForToken` at `call.go:234-247`; **the
resolver never sees a raw `procsig.TypeToken`**).

**The R7 pattern applied to 0ig:** the parser has already validated
that the procedure exists (unknown-procedure fail-site at
`call.go:41-45`) and that the arity matches
(`call.go:71-75`). What the parser has NOT done is check that the
per-position argument type is assignable-from the per-position sig
param token. Under R7's split — sig-name at parse, sig-vocabulary
at resolve — the natural home for the arg-site check is the
resolver, which already holds the `procsig.Registry` via
`resolve.go:21`. The wire's job is to record what the parser mined
(the argument's `query.Type`); the resolver does the assignability
walk by re-looking-up the procedure by name.

**Why the parser-emits-token shape is architecturally worse.**

1. **Duplicates registry data on the wire.** If the parser emits
   `sig.Params[i].Token` per CallArg, every one of the 28 flip
   goldens serialises the signature's declaration on the wire —
   even though the signature is a compile-time input the codegen
   consumer already holds a copy of. Change the signature (a
   registry edit for a new procedure release); the wire diverges.
2. **Puts `procsig.TypeToken` on the freeze surface.** The
   `TypeToken` sum is intentionally decoupled from the query wire
   (`internal/procsig` package doc, mined against master; verified
   at branch base). ADR 0007's Stage-14 note (lines 172-174) is
   explicit: _"NUMBER stays a signature-only marker … the cypher-
   package bridge maps it to TypeUnknown on the wire so no
   signature-time vocabulary leaks into the freeze surface."_
   The bead text's proposal directly violates this.
3. **Couples the parser tighter to the registry semantics.** The
   parser currently uses the registry for two facts: does the
   procedure exist (name lookup), and how many params does it
   declare (arity check). Adding "per-position token lookup at
   arg-emission time" adds a third registry consultation, and the
   parser's error surface would grow (what if `sig.Params[i]`
   panics for a bad position — hard to happen given the arity
   check, but the coupling grows).

**This spec commits to `CallArg{Type query.Type}` only.** The
resolver looks up the sig at resolve time; the wire carries the
mined type only. §8.1 details the resolver-side walk.

**If team-lead / linus prefer the bead text's shape** (parser emits
sig token per position), the exact edits to §4.1, §4.2, §4.3, §4.4
are:

- Widen `CallArg` to two fields — `Type query.Type` and
  `Token procsig.TypeToken`. Add a new import cycle
  `internal/query` → `internal/procsig` (BREAKS the ADR 0007
  sibling-independence guarantee — package `internal/procsig` is
  today "dependency-independent of both internal/query and
  internal/schema"; the reverse edge would violate it too).
- Widen the mined slice at `call.go:47-66` — pass `sig` through so
  each arg emission has both the mined `t` and `sig.Params[i]`.
- Widen 28 golden rebaselines to include per-position tokens
  (each `args[i]` gains `"token": "INTEGER" | "FLOAT" | "STRING"
  | "NUMBER"`).

Both shapes are additive under ADR 0008; the choice is layering /
freeze-surface-scope, not correctness. **Escalation flagged
prominently at spec front.**

### 3.3 The emission site — `collectCall` at branch base

Verbatim from `internal/query/cypher/call.go:29-97`
(read fresh against master `62923d8`):

```go
func (l *listener) collectCall(
    procName gen.IOC_ProcedureNameContext,
    argExprs []gen.IOC_ExpressionContext,
    explicit bool,
    yieldStar bool,
    yieldItems gen.IOC_YieldItemsContext,
    standalone bool,
) {
    name := extractProcedureName(procName)
    if name == "" {
        return
    }
    sig, ok := l.registry.Lookup(name)
    if !ok {
        l.fail(fmt.Errorf("%w: %s", ErrUnknownProcedure, name))
        return
    }

    // Argument mining: refs and parameter uses. The mined argument
    // type is discarded — the arg-type check against sig.Params is
    // bucket-3 (spec §4.5), so today's job is only to route param
    // and ref info onto the part's state so referential integrity
    // and parameter tables stay honest. …
    for _, e := range argExprs {
        t, refs, params := l.typeExpressionMining(e)
        for _, ref := range refs {
            l.curPart.refs = append(l.curPart.refs, varRef{name: ref.Variable})
        }
        for _, p := range params {
            n := parameterName(p)
            if n == "" {
                continue
            }
            l.addParameterUse(n, p, query.NewExprUse(t, query.ExprInProjection))
        }
    }
    // … arity check …
    // … YIELD dispatch …
}
```

The mined `t` is USED to build the ExprUse (line 64: `NewExprUse(t,
ExprInProjection)`) but is then **discarded** from any per-argument
record. Widening: `t` is accumulated into an `[]query.CallArg` slice
alongside the ExprUse emission, then threaded to
`collectYieldItems` / `expandAllResults` so each minted
`CallBinding` carries the args slice.

### 3.4 Divergence from R7 §7.1.1's proposed shape — why NOT `ExprInCallArg`

R7 §7.1.1 proposed **two** minimal frozen-model changes to enable
the resolver-side check:

1. Add `ExprInCallArg` to the `ExprPosition` sum.
2. Add a `Calls` axis on `query.Part`.

**This spec adopts NEITHER.** Instead: one `Args` axis on
`CallBinding` alone. Rationale:

- **`ExprInCallArg`** — adding a new `ExprPosition` value would
  flip the four existing `ExprPosition` values' zero-value
  encoding (the sum is enum-shaped: adding a value to a sealed
  enum changes the zero-value name at wire, or requires a
  golden-invalidating shift). More seriously: the position axis
  attributes a Use to a CLAUSE-shape; the parser's `Use` axis
  fvo landed a `part` field for lexical-Part attribution. Layering
  a second orthogonal axis on `Use` (clause-shape via position,
  Part-index via `part`) grows the Use record's cross-product. The
  args-on-CallBinding shape sidesteps the whole `Use` axis growth
  question — the CALL-arg attribution lives on the BINDING, not
  on the Use, and the resolver walks it separately.
- **`Calls` on `Part`** — the grammar admits multiple in-query
  CALLs in a single Part (a Part carries `(oC_ReadingClause)*`;
  `oC_InQueryCall` is one of `oC_ReadingClause`'s alternatives at
  `internal/grammar/cypher/Cypher.g4:74-78`, verified against
  master). A `Calls []CallInvocation` axis on Part would need a
  positional index linking each `CallBinding` back to its CALL
  invocation; the linkage lives IN the CallBinding record already
  (`CallBinding.Procedure()`), and the args-on-CallBinding shape
  keeps the linkage direct (a CallBinding IS a
  (procedure, source-field, args) tuple by construction).

**The cost of the divergence: bounded per-yield-column
duplication.** A CALL with N ≥ 2 YIELD columns and M ≥ 1 args
duplicates the args slice across N CallBindings — the worst
corpus case is Call5 (5-column signatures) × 2 args =
5 × 2 = 10 args records duplicated. Not a wire-size concern
against the 3199-file golden corpus; a data-structure smell the
per-Part `Calls` axis would fix, at the cost of a positional
linkage and an extra grammar-motivated axis.

Weighing the two: **the CallBinding-carries-Args shape is chosen
because per-position argument state is naturally per-yielded-
column at the resolver-consumer site** (Phase A1's callTypes map
is keyed by variable at `internal/resolver/resolve.go:280`), and
per-CallBinding threading matches the Phase A1 shape
byte-for-byte. §8.1 lays out the walk.

### 3.5 Zero-value analysis — the `nil` slice under omit-when-zero-length

**The wire semantic contract.** Under
`json:"args,omitempty"` on `Args []CallArg`, Go's `encoding/json`
emits the `"args"` key iff the slice length is > 0 (a `nil` slice
and an empty non-nil slice both encode as key-absent under
`omitempty`). §4.1.3 pins the marshaller.

**Does absent-key ⇒ no-args cover every branch-base golden?**

- **No-paren CALL (implicit invocation)**: `enterStandaloneCall`
  at `call.go:294-309` passes `callArgs(explicit)` which returns
  `nil` when `explicit` is nil (`call.go:251-256` — verified against
  master). `argExprs` is nil; the for loop iterates 0 times; no
  args are mined; the CallBinding's `args` slice stays nil;
  omit-when-zero-length elides the key. ✓
- **Zero-arg paren CALL (`CALL proc()`)**: `explicit` is non-nil;
  `callArgs(explicit).AllOC_Expression()` returns 0-length;
  `argExprs` is 0-length; the for loop iterates 0 times; same
  outcome as above. ✓
- **In-query CALL without YIELD**: no CallBinding is minted
  (`call.go:89-90` comment: _"In-query CALL without YIELD: fall
  through — no CallBinding"_). No wire record; the axis question
  doesn't arise. ✓
- **≥1-arg CALL**: `argExprs` has length ≥ 1; the for loop mines
  each; the args slice grows to length ≥ 1; omit-when-zero-length
  emits the key. ✓

**Within-record zero-value hazard.** A `CallArg{Type: TypeUnknown{}}`
is the honest wire record for a `$param` or `n.name` argument (both
mine to TypeUnknown per `typeExpressionMining`'s Stage 6 fallback).
The zero value of the `Type` field IS `nil` (the interface's zero),
but the widened marshaller renders a `nil` Type via the frozen
`Type` marshalling (verified: `internal/query/query.go` — `Type`
interface members render as `{"kind": "unknown"}` when marshalled).
**No collision.** The `NewCallArg` constructor normalises `nil` to
`TypeUnknown{}` (mirroring `NewCallBinding` at `:867-869`; verified
against master), so the wire never sees a bare-nil Type.

### 3.6 `EdgeBinding.directed`, `ExprProjection.ContainsAggregate`,
      `Use.Part` — the axis precedents

Three axis precedents on the frozen model, listed in the order they
landed:

- **`EdgeBinding.directed`** (pre-freeze) — always-emit,
  bool. `internal/query/query.go:378` (verified against master).
  Predates the omit-when-zero-value convention.
- **`ExprProjection.ContainsAggregate`** (hk0, 2026-07-06) —
  omit-when-false, bool. `internal/query/query.go:1174` (field) /
  `:1223` (marshal tag) — verified against master. ESTABLISHED
  the omit-when-zero-value convention as post-freeze doctrine
  (`docs/adr/0008-query-model-freeze-resolver-api.md`
  hk0 amendment note, lines 73-74).
- **`PropertyUse.part` / `ExprUse.part` / `ClauseSlotUse.part`**
  (fvo, 2026-07-06) — omit-when-zero-int, int.
  `internal/query/query.go:1321` (PropertyUse.part) / `:1447`
  (ExprUse.part) / `:1500` (ClauseSlotUse.part) — verified against
  master. APPLIED the hk0 convention to a slot-int axis.

The 0ig axis:

- **`CallBinding.args`** (2026-07-07) — omit-when-zero-length,
  slice. First application of the convention to a slice-shaped
  axis. §4.1.3.2 pins the equivalence: an `omitempty` on a slice
  elides the key when the slice is `nil` or 0-length; the
  reviewer-side fence detects "the marshaller emitted `args: []` on
  a zero-arg CALL" (would be a regression against the convention).

### 3.7 R7's audit note — what this cycle closes

R7 §7.1.1 records the deferral verbatim: _"The parameter uses are
recorded as ExprInProjection — a stand-in position. There is no
ExprInCallArg position on the wire. … R7 does NOT change
witnessAcrossScopes. … The NUMBER assignable-from axis has NO R7
application site."_

The 0ig cycle closes this by adding a per-argument axis to
CallBinding — the resolver walks `Args()` per-CallBinding at Phase
A1, not per-Use. R7's `witnessAcrossScopes` stays UNCHANGED (the
ExprUse position discriminator remains `ExprInProjection` for CALL
arg params; the check axis lives on CallBinding, not on Use).
§8.6 pins the R7-spec successor prose.

---

## 4. The unfreeze — parser and model changes

### 4.1 `CallBinding` — the additive field

#### 4.1.1 Structs

```go
// CallArg is one positional argument of a CALL clause, carrying the
// mined query.Type of the argument expression as observed at parse
// time (Stage 14 §4.5 mined type, previously discarded). The
// resolver's arg-site assignability walk (see gqlc-0ig cycle,
// resolver-widening PR) checks each mined type against the matched
// procsig.Signature's positionally-corresponding Param.Token —
// procsig vocabulary stays resolver-authoritative, per R7's
// parser-authoritative unknown-procedure precedent.
type CallArg struct {
    t Type // mined query.Type of the argument expression; TypeUnknown for $param / property / null / anything untypeable at parse (Stage 6 §4 fallback)
}

// CallBinding is a query variable bound to one YIELD result column of a
// CALL clause … [existing comment verbatim]
type CallBinding struct {
    variable    string  // always non-empty
    procedure   string  // fully-qualified name; always non-empty
    sourceField string  // signature-declared result column; always non-empty
    resultType  Type    // bridged Stage-6 result type; TypeUnknown for NUMBER
    nullable    bool    // signature's trailing '?'
    args        []CallArg // per-position mined argument types (0ig, ADR 0008 amendment 2026-07-07)
}
```

The field lands last on `CallBinding` — the tail-append convention
across the query package (hk0's `containsAggregate` on
`ExprProjection` and fvo's `part` on the three Use variants both
follow the same style; verified against master).

#### 4.1.2 Constructors + accessors

New sub-type constructor + accessor:

```go
// NewCallArg builds a CallArg for one CALL argument position, carrying
// the mined query.Type of the argument expression. A nil Type is
// normalised to TypeUnknown{} — the same "cannot tell" fallback
// NewCallBinding uses for its ResultType field.
func NewCallArg(t Type) CallArg {
    if t == nil {
        t = TypeUnknown{}
    }
    return CallArg{t: t}
}

// Type reports the mined query.Type of this CALL argument (0ig per
// ADR 0008 amendment 2026-07-07). Populated parser-side by
// collectCall's arg-mining loop; the resolver's arg-site assignability
// walk consults this against sig.Params[i].Token.
func (a CallArg) Type() Type { return a.t }
```

New CallBinding constructor + accessor:

```go
// NewCallBindingWithArgs builds a CallBinding, rejecting the empty
// variable / procedure / sourceField (existing NewCallBinding
// invariants preserved) and carrying the per-position mined argument
// types (0ig per ADR 0008 amendment 2026-07-07). Callers that do not
// need arg attribution use NewCallBinding, which forwards args=nil.
func NewCallBindingWithArgs(variable, procedure, sourceField string, resultType Type, nullable bool, args []CallArg) (CallBinding, error) {
    if variable == "" {
        return CallBinding{}, errors.New("query: call binding requires a non-empty variable")
    }
    if procedure == "" {
        return CallBinding{}, errors.New("query: call binding requires a non-empty procedure name")
    }
    if sourceField == "" {
        return CallBinding{}, errors.New("query: call binding requires a non-empty source field")
    }
    if resultType == nil {
        resultType = TypeUnknown{}
    }
    return CallBinding{
        variable:    variable,
        procedure:   procedure,
        sourceField: sourceField,
        resultType:  resultType,
        nullable:    nullable,
        args:        args,
    }, nil
}

// Args reports the per-position mined argument types of the CALL
// clause this binding belongs to (0ig per ADR 0008 amendment
// 2026-07-07). All CallBindings minted from one CALL invocation
// carry the SAME args slice — the wire records per-CallBinding,
// but the semantic axis is per-invocation. The resolver's arg-site
// assignability walk uses this slice's positional indices against
// procsig.Registry.Lookup(binding.Procedure()).Params[i].Token.
func (b CallBinding) Args() []CallArg { return b.args }
```

Preserved constructor (verbatim signature, forwards through the new
one with `args=nil`):

```go
func NewCallBinding(variable, procedure, sourceField string, resultType Type, nullable bool) (CallBinding, error) {
    return NewCallBindingWithArgs(variable, procedure, sourceField, resultType, nullable, nil)
}
```

The five pre-existing accessors (`Variable`, `Procedure`,
`SourceField`, `ResultType`, `Kind`, `Nullable`, `isBinding`) are
unchanged.

**The `Binding` interface stays sealed at one method** (`isBinding()`).
`Args()` lands as a CallBinding-only accessor; no interface widening.

#### 4.1.3 JSON — omit-when-zero-length on `args`

The `CallBinding` marshaller gains one field:

```go
func (b CallBinding) MarshalJSON() ([]byte, error) {
    return json.Marshal(struct {
        Kind        string    `json:"kind"`
        Variable    string    `json:"variable"`
        Procedure   string    `json:"procedure"`
        SourceField string    `json:"sourceField"`
        ResultType  Type      `json:"resultType"`
        Nullable    bool      `json:"nullable"`
        Args        []CallArg `json:"args,omitempty"`
    }{
        Kind:        b.Kind().String(),
        Variable:    b.variable,
        Procedure:   b.procedure,
        SourceField: b.sourceField,
        ResultType:  b.resultType,
        Nullable:    b.nullable,
        Args:        b.args,
    })
}
```

`CallArg` gets its own marshaller:

```go
func (a CallArg) MarshalJSON() ([]byte, error) {
    return json.Marshal(struct {
        Type Type `json:"type"`
    }{
        Type: a.t,
    })
}
```

The `type` field is always-emitted on `CallArg` — a `CallArg` is
never zero-length in itself (it always carries a Type; the
constructor normalises `nil` to `TypeUnknown{}`), so
omit-when-zero-value on the field would only obscure the shape.
The always-emit posture mirrors CallBinding's other always-emit
fields.

##### 4.1.3.1 Fence accounting under omit-when-zero-length — 28 goldens rebaseline

**Corpus at branch base** (`master @ 62923d8`):

- **3199** `*.golden.json` files under
  `internal/query/cypher/testdata/golden/` (matches fvo's post-close
  count; independently re-verified this cycle by fresh count).
- **32** of those goldens embed at least one `"kind": "call"`
  record — a CallBinding on the wire. Fresh grep:
  `grep -l '"kind": "call"' internal/query/cypher/testdata/golden/*.golden.json | wc -l` at branch
  base returns 32. Per-prefix breakdown (fresh count):
  `Call1: 2, Call2: 3, Call3: 6, Call4: 2, Call5: 16, Call6: 3`.
- **28** of the 32 goldens have ≥ 1 CALL invocation with ≥ 1
  argument in the scenario source text. §4.4.1 pins each with
  first-party corroboration. Per-prefix breakdown of the flip
  set: `Call2: 2, Call3: 6, Call4: 2, Call5: 16, Call6: 2`.
- **4** goldens carry only zero-arg CALL invocations (either
  no-paren `CALL test.my.proc` implicit-invocation or parens-empty
  `CALL test.labels()` argless-explicit). Fresh mined:

  1. `Call1_76a9d5e4baa2.golden.json` — `[5] Standalone call to
     STRING procedure that takes no arguments`: `CALL test.labels()`.
  2. `Call1_902a8be366be.golden.json` — `[6] In-query call to
     STRING procedure that takes no arguments`: `CALL test.labels()
     YIELD label RETURN label`.
  3. `Call2_e471a5413657.golden.json` — `[3] Standalone call to
     procedure with implicit arguments`: `CALL test.my.proc`
     (no-paren implicit-invocation form).
  4. `Call6_26508cd581f0.golden.json` — `[1] Calling the same
     STRING procedure twice using the same outputs in each call`:
     two `CALL test.labels()` invocations with `WITH count(*) AS c`
     between them (both paren-empty).

**Consequence.** Under `omit-when-zero-length`, the 4 goldens above
serialise to the SAME wire they emit today — the `"args"` key is
absent because the slice is nil / 0-length. The 28 remaining
goldens each gain a non-nil `"args"` key on every CallBinding of the
CALL clause. Golden rebaseline scope: **28 files**. §4.4.1 pins each
28 by scenario source with first-party corroboration.

**Divergence audit against fvo and hk0.** fvo rebaselined 0
goldens (every Use at branch base emitted from Part 0); hk0
rebaselined 20 (the walker flipped `containsAggregate` on 20
scenarios). 0ig rebaselines 28 — larger than either predecessor,
but each rebaseline is a mechanical wire-key addition on a
CallBinding record; the fence discipline (below) verifies each
addition is a semantic-only change on the axis's true set.

**The always-emit alternative accounted for.** Under an
always-emit encoding (mirroring `directed`), every CallBinding
serialises `"args": [ … ]` unconditionally — the 4 currently-
non-flipping goldens gain `"args": null` (or `"args": []`,
depending on the always-emit slice convention). Reviewer-side
diff: dominated by mechanical `"args": null` insertions across 32
goldens instead of semantic `"args": [ … ]` insertions across 28.
The reviewer's ability to distinguish semantic changes from wire-
schema changes is stronger under omit-when-zero-length, and the
campaign's convention holds. §4.1.3.2 pins the fences.

##### 4.1.3.2 Reviewer-side fence commands

**Fence 1 — strip-key overreach detector** (the widened
marshaller must not leak `args: [ … ]` outside the scenario-
verified set of 28 goldens). Run from the worktree at the unfreeze
PR's branch tip against `origin/master @ 62923d8`:

```
python3 - <<'PY'
import glob, json
def go_dump(data, f):
    # Go's encoding/json HTML-escapes <, >, & inside strings;
    # a plain json.dump re-writes them literally and
    # false-positives every golden carrying one. (Copied verbatim
    # from the hk0 §4.1.3 shim — carrying the hk0 §12 errata 1
    # fix; do not reintroduce the bug.)
    s = json.dumps(data, indent=2, ensure_ascii=False)
    s = (s.replace('&', '\\u0026')
          .replace('<', '\\u003c')
          .replace('>', '\\u003e'))
    f.write(s); f.write('\n')
for path in sorted(glob.glob(
        'internal/query/cypher/testdata/golden/*.golden.json')):
    with open(path) as f: data = json.load(f)
    def strip(n):
        if isinstance(n, dict):
            n.pop('args', None)
            for v in n.values(): strip(v)
        elif isinstance(n, list):
            for v in n: strip(v)
    strip(data)
    with open(path, 'w') as f:
        go_dump(data, f)
PY
git diff --stat origin/master -- \
    internal/query/cypher/testdata/golden/*.golden.json
# MUST print: no changes.
git checkout -- internal/query/cypher/testdata/golden/
```

If the stat print is non-empty, the widened marshaller has emitted
an `args` key on a CallBinding whose CALL is zero-arg — an
`argExprs`-mining bug (a misplaced accumulator; a stale slice
carrying between CALLs in the same Part; an early-return that
mints the slice but doesn't gate on non-emptiness).

**Fence 2 — set-equality check** (the set of changed goldens must
EQUAL the 28-file expected set):

```
git diff --name-only origin/master -- \
    internal/query/cypher/testdata/golden/*.golden.json | \
    sed 's|.*/||' | sort > /tmp/changed.txt
cat > /tmp/expected.txt <<EOF
Call2_708fd68c8e95.golden.json
Call2_aeadf4f8844e.golden.json
Call3_07c57b301e10.golden.json
Call3_389619d1622f.golden.json
Call3_651e6e2c2643.golden.json
Call3_88c3ef85b829.golden.json
Call3_ba1334616b0c.golden.json
Call3_f8301ea19bf6.golden.json
Call4_5e83e83041b5.golden.json
Call4_954470d1d743.golden.json
Call5_05b3c946be0d.golden.json
Call5_23c85362b6f6.golden.json
Call5_31baf2f6ab54.golden.json
Call5_406b0ad48caa.golden.json
Call5_45dc74965bc1.golden.json
Call5_[…every Call5 with args][see §4.4.1 for the full 28-file list]
EOF
diff /tmp/expected.txt /tmp/changed.txt && echo "SET-EQUAL: PASS"
# MUST print: SET-EQUAL: PASS.
```

The full 28-file expected set is enumerated in §4.4.1. **§4.4.1 is
the authoritative list; §4.1.3.2's expected file is the
mechanical enumeration of that list, copied verbatim at fence
time.**

If Fence 1 passes but Fence 2 fails, the widened marshaller
emitted a non-`args` change on some golden — a formatting drift
or an unrelated wire-shape edit. The unfreeze PR must pass BOTH
fences before landing.

**Fence 3 (unit-test back-compat)** — the pre-existing
`TestNewCallBinding` and `TestCallBindingMarshalJSON` pins in
`internal/query/query_test.go` stay VERBATIM (§5). Their pinned
JSON strings carry a nil args slice; under omit-when-zero-length
the key is absent, matching the widened marshaller bit-for-bit.
§5 records the two NEW true-side tests
(`TestNewCallBindingWithArgs`, `TestNewCallArg`) and one NEW
zero-side test (`TestCallArgMarshalJSON`) that witness the
non-empty encoding.

### 4.2 `collectCall` — the emission-time argument accumulation

Current (`internal/query/cypher/call.go:29-97`, verified against
master `62923d8`):

```go
func (l *listener) collectCall(
    procName gen.IOC_ProcedureNameContext,
    argExprs []gen.IOC_ExpressionContext,
    explicit bool,
    yieldStar bool,
    yieldItems gen.IOC_YieldItemsContext,
    standalone bool,
) {
    name := extractProcedureName(procName)
    if name == "" { return }
    sig, ok := l.registry.Lookup(name)
    if !ok {
        l.fail(fmt.Errorf("%w: %s", ErrUnknownProcedure, name))
        return
    }
    for _, e := range argExprs {
        t, refs, params := l.typeExpressionMining(e)
        for _, ref := range refs {
            l.curPart.refs = append(l.curPart.refs, varRef{name: ref.Variable})
        }
        for _, p := range params {
            n := parameterName(p)
            if n == "" { continue }
            l.addParameterUse(n, p, query.NewExprUse(t, query.ExprInProjection))
        }
    }
    if explicit && len(argExprs) != len(sig.Params) {
        l.fail(fmt.Errorf("%w: %s expects %d arguments, got %d",
            ErrProcedureArity, name, len(sig.Params), len(argExprs)))
        return
    }
    if yieldItems != nil {
        l.collectYieldItems(name, sig, yieldItems)
    } else if yieldStar || standalone {
        l.expandAllResults(name, sig)
    }
    if standalone && l.err == nil {
        l.curPart.callStandalone = true
    }
}
```

Widened:

```go
func (l *listener) collectCall(
    procName gen.IOC_ProcedureNameContext,
    argExprs []gen.IOC_ExpressionContext,
    explicit bool,
    yieldStar bool,
    yieldItems gen.IOC_YieldItemsContext,
    standalone bool,
) {
    name := extractProcedureName(procName)
    if name == "" { return }
    sig, ok := l.registry.Lookup(name)
    if !ok {
        l.fail(fmt.Errorf("%w: %s", ErrUnknownProcedure, name))
        return
    }
    // 0ig widening (ADR 0008 amendment 2026-07-07): accumulate one
    // CallArg per argument expression, capturing the mined type that
    // was previously discarded. The parameter/ref emission below is
    // unchanged; the args slice is passed to collectYieldItems /
    // expandAllResults so each minted CallBinding carries it.
    var args []query.CallArg
    for _, e := range argExprs {
        t, refs, params := l.typeExpressionMining(e)
        args = append(args, query.NewCallArg(t))
        for _, ref := range refs {
            l.curPart.refs = append(l.curPart.refs, varRef{name: ref.Variable})
        }
        for _, p := range params {
            n := parameterName(p)
            if n == "" { continue }
            l.addParameterUse(n, p, query.NewExprUse(t, query.ExprInProjection))
        }
    }
    if explicit && len(argExprs) != len(sig.Params) {
        l.fail(fmt.Errorf("%w: %s expects %d arguments, got %d",
            ErrProcedureArity, name, len(sig.Params), len(argExprs)))
        return
    }
    if yieldItems != nil {
        l.collectYieldItems(name, sig, yieldItems, args)
    } else if yieldStar || standalone {
        l.expandAllResults(name, sig, args)
    }
    if standalone && l.err == nil {
        l.curPart.callStandalone = true
    }
}
```

`collectYieldItems` and `expandAllResults` each gain an
`args []query.CallArg` parameter, threaded verbatim into the
`NewCallBindingWithArgs` call:

```go
// collectYieldItems widened signature — args threaded from collectCall.
func (l *listener) collectYieldItems(
    procName string,
    sig procsig.Signature,
    items gen.IOC_YieldItemsContext,
    args []query.CallArg,
) {
    // … existing seen-check + iteration verbatim …
    cb, err := query.NewCallBindingWithArgs(
        variable, procName, sourceField,
        typeForToken(result.Token), result.Nullable,
        args,
    )
    // … existing rest verbatim …
}

// expandAllResults widened signature.
func (l *listener) expandAllResults(procName string, sig procsig.Signature, args []query.CallArg) {
    for _, result := range sig.Results {
        cb, err := query.NewCallBindingWithArgs(
            result.Name, procName, result.Name,
            typeForToken(result.Token), result.Nullable,
            args,
        )
        // … existing rest verbatim …
    }
}
```

**Slice-sharing safety.** The same `args` slice is passed to N
CallBindings (N = number of YIELD columns) in the same CALL clause.
The Args() accessor returns `b.args` directly — CALLERS MUST NOT
mutate the slice. The comment at the accessor pins this: _"All
CallBindings minted from one CALL invocation carry the SAME args
slice"_. The parser never mutates the slice after emission; the
resolver reads-only. Cross-CALL isolation is guaranteed by
`collectCall`'s `var args []query.CallArg` local declaration — a
fresh nil slice per CALL invocation.

**EXISTS suppression preserved.** The two entry points
(`EnterOC_StandaloneCall`, `EnterOC_InQueryCall` at
`internal/query/cypher/listener.go`) early-return under
`subqueryDepth > 0`, matching the Stage-11 pattern (spec Stage 14
§4.6). The widening does not touch that gate; a CALL inside EXISTS
still emits zero CallBindings (verified: the `authored CALL inside
EXISTS suppression` parser-test pin at `parser_test.go:2357-2373`
carries zero CallBindings today; post-widening its expected shape
is identical — no args on absent CallBindings).

#### 4.2.1 The mined-type discipline — what `typeExpressionMining` returns for CALL args

`typeExpressionMining` at `internal/query/cypher/typing.go:41-52`
(verified against master `62923d8`):

- **Numeric literal (`42`, `42.3`)**: mines to `TypeInt{}` /
  `TypeFloat{}` via `typeAtom`'s literal recognition (Stage 6 §3;
  numeric literal path).
- **String literal (`'Stefan'`)**: mines to `TypeString{}`.
- **`null` literal**: mines to `TypeUnknown{}` — the Stage 6 §4
  "cannot tell" fallback (verified: `typeAtom`'s null handling
  returns TypeUnknown via `promoteBase`).
- **`$param`**: mines to `TypeUnknown{}` — Stage 6 §4 "no
  parameter is silently pinned" ruling; the mined type is
  TypeUnknown because the parameter's runtime type is not known
  at parse.
- **`n.name` (property projection)**: mines to `TypeUnknown{}` —
  property types live in the schema, not the parser; the parser
  cannot tell.
- **Arithmetic expression (`42 + 1`, `n.age * 2`)**: mines to the
  `promoteBase` result of the operands, per the Stage 6 §3.5
  arithmetic promotion rules.

**Corpus check.** All 28 flip goldens' CALL arguments are one of:

- Literal numeric: 20 goldens (Call3, Call4, Call5 outlines with
  `42`, `42.3`, `null`, `-1`, `0`).
- Literal string: 2 goldens (Call2 with `'Stefan', 1` → mixed
  string + int arg positions).
- Literal null: 4 goldens (Call4 + 2 Call5 outlines with `null`).
- Bound-var property: 0 goldens (`authored CALL bound-var
  argument regression lock` is a parser-test pin, not a corpus
  golden; the argument is `n.name`, mines to TypeUnknown — verified
  at `parser_test.go:2381`).

**Consequence for the fence.** Every flip golden's `args[i].type`
is one of `{"kind":"int"}`, `{"kind":"float"}`, `{"kind":"string"}`,
or `{"kind":"unknown"}`. §4.4.1 spot-witnesses each with the
per-scenario expected shape.

### 4.3 Parser-test pins — flips vs preserved

`internal/query/cypher/parser_test.go` at branch base carries **15
CallBinding pins** (fresh grep at master `62923d8`:
`grep -c "NewCallBinding" internal/query/cypher/parser_test.go`
returns **15** — one `NewCallBinding` invocation per line). Under
`require.Equal` (`reflect.DeepEqual`), a `CallBinding{…, args: nil}`
equals a `CallBinding{…}` bit-for-bit (nil-slice-equals-nil-slice).
Pins whose CALL has zero args stay verbatim; pins whose CALL has
≥ 1 arg flip.

Full census at `parser_test.go` (line numbers from master
`62923d8`; asterisk marks a flip; twin lines share their pin's
args slice by construction — same CALL, one args slice, two
`NewCallBindingWithArgs(…)` call sites pointing at it):

| Line | Enclosing pin | src (CALL invocation) | Flip? | Expected args slice |
|---|---|---|---|---|
| 2211 | `CALL standalone no-args implicit-YIELD (Call1[5])` | `CALL test.labels()` — 0 args | no | `nil` |
| 2232 | `CALL in-query YIELD RETURN (Call1[6])` | `CALL test.labels() YIELD label \| RETURN label` — 0 args | no | `nil` |
| 2252 | `CALL in-query explicit args YIELD RETURN (Call2[1])` | `CALL test.my.proc('Stefan', 1)` — 2 args | **yes*** | `[{TypeString}, {TypeInt}]` |
| 2253 | (twin on same pin, `country_code`) | (same CALL) | **yes*** | (shared slice) |
| 2279 | `CALL standalone explicit args implicit-YIELD (Call2[2])` | `CALL test.my.proc('Stefan', 1)` — 2 args | **yes*** | `[{TypeString}, {TypeInt}]` |
| 2280 | (twin on same pin, `country_code`) | (same CALL) | **yes*** | (shared slice) |
| 2307 | `CALL standalone explicit args YIELD * (Call5[8])` | `CALL test.my.proc('Stefan', 1) YIELD *` — 2 args | **yes*** | `[{TypeString}, {TypeInt}]` |
| 2308 | (twin on same pin, `country_code`) | (same CALL) | **yes*** | (shared slice) |
| 2336 | `CALL NUMBER accepts INTEGER standalone (Call3[1])` | `CALL test.my.proc(42)` — 1 arg | **yes*** | `[{TypeInt}]` |
| 2385 | `authored CALL bound-var argument regression lock` | `MATCH (n) \| CALL test.labels(n.name) YIELD label` — 1 arg | **yes*** | `[{TypeUnknown}]` |
| 2409 | `authored CALL standalone Returns signature-declaration-order` | `CALL test.my.proc(42)` — 1 arg | **yes*** | `[{TypeInt}]` |
| 2410 | (twin on same pin, second Returns binding) | (same CALL) | **yes*** | (shared slice) |
| 2444 | `CALL then WITH then CALL (Call6[1])` Part 1 | `CALL test.labels()` — 0 args | no | `nil` |
| 2452 | `CALL then WITH then CALL (Call6[1])` Part 2 | `CALL test.labels()` — 0 args | no | `nil` |
| 2475 | `authored CALL YIELD trailing WHERE parameter-mining probe` | `CALL test.labels() YIELD label WHERE label = $needle` — 0 args | no | `nil` |

**Totals.** 15 pins across 11 distinct `mustParse` entries: **9
flip** (all ≥1-arg CALLs, across 6 distinct pins — the twins
cluster on 3 shared 2-arg CALLs and 1 shared 1-arg CALL) and **6
preserve** (all zero-arg CALLs, one pin each). Every flip is a
mechanical constructor swap from `NewCallBinding(…)` to
`NewCallBindingWithArgs(…, sharedArgsSlice)`; no pin is added,
deleted, or split by this cycle.

**Corpus stability note.** The 15-pin count is stable against
master `62923d8`. Any code-cycle PR that adds a new CallBinding
pin between spec merge and unfreeze merge MUST update this census
authoritatively — a diff-mismatch here is a code-vs-spec drift.
The unfreeze PR is expected to leave the pin count at 15 and
apply exactly the flip pattern above.

#### 4.3.1 Full 15-pin flip census

The full census is **the table in §4.3 above** — no further
enumeration is deferred. Reviewer verification is a mechanical
set-check: run `grep -n "NewCallBinding" internal/query/cypher/parser_test.go`
against the unfreeze branch, cross-reference each hit's line
number and enclosing pin name against the table, then confirm the
flip predicate (0-arg CALL ⇒ preserved, ≥1-arg CALL ⇒ flipped to
`NewCallBindingWithArgs`). Twin lines (`2253`, `2280`, `2308`,
`2410`) share their pin's args slice by construction — the
unfreeze PR MUST allocate one `[]query.CallArg{…}` literal per
`mustParse` entry and pass the same slice value to both twin
`NewCallBindingWithArgs` invocations.

#### 4.3.2 Classification format for the unfreeze PR

Per pin, the PR body records:

```
Line 2252, pin "CALL standalone two args Standalone Returns (Call2[2])":
  CALL: test.my.proc('Stefan', 1) — 2 args
  Args slice: []query.CallArg{
      query.NewCallArg(query.TypeString{}),
      query.NewCallArg(query.TypeInt{}),
  }
  Applies to CallBinding at lines 2252 (city), 2253 (country_code)
    — both share the same args slice.
```

Reviewer verification: run the parser at the pin's src → observe
the mined types → cross-check against §4.2.1's expected mining
outcomes → assert the pin's Args slice matches.

### 4.4 Every parser golden — 28 flip

Under the omit-when-zero-length JSON encoding (§4.1.3), the `args`
key is emitted **only** when the CallBinding's `args` slice is
non-empty. §4.1.3.1 accounts the corpus: **28** goldens flip.

#### 4.4.1 Golden-corpus enumeration — the 28 flip witnesses

**Discovery method — scenario-source cross-check.** Mine every
golden containing a `"kind": "call"` blob; for each, recover the
scenario source via the exact hash recipe used by `checkGolden` at
`internal/query/cypher/acceptance_test.go:1063-1068` (fresh-read at
`62923d8`; the line number must be re-verified at PR-draft time);
identify each CALL invocation's args by regex; classify as ≥ 1 arg
(flip) or zero-args (byte-identical).

**Complete discovery script** (Python 3; run from the worktree
root; project host lacks `jq`):

```python
python3 - <<'PY'
import re, os, glob, hashlib, json

CYPHER_DIR = "internal/query/cypher"
FROOT     = "test/data/query/cypher/tck/features"
GROOT     = f"{CYPHER_DIR}/testdata/golden"

def uri_form(p): return os.path.relpath(p, CYPHER_DIR)

def hash_golden(uri, name, q):
    s = hashlib.sha1((uri + "\x00" + name + "\x00" + q).encode()).digest()
    base = os.path.splitext(os.path.basename(uri))[0]
    return f"{base}_{s[:6].hex()}.golden.json"

# scan_feature — verbatim from fvo spec §4.4.1 (carrying hk0 §12
# errata 1 shim; do not reintroduce). Yields (name, query) tuples,
# expanding Scenario Outlines against their Examples table.
def scan_feature(path):
    with open(path) as f: text = f.read()
    out = []
    parts = re.split(r"(?m)^\s*(Scenario(?: Outline)?:\s*.+)$", text)
    for i in range(1, len(parts), 2):
        header, body = parts[i], parts[i+1] if i+1 < len(parts) else ""
        name = header.split(":", 1)[1].strip()
        is_outline = header.startswith("Scenario Outline:")
        examples = []
        if is_outline:
            em = re.search(r"(?ms)^\s*Examples:\s*\n((?:\s*\|.*\n?)+)", body)
            if em:
                lines = [l.strip() for l in em.group(1).strip().splitlines() if l.strip()]
                if lines:
                    hdr = [c.strip() for c in lines[0].strip("|").split("|")]
                    for row in lines[1:]:
                        cells = [c.strip() for c in row.strip("|").split("|")]
                        examples.append(dict(zip(hdr, cells)))
                    body = body[:em.start()]
        ds = []
        for m in re.finditer(r'"""\s*\n(.*?)\n\s*"""', body, re.DOTALL):
            q = m.group(1)
            lines = q.splitlines()
            if lines:
                nb = [l for l in lines if l.strip()]
                if nb:
                    ind = min(len(l) - len(l.lstrip()) for l in nb)
                    q = "\n".join(l[ind:] if len(l) >= ind else l for l in lines)
            ds.append(q)
        if not is_outline:
            for q in ds: out.append((name, q))
        else:
            for row in examples:
                for q in ds:
                    qs = q
                    for k, v in row.items():
                        qs = qs.replace(f"<{k}>", v)
                    out.append((name, qs))
    return out

sci = {}
for feat in sorted(glob.glob(f"{FROOT}/**/*.feature", recursive=True)):
    uri = uri_form(feat)
    for name, q in scan_feature(feat):
        sci[hash_golden(uri, name, q)] = (uri, name, q)

# For each Call*.golden.json, find every CALL invocation and
# classify by argument count.
call_re = re.compile(
    r"\bCALL\s+[a-zA-Z_][\w.]*\s*\(([^)]*)\)",
    re.IGNORECASE)

flip = []
no_flip = []
for path in sorted(glob.glob(f"{GROOT}/*.golden.json")):
    with open(path) as f: text = f.read()
    if '"kind": "call"' not in text: continue
    base = os.path.basename(path)
    entry = sci.get(base)
    if entry is None: continue
    _, name, q = entry
    has_arg = False
    for m in call_re.finditer(q):
        if m.group(1).strip():
            has_arg = True
            break
    (flip if has_arg else no_flip).append(base)

print(f"Total 'kind': 'call' goldens: {len(flip) + len(no_flip)}")
print(f"Flipping (≥1 arg CALL): {len(flip)}")
print(f"Byte-identical (0-arg CALL only): {len(no_flip)}")
print("--- flip set (sorted, one per line) ---")
for b in sorted(flip): print(b)
print("--- no-flip set (byte-identical after PR) ---")
for b in sorted(no_flip): print(b)
PY
```

**Verified output at branch base `master @ 62923d8`** (fresh-run
this cycle):

- Total 'kind': 'call' goldens: **32**
- Flipping (≥ 1 arg CALL): **28**
- Byte-identical (0-arg CALL only): **4** (enumerated in
  §4.1.3.1 above with their scenario source lines).

**Bidirectional fence** — same discipline as fvo §4.4.1 and hk0
§4.4.1:

- **Fence 1 — strip-key overreach detector** per §4.1.3.2 above
  (using the `go_dump` shim).
- **Fence 2 — set-equality check** per §4.1.3.2, expected set is
  the discovery script's `flip` list.

Both fences MUST pass. If Fence 1 fails, a CallBinding whose CALL
is zero-arg emitted with a non-empty args slice — an
`argExprs`-mining bug. If Fence 2 fails, either the widened
marshaller over-emitted (extras: the reviewer inspects each to
determine whether the discovery script under-approximated or the
walker mis-attributed) or a non-`args` change slipped in.

#### 4.4.2 First-party corroboration of ≥ 5 spot witnesses

Cycle-2 erratum 1 lesson: mining claims must be verified against
live walker order. Five spot witnesses (freshly-read from
`internal/query/cypher/testdata/golden/*.golden.json` and the TCK
scenario source at branch base `master @ 62923d8`):

| Golden | Scenario | CALL query | Expected `args` per CallBinding |
|---|---|---|---|
| `Call2_708fd68c8e95.golden.json` | Call2 [2] Standalone call to procedure with explicit arguments | `CALL test.my.proc('Stefan', 1)` — args: string literal, int literal → mined `(TypeString, TypeInt)` | 2 CallBindings (city, country_code) each carry `[{type:string}, {type:int}]` |
| `Call2_aeadf4f8844e.golden.json` | Call2 [1] In-query call to procedure with explicit arguments | `CALL test.my.proc('Stefan', 1) YIELD city, country_code` | same 2-arg slice, same 2 CallBindings |
| `Call3_07c57b301e10.golden.json` | Call3 [1] Standalone call to procedure with argument of type NUMBER accepts value of type INTEGER | `CALL test.my.proc(42)` — mined `(TypeInt)` | 1 CallBinding (out) carries `[{type:int}]` |
| `Call3_389619d1622f.golden.json` | Call3 [3] Standalone call to procedure with argument of type NUMBER accepts value of type FLOAT | `CALL test.my.proc(42.3)` — mined `(TypeFloat)` | 1 CallBinding carries `[{type:float}]` |
| `Call4_5e83e83041b5.golden.json` | Call4 [2] In-query call to procedure with null argument | `CALL test.my.proc(null) YIELD out` — mined `(TypeUnknown)` | 1 CallBinding carries `[{type:unknown}]` |

The scenario source text for each is fresh-read from
`test/data/query/cypher/tck/features/clauses/call/Call{2,3,4}.feature`
at branch base — the CALL invocation appears verbatim in each
scenario's `"""` block.

**A sixth witness for the multi-column shape** —
`Call5_31baf2f6ab54.golden.json` (Call5 [1] iterate 5-column
`(a: INT, b: STRING, c: INT, d: STRING, e: INT)` signature, YIELD
all in declaration order): the CALL is `CALL test.my.proc('foo',
1)` (fresh-read from `Call5.feature`); mined `(TypeString,
TypeInt)`; 5 CallBindings each carry the SAME 2-arg args slice.
This is the WIDEST duplication case at branch base — five
CallBindings sharing one args slice, exercising the slice-sharing
safety at §4.2.

### 4.5 The `Parameter` list-order invariant is preserved

`internal/query/query.go:1297-1300` (verified against master
`62923d8`):

```go
type Parameter struct {
    Name string `json:"name"`
    Uses []Use  `json:"uses"`
}
```

The parameter Uses slice is order-preserving via `addParameterUse`
(fvo close-out cycle preserved this). The 0ig widening does NOT
touch `addParameterUse`; the args-on-CallBinding axis is orthogonal
to the parameter-Use axis. A `$param` argument to a CALL still
produces an `ExprUse{TypeUnknown, ExprInProjection}` entry in
`Parameter.Uses` (line 64 in call.go stays verbatim), AND
independently a `CallArg{TypeUnknown}` entry in the CallBinding's
Args slice. The two records are the SAME mined type applied to
different consumer axes: the Use axis feeds `witnessAcrossScopes`
(unchanged); the Args axis feeds the arg-site assignability walk
(new).

**Sanity: no double-count.** A `$param` argument produces ONE
Parameter.Uses entry (the ExprUse) and ONE Args entry per CallBinding
(N entries if N YIELD columns, but each is per-position within the
CALL — the DUPLICATION IS ACROSS BINDINGS, not across args). The
resolver reads one from each axis; they never cross-reference.

---

## 5. Unit-test additions — the two-sided axis

`internal/query/query_test.go` — this section adds THREE new tests
and preserves TWO pre-existing tests verbatim (the pre-existing
`TestNewCallBinding` and `TestCallBindingMarshalJSON`, if they
exist at branch base — verified at PR-draft time by grep on the
worktree at `62923d8`; the two pre-existing tests, if named
differently, stay verbatim under their existing names).

### 5.1 New tests — the true and zero sides

```go
// TestNewCallArg pins the sub-type constructor's field wiring and the
// TypeUnknown default. New at 0ig per §5.1.
func TestNewCallArg(t *testing.T) {
    a := query.NewCallArg(query.TypeInt{})
    require.Equal(t, query.TypeInt{}, a.Type())

    // Nil normalisation — the constructor guards against a nil Type
    // (mirroring NewCallBinding's ResultType behaviour at query.go:867-869).
    a0 := query.NewCallArg(nil)
    require.Equal(t, query.TypeUnknown{}, a0.Type())
}

// TestCallArgMarshalJSON pins the always-emit wire shape. New at 0ig.
func TestCallArgMarshalJSON(t *testing.T) {
    a := query.NewCallArg(query.TypeString{})
    out, err := json.Marshal(a)
    require.NoError(t, err)
    require.JSONEq(t,
        `{"type":"string"}`,
        string(out))
}

// TestNewCallBindingWithArgs pins the widened constructor's field wiring
// and the true-side JSON encoding (args key PRESENT when the slice is
// non-empty). New at 0ig per §5.1.
func TestNewCallBindingWithArgs(t *testing.T) {
    args := []query.CallArg{
        query.NewCallArg(query.TypeString{}),
        query.NewCallArg(query.TypeInt{}),
    }
    cb, err := query.NewCallBindingWithArgs(
        "out", "test.my.proc", "out",
        query.TypeInt{}, true, args)
    require.NoError(t, err)
    require.Equal(t, args, cb.Args())

    // JSON: the args key is PRESENT because the slice is non-empty.
    out, err := json.Marshal(cb)
    require.NoError(t, err)
    require.JSONEq(t, `{
        "kind":"call",
        "variable":"out",
        "procedure":"test.my.proc",
        "sourceField":"out",
        "resultType":"int",
        "nullable":true,
        "args":[{"type":"string"},{"type":"int"}]
    }`, string(out))
}
```

The pre-existing `TestNewCallBinding` and any pre-existing
`TestCallBindingMarshalJSON` (fresh-check at PR-draft time by
grepping `internal/query/query_test.go` at `62923d8`) stay
VERBATIM — under omit-when-zero-length their pinned JSON strings
match the widened marshaller bit-for-bit (the `args` key is
absent when the pre-existing tests use `NewCallBinding` — the
args slice is nil).

### 5.2 The unit-test run list for Fence 3

Under `go test ./internal/query/... -run '^(TestNewCallArg|TestCallArgMarshalJSON|TestNewCallBindingWithArgs|TestNewCallBinding|TestCallBindingMarshalJSON)$'`,
all five tests must pass at branch tip. The pre-existing two stay
verbatim (verify by `git diff origin/master --`
`internal/query/query_test.go` at PR-draft time shows only the
three new-test additions).

---

## 6. The `oneCall` builder — no listener change

`internal/query/cypher/listener.go` and `internal/query/cypher/build.go`
carry the branch/part builder machinery. The 0ig widening does not
touch either: the args slice is threaded through `collectCall`'s
existing dispatch to `collectYieldItems` / `expandAllResults`, both of
which construct CallBindings directly and append to
`l.curPart.callBindings`. No new listener callback, no new build-time
validation.

**Cycle-2 erratum 2 lesson applied.** The lesson: any helper algorithm
the spec sketches must be implementable from data available in the
frozen model. Verification: the mined `t` from `typeExpressionMining`
IS available at the emission moment (line 55 of call.go). No new
data plumbing is needed. §4.2's widening consumes only what the
current code already computes.

---

## 7. ADR 0008 amendment note — the dated stage note

Verbatim text to add at the top of
`docs/adr/0008-query-model-freeze-resolver-api.md`, following the
two existing amendment notes' format (fvo lines 3-46; hk0 lines
48-82; verified against master `62923d8`):

```markdown
> _Amendment (2026-07-07, gqlc-0ig unfreeze cycle): the
> per-position CALL-arg attribution axis on `CallBinding` — recorded
> as the R7 §7.1.1 CALL-arg-attribution deferral — is **adopted**
> under this ADR's additive-only revision protocol. The R7-shipped
> "no arg-site check" posture
> (`docs/specs/resolver-stage-r7.md §7.1.1`) was an honest
> workaround for the wire's missing per-argument attribution; the
> resolver widening (post-0ig) walks each CallBinding's `Args()`
> per Phase A1 and checks each mined type against
> `procsig.Registry.Lookup(procedure).Params[i].Token` under the
> ADR 0007 Stage-14 note's assignability rule
> (`docs/adr/0007-pre-freeze-scope-full-opencypher-surface.md`
> lines 172-174: NUMBER assignable-from INTEGER-or-FLOAT). The
> `CallBinding` sum gains one additive field `args []CallArg`, one
> new positional constructor `NewCallBindingWithArgs`, one new
> accessor `Args() []CallArg`, and one new sub-type `CallArg` with
> `NewCallArg(t Type)` and `Type() Type`. The `CallBinding.args`
> encoding is **omit-when-zero-length** (`,omitempty`), following
> the post-freeze convention this ADR's hk0 amendment established
> for additive axes. `procsig.TypeToken` stays a signature-only
> vocabulary (ADR 0007 Stage-14 note); the wire records only the
> parser-mined `query.Type`, and the resolver bridges by looking up
> the procedure name against the compile-time `procsig.Registry`.
> The Binding interface stays sealed at one method (`isBinding()`);
> Args attribution is a CallBinding-only field-and-accessor concern.
> See `docs/specs/unfreeze-0ig-call-args.md` for the full contract,
> the 28-golden rebaseline accounting with per-scenario spot
> witnesses, the layering divergence from the bead text's
> parser-emits-sig-token proposal, and the semantic-diff-only fence
> commands._
```

The Known-deferred-additions entry (mirroring the hk0 and fvo
close-out entries' format at lines 246-264; verified against master
`62923d8`):

```markdown
- **`CallBinding.Args` axis** — adopted 2026-07-07 (see the
  amendment note above and `docs/specs/unfreeze-0ig-call-args.md`).
  Populated parser-side by `collectCall`'s existing arg-mining
  loop capturing the mined `query.Type` per argument position;
  consumed by the resolver's Phase A1 arg-site assignability walk
  (`internal/resolver/resolve.go` CallBinding arm — see 0ig
  resolver-widening PR) which looks up
  `procsig.Registry.Lookup(procedure).Params[i].Token` and checks
  under ADR 0007's NUMBER-assignable-from-INTEGER-or-FLOAT rule.
  `procsig.TypeToken` stays signature-only vocabulary; the wire
  records only `query.Type`. The R7 §7.1.1 deferral is closed.
```

---

## 8. Resolver widening — the follow-up PR after the unfreeze merges

The unfreeze PR lands the model + parser change with the resolver
green (0ig's `CallBinding.Args()` is populated but unconsumed at
that point — see §9). The resolver-widening PR fires next, adding
the arg-site assignability walk and one new invalid fixture. This
section pins the semantics; the PR body implements them.

### 8.1 `resolvePart` CallBinding arm — the exact widening

Current (`internal/resolver/resolve.go:262-285`, verified against
master `62923d8`):

```go
case query.CallBinding:
    v := bb.Variable()
    // R7 §4.1: local CallBinding shadows any carried entity state
    // at the same name (parser-unreachable belt-and-braces …).
    delete(nodeTypes, v)
    delete(edgeTypes, v)
    delete(edgeKeys, v)
    delete(edgeCands, v)
    delete(edgeBindings, v)
    // Same-Part duplicate CallBinding variable is grammar-impossible
    // … Defensive tripwire.
    if _, seen := callTypes[v]; seen {
        return nil, branchState{}, nil, fmt.Errorf("%w: variable %q re-CALL-bound in single part", ErrPartBindingTypeConflict, v)
    }
    callTypes[v] = callBindingSlot{
        resultType:  bb.ResultType(),
        nullable:    bb.Nullable(),
        procedure:   bb.Procedure(),
        sourceField: bb.SourceField(),
    }
```

Widened — add the arg-site check as a new step BEFORE the
callTypes assignment (so a failed check surfaces before any state
is committed, mirroring the R7 shadowing rule at :264-268):

```go
case query.CallBinding:
    v := bb.Variable()
    // R7 §4.1 shadowing verbatim.
    delete(nodeTypes, v)
    delete(edgeTypes, v)
    delete(edgeKeys, v)
    delete(edgeCands, v)
    delete(edgeBindings, v)
    if _, seen := callTypes[v]; seen {
        return nil, branchState{}, nil, fmt.Errorf("%w: variable %q re-CALL-bound in single part", ErrPartBindingTypeConflict, v)
    }
    // 0ig widening: arg-site assignability check. Per-position
    // check against sig.Params[i].Token; TypeUnknown always accepts;
    // NUMBER accepts INTEGER-or-FLOAT (ADR 0007 Stage-14 note).
    // Parser has already checked existence and arity; the resolver
    // is unreachable from a nonexistent procedure (sig will be found
    // for every CallBinding).
    if err := checkCallArgs(bb, r); err != nil {
        return nil, branchState{}, nil, err
    }
    callTypes[v] = callBindingSlot{
        resultType:  bb.ResultType(),
        nullable:    bb.Nullable(),
        procedure:   bb.Procedure(),
        sourceField: bb.SourceField(),
    }
```

The new helper `checkCallArgs` lives in `internal/resolver/resolve.go`
(a package-private function, per the R7 helper style):

```go
// checkCallArgs walks a CallBinding's Args() slice, checking each mined
// type against the positionally-corresponding procsig.Signature.Params
// token under ADR 0007's Stage-14 assignability rule (NUMBER accepts
// INTEGER-or-FLOAT; INTEGER / FLOAT / STRING accept their own token;
// TypeUnknown always accepts because the parser could not tell at
// mining time — the Q4 posture from Stage 14 §4.5 preserved at resolve).
// Called from the CallBinding arm of resolvePart (§8.1). Zero args or a
// TypeUnknown arg at every position is a no-op (accept-path).
func checkCallArgs(bb query.CallBinding, r procsig.Registry) error {
    args := bb.Args()
    if len(args) == 0 {
        return nil // zero-arg or pre-0ig-golden CALL — accept.
    }
    sig, ok := r.Lookup(bb.Procedure())
    if !ok {
        // Parser-authoritative unknown-procedure (R7 §4.4) —
        // unreachable belt-and-braces. A missing signature at
        // resolve time indicates parser/resolver registry drift.
        return fmt.Errorf("%w: unknown procedure %q at resolver arg-site (parser-resolver registry drift)", ErrOutOfR0Scope, bb.Procedure())
    }
    // Arity was checked at parse (call.go:71-75). Position i < len(sig.Params)
    // is guaranteed for every explicit-invocation CallBinding. Defensive
    // guard against decoder / implicit-invocation edge cases.
    if len(args) != len(sig.Params) {
        return fmt.Errorf("%w: procedure %q arity mismatch at resolver arg-site (%d args, %d params) — parser-resolver drift", ErrOutOfR0Scope, bb.Procedure(), len(args), len(sig.Params))
    }
    for i, a := range args {
        if !argAssignable(a.Type(), sig.Params[i].Token) {
            return fmt.Errorf("%w: procedure %q argument %d has type %s, signature declares %s", ErrOutOfR0Scope, bb.Procedure(), i, a.Type().String(), sig.Params[i].Token.String())
        }
    }
    return nil
}

// argAssignable is the ADR 0007 Stage-14 assignability rule. TypeUnknown
// always accepts (the parser could not tell; the resolver could sometimes
// tell more, but the honest bucket-3 posture from Stage 14 §4.5 is
// preserved: the wire records the mined type verbatim, and the check
// widens to accept everything the mining could not disambiguate). NUMBER
// param accepts TypeInt or TypeFloat.
func argAssignable(argType query.Type, paramToken procsig.TypeToken) bool {
    if _, isUnknown := argType.(query.TypeUnknown); isUnknown {
        return true
    }
    switch paramToken {
    case procsig.TokenInteger:
        _, ok := argType.(query.TypeInt)
        return ok
    case procsig.TokenFloat:
        _, ok := argType.(query.TypeFloat)
        return ok
    case procsig.TokenString:
        _, ok := argType.(query.TypeString)
        return ok
    case procsig.TokenNumber:
        _, isInt := argType.(query.TypeInt)
        _, isFloat := argType.(query.TypeFloat)
        return isInt || isFloat
    default:
        // Unknown token — defensive; TypeToken sum is closed at 4
        // members (procsig package doc, verified against master).
        return false
    }
}
```

**Slot placement.** The helper lives in `resolve.go` alongside the
existing `callBindingSlot` machinery (`:95-103`). No new file.

### 8.2 The 5-case assignability table (worked)

Per §2.4:

| Sig `Params[i].Token` | Mined `args[i].Type()` | Assignable? | Fixture (§8.4) |
|---|---|---|---|
| INTEGER | TypeInt | yes | (existing R7 corpus) |
| INTEGER | TypeFloat | **no** | new invalid |
| FLOAT | TypeFloat | yes | (existing R7 corpus) |
| FLOAT | TypeInt | yes | (Call3 [5]/[6] — INTEGER accepts as FLOAT per Neo4j Cypher; ADR 0007 line 173 records this as part of the NUMBER assignability semantics) |
| STRING | TypeString | yes | (existing R7 corpus) |
| STRING | TypeInt | **no** | new invalid (§8.4) |
| NUMBER | TypeInt | yes | (Call3 [1]/[2]) |
| NUMBER | TypeFloat | yes | (Call3 [3]/[4]) |
| NUMBER | TypeString | **no** | (out of NUMBER scope — reject) |
| any | TypeUnknown | yes | (`$param` / `n.name` / `null`) |

**Wait — FLOAT accepts INTEGER? Re-check.** ADR 0007 line 173:
_"NUMBER stays a signature-only marker (assignable-from INTEGER-or-
FLOAT at the argument site)"_. This is the NUMBER-accepts-INT-OR-FLOAT
direction. The FLOAT-accepts-INTEGER direction is a Cypher runtime
semantic that goes beyond ADR 0007's explicit text; Call3 [5] (`FLOAT
accepts INTEGER value`) is a scenario in TCK. **Escalation: does 0ig
adopt the strict FLOAT-accepts-only-FLOAT rule (safer, but rejects
Call3 [5]'s accept-path), or the loose FLOAT-accepts-INTEGER-or-FLOAT
rule (matches TCK but is not in ADR 0007)?** §8.2.1 records the
resolution.

#### 8.2.1 Resolution — adopt the loose rule, cite Call3 [5] as ground truth

Call3 [5] `Standalone call to procedure with argument of type FLOAT
accepts value of type INTEGER` is a POSITIVE TCK scenario at branch
base (`test/data/query/cypher/tck/features/clauses/call/Call3.feature`,
verified against master). The TCK expected behaviour is
"the procedure accepts an INTEGER at a FLOAT position". The resolver
must not reject this shape — over-rejection would break golden
determinism against the corpus.

`argAssignable`'s FLOAT arm therefore widens:

```go
case procsig.TokenFloat:
    _, isFloat := argType.(query.TypeFloat)
    _, isInt := argType.(query.TypeInt) // 0ig §8.2.1: TCK Call3 [5] admits INTEGER-at-FLOAT.
    return isFloat || isInt
```

**INT accepts FLOAT?** There is NO TCK scenario asserting this
(Call3 does not have a symmetric case). The strict INT-accepts-only-
INT rule is safer; adopt it.

**STRING accepts anything else?** No TCK scenario admits string-vs-
non-string; the strict rule stands (STRING → TypeString only).

This resolution is a design escalation Linus must sign off on;
§10 §8.2.1 lists the two questions team-lead + linus resolve at
resolver-widening-PR review time.

### 8.3 Sentinel discipline — no new sentinel, message-set widening

R7 §5.1 committed to zero new sentinels; the 0ig resolver widening
holds that line. The arg-site mismatch fires the existing
`ErrOutOfR0Scope` sentinel with a widened message set:

- `"unknown procedure %q at resolver arg-site (parser-resolver registry drift)"` — parser-resolver drift; unreachable belt-and-braces.
- `"procedure %q arity mismatch at resolver arg-site (%d args, %d params) — parser-resolver drift"` — parser-resolver drift; unreachable belt-and-braces.
- `"procedure %q argument %d has type %s, signature declares %s"` — the semantic fail-site the check exists to detect.

**Why not a new `ErrCallArgType`?** R7 §5.1's precedent
(`docs/specs/resolver-stage-r7.md`): _"R7 introduces no new
sentinel. The R6 close-out committed to one per stage as a hard
maximum; zero is a floor R7 hits naturally"_. The 0ig widening is a
YIELD-adjacent CALL widening; adding a new sentinel duplicates
`ErrOutOfR0Scope`'s "resolver rejects a construct the type-interface
does not fully model" role. The wrapped message disambiguates for
the diagnostic reader (procedure name + argument index + the
mismatched types).

**Consistency with R5/R6/R7.** R5 introduced no new arg-site
sentinel (parameter conflicts fire `ErrParameterTypeConflict`).
R6 introduced `ErrInvalidEffectTarget` — for a semantically distinct
Effect-target arm. R7 introduced zero. 0ig introduces zero, matching
R7 and the "arg-site is a YIELD-neighbour widening" framing.

### 8.4 New invalid fixture — `call_arg_wrong_type_at_arg_site.cypher`

A new fixture in `test/data/resolver/invalid/` (path verified at PR-
draft time; the current invalid fixture directory is enumerated at
`test/data/resolver/invalid/*.cypher` — fresh-count at PR-draft
against `62923d8`). The fixture pins a CALL with a STRING literal at
a NUMBER param position, which the argAssignable rule rejects:

```cypher
CALL test.my.proc('not a number') YIELD out
RETURN out
```

Registry (fresh-authored in the fixture header, per the R7 fixture
convention — R7 §6.3 established the "signature slice inline in the
resolver_test.go" pattern, verified against master):

```
test.my.proc(in :: NUMBER?) :: (out :: STRING?)
```

**Expected `.error.json`**:

```json
{
  "sentinel": "ErrOutOfR0Scope",
  "message": "query construct not supported at resolver stage R0: procedure \"test.my.proc\" argument 0 has type string, signature declares NUMBER"
}
```

**Branch-base behaviour**: at R7 (pre-0ig-resolver), this fixture is
SILENTLY ADMITTED — `checkCallArgs` does not exist; the CallBinding
arm at :262-285 assigns callTypes and moves on. The parser accepts
the query (arg-type check is bucket-3 skiplisted); the resolver
does not check either. **This is the sole wire-observable
resolver behaviour change**: at post-0ig-resolver, the query
rejects with the message above.

**Fixture shape mirrors R7 §6.5 pattern.** R7's invalid-fixture map
uses `error.json` alongside `.cypher` (verified against
`internal/resolver/resolver_test.go` at PR-draft time; the R7
convention is stable per the R7 close-out).

#### 8.4.1 Discriminating twin — a valid fixture that MUST NOT reject

To exercise the "widened but not overreaching" side, one existing
R7 CALL fixture is validated against the widening:
`call_yield_two_columns.cypher` (§4.4.1 spot witness from
R7 §6.4). The fixture's CALL is `CALL test.my.proc('Stefan', 1)
YIELD city, country_code RETURN city, country_code` against
signature `test.my.proc(name :: STRING?, id :: INTEGER?) :: (city
:: STRING?, country_code :: INTEGER?)`. Args mine to `(TypeString,
TypeInt)`; sig positions accept STRING → TypeString ✓ and INTEGER
→ TypeInt ✓. Widening admits; the golden stays byte-identical.

**No new valid fixture is needed** — the R7 corpus's existing 5+
CALL fixtures exercise every assignability accept-path (INTEGER-at-
INTEGER, STRING-at-STRING, INT-at-NUMBER, FLOAT-at-NUMBER, INT-at-
FLOAT via Call3 [5]/[6] equivalents). §8.5's byte-identity fence
verifies every pre-existing golden stays.

### 8.5 Byte-identity fence over the R5/R6/R7 resolver goldens

The existing resolver goldens under `test/data/resolver/valid/` and
their paired `.golden.json` outputs must stay byte-identical after
the widening PR. The fence:

```
# From worktree root, at resolver-widening PR branch tip.
just test  # runs the whole test suite; the resolver golden
           # pair harness fails on any diff.
git diff --stat origin/master -- test/data/resolver/valid/
# MUST print: no changes.
```

**Rationale.** The widening's semantic surface is one new invalid
fixture. Every existing R5/R6/R7 valid fixture exercises CALL-free
or CALL-with-registry-conforming shapes; the argAssignable check
either does not run (no CALL) or admits (registry-conforming).

**If any R5/R6/R7 valid golden flips**, the widening is
over-rejecting on an accept-path — the fence surface for the
reviewer is the diff itself.

### 8.6 R7 §7.1.1 successor prose — verbatim

`docs/specs/resolver-stage-r7.md §7.1.1`, verbatim close-out
addition (appended below the existing R7.1.1 body; the existing
body stays for historical grounding, and the successor prose lands
under a new "**Closure (0ig cycle)**" subhead):

```markdown
#### 7.1.1 Closure — 0ig cycle closes the arg-site gap

**Status update, 2026-07-07 (post-`gqlc-0ig` close-out).** The
CALL-arg attribution deferral R7 §7.1.1 recorded is closed by the
0ig unfreeze cycle: an additive `Args []CallArg` axis on
`query.CallBinding`, populated parser-side by `collectCall`'s
existing arg-mining loop (the mined type at `call.go:55` is now
captured into the axis instead of discarded), consumed by the
resolver's Phase A1 arg-site assignability walk against
`procsig.Registry.Lookup(procedure).Params[i].Token`. NUMBER
assignable-from INTEGER-or-FLOAT (ADR 0007 Stage-14 note lines
172-174) is honoured; TypeUnknown accepts silently at every
position (Q4 posture preserved). The R7-shipped
"no arg-site check" workaround retires; the R7 §7.1.1 open axis is
closed. See `docs/specs/unfreeze-0ig-call-args.md` for the full
contract.

**What did NOT need to change.** R7 §7.1.1 proposed TWO minimal
model changes: `ExprInCallArg` on ExprPosition, and a `Calls` axis
on Part. Neither was adopted. The 0ig cycle instead added ONE axis
on CallBinding — Args slice — arguing that per-CallBinding
attribution matches Phase A1's callTypes-keyed-by-variable shape
byte-for-byte, and the alternative axes (Use position discriminator
grow, Part-level Calls slice) each carried a distinct cost (Use
axis cross-product growth; Part-level positional-index linkage).
See §3.4 of the 0ig spec for the layering rationale.

The parameter Uses under CALL argument expressions STAY as
`ExprUse{miningType, ExprInProjection}` per `call.go:64` — the 0ig
cycle did not change the ExprPosition sum. Their witness path in
`witnessAcrossScopes` is unchanged.
```

### 8.7 Follow-up beads flagged for close-out

At resolver-widening-PR merge, close:

- **R7 §7.1.1 owner-decision bead** — the closure prose at §8.6
  is the close-out record.

Do NOT close: `gqlc-4w5`, `gqlc-qcc` (cycle 2 residuals),
`gqlc-hk0` (already closed), `gqlc-fvo` (already closed).

---

## 9. Non-goals

**In-scope for this cycle**:

| Item | Landing PR |
|---|---|
| `CallBinding.Args` model addition | Unfreeze PR |
| `CallArg` sub-type | Unfreeze PR |
| Parser `collectCall` widening | Unfreeze PR |
| ADR 0008 amendment note | Unfreeze PR |
| 28-golden parser rebaseline | Unfreeze PR |
| Parser-test pin rebaseline for ≥ 1-arg pins | Unfreeze PR |
| Resolver arg-site assignability walk | Resolver-widening PR |
| `call_arg_wrong_type_at_arg_site.cypher` fixture | Resolver-widening PR |
| R7 §7.1.1 closure prose | Resolver-widening PR |

**Explicitly out-of-scope** (each a follow-up bead or a residual):

| Item | Status |
|---|---|
| `gqlc-hk0` (ContainsAggregate on ExprProjection) | CLOSED (cycle 1) |
| `gqlc-fvo` (Use → Part attribution) | CLOSED (cycle 2) |
| `gqlc-ay9` (R4 Class A: shortest-path aggregation) | Still open |
| `gqlc-5xg` (R4 Class B: nullability under UNION) | Still open |
| `gqlc-4w5` (fvo residual: semantic-scope axis, intra-clause WITH-WHERE-EXISTS asymmetry) | Still open |
| `gqlc-qcc` (fvo residual: `Use.Branch` axis for UNION shape resolution) | Still open |
| `ExprInCallArg` position on `ExprPosition` sum | Rejected in favour of `CallBinding.Args` (§3.4) |
| `Calls` axis on `query.Part` | Rejected in favour of `CallBinding.Args` (§3.4) |
| Emitting `procsig.TypeToken` on the wire | Rejected in favour of resolver-side sig lookup (§3.2) |
| Codegen consumption of Args | Human-gated; a follow-up ADR |
| Runtime type-widening for `TypeUnknown` args | Bucket-3 posture preserved (§4.2.1) |

---

## 10. Ground-truth cross-check

Every factual claim in this spec is verifiable against source; the
citations below name the file:line the claim rests on. All line
numbers verified against master `62923d8`.

- **`query.CallBinding` struct + `NewCallBinding` constructor** —
  `internal/query/query.go:843-877`. Field-by-field pins:
  - `variable` at `:844` — always non-empty
  - `procedure` at `:845` — fully-qualified, always non-empty
  - `sourceField` at `:846`
  - `resultType` at `:847`
  - `nullable` at `:848`
- **`query.CallBinding` accessors** — `:882-908`.
- **`query.CallBinding.MarshalJSON`** — `:916-932`. Every field
  always-emitted at branch base.
- **`collectCall`** — `internal/query/cypher/call.go:29-97`.
  Arg-mining loop at `:54-66`; mined `t` discarded at `:64` (used
  only for the ExprUse construction, not stored per-argument).
- **`typeExpressionMining`** — `internal/query/cypher/typing.go:41-52`.
- **`l.registry` procsig-registry access at parse** —
  `internal/query/cypher/listener.go:86-90` and `:177-182`.
- **`enterStandaloneCall` / `enterInQueryCall`** —
  `internal/query/cypher/call.go:277-309`. Grammar quirks (implicit
  invocation, YIELD *, EXISTS suppression) commented at `:273-277`
  and enforced by listener wrappers (EXISTS gate at
  `internal/query/cypher/listener.go`, `subqueryDepth > 0` early-
  return preserved).
- **`resolve` kernel signature** —
  `internal/resolver/resolve.go:21`. Registry threaded through
  as `r procsig.Registry`.
- **`resolvePart` CallBinding arm** —
  `internal/resolver/resolve.go:262-285`.
- **`callBindingSlot`** — `internal/resolver/resolve.go:98-103`.
- **`ErrOutOfR0Scope`** — `internal/resolver/errors.go:24-31`.
  Sentinel to widen at §8.3.
- **R7 §7.1.1 (CALL-arg attribution deferral)** —
  `docs/specs/resolver-stage-r7.md:2237-2326`. The closure-target
  prose.
- **R7 §4.4 (parser-authoritative unknown-procedure)** —
  `docs/specs/resolver-stage-r7.md:1207-1267`. The precedent for
  the layering split.
- **R7 §5.1 (sentinel additions — none)** —
  `docs/specs/resolver-stage-r7.md:1576-1591`. The precedent for
  §8.3.
- **R7 §4.8 (Parameter Uses under CALL argument expressions)** —
  `docs/specs/resolver-stage-r7.md:1520-1567`. The prose that
  0ig does NOT close (the Use position discriminator stays
  ExprInProjection; §3.4 pins the reason).
- **ADR 0008** — `docs/adr/0008-query-model-freeze-resolver-api.md`.
  Additive-only revision protocol at §Post-freeze revision protocol
  (`:224-264`). The two existing amendment notes at `:3-46` (fvo)
  and `:48-82` (hk0) — the template for §7.
- **ADR 0007 Stage-14 note** —
  `docs/adr/0007-pre-freeze-scope-full-opencypher-surface.md:166-181`.
  NUMBER assignable-from INTEGER-or-FLOAT at lines 172-174.
- **Stage 14 spec §4.5 bucket-3 skiplist** —
  `docs/specs/cypher-query-parser-stage-14.md:971-990`. The Q4
  ruling preserved at the parser side; the resolver widens per
  §8.
- **Grammar**: `oC_ReadingClause ::= oC_Match | oC_Unwind |
  oC_InQueryCall` — `internal/grammar/cypher/Cypher.g4:74-78`.
  Multiple in-query CALLs in one Part are grammar-legal (§3.4
  argument against a Part-level Calls axis).
- **`procsig` package** — `internal/procsig/procsig.go` package
  doc. `procsig.TypeToken` sum closed at four members
  (INTEGER, FLOAT, STRING, NUMBER); NUMBER intentionally decoupled
  from `query.Type` per ADR 0007 lines 172-174 — the layering
  argument at §3.2 rests on this.
- **Fresh corpus counts at branch base `master @ 62923d8`
  (all re-verified this cycle)**:
  - 3199 total `*.golden.json` files
  - 32 CallBinding-carrying goldens
  - 28 with-≥1-arg (flip set)
  - 4 zero-arg-only (byte-identical set) — §4.1.3.1 pins each
  - Per-prefix: Call1 2 (both no-arg), Call2 3 (2 flip, 1 no-arg
    implicit), Call3 6 (all 6 flip), Call4 2 (all 2 flip), Call5
    16 (all 16 flip), Call6 3 (2 flip, 1 no-arg)
- **Parser-test pin count** — 15 CallBinding pins at branch base
  (fresh grep `grep -c NewCallBinding internal/query/cypher/parser_test.go`
  returns 15; verified this cycle).

**Design-escalation checklist** (team-lead / linus resolve before
resolver-widening PR):

1. §3.2: layering — parser-emits-sig-token vs mined-type-only? Spec
   commits to mined-type-only; escalation flagged prominently.
2. §8.2.1: FLOAT param accepts INTEGER argument (per TCK Call3 [5])
   — adopted; escalation for confirmation.
3. §3.4: `CallBinding.Args` vs R7 §7.1.1's `Calls` on Part vs
   `ExprInCallArg` on Use — spec commits to `CallBinding.Args`;
   escalation for confirmation of the divergence from R7's
   deferred-list.
4. §4.2: slice-sharing across CallBindings — spec commits to
   direct sharing (all CallBindings from one CALL invocation carry
   the SAME slice); escalation for confirmation of the "callers
   MUST NOT mutate" invariant.

---

## 11. Definition of done for the spec cycle

The spec PR is done when:

- This file lands on master under `docs/specs/unfreeze-0ig-call-args.md`.
- No behavioural code changes yet (spec-only cycle).
- Linus review PASSES; team-lead PASSES; both explicitly ACK the
  four escalation items in §10.

The unfreeze PR (cycle 2) then implements §4 verbatim. The
resolver-widening PR (cycle 3) then implements §8 verbatim.

---

## 12. Errata

_(To be populated at cycle close-out per the fvo §12 / hk0 §12
convention. Cycle-2 lesson: any spec-text divergence from the
landed listener/resolver reality caught during the code cycles
is recorded here after both code PRs merge, with the fix applied
in-place above.)_
