package neo4j_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/areqag/gqlc/internal/codegen"
	"github.com/areqag/gqlc/internal/codegen/neo4j"
	"github.com/areqag/gqlc/internal/schema/gql"
)

const (
	// corpusPackage is the name the emission is asked for, so the emitted
	// decoders and the hand-written driver share a package clause.
	corpusPackage = "neo4jcorpus"
	// corpusSchema declares the entity shapes the driver decodes into. It
	// is this package's own schema rather than a corpus fixture because
	// the driver names the structs and the decode helpers the emission
	// derives from it, so what it declares is fixed by what the driver
	// exercises.
	corpusSchema = "corpus_schema.gql"
	// driverModule is the module path the v5 emission imports, and so the
	// path the stub has to claim.
	driverModule = "github.com/neo4j/neo4j-go-driver/v5"
)

// corpusModule resolves the driver import through a directory
// replacement, so the run needs neither the network nor a populated
// module cache: a module replaced by a filesystem path is neither
// downloaded nor summed, which is what lets GOPROXY=off through.
const corpusModule = "module " + corpusPackage + `

go 1.26.2

require ` + driverModule + ` v5.28.4

replace ` + driverModule + ` => ./driverstub
`

// stubModule lets the stub claim the driver's own path. It carries no
// requirements of its own, so the whole run is dependency-free.
const stubModule = "module " + driverModule + "\n\ngo 1.26.2\n"

// TestEmittedDecodersRunOnDriverValues runs the emitted entity decoders
// against the Go values a Bolt driver can actually put in a Props map.
// None of it can be established by reading the emission: an assertion on
// the source says a decoder was written, not that a list holding a null
// survives it, that an empty list stays distinguishable from an absent
// one, or that a property of no declared shape hands the caller back the
// value the graph held.
//
// The bytes under test come from Generate rather than from the golden
// tree, so regenerating goldens cannot make a decode bug agree with
// itself. The driver's own package is stubbed (testdata/driverstub_*)
// because neo4j.ResultWithContext carries unexported methods and so
// cannot be implemented outside it; the stub copies neo4j.PropertyValue
// and neo4j.GetProperty verbatim, and `just test-codegen-fence` compiles
// the same emitted shapes against the real driver, so what the stub is
// trusted for is behaviour rather than API surface.
func TestEmittedDecodersRunOnDriverValues(t *testing.T) {
	t.Parallel()

	src, err := os.ReadFile(filepath.Join("testdata", corpusSchema))
	require.NoError(t, err)
	sch, err := gql.New().Parse(bytes.NewReader(src))
	require.NoError(t, err)
	in := codegen.Input{Schema: sch}

	models := func(v neo4j.DriverVersion) string {
		files, err := neo4j.New(neo4j.WithPackageName(corpusPackage), neo4j.WithDriverVersion(v)).Generate(in)
		require.NoError(t, err)
		for _, f := range files {
			if f.Path == "models.go" {
				return string(f.Contents)
			}
		}
		require.FailNow(t, "emission has no models.go")
		return ""
	}
	// Running v5 is running both targets, and this is what says so: the
	// two emissions differ in the module path they import and in nothing
	// else, so a decode arm exercised below is the same arm on v6.
	v5 := models(neo4j.DriverV5)
	require.Equal(t, v5, strings.ReplaceAll(models(neo4j.DriverV6), "neo4j-go-driver/v6", "neo4j-go-driver/v5"),
		"the driver targets no longer emit the same decoders modulo the module path, so a v5 run no longer covers v6")

	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "driverstub", "neo4j", "dbtype"), 0o755))
	for name, body := range map[string]string{
		"go.mod":                              corpusModule,
		"models.go":                           v5,
		"corpus_test.go":                      corpusFile(t, "corpus_test.go.txt"),
		filepath.Join("driverstub", "go.mod"): stubModule,
		filepath.Join("driverstub", "neo4j", "graph.go"):            corpusFile(t, "driverstub_neo4j.go.txt"),
		filepath.Join("driverstub", "neo4j", "dbtype", "dbtype.go"): corpusFile(t, "driverstub_dbtype.go.txt"),
	} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644))
	}

	cmd := exec.CommandContext(t.Context(), "go", "test", "-count=1", ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOFLAGS=", "GOPROXY=off")
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "the emitted decoders do not satisfy the driver-value corpus:\n%s", out)
}

// corpusFile reads one of the hand-written sources the corpus module is
// assembled from. They carry a .txt suffix so nothing in this repo
// compiles them in place — they are only ever built against an emission.
func corpusFile(t *testing.T, name string) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("testdata", name))
	require.NoError(t, err)
	return string(body)
}
