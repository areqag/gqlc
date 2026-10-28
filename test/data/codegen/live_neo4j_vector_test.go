//go:build codegen_live

// The live half of the VECTOR width (bd gqlc-do1, ADR 0051): that a vector
// written through generated code is STORED as a vector rather than a list,
// and that it ROUND-TRIPS through every read position the golden emits.
//
// IT DOES NOT RUN AGAINST THE PINNED IMAGE, and says so by skipping. A Bolt
// VECTOR needs Bolt 6.0, which is Neo4j 2025.10 or later, and neo4jImage is
// Neo4j 5.26.28 on Bolt 5.8. Measured on 2026-10-27 with a v6.2.0 driver
// against that digest: the driver packs the vector without complaint — unlike
// dbtype.UUID, it has no client-side protocol gate — and the server refuses the
// whole request with
//
//	Neo.ClientError.Request.Invalid (Illegal value for field "params":
//	Unexpected struct tag: 0x56)
//
// A newer COMMUNITY image is not enough either. Measured the same day against
// neo4j:2025.10-community (Neo4j 2025.10.1, Bolt 6.0): the protocol carries the
// vector, and the store refuses it —
//
//	Neo.ClientError.Data.DataUnsupportedByStoreFormat (storing properties of
//	type vector is not supported in aligned store format)
//
// and that edition will not start a database in the block format a vector
// property needs ("No supported database format 'block'. Available formats
// are: 'aligned', ..."). Against neo4j:2025.10-enterprise under its evaluation
// licence, in a scratch copy of this module with only the image constant and
// the licence variable changed, every row below PASSED (6/6), and a mutant
// binding the embedding bare instead of through fromVector failed the storage
// row and the three read rows that decode it.
//
// So what this test waits on is an owner call twice over — the pin, and an
// edition whose licence the repository would then carry — with every live arm
// in its blast radius. It is written and left to wait for it. It skips three
// times, on purpose: on the pinned digest itself, before paying for a
// container already known to refuse; then, for any other image, on the
// protocol version the server negotiates; then on the edition. So the day the
// pin moves this test runs with no edit, and on an image that still cannot
// store a vector it says why instead of failing on a struct tag or a store
// format.
//
// neo4j-go-v6 ALONE. v5 refuses the width at generation
// (test/data/codegen/invalid/vector_width_driver_version), so it has no golden
// to drive.
//
// THE STORAGE WITNESS IS RAW, the write is generated, for the reason
// TestNeo4jStoresAndRoundTripsAUUID gives: an emission observed only through
// itself could be wrong in agreement with itself — and here the specific
// failure would be a fromVector that bound the slice bare, which the server
// stores as a LIST and toVector would then refuse, so only a raw read of the
// stored value's type separates "stored a vector" from "stored a list".
//
// NOT PARALLEL, for the uint64 probe's reason: its own container, beside the
// four the neo4j half already boots concurrently.
//
// The name is spelled into the test-codegen-live-neo4j recipe's -run list and
// into internal/liverecipes.LiveArms.

package fixtures_test

import (
	"context"
	"os"
	"testing"
	"time"

	neo4jv6 "github.com/neo4j/neo4j-go-driver/v6/neo4j"
	"github.com/stretchr/testify/require"

	vectorv6 "github.com/areqag/gqlc/test/data/codegen/valid/vector_property/golden/neo4j-go-v6"
)

// neo4jImageBolt5 is the digest neo4jImage pinned when this test was written,
// measured to be Neo4j 5.26.28 negotiating Bolt 5.8 with a v6 driver. It is a
// statement about this digest and nothing else: a different pin falls through
// to the measured protocol check below.
const neo4jImageBolt5 = "neo4j@sha256:362542416de6c09a971484d1893878016cc3b5cdec166e54b1c824a220ecd6b9"

// vectorBoltMajor is the first Bolt major that carries a VECTOR structure.
const vectorBoltMajor = 6

