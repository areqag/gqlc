package age_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/ast"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"

	"github.com/areqag/gqlc/internal/codegen"
	"github.com/areqag/gqlc/internal/codegen/age"
	"github.com/areqag/gqlc/internal/graph"
	"github.com/areqag/gqlc/internal/procsig"
	"github.com/areqag/gqlc/internal/query/cypher"
	"github.com/areqag/gqlc/internal/queryfile"
	"github.com/areqag/gqlc/internal/resolver"
	"github.com/areqag/gqlc/internal/schema"
	"github.com/areqag/gqlc/internal/schema/gql"
)

// captureProbeParam is the parameter name the baseline emission binds
// while the candidate set is read off it. It is deliberately a name no
// emission produces, so the candidates are the fixture's own vocabulary
// rather than one the probe seeded — and feeding it back through the
// sweep is a fixed point, which is the cheapest check that the probe is
// not itself capturing anything.
const captureProbeParam = "gqlcCaptureProbe"

// renamePrefix is prepended to every parameter a query text spells when
// the emission is perturbed. It preserves distinctness under the §4.2
// mangle — the mangle splits on underscores and concatenates, and a
// common prefix cannot make two names that differed collide — so a
// rename never turns a served batch into ErrParamNameCollision.
const renamePrefix = "renamed"

// ageFixture is one valid-corpus fixture this backend is enrolled in,
// loaded through the same front end the conformance suite drives:
// queryfile, then the Cypher parser, then the resolver. Reading the
// corpus rather than hand-building emission shapes is what makes the
// sweeps below cover every shape the corpus carries — multi-column rows,
// entity returns, edge unions and whatever lands next — and what makes a
// fixture added later covered on arrival with no edit here.
type ageFixture struct {
	name    string
	pkg     string
	schema  schema.Schema
	queries []codegen.NamedQuery
}

// ageManifest is the slice of a fixture's manifest.json this package
// reads. Enrolment is by target key, so a fixture reaches these sweeps
// only by naming this backend — the same rule the golden corpus applies.
type ageManifest struct {
	Package    string   `json:"package"`
	QueryFiles []string `json:"queryFiles"`
	Targets    []string `json:"targets"`
}

// ageCorpus loads every enrolled fixture once per process. The front end
// it runs is not cheap and the sweeps below walk the corpus twice.
var ageCorpus = sync.OnceValues(loadAgeCorpus)

// loadAgeCorpus reads valid/*/ and returns the fixtures enrolled in this
// backend, in directory order.
func loadAgeCorpus() ([]ageFixture, error) {
	dirs, err := filepath.Glob(filepath.Join(corpusRoot, "valid", "*"))
	if err != nil {
		return nil, err
	}
	reg, err := procsig.NewRegistry(nil)
	if err != nil {
		return nil, err
	}

	var out []ageFixture
	for _, dir := range dirs {
		m, err := readAgeManifest(dir)
		if err != nil {
			return nil, err
		}
		if !slices.Contains(m.Targets, ageTarget) {
			continue
		}
		sch, err := readCorpusSchema(dir)
		if err != nil {
			return nil, err
		}
		queries, err := readCorpusQueries(dir, m, sch, reg)
		if err != nil {
			return nil, err
		}
		out = append(out, ageFixture{
			name:    filepath.Base(dir),
			pkg:     m.Package,
			schema:  sch,
			queries: queries,
		})
	}
	return out, nil
}

// readAgeManifest reads one fixture's manifest.json.
func readAgeManifest(dir string) (ageManifest, error) {
	src, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		return ageManifest{}, err
	}
	var m ageManifest
	if err := json.Unmarshal(src, &m); err != nil {
		return ageManifest{}, fmt.Errorf("%s: %w", dir, err)
	}
	return m, nil
}

// readCorpusSchema parses one fixture's schema.gql.
func readCorpusSchema(dir string) (schema.Schema, error) {
	src, err := os.ReadFile(filepath.Join(dir, "schema.gql"))
	if err != nil {
		return schema.Schema{}, err
	}
	sch, err := gql.New().Parse(bytes.NewReader(src))
	if err != nil {
		return schema.Schema{}, fmt.Errorf("%s/schema.gql: %w", dir, err)
	}
	return sch, nil
}

