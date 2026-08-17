# ADR 0028 — the codegen spec fence grades written censuses, not derived markers

**Status:** Accepted
**Date:** 2026-08-17
**Beads:** gqlc-rz0l, gqlc-lhs3, gqlc-jfwo, gqlc-0rjn, gqlc-vu7z, gqlc-e143, gqlc-173n, gqlc-jnsk

## Context

`docs/specs/codegen-stage-c1.md` §5.3 specified the single-parameter query
method's argument as a mangle of the query author's parameter name
(`paramFieldName("minAge")` → `MinAge` → `minAge`). That is the capture vector
`gqlc-lhs3` removed from the emitter and replaced with `codegen.ParamArg`. The
prose kept documenting it for four stages because nothing held the specs against
the emitter, and a reader implementing to the spec would reintroduce the
vulnerability (`gqlc-rz0l`).

`internal/codegen/conformance/specfence_test.go` is the fence. It reads the
argument name from `codegen.ParamArg` and grades three shapes the specs write it
in: signatures printed whole, the `<param-list>` bullets that expand the
placeholder those signatures print instead of a list, and the values of the
driver-binding `map[string]any` literals.

This ADR records the design decisions behind that file, so its comments can
state contracts and cite here.

## Decision 1 — the requirement is written down, not derived from the documents

The first version derived which documents owed graded sites from marker regexps
run over the documents themselves. That cannot hold, and the failure is
structural rather than a matter of regexp quality: the markers were the same
shapes as the grading, so one edit to a graded site removed the document from
the requirement and from the grading in the same instant.

Measured: moving the comma out of a template's placeholder, so that the
parameter list `ctx context.Context, <param-list>` became
`ctx context.Context<param-list>`, unselected C1 from the marker and unread it
from the scanner together, and the capture vector went back into §5.3 green.
Defeating the signal defeated both sides of it. A tighter regexp only moves the
hole to the next spelling; it is the scanner auditing itself.

> This document prints parameter lists without their enclosing parentheses, and
> the driver-binding literal without its opening brace. An open parenthesis
> immediately followed by the context parameter is the signature sweep's anchor,
> and the literal with its brace is the binding sweep's; `docs/` is swept whole,
> so either spelling here would be a graded site in a document no census
> declares and the fence would redden on its own rationale. Keep it that way.

So the census is written in the test file, by name. It costs one line when a
document starts or stops printing the surface, and the failure prints the line
to add or remove.

## Decision 2 — a census, not a count

A site count sits beside the thing it counts, carries silent slack for every
site it is under, and fails as `13 != 14`, which names nothing to go and look
at. A census fails as a set difference with a document name in it.

The reconciliation runs in **both** directions. A listed document that grades
nothing is red (`lost`); a document that grades something without being listed
is red too (`undeclared`); a document named twice is red (`duplicated`). One
direction alone is free on the other side.

## Decision 3 — the floor is one graded site per listed document

A census keyed on documents is itself a count of one per document, and that
count has slack. A listed document keeps its entry on **one** surviving graded
site, so every site past the first can leave the sweep with nothing said — not
by being corrected, but by ceasing to print the anchor it is found on — the
context parameter for a signature, the opening brace of the `map[string]any`
literal for a binding.

Measured: C4 §3.2's `WriteQuerier` member `RemovePerson`, whose parameter list
is `ctx context.Context, arg int64`, is one of ten graded signatures in that
document. Rewrite its context parameter and every sweep stays green
(`gqlc-0rjn`).

The alternatives were considered and declined:

- **A per-site census keyed on the method name** does not close it. C4 grades
  `RemovePerson` at three separate sites, so its name survives any one of them
  leaving.
- **A per-site count** does close it, and is the shape Decision 2 rejects — it
  fails as `9 != 10`.
- **A per-site census of the graded text** closes it at the price of making the
  test a verbatim copy of the documents: red on every honest edit to an example.
  A fence that reddens on honest edits is one whose census gets bulk-updated
  without being read, which buys less than a floor of one that is written down.

So the floor is accepted and recorded, in this ADR and in
`docs/specs/codegen-stage-c1.md` §5.7.

## Decision 4 — the exemption list and the requirement list are the same list

A template writes the whole parameter list as a placeholder, in either of two
positions: `ctx context.Context<param-list>` glued to the context parameter, or
`ctx context.Context, <param-list>` past a comma. Either way there is no name in
the declaration to read, so the fence declines to grade it.

`specListRuleDocs` is what that exemption is declined *against* and what the
`<param-list>` bullet is required *from*. One list doing both jobs is what stops
either being free: a document that takes the exemption owes the bullet, and a
document that owes the bullet must be taking the exemption.

