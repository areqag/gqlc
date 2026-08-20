package vulnguard_test

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const (
	// repoRoot reaches the justfile from this package's directory.
	repoRoot = "../../.."

	// justfile holds the recipe under test.
	justfile = "justfile"

	// vulnRecipe is the recipe this package runs.
	vulnRecipe = "vuln"

	// blindDir is the throwaway tree's wholly build-constrained directory and
	// blindTag the tag its files carry. selftest_tagblind requires a directory
	// of that shape and requires the derivation to have found the tag, so the
	// stub toolchain has to agree with the fixture on both.
	blindDir = "tagblind"
	blindTag = "tagblind"

	// scanModules is the module count the stub scan's header reports. It is
	// not the 2 the recipe's own witness headers carry, which is what lets the
	// header match tell a grading of the scan from a grading of a witness
	// header on the count alone.
	scanModules = 43

	// stdlibVersion is what the stub scan reports govulncheck placed on the
	// standard library. A version, so the grading takes its accepting arm.
	stdlibVersion = "go1.26.6"

	// stubModule is the throwaway root module's path.
	stubModule = "gqlc.invalid/agt0"
)

// edit is one substitution applied to the `vuln` recipe before it is run.
//
// Applied to the recipe's own span of the justfile rather than to the whole
// file, and required to match exactly once: an anchor that has drifted stops
// the test rather than producing a run that measures nothing.
type edit struct {
	old, new string
}

// recipeSpan is the half-open line range a recipe occupies — its header line
// and the indented or empty lines after it, which is what just reads as the
// body.
func recipeSpan(t *testing.T, lines []string, name string) (int, int) {
	t.Helper()
	header := regexp.MustCompile(`^` + regexp.QuoteMeta(name) + `(\s[^:\n]*)?:`)
	for i, line := range lines {
		if !header.MatchString(line) {
			continue
		}
		end := i + 1
		for ; end < len(lines); end++ {
			next := lines[end]
			if next != "" && !strings.HasPrefix(next, " ") && !strings.HasPrefix(next, "\t") {
				break
			}
		}
		return i, end
	}
	t.Fatalf("the %s declares no `%s` recipe", justfile, name)
	return 0, 0
}

// justfileFor is the repository's justfile with the recipes `vuln` depends on
// stubbed out and edits applied to `vuln` itself.
//
// The dependencies are stubbed because they read the real tree — a discovery
// sweep over test/data and a residual measured against a checked-in baseline —
// and neither is what this package is asking about.
//
// The stub targets are derived from `vuln`'s own header, so what this harness
// notices about that header is exactly one thing: measured, dropping ALL of
// `vuln`'s dependencies reddens 8 of the 8 tests here, and dropping only the
// sweep edge reddens 0 of 8. That edge is guarded by modscope's
// TestEveryRecipeRunningModscopeSweepsProbesFirst, which reads the repository's
// justfile and never sees the copy written below.
func justfileFor(t *testing.T, src string, edits []edit) string {
	t.Helper()
	lines := strings.Split(src, "\n")

	start, end := recipeSpan(t, lines, vulnRecipe)
	deps := strings.Fields(strings.SplitN(lines[start], ":", 2)[1])
	require.NotEmptyf(t, deps, "`%s` names no dependencies, so this harness is stubbing "+
		"nothing and the run below is not the recipe just runs", vulnRecipe)

	body := strings.Join(lines[start+1:end], "\n")
	for _, e := range edits {
		require.Equalf(t, 1, strings.Count(body, e.old),
			"the anchor\n%s\noccurs %d times in the `%s` recipe body, and this test needs "+
				"exactly one. The recipe moved; re-anchor the edit rather than dropping it, "+
				"or the run below measures nothing (bd gqlc-agt0).",
			e.old, strings.Count(body, e.old), vulnRecipe)
		body = strings.Replace(body, e.old, e.new, 1)
	}
	lines = append(lines[:start+1:start+1], append(strings.Split(body, "\n"), lines[end:]...)...)

	for _, dep := range deps {
		dStart, dEnd := recipeSpan(t, lines, dep)
		require.Emptyf(t, strings.Fields(strings.SplitN(lines[dStart], ":", 2)[1]),
			"`%s` has dependencies of its own, which this harness does not stub", dep)
		lines = append(lines[:dStart+1:dStart+1], append([]string{"    @true"}, lines[dEnd:]...)...)
	}
	return strings.Join(lines, "\n")
}

