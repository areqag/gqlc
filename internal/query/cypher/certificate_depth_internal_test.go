package cypher

import "testing"

// This file asserts with the standard library and imports no third-party
// package, for the reason attributeuse_internal_test.go states in full:
// `just vuln`'s vuln-root-residual gate (bd gqlc-m5rc) fails any package whose
// IN-PACKAGE test variant pulls in third-party code, because govulncheck then
// discards that variant together with everything only it imports and the
// package goes silently blind. certifiesAtDepth is unexported, so the test has
// to live in `package cypher`.
//
// That constraint is why the bound is pinned through certifiesAtDepth rather
// than through classifyRichExpression: reaching the latter needs a parse tree,
// a parse tree needs antlr, and importing antlr here would have cost this
// package its vulnerability coverage to pin a guard no query reaches.

// TestCertifiedDepthClause pins the `depth >= 1` bound of the ref-valued-leaf
// certificate (bead gqlc-4s5t0, P5 — recorded there as living in
// refValuedShape, which it does not and did not; it is in the caller).
//
// No query reaches the depth-0 row. A bare ref classifies as a RefProjection
// before classifyRichExpression is called, so the grammar cannot deliver one,
// and every mutant of this bound survives the whole suite without this test.
func TestCertifiedDepthClause(t *testing.T) {
	for _, tc := range []struct {
		name      string
		refValued bool
		depth     int
		want      bool
	}{
		// The row the bead is about, and the one nothing else can observe.
		{"depth 0 ref-valued: a bare ref has no leaves to certify", true, 0, false},

		// The control, and it is load-bearing: the certificate must be
		// GRANTABLE, or the refusal above would also be satisfied by a
		// predicate that never certifies anything.
		{"depth 1 ref-valued: a list literal of refs certifies", true, 1, true},

		// Uncapped by design, per refValuedShape's own doc: the spine is
		// already committed in the type, so a cap would be a line defended by
		// nothing. A mutant turning >= into == would pass both rows above.
		{"depth 2 ref-valued: depth is uncapped", true, 2, true},

		// ok=false makes depth meaningless, so no depth may rescue it.
		{"not ref-valued at depth 0", false, 0, false},
		{"not ref-valued at depth 1", false, 1, false},
		{"not ref-valued at depth 2", false, 2, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := certifiesAtDepth(tc.refValued, tc.depth); got != tc.want {
				t.Fatalf("certifiesAtDepth(%t, %d) = %t, want %t", tc.refValued, tc.depth, got, tc.want)
			}
		})
	}
}
