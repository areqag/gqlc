package neo4j_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/areqag/gqlc/internal/codegen/neo4j"
)

// templateSentinelNames are the three exported Err* names this package
// writes into the package it GENERATES, as template text inside string
// literals in render_db.go. They are not this package's refusals and
// must never be published: nothing here returns them, and a manifest
// naming one would name a refusal no run of this backend can raise.
//
// They are listed so the sweep below can state that it did not read
// them, which is the one claim distinguishing the AST walk from the grep
// it replaces. Their absence is otherwise indistinguishable from a walk
// that read nothing at all.
var templateSentinelNames = []string{"ErrNoRows", "ErrMultipleResults", "ErrTxDone"}

// TestSentinelsNamesEveryRefusal reads this package's own source for the
// package-level sentinels it declares and requires each one to be
// published, and requires each published name to be one of them. It is
// the age twin (internal/codegen/age/sentinels_test.go) built for this
// package, and it exists here for the reason it exists there: the
// alternative — publishing whatever somebody remembered to add — has
// already failed once in age, where a design enumerated four sentinels
// and a fifth landed on master hours later with nothing red.
//
// Publication is what makes a refusal nameable in the conformance
// corpus, so an unpublished sentinel is a refusal no fixture can
// witness, which is the defect the feature exists to remove. The reverse
// direction matters for the same reason from the other end: a published
// name whose sentinel is gone is a promise to the corpus that this
// package can no longer keep.
//
// Neither existing guard reads this package's refusal sites.
// TestRegistryPublishesTheBackendSentinels (internal/cli/backends) sees
// that the registry publishes something; TestSentinelReachability sees
// that every published name is witnessed by an invalid fixture. Both
// start from the map, so a sentinel added here and never published is
// invisible to both.
//
// It walks the AST rather than grepping, which in this package is not a
// stylistic preference: render_db.go carries three `var ErrX =
// errors.New(...)` lines at the start of a line inside raw string
// literals, so a line-anchored grep reads four sentinels where there is
// one and reports three refusals this package cannot raise.
//
// What it does NOT catch: a key bound to the wrong sentinel value. The
// expectation is derived from the map the same way the map is built, so
// a transposed pair agrees with it. That is unreachable while the map
// has one entry, and is what age's hand-written
// TestSentinelsPinsNameToValue answers once there are two.
func TestSentinelsNamesEveryRefusal(t *testing.T) {
	declared := declaredSentinelIdents(t)
	require.NotEmpty(t, declared,
		"this sweep read no sentinel declaration at all, so it reconciles nothing; "+
			"the source walk is broken, not the package")
	for _, generated := range templateSentinelNames {
		require.NotContainsf(t, declared, generated,
			"the sweep read %s as a sentinel of this package; it is template text inside a "+
				"string literal in render_db.go, declared in the package this backend generates. "+
				"The walk is reading bytes rather than declarations", generated)
	}

	published := neo4j.Sentinels()
	for _, ident := range declared {
		_, ok := published["neo4j."+ident]
		require.Truef(t, ok,
			"neo4j.%s is a package-level sentinel this package declares and neo4j.Sentinels does not "+
				"publish, so no invalid fixture can name it; publish it and witness it with a fixture", ident)
	}
	require.Len(t, published, len(declared),
		"neo4j.Sentinels publishes a name that is not a package-level sentinel of this package")
}

// declaredSentinelIdents returns the names of every exported
// package-level `var ErrX = errors.New(...)` in this package's non-test
// source, in no particular order — the caller only membership-tests.
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
