package fixtures_test

import (
	"context"
	"math"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	uint64age "github.com/areqag/gqlc/test/data/codegen/valid/uint64_parameter/golden/apache-age-pgx-v5"
)

// aboveMaxInt64 is the smallest uint64 an agtype integer scalar cannot
// carry. Every refusal row below binds it; the two accepting rows bind
// math.MaxInt64, the largest one it can.
const aboveMaxInt64 uint64 = math.MaxInt64 + 1

// TestAGERefusesAUint64ParameterAboveMaxInt64 executes the refusal PR #2299
// emitted and nothing ran.
//
// Its mutation battery measured the gap, and this branch re-measured it
// rather than inheriting it: with the guard deleted from the emitted
// agtypeUnsigned, and again with its comparison off by one, the goldens
// regenerated each time, `just gates` reported all 13 arms passing
// (2026-09-03). The helper's BYTES are pinned by the golden comparison and
// its BEHAVIOUR by nothing, so a regeneration carrying a gutted guard —
// the ordinary way a generator is edited here — crosses the whole
// local gate set without a red cell.
//
// No container, by construction. The refusal is emitted into the generated
// method between cypherStmt and q.db.Query, and cypherStmt reads q.graph
// alone, so a handle built over a nil DBTX reaches the bind. That nil is
// the assertion rather than a convenience: reaching the wire dereferences
// it and panics, so a row that RETURNS is a row whose value stopped before
// the send, and the two accepting rows below are exactly the panic.
//
// This is the only place either fact is executed. `just test-codegen-fence`
// builds and vets the codegen module without running it.
func TestAGERefusesAUint64ParameterAboveMaxInt64(t *testing.T) {
	// The shapes are the four the encode composition takes, because
	// nullability and nesting are carried by combinators wrapped around the
	// leaf encoder rather than by a variant of it: agtypeUnsigned bare,
	// under agtypeEncodedNullable, under agtypeEncodedList, and under both.
	// A guard held only on the bare shape would leave the other three
	// binding whatever their combinator passed through.
	inRange := uint64age.CountersMatchingParams{
		Hits:   1,
		Misses: ptr[uint64](2),
		Runs:   []uint64{3, 4},
		Spans:  ptr([]uint64{5, 6}),
	}

	refusals := []struct {
		name string
		// params is inRange with exactly one field moved out of range, so a
		// row that fails names the shape that let the value through.
		params uint64age.CountersMatchingParams
		// wants are substrings of the returned error, in no order. The
		// parameter name is the one the query author wrote, which is the
		// only thing that says which bind refused; the index is the only
		// thing that says which element did, since a list's elements share
		// one parameter name.
		wants []string
	}{
		{
			name:   "$hits, bound bare",
			params: with(inRange, func(p *uint64age.CountersMatchingParams) { p.Hits = aboveMaxInt64 }),
			wants: []string{
				"CountersMatching: parameter $hits:",
				"does not fit the signed 64-bit integer an agtype scalar carries",
			},
		},
		{
			// The pointed-to value, not the pointer: agtypeEncodedNullable
			// short-circuits on nil and runs the encoder on the pointee, so
			// this row is the only thing that says the encoder it runs is
			// the guarded one.
			name:   "$misses, bound through agtypeEncodedNullable",
			params: with(inRange, func(p *uint64age.CountersMatchingParams) { p.Misses = ptr(aboveMaxInt64) }),
			wants: []string{
				"CountersMatching: parameter $misses:",
				"does not fit the signed 64-bit integer an agtype scalar carries",
			},
		},
		{
			// Element 1 and not element 0, so the index is read off the
			// failing element rather than being a constant that would
			// satisfy a first-element row.
			name:   "$runs, bound through agtypeEncodedList",
			params: with(inRange, func(p *uint64age.CountersMatchingParams) { p.Runs = []uint64{3, aboveMaxInt64} }),
			wants: []string{
				"CountersMatching: parameter $runs:",
				"element 1:",
				"does not fit the signed 64-bit integer an agtype scalar carries",
			},
		},
		{
			// The nullable list is agtypeEncodedNullable over
			// agtypeEncodedList, the one shape whose inner encoder is
			// spelled out at the call site rather than passed by name, so
			// it is the one a wrong encodedParamText entry would break.
			name:   "$spans, bound through both combinators",
			params: with(inRange, func(p *uint64age.CountersMatchingParams) { p.Spans = ptr([]uint64{5, 6, aboveMaxInt64}) }),
			wants: []string{
				"CountersMatching: parameter $spans:",
				"element 2:",
				"does not fit the signed 64-bit integer an agtype scalar carries",
			},
		},
	}

	for _, tc := range refusals {
		t.Run(tc.name, func(t *testing.T) {
			out, sent, err := countersMatching(tc.params)
			require.False(t, sent,
				"the bind accepted this value and the method went on to send it, so it was on its way to a server that cannot store it")
			require.Error(t, err)
			require.Nil(t, out)
			for _, want := range tc.wants {
				require.ErrorContains(t, err, want)
			}
		})
	}

	// The rows above are satisfied by any method that errors before the
	// send, including one that never sends at all. These two say the method
	// does reach the wire, and they are where the boundary is held: the
	// guard refuses ABOVE MaxInt64, so MaxInt64 itself and every value under
	// it must still cross. Without them a guard widened by one comparison —
	// the second mutation #2299 measured surviving — would pass every row of
	// this test.
	reaches := []struct {
		name   string
		params uint64age.CountersMatchingParams
	}{
		{name: "values well inside the range", params: inRange},
		{
			name: "MaxInt64 in every shape, the largest agtype carries",
			params: uint64age.CountersMatchingParams{
				Hits:   math.MaxInt64,
				Misses: ptr[uint64](math.MaxInt64),
				Runs:   []uint64{math.MaxInt64},
				Spans:  ptr([]uint64{math.MaxInt64}),
			},
		},
	}

	for _, tc := range reaches {
		t.Run(tc.name, func(t *testing.T) {
			_, sent, err := countersMatching(tc.params)
			require.True(t, sent,
				"the method returned before the send instead of reaching the wire, so nothing here witnesses that an accepted value crosses; it returned: %v", err)
		})
	}
}

