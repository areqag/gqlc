package neo4j_test

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/areqag/gqlc/internal/codegen"
	"github.com/areqag/gqlc/internal/codegen/neo4j"
	"github.com/areqag/gqlc/internal/queryfile"
	"github.com/areqag/gqlc/internal/resolver"
	"github.com/areqag/gqlc/internal/schema"
	"github.com/areqag/gqlc/internal/schema/gql"
)

// decodingEveryEntity is in with one query appended per node and edge type
// its schema declares, each returning that entity whole. A backend emits
// decode<Name> only for an entity some query decodes (bd gqlc-m1dk), so a
// test reading an entity decoder off a query-free batch would read none.
func decodingEveryEntity(in codegen.Input) codegen.Input {
	var cols []resolver.ResolvedType
	for _, k := range slices.Sorted(maps.Keys(in.Schema.Nodes)) {
		cols = append(cols, resolver.ResolvedNode{Labels: k})
	}
	edges := slices.SortedFunc(maps.Keys(in.Schema.Edges), func(a, b schema.EdgeKey) int {
		return strings.Compare(fmt.Sprint(a), fmt.Sprint(b))
	})
	for _, k := range edges {
		cols = append(cols, resolver.ResolvedEdge{EdgeKey: k})
	}
	in.Queries = slices.Clone(in.Queries)
	for i, ty := range cols {
		in.Queries = append(in.Queries, codegen.NamedQuery{
			Name:        fmt.Sprintf("DecodeEntity%d", i),
			Cardinality: queryfile.CardinalityMany,
			SourceFile:  "entities.cypher",
			SourceText:  "MATCH (n) RETURN n\n",
			Validated:   resolver.ValidatedQuery{Columns: []resolver.Column{{Name: "n", Type: ty}}},
		})
	}
	return in
}

// TestEntityDecoderIsEmittedOnlyWhereAQueryDecodesIt pins the gate bd
// gqlc-m1dk put on decode<Name>: the struct is the schema's, the decoder
// the batch's. Event declares a DATE, a UUID and a union, so its decoder
// would call toDate, toUUID and decodeUnion<…> and import neo4j and
// dbtype; with no query decoding it, models.go names neither package,
// temporal_neo4j.go and uuid_neo4j.go convert nothing and so import
// nothing, and no union_neo4j.go is emitted. temporal.go and uuid.go stay,
// since the struct names Date and UUID, and each keeps its (empty) bridge
// beside it: TestTemporalCarriersAreEmittedExactlyWhenReferenced holds the
// pair. (A record property is refused by this backend, so no entity
// reaches a record helper here.)
func TestEntityDecoderIsEmittedOnlyWhereAQueryDecodesIt(t *testing.T) {
	sch, err := gql.New().Parse(strings.NewReader(`CREATE PROPERTY GRAPH TYPE Gate AS {
    (:Person { id :: INT64 NOT NULL }),
    (:Event {
        id   :: INT64 NOT NULL,
        born :: DATE NOT NULL,
        ref  :: UUID NOT NULL,
        pick :: ANY<INT32 | STRING>
    })
}`))
	require.NoError(t, err)

	emit := func(in codegen.Input) map[string]string {
		files, err := neo4j.New().Generate(in)
		require.NoError(t, err)
		out := make(map[string]string, len(files))
		for _, f := range files {
			out[f.Path] = string(f.Contents)
		}
		return out
	}

	none := emit(codegen.Input{Schema: sch})
	require.Contains(t, none["models.go"], "type Event struct {")
	require.NotContains(t, none["models.go"], "func decodeEvent(")
	require.NotContains(t, none["models.go"], "func decodePerson(")
	require.NotContains(t, none["models.go"], "import (",
		"no decoder is emitted, so nothing in models.go names fmt, neo4j or dbtype")
	require.Contains(t, none, "temporal.go", "Event's struct names Date whether or not anything decodes it")
	require.Contains(t, none, "uuid.go", "Event's struct names UUID whether or not anything decodes it")
	require.NotContains(t, none["temporal_neo4j.go"], "import (",
		"nothing converts a Date, so the bridge file declares nothing that names dbtype")
	require.NotContains(t, none["temporal_neo4j.go"], "func toDate(")
	require.NotContains(t, none["uuid_neo4j.go"], "import (",
		"nothing converts a UUID, so the bridge file declares nothing that names fmt or uuid")
	require.NotContains(t, none["uuid_neo4j.go"], "func toUUID(")
	require.NotContains(t, none, "union_neo4j.go", "nothing decodes or binds Event's union")

	all := emit(decodingEveryEntity(codegen.Input{Schema: sch}))
	require.Contains(t, all["models.go"], "func decodeEvent(node dbtype.Node) (Event, error) {")
	require.Contains(t, all["models.go"], "func decodePerson(node dbtype.Node) (Person, error) {")
	require.Contains(t, all["temporal_neo4j.go"], "func toDate(")
	require.Contains(t, all["uuid_neo4j.go"], "func toUUID(")
	require.Contains(t, all["union_neo4j.go"], "func decodeUnion")
}
