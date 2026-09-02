package age_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/areqag/gqlc/internal/codegen/age"
)

// TestSentinelsPinsNameToValue is the typo guard for age.Sentinels, and
// the only place a sentinel's manifest spelling and its symbol are both
// visible. A key is a string a fixture author copies into JSON, so a
// wrong one is not a compile error anywhere: it makes the manifest name
// resolve to the wrong refusal, or to none.
//
// The rows are written out rather than derived from the map, because a
// test that builds its expectation the way the code does agrees with any
// implementation, including a transposed pair.
func TestSentinelsPinsNameToValue(t *testing.T) {
	want := []struct {
		name     string
		sentinel error
	}{
		{"age.ErrUnsupportedQuery", age.ErrUnsupportedQuery},
		{"age.ErrUnsupportedSchema", age.ErrUnsupportedSchema},
		{"age.ErrRelationshipTypeAlternation", age.ErrRelationshipTypeAlternation},
		{"age.ErrUndefinedFunction", age.ErrUndefinedFunction},
		{"age.ErrUndefinedSpatialFunction", age.ErrUndefinedSpatialFunction},
		{"age.ErrUndefinedNamespace", age.ErrUndefinedNamespace},
	}

	got := age.Sentinels()
	require.Len(t, got, len(want), "Sentinels publishes a name these rows do not pin")
	for _, row := range want {
		published, ok := got[row.name]
		require.Truef(t, ok, "Sentinels publishes no %q, so no manifest can name that refusal", row.name)
		require.Samef(t, row.sentinel, published,
			"%q resolves to a different sentinel than the symbol of that name", row.name)
	}
}

// TestSentinelsNamesEveryRefusal reads this package's own source for the
// package-level sentinels it declares and requires each one to be
// published. It exists because the alternative — publishing whatever
// somebody remembered to add — has already failed once: this bead's
// design enumerated four sentinels, and a fifth
// (ErrUndefinedSpatialFunction) landed on master hours later. Nothing
// was red. Publication is what makes a refusal nameable in the corpus,
// so an unpublished one is a refusal no fixture can witness, which is
// the defect the whole feature exists to remove.
//
// It walks the AST rather than grepping, because a commented-out
// declaration satisfies a grep and would let the source drift behind a
// comment while this sweep reported agreement.
//
// If a sentinel is ever genuinely unwitnessable through the front end,
// this test is where that is recorded — publication implies a fixture
// witness, so the answer is not to publish it quietly.
func TestSentinelsNamesEveryRefusal(t *testing.T) {
	declared := declaredSentinelIdents(t)
	require.NotEmpty(t, declared,
		"this sweep read no sentinel declaration at all, so it reconciles nothing; "+
			"the source walk is broken, not the package")

	published := age.Sentinels()
	for _, ident := range declared {
		_, ok := published["age."+ident]
		require.Truef(t, ok,
			"age.%s is a package-level sentinel this package declares and age.Sentinels does not publish, "+
				"so no invalid fixture can name it; publish it and witness it with a fixture", ident)
	}
	require.Len(t, published, len(declared),
		"age.Sentinels publishes a name that is not a package-level sentinel of this package")
}

// declaredSentinelIdents returns the names of every exported
// package-level `var ErrX = errors.New(...)` in this package's
// non-test source, sorted by nothing — the caller only membership-tests.
func declaredSentinelIdents(t *testing.T) []string {
	t.Helper()

	entries, err := os.ReadDir(".")
	require.NoError(t, err, "reading this package's directory")

	var idents []string
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), name, nil, parser.SkipObjectResolution)
		require.NoErrorf(t, err, "parsing %s", name)

		for _, decl := range file.Decls {
			gen, isGen := decl.(*ast.GenDecl)
			if !isGen || gen.Tok != token.VAR {
				continue
			}
			for _, spec := range gen.Specs {
				value, isValue := spec.(*ast.ValueSpec)
				if !isValue {
					continue
				}
				for i, declName := range value.Names {
					if !declName.IsExported() || !strings.HasPrefix(declName.Name, "Err") {
						continue
					}
					if i < len(value.Values) && isErrorsNewCall(value.Values[i]) {
						idents = append(idents, declName.Name)
					}
				}
			}
		}
	}
	return idents
}

// isErrorsNewCall reports whether expr is a call to errors.New. That is
// what distinguishes a sentinel from any other exported Err-prefixed
// var: a sentinel is a value callers match with errors.Is, and one built
// any other way — wrapped, formatted, or assigned from elsewhere — is
// not this package's own refusal to publish.
func isErrorsNewCall(expr ast.Expr) bool {
	call, isCall := expr.(*ast.CallExpr)
	if !isCall {
		return false
	}
	sel, isSelector := call.Fun.(*ast.SelectorExpr)
	if !isSelector || sel.Sel.Name != "New" {
		return false
	}
	pkg, isIdent := sel.X.(*ast.Ident)
	return isIdent && pkg.Name == "errors"
}
