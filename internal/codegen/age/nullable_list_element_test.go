package age_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/areqag/gqlc/internal/codegen"
	"github.com/areqag/gqlc/internal/codegen/age"
	"github.com/areqag/gqlc/internal/codegen/typescan"
	"github.com/areqag/gqlc/internal/graph"
	"github.com/areqag/gqlc/internal/resolver"
)

// TestTypeMapPropertyStarsANullableListElement is this backend's half of
// the element pointer rule (bd gqlc-dxhwp, design gqlc-sokgc). Both
// backends discarded element-position nullability in the same place and
// emitted the identical wrong signature, so both move together — a fix
// that moves one is a divergence rather than a fix, and the neo4j table
// of the same name is this one's mirror.
//
// The rows that differ from neo4j's are this backend's own refusals, and
// they are here because the star must not reach them. A zoned width is
// refused as a list element at every depth (the offset rides a sidecar
// named after the property, and a list has one name for all its
// elements), and carriesZone decides that by EXACT equality on the
// carrier text — so a star applied before the refusal turns "Time" into
// "*Time", matches nothing, and admits a width this backend has nowhere
// to put the zone of.
func TestTypeMapPropertyStarsANullableListElement(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		pt   graph.PropertyType
		want string
		// wantRefused is the zoned-list refusal, which answers
		// ("", false) and is not a carrier claim at all.
		wantRefused bool
	}{
		{
			name: "a nullable scalar element takes the star",
			pt:   graph.ListOf(graph.TypeInt64, false),
			want: "[]*int64",
		},
		{
			name: "a NOT NULL element is the opt-out and stays bare",
			pt:   graph.ListOf(graph.TypeInt64, true),
			want: "[]int64",
		},
		{
			name: "a nullable string element takes the star",
			pt:   graph.ListOf(graph.TypeString, false),
			want: "[]*string",
		},
		{
			name: "a NOT NULL string element stays bare",
			pt:   graph.ListOf(graph.TypeString, true),
			want: "[]string",
		},
		{
			// The unzoned temporal carriers ride a list on the ordinary
			// rule, so they take the star like any other value carrier.
			name: "a nullable DATE element takes the star",
			pt:   graph.ListOf(graph.TypeDate, false),
			want: "[]*Date",
		},
		{
			name: "a NOT NULL DATE element stays bare",
			pt:   graph.ListOf(graph.TypeDate, true),
			want: "[]Date",
		},
		{
			name: "a nullable LOCAL TIME element takes the star",
			pt:   graph.ListOf(graph.TypeLocalTime, false),
			want: "[]*LocalTime",
		},
		{
			// THE CARVE-OUT. An element of no declared shape already
			// carries null as nil.
			name: "an ANY VALUE element stays bare",
			pt:   graph.ListOf(graph.TypeAnyPropertyValue, false),
			want: "[]any",
		},
		{
			name: "a bare LIST stays bare",
			pt:   graph.TypeList,
			want: "[]any",
		},
		{
			// agtype nests without limit, so this backend carries the
			// nested list neo4j refuses to store (ADR 0035). Both stars
			// compose, each decided by its own level's qualifier.
			name: "nested lists star at each level independently",
			pt:   graph.ListOf(graph.ListOf(graph.TypeFloat32, false), false),
			want: "[]*[]*float32",
		},
		{
			name: "a NOT NULL inner element stars only the outer",
			pt:   graph.ListOf(graph.ListOf(graph.TypeInt16, true), false),
			want: "[]*[]int16",
		},
		{
			// THE REFUSAL THE STAR MUST NOT REACH. A zoned element is
			// refused whether or not it is nullable, and the star is not
			// what decides it.
			name:        "a nullable TIMESTAMP element is still refused",
			pt:          graph.ListOf(graph.TypeTimestamp, false),
			wantRefused: true,
		},
		{
			name:        "a NOT NULL TIMESTAMP element is still refused",
			pt:          graph.ListOf(graph.TypeTimestamp, true),
			wantRefused: true,
		},
		{
			name:        "a nullable TIME element is still refused",
			pt:          graph.ListOf(graph.TypeTime, false),
			wantRefused: true,
		},
		{
			// A zoned width one level down is refused at that level and
			// the refusal propagates, so the outer star never gets to
			// compose over it.
			name:        "a zoned element nested one level deep is still refused",
			pt:          graph.ListOf(graph.ListOf(graph.TypeTimestamp, false), false),
			wantRefused: true,
		},
		{
			// A width with no carrier on this backend at all. Refused on
			// the carrier axis, and the star must not turn that into an
			// answer either.
			name:        "a nullable BYTES element is still refused",
			pt:          graph.ListOf(graph.TypeBytes, false),
			wantRefused: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, ok := age.TypeMap{}.Property(tt.pt)
			if tt.wantRefused {
				require.False(t, ok, "%s must stay refused, and got %q", tt.pt, got)
				require.Empty(t, got, "a refusal names no carrier")
				return
			}
			require.True(t, ok, "the table has a carrier for %s", tt.pt)
			require.Equal(t, tt.want, got)
		})
	}
}

