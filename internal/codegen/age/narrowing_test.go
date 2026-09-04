package age_test

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/areqag/gqlc/internal/codegen"
	"github.com/areqag/gqlc/internal/codegen/age"
	"github.com/areqag/gqlc/internal/graph"
	"github.com/areqag/gqlc/internal/queryfile"
	"github.com/areqag/gqlc/internal/resolver"
	"github.com/areqag/gqlc/internal/schema/gql"
)

// narrowingWidths pairs every emitted Go type a stored agtype scalar has
// to be narrowed INTO with the property type that declares it. int64 and
// float64 are absent because they are the carriers themselves, so a
// conversion to one of them cannot lose a value; the temporal types are
// absent because they are not conversion-compatible with their carriers
// at all and go through an emitted helper, which is a shape change and
// has no range question.
//
// The pairing is written out rather than derived because the type table
// runs property type to Go type and has no inverse to walk. It does not
// get to drift from the table on that account:
// TestNarrowingWidthsAgreesWithTheTypeTable requires membership here and
// routing through the checked narrowing to be the same answer for every
// carrier the table names.
var narrowingWidths = map[string]graph.PropertyType{
	"int":     graph.TypeInt,
	"int8":    graph.TypeInt8,
	"int16":   graph.TypeInt16,
	"int32":   graph.TypeInt32,
	"uint":    graph.TypeUint,
	"uint8":   graph.TypeUint8,
	"uint16":  graph.TypeUint16,
	"uint32":  graph.TypeUint32,
	"float32": graph.TypeFloat32,
	"uint64":  graph.TypeUint64,
}

// narrowsThroughACheck reports whether the emitted decoder for goType is
// one of the two checked narrowings. It reads decodeFunc's answer rather
// than a second list of widths, so the question this asks is the one the
// emission actually decides.
func narrowsThroughACheck(t *testing.T, goType string) bool {
	t.Helper()
	decoder := decodeFuncOf(t, goType)
	return strings.HasPrefix(decoder, "agtypeIntAs[") || decoder == "agtypeFloat32"
}

// TestNarrowingWidthsAgreesWithTheTypeTable keeps the table above from
// drifting from the type table it describes. A carrier the property table
// gains that decodes through the checked narrowing and is not listed here
// would be a width the guard below walks past in silence — which is the
// exact failure the guard exists to prevent, arriving through its own
// vocabulary.
func TestNarrowingWidthsAgreesWithTheTypeTable(t *testing.T) {
	for _, carrier := range propertyCarriers(t) {
		t.Run(carrier, func(t *testing.T) {
			_, listed := narrowingWidths[carrier]
			require.Equal(t, narrowsThroughACheck(t, carrier), listed,
				"%s decodes through the checked narrowing but narrowingWidths does not list it (or the reverse); "+
					"the guard reads that map, so an unlisted narrowing width is one it cannot see", carrier)
		})
	}
}

// narrowConversionExempt names the emitted functions that legitimately
// hold a bare conversion to a narrowing width, so the walk below does not
// read one as a defect.
//
// Three kinds, exempt for three different reasons. agtypeFloat32
// performs the narrowing and reports on it, so the conversion inside it
// is the subject of the check rather than an escape from it. The temporal
// helpers convert time COMPONENTS out of a count that was bounded first —
// a time.Month to an int, a clock field out of a micros count already
// held to one day, a zone offset already held to within a day of UTC — so
// the int each lands in holds it. agtypeUnsigned is on the ENCODE side
// and its conversion is the range check itself.
//
// Exempting by FUNCTION rather than by file, which is what the neo4j
// guard does, because this backend has no file to exempt: AGE emits the
// temporal helpers and the entity decoders into the same models.go, so a
// per-file skip would take the decoders this guard exists to walk out
// with them.
//
// The list is closed against both kinds of drift.
// TestExemptedFunctionsStillHoldAConversion fails a name that stopped
// needing its exemption, so the list cannot quietly grow into a blind
// spot; a name that starts needing one arrives as a failure of the guard
// itself, naming the function and the width.
//
// What it costs is stated so nobody has to rediscover it: a bare
// narrowing introduced INTO one of these is not caught. That is narrow,
// because each is emitted as fixed text — render_models.go writes them as
// string constants — so no renderer arm composes a conversion into one
// per width, which is the thing this guard exists to catch.
//
// agtypeIntAs is deliberately NOT here, and its absence is a limit worth
// stating rather than a tightening: it narrows through the type parameter
// T, and a conversion spelled through a type parameter is not an
// identifier this walk recognises. So the walk would pass over a
// generically-spelled narrowing anywhere, not only in that helper.
var narrowConversionExempt = map[string]bool{
	"agtypeFloat32":   true,
	"agtypeDate":      true,
	"agtypeDateText":  true,
	"agtypeDuration":  true,
	"agtypeLocalTime": true,
	"agtypeOffset":    true,
	"agtypeTimeAt":    true,
	"agtypeUnsigned":  true,
}

