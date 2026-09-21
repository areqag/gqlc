package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"testing"

	"gopkg.in/yaml.v3"
)

const (
	repoRoot    = "../../.."
	fenceRecipe = "test-codegen-fence"
	gateRecipe  = "check-goldens-unused"
	fenceJob    = "codegen-fence"
)

// TestTheRequiredFenceReachesTheUnusedGate holds the two edges between a PR and
// `unused` over the goldens: ci.yml's codegen-fence job runs the fence recipe,
// and just runs the gate as one of that recipe's dependencies. With either
// removed every check stays green and the log is two lines shorter, which is
// all that would say so. It asks just for the edge rather than reading the
// recipe header, as internal/tools/ghorphan's justfile test does.
func TestTheRequiredFenceReachesTheUnusedGate(t *testing.T) {
	justBin, err := exec.LookPath("just")
	if err != nil {
		t.Fatalf("`just` is not on PATH: %v", err)
	}
	cmd := exec.CommandContext(t.Context(), justBin,
		"--justfile", "justfile", "--unstable", "--dump", "--dump-format", "json")
	cmd.Dir = repoRoot
	var errb bytes.Buffer
	cmd.Stderr = &errb
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("`just --dump` on the justfile: %v\n%s", err, errb.String())
	}
	var dumped struct {
		Recipes map[string]struct {
			Dependencies []struct {
				Recipe string `json:"recipe"`
			} `json:"dependencies"`
		} `json:"recipes"`
	}
	if err := json.Unmarshal(out, &dumped); err != nil {
		t.Fatalf("read `just --dump --dump-format json`: %v", err)
	}
	fence, ok := dumped.Recipes[fenceRecipe]
	if !ok {
		t.Fatalf("just reports no recipe named %s", fenceRecipe)
	}
	reaches := false
	for _, d := range fence.Dependencies {
		reaches = reaches || d.Recipe == gateRecipe
	}
	if !reaches {
		t.Errorf("just runs %+v for %s and %s is not among them, so no required check runs "+
			"`unused` over the goldens (bd gqlc-ukzq)", fence.Dependencies, fenceRecipe, gateRecipe)
	}

	raw, err := os.ReadFile(repoRoot + "/.github/workflows/ci.yml")
	if err != nil {
		t.Fatal(err)
	}
	var workflow struct {
		Jobs map[string]struct {
			Steps []struct {
				Run string `yaml:"run"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(raw, &workflow); err != nil {
		t.Fatalf("read ci.yml: %v", err)
	}
	runs := false
	for _, s := range workflow.Jobs[fenceJob].Steps {
		runs = runs || s.Run == "just "+fenceRecipe
	}
	if !runs {
		t.Errorf("ci.yml's %s job has no step running `just %s`", fenceJob, fenceRecipe)
	}
}
