package main

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// A discovery probe is a go.mod with no Go file beneath it — the one module
// shape goDirs refuses (see its comment). Three justfile recipes make one on
// purpose, to witness that the module set is read off the tree rather than
// remembered, and each removes its own on the way out under an EXIT trap. A
// trap does not run under SIGKILL, so a run killed by a watchdog or a quota
// leaves the probe behind, and the next run of any gate that walks the tree
// dies on goDirs' refusal instead of doing its job (bd gqlc-c7o7).
//
// `sweep-discovery-probes` is what clears that residue. It is wired in as a
// dependency of the four recipes that run this program, and until this file
// those four edges were a hand-maintained fact held to nothing: deleting one
// leaves the tree green and silent until someone leaks a probe, at which point
// the gate that lost its edge fails with a message about a walk (measured —
// dropping `check-golangci-build-tags: sweep-discovery-probes` and running the
// recipe on a clean tree exits 0 with no output).
//
// So the edges are derived here rather than trusted: a recipe whose body
// spells this program's package path has to reach the sweep first. Two limits
// on that reading are stated where they arise: an invocation that never spells
// the path, at modscopePkg below (bd gqlc-wkio), and the header shapes this
// file's reader still reads differently from just, at
// TestParseJustfileReadsWhatJustReads below.

const (
	// repoRoot reaches the justfile from this package's directory.
	repoRoot = "../../.."

	// justfilePath holds the recipes CI runs.
	justfilePath = "justfile"

	// probeSweep clears the probes a killed run left under test/data.
	probeSweep = "sweep-discovery-probes"

	// modscopePkg is what a recipe spells to run this program through the go
	// command. `go run`, `go build` and `go test` all take the package path,
	// so matching the path covers the three of them where matching one
	// command's spelling covers one.
	//
	// WHAT THIS DOES NOT REACH: an invocation that never spells the path — a
	// binary built once under another name and called by that name, or a path
	// assembled from a justfile variable and interpolated at the site. Not
	// reachable on this tree, where the four sites are all
	// `go run ./internal/tools/modscope`; recorded as a limit in bd gqlc-wkio,
	// the same shape gqlc-lj9s records for the probe sites.
	modscopePkg = "internal/tools/modscope"
)

// justRecipe is one recipe as the justfile spells it: the name, the
// dependencies just runs before the body, and the body.
type justRecipe struct {
	name string
	deps []string
	body string
}

// parseJustfile reads the recipes out of justfile source.
//
// A recipe header is an unindented line whose first character can start a name
// and whose first ':' is not the ':=' of an assignment; the body is the run of
// lines after it that are indented or empty. Empty lines are included because
// that is just's own rule — several recipes here separate blocks with one, and
// a reader that stopped at the first blank line would read the sweep recipe as
// four lines long and see none of what it does.
//
// Comments in a body are kept rather than stripped, so a commented-out
// invocation counts as an invocation. That is the stance
// `sweep-discovery-probes` takes on a commented-out mktemp site, for the same
// reason: it asks for a dependency edge the tree may not need, which is noise,
// where dropping it would hide a site.
//
// Dependencies written after `&&` are excluded: just runs those AFTER the body,
// so a sweep in that position would run after the walk it protects. What this
// reader will not read at all it refuses rather than guesses at — see the
// parenthesised-dependency complaint below.
func parseJustfile(src string) ([]justRecipe, []string) {
	var (
		out        []justRecipe
		complaints []string
	)
	lines := strings.Split(src, "\n")
	for i, line := range lines {
		name, rest, ok := justHeader(line)
		if !ok {
			continue
		}
		// A parenthesised dependency carries arguments, and this reader takes
		// the whole whitespace-separated token as a name. Refused rather than
		// mis-read: a dependency parsed as `(foo` matches no recipe, and the
		// closure it feeds would be short without saying so.
		if strings.ContainsAny(rest, "()") {
			complaints = append(complaints, fmt.Sprintf(
				"recipe %s has a parenthesised dependency, which this reader does not read, "+
					"so the dependency closure below is not the one just runs", name))
			continue
		}
		pre, _, _ := strings.Cut(rest, "&&")
		var body []string
		for _, next := range lines[i+1:] {
			if next != "" && !strings.HasPrefix(next, " ") && !strings.HasPrefix(next, "\t") {
				break
			}
			body = append(body, next)
		}
		out = append(out, justRecipe{
			name: name,
			deps: strings.Fields(pre),
			body: strings.Join(body, "\n"),
		})
	}
	return out, complaints
}

