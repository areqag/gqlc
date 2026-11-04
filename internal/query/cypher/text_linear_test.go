package cypher_test

import (
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/areqag/gqlc/internal/query/cypher"
)

// allocatedBytes is what f allocates, read off the runtime's cumulative
// counter; nothing else allocates while a non-parallel test runs it.
func allocatedBytes(f func()) uint64 {
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	f()
	runtime.ReadMemStats(&after)
	return after.TotalAlloc - before.TotalAlloc
}

// TestQuotedTextIsLinear pins that the places which quote a construct's source
// text cost linear memory in its length (bd gqlc-jjot2.33). antlr4-go builds a
// rule context's GetText and CommonTokenStream.GetTextFromInterval with `s +=`
// per child or token, so quoting a long `x+1+1…` projection allocated
// quadratically and a 256 KiB one took 3 s.
//
// It compares bytes allocated, not time: four times the input must cost under
// eight times the bytes. Linear lands near four, the quadratic quoting above
// near sixteen, and allocation does not vary with machine load.
func TestQuotedTextIsLinear(t *testing.T) {
	parse := func(s string) error {
		_, err := cypher.New().Parse(strings.NewReader(s))
		return err
	}
	for _, c := range []struct {
		name, pre, unit, post string
		run                   func(string) error
		// wantErr is the parse's verdict, so a row cannot pass by failing
		// before it reaches the quoting it measures.
		wantErr error
	}{
		{"projection name", "MATCH (x) RETURN x", "+1", "", parse, nil},
		{"aliased projection", "MATCH (x) RETURN x", ".a", " AS y", parse, nil},
		{"nested SET target refusal", "MATCH (n) SET n", ".a", " = 1", parse, cypher.ErrNestedPropertyTarget},
		{"relationship type alternation", "MATCH ()-[r:A", "|A", "]->() RETURN r", func(s string) error {
			cypher.RelationshipTypeAlternations(s)
			return nil
		}, nil},
		{"qualified function call", "RETURN ", "a.", "f()", func(s string) error {
			cypher.QualifiedFunctionCalls(s)
			return nil
		}, nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			src := func(n int) string { return c.pre + strings.Repeat(c.unit, n) + c.post }
			var err error
			small := allocatedBytes(func() { err = c.run(src(4 << 10)) })
			require.ErrorIs(t, err, c.wantErr)
			large := allocatedBytes(func() { err = c.run(src(16 << 10)) })
			require.ErrorIs(t, err, c.wantErr)
			ratio := float64(large) / float64(small)
			t.Logf("%d -> %d bytes, ratio %.1f", small, large, ratio)
			if ratio >= 8 {
				t.Fatalf("4x the input allocated %.1fx the bytes (%d -> %d): quoting is superlinear", ratio, small, large)
			}
		})
	}
}
