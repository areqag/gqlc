package neo4j

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"maps"
	"slices"
	"strconv"
	"strings"
	"testing"
	"unicode"

	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	"github.com/areqag/gqlc/internal/codegen"
	"github.com/areqag/gqlc/internal/graph"
	"github.com/areqag/gqlc/internal/schema"
	"github.com/areqag/gqlc/internal/schema/gql"
)

// decoderProbeWidth is one width the probe declares a property at: the
// normalised graph type, and the source spelling that reaches it. The
// spelling is carried because a normalised type is not always a legal
// one — TIME is spelled ZONED TIME here, LOCALTIME is spelled LOCAL TIME,
// and the grammar makes DURATION's qualifier mandatory.
type decoderProbeWidth struct {
	pt       graph.PropertyType
	spelling string
}

// decoderProbeScalars is one entry per scalar graph.PropertyType this
// backend has a Go carrier for. It is held to being exactly that set by
// TestDecoderProbeCoversTheTypeTable, which reads the constants off
// internal/graph's own source: a width added there that this backend
// admits fails that test naming the constant, rather than joining the
// type table with no probe property behind it.
var decoderProbeScalars = []decoderProbeWidth{
	{graph.TypeString, "STRING"},
	{graph.TypeBytes, "BYTES"},
	{graph.TypeBool, "BOOL"},
	{graph.TypeDate, "DATE"},
	{graph.TypeTime, "ZONED TIME"},
	{graph.TypeLocalTime, "LOCAL TIME"},
	{graph.TypeTimestamp, "TIMESTAMP"},
	{graph.TypeDuration, "DURATION(DAY TO SECOND)"},
	{graph.TypeInt, "INT"},
	{graph.TypeInt8, "INT8"},
	{graph.TypeInt16, "INT16"},
	{graph.TypeInt32, "INT32"},
	{graph.TypeInt64, "INT64"},
	{graph.TypeUint, "UINT"},
	{graph.TypeUint8, "UINT8"},
	{graph.TypeUint16, "UINT16"},
	{graph.TypeUint32, "UINT32"},
	{graph.TypeUint64, "UINT64"},
	{graph.TypeFloat, "FLOAT"},
	{graph.TypeFloat32, "FLOAT32"},
	{graph.TypeFloat64, "FLOAT64"},
	{graph.TypeAnyPropertyValue, "ANY VALUE"},
}

// decoderProbeWidths is every width the probe declares: each scalar
// above, that scalar under the list constructor, and that scalar under
// it twice.
//
// The list arms are derived rather than written out, so a scalar added
// to the table above arrives with its slice and its slice-of-slice
// already declared. Two levels is where the derivation stops: the
// constructor nests without bound and the emitted walk recurses with it,
// so a decode arm reachable only at three levels of nesting is outside
// what this covers.
func decoderProbeWidths() []decoderProbeWidth {
	out := make([]decoderProbeWidth, 0, 3*len(decoderProbeScalars))
	for _, w := range decoderProbeScalars {
		list := decoderProbeWidth{pt: graph.ListOf(w.pt, false), spelling: "LIST<" + w.spelling + ">"}
		nested := decoderProbeWidth{pt: graph.ListOf(list.pt, false), spelling: "LIST<" + list.spelling + ">"}
		out = append(out, w, list, nested)
	}
	return out
}