// justHeader splits a recipe header into its name and everything after the
// colon. It reports false for every other line: an indented line is a body
// line, a line opening with '#' is a doc comment, one opening with '[' is an
// attribute, and a line whose separating colon opens ':=' is an assignment —
// including `export NAME := ...` and `set shell := ...`, which otherwise look
// like a recipe with parameters.
func justHeader(line string) (name, rest string, ok bool) {
	if line == "" || line[0] == ' ' || line[0] == '\t' {
		return "", "", false
	}
	colon := headerColon(line)
	if colon < 0 || strings.HasPrefix(line[colon:], ":=") {
		return "", "", false
	}
	head := strings.Fields(line[:colon])
	if len(head) == 0 {
		return "", "", false
	}
	// `@name:` is a recipe whose body is not echoed. The '@' is not part of
	// the name a dependency spells.
	name = strings.TrimPrefix(head[0], "@")
	if !isJustName(name) {
		return "", "", false
	}
	return name, line[colon+1:], true
}

// headerColon returns the index of the colon separating a recipe header from
// its dependencies: the first colon that is not inside a parameter default's
// string literal. It returns -1 when there is no such colon on the line, which
// includes a line that opens a literal and does not close it.
//
// Taking the first colon in the line instead reads
// `vuln x="a:=b": vuln-root-residual` as an assignment and so does not read the
// recipe at all, while just runs it with both its dependency and its body —
// measured on a copy of this repo's justfile, where that rewrite leaves
// `just --dump` at rc=0 with vuln's body still naming this package three times
// and leaves TestEveryRecipeRunningModscopeSweepsProbesFirst passing, and where
// dropping the same edge under a header this reader does read fails it.
func headerColon(line string) int {
	for i := 0; i < len(line); {
		switch line[i] {
		case ':':
			return i
		case '"', '\'', '`':
			end := skipJustString(line, i)
			if end < 0 {
				return -1
			}
			i = end
		default:
			i++
		}
	}
	return -1
}

// skipJustString returns the index one past the string literal opening at
// line[i], or -1 if that literal is not closed on this line.
//
// just spells a parameter default six ways and takes a ':=' inside any of them,
// so all six have to be skipped or the miss stays live in whichever family is
// not skipped. Measured against just 1.57.0, every one of these parses to a
// recipe still carrying its dependency:
//
//	vuln x="a:=b": sweep
//	vuln x='a:=b': sweep
//	vuln x=`a:=b`: sweep
//	vuln x="""a:=b""": sweep
//	vuln x='''a:=b''': sweep
//	vuln x=```a:=b```: sweep
//
// Only the double-quote family takes a backslash escape, which is why escapes
// are keyed on the delimiter rather than applied throughout. These two are
// refused as unterminated strings:
//
//	vuln x="a\": sweep
//	vuln x="""a\""": sweep
//
// while these three are accepted with the backslash a literal character, so
// reading a backslashed delimiter as an escape in a raw family would run the
// literal off the end of the line and lose the recipe:
//
//	vuln x='a\': sweep
//	vuln x=`a\`: sweep
//	vuln x='''a\''': sweep
//
// The triple forms are checked before the single ones because they can hold a
// lone delimiter. just accepts this, where pairing single delimiters reads two
// literals with the ':=' between them rather than one literal containing it:
//
//	vuln x="""a"b:=c""": sweep
func skipJustString(line string, i int) int {
	delim := line[i : i+1]
	if triple := strings.Repeat(delim, 3); strings.HasPrefix(line[i:], triple) {
		delim = triple
	}
	escapes := line[i] == '"'
	for j := i + len(delim); j < len(line); {
		if escapes && line[j] == '\\' {
			j += 2
			continue
		}
		if strings.HasPrefix(line[j:], delim) {
			return j + len(delim)
		}
		j++
	}
	return -1
}

// isJustName reports whether s is spelled the way a recipe name is. Prose
// lines carrying a colon reach justHeader too, and this is what keeps them out.
func isJustName(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		alnum := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
		if alnum || c == '_' {
			continue
		}
		if (c == '-' || c == '.') && i > 0 {
			continue
		}
		return false
	}
	return true
}

