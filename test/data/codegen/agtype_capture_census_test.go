// The Docker-free half of the agtype corpus witness.
//
// TestAGEAgtypeCaptureMatchesTheServer, in live_age_agtype_capture_test.go,
// reproduces each literal in internal/codegen/age/testdata/corpus_test.go.txt
// on a real apache/age 1.7.0 and compares bytes. It can only check the
// literals its table names, and it needs Docker, so on its own it leaves the
// obvious hole: a capture added to the fixture with no row here is witnessed
// by nothing and looks exactly like one that is.
//
// This file closes that hole without a container. It parses the fixture and
// requires the set of its package-level string consts to equal the set of
// names declared across the two tables below — live and synthetic — so
// adding a literal to the fixture and forgetting to witness it fails a gate
// that runs on every pull request rather than one gated on Docker (bd
// gqlc-6308).
//
// WHY THE TABLES LIVE HERE rather than beside the test that drives them:
// they have to be visible to an untagged build. live_age_agtype_capture_test.go
// carries //go:build codegen_live, so a census in there would be compiled
// out of exactly the runs that are supposed to catch the omission.
//
// WHAT THIS DOES NOT CHECK. It compares NAMES. That a live row's want
// matches the fixture's literal is the live test's job, and that a synthetic
// row's reason is true is nobody's — it is prose, and the reason it is
// required is that a synthetic table with no reasons is a way to opt any
// literal out of the live comparison by naming it. The census does hold
// every reason to being non-empty and every name to being in exactly one
// table, which is the part a mistake reaches.

package fixtures_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
)

// agtypeFixturePath is the corpus fixture, from this module's directory.
// It is a .txt because it is assembled into a throwaway module at test time
// rather than compiled here; go/parser does not care about the extension.
const agtypeFixturePath = "../../../internal/codegen/age/testdata/corpus_test.go.txt"

// agtypeLabel is one create_vlabel / create_elabel call. The order of the
// slice is the order of the calls, which is the order of the label ids,
// which is the high half of every graphid below.
type agtypeLabel struct {
	name string
	edge bool
}

// agtypeCapture is one literal in the fixture: the cypher that produces it
// and the exact bytes pgx must hand back. constName is the fixture's own
// identifier, and it is the join key the census checks.
type agtypeCapture struct {
	constName string
	cypher    string
	want      string
}

// agtypeGraph groups captures that share a label sequence. They have to be
// grouped: two captures in one graph can only both reproduce their ids if
// they were written in the order their sequence numbers record.
type agtypeGraph struct {
	graph    string
	labels   []agtypeLabel
	captures []agtypeCapture
}

