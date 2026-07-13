package codegen

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestRenderBoundaryNoResolverRef enforces the D3-mapping-table
// boundary: no render_*.go file (production or test) is allowed to
// reference the resolver package (spec §4.3, gqlc-ls8.3 acceptance).
// Prepare + types.go are the sole home of the mapping table; render
// walks committed data on preparedQuery / preparedRow / preparedListElem
// alone.
//
// Grep pattern is bare `resolver.` — catches every symbol, not just
// the enum names, so a future `resolver.Column.GroupingKey` or new
// symbol import cannot sneak in unchallenged. Documented exemptions:
// none.
func TestRenderBoundaryNoResolverRef(t *testing.T) {
	entries, err := os.ReadDir(".")
	require.NoError(t, err)
	var offenders []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasPrefix(name, "render") {
			continue
		}
		if !strings.HasSuffix(name, ".go") {
			continue
		}
		// The fence itself names the banned substring inside string
		// literals; it is not a semantic reference to the package and
		// is excluded to prevent the fence from firing on itself.
		if name == "render_boundary_test.go" {
			continue
		}
		body, err := os.ReadFile(filepath.Clean(name))
		require.NoError(t, err)
		for lineNo, line := range strings.Split(string(body), "\n") {
			if strings.Contains(line, "resolver.") {
				offenders = append(offenders, name+":"+itoa(lineNo+1)+": "+strings.TrimSpace(line))
			}
		}
	}
	require.Empty(t, offenders, "render_*.go files must not reference the resolver package (spec §4.3)")
}

// itoa is a dependency-free int → decimal string used by the fence's
// offender report. Kept local to avoid pulling `fmt` into a test that
// exists to enforce a boundary.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