// stubGo stands in for the go command. Only the invocations the recipe makes
// are answered; anything else exits 2 with a message, so a recipe that grows
// one more fails here rather than reading a silent empty answer as a
// measurement.
//
// pwd -P throughout: the recipe compares an absolute path go_dirs printed
// against one `go list -m -f {{.Dir}}` printed, and a temporary directory
// reached through a symlink would make those two spellings of one directory.
const stubGo = `#!/usr/bin/env bash
set -euo pipefail

root='@ROOT@'
ids=(@IDS@)

die() { printf 'stub go: %s\n' "$*" >&2; exit 2; }

modroot() {
    local d="$1"
    while :; do
        if [ -f "${d}/go.mod" ]; then printf '%s\n' "${d}"; return 0; fi
        if [ "${d}" = "/" ]; then return 1; fi
        d="$(dirname "${d}")"
    done
}

modpath() { sed -n 's/^module[[:space:]]\{1,\}//p' "$1/go.mod" | head -n1; }

# Every directory of the module rooted at $1 holding a Go file, absolute,
# stopping at a nested module's root and applying go's ./... exclusions.
go_dirs() {
    local top="$1" rel dir
    ( cd "${top}" && find . -type f -name '*.go' ) | while IFS= read -r rel; do
        case "${rel}" in */.*|*/_*|*/testdata/*|*/vendor/*) continue ;; esac
        dir="$(cd "${top}/$(dirname "${rel}")" && pwd -P)"
        if [ "$(modroot "${dir}")" != "${top}" ]; then continue; fi
        printf '%s\n' "${dir}"
    done | sort -u
}

list_modules() {
    local f
    ( cd "${root}" && find . -name go.mod ) | while IFS= read -r f; do
        f="$(dirname "${f}")"
        printf '%s\n' "${f#./}"
    done | sort -u
}

list_tags() { if [ "$1" = "." ]; then printf '%s\n' '@BLINDTAG@'; fi; }

scan() {
    local i=1 id
    printf 'Scanning your code and 210 packages across @MODULES@ dependent modules...\n\n'
    printf '=== Symbol Results ===\n\nNo vulnerabilities found.\n\n=== Package Results ===\n\n'
    for id in "${ids[@]}"; do
        printf 'Vulnerability #%d: %s\n' "${i}" "${id}"
        printf '    reported by the stub so the register in the recipe balances\n'
        printf '  More info: https://pkg.go.dev/vuln/%s\n\n' "${id}"
        i=$((i + 1))
    done
    printf 'Govulncheck scanned the following @MODULES@ modules and the @STDLIB@ standard library:\n\n'
    printf '  @MODULE@\n\n'
    printf 'Your code is affected by 0 vulnerabilities.\n'
}

cmd="${1:-}"
shift || true

if [ "${cmd}" = "run" ]; then
    pkg="${1:-}"
    shift || true
    case "${pkg}" in
        ./internal/tools/modscope)
            sub="${1:-}"
            shift || true
            case "${sub}" in
                modules) list_modules ;;
                dirs) go_dirs "$(cd "${1}" && pwd -P)" ;;
                tags) list_tags "${1}" ;;
                *) die "unexpected modscope subcommand '${sub}'" ;;
            esac
            exit 0
            ;;
        golang.org/x/vuln/cmd/govulncheck@latest) scan; exit 0 ;;
        *) die "unexpected 'go run' of '${pkg}'" ;;
    esac
fi

if [ "${cmd}" != "list" ]; then die "unexpected subcommand '${cmd}'"; fi

mflag=0
fmt=""
tags=""
while [ "$#" -gt 0 ]; do
    case "$1" in
        -m) mflag=1 ;;
        -e) ;;
        -f) fmt="$2"; shift ;;
        -tags) tags="$2"; shift ;;
        ./...) ;;
        *) die "unexpected 'go list' argument '$1'" ;;
    esac
    shift
done

here="$(modroot "$(pwd -P)")" || die "no go.mod at or above $(pwd -P)"
if [ "${mflag}" -eq 1 ]; then
    if [ -z "${fmt}" ]; then modpath "${here}"; else printf '%s\n' "${here}"; fi
    exit 0
fi
case "${fmt}" in
    *IgnoredGoFiles*) exit 0 ;;
esac
go_dirs "${here}" | while IFS= read -r d; do
    case "${d}" in
        */'@BLINDDIR@')
            case ",${tags}," in
                *",@BLINDTAG@,"*) ;;
                *) continue ;;
            esac
            ;;
    esac
    printf '%s\n' "${d}"
done
`

// acceptedIDs is the advisory register the recipe carries, read out of the
// recipe by the recipe's own extraction. The stub scan reports exactly these,
// because the register is compared against what was reported in both
// directions: an id the stub invented would be unregistered and one it left
// out would be stale, and either fails every run here for a reason that is
// this harness rather than the recipe.
var acceptedIDs = regexp.MustCompile(`(?m)^\s*(GO-[0-9]{4}-[0-9]+)\s*$`)

