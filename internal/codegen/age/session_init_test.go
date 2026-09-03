package age_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestEveryTargetEmitsTheSameSessionInit requires the SessionInit every
// AGE golden carries to be byte-identical across the whole corpus.
//
// The property holds by construction today — SessionInit is emitted from
// one template with no per-target variation — and the codegen fence
// proves each golden matches what the emitter produces, so it holds
// transitively. What that leaves unpinned is the template GAINING a
// per-target conditional: the fence would stay green on regenerated
// goldens while the copies diverged.
//
// It is worth pinning because live_age_session_test.go binds one
// package's SessionInit to pools handing out handles generated into
// others, which is sound only while the copies issue the same
// statements. The live arm checks the two packages it mixes, rightly,
// since that is its own precondition; the general claim is a pure-text
// property of the emitted tree and does not belong behind a
// testcontainer. This runs in the default build with no container, and
// reds the moment the template grows a branch.
//
// The comparison is over raw source bytes rather than over the AST, so
// the statements' explanatory comments are held identical too. Those
// comments say WHY the search_path call and the operator canary are
// shaped as they are, and a per-target branch would plausibly arrive in
// them first.
func TestEveryTargetEmitsTheSameSessionInit(t *testing.T) {
	goldens, err := filepath.Glob(filepath.Join(corpusRoot, "valid", "*", "golden", ageTarget, "graph.go"))
	require.NoError(t, err)
	// A floor rather than NotEmpty: this sweep proves nothing if the glob
	// silently narrows to one file, and an all-identical answer over an
	// empty or singleton set is the shape that reads green while
	// asserting nothing.
	require.Greater(t, len(goldens), 10,
		"the golden glob matched %d graph.go, too few for an agreement sweep to mean anything", len(goldens))

	var firstPath, first string
	for _, path := range goldens {
		body := sessionInitSource(t, path)
		if firstPath == "" {
			firstPath, first = path, body
			continue
		}
		// Named per file, not counted: a caller who has to reconcile two
		// diverged copies needs to know WHICH target drifted from the
		// baseline, and a tally of distinct bodies names none of them.
		require.Equal(t, first, body,
			"%s emits a SessionInit that differs from %s, so the two targets no longer issue the same "+
				"session statements and a pool built by one cannot be handed to the other",
			path, firstPath)
	}
}

// sessionInitSource returns the emitted source of path's SessionInit,
// from the func keyword through its closing brace.
//
// A file that declares no SessionInit is a failure here rather than a
// skip. Reading it as "nothing to compare" would let a golden that lost
// the function entirely pass a sweep whose whole subject is that every
// target carries one.
func sessionInitSource(t *testing.T, path string) string {
	t.Helper()

	src, err := os.ReadFile(path)
	require.NoError(t, err)
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, src, parser.ParseComments)
	require.NoError(t, err)

	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "SessionInit" || fn.Recv != nil {
			continue
		}
		require.NotNil(t, fn.Body, "%s declares SessionInit without a body", path)
		return string(src[fset.Position(fn.Pos()).Offset:fset.Position(fn.End()).Offset])
	}
	t.Fatalf("%s declares no top-level SessionInit, so this target carries no session setup at all", path)
	return ""
}
