package neo4j_test

import (
	"bytes"
	"os"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/areqag/gqlc/internal/codegen"
	"github.com/areqag/gqlc/internal/codegen/neo4j"
	"github.com/areqag/gqlc/internal/schema/gql"
)

const (
	// skeletonSchema is the golden corpus's minimal valid input: one node
	// type and no queries, so db.go is the only emitted file that names
	// the driver interface.
	skeletonSchema = "../../../test/data/codegen/valid/skeleton/schema.gql"
	// skeletonPackage is the name Schema.Name derivation produces for
	// that schema's `CREATE PROPERTY GRAPH TYPE Skeleton`.
	skeletonPackage = "skeleton"
)

// OptionsSuite pins the construction options this backend's New owns.
// The fixture-driven golden corpus lives in internal/codegen/conformance.
type OptionsSuite struct {
	suite.Suite

	in codegen.Input
}

func TestOptionsSuite(t *testing.T) {
	suite.Run(t, new(OptionsSuite))
}

func (s *OptionsSuite) SetupSuite() {
	src, err := os.ReadFile(skeletonSchema)
	s.Require().NoError(err)
	sch, err := gql.New().Parse(bytes.NewReader(src))
	s.Require().NoError(err)
	s.in = codegen.Input{Schema: sch}
}

// TestWithDriverVersion pins the driver-target seam at the unit level,
// independent of the golden corpus: the default (zero-value) Codegen
// emits the v5 module path and neo4j.DriverWithContext; a
// WithDriverVersion(DriverV6) Codegen emits the v6 module path and
// neo4j.Driver (v6 renamed the interface back, keeping the old name as
// an alias — generated v6 code uses the native name).
func (s *OptionsSuite) TestWithDriverVersion() {
	cases := []struct {
		name         string
		gen          *neo4j.Codegen
		wantImport   string
		wantDriver   string
		bannedDriver string
	}{
		{
			name:         "default is v5",
			gen:          neo4j.New(),
			wantImport:   `"github.com/neo4j/neo4j-go-driver/v5/neo4j"`,
			wantDriver:   "neo4j.DriverWithContext",
			bannedDriver: "/v6/",
		},
		{
			name:         "explicit v5",
			gen:          neo4j.New(neo4j.WithDriverVersion(neo4j.DriverV5)),
			wantImport:   `"github.com/neo4j/neo4j-go-driver/v5/neo4j"`,
			wantDriver:   "neo4j.DriverWithContext",
			bannedDriver: "/v6/",
		},
		{
			name:         "v6",
			gen:          neo4j.New(neo4j.WithDriverVersion(neo4j.DriverV6)),
			wantImport:   `"github.com/neo4j/neo4j-go-driver/v6/neo4j"`,
			wantDriver:   "neo4j.Driver",
			bannedDriver: "DriverWithContext",
		},
	}
	for _, tc := range cases {
		s.Run(tc.name, func() {
			files, err := tc.gen.Generate(s.in)
			s.Require().NoError(err)
			var db []byte
			for _, f := range files {
				s.Require().NotContains(string(f.Contents), tc.bannedDriver, "file %s", f.Path)
				if f.Path == "db.go" {
					db = f.Contents
				}
			}
			s.Require().NotNil(db)
			s.Require().Contains(string(db), tc.wantImport)
			s.Require().Contains(string(db), "func New(driver "+tc.wantDriver+") *Queries {")
		})
	}
}

// TestWithPackageName pins the CLI-1 §3.4 widening at the unit level:
// a configured name replaces the Schema.Name derivation across every
// emitted file; the empty string (the zero value) keeps the derivation
// so the golden corpus stays byte-identical; a value outside the
// packageIdent grammar is codegen.ErrInvalidPackageName naming the configured
// string, not the schema name.
func (s *OptionsSuite) TestWithPackageName() {
	s.Run("configured name wins", func() {
		files, err := neo4j.New(neo4j.WithPackageName("configuredpkg")).Generate(s.in)
		s.Require().NoError(err)
		s.assertPackage(files, "configuredpkg")
	})
	s.Run("empty keeps derivation", func() {
		files, err := neo4j.New(neo4j.WithPackageName("")).Generate(s.in)
		s.Require().NoError(err)
		s.assertPackage(files, skeletonPackage)
	})
	s.Run("grammar violation names the configured string", func() {
		files, err := neo4j.New(neo4j.WithPackageName("Not_OK")).Generate(s.in)
		s.Require().Error(err)
		s.Require().Nil(files)
		s.Require().ErrorIs(err, codegen.ErrInvalidPackageName)
		s.Require().ErrorContains(err, `configured package "Not_OK"`)
	})
}

// assertPackage checks every emitted file's package clause matches want.
func (s *OptionsSuite) assertPackage(files []codegen.File, want string) {
	for _, f := range files {
		lines := bytes.SplitN(f.Contents, []byte{'\n'}, 4)
		s.Require().GreaterOrEqual(len(lines), 3, "file %s too short for header + package", f.Path)
		// Line 2 is the mandatory blank; line 3 is the package clause.
		s.Require().Equal([]byte("package "+want), lines[2],
			"file %s has wrong package clause: %q", f.Path, lines[2])
	}
}
