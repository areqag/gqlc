package cypher_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"pgregory.net/rapid"

	"github.com/areqag/gqlc/internal/fuzzkit"
	"github.com/areqag/gqlc/internal/procsig"
	"github.com/areqag/gqlc/internal/query"
	"github.com/areqag/gqlc/internal/query/cypher"
	"github.com/areqag/gqlc/internal/queryfile"
)

// tckSeedCap bounds the TCK queries FuzzCypherParse seeds from, so that its
// seed pass keeps `just test` fast (docs/specs/nightly-fuzzing-plan.md §5, T1).
const tckSeedCap = 500

// fuzzRegistry is the procedure registry every FuzzCypherParse call parses
// under. Without one, every CALL is refused at the name lookup and nothing
// past it is reached; the canonical procsig fixture declares the test.*
// procedures the TCK's CALL scenarios name.
const fuzzRegistry = "../../procsig/testdata/canonical.procsig.json"

// parseResult is one Parse call's whole output, compared field by field for
// determinism.
type parseResult struct {
	q   query.Query
	err error
}

// FuzzCypherParse is target T1 of docs/specs/nightly-fuzzing-plan.md. Beyond
// "Parse does not panic" it checks, on every input:
//
//   - property zero on error: a non-nil error comes with the zero Query;
//   - property error class: an error is a cypher sentinel or a syntax error,
//     never a plain error and never one carrying the "bug:" marker;
//   - property model invariants: a parsed Query satisfies parser_test.go's
//     assertReferentialIntegrity, assertNamedBindingsUnique and
//     assertParametersDeduped;
//   - property marshal: a parsed Query marshals with encoding/json;
//   - property determinism: two Parse calls on the same text return
//     reflect.DeepEqual results and the same error text.
//
// Each call runs under fuzzkit.Guard's 2 s limit, so a slow or hung input is a
// finding as well.
func FuzzCypherParse(f *testing.F) {
	fuzzkit.SeedFiles(f,
		"../../../test/data/resolver/valid/*.cypher",
		"../../../test/data/resolver/invalid/*.cypher",
	)
	seedQueryFileBodies(f,
		"../../../test/data/codegen/valid/*/queries.cypher",
		"../../../test/data/codegen/invalid/*/queries.cypher",
	)
	for _, q := range everyNth(harvestExecutingQueries(f, readCoreDirs), tckSeedCap) {
		f.Add(q)
	}

	reg, err := procsig.Load(fuzzRegistry)
	if err != nil {
		f.Fatalf("procedure registry %s: %v", fuzzRegistry, err)
	}
	parser := cypher.New(cypher.WithRegistry(reg))
	parse := func(src string) parseResult {
		q, err := parser.Parse(strings.NewReader(src))
		return parseResult{q: q, err: err}
	}

	f.Fuzz(func(t *testing.T, src string) {
		fuzzkit.Skip(t, []byte(src))
		var first, second parseResult
		fuzzkit.Guard(t, 2*time.Second, func() { first = parse(src) })
		fuzzkit.Guard(t, 2*time.Second, func() { second = parse(src) })

		if !sameResult(first, second) {
			t.Fatalf("property determinism: two Parse calls on %q disagree:\nfirst:  %#v, %v\nsecond: %#v, %v",
				src, first.q, first.err, second.q, second.err)
		}
		if first.err != nil {
			if !reflect.DeepEqual(first.q, query.Query{}) {
				t.Fatalf("property zero on error: Parse(%q) failed with %v but returned a non-zero Query %#v",
					src, first.err, first.q)
			}
			if c := classify(src, first.err); c != fuzzkit.ClassSentinel && c != fuzzkit.ClassSyntax {
				t.Fatalf("property error class: Parse(%q) returned a %s error: %v", src, c, first.err)
			}
			return
		}
		if _, err := json.Marshal(first.q); err != nil {
			t.Fatalf("property marshal: Parse(%q) returned a Query json.Marshal refuses: %v", src, err)
		}
		// The three invariants are written against *rapid.T. MakeFuzz runs a
		// property exactly once over the given bytes and reports a rapid
		// Fatalf through t; nil bytes are enough because the property draws
		// nothing.
		rapid.MakeFuzz(func(rt *rapid.T) {
			assertReferentialIntegrity(rt, first.q, src)
			assertNamedBindingsUnique(rt, first.q, src)
			assertParametersDeduped(rt, first.q, src)
		})(t, nil)
	})
}

// sameResult compares two Parse results: the Queries by reflect.DeepEqual, the
// errors by what a caller can observe of them, their text and which sentinels
// errors.Is finds.
func sameResult(a, b parseResult) bool {
	if !reflect.DeepEqual(a.q, b.q) || (a.err == nil) != (b.err == nil) {
		return false
	}
	if a.err == nil {
		return true
	}
	if a.err.Error() != b.err.Error() {
		return false
	}
	for _, s := range cypher.AllSentinels() {
		if errors.Is(a.err, s) != errors.Is(b.err, s) {
			return false
		}
	}
	return true
}

// isSyntaxError is the cypher parser's syntax-error shape: the listener's
// SyntaxError sink formats every ANTLR lexer and parser error as
// "syntax error at LINE:COL ..." and returns it unwrapped.
func isSyntaxError(err error) bool {
	return strings.HasPrefix(err.Error(), "syntax error at ")
}

// classify is fuzzkit.ClassifyError with one correction. The parser quotes
// the offending token, or a name from the query, in its messages, so an input
// that itself contains "bug:" can produce an error carrying the marker that
// no fail site wrote. When the input contains the marker, the error's text is
// read with the input's occurrences defused; errors.Is still sees the original
// chain. A fail site's own "bug:" in such an input is missed: the cost is
// confined to inputs that spell the marker.
func classify(src string, err error) fuzzkit.ErrClass {
	if strings.Contains(src, "bug:") {
		err = defused{err}
	}
	return fuzzkit.ClassifyError(err, isSyntaxError, cypher.AllSentinels()...)
}

type defused struct{ error }

func (d defused) Error() string { return strings.ReplaceAll(d.error.Error(), "bug:", "bug․") }
func (d defused) Unwrap() error { return d.error }

// seedQueryFileBodies adds each query of every annotated query file matching
// the globs, split by the queryfile front end the pipeline uses. A file that
// front end refuses is added whole. Like fuzzkit.SeedFiles, a glob matching
// no file fails f.
func seedQueryFileBodies(f *testing.F, globs ...string) {
	f.Helper()
	for _, g := range globs {
		paths, err := filepath.Glob(g)
		if err != nil || len(paths) == 0 {
			f.Fatalf("seedQueryFileBodies: glob %q matched no file (err %v)", g, err)
		}
		for _, p := range paths {
			b, err := os.ReadFile(p)
			if err != nil {
				f.Fatal(err)
			}
			qs, err := queryfile.New().Parse(strings.NewReader(string(b)))
			if err != nil {
				f.Add(string(b))
				continue
			}
			for _, q := range qs {
				f.Add(q.Text)
			}
		}
	}
}

// everyNth returns at most n of xs, evenly spaced, in order: a deterministic
// sample, so the seed set is the same on every run.
func everyNth(xs []string, n int) []string {
	if len(xs) <= n {
		return xs
	}
	out := make([]string, 0, n)
	for i := range n {
		out = append(out, xs[i*len(xs)/n])
	}
	return out
}
