package fixtures_test

import (
	"context"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	unionlistage "github.com/areqag/gqlc/test/data/codegen/valid/union_list_two_widths/golden/apache-age-pgx-v5"
)

// TestAGEBindsANullElementInAUnionListParameter executes the encode half of
// bd gqlc-3jhv: a list of closed unions bound as a parameter crosses with a
// NULL element in it, and still refuses an element outside the member set.
//
// Every list of unions a schema can spell has nullable elements — the
// closed-union alternatives take no NOT NULL — so []any{1, nil} is a value
// the schema declares legal. The emitted list encoder handed every element
// to the union's member switch, which refuses nil as carrying no member,
// so the bind failed with "element 1: encode UNION<…>: no member carries
// <nil>" before the method reached the wire.
//
// No container, on union_param_test.go's model and for its reason: the bind
// runs before q.db.Query, so a handle over a nil DBTX reaches it, and
// reaching the send is the nil-dereference panic that ledgersByTags reads
// as `sent`. The decode half has no such position and is executed live, by
// TestAGERoundTripsANullElementInAUnionList.
func TestAGEBindsANullElementInAUnionListParameter(t *testing.T) {
	t.Run("a null element crosses", func(t *testing.T) {
		_, sent, err := ledgersByTags(&[]any{int32(1), nil})
		require.True(t, sent,
			"the bind refused a null element the schema declares legal; it returned: %v", err)
	})

	// The row above is satisfied by a list encoder that validates nothing,
	// which is what a nil test placed AFTER dropping the member switch would
	// look like. This says the switch still runs on the other elements.
	t.Run("an element outside the member set beside a null is still refused", func(t *testing.T) {
		out, sent, err := ledgersByTags(&[]any{nil, true})
		require.False(t, sent,
			"a bool reached the wire in a list of STRING|INT32, so passing the null through also skipped the member check")
		require.Nil(t, out)
		require.ErrorContains(t, err, "LedgersByTags: parameter $tags:")
		require.ErrorContains(t, err, "no member carries bool")
	})
}

// ledgersByTags runs the generated method over a nil DBTX and reports which
// of the two things it did, as rowsByPick does and for its reasons.
func ledgersByTags(arg *[]any) (out []int64, sent bool, err error) {
	defer func() {
		r := recover()
		if r == nil {
			return
		}
		re, ok := r.(runtime.Error)
		if !ok || !strings.Contains(re.Error(), "nil pointer dereference") {
			panic(r)
		}
		sent = true
	}()
	out, err = unionlistage.New(nil, "g").LedgersByTags(context.Background(), arg)
	return out, false, err
}
