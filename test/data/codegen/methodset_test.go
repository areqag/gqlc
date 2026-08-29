package fixtures_test

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"

	mixedage "github.com/areqag/gqlc/test/data/codegen/valid/mixed_read_write_batch/golden/apache-age-pgx-v5"
	mixedv5 "github.com/areqag/gqlc/test/data/codegen/valid/mixed_read_write_batch/golden/neo4j-go-v5"
)

// The generated packages' own claim that a *Tx satisfies the querier
// interfaces, checked by the compiler rather than by reflection. A *Tx
// carries no query method of its own; every one of them is promoted from
// the embedded core, so these two lines do not compile unless promotion
// works.
var (
	_ mixedage.Querier = (*mixedage.Tx)(nil)
	_ mixedv5.Querier  = (*mixedv5.Tx)(nil)
)

// TestTxMethodSet witnesses the promoted transaction surface over real
// generated packages, at the boundary a caller writes against.
//
// internal/codegen/txsurface_test.go asserts the STRUCTURE that should
// produce this surface — the embed, the receivers, the emitted pins — by
// walking the AST. That plus the Go spec's promotion rules implies the
// method sets, but implication is not a witness: this test asks the
// compiled packages what their method sets actually are.
//
// The absences are the load-bearing half. `MethodByName` reporting
// nothing for `Begin` on `*Tx` and `tx.Begin(ctx)` failing to compile are
// the same language fact, so these rows are how "nesting is impossible"
// is held. Note what is NOT claimed: Begin is still refused at RUNTIME on
// a handle WithTx bound to a caller's transaction, because that boundness
// is a dynamic-type fact no method set can express. The live battery's
// txBeginIsRefusedOnATransactionBoundHandle holds that half.
func TestTxMethodSet(t *testing.T) {
	tests := []struct {
		name string
		tx   reflect.Type
		root reflect.Type
		// absent are the names that must not be in *Tx's method set.
		// Each is a door back to the root handle or to nesting.
		absent []string
	}{
		{
			name:   "neo4j-go-v5",
			tx:     reflect.TypeOf((*mixedv5.Tx)(nil)),
			root:   reflect.TypeOf((*mixedv5.Queries)(nil)),
			absent: []string{"Begin", "Queries", "WithTx"},
		},
		{
			name: "apache-age-pgx-v5",
			tx:   reflect.TypeOf((*mixedage.Tx)(nil)),
			root: reflect.TypeOf((*mixedage.Queries)(nil)),
			// EnsureGraph and DropGraph are AGE-only and stay on the root
			// handle, so no graph-lifecycle call is reachable from a
			// transaction (docs/specs/codegen-tx-embedded-querier.md §6).
			absent: []string{"Begin", "Queries", "WithTx", "EnsureGraph", "DropGraph"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			for _, name := range tc.absent {
				_, found := tc.tx.MethodByName(name)
				require.False(t, found,
					"%s is in *Tx's method set; it must not be reachable from a transaction handle", name)
			}

			// The transaction's own lifecycle, and the promoted queries.
			// Without these rows the absences above would be satisfied by a
			// Tx that had no methods at all.
			for _, name := range []string{"Commit", "Rollback", "RemovePerson", "GetPersonName"} {
				_, found := tc.tx.MethodByName(name)
				require.True(t, found,
					"%s is not in *Tx's method set", name)
			}

			// Begin did not move: it is still the root handle's, which is
			// what makes it reachable at all.
			_, found := tc.root.MethodByName("Begin")
			require.True(t, found, "Begin is not in *Queries' method set")
		})
	}
}
