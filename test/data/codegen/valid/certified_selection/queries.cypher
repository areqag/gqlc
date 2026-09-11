// The two rows ADR 0044's table needs a golden for, and the two it cannot have
// here: a column that stays `any` is one the AGE backend declines to serve at
// all, so SumRank and MinMeta would take this whole fixture off that target.
// Both degrades keep their witnesses in the resolver corpus instead
// (certified_sum_stays_any, certified_min_unorderable_stays_any), which is the
// layer that decides them.

// MinRank is the rule. `rank` is declared INT32 NOT NULL and this column is
// `*int32`: the declared width, because min SELECTS one of the operand's own
// values rather than folding them, and a POINTER, because the group can be
// empty however non-null the property is.
// name: MinRank :one
MATCH (p:Person) RETURN min(p.rank) AS m

// MaxName is the same rule off the integer families, and the same NOT NULL
// trap: `*string` over a `STRING NOT NULL`.
// name: MaxName :one
MATCH (p:Person) RETURN max(p.name) AS m
