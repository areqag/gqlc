package cypher_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// TestSinkWriteFence enforces spec §4.3 / §5 Phase E: all writes to the
// six listener collection-state fields must go through the sanctioned
// sink methods. It is the AST-based replacement for the forbidigo rule
// Rev 3 §4.3 originally specified (see Rev 5 for the mechanism swap):
// forbidigo v2 matches identifier chains, not source substrings, so it
// silently drops the "= append(" / "= " context and flags reads too;
// on this package that produced 38 read-site false positives
// (measurement: 45 total hits / 7 writes / 38 reads across the six
// fields). An AST walk over AssignStmt.LHS and IncDecStmt.X sees the
// write-vs-read distinction precisely and lets us assert the invariant
// with zero nolint noise. Failure names the file:line of the offending
// write and the enclosing function.
//
// Scope discipline: exactly the six fields the sinks are named after,
// exactly the seven-method sink whitelist. No generalisation to other
// fields or packages. Add a field to a sink's mutation surface → add
// its name to fencedFields; add a sink → add its name to sinkFuncs.
func TestSinkWriteFence(t *testing.T) {
	// The six §4.3 listener collection-state fields whose writes must
	// go through a sink. Reads are unrestricted (build.go materialisers
	// and l.err != nil guards are legitimate everywhere).
	fencedFields := map[string]struct{}{
		"err":              {},
		"branches":         {},
		"combinators":      {},
		"writeSeen":        {},
		"params":           {},
		"optionalGroupSeq": {},
	}

	// The seven sanctioned sink methods on *listener. Each method is
	// the single write-authority for the field(s) named in its
	// mutation body. addParameterUse (gated) and
	// addParameterUseUnsuppressed (Category-E bypass, spec §1.4) both
	// write l.params — spec-sanctioned pair.
	sinkFuncs := map[string]struct{}{
		"fail":                        {}, // writes err
		"openBranch":                  {}, // writes branches (+ curBranch/curPart, not fenced)
		"recordUnionKind":             {}, // writes combinators
		"markWriteSeen":               {}, // writes writeSeen
		"mintOptionalGroup":           {}, // writes optionalGroupSeq (++)
		"addParameterUse":             {}, // writes params (gated)
		"addParameterUseUnsuppressed": {}, // writes params (Category-E bypass)
	}

	fset := token.NewFileSet()
	pkgDir := "." // this test file lives in internal/query/cypher/
	entries, err := os.ReadDir(pkgDir)
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}

	type violation struct {
		pos   token.Position
		field string
		fn    string
	}
	var violations []violation

	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		src, err := os.ReadFile(filepath.Join(pkgDir, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		file, err := parser.ParseFile(fset, name, src, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}

		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			// Only fence methods with a *listener receiver — the
			// six fields live on that type. Free functions and
			// methods on other types cannot write them.
			if !hasListenerReceiver(fn) {
				continue
			}
			fnName := fn.Name.Name
			_, isSink := sinkFuncs[fnName]

			ast.Inspect(fn.Body, func(n ast.Node) bool {
				switch stmt := n.(type) {
				case *ast.AssignStmt:
					for _, lhs := range stmt.Lhs {
						field, ok := fencedFieldWrite(lhs, fencedFields)
						if !ok {
							continue
						}
						if isSink {
							continue
						}
						violations = append(violations, violation{
							pos: fset.Position(lhs.Pos()), field: field, fn: fnName,
						})
					}
				case *ast.IncDecStmt:
					field, ok := fencedFieldWrite(stmt.X, fencedFields)
					if !ok {
						return true
					}
					if isSink {
						return true
					}
					violations = append(violations, violation{
						pos: fset.Position(stmt.X.Pos()), field: field, fn: fnName,
					})
				}
				return true
			})
		}
	}

	if len(violations) == 0 {
		return
	}
	sort.Slice(violations, func(i, j int) bool {
		if violations[i].pos.Filename != violations[j].pos.Filename {
			return violations[i].pos.Filename < violations[j].pos.Filename
		}
		return violations[i].pos.Line < violations[j].pos.Line
	})
	var msg strings.Builder
	msg.WriteString("sink write-fence violated — direct writes to fenced fields outside sink methods:\n")
	for _, v := range violations {
		msg.WriteString("  ")
		msg.WriteString(v.pos.String())
		msg.WriteString(": l.")
		msg.WriteString(v.field)
		msg.WriteString(" written in ")
		msg.WriteString(v.fn)
		msg.WriteString(" (not a sanctioned sink)\n")
	}
	msg.WriteString("\nsanctioned sinks: fail, openBranch, recordUnionKind, markWriteSeen, mintOptionalGroup, addParameterUse, addParameterUseUnsuppressed")
	t.Fatal(msg.String())
}

// hasListenerReceiver reports whether fn is a method on *listener (the
// unexported package-local struct). Free functions and methods on
// other types (procsig helpers, etc.) cannot reach the six fields, so
// they are excluded from the walk to avoid spurious false-positive
// selector-chain matches on a different receiver named "l".
func hasListenerReceiver(fn *ast.FuncDecl) bool {
	if fn.Recv == nil || len(fn.Recv.List) != 1 {
		return false
	}
	typ := fn.Recv.List[0].Type
	star, ok := typ.(*ast.StarExpr)
	if !ok {
		return false
	}
	ident, ok := star.X.(*ast.Ident)
	if !ok {
		return false
	}
	return ident.Name == "listener"
}

// fencedFieldWrite reports whether expr is a `<receiver>.<fencedField>`
// selector — the LHS of a write against one of the six fields on the
// listener receiver. Since hasListenerReceiver already filtered the
// enclosing method to a *listener receiver, any SelectorExpr on an
// Ident whose Sel matches one of the fenced field names is a write to
// the listener's field. The receiver identifier's name is not checked
// (idiom is `l`, but `receiver` renames don't change semantics — the
// enclosing-func filter is what makes this safe).
func fencedFieldWrite(expr ast.Expr, fencedFields map[string]struct{}) (string, bool) {
	sel, ok := expr.(*ast.SelectorExpr)
	if !ok {
		return "", false
	}
	if _, isIdent := sel.X.(*ast.Ident); !isIdent {
		return "", false
	}
	name := sel.Sel.Name
	if _, ok := fencedFields[name]; !ok {
		return "", false
	}
	return name, true
}
