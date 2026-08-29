package codegen_test

import (
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/areqag/gqlc/internal/cli/backends"
	"github.com/areqag/gqlc/internal/codegen"
	"github.com/areqag/gqlc/internal/graph"
	"github.com/areqag/gqlc/internal/resolver"
	"github.com/areqag/gqlc/internal/schema"
)

// txTargets are the two backends whose Tx surfaces must agree. v6 is left
// out on purpose: the v5≡v6 emission is held byte for byte by
// driverAgnostic in internal/codegen/neo4j/corpus_test.go, so comparing v5
// against AGE here lets transitivity cover v6 without this test importing
// a second neo4j target.
var txTargets = []string{"neo4j-go-v5", "apache-age-pgx-v5"}

// txSurfaceNames is the exported Tx surface both backends owe, spelled
// with receivers because `Begin` hangs off `*Queries` while `Commit` and
// `Rollback` hang off `*Tx`. A bare name set cannot tell those apart, and
// a gate that cannot tell them apart would pass on an emission that moved
// a method to the wrong receiver.
//
// The set is closed — the gate refuses an emission carrying MORE exported
// Tx surface than this, not only one carrying less. Two concrete cases it
// holds. A `(*Tx).Close` is declined by docs/specs/codegen-tx-object.md
// §10 because Rollback's nil-after-done already covers the defer idiom,
// and a decline that nothing enforces is re-proposed by the next author
// to want one. A `(*Tx).Queries` is the accessor
// docs/specs/codegen-tx-embedded-querier.md removes: because the
// comparison is equality, an emission that still carries it FAILS here,
// so this gate enforces the removal rather than merely tolerating it.
var txSurfaceNames = []string{
	"func (*Queries) Begin",
	"func (*Tx) Commit",
	"func (*Tx) Rollback",
	"type Tx",
	"var ErrTxDone",
}

// txCoreLifecycleNames are the names that must not hang off the embedded
// core. Each would promote into `*Tx`'s method set: `Begin` and `WithTx`
// would make nesting expressible again, and `Commit`/`Rollback`/`Queries`
// would either collide with the real ones or hand the core back out.
var txCoreLifecycleNames = []string{"Begin", "Commit", "Rollback", "Queries", "WithTx"}

// txPrivateBeginErrors are the errors.New literals inside Begin that one
// backend emits and the other does not. AGE's `DBTX` seam admits a caller
// fake that cannot begin a transaction, so AGE must answer that case;
// neo4j's driverDB always can, so no counterpart exists there (spec §4.3).
// That is a body difference, not a surface difference, which is why it is
// declared here rather than compared across backends — but it is declared
// exhaustively, so a third literal appearing on either side fails.
var txPrivateBeginErrors = map[string][]string{
	"neo4j-go-v5":       {},
	"apache-age-pgx-v5": {"gqlc: the DBTX bound by New cannot begin a transaction"},
}

