package gen_test

import (
	"flag"
	"fmt"
	"math/rand/v2"
	"reflect"
	"regexp"
	"runtime/debug"
	"sort"
	"strings"
	"testing"

	"github.com/areqag/gqlc/internal/codegen"
	"github.com/areqag/gqlc/internal/codegen/age"
	"github.com/areqag/gqlc/internal/codegen/neo4j"
	"github.com/areqag/gqlc/internal/fuzzkit"
	"github.com/areqag/gqlc/internal/fuzzkit/gen"
	"github.com/areqag/gqlc/internal/graph"
	"github.com/areqag/gqlc/internal/query"
	"github.com/areqag/gqlc/internal/query/cypher"
	"github.com/areqag/gqlc/internal/queryfile"
	"github.com/areqag/gqlc/internal/resolver"
	"github.com/areqag/gqlc/internal/schema"
	"github.com/areqag/gqlc/internal/schema/gql"
)

var (
	sampleSize   = flag.Int("gen.sample", 1000, "streams TestAdmittedShare draws")
	sampleSeed   = flag.Uint64("gen.seed", 1, "PCG seed for TestAdmittedShare's streams; the committed figures are seed 1")
	showExamples = flag.Bool("gen.examples", false, "TestAdmittedShare logs the first input refused for each reason")
	withCodegen  = flag.Bool("gen.codegen", false,
		"TestAdmittedShare also runs each resolved pair through neo4j v6 Generate and logs the share it admits")
)

// admittedFloor is the plan's acceptance floor (§6.2): below it the resolver
// and codegen targets spend their budget on refusals the parser targets
// already cover.
const admittedFloor = 0.30

// streams returns n byte slices of 64–1024 bytes from a PCG seeded (seed, 2),
// so the sample, and so the measured share, is the same on every run.
func streams(n int, seed uint64) [][]byte {
	r := rand.New(rand.NewPCG(seed, 2))
	out := make([][]byte, n)
	for i := range out {
		b := make([]byte, 64+r.IntN(961))
		for j := range b {
			b[j] = byte(r.UintN(256))
		}
		out[i] = b
	}
	return out
}

// fixedInputs are the byte shapes a fuzzer starts from or degenerates to.
func fixedInputs() map[string][]byte {
	ff := make([]byte, 512)
	for i := range ff {
		ff[i] = 0xff
	}
	ramp := make([]byte, 512)
	for i := range ramp {
		ramp[i] = byte(i)
	}
	return map[string][]byte{
		"nil":   nil,
		"zeros": make([]byte, 512),
		"0xff":  ff,
		"ramp":  ramp,
		"one":   {7},
	}
}

func generate(b []byte) (string, gen.Vocab, string, string) {
	s := fuzzkit.NewStream(b)
	text, v := gen.Schema(s)
	q := gen.Query(s, v)
	qf := gen.QueryFile(s, v)
	return text, v, q, qf
}

// TestGeneratorsTotalAndDeterministic: every generator returns on the
// degenerate inputs and on a random sample, and the same bytes give the same
// text twice.
func TestGeneratorsTotalAndDeterministic(t *testing.T) {
	inputs := fixedInputs()
	for i, b := range streams(300, 1) {
		inputs[fmt.Sprintf("rand%d", i)] = b
	}
	for name, b := range inputs {
		s1, v1, q1, f1 := generate(b)
		s2, v2, q2, f2 := generate(b)
		if s1 != s2 || q1 != q2 || f1 != f2 {
			t.Fatalf("%s: two generations from the same bytes differ", name)
		}
		if !reflect.DeepEqual(v1, v2) {
			t.Fatalf("%s: two Vocabs from the same bytes differ", name)
		}
		if s1 == "" || q1 == "" || f1 == "" {
			t.Fatalf("%s: a generator returned empty text", name)
		}
	}
}

// TestQueryTotalOverAHandBuiltVocab: Query and QueryFile read only Vocab's
// fields, and a target may build one by hand, so a Vocab Schema would never
// return — no nodes, an empty label set, an edge indexing past Nodes, empty
// names — must still yield text rather than a panic.
func TestQueryTotalOverAHandBuiltVocab(t *testing.T) {
	vocabs := []gen.Vocab{
		{},
		{Nodes: []gen.Node{{}}},
		{Nodes: []gen.Node{{Labels: []string{}, Props: []gen.Prop{{}}}}, Edges: []gen.Edge{{From: 3, To: -1}}},
		{Nodes: []gen.Node{{Labels: []string{"A"}}}, Edges: []gen.Edge{{Type: "R", From: 0, To: 7, Props: []gen.Prop{{Name: "p", Type: "LIST<INT64>"}}}}},
	}
	inputs := fixedInputs()
	for i, b := range streams(200, 1) {
		inputs[fmt.Sprintf("rand%d", i)] = b
	}
	for vi, v := range vocabs {
		for name, b := range inputs {
			if gen.Query(fuzzkit.NewStream(b), v) == "" || gen.QueryFile(fuzzkit.NewStream(b), v) == "" {
				t.Fatalf("vocab %d, input %s: empty text", vi, name)
			}
		}
	}
}

