package codegen

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	"github.com/areqag/gqlc/internal/procsig"
	"github.com/areqag/gqlc/internal/query/cypher"
	"github.com/areqag/gqlc/internal/queryfile"
	"github.com/areqag/gqlc/internal/resolver"
	"github.com/areqag/gqlc/internal/schema"
	"github.com/areqag/gqlc/internal/schema/gql"
)

var update = flag.Bool("update", false, "regenerate codegen golden files")

const fixtureDir = "../../test/data/codegen"

// manifest is the on-disk descriptor per fixture directory. Present in
// both valid and invalid fixtures; the invalid arm additionally carries
// ExpectedError (fully-qualified sentinel name) and, for the hand-
// constructed ErrInvalidCardinality case, SyntheticZeroCardinality —
// see loadInvalidInput.
type manifest struct {
	Package                  string   `json:"package"`
	QueryFiles               []string `json:"queryFiles"`
	ExpectedError            string   `json:"expectedError,omitempty"`
	SyntheticZeroCardinality bool     `json:"syntheticZeroCardinality,omitempty"`
}

// sentinelByName maps the manifest's fully-qualified sentinel string
// back to the actual error value at load time. Built from the two
// package's allSentinels slices — a change there without a fixture
// update fails the queryfile / codegen reachability sweeps, and a
// fixture that names a non-canonical sentinel fails invalidFixtures'
// map lookup.
var sentinelByName = func() map[string]error {
	m := make(map[string]error)
	pairs := []struct {
		prefix string
		set    []error
	}{
		{"codegen.", allSentinels},
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
	case ErrInvalidPackageName:
		return "ErrInvalidPackageName"
	case ErrDuplicateSourceFile:
		return "ErrDuplicateSourceFile"
	case ErrDuplicateQueryName:
		return "ErrDuplicateQueryName"
	case ErrInvalidCardinality:
		return "ErrInvalidCardinality"
	case ErrFormatFailure:
		return "ErrFormatFailure"
	case ErrOutOfC1Scope:
		return "ErrOutOfC1Scope"
	case ErrParamNameCollision:
		return "ErrParamNameCollision"
	case ErrRowFieldCollision:
		return "ErrRowFieldCollision"
	case ErrAliasRequired:
		return "ErrAliasRequired"
	case ErrIdentifierCollision:
		return "ErrIdentifierCollision"
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

// CodegenSuite is the testify suite for the codegen tests.
type CodegenSuite struct {
	suite.Suite
}

func TestCodegenSuite(t *testing.T) {
	suite.Run(t, new(CodegenSuite))
}

// loadManifest reads a manifest.json from the given fixture directory.
func (s *CodegenSuite) loadManifest(dir string) manifest {
	src, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	s.Require().NoError(err)
	var m manifest
	s.Require().NoError(json.Unmarshal(src, &m))
	return m
}

// loadSchema parses schema.gql in the given fixture directory.
func (s *CodegenSuite) loadSchema(dir string) schema.Schema {
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
// s.Require().NoError below; the invalid-arm variant
// (loadNamedQueriesAllowing) permits pre-codegen errors when the
// fixture declares a non-codegen expected sentinel.
func (s *CodegenSuite) loadNamedQueries(dir string, m manifest, sch schema.Schema) []NamedQuery {
	out, err := loadNamedQueries(dir, m, sch)
	s.Require().NoError(err)
	return out
}

// loadNamedQueries is the shared load path used by both TestValid and
// TestInvalid. Returns the first resolution error verbatim so the
// invalid arm can decide whether to accept it (a fixture may target a
// non-codegen sentinel that fires upstream of codegen).
func loadNamedQueries(dir string, m manifest, sch schema.Schema) ([]NamedQuery, error) {
	emptyReg, err := procsig.NewRegistry(nil)
	if err != nil {
		return nil, err
	}
	res := resolver.New(sch, resolver.WithRegistry(emptyReg))
	var out []NamedQuery
	for _, qf := range m.QueryFiles {
		src, err := os.ReadFile(filepath.Join(dir, qf))
		if err != nil {
			return nil, err
		}
		parsed, err := queryfile.New().Parse(bytes.NewReader(src))
		if err != nil {
			return nil, err
		}
		base := filepath.Base(qf)
		for _, aq := range parsed {
			q, err := cypher.New(cypher.WithRegistry(emptyReg)).Parse(bytes.NewReader([]byte(aq.Text)))
			if err != nil {
				return nil, err
			}
			vq, err := res.Resolve(q)
			if err != nil {
				return nil, err
			}
			out = append(out, NamedQuery{
				Name:        aq.Name,
				Cardinality: aq.Cardinality,
				SourceFile:  base,
				SourceText:  aq.Text,
				Validated:   vq,
			})
		}
	}
	return out, nil
}

// validFixtures walks valid/*/.
func (s *CodegenSuite) validFixtures() []string {
	dirs, err := filepath.Glob(filepath.Join(fixtureDir, "valid", "*"))
	s.Require().NoError(err)
	s.Require().NotEmpty(dirs)
	return dirs
}

// invalidFixtures walks invalid/*/.
func (s *CodegenSuite) invalidFixtures() []string {
	dirs, err := filepath.Glob(filepath.Join(fixtureDir, "invalid", "*"))
	s.Require().NoError(err)
	s.Require().NotEmpty(dirs)
	return dirs
}

// TestValid walks valid/*/ and either writes the golden directory
// (-update) or asserts byte-equality against every file it contains.
// The comparison is bytes.Equal, not JSONEq: the output is Go source,
// and every whitespace character matters (gofmt normalises, but the
// tree it produces is stable).
func (s *CodegenSuite) TestValid() {
	for _, dir := range s.validFixtures() {
		name := filepath.Base(dir)
		s.Run(name, func() {
			m := s.loadManifest(dir)
			sch := s.loadSchema(dir)
			queries := s.loadNamedQueries(dir, m, sch)

			got, err := New().Generate(Input{Schema: sch, Queries: queries})
			s.Require().NoError(err)
			s.assertPackage(got, m.Package)

			goldenDir := filepath.Join(dir, "golden")
			if *update {
				s.Require().NoError(syncGoldenDir(goldenDir, got))
				return
			}
			s.assertGoldenTree(goldenDir, got)
		})
	}
}

// TestInvalid walks invalid/*/, resolves the manifest's ExpectedError
// to a sentinel, calls the pipeline, and asserts (a) the returned
// []File is nil and (b) errors.Is(err, wantErr).
func (s *CodegenSuite) TestInvalid() {
	dirs := s.invalidFixtures()
	for _, dir := range dirs {
		name := filepath.Base(dir)
		s.Run(name, func() {
			m := s.loadManifest(dir)
			s.Require().NotEmpty(m.ExpectedError, "invalid fixture %q must declare expectedError", name)

			wantErr, ok := sentinelByName[m.ExpectedError]
			s.Require().True(ok, "unknown sentinel name %q in fixture %q", m.ExpectedError, name)

			in := s.loadInvalidInput(dir, m)
			got, err := New().Generate(in)
			s.Require().Error(err)
			s.Require().Nil(got, "files must be nil on error")
			s.Require().ErrorIs(err, wantErr)
		})
	}
}

// loadInvalidInput assembles the Input for an invalid fixture. Two
// paths: normal (schema + queryFiles pipeline) and synthetic (a hand-
// constructed NamedQuery with a zero-valued Cardinality, the only way
// to reach ErrInvalidCardinality — the queryfile front end never emits
// one).
func (s *CodegenSuite) loadInvalidInput(dir string, m manifest) Input {
	sch := s.loadSchema(dir)
	if m.SyntheticZeroCardinality {
		return Input{
			Schema: sch,
			Queries: []NamedQuery{{
				Name:       "ZeroCardinality",
				SourceFile: "synthetic.cypher",
				SourceText: "MATCH (n) RETURN n",
			}},
		}
	}
	return Input{Schema: sch, Queries: s.loadNamedQueries(dir, m, sch)}
}

// TestDoubleRun asserts Generate is byte-deterministic: same Input in,
// byte-identical []File out, twice. Independent of the golden
// comparison — a golden diff catches within-run nondeterminism (map
// iteration) only flakily; this test catches it in a single run.
func (s *CodegenSuite) TestDoubleRun() {
	for _, dir := range s.validFixtures() {
		name := filepath.Base(dir)
		s.Run(name, func() {
			m := s.loadManifest(dir)
			sch := s.loadSchema(dir)
			in := Input{Schema: sch, Queries: s.loadNamedQueries(dir, m, sch)}
			first, err := New().Generate(in)
			s.Require().NoError(err)
			second, err := New().Generate(in)
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
}

// TestSentinelReachability is the bidirectional sweep: every
// codegen.allSentinels member has at least one invalid fixture; every
// mapped codegen sentinel is in allSentinels. Queryfile sentinels
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
	canonical := make(map[error]bool, len(allSentinels))
	for _, sentinel := range allSentinels {
		canonical[sentinel] = true
	}
	for _, sentinel := range allSentinels {
		require.True(t, covered[sentinel], "sentinel %q has no negative fixture", sentinel)
	}
	for sentinel := range covered {
		require.True(t, canonical[sentinel], "fixture maps to non-canonical sentinel %q", sentinel)
	}
}

// isCodegenSentinel reports whether err is one of the codegen package's
// user-input-reachable sentinels (i.e. in allSentinels). Identity match
// is intentional — see sentinelIdent.
//
//nolint:errorlint // identity match on package-level sentinels is intended
func isCodegenSentinel(err error) bool {
	for _, s := range allSentinels {
		if s == err {
			return true
		}
	}
	return false
}

// assertPackage checks every emitted file's package clause matches the
// manifest's declared package. Cheap; catches a template regression
// that swaps package names between files.
func (s *CodegenSuite) assertPackage(files []File, want string) {
	for _, f := range files {
		lines := bytes.SplitN(f.Contents, []byte{'\n'}, 4)
		s.Require().GreaterOrEqual(len(lines), 3, "file %s too short for header + package", f.Path)
		// Line 2 is the mandatory blank; line 3 is the package clause.
		s.Require().Equal([]byte("package "+want), lines[2],
			"file %s has wrong package clause: %q", f.Path, lines[2])
	}
}

// assertGoldenTree walks the golden directory and asserts every file
// there is present in got with byte-identical contents, and every file
// in got is present on disk. On mismatch, the assertion reports the
// file path and a diff-shaped message.
func (s *CodegenSuite) assertGoldenTree(dir string, got []File) {
	gotByPath := make(map[string][]byte, len(got))
	for _, f := range got {
		gotByPath[f.Path] = f.Contents
	}

	entries, err := os.ReadDir(dir)
	s.Require().NoError(err, "missing golden dir; run go test -update")

	diskByPath := make(map[string][]byte, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
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

// syncGoldenDir wipes and rewrites the golden directory from got.
// Equivalent to what the future CLI's out-dir sync will do: delete not-
// in-output, write all-in-output. A query removed from the input must
// not leave its old .cypher.go in the golden dir.
func syncGoldenDir(dir string, got []File) error {
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
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