// TestExemptedFunctionsStillHoldAConversion requires every exempted name
// to be emitted by a fixture the guard walks AND to still hold a narrowing
// conversion there.
//
// An exemption that has stopped being needed is not harmless. It is a
// function name the guard will not look inside, kept for a reason that no
// longer applies, and the next bare conversion written into one of them
// is the defect this whole file exists to catch — arriving at the one
// place that is not watched.
//
// Names the fixtures do not emit are reported rather than skipped: an
// exemption nothing walks is one nobody has checked.
func TestExemptedFunctionsStillHoldAConversion(t *testing.T) {
	holds := map[string]bool{}
	for name, src := range emissionsUnderNarrowingGuard(t) {
		file, err := parser.ParseFile(token.NewFileSet(), name, src, parser.SkipObjectResolution)
		require.NoError(t, err, "%s does not parse", name)

		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil || !narrowConversionExempt[fn.Name.Name] {
				continue
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, isCall := n.(*ast.CallExpr)
				if !isCall || len(call.Args) != 1 {
					return true
				}
				if id, isIdent := call.Fun.(*ast.Ident); isIdent {
					if _, narrowing := narrowingWidths[id.Name]; narrowing {
						holds[fn.Name.Name] = true
					}
				}
				return true
			})
		}
	}

	var stale []string
	for name := range narrowConversionExempt {
		if !holds[name] {
			stale = append(stale, name)
		}
	}
	sort.Strings(stale)
	require.Empty(t, stale,
		"these functions are exempted from the narrowing guard but no emission the guard walks shows one "+
			"holding a narrowing conversion; an exemption that is not needed is a body nobody checks")
}

// TestEmittedDecodersNarrowThroughACheck refuses a bare Go conversion to
// a declared width anywhere in an emitted body but the helpers that
// implement the check.
//
// AGE decodes through a single chokepoint — decodeFunc names the helper
// and every site takes it as a function value — and
// TestDecodeFuncNamesTheHelperForEveryServedCarrier already pins that
// mapping arm by arm. This catches the other shape: a renderer arm that
// goes AROUND decodeFunc and converts at the site.
//
// That shape is not hypothetical and it is not caught elsewhere. Planting
// it at the query-column site (columnDecoder) and running the whole tree
// reddens exactly one thing — the float32_column golden — and nothing
// behavioural; regenerate the goldens alongside it and the tree is green
// with every out-of-range value silently wrapping. The other two sites
// are behaviourally covered (the entity-property site by
// TestDecodeVertexRefusesAValueTheDeclaredWidthCannotHold, the
// list-element site by TestAgtypeListRefusesAnOutOfRangeElement), so the
// column is the site this buys, and it buys it at every width rather than
// at the one the corpus happens to reach.
func TestEmittedDecodersNarrowThroughACheck(t *testing.T) {
	t.Parallel()

	for name, src := range emissionsUnderNarrowingGuard(t) {
		file, err := parser.ParseFile(token.NewFileSet(), name, src, parser.SkipObjectResolution)
		require.NoError(t, err, "%s does not parse", name)

		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil || narrowConversionExempt[fn.Name.Name] {
				continue
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, isCall := n.(*ast.CallExpr)
				if !isCall || len(call.Args) != 1 {
					return true
				}
				id, isIdent := call.Fun.(*ast.Ident)
				if !isIdent {
					return true
				}
				if _, narrowing := narrowingWidths[id.Name]; !narrowing {
					return true
				}
				t.Errorf("%s: %s narrows to %s by a bare conversion, which wraps a value the declared "+
					"width cannot hold; it must decode through agtypeIntAs/agtypeFloat32 (ADR 0037)",
					name, fn.Name.Name, id.Name)
				return true
			})
		}
	}
}