// readCorpusQueries turns one fixture's declared query files into the
// resolved batch codegen takes.
func readCorpusQueries(dir string, m ageManifest, sch schema.Schema, reg procsig.Registry) ([]codegen.NamedQuery, error) {
	res := resolver.New(sch, resolver.WithRegistry(reg))
	var out []codegen.NamedQuery
	for _, qf := range m.QueryFiles {
		src, err := os.ReadFile(filepath.Join(dir, qf))
		if err != nil {
			return nil, err
		}
		parsed, err := queryfile.New().Parse(bytes.NewReader(src))
		if err != nil {
			return nil, fmt.Errorf("%s/%s: %w", dir, qf, err)
		}
		for _, aq := range parsed {
			q, err := cypher.New(cypher.WithRegistry(reg)).Parse(strings.NewReader(aq.Text))
			if err != nil {
				return nil, fmt.Errorf("%s/%s: query %s: %w", dir, qf, aq.Name, err)
			}
			vq, err := res.Resolve(q)
			if err != nil {
				return nil, fmt.Errorf("%s/%s: query %s: %w", dir, qf, aq.Name, err)
			}
			out = append(out, codegen.NamedQuery{
				Name:        aq.Name,
				Cardinality: aq.Cardinality,
				SourceFile:  qf,
				SourceText:  aq.Text,
				Validated:   vq,
			})
		}
	}
	return out, nil
}

// ageFixtures is the enrolled corpus, held to being non-empty: a sweep
// over nothing agrees with every emission there could have been.
func (s *EmissionSuite) ageFixtures() []ageFixture {
	fixtures, err := ageCorpus()
	s.Require().NoError(err)
	s.Require().NotEmpty(fixtures, "no corpus fixture is enrolled in %s", ageTarget)
	return fixtures
}

// TestNoEmittedNameTakesAQueryParameterName pins the one identifier an
// emitted method's signature does not choose. The single-parameter form
// names its argument after the parameter the author wrote, so anything
// the method resolves under that name resolves to the caller's value
// instead. Declaring a name is not what puts it at risk — resolving it
// is, and a body resolves the package-level query-text const without
// ever declaring it.
//
// Neither half is reliably a compile error. A shadowed body local
// against a STRING property lets the composition assign the SQL text
// over the argument; a shadowed query-text const is worse, because the
// argument silently becomes the statement — $<bare>QueryText makes
// Method(ctx, "MATCH (n) DETACH DELETE n") run that text, with no
// concatenation anywhere to find.
//
// Two axes, and both are read rather than listed. The candidate names
// are every identifier the emission mentions, taken off the syntax tree,
// so a name the emitter starts using later is covered without anyone
// remembering to add it. The emission shapes are the golden corpus's,
// so a multi-column row, an entity return, an edge union and every other
// shape a fixture carries are swept as they stand — and a fixture added
// later is swept on arrival. A sweep over hand-built shapes covers only
// the branches whoever wrote it happened to reach: a declaration emitted
// under `len(p.RowFields) >= 2` is invisible to a batch with no
// two-column query, which is the same fixtures-shaped-so-the-bug-cannot-
// appear failure one level up.
//
// Every query in the batch is rebound to a single parameter, because the
// single-parameter form is the only one that puts an author-chosen
// identifier in a method's scope at all. The perturbation test below is
// what holds that true rather than assumed. Rebinding leaves each
// query's row shape exactly as the corpus wrote it, which is the axis
// under test here.
func (s *EmissionSuite) TestNoEmittedNameTakesAQueryParameterName() {
	multiColumn := 0
	for _, fx := range s.ageFixtures() {
		baseline := s.emitCapturing(fx, captureProbeParam)
		if len(baseline) == 0 {
			// A fixture declaring no query emits no method, so it has no
			// scope a parameter is bound in. Its schema surface is swept
			// by the conformance corpus, not here.
			continue
		}
		multiColumn += multiColumnQueries(fx.queries)

		s.Run(fx.name, func() {
			candidates := s.candidateNames(baseline)
			s.Require().NotEmpty(candidates, "the emission mentions no identifiers to check")
			for _, name := range candidates {
				s.Run(name, func() {
					emitted := s.emitCapturing(fx, name)
					for _, path := range sortedKeys(emitted) {
						s.requireNameIsUncapturable(emitted[path], name, path)
					}
				})
			}
		})
	}

	// A sweep that never reaches the `len(p.RowFields) >= 2` branch is
	// the hole this test was widened to close, and it would otherwise
	// close silently — so the count above is that branch's own condition,
	// and it is read off the batch rather than off the emission.
	//
	// Counting package-level types in the emitted files says the same
	// thing today — under one parameter per query a Params struct cannot
	// be emitted, so a type in a .cypher.go file is a Row struct — and
	// stops saying it the day the emitter adds a package-level type for
	// any other reason. A census an unrelated emitter change can satisfy
	// without bringing a multi-column fixture with it is a census
	// satisfied vacuously, which is the failure this whole test exists to
	// close, reappearing inside the fix.
	s.Require().Positive(multiColumn,
		"no swept fixture projects two columns, so the multi-column emission branch is unswept")
}

