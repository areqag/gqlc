package resolver_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/areqag/gqlc/internal/fuzzkit"
	"github.com/areqag/gqlc/internal/fuzzkit/gen"
	"github.com/areqag/gqlc/internal/query"
	"github.com/areqag/gqlc/internal/query/cypher"
	"github.com/areqag/gqlc/internal/resolver"
	"github.com/areqag/gqlc/internal/schema"
	"github.com/areqag/gqlc/internal/schema/gql"
)

const (
	fuzzLimit       = 2 * time.Second
	fuzzFixtureDir  = "../../test/data/resolver"
	fuzzManifest    = fuzzFixtureDir + "/sweep.manifest.tsv"
	fuzzGeneratedN  = 32
	fuzzCorpusArmOn = '#'
)

// callRegistryArm is the resolver's documented plain error (WithRegistry's
// doc): a CALL whose procedure the registry lacks, or whose argument count
// disagrees with it, at the one site the resolver consults the registry. The
// parser is authoritative for both and runs under the same registry here, so
// the arm is parser drift; plan §5 T7 counts it rather than failing on it.
var callRegistryArm = regexp.MustCompile(`^resolver: procedure ".*" (missing from registry|expects \d+ arguments, got \d+) \(parser drift\)$`)

// resolveResult is one Resolve call's whole output.
type resolveResult struct {
	vq  resolver.ValidatedQuery
	err error
}

// FuzzResolve is target T7 of docs/specs/nightly-fuzzing-plan.md. The input's
// first byte picks the arm:
//
//   - even (or no byte): the rest is a fuzzkit.Stream from which gen.Schema
//     and gen.Query draw a schema and a query over its vocabulary;
//   - odd: the rest is a corpus schema's path under test/data/resolver, a
//     newline, and query text. A path naming no corpus schema is skipped.
//
// A schema or query the parsers refuse is skipped: T1 and T2 own those. Beyond
// "Resolve does not panic" it checks, on every input that parses:
//
//   - property determinism: two Resolve calls return reflect.DeepEqual models
//     and the same error text and sentinels;
//   - property zero on error: a refusal comes with the zero ValidatedQuery;
//   - property error class: a refusal wraps a resolver sentinel, or is the
//     documented CALL-registry plain arm (callRegistryArm), which is logged and
//     not failed; never a "bug:" a fail site wrote, never another plain error;
//   - property well-formed: every column and parameter carries a non-nil
//     ResolvedType (a list's element too), and column names are unique;
//   - property marshal: the model marshals with encoding/json to valid JSON.
//
// Each parse and each Resolve runs under fuzzkit.Guard's 2 s limit.
func FuzzResolve(f *testing.F) {
	schemas := loadCorpusSchemas(f)
	seedFromManifest(f, schemas)
	f.Add([]byte{})
	r := rand.New(rand.NewPCG(7, 7))
	for range fuzzGeneratedN {
		b := make([]byte, 1+r.IntN(512))
		for i := range b {
			b[i] = byte(r.UintN(256))
		}
		b[0] &^= 1
		f.Add(b)
	}

	parser := cypher.New(cypher.WithRegistry(resolver.RegR7))

	f.Fuzz(func(t *testing.T, b []byte) {
		fuzzkit.Skip(t, b)
		var (
			sch   schema.Schema
			qtext string
			input string
		)
		if len(b) > 0 && b[0]%2 == 1 {
			name, text, _ := strings.Cut(string(b[1:]), "\n")
			s, ok := schemas[name]
			if !ok {
				t.Skipf("no corpus schema %q", name)
			}
			sch, qtext, input = s, text, text
		} else {
			s := fuzzkit.NewStream(b[min(1, len(b)):])
			stext, v := gen.Schema(s)
			qtext = gen.Query(s, v)
			input = stext + "\n" + qtext
			var err error
			fuzzkit.Guard(t, fuzzLimit, func() { sch, err = gql.New().Parse(strings.NewReader(stext)) })
			if err != nil {
				t.Skipf("generated schema does not parse (T2's to find): %v", err)
			}
		}
		var q query.Query
		var perr error
		fuzzkit.Guard(t, fuzzLimit, func() { q, perr = parser.Parse(strings.NewReader(qtext)) })
		if perr != nil {
			t.Skipf("query does not parse (T1's to find): %v", perr)
		}

		res := resolver.New(sch, resolver.WithRegistry(resolver.RegR7))
		var first, second resolveResult
		fuzzkit.Guard(t, fuzzLimit, func() { first.vq, first.err = res.Resolve(q) })
		fuzzkit.Guard(t, fuzzLimit, func() { second.vq, second.err = res.Resolve(q) })

		if !sameResolve(first, second) {
			msg := fmt.Sprintf("two Resolve calls on %q disagree:\nfirst:  %#v, %v\nsecond: %#v, %v",
				input, first.vq, first.err, second.vq, second.err)
			if nullabilityOnly(first, second) {
				msg = "only in nullability: " + msg
			}
			if bead := openFinding(msg); bead == "" {
				t.Fatalf("property determinism: %s", msg)
			} else {
				t.Logf("tolerated, open finding %s: %s", bead, msg)
			}
		}
		if first.err != nil {
			if !reflect.DeepEqual(first.vq, resolver.ValidatedQuery{}) {
				t.Fatalf("property zero on error: Resolve on %q failed with %v but returned a non-zero model %#v",
					input, first.err, first.vq)
			}
			switch c := fuzzkit.ClassifyInputError(input, first.err, nil, resolver.AllSentinels...); {
			case c == fuzzkit.ClassSentinel:
			case c == fuzzkit.ClassPlain && callRegistryArm.MatchString(first.err.Error()):
				t.Logf("counted: the CALL-registry plain arm: %v", first.err)
			default:
				t.Fatalf("property error class: Resolve on %q returned a %s error: %v", input, c, first.err)
			}
			return
		}
		out, err := json.Marshal(first.vq)
		if err != nil {
			t.Fatalf("property marshal: Resolve on %q returned a model json.Marshal refuses: %v", input, err)
		}
		if !json.Valid(out) {
			t.Fatalf("property marshal: Resolve on %q marshalled to invalid JSON %q", input, out)
		}
		if msg := wellFormedViolation(first.vq); msg != "" {
			if bead := openFinding(msg); bead != "" {
				t.Logf("tolerated, open finding %s: %s", bead, msg)
				return
			}
			t.Fatalf("property well-formed: Resolve on %q: %s", input, msg)
		}
	})
}

