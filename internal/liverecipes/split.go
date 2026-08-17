package liverecipes

import (
	"fmt"
	"go/ast"
	"go/build/constraint"
	"go/parser"
	"go/token"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	// LiveBuildTag is the tag the live battery is behind. A `go test` that does
	// not build it compiles none of those files, so it runs no live test
	// however its -run reads: measured as review mutation T1, which left the
	// AGE witness sweep green while `go test` printed "[no test files]" for
	// every package in the codegen module.
	LiveBuildTag = "codegen_live"

	justfilePath  = "justfile"
	liveSourceDir = "test/data/codegen"
	workflowDir   = ".github/workflows"
)

// Invocation is one live `go test` command line out of the justfile. Recipe is
// the recipe it was read under, and carries no weight beyond a message: which
// invocations CI reaches is decided by the command lines themselves, so a
// mis-attributed name misnames a complaint and changes no answer.
type Invocation struct {
	Recipe string
	Fields []string
}

func (inv Invocation) String() string {
	cmd := strings.Join(inv.Fields, " ")
	if inv.Recipe == "" {
		return cmd
	}
	return inv.Recipe + ": " + cmd
}

// claims reports whether this command line runs the top-level test named name.
//
// -run is read as an exact list of names split on `|`, where go test reads it
// as an unanchored regexp. The divergence is the point: `-run TestLiveSmoke`
// selects a later TestLiveSmokeFoo without naming it, so a regexp reading would
// report a new live test as covered by an allowlist that never mentioned it.
// This reading is the narrower one wherever they differ, and the difference is
// therefore a complaint about a test the halves do run rather than a silence
// about one they do not.
//
// -skip stays a regexp, and only a WHOLE match counts: `-skip TestLiveSmoke`
// removes the test, `-skip TestLiveSmoke/apache-age` removes one subtest and
// leaves the test running. Reading a narrowed -skip as removal would make the
// arm split unstateable, since that carve-out is what the halves are.
//
// A flag written twice is read as all of its values, where go test honours the
// last. That direction complains: a name only the last -run carries reads as
// unclaimed, and a test only the last -skip drops reads as dropped.
func (inv Invocation) claims(name string) bool {
	for _, pattern := range FlagValues(inv.Fields, "run") {
		if !slices.Contains(strings.Split(pattern, "|"), name) {
			return false
		}
	}
	for _, pattern := range FlagValues(inv.Fields, "skip") {
		if _, wholly := Selects(pattern, name); wholly {
			return false
		}
	}
	return true
}

// Split is the live battery as its two artefacts declare it: the top-level
// tests the codegen module's sources declare, and the live `go test` command
// lines the justfile runs, divided by whether a workflow reaches them.
type Split struct {
	Declared []string
	CI       []Invocation
	Local    []Invocation
}

// Complaints is every way the two artefacts disagree.
//
// The arm split is a partition and not one equality, because the halves are
// deliberately unequal: the PR-blocking half runs the neo4j battery alone, and
// charging a pull request for AGE containers is the cost that split exists to
// refuse (.github/workflows/codegen-live.yml). So what has to hold is that
// every declared test is claimed by SOME half, and that every name a half
// claims is declared — both directions, because a test declared and claimed by
// nothing is a battery that is green from never running, and a name claimed and
// declared by nothing is the same rot pointing the other way.
//
// What this does NOT say is WHICH half a test belongs in. Which containers a
// live test needs is a property of its body, declared nowhere this reads, so a
// half that stops running a test another half still runs passes here (bd
// gqlc-vh74). A disagreement complaint names the test or the name at fault, and
// a vacuity complaint names the collection that is empty; neither reports a
// count, because a count pins the size of a guard and not its membership.
func (s Split) Complaints() []string {
	var complaints []string
	// The vacuity guards. Every rule below is written over one of these three
	// collections, so an empty one makes its rule pass by iterating nothing.
	// Local is absent on purpose: a justfile whose every live recipe is reached
	// by a workflow is a sound tree, not a silent one.
	if len(s.Declared) == 0 {
		complaints = append(complaints,
			"no top-level live test was found, so every rule here compares the recipes against nothing")
	}
	if len(s.CI) == 0 {
		complaints = append(complaints,
			"no workflow reaches a live `go test`, so no live test is required to run anywhere")
	}

	for _, inv := range append(slices.Clone(s.CI), s.Local...) {
		for _, pattern := range FlagValues(inv.Fields, "run") {
			for _, alt := range strings.Split(pattern, "|") {
				if !slices.Contains(s.Declared, alt) {
					complaints = append(complaints, fmt.Sprintf(
						"%q is selected by `%s` and declared by no live test: either it was renamed "+
							"and the allowlist kept the old spelling, or the allowlist is a pattern "+
							"rather than a name and would claim a test nobody wrote down", alt, inv))
				}
			}
		}
	}

	for _, name := range s.Declared {
		if !slices.ContainsFunc(s.CI, func(inv Invocation) bool { return inv.claims(name) }) {
			complaints = append(complaints, fmt.Sprintf(
				"%s is a live test no CI job runs: add it to a live recipe's -run, or the job that "+
					"was meant to gate it goes green without it", name))
		}
	}

	for _, inv := range s.Local {
		for _, name := range s.Declared {
			if !inv.claims(name) {
				complaints = append(complaints, fmt.Sprintf(
					"%s is a live test `%s` does not run: a live recipe no workflow reaches runs the "+
						"whole battery, so it selects tests by no name at all", name, inv))
			}
		}
	}
	return complaints
}

