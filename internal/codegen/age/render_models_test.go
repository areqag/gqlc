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

// propertyRow is one row of the three sweeps below: a carrier and the
// width it came from. The width is what helpers.need dispatches the
// record arm on, and it is the zero value for every derived row —
// which is what those rows carried implicitly before records existed,
// since a zero width is neither a list nor a record.
type propertyRow struct {
	goType string
	width  graph.PropertyType
}

// propertyRows is propertyCarriers plus the record rows that walk cannot
// reach.
//
// The derivation reads the texts typeMap.Property returns as string
// LITERALS. A record's carrier is not one: it is composed per encoding
// from the field list, exactly as the list arm's `"[]" + elem` is, so no
// record row can appear in the derived population however many records
// the table learns to carry. Hand-writing the record rows here is the
// same concession the doc above records for lists — and unlike a list,
// it is not covered by peeling to an element, because what a record row
// asserts is about helpers named from the ENCODING.
//
// The carriers are asked of the table rather than spelled, so a row is
// dropped rather than faked if this backend stops carrying the width.
func propertyRows(t *testing.T) []propertyRow {
	t.Helper()

	var rows []propertyRow
	for _, goType := range propertyCarriers(t) {
		rows = append(rows, propertyRow{goType: goType})
	}
	for _, width := range []graph.PropertyType{
		recordWidth, graph.ListOf(recordWidth, false), graph.TypeAnyRecord,
	} {
		goType, ok := age.TypeMap{}.Property(width)
		require.True(t, ok, "this backend no longer carries %s, so the record rows are stale", width)
		rows = append(rows, propertyRow{goType: goType, width: width})
	}
	return rows
}

// recordWidth is the record these sweeps carry. Its fields are chosen so
// that the row is not a restatement of the scalar rows above it:
//
//   - the DATE marks a helper the record's own decoder names, so a
//     record that marked nothing but itself renders a file naming
//     agtypeDate without declaring it, and the closure sweep says so;
//   - the nullable field is the arm that writes no key on the encode
//     side and reads absence as null on the decode side, and it is the
//     only one of the two arms a record of all-NOT NULL fields reaches;
//   - the field names are not the Go field names they mangle to, so an
//     emission that spelled the Go name on the wire is visible.
//
// It is a package-level var rather than a literal in each sweep because
// two spellings of "the record" would be two encodings, and the helper
// suffix is derived from the encoding.
var recordWidth = graph.RecordOf([]graph.RecordField{
	{Name: "zip_code", Type: graph.TypeInt32, NotNull: true},
	{Name: "seen_on", Type: graph.TypeDate, NotNull: true},
	{Name: "note", Type: graph.TypeString},
})

// TestZoneIsMarkedOnlyBesideTheInstant pins the invariant importsTime
// leans on. The sidecar read is marked on an entity field whose Go type
// is the instant, and that same field marks the instant decode; a zone
// marked without one would answer importsTime false and leave models.go
// naming time with no import for it.
//
// The rows are propertyRows, so a carrier the property table gains is a
// row this sweep gains.
func TestZoneIsMarkedOnlyBesideTheInstant(t *testing.T) {
	sawInstant := false
	for _, row := range propertyRows(t) {
		var h age.Helpers
		h.ForEntities([]age.WiredEntity{{Entity: codegen.Entity{
			Name:   "E",
			Fields: []codegen.EntityField{{PropName: "p", Field: "P", GoType: row.goType, Width: row.width}},
		}}})

		if h.Zone() {
			require.True(t, h.Instant(), "%s marks the sidecar read with no instant beside it", row.goType)
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
	for _, row := range propertyRows(t) {
		t.Run(row.goType, func(t *testing.T) {
			entities := []age.WiredEntity{age.WiredEntity{
				Entity: codegen.Entity{Name: "E", Fields: []codegen.EntityField{{PropName: "p", Field: "P", GoType: row.goType, Width: row.width}}},
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
//
// The two DIRECTIONS are separate batches for the same reason the widths
// are. A batch that both reads and binds the width supplies each
// direction's helpers to the other, so it cannot see an emission that
// writes one direction's helper on the other direction's evidence — which
// is precisely the hole a record opened: a record READ but never bound
// emitted an encoder naming agtypeDateText, a helper only the bind path
// marks. Read-only and bind-only are the batches a real schema has, and
// they are the two that expose it.
func TestEmittedHelpersAreClosedOverWhatTheyCall(t *testing.T) {
	for _, row := range propertyRows(t) {
		for _, nullable := range []bool{false, true} {
			e := age.WiredEntity{
				Entity: codegen.Entity{
					Name:   "E",
					Kind:   codegen.EntityNode,
					Fields: []codegen.EntityField{{PropName: "p", Field: "P", GoType: row.goType, Width: row.width, Nullable: nullable}},
				},
			}.WithLabels("E", age.VertexAnnotation)
			param := codegen.Param{RawName: "p", Field: "P", GoType: row.goType, Width: row.width, Nullable: nullable}

			for _, d := range []struct {
				name string
				read bool
				bind bool
			}{
				{"read only", true, false},
				{"bind only", false, true},
				{"both", true, true},
			} {
				var h age.Helpers
				var entities []age.WiredEntity
				if d.read {
					entities = []age.WiredEntity{e}
					h.ForEntities(entities)
				}
				if d.bind {
					age.HelpersForParams(&h, []codegen.Param{param})
				}

				src := age.RenderModels("models", entities, h)
				require.Empty(t, undeclaredHelperIdents(t, src),
					"a %s batch of one %s property (nullable=%t) names an agtype helper it does not declare, so the emitted package does not compile",
					d.name, row.goType, nullable)
			}
		}
	}
}

// undeclaredHelperIdents parses one emitted models.go and returns every
// generated helper it names without declaring. A helper reaches the list
// whether it is called outright or passed by value as a decoder
// argument, because either one is a reference the compiler resolves.
//
// The helper families are named rather than "every undeclared identifier"
// because the file legitimately names identifiers it does not declare —
// fmt, json, the language's own builtins — and resolving those is the
// compiler's job, not this walk's.
func undeclaredHelperIdents(t *testing.T, src []byte) []string {
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
				switch spec := spec.(type) {
				case *ast.ValueSpec:
					for _, name := range spec.Names {
						declared[name.Name] = struct{}{}
					}
				case *ast.TypeSpec:
					// A record's carrier is an ALIAS, so it is a type
					// declaration and not a func. A walk reading only funcs
					// reports every alias the helper signatures name as
					// missing.
					declared[spec.Name.Name] = struct{}{}
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
		if _, isDeclared := declared[id.Name]; isDeclared || !isEmittedHelper(id.Name) {
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

// isEmittedHelper reports whether a name belongs to one of the families
// renderModels emits on demand, which are exactly the names that can go
// missing: everything else the file spells is either declared beside its
// use or is the standard library's.
//
// The record families are here and not only "agtype" because a record's
// carrier and its helper pair are named from the ENCODING's digest —
// record<h>, decodeRecord<h>, encodeRecord<h> — so a walk keyed on the
// agtype prefix alone cannot see a record helper the emission names and
// does not declare. Measured: mutating the width descent in helpers.need
// so a list element's record went unmarked left the emission naming
// decodeRecord<h> with no declaration, and this sweep called it a pass.
//
// An ENTITY decoder is decode<Entity>, which collides with this family
// only for an entity actually named Record-something — and that one is
// declared by writeEntities, so it never reaches the test.
func isEmittedHelper(name string) bool {
	for _, prefix := range []string{"agtype", "record", "decodeRecord", "encodeRecord"} {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}
