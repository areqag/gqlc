# ADR 0029 — the codegen spec fence grades written censuses, not derived markers

**Status:** Accepted
**Date:** 2026-08-17
**Beads:** gqlc-rz0l, gqlc-lhs3, gqlc-jfwo, gqlc-0rjn, gqlc-vu7z, gqlc-e143, gqlc-173n, gqlc-jnsk, gqlc-offa, gqlc-ipx6, gqlc-x2sg, gqlc-cgat

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

> This document prints the driver-binding literal without its opening brace.
> The literal with its brace is the binding sweep's anchor and `docs/` is swept
> whole, so spelling it here would be a graded site in a document no census
> declares. The parameter lists below are a different case: they are printed
> without their enclosing parentheses and are read anyway, and three of them are
> named in `specBareListExhibits` (decision 10) as exhibits. Nothing else here
> is exempted by that census: a parenthesised parameter list, and a
> parenthesis-less one added without its census entry, are both read exactly
> as they would be in any other document.

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

## Decision 3 — the floor is a per-document count of graded sites

**Superseded in part.** As first decided, the floor was *one* graded site per
listed document; it is now a number written beside each document in
`specSigDocs` and `specBindDocs`, and the paragraphs below record why the
weaker form was accepted and what changed. The measurement that motivated it,
and the three declined alternatives, still stand as written.

A census keyed on documents is itself a count of one per document, and that
count has slack. A listed document kept its entry on **one** surviving graded
site, so every site past the first could leave the sweep with nothing said —
not by being corrected, but by ceasing to print an anchor it is found on. Each
anchor is a delimiter and the context parameter or the literal's type, both
halves: an open parenthesis or a code span's opening backticks before
`ctx context.Context` for a signature, the opening brace of the `map[string]any`
literal for a binding. Dropping the delimiter alone is enough, which is why
there are two signature anchors rather than one (decision 10).

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

So the floor of one was accepted and recorded, in this ADR and in
`docs/specs/codegen-stage-c1.md` §5.3.

**What changed.** The declined alternatives above are all *per-site*: they ask
what the sites are. The form now in the tree asks only how many there are, per
document — `specSigDocs` and `specBindDocs` map each listed document to the
number of graded sites it owes, and `requireCensusFloors` reports a shortfall by
document name with both numbers beside it. That is not the bare `9 != 10`
Decision 2 rejects, which names nothing a reader can open; and it is not the
verbatim census, which is a copy of these documents. It closes the measurement
above: rewriting C4 §3.2's `RemovePerson` context parameter takes C4 from ten
graded signatures to nine and is red, by name.

The comparison is `>=`, not `==`. An equality reddens on every honest addition,
and a census that reddens on honest edits is a census maintainers bump without
reading — the same objection that declined the verbatim form. Only a removal has
to be written down, which is the price the membership half already charges.

What a count still does not distinguish is a document that loses one site and
gains another. A count is a size, not a membership; the membership reading is
the verbatim census, still declined, still for the reason above.

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

## Decision 10 — the delimiter is half the anchor, so a code span is the second one

The signature anchor was an open parenthesis followed by the context parameter,
and three places in this branch described it as the context parameter alone.
The difference is what an escape costs. Rewriting the context parameter is a
visibly wrong edit; dropping the parentheses is a cosmetic one, and it was
enough. Measured: a bullet reading ``the parameter list is `ctx
context.Context, minAge int64` `` inserted into C1 §5.3 — the section this
branch exists to correct, spelling the capture vector `gqlc-lhs3` removed —
left both sweeps green.

So a second anchor reads the same context parameter behind a code span's opening
backticks, and the matching closing run terminates the list the way the closing
paren does. The delimiter is the whole run rather than one backtick of it, so a
list written inside two backticks is read the way one written inside a single
backtick is. The two anchors share one grading step (`gradeParams`), so arity,
the whole-list placeholder and the name position are decided identically
whichever delimiter carried the list.

A document explaining a fence has to quote what the fence catches, and this one
does three times — decision 4's two placeholder positions and the capture vector
above. `specBareListExhibits` names those parenthesis-less parameter lists
verbatim, per document, and they are read but not graded: they are exhibits
rather than claims about the emitted surface.

