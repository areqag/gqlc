# Spec-quoted error messages, measured against the code that emits them

A census taken on 2026-09-10 at `origin/master` c832d871, for bd
`gqlc-tsuu5`. It asks one question — **how far have the error messages
quoted in `docs/` drifted from the Go that emits them** — and answers it
with a number, a list, and a decision about whether to build a guard.

The decision, up front: **no guard.** Section 4 gives the argument and
names what would change it.

---

## 1. What was counted, and how

A *quotation site* is one place in a tracked markdown file under `docs/`
that reproduces an error-message format string or a rendered error
message. Two syntactic families, both swept by script:

- **F — a quoted `fmt.Errorf(...)` call.** 155 sites. The format-string
  literal is read out whole, folding a markdown line wrap back into one
  space, so a quote broken across two prose lines is recovered entire
  rather than truncated at the line end. One of the 155 is an extraction
  artefact — `codegen-stage-c5.md:548` quotes a prose ellipsis inside
  the literal — leaving **154** real sites.
- **S — an inline code span of the form `` `ErrX: <body>` `` or
  `` `%w: <body>` ``.** 17 sites. This is the shape R5 §4.3 uses, and
  the shape the drift that motivated this bead (bd `gqlc-eeq4y`) sat in.

**171 sites** between them.

Each site was matched against the code by *sentinel*, then compared
**whole-string**. A substring test is not a match here: `strings.Contains`
passes when the code emits `MANGLED: <want>`, and it passes when the code
has appended a clause the document does not know about — which is exactly
half the drift found below. Every verdict in §2 is whole-string equality
after two declared normalisations and nothing else:

1. **The `%%` escape.** `internal/codegen` writes generated source
   through `Fprintf`, so a message the generated code emits as `%q` is
   spelled `%%q` in the renderer. Renderer templates are unescaped once
   before comparison.
2. **The method-name slot.** A renderer template begins `%s: …` where
   the specs write `<method>:`, `<Method>:` or a concrete method name
   (`OnePerson:`). That one leading identifier slot is normalised; every
   other byte is compared literally.

Both normalisations over-accept in the same direction — they can only
turn a mismatch into a match, never the reverse — so the drift count in
§2 is a **floor**.

### 1.1 What this census does not see

State the limits, because the number means nothing without them.

- **A spliced message is invisible.** Where a format string is assembled
  from a constant rather than written inline — `unionColumnTypeArm` in
  `internal/resolver/resolve.go` is the known one, and lines 360–372
  there say why it exists — the phrase is not in any `fmt.Errorf` literal
  and no textual sweep can find it. Every such site is counted as
  IDENTICAL or missed entirely. That is the single largest reason to read
  §2's drift count as a floor.
- **Only `fmt.Errorf` is read.** `errors.New`, `l.fail` with a non-
  `Errorf` argument, `t.Fatalf`, and anything routed through a helper are
  outside the sweep.
- **Only the two syntactic families above.** A message paraphrased in
  prose without backticks is not a quotation site and is not counted;
  nothing here measures how many of those exist.
- **`docs/` only.** `README.md`, `CONTEXT.md`, `AGENTS.md`, `CLAUDE.md`
  and `CONTRIBUTING.md` were not swept.
- **One tree, one moment.** The counts are of c832d871 and go stale on
  the next merge. Nothing re-derives them.

---

## 2. The census

| Verdict | Sites | |
|---|---:|---|
| **IDENTICAL** — quote equals a live format string, whole | 138 | |
| **DRIFTED** — a present-tense claim about live code, wrong | 8 | corrected by this pass, §3 |
| **GONE** — the quoted message exists nowhere in the tree | 8 | §2.2 |
| **SUPERSEDED / PLANNED** — the document itself records a prior or a never-built state | 13 | §2.3 |
| **PLACEHOLDER** — `` `Error: <text>` ``, `%w: %s` with elided args | 4 | not a claim about any site |
| **total** | **171** | |

Read the top row carefully. 138 identical is a large number and it is
not evidence of health, because §2.4 shows that three quarters of the
messages the code emits are quoted by no document at all. A document
cannot drift from a message it never mentions.

### 2.1 DRIFTED — the eight

Each is a statement in the present tense about code or a test that exists
today, and each was wrong. All eight are corrected in this change. Line
numbers are as-found at c832d871, before the corrections moved them.

The table carries a ninth row, `codegen-stage-c5.md:961`. It is a
rendered example in a prose bullet, beginning with an ellipsis rather
than a sentinel, so neither family in §1 counts it as a quotation site —
it was found by eye beside the C5 templates and corrected with them. It
is in the table and out of the count, which is the only honest place for
it.

| Site | Document said | Code emits |
|---|---|---|
| `docs/specs/codegen-stage-c0.md:399` | `%w: line %d: %s`, arg `tok` | `%w: line %d: %q`, arg `cardTok` — `internal/queryfile/parse.go:67` |
| `docs/specs/cypher-query-parser-stage-14.md:891` | `%w: %s on %s` | `%w result field: %s on %s` — `internal/query/cypher/call.go:140` |
| `docs/specs/codegen-stage-c5.md:951` | `<Method>: record %d: decode column %q: %w` | `<Method>: decode column %q: %w` |
| `docs/specs/codegen-stage-c5.md:955` | `<Method>: record %d: column %q: unexpected relationship type %q` | `<Method>: column %q: unexpected relationship type %q` |
| `docs/specs/codegen-stage-c5.md:961` (prose) | `... record 42: column "action": …` | no record index is emitted anywhere |
| `docs/specs/codegen-stage-c5.md:1002` | `<Method>: column %q element %d: unexpected null (list-of-non-null)` | nothing — a nil element fails the `dbtype.Relationship` assertion instead |
| `docs/specs/codegen-stage-c5.md:1013` | `<Method>: column %q element %d: unexpected relationship type %q` | `<Method>: decode column %q element %d: …` |
| `docs/specs/resolver-stage-r5.md:854` | `ErrUnknownLabel: cannot infer type of unlabelled binding "a"` | the same, plus `— no edge in the pattern reaches a compatible schema node type` |
| `docs/specs/resolver-stage-r7.md:1162` | "Golden pins `ErrUnknownProperty: city.length`" | the golden pins the sentinel only; the message carries a `(CALL YIELD variable %q is a scalar)` tail |