// TestTxSurfaceAgreesAcrossBackends is the whole assertion that the
// emitted transaction handle is target-portable. Nothing else makes it:
// Tx is emitted into db.go, and the corpus's cross-target comparison
// TestBackendInvariantSurface skips db.go and graph.go via
// connectionSurface (internal/codegen/conformance/conformance_test.go:651,
// applied at :751) because those files hold the backend-specific handle
// and differ by construction. Do not delete this test as redundant with
// that one, and do not un-exclude db.go there instead — the rest of db.go
// differs on purpose.
func TestTxSurfaceAgreesAcrossBackends(t *testing.T) {
	surfaces := make(map[string]txSurface, len(txTargets))
	for _, target := range txTargets {
		surfaces[target] = extractTxSurface(t, target)
	}

	// Per backend, by name, before any cross-backend comparison: two
	// backends that both omit the Tx block produce two equal empty
	// surfaces, and an equality-only gate reports that as agreement.
	for _, target := range txTargets {
		require.Equal(t, txSurfaceNames, surfaces[target].names,
			"%s: emitted Tx surface is not the declared set", target)
		require.NotEmpty(t, surfaces[target].errTxDone,
			"%s: ErrTxDone carries no message", target)
		require.NotEmpty(t, surfaces[target].beginErrors,
			"%s: Begin refuses nothing", target)
	}

	// The portable refusal is derived from the emissions rather than
	// declared, so a one-character drift on either side empties the
	// intersection and reddens this.
	shared := surfaces[txTargets[0]].beginErrors
	for _, target := range txTargets[1:] {
		shared = intersect(shared, surfaces[target].beginErrors)
	}
	require.Len(t, shared, 1,
		"the backends share no single Begin refusal message: %v",
		map[string][]string{
			txTargets[0]: surfaces[txTargets[0]].beginErrors,
			txTargets[1]: surfaces[txTargets[1]].beginErrors,
		})

	for _, target := range txTargets {
		private := except(surfaces[target].beginErrors, shared)
		want := txPrivateBeginErrors[target]
		require.Equal(t, want, private,
			"%s: backend-private Begin refusals are not the declared set", target)
	}

	first := surfaces[txTargets[0]]
	for _, target := range txTargets[1:] {
		require.Equal(t, first.signatures, surfaces[target].signatures,
			"%s and %s disagree on the Tx signatures a caller writes against",
			txTargets[0], target)
		require.Equal(t, first.errTxDone, surfaces[target].errTxDone,
			"%s and %s disagree on the ErrTxDone message", txTargets[0], target)
	}
}

// TestTxPromotionPreconditions holds the structure that makes the
// promoted surface true, on both backends. The closed set above says
// which exported names exist; none of it says that `tx.GetPerson`
// resolves, and that is a fact about embedding and receivers rather than
// about names.
//
// Each assertion here is a precondition of promotion, and each is
// falsifiable on its own: change the embed, or move a query method's
// receiver, or drop the emitted interface pin, and exactly one of these
// reddens by name.
func TestTxPromotionPreconditions(t *testing.T) {
	for _, target := range txTargets {
		t.Run(target, func(t *testing.T) {
			files := probeFiles(t, target)
			_, dbGo := parseProbeFile(t, target, "db.go", files)

			// Tx must embed the unexported core and nothing else. Embedding
			// `Queries` would promote Begin and WithTx back onto Tx, which
			// is the whole nesting door this shape closes; embedding
			// `*queries` would buy nothing and change the zero value.
			requireEmbedsCore(t, target, "Tx", dbGo)
			requireEmbedsCore(t, target, "Queries", dbGo)

			for _, d := range dbGo.Decls {
				fn, ok := d.(*ast.FuncDecl)
				if !ok || fn.Recv == nil || len(fn.Recv.List) != 1 {
					continue
				}
				recv, ok := receiverIdent(fn.Recv.List[0].Type)
				if !ok || recv != "queries" {
					continue
				}
				require.NotContains(t, txCoreLifecycleNames, fn.Name.Name,
					"%s: %q is declared on the embedded core, so it promotes into *Tx's method set",
					target, fn.Name.Name)
			}

			// Every generated query method hangs off the core. A renderer
			// regressed to *Queries reddens here by name, and the promoted
			// calls in the live adapters stop compiling.
			_, queryFile := parseProbeFile(t, target, "txprobe.cypher.go", files)
			methods := 0
			for _, d := range queryFile.Decls {
				fn, ok := d.(*ast.FuncDecl)
				if !ok || fn.Recv == nil {
					continue
				}
				methods++
				star, isStar := fn.Recv.List[0].Type.(*ast.StarExpr)
				require.True(t, isStar,
					"%s: query method %s has a value receiver", target, fn.Name.Name)
				ident, isIdent := star.X.(*ast.Ident)
				require.True(t, isIdent,
					"%s: query method %s has an unreadable receiver", target, fn.Name.Name)
				require.Equal(t, "queries", ident.Name,
					"%s: query method %s hangs off %q, so it does not promote onto Tx",
					target, fn.Name.Name, ident.Name)
			}
			// Without this the loop above is vacuous on an emission that
			// produced no query methods at all.
			require.Equal(t, len(txProbeInput().Queries), methods,
				"%s: the probe batch's query methods are not all emitted", target)

			// The emitted pins are the generated package's own compile-time
			// claim that both handles satisfy Querier. Asserting them here
			// rather than only in a test file's private copy is what makes a
			// renderer that drops one fail.
			_, querier := parseProbeFile(t, target, "querier.go", files)
			pins := interfacePins(querier)
			require.Contains(t, pins, "Querier = (*Queries)(nil)",
				"%s: querier.go does not pin *Queries to Querier", target)
			require.Contains(t, pins, "Querier = (*Tx)(nil)",
				"%s: querier.go does not pin *Tx to Querier, so nothing emitted claims the promoted surface satisfies the interface", target)
		})
	}
}

