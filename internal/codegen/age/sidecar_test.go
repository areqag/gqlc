package age

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/areqag/gqlc/internal/codegen"
	"github.com/areqag/gqlc/internal/graph"
)

// TestTheDecodeAndTheGateReadTheSameSidecarKeys holds the emitted decode
// and rejectOffsetSidecarCollisions to one answer, over every width
// internal/graph declares.
//
// The gate exists because the sidecar name is derived and can collide
// with a property the author owns. It is only worth having while it
// covers every field whose decode reads a sidecar: a width whose decode
// gained a sidecar the gate did not would generate a decoder that reads
// one key twice and re-zones the value by the author's own property,
// which is the defect the gate was built to refuse — and it would do so
// with the gate still compiling and still passing its own rows.
//
// The comparison runs per width: the emitted decode is read for the key
// it names, and the gate is run over a schema declaring that key and
// over one declaring a different key. Neither side is read off
// offsetSidecar, so a decode taught to zone a width by any other route
// is a disagreement here.
//
// The domain includes the widths this backend refuses today, which is
// where the forward coverage comes from: TIME is in it now, carries no
// Go type now, and joins the comparison on the day the table gives it
// one.
func TestTheDecodeAndTheGateReadTheSameSidecarKeys(t *testing.T) {
	widths := declaredPropertyTypes(t)

	// The widths the type table gives a carrier, collected before the
	// sweep so the sweep answers to a list it did not build. A width
	// skipped inside the loop is missing from compared and named at the
	// match below; a count of the rows run would report the sweep was
	// non-empty and nothing more.
	var carried []graph.PropertyType
	for _, pt := range widths {
		if _, ok := (typeMap{}).Property(pt); ok {
			carried = append(carried, pt)
		}
	}
	require.NotEmpty(t, carried, "no width in the domain has a carrier, so nothing was compared")

	compared, zoned := make([]graph.PropertyType, 0, len(carried)), 0
	for _, pt := range widths {
		goType, ok := typeMap{}.Property(pt)
		if !ok {
			// No carrier, so no entity field is ever built for this width
			// and neither the decode nor the gate can see one.
			continue
		}
		compared = append(compared, pt)
		for _, nullable := range []bool{false, true} {
			f := codegen.EntityField{PropName: "at", Field: "At", GoType: goType, Nullable: nullable}

			key, reads := decodedSidecarKey(t, f)
			if reads {
				zoned++
				require.Error(t, rejectOffsetSidecarCollisions([]codegen.Entity{entityDeclaring(f, key)}),
					"%s (nullable=%t): the decode reads property %q, and the gate serves a schema whose author declares it",
					pt, nullable, key)
			}

			// Every other name is one the author keeps: a gate refusing
			// it would fail a schema this backend serves. atOffset is in
			// the list because it is the name the derivation produces, so
			// a width whose decode reads nothing is checked against the
			// name it would have read.
			for _, prop := range []string{"atOffset", "atElsewhere"} {
				if reads && prop == key {
					continue
				}
				require.NoError(t, rejectOffsetSidecarCollisions([]codegen.Entity{entityDeclaring(f, prop)}),
					"%s (nullable=%t): the gate refused property %q, which this width's decode does not read",
					pt, nullable, prop)
			}

			// The helper the decode names has to be one the emission
			// declares, and h.zone is what declares agtypeZone.
			var h helpers
			h.forEntities([]wiredEntity{{Entity: entityDeclaring(f, "")}})
			require.Equal(t, reads, h.zone,
				"%s (nullable=%t): the decode reads a sidecar=%t and the emission marks agtypeZone for declaration=%t",
				pt, nullable, reads, h.zone)
		}
	}
	require.ElementsMatch(t, carried, compared,
		"the sweep did not run over every width the type table gives a carrier")
	require.NotZero(t, zoned, "no width in the domain reads a sidecar, so the agreement held vacuously")
}

// decodedSidecarKey is the property key one field's emitted decode reads
// its offset from, with ok=false for a decode that reads none. Read back
// out of the emitted text rather than from offsetSidecar, so what it
// reports is what the generated package will do.
func decodedSidecarKey(t *testing.T, f codegen.EntityField) (string, bool) {
	t.Helper()

	var b strings.Builder
	writeEntityFieldDecode(&b, codegen.Entity{Name: "E", Kind: codegen.EntityNode, Fields: []codegen.EntityField{f}}, 0, f)

	const call = "agtypeZone(props, "
	rest := b.String()
	found, reads := "", false
	for {
		_, after, ok := strings.Cut(rest, call)
		if !ok {
			return found, reads
		}
		end := strings.Index(after, ",")
		require.NotEqual(t, -1, end, "the emitted call to agtypeZone has no second argument:\n%s", b.String())
		key, err := strconv.Unquote(after[:end])
		require.NoError(t, err, "the key the emitted decode reads is not a Go string literal:\n%s", b.String())
		if reads {
			require.Equal(t, found, key, "one field's decode reads two different offset keys:\n%s", b.String())
		}
		found, reads, rest = key, true, after[end:]
	}
}