The exemption is per list rather than per document, so a claim this document
makes about the emitted parameter list is read on the same terms as any other
document's, and each entry covers one site, so a second list spelled the same
way is graded — it is red in the `undeclared` direction, because the entry
exempted the first occurrence and no entry covers the second. It is a written
census reconciled in both directions: an entry the document has stopped
printing is red in the `lost` direction by its text. What that leaves
open is a claim put in an exhibit's place and spelled the way the exhibit was:
it takes the entry (`gqlc-x2sg`).

Two things are deliberately not reached, and the first drags a third along that
was not chosen:

- **A parenthesis-less parameter list with no code span around it**, in running
  prose or on its own line inside a code block. The paren anchor needs no span
  and reaches a code block like any other text, so what a block hides is this
  spelling alone. Grading unfenced prose means grading every comma in the
  documents; reading a code block means parsing markdown block structure before
  any anchor runs, and a block body is lines rather than a signature. So the
  signature sweep takes a run of three or more backticks opening a line as such
  a block's delimiter rather than as a span's, and the fenced and the indented
  spellings are both pinned as skips in `TestSpecBareSigScannerDetectsDrift`.
  The prose half is the same surface as `gqlc-e143`; the block half is
  `gqlc-cgat`.

  That rule is a byte test, not a parse, so it does not divide spellings where a
  renderer divides them, and the third thing not reached is a list that *does*
  carry a span. A run of three opening its line but closed before the line ends
  is an ordinary code span to CommonMark — a backtick fence's info string may
  not hold a backtick — and is skipped here regardless; a single backtick on a
  tab-indented line is read here while a renderer shows the contents of an
  indented code block. Neither direction can be closed by a better fence rule,
  because the line-opening test is reached only by a run of three or more. Both
  are pinned, C1 §5.3 states the exemptions as a floor for that reason, and this
  overreach of the block rule is `gqlc-cgat` too.
- **The binding half.** The symmetric move — reading a `"key": value` pair with
  no `map[string]any` literal around it — was measured before it was declined:
  the swept documents hold over 500 such spans across more than 30 files,
  essentially all of them JSON model shapes with no relation to a driver
  binding. A sweep that reddens on those is worse than a named limit, so the
  limit is named (`gqlc-offa`) and the brace stays part of the binding anchor.

## Consequences

The fence is a graded-site check, not a document check. What it does **not**
reach is enumerated in `docs/specs/codegen-stage-c1.md` §5.3 and in the file's
own header:

- Dropping `README.md` or `CONTEXT.md` from `docRoots` is green, because
  everything the fence observes is walked out of `docRoots` — the observation is
  derived from the list being audited, and no census names a document under
  either. Catching a root that shrank, or one that should have grown, needs
  candidate roots from outside the list (`gqlc-jfwo`). Dropping `docs`, or
  narrowing it to `docs/specs`, is red by the censuses naming documents beneath
  it.
- A claim put in the place of one of `specBareListExhibits`' exhibits, spelled
  the way that exhibit was, takes its exemption (`gqlc-x2sg`, Decision 10).
- A site swapped for another inside one document is invisible: the per-document
  floor is a count, not a membership (`gqlc-0rjn`, Decision 3).
- A signature carrying the author's names as separate arguments is past the
  arity the signature sweep reads (`gqlc-vu7z`).
- The prose around an intact graded span may say the opposite of it
  (`gqlc-e143`).
- The binding sweep peels pointer operators along with carrier conversions, so
  `*arg` and `&arg` unwrap to `arg` and stay green (`gqlc-173n`).
- A driver binding stated with no `map[string]any` literal around it is unswept,
  and so is a parenthesis-less parameter list with no code span around it — in
  running prose, or on its own line inside a fenced or indented code block
  (`gqlc-offa`, `gqlc-cgat`, Decision 10).
- `docFiles`'s guard that every `docRoots` entry exists on disk is neither
  witnessed nor in Decision 7's enumeration (`gqlc-ipx6`).
- The sweeps read raw markdown bytes, so a site inside an HTML comment is graded
  exactly as visible text is — invisible to a reader, present to the census
  (`gqlc-jnsk`).
