# A declared bit width the model cannot express is accepted and lost, not rejected

`INT(7)` is valid ISO GQL. gqlc accepts it, resolves it to `graph.TypeInt`, and
records that the declared width was discarded. It does not reject it, and it
does not round it up to `TypeInt8`.

The same holds for `UINT(p)` and `INTEGER(p)` at any `p` the model has no
constant for, and — for a different reason, below — for `FLOAT(p)` at every `p`.

## Context

### The residue

ADR 0002 folds a parenthesised binary width onto a width constant "where the
grammar makes the parenthesised form a sibling of an explicit width token *and*
the parenthetical admits no scale". `gqlc-h9n.16` implemented that: `INT(8)`
resolves to `TypeInt8`, exactly as `INT8` does.

The criterion is stated in terms of what the model can already express, so what
it cannot express falls out of the bottom of it. `PropertyType` enumerates eight
integer widths; ISO's `precision` is an arbitrary unsigned integer. `INT(7)`,
`INT(10)`, `INT(33)` all miss the lattice, canonicalise to `INT` through
`normaliseType`'s truncating fallback, and land on a 64-bit machine int. The
author said seven bits; the model says sixty-four.

That is jagged, and the jaggedness is not a principle — it is the enum's lattice
showing through. Saying so is most of the point of this ADR: a reader who sees
`INT(8)` preserved will otherwise infer a rule ("gqlc honours declared widths")
that is false one width later.

### Why it needed deciding

Two things already written down predict opposite answers.

ADR 0002's rationale, verbatim: *"Losing signedness and width would force codegen
to emit `int`/`uint` everywhere and discard information the schema author
explicitly stated."* Read straight, that condemns the status quo.

The project directive behind this epic, verbatim: *"we should be able to accept
any valid ISO GQL. we should not have our own GQLC flavour of ISO GQL."* Read
straight, that forbids rejecting `INT(7)`.

Nothing in the tree adjudicated. `gqlc-h9n.16` was byte-identical on this
behaviour before and after, so it inherited the question rather than answering
it.

### The discriminator, and how it differs from ADR 0016

ADR 0016 declined undirected edges, and the reason that carried it was
observability: an undirected edge stored with a canonical direction answers
`IS DIRECTED`, `IS SOURCE OF` and `IS DESTINATION OF` **incorrectly**, not
imprecisely. Rejection there removes a wrong answer.

That test does not transfer, and the reason is about what the alternative costs
rather than about what is lost.

For an undirected edge, the alternative to rejecting is accepting and answering
a standard predicate wrongly. For `INT(7)`, the alternative to accepting is not
compiling the schema at all. A property declaration is one line inside a graph
type; rejecting it takes down every node type, every edge type and every other
property in the file. The blast radius of the rejection is the whole schema, and
what it buys is the suppression of one discarded qualifier on one property.
Rejecting does not make gqlc *right* about the width — it makes gqlc unable to
say anything about the schema.

GQL does have a value-type predicate — `valueTypePredicate` at `GQL.g4:2052`,
`<value expression> IS [NOT] TYPED <value type>` — which is the nearest thing to
an `IS DIRECTED` analogue. Two reasons it does not change the answer, one solid
and one hedged:

- **Solid:** it takes a `valueExpressionPrimary`, so it is a predicate over a
  *value*. What gqlc discards is a *declaration*. gqlc also has no GQL query
  surface at all (ADR 0007 scopes the query parser to openCypher), so nothing in
  gqlc evaluates it today.
- **Hedged:** whether ISO's General Rules for `IS TYPED` can reach a declared
  type is in the paywalled text, and `gqlc-lir` decided against buying it. So
  this is not a proof that the predicate is unreachable, and it is not offered
  as one.

The decision does not rest on the hedge. Even if `IS TYPED INT(7)` were
answerable from declarations, rejection would not answer it correctly — it would
refuse the schema that contains the question.

### Why not reject: the conservatism argument, answered

ADR 0006 established that relaxing a rejection later is non-breaking while
tightening an acceptance is not, so the conservative starting position is to
reject. A reviewer will reach for that here, and it does not apply.

ADR 0006's asymmetry is about gqlc's own inferences — how hard to push static
nullability flow typing — where being wrong means gqlc drew a conclusion nobody
asked for. Narrowing the *accepted language* is a different act. A rejection of
valid ISO GQL is not a conservative default that can be relaxed later; for as
long as it stands it is a gqlc dialect, which is the single thing this epic
exists to remove. Conservatism about our own conclusions does not license
conservatism about the standard's surface.

### Why not round up to the next supported width

`INT(7)` → `TypeInt8`, `INT(10)` → `TypeInt16`. Two objections, either fatal.

