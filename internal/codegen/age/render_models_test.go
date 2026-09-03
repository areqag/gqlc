package age_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/areqag/gqlc/internal/codegen"
	"github.com/areqag/gqlc/internal/codegen/age"
	"github.com/areqag/gqlc/internal/graph"
)

// propertyCarriers is every Go type text the property table names, read
// out of the package's AST rather than written out here — so a carrier
// the table gains is a row the sweeps below gain, with no edit to this
// file.
//
// It was a hand-written list of eleven graph.PropertyType values until bd
// gqlc-4lkv, under a doc comment that claimed this derivation and did not
// have it. Those eleven reached nine of the twenty-one carriers the table
// names. TIME was among the twelve with no row, and it is the second of
// the two carriers that keep a zone — the subject of the first sweep
// below.
//
// The sweeps range over CARRIERS and not over widths because a carrier is
// the whole of what any of them uses a width for: each maps its width
// through the table and then names only the Go type that came back.
// Ranging over the carrier is that same population one step shorter, and
// it is the population typeTableGoTypes can derive; a width is not, since
// graph.PropertyType is an open string type with no set to walk.
//
// What this inherits is that walk's reach, which is narrower than the
// table's output: the texts are the ones the table returns as string
// LITERALS, so the list arm's composed `"[]" + elem` contributes nothing
// and LIST<ANY>'s own literal is the only list carrier here. That is why
// the two hand-written list rows are gone rather than carried over.
// Nothing is lost to these three invariants by that: helpers.need peels a
// slice to its element before marking anything, so what a []Date row
// asserted about the time import is what the Date row asserts.
func propertyCarriers(t *testing.T) []string {
	t.Helper()

	carriers := typeTableGoTypes(t)["Property"]
	require.NotEmpty(t, carriers,
		"the walk read no Go type out of typeMap.Property, so every sweep keyed on this ranges over "+
			"nothing and passes having asserted nothing")
	return carriers
}

// TestZoneIsMarkedOnlyBesideTheInstant pins the invariant importsTime
// leans on. The sidecar read is marked on an entity field whose Go type
// is the instant, and that same field marks the instant decode; a zone
// marked without one would answer importsTime false and leave models.go
// naming time with no import for it.
//
// The rows are propertyCarriers, so a carrier the property table gains is
// a row this sweep gains.
func TestZoneIsMarkedOnlyBesideTheInstant(t *testing.T) {
	sawInstant := false
	for _, goType := range propertyCarriers(t) {
		var h age.Helpers
		h.ForEntities([]age.WiredEntity{{Entity: codegen.Entity{
			Name:   "E",
			Fields: []codegen.EntityField{{PropName: "p", Field: "P", GoType: goType}},
		}}})

		if h.Zone() {
			require.True(t, h.Instant(), "%s marks the sidecar read with no instant beside it", goType)
			sawInstant = true
		}
	}
	require.True(t, sawInstant, "no carrier in the sweep marked the sidecar read, so the invariant went untested")
}

// TestImportsTimeAgreesWithTheEmittedFile is the check that keeps the
// time import honest against the helpers themselves rather than against
// a list of widths written beside them. models.go is rendered for a batch
// carrying one width, and the answer importsTime gave is required to
// match whether the rendered bytes actually spell a time qualifier.
//
// Both directions matter and both are cheap to get wrong. A width that
// marks a helper naming time without marking importsTime emits a file
// that does not compile; one that marks importsTime without needing it
// emits an unused import, which does not compile either. LOCAL TIME and
// DURATION are the widths that make this more than a restatement: they
// are temporals whose helpers do int64 arithmetic and name no time at
// all, so a gate written as "the batch carries a temporal" is red here.
func TestImportsTimeAgreesWithTheEmittedFile(t *testing.T) {
	for _, goType := range propertyCarriers(t) {
		t.Run(goType, func(t *testing.T) {
			entities := []age.WiredEntity{age.WiredEntity{
				Entity: codegen.Entity{Name: "E", Fields: []codegen.EntityField{{PropName: "p", Field: "P", GoType: goType}}},
			}.WithLabels("E", age.VertexAnnotation)}
			var h age.Helpers
			h.ForEntities(entities)

			src := string(age.RenderModels("m", entities, h))
			// The import line itself is what importsTime writes, so the
			// witness has to be a use of the package and not that line.
			names := strings.Contains(src, "time.Time") || strings.Contains(src, "time.Parse") ||
				strings.Contains(src, "time.Date") || strings.Contains(src, "time.UnixMicro") ||
				strings.Contains(src, "time.FixedZone")
			require.Equal(t, names, h.ImportsTime(),
				"importsTime()=%v but the rendered models.go %s time", h.ImportsTime(),
				map[bool]string{true: "names", false: "does not name"}[names])
		})
	}
}

