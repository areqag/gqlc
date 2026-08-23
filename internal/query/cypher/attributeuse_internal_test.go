package cypher

import (
	"fmt"
	"testing"

	"github.com/areqag/gqlc/internal/query"
)

// This file asserts with the standard library and imports no third-party
// package on purpose. `just vuln`'s vuln-root-residual gate (bd gqlc-m5rc)
// fails any package whose IN-PACKAGE test variant pulls in third-party code,
// because govulncheck then discards that variant together with everything only
// it imports and the package goes silently blind. attributeUse is unexported,
// so the test has to live in `package cypher`; testify would therefore cost the
// whole package its vulnerability coverage, which is not a trade worth two
// helper calls.

// foreignUse inhabits query.Use without being one of the three variants
// attributeUse dispatches on. It is what bead gqlc-vt45 (from PR #916) says an
// unexported marker with value receivers does NOT prevent: the marker seals
// DECLARATION of the interface, not INHABITATION of it — embedding the
// interface satisfies isUse/Part/Branch from any package in the module and
// matches no `case query.PropertyUse:` arm. So control really does reach
// attributeUse's default.
type foreignUse struct{ query.Use }

// TestAttributeUseStampsEveryKnownVariant is the direction that must keep
// working: each variant attributeUse knows is returned with the caller's
// (part, branch) written onto it, whatever coordinate it arrived carrying. The
// arrival coordinates below are deliberately non-zero and different from the
// stamped ones, so a default-arm pass-through cannot pass this by accident.
func TestAttributeUseStampsEveryKnownVariant(t *testing.T) {
	const part, branch = 3, 7

	for _, tc := range []struct {
		name string
		in   query.Use
	}{
		{"PropertyUse", query.NewPropertyUseAt(query.Ref{Variable: "a", Property: "age"}, 1, 2)},
		{"ExprUse", query.NewExprUseAt(query.TypeInt{}, query.ExprInProjection, 1, 2)},
		{"ClauseSlotUse", query.NewClauseSlotUseAt(query.ClauseSlotLimit, 1, 2)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.in.Part() == part || tc.in.Branch() == branch {
				t.Fatalf("the input must arrive with a DIFFERENT coordinate than the stamped one, or a pass-through would pass: got (%d, %d)", tc.in.Part(), tc.in.Branch())
			}
			got := attributeUse(tc.in, part, branch)
			if gotT, wantT := fmt.Sprintf("%T", got), fmt.Sprintf("%T", tc.in); gotT != wantT {
				t.Fatalf("attributeUse is sum-preserving: variant changed from %s to %s", wantT, gotT)
			}
			if got.Part() != part || got.Branch() != branch {
				t.Fatalf("coordinate not stamped: got (%d, %d), want (%d, %d)", got.Part(), got.Branch(), part, branch)
			}
		})
	}
}

// TestAttributeUseRefusesAForeignUse is the other direction, and the one bead
// gqlc-vt45 site 1 is about. The default arm used to return its argument
// UNCHANGED, so a Use variant this switch does not know passed through carrying
// whatever (branch, part) it already had — reachable and silent, and the
// coordinate is what the resolver keys parameter-type unification on, so the
// wrong value is worse here than no value.
//
// It is spelled as a panic rather than an error because attributeUse has no
// error channel and its two call sites (addParameterUse, and
// addParameterUseUnsuppressed in listener.go) have none either. The limit that
// makes a panic affordable is not that the arm is unreachable — inhabitation is
// open, which is the whole point above — but that every Use reaching either
// call site is constructed inside this package, so reaching the arm takes a
// change to this package's own code and not a query. Loud at test time beats a
// silently mis-attributed parameter in a shipped column.
func TestAttributeUseRefusesAForeignUse(t *testing.T) {
	const want = "cypher bug: attributeUse cannot stamp a coordinate onto query.Use variant cypher.foreignUse"

	got, panicked := recoverValue(func() { attributeUse(foreignUse{}, 3, 7) })
	if !panicked {
		t.Fatal("a Use this switch does not know must be refused loudly, not returned with its old coordinate")
	}
	if got != want {
		t.Fatalf("panic value:\n got: %v\nwant: %v", got, want)
	}
}

// recoverValue runs f and reports its panic value, plus whether it panicked at
// all. The second return is what separates "panicked with nil" from "did not
// panic", which a bare recover() cannot.
func recoverValue(f func()) (value any, panicked bool) {
	defer func() {
		if r := recover(); r != nil {
			value, panicked = r, true
		}
	}()
	f()
	return nil, false
}