// requireEmbedsCore asserts that typeName is a struct whose only embedded
// field is the plain ident `queries`.
func requireEmbedsCore(t *testing.T, target, typeName string, file *ast.File) {
	t.Helper()

	var st *ast.StructType
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok {
			continue
		}
		for _, spec := range gen.Specs {
			ts, ok := spec.(*ast.TypeSpec)
			if !ok || ts.Name.Name != typeName {
				continue
			}
			st, ok = ts.Type.(*ast.StructType)
			require.True(t, ok, "%s: %s is not a struct type", target, typeName)
		}
	}
	require.NotNil(t, st, "%s: no %s type declared", target, typeName)

	var embedded []ast.Expr
	for _, f := range st.Fields.List {
		if len(f.Names) == 0 {
			embedded = append(embedded, f.Type)
		}
	}
	require.Len(t, embedded, 1,
		"%s: %s must embed exactly one type, the core", target, typeName)

	ident, ok := embedded[0].(*ast.Ident)
	require.True(t, ok,
		"%s: %s embeds a %T, not the plain ident `queries` — a pointer embed changes the zero value and buys nothing",
		target, typeName, embedded[0])
	require.Equal(t, "queries", ident.Name,
		"%s: %s embeds %q; embedding the exported handle would promote Begin and WithTx",
		target, typeName, ident.Name)
}

// interfacePins renders every `var _ X = (*Y)(nil)` declaration in a
// file, as the source spells it.
func interfacePins(file *ast.File) []string {
	var out []string
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.VAR {
			continue
		}
		for _, spec := range gen.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok || len(vs.Names) != 1 || vs.Names[0].Name != "_" || vs.Type == nil {
				continue
			}
			if len(vs.Values) != 1 {
				continue
			}
			var b strings.Builder
			if err := printer.Fprint(&b, token.NewFileSet(), vs.Type); err != nil {
				continue
			}
			b.WriteString(" = ")
			var v strings.Builder
			if err := printer.Fprint(&v, token.NewFileSet(), vs.Values[0]); err != nil {
				continue
			}
			out = append(out, b.String()+v.String())
		}
	}
	return out
}

// txSurface is one backend's emitted Tx surface, read out of the AST of
// its db.go. Source bytes are deliberately not consulted: a commented-out
// declaration is source bytes that satisfy a grep and satisfy no caller.
type txSurface struct {
	// names is txSurfaceNames' vocabulary, sorted, as actually emitted.
	names []string
	// signatures maps a name from names to its rendered signature with
	// the receiver dropped, which is what a caller actually writes.
	signatures map[string]string
	// errTxDone is the message inside ErrTxDone's errors.New.
	errTxDone string
	// beginErrors are the errors.New messages Begin's own body can
	// return, sorted.
	beginErrors []string
}

