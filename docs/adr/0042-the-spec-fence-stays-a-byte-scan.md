# The codegen spec fence stays a byte scan of markdown

`internal/codegen/conformance/specfence_test.go` keeps reading raw document
bytes with one anchor per shape. No CommonMark parser enters the conformance
package, and none of the byte-versus-renderer divergences ADR 0029 records is
closed by widening the scan.

Three open beads were each waiting on this one answer. They do not receive the
same answer, because they are not all the same question: `gqlc-jnsk` is
executable now and needs no parser, `gqlc-cgat` stays a declared limit, and
`gqlc-x2sg` was never on this axis at all.

Written 2026-09-10, ruling the design bead `gqlc-r8na3`. It settles
[ADR 0029](0029-the-codegen-spec-fence.md) decision 10, which priced the parse
as a trade-off without deciding it. It changes no behaviour: nothing here moves
a byte of the scan.

## Why the question was open

`scanBareSigs`' own header states the trade-off and then declines to settle it —
the byte rule stands, it says, and that is "not proof that no parse rule could"
close the divergences. The limits that leaves are already declared in three
places: that header, ADR 0029's consequences, and
[`docs/specs/codegen-stage-c1.md`](../specs/codegen-stage-c1.md) §5.3. So what
was missing was never a description of the gaps. It was a decision about the
scan's shape, and while it was missing, three beads each carried the same
unanswered design call and none of them could be executed.

## What the corpus says

Measured 2026-09-10 at `6187c2f3`, over the 102 markdown files `docRoots`
reaches, with `cmark-gfm 0.29.0.gfm.13` supplying the ground truth for what a
renderer sees.

**Where the graded material is.** The signature anchor's text occurs 79 times in
source, and 81 times once CommonMark has normalised a newline inside a code span
to a space:

| position | signature anchor | binding anchor |
|---|---|---|
| inside a fenced or indented code block | 58 | 6 |
| inside an inline code span | 23 | 11 |
| in rendered prose text | **0** | **0** |

The two occurrences past the raw count are code spans reflowed across a source
line break — one in ADR 0029's decision 10, one in C1 §5.3 — which are an anchor
only after the normalisation ADR 0029 decision 8 compiles into `anchorPattern`.
That decision turns out to be load-bearing over two live sites rather than
defensive.

**Where the gaps are occupied.** Each bead names a construct the byte scan reads
differently from a renderer. In this corpus, none of them occurs:

| construct | bead | occurrences |
|---|---|---|
| a code-block line carrying the anchor with its parentheses left off | `gqlc-cgat` | 0 |
| a run of three or more backticks that opens a line and is closed on it | `gqlc-cgat` | 0 |
| a single backtick carrying the anchor on an indented line | `gqlc-cgat` | 0 |
| an HTML comment carrying either graded anchor | `gqlc-jnsk` | 0 |

There is exactly one HTML comment in the swept corpus and it carries neither
anchor. No code block contains literal `<!--` bytes.

## The ruling

**The fence does not parse markdown.** The decisive number is the third row of
the first table. A CommonMark parser's central service to a document consumer is
telling prose apart from code, and across this corpus that question has one
answer: no graded site is in prose. The fence's material is *examples* — the
code blocks and code spans a renderer shows as literal text — so a parser would
be bought here to arbitrate boundary cases, and every boundary spelling anybody
has written down is currently unoccupied.

Against that, a CommonMark implementation in `go.mod` is a third-party parser in
the supply chain of a test whose reason to exist is a capture vector
(`gqlc-lhs3`). A test dependency is still a `go.mod` dependency and still inside
`just vuln`'s surface.

**A declared limit stops being silent by corpus discipline, not by a parser.**
The weak point of leaving these limits declared is not that they are unclosed.
It is that "unoccupied" is a fact about one commit which nothing re-reads — in a
file whose own `nonSpecRootDocs` comment says that a reason which goes false
goes false silently. Where a limit is worth closing, the cheaper close is a check
that the ambiguous construct is *absent from the swept documents*: the same byte
scan, asked a different question. That is available for all four rows of the
second table, and it is the shape `gqlc-jnsk` should take.

