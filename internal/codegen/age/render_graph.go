package age

import (
	"github.com/areqag/gqlc/internal/codegen"
)

// canaryStmt is the operator-resolution probe SessionInit runs before a
// connection is handed out.
const canaryStmt = "SELECT '1'::ag_catalog.agtype + '1'::ag_catalog.agtype = '2'::ag_catalog.agtype"

// renderGraph emits graph.go: the pgxpool.Config.AfterConnect hook and
// the two graph lifecycle helpers. These are methods on *Queries but not
// query methods, so they stay off the Querier interfaces — those list
// the batch's queries and nothing else.
func renderGraph(pkg string) []byte {
	return []byte(codegen.Header() + `package ` + pkg + `

import (
	"context"
	"errors"
	"fmt"

	"` + pgxModule + `"
)

// SessionInit prepares a freshly opened connection for AGE. Assign it to
// pgxpool.Config.AfterConnect: a connection that fails here is discarded
// instead of entering the pool, so a misconfigured session surfaces at
// pool acquisition and not at the first WHERE clause.
//
// Sharp edge: this hook runs once, when pgx opens the connection, and
// what it sets is session state on the backend it opened. Session state
// set at connect time survives only under session pooling — any mode
// that does not give this connection a backend of its own for its
// lifetime routes later queries to a backend the hook never ran on.
func SessionInit(ctx context.Context, conn *pgx.Conn) error {
	// concat_ws drops the NULL, so a role whose search_path is empty
	// yields "ag_catalog" rather than a trailing empty element —
	// PostgreSQL rejects the latter as invalid list syntax.
	if _, err := conn.Exec(ctx, "SELECT set_config('search_path', concat_ws(', ', 'ag_catalog', nullif(current_setting('search_path'), '')), false)"); err != nil {
		return fmt.Errorf("gqlc: put ag_catalog on the search_path: %w", err)
	}
	// AGE expands a query into bare operators, so schema-qualifying every
	// call site is necessary but not sufficient — operator resolution
	// still runs through the search_path. The probe is arithmetic
	// because agtype has no implicit cast to a numeric type: with
	// ag_catalog off the path, + has no candidate at all and resolution
	// fails whatever the literals are.
	//
	// Evaluating the probe is also what loads AGE: + is a C function in
	// the extension's library, so PostgreSQL loads that library and runs
	// its _PG_init here, ahead of any user query.
	var ok bool
	err := conn.QueryRow(ctx, "` + canaryStmt + `").Scan(&ok)
	if err != nil {
		return fmt.Errorf("gqlc: AGE operator canary: %w", err)
	}
	if !ok {
		return errors.New("gqlc: AGE operator canary returned false")
	}
	return nil
}

// EnsureGraph creates the bound graph unless it already exists, so a
// repeated call is a no-op. The catalogue check and the create are not
// atomic: two sessions racing on the same new name can both pass the
// guard, and the loser gets AGE's duplicate-graph error.
//
// This is the first call that puts the bound name in front of AGE, so a
// name AGE will not have arrives here as its own "graph name is
// invalid". The wrap names the value and where it was bound, because
// AGE's message says neither.
func (q *Queries) EnsureGraph(ctx context.Context) error {
	const stmt = "SELECT ag_catalog.create_graph($1::name) WHERE NOT EXISTS (SELECT 1 FROM ag_catalog.ag_graph WHERE name = $1::name)"
	if _, err := q.db.Exec(ctx, stmt, q.graph); err != nil {
		return fmt.Errorf("gqlc: ensure graph %q (the name bound at New): %w", q.graph, err)
	}
	return nil
}

// DropGraph removes the bound graph and every label in it. An absent
// graph is not an error, so teardown is repeatable.
func (q *Queries) DropGraph(ctx context.Context) error {
	const stmt = "SELECT ag_catalog.drop_graph($1::name, true) WHERE EXISTS (SELECT 1 FROM ag_catalog.ag_graph WHERE name = $1::name)"
	if _, err := q.db.Exec(ctx, stmt, q.graph); err != nil {
		return fmt.Errorf("gqlc: drop graph %q: %w", q.graph, err)
	}
	return nil
}
`)
}