func registerIn(t *testing.T, src string) []string {
	t.Helper()
	lines := strings.Split(src, "\n")
	start, end := recipeSpan(t, lines, vulnRecipe)
	body := strings.Join(lines[start+1:end], "\n")

	open := strings.Index(body, "<<'ACCEPTED'")
	require.GreaterOrEqualf(t, open, 0, "the `%s` recipe carries no ACCEPTED heredoc, so "+
		"this harness cannot tell which advisories its stub scan has to report", vulnRecipe)
	rest := body[open:]
	term := strings.Index(rest, "\n    ACCEPTED\n")
	require.GreaterOrEqualf(t, term, 0, "the ACCEPTED heredoc in `%s` is not terminated where "+
		"this harness looks for it", vulnRecipe)

	var ids []string
	for _, m := range acceptedIDs.FindAllStringSubmatch(rest[:term], -1) {
		ids = append(ids, m[1])
	}
	require.NotEmptyf(t, ids, "the ACCEPTED register in `%s` reads as empty here. The recipe "+
		"refuses a scan that named no advisory, so every run below would fail on that "+
		"rather than on what it was asked", vulnRecipe)
	return ids
}

// writeTree lays out the throwaway tree the recipe is run over.
//
// The two fixture directories are the ones the recipe's selftests require to
// exist: a directory whose every Go file is build-constrained, and one file
// carrying a negated GOOS term. The nested module is what makes the loop run
// more than once, which is what the per-module tally after it is about.
func writeTree(t *testing.T, dir, justfileSrc string, ids []string) {
	t.Helper()
	write := func(rel, content string, mode os.FileMode) {
		abs := filepath.Join(dir, filepath.FromSlash(rel))
		require.NoError(t, os.MkdirAll(filepath.Dir(abs), 0o755))
		require.NoError(t, os.WriteFile(abs, []byte(content), mode))
	}

	write(justfile, justfileSrc, 0o600)
	write("go.mod", "module "+stubModule+"\n\ngo 1.26.5\n", 0o600)
	write("root.go", "package agt0\n", 0o600)
	write("test/data/"+blindDir+"/"+blindDir+".go",
		"//go:build "+blindTag+"\n\npackage "+blindTag+"\n", 0o600)
	write("test/data/platformtag/platformtag.go",
		"//go:build !windows\n\npackage platformtag\n", 0o600)
	write("test/data/codegen/go.mod", "module "+stubModule+"/codegen\n\ngo 1.26.5\n", 0o600)
	write("test/data/codegen/codegen.go", "package codegen\n", 0o600)

	root, err := filepath.EvalSymlinks(dir)
	require.NoError(t, err)
	quoted := make([]string, 0, len(ids))
	for _, id := range ids {
		quoted = append(quoted, "'"+id+"'")
	}
	stub := strings.NewReplacer(
		"@ROOT@", root,
		"@IDS@", strings.Join(quoted, " "),
		"@MODULES@", strconv.Itoa(scanModules),
		"@STDLIB@", stdlibVersion,
		"@MODULE@", stubModule,
		"@BLINDDIR@", blindDir,
		"@BLINDTAG@", blindTag,
	).Replace(stubGo)
	write("bin/go", stub, 0o700)
}

// vulnRun is what happened when the recipe ran.
type vulnRun struct {
	status int
	output string
}

// runVuln runs the real `vuln` recipe, with the given edits applied, over a
// throwaway tree and a stubbed toolchain.
//
// `just` is required rather than skipped over: it is what the CI test job runs
// and what .githooks/pre-push runs, so an absent `just` is a broken
// environment. A skip here would be the fail-open shape this package exists to
// close.
func runVuln(t *testing.T, edits ...edit) vulnRun {
	t.Helper()

	justBin, err := exec.LookPath("just")
	require.NoError(t, err, "`just` is not on PATH, and this test runs the %q recipe rather "+
		"than reading it. CI runs these tests through `just`, so this is a broken "+
		"environment and not a case to skip past.", vulnRecipe)

	src, err := os.ReadFile(filepath.Join(repoRoot, justfile))
	require.NoError(t, err)

	dir := t.TempDir()
	writeTree(t, dir, justfileFor(t, string(src), edits), registerIn(t, string(src)))

	cmd := exec.CommandContext(t.Context(), justBin, vulnRecipe)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "PATH="+filepath.Join(dir, "bin")+":"+os.Getenv("PATH"))
	out, runErr := cmd.CombinedOutput()

	run := vulnRun{output: string(out)}
	var ee *exec.ExitError
	switch {
	case runErr == nil:
	case errors.As(runErr, &ee):
		run.status = ee.ExitCode()
	default:
		t.Fatalf("could not run %q: %v\n%s", vulnRecipe, runErr, out)
	}
	return run
}