// probeFiles generates the probe batch for one target and returns the
// emitted files by path.
func probeFiles(t *testing.T, target string) map[string][]byte {
	t.Helper()

	registry, err := backends.Registry()
	require.NoError(t, err)
	newGen, ok := registry.Lookup(target)
	require.True(t, ok, "no backend registered for %q", target)

	files, err := newGen("txprobe").Generate(txProbeInput())
	require.NoError(t, err, "%s: generating the probe batch", target)

	out := make(map[string][]byte, len(files))
	for _, f := range files {
		out[f.Path] = f.Contents
	}
	return out
}

// parseProbeFile parses one emitted file by path. An absent path is a
// failure, not an empty parse: every assertion below reads a file that
// must have been emitted, and a silent zero AST would satisfy every walk
// over it.
func parseProbeFile(t *testing.T, target, path string, files map[string][]byte) (*token.FileSet, *ast.File) {
	t.Helper()
	src, ok := files[path]
	require.True(t, ok, "%s: emitted no %s", target, path)

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, src, parser.SkipObjectResolution)
	require.NoError(t, err, "%s: parsing the emitted %s", target, path)
	return fset, file
}

func extractTxSurface(t *testing.T, target string) txSurface {
	t.Helper()

	fset, file := parseProbeFile(t, target, "db.go", probeFiles(t, target))

	out := txSurface{signatures: map[string]string{}}
	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				switch s := spec.(type) {
				case *ast.TypeSpec:
					if s.Name.Name == "Tx" {
						out.names = append(out.names, "type Tx")
					}
				case *ast.ValueSpec:
					for i, name := range s.Names {
						if name.Name != "ErrTxDone" {
							continue
						}
						out.names = append(out.names, "var ErrTxDone")
						require.Less(t, i, len(s.Values),
							"%s: ErrTxDone is declared with no value", target)
						out.errTxDone = errorsNewLiteral(t, s.Values[i])
					}
				}
			}
		case *ast.FuncDecl:
			recv, isTx := txReceiver(d)
			if recv == "" || !d.Name.IsExported() {
				continue
			}
			if !isTx && d.Name.Name != "Begin" {
				// *Queries carries the whole generated query surface;
				// only its Begin belongs to the Tx block.
				continue
			}
			name := "func (*" + recv + ") " + d.Name.Name
			out.names = append(out.names, name)
			out.signatures[name] = renderSignature(t, fset, d)
			if d.Name.Name == "Begin" {
				out.beginErrors = beginRefusals(t, d)
			}
		}
	}
	slices.Sort(out.names)
	slices.Sort(out.beginErrors)
	return out
}

// txReceiver names the receiver type of a method on *Queries, *Tx or the
// embedded core, and reports whether it was *Tx. A non-method or any
// other receiver yields "", which the caller skips.
//
// The core is here so that a lifecycle method moved onto it is VISIBLE to
// the closed set rather than silently dropped: `func (q *queries) Begin`
// promotes into `*Tx` and would otherwise leave the set merely missing
// `func (*Queries) Begin`, which reads as a deletion rather than a move.
func txReceiver(d *ast.FuncDecl) (name string, isTx bool) {
	if d.Recv == nil || len(d.Recv.List) != 1 {
		return "", false
	}
	ident, ok := receiverIdent(d.Recv.List[0].Type)
	if !ok {
		return "", false
	}
	switch ident {
	case "Tx":
		return "Tx", true
	case "Queries", "queries":
		return ident, false
	}
	return "", false
}

// receiverIdent names the type a receiver is written against, through a
// pointer or not. Both forms are read because a value receiver promotes
// into the embedding type's method set exactly as a pointer one does, so
// a gate that saw only `*queries` would miss `queries`.
func receiverIdent(expr ast.Expr) (string, bool) {
	if star, ok := expr.(*ast.StarExpr); ok {
		expr = star.X
	}
	ident, ok := expr.(*ast.Ident)
	if !ok {
		return "", false
	}
	return ident.Name, true
}

