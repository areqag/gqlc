package cli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/areqag/gqlc/internal/version"
)

// executeRoot drives a fresh root command in-process with its output
// streams captured, returning stdout, stderr, and Execute's error —
// the error Main maps to exit 0/1.
func executeRoot(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	// nil args make cobra fall back to os.Args[1:], which under `go
	// test` smuggles in -test.* flags; normalise to an empty slice.
	if args == nil {
		args = []string{}
	}
	root := newRootCmd()
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs(args)
	err := root.Execute()
	return stdout.String(), stderr.String(), err
}

// TestRootBareInvocation: bare `gqlc` has no Run, so cobra's default
// prints help and Execute returns nil — exit 0 via Main's mapping.
// Cobra's own-help template renders Long, not Short, so the opening
// Long line is the asserted copy.
func TestRootBareInvocation(t *testing.T) {
	stdout, _, err := executeRoot(t)
	require.NoError(t, err)
	require.Contains(t, stdout,
		"gqlc generates type-safe Go from a graph schema file and a directory of")
	require.Contains(t, stdout, "\n  version ")
}

// TestRootHelpCommandList fences the CLI-0 command surface: version
// and cobra's help are the only listed commands; completion exists but
// is hidden; generate/init have not landed. Matched as "\n  <name> "
// list entries — the Short/Long prose contains "Generate"/"generates",
// so a bare substring check would false-positive.
func TestRootHelpCommandList(t *testing.T) {
	stdout, _, err := executeRoot(t, "--help")
	require.NoError(t, err)
	require.Contains(t, stdout, "\n  version ")
	require.Contains(t, stdout, "\n  help ")
	require.NotContains(t, stdout, "\n  completion ")
	require.NotContains(t, stdout, "\n  generate ")
	require.NotContains(t, stdout, "\n  init ")
}

// TestVersionOutput: bare version plus one LF on stdout, nothing on
// stderr (spec §2.2).
func TestVersionOutput(t *testing.T) {
	stdout, stderr, err := executeRoot(t, "version")
	require.NoError(t, err)
	require.Equal(t, version.Version+"\n", stdout)
	require.Empty(t, stderr)
}

func TestVersionRejectsArgs(t *testing.T) {
	_, _, err := executeRoot(t, "version", "extra")
	require.Error(t, err)
}

func TestUnknownCommand(t *testing.T) {
	_, _, err := executeRoot(t, "frobnicate")
	require.ErrorContains(t, err, "frobnicate")
}

// TestVersionLdflagsOverride is the suite's only process-spawning test
// (~1–2 s, build-cached): the sole automated fence for acceptance
// criterion 3 and for the C6 §4.1 const-regression class, where a
// release override silently no-ops.
func TestVersionLdflagsOverride(t *testing.T) {
	const overridden = "v1.2.3-test"
	bin := filepath.Join(t.TempDir(), "gqlc")
	build := exec.CommandContext(t.Context(), "go", "build",
		"-ldflags", "-X github.com/areqag/gqlc/internal/version.Version="+overridden,
		"-o", bin, "./cmd/gqlc")
	build.Dir = "../.."
	// GOPROXY=off: every dependency is already in the local module cache
	// (this test binary just built from the same set), so the child build
	// can never reach for the network in CI.
	build.Env = append(os.Environ(), "GOPROXY=off")
	out, err := build.CombinedOutput()
	require.NoError(t, err, "go build ./cmd/gqlc failed:\n%s", out)

	run := exec.CommandContext(t.Context(), bin, "version")
	var stdout, stderr bytes.Buffer
	run.Stdout = &stdout
	run.Stderr = &stderr
	require.NoError(t, run.Run(), "gqlc version failed: %s", stderr.String())
	require.Equal(t, overridden+"\n", stdout.String())
	require.Empty(t, stderr.String())
}