// Read assembles the split from the repository rooted at root. The complaints
// it returns are about READING the artefacts — one that could not be found, a
// name declared twice, a command line whose quoting never closed — and are
// separate from Split.Complaints, which is about what they say. Both have to
// be empty; the split is a guess wherever the first is not.
func Read(root string) (Split, []string, error) {
	declared, complaints, err := readDeclared(filepath.Join(root, liveSourceDir))
	if err != nil {
		return Split{}, nil, err
	}

	justSrc, err := os.ReadFile(filepath.Join(root, justfilePath)) //nolint:gosec // a path built from the caller's own repo root
	if err != nil {
		return Split{}, nil, err
	}
	all, unreadable := liveInvocations(string(justSrc))
	complaints = append(complaints, unreadable...)

	recipes, workflowComplaints, err := readWorkflowRecipes(filepath.Join(root, workflowDir))
	if err != nil {
		return Split{}, nil, err
	}
	complaints = append(complaints, workflowComplaints...)

	var ci []Invocation
	for _, name := range recipes {
		body, ok := RecipeBody(string(justSrc), name)
		if !ok {
			// Not a complaint: a workflow runs recipes this package has no
			// claim on, and one it cannot read by name falls through to Local,
			// where the rule is stricter rather than absent.
			continue
		}
		found, unreadableBody := liveInvocations(body)
		complaints = append(complaints, unreadableBody...)
		for _, inv := range found {
			inv.Recipe = name
			ci = append(ci, inv)
		}
	}

	return Split{Declared: declared, CI: ci, Local: subtract(all, ci)}, complaints, nil
}

// subtract removes each of taken from all by command line, once per occurrence,
// so two recipes running byte-identical commands stay two invocations.
func subtract(all, taken []Invocation) []Invocation {
	remaining := make(map[string]int, len(taken))
	for _, inv := range taken {
		remaining[strings.Join(inv.Fields, "\x00")]++
	}
	var left []Invocation
	for _, inv := range all {
		key := strings.Join(inv.Fields, "\x00")
		if remaining[key] > 0 {
			remaining[key]--
			continue
		}
		left = append(left, inv)
	}
	return left
}

// liveInvocations is every `go test` in a justfile fragment that builds
// LiveBuildTag, alongside the complaints about lines it could not finish
// reading.
//
// The recipe a line belongs to is taken from the last unindented line ending in
// `:`, which is a guess about justfile syntax and is used for no more than the
// text of a complaint.
func liveInvocations(src string) ([]Invocation, []string) {
	var (
		found      []Invocation
		complaints []string
		recipe     string
	)
	for _, line := range strings.Split(src, "\n") {
		if line != "" && !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") {
			name, _, isHeader := strings.Cut(line, ":")
			recipe = ""
			if isHeader && !strings.ContainsAny(name, " \t#") {
				recipe = name
			}
		}
		stripped := StripComment(line)
		commands, unterminated := Commands(stripped)
		// A line whose quoting never closed hands back fields that are a guess,
		// and the guess is not one safe direction: what a swallowed argument
		// takes with it can be the -tags that makes the command live or the
		// -run that narrows it. Read from the raw text rather than from the
		// commands, because the `go test` itself can be inside the open quote.
		if unterminated && strings.Contains(stripped, "go test") {
			complaints = append(complaints, fmt.Sprintf(
				"a `go test` line the reader could not finish: %s", strings.TrimSpace(line)))
			continue
		}
		for _, command := range commands {
			if runs(command, "go", "test") && buildsLiveTag(command) {
				found = append(found, Invocation{Recipe: recipe, Fields: command})
			}
		}
	}
	return found, complaints
}

// buildsLiveTag reports whether a command line compiles the live battery. Every
// -tags value has to carry the tag where go test honours the last, which is a
// complaint rather than a silence, and a command line with no -tags at all
// builds none of those files.
func buildsLiveTag(fields []string) bool {
	values := FlagValues(fields, "tags")
	if len(values) == 0 {
		return false
	}
	for _, value := range values {
		if !slices.Contains(strings.FieldsFunc(value, func(r rune) bool {
			return r == ',' || r == ' '
		}), LiveBuildTag) {
			return false
		}
	}
	return true
}

// readDeclared is DeclaredTests over every test file under dir. A name declared
// twice is a complaint rather than a last-one-wins: `go test -run` would select
// both, and a rule written over one of them says nothing about the other.
func readDeclared(dir string) ([]string, []string, error) {
	fset := token.NewFileSet()
	declaredIn := make(map[string]string)
	var complaints []string
	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), "_test.go") {
			return nil
		}
		src, err := os.ReadFile(path) //nolint:gosec // a path WalkDir produced under the caller's own repo root
		if err != nil {
			return err
		}
		names, err := DeclaredTests(fset, path, src)
		if err != nil {
			return err
		}
		for _, name := range names {
			if first, seen := declaredIn[name]; seen {
				complaints = append(complaints, fmt.Sprintf(
					"%s is declared by both %s and %s, so a recipe naming it runs a test this reader "+
						"cannot identify", name, first, path))
				continue
			}
			declaredIn[name] = path
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	names := slices.Sorted(maps.Keys(declaredIn))
	return names, complaints, nil
}

