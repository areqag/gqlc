package liverecipes

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// The tests here reach names this package does not export, so they cannot move
// to the external test package beside them, and they assert with the standard
// library alone: an in-package test file importing third-party code takes the
// whole package out of govulncheck's call graph, which `just vuln-root-residual`
// refuses (bd gqlc-m5rc). Everything reachable through the exported surface is
// in split_test.go.

// TestLiveInvocationsReadsWhatTheShellWouldRun covers the step before the
// rules: a command line this misses is a half the rules never see, and a
// command line it invents is a half they see twice.
func TestLiveInvocationsReadsWhatTheShellWouldRun(t *testing.T) {
	for _, tc := range []struct {
		name           string
		src            string
		wantFields     [][]string
		wantComplaints int
	}{
		{
			name:       "a live recipe body is one invocation",
			src:        "r:\n    cd test/data/codegen && go test -count=1 -tags codegen_live ./...\n",
			wantFields: [][]string{{"go", "test", "-count=1", "-tags", "codegen_live", "./..."}},
		},
		{
			name: "a command line with no -tags builds no live test",
			src:  "r:\n    go test -count=1 ./...\n",
		},
		{
			name: "a -tags without the live tag builds no live test",
			src:  "r:\n    go test -tags integration ./...\n",
		},
		{
			name:       "the live tag beside another is still the live build",
			src:        "r:\n    go test -tags integration,codegen_live ./...\n",
			wantFields: [][]string{{"go", "test", "-tags", "integration,codegen_live", "./..."}},
		},
		{
			// go test honours the last -tags, so a second one without the tag
			// leaves the live files uncompiled.
			name: "a second -tags dropping the live tag is not the live build",
			src:  "r:\n    go test -tags codegen_live -tags integration ./...\n",
		},
		{
			name: "a go test the line only prints is not one it runs",
			src:  "r:\n    echo go test -tags codegen_live ./...\n",
		},
		{
			name: "a commented-out invocation is not one",
			src:  "r:\n    # go test -tags codegen_live ./...\n",
		},
		{
			// The `&&` puts the commented text in command position, so what
			// keeps it out is the comment cut and not the position check.
			name: "a comment spelling a command after an operator is still a comment",
			src:  "r:\n    # disabled && go test -tags codegen_live ./...\n",
		},
		{
			// The direction that matters: a trailing comment's words read as
			// flags would narrow an invocation the shell does not narrow.
			name:       "a trailing comment's words are not the command's arguments",
			src:        "r:\n    go test -tags codegen_live ./...  # -run TestLiveSmoke\n",
			wantFields: [][]string{{"go", "test", "-tags", "codegen_live", "./..."}},
		},
		{
			name:           "a line whose quoting never closed is a complaint",
			src:            "r:\n    go test -tags codegen_live -run 'TestLiveSmoke ./...\n",
			wantComplaints: 1,
		},
		{
			// Nothing here builds the live tag, so the unterminated line is
			// still read: what the open quote swallowed can be the -tags.
			name:           "an unterminated line is a complaint before it is classified",
			src:            "r:\n    go test -run 'TestLiveSmoke ./...\n",
			wantComplaints: 1,
		},
		{
			name: "an unterminated line with no go test on it is not this reader's",
			src:  "r:\n    echo 'unclosed\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			found, complaints := liveInvocations(tc.src)
			if len(complaints) != tc.wantComplaints {
				t.Fatalf("complaints: got %d %v, want %d", len(complaints), complaints, tc.wantComplaints)
			}
			var fields [][]string
			for _, one := range found {
				fields = append(fields, one.Fields)
			}
			if !slices.EqualFunc(fields, tc.wantFields, slices.Equal) {
				t.Fatalf("fields: got %v, want %v", fields, tc.wantFields)
			}
		})
	}
}

// TestReadDeclaredComplainsWhenTwoFilesDeclareTheName holds the shape a
// last-one-wins map would swallow: `-run TestLiveSmoke` would select both
// bodies, and a rule satisfied by one of them says nothing about the other.
func TestReadDeclaredComplainsWhenTwoFilesDeclareTheName(t *testing.T) {
	dir := t.TempDir()
	const src = "//go:build codegen_live\n\npackage p\n\nimport \"testing\"\n\nfunc TestLiveSmoke(t *testing.T) {}\n"
	for _, name := range []string{"live_a_test.go", "live_b_test.go"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(src), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	names, complaints, err := readDeclared(dir)
	if err != nil {
		t.Fatalf("readDeclared: %v", err)
	}
	if want := []string{"TestLiveSmoke"}; !slices.Equal(names, want) {
		t.Fatalf("names: got %v, want %v", names, want)
	}
	if len(complaints) != 1 || !strings.Contains(complaints[0], "TestLiveSmoke") {
		t.Fatalf("complaints: got %v, want exactly one naming TestLiveSmoke", complaints)
	}
}

// TestReadWorkflowRecipesComplainsWhenItReadsNoWorkflow is the CI half's
// vacuity guard at its source: a directory that yields no file makes every
// live recipe Local, where the rule is different rather than absent, and the
// disagreement it was pointed at goes unstated.
func TestReadWorkflowRecipesComplainsWhenItReadsNoWorkflow(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "notes.md"), []byte("just test-codegen-live-age\n"), 0o600); err != nil {
		t.Fatalf("write notes.md: %v", err)
	}

	names, complaints, err := readWorkflowRecipes(dir)
	if err != nil {
		t.Fatalf("readWorkflowRecipes: %v", err)
	}
	if len(names) != 0 {
		t.Fatalf("names: got %v, want none", names)
	}
	if len(complaints) != 1 || !strings.Contains(complaints[0], dir) {
		t.Fatalf("complaints: got %v, want exactly one naming %s", complaints, dir)
	}
}