// requireNameIsUncapturable holds one emitted file against one candidate
// parameter name: the signature still names its argument after the
// query's parameter, no body local shadows that argument, and every
// package-level name the file declares is still resolved by some method.
func (s *EmissionSuite) requireNameIsUncapturable(body, name, path string) {
	// The surface cannot move to buy the emission room. Only names the
	// mangle leaves alone can be compared directly: it splits on
	// underscores and capitalises, so those are the ones a query text
	// can put in scope verbatim.
	if !strings.Contains(name, "_") && name == codegen.LowerFirstRune(name) {
		for _, local := range s.paramLocalsOf(body) {
			s.Require().Equal(name, local,
				"%s: the signature must still name its argument after the query's parameter", path)
		}
	}
	s.Require().NotContains(s.bodyLocalsOf(body), name,
		"%s: a body local shadows the caller's argument", path)
	s.requireNothingDeclaredIsCaptured(body)
}

// candidateNames is every identifier the emitted query files mention,
// deduplicated across the batch and ordered. Deliberately the widest set
// the syntax tree offers rather than a list of names anyone thought of.
func (s *EmissionSuite) candidateNames(files map[string]string) []string {
	seen := make(map[string]bool)
	for _, body := range files {
		for _, name := range s.identsOf(body) {
			seen[name] = true
		}
	}
	out := make([]string, 0, len(seen))
	for name := range seen {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// multiColumnQueries counts the queries in a batch that project two or
// more columns.
//
// This is `len(p.RowFields) >= 2` — the condition the multi-column
// emission branch keys on — evaluated on the branch's own input: Phase B
// appends exactly one RowField per entry in Validated.Columns and fails
// the whole batch rather than dropping one, so a query's column count is
// its prepared row-field count. Measuring the input is what makes the
// census unsatisfiable by anything the emitter does, which counting the
// emission's own declarations is not.
func multiColumnQueries(queries []codegen.NamedQuery) int {
	count := 0
	for _, q := range queries {
		if len(q.Validated.Columns) >= 2 {
			count++
		}
	}
	return count
}

// emitCapturing emits one fixture with every query rebound to a single
// STRING parameter of the given name, and returns its query files by
// path. STRING is the width that makes a collision silent: it is what
// the composed statement and the query-text const both are, so an
// argument that displaces one of them is assigned over rather than
// rejected by the compiler.
func (s *EmissionSuite) emitCapturing(fx ageFixture, param string) map[string]string {
	queries := slices.Clone(fx.queries)
	for i := range queries {
		queries[i].Validated.Parameters = []resolver.ResolvedParameter{
			{Name: param, Type: resolver.ResolvedProperty{Type: graph.TypeString}},
		}
	}
	return s.emitQueryFiles(fx, queries, param)
}

// emitQueryFiles generates a fixture's batch and returns the
// <name>.cypher.go files by path. Those are the files a query parameter
// is in scope in: a name declared in another file of the package is
// reachable from a method body too, but capturing one is a hard failure
// of the generated package rather than a silent one (gqlc-ni66).
func (s *EmissionSuite) emitQueryFiles(fx ageFixture, queries []codegen.NamedQuery, label string) map[string]string {
	files, err := age.New(age.WithPackageName(fx.pkg)).Generate(codegen.Input{Schema: fx.schema, Queries: queries})
	s.Require().NoError(err, "fixture %s under %s", fx.name, label)
	out := make(map[string]string)
	for _, f := range files {
		if strings.HasSuffix(f.Path, ".cypher.go") {
			out[f.Path] = string(f.Contents)
		}
	}
	return out
}

// TestOnlyTheSingleParameterFormPutsAQueryChosenNameInScope pins the
// premise the sweep above rests on, and closes the shape the sweep
// cannot reach. A method binding two or more parameters takes them in a
// generated Params struct under a generated binder, so nothing the
// author spelled is resolvable in its body — which is why rebinding
// every query to one parameter loses no coverage, and why the Params
// shape needs no sweep of its own.
//
// Held by perturbation rather than by a name list: renaming every
// parameter a query text spells must move the identifiers in scope for
// the single-parameter form and must move none of them for any other
// arity. A binder named after the query would fail this, whatever it
// happened to be called, and so would a body local derived from a
// parameter name — neither has to be predicted here to be caught.
func (s *EmissionSuite) TestOnlyTheSingleParameterFormPutsAQueryChosenNameInScope() {
	singleParam, multiParam := 0, 0
	for _, fx := range s.ageFixtures() {
		if len(fx.queries) == 0 {
			continue
		}
		// Counted outside the subtest so the census below measures the
		// corpus rather than whatever `go test -run` selected from it.
		params := parameterCounts(fx.queries)
		for _, count := range params {
			switch {
			case count == 1:
				singleParam++
			case count >= 2:
				multiParam++
			}
		}

		s.Run(fx.name, func() {
			before := s.emitQueryFiles(fx, fx.queries, "the corpus parameters")
			after := s.emitQueryFiles(fx, renameParameters(fx.queries), "renamed parameters")
			s.Require().Equal(sortedKeys(before), sortedKeys(after),
				"renaming parameters changed which files the batch emits")

			for _, path := range sortedKeys(before) {
				scopesBefore, scopesAfter := s.methodScopes(before[path]), s.methodScopes(after[path])
				for _, method := range sortedKeys(scopesBefore) {
					want := scopesBefore[method]
					got, ok := scopesAfter[method]
					s.Require().True(ok, "%s: method %s disappeared under a parameter rename", path, method)

					count, known := params[method]
					s.Require().True(known, "%s: emitted method %s has no query in the batch", path, method)

					if count == 1 {
						s.Require().NotEqual(want, got,
							"%s: method %s resolves the same names whatever the parameter is called, "+
								"so the single-parameter form no longer takes its argument from the query",
							path, method)
						continue
					}
					s.Require().Equal(want, got,
						"%s: method %s resolves a different set of names when the query's parameters are renamed, "+
							"so an author-chosen identifier is in the scope its body resolves in",
						path, method)
				}
			}
		})
	}

	// Both arities have to be present or half the assertion above held
	// nothing. A zero-parameter method is not counted: renaming a batch
	// that spells no parameter moves nothing by construction, so it
	// satisfies the invariant without measuring it.
	s.Require().Positive(singleParam,
		"no method in the corpus binds exactly one parameter, so the sweep's premise is untested")
	s.Require().Positive(multiParam,
		"no method in the corpus binds two or more parameters, so the Params shape is unmeasured")
}

// methodScopes maps each emitted method to the sorted set of identifiers
// it resolves in a scope a parameter is bound in — every identifier it
// mentions, less the selector suffixes and struct-literal keys that
// resolve against a type instead. Bound and free alike: a parameter can
// capture a name the body reads, and a body local named after a
// parameter would move with it.
func (s *EmissionSuite) methodScopes(body string) map[string][]string {
	out := make(map[string][]string)
	for _, decl := range s.parseEmission(body).Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		names := referencedIdents(fn)
		sort.Strings(names)
		out[fn.Name.Name] = slices.Compact(names)
	}
	return out
}

// renameParameters returns the batch with every parameter a query text
// spells renamed, leaving every other axis — columns, cardinality,
// source text — as the corpus wrote it.
func renameParameters(queries []codegen.NamedQuery) []codegen.NamedQuery {
	out := slices.Clone(queries)
	for i := range out {
		renamed := slices.Clone(out[i].Validated.Parameters)
		for j := range renamed {
			renamed[j].Name = renamePrefix + renamed[j].Name
		}
		out[i].Validated.Parameters = renamed
	}
	return out
}

// parameterCounts maps each query's name — which is verbatim the name of
// the method emitted for it — to the number of parameters its text
// spells.
func parameterCounts(queries []codegen.NamedQuery) map[string]int {
	out := make(map[string]int, len(queries))
	for _, q := range queries {
		out[q.Name] = len(q.Validated.Parameters)
	}
	return out
}

// sortedKeys orders a file map's paths so a sweep over it reports the
// same file first every run.
func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
