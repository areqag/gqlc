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
	"strconv"
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
// A third axis is parameter arity. The emitter switches on it — nothing,
// one parameter, or a generated Params struct — and each arm writes its
// own signature, so a capture one arm performs is invisible to a sweep
// that only ever binds another. Every candidate is therefore swept at
// every shape in captureArities, and at more than one position within
// the multi-parameter shapes, because an arm that named its argument
// after the FIRST parameter and an arm that named it after the LAST are
// different bugs and only one of them is caught by sweeping position 0.
// Rebinding leaves each query's row shape exactly as the corpus wrote
// it, which is the axis the corpus is carrying.
func (s *EmissionSuite) TestNoEmittedNameTakesAQueryParameterName() {
	multiColumn := 0
	for _, fx := range s.ageFixtures() {
		// The candidate set is read off the one-parameter shape so it is
		// the fixture's own vocabulary. Reading it off a multi-parameter
		// emission would pull the filler parameters' own field names in,
		// and feeding those back would collide under the §4.2 mangle
		// rather than measuring anything.
		baseline := s.emitCapturing(fx, captureProbeParam, singleCaptureArity)
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
					for _, shape := range captureArities {
						s.Run(shape.label, func() {
							s.requirePackageIsUncapturable(
								s.emitCapturingPackage(fx, captureProbeParam, shape),
								s.emitCapturingPackage(fx, name, shape), name)
						})
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
// parameter name: the signature names its argument itself whatever the
// query said, no body local shadows that argument, and every
// package-level name the file declares is still resolved by some method.
//
// The candidate name is not itself excluded from the body's locals, and
// must not be: every body local is generator-owned and positionally
// named, so the sweep feeds names like err and stmt straight back in. A
// body local called err is not a capture — the parameter is not an
// identifier the body resolves at all, which is the whole point. What
// would be a capture is that local displacing the caller's argument, and
// the argument is codegen.ParamArg whatever the query said, so that is
// the name the shadow check is anchored on.
func (s *EmissionSuite) requireNameIsUncapturable(body, name, path string) {
	for _, local := range s.paramLocalsOf(body) {
		s.Require().Equal(codegen.ParamArg, local,
			"%s: the signature took its argument name from the query text (parameter %q)", path, name)
	}
	s.Require().NotContains(s.bodyLocalsOf(body), codegen.ParamArg,
		"%s: a body local shadows the caller's argument", path)
}

// requirePackageIsUncapturable is requireNameIsUncapturable over a whole
// emitted package, plus the one check that cannot be asked of a single
// file: reachability.
//
// The signature and body-local checks are per file and stay per file — a
// method captures in the file it is written in, and they read a query
// method's shape, which db.go's WithTx does not have. Reachability is
// not per file. Go's package scope is package-wide, so "is this
// declaration still resolved by somebody" has to be asked of every
// file's declarations against every file's method bodies at once. Asked
// per file it reports two different falsehoods: db.go's New and Queries
// are resolved only from .cypher.go bodies and read as captured, while a
// string-typed const declared in db.go and captured out of a .cypher.go
// body reads as fine.
//
// gqlc-dfcb is that second falsehood. Before this, the sweep's parse set
// was the .cypher.go files alone, so a package-level declaration emitted
// into db.go or models.go was never a candidate for capture at all. That
// was safe only by accident of typing: the cross-file set happened to be
// error-typed, type-named, func-named or unreferenced, so a string
// parameter shadowing one failed to compile. A string-typed cross-file
// declaration read from a method body is the silent case, and it is the
// same shape as the query-text const — the worst instance of this class.
//
// The reachability claim is DIFFERENTIAL, against the same batch emitted
// under a generated parameter name, and that is forced by widening the
// parse set. Capture's signature is a declaration a method used to
// resolve and no longer does; the per-file version approximated that
// with the absolute claim "every declaration is resolved by somebody",
// which happens to hold inside a .cypher.go file and does not hold of
// the package. ReadQuerier is the witness: it is the consumer-facing
// interface, resolved by the caller and by nothing the emitter writes,
// so the absolute claim calls every AGE package captured. An allowlist
// of consumer-facing names would have to be maintained by hand and would
// admit the next one silently; the differential claim needs no list and
// is strictly stronger, because a name the baseline does not resolve
// cannot be captured out of a body that never read it.
func (s *EmissionSuite) requirePackageIsUncapturable(baseline, files map[string]string, name string) {
	paths := sortedKeys(files)
	s.Require().NotEmpty(paths, "the emission has no files to sweep")

	queryFiles := 0
	for _, path := range paths {
		// The signature and body-local checks read a query method's
		// shape — ctx plus exactly one generated argument — so they are
		// asked of the query files alone.
		if strings.HasSuffix(path, ".cypher.go") {
			queryFiles++
			s.requireNameIsUncapturable(files[path], name, path)
		}
	}

	// The per-file half is conditional on a suffix, and a conditional
	// guard is silent when its condition never holds. A batch reaching
	// here has queries — the caller skips a fixture whose one-parameter
	// emission is empty — so at least one query file must have been read.
	s.Require().Positive(queryFiles, "no .cypher.go file was swept, so the per-file half ran on nothing")

	wantDeclared, wantResolved := s.packageScope(baseline)
	gotDeclared, gotResolved := s.packageScope(files)

	// A sweep over a collapsed parse set agrees with every emission there
	// could have been, so the two halves of the differential are pinned
	// non-empty before they are compared.
	s.Require().NotEmpty(wantDeclared, "the baseline package declares nothing at package level")
	s.Require().NotEmpty(wantResolved, "no method in the baseline package resolves a package-level name")

	s.Require().Equal(wantDeclared, gotDeclared,
		"renaming a query parameter to %q changed what the emitted package declares", name)
	s.Require().Equal(wantResolved, gotResolved,
		"renaming a query parameter to %q changed which package-level names the emitted package's "+
			"methods resolve: the caller's argument captured one", name)
}

// packageScope reads an emitted package's two name sets: what it declares
// at package level, and which of those declarations some method in the
// package resolves. Both sorted, both package-wide, both read off the
// syntax tree.
//
// The second is intersected with the first on purpose. A method resolves
// plenty of names the package does not declare — imported ones, universe
// ones — and those are gqlc-ni66's class, loud rather than silent,
// tracked elsewhere. Leaving them in would make the differential move
// whenever an unrelated import did.
func (s *EmissionSuite) packageScope(files map[string]string) (declared, resolved []string) {
	declaredSet := make(map[string]bool)
	free := make(map[string]bool)
	for _, path := range sortedKeys(files) {
		file := s.parseEmission(files[path])
		for _, decl := range packageDecls(file) {
			declaredSet[decl] = true
		}
		for _, d := range file.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok {
				continue
			}
			for ident := range freeIdents(fn) {
				free[ident] = true
			}
		}
	}
	for decl := range declaredSet {
		declared = append(declared, decl)
		if free[decl] {
			resolved = append(resolved, decl)
		}
	}
	sort.Strings(declared)
	sort.Strings(resolved)
	return declared, resolved
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

// captureArity is one parameter shape the sweep rebinds a batch to:
// arity many parameters, with the swept name at index swept and
// generated fillers elsewhere.
type captureArity struct {
	label string
	arity int
	swept int
}

// captureArities is every signature arm the emitter has that binds a
// parameter at all, plus a position within the multi-parameter arm that
// is not the first. The zero-parameter arm is not here: it emits no
// argument, so there is no name for a query to capture, and binding zero
// parameters would also make the sweep's own candidate emission empty.
var captureArities = []captureArity{
	{label: "one", arity: 1, swept: 0},
	{label: "first-of-two", arity: 2, swept: 0},
	{label: "last-of-three", arity: 3, swept: 2},
}

// singleCaptureArity is the shape the candidate set is read off.
var singleCaptureArity = captureArities[0]

// captureFillerPrefix names the parameters that pad a multi-parameter
// shape out. It is a name no emission produces, so a filler's own §4.2
// field name never lands back in the candidate set, and it cannot
// collide with a swept candidate under the mangle.
const captureFillerPrefix = "gqlcCaptureFiller"

// capturingBatch rebinds every query in a fixture to the given parameter
// shape, carrying the swept name at that shape's swept index, and
// returns the batch with the label an emission failure should print.
//
// Every parameter is STRING, which is the width that makes a collision
// silent: it is what the composed statement and the query-text const
// both are, so an argument that displaces one of them is assigned over
// rather than rejected by the compiler. A narrower width would turn the
// interesting case into a build failure and the sweep would stop
// measuring it.
func (s *EmissionSuite) capturingBatch(fx ageFixture, param string, shape captureArity) ([]codegen.NamedQuery, string) {
	s.Require().Positive(shape.arity, "shape %s binds no parameter", shape.label)
	s.Require().Less(shape.swept, shape.arity, "shape %s sweeps past its own arity", shape.label)

	params := make([]resolver.ResolvedParameter, 0, shape.arity)
	for i := range shape.arity {
		name := fmt.Sprintf("%s%d", captureFillerPrefix, i)
		if i == shape.swept {
			name = param
		}
		params = append(params, resolver.ResolvedParameter{
			Name: name,
			Type: resolver.ResolvedProperty{Type: graph.TypeString},
		})
	}

	queries := slices.Clone(fx.queries)
	for i := range queries {
		queries[i].Validated.Parameters = params
	}
	return queries, fmt.Sprintf("%s as %s", param, shape.label)
}

// emitCapturing emits a capturing batch and returns its QUERY files by
// path. Used to read a fixture's own vocabulary, which is what the
// .cypher.go files carry: reading it off the whole package would feed
// db.go's boilerplate names back into the sweep at every fixture, which
// multiplies the runtime by the size of a set that does not vary between
// fixtures.
func (s *EmissionSuite) emitCapturing(fx ageFixture, param string, shape captureArity) map[string]string {
	queries, label := s.capturingBatch(fx, param, shape)
	return s.emitQueryFiles(fx, queries, label)
}

// emitCapturingPackage emits a capturing batch and returns EVERY file,
// which since gqlc-dfcb is the capture sweep's parse set.
//
// The old parse set was the .cypher.go files alone — narrower than the
// scope a query parameter is bound in, because the generated package is
// one Go package and a method body also resolves what db.go and
// models.go declare.
//
// Nothing structural had closed that gap. It was closed only by how the
// cross-file set happens to be typed — ErrNoRows and ErrMultipleResults
// are error-typed, DBTX and Queries are types, New is a func,
// maxGraphNameBytes is an untyped int no .cypher.go body reads — so a
// STRING parameter shadowing any of them failed to compile rather than
// silently taking its place. A string-typed cross-file declaration
// referenced from a method body is the silent case, and the old parse
// set could not see it.
func (s *EmissionSuite) emitCapturingPackage(fx ageFixture, param string, shape captureArity) map[string]string {
	queries, label := s.capturingBatch(fx, param, shape)
	return s.emitAllFiles(fx, queries, label)
}

// emitQueryFiles generates a fixture's batch and returns the
// <name>.cypher.go files by path. The readers that ask for it —
// paramLocalsOf and the arity guard — read a query METHOD's shape, which
// db.go's New and WithTx do not have.
func (s *EmissionSuite) emitQueryFiles(fx ageFixture, queries []codegen.NamedQuery, label string) map[string]string {
	files := s.emitAllFiles(fx, queries, label)
	out := make(map[string]string)
	for path, body := range files {
		if strings.HasSuffix(path, ".cypher.go") {
			out[path] = body
		}
	}
	return out
}

// emitAllFiles generates a fixture's batch and returns every Go file it
// emits by path. Package scope is package-wide, so a question about what
// an emitted name can be — as opposed to what one method body does with
// it — has to be asked of the whole package: the edge-union sum types
// are declared in models.go and referenced from the .cypher.go bodies,
// and a parse set that stopped at .cypher.go would call those references
// undeclared.
func (s *EmissionSuite) emitAllFiles(fx ageFixture, queries []codegen.NamedQuery, label string) map[string]string {
	files, err := age.New(age.WithPackageName(fx.pkg)).Generate(codegen.Input{Schema: fx.schema, Queries: queries})
	s.Require().NoError(err, "fixture %s under %s", fx.name, label)
	out := make(map[string]string)
	for _, f := range files {
		if strings.HasSuffix(f.Path, ".go") {
			out[f.Path] = string(f.Contents)
		}
	}
	return out
}

// TestNoParameterArityPutsAQueryChosenNameInScope closes the arity axis
// the sweep above holds fixed. That sweep rebinds every query to one
// parameter, so it measures the one-parameter signature and nothing
// else; a capture that only the two-or-more form performs is invisible
// to it. This test varies arity instead of name, over the arities the
// corpus itself carries, and it does so without a name list.
//
// Held by perturbation: renaming every parameter a query text spells
// must move NOTHING in the scope any emitted method resolves in, at
// every arity. Under the one-parameter form the argument is a generated
// name and the author's name reaches the wire only as a map key; under
// the two-or-more form it reaches the wire only as a Params field, read
// through a generated receiver. Neither is an identifier the body
// resolves, so neither moves.
//
// A signature that named its argument after the query would fail this at
// whichever arity it did so, whatever the name happened to be — as would
// a binder or a body local derived from a parameter name. None of those
// has to be predicted here to be caught.
func (s *EmissionSuite) TestNoParameterArityPutsAQueryChosenNameInScope() {
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

			// The scope comparison below reads what a rename moved, so the
			// rename has to have moved something first. A fixture spelling
			// no parameter cannot move anything by construction, and it
			// contributes to neither arity census, so it is skipped rather
			// than asserted on.
			if renamed := renamedParameterNames(fx.queries); len(renamed) > 0 {
				s.Require().NotEqual(before, after,
					"renaming every parameter changed nothing in the emission, so the "+
						"perturbation never reached it and the scope comparison below holds vacuously")
				s.requireParametersReachTheWire(after, renamed)
			}

			for _, path := range sortedKeys(before) {
				scopesBefore, scopesAfter := s.methodScopes(before[path]), s.methodScopes(after[path])
				for _, method := range sortedKeys(scopesBefore) {
					want := scopesBefore[method]
					got, ok := scopesAfter[method]
					s.Require().True(ok, "%s: method %s disappeared under a parameter rename", path, method)

					_, known := params[method]
					s.Require().True(known, "%s: emitted method %s has no query in the batch", path, method)

					s.Require().Equal(want, got,
						"%s: method %s resolves a different set of names when the query's parameters are renamed, "+
							"so an author-chosen identifier is in the scope its body resolves in",
						path, method)
				}
			}
		})
	}

	// Both arities have to be present or the assertion above measured one
	// signature form and reported on two. A zero-parameter method is not
	// counted: renaming a batch that spells no parameter moves nothing by
	// construction, so it satisfies the invariant without measuring it.
	s.Require().Positive(singleParam,
		"no method in the corpus binds exactly one parameter, so the one-parameter form is unmeasured")
	s.Require().Positive(multiParam,
		"no method in the corpus binds two or more parameters, so the Params form is unmeasured")
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

// TestOnlyPackageLevelNamesFollowAColumnName closes the OTHER thing a
// query author names. Everything above varies the parameter, because the
// parameter is what the signature carries — but a return clause names
// its columns, and those names are just as much the author's.
//
// On this backend the axis is closed, and the census at the end is what
// says so. The one construct that ever derived a package-level name from
// a column was the edge-union sum type, built as
// `q.Name + rowFieldName(col.Name)` — `MATCH ... RETURN r` yielding the
// interface ListActionsR, half method name, half author-chosen column.
// This backend no longer emits one: an edge-union column is reachable
// only through a relationship-type alternation, AGE will not parse one,
// and so the column is refused at generation (errors.go, edgeUnionReason)
// rather than decoded. Every package-level name the emitter now declares
// is generator-owned, and the residue gqlc-vac9 holds is a neo4j-only
// residue — one that is itself collision-refused there by the shared
// identifier sweep (source 6), witnessed by the invalid fixture
// identifier_collision_edge_union_interface_pair. The bead is closed, not
// pending.
//
// The assertion is still written against a substitution rather than as a
// plain equality, and the census then requires the substitution to be
// EMPTY. Written that way, the day a package-level name starts following
// a column again — some emission mangling a column into a declaration —
// the census reddens and names it, instead of the equality failing
// somewhere further down with a diff that does not say why. A readmitted
// edge union is NOT among what this reaches: it would have to be enrolled
// on AGE to enter this corpus at all, and the refusal is what keeps it
// out. The census at the end states that boundary. The two halves below
// are what the substitution is applied to.
//
// The exact half: the names a method BINDS — its argument and its body
// locals, which are the positions a capture actually occupies — must be
// byte-identical before and after. No substitution is allowed there, so
// a decoder that started naming a local after the column it decodes
// fails immediately, which is the regression this test is here for.
//
// The substituted half: the names a method RESOLVES may move, but only
// as far as the emission's own package-level declarations moved. That
// substitution is read off the two emissions' declaration lists in
// emission order rather than listed here, so anything that followed a
// column is in neither list, does not substitute, and fails there as
// well as in the census.
func (s *EmissionSuite) TestOnlyPackageLevelNamesFollowAColumnName() {
	oneColumn, multiColumn, moved, reached := 0, 0, 0, 0
	for _, fx := range s.ageFixtures() {
		if len(fx.queries) == 0 {
			continue
		}
		// Counted outside the subtest so the census measures the corpus
		// rather than whatever `go test -run` selected from it.
		for _, q := range fx.queries {
			switch {
			case len(q.Validated.Columns) == 1:
				oneColumn++
			case len(q.Validated.Columns) >= 2:
				multiColumn++
			}
		}

		before := s.emitAllFiles(fx, fx.queries, "the corpus columns")
		after := s.emitAllFiles(fx, renameColumns(fx.queries), "renamed columns")
		sub := s.declRenaming(before, after)
		for name, renamed := range sub {
			if name != renamed {
				moved++
			}
		}
		for path, body := range before {
			if after[path] != body {
				reached++
			}
		}

		s.Run(fx.name, func() {
			s.Require().Equal(sortedKeys(before), sortedKeys(after),
				"renaming columns changed which files the batch emits")

			for _, path := range sortedKeys(before) {
				s.Require().Equal(s.signatureNamesOf(before[path]), s.signatureNamesOf(after[path]),
					"%s: a name bound by a method signature follows the query's column names", path)
				s.Require().Equal(s.bodyLocalsOf(before[path]), s.bodyLocalsOf(after[path]),
					"%s: a body local's name follows the query's column names", path)

				scopesBefore, scopesAfter := s.methodScopes(before[path]), s.methodScopes(after[path])
				s.Require().Equal(substituted(sortedKeys(scopesBefore), sub), sortedKeys(scopesAfter),
					"%s: renaming columns moved a declared method's name further than it moved the "+
						"package-level declarations", path)
				for _, method := range sortedKeys(scopesBefore) {
					want := substituted(scopesBefore[method], sub)
					got, ok := scopesAfter[substituteOne(method, sub)]
					s.Require().True(ok, "%s: method %s disappeared under a column rename", path, method)
					s.Require().Equal(want, got,
						"%s: method %s resolves a name that follows the query's column names but is not a "+
							"package-level declaration, so an author-chosen column name is in the scope "+
							"its body resolves in",
						path, method)
				}
			}
		})
	}

	// One column and two are different emission branches — a single
	// column may not build a Row struct at all — so both have to be
	// present or the assertion above reported on one of them.
	s.Require().Positive(oneColumn,
		"no query in the corpus projects exactly one column, so that branch is unmeasured")
	s.Require().Positive(multiColumn,
		"no query in the corpus projects two or more columns, so the Row shape is unmeasured")

	// The substitution above is an escape hatch, and on this backend it is
	// meant to stay shut: no package-level name the emitter declares is
	// built from a column, because the one that was — the edge-union sum
	// type — is a column this backend refuses at generation rather than
	// decodes. Measuring it here rather than asserting it in the comment
	// is what makes a later emission that mangles a column into a
	// declaration fail as itself instead of as a scope diff further up.
	//
	// It does NOT hold the edge-union refusal, and the wording here used to
	// claim it did. This census reads the AGE-enrolled corpus, and no
	// AGE-enrolled fixture carries an edge-union column — the enrolment is
	// what the refusal removes — so nulling edgeUnionReason leaves this
	// test green. The refusal is pinned by TestRejectsEdgeUnionColumns,
	// TestRejectsQueriesItCannotServe and TestRunApacheAgeRefusesEdgeUnions,
	// all three of which redden for it. A census over a corpus that cannot
	// contain the construct is not evidence about the construct.
	s.Require().Zero(moved,
		"a package-level name in the corpus follows a column name, so this backend has grown the "+
			"sum-type residue that refusing edge-union columns is what removes")

	// The census above is a claim about what a rename did NOT move, so it
	// is worth nothing unless the rename moved something. It is a rename
	// of the resolved model rather than of the query text, so an emitter
	// that stopped reading Column.Name — or a renameColumns that stopped
	// renaming — would leave the two emissions byte-identical and every
	// assertion in this test would hold vacuously. A column name does
	// reach the emission, as a Row field and as the key the decode arm
	// quotes, so requiring at least one emitted file to differ is what
	// keeps the whole sweep honest.
	s.Require().Positive(reached,
		"renaming every column in the corpus changed no emitted file, so nothing here is measuring "+
			"what a column name reaches")
}

// declRenaming pairs two emissions of the same batch by their
// package-level declarations, in emission order, and returns the
// substitution taking the first's names to the second's. The pairing is
// package-wide because Go's package scope is: an interface declared in
// models.go and a method that resolves it in queries.cypher.go share one
// scope.
//
// Order is the pairing key because the emitter walks its input in a
// fixed order and a rename does not reorder that walk. That is asserted
// rather than assumed — a reordering would pair unrelated declarations
// and blow the equality up in the caller, rather than quietly widening
// what the substitution admits.
func (s *EmissionSuite) declRenaming(before, after map[string]string) map[string]string {
	s.Require().Equal(sortedKeys(before), sortedKeys(after),
		"the two emissions do not declare the same files")

	var b, a []string
	for _, path := range sortedKeys(before) {
		b = append(b, topLevelDecls(s.parseEmission(before[path]))...)
		a = append(a, topLevelDecls(s.parseEmission(after[path]))...)
	}
	s.Require().Len(a, len(b), "the two emissions declare different numbers of package-level names")

	out := make(map[string]string, len(b))
	for i := range b {
		if prior, seen := out[b[i]]; seen {
			s.Require().Equal(prior, a[i], "package-level name %s pairs with two different names", b[i])
			continue
		}
		out[b[i]] = a[i]
	}
	return out
}

// topLevelDecls names everything an emitted file declares at package
// level: the consts, vars and types packageDecls reads, plus the funcs
// and methods it does not. Methods are in because the sealed-union
// marker is one — an edge-union interface named after a column carries a
// marker method named after the interface — so leaving them out would
// pair two declaration lists of different lengths and report a
// substitution failure as a missing declaration.
func topLevelDecls(file *ast.File) []string {
	out := packageDecls(file)
	for _, decl := range file.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok {
			out = append(out, fn.Name.Name)
		}
	}
	return out
}

