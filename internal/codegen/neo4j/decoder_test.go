package neo4j_test

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"maps"
	"os"
	"path/filepath"
	"regexp"
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
// above, and that scalar under the list constructor once.
//
// The list arm is derived rather than written out, so a scalar added to
// the table above arrives with its slice already declared. ONE level is
// where the derivation stops, and the reason is not economy: under ADR
// 0035 the neo4j server will not hold a list of lists as a stored
// property, so a schema declaring one is refused at generation and no
// probe can be built on it. The derivation stopped at two before that
// ruling; TestDecoderProbeCoversTheTypeTable now asserts the dual —
// that every scalar's doubly-nested width is refused by
// StorableProperty — so the level's absence is a measured consequence
// rather than a gap somebody trimmed.
//
// One casualty is named rather than hidden: depth 2 was the first depth
// reaching render_models.go's writeSliceNarrow recursive arm, and no
// stored property can reach it now. That arm is unreachable-by-
// construction on this backend, and it is DELETED in the same change that
// retires these probes — the probes were its only guard, so leaving it
// would publish an unreachable and unguarded path.
// The recursive decode a nested list gets as a QUERY VALUE is a
// different emitter binding a different local family (render_queries.go,
// inner<n>/innerAcc<n>), so nothing here witnesses it and this comment
// does not claim it does.
//
// No depth finished the job on its own even before the ruling: that walk
// names its locals off the recursion depth, so each further level
// introduces identifiers no shallower probe has seen (bd gqlc-wdo7).
// What closes it is a different measurement, not a deeper one —
// TestDecoderLocalFamiliesAreClosedUnderDepth holds the *families* the
// emitter draws from rather than the set one probe happens to observe.
// This width table is what the per-width sweeps below read; the closure
// test builds its own narrow schema.
func decoderProbeWidths() []decoderProbeWidth {
	out := make([]decoderProbeWidth, 0, 2*len(decoderProbeScalars))
	for _, w := range decoderProbeScalars {
		list := decoderProbeWidth{pt: graph.ListOf(w.pt, false), spelling: "LIST<" + w.spelling + ">"}
		out = append(out, w, list)
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

	// The probe declares no width whose element is itself a list, and
	// this is the assertion that says WHY. Before ADR 0035 the claim here
	// was the opposite — require.Positive on the count of nested widths,
	// because depth 2 was the first depth reaching the emitted walk's
	// recursive arm. The ruling makes such a property undeclarable on
	// this backend, so that obligation is replaced by its dual rather
	// than deleted: for every scalar the probe covers, the doubly-nested
	// width must be REFUSED by the storage axis.
	//
	// Stated per scalar rather than as one aggregate count. An aggregate
	// ("at least one nested width is refused") is satisfied by a single
	// scalar and would stay green if the refusal came to depend on the
	// leaf type — which is exactly the over-narrow check
	// TestStorablePropertyRefusesANestedList's rows exist to catch, and a
	// count here would let this test vouch for it anyway.
	//
	// The absence itself is also asserted, in the same loop: a nested
	// width that WERE declared would be refused at generation and the
	// whole probe schema would fail to parse, so the two halves cannot
	// both be true by accident.
	for _, w := range decoderProbeScalars {
		nested := graph.ListOf(graph.ListOf(w.pt, false), false)
		require.False(t, neo4j.TypeMap{}.StorableProperty(nested),
			"%s is not refused by the storage axis, so a probe entity could declare it as a stored "+
				"property and the level decoderProbeWidths stops at is a gap rather than a consequence "+
				"of ADR 0035", nested)
		require.False(t, covered[nested],
			"the probe declares %s, which this backend refuses as a stored property (ADR 0035)", nested)
	}
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
// constructor. ONE, and the ceiling is the server's rather than this
// file's: under ADR 0035 a stored property may not be a list of lists,
// so a probe schema nesting deeper is refused at generation and cannot
// be emitted at all. It was three before that ruling.
//
// WHAT THAT COSTS, said plainly rather than left for a reader to
// discover. The test below used to compare the emissions at depth 2 and
// depth 3 and require the same set of stems, which is how it read "the
// suffix is the recursion depth" off three observed suffixes rather than
// off two points. No such comparison is constructible now: one depth is
// declarable, so there is no ladder. The SUFFIX half — the closure sweep
// that refuses `<stem><m>` as a property name for every m past any
// suffix the emission reaches — is untouched and is the half that
// carries the claim, because it never depended on the emission reaching
// depth m (bd gqlc-wdo7).
//
// The lost half was guarding render_models.go's writeSliceNarrow
// recursive arm, which the same ruling makes unreachable by
// construction and the same change deletes. A nested list arriving as a QUERY
// VALUE still emits a recursive decode, from render_queries.go binding
// inner<n>/innerAcc<n>/elem<n> — a family this suite does not sweep and
// has never swept, since it reads models.go alone.
const closureProbeDepth = 1

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
// This holds the families instead. It had two halves; ADR 0035 leaves
// one, and closureProbeDepth records which and why.
//
// The stem half is GONE, not quietly dropped: it emitted at each depth
// from the first reaching the recursive arm up to closureProbeDepth and
// required the same set of stems, and no such ladder can be built out of
// stored properties now that a list of lists is not one.
//
// The suffix half, which is the one carrying the claim: for every stem
// observed with a numeric suffix, a
// property named `<stem><m>` — for every m in a range past any suffix
// the emission reaches — must leave the decoders' scope exactly where
// it was. That is the closure. It does not depend on the emission ever
// reaching depth m, which is the whole point: the identifier `elem7` is
// refused as a property name today, so the day a user's schema nests
// seven deep and the emitter binds it, nothing collides.
func (s *DecoderSuite) TestDecoderLocalFamiliesAreClosedUnderDepth() {
	// One emission, at the deepest nesting a stored property may take.
	// The cross-depth stem comparison this test used to open with is
	// unconstructible under ADR 0035 — closureProbeDepth says why — so
	// what follows is the suffix half alone.
	models, err := s.emitClosureModels(unclaimedProperty, closureProbeDepth)
	s.Require().NoError(err)
	reference := s.decoderScopeOf(models)
	s.Require().NotEmpty(reference, "the emitted decoders bind no identifiers to check")

	// Asserted rather than assumed, and this is load-bearing now that
	// there is no ladder above it: with a single emission, an empty stem
	// set would run the whole sweep below over nothing and report a
	// closure it never probed. The vacuous pass is the failure mode a
	// retired ladder invites.
	stems := suffixedStems(reference)
	s.Require().NotEmpty(stems,
		"no identifier the decoders bind carries a numeric suffix, so there is no family here to close "+
			"and the candidate sweep below would run over nothing")

	for _, stem := range stems {
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

// narrowingWidths names every emitted Go type a driver carrier has to be
// narrowed INTO. int64 and float64 are absent because they are the
// carriers themselves, so a conversion to one of them is a widening and
// cannot lose a value; the neutral temporal types are absent because they
// are not conversion-compatible with their carriers at all and go through
// an emitted to<X> helper (ADR 0033), which is a shape change and has no
// range question.
var narrowingWidths = map[string]bool{
	"int": true, "int8": true, "int16": true, "int32": true,
	"uint": true, "uint8": true, "uint16": true, "uint32": true, "uint64": true,
	"float32": true,
}

// narrowHelperNames are the two emitted helpers that legitimately hold a
// bare narrowing conversion, because performing one and reporting on it
// is what they are for.
var narrowHelperNames = map[string]bool{"narrowInt": true, "narrowFloat32": true}

// TestEmittedDecodersNarrowThroughACheck refuses a bare Go conversion to a
// declared width anywhere in an emitted body but the two helpers that
// implement the check.
//
// It is the mechanical form of the question ADR 0037 answers. A renderer
// arm that converts at the site rather than calling the helper wraps a
// value the declared width cannot hold, silently and with no error — and
// nothing else in this package fails when it does: the suite is green
// either way, the emission compiles, the goldens agree with themselves,
// and the wrong number reaches the caller looking exactly like a right
// one. The corpus rows catch the eight sites that exist today. This
// catches the ninth.
//
// Both fixtures are walked because neither reaches the other's arms: the
// probe declares every width the type table carries but only on entity
// properties, and the corpus reaches the query column and list-element
// paths but at a handful of widths.
func TestEmittedDecodersNarrowThroughACheck(t *testing.T) {
	t.Parallel()

	for name, src := range emissionsUnderNarrowingGuard(t) {
		file, err := parser.ParseFile(token.NewFileSet(), name, src, parser.SkipObjectResolution)
		require.NoError(t, err, "%s does not parse", name)
		fset := token.NewFileSet()
		fset.AddFile(name, fset.Base(), len(src))

		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil || narrowHelperNames[fn.Name.Name] {
				continue
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, isCall := n.(*ast.CallExpr)
				if !isCall || len(call.Args) != 1 {
					return true
				}
				id, isIdent := call.Fun.(*ast.Ident)
				if !isIdent || !narrowingWidths[id.Name] {
					return true
				}
				t.Errorf("%s: %s narrows to %s by a bare conversion, which wraps a value "+
					"the declared width cannot hold; it must go through narrowInt/narrowFloat32 (ADR 0037)",
					name, fn.Name.Name, id.Name)
				return true
			})
		}
	}
}

// narrowingGuardSkips names emitted files the guard above does not walk.
//
// temporal_neo4j.go is the only one, and it is skipped because it holds a
// legitimate narrowing the guard cannot tell from an illegitimate one:
// toDate converts a time.Month to int, which is a conversion between two
// integer types of the same width and loses nothing. Excluding it costs
// exactly one thing, stated so nobody has to rediscover it — a bare
// narrowing introduced INTO a temporal helper is not caught here. That is
// a narrow loss because the helpers are fixed text: render_temporal.go
// returns each as a string constant, so no renderer arm composes a
// conversion into one per width, which is the thing this guard exists to
// catch.
var narrowingGuardSkips = map[string]bool{"temporal_neo4j.go": true}

// emissionsUnderNarrowingGuard is every emitted Go file the guard above
// walks, keyed by a name that says which fixture it came from.
//
// The bytes come from Generate rather than from the golden tree, for the
// reason the corpus states: a golden regenerated alongside a defect
// agrees with itself.
func emissionsUnderNarrowingGuard(t *testing.T) map[string]string {
	t.Helper()
	out := map[string]string{}
	keep := func(prefix string, files []codegen.File) {
		for _, f := range files {
			if !narrowingGuardSkips[f.Path] {
				out[prefix+f.Path] = string(f.Contents)
			}
		}
	}

	probe, err := gql.New().Parse(strings.NewReader(decoderProbeSchema(unclaimedProperty)))
	require.NoError(t, err, "the probe schema does not parse")
	probeFiles, err := neo4j.New().Generate(codegen.Input{Schema: probe})
	require.NoError(t, err)
	keep("probe/", probeFiles)

	src, err := os.ReadFile(filepath.Join("testdata", corpusSchema))
	require.NoError(t, err)
	sch, err := gql.New().Parse(bytes.NewReader(src))
	require.NoError(t, err)
	corpusFiles, err := neo4j.New(neo4j.WithPackageName(corpusPackage)).
		Generate(codegen.Input{Schema: sch, Queries: corpusNamedQueries(t, sch)})
	require.NoError(t, err)
	keep("corpus/", corpusFiles)

	require.NotEmpty(t, out, "the guard walked no emitted files at all")
	return out
}

// densityProbeSchema declares entities whose required and nullable
// properties interleave, which is the only shape that can expose a gap:
// with the nullable ones at either end the sequence stays dense by
// accident and the defect hides.
//
// The interleaving is spelled in the NAMES, not in the declaration
// order, and that is not decoration. prepareEntityFields sorts an
// entity's properties by property name (prepare.go:561), so a schema
// that alternates as written but groups when sorted emits a run of
// required properties and no gap at all -- which is what the first
// draft of this probe did, and it reported a 4-offset rather than the
// interleaving it was written to show.
const densityProbeSchema = `CREATE PROPERTY GRAPH TYPE DensityProbe AS {
    (:Endpoint),
    (:Interleaved {
        alphaOptional :: STRING,
        bravoRequired :: INT64 NOT NULL,
        chalkOptional :: STRING,
        deltaRequired :: INT64 NOT NULL,
        elderRequired :: INT64 NOT NULL,
        fennelOptional :: STRING,
        gorseRequired :: INT64 NOT NULL,
        hazelOptional :: STRING
    }),
    (:Endpoint) -[:Edged {
        irisRequired :: INT64 NOT NULL,
        jasmineOptional :: STRING,
        kelpRequired :: INT64 NOT NULL
    }]-> (:Endpoint)
}`

// positionalLocal matches the generator-owned decode local and nothing
// derived from it: writeSliceNarrow and the narrowing arm bind `value0s`
// and `value0n` off the same stem, and those are not positions.
var positionalLocal = regexp.MustCompile(`^value(\d+)$`)

// TestPositionalDecoderLocalsAreDense holds the emitted sequence to
// value0..valueN with no gaps (bd gqlc-7qr5).
//
// Nothing breaks when it is violated, and saying so is the point of this
// comment: the indices are unique by construction, which is the only
// property the naming scheme needs, and Go's unused-variable rule turns
// any positional mis-wiring into a compile error whatever the density.
// What a gap costs is a reader. `value1` followed by `value3` in
// generated code reads as though a statement was deleted, and someone
// who notices goes looking for a bug that is not there.
func (s *DecoderSuite) TestPositionalDecoderLocalsAreDense() {
	sch, err := gql.New().Parse(strings.NewReader(densityProbeSchema))
	s.Require().NoError(err)
	files, err := neo4j.New().Generate(codegen.Input{Schema: sch})
	s.Require().NoError(err)

	var models string
	for _, f := range files {
		if f.Path == "models.go" {
			models = string(f.Contents)
		}
	}
	s.Require().NotEmpty(models, "no models.go in the emission")

	checked := 0
	for _, decl := range s.parseModels(models).Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv != nil || fn.Body == nil ||
			!strings.HasPrefix(fn.Name.Name, "decode") {
			continue
		}

		// Source order, taken off the tree rather than the bytes: a
		// commented-out declaration is not one, and a raw scan would
		// count it.
		var positions []int
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			for _, id := range declaredIdents(n) {
				if m := positionalLocal.FindStringSubmatch(id.Name); m != nil {
					n, err := strconv.Atoi(m[1])
					s.Require().NoError(err)
					positions = append(positions, n)
				}
			}
			return true
		})

		// An entity with no required property binds none, and :Endpoint is
		// declared here to be exactly that. Skipping it is safe only
		// because the population assertion below names the exact number
		// of helpers that must NOT have taken this branch.
		if len(positions) == 0 {
			continue
		}
		checked++

		want := make([]int, len(positions))
		for i := range want {
			want[i] = i
		}
		s.Require().Equal(want, positions,
			"%s emits positional locals %v; the sequence must be dense value0..value%d, "+
				"because a gap reads like a deleted statement to whoever opens the "+
				"generated file",
			fn.Name.Name, positions, len(positions)-1)
	}

	// The vacuity guard, one level up, and it is exact rather than a
	// lower bound. An emission carrying no decode helper would run the
	// loop zero times and report success; so would one where every
	// helper hit the skip above, which is what a probe that quietly
	// stopped declaring required properties looks like. A `>= 1` would
	// pass on both of those halves of the probe going missing.
	s.Require().Equal(2, checked,
		"exactly the interleaved node and the interleaved edge carry required properties, "+
			"so exactly two helpers owe a density comparison; %d made one", checked)
}