// unsweptModscopeCallers returns the recipes that run this program without
// reaching probeSweep first, alongside complaints about the states in which
// that answer would be empty for a reason other than the tree being right.
//
// The complaints are the point. Every clause below compares two sets derived
// from the same source, and a reader that came back with nothing to compare
// would agree with any tree at all — which is the defect this program's own
// goDirs refuses one level down (bd gqlc-s3lt).
func unsweptModscopeCallers(recipes []justRecipe) (unswept, complaints []string) {
	if len(recipes) == 0 {
		complaints = append(complaints,
			"no recipe was read out of the justfile, so no recipe can be found running "+
				"modscope and this check passes over every tree")
		return nil, complaints
	}

	byName := make(map[string]justRecipe, len(recipes))
	for _, r := range recipes {
		if _, dup := byName[r.name]; dup {
			complaints = append(complaints, fmt.Sprintf(
				"recipe %s was read twice, so one of the two bodies is the only one this "+
					"check looks at", r.name))
		}
		byName[r.name] = r
	}

	// The reader's own control, taken off the artefact rather than off a list
	// kept by hand: just refuses a dependency that names no recipe, so every
	// dependency in a justfile it accepts resolves. One that does not resolve
	// here is a header this reader failed to recognise — and a header it
	// missed is a caller it cannot find.
	//
	// It reaches a missed header through the recipes that depend on it, so a
	// missed header nothing depends on leaves the answer set quietly shorter:
	// of the four callers on this tree, vuln and test-codegen-fence have no
	// dependents at all. That is the fail-open that
	// TestParseJustfileReadsWhatJustReads covers shape by shape below, and why
	// the pinned header rows there are not decoration for this control.
	for _, r := range recipes {
		for _, d := range r.deps {
			if _, found := byName[d]; !found {
				complaints = append(complaints, fmt.Sprintf(
					"recipe %s depends on %s, which is not among the recipes read, so this "+
						"reader missed a header and the callers it finds are a subset",
					r.name, d))
			}
		}
	}

	if _, found := byName[probeSweep]; !found {
		complaints = append(complaints, fmt.Sprintf(
			"%s is not a recipe in this justfile, so the dependency this check requires "+
				"cannot be declared and requiring it says nothing", probeSweep))
	}

	var callers []string
	for _, r := range recipes {
		if strings.Contains(r.body, modscopePkg) {
			callers = append(callers, r.name)
		}
	}
	if len(callers) == 0 {
		complaints = append(complaints, fmt.Sprintf(
			"no recipe body names %s, so either nothing runs this program or it is run "+
				"through a spelling this check does not read (bd gqlc-wkio) — either way "+
				"the sweep dependency is required of nothing", modscopePkg))
	}

	for _, name := range callers {
		if !reaches(byName, name, probeSweep) {
			unswept = append(unswept, name)
		}
	}
	slices.Sort(unswept)
	return unswept, complaints
}

// reaches reports whether want is in name's transitive dependency closure.
// Transitive rather than direct because just runs the whole closure before the
// body: test-codegen-fence would still be swept through
// check-codegen-external-tests if its own edge went away, and calling that
// unswept would be a false report.
//
// name itself is not in the closure. A recipe that both runs this program and
// IS the sweep would be reported unswept here; there is no such recipe, and the
// message it would print — reach the sweep through dependencies — is the wrong
// advice for that one case.
func reaches(byName map[string]justRecipe, name, want string) bool {
	seen := map[string]bool{name: true}
	queue := append([]string(nil), byName[name].deps...)
	for len(queue) > 0 {
		d := queue[0]
		queue = queue[1:]
		if d == want {
			return true
		}
		if seen[d] {
			continue
		}
		seen[d] = true
		queue = append(queue, byName[d].deps...)
	}
	return false
}