// decoderProbeSchema is one schema spelled around the single property
// name prop, declared at every width decoderProbeWidths carries and in
// every arm a decode helper has: required and nullable, on a node type
// and on an edge type, whose decoder takes the other carrier.
//
// Every entity declares exactly one property, so the positional local
// the non-nullable arm binds is always value0 and the second position is
// unreached — a decoder that named the local after the property only
// from the second field onwards would not be caught here.
func decoderProbeSchema(prop string) string {
	var b strings.Builder
	b.WriteString("CREATE PROPERTY GRAPH TYPE DecoderProbe AS {\n")
	b.WriteString("    (:Endpoint)")
	for _, w := range decoderProbeWidths() {
		tag := decoderProbeTag(w.pt)
		fmt.Fprintf(&b, ",\n    (:Req%s { %s :: %s NOT NULL })", tag, prop, w.spelling)
		fmt.Fprintf(&b, ",\n    (:Opt%s { %s :: %s })", tag, prop, w.spelling)
		fmt.Fprintf(&b, ",\n    (:Endpoint) -[:EdgeReq%s { %s :: %s NOT NULL }]-> (:Endpoint)", tag, prop, w.spelling)
		fmt.Fprintf(&b, ",\n    (:Endpoint) -[:EdgeOpt%s { %s :: %s }]-> (:Endpoint)", tag, prop, w.spelling)
	}
	b.WriteString("\n}")
	return b.String()
}

// decoderProbeTag is the element-name fragment one width contributes,
// derived from the normalised type so two widths get separate graph
// elements: LIST<INT32> becomes ListInt32. Non-alphanumerics are dropped,
// so two types differing only in punctuation would land on one tag; the
// schema parse refuses the duplicate element type and the suite fails on
// the parse rather than measuring one width twice.
func decoderProbeTag(pt graph.PropertyType) string {
	var b strings.Builder
	for _, part := range strings.FieldsFunc(string(pt), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}) {
		b.WriteString(strings.ToUpper(part[:1]))
		b.WriteString(strings.ToLower(part[1:]))
	}
	return b.String()
}

// graphPropertyTypeSource is where internal/graph declares the
// normalised property types. The obligation below is read off that
// declaration rather than restated here, so a width added upstream joins
// the obligation without an edit in this file.
const graphPropertyTypeSource = "../../graph/propertytype.go"

// typeTableSource is where this backend declares which of those types it
// emits a carrier for.
const typeTableSource = "types.go"

// TestDecoderProbeCoversTheTypeTable holds the probe to declaring a
// property at every width this backend emits a carrier for. A width the
// type table admits and no probe entity declares is a decode arm the
// scope sweep below never emits, so a local that arm binds never enters
// the candidate set and the sweep's silence about it means nothing.
//
// The coverage obligations are membership in both directions, because an
// obligation satisfied by swapping one width for another is not the
// obligation. The one count here is the nested-list census at the end,
// which is a floor on a shape rather than a size pin on a set.
func TestDecoderProbeCoversTheTypeTable(t *testing.T) {
	declared := graphPropertyTypes(t)
	arms := propertyArmNames(t)

	// The scalar table is held to naming real constants first, before its
	// entries are relied on below. A mistyped entry would otherwise widen
	// the probe with a spelling nothing upstream declares while leaving
	// the constant it was meant to stand for uncovered; a list type would
	// arrive with the derivation already applied to it and nest a level
	// deeper than the table it is read from claims.
	//
	// This is also what stops either walk passing vacuously: every entry
	// of the scalar table has to come back out of both of them.
	for _, w := range decoderProbeScalars {
		name, known := declared[w.pt]
		require.True(t, known, "decoderProbeScalars names %s, which %s declares no constant for", w.pt, graphPropertyTypeSource)
		require.Equal(t, graph.KindScalar, w.pt.Kind(),
			"decoderProbeScalars names graph.%s, which is a list type: the list arms are derived, not listed", name)
		require.Contains(t, arms, name,
			"decoderProbeScalars names graph.%s, which Property has no arm for", name)
	}

	// Every width Property names is one graphPropertyTypes found. The two
	// walks read different files and neither confirms the other's form, so
	// a constant written in a shape the value walk skips is invisible to
	// it — unless this backend also grew an arm for that constant, which
	// is the only case where the probe owes it a property.
	names := make(map[string]bool, len(declared))
	for _, name := range declared {
		names[name] = true
	}
	for name := range arms {
		require.True(t, names[name],
			"%s has an arm for graph.%s and %s yielded no constant of that name, so the coverage "+
				"obligation below cannot see it", typeTableSource, name, graphPropertyTypeSource)
	}

	covered := make(map[graph.PropertyType]bool)
	for _, w := range decoderProbeWidths() {
		_, ok := typeMap{}.Property(w.pt)
		require.True(t, ok, "the probe declares %s, which this backend has no carrier for", w.pt)
		require.False(t, covered[w.pt], "the probe declares %s twice", w.pt)
		covered[w.pt] = true
	}

	for pt, name := range declared {
		if _, ok := (typeMap{}).Property(pt); !ok {
			continue
		}
		require.True(t, covered[pt],
			"this backend emits a carrier for graph.%s (%s) and no probe entity declares it, so the decode "+
				"arms that width reaches are unswept: add it to decoderProbeScalars with the spelling a "+
				"schema writes it as", name, pt)
	}

	// The recursive arm of the emitted walk is reached only by a width
	// whose element is itself a list. Counted off the widths rather than
	// off the emission because the scope sweep compares an emission
	// against its own reference: dropping the arm takes the locals out of
	// both sides at once and neither side notices.
	nested := 0
	for pt := range covered {
		if pt.Kind() == graph.KindList && pt.Elem().Kind() == graph.KindList {
			nested++
		}
	}
	require.Positive(t, nested,
		"no probe width nests the list constructor, so the walk's recursive arm is unreached")
}