// entityDeclaring is an entity carrying f and a second INT64 property of
// the given name — the shape of a schema whose author declared the name
// the sidecar read derives. An empty name yields the field alone.
func entityDeclaring(f codegen.EntityField, prop string) codegen.Entity {
	e := codegen.Entity{Name: "E", Kind: codegen.EntityNode, Fields: []codegen.EntityField{f}}
	if prop != "" {
		e.Fields = append(e.Fields, codegen.EntityField{PropName: prop, Field: "Second", GoType: "int64"})
	}
	return e
}

// declaresAWidth reports whether a spec carrying no PropertyType
// annotation is one a width would be declared as.
//
// Go does not carry a type across a spec that has its own value, so
// `TypeFoo = "FOO"` is an untyped string constant and
// `TypeFoo = PropertyType("FOO")` a typed one written without the
// annotation. Both serve as a graph.PropertyType at every call site that
// takes one, which is what makes them widths everywhere except in a walk
// keyed on the annotation.
//
// The marks read are a spec sharing a block with the annotated widths
// and a name prefixed as they are; the values recognised are a string
// literal and a conversion whose function is the identifier
// PropertyType. A declaration bearing neither mark, or building its text
// some other way, is not seen here.
func declaresAWidth(gen *ast.GenDecl, value *ast.ValueSpec) bool {
	if len(value.Values) == 0 {
		return false
	}
	for _, v := range value.Values {
		if call, ok := v.(*ast.CallExpr); ok {
			if id, ok := call.Fun.(*ast.Ident); !ok || id.Name != "PropertyType" {
				return false
			}
			continue
		}
		if lit, ok := v.(*ast.BasicLit); !ok || lit.Kind != token.STRING {
			return false
		}
	}
	for _, n := range value.Names {
		if strings.HasPrefix(n.Name, "Type") {
			return true
		}
	}
	for _, s := range gen.Specs {
		other, ok := s.(*ast.ValueSpec)
		if !ok {
			continue
		}
		if id, ok := other.Type.(*ast.Ident); ok && id.Name == "PropertyType" {
			return true
		}
	}
	return false
}

// declaredPropertyTypes is every width internal/graph declares under its
// PropertyType annotation, derived from the declaration itself, so an
// annotated width joins the domain on the day the vocabulary gains it
// and no edit is needed here.
//
// const and var blocks are both read: the annotation is legal on either
// and it is the mark this walk keys on. A spec carrying no annotation is
// put to declaresAWidth, which fails the walk at the shapes named there
// instead of letting the domain go quietly short a width.
//
// Every .go file in the package is read, so moving the constants between
// files does not shrink the domain, and a walk that reached none of them
// fails the anchors below rather than reporting agreement over nothing.
func declaredPropertyTypes(t *testing.T) []graph.PropertyType {
	t.Helper()

	const dir = "../../graph"
	entries, err := os.ReadDir(dir)
	require.NoError(t, err, "internal/graph is not where this sweep reads the width vocabulary from")

	var out []graph.PropertyType
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), filepath.Join(dir, name), nil, parser.SkipObjectResolution)
		require.NoError(t, err, "%s does not parse", name)

		for _, decl := range file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || (gen.Tok != token.CONST && gen.Tok != token.VAR) {
				continue
			}
			for _, spec := range gen.Specs {
				value, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				if id, ok := value.Type.(*ast.Ident); !ok || id.Name != "PropertyType" {
					require.False(t, declaresAWidth(gen, value),
						"%s declares %v without the PropertyType annotation this walk reads: the domain would be short a width with the sweep still reporting agreement",
						name, value.Names)
					continue
				}
				require.NotEmpty(t, value.Values, "%s declares %v as a PropertyType with no value this walk can read", name, value.Names)
				for _, v := range value.Values {
					lit, ok := v.(*ast.BasicLit)
					require.True(t, ok && lit.Kind == token.STRING,
						"%s declares %v as a PropertyType whose value is not a string literal, so the domain is short one width", name, value.Names)
					text, err := strconv.Unquote(lit.Value)
					require.NoError(t, err)
					out = append(out, graph.PropertyType(text))
				}
			}
		}
	}

	// Anchors spanning the declaration: the width that reads a sidecar
	// today, the one the encoding table hands the same sidecar against a
	// carrier arriving, a scalar with no zone at all, and the last
	// constant declared. A walk that matched nothing, or stopped part way
	// through, misses one of them.
	for _, anchor := range []graph.PropertyType{graph.TypeTimestamp, graph.TypeTime, graph.TypeString, graph.TypeList} {
		require.Contains(t, out, anchor, "the width vocabulary read from %s does not hold %s", dir, anchor)
	}
	return out
}