// signatureNamesOf names every identifier an emitted file's method
// signatures BIND — receivers, arguments and named results alike —
// deduplicated and ordered. These are binding positions, so they are
// held to exact equality under a perturbation rather than to a
// substitution.
//
// Unlike paramLocalsOf this tolerates any arity, because it reads the
// corpus's own queries rather than a batch rebound to one parameter, and
// the corpus carries queries that spell no parameter at all.
func (s *EmissionSuite) signatureNamesOf(body string) []string {
	seen := make(map[string]bool)
	for _, decl := range s.parseEmission(body).Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		for _, fl := range []*ast.FieldList{fn.Recv, fn.Type.Params, fn.Type.Results} {
			if fl == nil {
				continue
			}
			for _, f := range fl.List {
				for _, n := range f.Names {
					if n.Name != "_" {
						seen[n.Name] = true
					}
				}
			}
		}
	}
	out := make([]string, 0, len(seen))
	for name := range seen {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// substituteOne applies a renaming to a single name, leaving a name the
// renaming does not mention where it is.
func substituteOne(name string, sub map[string]string) string {
	if renamed, ok := sub[name]; ok {
		return renamed
	}
	return name
}

// substituted applies a renaming to a scope, leaving names the renaming
// does not mention where they are, and returns it ordered and
// deduplicated so it compares against another scope directly.
func substituted(scope []string, sub map[string]string) []string {
	out := make([]string, 0, len(scope))
	for _, name := range scope {
		out = append(out, substituteOne(name, sub))
	}
	sort.Strings(out)
	return slices.Compact(out)
}

// renameColumns returns the batch with every column a query projects
// renamed, leaving every other axis — parameters, cardinality, source
// text — as the corpus wrote it. The §4.2 mangle cannot make two names
// that differed collide under a common prefix, so a rename never turns a
// served batch into a field-name collision.
func renameColumns(queries []codegen.NamedQuery) []codegen.NamedQuery {
	out := slices.Clone(queries)
	for i := range out {
		renamed := slices.Clone(out[i].Validated.Columns)
		for j := range renamed {
			renamed[j].Name = renamePrefix + renamed[j].Name
		}
		out[i].Validated.Columns = renamed
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

// renamedParameterNames is the set of names renameParameters puts on a
// batch, which is empty exactly when the batch spells no parameter.
func renamedParameterNames(queries []codegen.NamedQuery) []string {
	var out []string
	for _, q := range renameParameters(queries) {
		for _, p := range q.Validated.Parameters {
			out = append(out, p.Name)
		}
	}
	sort.Strings(out)
	return slices.Compact(out)
}

// requireParametersReachTheWire holds the half of a parameter name that
// must NOT become generator-owned. The Go argument is free to be renamed
// because it is positional, but the name the query text wrote is the key
// the driver substitutes on, and an emitter that stopped spelling it
// would satisfy every scope comparison in this file while silently
// unbinding the query.
func (s *EmissionSuite) requireParametersReachTheWire(files map[string]string, params []string) {
	s.T().Helper()
	for _, param := range params {
		quoted := strconv.Quote(param)
		found := false
		for _, path := range sortedKeys(files) {
			if strings.Contains(files[path], quoted) {
				found = true
				break
			}
		}
		s.Require().True(found,
			"parameter %s is spelled nowhere in the emitted query files, so the batch no longer "+
				"binds it and the scope comparison reads an emission the rename never reached",
			quoted)
	}
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
