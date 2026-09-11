package fixtures_test

import (
	"context"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	unionage "github.com/areqag/gqlc/test/data/codegen/valid/union_property/golden/apache-age-pgx-v5"
)

// TestAGERefusesAUnionParameterOutsideTheDeclaredMemberSet executes the
// bind-time member validation that is the whole of what a CLOSED union
// buys over ADR 0020's open `any`: the declared set is checked before the
// value is marshalled, and a value outside it is refused by name.
//
// Without this the validation has no behavioural witness in the tree.
// Measured on this branch (bd gqlc-x2uy, row M1): deleting the union arm
// from AGE's fallibleParamEncoder, so the emitted method binds `arg`
// straight into agtypeArgs with the member set unenforced, regenerated
// clean goldens and SURVIVED the whole local gate set with no red cell.
// The emitted encoder's BYTES are pinned by the golden comparison and its
// BEHAVIOUR by nothing, and a regeneration is the ordinary way a generator
// is edited here.
//
// No container, by construction, on uint64_param_test.go's model: the bind
// is emitted between cypherStmt and q.db.Query and cypherStmt reads q.graph
// alone, so a handle over a nil DBTX reaches it. That nil is the assertion
// and not a convenience — reaching the wire dereferences it and panics, so
// a row that RETURNS is a row whose value stopped before the send and the
// accepting rows below are exactly the panic.
//
// The DECODE direction has no such reachable position: decodeUnion… is
// unexported in the emitted package and is called only after q.db.Query
// returns rows, so nothing short of a live server executes it. It is
// pinned by golden text alone, which is the same gap this test closes on
// the encode side; gqlc-npus owns the live half.
func TestAGERefusesAUnionParameterOutsideTheDeclaredMemberSet(t *testing.T) {
	// $pick is ANY<INT32 | STRING>. Each refusal below is a Go value the
	// emitted type switch must fall through, chosen so that a switch
	// written against a WIDER notion of the members would admit it:
	//
	//   int64   — an integer, and the member is INT32. The declared WIDTH
	//             is part of the set, so an arm written `case int64` (the
	//             carrier agtype's integer scalar itself uses) accepts it.
	//   float64 — a number, and a member of the OTHER union this same
	//             batch emits, so an encoder keyed on the batch rather
	//             than on the encoding accepts it.
	//   bool    — neither, the plain outsider.
	//   []any   — a list, which agtype carries and json.Marshal renders
	//             happily, so it is what an unguarded bind sends.
	refusals := []struct {
		name string
		arg  any
		want string
	}{
		{name: "int64, the wrong integer width", arg: int64(1), want: "no member carries int64"},
		{name: "float64, a member of the other union", arg: float64(1), want: "no member carries float64"},
		{name: "bool, in neither member set", arg: true, want: "no member carries bool"},
		{name: "a list, which agtype would carry", arg: []any{int32(1)}, want: "no member carries []interface {}"},
	}

	for _, tc := range refusals {
		t.Run(tc.name, func(t *testing.T) {
			out, sent, err := rowsByPick(ptr(tc.arg))
			require.False(t, sent,
				"the bind accepted this value and the method went on to send it, so a value outside the declared member set was on its way to the server")
			require.Error(t, err)
			require.Nil(t, out)
			// The parameter name is the only thing that says which bind
			// refused; the union is named rather than a member, because
			// what is wrong with the value is that it is no member's type.
			require.ErrorContains(t, err, "RowsByPick: parameter $pick:")
			require.ErrorContains(t, err, "encode UNION<INT32|STRING>:")
			require.ErrorContains(t, err, tc.want)
		})
	}

	// The rows above are satisfied by a method that refuses everything,
	// including one that never binds at all. These say the declared members
	// still cross — one row per member, so an encoder that kept only the
	// first arm fails here rather than passing on the refusals alone.
	reaches := []struct {
		name string
		arg  *any
	}{
		{name: "int32, the first declared member", arg: ptr[any](int32(7))},
		{name: "string, the second declared member", arg: ptr[any]("seven")},
		{
			// Every closed-union property is nullable — neither
			// closed-union alternative admits a notNull of its own
			// (GQL.g4:1731-1732) — so nil is a legal bind, and it is
			// agtypeEncodedNullable rather than the member switch that has
			// to say so. A validation run BEFORE the nil check would refuse
			// it here as carrying no member.
			name: "nil, which the schema admits and the member switch never sees",
			arg:  nil,
		},
	}

	for _, tc := range reaches {
		t.Run(tc.name, func(t *testing.T) {
			_, sent, err := rowsByPick(tc.arg)
			require.True(t, sent,
				"the method returned before the send instead of reaching the wire, so nothing here witnesses that a declared member crosses; it returned: %v", err)
		})
	}
}

// rowsByPick runs the generated method over a nil DBTX and reports which of
// the two things it did: returned, or reached the send.
//
// The recovered value is pinned to the message the nil handle produces
// rather than to runtime.Error, for the reason countersMatching states: a
// genuine nil-map or index defect in the generated code between the bind
// and the send would otherwise read as `sent`, which is a passing
// accepting row.
func rowsByPick(arg *any) (out []int64, sent bool, err error) {
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
	out, err = unionage.New(nil, "g").RowsByPick(context.Background(), arg)
	return out, false, err
}
