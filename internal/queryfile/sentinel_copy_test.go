package queryfile_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/areqag/gqlc/internal/queryfile"
)

// TestAllSentinelsReturnsACopy witnesses the property AllSentinels' doc comment
// asserts. Every caller in the tree only ranges over the result, so returning
// the canonical slice itself passed the whole repository (bd gqlc-95b8).
//
// The canonical element is put back BEFORE the assertion rather than after or in
// a defer: require aborts the goroutine, so a restore placed after a failing
// assert never runs, and under the leak that would leave the package's real
// sentinel set holding the canary for every later test in the binary.
//
// The comparison is on messages because errorlint forbids == between errors and
// errors.Is is the wrong question here: this asks whether the slot still holds
// the same value, not whether one error wraps another. The canary's message is
// unique, so message equality answers it.
func TestAllSentinelsReturnsACopy(t *testing.T) {
	first := queryfile.AllSentinels()
	require.NotEmpty(t, first, "an empty canonical set would make this test vacuous")

	canonical := first[0]
	first[0] = errors.New("written through AllSentinels' result")
	observed := queryfile.AllSentinels()[0].Error()
	first[0] = canonical

	require.Equal(t, canonical.Error(), observed,
		"AllSentinels handed back the package's own slice: a caller's write to element 0 was visible to the next call, so the documented copy is not being made and any one caller can corrupt the canonical set for every other")
}