// narrowingProbeEntity is the vertex label the probe schema declares.
const narrowingProbeEntity = "Widths"

// narrowingProbeSchema declares one property per narrowing width three
// times over — non-nullable, nullable, and as a list element — so the
// probe reaches each of the two model-side sites at every width rather
// than at the widths some fixture happens to carry.
//
// It is composed from narrowingWidths rather than written out, so a width
// added there is a property here with no second edit.
func narrowingProbeSchema() string {
	var b strings.Builder
	b.WriteString("CREATE PROPERTY GRAPH TYPE NarrowingProbe AS {\n    (:" + narrowingProbeEntity + " {\n")
	b.WriteString("        id :: INT64 NOT NULL")
	for _, goType := range slices.Sorted(maps.Keys(narrowingWidths)) {
		pt := string(narrowingWidths[goType])
		fmt.Fprintf(&b, ",\n        req_%s :: %s NOT NULL", goType, pt)
		fmt.Fprintf(&b, ",\n        opt_%s :: %s", goType, pt)
		fmt.Fprintf(&b, ",\n        lst_%s :: LIST<%s NOT NULL> NOT NULL", goType, pt)
	}
	b.WriteString("\n    })\n}\n")
	return b.String()
}

// narrowingProbeQueries is one query per narrowing width, each projecting
// a single column of that width.
//
// This is the half a fixture built from schema properties alone does not
// have. The column decoder is a separate emission site from the entity
// decoder, and it is the site nothing else in the tree covers: the corpus
// reaches it at float32 and at no other width, and only through a golden.
func narrowingProbeQueries() []codegen.NamedQuery {
	var out []codegen.NamedQuery
	for _, goType := range slices.Sorted(maps.Keys(narrowingWidths)) {
		method := "Column" + strings.ToUpper(goType[:1]) + goType[1:]
		out = append(out, codegen.NamedQuery{
			Name:        method,
			Cardinality: queryfile.CardinalityOne,
			SourceFile:  "widths.cypher",
			SourceText:  "MATCH (w:" + narrowingProbeEntity + ") RETURN w.req_" + goType + " AS v\n",
			Validated: resolver.ValidatedQuery{Columns: []resolver.Column{{
				Name: "v",
				Type: resolver.ResolvedProperty{Type: narrowingWidths[goType]},
			}}},
		})
	}

	// One query that BINDS rather than projects. Two of the exempted
	// helpers — agtypeDateText and agtypeUnsigned — are encode-side, so a
	// probe that only reads columns never emits them and their exemptions
	// would sit over bodies this walk cannot see.
	out = append(out, codegen.NamedQuery{
		Name:        "BindEncoded",
		Cardinality: queryfile.CardinalityMany,
		SourceFile:  "widths.cypher",
		SourceText:  "MATCH (w:" + narrowingProbeEntity + ") WHERE w.id = $big RETURN w.id AS v\n",
		Validated: resolver.ValidatedQuery{
			Parameters: []resolver.ResolvedParameter{
				{Name: "big", Type: resolver.ResolvedProperty{Type: graph.TypeUint64}},
				{Name: "day", Type: resolver.ResolvedProperty{Type: graph.TypeDate}},
			},
			Columns: []resolver.Column{{
				Name: "v",
				Type: resolver.ResolvedProperty{Type: graph.TypeInt64},
			}},
		},
	})
	return out
}