// agtypeLiveCaptures is every fixture literal that claims to be a capture,
// with the write that produces it.
//
// Graph names are five characters or more on purpose: AGE refuses
// create_graph('g') and create_graph('g1') with "graph name is invalid",
// so a short name here fails at setup with a message that says nothing
// about length.
var agtypeLiveCaptures = []agtypeGraph{
	{
		graph: "agtype_entities",
		// Person is 3, ACTED_IN is 4, Marker is 5, Tricky is 6. An edge
		// label draws from the same counter as a vertex label, which is why
		// ACTED_IN sits between two vertex labels rather than after them.
		labels: []agtypeLabel{
			{name: "Person"},
			{name: "ACTED_IN", edge: true},
			{name: "Marker"},
			{name: "Tricky"},
		},
		captures: []agtypeCapture{
			{
				constName: "alice",
				cypher:    `CREATE (n:Person {id: 1, name: 'Alice', rank: 3, score: 9.5, tally: 7, active: true}) RETURN n`,
				want:      `{"id": 844424930131969, "label": "Person", "properties": {"id": 1, "name": "Alice", "rank": 3, "score": 9.5, "tally": 7, "active": true}}::vertex`,
			},
			{
				constName: "bob",
				cypher:    `CREATE (n:Person {id: 2, name: 'Bob', rank: 4, score: 1.0, tally: 8, active: false, weight: 0.5, middleName: 'Q'}) RETURN n`,
				want:      `{"id": 844424930131970, "label": "Person", "properties": {"id": 2, "name": "Bob", "rank": 4, "score": 1.0, "tally": 8, "active": false, "weight": 0.5, "middleName": "Q"}}::vertex`,
			},
			{
				constName: "actedIn",
				cypher:    `MATCH (a:Person {id: 1}), (b:Person {id: 2}) CREATE (a)-[r:ACTED_IN {since: 2019}]->(b) RETURN r`,
				want:      `{"id": 1125899906842625, "label": "ACTED_IN", "end_id": 844424930131970, "start_id": 844424930131969, "properties": {"since": 2019}}::edge`,
			},
			{
				constName: "marker",
				cypher:    `CREATE (n:Marker) RETURN n`,
				want:      `{"id": 1407374883553281, "label": "Marker", "properties": {}}::vertex`,
			},
			{
				constName: "tricky",
				cypher:    `CREATE (n:Tricky {big: 1.5, huge: 9223372036854775807, name: 'a, b} c": {d', tags: ['x', 'y'], nested: {x: 1, y: [1, 2]}}) RETURN n`,
				want:      `{"id": 1688849860263937, "label": "Tricky", "properties": {"big": 1.5, "huge": 9223372036854775807, "name": "a, b} c\": {d", "tags": ["x", "y"], "nested": {"x": 1, "y": [1, 2]}}}::vertex`,
			},
		},
	},
	{
		graph: "agtype_lists",
		// Listy is 3, Anything is 4, Slot is 5.
		labels: []agtypeLabel{
			{name: "Listy"},
			{name: "Anything"},
			{name: "Slot"},
		},
		captures: []agtypeCapture{
			{
				constName: "listy",
				cypher:    `CREATE (n:Listy {tags: ['x', 'y'], ranks: [1, -2, 2147483647], depths: [0.5, 1.5], matrix: [[1], [2, 3]]}) RETURN n`,
				want:      `{"id": 844424930131969, "label": "Listy", "properties": {"tags": ["x", "y"], "ranks": [1, -2, 2147483647], "depths": [0.5, 1.5], "matrix": [[1], [2, 3]]}}::vertex`,
			},
			{
				constName: "listyEmpty",
				cypher:    `CREATE (n:Listy {tags: [], ranks: [], depths: [], matrix: [[]]}) RETURN n`,
				want:      `{"id": 844424930131970, "label": "Listy", "properties": {"tags": [], "ranks": [], "depths": [], "matrix": [[]]}}::vertex`,
			},
			{
				// depths is written as null and comes back as no key at all.
				// That is the whole null-property claim the fixture's header
				// makes, and this row is where it is measured rather than
				// stated.
				constName: "listyNulls",
				cypher:    `CREATE (n:Listy {tags: ['a, b] c": {d', ''], ranks: [0], depths: null, matrix: []}) RETURN n`,
				want:      `{"id": 844424930131971, "label": "Listy", "properties": {"tags": ["a, b] c\": {d", ""], "ranks": [0], "matrix": []}}::vertex`,
			},
			{
				// The mirror image: a null ELEMENT is on the wire as a token.
				constName: "listyNullElems",
				cypher:    `CREATE (n:Listy {tags: [null, 'y'], ranks: [1, null, 3], depths: [0.5, null], matrix: [null]}) RETURN n`,
				want:      `{"id": 844424930131972, "label": "Listy", "properties": {"tags": [null, "y"], "ranks": [1, null, 3], "depths": [0.5, null], "matrix": [null]}}::vertex`,
			},
			{
				constName: "anyInt",
				cypher:    `CREATE (n:Anything {other: 'text', payload: 42}) RETURN n`,
				want:      `{"id": 1125899906842625, "label": "Anything", "properties": {"other": "text", "payload": 42}}::vertex`,
			},
			{
				constName: "anyMap",
				cypher:    `CREATE (n:Anything {payload: {a: 1, b: [true, null, 'z'], c: {d: 1.5}}}) RETURN n`,
				want:      `{"id": 1125899906842626, "label": "Anything", "properties": {"payload": {"a": 1, "b": [true, null, "z"], "c": {"d": 1.5}}}}::vertex`,
			},
			{
				constName: "anyList",
				cypher:    `CREATE (n:Anything {payload: [1, 'two', 3.5, true, null, [1, 2], {k: 'v'}]}) RETURN n`,
				want:      `{"id": 1125899906842627, "label": "Anything", "properties": {"payload": [1, "two", 3.5, true, null, [1, 2], {"k": "v"}]}}::vertex`,
			},
			{
				constName: "anyTag",
				cypher:    `CREATE (n:Anything {tag: {a: 1}, payload: 42}) RETURN n`,
				want:      `{"id": 1125899906842628, "label": "Anything", "properties": {"tag": {"a": 1}, "payload": 42}}::vertex`,
			},
			{
				constName: "slotOK",
				cypher:    `CREATE (n:Slot {tags: ['x', 'y'], ranks: [1, 2], depths: [0.5], matrix: [[1], [2, 3]]}) RETURN n`,
				want:      `{"id": 1407374883553281, "label": "Slot", "properties": {"tags": ["x", "y"], "ranks": [1, 2], "depths": [0.5], "matrix": [[1], [2, 3]]}}::vertex`,
			},
			{
				constName: "slotNulls",
				cypher:    `CREATE (n:Slot {tags: ['x', null], ranks: [1, 2], depths: [0.5], matrix: [[1]]}) RETURN n`,
				want:      `{"id": 1407374883553282, "label": "Slot", "properties": {"tags": ["x", null], "ranks": [1, 2], "depths": [0.5], "matrix": [[1]]}}::vertex`,
			},
		},
	},
	{
		graph: "agtype_temporals",
		// Event is 7 and Meeting is 8 in the fixture's ids, so four labels
		// were allocated before them in whatever graph they were captured
		// from. The four pads below reproduce that offset; their names are
		// arbitrary and only their COUNT is load-bearing.
		labels: []agtypeLabel{
			{name: "AgtypePadThree"},
			{name: "AgtypePadFour"},
			{name: "AgtypePadFive"},
			{name: "AgtypePadSix"},
			{name: "Event"},
			{name: "Meeting"},
		},
		captures: []agtypeCapture{
			{
				constName: "eventPlain",
				cypher:    `CREATE (n:Event {id: 1, occurredAt: 1704112496123456}) RETURN n`,
				want:      `{"id": 1970324836974593, "label": "Event", "properties": {"id": 1, "occurredAt": 1704112496123456}}::vertex`,
			},
			{
				constName: "eventZoned",
				cypher:    `CREATE (n:Event {id: 2, occurredAt: 1704112496123456, occurredAtOffset: 12600}) RETURN n`,
				want:      `{"id": 1970324836974594, "label": "Event", "properties": {"id": 2, "occurredAt": 1704112496123456, "occurredAtOffset": 12600}}::vertex`,
			},
			{
				constName: "eventSeen",
				cypher:    `CREATE (n:Event {id: 3, seenAt: 0, occurredAt: 1704112496123456, seenAtOffset: -18000}) RETURN n`,
				want:      `{"id": 1970324836974595, "label": "Event", "properties": {"id": 3, "seenAt": 0, "occurredAt": 1704112496123456, "seenAtOffset": -18000}}::vertex`,
			},
			{
				constName: "meetingPlain",
				cypher:    `CREATE (n:Meeting {id: 1, startsAt: 81000000000}) RETURN n`,
				want:      `{"id": 2251799813685249, "label": "Meeting", "properties": {"id": 1, "startsAt": 81000000000}}::vertex`,
			},
			{
				constName: "meetingZoned",
				cypher:    `CREATE (n:Meeting {id: 2, startsAt: 81000000000, startsAtOffset: 7200}) RETURN n`,
				want:      `{"id": 2251799813685250, "label": "Meeting", "properties": {"id": 2, "startsAt": 81000000000, "startsAtOffset": 7200}}::vertex`,
			},
			{
				constName: "meetingEnds",
				cypher:    `CREATE (n:Meeting {id: 3, endsAt: 5400000000, startsAt: 81000000000, endsAtOffset: -7200}) RETURN n`,
				want:      `{"id": 2251799813685251, "label": "Meeting", "properties": {"id": 3, "endsAt": 5400000000, "startsAt": 81000000000, "endsAtOffset": -7200}}::vertex`,
			},
		},
	},
}