// DeclaredTests is every top-level test the live build compiles out of one
// file: a function `go test -run` can select by name, in a file the live tag
// does not exclude.
//
// Parsed rather than scanned for "func Test", because a scan cannot tell a
// declaration from a string literal or a commented-out one spelling it, and a
// commented-out test that counted as declared would force an allowlist entry
// for a test that does not exist. go/parser reads a build-tagged file without
// honouring the tag, which is what lets a binary built without it read one.
//
// Methods are skipped, and so is TestMain: a method sharing a test's name puts
// a body under a name -run cannot select, and TestMain takes a *testing.M and
// runs whatever the selection is rather than being selected.
func DeclaredTests(fset *token.FileSet, path string, src []byte) ([]string, error) {
	if !strings.HasSuffix(path, "_test.go") {
		return nil, nil
	}
	file, err := parser.ParseFile(fset, path, src, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if !builtWithLiveTag(file) {
		return nil, nil
	}
	var names []string
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv != nil || fn.Body == nil || !isTestFunc(fn) {
			continue
		}
		names = append(names, fn.Name.Name)
	}
	return names, nil
}

// builtWithLiveTag reports whether the live build compiles this file. It asks
// whether the constraint is satisfiable with every tag set, LiveBuildTag among
// them, so the only shape it excludes is a negated term — which is the shape
// that takes a file out of the live build. A file constrained to a GOOS this
// reader is not running on therefore still counts, and the direction is
// deliberate: over-counting demands an allowlist entry and fails loudly, while
// under-counting drops a test out of the split with nothing said.
func builtWithLiveTag(file *ast.File) bool {
	for _, group := range file.Comments {
		if group.Pos() > file.Package {
			break
		}
		for _, comment := range group.List {
			if !constraint.IsGoBuild(comment.Text) {
				continue
			}
			expr, err := constraint.Parse(comment.Text)
			if err != nil {
				return false
			}
			return expr.Eval(func(string) bool { return true })
		}
	}
	return true
}

// isTestFunc applies go test's own rule for a name it will run: Test followed
// by nothing, or by a rune that is not lower-case, taking one *testing.T.
func isTestFunc(fn *ast.FuncDecl) bool {
	name, ok := strings.CutPrefix(fn.Name.Name, "Test")
	if !ok {
		return false
	}
	if runes := []rune(name); len(runes) > 0 && (runes[0] >= 'a' && runes[0] <= 'z') {
		return false
	}
	params := fn.Type.Params.List
	if len(params) != 1 {
		return false
	}
	star, ok := params[0].Type.(*ast.StarExpr)
	if !ok {
		return false
	}
	selector, ok := star.X.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := selector.X.(*ast.Ident)
	return ok && pkg.Name == "testing" && selector.Sel.Name == "T"
}

// readWorkflowRecipes is every recipe the workflows in dir invoke `just` with,
// deduplicated. Read from every workflow file rather than from the live one by
// name: a live recipe wired into a second workflow is reached by CI too, and a
// list of filenames here is a list a workflow can be added without.
func readWorkflowRecipes(dir string) ([]string, []string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil, err
	}
	seen := make(map[string]bool)
	read := 0
	for _, entry := range entries {
		if entry.IsDir() || (!strings.HasSuffix(entry.Name(), ".yml") && !strings.HasSuffix(entry.Name(), ".yaml")) {
			continue
		}
		src, err := os.ReadFile(filepath.Join(dir, entry.Name())) //nolint:gosec // a path read out of the caller's own repo root
		if err != nil {
			return nil, nil, err
		}
		names, err := WorkflowRecipes(src)
		if err != nil {
			return nil, nil, fmt.Errorf("%s: %w", entry.Name(), err)
		}
		read++
		for _, name := range names {
			seen[name] = true
		}
	}
	var complaints []string
	if read == 0 {
		complaints = append(complaints, fmt.Sprintf(
			"no workflow file was read from %s, so nothing here knows which recipes CI runs", dir))
	}
	return slices.Sorted(maps.Keys(seen)), complaints, nil
}

// WorkflowRecipes is every recipe a workflow's steps invoke `just` with. Only
// `run:` is read: a recipe reached through a composite action or a reusable
// workflow is not found here, and falls through to the stricter Local rule.
func WorkflowRecipes(src []byte) ([]string, error) {
	var doc struct {
		Jobs map[string]struct {
			Steps []struct {
				Run string `yaml:"run"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(src, &doc); err != nil {
		return nil, err
	}
	var names []string
	for _, job := range doc.Jobs {
		for _, step := range job.Steps {
			names = append(names, JustRecipes(step.Run)...)
		}
	}
	return names, nil
}