// TestEveryRecipeRunningModscopeSweepsProbesFirst is the live assertion, over
// the justfile CI runs.
func TestEveryRecipeRunningModscopeSweepsProbesFirst(t *testing.T) {
	src, err := os.ReadFile(filepath.Join(repoRoot, justfilePath))
	if err != nil {
		t.Fatalf("read the justfile: %v", err)
	}
	recipes, readComplaints := parseJustfile(string(src))
	unswept, complaints := unsweptModscopeCallers(recipes)
	for _, c := range append(readComplaints, complaints...) {
		t.Errorf("%s", c)
	}
	for _, name := range unswept {
		t.Errorf("recipe %s runs %s and does not reach %s through its dependencies, so a "+
			"discovery probe a killed run left under test/data stops it on goDirs' "+
			"empty-walk refusal — add %s to its dependency list (bd gqlc-c7o7)",
			name, modscopePkg, probeSweep, probeSweep)
	}
}

// TestUnsweptModscopeCallersFindsEachBrokenWiring cuts the wiring one way at a
// time against justfile source written here, so every clause above is shown to
// fail rather than assumed to. Without these rows the live assertion is one
// nothing can falsify: it passes on a tree where the reader read nothing.
func TestUnsweptModscopeCallersFindsEachBrokenWiring(t *testing.T) {
	const sweep = probeSweep + ":\n    #!/usr/bin/env bash\n    rm -rf test/data/*probe.*\n"
	const caller = "vuln: " + probeSweep + "\n    #!/usr/bin/env bash\n" +
		"    go run ./" + modscopePkg + " modules\n"

	cases := []struct {
		name       string
		src        string
		unswept    []string
		complaints []string
	}{
		{
			name: "a caller that reaches the sweep is not reported",
			src:  sweep + "\n" + caller,
		},
		{
			name: "a caller that reaches the sweep through another recipe is not reported",
			src: sweep + "\n" + "check-codegen-external-tests: " + probeSweep + "\n    echo hi\n" +
				"\ntest-codegen-fence: check-codegen-external-tests\n    #!/usr/bin/env bash\n" +
				"    go run ./" + modscopePkg + " modules\n",
		},
		{
			name:    "a caller whose dependency edge was deleted",
			src:     sweep + "\nvuln:\n    #!/usr/bin/env bash\n    go run ./" + modscopePkg + " modules\n",
			unswept: []string{"vuln"},
		},
		{
			name: "a caller swept only after its own body",
			src: sweep + "\nvuln: && " + probeSweep + "\n    #!/usr/bin/env bash\n" +
				"    go run ./" + modscopePkg + " modules\n",
			unswept: []string{"vuln"},
		},
		{
			name:       "no recipe at all",
			src:        "# just a comment\n",
			complaints: []string{"no recipe was read out of the justfile"},
		},
		{
			// The edge is still declared, so the caller is not what is wrong
			// here and is not reported. Two complaints carry it instead: just
			// itself refuses a justfile whose dependency names no recipe, so
			// this state is one a reader reaches before just does.
			name: "the sweep recipe is gone",
			src:  caller,
			complaints: []string{
				"depends on " + probeSweep + ", which is not among the recipes read",
				probeSweep + " is not a recipe in this justfile",
			},
		},
		{
			name:       "nothing runs modscope",
			src:        sweep + "\nlint:\n    echo hi\n",
			complaints: []string{"no recipe body names " + modscopePkg},
		},
		{
			name: "a dependency naming no recipe the reader found",
			src:  sweep + "\nvuln: " + probeSweep + " ensure-golangci\n    echo hi\n",
			complaints: []string{
				"depends on ensure-golangci, which is not among the recipes read",
				"no recipe body names " + modscopePkg,
			},
		},
		{
			name: "one recipe name carrying two bodies",
			src:  sweep + "\n" + caller + "\nvuln: " + probeSweep + "\n    echo hi\n",
			complaints: []string{
				"recipe vuln was read twice",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			recipes, readComplaints := parseJustfile(tc.src)
			unswept, complaints := unsweptModscopeCallers(recipes)
			complaints = append(readComplaints, complaints...)
			if !slices.Equal(unswept, tc.unswept) {
				t.Errorf("unswept = %v, want %v", unswept, tc.unswept)
			}
			if len(complaints) != len(tc.complaints) {
				t.Fatalf("complaints = %v, want %d matching %v", complaints, len(tc.complaints), tc.complaints)
			}
			for i, want := range tc.complaints {
				if !strings.Contains(complaints[i], want) {
					t.Errorf("complaint %d = %q, want it to contain %q", i, complaints[i], want)
				}
			}
		})
	}
}