// TestExhaustedStreamIsAdmitted pins the package doc's claim that the first
// choice is the valid one: no bytes at all must parse, resolve and pass the
// queryfile front end.
func TestExhaustedStreamIsAdmitted(t *testing.T) {
	for _, b := range [][]byte{nil, make([]byte, 64)} {
		text, v, q, qf := generate(b)
		sch, err := gql.New().Parse(strings.NewReader(text))
		if err != nil {
			t.Fatalf("schema refused: %v\n%s", err, text)
		}
		pq, err := cypher.New().Parse(strings.NewReader(q))
		if err != nil {
			t.Fatalf("query refused: %v\n%s", err, q)
		}
		if _, err := resolver.New(sch).Resolve(pq); err != nil {
			t.Fatalf("resolve refused: %v\n%s\n%s", err, text, q)
		}
		if _, err := queryfile.New().Parse(strings.NewReader(qf)); err != nil {
			t.Fatalf("query file refused: %v\n%s", err, qf)
		}
		if len(v.Nodes) == 0 {
			t.Fatalf("an exhausted stream declared no node type")
		}
	}
}

// TestVocabMatchesSchema: what Vocab says was declared is what the parser
// reads back, on every schema of the sample the parser admits — the node
// label sets, the edge types and the properties with their nullability.
func TestVocabMatchesSchema(t *testing.T) {
	for i, b := range streams(500, 1) {
		text, v := gen.Schema(fuzzkit.NewStream(b))
		sch, err := gql.New().Parse(strings.NewReader(text))
		if err != nil {
			continue
		}
		if len(sch.Nodes) != len(v.Nodes) || len(sch.Edges) != len(v.Edges) {
			t.Fatalf("stream %d: parsed %d nodes / %d edges, Vocab says %d / %d\n%s",
				i, len(sch.Nodes), len(sch.Edges), len(v.Nodes), len(v.Edges), text)
		}
		got := map[string]bool{}
		for _, n := range sch.Nodes {
			for _, p := range n.Properties {
				got[fmt.Sprintf("%s.%s/%v", string(n.KeyLabels), p.Name, p.Nullable)] = true
			}
		}
		for _, e := range sch.Edges {
			for _, p := range e.Properties {
				got[fmt.Sprintf("%s.%s/%v", e.KeyLabels, p.Name, p.Nullable)] = true
			}
		}
		for _, n := range v.Nodes {
			for _, p := range n.Props {
				key := fmt.Sprintf("%s.%s/%v", graph.LabelSet(n.Labels).Key(), p.Name, !p.NotNull)
				if !got[key] {
					t.Fatalf("stream %d: Vocab declares %s, the parser did not read it\n%s", i, key, text)
				}
			}
		}
	}
}

// stage runs fn, turning a panic into a recorded crash rather than a dead test:
// a generated input that panics production code is a finding for the fuzz
// targets to report, and this package's job is only to count it.
func stage(fn func() error) (crash string, err error) {
	defer func() {
		if r := recover(); r != nil {
			crash = fmt.Sprintf("%v\n%s", r, debug.Stack())
		}
	}()
	return "", fn()
}

// refusal names an error by its text up to the first ": ", which for every
// sentinel-wrapping fail site in the pipeline is the sentinel's own text.
func refusal(err error) string {
	msg := syntaxPos.ReplaceAllString(err.Error(), "syntax error near")
	msg = codegenSite.ReplaceAllString(msg, "")
	if i := strings.Index(msg, ": "); i > 0 {
		msg = msg[:i]
	}
	if len(msg) > 70 {
		msg = msg[:70]
	}
	return msg
}

var (
	syntaxPos   = regexp.MustCompile(`syntax error at \d+:\d+ near`)
	codegenSite = regexp.MustCompile(`^query "[^"]*" (column|parameter) \d+ "[^"]*": `)
)

