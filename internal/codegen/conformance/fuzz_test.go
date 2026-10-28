package conformance_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"go/format"
	"go/parser"
	"go/token"
	"math/rand/v2"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/areqag/gqlc/internal/cli/backends"
	"github.com/areqag/gqlc/internal/codegen"
	"github.com/areqag/gqlc/internal/config"
	"github.com/areqag/gqlc/internal/fuzzkit"
	"github.com/areqag/gqlc/internal/fuzzkit/gen"
	"github.com/areqag/gqlc/internal/procsig"
	"github.com/areqag/gqlc/internal/query/cypher"
	"github.com/areqag/gqlc/internal/queryfile"
	"github.com/areqag/gqlc/internal/resolver"
	"github.com/areqag/gqlc/internal/schema/gql"
)

// generateLimit is how long FuzzGenerate's front end plus one emission per
// backend may take before fuzzkit.Guard reports the input as slow.
const generateLimit = 2 * time.Second

// fuzzPackage is the package every fuzzed emission is made under. It is a
// valid Go identifier, and so is every query name either arm writes (gen's
// "Q", and queryfile's own ^[A-Z][A-Za-z0-9]*$), which is what makes an
// ErrFormatFailure here a template defect rather than a refused input: the
// one input-side cause that sentinel documents is a non-identifier name.
const fuzzPackage = "db"

// fuzzRegistry is the composed registry, the one the CLI emits through, so a
// backend enrolled there is fuzzed without this file naming it.
var fuzzRegistry = func() codegen.Registry {
	reg, err := backends.Registry()
	if err != nil {
		panic(err)
	}
	return reg
}()

// fuzzSentinels is every refusal a caller of Generate may match with
// errors.Is: the core codegen set and whatever the registered backends
// publish.
var fuzzSentinels = func() []error {
	out := codegen.AllSentinels()
	for _, err := range fuzzRegistry.Sentinels() {
		out = append(out, err)
	}
	return out
}()

// A FuzzGenerate input is one of two arms, picked by its first byte:
//
//   - even (or no byte at all): the rest is a fuzzkit.Stream that gen.Schema
//     and gen.Query write a schema and one query from, and one more decision
//     picks the query's cardinality;
//   - odd: the rest is schema text, a NUL, then query-file text, which is
//     how the codegen corpus's fixtures are seeded, so byte mutation works
//     on real fixtures and on several queries per batch.
//
// The generated arm reaches the backends on about half its inputs
// (internal/fuzzkit/gen's measurement); the text arm keeps the shapes gen
// does not write (CALL aside, which the empty registry refuses).
const textArm = 1

// textInput encodes a schema and a query file as a text-arm input.
func textInput(schemaText, queries string) []byte {
	return append([]byte{textArm}, schemaText+"\x00"+queries...)
}

// fuzzCardinalities is the generated arm's cardinality decision. The first
// entry, zero, means "the one the resolved shape asks for", so an exhausted
// stream stays on the admitted path.
var fuzzCardinalities = []queryfile.Cardinality{0, queryfile.CardinalityOne, queryfile.CardinalityIter, queryfile.CardinalityExec}

