package cypher_test

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	gherkin "github.com/cucumber/gherkin/go/v26"
	"github.com/cucumber/godog"
	messages "github.com/cucumber/messages/go/v21"

	"github.com/areqag/gqlc/internal/query"
	"github.com/areqag/gqlc/internal/query/cypher"
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
	"../../../test/data/query/cypher/tck/features/clauses/return-skip-limit",
	"../../../test/data/query/cypher/tck/features/clauses/union",
	"../../../test/data/query/cypher/tck/features/clauses/with",
}

const goldenDir = "testdata/golden"

// skiplist excludes by name the negative TCK scenarios that this parser
// deliberately accepts: valid openCypher whose error lives on the other side of
// the type-interface boundary, so it is accept-and-ignored and caught at
// execution via the original text (ADR 0005, principle B1). Each entry is a
// TCK error this parser cannot and need not raise; later stages never need to
// remove them (they aren't "unsupported features"). A skipped scenario is
// reported and counted, never deleted from the corpus.
//
// The Stage 1 audit of clauses/return-skip-limit/ classified its 31 scenarios
// as 11 parse-green (snapshotted goldens), 4 PENDING via existing
// ErrUnsupportedClause/ErrUnsupportedProjection sentinels (WITH/UNWIND/
// aggregation in RETURN), 0 true parse-rejection, and 16 accept-then-runtime-
// or-compile-time-value-error scenarios listed below in two groups.
//
// Heterogeneous reasons-to-skip live together here; each entry is pinned to
// its actual cause rather than collapsed to a single rationale.
var skiplist = map[string]bool{
	// --- pattern semantics that live below the type-interface boundary ---
	//
	// MATCH (a)-[r]->()-[r]->(a): reusing a relationship variable is a runtime
	// uniqueness rule, not a type-interface concern. Spec Cluster C: relationship
	// reuse is not special-cased — first occurrence defines endpoints, later
	// occurrences merge labels.
	"[29] Fail when re-using a relationship in the same pattern": true,
	// WHERE count(a) > 10: an aggregation inside WHERE. The model mines WHERE only
	// for parameters and ignores predicate structure; aggregation is rejected only
	// as a RETURN item (ErrUnsupportedProjection), not in a predicate.
	"[15] Fail on aggregation in WHERE": true,

	// --- SKIP/LIMIT with a literal the TCK rejects as compile-time
	//     NonConstantExpression / NegativeIntegerArgument / InvalidArgumentType ---
	//
	// The value lives below the type-interface boundary (B1 — execution validates
	// the literal via the original text per ADR 0005), so this parser
	// accept-and-ignores. The TCK names these "compile time SyntaxError" but the
	// rejection is a value-constraint check, not a parse-shape check; an engine
	// reading our generated method body still sees the original SKIP -1 / LIMIT
	// 1.5 / SKIP n.count text and raises the same error.
	"[5] SKIP with an expression that depends on variables should fail": true,
	"[7] Negative SKIP should fail":                                     true,
	"[9] Floating point SKIP should fail":                               true,
	"[10] Fail when using non-constants in SKIP":                        true,
	"[11] Fail when using negative value in SKIP":                       true,
	"[9] Fail when using non-constants in LIMIT":                        true,
	"[12] Fail when using negative value in LIMIT 1":                    true,
	"[13] Fail when using negative value in LIMIT 2":                    true,
	"[16] Fail when using floating point in LIMIT 1":                    true,
	"[17] Fail when using floating point in LIMIT 2":                    true,

	// --- SKIP/LIMIT with a parameter whose runtime value the TCK rejects as
	//     NegativeIntegerArgument / InvalidArgumentType ---
	//
	// The parameter's name is what the model carries (a ClauseSlotUse on the
	// Parameter); the runtime-bound argument value lives below the type-interface
	// boundary (B1), so this parser accept-and-ignores. An engine reading the
	// generated method body sees the original SKIP $_skip / LIMIT $_limit text
	// and binds the caller's value, raising the same error.
	"[6] Negative parameter for SKIP should fail":                       true,
	"[8] Floating point parameter for SKIP should fail":                 true,
	"[10] Negative parameter for LIMIT should fail":                     true,
	"[11] Negative parameter for LIMIT with ORDER BY should fail":       true,
	"[14] Floating point parameter for LIMIT should fail":               true,
	"[15] Floating point parameter for LIMIT with ORDER BY should fail": true,

	// --- projection value/semantics below the type-interface boundary (Stage 3) ---
	//
	// Stage 3 widens RETURN to a projection sum (var/var.prop, scalar literal,
	// function, aggregate, RETURN *), so these negatives now parse-accept; their
	// error lives below the boundary and the re-executed original text raises it
	// (ADR 0005, B1).
	//
	// RETURN * with nothing in scope expands to zero columns: a scope/value error
	// (NoVariablesInScope), not a parse-shape one. We record ReturnsAll and the
	// resolver expands * post-freeze (spec §3).
	"[2] Fail when using RETURN * without variables in scope": true,
	// RETURN foo(a): the parser carries no function name (FuncProjection holds only
	// the referenced bindings, §2), so a non-existent function (UnknownFunction) is
	// not a distinction it can make — the engine re-executing foo(a) raises it.
	"[18] Fail on projecting a non-existent function": true,
	// RETURN 1 AS a, 2 AS a: duplicate column names are a value-level result-shape
	// check (ColumnNameConflict); Returns is duplicate-preserving (Stage-0 rule),
	// so two LiteralProjections both named "a" parse-accept.
	"[10] Fail when returning multiple columns with same name": true,

	// --- WITH/UNION value & result-shape errors below the boundary (Stage 4) ---
	//
	// Stage 4 adds WITH chaining (per-part scopes) and UNION (parallel branches),
	// so these negatives now parse-accept; each error is a value- or result-shape
	// rule the type-interface model does not carry (B1, ADR 0003), raised by the
	// re-executed original text (ADR 0005).
	//
	// WITH <literal> AS n / MATCH (n): n imports a name into the next part and is
	// re-bound there as a node; the conflict is that the WITH expression's value is
	// not a node (VariableTypeConflict). We model n's binding kind, not the type of
	// the projected expression, so the two reconcile structurally. (Scenario Outline
	// with 3 examples — true/123/123.4 — each pickle carries the same name.)
	"[11] Fail when matching a node variable bound to a value": true,
	// WITH <invalid> AS r / MATCH ()-[r]-(): the edge analogue of the node entry
	// above — r imports a name and is re-bound as a relationship; the conflict is
	// that the WITH expression's value is not a relationship (VariableTypeConflict).
	// We model r's binding kind, not the projected expression's type. Reachable only
	// at Stage 5 because the pattern is undirected (()-[r]-()); the error is the same
	// value-level rule below the type-interface boundary (B1, ADR 0003/0005).
	"[13] Fail when matching a relationship variable bound to a value": true,
	// RETURN 1 AS a UNION RETURN 2 AS b: the two branches expose different column
	// names (DifferentColumnsInUnion). Column compatibility across branches is not
	// modelled (ADR 0003); we record each branch's Returns verbatim.
	"[5] Failing when UNION has different columns":     true,
	"[5] Failing when UNION ALL has different columns": true,
	// Mixing UNION with UNION ALL in one query (InvalidClauseComposition): we record
	// the combinator sequence faithfully ([union, unionAll]); the no-mixing rule is a
	// clause-composition constraint, not a parse-shape one.
	"[1] Failing when mixing UNION and UNION ALL": true,
	"[2] Failing when mixing UNION ALL and UNION": true,
	// WITH 1 AS a, 2 AS a: duplicate forwarded column names (ColumnNameConflict),
	// the WITH analogue of the RETURN entry above; Returns is duplicate-preserving.
	"[4] Fail when forwarding multiple aliases with the same name": true,
	// WITH a, count(*): a non-aliased expression in WITH (NoExpressionAlias). We
	// synthesise a Name from the item's source text (here "count(*)"), so every WITH
	// item carries a name and the must-alias rule has nothing to check against.
	"[5] Fail when not aliasing expressions in WITH": true,
}

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
	// Non-strict: a PENDING scenario (valid Cypher out of Stage-0 scope) is the
	// progress meter and must not fail the suite. An UNDEFINED step (a phrasing we
	// have no step def for) would be a real harness gap, so it is guarded
	// separately by TestNoUndefinedSteps below — non-strict would otherwise let it
	// pass silently.
	suite := godog.TestSuite{
		Name:                "cypher-read-core",
		ScenarioInitializer: initScenario,
		Options: &godog.Options{
			Format:   "pretty",
			Paths:    readCoreDirs,
			Strict:   false,
			TestingT: t,
		},
	}
	if suite.Run() != 0 {
		t.Fatal("read-core acceptance suite failed")
	}
}