// TestParseJustfileReadsWhatJustReads holds the reader to the shapes this
// justfile is written in. A header it does not recognise is a caller it cannot
// find, and a body it truncates is an invocation it does not see — both fail
// open, which is why each shape has a row.
//
// The parameter-default rows are where a reader of header text loses most,
// because a default is the one part of a header carrying bytes just does not
// read as syntax. just 1.57.0 accepts six spellings of one, listed at
// skipJustString above, and takes a ':=' inside every one of them; a reader
// that skips one family and not the other keeps the miss alive in the family
// it does not skip, so every family has a row here. The escaping rows are the
// same argument one level down — the double-quote family escapes and the raw
// families do not, so a single rule applied to both mis-reads one of them.
//
// WHAT THIS STILL DOES NOT REACH. Three header shapes are known to be read
// differently by just 1.57.0 and by this reader, and the useful thing about
// them is which way each one fails:
//
//   - A header whose parameter default opens a literal it does not close on
//     the same line. just accepts one, because a triple-quoted default may
//     span lines, and this reader is line-based, so it drops the recipe. This
//     is the only one of the three that fails open — a caller lost this way is
//     not reported and nothing else here notices — which is why the last row
//     below pins the drop, so that it stays a choice.
//   - A trailing `# …` comment on a header, which just ignores and this reader
//     takes for dependency names. Fails closed: the invented names resolve to
//     no recipe, so the dangling-dependency complaint fires.
//     TestParseJustfileMisreadsATrailingCommentLoudly below holds it there.
//   - A line inside a multi-line string assignment that is spelled like a
//     header, which this reader reads as a recipe just does not have. It adds
//     rather than hides — nothing it invents conceals a caller the reader
//     would otherwise find — but what it adds is not harmless. A phantom
//     sharing a real recipe's name raises the read-twice complaint, an
//     invented dependency raises the dangling-dependency one, and a phantom
//     whose indented lines spell the package path is reported as an unswept
//     caller that does not exist. Measured on a fixture just reads as two
//     recipes and this reader as three, where the third is reported by name.
//     Neither triple-quote form appears in this repo's justfile today, so this
//     is prospective; the row below is what would notice it changing.
func TestParseJustfileReadsWhatJustReads(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want []justRecipe
	}{
		{
			name: "a doc comment and an attribute are not headers",
			src:  "# doc: a thing\n[private]\nvuln: sweep\n    echo hi\n",
			want: []justRecipe{{name: "vuln", deps: []string{"sweep"}, body: "    echo hi\n"}},
		},
		{
			name: "an assignment is not a header",
			src:  "probe := \"vulnprobe\"\nexport CACHE := \"x\"\nset shell := [\"bash\"]\nvuln:\n    echo hi\n",
			want: []justRecipe{{name: "vuln", body: "    echo hi\n"}},
		},
		{
			name: "a blank line inside a body does not end it",
			src:  "vuln:\n    echo one\n\n    echo two\nlint:\n    echo hi\n",
			want: []justRecipe{
				{name: "vuln", body: "    echo one\n\n    echo two"},
				{name: "lint", body: "    echo hi\n"},
			},
		},
		{
			name: "a parameter is not a dependency",
			src:  "bd-export-monotonic base: check-hooks\n    echo hi\n",
			want: []justRecipe{{name: "bd-export-monotonic", deps: []string{"check-hooks"}, body: "    echo hi\n"}},
		},
		{
			name: "a quiet recipe is named without its @",
			src:  "@vuln: sweep\n    echo hi\n",
			want: []justRecipe{{name: "vuln", deps: []string{"sweep"}, body: "    echo hi\n"}},
		},
		{
			name: "a dependency after && is not one that runs first",
			src:  "vuln: sweep && report\n    echo hi\n",
			want: []justRecipe{{name: "vuln", deps: []string{"sweep"}, body: "    echo hi\n"}},
		},
		{
			name: "a comment in a body is part of it",
			src:  "vuln:\n    # go run ./" + modscopePkg + "\n",
			want: []justRecipe{{name: "vuln", body: "    # go run ./" + modscopePkg + "\n"}},
		},
		{
			name: "a parameter carrying a default is not a dependency",
			src:  "lint-hooks dir=\".githooks\": ensure-shellcheck\n    echo hi\n",
			want: []justRecipe{{name: "lint-hooks", deps: []string{"ensure-shellcheck"}, body: "    echo hi\n"}},
		},
		{
			name: "a parameter default spelling := is not an assignment",
			src:  "vuln x=\"a:=b\": sweep\n    echo ./" + modscopePkg + "\n",
			want: []justRecipe{{name: "vuln", deps: []string{"sweep"}, body: "    echo ./" + modscopePkg + "\n"}},
		},
		{
			name: "a := in a raw default is not an assignment either",
			src:  "vuln x='a:=b': sweep\n    echo hi\n",
			want: []justRecipe{{name: "vuln", deps: []string{"sweep"}, body: "    echo hi\n"}},
		},
		{
			name: "a := in a backtick default is not an assignment either",
			src:  "vuln x=`echo a:=b`: sweep\n    echo hi\n",
			want: []justRecipe{{name: "vuln", deps: []string{"sweep"}, body: "    echo hi\n"}},
		},
		{
			// just accepts a lone delimiter inside the triple forms, so these
			// two are what a reader pairing single delimiters gets wrong: it
			// would leave `b:=c` outside any literal and read an assignment.
			name: "a triple-quoted default holding a lone quote",
			src:  "vuln x=\"\"\"a\"b:=c\"\"\": sweep\n    echo hi\n",
			want: []justRecipe{{name: "vuln", deps: []string{"sweep"}, body: "    echo hi\n"}},
		},
		{
			name: "a triple-raw default holding a lone quote",
			src:  "vuln x='''a'b:=c''': sweep\n    echo hi\n",
			want: []justRecipe{{name: "vuln", deps: []string{"sweep"}, body: "    echo hi\n"}},
		},
		{
			name: "a triple-backtick default",
			src:  "vuln x=```a:=b```: sweep\n    echo hi\n",
			want: []justRecipe{{name: "vuln", deps: []string{"sweep"}, body: "    echo hi\n"}},
		},
		{
			// The double-quote family escapes, so the default runs to the
			// quote after c and the separator is the colon after it.
			name: "an escaped quote does not end a default",
			src:  "vuln x=\"a\\\"b:=c\": sweep\n    echo hi\n",
			want: []justRecipe{{name: "vuln", deps: []string{"sweep"}, body: "    echo hi\n"}},
		},
		{
			// The raw families do not escape, so this default ends at the
			// second quote with the backslash inside it. Reading `\'` as an
			// escape would run the literal off the end of the line and drop
			// the recipe.
			name: "a backslash in a raw default is a character, not an escape",
			src:  "vuln x='a\\': sweep\n    echo hi\n",
			want: []justRecipe{{name: "vuln", deps: []string{"sweep"}, body: "    echo hi\n"}},
		},
		{
			// A colon that is not part of := was read as the separator before,
			// which made the rest of the default into dependencies naming no
			// recipe. The parens are the same shape one step on: they sit in
			// the name half of the line, so they are not the parenthesised
			// dependency the reader refuses.
			name: "a plain colon and parens in a default are not dependencies",
			src:  "vuln x=(\"a\" + \":b\"): sweep\n    echo hi\n",
			want: []justRecipe{{name: "vuln", deps: []string{"sweep"}, body: "    echo hi\n"}},
		},
		{
			name: "an import path carrying a colon is not a recipe",
			src:  "import 'mod:ules.just'\nvuln: sweep\n    echo hi\n",
			want: []justRecipe{{name: "vuln", deps: []string{"sweep"}, body: "    echo hi\n"}},
		},
		{
			// A LIMIT THIS READER STILL HAS, and the one it answers by
			// inventing rather than dropping: the assignment's value is a
			// multi-line string, so just reads no recipe here at all.
			name: "a header spelled inside a string assignment is read anyway",
			src:  "x := \"\"\"\nhello: world\n\"\"\"\nvuln: sweep\n    echo hi\n",
			want: []justRecipe{
				{name: "hello", deps: []string{"world"}},
				{name: "vuln", deps: []string{"sweep"}, body: "    echo hi\n"},
			},
		},
		{
			// A LIMIT THIS READER STILL HAS. just accepts a triple-quoted
			// default spanning lines, and this reader is line-based: neither
			// physical line carries a separating colon outside a literal, so
			// the recipe is dropped rather than mis-read. Dropped is the
			// fail-open direction — a caller lost this way is not reported —
			// and this row is here so that the drop stays a choice. Measured:
			// just 1.57.0 reads this source at rc=0 as vuln depending on sweep.
			//
			// The default carries a colon on its first physical line on
			// purpose. That is what separates dropping the line from resuming
			// after an unclosed delimiter: a reader that stepped over the
			// opening quote instead would re-pair the remaining quotes, find
			// the colon in `a:b`, and read a recipe named vuln that depends on
			// something called b.
			name: "a default spanning two lines leaves the header unread",
			src:  "vuln x=\"\"\"a:b\nc:=d\"\"\": sweep\n    echo ./" + modscopePkg + "\n",
			want: nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, complaints := parseJustfile(tc.src)
			if len(complaints) != 0 {
				t.Fatalf("complaints = %v, want none", complaints)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("read %d recipes %v, want %d %v", len(got), got, len(tc.want), tc.want)
			}
			for i, w := range tc.want {
				if got[i].name != w.name {
					t.Errorf("recipe %d name = %q, want %q", i, got[i].name, w.name)
				}
				if !slices.Equal(got[i].deps, w.deps) {
					t.Errorf("recipe %s deps = %v, want %v", w.name, got[i].deps, w.deps)
				}
				if got[i].body != w.body {
					t.Errorf("recipe %s body = %q, want %q", w.name, got[i].body, w.body)
				}
			}
		})
	}
}