// TestNullableListElementDecodesThroughANullableElemWrapper is the
// emission half, and the one the bead's reproduction lands on. It
// follows TestListExpressionColumnDecodesThroughTheSliceWrapper: the
// gate above only says what text the table answers, and this says the
// emitted package actually carries a decoder that produces it.
//
// Three things have to move together for a nullable element to decode,
// and each is its own assertion because each fails differently:
//
//   - the named wrapper's IDENTIFIER. listHelperName mangles a slice type
//     into a helper name, and a `*` segment spelled literally yields
//     `agtypeListOfNullableString`'s invalid cousin `agtypeListOf*String`
//     — a package that does not parse, let alone build.
//   - the wrapper's SIGNATURE, which is where the star has to surface.
//   - the ELEMENT DECODER it binds. `agtypeString` on a literal `null`
//     returns an error, which is the runtime failure the bead reports;
//     the combinator is what maps `null` to a nil pointer instead.
//
// Every row is its own one-property schema, so a NOT NULL row is a claim
// about that property's own helper and not about a total.
func TestNullableListElementDecodesThroughANullableElemWrapper(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		pt         graph.PropertyType
		wantField  string
		wantHelper string
		wantSig    string
	}{
		{
			name:       "a nullable string element",
			pt:         graph.ListOf(graph.TypeString, false),
			wantField:  "Payload []*string",
			wantHelper: "agtypeListOfNullableString",
			wantSig:    "func agtypeListOfNullableString(raw []byte) ([]*string, error) {",
		},
		{
			name:       "a NOT NULL string element",
			pt:         graph.ListOf(graph.TypeString, true),
			wantField:  "Payload []string",
			wantHelper: "agtypeListOfString",
			wantSig:    "func agtypeListOfString(raw []byte) ([]string, error) {",
		},
		{
			name:       "a nullable narrowed element",
			pt:         graph.ListOf(graph.TypeInt32, false),
			wantField:  "Payload []*int32",
			wantHelper: "agtypeListOfNullableInt32",
			wantSig:    "func agtypeListOfNullableInt32(raw []byte) ([]*int32, error) {",
		},
		{
			name:       "a NOT NULL narrowed element",
			pt:         graph.ListOf(graph.TypeInt32, true),
			wantField:  "Payload []int32",
			wantHelper: "agtypeListOfInt32",
			wantSig:    "func agtypeListOfInt32(raw []byte) ([]int32, error) {",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			models := payloadModels(t, tt.pt)
			require.Contains(t, models, tt.wantField)
			require.Contains(t, models, tt.wantSig,
				"the named list wrapper for %s is not emitted with that signature", tt.pt)
			require.Contains(t, models, tt.wantHelper+"(",
				"the property does not decode through its named wrapper")
		})
	}
}

