package age_test

import (
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/areqag/gqlc/internal/codegen/age"
)

// corpusPackage is the name the emission is asked for, so the extracted
// declarations and the hand-written driver share a package clause.
const corpusPackage = "agecorpus"

// corpusModule is dependency-free by construction: the driver exercises
// declarations that import only the standard library, so the module
// resolves with the proxy off and the run needs no network.
const corpusModule = "module " + corpusPackage + "\n\ngo 1.26.2\n"

// graphStub stands in for the emitted Queries, which carries a pgx
// handle this module has no dependency on. The field name and the method
// bodies under test are the emitted ones; only the surrounding struct is
// written here.
const graphStub = "package " + corpusPackage + "\n\nimport (\n\t\"fmt\"\n\t\"strings\"\n)\n\ntype Queries struct{ graph string }\n\n"

// TestEmittedHelpersDecodeTheAgtypeCorpus runs the emitted agtype
// helpers, the emitted graph-name check and the emitted statement
// composer against captured agtype text and against the names and query
// texts that make composition hard. All are pure functions over their
// arguments and none can be exercised by reading the emission: an
// assertion on the source says the helper was written, not that the
// value it produces from `1.5::numeric` is 1.5, nor that a name carrying
// a quote arrives as one SQL literal.
//
// The bytes under test come from Generate rather than from the golden
// tree, so regenerating goldens cannot make a decode or composition bug
// agree with itself.
func (s *EmissionSuite) TestEmittedHelpersDecodeTheAgtypeCorpus() {
	files := s.emitReadBatch(age.WithPackageName(corpusPackage))
	driver, err := os.ReadFile(filepath.Join("testdata", "corpus_test.go.txt"))
	s.Require().NoError(err)

	dir := s.T().TempDir()
	for name, body := range map[string]string{
		"go.mod":         corpusModule,
		"models.go":      files["models.go"],
		"boundgraph.go":  graphStub + s.declarations(files["db.go"], "maxGraphNameBytes", "boundGraph", "cypherStmt"),
		"corpus_test.go": string(driver),
	} {
		s.Require().NoError(os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644))
	}

	cmd := exec.CommandContext(s.T().Context(), "go", "test", "-count=1", ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOFLAGS=", "GOPROXY=off")
	out, err := cmd.CombinedOutput()
	s.Require().NoError(err, "the emitted helpers do not satisfy the captured corpus:\n%s", out)
}

// declarations prints the named top-level declarations of an emitted
// file, so what the driver compiles is the emitted bytes and not a copy
// of them kept in step by hand.
func (s *EmissionSuite) declarations(body string, names ...string) string {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "db.go", body, parser.SkipObjectResolution)
	s.Require().NoError(err)

	wanted := make(map[string]bool, len(names))
	for _, n := range names {
		wanted[n] = true
	}
	var b strings.Builder
	found := 0
	for _, decl := range f.Decls {
		if !wanted[declName(decl)] {
			continue
		}
		s.Require().NoError(printer.Fprint(&b, fset, decl))
		b.WriteString("\n\n")
		found++
	}
	s.Require().Equal(len(names), found, "emitted db.go does not declare all of %v", names)
	return b.String()
}

// declName is the single name a declaration introduces, or the empty
// string for every shape that introduces some other number of them.
func declName(decl ast.Decl) string {
	switch d := decl.(type) {
	case *ast.FuncDecl:
		return d.Name.Name
	case *ast.GenDecl:
		if len(d.Specs) != 1 {
			return ""
		}
		if spec, ok := d.Specs[0].(*ast.ValueSpec); ok && len(spec.Names) == 1 {
			return spec.Names[0].Name
		}
	}
	return ""
}
