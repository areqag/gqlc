package fixtures

import (
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	ageskeleton "github.com/areqag/gqlc/test/data/codegen/valid/skeleton/golden/apache-age-pgx-v5"
)

// Compile-time contract for the Apache AGE emission. The golden files
// pin the emitted text; these assertions pin the shapes that text has to
// satisfy to be usable at all — a DBTX every pgx scope fits, and a
// SessionInit assignable to pgxpool.Config.AfterConnect, which is what
// makes a connection that fails the AGE canary a pool-acquisition
// failure rather than a query failure later. Both break the module
// build, so `go build ./...` over this module is the check.
var (
	_ ageskeleton.DBTX = (*pgxpool.Pool)(nil)
	_ ageskeleton.DBTX = (*pgx.Conn)(nil)
	_ ageskeleton.DBTX = pgx.Tx(nil)

	_ = pgxpool.Config{AfterConnect: ageskeleton.SessionInit}
)
