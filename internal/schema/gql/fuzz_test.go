package gql_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/areqag/gqlc/internal/fuzzkit"
	"github.com/areqag/gqlc/internal/graph"
	"github.com/areqag/gqlc/internal/schema"
	"github.com/areqag/gqlc/internal/schema/gql"
)

// fuzzLimit is how long one Parse or Load may take before fuzzkit.Guard
// reports the input as slow.
const fuzzLimit = 2 * time.Second

// fuzzSentinels is every error value a caller of this package may match with
// errors.Is: the leaves in allSentinels plus the two classes and the Parse-only
// leaf that allSentinels leaves out (sentinelsWithoutAFile says why), since a
// fuzzer is not bound to the files that register works from.
var fuzzSentinels = func() []error {
	out := []error{gql.ErrUnsupportedSource, gql.ErrUnsupportedType, gql.ErrCopyOfSource}
	for _, err := range allSentinels {
		out = append(out, err)
	}
	return out
}()

// isSyntaxError is this parser's syntax error: the listener's SyntaxError is
// the one site that spells it, and it reports lexer and parser errors alike.
func isSyntaxError(err error) bool {
	return strings.HasPrefix(err.Error(), "syntax error at ")
}

// isFSError is an error the catalogue itself raised rather than the parser. The
// fuzzed catalogue always holds the root file, so none is expected; T3 admits
// the class because a Loader over a real directory can raise one.
func isFSError(err error) bool {
	var pathErr *fs.PathError
	return errors.Is(err, fs.ErrNotExist) || errors.Is(err, fs.ErrInvalid) || errors.As(err, &pathErr)
}

// gqlSeedGlobs are every .gql under test/data and internal/codegen/*/testdata,
// relative to this package. filepath.Glob has no `**`, so each depth the tree
// uses is its own glob, and SeedFiles refuses a glob that matches nothing.
var gqlSeedGlobs = []string{
	"../../../test/data/schema/gql/corpus/*/*.gql",
	"../../../test/data/schema/gql/corpus/*/*/*.gql",
	"../../../test/data/schema/gql/corpus/*/*/*/*.gql",
	"../../../test/data/schema/gql/valid/*.gql",
	"../../../test/data/schema/gql/invalid/*.gql",
	"../../../test/data/resolver/*/schemas/*.gql",
	"../../../test/data/codegen/*/*/*.gql",
	"../../codegen/*/testdata/*.gql",
}

// FuzzGQLParse is target T2 of docs/specs/nightly-fuzzing-plan.md. Beyond not
// panicking, Parse must: return the zero Schema with any error; fail only with
// one of this package's sentinels or a syntax error; on success return a
// schema whose invariants hold (schemaViolation) and which marshals to JSON;
// and give the same result twice for the same input.
func FuzzGQLParse(f *testing.F) {
	fuzzkit.SeedFiles(f, gqlSeedGlobs...)
	f.Fuzz(func(t *testing.T, src string) {
		fuzzkit.Skip(t, []byte(src))
		var first, second parseResult
		fuzzkit.Guard(t, fuzzLimit, func() { first = parseGQL(src) })
		fuzzkit.Guard(t, fuzzLimit, func() { second = parseGQL(src) })
		if msg := resultViolation(first, false); msg != "" {
			t.Fatal(msg)
		}
		if msg := determinismViolation(first, second); msg != "" {
			t.Fatal(msg)
		}
	})
}