// fuzzFrontEnd turns an input into a codegen.Input the way the pipeline does:
// parse the schema, split the query file, parse and resolve each query
// against an empty procedure registry. It returns false when any stage
// refuses: refusals there are FuzzGQLParse's, FuzzQueryfileParse's,
// FuzzCypherParse's and FuzzResolve's to judge, not this target's.
func fuzzFrontEnd(b []byte) (codegen.Input, bool) {
	type text struct {
		name string
		card queryfile.Cardinality
		src  string
	}
	var schemaText string
	var queries []text
	if len(b) > 0 && b[0]%2 == textArm {
		var qf string
		schemaText, qf, _ = strings.Cut(string(b[1:]), "\x00")
		parsed, err := queryfile.New().Parse(strings.NewReader(qf))
		if err != nil {
			return codegen.Input{}, false
		}
		for _, aq := range parsed {
			queries = append(queries, text{aq.Name, aq.Cardinality, aq.Text})
		}
	} else {
		if len(b) > 0 {
			b = b[1:]
		}
		s := fuzzkit.NewStream(b)
		var v gen.Vocab
		schemaText, v = gen.Schema(s)
		q := gen.Query(s, v)
		queries = append(queries, text{"Q", fuzzCardinalities[s.Int(len(fuzzCardinalities))], q})
	}

	sch, err := gql.New().Parse(strings.NewReader(schemaText))
	if err != nil {
		return codegen.Input{}, false
	}
	procs, err := procsig.NewRegistry(nil)
	if err != nil {
		panic(err)
	}
	res := resolver.New(sch, resolver.WithRegistry(procs))
	in := codegen.Input{Schema: sch}
	for _, q := range queries {
		parsed, err := cypher.New(cypher.WithRegistry(procs)).Parse(strings.NewReader(q.src))
		if err != nil {
			return codegen.Input{}, false
		}
		vq, err := res.Resolve(parsed)
		if err != nil {
			return codegen.Input{}, false
		}
		card := q.card
		if card == 0 {
			card = queryfile.CardinalityMany
			if len(vq.Columns) == 0 {
				card = queryfile.CardinalityExec
			}
		}
		in.Queries = append(in.Queries, codegen.NamedQuery{
			Name: q.name, Cardinality: card, SourceFile: "queries.cypher", SourceText: q.src, Validated: vq,
		})
	}
	return in, true
}

// emission is one backend's answer to one Input.
type emission struct {
	files []codegen.File
	err   error
}

// emitAll runs every registered backend over in, each through a generator
// built fresh for the call, keyed by its registry key.
func emitAll(in codegen.Input) map[string]emission {
	out := make(map[string]emission, len(fuzzRegistry.Keys()))
	for _, key := range fuzzRegistry.Keys() {
		newGen, _ := fuzzRegistry.Lookup(key)
		files, err := newGen(fuzzPackage).Generate(in)
		out[key] = emission{files, err}
	}
	return out
}

// emissionViolation names the first property one backend's emission breaks,
// or "". Each message starts "property <name>:", which is what the liveness
// rows match on.
func emissionViolation(key string, e emission) string {
	if e.err != nil {
		if e.files != nil {
			return fmt.Sprintf("property error-nil-files: %s returned %d files alongside %v", key, len(e.files), e.err)
		}
		switch fuzzkit.ClassifyError(e.err, nil, fuzzSentinels...) {
		case fuzzkit.ClassBug:
			return fmt.Sprintf("property no-bug: %s reported an internal bug: %v", key, e.err)
		case fuzzkit.ClassSentinel:
			if errors.Is(e.err, codegen.ErrFormatFailure) {
				return fmt.Sprintf("property no-bug: %s emitted source go/format refuses, under names that are all Go identifiers: %v", key, e.err)
			}
			return ""
		default:
			return fmt.Sprintf("property sentinel: %s refused with an error no sentinel matches: %v", key, e.err)
		}
	}
	if len(e.files) == 0 {
		return fmt.Sprintf("property non-empty: %s succeeded with no file", key)
	}
	for i, f := range e.files {
		if i > 0 && e.files[i-1].Path >= f.Path {
			return fmt.Sprintf("property path-order: %s emitted %q after %q; paths must be sorted and unique", key, f.Path, e.files[i-1].Path)
		}
		if !filepath.IsLocal(f.Path) || path.Ext(f.Path) != ".go" {
			return fmt.Sprintf("property go-file: %s emitted %q, which is not a local .go path", key, f.Path)
		}
		if msg := sourceViolation(f); msg != "" {
			return fmt.Sprintf("property %s: %s %s", msg, key, f.Path)
		}
	}
	return ""
}

// sourceViolation reports whether one emitted file is go/parser-clean, in
// fuzzPackage, and byte-for-byte what gofmt would write.
func sourceViolation(f codegen.File) string {
	parsed, err := parser.ParseFile(token.NewFileSet(), f.Path, f.Contents, parser.AllErrors)
	if err != nil {
		return fmt.Sprintf("go-parse: does not parse: %v", err)
	}
	if parsed.Name.Name != fuzzPackage {
		return fmt.Sprintf("go-parse: declares package %q, not %q", parsed.Name.Name, fuzzPackage)
	}
	formatted, err := format.Source(f.Contents)
	if err != nil {
		return fmt.Sprintf("gofmt: go/format refuses it: %v", err)
	}
	if !bytes.Equal(formatted, f.Contents) {
		return "gofmt: is not gofmt-clean"
	}
	return ""
}