// undefinedStepsRe matches the pretty formatter's step summary when one or more
// steps had no definition (e.g. "1887 steps (… 3 undefined …)"). It is precise
// where a plain "undefined" substring is not — scenario titles legitimately
// contain the word (e.g. "Fail when returning an undefined variable").
var undefinedStepsRe = regexp.MustCompile(`\d+ undefined`)

// TestNoUndefinedSteps guards the harness gap that non-strict mode hides: every
// step in the read-core corpus must match a step definition. It runs the suite
// into a buffer (no TestingT, so subtests aren't re-emitted) and fails if the
// step summary reports any undefined step.
func TestNoUndefinedSteps(t *testing.T) {
	var buf bytes.Buffer
	godog.TestSuite{
		Name:                "cypher-read-core-stepcheck",
		ScenarioInitializer: initScenario,
		Options: &godog.Options{
			Format: "pretty",
			Paths:  readCoreDirs,
			Output: &buf,
		},
	}.Run()
	if undefinedStepsRe.MatchString(buf.String()) {
		t.Fatalf("undefined steps in the read-core corpus:\n%s", buf.String())
	}
}

// TestSkiplistOrphans guards against a stale skiplist entry: every key must
// match at least one scenario in the in-suite corpus. A TCK rename or reindex
// would orphan a key silently otherwise — the skiplist is consulted by name
// (acceptance_test.go's Before hook does `skiplist[sc.Name]`), so an unmatched
// key has no effect and the scenario it used to cover would surface as a
// regression. Mirrors TestNoUndefinedSteps's role as a harness-gap guard.
func TestSkiplistOrphans(t *testing.T) {
	seen := make(map[string]bool)
	for _, dir := range readCoreDirs {
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
			_ = f.Close()
			if err != nil {
				t.Fatalf("parse %s: %v", path, err)
			}
			for _, p := range gherkin.Pickles(*doc, path, newIDGen()) {
				seen[p.Name] = true
			}
		}
	}
	for name := range skiplist {
		if !seen[name] {
			t.Errorf("skiplist entry %q matched no scenario — TCK rename or stale entry?", name)
		}
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

func noop(_ context.Context) error                        { return nil }
func noopDoc(_ context.Context, _ *godog.DocString) error { return nil }
func noopTable(_ context.Context, _ *godog.Table) error   { return nil }

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
			_ = f.Close()
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
