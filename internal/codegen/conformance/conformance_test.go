// Package conformance holds the golden-corpus suite for code
// generation. It sits above the backend packages and resolves every
// fixture's declared emission targets through the composed registry, so
// it must import no single backend.
package conformance_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	"github.com/areqag/gqlc/internal/cli/backends"
	"github.com/areqag/gqlc/internal/codegen"
	"github.com/areqag/gqlc/internal/procsig"
	"github.com/areqag/gqlc/internal/query/cypher"
	"github.com/areqag/gqlc/internal/queryfile"
	"github.com/areqag/gqlc/internal/resolver"
	"github.com/areqag/gqlc/internal/schema"
	"github.com/areqag/gqlc/internal/schema/gql"
)

var update = flag.Bool("update", false, "regenerate codegen golden files")

const (
	// trackedFixtureDir is the committed golden corpus.
	trackedFixtureDir = "../../../test/data/codegen"
	// childRootEnv names the corpus this process reads, overriding the
	// committed one. Only updateCopy sets it, on the run it spawns, so
	// that run's writes land on a throwaway copy. Recursion is not a
	// concern: updateCopy narrows -run to a TestValid subtest path it
	// builds itself, which no top-level test can match.
	childRootEnv = "GQLC_CONFORMANCE_CHILD_ROOT"
)

// fixtureRoot is the corpus root this process reads.
func fixtureRoot() string {
	if root := os.Getenv(childRootEnv); root != "" {
		return root
	}
	return trackedFixtureDir
}

// manifest is the on-disk descriptor per fixture directory. Present in
// both valid and invalid fixtures; the invalid arm additionally carries
// ExpectedError (fully-qualified sentinel name) and, for the hand-
// constructed codegen.ErrInvalidCardinality case, SyntheticZeroCardinality —
// see loadInvalidInput.
//
// Targets names the registry wire keys the fixture is enrolled in, one
// golden subtree each. Required, with no default: a backend reaches a
// fixture only by being named here, so registering one does not enrol
// it across the corpus.
type manifest struct {
	Package                  string   `json:"package"`
	QueryFiles               []string `json:"queryFiles"`
	Targets                  []string `json:"targets"`
	ExpectedError            string   `json:"expectedError,omitempty"`
	SyntheticZeroCardinality bool     `json:"syntheticZeroCardinality,omitempty"`
}

// generate emits in through the backend registered under target. A key
// no backend is registered under fails the suite: a typo must not
// silently fall back to another backend and pass against the wrong
// golden tree.
func (s *ConformanceSuite) generate(target string, in codegen.Input) ([]codegen.File, error) {
	newGen, ok := s.backends.Lookup(target)
	s.Require().True(ok, "manifest enrols target %q, which no backend is registered under", target)
	return newGen("").Generate(in)
}

// codegenSentinels is the core package's canonical reachable set,
// resolved once so the name map, the sweep, and the identity test all
// read the same slice.
var codegenSentinels = codegen.AllSentinels()

// sentinelLanes is every pipeline package a fixture may name a sentinel
// from, paired with the prefix its manifest entries carry. The cypher
// lane covers fixtures the query parser refuses outright: those never
// reach a backend, so their refusal is the whole assertion.
var sentinelLanes = []struct {
	prefix string
	set    []error
}{
	{"codegen.", codegenSentinels},
	{"queryfile.", queryfile.AllSentinels()},
	{"cypher.", cypher.AllSentinels()},
}

// sentinelByName maps the manifest's fully-qualified sentinel string
// back to the actual error value at load time. A change to a lane's set
// without a fixture update fails the queryfile / codegen reachability
// sweeps, and a fixture that names a non-canonical sentinel fails
// invalidFixtures' map lookup.
var sentinelByName = func() map[string]error {
	m := make(map[string]error)
	for _, lane := range sentinelLanes {
		for _, s := range lane.set {
			m[lane.prefix+sentinelIdent(s)] = s
		}
	}
	return m
}()

// TestSentinelNameMapIsTotal holds sentinelIdent in step with the lanes
// it is asked about. A sentinel it does not know answers "unknown", so
// the lane's entry is keyed "<pkg>.unknown" — a name no fixture would
// write, which makes the sentinel unnameable rather than misnamed. Two
// such sentinels in one lane collide on that key and the map silently
// holds whichever landed last.
//
// Only the codegen lane is forced by anything else: its reachability
// sweep demands a fixture per sentinel, and a fixture resolves through
// this map. The queryfile and cypher lanes carry entries no fixture
// names, so nothing but this test keeps them honest.
func TestSentinelNameMapIsTotal(t *testing.T) {
	total := 0
	for _, lane := range sentinelLanes {
		require.NotEmpty(t, lane.set, "lane %q contributes no sentinel, so this sweep holds nothing for it", lane.prefix)
		total += len(lane.set)
		for _, s := range lane.set {
			require.NotEqual(t, "unknown", sentinelIdent(s),
				"sentinelIdent does not know %s sentinel %q, so no fixture can name it", lane.prefix, s)
		}
	}
	require.Len(t, sentinelByName, total,
		"two sentinels resolved to one name; the map holds whichever was built last")
}