// determinismViolation compares two emissions of one Input by one backend.
func determinismViolation(key string, a, b emission) string {
	if fmt.Sprint(a.err) != fmt.Sprint(b.err) {
		return fmt.Sprintf("property determinism: %s refused one run and not the other, or differently: %v / %v", key, a.err, b.err)
	}
	if !reflect.DeepEqual(a.files, b.files) {
		return fmt.Sprintf("property determinism: %s emitted different files for one Input", key)
	}
	return ""
}

// fuzzSeedRoot is the codegen corpus, relative to this package.
const fuzzSeedRoot = "../../../test/data/codegen/valid"

// fixtureInput reads one valid fixture as a text-arm input: its schema, and
// its query files joined in manifest order.
func fixtureInput(tb testing.TB, dir string) []byte {
	tb.Helper()
	m := readManifest(tb, dir)
	schemaText, err := os.ReadFile(filepath.Join(dir, "schema.gql"))
	if err != nil {
		tb.Fatal(err)
	}
	var queries strings.Builder
	for _, qf := range m.QueryFiles {
		src, err := os.ReadFile(filepath.Join(dir, qf))
		if err != nil {
			tb.Fatal(err)
		}
		queries.Write(src)
		queries.WriteString("\n")
	}
	return textInput(string(schemaText), queries.String())
}

func readManifest(tb testing.TB, dir string) manifest {
	tb.Helper()
	src, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		tb.Fatal(err)
	}
	var m manifest
	if err := json.Unmarshal(src, &m); err != nil {
		tb.Fatalf("%s: %v", dir, err)
	}
	return m
}

// generatedSeeds are generated-arm inputs: the empty stream, which gen
// guarantees the pipeline admits, and a fixed PCG sample.
func generatedSeeds() [][]byte {
	out := [][]byte{nil, make([]byte, 64)}
	r := rand.New(rand.NewPCG(7, 8))
	for range 16 {
		b := make([]byte, 64+r.IntN(449))
		for i := range b {
			b[i] = byte(r.UintN(256))
		}
		b[0] &^= 1
		out = append(out, b)
	}
	return out
}

// FuzzGenerate is target T8 of docs/specs/nightly-fuzzing-plan.md: a schema
// and queries the front end admits, through every registered backend. Beyond
// not panicking, each backend must refuse only with a codegen or backend
// sentinel and nil files, never with an internal bug (the AGE codegenBug
// recover, or go/format refusing what a template wrote); on success emit
// sorted, unique .go paths, each go/parser-clean, in fuzzPackage and
// gofmt-clean; and answer the same Input the same way twice.
func FuzzGenerate(f *testing.F) {
	if errDriverVersion == nil {
		f.Fatal("the registry publishes no neo4j.ErrUnrepresentableOnDriverVersion, which the v5-v6 property exempts by value")
	}
	dirs, err := filepath.Glob(filepath.Join(fuzzSeedRoot, "*"))
	if err != nil || len(dirs) == 0 {
		f.Fatalf("no codegen fixture to seed from under %s (%v)", fuzzSeedRoot, err)
	}
	for _, dir := range dirs {
		f.Add(fixtureInput(f, dir))
	}
	for _, b := range generatedSeeds() {
		f.Add(b)
	}
	f.Fuzz(func(t *testing.T, b []byte) {
		fuzzkit.Skip(t, b)
		var in codegen.Input
		var ok bool
		var first, second map[string]emission
		fuzzkit.Guard(t, generateLimit, func() {
			if in, ok = fuzzFrontEnd(b); ok {
				first = emitAll(in)
			}
		})
		if !ok {
			return
		}
		fuzzkit.Guard(t, generateLimit, func() { second = emitAll(in) })
		for _, key := range fuzzRegistry.Keys() {
			if msg := emissionViolation(key, first[key]); msg != "" {
				t.Fatal(msg)
			}
			if msg := determinismViolation(key, first[key], second[key]); msg != "" {
				t.Fatal(msg)
			}
		}
		if msg := crossBackendViolation(first); msg != "" {
			t.Fatal(msg)
		}
	})
}

