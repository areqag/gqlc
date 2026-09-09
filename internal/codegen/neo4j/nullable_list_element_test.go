package neo4j_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/areqag/gqlc/internal/codegen"
	"github.com/areqag/gqlc/internal/codegen/neo4j"
	"github.com/areqag/gqlc/internal/codegen/typescan"
	"github.com/areqag/gqlc/internal/graph"
	"github.com/areqag/gqlc/internal/queryfile"
	"github.com/areqag/gqlc/internal/resolver"
)

// TestTypeMapPropertyStarsANullableListElement pins the element half of
// the pointer rule (bd gqlc-dxhwp, design gqlc-sokgc). CONTEXT.md
// "Nullable" says a nullable position is emitted as a pointer, and both
// backends already honour that at every position but one: the element of
// a list. `LIST<STRING>` declares elements that may be NULL and emitted
// `[]string`, so the first NULL failed the generated decode's type
// assertion at runtime.
//
// The rows below are the whole rule. A list whose element carries NOT
// NULL keeps its bare slice — that is the opt-out, and it is what makes
// the break bounded rather than fleet-wide. An element whose mapped type
// is `any` also stays bare: `any` already carries null as nil, and this
// backend deliberately does not walk `[]any` at all (isSliceType), so a
// star there would force a walk and add a second spelling of the same
// null.
//
// Property is asked directly rather than through Generate because it is
// the single source for EntityField.GoType, Param.GoType and a stored
// list's Row.GoType; a row here is a claim about all three at once.
func TestTypeMapPropertyStarsANullableListElement(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		pt   graph.PropertyType
		want string
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
			// The width whose Go carrier is itself a slice. It takes the
			// star like everything else: a nil []byte is indistinguishable
			// from an empty one, which is the ambiguity the star removes.
			name: "a nullable BYTES element takes the star",
			pt:   graph.ListOf(graph.TypeBytes, false),
			want: "[]*[]byte",
		},
		{
			// The gqlc-owned neutral carrier (ADR 0033), not dbtype. It
			// stars like any other value carrier.
			name: "a nullable temporal element takes the star",
			pt:   graph.ListOf(graph.TypeDate, false),
			want: "[]*Date",
		},
		{
			name: "a NOT NULL temporal element stays bare",
			pt:   graph.ListOf(graph.TypeDate, true),
			want: "[]Date",
		},
		{
			// THE CARVE-OUT. An element of no declared shape already
			// carries null as nil.
			name: "an ANY VALUE element stays bare",
			pt:   graph.ListOf(graph.TypeAnyPropertyValue, false),
			want: "[]any",
		},
		{
			// The bare LIST / ARRAY reaches the recursion, whose element is
			// TypeAnyPropertyValue with no NOT NULL. It is the carve-out's
			// other spelling, and the row exists so a change to the
			// carve-out cannot leave `[]any` reading `[]*any` through this
			// door while the row above still passes.
			name: "a bare LIST stays bare",
			pt:   graph.TypeList,
			want: "[]any",
		},
		{
			// Both stars compose, and each is decided by its own level's
			// qualifier. Storage of a nested list is refused by this
			// backend on a different axis (StorableProperty, ADR 0035); the
			// carrier text is still the table's answer, and a query-valued
			// nested list reaches it.
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
			name: "a NOT NULL outer element stars only the inner",
			pt:   graph.ListOf(graph.ListOf(graph.TypeInt16, false), true),
			want: "[][]*int16",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, ok := neo4j.TypeMap{}.Property(tt.pt)
			require.True(t, ok, "the table has a carrier for %s", tt.pt)
			require.Equal(t, tt.want, got)
		})
	}
}

