package resolver

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/areqag/gqlc/internal/query"
	"github.com/areqag/gqlc/internal/schema"
)

// foreignType and foreignEffect inhabit their query sums without being any
// declared variant. That is what bead gqlc-vt45 (from PR #916) is about: an
// unexported marker with value receivers seals DECLARATION of the interface,
// not INHABITATION of it, so embedding the interface satisfies it from any
// package in the module and matches no `case query.TypeInt:` arm. Both defaults
// below are therefore live branches, not dead code.
type foreignType struct{ query.Type }

type foreignEffect struct{ query.Effect }

// TestResolveTypeDefaultPanicsOnAForeignType pins resolveType's chosen answer
// for the arm.
//
// The panic is kept rather than turned into an error, and the reason is not
// that the arm is unreachable. Only parser-constructed Types reach resolveType
// today, so reaching it takes a change inside this module rather than a query —
// but the arm is reachable in the language sense, and the three arms just above
// it (TypeNode, TypeEdge, TypePath) already panic for exactly that reading of
// "resolver bug". Returning an ErrOutOfR0Scope here instead would put an
// in-module programming mistake into the same channel as a user writing an
// unsupported query, and the resolver's callers surface that channel to the
// user as a diagnostic about their Cypher.
func TestResolveTypeDefaultPanicsOnAForeignType(t *testing.T) {
	var got ResolvedType
	var err error
	require.PanicsWithValue(t,
		"resolver bug: resolveType reached unhandled query.Type resolver.foreignType",
		func() { got, err = resolveType(foreignType{}) },
		"a Type variant the mapper does not know is a resolver bug, not a query the user can write")
	require.Nil(t, got, "the panic unwinds before the assignment, so nothing was returned")
	require.NoError(t, err)
}

// TestResolveTypeMapsADeclaredVariant is the other direction: the default is
// not swallowing a variant the switch is supposed to handle.
func TestResolveTypeMapsADeclaredVariant(t *testing.T) {
	got, err := resolveType(query.TypeInt{})
	require.NoError(t, err)
	require.Equal(t, ResolvedScalar{Kind: ScalarInt}, got)
}

// TestValidateEffectDefaultRefusesAForeignEffect pins the other half of the
// bead: validateEffect HAS an error channel, so its default refuses through it
// instead of panicking. The two defaults answer differently on purpose —
// validateEffect is reached from Resolve with an Effect the parser built, and a
// variant it does not know is a gap in R6's coverage of the model rather than a
// corrupted value, so refusing the query is the honest answer.
//
// sc is nil because the default arm returns before reading it; a variant that
// started dereferencing sc first would fail this row rather than pass it.
func TestValidateEffectDefaultRefusesAForeignEffect(t *testing.T) {
	err := validateEffect(nil, foreignEffect{}, schema.Schema{})
	require.ErrorIs(t, err, ErrOutOfR0Scope)
	require.Contains(t, err.Error(), "unknown Effect variant")
}

// TestValidateEffectDispatchesADeclaredVariant keeps the row above from being
// the only thing the default is measured against: a declared variant must not
// reach it.
func TestValidateEffectDispatchesADeclaredVariant(t *testing.T) {
	sc := newScope(branchState{})
	err := validateEffect(sc, query.NewCreateEffect(nil), schema.Schema{})
	require.NotErrorIs(t, err, ErrOutOfR0Scope,
		"a declared Effect variant must be dispatched, not fall through to the tripwire")
}