**It is unsourced.** Rounding up reads `precision` as a *minimum* capacity ("at
least p bits") rather than an exact one. Nobody here has sourced that from ISO
39075, and `gqlc-h9n.28` sets the standing rule for precisely this situation:
a web-search paraphrase, a vendor conformance table, or an analogy to SQL is not
a citation. `gqlc-cfj` tracks the habit this rule exists to break.

**It buys no correctness even if granted.** A signed 7-bit integer holds −64…63;
`int8` holds −128…127. Rounding up is still permissive-imprecise, exactly like
`TypeInt` — it is the same kind of wrong in a smaller quantity. It would trade an
unsourced claim about the standard for a narrower Go emission and no new
guarantee, and it would make the model's answer depend on an arithmetic rule
that appears nowhere in the grammar. Inventing a rule is how a dialect starts.

### Why not grow the model an exact width now

This is the fix, and it is deferred rather than refused. It is filed as
`gqlc-h9n.31`, a third sibling to `gqlc-5md` (magnitude qualifiers) and
`gqlc-8pe` (duration qualifier), and it carries the same acceptance criterion
both of those carry: **name the consumer first**. Today nothing would read the
field — `codegen.goType` dispatches on `PropertyType` alone, and Go has no
carrier for a 7-bit integer to emit into. It also carries the same measured
regression risk: a width inside the compared value makes `INT(7)` and `INT(10)`
stop unifying at `unionProperty` (`internal/resolver/resolve.go:744`) and `unify`
(`:844`), silently, in exchange for a field nothing reads.

`gqlc-h9n.31` is a sibling rather than part of `gqlc-5md` because what is missing
is not the same kind of thing. `gqlc-5md` and `gqlc-8pe` are both "the model has
no field for this information". Here the model *has* a width axis and quantises
it, so the fix is to replace an enumerated axis with an integer — a different
edit, a different consumer, and a different unification question, since integer
widths are ordered and nested in a way that lengths and disjoint duration fields
are not.

### FLOAT(p): same disposition, different reason

`FLOAT(p)` is not folded at all, so its loss is total rather than jagged, and
ADR 0002 already records the behaviour (`FLOAT(16)` → `Float`). It is decided
here only because `gqlc-h9n.25` put it in scope and because nothing had said
that shipping it was a choice.

The reason differs: for `INT(7)` we know what the author said and cannot store
it; for `FLOAT(10)` we do not know what the author said. `GQL.g4:1849` gives
`FLOAT` the scale-bearing parenthetical shape of `DECIMAL(p, s)`, not the
bit-width shape of `INT(p)`, and whether `p` counts mantissa bits or decimal
digits is answerable only from the paywalled General Rules. `gqlc-h9n.28` is
blocked on that permanently and instructs that a shipped behaviour be recorded
"as a gqlc decision under gqlc's own name — never as *ISO says*". This is that
record. `gqlc-h9n.28` stays open; answering it could still move `FLOAT(p)`'s
ownership from `gqlc-h9n.31` to `gqlc-5md`.

## Considered options

**Reject `INT(7)` with `ErrUnsupportedType`.** Rejected: it fails a whole schema
to suppress one qualifier, and a rejection of valid ISO GQL is a dialect, not a
conservative default.

**Round up to the next supported width.** Rejected: unsourced as a reading of
`precision`, and permissive-imprecise anyway, so it pays a citation debt for a
smaller quantity of the same loss.

**Warn rather than reject.** Rejected: `Parse` returns a model and an error, with
no third channel, and adding one to carry an advisory about a qualifier that no
consumer reads is more surface than the problem. When a consumer exists,
`gqlc-h9n.31` is where it lands.

**Grow `PropertyType` an exact width now.** Not rejected on merit — deferred as
`gqlc-h9n.31` on the consumer bar `gqlc-5md` and `gqlc-8pe` already set, and on
the measured `unionProperty` regression.

**Leave it undecided and let the fallback speak.** Rejected: that is the status
quo ante, and it is how a reader of ADR 0002 and a reader of the project
directive end up predicting different behaviours from the same tree.

## Consequences

- `normaliseType`'s doc comment states the decision instead of naming an open
  bead. The fallback stays lossy, on purpose, with this ADR as the reason.
- Three corpus entries become resolving-but-wrong entries under `gqlc-h9n.31`,
  with matching `semanticCase` rows and asserted collisions:
  `scalar_signed_integer.gql` (`INT(10)`, `INTEGER(10)`),
  `scalar_unsigned_integer.gql` (`UINT(10)`), `scalar_float.gql` (`FLOAT(10)`,
  `FLOAT(10, 2)`). `wantSemanticCases` moves from 10 to 13. Until now the loss
  was stated in a doc comment and checked by nothing.
- `gqlc-0ri`, the deviation register, gains **no row**. Its table is one row per
  rejection sentinel, and this decision adds no rejection — accepting everything
  valid is the outcome that register exists to drive toward. The fidelity limit
  is recorded here instead, which is the distinction the register cannot make.
- `gqlc-h9n.28` is unaffected and stays open. So does the possibility that
  `FLOAT(p)`'s eventual owner is `gqlc-5md` rather than `gqlc-h9n.31`.
- The day `gqlc-h9n.31` lands, `TestSemanticCaseCollisions` goes red on all three
  files and the rows are deleted with `wantSemanticCases`. That is the intended
  end state, not a regression.