// agtypeSynthetic is every fixture literal that is NOT a capture, with the
// reason it is written rather than measured. The reason is the row's whole
// value: a synthetic table with no reasons is a way to opt any literal out
// of the live comparison by naming it.
var agtypeSynthetic = map[string]string{
	"dwelling": "a record-typed property written by hand. What a record decode " +
		"does with the map it is handed is gqlc's own contract and is what the " +
		"row tests; whether AGE will STORE a record as a nested object is a " +
		"separate claim gqlc-jffyz step 5 owes and nothing here asserts.",
	"dwellingSparse": "the same constructed map with a nullable property and a " +
		"nullable field both absent.",
	"dwellingNoZip": "the same constructed map missing a NOT NULL field, which is " +
		"the input the emitted refusal exists for.",
}

// TestEveryAgtypeCaptureIsWitnessedOrDeclaredSynthetic reads the fixture and
// requires every package-level string const in it to be named in exactly one
// of the two tables above.
func TestEveryAgtypeCaptureIsWitnessedOrDeclaredSynthetic(t *testing.T) {
	t.Parallel()

	fixture := agtypeFixtureConsts(t)
	require.NotEmpty(t, fixture,
		"the fixture must declare package-level string consts; an empty reading "+
			"here means the parse found nothing and every row below would pass vacuously")

	declared := map[string]string{}
	for _, g := range agtypeLiveCaptures {
		for _, c := range g.captures {
			require.NotContains(t, declared, c.constName,
				"%s is declared twice in agtypeLiveCaptures", c.constName)
			declared[c.constName] = "live"
		}
	}
	for name, reason := range agtypeSynthetic {
		require.NotContains(t, declared, name,
			"%s is declared both live and synthetic; a literal is captured or it is "+
				"written, and the two tables mean opposite things", name)
		require.NotEmpty(t, reason,
			"a synthetic row must say why it is written rather than captured; without "+
				"the reason the table is a way to opt any literal out of the live comparison")
		declared[name] = "synthetic"
	}

	require.Equal(t, sortedNames(fixture), sortedNames(declared),
		"every agtype literal in %s must be either reproduced from the live pinned "+
			"image (agtypeLiveCaptures) or declared synthetic with a reason "+
			"(agtypeSynthetic). A name on the left only is a literal nothing witnesses; "+
			"a name on the right only is a row for a literal that no longer exists.",
		agtypeFixturePath)

	// The live test compares the server against a live row's want, and a want
	// is a COPY of the fixture's literal. Without this loop the chain has a
	// break in the middle: corrupting the fixture would leave the live suite
	// green, because nothing over there ever reads the fixture. Joining the
	// two here is what makes "the fixture holds what the server emits" one
	// fact rather than two facts about a copy.
	for _, g := range agtypeLiveCaptures {
		for _, c := range g.captures {
			require.Equal(t, c.want, fixture[c.constName],
				"agtypeLiveCaptures[%s].want must be the fixture's own literal; it is what "+
					"TestAGEAgtypeCaptureMatchesTheServer holds the server to, so a want that "+
					"has drifted from the fixture witnesses bytes the corpus does not use",
				c.constName)
		}
	}
}

// agtypeFixtureConsts is every package-level string const in the fixture,
// name to value.
//
// Package-level only, and deliberately: a literal declared inside a test
// body is an input that test composed for itself and is read beside the
// assertion that uses it, while these are shared across the file and are
// what the fixture's header makes its provenance claim about.
func agtypeFixtureConsts(t *testing.T) map[string]string {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, agtypeFixturePath, nil, 0)
	require.NoError(t, err, "parse %s", agtypeFixturePath)

	out := map[string]string{}
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		for _, spec := range gen.Specs {
			value, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for i, name := range value.Names {
				if i >= len(value.Values) {
					continue
				}
				lit, ok := value.Values[i].(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					continue
				}
				unquoted, err := strconv.Unquote(lit.Value)
				require.NoError(t, err, "unquote const %s", name.Name)
				out[name.Name] = unquoted
			}
		}
	}
	return out
}

// sortedNames is the key set of either map, ordered, so a mismatch prints as
// a diff a reader can act on rather than as two unordered maps.
func sortedNames(m map[string]string) []string {
	names := make([]string, 0, len(m))
	for name := range m {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
