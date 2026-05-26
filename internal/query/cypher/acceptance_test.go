package cypher_test

import (
	"context"
	"crypto/sha1"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	gherkin "github.com/cucumber/gherkin/go/v26"
	"github.com/cucumber/godog"
	messages "github.com/cucumber/messages/go/v21"

	"github.com/antranig-yeretzian/gqlc/internal/query"
	"github.com/antranig-yeretzian/gqlc/internal/query/cypher"
)

// updateGolden regenerates the .golden.json snapshots from parser output. It is
// shared with parser_test.go (one -update flag for the whole package).
var updateGolden = flag.Bool("update", false, "regenerate golden snapshots from parser output")

// readCoreDirs are the TCK feature directories Stage 0 points godog at: the read
// core. The spec names match/return/where; in the vendored TCK the WHERE
// scenarios live under match-where (there is no standalone where dir at this
// tag). Later stages add dirs and shrink the skiplist; the corpus is never edited.
var readCoreDirs = []string{
	"../../../test/data/query/cypher/tck/features/clauses/match",
	"../../../test/data/query/cypher/tck/features/clauses/return",
	"../../../test/data/query/cypher/tck/features/clauses/match-where",
}

const goldenDir = "testdata/golden"

// skiplist excludes valid-but-out-of-scope scenarios by name. Empty for run A;
// populated in run B only if a scenario cannot be classified by the sentinel
// taxonomy alone. A skipped scenario is reported and counted, never deleted from
// the corpus.
var skiplist = map[string]bool{}

// the six public sentinels — the "valid Cypher we don't support yet" set. A
// positive scenario that fails with one of these is the progress meter (PENDING),
// not a test failure. Mirrors the spec's category-grained taxonomy.
var unsupportedSentinels = []error{
	cypher.ErrUnsupportedClause,
	cypher.ErrUnsupportedProjection,
	cypher.ErrUnsupportedPattern,
	cypher.ErrUnsupportedParameter,
}

func isUnsupported(err error) bool {
	for _, s := range unsupportedSentinels {
		if errors.Is(err, s) {
			return true
		}
	}
	return false
}

// scenarioState carries the parse outcome of the "When executing query" step to
// the "Then" steps, plus the scenario identity for golden snapshotting and the
// skiplist. It is held per-scenario in the context.
type scenarioState struct {
	name    string
	uri     string
	query   string
	got     query.Query
	err     error
	skipped bool
}

type stateKey struct{}

func stateFrom(ctx context.Context) *scenarioState {
	st, _ := ctx.Value(stateKey{}).(*scenarioState)
	return st
}

func TestReadCoreAcceptance(t *testing.T) {
	suite := godog.TestSuite{
		Name:                "cypher-read-core",
		ScenarioInitializer: initScenario,
		Options: &godog.Options{
			Format:   "pretty",
			Paths:    readCoreDirs,
			Strict:   true, // a PENDING/undefined step is a failure, never a silent pass
			TestingT: t,
		},
	}
	if suite.Run() != 0 {
		t.Fatal("read-core acceptance suite failed")
	}
}

func initScenario(ctx *godog.ScenarioContext) {
	ctx.Before(func(c context.Context, sc *godog.Scenario) (context.Context, error) {
		st := &scenarioState{name: sc.Name, uri: sc.Uri}
		if skiplist[sc.Name] {
			st.skipped = true
		}
		return context.WithValue(c, stateKey{}, st), nil
	})

	// Setup steps hold no graph, so they are no-ops (we are a parser, spec §7).
	ctx.Step(`^an empty graph$`, noop)
	ctx.Step(`^any graph$`, noop)
	ctx.Step(`^having executed:$`, noopDoc)
	ctx.Step(`^parameters are:$`, noopTable)

	// The query under test.
	ctx.Step(`^executing query:$`, executingQuery)

	// Positive outcomes: the scenario expected a result, so the query must parse.
	// The order qualifier is a non-capturing group: we don't bind it, and a
	// capturing group would force a string argument onto the step function.
	ctx.Step(`^the result should be(?:, in any order| \(ignoring element order for lists\)|, in order)?:$`, resultShouldBe)
	ctx.Step(`^no side effects$`, noSideEffects)
	ctx.Step(`^the side effects should be:$`, noopTable)

	// Negative outcomes: the scenario expected an error, so the query must be rejected.
	ctx.Step(`^a (\w+) should be raised at (compile time|runtime): (\w+)$`, shouldBeRejected)
	ctx.Step(`^a (\w+) should be raised at (compile time|runtime)$`, shouldBeRejectedNoDetail)
}

func noop(ctx context.Context) error                        { return nil }
func noopDoc(ctx context.Context, _ *godog.DocString) error { return nil }
func noopTable(ctx context.Context, _ *godog.Table) error   { return nil }