## What this does not decide, and what it still cannot see

- **It does not close `gqlc-cgat`.** The capture vector can still be restated as
  a parenthesis-less parameter list on its own line inside a fenced or indented
  code block — the most normative-looking place a spec has — and every sweep
  stays green. This ruling accepts that gap rather than repairing it. What
  changes is only that it is a decision instead of an open bug.
- **The zeros above are dated.** Nothing in the tree re-reads them. Each can go
  false in one ordinary edit with no gate reporting it. That is the price of
  this ruling, and the reason the paragraph above prefers a corpus check to a
  scanner wherever a limit is worth closing at all.
- **An absence check over-rejects a document that explains the fence.** A
  document quoting what the fence catches has to print the construct being
  forbidden, and C1 §5.3 already does: one sentence there prints `<!-- ... -->`
  and the context parameter a clause apart. Any absence check needs an exemption
  census on the terms `specBareListExhibits` already sets — and that census is
  itself claimed positionally, which is `gqlc-x2sg`.
- **Two of the four constructs are out of reach of any byte rule.**
  `scanBareSigs` consults its line-opening test only from a run of three or more
  backticks, so the indented-single-backtick divergence cannot be closed by a
  better byte rule; absence-checking the construct or parsing are the only two
  things that reach it.
- **`cmark-gfm` is not the renderer of record.** These measurements are
  CommonMark plus GitHub's extensions, which is what GitHub renders and is not
  necessarily what every reader of these documents uses.
- **The scope is this fence.** Nothing here is a repository position on parsing
  markdown, and nothing here transfers to a sweep whose material is prose rather
  than examples. Such a sweep would face the reverse of the first table and
  would be entitled to the opposite answer.

## Consequences — the three beads

The premise that the three shared one question is two-thirds right, and they are
dispositioned differently.

- **`gqlc-jnsk` is executable, and needs no parser.** It is an over-read, and
  narrowing an over-read needs no block structure. The remedy its own bead
  proposes — stripping comment spans before scanning — is a byte transform,
  admitted by this ruling. Its hazard is that a `<!--` written inside a code
  block is literal text a reader sees, so a strip that cannot see block
  structure blinds the scan from there to the next `-->`; that is 0 occurrences
  today and is what its witness has to pin. The absence check is the alternative
  remedy, and the one this ruling prefers.

  Taken as the absence check, in
  [ADR 0029](0029-the-codegen-spec-fence.md) decision 15. The hazard above was
  real and this document was its victim: the three code-span quotes at lines 62,
  101 and 126 here mean a reader that cannot tell a span from prose opens a
  comment at the first and runs to the second. The check skips openers inside an
  inline code span for that reason, and an unterminated opener runs to end of
  file rather than being discarded. `scanBareBinds` is left out — it has no
  anchor to refuse — so the row above is closed for four of the five sweeps.
- **`gqlc-cgat` is a documented limit rather than a bug.** It is the only one of
  the three whose repair genuinely wants the block structure a parse supplies,
  and its hiding place is unoccupied. Its ALLOW pins in
  `TestSpecBareSigScannerDetectsDrift` stay, and a scanner that starts reading
  blocks is red on them by design. If it is ever closed, it is closed by an
  absence check over code-block lines and not by a parser.
- **`gqlc-x2sg` was never on this axis, and is not gated by this ruling.** A
  parser tells an exhibit from a claim exactly as well as a byte scan does,
  which is not at all: both are the same bytes in the same construct, and what
  separates them is authorial intent. What it needs is intent written where the
  fence can read it — a marker on the line, or a section the exemption is scoped
  to — which is a decision about prose a reader sees, and is independent of
  everything above.