The two placeholder positions are read on identical terms. They were not: a
declaration glued to the context parameter was skipped as "something else glued
to `ctx`", so the comma'd `ctx context.Context, <bareParam> <T>` was red while
the glued `ctx context.Context<bareParam> <T>` was green. The exemption census
could not see the difference, because C4 writes the placeholder in both
positions and one intact template satisfied the document.

`listPlaceholderRe` is an enumeration (`<param-list>`, `<param-list-N>`) rather
than a test for `<…>`, because a generic placeholder test cannot tell
`<param-list>`, which stands for a list, from `<bareParam>`, which stands for
the author's parameter name — and exempting the second is the drift this file
exists to catch, wearing the first one's clothes.

## Decision 5 — the `<param-list>` tails are graded by identity, not by shape

`specListRules` holds the two tails verbatim. Reading "which arity is this?" off
the type position — a placeholder alone versus a placeholder with a generated
suffix — is an inference, and an inference is satisfiable by a spelling that
means something else. `, arg <ParamsType>` is the two-plus rule and reads as the
single-parameter one, so stating the two-plus rule twice in two spellings
satisfied both arities while the single-parameter rule sat in prose with the
capture vector in it. An illustration (`, arg int64`) did the same.

Graded by identity, neither is a rule: it is an extra span, and an extra span is
as red as a missing one. This is why the fence reads the **type** in this one
place, unlike the signature sweep, which reads only the name position.

## Decision 6 — a lone token is drift, not a legitimately unnamed parameter

Go's grammar reads a one-token parameter as a type and calls the parameter
unnamed. The fence deliberately does not, because the emitter names the argument
at every arity. The ruling is applied whole: a lone `<bareParam>` is not a type
either — it is the author's name standing where a declaration should be — so it
grades on the same arm as a lone `int64` rather than being waved through for
wearing angle brackets.

## Decision 7 — plumbing is witnessed; assertions are not

Six lines between a scanner's reading and an actual failure were structurally
unable to fail: `requireClean`'s empty-set guard, `requireCensus`'s `lost`,
`undeclared` and `duplicated` arms, and the two lines carrying a scanner's
unreadable-site return into the sweep that fails on it. Neuter any one, then
revert a signature, add a signature to an undeclared document, grow a declared
document's signatures past what the scanner reads, name a document twice, or
leave a parameter list or a map literal unterminated — and every sweep stayed
green.

`reconcile` and the two sweep accumulators are functions precisely so a witness
can supply a synthetic document set and reader and observe their judgement on a
clean tree. Inline, the only available witness was a swept document with an
unterminated span in it, which is not a state the repository is ever in.

The sweeps' own comparison bodies (`sig.arg != codegen.ParamArg` and the
binding's prefix test) are **not** witnessed. The line is drawn between an
assertion, whose deletion a reader sees in the diff, and plumbing, whose
neutering reads as bookkeeping and takes a failure with it. Witnessing the first
buys a guard against a reviewer missing a deleted `require`; witnessing the
second was the only way to see those six at all.

## Decision 8 — anchors normalise whitespace in the pattern, not in the document

`anchorPattern` compiles one anchor literal into a matcher reading whitespace
the way Go's tokeniser does: required between two identifier characters,
optional at every other boundary. gofmt wraps a long parameter list after the
open paren, which puts `ctx` on its own line and leaves a literal match finding
nothing; prose reflows the same way, and a second space is invisible in rendered
markdown.

The normalisation is compiled into the pattern rather than applied to the
documents, because collapsing a document's whitespace would move every byte in
it and these failure messages are only useful while they can still name a line.

## Decision 9 — one census per binding sweep, no union

The binding requirement was once the map anchor unioned with the query-method
anchor, so that gutting the literal out of a document still declaring the method
could not take its requirement with it. That union over-reached in the other
direction: any new document quoting a signature owed a map literal it had no
reason to carry. A written list needs neither half — gutting C1's literals fails
the `lost` direction by name, and a document quoting a signature owes a binding
only if it is listed.

## Consequences

The fence is a graded-site check, not a document check. What it does **not**
reach is enumerated in `docs/specs/codegen-stage-c1.md` §5.7 and in the file's
own header:

- `docRoots` losing or narrowing an entry is green, because everything the fence
  observes is walked out of `docRoots` — the observation is derived from the
  list being audited. Catching it needs candidate roots from outside the list
  (`gqlc-jfwo`).
- One graded site per listed document is the floor (`gqlc-0rjn`, Decision 3).
- A signature carrying the author's names as separate arguments is past the
  arity the signature sweep reads (`gqlc-vu7z`).
- The prose around an intact graded span may say the opposite of it
  (`gqlc-e143`).
- The binding sweep peels pointer operators along with carrier conversions, so
  `*arg` and `&arg` unwrap to `arg` and stay green (`gqlc-173n`).
- The sweeps read raw markdown bytes, so a site inside an HTML comment is graded
  exactly as visible text is — invisible to a reader, present to the census
  (`gqlc-jnsk`).
