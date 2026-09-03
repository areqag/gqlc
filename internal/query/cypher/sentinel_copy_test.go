package cypher_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/areqag/gqlc/internal/query/cypher"
)

// TestAllSentinelsReturnsACopy witnesses the property AllSentinels' doc comment
// asserts. Every caller in the tree only ranges over the result, so returning
// the canonical slice itself passed the whole repository (bd gqlc-95b8).
//
// The canonical element is put back BEFORE the assertion rather than after or in
// a defer: require aborts the goroutine, so a restore placed after a failing
// assert never runs, and under the leak that would leave the package's real
// sentinel set holding the canary for every later test in the binary.
func TestAllSentinelsReturnsACopy(t *testing.T) {
	first := cypher.AllSentinels()
	require.NotEmpty(t, first, "an empty canonical set would make this test vacuous")

	canonical := first[0]
	first[0] = errors.New("written through AllSentinels' result")
	observed := cypher.AllSentinels()[0]
	first[0] = canonical

	require.True(t, observed == canonical,
		"AllSentinels handed back the package's own slice: a caller's write to element 0 was visible to the next call, so the documented copy is not being made and any one caller can corrupt the canonical set for every other")
}
