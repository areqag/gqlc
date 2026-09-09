// A projected nested list whose OUTER element is nullable. Before this
// fixture the corpus had no such row on neo4j: every neo4j nested-list
// golden was [][]int64 or [][]any, so `e.Nullable` was false on that path
// throughout, and three production sites went unwitnessed — the
// `Nullable: elemNullable` on prepare.go's ColumnList arm, and the nil arm
// plus the address-of in neo4j's walkListElemBody ColumnList arm. All three
// were deleted in a mutation battery on bd gqlc-dxhwp and the whole neo4j
// suite plus all ~60 goldens stayed green.
//
// Deleting them now emits `acc = append(acc, innerAcc1)` into an
// `[]*[]*string`, which does not compile, so these goldens fail to build
// rather than merely differing.
//
// Reached with no nested-list DECLARATION: `[g.tags, g.tags]` builds the
// second level out of a flat stored property, which is why neo4j enrols
// here (ADR 0035 refuses the storage, never the projection — "neo4j serves
// nested lists perfectly well as query results").

// name: TagsPair :many
MATCH (g:Grid) RETURN [g.tags, g.tags] AS tagss

// name: RanksPair :many
MATCH (g:Grid) RETURN [g.ranks, g.ranks] AS rankss

// name: LabelsPair :many
MATCH (g:Grid) RETURN [g.labels, g.labels] AS labelss
