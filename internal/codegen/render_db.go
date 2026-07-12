package codegen

// renderDB emits db.go (spec §5.3, §5.6). The template is the spec's
// snippet verbatim; format.Source normalises whitespace on the way out.
// C1 revises the driverOrTx.run seam signature to []*neo4j.Record —
// self-contained snapshots that survive transaction close (§5.6).
// emitOneSentinels controls whether ErrNoRows / ErrMultipleResults are
// declared: true iff the batch contains at least one :one query.
func renderDB(pkg string, emitOneSentinels bool, target driverTarget) []byte {
	var sentinelBlock string
	if emitOneSentinels {
		sentinelBlock = `
// ErrNoRows is returned by a :one method when the query produced zero
// rows. Callers branch with errors.Is.
var ErrNoRows = errors.New("gqlc: no rows in result set")

// ErrMultipleResults is returned by a :one method when the query
// produced more than one row. Callers branch with errors.Is.
var ErrMultipleResults = errors.New("gqlc: multiple rows in :one result set")
`
	}
	importsBlock := "import (\n\t\"context\"\n\t\"fmt\"\n"
	if emitOneSentinels {
		importsBlock += "\t\"errors\"\n"
	}
	importsBlock += "\n\t\"" + target.neo4jImport + "\"\n)\n"

	return []byte(header() + `package ` + pkg + `

` + importsBlock + sentinelBlock + `
type Queries struct {
	db driverOrTx
}

func New(driver neo4j.DriverWithContext) *Queries {
	return &Queries{db: driverDB{driver: driver}}
}

func (q *Queries) WithTx(tx neo4j.ManagedTransaction) *Queries {
	return &Queries{db: txDB{tx: tx}}
}

// driverOrTx is the unexported run indirection: every generated query
// body routes through it, dispatching between the per-call-session
// path (New) and the caller-owned managed-transaction path (WithTx).
// C1 returns []*neo4j.Record — driver-documented self-contained value
// snapshots safe to consume after the transaction closes (§5.6).
type driverOrTx interface {
	run(ctx context.Context, cypher string, params map[string]any, access neo4j.AccessMode) ([]*neo4j.Record, error)
}

type driverDB struct {
	driver neo4j.DriverWithContext
}

func (d driverDB) run(ctx context.Context, cypher string, params map[string]any, access neo4j.AccessMode) ([]*neo4j.Record, error) {
	session := d.driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: access})
	defer session.Close(ctx)
	switch access {
	case neo4j.AccessModeRead:
		return neo4j.ExecuteRead(ctx, session, func(tx neo4j.ManagedTransaction) ([]*neo4j.Record, error) {
			result, err := tx.Run(ctx, cypher, params)
			if err != nil {
				return nil, err
			}
			return result.Collect(ctx)
		})
	case neo4j.AccessModeWrite:
		return neo4j.ExecuteWrite(ctx, session, func(tx neo4j.ManagedTransaction) ([]*neo4j.Record, error) {
			result, err := tx.Run(ctx, cypher, params)
			if err != nil {
				return nil, err
			}
			return result.Collect(ctx)
		})
	default:
		return nil, fmt.Errorf("gqlc: unknown access mode %v", access)
	}
}

type txDB struct {
	tx neo4j.ManagedTransaction
}

func (t txDB) run(ctx context.Context, cypher string, params map[string]any, _ neo4j.AccessMode) ([]*neo4j.Record, error) {
	result, err := t.tx.Run(ctx, cypher, params)
	if err != nil {
		return nil, err
	}
	return result.Collect(ctx)
}
`)
}