// TestParseJustfileRefusesADependencyShapeItCannotRead is separate from the
// rows above because it is the one shape the reader answers with a complaint
// rather than a reading. just accepts `recipe: (dep arg)`; this reader would
// take `(dep` for a name, so it says so instead.
func TestParseJustfileRefusesADependencyShapeItCannotRead(t *testing.T) {
	got, complaints := parseJustfile("vuln: (sweep \"arg\")\n    echo hi\n")
	if len(got) != 0 {
		t.Errorf("read %v, want the recipe left unread", got)
	}
	if len(complaints) != 1 || !strings.Contains(complaints[0], "parenthesised dependency") {
		t.Fatalf("complaints = %v, want one naming a parenthesised dependency", complaints)
	}
}

// TestParseJustfileMisreadsATrailingCommentLoudly pins the one shape this
// reader is known to read differently from just and does not refuse. just takes
// a `# …` after a dependency list as a comment and ignores it — measured, `just
// --dump` on `vuln: sweep-discovery-probes # note` is rc=0 with vuln depending
// on the sweep alone — where this reader takes the words after the '#' for
// dependency names.
//
// Nothing wants that reading. It is pinned because the direction is what makes
// it survivable: names invented from a comment resolve to no recipe, so the
// dangling-dependency complaint fires and the live assertion fails rather than
// passing over a header it read wrong. Teaching the reader to read past a '#'
// silently would move this to the side where a miss is quiet, and this row is
// what would notice.
func TestParseJustfileMisreadsATrailingCommentLoudly(t *testing.T) {
	src := probeSweep + ":\n    echo s\n" +
		"vuln: " + probeSweep + " # note\n    go run ./" + modscopePkg + "\n"

	recipes, readComplaints := parseJustfile(src)
	if len(readComplaints) != 0 {
		t.Fatalf("read complaints = %v, want none", readComplaints)
	}
	var deps []string
	for _, r := range recipes {
		if r.name == "vuln" {
			deps = r.deps
		}
	}
	if !slices.Equal(deps, []string{probeSweep, "#", "note"}) {
		t.Fatalf("vuln deps = %v, want the comment read as two more dependencies", deps)
	}

	// The caller is still found and still reaches the sweep, so what the
	// comment costs is noise rather than a missed caller.
	unswept, complaints := unsweptModscopeCallers(recipes)
	if len(unswept) != 0 {
		t.Errorf("unswept = %v, want none", unswept)
	}
	if len(complaints) != 2 {
		t.Fatalf("complaints = %v, want one for each word the comment contributed", complaints)
	}
	for i, want := range []string{"depends on #", "depends on note"} {
		if !strings.Contains(complaints[i], want) {
			t.Errorf("complaint %d = %q, want it to contain %q", i, complaints[i], want)
		}
	}
}
