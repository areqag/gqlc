package neo4j

import (
	"github.com/areqag/gqlc/internal/codegen"
)

// renderDB emits db.go (spec §5.3, §5.6). The template is the spec's
// snippet verbatim; format.Source normalises whitespace on the way out.
// C1 revises the driverOrTx.run seam signature to []*neo4j.Record —
// self-contained snapshots that survive transaction close (§5.6).
// emitOneSentinels controls whether ErrNoRows / ErrMultipleResults are
// declared: true iff the batch contains at least one :one query.
// emitStream controls whether the streaming seam is declared: true iff
// the batch contains at least one :iter query. It is gated rather than
// unconditional so a batch with no :iter query emits the db.go it always
// did — the seam is a second method on driverOrTx, and adding it to every
// generated package would churn every golden to carry an interface method
// nothing in the package calls.
func renderDB(pkg string, emitOneSentinels, emitStream bool, target driverTarget) []byte {
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
	// The three streaming fragments are emitted together or not at all:
	// the interface method, the two implementations, and the exit
	// sentinel the managed-retry implementation returns.
	var streamSentinel, streamSeam, streamImpls string
	if emitStream {
		streamSentinel = `
// errIterStreamStarted aborts the unit of work of a :iter method that has
// already handed a row to the consumer. It never reaches the caller.
//
// Returning an error rather than nil is what keeps the driver's managed
// retry away from a stream in progress. ExecuteRead re-enters the unit of
// work when the transaction it wraps fails retriably — including on a
// commit failure, where the retry state reports "not done" and calls the
// work function a second time. A second pass would call yield on a range
// loop the consumer may already have broken out of, and the Go runtime
// panics on that ("range function continued iteration after function for
// loop body returned false").
//
// So once a row is delivered, every exit returns this: the driver never
// reaches TxCommit, and the re-entry window does not exist. The cost is
// that a :iter transaction which delivered a row is never committed,
// which is sound because :iter is refused on writes (ErrIterOnWrite).
// This sentinel is not retriable — it is not a driver error type — so
// returning it ends the managed envelope rather than restarting it.
var errIterStreamStarted = errors.New("gqlc: iter stream already delivered a row")
`
		streamSeam = `
	stream(ctx context.Context, cypher string, params map[string]any, yield func(*neo4j.Record, error) bool)`
		streamImpls = `
// stream runs cypher in a managed read transaction and hands each record
// to yield in order, stopping when yield returns false. Every error
// reaches the consumer through yield, so the caller has one delivery
// channel and never a second return value.
//
// Retry is pre-first-yield only. An error arriving before any record has
// been delivered is returned to the managed envelope, which may retry the
// whole unit of work; an error arriving after one has been delivered is
// yielded, because the consumer has already seen rows the retry would
// re-produce. Either way the consumer sees the error exactly once.
//
// stopped is what makes "exactly once" true, and it is not the same claim
// as delivered. It is set the moment the consumer can take no further
// item — because yield returned false, or because the error that is the
// sequence's last item has just gone out — and it fences the yield below
// the envelope. Without it the two delivery points are independent: the
// envelope reports the context error in its own words as well, so a
// cancelled stream yields twice, and a consumer that broke out of its
// range on the first error gets the second one thrown at a range loop
// that has already returned false, which is the runtime panic the exit
// rule exists to prevent (measured live 2026-09-11: rows=1, errs=2).
//
// The unit of work is typed struct{} rather than any, and that is a
// correctness requirement rather than a style choice. ExecuteRead is
// generic over the work's result and casts it UNCONDITIONALLY whenever the
// work returns no error — castGeneric type-asserts result to T — so a work
// function typed (any, error) returning a nil any beside a nil error
// panics inside the driver with "interface conversion: interface is nil,
// not interface {}". That return is the zero-row path: a query whose
// predicate matches nothing delivers no record and reports no error, which
// is ordinary input rather than an edge case. struct{} makes the nil
// unspellable, so no path can reintroduce it. Measured 2026-09-11 against
// neo4j-go-v5 5.28.4 and v6 6.2.0; the two drivers are identical here.
func (d driverDB) stream(ctx context.Context, cypher string, params map[string]any, yield func(*neo4j.Record, error) bool) {
	session := d.driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeRead})
	defer session.Close(ctx)
	delivered := false
	stopped := false
	_, err := neo4j.ExecuteRead(ctx, session, func(tx neo4j.ManagedTransaction) (struct{}, error) {
		// Re-entry guard. Reached only when managed retry calls this unit
		// of work again after a row has already gone to the consumer;
		// yielding a second time would panic. See errIterStreamStarted.
		if delivered {
			return struct{}{}, errIterStreamStarted
		}
		result, err := tx.Run(ctx, cypher, params)
		if err != nil {
			return struct{}{}, err
		}
		for record, recordErr := range result.Records(ctx) {
			if recordErr != nil {
				if !delivered {
					return struct{}{}, recordErr
				}
				// Set before the yield, not after: this error is the
				// sequence's last item whatever the consumer answers,
				// so a true from yield must not re-open the fence.
				stopped = true
				yield(nil, recordErr)
				return struct{}{}, errIterStreamStarted
			}
			delivered = true
			if !yield(record, nil) {
				stopped = true
				return struct{}{}, errIterStreamStarted
			}
		}
		if delivered {
			return struct{}{}, errIterStreamStarted
		}
		return struct{}{}, nil
	})
	if err != nil && !errors.Is(err, errIterStreamStarted) && !stopped {
		yield(nil, err)
	}
}

// stream runs cypher inside the caller's transaction. There is no managed
// envelope here and so no retry to re-enter: the exit sentinel the
// driverDB path needs has no counterpart on this one.
func (t txDB) stream(ctx context.Context, cypher string, params map[string]any, yield func(*neo4j.Record, error) bool) {
	result, err := t.tx.Run(ctx, cypher, params)
	if err != nil {
		yield(nil, err)
		return
	}
	for record, recordErr := range result.Records(ctx) {
		if recordErr != nil {
			yield(nil, recordErr)
			return
		}
		if !yield(record, nil) {
			return
		}
	}
}
`
	}
	// errors is unconditional: the Tx block below is emitted whatever the
	// batch holds, and ErrTxDone needs it even when no :one query does.
	importsBlock := "import (\n\t\"context\"\n\t\"errors\"\n\t\"fmt\"\n"
	importsBlock += "\n\t\"" + target.neo4jImport + "\"\n)\n"

	return []byte(codegen.Header() + `package ` + pkg + `

` + importsBlock + sentinelBlock + streamSentinel + `
// queries is the core every generated query method hangs off. Queries
// and Tx both embed it, which is what lets one emission of each method
// serve both handles; it is unexported so a Tx cannot hand it out.
type queries struct {
	db driverOrTx
}

type Queries struct {
	queries
}

func New(driver ` + target.driverIface + `) *Queries {
	return &Queries{queries: queries{db: driverDB{driver: driver}}}
}

func (q *Queries) WithTx(tx neo4j.ManagedTransaction) *Queries {
	return &Queries{queries: queries{db: txDB{tx: tx}}}
}

// driverOrTx is the unexported run indirection: every generated query
// body routes through it, dispatching between the per-call-session
// path (New) and the caller-owned managed-transaction path (WithTx).
// C1 returns []*neo4j.Record — driver-documented self-contained value
// snapshots safe to consume after the transaction closes (§5.6).
type driverOrTx interface {
	run(ctx context.Context, cypher string, params map[string]any, access neo4j.AccessMode) ([]*neo4j.Record, error)` + streamSeam + `
}

type driverDB struct {
	driver ` + target.driverIface + `
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
` + streamImpls + `
// ErrTxDone is returned by Commit when the transaction has already
// been committed or rolled back. Rollback on a finished transaction
// returns nil instead, so a deferred tx.Rollback(ctx) is always safe.
var ErrTxDone = errors.New("gqlc: transaction has already been committed or rolled back")

// Tx is an open write transaction and the session that owns it. Begin is
// the only constructor; the zero value is not usable. Finish it with
// exactly one Commit or Rollback — a Tx left unfinished holds a pooled
// connection until the driver's own pool reclaims it, which this package
// cannot do for you.
//
// Generated transactions are always write mode, and they do not retry:
// the managed ExecuteRead/ExecuteWrite path the other methods use retries
// transient cluster errors within MaxTransactionRetryTime, and an object
// with a Commit cannot, because retrying would re-run caller code that
// has already seen results.
//
// The query methods are promoted from the embedded core and run inside
// this transaction, reading its own uncommitted writes.
type Tx struct {
	queries
	session ` + target.sessionIface + `
	tx      neo4j.ExplicitTransaction
	done    bool
}

// Begin opens a write transaction. It is refused on a handle already
// bound to a transaction by WithTx: with the querier embedded on Tx
// there is no other transaction-bound handle to hold. neo4j allows one
// explicit transaction per session and cannot nest.
func (q *Queries) Begin(ctx context.Context) (*Tx, error) {
	d, ok := q.db.(driverDB)
	if !ok {
		return nil, errors.New("gqlc: Begin on a transaction-bound Queries")
	}
	session := d.driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeWrite})
	tx, err := session.BeginTransaction(ctx)
	if err != nil {
		return nil, errors.Join(err, session.Close(ctx))
	}
	return &Tx{queries: queries{db: txDB{tx: tx}}, session: session, tx: tx}, nil
}

// Commit commits the transaction and closes the session it owns. It
// returns ErrTxDone, without reaching the driver, if the transaction has
// already been committed or rolled back.
func (tx *Tx) Commit(ctx context.Context) error {
	if tx.done {
		return ErrTxDone
	}
	tx.done = true
	return errors.Join(tx.tx.Commit(ctx), tx.session.Close(ctx))
}

// Rollback rolls the transaction back and closes the session it owns. It
// returns nil, not ErrTxDone, on a transaction that is already finished,
// so a deferred Rollback beside a Commit is correct and needs no guard.
func (tx *Tx) Rollback(ctx context.Context) error {
	if tx.done {
		return nil
	}
	tx.done = true
	// Close, not Rollback: the driver's Close is rollback-if-pending and
	// returns nil on a transaction its own failed Run already tore down,
	// which is what makes rollback-after-a-failed-statement clean here
	// without a second state flag.
	return errors.Join(tx.tx.Close(ctx), tx.session.Close(ctx))
}
`)
}
