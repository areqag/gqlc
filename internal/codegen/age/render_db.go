package age

import (
	"github.com/areqag/gqlc/internal/codegen"
)

// renderDB emits db.go: the Queries handle and the pgx seam every
// generated method runs against.
func renderDB(pkg string) []byte {
	return []byte(codegen.Header() + `package ` + pkg + `

import (
	"context"

	"` + pgxModule + `"
	"` + pgxModule + `/pgconn"
)

// DBTX is the pgx surface the generated methods run against.
// *pgxpool.Pool, *pgx.Conn and pgx.Tx all satisfy it, so the caller
// chooses the scope without this package naming a concrete type.
type DBTX interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

type Queries struct {
	db DBTX
}

// New returns a Queries bound to db. Every connection db hands out must
// have been through SessionInit.
func New(db DBTX) *Queries {
	return &Queries{db: db}
}

func (q *Queries) WithTx(tx pgx.Tx) *Queries {
	return &Queries{db: tx}
}
`)
}
