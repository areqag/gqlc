package age_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/areqag/gqlc/internal/codegen"
	"github.com/areqag/gqlc/internal/codegen/age"
	"github.com/areqag/gqlc/internal/graph"
	"github.com/areqag/gqlc/internal/resolver"
	"github.com/areqag/gqlc/internal/schema"
)

// This file is package age_test rather than package age, and the choice is
// what the rows measure. unservedColumn and unservedParam are unexported, so
// a test inside the package could hand either a value directly; every row
// here arrives the way a consumer's would, through an assembled
// codegen.Input handed to the exported age.New().Generate. That is the seam
// gqlc-aefe measured the faults on, and it is what says the guard sits on a
// path production reaches rather than on one only a test can enter.
//
// The counterpart at internal/codegen/unmatched_resolvedtype_test.go makes
// the same choice for internal/codegen's own fail-sites, and for the same
// reason.

// nilEmbeddedIface satisfies resolver.ResolvedType by embedding the interface
// rather than a variant. Go promotes an embedded interface's whole method
// set, the unexported marker included, so the zero value satisfies
// resolver.ResolvedType from this package while naming no variant at all, and
// String() on it dispatches through the nil embedded interface and faults.
//
// It is one of the two shapes gqlc-aefe names. Neither of the two cheap
// guards sees it: the outer interface holds a non-nil nilEmbeddedIface, so a
// `t == nil` test does not fire, and its reflect.Kind is Struct, so a
// nil-pointer check does not either.
type nilEmbeddedIface struct{ resolver.ResolvedType }

// nilEmbeddedNode satisfies resolver.ResolvedType through an embedded
// *ResolvedNode. The pointer's method set carries ResolvedNode's value
// methods, marker included, so the zero value satisfies the interface with a
// nil embedded pointer and String() reaches ResolvedNode.String through it.
//
// gqlc-aefe does not name this shape. It is here because the guard the bead
// asks for enumerates no shape, so it answers for shapes the bead did not
// name, and a row that only covered the two named ones would not show that.
type nilEmbeddedNode struct{ *resolver.ResolvedNode }

// answeringEmbedder is the same construction with a value field, so its
// promoted String() returns ResolvedNode's wire tag instead of faulting. It
// is one of the two rows in the ALLOW half below.
type answeringEmbedder struct{ resolver.ResolvedNode }

var (
	_ resolver.ResolvedType = nilEmbeddedIface{}
	_ resolver.ResolvedType = nilEmbeddedNode{}
	_ resolver.ResolvedType = answeringEmbedder{}
)

// unservedProbeInput wraps one query in the batch a consumer hands a backend.
// The schema is a placeholder: rejectUnservedQueries runs ahead of
// codegen.Prepare (generate.go), and it reads the queries alone, so no row
// below depends on what this schema declares.
func unservedProbeInput(q codegen.NamedQuery) codegen.Input {
	return codegen.Input{
		Schema:  schema.Schema{Name: "Probe"},
		Queries: []codegen.NamedQuery{q},
	}
}

// generateUnserved runs the batch through the exported seam and returns the
// refusal. It takes no recover of its own: a fault inside Generate has to
// surface as this test's own panic, naming the site, which is what each
// faulting row below reported before the guard landed.
func generateUnserved(t *testing.T, q codegen.NamedQuery) error {
	t.Helper()
	files, err := age.New().Generate(unservedProbeInput(q))
	require.Nil(t, files, "a refused batch must emit nothing")
	require.Error(t, err)
	return err
}

// unservedColumnProbe projects one column carrying t. readQuery's source text
// spells none of the constructs rejectDialectGaps reads, so the column gate
// does not yield and the reason reaching the author is this column's.
func unservedColumnProbe(t resolver.ResolvedType) codegen.NamedQuery {
	return readQuery("Fetch", resolver.Column{Name: "n", Type: t})
}

// unservedParamProbe binds one parameter carrying t beside a served scalar
// column. The served column is what makes a reason from this query the
// parameter's: unservedReason reads the columns first and returns on the
// first refusal, so a column that also refused would answer instead.
func unservedParamProbe(t resolver.ResolvedType) codegen.NamedQuery {
	q := readQuery("Bind", scalarColumn("p.name", graph.TypeString))
	q.Validated.Parameters = []resolver.ResolvedParameter{{Name: "p", Type: t}}
	return q
}

