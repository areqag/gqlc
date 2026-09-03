# `valid/` means valid **at R3**, not valid end to end

Every fixture in this directory is a query the **resolver** accepts against its
paired schema. That is the whole of the claim. It is not a claim that codegen
can emit the query, and for most of this corpus codegen cannot.

A reader adding a fixture here is declaring one thing: the resolver resolves it.

## What was measured

Measured 2026-09-03 against both shipped backends, on the fixtures and schemas
in this directory:

- Most of the schemas under `schemas/` are refused by codegen at **schema
  admission** — before any query is looked at. Both backends refuse the same
  ones, with the same sentinel. Which they are is the partition at the foot of
  this file, which a test holds true; no count is written here, because a
  number in prose is the part that goes stale.
- Behind those schemas sit most of the fixtures here. Their queries are never
  examined by codegen at all, so "codegen refuses this fixture" would be a
  statement about the schema, not about the query the fixture is.
- Of the fixtures behind an admissible schema, some still generate and some are
  refused at the query level.

The refusal is one sentinel, reached two ways: a node type with a multi-label
set (`Employee&Person`) and an edge label shared across endpoint pairs both
require an explicit `Name` before codegen can derive a Go identifier.

That is not an accident to be repaired. Multi-label node types and reused edge
labels are a large part of what the resolver corpus exists to exercise —
plural endpoints, narrowing, orientation ambiguity. A schema written to make
those cases reachable is a schema codegen is entitled to refuse to name.

## Why the per-fixture codegen gate was not built

The stronger check considered was: run every fixture here through codegen, and
require either success or a recorded marker saying why codegen refuses it.

It was not built because the markers would not carry what they appear to carry.
The overwhelming majority would record, once per fixture, a single fact about
the fixture's *schema* — restating one refusal many times, in the place a
reader would go looking for a fact about the query. A per-fixture file is the
wrong shape for a per-schema disagreement.

## What is pinned, and what is not

`TestResolverValidCorpusStageBoundary` in `internal/codegen/conformance` pins
the partition below: it admits every schema in `schemas/` through both
backends and requires the two lists to match exactly, naming any schema that
moved sides, appears in neither list, or is listed but absent from disk. It
also requires the two backends to agree, so a divergence is named rather than
silently taken from whichever the test happened to ask.

So a schema that starts or stops being codegen-admissible cannot drift away
from this document. That is the event worth catching: it means either codegen
learned to name these types, or the corpus grew a shape it cannot.

**Not pinned:** the query-level half. Which fixtures behind an *admissible*
schema codegen accepts is deliberately left unasserted here, because it moves
whenever codegen grows a construct, and pinning it would redden this corpus for
changes that have nothing to do with it. Counts for that half live in the bead
record, not in this file, because nothing here holds them true.

<!-- The two lists below are parsed by TestResolverValidCorpusStageBoundary.
     Keep one bare filename per line between the markers. -->

<!-- BEGIN codegen-admissible -->
certificate.gql
satisfy_implied_label_endpoint.gql
social.gql
social_edgeunion.gql
social_r1.gql
social_r2.gql
social_r3_unionnn.gql
social_selfloop.gql
<!-- END codegen-admissible -->

<!-- BEGIN codegen-inadmissible -->
multiround.gql
satisfy_plural_edges.gql
satisfy_plural_edges_inline_subtype.gql
satisfy_plural_edges_mixed_symmetry.gql
satisfy_plural_edges_reversed_subtype.gql
satisfy_plural_edges_symmetric.gql
satisfy_singular.gql
social_r3.gql
social_r4.gql
social_r5.gql
social_r6.gql
social_r7.gql
<!-- END codegen-inadmissible -->