// TestNeo4jStoresAndRoundTripsAVector is the live half of bd gqlc-do1.
func TestNeo4jStoresAndRoundTripsAVector(t *testing.T) {
	if os.Getenv("GQLC_SKIP_LIVE") != "" {
		t.Skip("GQLC_SKIP_LIVE set; skipping live backend containers")
	}
	if neo4jImage == neo4jImageBolt5 {
		t.Skip("the pinned neo4j image is Neo4j 5.26 on Bolt 5.8, and a VECTOR needs Bolt 6.0 (Neo4j 2025.10+); " +
			"measured 2026-10-27, the server refuses a vector parameter with \"Unexpected struct tag: 0x56\". " +
			"Moving the pin is an owner call (bd gqlc-do1, ADR 0051); this test runs unedited once it moves")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	t.Cleanup(cancel)

	boltURI := startNeo4jContainer(ctx, t)
	driver, err := neo4jv6.NewDriver(boltURI, neo4jv6.BasicAuth(neo4jUser, neo4jPassword, ""))
	require.NoError(t, err, "construct neo4j v6 driver")
	t.Cleanup(func() {
		if err := driver.Close(ctx); err != nil {
			t.Logf("close driver: %v", err)
		}
	})
	// Retried, because GetServerInfo is not: one run against a 2025.10
	// enterprise image asked it before the server served sessions, though
	// the container had passed the harness's readiness wait.
	var info neo4jv6.ServerInfo
	require.Eventually(t, func() bool {
		info, err = driver.GetServerInfo(ctx)
		return err == nil
	}, time.Minute, time.Second, "read the server's negotiated protocol")
	if v := info.ProtocolVersion(); v.Major < vectorBoltMajor {
		t.Skipf("the server negotiated Bolt %d.%d (%s), and a VECTOR needs Bolt %d.0 (Neo4j 2025.10+)",
			v.Major, v.Minor, info.Agent(), vectorBoltMajor)
	}

	editions, err := neo4jv6.ExecuteQuery(ctx, driver,
		"CALL dbms.components() YIELD edition RETURN edition", nil, neo4jv6.EagerResultTransformer)
	require.NoError(t, err, "read the server's edition")
	require.NotEmpty(t, editions.Records, "dbms.components() returned no rows")
	edition, ok := editions.Records[0].AsMap()["edition"].(string)
	require.True(t, ok, "dbms.components() edition is not a string: %v", editions.Records[0].AsMap())
	if edition == "community" {
		t.Skipf("%s is a community edition, which stores no vector property: only the block store format holds "+
			"one, and community has the aligned format alone (measured on 2025.10.1, 2026-10-27)", info.Agent())
	}

	q := vectorv6.New(driver)
	raw := func(t *testing.T, cypher string, params map[string]any) []map[string]any {
		t.Helper()
		res, err := neo4jv6.ExecuteQuery(ctx, driver, cypher, params, neo4jv6.EagerResultTransformer)
		require.NoError(t, err, "run the raw statement: %s", cypher)
		out := make([]map[string]any, 0, len(res.Records))
		for _, r := range res.Records {
			out = append(out, r.AsMap())
		}
		return out
	}
	reset := func(t *testing.T) {
		t.Helper()
		raw(t, wipeCypher, nil)
	}

	full := vectorv6.WriteDocumentParams{
		Id:        1,
		Embedding: []float32{0.5, -1.25, 3, 1e-3},
		Sketch:    &[]int8{1, -2, 3, 127},
	}
	bare := vectorv6.WriteDocumentParams{
		Id:        2,
		Embedding: []float32{4, 3, 2, 1},
	}
	seed := func(t *testing.T) {
		t.Helper()
		reset(t)
		require.NoError(t, q.WriteDocument(ctx, full))
		require.NoError(t, q.WriteDocument(ctx, bare))
	}

	t.Run("a vector written through the emission is stored as a vector", func(t *testing.T) {
		seed(t)
		rows := raw(t, "MATCH (d:Document {id: 1}) RETURN d.embedding AS e, d.sketch AS s", nil)
		require.Len(t, rows, 1)
		require.Equal(t, neo4jv6.Vector[float32]{Elems: full.Embedding}, rows[0]["e"],
			"the stored embedding is not a FLOAT32 vector; a bare []float32 bind would have stored a LIST")
		require.Equal(t, neo4jv6.Vector[int8]{Elems: *full.Sketch}, rows[0]["s"])
	})

	t.Run("a nil nullable vector binds the Cypher null", func(t *testing.T) {
		seed(t)
		rows := raw(t, "MATCH (d:Document {id: 2}) RETURN d.sketch IS NULL AS absent", nil)
		require.Len(t, rows, 1)
		absent, ok := rows[0]["absent"].(bool)
		require.True(t, ok && absent, "a nil *[]int8 bound something other than null: %v", rows[0]["absent"])
	})

	t.Run("the entity decode reads both back", func(t *testing.T) {
		seed(t)
		got, err := q.DocumentWhole(ctx, 1)
		require.NoError(t, err)
		require.Equal(t, vectorv6.Document{Id: 1, Embedding: full.Embedding, Sketch: full.Sketch}, got)

		got, err = q.DocumentWhole(ctx, 2)
		require.NoError(t, err)
		require.Nil(t, got.Sketch, "an absent nullable vector decoded as a value")
		require.Equal(t, bare.Embedding, got.Embedding)
	})

	t.Run("the column decode reads both back", func(t *testing.T) {
		seed(t)
		rows, err := q.DocumentVectors(ctx)
		require.NoError(t, err)
		require.ElementsMatch(t, []vectorv6.DocumentVectorsRow{
			{Id: 1, Embedding: full.Embedding, Sketch: full.Sketch},
			{Id: 2, Embedding: bare.Embedding},
		}, rows)

		one, err := q.DocumentEmbedding(ctx, 1)
		require.NoError(t, err)
		require.Equal(t, full.Embedding, one)
	})

	t.Run("a vector parameter matches the node holding it", func(t *testing.T) {
		seed(t)
		ids, err := q.DocumentsByEmbedding(ctx, full.Embedding)
		require.NoError(t, err)
		require.Equal(t, []int64{1}, ids)

		ids, err = q.DocumentsBySketch(ctx, full.Sketch)
		require.NoError(t, err)
		require.Equal(t, []int64{1}, ids)
	})

	t.Run("a list stored by another writer fails the read", func(t *testing.T) {
		reset(t)
		raw(t, "CREATE (:Document {id: 3, embedding: [1.0, 2.0, 3.0, 4.0]})", nil)
		_, err := q.DocumentEmbedding(ctx, 3)
		require.ErrorContains(t, err, "expected dbtype.Vector[float32]",
			"a LIST under a VECTOR property read back as a vector; toVector must refuse it")
	})
}
