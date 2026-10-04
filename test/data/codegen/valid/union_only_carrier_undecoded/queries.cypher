// union_only_temporal_carrier and union_only_uuid_carrier with every read of
// the union taken away: no query returns the node whole, projects either union
// or binds one. Read the first of those head comments first.
//
// So no helper pair is emitted for either union. The entity's decoder is gated
// off (codegen.DecodedEntities, bd gqlc-m1dk), and the struct carries each
// union as *any, which names nothing. The package names no carrier, and it
// emits neither temporal.go nor uuid.go.
//
// Measured on origin/master 2fb767a6: all three targets emitted both files,
// naming no carrier anywhere else. The trigger walked every entity's unions
// while the helper emission walked the decoded entities' alone (bd gqlc-r2dp).
// TestTemporalCarriersAreEmittedExactlyWhenReferenced and its UUID twin hold
// the absence; the fixtures named above hold the presence.

// name: AccountIDs :many
MATCH (a:Account) RETURN a.id AS id