// graphPropertyTypes reads internal/graph's normalised property types
// off the source that declares them, mapping each to its constant name.
//
// The form it models is a spec that spells PropertyType and carries its
// own string literal. A const block holding one of those and also a spec
// written some other way fails here naming that spec, rather than
// dropping it in silence: a spec that inherits its predecessor's value,
// and one that is untyped, both leave the type off and both are usable
// where a PropertyType is wanted.
func graphPropertyTypes(t *testing.T) map[graph.PropertyType]string {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), graphPropertyTypeSource, nil, parser.SkipObjectResolution)
	require.NoError(t, err, "%s does not parse", graphPropertyTypeSource)

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
				require.True(t, isLit, "%s: constant %s is not a literal", graphPropertyTypeSource, name.Name)
				value, unquoteErr := strconv.Unquote(lit.Value)
				require.NoError(t, unquoteErr, "%s: constant %s", graphPropertyTypeSource, name.Name)
				out[graph.PropertyType(value)] = name.Name
				read++
			}
		}
		require.True(t, read == 0 || len(skipped) == 0,
			"%s: a const block declaring PropertyType constants also declares %v, which this walk "+
				"cannot read a PropertyType value off", graphPropertyTypeSource, skipped)
	}
	return out
}

// propertyArmNames names every graph constant typeMap.Property switches
// on, whether or not its arm answers with a carrier. Read off the source
// because the compiled function does not enumerate its arms: it answers
// about a candidate handed to it, and which candidates exist is the
// question being asked.
func propertyArmNames(t *testing.T) map[string]bool {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), typeTableSource, nil, parser.SkipObjectResolution)
	require.NoError(t, err, "%s does not parse", typeTableSource)

	out := make(map[string]bool)
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "Property" || fn.Recv == nil {
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
	return out
}

// unclaimedProperty is a property name no emitted decoder holds, so an
// emission spelled with it is the reference the probes are measured
// against.
const unclaimedProperty = "alpha"

// DecoderSuite pins what an emitted decode<Name> may name. The fixture-
// driven golden corpus lives in internal/codegen/conformance.
type DecoderSuite struct {
	suite.Suite
}

func TestDecoderSuite(t *testing.T) {
	suite.Run(t, new(DecoderSuite))
}