// TestReadAssemblesTheSplitFromDisk drives the three readers together over a
// tree written here, because which half an invocation lands in is decided by
// the join between the workflow's recipe names and the justfile's headers, and
// neither reader alone can be wrong about it.
func TestReadAssemblesTheSplitFromDisk(t *testing.T) {
	justfile := "test-live-half:\n" +
		"    cd test/data/codegen && go test -tags codegen_live -run TestLiveSmoke ./...\n" +
		"\n" +
		"test-live-whole:\n" +
		"    cd test/data/codegen && go test -count=1 -tags codegen_live ./...\n" +
		"\n" +
		"test-unit:\n" +
		"    go test ./...\n"
	workflow := "jobs:\n  live:\n    steps:\n      - run: just test-live-half\n      - run: just test-live-absent\n"
	source := "//go:build codegen_live\n\npackage p\n\nimport \"testing\"\n\nfunc TestLiveSmoke(t *testing.T) {}\n"

	root := t.TempDir()
	for path, content := range map[string]string{
		justfilePath:                                 justfile,
		filepath.Join(workflowDir, "live.yml"):       workflow,
		filepath.Join(liveSourceDir, "live_test.go"): source,
	} {
		full := filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(full), err)
		}
		if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
			t.Fatalf("write %s: %v", full, err)
		}
	}

	split, complaints, err := Read(root)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(complaints) != 0 {
		t.Fatalf("complaints: got %v; a recipe the workflow names and the justfile does not falls through to Local", complaints)
	}
	if want := []string{"TestLiveSmoke"}; !slices.Equal(split.Declared, want) {
		t.Fatalf("declared: got %v, want %v; the unit recipe builds no live tag, so its command line is in neither half", split.Declared, want)
	}
	if len(split.CI) != 1 || split.CI[0].Recipe != "test-live-half" {
		t.Fatalf("CI: got %v, want one invocation from test-live-half", split.CI)
	}
	if len(split.Local) != 1 || split.Local[0].Recipe != "test-live-whole" {
		t.Fatalf("local: got %v, want one invocation from test-live-whole", split.Local)
	}
	if rules := split.Complaints(); len(rules) != 0 {
		t.Fatalf("rule complaints: got %v, want none", rules)
	}
}

// TestSubtractLeavesLocalRecipesOnlyCIDoesNotReach keeps two recipes running
// byte-identical commands two invocations: collapsing them would move a live
// recipe no workflow reaches into the half that is not checked for it.
func TestSubtractLeavesLocalRecipesOnlyCIDoesNotReach(t *testing.T) {
	fields := []string{"go", "test", "-tags", LiveBuildTag, "./..."}
	one := Invocation{Recipe: "a", Fields: fields}
	two := Invocation{Recipe: "b", Fields: fields}
	if left := subtract([]Invocation{one, two}, []Invocation{one}); len(left) != 1 {
		t.Fatalf("subtract: got %v, want one invocation left", left)
	}
	if left := subtract([]Invocation{one, two}, []Invocation{one, two}); len(left) != 0 {
		t.Fatalf("subtract: got %v, want none left", left)
	}
	// The empty-taken path. Complaints' vacuity-guard comment leans on it: an
	// empty CI is the state where every live invocation sits in Local, not one
	// where there are none. The two rows above pass a partial and a full taken,
	// so a short-circuit returning nil for an empty one satisfies both while
	// falsifying that sentence. Asserted by membership rather than by count,
	// for the reason Complaints gives about its own guards.
	all := []Invocation{one, two}
	left := subtract(all, nil)
	if !slices.EqualFunc(left, all, func(a, b Invocation) bool {
		return a.Recipe == b.Recipe && slices.Equal(a.Fields, b.Fields)
	}) {
		t.Fatalf("subtract(all, nil): got %v, want all of them", left)
	}
}