// executingQuery runs Parse and stashes the outcome for the Then steps.
func executingQuery(ctx context.Context, doc *godog.DocString) error {
	st := stateFrom(ctx)
	if st == nil {
		return errors.New("scenario state missing")
	}
	st.query = doc.Content
	st.got, st.err = cypher.New().Parse(strings.NewReader(doc.Content))
	return nil
}

// resultShouldBe asserts the positive contract: the query parsed and its model
// matches the golden snapshot. A rejection with an ErrUnsupported* sentinel is the
// progress meter (PENDING/skip); any other error fails — including the run-A stub
// (errNotImplemented), which is the genuine implementation-gap signal.
func resultShouldBe(ctx context.Context, _ *godog.Table) error {
	st := stateFrom(ctx)
	if st.skipped {
		return godog.ErrPending
	}
	if st.err != nil {
		if isUnsupported(st.err) {
			return godog.ErrPending // valid Cypher we don't support yet
		}
		return fmt.Errorf("expected a parsed query, got error: %w", st.err)
	}
	return checkGolden(st)
}

// noSideEffects is a positive corroborating step. It only needs the query to have
// parsed (or be a known-unsupported / skipped scenario); resultShouldBe carries
// the snapshot assertion, so here we just guard the outcome.
func noSideEffects(ctx context.Context) error {
	st := stateFrom(ctx)
	if st.skipped {
		return godog.ErrPending
	}
	if st.err != nil {
		if isUnsupported(st.err) {
			return godog.ErrPending
		}
		return fmt.Errorf("expected a parsed query, got error: %w", st.err)
	}
	return nil
}

// shouldBeRejected asserts the negative contract: any rejection passes, a parsed
// query fails. The error type/detail the TCK names targets a full engine; we are
// a parser, so we only assert rejection.
func shouldBeRejected(ctx context.Context, _ /*kind*/, _ /*phase*/, _ /*detail*/ string) error {
	return assertRejected(ctx)
}

func shouldBeRejectedNoDetail(ctx context.Context, _ /*kind*/, _ /*phase*/ string) error {
	return assertRejected(ctx)
}

func assertRejected(ctx context.Context) error {
	st := stateFrom(ctx)
	if st.skipped {
		return godog.ErrPending
	}
	if st.err == nil {
		return fmt.Errorf("expected the query to be rejected, but it parsed")
	}
	return nil
}

// checkGolden marshals the parsed query and compares it to its snapshot, keyed by
// a stable hash of the scenario URI and name. -update regenerates the snapshot.
func checkGolden(st *scenarioState) error {
	got, err := json.MarshalIndent(st.got, "", "  ")
	if err != nil {
		return err
	}
	path := goldenPath(st)

	if *updateGolden {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		return os.WriteFile(path, append(got, '\n'), 0o644)
	}

	want, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("missing golden snapshot (run go test -update): %w", err)
	}
	if strings.TrimRight(string(want), "\n") != string(got) {
		return fmt.Errorf("query model does not match golden %s", path)
	}
	return nil
}

func goldenPath(st *scenarioState) string {
	base := filepath.Base(st.uri)
	base = strings.TrimSuffix(base, filepath.Ext(base))
	sum := sha1.Sum([]byte(st.uri + "\x00" + st.name))
	return filepath.Join(goldenDir, fmt.Sprintf("%s_%x.golden.json", base, sum[:6]))
}

// harvestExecutingQueries reads every .feature file under dirs and returns the
// docstring of each "When executing query" step, deduplicated and sorted for a
// stable order. It reuses the gherkin parser (godog's dependency) so the harvest
// matches what the acceptance suite actually runs, rather than re-scanning text.
func harvestExecutingQueries(t *testing.T, dirs []string) []string {
	t.Helper()
	seen := make(map[string]bool)
	for _, dir := range dirs {
		files, err := filepath.Glob(filepath.Join(dir, "*.feature"))
		if err != nil {
			t.Fatalf("glob %s: %v", dir, err)
		}
		for _, path := range files {
			f, err := os.Open(path)
			if err != nil {
				t.Fatalf("open %s: %v", path, err)
			}
			doc, err := gherkin.ParseGherkinDocument(f, func() string { return "" })
			f.Close()
			if err != nil {
				t.Fatalf("parse %s: %v", path, err)
			}
			for _, p := range gherkin.Pickles(*doc, path, newIDGen()) {
				for _, step := range p.Steps {
					if isExecutingQueryStep(step) {
						seen[step.Argument.DocString.Content] = true
					}
				}
			}
		}
	}
	out := make([]string, 0, len(seen))
	for q := range seen {
		out = append(out, q)
	}
	sort.Strings(out)
	return out
}

func isExecutingQueryStep(step *messages.PickleStep) bool {
	return strings.Contains(step.Text, "executing query") &&
		step.Argument != nil && step.Argument.DocString != nil
}

// newIDGen returns a fresh incrementing id generator, required by Pickles.
func newIDGen() func() string {
	n := 0
	return func() string {
		n++
		return fmt.Sprintf("%d", n)
	}
}