// countersMatching runs the generated method over a nil DBTX and reports
// which of the two things it did: returned, or reached the send.
//
// The nil handle turns the send into a panic, so recovering it here is what
// lets both halves of this test read off ONE observation instead of two
// differently-shaped assertions. It also keeps the battery running: a
// panic escaping a subtest takes the whole binary down, so the first
// refusal row to stop refusing would be the only row anybody saw.
//
// Any other panic is re-raised. Swallowing them would turn a genuine
// defect in the generated code into `sent`, which is a passing accepting
// row.
//
// So the recovered value is pinned to the message the nil handle actually
// produces, not merely to runtime.Error. A genuine defect in generated Go
// throws runtime errors too — nil map write, index out of range — and the
// window they can hide in is the code between the last parameter guard and
// the send, today agtypeArgs: place one there and every accepting row
// panics before reaching a send while the whole test still reads green
// (measured, bd gqlc-53c98). The pin narrows that masked class to
// nil-deref defects in the same window; it does not empty it. Emptying it
// needs a real server, which is gqlc-lr0v6's standard and not this test's.
func countersMatching(params uint64age.CountersMatchingParams) (out []int64, sent bool, err error) {
	defer func() {
		r := recover()
		if r == nil {
			return
		}
		re, ok := r.(runtime.Error)
		if !ok || !strings.Contains(re.Error(), "nil pointer dereference") {
			panic(r)
		}
		sent = true
	}()
	out, err = uint64age.New(nil, "g").CountersMatching(context.Background(), params)
	return out, false, err
}

func ptr[T any](v T) *T { return &v }

// with copies params and applies edit, so each row states only the field it
// moves out of range and the rest stay the values the accepting row uses.
// Params holds a pointer and two slices; every row that touches one
// replaces it rather than writing through it, so the shallow copy is not
// shared with the original.
func with(
	params uint64age.CountersMatchingParams,
	edit func(*uint64age.CountersMatchingParams),
) uint64age.CountersMatchingParams {
	edit(&params)
	return params
}