// TestProbeDeclaresEveryWidthInEveryArm holds the probe's spellings to
// reaching what they claim. TestDecoderProbeCoversTheTypeTable compares
// graph types and says nothing about whether `ZONED TIME` normalises to
// TypeTime — a spelling that landed on some other width would leave that
// comparison green while the arm it was added for went unemitted. This
// reads the widths back off the parsed schema, at each of the four
// element declarations decoderProbeSchema writes per width, and then
// reads the Go types back off the emission: a width the schema declares
// and codegen drops reaches no decoder either.
func (s *DecoderSuite) TestProbeDeclaresEveryWidthInEveryArm() {
	sch, err := gql.New().Parse(strings.NewReader(decoderProbeSchema(unclaimedProperty)))
	s.Require().NoError(err)

	// Keyed on the key label set, which is what identifies an element
	// type; Name is the emission's business and the parse leaves it empty.
	nodes := make(map[string]schema.Property, len(sch.Nodes))
	for _, n := range sch.Nodes {
		if p, ok := n.Properties[unclaimedProperty]; ok {
			nodes[string(n.KeyLabels)] = p
		}
	}
	edges := make(map[string]schema.Property, len(sch.Edges))
	for _, e := range sch.Edges {
		if p, ok := e.Properties[unclaimedProperty]; ok {
			edges[string(e.KeyLabels)] = p
		}
	}

	want := make(map[string]bool)
	for _, w := range decoderProbeWidths() {
		goType, ok := typeMap{}.Property(w.pt)
		s.Require().True(ok, "the probe declares %s, which this backend has no carrier for", w.pt)
		want[goType] = true

		tag := decoderProbeTag(w.pt)
		s.requireArm(nodes, "Req"+tag, w.pt, false)
		s.requireArm(nodes, "Opt"+tag, w.pt, true)
		s.requireArm(edges, "EdgeReq"+tag, w.pt, false)
		s.requireArm(edges, "EdgeOpt"+tag, w.pt, true)
	}

	models, err := s.emitModels(unclaimedProperty)
	s.Require().NoError(err)
	s.Require().Equal(slices.Sorted(maps.Keys(want)), s.structFieldTypesOf(models),
		"the emitted structs do not carry exactly the Go types the probe's widths claim")
}

// requireArm holds one element the probe declares to carrying the width
// and the nullability its name says it does.
func (s *DecoderSuite) requireArm(declared map[string]schema.Property, element string, pt graph.PropertyType, nullable bool) {
	s.T().Helper()
	got, ok := declared[element]
	s.Require().True(ok, "the probe declares no element %s carrying a property named %s", element, unclaimedProperty)
	s.Require().Equal(pt, got.Type, "element %s carries a width the probe did not spell", element)
	s.Require().Equal(nullable, got.Nullable, "element %s carries the wrong nullability", element)
}

// TestNoDecoderLocalTakesAPropertyName pins the decoder's scope against
// the one thing in it a schema author chooses. A property name reaches
// the emission as a Props key and a struct field; a local named after it
// as well lands in the same scope as the accumulator, the error and the
// carrier the helper was handed, and `err :: STRING NOT NULL` emits
// `err, err :=`. Generation exits 0 over that, because the format gate
// parses the emission and does not type-check it.
//
// The names are read off an emission rather than listed here, so a local
// a decoder gains later is held by this without anyone remembering to
// add it — as far as the probe's own widths reach, which is one property
// per width the type table admits and the two list arms over each of
// them. Each name is then fed back as a property name, and what must not
// move is the set itself: the decoder's identifiers are the generator's
// own, so they are the same whatever the schema declares.
//
// The scope read here is models.go's entity decoders. A query column's
// decode is emitted elsewhere and its locals are swept by the query-side
// corpus in internal/codegen/conformance, not by this.
func (s *DecoderSuite) TestNoDecoderLocalTakesAPropertyName() {
	models, err := s.emitModels(unclaimedProperty)
	s.Require().NoError(err)
	declared := s.decoderScopeOf(models)
	s.Require().NotEmpty(declared, "the emitted decoders bind no identifiers to check")

	for _, name := range declared {
		s.Run(name, func() {
			models, err := s.emitModels(name)
			if err != nil {
				// The parse refusing the name is the schema's answer, not
				// a gap: a name no property can be spelled is a name no
				// decoder can collide with. No identifier the generator
				// currently binds trips this — a decoder that later took
				// its name from the GQL keyword list would.
				s.T().Skipf("no property can be named %q: %v", name, err)
			}
			s.Require().Contains(models, "\t"+exportedField(name)+" ",
				"the struct no longer carries the property under the name the schema gave it")
			s.Require().Equal(declared, s.decoderScopeOf(models),
				"a property name reached the decoder's scope")
		})
	}
}

