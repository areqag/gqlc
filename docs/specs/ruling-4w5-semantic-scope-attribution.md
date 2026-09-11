# Ruling — the WITH-trailing WHERE is mined in the wrong Part; no semantic-scope axis

The design answer for **gqlc-4w5** ("Semantic-scope attribution: WITH…WHERE
aliased-shadow residual (post-fvo)"), and the implementation brief for its
execution bead.

The bead asks whether to add a SEMANTIC-scope attribution axis — "which scope
evaluates the predicate" — beside the lexical Part axis gqlc-fvo added, and
names two divergences: FINDING-9 (the aliased-projection shadow,
`docs/specs/model-change-fvo-use-part.md` §7.6.1) and an intra-clause asymmetry
between a direct `$param` and an EXISTS-body `$param` in one syntactic WHERE.

Every file:line below was read at this branch's base, master `687896a9`. Every
verdict in §1 and §3 was produced by running the shipped parser and resolver
over an out-of-tree copy of that commit, not read off the source.

**The short answer.** The axis is **declined**. For the one construct the bead
is about, the semantic scope *is* a Part — the Part the WITH opens — and the
lexical axis can already name it. What is wrong is not which axes exist but
**when the existing one is stamped**: `EnterOC_With` mines its trailing WHERE
before the Part swap, so every Use in that WHERE is attributed to the Part the
WITH closed rather than the Part it opened. A second axis would record the same
integer the first one would have recorded had it been read one statement later.

The reproduction found more than the bead did, and in the opposite direction
from the bead's concern: the same pre-swap mining makes the parser **refuse five
legal Cypher queries**, including the ordinary post-aggregation filter
`WITH count(a) AS c WHERE c = $p`. §1.2. That half is not in the bead, not in
the fvo spec, and not in ADR 0008.

---

## 1. What reproduced

### 1.1 FINDING-9 reproduces, end to end

Against a schema declaring `(:Post { title :: STRING NOT NULL, x :: INT NOT
NULL })`, the bead's verbatim shape

```
MATCH (a:Post) WITH a.title AS a WHERE a.x = $p RETURN a
```

parses with `$p`'s `PropertyUse{Ref{a,x}}` at **Part 0**, and the resolver
**admits**, committing `$p :: property:INT` — `Post.x`'s type, read off the
pre-projection binding. Cypher evaluates that WHERE against the post-projection
scope where `a` is a STRING, so `a.x` is a property access on a string. The
bead's account of this shape is accurate in every particular, including the
mechanism: `mineWhere` (`internal/query/cypher/expr.go:512`) is called from
`EnterOC_With` (`listener.go:469-471`) before `closePartOpenNext`
(`listener.go:475`).

Only the line numbers moved. The bead cites `listener.go:283-284` and `:292-293`
from 2026-07-07; the same three statements are at `:469-471` and `:475` today.

### 1.2 The same mining order refuses legal Cypher — and this is not in the bead

`pairAddSub` (`expr.go:597`) does two things when it recognises a
`var[.prop]` vs `$param` pair: it records the `PropertyUse`, and it calls
`l.appendRef(varRef{name: ref.Variable})` (`expr.go:604`, `:611`) — putting the
variable into the **current** Part's referential-integrity set. Pre-swap, that
is the Part the WITH closed. A name the WITH *introduces* is not bound there, so
`build`'s integrity sweep raises `ErrUnboundVariable` on a query openCypher
accepts.

`refFromNonArithmetic` (`internal/query/cypher/shape.go:29`, case 0 at `:41-42`)
returns `ok` for a **bare** variable with no property lookup, so the leak covers
`c = $p` as well as `b.title = $p`.

Measured verdicts, parser only:

| query | master | Cypher |
|---|---|---|
| `MATCH (a:Post) WITH a AS b WHERE b.title = $p RETURN b` | `unbound variable: b` | legal |
| `MATCH (a:Post) WITH count(a) AS c WHERE c = $p RETURN c` | `unbound variable: c` | legal |
| `MATCH (a:Post) WITH a, count(a) AS c WHERE c = $p RETURN a` | `unbound variable: c` | legal |
| `MATCH (a:Post) WITH a.title AS t WHERE t = $p RETURN t` | `unbound variable: t` | legal |
| `MATCH (a:Post) UNWIND [1,2] AS u WITH u AS v WHERE v = $p RETURN v` | `unbound variable: v` | legal |

Each of the five parses **if the `$param` is replaced by a literal** —
`WITH count(a) AS c WHERE c > 1` is fine. The refusal is triggered by the
parameter, which is why it reads as a parameter bug and is in fact a scoping
bug.

That literal form is exactly what **gqlc-cmp** pinned (PR #147, 2026-07-10:
"Layer-2 mustParse pin: WITH-aggregate-WHERE 'foaf' scope-snapshot from
With7[2]"), against the refs snapshot/restore now at `expr.go:518-520`. The pin
holds the arm it names. It cannot see this one, because `mineComparisons`
(`expr.go:516`) runs **before** `savedRefs` is taken, so its `appendRef` is
captured into the snapshot and survives the restore. The guard and the defect
are one line apart and the guard's fixture is the sibling that does not trip it.

### 1.3 And it admits references Cypher puts out of scope

The mirror image, same cause:

| query | master | Cypher |
|---|---|---|
| `MATCH (a:Post) WITH a.title AS t WHERE a.x = $p RETURN t` | admits, `$p :: INT` | `a` not defined |
| `MATCH (a:Post) WITH count(a) AS c WHERE a.x = $p RETURN c` | admits, `$p :: INT` | `a` not defined |
| `MATCH (a:Post) WITH a.title AS t WHERE a.title = 'x' RETURN t` | admits | `a` not defined |

The third row has no parameter, and it is the one the remedy in §4 does **not**
fix: `mineWhere` deliberately discards the rich typer's refs (`expr.go:518-520`,
and the reason is in its doc comment), so a WITH-trailing WHERE's variable
references are not scope-checked at all. `pairAddSub`'s leak is the *only* scope
check that clause gets, it fires only in the presence of a parameter, and it
fires against the wrong Part. Those three facts are one defect.

### 1.4 The corpus could not have caught any of it

- **TCK**: 34 `WITH … / WHERE …` occurrences across the feature files; **1**
  carries a `$param` (`WithWhere2.feature` scenario [2]).
- **Resolver corpus**: 5 fixtures have a WITH-trailing WHERE. Four of the five
  files that pair `WITH` with a parameterised `WHERE` put the WHERE on a
  *later MATCH*, not trailing the WITH —
  `parameter_across_with_alias_shadow.cypher`,
  `parameter_across_with_alias_shadow_reversed.cypher`,
  `parameter_across_with_multi_part.cypher`,
  `parameter_union_later_part.cypher`. The one genuine WITH-trailing WHERE,
  `valid/with_where_predicate.cypher`, filters on the literal `18`.

So **no resolver fixture exercises a WITH-trailing WHERE carrying a parameter**.
Every row in §1.2, §1.3 and §3 is outside the corpus, which is why all of it is
green today.

## 2. Divergence 2 reproduces, and is unobservable

The intra-clause asymmetry is real at the parser. In one syntactic WHERE:

```
MATCH (n:Post) WITH n WHERE $x AND exists { (n)-->(m) WHERE m.x = $y } RETURN n
```

`$x` lands **Part 0** (mined by `mineWhere` pre-swap) and `$y` lands **Part 1**
(`EnterOC_ExistentialSubquery`, `listener.go:811`, is a walker callback and
fires after `EnterOC_With` has returned). The bead is right, and the Part-1 half
is pinned at `parser_test.go:1639-1660`.

**It changes no answer.** Both are `ExprUse`, and the resolver does not read
`Part` on an `ExprUse`. `selectPartScope` (`resolve.go:670-679`) type-asserts
`query.PropertyUse` and returns a **zero** `partScope` for everything else;
`WitnessUse`'s `ExprUse` arm (`scope.go:1492-1499`) answers from
`uu.EnclosingType()` alone and never touches the receiver's tables. Resolved
end to end, `$x` and `$y` both commit `scalar(bool)`.

`resolve.go:675` is the **only** read of `Use.Part()` in non-test code in the
repository. So the observable surface of the whole Part axis is: a `PropertyUse`
in a clause the resolver can reach. That is the surface §1 is about, and
divergence 2 is not on it.

A third route exists and the bead does not mention it: a quantifier body
(`any(k IN n.tags WHERE k = $q)`) is mined by `typeQuantifier` from **inside**
`mineWhere`, so its parameter lands Part 0 — with the direct half, not with the
EXISTS half. The split is not "outer vs nested scope". It is "mined inside
`mineWhere`" vs "mined by its own walker hook", and it has three members.

**Therefore divergence 2 is not a defect to fix and not a reason to add an
axis.** It is a discrepancy in a field that has no reader, and it is repaired as
a side effect of §4 rather than pursued for itself.

## 3. Why the axis is declined

**The semantic scope of a WITH-trailing WHERE is a Part, and it is already
built.** openCypher evaluates that WHERE against the projection's output scope.
The parser materialises exactly that scope as Part K+1, seeded from Part K's
export (`exportedTypes`, `listener.go:478`), and the resolver materialises it as
Part K+1's `partScope` (`newScope`, `scope.go:109`). A "semantic scope" axis on
the Use would carry the integer K+1. The lexical axis carries K only because it
is stamped one statement too early.

An axis is warranted when the wire cannot express the answer. Here it can, so
the four costs a second axis brings — a field on three Use variants, three
positional constructors, a wire key, and a second number a reader must learn to
choose between — buy nothing the reordering does not.

The empirical form of that argument: moving the call reproduces the answer a
semantic axis would give, on every row measured, at a corpus cost of two
integers in one golden. §4.

**What this does not claim.** It does not claim no construct in this language
ever needs a scope that is not a Part. It claims it for the WITH-trailing WHERE,
which is the construct gqlc-4w5 is about, and it makes no finding about ORDER BY
/ SKIP / LIMIT sort items (whose Uses are `ClauseSlotUse`, Part-unread), about
CALL arguments (gqlc-fvo §7.7's separate follow-up), or about a future consumer
of `ExprUse.Part()` that does not exist.

## 4. The remedy, measured

Two separable edits. Regime letters are the ones used in the census table.

- **Regime B — drop `pairAddSub`'s `appendRef`** (`expr.go:604`, `:611`).
  Clears the §1.2 false refusals. Leaves FINDING-9 and §1.3 untouched. Corpus
  cost: **zero** goldens, whole suite green. That zero is the finding, not the
  reassurance — it means the leak has no test.
- **Regime C — move `mineWhere` after `closePartOpenNext`** in `EnterOC_With`,
  keeping `appendRef`:

  ```go
  func (l *listener) EnterOC_With(c *gen.OC_WithContext) {
      l.collectProjection(c.OC_ProjectionBody())
      if l.err != nil {
          return
      }
      l.closePartOpenNext(exportedTypes(l.curPart))
      if w := c.OC_Where(); w != nil {
          l.mineWhere(w)
      }
  }
  ```

  Under C, `appendRef` stops being a leak and becomes the mechanism that
  produces the §1.3 refusals: the integrity sweep now runs the WHERE's names
  against the scope the WHERE actually sees.

**C is the recommendation. B is not.** B fixes the half of the defect whose
symptom is loudest and leaves the half whose symptom is a wrong type. It is
listed because it is separable, cheap, and a legitimate first commit if the
execution wants the acceptance repair landed under its own fixtures before the
attribution moves.

### 4.1 The census

`$p@Pn` is the Part the Use was stamped at. Schema as in §1.1.

| # | query | A (master) | B | C |
|---|---|---|---|---|
| L1 | `WITH a AS b WHERE b.title = $p` | refused `b` | admit, unknown | admit @P1, unknown |
| L2 | `WITH count(a) AS c WHERE c = $p` | refused `c` | admit, unknown | admit @P1, unknown |
| L3 | `WITH a, count(a) AS c WHERE c = $p` | refused `c` | admit, unknown | admit @P1, unknown |
| L4 | `WITH a.title AS t WHERE t = $p` | refused `t` | admit, unknown | admit @P1, unknown |
| L5 | `UNWIND [1,2] AS u WITH u AS v WHERE v = $p` | refused `v` | R0: unwind binding | R0: unwind binding |
| L6 | `WITH a WHERE a.x = $p` | admit @P0, INT | admit @P0, INT | admit @P1, **INT** |
| L7 | `WITH a WHERE a.nosuch = $p` | `ErrUnknownProperty` | `ErrUnknownProperty` | `ErrUnknownProperty` |
| I1 | `WITH a.title AS t WHERE a.x = $p` | admit, INT | admit, INT | **refused `a`** |
| I2 | `WITH count(a) AS c WHERE a.x = $p` | admit, INT | admit, INT | **refused `a`** |
| I3 | `WITH a.title AS t WHERE a.title = 'x'` | admit | admit | admit |
| S1 | `WITH a.title AS a WHERE a.x = $p` | admit, **INT** | admit, **INT** | admit, unknown |
| S2 | `WITH a.title AS a WHERE a.nosuch = $p` | `ErrUnknownProperty` | `ErrUnknownProperty` | admit, unknown |
| S3 | `WITH a.title AS a WHERE a.title = $p` | admit, STRING | admit, STRING | admit, unknown |
| A1 | `WITH n WHERE $x` | @P0 bool | @P0 bool | @P1 bool |
| A2 | `WITH n WHERE exists{… $y}` | @P1 bool | @P1 bool | @P1 bool |
| C1 | `MATCH (a:Post) WHERE a.x = $p` | @P0 INT | @P0 INT | @P0 INT |

L6 is the row that decides whether C is affordable: the ordinary
`WITH a WHERE a.x = $p` keeps `property:INT`, because Part K+1's `partScope`
carries an unrenamed entity binding in the same lanes Part K did
(`newScope`, `scope.go:126-155`). Confirmed separately across carriage kinds —
single-candidate edge, edge union, `WITH *`, OPTIONAL-nullable node, two names
in one WHERE, chained WITHs — all unchanged between A and C.

C1 is the control: a MATCH-trailing WHERE is unmoved, because `EnterOC_Match`
(`listener.go:450`) opens no Part.

A1/A2 is divergence 2, agreeing at Part 1 under C without being aimed at.

### 4.2 The whole-tree price of C

`go test ./...` over the tree: **one** golden moves,
`internal/query/cypher/testdata/golden/WithWhere2_6f8b82aba9de.golden.json`, and
the diff is two `"part": 1` keys appearing on two `PropertyUse` records. Its
scenario is `WITH a, advertiser, red, out WHERE advertiser.id = $1 AND a.id = $2`
— unrenamed carries, so Part 0 and Part 1 witness identically and the resolver's
answer does not change. The resolver suite, the codegen corpus and the AGE and
neo4j backends are byte-identical.

## 5. What C still gets wrong, stated as limits

- **S1 stops being wrong and does not become right.** `$p` goes from the
  confidently wrong `INT` to `unknown`, and the query is still **admitted**.
  Neo4j refuses it ("expected Map, Node or Relationship but was String"). C
  removes a false type; it does not add the refusal.
- **S2 is an acceptance widening.** `WITH a.title AS a WHERE a.nosuch = $p`
  goes from `ErrUnknownProperty` to admitted-with-unknown. Master's refusal
  there is right by accident — it is refusing on the pre-projection binding,
  which is the same mistake as S1 with the sign flipped. It is still a query the
  toolchain used to reject and would then accept, and it must be declared.
- **I1 and I2 are an acceptance narrowing**, which is the direction that breaks
  a user. No corpus fixture has the shape (§1.4), so nothing in the tree moves,
  but a query outside the tree that reads a WITH-dropped name beside a `$param`
  starts failing at parse. §7.
- **I3 stays wrong.** An out-of-scope reference with no parameter anywhere in
  the WHERE is still not checked. Full scope-checking of a trailing WHERE means
  collecting its refs properly instead of discarding them, which is a different
  and larger change; `mineWhere`'s doc comment records why they are discarded
  and that reason does not evaporate under C.
- **A renamed entity carry witnesses `unknown`** (L1). This is **pre-existing
  and orthogonal**: `MATCH (a:Post) WITH a AS z RETURN z.x` already fails on
  master with `ErrOutOfR0Scope: z`, and `WITH a AS z WITH z WHERE z.x = $p`
  already commits `unknown` on master. C does not create the gap; it routes the
  trailing WHERE's parameter into a gap the projection path already has.
- **L5 lands on an unrelated R0 limit.** Once the parser stops refusing it, the
  resolver refuses it for `unwind binding`. The execution should report that
  rather than count L5 as repaired.

### 5.1 The refusal S1 wants, and why it is not in this ruling's recommendation

Part K+1 *does* know that `a` is a STRING — the column `a` resolves to
`ResolvedProperty{STRING}` through the carry. What blocks the refusal is that
`partScope` (`resolve.go:82-89`) has six lanes and none of them is the carried
non-entity type: `Snapshot` (`scope.go:1371`) copies the entity lanes only, and
its doc states the exclusion as **§2.3 invariant #3 — "carry-only lanes and
callTypes are NOT observable through partScope"**. `WitnessUse` then gates on
`Contains` (`scope.go:1401`), which reads entity lanes alone, so a property
access on a carried scalar is a miss rather than an error.

Making S1 refuse therefore means adding a seventh lane and a
`PropertyUse`-against-a-carried-scalar arm — a resolver-internal change that
**contradicts a documented invariant**, with `callTypes` sitting behind the same
sentence and a narrowing-precision reason behind it (`scope.go:118-125`). It is
not a wire change and it is not a new axis on `Use`. It is the right next
question and it is not this one; it belongs to the execution bead as a named
stretch, to be taken only with §2.3 amended deliberately rather than
incidentally.

## 6. What the execution owes

**Fixtures.** The corpus has no WITH-trailing-WHERE-with-parameter fixture at
all (§1.4), so every row below is new, not a rebaseline.

| fixture | expectation |
|---|---|
| `valid/parameter_with_trailing_where_carried_node.cypher` — L6 | `$p :: INT`; the row that proves C did not regress the ordinary shape |
| `valid/parameter_with_trailing_where_aggregate_alias.cypher` — L2 | admits; `$p` unknown. Master refuses this; it is the headline repair |
| `valid/parameter_with_trailing_where_renamed_carry.cypher` — L1 | admits; `$p` unknown, with a comment pointing at §5's renamed-carry gap |
| `invalid/parameter_with_trailing_where_dropped_name.cypher` — I1 | `ErrUnboundVariable` on `a` |
| `valid/parameter_with_trailing_where_alias_shadow.cypher` — S1 | admits; `$p` unknown. **Not** `INT`. This is FINDING-9's fixture and it must carry a comment saying the admit is a known residual, not the target state |
| `valid/parameter_with_trailing_where_carried_edge.cypher` | single-candidate edge property; `$p` typed, proving the edge lane survives the move |
| parser pin, L2 with a literal and L2 with a `$param`, side by side | the pair gqlc-cmp's pin is missing; neither may refuse |

**Mutation rows**, victim declared before each run, per decision 0005:

| # | mutation | expected victim |
|---|---|---|
| 1 | restore `mineWhere` to its pre-swap position | L2 fixture refuses `ErrUnboundVariable`; S1 fixture's `$p` reverts to `INT` |
| 2 | delete `appendRef` at `expr.go:604` only (a→b arm) | I1 fixture stops refusing — `a.x = $p` is the a→b arm |
| 3 | delete `appendRef` at `expr.go:611` only (b→a arm) | needs an I1 twin written `$p = a.x`; **if that fixture is not written the row is vacuous**, and the two arms ship on one witness |
| 4 | move `collectProjection` after the swap as well | L6 loses its binding — the projection must stay pre-swap, and nothing else pins that |
| 5 | drop the `PropertyUse` type assertion in `selectPartScope` so every Use reads its Part | S1 or L6 moves; if neither does, `selectPartScope`'s Part-agnostic arm is unwitnessed and §2's claim needs its own unit row |

Row 3 is the one most likely to come back vacuous: `pairAddSub`'s two arms are
symmetric and the corpus writes comparisons one way round.

Screen every row with `go test -c -o /dev/null ./internal/query/cypher/
./internal/resolver/` before trusting a RED — a mutation the compiler rejects is
not a mutation the tests killed. Regenerate goldens before each row: a golden
that absorbs the mutant goes green, and that SURVIVED is the finding.

**Beyond the code.** A short note appended to fvo spec §7.6.1 saying the residual
was diagnosed as a mining-order defect and pointing here, so a reader arriving
at §7.6.1 is not sent looking for an axis that was declined.

## 7. The ADR question

**One is owed, and only for the narrowing.** I1/I2 mean a query the toolchain
accepted now fails at parse with `ErrUnboundVariable`. That is the same class as
ADR 0030 ("a repeated property name is rejected") — an acceptance change with no
generated-code delta — and the model to follow is its shape: state the refused
shape in the first paragraph, give the sentinel and the message, say that the
refusal matches openCypher, and give the remedy (name the projected alias in the
WHERE).

S2's widening goes in the same ADR as the counter-direction, named rather than
buried: one edit both refuses more and accepts more, and an ADR that reports
only the half that breaks callers is not the record.

**No ADR is owed for the attribution move itself.** Zero generated-code bytes
change; the only artefact that moves is one parser golden's two integers (§4.2).
Re-derive the ordinal with `just adr-next` **at push time**, not when the branch
is cut.

## 8. Rejected alternatives

- **Add a semantic-scope axis on `Use`** (the bead's own proposal). §3 — for
  this construct the semantic scope is a Part, it is constructed at both ends
  already, and the new axis would carry an integer the existing one would have
  carried if read one statement later. It also would not help: a correct
  semantic Part index still witnesses S1 through `Contains`, which does not see
  the carried scalar, so the axis lands on the same `unknown` C lands on (§5.1)
  having cost three constructors and a wire key.
- **Fix only the intra-clause asymmetry** (the brief's option (b)). §2 — the two
  halves are `ExprUse`s and no production code reads `Part` on an `ExprUse`, so
  the fix would change no answer. The asymmetry also has three members, not two,
  and a fix framed as reconciling two of them would leave the quantifier route
  unexamined.
- **Regime B alone.** §4 — repairs the loud half and leaves FINDING-9 exactly as
  the bead describes it. Admissible as a first commit, not as the answer.
- **Decline and document the residual** (the brief's option (c)). Defensible
  while the residual was believed to be one exotic shape with no regression.
  Falsified by §1.2: the same statement order refuses ordinary Cypher, and the
  strongest argument for declining — "no regression versus pre-fvo
  any-valid-witness" — is an argument about S1 only and says nothing about L1–L5,
  which pre-date fvo and were never assessed.
- **Move `collectProjection` after the swap too**, for symmetry. Refused: the
  projection is evaluated in the PRE-projection scope, so Part K is correct for
  it. Mutation row 4 is what keeps that from being re-tried.
- **Full referential-integrity collection for a trailing WHERE**, closing I3.
  Not taken here. `mineWhere` discards the rich typer's refs for a reason its
  doc comment states (an aggregation alias in the WHERE would otherwise be
  forced into the closed Part's scope), and C makes that reason *less* pressing
  without removing it. It is a larger change with its own regression surface and
  it should be a bead with its own measurement, not a rider.

## 9. What would falsify this ruling

- **L6, or any unrenamed carried-entity shape, degrading under C.** The whole
  argument is that Part K+1's `partScope` is adequate for the names a
  WITH-trailing WHERE can legally mention. One counterexample where Part K
  witnesses and Part K+1 does not — a carriage kind not in §4.1's list — makes
  the move a precision regression and reopens the axis question.
- **A WITH-trailing WHERE construct whose semantic scope is neither Part K nor
  Part K+1.** That is what a semantic axis would be for, and none was found.
- **A reader of `ExprUse.Part()` appearing.** §2's "unobservable" is a statement
  about `resolve.go:675` being the only production read at `687896a9`. A
  consumer that reads the axis on a non-`PropertyUse` variant makes divergence 2
  observable and makes it a defect on its own.
- **I1/I2's narrowing hitting a real query.** If it does, the answer is not to
  revert C — the refusal is correct — but the ADR of §7 is then load-bearing
  rather than a formality, and the remedy sentence in it has to be right.
- **A driver that accepts `WITH a.title AS a WHERE a.x = $p`.** S1 is argued
  from openCypher's stated scoping and from Neo4j's error text, and it is not
  witnessed against a live server here. If a backend accepts it, the whole
  premise that the post-projection scope is the evaluating scope is wrong for
  that backend and this document is about a divergence that does not exist
  there.