// TestNullableListElementDecodeAdmitsNull is the runtime half of the
// same rule, and the one that answers the bead's own reproduction. The
// table above only says what the field is TYPED as; this says the
// generated walk can actually produce it.
//
// The emitted walk asserts each driver element to its carrier. On a NULL
// element the driver hands back a nil `any`, so `elem0.(string)` reports
// ok=false and the whole decode fails naming an element index —
// fail-closed, but for a value the schema explicitly permits. A nil arm
// is what stops that, and it has to come BEFORE the assertion or the
// assertion is still the thing that runs.
//
// Every row is its own one-property schema, so `wantNilArm: false` is a
// claim about that property's walk alone rather than about a total. That
// matters more than the convenience: a fix that stars EVERY element
// would admit null where the schema forbade it, silently substituting a
// zero value for a violation this backend exists to report, and only a
// per-property negative row catches it.
func TestNullableListElementDecodeAdmitsNull(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		pt         graph.PropertyType
		wantField  string
		wantNilArm bool
	}{
		{
			name:       "a nullable string element",
			pt:         graph.ListOf(graph.TypeString, false),
			wantField:  "Payload []*string",
			wantNilArm: true,
		},
		{
			name:       "a NOT NULL string element",
			pt:         graph.ListOf(graph.TypeString, true),
			wantField:  "Payload []string",
			wantNilArm: false,
		},
		{
			// A narrowed width: the driver hands back int64 and the walk
			// narrows to int32 before storing. The nil arm has to precede
			// the assertion AND the narrowing, and the address taken is the
			// narrowed local's, not the carrier's.
			name:       "a nullable narrowed element",
			pt:         graph.ListOf(graph.TypeInt32, false),
			wantField:  "Payload []*int32",
			wantNilArm: true,
		},
		{
			name:       "a NOT NULL narrowed element",
			pt:         graph.ListOf(graph.TypeInt32, true),
			wantField:  "Payload []int32",
			wantNilArm: false,
		},
		{
			// The carve-out, reached through emission rather than the type
			// table. `[]any` is not walked at all (isSliceType excludes
			// it), so there is no arm here to add and none to want.
			name:       "an ANY VALUE element",
			pt:         graph.ListOf(graph.TypeAnyPropertyValue, false),
			wantField:  "Payload []any",
			wantNilArm: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			models := payloadModels(t, tt.pt)
			require.Contains(t, models, tt.wantField)

			if tt.wantNilArm {
				require.Contains(t, models, "if elem0 == nil {",
					"the walk has no nil arm, so a NULL element still reaches the type assertion")
				return
			}
			require.NotContains(t, models, "elem0 == nil",
				"this element carries NOT NULL, so admitting null substitutes a zero value for a violation")
		})
	}
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
// sides discard element-position nullability in the same way and so
// agree on the wrong answer. What it is for is the change that stops
// them discarding it (bd gqlc-dxhwp, design item 3): the star has to be
// applied on both routes, and applying it on one is exactly the mistake
// this catches — a struct field of one type filled by a decoder
// producing another.
//
// Both refusal channels count as agreement, deliberately: a width one
// route carries and the other refuses is the same drift wearing a
// different face.
//
// The width goes on the COLUMN and never on the schema, so Phase Z's
// storage axis is not consulted and the row reads the carrier axis
// alone — which is what the invariant is about. That is also why a
// nested list is on this table for a backend whose store refuses one
// (ADR 0035): its carrier text is still an answer, and a query-valued
// nested list still reaches it.
func TestListColumnTextAgreesWithItsElementPlan(t *testing.T) {
	t.Parallel()

	widths, err := typescan.PropertyTypes(graphPropertyTypeSource)
	require.NoError(t, err)
	require.NotEmpty(t, widths, "the width vocabulary read empty, so this test ranged over nothing")

	tm := neo4j.TypeMap{}
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

				prepared, prepErr := codegen.Prepare(listColumnInput(list), tm, "probe")
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

// listColumnInput is a one-query batch projecting a single column of the
// given list width over a schema that does not declare it. The width is
// on the column alone so that the storage axis never sees it: this
// batch's business is the carrier axis.
func listColumnInput(list graph.PropertyType) codegen.Input {
	return codegen.Input{
		Schema: schemaWithPayload(graph.TypeInt64),
		Queries: []codegen.NamedQuery{{
			Name:        "Xs",
			Cardinality: queryfile.CardinalityMany,
			SourceFile:  "q.cypher",
			SourceText:  "MATCH (b:Blob) RETURN b.payload AS xs\n",
			Validated: resolver.ValidatedQuery{Columns: []resolver.Column{{
				Name: "xs",
				Type: resolver.ResolvedProperty{Type: list},
			}}},
		}},
	}
}

// payloadModels emits models.go for the one-property schema
// schemaWithPayload builds, so each row's assertions are over exactly
// one property's walk. The property is NOT NULL at the whole-value
// position, which keeps the outer star off the field and leaves the
// element star the only thing the row can be reading.
func payloadModels(t *testing.T, pt graph.PropertyType) string {
	t.Helper()

	files, err := neo4j.New().Generate(codegen.Input{Schema: schemaWithPayload(pt)})
	require.NoError(t, err)

	for _, f := range files {
		if f.Path == "models.go" {
			return string(f.Contents)
		}
	}
	require.FailNow(t, "no models.go in the emission")
	return ""
}