// openFinding names the open bead that already records a violation, or "".
// Each arm is a known resolver defect this target found and is not to report
// again every night until it is fixed; the fix PR deletes its arm and commits
// the bead's seed (plan §11).
func openFinding(violation string) string {
	switch {
	case strings.Contains(violation, "is not unique"):
		// Duplicate result column names (openCypher ColumnNameConflict) are
		// checked by neither the parser nor the resolver. What this hides:
		// nothing else, since wellFormedViolation checks names last.
		return "gqlc-jjot2.19"
	case strings.HasPrefix(violation, "only in nullability: "):
		// OPTIONAL group demotion depends on map iteration order. What this
		// hides: any other source of nondeterminism confined to nullability.
		return "gqlc-jjot2.20"
	}
	return ""
}

var nullableField = regexp.MustCompile(`"nullable":(true|false)`)

// nullabilityOnly reports whether two accepted models differ in nothing but
// their "nullable" fields.
func nullabilityOnly(a, b resolveResult) bool {
	if a.err != nil || b.err != nil {
		return false
	}
	ja, errA := json.Marshal(a.vq)
	jb, errB := json.Marshal(b.vq)
	if errA != nil || errB != nil {
		return false
	}
	return bytes.Equal(nullableField.ReplaceAll(ja, nil), nullableField.ReplaceAll(jb, nil))
}