// faultingShapes are values that satisfy resolver.ResolvedType and whose
// String() faults, paired with the name the refusal renders for each.
//
// The first two are gqlc-aefe's own: a column type left nil, and a struct
// embedding resolver.ResolvedType and so carrying no variant to dispatch on.
// The other two are not named in the bead and are here to show the guard is
// not written against a list: it enumerates no shape, so it answers for these
// as well.
//
// What the names are is decided by fmt's %T, which reads the dynamic type
// through reflection and dispatches no method. The nil interface has no
// dynamic type, and %T renders that as "<nil>".
//
// This is a witness list and not a claim about the set. Whether a value
// faults is a fact about the String() it ends up dispatching, and
// resolver.ResolvedType is satisfiable from any package in the module through
// promotion, so no list written here closes it.
func faultingShapes() []struct {
	name string
	typ  resolver.ResolvedType
	want string
} {
	return []struct {
		name string
		typ  resolver.ResolvedType
		want string
	}{
		{"nil interface", nil, "<nil>"},
		{"struct embedding the interface", nilEmbeddedIface{}, "age_test.nilEmbeddedIface"},
		{"struct embedding a nil pointer to a variant", nilEmbeddedNode{}, "age_test.nilEmbeddedNode"},
		{"typed-nil pointer to a variant", (*resolver.ResolvedNode)(nil), "*resolver.ResolvedNode"},
	}
}

// TestUnservedRefusalNamesATypeThatCannotNameItself is gqlc-aefe's
// measurement. This backend's unserved-query gate names the offending type by
// asking it — unservedColumn's fall-through and unservedParam's non-property
// arm both render one — and asking is a call into code this package does not
// own, made on a value chosen because no arm matched it. Each row below
// panicked inside Generate rather than returning a refusal before the guard.
//
// The gate is reached ahead of codegen.Prepare (generate.go), so
// resolvedTypeName's guard on Prepare's own fail-sites does not stand in
// front of these two: this is the first render of the arriving type in the
// run, not the second.
//
// Reverting either render to a direct t.String() reddens every subtest here,
// by panic. What it does not redden is
// TestUnservedRefusalKeepsTheTagWhereThereIsOne below, which is why that test
// is separate rather than more rows in this one.
func TestUnservedRefusalNamesATypeThatCannotNameItself(t *testing.T) {
	t.Parallel()

	t.Run("column", func(t *testing.T) {
		t.Parallel()
		for _, tc := range faultingShapes() {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()
				err := generateUnserved(t, unservedColumnProbe(tc.typ))
				require.ErrorIs(t, err, age.ErrUnsupportedQuery)
				require.ErrorContains(t, err,
					`1 query would be dropped: Fetch (column "n" projects `+tc.want+`)`)
			})
		}
	})

	t.Run("parameter", func(t *testing.T) {
		t.Parallel()
		for _, tc := range faultingShapes() {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()
				err := generateUnserved(t, unservedParamProbe(tc.typ))
				require.ErrorIs(t, err, age.ErrUnsupportedQuery)
				require.ErrorContains(t, err,
					`1 query would be dropped: Bind (parameter $p is `+tc.want+`)`)
			})
		}
	})
}

// TestUnservedRefusalKeepsTheTagWhereThereIsOne is the other half, and the
// half a fallback to the dynamic type name alone would lose. Both renders
// prefer the value's own tag and fall back only where asking for it faults,
// so an implementation that can name itself is still named by its own answer.
//
// Neither row here reddens when the guard is reverted — a direct t.String()
// answers these two — and both redden when the guard is replaced by an
// unconditional fmt.Sprintf("%T", t), which is the mutation this test is for.
// On the column axis sealedsum_test.go's pointer and embedded rows redden
// under that mutation too; on the parameter axis these rows are what does.
func TestUnservedRefusalKeepsTheTagWhereThereIsOne(t *testing.T) {
	t.Parallel()

	answering := []struct {
		name string
		typ  resolver.ResolvedType
	}{
		{"pointer form", &resolver.ResolvedNode{Labels: "Person"}},
		{"value embedder", answeringEmbedder{resolver.ResolvedNode{Labels: "Person"}}},
	}

	for _, tc := range answering {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// "node" is resolver.ResolvedNode's own String, promoted to each
			// form here. Asserting it rather than non-emptiness is what
			// separates the tag from the %T fallback, whose answers for these
			// two would be "*resolver.ResolvedNode" and
			// "age_test.answeringEmbedder".
			colErr := generateUnserved(t, unservedColumnProbe(tc.typ))
			require.ErrorIs(t, colErr, age.ErrUnsupportedQuery)
			require.ErrorContains(t, colErr, `1 query would be dropped: Fetch (column "n" projects node)`)

			paramErr := generateUnserved(t, unservedParamProbe(tc.typ))
			require.ErrorIs(t, paramErr, age.ErrUnsupportedQuery)
			require.ErrorContains(t, paramErr, `1 query would be dropped: Bind (parameter $p is node)`)
		})
	}
}
