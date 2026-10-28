package resolver_test

import (
	"strings"
	"testing"

	"github.com/areqag/gqlc/internal/query/cypher"
	"github.com/areqag/gqlc/internal/resolver"
	"github.com/areqag/gqlc/internal/schema/gql"
)

// demoteOrderSchema is the smallest schema gqlc-jjot2.20 reproduced on:
// a Person-KNOWS->Post edge for the first OPTIONAL group, and a Post-AUTHORED->Post
// edge with a NOT NULL property for the later one.
const demoteOrderSchema = `CREATE PROPERTY GRAPH TYPE G AS {
    (n0 :Person { name :: SMALLINT }),
    (n1 :Post),
    DIRECTED EDGE TYPE KNOWS01 LABEL KNOWS CONNECTING (n1 <- n0),
    DIRECTED EDGE TYPE AUTHORED11 :AUTHORED { name :: SMALLINT NOT NULL } CONNECTING (n1 TO n1)
}`

// TestCarriedGroupProofIsOrderIndependent: MATCH (n2) proves n2, which proves
// the first OPTIONAL group and so n0. n0 is also a member of the later OPTIONAL
// MATCH's group, which n0's being bound does not prove, so r5 can still be
// null. Across the WITH, that answer used to follow map iteration order, about
// half the time each way (gqlc-jjot2.20). Every call must give one answer, and
// it must be the one the same query gives without the WITH.
//
// One call on the old code had about an even chance of either answer, so 64
// calls agreeing by luck is about 2^-63.
func TestCarriedGroupProofIsOrderIndependent(t *testing.T) {
	s, err := gql.New().Parse(strings.NewReader(demoteOrderSchema))
	if err != nil {
		t.Fatal(err)
	}
	nullable := func(src string) bool {
		t.Helper()
		q, err := cypher.New().Parse(strings.NewReader(src))
		if err != nil {
			t.Fatalf("parse %q: %v", src, err)
		}
		vq, err := resolver.New(s).Resolve(q)
		if err != nil {
			t.Fatalf("resolve %q: %v", src, err)
		}
		p, ok := vq.Columns[0].Type.(resolver.ResolvedProperty)
		if !ok {
			t.Fatalf("resolve %q: column 0 is %T, want ResolvedProperty", src, vq.Columns[0].Type)
		}
		return p.Nullable
	}

	const (
		single  = "OPTIONAL MATCH (n0:Post)<-[r1:KNOWS]-(n2:Person) MATCH (n2) OPTIONAL MATCH (n0)<-[r5:AUTHORED]-(n6:Post) RETURN r5.name"
		carried = "OPTIONAL MATCH (n0:Post)<-[r1:KNOWS]-(n2:Person) WITH * MATCH (n2) OPTIONAL MATCH (n0)<-[r5:AUTHORED]-(n6:Post) RETURN r5.name"
	)
	if !nullable(single) {
		t.Fatalf("%q: r5.name is not nullable; the later OPTIONAL MATCH can fail to match", single)
	}
	for i := range 64 {
		if !nullable(carried) {
			t.Fatalf("call %d of %q: r5.name is not nullable, unlike the same query without the WITH", i, carried)
		}
	}
}