// FuzzGQLLoad is target T3: three fuzzed files a.gql, b.gql and c.gql in one
// catalogue, loaded from a.gql, so that COPY OF chains, cycles, dangling and
// misnamed references are all one mutation away. Beyond not panicking and not
// hanging (Guard is what a cycle the Loader failed to see would trip), Load
// must hold every property FuzzGQLParse holds, an fs error being an admitted
// class here; and when a.gql carries no COPY OF, so that the catalogue has
// nothing to contribute, Load must agree with Parse exactly.
func FuzzGQLLoad(f *testing.F) {
	for _, s := range loadSeeds {
		f.Add(s[0], s[1], s[2])
	}
	paths, err := filepath.Glob("../../../test/data/schema/gql/corpus/17-references/*.gql")
	if err != nil || len(paths) == 0 {
		f.Fatalf("no reference corpus to seed from (%v)", err)
	}
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			f.Fatal(err)
		}
		f.Add(string(b), body("b"), body("c"))
	}
	f.Fuzz(func(t *testing.T, a, b, c string) {
		fuzzkit.Skip(t, []byte(a+b+c))
		fsys := fstest.MapFS{"a.gql": file(a), "b.gql": file(b), "c.gql": file(c)}
		var first, second, parsed parseResult
		fuzzkit.Guard(t, fuzzLimit, func() { first = loadGQL(fsys) })
		fuzzkit.Guard(t, fuzzLimit, func() { second = loadGQL(fsys) })
		if msg := resultViolation(first, true); msg != "" {
			t.Fatal(msg)
		}
		if msg := determinismViolation(first, second); msg != "" {
			t.Fatal(msg)
		}
		fuzzkit.Guard(t, fuzzLimit, func() { parsed = parseGQL(a) })
		if errors.Is(parsed.err, gql.ErrCopyOfSource) {
			return
		}
		if msg := agreementViolation(first, parsed); msg != "" {
			t.Fatal(msg)
		}
	})
}

// loadSeeds are the chain shapes loader_test.go pins, spelled over the three
// fuzzed file names: chains of one, two and three files, the three anchorings
// a flat catalogue can satisfy, and each way resolution fails, cycles of every
// length the catalogue can hold among them.
var loadSeeds = [][3]string{
	{body("a"), "", ""},
	{copyOf("a", "b"), body("b"), ""},
	{copyOf("a", "b"), copyOf("b", "c"), body("c")},
	{copyOf("a", "/b"), body("b"), ""},
	{copyOf("a", "./b"), body("b"), ""},
	{copyOf("a", "CURRENT_SCHEMA/b"), body("b"), ""},
	{copyOf("a", "a"), "", ""},
	{copyOf("a", "b"), copyOf("b", "a"), ""},
	{copyOf("a", "b"), copyOf("b", "c"), copyOf("c", "a")},
	{copyOf("a", "d"), "", ""},
	{copyOf("a", "b"), body("x"), ""},
	{copyOf("a", "../b"), body("b"), ""},
	{copyOf("a", "b"), "CREATE GRAPH TYPE b {", ""},
}

// parseResult is one call's outcome, compared whole for determinism.
type parseResult struct {
	s   schema.Schema
	err error
}

func parseGQL(src string) parseResult {
	s, err := gql.New().Parse(strings.NewReader(src))
	return parseResult{s, err}
}

func loadGQL(fsys fs.FS) parseResult {
	s, err := gql.NewLoader(fsys).Load("a.gql")
	return parseResult{s, err}
}

// resultViolation checks one call's outcome: the error half, or the schema
// invariants and the JSON rendering. It returns the broken property, or "".
func resultViolation(r parseResult, fsAdmitted bool) string {
	if r.err != nil {
		if !reflect.DeepEqual(r.s, schema.Schema{}) {
			return fmt.Sprintf("property zero on error: the call failed with %q and returned %#v", r.err, r.s)
		}
		switch class := fuzzkit.ClassifyError(r.err, isSyntaxError, fuzzSentinels...); class {
		case fuzzkit.ClassSentinel, fuzzkit.ClassSyntax:
		default:
			if !fsAdmitted || !isFSError(r.err) {
				return fmt.Sprintf("property error class: %q is class %s, not a sentinel or a syntax error", r.err, class)
			}
		}
		return ""
	}
	if msg := schemaViolation(r.s); msg != "" {
		return msg
	}
	b, err := json.Marshal(r.s)
	if err != nil {
		return fmt.Sprintf("property json: MarshalJSON failed: %v", err)
	}
	if !json.Valid(b) {
		return fmt.Sprintf("property json: MarshalJSON wrote invalid JSON %q", b)
	}
	return ""
}