// sentinelIdent recovers the exported symbol name of a sentinel. Kept
// internal to the test so the production types do not need to expose a
// reflection helper. Identity comparison is intentional: fixture-lookup
// callers register the raw package-level values, never wrapped errors.
//
//nolint:errorlint // identity match on package-level sentinels is intended
func sentinelIdent(err error) string {
	switch err {
	case codegen.ErrInvalidPackageName:
		return "ErrInvalidPackageName"
	case codegen.ErrDuplicateSourceFile:
		return "ErrDuplicateSourceFile"
	case codegen.ErrDuplicateQueryName:
		return "ErrDuplicateQueryName"
	case codegen.ErrInvalidCardinality:
		return "ErrInvalidCardinality"
	case codegen.ErrFormatFailure:
		return "ErrFormatFailure"
	case codegen.ErrOutOfC6Scope:
		return "ErrOutOfC6Scope"
	case codegen.ErrExecOnProjection:
		return "ErrExecOnProjection"
	case codegen.ErrCardinalityShapeMismatch:
		return "ErrCardinalityShapeMismatch"
	case codegen.ErrUnrepresentableWidth:
		return "ErrUnrepresentableWidth"
	case codegen.ErrUnrepresentableEdgeUnion:
		return "ErrUnrepresentableEdgeUnion"
	case codegen.ErrUnrepresentableTemporal:
		return "ErrUnrepresentableTemporal"
	case codegen.ErrParamNameCollision:
		return "ErrParamNameCollision"
	case codegen.ErrRowFieldCollision:
		return "ErrRowFieldCollision"
	case codegen.ErrAliasRequired:
		return "ErrAliasRequired"
	case codegen.ErrIdentifierCollision:
		return "ErrIdentifierCollision"
	case codegen.ErrInvalidEntityName:
		return "ErrInvalidEntityName"
	case codegen.ErrUnnamedMultiLabelType:
		return "ErrUnnamedMultiLabelType"
	case codegen.ErrPropertyFieldCollision:
		return "ErrPropertyFieldCollision"
	case queryfile.ErrMissingAnnotation:
		return "ErrMissingAnnotation"
	case queryfile.ErrUnknownCardinality:
		return "ErrUnknownCardinality"
	case queryfile.ErrInvalidQueryName:
		return "ErrInvalidQueryName"
	case queryfile.ErrDuplicateQueryName:
		return "ErrDuplicateQueryName"
	case queryfile.ErrMalformedAnnotation:
		return "ErrMalformedAnnotation"
	case queryfile.ErrTextBeforeAnnotation:
		return "ErrTextBeforeAnnotation"
	case queryfile.ErrNoQueries:
		return "ErrNoQueries"
	case cypher.ErrUnsupportedParameter:
		return "ErrUnsupportedParameter"
	case cypher.ErrUnboundVariable:
		return "ErrUnboundVariable"
	case cypher.ErrVariableKindConflict:
		return "ErrVariableKindConflict"
	case cypher.ErrPatternInProjection:
		return "ErrPatternInProjection"
	case cypher.ErrNestedPropertyTarget:
		return "ErrNestedPropertyTarget"
	case cypher.ErrUnknownProcedure:
		return "ErrUnknownProcedure"
	case cypher.ErrProcedureArity:
		return "ErrProcedureArity"
	case cypher.ErrUnsatisfiableRelationshipType:
		return "ErrUnsatisfiableRelationshipType"
	default:
		return "unknown"
	}
}

// ConformanceSuite is the testify suite for the golden-corpus tests.
type ConformanceSuite struct {
	suite.Suite

	backends codegen.Registry
}

func TestConformanceSuite(t *testing.T) {
	suite.Run(t, new(ConformanceSuite))
}

func (s *ConformanceSuite) SetupSuite() {
	reg, err := backends.Registry()
	s.Require().NoError(err)
	s.backends = reg
}

// loadManifest reads a manifest.json from the given fixture directory and
// holds its targets to the registry.
//
// The registration check lives here, not at generate(), because generate() is
// on the emitting path and not every fixture reaches it: a fixture the front
// end refuses returns from TestInvalid before any target is looked up, so a
// misspelled target on such a fixture would never be read by anything. That
// is a typo hole, not a missing refusal — the target is exercised the instant
// the front end stops refusing — and it is one every future lane that loads a
// manifest without emitting would re-open. Validating at load time closes it
// for every fixture in every lane at once.
func (s *ConformanceSuite) loadManifest(dir string) manifest {
	src, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	s.Require().NoError(err)
	var m manifest
	s.Require().NoError(json.Unmarshal(src, &m))
	s.Require().NotEmpty(m.Targets,
		"fixture %q declares no targets; every fixture must name the emission targets it is enrolled in "+
			"(one of %v), because there is no default enrolment", dir, s.backends.Keys())
	for _, target := range m.Targets {
		_, ok := s.backends.Lookup(target)
		s.Require().True(ok,
			"fixture %q enrols target %q, which no backend is registered under (registered: %v)",
			dir, target, s.backends.Keys())
	}
	return m
}

// loadSchema parses schema.gql in the given fixture directory.
func (s *ConformanceSuite) loadSchema(dir string) schema.Schema {
	src, err := os.ReadFile(filepath.Join(dir, "schema.gql"))
	s.Require().NoError(err)
	sch, err := gql.New().Parse(bytes.NewReader(src))
	s.Require().NoError(err)
	return sch
}