// TestTheWireLabelsReachTheEmittedDecoder pins that the two labels an
// entity is wired with are the two the generated decoder enforces: the
// annotation is what agtypeEntity strips, and the wire label is what the
// equality check beneath it demands.
//
// It exists as the positive control for the WithLabels bridge. Every
// other test that wires an entity asserts on what its FIELDS render to,
// so a WithLabels that dropped both labels on the floor left those tests
// green — the emitted decoder then strips the empty annotation and
// demands the empty label, and nothing in the package noticed. The label
// here is deliberately not the entity's name, so a decoder that echoed
// the name instead of the label would not satisfy it either.
func TestTheWireLabelsReachTheEmittedDecoder(t *testing.T) {
	goType, ok := age.TypeMap{}.Property(graph.TypeString)
	require.True(t, ok)

	e := age.WiredEntity{Entity: codegen.Entity{
		Name:   "E",
		Kind:   codegen.EntityNode,
		Fields: []codegen.EntityField{{PropName: "p", Field: "P", GoType: goType}},
	}}.WithLabels("Widget", age.VertexAnnotation)

	var h age.Helpers
	h.ForEntities([]age.WiredEntity{e})
	src := string(age.RenderModels("models", []age.WiredEntity{e}, h))

	require.Contains(t, src, strconv.Quote(age.VertexAnnotation),
		"the emitted decoder does not strip the annotation the entity was wired with")
	require.Contains(t, src, strconv.Quote("Widget"),
		"the emitted decoder does not demand the wire label the entity was wired with")
}

// TestEmittedHelpersAreClosedOverWhatTheyCall renders models.go for a
// batch carrying one width and nothing else, and requires every agtype
// helper the file names to be one the file declares.
//
// A helper is emitted only when the batch marks it, and one helper is
// allowed to be written in terms of another — agtypeInstant reads its
// micros through agtypeInt64 — so a width that marks the caller without
// also marking the callee emits a package that does not compile. Every
// schema fixture in this package carries several widths at once, so each
// of them supplies the callee by some other route and none of them can
// see that hole; a batch of exactly one width is what exposes it.
func TestEmittedHelpersAreClosedOverWhatTheyCall(t *testing.T) {
	for _, goType := range propertyCarriers(t) {
		for _, nullable := range []bool{false, true} {
			e := age.WiredEntity{
				Entity: codegen.Entity{
					Name:   "E",
					Kind:   codegen.EntityNode,
					Fields: []codegen.EntityField{{PropName: "p", Field: "P", GoType: goType, Nullable: nullable}},
				},
			}.WithLabels("E", age.VertexAnnotation)
			var h age.Helpers
			h.ForEntities([]age.WiredEntity{e})
			age.HelpersForParams(&h, []codegen.Param{{RawName: "p", Field: "P", GoType: goType, Nullable: nullable}})

			src := age.RenderModels("models", []age.WiredEntity{e}, h)
			require.Empty(t, undeclaredAgtypeIdents(t, src),
				"a batch of one %s property (nullable=%t) names an agtype helper it does not declare, so the emitted package does not compile",
				goType, nullable)
		}
	}
}

// undeclaredAgtypeIdents parses one emitted models.go and returns every
// agtype helper it names without declaring. A helper reaches the list
// whether it is called outright or passed by value as a decoder
// argument, because either one is a reference the compiler resolves.
func undeclaredAgtypeIdents(t *testing.T, src []byte) []string {
	t.Helper()

	file, err := parser.ParseFile(token.NewFileSet(), "models.go", src, parser.SkipObjectResolution)
	require.NoError(t, err, "the emitted models.go does not parse:\n%s", src)

	declared := map[string]struct{}{}
	for _, d := range file.Decls {
		switch d := d.(type) {
		case *ast.FuncDecl:
			if d.Recv == nil {
				declared[d.Name.Name] = struct{}{}
			}
		case *ast.GenDecl:
			// A helper's constants are declarations the compiler resolves
			// just as it resolves the helpers — the date layout is one —
			// so a walk that reads only funcs reports them all missing.
			for _, spec := range d.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for _, name := range vs.Names {
					declared[name.Name] = struct{}{}
				}
			}
		}
	}

	var missing []string
	seen := map[string]struct{}{}
	record := func(n ast.Node) bool {
		id, ok := n.(*ast.Ident)
		if !ok {
			return true
		}
		// An identifier is a leaf, so the walk stops here either way.
		if _, isDeclared := declared[id.Name]; isDeclared || !strings.HasPrefix(id.Name, "agtype") {
			return false
		}
		if _, dup := seen[id.Name]; !dup {
			seen[id.Name] = struct{}{}
			missing = append(missing, id.Name)
		}
		return false
	}
	ast.Inspect(file, func(n ast.Node) bool {
		fn, ok := n.(*ast.FuncDecl)
		if !ok {
			return true
		}
		// A declaration's own name is not a reference to it, so the walk
		// skips the name and descends into the signature and the body.
		ast.Inspect(fn.Type, record)
		if fn.Body != nil {
			ast.Inspect(fn.Body, record)
		}
		return false
	})
	return missing
}
