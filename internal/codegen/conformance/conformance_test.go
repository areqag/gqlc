// Package conformance holds the golden-corpus suite for code
// generation. It sits above the backend packages and resolves every
// fixture's declared emission targets through the composed registry, so
// it must import no single backend.
package conformance_test

import (
	"bytes"
	"encoding/json"
	"flag"
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

const fixtureDir = "../../../test/data/codegen"

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

// sentinelByName maps the manifest's fully-qualified sentinel string
// back to the actual error value at load time. Built from the two
// packages' sentinel sets — a change there without a fixture update
// fails the queryfile / codegen reachability sweeps, and a fixture that
// names a non-canonical sentinel fails invalidFixtures' map lookup.
var sentinelByName = func() map[string]error {
	m := make(map[string]error)
	pairs := []struct {
		prefix string
		set    []error
	}{
		{"codegen.", codegenSentinels},
		{"queryfile.", queryfile.AllSentinels()},
	}
	for _, p := range pairs {
		for _, s := range p.set {
			m[p.prefix+sentinelIdent(s)] = s
		}
	}
	return m
}()

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

// loadManifest reads a manifest.json from the given fixture directory.
func (s *ConformanceSuite) loadManifest(dir string) manifest {
	src, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	s.Require().NoError(err)
	var m manifest
	s.Require().NoError(json.Unmarshal(src, &m))
	s.Require().NotEmpty(m.Targets,
		"fixture %q declares no targets; every fixture must name the emission targets it is enrolled in "+
			"(one of %v), because there is no default enrolment", dir, s.backends.Keys())
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

// loadNamedQueries walks the manifest's queryFiles and turns each into
// NamedQueries. C1 threads the cypher parser and the resolver into the
// pipeline so every read query carries a real Validated shape — Phase A
// and Phase B key on it (spec §2.1). A fixture whose queries fail
// resolution earlier than codegen fails the suite via
// s.Require().NoError below.
func (s *ConformanceSuite) loadNamedQueries(dir string, m manifest, sch schema.Schema) []codegen.NamedQuery {
	out, err := loadNamedQueries(dir, m, sch)
	s.Require().NoError(err)
	return out
}

// loadNamedQueries is the shared load path used by both TestValid and
// TestInvalid. Returns the first resolution error verbatim so the
// invalid arm can decide whether to accept it (a fixture may target a
// non-codegen sentinel that fires upstream of codegen).
func loadNamedQueries(dir string, m manifest, sch schema.Schema) ([]codegen.NamedQuery, error) {
	emptyReg, err := procsig.NewRegistry(nil)
	if err != nil {
		return nil, err
	}
	res := resolver.New(sch, resolver.WithRegistry(emptyReg))
	var out []codegen.NamedQuery
	for _, qf := range m.QueryFiles {
		src, err := os.ReadFile(filepath.Join(dir, qf))
		if err != nil {
			return nil, err
		}
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

// validFixtures walks valid/*/.
func (s *ConformanceSuite) validFixtures() []string {
	dirs, err := filepath.Glob(filepath.Join(fixtureDir, "valid", "*"))
	s.Require().NoError(err)
	s.Require().NotEmpty(dirs)
	return dirs
}

// invalidFixtures walks invalid/*/.
func (s *ConformanceSuite) invalidFixtures() []string {
	dirs, err := filepath.Glob(filepath.Join(fixtureDir, "invalid", "*"))
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
			if *update {
				// The whole root goes, so a target dropped from the manifest
				// leaves no subtree behind.
				s.Require().NoError(os.RemoveAll(goldenRoot))
			} else {
				s.assertGoldenTargets(goldenRoot, m.Targets)
			}

			for _, target := range m.Targets {
				s.Run(target, func() {
					got, err := s.generate(target, codegen.Input{Schema: sch, Queries: queries})
					s.Require().NoError(err)
					s.assertPackage(got, m.Package)

					goldenDir := filepath.Join(goldenRoot, target)
					if *update {
						s.Require().NoError(writeGoldenDir(goldenDir, got))
						return
					}
					s.assertGoldenTree(goldenDir, got)
				})
			}
		})
	}
}

// TestInvalid walks invalid/*/ x its enrolled targets, resolves the
// manifest's ExpectedError to a sentinel, calls the pipeline, and
// asserts (a) the returned []codegen.File is nil and (b)
// errors.Is(err, wantErr).
func (s *ConformanceSuite) TestInvalid() {
	for _, dir := range s.invalidFixtures() {
		name := filepath.Base(dir)
		s.Run(name, func() {
			m := s.loadManifest(dir)
			s.Require().NotEmpty(m.ExpectedError, "invalid fixture %q must declare expectedError", name)

			wantErr, ok := sentinelByName[m.ExpectedError]
			s.Require().True(ok, "unknown sentinel name %q in fixture %q", m.ExpectedError, name)

			in := s.loadInvalidInput(dir, m)
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

// loadInvalidInput assembles the codegen.Input for an invalid fixture. Two
// paths: normal (schema + queryFiles pipeline) and synthetic (a hand-
// constructed codegen.NamedQuery with a zero-valued Cardinality, the only way
// to reach codegen.ErrInvalidCardinality — the queryfile front end never emits
// one).
func (s *ConformanceSuite) loadInvalidInput(dir string, m manifest) codegen.Input {
	sch := s.loadSchema(dir)
	if m.SyntheticZeroCardinality {
		return codegen.Input{
			Schema: sch,
			Queries: []codegen.NamedQuery{{
				Name:       "ZeroCardinality",
				SourceFile: "synthetic.cypher",
				SourceText: "MATCH (n) RETURN n",
			}},
		}
	}
	return codegen.Input{Schema: sch, Queries: s.loadNamedQueries(dir, m, sch)}
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
	dirs, err := filepath.Glob(filepath.Join(fixtureDir, "invalid", "*"))
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
	s.Require().NoError(err)

	diskByPath := make(map[string][]byte, len(entries))
	for _, e := range entries {
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

// writeGoldenDir writes got into one target's golden directory. It
// assumes the caller has already wiped the fixture's golden root: that
// wipe is what removes the .cypher.go of a query deleted from the input.
func writeGoldenDir(dir string, got []codegen.File) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for _, f := range got {
		if err := os.WriteFile(filepath.Join(dir, f.Path), f.Contents, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// TestGoldenBuild compiles the nested test/data/codegen module so that
// spurious or missing imports in generated golden packages are caught by
// go test ./internal/codegen/... rather than only by just test-codegen-fence.
func TestGoldenBuild(t *testing.T) {
	abs, err := filepath.Abs(fixtureDir)
	require.NoError(t, err)
	cmd := exec.CommandContext(t.Context(), "go", "build", "./...")
	cmd.Dir = abs
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "generated golden packages do not compile:\n%s", out)
}

// TestGeneratedHeaderFormat fences the emitted-file header format (spec
// §5.2, §6.1) byte-level: fixed prefix, one non-whitespace version token,
// fixed suffix, LF, blank line. Walks every valid fixture's golden .go
// files so a future template regression that smuggled a build timestamp,
// UUID, hostname, doc comment, or //go:build line between the header and
// the package clause fails at unit-test time — not at the golden-diff
// review surface. Regex-anchored: pathological version overrides with
// embedded whitespace (`v1.0 dev`) fail the match.
//
// The regex is test-local per spec §6.1 rejected-alternative: exposing it
// as a package-level codegen.HeaderRegex var would invite consumers to
// depend on it and freeze the format under a public contract. The test
// carries the invariant without the surface cost.
func TestGeneratedHeaderFormat(t *testing.T) {
	headerRe := regexp.MustCompile(`^// Code generated by gqlc \S+\. DO NOT EDIT\.\n\n`)

	// The skeleton fixture's db.go carries the header at its most minimal
	// form; assert the exact byte layout the current binary's "dev" default
	// produces so a template regression that dropped the trailing blank
	// line or the LF still fails even if the regex accidentally accepts.
	const skeletonExpected = "// Code generated by gqlc dev. DO NOT EDIT.\n\n"
	skeletonDB := filepath.Join(fixtureDir, "valid", "skeleton", "golden", "neo4j-go-v5", "db.go")
	skeletonBytes, err := os.ReadFile(skeletonDB)
	require.NoError(t, err, "skeleton golden must exist")
	require.True(t,
		strings.HasPrefix(string(skeletonBytes), skeletonExpected),
		"skeleton db.go must open with byte-exact header %q; got %q",
		skeletonExpected, string(skeletonBytes[:min(len(skeletonBytes), len(skeletonExpected))]))

	// Walk every valid fixture's golden tree. Every .go file in every
	// golden subtree carries the header; a fixture with no .go file
	// (unlikely but not forbidden) just contributes nothing.
	validDirs, err := filepath.Glob(filepath.Join(fixtureDir, "valid", "*"))
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