// sameResolve compares two Resolve results: the models by reflect.DeepEqual,
// the errors by their text and which sentinels errors.Is finds.
func sameResolve(a, b resolveResult) bool {
	if !reflect.DeepEqual(a.vq, b.vq) || (a.err == nil) != (b.err == nil) {
		return false
	}
	if a.err == nil {
		return true
	}
	if a.err.Error() != b.err.Error() {
		return false
	}
	for _, s := range resolver.AllSentinels {
		if errors.Is(a.err, s) != errors.Is(b.err, s) {
			return false
		}
	}
	return true
}

// wellFormedViolation names the first broken shape of an accepted model, or "".
// Names are checked last so that openFinding's arm for them hides no type check.
func wellFormedViolation(vq resolver.ValidatedQuery) string {
	for i, c := range vq.Columns {
		if !typed(c.Type) {
			return fmt.Sprintf("column %d %q has a nil ResolvedType (%#v)", i, c.Name, c.Type)
		}
	}
	for i, p := range vq.Parameters {
		if !typed(p.Type) {
			return fmt.Sprintf("parameter %d %q has a nil ResolvedType (%#v)", i, p.Name, p.Type)
		}
	}
	seen := make(map[string]bool, len(vq.Columns))
	for i, c := range vq.Columns {
		if seen[c.Name] {
			return fmt.Sprintf("column %d name %q is not unique", i, c.Name)
		}
		seen[c.Name] = true
	}
	return ""
}

// typed reports whether t is a non-nil ResolvedType all the way down a list's
// elements.
func typed(t resolver.ResolvedType) bool {
	for t != nil {
		l, ok := t.(resolver.ResolvedList)
		if !ok {
			return true
		}
		t = l.Element
	}
	return false
}

// loadCorpusSchemas parses every schema the corpus sweep crosses, keyed by its
// path under test/data/resolver, which is how the corpus arm names one.
func loadCorpusSchemas(f *testing.F) map[string]schema.Schema {
	f.Helper()
	out := map[string]schema.Schema{}
	for _, dir := range []string{"valid/schemas", "invalid/schemas"} {
		paths, err := filepath.Glob(filepath.Join(fuzzFixtureDir, dir, "*.gql"))
		if err != nil || len(paths) == 0 {
			f.Fatalf("no corpus schema under %s (err %v)", dir, err)
		}
		for _, p := range paths {
			src, err := os.ReadFile(p)
			if err != nil {
				f.Fatal(err)
			}
			s, err := gql.New().Parse(bytes.NewReader(src))
			if err != nil {
				f.Fatalf("corpus schema %s: %v", p, err)
			}
			out[dir+"/"+filepath.Base(p)] = s
		}
	}
	return out
}

// seedFromManifest adds, for every query sweep.manifest.tsv names, the first
// pair it records as accepted and the first it records as refused, so the seeds
// reach both the model properties and the error class from every query.
func seedFromManifest(f *testing.F, schemas map[string]schema.Schema) {
	f.Helper()
	manifest, err := os.ReadFile(fuzzManifest)
	if err != nil {
		f.Fatal(err)
	}
	seeded := map[[2]string]bool{}
	n := 0
	for line := range strings.Lines(string(manifest)) {
		// cell, query, schema, verdict, sentinel, detail
		cols := strings.Split(strings.TrimSuffix(line, "\n"), "\t")
		if len(cols) != 6 || cols[0] != "cell" {
			continue
		}
		qname, sname, verdict := cols[1], cols[2], cols[3]
		if seeded[[2]string{qname, verdict}] {
			continue
		}
		if _, ok := schemas[sname]; !ok {
			f.Fatalf("%s names schema %s, which is not a corpus schema", fuzzManifest, sname)
		}
		src, err := os.ReadFile(filepath.Join(fuzzFixtureDir, qname))
		if err != nil {
			f.Fatal(err)
		}
		f.Add(append([]byte{fuzzCorpusArmOn}, sname+"\n"+string(src)...))
		seeded[[2]string{qname, verdict}] = true
		n++
	}
	if n == 0 {
		f.Fatalf("%s seeded nothing", fuzzManifest)
	}
}