// The registry keys the cross-backend properties compare.
var (
	keyV5  = string(config.DriverNeo4jGoV5)
	keyV6  = string(config.DriverNeo4jGoV6)
	keyAGE = string(config.DriverApacheAgePgxV5)

	// errDriverVersion is neo4j's ErrUnrepresentableOnDriverVersion, read
	// through the registry like every other backend name in this package.
	errDriverVersion = fuzzRegistry.Sentinels()["neo4j.ErrUnrepresentableOnDriverVersion"]
)

// crossBackendViolation holds the agreements between backends that were
// measured over the valid codegen corpus before being pinned; the table is in
// docs/mutation/fuzz-liveness-FuzzGenerate.md, beside what was measured and
// not pinned.
//
//   - neo4j v5 and v6 are one backend over two driver majors, so they admit
//     the same inputs, refuse with the same sentinels and emit the same path
//     set. The exception is the refusal that names the difference between
//     the majors, ErrUnrepresentableOnDriverVersion (VECTOR, ADR 0051): where
//     either side raises it, the two are not compared.
//   - Where AGE and a neo4j major both succeed, they emit the same files
//     apart from each backend's own: graph.go, which AGE always emits, and
//     the *_neo4j.go carrier bridges. The shared carriers (temporal.go,
//     uuid.go) are emitted by both or by neither.
//
// Admission is not compared between AGE and neo4j: on the corpus each
// refuses inputs the other admits, by design (ADR 0027 among others).
func crossBackendViolation(e map[string]emission) string {
	v5, v6 := e[keyV5], e[keyV6]
	if !errors.Is(v5.err, errDriverVersion) && !errors.Is(v6.err, errDriverVersion) {
		switch {
		case (v5.err == nil) != (v6.err == nil):
			return fmt.Sprintf("property v5-v6: one driver major admits what the other refuses: v5 %v / v6 %v", v5.err, v6.err)
		case v5.err != nil && sentinelsOf(v5.err) != sentinelsOf(v6.err):
			return fmt.Sprintf("property v5-v6: the driver majors refuse with different sentinels: v5 %v / v6 %v", v5.err, v6.err)
		case v5.err == nil && pathSet(v5.files, "") != pathSet(v6.files, ""):
			return fmt.Sprintf("property v5-v6: the driver majors emit different files: v5 %s / v6 %s", pathSet(v5.files, ""), pathSet(v6.files, ""))
		}
	}
	age := e[keyAGE]
	if age.err != nil {
		return ""
	}
	if !strings.Contains(","+pathSet(age.files, "")+",", ",graph.go,") {
		return fmt.Sprintf("property age-neo4j: AGE succeeded without graph.go: %s", pathSet(age.files, ""))
	}
	for _, key := range []string{keyV5, keyV6} {
		if n := e[key]; n.err == nil && pathSet(age.files, "graph.go") != pathSet(n.files, "_neo4j.go") {
			return fmt.Sprintf("property age-neo4j: AGE and %s emit different shared files: %s / %s", key, pathSet(age.files, "graph.go"), pathSet(n.files, "_neo4j.go"))
		}
	}
	return ""
}

// sentinelsOf names every sentinel err matches, so two refusals compare by
// class rather than by text.
func sentinelsOf(err error) string {
	var names []string
	for _, s := range fuzzSentinels {
		if errors.Is(err, s) {
			names = append(names, s.Error())
		}
	}
	slices.Sort(names)
	return strings.Join(names, ",")
}

// pathSet joins the emitted paths, leaving out those ending in omit.
func pathSet(files []codegen.File, omit string) string {
	var out []string
	for _, f := range files {
		if omit == "" || !strings.HasSuffix(f.Path, omit) {
			out = append(out, f.Path)
		}
	}
	return strings.Join(out, ",")
}