Two of those eight — the last two — are **prefix** drift, where the code
appended a clause and the quote stopped short. A `Contains` check passes
on both. That is the measurement trap this bead was written around, and
it is 25% of what was found.

The C5 cluster is one design divergence rather than five typos: §5.5
proposed threading a record index through the `:many` edgeUnion messages
and the implementation declined, reusing the `:one` message set
byte-for-byte. The witness is `ListActions` in
`test/data/codegen/valid/edge_union_two_queries_same_column_shape/golden/neo4j-go-v5/queries.cypher.go`.

### 2.2 GONE — the eight

The quoted message exists nowhere in the tree, in code or golden.

- `gqlc: write path not implemented` — 4 sites (`codegen-stage-c1.md:1216`,
  `codegen-stage-c4.md:242`, `:307`, `:933`). C4 implemented the write
  path; three of the four sites are framed as the stub C4 replaces, so
  they are correct as history and are left.
- `ErrUnsupportedClause: <clause>` — 4 sites
  (`cypher-query-parser-stage-2.md:42`, `-stage-9.md:95`, `:218`,
  `-stage-12.md:312`). Stage 14 retired the sentinel entirely; the only
  trace left in code is the comment at
  `internal/query/cypher/errors.go:21` saying so. Each site is its own
  stage's record and is left.

### 2.3 SUPERSEDED / PLANNED — the thirteen

The document's own framing says the quote is not about today's code.
None is corrected, and correcting any of them would destroy a record
without making anything true — the ruling `docs/specs/codegen-sentinel-taxonomy.md`
already took for the sentinel names.

- **6** in `resolver-stage-r6.md` (`:546`, `:1092`, `:1094`, and their
  restatements at `:2073`, `:2081`, `:2083`). The section quotes the R5
  code it is about to retire, under the words *"currently"* and *"Verify
  these exact lines … at branch base"*.
- **3** in `model-change-0ig-call-args.md` (`:1806`, `:1812`, `:1816`) —
  resolver arg-site drift checks. `resolver arg-site` appears in no Go
  file; the proposal was never built.
- **2** — `resolver-stage-r0.md:572` and `resolver-stage-r7.md:875`, both
  `fmt.Errorf("%w: %s", ErrUnknownLabel, …)`. The R0-era message was
  widened; today `ErrUnknownLabel` carries eight distinct messages, none
  of them `%w: %s`.
- **1** — `codegen-stage-c0.md:1010`, inside a section already carrying
  the repository's `*Historical:*` marker.
- **1** — `cypher-query-parser-stage-13.md:316`, `%w: MERGE`, retired
  one stage later.

### 2.4 The reverse direction

Counting from the code instead of the documents:

- **141** distinct format strings in non-test, non-golden Go that wrap a
  sentinel with `%w`.
- **35** of them are quoted verbatim somewhere under `docs/`.
- **106** — 75% — are quoted by no document at all: 45 in
  `internal/codegen`, 41 in `internal/resolver`, 11 in
  `internal/schema/gql`, 4 in `internal/queryfile`, 3 in
  `internal/query/cypher`, 2 in `internal/codegen/age`.

---

## 3. What this change did

Corrected the eight DRIFTED sites in §2.1, each with a one-line note at
the site naming the bead and the witness, so a later reader can tell a
correction from an original. Nothing else in any document was touched,
and no test was added.

---

## 4. Why no guard was built

A fence that held every spec-quoted message against the code would have
to exempt **25 of the 33** non-identical sites — the 13 of §2.3 plus the
8 of §2.2 plus the 4 placeholders — because those documents are correct
precisely by *not* matching today's code. An exemption list four fifths
the size of the population is a list of assertions nobody re-reads, which
is the failure mode `codegen-sentinel-taxonomy.md` already names for
bare-name censuses.

The repository has settled this question once already, for sentinel
*names*, and the settlement is the argument here. `TestSentinelTaxonomy`
holds one living document against `allSentinels` in both directions, and
the C0–C6 stage specs are explicitly *not* held: they "stay as they are",
each carrying a note pointing at the live answer. That shape works
because there is a living document to point at. For message *text* there
is none — `codegen-sentinel-taxonomy.md` quotes no message anywhere, by
construction. So the honest sequence is: a living message index first, a
guard over it second. Not a guard over the stage specs, which are
history.

`internal/codegen/conformance/specfence_test.go` is the wrong instrument
for a different reason: it byte-scans markdown and grades spans against a
constant it reads from the emitter. Message drift needs the *other* side
too — every `fmt.Errorf` format string in the tree, recovered from source
— and the spliced-constant case in §1.1 means even that would under-report.

### What would falsify this

- A fence is worth building the moment a living message index exists to
  hold, for one package. The 41 unquoted `internal/resolver` formats in
  §2.4 are the obvious first population.
- If a re-run of this census finds the DRIFTED count above 8 without a
  matching rise in the total, hand correction is losing and the argument
  flips.
- The census is a floor, not a measurement of the whole. If the spliced-
  constant pattern of §1.1 spreads past `unionColumnTypeArm`, the
  textual sweep behind this document stops being able to answer the
  question at all.