// buildNamedQueries walks the manifest's queryFiles through the front end
// — queryfile split, cypher parse, resolve — and turns each into a
// NamedQuery. C1 threads the cypher parser and the resolver into the
// pipeline so every read query carries a real Validated shape — Phase A
// and Phase B key on it (spec §2.1).
//
// A front-end refusal is returned, not asserted: an invalid fixture may
// name a front-end sentinel, in which case being refused here is the
// whole point of the fixture. Missing or unreadable files stay fatal —
// those are harness faults, not fixture verdicts.
func (s *ConformanceSuite) buildNamedQueries(dir string, m manifest, sch schema.Schema) ([]codegen.NamedQuery, error) {
	emptyReg, err := procsig.NewRegistry(nil)
	s.Require().NoError(err)
	res := resolver.New(sch, resolver.WithRegistry(emptyReg))
	var out []codegen.NamedQuery
	for _, qf := range m.QueryFiles {
		src, err := os.ReadFile(filepath.Join(dir, qf))
		s.Require().NoError(err)
		parsed, err := queryfile.New().Parse(bytes.NewReader(src))
		if err != nil {
			return nil, err
		}
		for _, aq := range parsed {
			q, err := cypher.New(cypher.WithRegistry(emptyReg)).Parse(bytes.NewReader([]byte(aq.Text)))
			if err != nil {
				return nil, err
			}
			vq, err := res.Resolve(q)
			if err != nil {
				return nil, err
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

// loadNamedQueries is the accept-path entry to the front end: a fixture
// in valid/ must reach codegen, so a refusal fails the suite here.
func (s *ConformanceSuite) loadNamedQueries(dir string, m manifest, sch schema.Schema) []codegen.NamedQuery {
	out, err := s.buildNamedQueries(dir, m, sch)
	s.Require().NoError(err, "fixture %s must reach codegen", dir)
	return out
}

// validFixtures walks valid/*/.
func (s *ConformanceSuite) validFixtures() []string {
	dirs, err := filepath.Glob(filepath.Join(fixtureRoot(), "valid", "*"))
	s.Require().NoError(err)
	s.Require().NotEmpty(dirs)
	return dirs
}

// invalidFixtures walks invalid/*/.
func (s *ConformanceSuite) invalidFixtures() []string {
	dirs, err := filepath.Glob(filepath.Join(fixtureRoot(), "invalid", "*"))
	s.Require().NoError(err)
	s.Require().NotEmpty(dirs)
	return dirs
}

// TestValid walks valid/*/ x its enrolled targets and either writes the
// target's golden directory (-update) or asserts byte-equality against
// every file it contains. The comparison is bytes.Equal, not JSONEq: the
// output is Go source, and every whitespace character matters (gofmt
// normalises, but the tree it produces is stable).
func (s *ConformanceSuite) TestValid() {
	for _, dir := range s.validFixtures() {
		name := filepath.Base(dir)
		s.Run(name, func() {
			m := s.loadManifest(dir)
			sch := s.loadSchema(dir)
			queries := s.loadNamedQueries(dir, m, sch)

			goldenRoot := filepath.Join(dir, "golden")
			in := codegen.Input{Schema: sch, Queries: queries}

			// -update rewrites the fixture's whole golden root from one
			// emission per declared target, and does so here rather than in
			// the per-target subtests below: those are what `go test -run`
			// filters, and a rewrite that narrowed with the filter while the
			// wipe did not would delete an unselected target's goldens.
			//
			// The wipe precedes the generation it feeds, so a target that
			// fails to generate leaves a hole rather than its previous
			// emission (TestUpdateCannotPreserveGoldensOnFailure).
			if *update {
				s.Require().NoError(os.RemoveAll(goldenRoot))
				for _, target := range m.Targets {
					got, err := s.generate(target, in)
					s.Require().NoError(err)
					s.assertPackage(got, m.Package)
					s.Require().NoError(writeGoldenTarget(goldenRoot, target, got))
				}
				return
			}

			s.assertGoldenTargets(goldenRoot, m.Targets)
			for _, target := range m.Targets {
				s.Run(target, func() {
					got, err := s.generate(target, in)
					s.Require().NoError(err)
					s.assertPackage(got, m.Package)
					s.assertGoldenTree(filepath.Join(goldenRoot, target), got)
				})
			}
		})
	}
}

// TestInvalid walks invalid/*/, resolves the manifest's ExpectedError to
// a sentinel, and asserts the pipeline refuses the fixture with it.
//
// A fixture is refused at one of two stages. The front end (queryfile,
// cypher, resolver) rejects before any backend runs, and that verdict is
// one per fixture — the query never reaches a target, so there is nothing
// per-target to assert. Otherwise the input reaches codegen and each
// enrolled target must refuse it, returning nil files.
//
// The stage is not declared in the manifest; it follows from where the
// refusal actually happens, so a fixture that starts being refused
// earlier (or later) than its sentinel expects fails on the sentinel
// match rather than silently changing which assertion ran.
func (s *ConformanceSuite) TestInvalid() {
	for _, dir := range s.invalidFixtures() {
		name := filepath.Base(dir)
		s.Run(name, func() {
			m := s.loadManifest(dir)
			s.Require().NotEmpty(m.ExpectedError, "invalid fixture %q must declare expectedError", name)

			wantErr, ok := sentinelByName[m.ExpectedError]
			s.Require().True(ok, "unknown sentinel name %q in fixture %q", m.ExpectedError, name)

			in, frontEndErr := s.loadInvalidInput(dir, m)
			if frontEndErr != nil {
				s.Require().ErrorIs(frontEndErr, wantErr)
				return
			}
			for _, target := range m.Targets {
				s.Run(target, func() {
					got, err := s.generate(target, in)
					s.Require().Error(err)
					s.Require().Nil(got, "files must be nil on error")
					s.Require().ErrorIs(err, wantErr)
				})
			}
		})
	}
}

// loadInvalidInput assembles the codegen.Input for an invalid fixture, or
// returns the front end's refusal. Two paths: normal (schema + queryFiles
// pipeline) and synthetic (a hand-constructed codegen.NamedQuery with a
// zero-valued Cardinality, the only way to reach
// codegen.ErrInvalidCardinality — the queryfile front end never emits one).
func (s *ConformanceSuite) loadInvalidInput(dir string, m manifest) (codegen.Input, error) {
	sch := s.loadSchema(dir)
	if m.SyntheticZeroCardinality {
		return codegen.Input{
			Schema: sch,
			Queries: []codegen.NamedQuery{{
				Name:       "ZeroCardinality",
				SourceFile: "synthetic.cypher",
				SourceText: "MATCH (n) RETURN n",
			}},
		}, nil
	}
	queries, err := s.buildNamedQueries(dir, m, sch)
	if err != nil {
		return codegen.Input{}, err
	}
	return codegen.Input{Schema: sch, Queries: queries}, nil
}

// TestDoubleRun asserts Generate is byte-deterministic: same codegen.Input in,
// byte-identical []codegen.File out, twice. Independent of the golden
// comparison — a golden diff catches within-run nondeterminism (map
// iteration) only flakily; this test catches it in a single run.
func (s *ConformanceSuite) TestDoubleRun() {
	for _, dir := range s.validFixtures() {
		name := filepath.Base(dir)
		s.Run(name, func() {
			m := s.loadManifest(dir)
			sch := s.loadSchema(dir)
			in := codegen.Input{Schema: sch, Queries: s.loadNamedQueries(dir, m, sch)}
			for _, target := range m.Targets {
				s.Run(target, func() {
					first, err := s.generate(target, in)
					s.Require().NoError(err)
					second, err := s.generate(target, in)
					s.Require().NoError(err)
					s.Require().Len(second, len(first))
					for i := range first {
						s.Require().Equal(first[i].Path, second[i].Path, "file %d path drift", i)
						s.Require().True(bytes.Equal(first[i].Contents, second[i].Contents),
							"file %s contents drift: %d vs %d bytes",
							first[i].Path, len(first[i].Contents), len(second[i].Contents))
					}
				})
			}
		})
	}
}

// TestSentinelReachability is the bidirectional sweep: every
// codegenSentinels member has at least one invalid fixture; every
// mapped codegen sentinel is in codegenSentinels. Queryfile sentinels
// have their own sweep in internal/queryfile — this one is codegen-
// only, matching the two-disjoint-sets discipline (spec §9.3).
func TestSentinelReachability(t *testing.T) {
	dirs, err := filepath.Glob(filepath.Join(fixtureRoot(), "invalid", "*"))
	require.NoError(t, err)

	covered := make(map[error]bool)
	for _, dir := range dirs {
		src, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
		require.NoError(t, err)
		var m manifest
		require.NoError(t, json.Unmarshal(src, &m))
		if m.ExpectedError == "" {
			continue
		}
		sentinel, ok := sentinelByName[m.ExpectedError]
		require.True(t, ok, "unknown sentinel name %q in fixture %q", m.ExpectedError, dir)
		// Sweep only codegen-side sentinels here.
		if !isCodegenSentinel(sentinel) {
			continue
		}
		covered[sentinel] = true
	}
	canonical := make(map[error]bool, len(codegenSentinels))
	for _, sentinel := range codegenSentinels {
		canonical[sentinel] = true
	}
	for _, sentinel := range codegenSentinels {
		require.True(t, covered[sentinel], "sentinel %q has no negative fixture", sentinel)
	}
	for sentinel := range covered {
		require.True(t, canonical[sentinel], "fixture maps to non-canonical sentinel %q", sentinel)
	}
}

// isCodegenSentinel reports whether err is one of the codegen package's
// user-input-reachable sentinels (i.e. in codegenSentinels). Identity
// match is intentional — see sentinelIdent.
//
//nolint:errorlint // identity match on package-level sentinels is intended
func isCodegenSentinel(err error) bool {
	for _, s := range codegenSentinels {
		if s == err {
			return true
		}
	}
	return false
}

// assertPackage checks every emitted file's package clause matches the
// manifest's declared package. Cheap; catches a template regression
// that swaps package names between files.
func (s *ConformanceSuite) assertPackage(files []codegen.File, want string) {
	for _, f := range files {
		lines := bytes.SplitN(f.Contents, []byte{'\n'}, 4)
		s.Require().GreaterOrEqual(len(lines), 3, "file %s too short for header + package", f.Path)
		// Line 2 is the mandatory blank; line 3 is the package clause.
		s.Require().Equal([]byte("package "+want), lines[2],
			"file %s has wrong package clause: %q", f.Path, lines[2])
	}
}

// assertGoldenTargets holds the fixture's golden subtrees and its
// enrolled targets in exact correspondence. A target with no subtree is
// the forcing function that keeps a newly enrolled backend inside the
// golden gate; a subtree with no target is a stale tree that nothing
// would otherwise compare.
func (s *ConformanceSuite) assertGoldenTargets(root string, targets []string) {
	entries, err := os.ReadDir(root)
	s.Require().NoError(err, "fixture has no golden directory at %s", root)
	onDisk := make([]string, 0, len(entries))
	for _, e := range entries {
		s.Require().True(e.IsDir(),
			"golden root %s holds file %q; goldens live one directory per target", root, e.Name())
		onDisk = append(onDisk, e.Name())
	}
	want := slices.Sorted(slices.Values(targets))
	slices.Sort(onDisk)
	s.Require().Equal(want, onDisk,
		"golden subtrees under %s do not match the manifest's targets: "+
			"enrol a target only alongside its goldens (go test -update), and delete the subtree of a target you drop",
		root)
}

// assertGoldenTree walks one target's golden directory and asserts every
// file there is present in got with byte-identical contents, and every
// file in got is present on disk. On mismatch, the assertion reports the
// file path and a diff-shaped message.
func (s *ConformanceSuite) assertGoldenTree(dir string, got []codegen.File) {
	gotByPath := make(map[string][]byte, len(got))
	for _, f := range got {
		gotByPath[f.Path] = f.Contents
	}

	entries, err := os.ReadDir(dir)
	s.Require().NoError(err, "missing golden dir %s; run go test -update", dir)

	diskByPath := make(map[string][]byte, len(entries))
	for _, e := range entries {
		s.Require().False(e.IsDir(),
			"golden dir %s holds subdirectory %q; a target's goldens are a flat set of emitted files",
			dir, e.Name())
		contents, err := os.ReadFile(filepath.Join(dir, e.Name()))
		s.Require().NoError(err)
		diskByPath[e.Name()] = contents
	}

	for path, want := range diskByPath {
		gotBytes, ok := gotByPath[path]
		s.Require().True(ok, "golden %q has no emitted counterpart", path)
		s.Require().True(bytes.Equal(want, gotBytes),
			"golden %q mismatch\n--- want (%d bytes) ---\n%s\n--- got (%d bytes) ---\n%s",
			path, len(want), want, len(gotBytes), gotBytes)
	}
	for path := range gotByPath {
		_, ok := diskByPath[path]
		s.Require().True(ok, "emitted file %q missing from golden dir; run go test -update", path)
	}
}

// writeGoldenTarget writes one target's emitted files under root.
//
// It adds to root rather than replacing it, because the caller wipes root
// once before generating any target: that single wipe is what makes a
// dropped target's subtree, and a deleted query's stale .cypher.go,
// disappear, and doing it up front is what stops a failed regeneration
// from leaving the previous emission behind.
func writeGoldenTarget(root, target string, files []codegen.File) error {
	dir := filepath.Join(root, target)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for _, f := range files {
		if err := os.WriteFile(filepath.Join(dir, f.Path), f.Contents, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// connectionSurface names the emitted files that hold a handle and what
// it talks to: a pgx pool and a bound graph on one backend, a driver and
// a managed transaction on the other. Those differ by construction.
// Every other emitted file is compared across targets, so a file some
// future backend adds is held invariant until it is named here on
// purpose.
var connectionSurface = map[string]bool{"db.go": true, "graph.go": true}

// TestBackendInvariantSurface pins the property the multi-backend design
// rests on: the Go a caller writes against does not vary by backend. A
// fixture enrolled in more than one target must declare the same types
// under each — fields with their names, types and tags in declaration
// order, interfaces with their method sets, and the marker methods that
// make an edge-union sum type closed — so that porting a program from
// one backend to another is a change of import and constructor and
// nothing else.
//
// Declarations only, never bodies: decoding agtype and reading a
// dbtype.Node is the one thing that must differ, and comparing whole
// files would only assert they are the same backend.
//
// This is the invariant the corpus exists to protect and the one nothing
// else checks. TestValid compares each target against its own golden, so
// a divergence introduced on one backend is regenerated into its golden
// and passes; only a comparison across targets sees it.
//
// It holds nothing about a multi-candidate edge column, and cannot. The
// edge_union_* fixtures enrolled in two targets are enrolled in
// neo4j-go-v5 and neo4j-go-v6, which are one emitter under two version
// options (internal/cli/backends), so the comparison is a golden against
// itself; Apache AGE refuses the column ahead of emission and has no
// golden to enrol. Even enrolled it would add nothing here: the sealed
// interface and its marker methods are declarations and would match,
// while the label dispatch that decides which candidate fills the column
// is a body, which this test does not read by design. A dispatch that
// picked the wrong candidate is a live-run question, and that is where it
// is asked (test/data/codegen, edgeUnionDispatch).
//
// # Scope, and what covers the residual
//
// The subject is what a caller writes against, so the comparison is over
// caller-visible declarations and deliberately no wider: type declarations
// and methods, which a caller can name, and not the unexported
// receiver-less functions the emitted decoders are. That exclusion is a
// decision and not an oversight. Widening it would point the gate at the
// encode/decode path, which is the one part of the emission that is
// supposed to differ per backend, so it would redden on legitimate
// divergence — and a gate that cries wolf gets weakened until it says
// nothing.
//
// The cost is real and is covered elsewhere. A backend can declare a
// struct whose surface matches under every target while the decoder that
// fills it can never fire — an emitted decodeFoo guarding on a label no
// value that backend can stamp leaves this comparison green, because
// decodeFoo is not a declaration it reads. That residual is
// TestEmittedDecodersGuardOnlyOnStampableLabels' (decoder_reachability_test.go),
// which sweeps the emitted decoder bodies for label guards the schema's
// own label alphabet cannot satisfy. The two gates partition the emission:
// this one owns the surface, that one owns the decoders underneath it.
func TestBackendInvariantSurface(t *testing.T) {
	goldens, err := filepath.Glob(filepath.Join(fixtureRoot(), "valid", "*", "golden"))
	require.NoError(t, err)
	require.NotEmpty(t, goldens, "valid goldens must exist")

	compared := 0
	for _, golden := range goldens {
		targets, err := os.ReadDir(golden)
		require.NoError(t, err)
		if len(targets) < 2 {
			continue
		}
		compared++
		fixture := filepath.Base(filepath.Dir(golden))
		t.Run(fixture, func(t *testing.T) {
			ref := targets[0].Name()
			// A surface read as empty would agree with every other
			// empty one, so an extractor that stopped seeing
			// declarations would pass the corpus silently.
			want := declaredSurface(t, filepath.Join(golden, ref))
			require.NotEmpty(t, want, "%s/%s declares nothing outside the connection surface", fixture, ref)
			for _, target := range targets[1:] {
				got := declaredSurface(t, filepath.Join(golden, target.Name()))
				require.Equal(t, want, got,
					"%s declares a different Go surface under %s than under %s",
					fixture, target.Name(), ref)
			}
		})
	}
	require.NotZero(t, compared, "no fixture is enrolled in two targets, so this test holds nothing")
}

// declaredSurface is one golden target's declared Go surface: every type
// declaration and every method signature outside the connection surface,
// keyed by what declares it and valued as rendered source. The doc
// comment is part of the value so that a directive such as
// //sumtype:decl, which is what makes an exhaustiveness check bind,
// counts as surface rather than as prose.
func declaredSurface(t *testing.T, dir string) map[string]string {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(dir, "*.go"))
	require.NoError(t, err)
	require.NotEmpty(t, paths, "%s holds no Go", dir)

	out := make(map[string]string)
	fset := token.NewFileSet()
	for _, path := range paths {
		if connectionSurface[filepath.Base(path)] {
			continue
		}
		file, err := parser.ParseFile(fset, path, nil, parser.ParseComments|parser.SkipObjectResolution)
		require.NoError(t, err, "parsing %s", path)
		for _, decl := range file.Decls {
			switch d := decl.(type) {
			case *ast.GenDecl:
				// A const or a var declares no surface a caller
				// writes against: the query texts are unexported
				// and the interface assertions are compile-time.
				if d.Tok != token.TYPE {
					continue
				}
				for _, spec := range d.Specs {
					ts, ok := spec.(*ast.TypeSpec)
					require.True(t, ok, "%s: type declaration holds a %T", path, spec)
					doc := ts.Doc
					if doc == nil && len(d.Specs) == 1 {
						doc = d.Doc
					}
					addDeclaration(t, out, path, "type "+ts.Name.Name, docLines(doc)+render(t, fset, ts))
				}
			case *ast.FuncDecl:
				// A receiver-less function is a decode helper. Passing
				// over it is sound only while it stays unexported: an
				// exported one is surface a caller writes against, and
				// one skipped here could diverge between backends with
				// nothing to see it.
				//
				// What is skipped here is not unwatched. The decoders
				// this passes over are swept for unsatisfiable label
				// guards by TestEmittedDecodersGuardOnlyOnStampableLabels;
				// their per-backend divergence is what that gate assumes
				// and this one must not assert.
				if d.Recv == nil {
					require.False(t, ast.IsExported(d.Name.Name),
						"%s: package-level func %s is exported, so it is caller-facing surface this comparison skips",
						path, d.Name.Name)
					continue
				}
				doc := d.Doc
				d.Doc, d.Body = nil, nil
				addDeclaration(t, out, path, "method "+receiverName(t, path, d)+"."+d.Name.Name, docLines(doc)+render(t, fset, d))
			}
		}
	}
	return out
}

// addDeclaration records one declaration, failing a name declared twice:
// two entries under one key would compare as whichever landed last.
func addDeclaration(t *testing.T, out map[string]string, path, key, body string) {
	t.Helper()
	_, dup := out[key]
	require.False(t, dup, "%s: %s is declared twice in one target", path, key)
	out[key] = body
}

// render prints a node as source, so that comparison is over what the
// declaration says and not over the byte offsets it says it at.
func render(t *testing.T, fset *token.FileSet, node ast.Node) string {
	t.Helper()
	var b strings.Builder
	require.NoError(t, printer.Fprint(&b, fset, node))
	return b.String()
}

// docLines is a doc comment as written. Built by hand rather than with
// CommentGroup.Text, which drops directive lines — exactly the lines
// that carry meaning to a linter.
func docLines(c *ast.CommentGroup) string {
	if c == nil {
		return ""
	}
	var b strings.Builder
	for _, line := range c.List {
		b.WriteString(line.Text)
		b.WriteByte('\n')
	}
	return b.String()
}

// receiverName is the base type a method hangs off, pointer receiver or
// not: the two forms name the same method set to a caller.
func receiverName(t *testing.T, path string, d *ast.FuncDecl) string {
	t.Helper()
	expr := d.Recv.List[0].Type
	if star, ok := expr.(*ast.StarExpr); ok {
		expr = star.X
	}
	ident, ok := expr.(*ast.Ident)
	require.True(t, ok, "%s: %s hangs off a %T", path, d.Name.Name, expr)
	return ident.Name
}

// The Bolt driver's own carrier vocabulary, split by the shape the
// emitted assertion takes. neo4j.PropertyValue is
//
//	bool | int64 | float64 | string | Point2D | Point3D |
//	Date | LocalTime | LocalDateTime | Time | Duration | time.Time |
//	[]byte | []any
//
// (neo4j/graph.go:25 under v5, the same plus the Vector and UUID named
// types under v6) and neo4j.RecordValue (neo4j/record.go:25) is that
// set plus map[string]any, Node, Relationship and Path. A value the
// driver hands to generated code came off one of those two constraints,
// so a type assertion on it can only be true for a member of them —
// every other name is an assertion that is false for every value the
// driver can produce, which is not a decode but a decode that always
// fails.
//
// Two sets rather than one because the two axes fail differently: a
// wrong slice carrier is caught by the compiler wherever it reaches
// neo4j.GetProperty[T], and a wrong element carrier is caught by nothing
// at all (see TestNeo4jGoldensAssertOnlyDriverCarriers).
//
// The value is a witness flag: true says a golden in this corpus asserts
// this carrier, false says the driver allows it and no golden reaches
// it. Both halves are checked, so each entry is a claim about the corpus
// that a corpus edit can falsify — which is what a bare allow-list could
// not be. An allow-list only ever says "not this", and a corpus that
// asserted nothing at all would satisfy it; it is also how the []byte
// entry sat here through a fixture's worth of BYTES properties without
// one golden ever reaching it, because a non-nullable BYTES decodes
// through GetProperty[[]byte] and never emits an assertion.
//
// The ledger lives here rather than beside the goldens on purpose:
// -update rewrites goldens and cannot touch this file, so deleting the
// one fixture that witnesses a carrier still reddens after a
// regeneration.
//
// Slice axis: GetProperty's own doc says "any property array value other
// than byte array is typed as []any", and the hydrator builds exactly
// that — `func (h *hydrator) array() []any` (internal/bolt/hydrator.go).
// So these two are the whole set, whatever the schema declared the
// element width to be.
var driverSliceCarriers = map[string]bool{
	"[]any":  true,
	"[]byte": true,
}

// Scalar axis: the named types of the two constraints, spelled as the
// emission spells them. `any` is deliberately absent. It is a member of
// neither constraint, and `x.(any)` is not a widening no-op: a type
// assertion on a nil interface value is false whatever type it names, so
// asserting to `any` fails on exactly the null the property or element
// was declared able to hold. The emission has to omit the assertion
// rather than name `any` in one.
//
// The unwitnessed half below is not a backlog. Most of those carriers
// are only ever named by an assertion inside a list walk — a scalar
// column of the same width decodes through neo4j.GetRecordValue[T],
// whose constraint the compiler checks — so their flags flip the day a
// fixture declares LIST<POINT>, LIST<DURATION> or a projected list of
// nodes, and not before.
var driverScalarCarriers = map[string]bool{
	"bool":                true,
	"int64":               true,
	"float64":             true,
	"string":              true,
	"time.Time":           true,
	"dbtype.Date":         true,
	"dbtype.Relationship": true,

	"map[string]any":       false,
	"dbtype.Point2D":       false,
	"dbtype.Point3D":       false,
	"dbtype.LocalTime":     false,
	"dbtype.LocalDateTime": false,
	"dbtype.Time":          false,
	"dbtype.Duration":      false,
	"dbtype.Node":          false,
	"dbtype.Path":          false,
}

// witnessedCarriers names the carriers a ledger claims some golden in
// the corpus asserts.
func witnessedCarriers(ledger map[string]bool) []string {
	var names []string
	for name, witnessed := range ledger {
		if witnessed {
			names = append(names, name)
		}
	}
	return names
}

// TestNeo4jGoldensAssertOnlyDriverCarriers pins the emitted decode
// against the driver's type set rather than against the schema's. A
// property declared LIST<STRING> is a []string in the struct the caller
// writes against, but it never arrives as one: Bolt hydrates every
// array but a byte array into []any and narrows nothing, so `v.([]string)`
// is an assertion that is false for every value the driver can produce.
// Such an assertion is not a decode, it is a decode that always fails,
// and the emission has to walk the []any and convert element by element
// instead.
//
// The same defect lands one axis over, on the element the walk reaches:
// `elem.(int32)` for a LIST<INT32> is as false as `v.([]int32)` was, and
// nothing but this sweep can see it. The slice arm is held up by the
// compiler wherever a wrong carrier reaches neo4j.GetProperty[T], whose
// PropertyValue constraint refuses it; inside the walk there is no such
// constraint, because a []any element is an `any` and asserting an `any`
// to anything at all compiles.
//
// What the sweep finds is then compared against the ledger's witness
// flags rather than only counted. A count says an assertion was
// examined; it does not say which, and there is no corpus this repo
// would accept in which the scalar count reaches zero, so counting it
// guarded nothing. Set equality names each carrier separately, so
// deleting the one fixture that reaches a carrier reddens naming that
// carrier — and a fixture that reaches a new one reddens too, which is
// what keeps the ledger's "no golden asserts this" half from quietly
// becoming false.
//
// Swept syntactically over every neo4j golden rather than checked on one
// fixture, because the defect is a missing arm and a missing arm is
// invisible wherever no fixture reaches it. The sweep asserts a property
// of the text and not equality with a stored blob, so -update cannot
// bless a violation: regeneration rewrites the goldens from the same
// emission, and an emission that still names []string still fails here.
func TestNeo4jGoldensAssertOnlyDriverCarriers(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join(fixtureRoot(), "valid", "*", "golden", "neo4j-go-v*", "*.go"))
	require.NoError(t, err)
	require.NotEmpty(t, paths, "no neo4j golden was swept, so this test holds nothing")

	var offenders []string
	sweptSlice, sweptScalar := map[string]bool{}, map[string]bool{}
	fset := token.NewFileSet()
	for _, path := range paths {
		file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		require.NoError(t, err, "parsing %s", path)
		ast.Inspect(file, func(n ast.Node) bool {
			assertion, ok := n.(*ast.TypeAssertExpr)
			if !ok || assertion.Type == nil {
				return true
			}
			named := render(t, fset, assertion.Type)
			// A carrier counts as swept only once the assertion is known
			// to be on its axis and known to be one the ledger admits, so
			// an offender is never recorded as its own witness.
			ledger, swept := driverScalarCarriers, sweptScalar
			if _, isSlice := assertion.Type.(*ast.ArrayType); isSlice {
				ledger, swept = driverSliceCarriers, sweptSlice
			}
			if _, admitted := ledger[named]; admitted {
				swept[named] = true
				return true
			}
			offenders = append(offenders, fmt.Sprintf("%s:%d: %s",
				path, fset.Position(assertion.Pos()).Line, render(t, fset, assertion)))
			return true
		})
	}
	require.Empty(t, offenders,
		"a Bolt driver hands back only what neo4j.PropertyValue and neo4j.RecordValue name, so these assertions are false for every value it can produce")

	for _, axis := range []struct {
		name   string
		ledger map[string]bool
		swept  map[string]bool
	}{
		{"slice", driverSliceCarriers, sweptSlice},
		{"scalar", driverScalarCarriers, sweptScalar},
	} {
		want := witnessedCarriers(axis.ledger)
		require.NotEmpty(t, want,
			"the %s ledger claims no witness at all, so its half of this sweep would hold on a corpus that asserts nothing", axis.name)
		require.ElementsMatch(t, want, slices.Collect(maps.Keys(axis.swept)),
			"the %s carriers the goldens assert are no longer the ones this ledger claims: a name only the first list holds "+
				"has lost the fixture that reached it, and a name only the second holds is one a golden now reaches and the "+
				"ledger still calls unwitnessed", axis.name)
	}
}

// TestGoldenBuild compiles the nested test/data/codegen module so that
// spurious or missing imports in generated golden packages are caught by
// go test ./internal/codegen/... rather than only by just test-codegen-fence.
func TestGoldenBuild(t *testing.T) {
	abs, err := filepath.Abs(fixtureRoot())
	require.NoError(t, err)
	cmd := exec.CommandContext(t.Context(), "go", "build", "./...")
	cmd.Dir = abs
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "generated golden packages do not compile:\n%s", out)
}

// TestUpdateIgnoresTheTargetFilter pins the blast radius of -update: a
// run narrowed to one target must leave the fixture's other targets
// byte-for-byte alone. A rewrite driven by the per-target subtests
// narrows with `go test -run` while a wipe in the fixture body does
// not, so such a run exits 0 having deleted the unselected target's
// goldens.
//
// Runs the suite as a subprocess because nothing else exercises the
// interaction between -run's filtering and the update path, and points
// that subprocess at a throwaway copy: -update writes, and a test run
// that did not ask for -update must leave the committed corpus alone.
// The copy is brought into sync before the measurement so that corpus
// drift, whose regeneration is legitimate, cannot present as a target-
// filter failure. Whether -update is a no-op on the committed corpus is
// TestValid's question, not this one's.
func TestUpdateIgnoresTheTargetFilter(t *testing.T) {
	const (
		fixture  = "many_col_many"
		selected = "neo4j-go-v5"
	)
	root := t.TempDir()
	require.NoError(t, os.CopyFS(root, os.DirFS(trackedFixtureDir)))
	golden := filepath.Join(root, "valid", fixture, "golden")

	updateCopy(t, root, fixture)
	before := snapshotTree(t, golden)
	require.Contains(t, before, filepath.Join("neo4j-go-v6", "db.go"),
		"fixture %s must be enrolled in a second target for this test to mean anything", fixture)

	updateCopy(t, root, fixture+"/"+selected)

	require.Equal(t, before, snapshotTree(t, golden),
		"-update narrowed to %q changed %s's golden tree", selected, fixture)
}

// TestUpdateCannotPreserveGoldensOnFailure pins the other half of
// -update's blast radius: a run that fails to generate must not leave a
// golden tree behind that a later run will accept.
//
// Regenerating into memory and writing only once every target succeeded
// looks like the safe ordering and is the dangerous one. Generation
// aborts the fixture before anything is written, so the previous
// emission survives intact — and the corpus's compile gate then builds
// those stale goldens and passes. The evidence for a change lives in the
// golden diff, so a change whose regeneration failed presents as a change
// with no diff: indistinguishable from one that legitimately emits the
// same bytes.
//
// Wiping first makes the failure visible in the corpus instead. A fixture
// whose regeneration did not finish has no goldens, so the next ordinary
// run fails on the missing tree rather than passing on the old one.
//
// Runs as a subprocess for the same reason the target-filter test does:
// -update writes, and it must write to a throwaway copy.
func TestUpdateCannotPreserveGoldensOnFailure(t *testing.T) {
	const fixture = "many_col_many"
	root := t.TempDir()
	require.NoError(t, os.CopyFS(root, os.DirFS(trackedFixtureDir)))
	golden := filepath.Join(root, "valid", fixture, "golden")

	updateCopy(t, root, fixture)
	require.NotEmpty(t, snapshotTree(t, golden),
		"fixture %s must have goldens for this test to mean anything", fixture)

	// An unaliased expression column: it parses and resolves, so the
	// failure lands in generation rather than in the fixture loader, which
	// is the case the ordering has to survive.
	queries := filepath.Join(root, "valid", fixture, "queries.cypher")
	text, err := os.ReadFile(queries)
	require.NoError(t, err)
	broken := string(text) + "\n// name: ProbeUngeneratable :one\nMATCH (p:Person) RETURN p.age + 1\n"
	require.NoError(t, os.WriteFile(queries, []byte(broken), 0o644))

	out, err := runUpdate(t, root, fixture)
	require.Error(t, err, "-update accepted a fixture it cannot generate:\n%s", out)

	require.Empty(t, snapshotTree(t, golden),
		"-update failed and left %s's previous goldens in place, so the corpus still compiles and diffs clean", fixture)
}

// updateCopy runs the suite under -update against the corpus at root,
// narrowed to the named TestValid subtest path, and requires it to
// succeed.
func updateCopy(t *testing.T, root, subtest string) {
	t.Helper()
	out, err := runUpdate(t, root, subtest)
	require.NoError(t, err, "-update run over %q failed:\n%s", subtest, out)
}

// runUpdate is updateCopy without the verdict, for the caller that wants
// the failure.
func runUpdate(t *testing.T, root, subtest string) ([]byte, error) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "go", "test", ".", "-count=1", "-update",
		"-run", "TestConformanceSuite/TestValid/"+subtest)
	cmd.Env = append(os.Environ(), childRootEnv+"="+root)
	return cmd.CombinedOutput()
}

// snapshotTree reads every file under root into a map keyed by path
// relative to root. A root that does not exist is the empty tree, so that
// "these goldens are gone" and "these goldens are empty" are the same
// answer to a caller comparing trees.
func snapshotTree(t *testing.T, root string) map[string]string {
	t.Helper()
	out := make(map[string]string)
	if _, err := os.Stat(root); errors.Is(err, os.ErrNotExist) {
		return out
	}
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		contents, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		out[rel] = string(contents)
		return nil
	})
	require.NoError(t, err)
	return out
}

