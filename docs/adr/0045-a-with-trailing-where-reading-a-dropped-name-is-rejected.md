# A WITH-trailing WHERE that reads a dropped name is rejected, and one that reads an unknown property on a shadowed alias is now accepted

`MATCH (a:Post) WITH a.title AS t WHERE a.id = $p RETURN t` is rejected with
`ErrUnboundVariable`, wrapped with the name: `unbound variable: a`. It
previously resolved, committing `$p :: property:INT` — `Post.id`'s type, read
off a binding the WITH had already dropped.

The same edit that produces that refusal also makes
`MATCH (a:Post) WITH a.title AS a WHERE a.nosuch = $p RETURN a` **accepted**
where it was previously rejected with `ErrUnknownProperty`. One change moves
acceptance in both directions, and an ADR that reported only the half that
breaks callers would not be the record. Both halves are below.

Decided in
[`docs/specs/ruling-4w5-semantic-scope-attribution.md`](../specs/ruling-4w5-semantic-scope-attribution.md),
implemented under `gqlc-dvd1`. No generated-code bytes change; this is an
acceptance change only, in the same class as ADR 0030.

## The refused shape

A WITH's trailing WHERE is evaluated by openCypher against the projection's
**output** scope. `WITH a.title AS t` exports `t` and nothing else, so `a` is
not a name that WHERE may mention. Neo4j reports `Variable 'a' not defined`.

gqlc's parser materialises that output scope as the Part the WITH opens.
`EnterOC_With` now mines the trailing WHERE **after** the Part swap rather than
before it, so the WHERE's variable references are checked against the scope the
WHERE actually sees. `pairAddSub` records `a` on that Part's referential-
integrity set, `buildPart`'s sweep finds nothing binding it, and the query is
refused.

- **Sentinel:** `cypher.ErrUnboundVariable`.
- **Message:** `unbound variable: a`.
- **Fail-site:** `buildPart`'s refs-vs-scope sweep, `internal/query/cypher/build.go`.
- **Remedy for the author:** name the projected alias. `WHERE t = $p` if the
  filter is on the projected value; carry the entity too — `WITH a, a.title AS
  t WHERE a.id = $p` — if the filter genuinely needs the node.

**The refusal matches openCypher.** It is the same refusal Neo4j gives, for the
same reason, and it is the behaviour gqlc already has for every other
out-of-scope read across a WITH boundary. What was anomalous was accepting it
here.

### Its reach is narrower than the rule

The refusal fires only when a `$param` is in the comparison. `pairAddSub` is
reached from `mineComparisons`, which recognises a `var[.prop]` against
`$param` pair; a WHERE with no parameter in it — `WITH a.title AS t WHERE
a.title = 'x'` — is still not scope-checked at all, because `mineWhere`
deliberately discards the rich typer's refs for a reason its doc comment
states. So this ADR records a refusal that is **correct where it fires and does
not fire everywhere it should**. Closing that gap (issue I3 in the ruling) is a
larger change with its own regression surface and is not taken here.

## The accepted shape, which is the counter-direction

`WITH a.title AS a WHERE a.nosuch = $p` was rejected with `ErrUnknownProperty`
and is now admitted, with `$p` typed `unknown`.

Master's refusal there was **right by accident**. It was refusing because
`nosuch` is not a property of `Post`, having read `a` as the pre-projection
node binding — the same mistake as the refused shape above with the sign
flipped. After the move, `a` in that WHERE is the post-projection alias, a
STRING, and a property access on a carried scalar is a miss rather than an
error: `partScope` has six lanes and none of them is the carried non-entity
type (`Snapshot`, `internal/resolver/scope.go`, documented §2.3 invariant #3 —
"carry-only lanes and callTypes are NOT observable through partScope").

So a query the toolchain used to reject it now accepts. The type it commits is
honestly `unknown` rather than confidently wrong, which is the improvement; the
missing refusal is a residual, recorded below.

## Why this direction rather than the reverse

The alternative was to leave the mining where it was and add a second,
"semantic scope" attribution axis on `query.Use`. The ruling declines it: for a
WITH-trailing WHERE the semantic scope **is** a Part — the one the WITH opens —
so the existing lexical axis can already name it, and the axis would carry an
integer the existing one carries once read one statement later. §3 and §8 of
the ruling.

The narrowing was also not the change's purpose, and it is not its largest
effect. The same pre-swap mining made the parser **refuse five legal Cypher
queries** whenever a `$param` was present, including the ordinary
post-aggregation filter `MATCH (a:Post) WITH count(a) AS c WHERE c = $p RETURN
c`, which failed with `unbound variable: c`. Each of the five parsed if the
parameter was replaced with a literal. Those refusals are repaired by the same
edit; the narrowing here is its cost.

## What it costs a caller

**Nothing in this repository.** No corpus fixture had the refused shape before
this change — the whole corpus contained no WITH-trailing-WHERE-with-parameter
fixture at all, which is why none of this was caught. The resolver sweep moved
zero existing cells.

**Out of tree, a query that reads a WITH-dropped name beside a `$param` starts
failing at parse.** It is a narrowing, it is the direction that breaks a user,
and the remedy sentence above is the whole of what such a user needs.

## Residuals this does not close

Stated because a reader arriving here should not mistake the refusal for
completeness. All are in the ruling's §5.

- **A shadowed alias is still admitted.** `WITH a.title AS a WHERE a.id = $p`
  goes from the confidently wrong `property:INT` to `unknown`, and is still
  accepted; Neo4j refuses it. Making it refuse needs a seventh `partScope` lane
  for the carried non-entity type, which contradicts the documented §2.3
  invariant. A separate bead, taken only with that invariant amended
  deliberately.
- **An out-of-scope read with no parameter is unchecked** (I3, above).
- **A renamed entity carry types its parameter `unknown`.** Pre-existing and
  orthogonal: `MATCH (a:Post) WITH a AS z RETURN z.id` already fails with
  `ErrOutOfR0Scope`. This change routes a trailing WHERE's parameter into a gap
  the projection path already had.
- **`UNWIND [1,2] AS u WITH u AS v WHERE v = $p`** stops being refused by the
  parser and is then refused by the resolver for `unwind binding`, an unrelated
  R0 limit. Not counted as repaired.

## What would change this

- **The refusal hitting a real query.** The answer is not to restore the
  pre-swap mining — the refusal is correct — but this ADR becomes load-bearing
  rather than a formality and its remedy sentence has to be right.
- **A backend that accepts a shadowed alias.** The premise throughout is
  openCypher's stated scoping plus Neo4j's error text; it is not witnessed
  against a live server. A backend that accepts `WITH a.title AS a WHERE a.id =
  $p` is one for which the post-projection scope is not the evaluating scope,
  and the divergence this records does not exist there.
- **I3 being closed.** Collecting a trailing WHERE's refs properly instead of
  discarding them would make the refusal fire on parameterless queries too, at
  which point the reach caveat above comes out.

## Provenance

Ruled under `gqlc-4w5`, implemented under `gqlc-dvd1`. Filed as a numbered ADR
here in `docs/adr/`, which holds decisions about the compiler.