// emissionsUnderNarrowingGuard is every emitted Go file the guard walks,
// keyed by a name that says which fixture it came from.
//
// The bytes come from Generate rather than from the golden tree, for the
// reason the corpus states: a golden regenerated alongside a defect
// agrees with itself. That is not a general caution here — it is the
// measured behaviour of this exact defect, which survives the whole suite
// once the goldens are refreshed.
//
// Both fixtures are walked because neither reaches the other's arms: the
// probe declares every narrowing width but only the shapes it was built
// from, and the corpus carries the entity surface the backend is
// otherwise tested against.
//
// The returns are guarded rather than trusted. A fixture that stopped
// reaching a site would leave this test walking bodies with no narrowing
// in them at all, and it would pass — green because it asserted nothing,
// which is the failure mode of every guard shaped like this one.
func emissionsUnderNarrowingGuard(t *testing.T) map[string]string {
	t.Helper()

	out := map[string]string{}
	keep := func(prefix string, files []codegen.File) {
		for _, f := range files {
			out[prefix+f.Path] = string(f.Contents)
		}
	}

	probeSchema, err := gql.New().Parse(strings.NewReader(narrowingProbeSchema()))
	require.NoError(t, err, "the probe schema does not parse:\n%s", narrowingProbeSchema())
	probeFiles, err := age.New(age.WithPackageName("narrowingprobe")).
		Generate(codegen.Input{Schema: probeSchema, Queries: narrowingProbeQueries()})
	require.NoError(t, err)
	keep("probe/", probeFiles)

	src, err := os.ReadFile(filepath.Join("testdata", corpusSchema))
	require.NoError(t, err)
	sch, err := gql.New().Parse(bytes.NewReader(src))
	require.NoError(t, err)
	corpusFiles, err := age.New(age.WithPackageName(corpusPackage)).Generate(codegen.Input{Schema: sch})
	require.NoError(t, err)
	keep("corpus/", corpusFiles)

	require.NotEmpty(t, out, "the guard walked no emitted files at all")
	requireProbeReachesEverySite(t, out)
	return out
}

// requireProbeReachesEverySite is the guard's own positive control: it
// requires the probe emission to contain, at every width, the body that
// each of the three sites writes — so the walk above is looking at
// somewhere the defect could be rather than at nothing.
//
// Without this the guard's green is worth nothing, and the way it goes
// worthless is not exotic. Measuring this gap, the first mutation planted
// at the column site covered the integer widths only and reported the
// whole tree green — not because the site was guarded but because no
// fixture reached it at an integer width. A guard whose fixture stops
// reaching a site fails in exactly that shape, and reports success.
//
// What it looks for is deliberately blind to HOW each site decodes. An
// earlier version asked for the checked helper by name, which is the
// guard's own question, and that made this a second detector rather than
// a control: planting the real defect tripped this require — which calls
// FailNow — before the walk ran even once, so the battery scored three
// rows in which the thing under test never executed. A control that can
// pre-empt what it certifies is not certifying it.
func requireProbeReachesEverySite(t *testing.T, files map[string]string) {
	t.Helper()

	models, ok := files["probe/models.go"]
	require.True(t, ok, "the probe emitted no models.go, so neither model-side site is under the guard")
	queries, ok := files["probe/widths.cypher.go"]
	require.True(t, ok, "the probe emitted no query file, so the column site — the one this guard buys — is not under it")

	var missing []string
	for goType := range narrowingWidths {
		exported := strings.ToUpper(goType[:1]) + goType[1:]
		for _, site := range []struct{ what, in, want string }{
			{"entity property", models, `agtypeProperty(props, "req_` + goType + `", `},
			{"list element", models, "func agtypeListOf" + exported + "(raw []byte) ([]" + goType + ", error) {"},
			{"query column", queries, "func (q *queries) Column" + exported + "("},
		} {
			if !strings.Contains(site.in, site.want) {
				missing = append(missing, fmt.Sprintf("%s at %s: no %q", goType, site.what, site.want))
			}
		}
	}
	sort.Strings(missing)
	require.Empty(t, missing,
		"the probe does not reach every narrowing site at every width, so the guard walks bodies that "+
			"cannot hold the defect and passes having asserted nothing about them")
}
