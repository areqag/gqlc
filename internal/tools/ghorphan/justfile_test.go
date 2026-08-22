package main

import (
	"bytes"
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
)

// This file holds the justfile half of this tool's write-opt-in to just's own
// reading of it, rather than to a reading of the recipe text.
//
// The shape it exists to refuse was live and green on this branch until bd
// gqlc-mb8v's round-1 review: `gh-orphans` took a `*args` tail and forwarded
// it, so `just gh-orphans -close` reached `go run ./internal/tools/ghorphan
// -close`, while the recipe's `just --list` line read "mutates nothing" and the
// comment above it claimed the acting arm was "a separate recipe rather than a
// flag on this one". `gh-orphans-close`'s body was `just gh-orphans -close`, so
// the two recipes were the same entry point.
//
// Two things are asked of just here, because either alone passes on a tree
// where the recipes are gone: that the reporting name refuses the flag, and
// that the acting name still carries it.
//
// WHAT THIS DOES NOT REACH. `go run ./internal/tools/ghorphan -close` typed
// directly, and any recipe added later under another name. The property held is
// about the two names this file spells, not about the tool's flag surface —
// there, what stands between an invocation and a close is that -close has to be
// typed, which is main()'s default and is pinned by
// TestRunIsADryRunUnlessToldOtherwise.

const (
	// repoRoot reaches the justfile from this package's directory.
	repoRoot = "../../.."

	// justfilePath is the justfile relative to repoRoot.
	justfilePath = "justfile"

	reportRecipe = "gh-orphans"
	actRecipe    = "gh-orphans-close"

	// closeFlag is what the tool reads as the opt-in. main() registers it with
	// flag.Bool. Measured against a flag.Bool("close", …) binary: `-close`,
	// `--close`, `-close=true` and `--close=true` all set it, and `-close true`
	// sets it too while leaving "true" as a positional the tool ignores. The
	// four that carry the flag in one word are tried against the reporting
	// recipe below; the fifth is the first of them followed by a word, so it
	// fails wherever they do.
	closeFlag = "-close"
)

// justOrFatal runs just in repoRoot against this repo's justfile and returns
// what it wrote and whether it exited 0.
//
// An absent just fails rather than skips: every question here is one just
// answers, so without it there is no reduced question left. CI's test job
// installs just before running this (.github/workflows/ci.yml).
func justOrFatal(t *testing.T, args ...string) (stdout string, ok bool) {
	t.Helper()
	justBin, err := exec.LookPath("just")
	if err != nil {
		t.Fatalf("`just` is not on PATH, and this test asks just what the justfile "+
			"does with `%s` rather than reading the recipe text, so this is a broken "+
			"environment and not a case to skip past: %v", closeFlag, err)
	}
	// --dry-run renders a recipe body without executing it, which is the only
	// mode this file may use: the recipes under test invoke a tool whose acting
	// arm closes issues on the live repository. What just still evaluates under
	// it is backticks and `shell()` in variable assignments; this justfile has
	// none (`grep -nE '^[a-zA-Z_-]+ *:?= *`|shell\(' justfile` is empty), and
	// the two recipes read here interpolate nothing that runs.
	cmd := exec.CommandContext(t.Context(), justBin,
		append([]string{"--justfile", justfilePath, "--dry-run"}, args...)...)
	cmd.Dir = repoRoot
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	err = cmd.Run()
	// Measured: under --dry-run just writes the rendered body to stderr and
	// leaves stdout empty, and its error messages go to stderr too. Both
	// streams are returned as one blob so a caller searching it does not have
	// to know which.
	return out.String() + errb.String(), err == nil
}