// TestGeneratedHeaderFormat fences the emitted-file header format (spec
// §5.2, §6.1) byte-level: fixed prefix, one non-whitespace version token,
// fixed suffix, LF, blank line. Walks every valid fixture's golden .go
// files so a future template regression that smuggled a build timestamp,
// UUID, hostname, doc comment, or //go:build line between the header and
// the package clause fails at unit-test time — not at the golden-diff
// review surface. Regex-anchored: pathological version overrides with
// embedded whitespace (`v1.0 dev`) fail the match.
func TestGeneratedHeaderFormat(t *testing.T) {
	headerRe := regexp.MustCompile(`^// Code generated by gqlc \S+\. DO NOT EDIT\.\n\n`)

	// The skeleton fixture's db.go carries the header at its most minimal
	// form; assert the exact byte layout the current binary's "dev" default
	// produces so a template regression that dropped the trailing blank
	// line or the LF still fails even if the regex accidentally accepts.
	// Every target the fixture is enrolled in gets the same treatment.
	const skeletonExpected = "// Code generated by gqlc dev. DO NOT EDIT.\n\n"
	skeletonDBs, err := filepath.Glob(filepath.Join(fixtureRoot(), "valid", "skeleton", "golden", "*", "db.go"))
	require.NoError(t, err)
	require.NotEmpty(t, skeletonDBs, "skeleton golden must exist")
	for _, path := range skeletonDBs {
		skeletonBytes, err := os.ReadFile(path)
		require.NoError(t, err)
		require.True(t,
			strings.HasPrefix(string(skeletonBytes), skeletonExpected),
			"%s must open with byte-exact header %q; got %q",
			path, skeletonExpected, string(skeletonBytes[:min(len(skeletonBytes), len(skeletonExpected))]))
	}

	// Walk every valid fixture's golden tree. Every .go file in every
	// golden subtree carries the header; a fixture with no .go file
	// (unlikely but not forbidden) just contributes nothing.
	validDirs, err := filepath.Glob(filepath.Join(fixtureRoot(), "valid", "*"))
	require.NoError(t, err)
	require.NotEmpty(t, validDirs, "valid fixtures must exist")

	sawAny := false
	for _, dir := range validDirs {
		goldens, err := filepath.Glob(filepath.Join(dir, "golden", "*", "*.go"))
		require.NoError(t, err)
		for _, path := range goldens {
			sawAny = true
			contents, err := os.ReadFile(path)
			require.NoError(t, err, "reading %s", path)
			// Longest expected header comfortably under 128 bytes:
			// `// Code generated by gqlc v0.0.0-20260711-<40chars>. DO NOT EDIT.\n\n`
			// weighs ~85; 128 gives slack for future release-recipe forms.
			window := contents
			if len(window) > 128 {
				window = window[:128]
			}
			require.True(t, headerRe.Match(window),
				"golden %s does not match header regex; first 128 bytes: %q",
				path, string(window))
		}
	}
	require.True(t, sawAny, "walk must encounter at least one golden .go file")
}
