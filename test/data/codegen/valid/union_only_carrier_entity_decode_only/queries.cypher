// union_only_carrier_undecoded's schema with the node returned whole, and
// nothing else. Read that fixture's head comment first.
//
// The entity's decoder is the one place either union is read, so its helper
// pair is reached from the decoded entity and from no query position. Every
// other union_only_* fixture also projects or binds the union, so a trigger
// that read the queries' unions alone stayed green over all of them, the
// decoded entities' unions being the half this fixture exists for
// (bd gqlc-r2dp). TestGoldenBuild holds it: without temporal.go and uuid.go
// the decode helpers name Date and UUID, or toDate and toUUID, and nothing
// declares them.

// name: AccountWhole :one
MATCH (a:Account) WHERE a.id = $id RETURN a