// schemaViolation checks the invariants a resolved schema carries whatever the
// input: every map entry is filed under its own identity, every label set key is
// non-empty and canonical (so no two keys spell one set), every complete label
// set contains its key labels, an edge has exactly one key label, both of its
// endpoints are declared node types, and every property is filed under its own
// non-empty name with a type.
func schemaViolation(s schema.Schema) string {
	if s.Nodes == nil || s.Edges == nil {
		return fmt.Sprintf("property maps: a successful schema has nil maps (nodes %v, edges %v)", s.Nodes == nil, s.Edges == nil)
	}
	for k, n := range s.Nodes {
		if n.KeyLabels != k {
			return fmt.Sprintf("property node key: node filed under %q has key labels %q", k, n.KeyLabels)
		}
		if msg := labelsViolation("node", k, n.CompleteLabels); msg != "" {
			return msg
		}
		if msg := propertiesViolation("node "+string(k), n.Properties); msg != "" {
			return msg
		}
	}
	for k, e := range s.Edges {
		if e.EdgeKey != k {
			return fmt.Sprintf("property edge key: edge filed under %+v has key %+v", k, e.EdgeKey)
		}
		if msg := labelsViolation("edge", k.KeyLabels, e.CompleteLabels); msg != "" {
			return msg
		}
		if n := len(k.KeyLabels.Split()); n != 1 {
			return fmt.Sprintf("property edge key: edge %+v has %d key labels, not one", k, n)
		}
		for _, end := range []graph.LabelSetKey{k.Source, k.Target} {
			if _, ok := s.Nodes[end]; !ok {
				return fmt.Sprintf("property edge endpoint: edge %+v names %q, which is no declared node type", k, end)
			}
		}
		if msg := propertiesViolation(fmt.Sprintf("edge %+v", k), e.Properties); msg != "" {
			return msg
		}
	}
	return ""
}

func labelsViolation(kind string, key, complete graph.LabelSetKey) string {
	if key == "" {
		return fmt.Sprintf("property %s key: a %s type has an empty key label set", kind, kind)
	}
	for _, k := range []graph.LabelSetKey{key, complete} {
		if canon := k.Split().Key(); canon != k {
			return fmt.Sprintf("property label set canonical: %s label set %q is not canonical (%q)", kind, k, canon)
		}
	}
	all := complete.Split()
	for _, label := range key.Split() {
		if !slices.Contains(all, label) {
			return fmt.Sprintf("property complete labels: %s complete label set %q lacks key label %q", kind, complete, label)
		}
	}
	return ""
}

func propertiesViolation(owner string, props map[string]schema.Property) string {
	for name, p := range props {
		if name == "" || p.Name != name {
			return fmt.Sprintf("property property-name: %s files property %q under %q", owner, p.Name, name)
		}
		if p.Type == "" {
			return fmt.Sprintf("property property-type: %s property %q has no type", owner, name)
		}
	}
	return ""
}

// determinismViolation compares two calls on the same input. Errors compare by
// text: a fresh call wraps a fresh chain, so identity is not the question.
func determinismViolation(a, b parseResult) string {
	if errText(a.err) != errText(b.err) || !reflect.DeepEqual(a.s, b.s) {
		return fmt.Sprintf("property determinism: two calls on one input disagreed: (%q, %v) then (%q, %v)",
			errText(a.err), a.s, errText(b.err), b.s)
	}
	return ""
}

// agreementViolation compares Load on a catalogue against Parse on its root
// file, for a root that names no other file: the catalogue then contributes
// nothing, so the two entry points must give one answer.
func agreementViolation(load, parse parseResult) string {
	if errText(load.err) != errText(parse.err) || !reflect.DeepEqual(load.s, parse.s) {
		return fmt.Sprintf("property load agrees with parse: Load gave (%q, %v) and Parse (%q, %v)",
			errText(load.err), load.s, errText(parse.err), parse.s)
	}
	return ""
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
