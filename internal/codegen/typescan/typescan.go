// Package typescan reads two declarations as syntax trees: the property
// types internal/graph declares, and the ones a backend's type table has
// a switch arm for.
//
// Neither can be read off the compiled program. internal/graph's
// constants are values of an open string type, so nothing enumerates
// them at run time; and typeMap.Property answers about a candidate handed
// to it, while the question here is which candidates it decides about at
// all. Both answers live in the source or nowhere.
//
// What the pair buys is a bidirectional obligation a backend's test can
// state: every arm owes a row in a table that says what it answers, and
// every row owes an arm, so a row cannot quietly measure the fallthrough
// instead of a decision. It fired on gqlc-h9n.33 the moment an arm was
// added, and named the repair.
//
// It is a package rather than a helper in one backend's test files
// because it was one, in internal/codegen/neo4j, and internal/codegen/age
// had the same two tables and no walk at all — so an AGE arm could answer
// anything and no test named it (bd gqlc-ozdkx). What a Go const block is
// and what a switch arm is do not vary by backend, which is the same
// reason internal/codegen/emitscan exists.
//
// Every error names the file it was read from. The caller passes that
// path, so the message stays true when a backend's table moves and there
// is no second copy of the location to keep in step.
package typescan

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"

	"github.com/areqag/gqlc/internal/graph"
)

// PropertyTypes reads the normalised property types off the source that
// declares them, mapping each to its Go constant name.
//
// The form it models is a spec that spells PropertyType and carries its
// own string literal. A const block holding one of those and also a spec
// written some other way is an error naming that spec, rather than a
// silent drop: a spec that inherits its predecessor's value, and one that
// is untyped, both leave the type off and both are usable where a
// PropertyType is wanted. Dropping either would narrow the obligation
// below without saying so.
func PropertyTypes(source string) (map[graph.PropertyType]string, error) {
	file, err := parser.ParseFile(token.NewFileSet(), source, nil, parser.SkipObjectResolution)
	if err != nil {
		return nil, fmt.Errorf("%s does not parse: %w", source, err)
	}

	out := make(map[graph.PropertyType]string)
	for _, decl := range file.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.CONST {
			continue
		}
		read, skipped := 0, []string(nil)
		for _, spec := range gd.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			id, isIdent := vs.Type.(*ast.Ident)
			if !isIdent || id.Name != "PropertyType" || len(vs.Values) != len(vs.Names) {
				for _, name := range vs.Names {
					skipped = append(skipped, name.Name)
				}
				continue
			}
			for i, name := range vs.Names {
				lit, isLit := vs.Values[i].(*ast.BasicLit)
				if !isLit {
					return nil, fmt.Errorf("%s: constant %s is not a literal", source, name.Name)
				}
				value, unquoteErr := strconv.Unquote(lit.Value)
				if unquoteErr != nil {
					return nil, fmt.Errorf("%s: constant %s: %w", source, name.Name, unquoteErr)
				}
				out[graph.PropertyType(value)] = name.Name
				read++
			}
		}
		if read != 0 && len(skipped) != 0 {
			return nil, fmt.Errorf(
				"%s: a const block declaring PropertyType constants also declares %v, which this walk "+
					"cannot read a PropertyType value off", source, skipped)
		}
	}
	return out, nil
}

// PropertyArms names every graph constant the named method switches on in
// the given source, whether or not its arm answers with a carrier: an arm
// returning ("", false) is as much a decision as one returning a carrier,
// and is exactly the kind that goes unexamined.
//
// An empty result is returned without complaint. Whether a backend owing
// arms has none is the caller's question, and only the caller knows
// whether the obligation it is about to state would be vacuous.
func PropertyArms(source, method string) (map[string]bool, error) {
	file, err := parser.ParseFile(token.NewFileSet(), source, nil, parser.SkipObjectResolution)
	if err != nil {
		return nil, fmt.Errorf("%s does not parse: %w", source, err)
	}

	out := make(map[string]bool)
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != method || fn.Recv == nil {
			continue
		}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			clause, isClause := n.(*ast.CaseClause)
			if !isClause {
				return true
			}
			for _, expr := range clause.List {
				sel, isSel := expr.(*ast.SelectorExpr)
				if !isSel {
					continue
				}
				if pkg, isIdent := sel.X.(*ast.Ident); isIdent && pkg.Name == "graph" {
					out[sel.Sel.Name] = true
				}
			}
			return true
		})
	}
	return out, nil
}
