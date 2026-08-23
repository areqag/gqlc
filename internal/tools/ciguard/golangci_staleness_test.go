// A linter older than the toolchain it is pointed at must be refused BY NAME.
//
// `ensure-golangci` provisions the version pinned by `golangci_version` in the
// justfile of the tree you are standing in. A branch based before the commit
// that last bumped that line therefore provisions the OLD linter, and a
// go1.N-built golangci-lint cannot load go1.(N+1) source: it dies inside
// go/types with a stack trace that names neither the pin nor the branch base.
// The gap this closes is not that lint fails — it is that lint fails
// unattributably, so the reader's next move is a guess (bd gqlc-6rf3).
//
// These rows are behavioural. The recipe is run for real, in a throwaway tree
// holding a copy of the repository's own justfile and a stub linter, because
// what is being asserted is what a citizen SEES: a text pin over the recipe
// body could be satisfied by a message that never prints.
package ciguard_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// localGoMinor is the minor of `go env GOVERSION`, which is the same question
// the recipe asks. Reading it here rather than from runtime.Version() keeps the
// rows measuring the comparison instead of a disagreement between two sources.
func localGoMinor(t *testing.T) int {
	t.Helper()
	goBin, err := exec.LookPath("go")
	require.NoError(t, err, "`go` is not on PATH, and the recipe reads `go env GOVERSION`")
	out, err := exec.CommandContext(t.Context(), goBin, "env", "GOVERSION").Output()
	require.NoError(t, err, "go env GOVERSION")

	m := regexp.MustCompile(`^go(\d+)\.(\d+)`).FindStringSubmatch(strings.TrimSpace(string(out)))
	require.NotNilf(t, m, "unrecognised GOVERSION %q, so no row below can name a "+
		"version older than it", strings.TrimSpace(string(out)))
	minor, err := strconv.Atoi(m[2])
	require.NoError(t, err)
	return minor
}

// pinnedGolangci is the version `ensure-golangci` will accept without
// provisioning. The stub has to answer `version --short` with exactly this or
// the recipe reaches for the network before it reaches the comparison.
func pinnedGolangci(t *testing.T) string {
	t.Helper()
	src, err := os.ReadFile(filepath.Join(repoRoot, justfilePath))
	require.NoError(t, err, "read %s", justfilePath)

	m := regexp.MustCompile(`(?m)^golangci_version\s*:=\s*"([^"]+)"`).FindStringSubmatch(string(src))
	require.NotNil(t, m, "%s no longer assigns golangci_version, so the stub below "+
		"cannot answer `version --short` with the pin and the recipe would "+
		"provision over the network instead of comparing", justfilePath)
	return m[1]
}

// runEnsureGolangci copies the repository's justfile into a throwaway tree,
// plants a stub linter whose two version spellings are given by the caller, and
// runs the real recipe there.
func runEnsureGolangci(t *testing.T, short, banner string) (combined string, exitCode int) {
	t.Helper()
	justBin, err := exec.LookPath("just")
	require.NoError(t, err, "`just` is not on PATH, and these rows run the real recipe")

	src, err := os.ReadFile(filepath.Join(repoRoot, justfilePath))
	require.NoError(t, err, "read %s", justfilePath)

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "justfile"), src, 0o600))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".bin"), 0o750))

	stub := fmt.Sprintf(`#!/usr/bin/env bash
if [ "$1" = "version" ] && [ "${2:-}" = "--short" ]; then printf '%%s\n' %q; exit 0; fi
if [ "$1" = "version" ]; then printf '%%s\n' %q; exit 0; fi
exit 0
`, short, banner)
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".bin", "golangci-lint"), []byte(stub), 0o700))

	cmd := exec.CommandContext(t.Context(), justBin, "ensure-golangci")
	cmd.Dir = dir
	out, runErr := cmd.CombinedOutput()
	combined = string(out)

	// A justfile that stops parsing also prints `error:` and exits non-zero, and
	// that is the fake RED this repository screens mutations for. Naming it here
	// means a row can never read as a refusal it did not cause.
	require.NotContainsf(t, combined, "error: Expected",
		"the justfile copy did not parse, so this row measures a syntax error "+
			"rather than the staleness comparison:\n%s", combined)

	if runErr == nil {
		return combined, 0
	}
	var ee *exec.ExitError
	require.ErrorAsf(t, runErr, &ee, "could not run `just ensure-golangci`:\n%s", combined)
	return combined, ee.ExitCode()
}

func TestEnsureGolangciRefusesALinterOlderThanTheToolchain(t *testing.T) {
	t.Parallel()

	pin := pinnedGolangci(t)
	short := strings.TrimPrefix(pin, "v")
	minor := localGoMinor(t)

	banner := func(builtMinor int) string {
		return fmt.Sprintf("golangci-lint has version %s built with go1.%d "+
			"from abcdef on 2026-01-01", short, builtMinor)
	}

	for _, row := range []struct {
		name string
		// banner is what the stub prints for a bare `version`.
		banner string
		// refuses is whether the recipe must stop.
		refuses bool
		// why states what the row is FOR, so a failure reads as a claim rather
		// than as an exit code.
		why string
	}{
		{
			name:    "built with an older Go",
			banner:  banner(minor - 1),
			refuses: true,
			why: "a linter one minor behind the local toolchain cannot load this " +
				"tree's source, and the go/types panic names no cause",
		},
		{
			name:    "built with a much older Go",
			banner:  banner(minor - 7),
			refuses: true,
			why:     "distance from the toolchain does not change the verdict",
		},
		{
			name:    "built with the same Go",
			banner:  banner(minor),
			refuses: false,
			why:     "the matching case is the ordinary one and must not be reddened",
		},
		{
			name:    "built with a newer Go",
			banner:  banner(minor + 1),
			refuses: false,
			why: "a linter built with a NEWER Go loads older source fine, so " +
				"refusing that direction would redden a working tree",
		},
		{
			name:    "banner reworded by upstream",
			banner:  fmt.Sprintf("golangci-lint %s (rev abcdef)", short),
			refuses: false,
			why: "this is a diagnosis attached to a provisioning step; a linter " +
				"that runs must not be blocked because the version banner changed shape",
		},
		{
			name:    "no banner at all",
			banner:  "",
			refuses: false,
			why:     "an unreadable version is silence, not an accusation",
		},
	} {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			out, code := runEnsureGolangci(t, short, row.banner)

			if !row.refuses {
				require.Zerof(t, code, "ensure-golangci refused %q, but %s:\n%s",
					row.banner, row.why, out)
				return
			}

			require.NotZerof(t, code, "ensure-golangci accepted %q, but %s:\n%s",
				row.banner, row.why, out)

			// The verdict alone is not the claim. What gqlc-6rf3 is about is
			// that the reader learns the CAUSE, so each part of the message a
			// citizen needs is pinned separately: without the pin they cannot
			// tell which line is stale, and without the remedy they cannot act.
			require.Containsf(t, out, pin,
				"the refusal does not name the pinned version %s, so a reader "+
					"cannot tell which justfile line is stale:\n%s", pin, out)
			require.Containsf(t, out, "git merge origin/master",
				"the refusal does not name the remedy, which leaves the reader "+
					"with a verdict and no next move:\n%s", out)
			require.Containsf(t, strings.ToLower(out), "base",
				"the refusal does not name the branch base as the cause, which "+
					"is the diagnosis the go/types stack trace withholds:\n%s", out)
		})
	}
}
