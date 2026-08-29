package neo4j_test

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
	"github.com/areqag/gqlc/internal/codegen/neo4j"
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
// already declared. Two levels is where the derivation stops, and it is
// the first depth that reaches writeSliceNarrow's recursive arm at all.
//
// No depth finishes the job on its own: that walk names its locals off
// the recursion depth, so each further level of nesting introduces
// identifiers no shallower probe has seen, and extending this to three
// would only move the boundary (bd gqlc-wdo7). What closes it is a
// different measurement, not a deeper one —
// TestDecoderLocalFamiliesAreClosedUnderDepth holds the *families* the
// emitter draws from rather than the set one probe happens to observe.
// This width table is what the per-width sweeps below read; the closure
// test builds its own narrow schema.
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
// No entity declares more than one property, so the positional local
// the non-nullable arm binds is always value0 and the second position is
// unreached — a decoder that named the local after the property only
// from the second field onwards would not be caught here. The Endpoint
// node the edge arms attach to declares none.
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
// property at every width graphPropertyTypeSource declares a constant
// for and this backend emits a carrier for. A width the type table
// admits and no probe entity declares is a decode arm the scope sweep
// below never emits, so a local that arm binds never enters the
// candidate set and the sweep's silence about it means nothing.
//
// Lists nested deeper than two fall inside that silence rather than
// outside it. Property recurses on the element, so depth does not bound
// the carrier — a list is carried when its leaf scalar is — while
// decoderProbeWidths stops at two. A deeper width is one the type table
// admits and no probe entity declares, and this test passes over it
// (gqlc-wdo7).
//
// The obligation runs in both directions: every width the probe
// declares has a carrier, and every carried constant is one the probe
// declares.
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
		_, ok := neo4j.TypeMap{}.Property(w.pt)
		require.True(t, ok, "the probe declares %s, which this backend has no carrier for", w.pt)
		require.False(t, covered[w.pt], "the probe declares %s twice", w.pt)
		covered[w.pt] = true
	}

	for pt, name := range declared {
		if _, ok := (neo4j.TypeMap{}).Property(pt); !ok {
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
	// both sides at once and neither side notices. Each entity's struct,
	// its fields and its decode helper are held against the parsed schema
	// by TestEveryElementTypeGetsItsOwnDecodeArm; the locals a decode arm
	// binds are not, and stay inside that silence.
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

// DecoderSuite pins which decode<Name> helpers an emission carries and
// what each of them may name. The fixture-driven golden corpus lives in
// internal/codegen/conformance.
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
// element declarations decoderProbeSchema writes per width.
//
// It stops at the parse. What the emission then does with each of those
// declarations is TestEveryElementTypeGetsItsOwnDecodeArm's subject.
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

	for _, w := range decoderProbeWidths() {
		tag := decoderProbeTag(w.pt)
		s.requireArm(nodes, "Req"+tag, w.pt, false)
		s.requireArm(nodes, "Opt"+tag, w.pt, true)
		s.requireArm(edges, "EdgeReq"+tag, w.pt, false)
		s.requireArm(edges, "EdgeOpt"+tag, w.pt, true)
	}
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

// decoderArm is what the emission carries for one element type: the
// struct's fields by name, the carrier its decode helper takes, what that
// helper answers with, and the fields it assigns. Assembled per entity
// rather than folded across the emission, so an entity that lost its
// struct, its helper or its fields is one entry short of what the schema
// asks for instead of a contribution some other entity also makes.
type decoderArm struct {
	fields  map[string]string
	carrier string
	returns []string
	decoded []string
}

// TestEveryElementTypeGetsItsOwnDecodeArm holds the emission to one
// struct and one decode helper per element type the probe schema
// declares, each struct carrying a field per property that element
// declares and each helper assigning every one of them.
//
// The reference is the parsed schema. That is what separates this from
// the scope sweep below, whose reference is a second emission of the same
// generator: an arm the emitter stopped emitting is missing from both of
// that comparison's sides at once, so an emitter that dropped the edge
// decode helpers — or emitted edge structs with no fields — passes it.
// Neither edit reaches a parse that ran before generation.
//
// One thing it does not separate out is the type table. The Go type
// expected of a field is read through typeMap.Property, which is the
// table the emission narrows through as well, so a carrier respelled
// there moves both sides. TestDecoderProbeCoversTheTypeTable holds that
// table's arms to internal/graph's constants and the golden corpus in
// internal/codegen/conformance pins the spellings; neither is this.
//
// Struct names are the probe's own labels. Each is a single label that is
// already an exported Go identifier, which is what makes §4.5's
// derivation identity on them — a derivation that stopped being identity
// fails the name comparison below carrying both spellings, rather than
// passing silently.
func (s *DecoderSuite) TestEveryElementTypeGetsItsOwnDecodeArm() {
	sch, err := gql.New().Parse(strings.NewReader(decoderProbeSchema(unclaimedProperty)))
	s.Require().NoError(err)

	want := make(map[string]decoderArm, len(sch.Nodes)+len(sch.Edges))
	for _, n := range sch.Nodes {
		s.declareArm(want, string(n.KeyLabels), "dbtype.Node", n.Properties)
	}
	for _, e := range sch.Edges {
		s.declareArm(want, string(e.KeyLabels), "dbtype.Relationship", e.Properties)
	}

	// A carrier no probe element declares a property at is a carrier whose
	// field census below holds over nothing. Counted off the schema, since
	// an emission that dropped one is what this exists to catch.
	carriers := make(map[string]int, 2)
	for _, arm := range want {
		if len(arm.fields) > 0 {
			carriers[arm.carrier]++
		}
	}
	s.Require().Len(carriers, 2,
		"the probe declares a property-carrying element type at %d of the two driver carriers, so one "+
			"carrier's decode arms are swept for existence only", len(carriers))

	models, err := s.emitModels(unclaimedProperty)
	s.Require().NoError(err)
	got := s.decoderArmsOf(models)
	s.Require().Equal(slices.Sorted(maps.Keys(want)), slices.Sorted(maps.Keys(got)),
		"the emission does not name one entity per element type the schema declares")

	for _, name := range slices.Sorted(maps.Keys(want)) {
		w := want[name]
		s.Require().Equal(w.fields, got[name].fields,
			"struct %s does not carry exactly the properties the schema declares on it", name)
		s.Require().Equal(w.carrier, got[name].carrier,
			"decode%s does not take the driver carrier its element kind is read from", name)
		s.Require().Equal([]string{name, "error"}, got[name].returns,
			"decode%s does not answer with the struct it decodes into", name)
		s.Require().Equal(slices.Sorted(maps.Keys(w.fields)), got[name].decoded,
			"decode%s assigns other than every field %s carries", name, name)
	}
}

// declareArm records what one element type obliges the emission to carry.
// The Go type is the carrier the type table gives the declared width,
// under a pointer on the nullable arm.
func (s *DecoderSuite) declareArm(into map[string]decoderArm, name, carrier string, props map[string]schema.Property) {
	s.T().Helper()
	_, dup := into[name]
	s.Require().False(dup, "two probe element types are named %s, so one of them is unswept", name)

	fields := make(map[string]string, len(props))
	for _, p := range props {
		goType, ok := neo4j.TypeMap{}.Property(p.Type)
		s.Require().True(ok, "%s declares %s at %s, which this backend has no carrier for", name, p.Name, p.Type)
		if p.Nullable {
			goType = "*" + goType
		}
		fields[exportedField(p.Name)] = goType
	}
	into[name] = decoderArm{fields: fields, carrier: carrier}
}

// decoderArmsOf reads back what the emission carries per entity, keyed on
// the entity name. A struct and a decode<Name> helper contribute to one
// entry, so a helper with no struct behind it — or a struct with no
// helper — is an entry that answers the comparison rather than a name
// that never comes up.
//
// The assigned fields are read off the selector each assignment targets,
// not off the accumulator's name, which is the generator's to choose.
func (s *DecoderSuite) decoderArmsOf(models string) map[string]decoderArm {
	out := make(map[string]decoderArm)
	fieldsOf := func(name string) map[string]string {
		if arm, ok := out[name]; ok {
			return arm.fields
		}
		return make(map[string]string)
	}

	for _, decl := range s.parseModels(models).Decls {
		switch d := decl.(type) {
		case *ast.GenDecl:
			if d.Tok != token.TYPE {
				continue
			}
			for _, spec := range d.Specs {
				ts, isType := spec.(*ast.TypeSpec)
				if !isType {
					continue
				}
				st, isStruct := ts.Type.(*ast.StructType)
				if !isStruct {
					continue
				}
				arm := out[ts.Name.Name]
				arm.fields = fieldsOf(ts.Name.Name)
				for _, field := range st.Fields.List {
					for _, id := range field.Names {
						arm.fields[id.Name] = types.ExprString(field.Type)
					}
				}
				out[ts.Name.Name] = arm
			}
		case *ast.FuncDecl:
			name, isDecoder := strings.CutPrefix(d.Name.Name, "decode")
			if !isDecoder || d.Recv != nil || name == "" {
				continue
			}
			assigned := make(map[string]bool)
			if d.Body != nil {
				ast.Inspect(d.Body, func(n ast.Node) bool {
					assign, isAssign := n.(*ast.AssignStmt)
					if !isAssign {
						return true
					}
					for _, lhs := range assign.Lhs {
						if sel, isSel := lhs.(*ast.SelectorExpr); isSel {
							assigned[sel.Sel.Name] = true
						}
					}
					return true
				})
			}
			out[name] = decoderArm{
				fields:  fieldsOf(name),
				carrier: strings.Join(fieldTypeStrings(d.Type.Params), ", "),
				returns: fieldTypeStrings(d.Type.Results),
				decoded: slices.Sorted(maps.Keys(assigned)),
			}
		}
	}
	return out
}

// fieldTypeStrings renders a signature's parameter or result types in
// declaration order, one entry per name a grouped field declares.
func fieldTypeStrings(list *ast.FieldList) []string {
	if list == nil {
		return nil
	}
	var out []string
	for _, field := range list.List {
		rendered := types.ExprString(field.Type)
		for range max(len(field.Names), 1) {
			out = append(out, rendered)
		}
	}
	return out
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
// per scalar width the type table admits and the two list arms over each
// of them. Each name is then fed back as a property name, and what
// must not move is the set itself: the decoder's identifiers are the
// generator's own, so they are the same whatever the schema declares.
//
// Both sides of the comparison are emissions of the same generator, so
// what it measures is movement under a renamed property and nothing else:
// an arm the emitter stopped emitting is absent from the reference too,
// and the equality holds over what is left. Which arms have to be there
// at all is TestEveryElementTypeGetsItsOwnDecodeArm's subject, against
// the parsed schema.
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

// closureProbeDepth is how deep the closure probe nests the list
// constructor. Three is one level past decoderProbeWidths, which is
// enough for a stem to be observed at three suffixes (elem0, elem1,
// elem2) and so for "the suffix is the recursion depth" to be a reading
// of the emission rather than of two points.
const closureProbeDepth = 3

// closureProbeFill is how many properties each entity in the closure
// probe declares. The positional locals (value0, value1, …) are indexed
// by field position rather than by nesting depth, and a schema of one
// property per entity never reaches the second position — which is the
// silence decoderProbeSchema's own doc comment records. Eight is past
// any suffix the depth families reach here, so the two families are
// probed over one shared candidate range.
const closureProbeFill = 8

// closureProbeSchema is a schema built to exercise every family of local
// the entity decoders bind, at more than one index in each: the list
// constructor nested 0..closureProbeDepth deep (elem<n>, i<n>,
// nested<n>, acc<n>, v<n>), closureProbeFill properties per entity
// (value<n>), and a multi-label node type (has<n>).
//
// prop names the one property whose name is under test; the rest are
// fillers, spelled so they cannot themselves collide with anything.
//
// It is deliberately narrow on width — INT64 and nothing else — because
// the claim it serves is about the identifiers the walk binds, which
// depend on depth and position and not on the leaf carrier. The width
// axis is decoderProbeSchema's.
func closureProbeSchema(prop string, maxDepth int) string {
	props := func() string {
		var b strings.Builder
		for depth := range maxDepth + 1 {
			ty := "INT64"
			for range depth {
				ty = "LIST<" + ty + ">"
			}
			for i := range closureProbeFill {
				name := fmt.Sprintf("zfiller%dx%d", depth, i)
				if depth == maxDepth && i == 0 {
					name = prop
				}
				fmt.Fprintf(&b, "%s :: %s NOT NULL, ", name, ty)
			}
		}
		return strings.TrimSuffix(b.String(), ", ")
	}()

	var b strings.Builder
	b.WriteString("CREATE PROPERTY GRAPH TYPE ClosureProbe AS {\n")
	b.WriteString("    (:Endpoint),\n")
	fmt.Fprintf(&b, "    (:Deep { %s }),\n", props)
	fmt.Fprintf(&b, "    NODE TYPE Pair (p :Alpha&Beta&Gamma { %s }),\n", props)
	fmt.Fprintf(&b, "    (:Endpoint) -[:EdgeDeep { %s }]-> (:Endpoint)\n", props)
	b.WriteString("}")
	return b.String()
}

// TestDecoderLocalFamiliesAreClosedUnderDepth is what
// TestNoDecoderLocalTakesAPropertyName cannot be. That test reads the
// identifiers off one emission and feeds each back as a property name,
// so what it holds is the *set* a probe of a fixed shape happens to
// produce. The emitter does not draw from a set: writeSliceNarrow names
// its locals `fmt.Sprintf("elem%d", depth)` and three siblings the same
// way, and writeLabelGuard and writeEntityFieldDecode index theirs by
// key-label position and field position. Every one of those is a family
// `<stem><n>` with no bound on n, so a schema nesting one level deeper
// than the probe emits five identifiers no sweep has ever seen, and a
// property named after any of them collides undetected (bd gqlc-wdo7).
//
// This holds the families instead, in two halves.
//
// The stem half: emitting at each depth from the first that reaches the
// recursive arm up to closureProbeDepth must yield the same set of
// stems. A depth that introduced a stem no shallower one carries would
// be a family this file has not enumerated, and the candidate half
// below would not be probing it.
//
// The suffix half: for every stem observed with a numeric suffix, a
// property named `<stem><m>` — for every m in a range past any suffix
// the emission reaches — must leave the decoders' scope exactly where
// it was. That is the closure. It does not depend on the emission ever
// reaching depth m, which is the whole point: the identifier `elem7` is
// refused as a property name today, so the day a user's schema nests
// seven deep and the emitter binds it, nothing collides.
func (s *DecoderSuite) TestDecoderLocalFamiliesAreClosedUnderDepth() {
	scopes := make(map[int][]string)
	stems := make(map[int][]string)
	for depth := 2; depth <= closureProbeDepth; depth++ {
		models, err := s.emitClosureModels(unclaimedProperty, depth)
		s.Require().NoError(err)
		scopes[depth] = s.decoderScopeOf(models)
		s.Require().NotEmpty(scopes[depth], "the emitted decoders bind no identifiers to check at depth %d", depth)
		stems[depth] = suffixedStems(scopes[depth])
		s.Require().NotEmpty(stems[depth],
			"no identifier the decoders bind at depth %d carries a numeric suffix, so there is no family here "+
				"to close and the candidate sweep below would run over nothing", depth)
	}
	for depth := 3; depth <= closureProbeDepth; depth++ {
		s.Require().Equal(stems[2], stems[depth],
			"nesting the list constructor %d deep rather than 2 changes which families of local the decoders "+
				"bind. A stem only the deeper emission carries is one the closure sweep below never probes, so "+
				"a property named after it would collide; a stem only the shallower one carries means the "+
				"suffix is not the recursion depth after all", depth)
	}

	// The deepest emission is the reference: it is the one whose scope
	// holds the highest suffix of every family, so a candidate that
	// slipped into it would move the comparison.
	reference := scopes[closureProbeDepth]
	for _, stem := range stems[closureProbeDepth] {
		for m := range closureProbeFill {
			name := fmt.Sprintf("%s%d", stem, m)
			s.Run(name, func() {
				models, err := s.emitClosureModels(name, closureProbeDepth)
				if err != nil {
					s.T().Skipf("no property can be named %q: %v", name, err)
				}
				s.Require().Contains(models, "\t"+exportedField(name)+" ",
					"the struct no longer carries the property under the name the schema gave it")
				s.Require().Equal(reference, s.decoderScopeOf(models),
					"a property named %q reached the decoder's scope: the %s family is not closed, so a "+
						"schema deep or wide enough for the emitter to bind this identifier itself would "+
						"emit a redeclaration", name, stem)
			})
		}
	}
}

// suffixedStems is the sorted, deduplicated set of stems among names
// ending in decimal digits. A name carrying no digit contributes
// nothing: it is a fixed identifier, held by
// TestNoDecoderLocalTakesAPropertyName, and has no family behind it.
func suffixedStems(names []string) []string {
	seen := make(map[string]bool)
	for _, name := range names {
		stem := strings.TrimRightFunc(name, unicode.IsDigit)
		if stem == name || stem == "" {
			continue
		}
		seen[stem] = true
	}
	return slices.Sorted(maps.Keys(seen))
}

// emitClosureModels emits models.go for the closure probe schema spelled
// around prop at the given nesting depth. The error is the schema
// parse's alone, for the reason emitModels gives.
func (s *DecoderSuite) emitClosureModels(prop string, depth int) (string, error) {
	sch, err := gql.New().Parse(strings.NewReader(closureProbeSchema(prop, depth)))
	if err != nil {
		return "", err
	}
	files, err := neo4j.New().Generate(codegen.Input{Schema: sch})
	s.Require().NoError(err)
	for _, f := range files {
		if f.Path == "models.go" {
			return string(f.Contents), nil
		}
	}
	s.Require().Fail("no models.go in the emission")
	return "", nil
}

// emitModels emits models.go for the probe schema spelled around the
// property name prop. The error is the schema parse's alone: a
// generation that failed would be this suite's business, and a parse
// that failed is the caller's.
func (s *DecoderSuite) emitModels(prop string) (string, error) {
	sch, err := gql.New().Parse(strings.NewReader(decoderProbeSchema(prop)))
	if err != nil {
		return "", err
	}
	files, err := neo4j.New().Generate(codegen.Input{Schema: sch})
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
		// The methods models.go declares are edge-union markers; the
		// receiver is what excludes them, since an empty body is still a
		// body. The decode helpers are what is left.
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

// parseModels is the emitted file as a syntax tree.
func (s *DecoderSuite) parseModels(models string) *ast.File {
	f, err := parser.ParseFile(token.NewFileSet(), "models.go", models, parser.SkipObjectResolution)
	s.Require().NoError(err, "the emitted file does not parse")
	return f
}

// declaredIdents returns the identifiers a node binds: short variable
// declarations, var declarations and range clauses. A name a body
// introduces some other way is one this does not return — with labels
// the deliberate omission, since Go scopes them apart from variables and
// a label spelled like a property collides with nothing.
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