// renderSignature prints the declaration a caller writes against: the
// method name and its type, with the receiver dropped. Dropping the
// receiver is what lets the two backends' signatures be compared at all —
// they are identical everywhere else, so no further normalisation is
// needed and none is done (spec §7 step 4).
func renderSignature(t *testing.T, fset *token.FileSet, d *ast.FuncDecl) string {
	t.Helper()
	var b strings.Builder
	require.NoError(t, printer.Fprint(&b, fset, &ast.FuncDecl{
		Name: d.Name,
		Type: d.Type,
	}))
	return strings.TrimPrefix(b.String(), "func ")
}

// beginRefusals collects every errors.New message Begin's own body can
// return, sorted. It walks the body rather than reading the emitter's
// template, so a refusal deleted from one backend is visible here.
func beginRefusals(t *testing.T, d *ast.FuncDecl) []string {
	t.Helper()
	var out []string
	ast.Inspect(d.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || !isErrorsNew(call.Fun) {
			return true
		}
		out = append(out, errorsNewLiteral(t, call))
		return true
	})
	slices.Sort(out)
	return out
}

// errorsNewLiteral reads the message out of an errors.New(...) call. It
// requires the expression to BE that call: anything else is an emission
// this gate has not been taught to read, and reporting it as an empty
// message would be a silent pass.
func errorsNewLiteral(t *testing.T, expr ast.Expr) string {
	t.Helper()
	call, ok := expr.(*ast.CallExpr)
	require.True(t, ok, "expected an errors.New call, got %T", expr)
	require.True(t, isErrorsNew(call.Fun), "expected errors.New, got %#v", call.Fun)
	require.Len(t, call.Args, 1, "errors.New takes one argument")
	lit, ok := call.Args[0].(*ast.BasicLit)
	require.True(t, ok && lit.Kind == token.STRING,
		"errors.New's argument is not a string literal")
	value, err := strconv.Unquote(lit.Value)
	require.NoError(t, err)
	return value
}

func isErrorsNew(fun ast.Expr) bool {
	sel, ok := fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "New" {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	return ok && pkg.Name == "errors"
}

func intersect(a, b []string) []string {
	var out []string
	for _, s := range a {
		if slices.Contains(b, s) {
			out = append(out, s)
		}
	}
	return out
}

func except(all, remove []string) []string {
	out := []string{}
	for _, s := range all {
		if !slices.Contains(remove, s) {
			out = append(out, s)
		}
	}
	return out
}

// txProbeInput is the smallest batch both backends accept: one node type,
// STRING and INTEGER properties only, one :one query and one :exec. Both
// cardinalities are present so that db.go is emitted with its :one
// sentinels and its statement composer, which is the shape every real
// fixture has — the Tx block's own emission is unconditional, and the
// goldens for the nine fixtures that import no `errors` today are what
// witness that.
func txProbeInput() codegen.Input {
	person := resolver.Column{Name: "n", Type: resolver.ResolvedNode{Labels: "Person"}}
	return codegen.Input{
		Schema: schema.Schema{
			Name: "TxProbe",
			Nodes: map[graph.LabelSetKey]schema.NodeType{
				"Person": {KeyLabels: "Person", CompleteLabels: "Person"},
			},
		},
		Queries: []codegen.NamedQuery{
			{
				Name:        "GetPerson",
				Cardinality: codegen.CardinalityOne,
				SourceFile:  "txprobe.cypher",
				SourceText:  "MATCH (n:Person) RETURN n",
				Validated:   resolver.ValidatedQuery{Columns: []resolver.Column{person}},
			},
			{
				Name:        "TouchPerson",
				Cardinality: codegen.CardinalityExec,
				SourceFile:  "txprobe.cypher",
				SourceText:  "MATCH (n:Person) SET n.seen = true",
				Validated:   resolver.ValidatedQuery{},
			},
		},
	}
}