// TestAdmittedShare measures how far a sample of generated (schema, query)
// pairs gets through the pipeline and holds the plan's floor on the share that
// parses and resolves. -v prints the measurement, and -gen.sample /
// -gen.codegen widen it.
func TestAdmittedShare(t *testing.T) {
	n := *sampleSize
	var schemaOK, queryOK, resolved, anyGenerated, queryFileOK, crashes int
	backends := []struct {
		name string
		gen  interface {
			Generate(codegen.Input) ([]codegen.File, error)
		}
	}{
		{"neo4j-v5", neo4j.New(neo4j.WithDriverVersion(neo4j.DriverV5), neo4j.WithPackageName("db"))},
		{"neo4j-v6", neo4j.New(neo4j.WithDriverVersion(neo4j.DriverV6), neo4j.WithPackageName("db"))},
		{"age", age.New(age.WithPackageName("db"))},
	}
	generated := make([]int, len(backends))
	reasons := map[string]int{}
	example := map[string]string{}
	refuse := func(key, input string) {
		if reasons[key] == 0 {
			example[key] = input
		}
		reasons[key]++
	}
	for i, b := range streams(n, *sampleSeed) {
		s := fuzzkit.NewStream(b)
		text, v := gen.Schema(s)
		qtext := gen.Query(s, v)
		qf := gen.QueryFile(s, v)
		if crash, err := stage(func() (e error) { _, e = queryfile.New().Parse(strings.NewReader(qf)); return }); crash != "" {
			crashes++
			t.Logf("stream %d: queryfile parse panicked: %s\n%s", i, crash, qf)
		} else if err == nil {
			queryFileOK++
		}

		var sch schema.Schema
		crash, err := stage(func() (e error) { sch, e = gql.New().Parse(strings.NewReader(text)); return })
		if crash != "" {
			crashes++
			t.Logf("stream %d: gql parse panicked: %s\n%s", i, crash, text)
			continue
		}
		if err != nil {
			refuse("schema: "+refusal(err), text)
			continue
		}
		schemaOK++

		var parsed query.Query
		crash, err = stage(func() (e error) { parsed, e = cypher.New().Parse(strings.NewReader(qtext)); return })
		if crash != "" {
			crashes++
			t.Logf("stream %d: cypher parse panicked: %s\n%s", i, crash, qtext)
			continue
		}
		if err != nil {
			refuse("query: "+refusal(err), qtext)
			continue
		}
		queryOK++

		var vq resolver.ValidatedQuery
		crash, err = stage(func() (e error) { vq, e = resolver.New(sch).Resolve(parsed); return })
		if crash != "" {
			crashes++
			t.Logf("stream %d: resolve panicked: %s\n%s\n%s", i, crash, text, qtext)
			continue
		}
		if err != nil {
			refuse("resolve: "+refusal(err), qtext)
			continue
		}
		resolved++

		if !*withCodegen {
			continue
		}
		card := queryfile.CardinalityMany
		if len(vq.Columns) == 0 {
			card = queryfile.CardinalityExec
		}
		in := codegen.Input{Schema: sch, Queries: []codegen.NamedQuery{{
			Name: "Q", Cardinality: card, SourceFile: "q.cypher", SourceText: qtext, Validated: vq,
		}}}
		got := false
		for bi, be := range backends {
			crash, err = stage(func() (e error) { _, e = be.gen.Generate(in); return })
			if crash != "" {
				crashes++
				t.Logf("stream %d: %s Generate panicked: %s\n%s\n%s", i, be.name, crash, text, qtext)
				continue
			}
			if err != nil {
				refuse(be.name+": "+refusal(err), err.Error())
				continue
			}
			generated[bi]++
			got = true
		}
		if got {
			anyGenerated++
		}
	}

	pct := func(k int) string { return fmt.Sprintf("%d/%d (%.1f%%)", k, n, 100*float64(k)/float64(n)) }
	t.Logf("schemas parsed:      %s", pct(schemaOK))
	t.Logf("queries parsed:      %s", pct(queryOK))
	t.Logf("pairs resolved:      %s", pct(resolved))
	if *withCodegen {
		for bi, be := range backends {
			t.Logf("%-20s %s", be.name+" generated:", pct(generated[bi]))
		}
		t.Logf("any backend generated: %s", pct(anyGenerated))
	}
	t.Logf("query files parsed:  %s", pct(queryFileOK))
	t.Logf("panics:              %d", crashes)
	keys := make([]string, 0, len(reasons))
	for k := range reasons {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return reasons[keys[i]] > reasons[keys[j]] })
	for _, k := range keys {
		t.Logf("  %5d  %s", reasons[k], k)
		if testing.Verbose() && *showExamples {
			t.Logf("         e.g. %s", example[k])
		}
	}

	if share := float64(resolved) / float64(n); share < admittedFloor {
		t.Fatalf("%s of generated pairs parse and resolve, under the plan's %.0f%% floor", pct(resolved), 100*admittedFloor)
	}
}