// TestNullableElemCombinatorMapsNullToANilPointer pins the piece the
// wrapper is built on. agtypeList walks the elements and hands each raw
// span to an element decoder; every existing element decoder REFUSES the
// literal `null`, which is exactly the runtime failure this bead
// reports. The combinator is the one place that answer changes, and it
// has to answer for a nil pointer rather than for a zero value — a `null`
// decoded to "" is the corruption the fail-closed behaviour was at least
// protecting against.
func TestNullableElemCombinatorMapsNullToANilPointer(t *testing.T) {
	t.Parallel()

	models := payloadModels(t, graph.ListOf(graph.TypeString, false))

	require.Contains(t, models, "func agtypeNullableElem[T any](",
		"the element-decoder combinator is not emitted")
	require.Contains(t, models, "return agtypeList(raw, agtypeNullableElem(agtypeString))",
		"the nullable wrapper does not bind its element decoder through the combinator")
}

// TestListColumnTextAgreesWithItsElementPlan pins the seam between the
// two answers a list width gets, which are computed by different code
// and are required to be the same answer.
//
// A list column's Row.GoType is built as `"[]" + plan.GoType` from
// buildListElemPlan's walk over the resolved element. Every OTHER
// position carrying the same width — an entity field, a parameter —
// takes its text from TypeMap.Property's own recursion. Nothing makes
// the two agree; they agree because two separate pieces of code make the
// same decision each time the width vocabulary or the emission rules
// move.
//
// This is a FENCE and not a witness. It holds on master, because both
// routes discard element-position nullability in the same way and so
// agree on the wrong answer. What it is for is the change that stops
// them discarding it (bd gqlc-dxhwp, design item 3): applying the star
// on one route only emits a struct field of one type filled by a decoder
// producing another, and this is what catches that.
//
// It is worth having on this backend in its own right rather than as a
// copy of neo4j's, because this table has a refusal the other has not:
// a zoned element is refused as a list element at every depth, and
// carriesZone decides it by EXACT equality on the carrier text. So this
// sweep asks a question neo4j's cannot — whether the column plan refuses
// the same zoned widths the table does, once a star is in play that
// could make the text no longer match.
func TestListColumnTextAgreesWithItsElementPlan(t *testing.T) {
	t.Parallel()

	widths, err := typescan.PropertyTypes(graphPropertyTypeSource)
	require.NoError(t, err)
	require.NotEmpty(t, widths, "the width vocabulary read empty, so this test ranged over nothing")

	tm := age.TypeMap{}
	for width, constName := range widths {
		for _, elemNotNull := range []bool{false, true} {
			name := constName
			if elemNotNull {
				name += "_NOT_NULL"
			}
			t.Run(name, func(t *testing.T) {
				t.Parallel()

				list := graph.ListOf(width, elemNotNull)
				text, carried := tm.Property(list)

				in := listColumnInput(t, resolver.Column{
					Name: "xs",
					Type: resolver.ResolvedProperty{Type: list},
				})
				prepared, prepErr := codegen.Prepare(in, tm, "probe")

				if !carried {
					require.Error(t, prepErr,
						"the type table refuses %s as a carrier, but the column plan builds one for it — "+
							"so a decode exists for a field no other position can be emitted with", list)
					return
				}
				require.NoError(t, prepErr,
					"the type table carries %s as %q, but the column plan refuses it — so an entity field "+
						"or parameter of that text has no matching decode", list, text)

				require.Len(t, prepared.Queries, 1)
				require.Len(t, prepared.Queries[0].RowFields, 1)
				require.Equal(t, text, prepared.Queries[0].RowFields[0].GoType,
					"the carrier text and the column plan disagree about %s", list)
			})
		}
	}
}

// payloadModels emits models.go for the one-property schema
// schemaWithPayload builds, so each row's assertions are over exactly
// one property. The property is NOT NULL at the whole-value position,
// which keeps agtypeNullableProperty and the outer star out of the way
// and leaves the element star the only thing a row can be reading.
func payloadModels(t *testing.T, pt graph.PropertyType) string {
	t.Helper()

	files, err := age.New().Generate(codegen.Input{Schema: schemaWithPayload(pt)})
	require.NoError(t, err)

	for _, f := range files {
		if f.Path == "models.go" {
			return string(f.Contents)
		}
	}
	require.FailNow(t, "no models.go in the emission")
	return ""
}