// emitModels emits models.go for a schema whose every entity declares
// one property named prop. The error is the schema parse's alone: a
// generation that failed would be this suite's business, and a parse
// that failed is the caller's.
func (s *DecoderSuite) emitModels(prop string) (string, error) {
	sch, err := gql.New().Parse(strings.NewReader(decoderProbeSchema(prop)))
	if err != nil {
		return "", err
	}
	files, err := New().Generate(codegen.Input{Schema: sch})
	s.Require().NoError(err)
	for _, f := range files {
		if f.Path == "models.go" {
			return string(f.Contents), nil
		}
	}
	s.Require().Fail("no models.go in the emission")
	return "", nil
}

// decoderScopeOf names every identifier the emitted decode helpers bind,
// deduplicated and ordered. The carrier argument counts: a parameter
// shares the body's outermost scope, so a local of that name is not a
// declaration but an assignment over the value the helper was handed.
func (s *DecoderSuite) decoderScopeOf(models string) []string {
	seen := make(map[string]bool)
	for _, decl := range s.parseModels(models).Decls {
		fn, ok := decl.(*ast.FuncDecl)
		// The methods models.go declares are edge-union markers, which
		// take no argument and hold no body; the decode helpers are what
		// is left.
		if !ok || fn.Recv != nil || fn.Body == nil {
			continue
		}
		for _, param := range fn.Type.Params.List {
			for _, id := range param.Names {
				seen[id.Name] = true
			}
		}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			for _, id := range declaredIdents(n) {
				if id.Name != "_" {
					seen[id.Name] = true
				}
			}
			return true
		})
	}
	return slices.Sorted(maps.Keys(seen))
}

// structFieldTypesOf names the Go types the emitted structs carry,
// deduplicated and ordered, with the nullable arm's pointer dropped so a
// width contributes the same type from either arm.
func (s *DecoderSuite) structFieldTypesOf(models string) []string {
	seen := make(map[string]bool)
	ast.Inspect(s.parseModels(models), func(n ast.Node) bool {
		st, ok := n.(*ast.StructType)
		if !ok {
			return true
		}
		for _, field := range st.Fields.List {
			expr := field.Type
			if star, isStar := expr.(*ast.StarExpr); isStar {
				expr = star.X
			}
			seen[types.ExprString(expr)] = true
		}
		return true
	})
	return slices.Sorted(maps.Keys(seen))
}

// parseModels is the emitted file as a syntax tree.
func (s *DecoderSuite) parseModels(models string) *ast.File {
	f, err := parser.ParseFile(token.NewFileSet(), "models.go", models, parser.SkipObjectResolution)
	s.Require().NoError(err, "the emitted file does not parse")
	return f
}

// declaredIdents returns the identifiers a node binds. Short variable
// declarations, var declarations and range clauses are the whole of what
// an emitted body uses to introduce a name.
func declaredIdents(n ast.Node) []*ast.Ident {
	var out []*ast.Ident
	switch stmt := n.(type) {
	case *ast.AssignStmt:
		if stmt.Tok != token.DEFINE {
			return nil
		}
		for _, lhs := range stmt.Lhs {
			if id, ok := lhs.(*ast.Ident); ok {
				out = append(out, id)
			}
		}
	case *ast.ValueSpec:
		out = append(out, stmt.Names...)
	case *ast.RangeStmt:
		if stmt.Tok != token.DEFINE {
			return nil
		}
		for _, e := range []ast.Expr{stmt.Key, stmt.Value} {
			if id, ok := e.(*ast.Ident); ok {
				out = append(out, id)
			}
		}
	}
	return out
}

// exportedField is the struct field a property named name lands on. The
// names this suite probes are ASCII and carry no underscore, which is
// the whole of what the §4.2 mangle does to them.
func exportedField(name string) string {
	return strings.ToUpper(name[:1]) + name[1:]
}