func TestTheReportingRecipeCannotBeHandedTheCloseFlag(t *testing.T) {
	// The control. Without it every case below passes on a justfile that has
	// no gh-orphans recipe at all, for the wrong reason.
	rendered, ok := justOrFatal(t, reportRecipe)
	if !ok {
		t.Fatalf("`just %s` did not render; the rest of this test would pass "+
			"vacuously:\n%s", reportRecipe, rendered)
	}
	if !strings.Contains(rendered, "go run ./internal/tools/ghorphan") {
		t.Fatalf("`just %s` renders %q, which does not run this tool", reportRecipe, rendered)
	}
	if strings.Contains(rendered, closeFlag) {
		t.Fatalf("`just %s` renders %q, which carries %s", reportRecipe, rendered, closeFlag)
	}

	for _, spelling := range []string{closeFlag, "--close", closeFlag + "=true", "--close=true"} {
		t.Run(spelling, func(t *testing.T) {
			rendered, ok := justOrFatal(t, reportRecipe, spelling)
			if ok {
				t.Errorf("`just %s %s` succeeded, rendering:\n%s\nThe reporting recipe "+
					"must take no parameter that forwards %s to the tool.",
					reportRecipe, spelling, rendered, spelling)
			}
			if strings.Contains(rendered, "go run ./internal/tools/ghorphan "+spelling) {
				t.Errorf("`just %s %s` rendered an invocation carrying %s:\n%s",
					reportRecipe, spelling, spelling, rendered)
			}
		})
	}
}

// TestTheActingRecipeCarriesTheCloseFlag is the other half. The reporting
// recipe refusing the flag is only worth something while some recipe still
// spells it: deleting the acting arm would leave the test above green over a
// tool nothing can run.
func TestTheActingRecipeCarriesTheCloseFlag(t *testing.T) {
	rendered, ok := justOrFatal(t, actRecipe)
	if !ok {
		t.Fatalf("`just %s` did not render:\n%s", actRecipe, rendered)
	}
	if !strings.Contains(rendered, "go run ./internal/tools/ghorphan "+closeFlag) {
		t.Fatalf("`just %s` renders %q, want it to run this tool with %s",
			actRecipe, rendered, closeFlag)
	}
	// The acting arm must not reach the tool by way of the reporting name.
	// That spelling is what made `just gh-orphans -close` a rendering of the
	// reporting recipe.
	if strings.Contains(rendered, "just "+reportRecipe) {
		t.Fatalf("`just %s` renders %q, which runs the reporting recipe with a flag "+
			"rather than the tool", actRecipe, rendered)
	}
}

// TestJustReadsTheReportingRecipeAsTakingNoParameters asks just for the
// structure the two tests above depend on, so a failure names the cause rather
// than the symptom. `--dump` is just's own parse of the file.
func TestJustReadsTheReportingRecipeAsTakingNoParameters(t *testing.T) {
	justBin, err := exec.LookPath("just")
	if err != nil {
		t.Fatalf("`just` is not on PATH: %v", err)
	}
	// just documents the JSON dump as unstable, so the flag is passed —
	// the same spelling internal/tools/modscope's justfile_test.go uses.
	cmd := exec.CommandContext(t.Context(), justBin,
		"--justfile", justfilePath, "--unstable", "--dump", "--dump-format", "json")
	cmd.Dir = repoRoot
	var errb bytes.Buffer
	cmd.Stderr = &errb
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("`just --dump` on the justfile: %v\n%s", err, errb.String())
	}

	var dumped struct {
		Recipes map[string]struct {
			Parameters []struct {
				Name string `json:"name"`
				Kind string `json:"kind"`
			} `json:"parameters"`
			Doc string `json:"doc"`
		} `json:"recipes"`
	}
	if err := json.Unmarshal(out, &dumped); err != nil {
		t.Fatalf("read `just --dump --dump-format json`: %v", err)
	}

	report, found := dumped.Recipes[reportRecipe]
	if !found {
		t.Fatalf("just reports no recipe named %s", reportRecipe)
	}
	if len(report.Parameters) != 0 {
		t.Errorf("just reads %s as taking %+v; a parameter is how the tool's flags "+
			"reach it, and this recipe's `just --list` line says it mutates nothing",
			reportRecipe, report.Parameters)
	}
	// The `just --list` line is the sentence an operator reads before typing
	// the name, so it is held to the same thing the recipe is.
	if !strings.Contains(report.Doc, "mutates nothing") {
		t.Errorf("the `just --list` line for %s is %q; it no longer claims to mutate "+
			"nothing, so either it or this test is out of date", reportRecipe, report.Doc)
	}
}