// buildLimit is how long FuzzGeneratedBuilds' front end and three emissions
// may take before fuzzkit.Guard reports the input as slow (watchdog 50 s), and
// compileLimit is the same for the `go build` of what they emitted (watchdog
// 5 min). They are two because their costs differ in kind: the first is this
// process's own CPU, the second a child process contending for the shared
// build cache. A warm build measured 0.04 to 0.07 s, but one 5-minute run
// under a shared host's load reported an 11.7 s exec whose input replayed in
// 0.08 s, so a single 10 s limit over both reds the night on the host, not the
// input. The measurements are in
// docs/mutation/fuzz-liveness-FuzzGeneratedBuilds.md; the driver dependencies
// are compiled before fuzzing starts (warmBuild), so no exec pays for them.
const (
	buildLimit   = 10 * time.Second
	compileLimit = time.Minute
)

// buildSeeds are the fixtures FuzzGeneratedBuilds starts from, three so that
// every `go test` of this package pays three builds and no more. Between them
// every backend admits a read batch, a write, and the shared temporal and
// UUID carriers with their neo4j bridges.
var buildSeeds = []string{"many_col_many", "write_exec_temporal_param", "union_only_carrier_entity_decode_only"}

// FuzzGeneratedBuilds is target T9: for an input every registered backend
// admits, the emitted packages compile against the real drivers. Each
// backend's files are written to their own package directory under a fresh
// _fuzzbuild-* directory inside the test/data/codegen module, which carries
// the driver dependencies, and `go build` runs there as TestGoldenBuild runs
// it over the goldens. The leading underscore keeps the directory out of every
// ./... walk, TestGoldenBuild's included, while it exists. A process the
// watchdog ends leaves its directory behind, untracked.
//
// FuzzGenerate checks everything about an emission short of compiling it;
// this target is that one step.
func FuzzGeneratedBuilds(f *testing.F) {
	for _, name := range buildSeeds {
		f.Add(fixtureInput(f, filepath.Join(fuzzSeedRoot, name)))
	}
	module, err := filepath.Abs(filepath.Dir(fuzzSeedRoot))
	if err != nil {
		f.Fatal(err)
	}
	warmBuild(f, module)
	f.Fuzz(func(t *testing.T, b []byte) {
		fuzzkit.Skip(t, b)
		var emitted map[string]emission
		fuzzkit.Guard(t, buildLimit, func() {
			if in, ok := fuzzFrontEnd(b); ok {
				emitted = emitAll(in)
			}
		})
		if emitted == nil {
			return
		}
		for _, e := range emitted {
			if e.err != nil {
				return
			}
		}
		var out []byte
		var err error
		fuzzkit.Guard(t, compileLimit, func() { out, err = buildEmitted(t, module, emitted) })
		if err != nil {
			t.Fatalf("property builds: the emitted packages do not compile (%v):\n%s", err, out)
		}
	})
}

// warmBuild compiles the seed fixtures' committed goldens, so that the driver
// packages they import are in the build cache before the first guarded exec.
// On a cold cache that compile is seconds of work no input is responsible
// for (measured 2026-10-28: 4.8 s wall, 28 s CPU, on 24 cores).
func warmBuild(f *testing.F, module string) {
	f.Helper()
	args := []string{"build"}
	for _, name := range buildSeeds {
		args = append(args, "./"+path.Join("valid", name, "golden")+"/...")
	}
	cmd := exec.CommandContext(f.Context(), "go", args...)
	cmd.Dir = module
	if out, err := cmd.CombinedOutput(); err != nil {
		f.Fatalf("the seed fixtures' goldens do not compile (%v):\n%s", err, out)
	}
}

// buildEmitted writes each backend's files into its own package under a new
// _fuzzbuild-* directory in module and runs `go build` over the packages.
func buildEmitted(t *testing.T, module string, emitted map[string]emission) ([]byte, error) {
	t.Helper()
	dir, err := os.MkdirTemp(module, "_fuzzbuild-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(dir); err != nil {
			t.Error(err)
		}
	})
	args := []string{"build"}
	for key, e := range emitted {
		pkg := filepath.Join(dir, key)
		if err := os.Mkdir(pkg, 0o755); err != nil {
			t.Fatal(err)
		}
		for _, file := range e.files {
			if err := os.WriteFile(filepath.Join(pkg, file.Path), file.Contents, 0o644); err != nil {
				t.Fatal(err)
			}
		}
		args = append(args, "./"+path.Join(filepath.Base(dir), key))
	}
	cmd := exec.CommandContext(t.Context(), "go", args...)
	cmd.Dir = module
	return cmd.CombinedOutput()
}
