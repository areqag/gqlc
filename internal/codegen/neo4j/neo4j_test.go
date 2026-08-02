package neo4j_test

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"maps"
	"os"
	"slices"
	"strings"
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

// decoderProbeSchema is one schema spelled around a single property
// name, placed in every arm a decode helper has: required and nullable,
// each at the driver's own width and at a narrower one, and the same
// again on an edge type, whose decoder takes the other carrier. %[1]s is
// the property name under test.
const decoderProbeSchema = `CREATE PROPERTY GRAPH TYPE DecoderProbe AS {
    (:Required { %[1]s :: STRING NOT NULL }),
    (:Nullable { %[1]s :: STRING }),
    (:Narrow { %[1]s :: FLOAT32 NOT NULL }),
    (:NarrowNullable { %[1]s :: FLOAT32 }),
    (:Required) -[:LINKS { %[1]s :: STRING NOT NULL }]-> (:Nullable)
}`

// unclaimedProperty is a property name no emitted decoder holds, so an
// emission spelled with it is the reference the probes are measured
// against.
const unclaimedProperty = "alpha"

// DecoderSuite pins what an emitted decode<Name> may name. The fixture-
// driven golden corpus lives in internal/codegen/conformance.
type DecoderSuite struct {
	suite.Suite
}

func TestDecoderSuite(t *testing.T) {
	suite.Run(t, new(DecoderSuite))
}

// TestNoDecoderLocalTakesAPropertyName pins the decoder's scope against
// the one thing in it a schema author chooses. A property name reaches
// the emission as a Props key and a struct field; a local named after it
// as well lands in the same scope as the accumulator, the error and the
// carrier the helper was handed, and `err :: STRING NOT NULL` emits
// `err, err :=`. Generation exits 0 over that, because the format gate
// parses the emission and does not type-check it.
//
// The names are read off an emission rather than listed here, so a local
// a decoder gains later is held by this without anyone remembering to
// add it. Each one is then fed back as a property name, and what must
// not move is the set itself: the decoder's identifiers are the
// generator's own, so they are the same whatever the schema declares.
func (s *DecoderSuite) TestNoDecoderLocalTakesAPropertyName() {
	models, err := s.emitModels(unclaimedProperty)
	s.Require().NoError(err)
	declared := s.decoderScopeOf(models)
	s.Require().NotEmpty(declared, "the emitted decoders bind no identifiers to check")

	for _, name := range declared {
		s.Run(name, func() {
			models, err := s.emitModels(name)
			if err != nil {
				// A word the GQL grammar reserves is one no schema can
				// spell a property after, and so one no decoder can
				// collide with.
				s.T().Skipf("no property can be named %q: %v", name, err)
			}
			s.Require().Contains(models, "\t"+exportedField(name)+" ",
				"the struct no longer carries the property under the name the schema gave it")
			s.Require().Equal(declared, s.decoderScopeOf(models),
				"a property name reached the decoder's scope")
		})
	}
}

// emitModels emits models.go for a schema whose every entity declares
// one property named prop. The error is the schema parse's alone: a
// generation that failed would be this suite's business, and a parse
// that failed is the caller's.
func (s *DecoderSuite) emitModels(prop string) (string, error) {
	sch, err := gql.New().Parse(strings.NewReader(fmt.Sprintf(decoderProbeSchema, prop)))
	if err != nil {
		return "", err
	}
	files, err := neo4j.New().Generate(codegen.Input{Schema: sch})
	s.Require().NoError(err)
	for _, f := range files {
		if f.Path == "models.go" {
			return string(f.Contents), nil
		}
	}
	s.Require().Fail("no models.go in the emission")
	return "", nil
}

// decoderScopeOf names every identifier the emitted decode helpers bind,
// deduplicated and ordered. The carrier argument counts: a parameter
// shares the body's outermost scope, so a local of that name is not a
// declaration but an assignment over the value the helper was handed.
func (s *DecoderSuite) decoderScopeOf(models string) []string {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "models.go", models, parser.SkipObjectResolution)
	s.Require().NoError(err, "the emitted file does not parse")

	seen := make(map[string]bool)
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		// The methods models.go declares are edge-union markers, which
		// take no argument and hold no body; the decode helpers are what
		// is left.
		if !ok || fn.Recv != nil || fn.Body == nil {
			continue
		}
		for _, param := range fn.Type.Params.List {
			for _, id := range param.Names {
				seen[id.Name] = true
			}
		}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			for _, id := range declaredIdents(n) {
				if id.Name != "_" {
					seen[id.Name] = true
				}
			}
			return true
		})
	}
	return slices.Sorted(maps.Keys(seen))
}

// declaredIdents returns the identifiers a node binds. Short variable
// declarations, var declarations and range clauses are the whole of what
// an emitted body uses to introduce a name.
func declaredIdents(n ast.Node) []*ast.Ident {
	var out []*ast.Ident
	switch stmt := n.(type) {
	case *ast.AssignStmt:
		if stmt.Tok != token.DEFINE {
			return nil
		}
		for _, lhs := range stmt.Lhs {
			if id, ok := lhs.(*ast.Ident); ok {
				out = append(out, id)
			}
		}
	case *ast.ValueSpec:
		out = append(out, stmt.Names...)
	case *ast.RangeStmt:
		if stmt.Tok != token.DEFINE {
			return nil
		}
		for _, e := range []ast.Expr{stmt.Key, stmt.Value} {
			if id, ok := e.(*ast.Ident); ok {
				out = append(out, id)
			}
		}
	}
	return out
}

// exportedField is the struct field a property named name lands on. The
// names this suite probes are ASCII and carry no underscore, which is
// the whole of what the §4.2 mangle does to them.
func exportedField(name string) string {
	return strings.ToUpper(name[:1]) + name[1:]
}
