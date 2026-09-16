// Package typescan reads two declarations as syntax trees: the property
// types internal/graph declares, and the ones a backend's type table has
// a row for.
//
// Neither can be read off the compiled program. internal/graph's
// constants are values of an open string type, so nothing enumerates
// them at run time; and typeMap.Property answers about a candidate handed
// to it, while the question here is which candidates it decides about at
// all. Both answers live in the source or nowhere.
//
// What the pair buys is a bidirectional obligation a backend's test can
// state: every table row owes a row in a test table that says what it
// answers, and every test row owes a table row, so a test row cannot
// quietly measure the fallthrough instead of a decision. It fired on
// gqlc-h9n.33 the moment an arm was added, and named the repair.
//
// It is a package rather than a helper in one backend's test files
// because it was one, in internal/codegen/neo4j, and internal/codegen/age
// had the same two tables and no walk at all — so an AGE arm could answer
// anything and no test named it (bd gqlc-ozdkx). What a Go const block is
// and what a map literal's keys are do not vary by backend, which is the
// same reason internal/codegen/emitscan exists.
//
// The table was a switch until bd gqlc-ek1w, when it became a map literal
// keyed by the graph constants so that Property could pass the gocyclo
// gate at 10; PropertyRows reads the keys the way PropertyArms read the
// case expressions, and the obligation is the same one.
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
		if err := propertyTypeConsts(source, gd, out); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// propertyTypeConsts reads one const block's PropertyType constants into
// out.
func propertyTypeConsts(source string, gd *ast.GenDecl, out map[graph.PropertyType]string) error {
	read, skipped := 0, []string(nil)
	for _, spec := range gd.Specs {
		vs, ok := spec.(*ast.ValueSpec)
		if !ok {
			continue
		}
		if !propertyTypeSpec(vs) {
			for _, name := range vs.Names {
				skipped = append(skipped, name.Name)
			}
			continue
		}
		if err := readPropertyTypeSpec(source, vs, out); err != nil {
			return err
		}
		read += len(vs.Names)
	}
	if read != 0 && len(skipped) != 0 {
		return fmt.Errorf(
			"%s: a const block declaring PropertyType constants also declares %v, which this walk "+
				"cannot read a PropertyType value off", source, skipped)
	}
	return nil
}

// propertyTypeSpec reports whether vs is the form PropertyTypes models:
// spelled PropertyType, with a value per name.
func propertyTypeSpec(vs *ast.ValueSpec) bool {
	id, isIdent := vs.Type.(*ast.Ident)
	return isIdent && id.Name == "PropertyType" && len(vs.Values) == len(vs.Names)
}

// readPropertyTypeSpec reads one spec's string literals into out.
func readPropertyTypeSpec(source string, vs *ast.ValueSpec, out map[graph.PropertyType]string) error {
	for i, name := range vs.Names {
		lit, isLit := vs.Values[i].(*ast.BasicLit)
		if !isLit {
			return fmt.Errorf("%s: constant %s is not a literal", source, name.Name)
		}
		value, unquoteErr := strconv.Unquote(lit.Value)
		if unquoteErr != nil {
			return fmt.Errorf("%s: constant %s: %w", source, name.Name, unquoteErr)
		}
		out[graph.PropertyType(value)] = name.Name
	}
	return nil
}

// PropertyRows names every graph constant the named package-level map
// literal has a row for in the given source, whether or not its row answers
// with a carrier: a row holding "" is as much a decision as one holding a
// carrier, and is exactly the kind that goes unexamined.
//
// An empty result is returned without complaint. Whether a backend owing
// rows has none is the caller's question, and only the caller knows
// whether the obligation it is about to state would be vacuous. A var of
// that name whose value is not a composite literal reads the same as no
// var at all, for the same reason: it is the caller's NotEmpty that says
// the walk read nothing.
func PropertyRows(source, table string) (map[string]bool, error) {
	file, err := parser.ParseFile(token.NewFileSet(), source, nil, parser.SkipObjectResolution)
	if err != nil {
		return nil, fmt.Errorf("%s does not parse: %w", source, err)
	}

	out := make(map[string]bool)
	for _, decl := range file.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.VAR {
			continue
		}
		for _, spec := range gd.Specs {
			tableRowConsts(spec, table, out)
		}
	}
	return out, nil
}

// tableRowConsts records into out every graph-qualified selector used as a
// key of the composite literal a var spec of the given name is initialised
// with.
func tableRowConsts(spec ast.Spec, table string, out map[string]bool) {
	vs, ok := spec.(*ast.ValueSpec)
	if !ok || len(vs.Names) != 1 || vs.Names[0].Name != table || len(vs.Values) != 1 {
		return
	}
	lit, ok := vs.Values[0].(*ast.CompositeLit)
	if !ok {
		return
	}
	for _, elt := range lit.Elts {
		if kv, isKV := elt.(*ast.KeyValueExpr); isKV {
			recordGraphConst(kv.Key, out)
		}
	}
}

// recordGraphConst records into out the name a graph-qualified selector
// expression names, and nothing for any other expression.
func recordGraphConst(expr ast.Expr, out map[string]bool) {
	sel, isSel := expr.(*ast.SelectorExpr)
	if !isSel {
		return
	}
	if pkg, isIdent := sel.X.(*ast.Ident); isIdent && pkg.Name == "graph" {
		out[sel.Sel.Name] = true
	}
}
