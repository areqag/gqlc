package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"os/exec"
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
// file's reader is known to read differently from just, at
// TestParseJustfileReadsWhatJustReads below. That second list is what has been
// looked for and not a boundary anyone has proved — it gained a shape under
// review while this file was being written — so
// TestParseJustfileAgreesWithJustOnThisJustfile puts the reader's reading of
// the real justfile beside just's own and reports where they part, which is a
// question the list does not have to have anticipated.

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
// A recipe header is an unindented line — taken together with the lines a
// trailing backslash continues it onto — whose separating colon is the first
// one lying outside a parameter default's string literal and does not open
// ':=', and whose first word, less any leading '@', is spelled the way a recipe
// name is. The ':=' clause is about that separating colon and not about the
// first colon on the line: `vuln x="a:=b": sweep` opens ':=' at its first colon
// and is read here as the recipe vuln depending on sweep, which is what just
// runs it as. The body is the run of lines after the header that are indented
// or empty. Empty lines are included because
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
	for i := 0; i < len(lines); i++ {
		line, end := joinContinuedHeader(lines, i)
		// The physical lines a header was spelled across are one line to just,
		// so none of them is examined again as a header of its own. Examining
		// them would read `vuln \` over `lint: sweep` as two recipes where just
		// has one recipe vuln taking a parameter named lint.
		i = end
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
		for _, next := range lines[end+1:] {
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

// joinContinuedHeader returns the one line just reads a header as, together
// with the index of the last physical line that went into it. A line ending in
// a lone backslash is continued on the next one, so a header can be spelled
// across several lines and none of them carries the whole thing.
//
// Reading only the physical line loses the recipe without saying anything: no
// literal is left open, so headerColon simply finds no colon and returns -1,
// and the continuation is indented so it is no header either. Measured on this
// repo's own justfile, rewriting `vuln: sweep-discovery-probes
// vuln-root-residual` to `vuln x="a" \` over `  : vuln-root-residual` leaves
// just reading 29 recipes with vuln's sweep edge gone, this reader reading 28
// with vuln absent, and every test here passing — the same silent outcome as the
// `x="a:=b"` header that headerColon was written for. The idiom is live: this
// justfile spells 16 continuations today, all of them in bodies.
//
// The join puts a space where the backslash was, because that is what just does
// with it. `vu\` over `ln: sweep` is a recipe named vu taking a parameter ln,
// not a recipe named vuln, and `vuln: swe\` over `  ep` asks for a dependency
// swe rather than sweep — measured on just 1.55.1, the version CI pins, and on
// 1.57.0.
//
// A comment is not continued. `# note \` above `vuln: sweep` leaves vuln a
// recipe just runs at rc=0, so joining a line that opens with '#' would swallow
// the header under it and drop that recipe silently — the fail-open direction
// this whole file exists to close, introduced by the repair for another one.
//
// Two spellings are left to just to reject rather than handled here, both
// measured at rc=1 on 1.55.1 and 1.57.0: a backslash followed by a space, and a
// line ending in two backslashes. just calls each an invalid escape sequence, so
// a justfile carrying one runs nothing at all.
func joinContinuedHeader(lines []string, i int) (string, int) {
	line := lines[i]
	if line == "" || line[0] == ' ' || line[0] == '\t' || line[0] == '#' {
		return line, i
	}
	end := i
	for strings.HasSuffix(line, `\`) && end+1 < len(lines) {
		end++
		head := strings.TrimSuffix(line, `\`)
		next := strings.TrimLeft(lines[end], " \t")
		// A line holding nothing but the backslash contributes no text, and
		// joining it would put a space in front of what follows and so move it
		// out of header position. just reads the header under such a line.
		if strings.TrimSpace(head) == "" {
			line = next
			continue
		}
		line = head + " " + next
	}
	return line, end
}

// headerColon returns the index of the colon separating a recipe header from
// its dependencies: the first colon that is not inside a parameter default's
// string literal. It returns -1 when there is no such colon on the line, which
// includes a line that opens a literal and does not close it.
//
// Which colon is taken decides whether the recipe is read at all, not just where
// it is split. A header can carry a second colon after the separator — in a
// trailing comment, most often a URL — and taking the last one instead puts the
// separator inside the name half, where `vuln:` is not a name a recipe can have
// and the whole recipe is dropped without a word.
// TestHeaderColonTakesTheFirstSeparatingColon pins that.
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

// TestParseJustfileAgreesWithJustOnThisJustfile asks just what this repo's
// justfile declares and compares that with what the reader above finds in the
// same bytes: the recipe names, and for each name the dependencies just runs
// before the body. Those two are what unsweptModscopeCallers walks to decide
// whether a caller reaches the sweep. Which recipes are callers at all it
// decides from bodies, and bodies are not compared here — see below.
//
// Every row in TestParseJustfileReadsWhatJustReads is a shape somebody here
// thought of, and a reader held only to those rows is as good as that list and
// no better. This test is not keyed to the list: it compares the two readings
// of whatever the file says today, so a header spelling no row here covers is
// still compared, and one that costs a recipe or a dependency edge is named.
//
// That is not a hypothetical benefit. Measured on the commit before
// joinContinuedHeader existed, with a backslash-continued header written into
// this justfile in place of vuln's: the reader lost vuln, this comparison named
// vuln, and every other test in the package passed. The shape had been live in
// the reader for as long as the reader had existed, and it was found by
// inventing a case rather than by working through just's grammar. The count of
// shapes this file records itself getting wrong was three until the review that
// found that fourth one, which is the argument for not treating the list as
// finished.
//
// WHAT THIS DOES NOT REACH:
//
//   - Bodies. just's dump spells a body as parsed fragments and this reader
//     keeps raw text, so they are not compared. A body this reader truncates
//     could hold a `go run ./internal/tools/modscope` line it never sees, and
//     nothing here would say so.
//   - Shapes this justfile does not contain. It reports on these bytes, and
//     says nothing about a header spelling no file here has yet.
//   - A divergence both readings share. just is the reference, so a recipe just
//     itself reads differently from the way it runs it is outside this.
//
// `just` is required rather than skipped over, for the reason
// internal/tools/ciguard/hooktests_test.go gives for the same choice: the CI
// test job installs it immediately before running the tests through it, so an
// absent just is a broken environment, and a skip here would be the fail-open
// this file exists to close.
//
// Both readings are asserted non-empty before they are compared. Two empty sets
// agree, and an agreement reached that way would pass over any justfile at all
// — the shape unsweptModscopeCallers refuses one level down.
func TestParseJustfileAgreesWithJustOnThisJustfile(t *testing.T) {
	dumped := dumpJustfile(t, repoRoot, justfilePath)

	src, err := os.ReadFile(filepath.Join(repoRoot, justfilePath))
	if err != nil {
		t.Fatalf("read the %s: %v", justfilePath, err)
	}
	read, readComplaints := parseJustfile(string(src))
	for _, c := range readComplaints {
		t.Errorf("%s", c)
	}

	declared, err := priorDependencies(dumped)
	if err != nil {
		t.Fatalf("read the dependencies out of `just --dump`: %v", err)
	}

	bodies := recipeBodies(dumped)

	// Asserted of just's reading specifically, and as a second witness rather
	// than the only one. Measured: make recipeBodies keep no text at all and
	// the mention clause below already reports it, because the recipes this
	// reader reads as naming modscopePkg then have no counterpart on just's
	// side — that mutation stays RED with this guard removed. What the guard
	// adds is that the catch does not rest on the clause below keeping both
	// of its directions; drop both and this line is what still fires.
	//
	// unsweptModscopeCallers refuses a run in which no body IT read names
	// modscopePkg. That is the other source, and one source going quiet is
	// not the other one agreeing.
	naming := 0
	for _, body := range bodies {
		if strings.Contains(body, modscopePkg) {
			naming++
		}
	}
	if naming == 0 {
		t.Errorf("no body in `just --dump` names %s, so comparing which bodies name it "+
			"compares nothing on just's side of this reading", modscopePkg)
	}

	for _, d := range justfileDisagreements(declared, bodies, read) {
		t.Errorf("%s", d)
	}
}

// dumpJustfile asks just what a justfile declares. dir is the directory just
// runs in and path is the justfile relative to it.
//
// An absent just fails rather than skips: every caller here asks just what a
// justfile declares rather than assuming it, so with just missing there is no
// reduced question left to answer. CI installs just immediately before running
// these tests through it.
func dumpJustfile(t *testing.T, dir, path string) justDump {
	t.Helper()
	justBin, err := exec.LookPath("just")
	if err != nil {
		t.Fatalf("`just` is not on PATH, and this test asks just what the %s declares "+
			"rather than assuming it, so this is a broken environment and not a case to "+
			"skip past: %v", path, err)
	}

	// just documents the JSON dump as unstable, so the flag is passed. Measured
	// on the version CI pins and on the newer one this was written against:
	// both accept the flag, and each reports the same recipes with it as
	// without, so passing it is not what decides the answer.
	cmd := exec.CommandContext(t.Context(), justBin,
		"--justfile", path, "--unstable", "--dump", "--dump-format", "json")
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("`just --dump` on the %s: %v\n%s", path, err, stderr.String())
	}

	var dumped justDump
	if err := json.Unmarshal(out, &dumped); err != nil {
		t.Fatalf("read `just --dump --dump-format json`: %v", err)
	}
	return dumped
}

// justDump is the part of `just --dump --dump-format json` this file reads.
type justDump struct {
	Recipes map[string]struct {
		// Priors counts the dependencies just runs before the body. The rest
		// are the ones written after `&&`, which parseJustfile drops on
		// purpose: a sweep in that position runs after the walk it is there to
		// protect. TestJustDumpCountsPriorsSeparatelyFromLaterDependencies
		// asks just itself whether that is what the field means.
		Priors       int `json:"priors"`
		Dependencies []struct {
			Recipe string `json:"recipe"`
		} `json:"dependencies"`

		// Body is one entry per body line, each a list of fragments. A
		// fragment is a string, or a nested list for an interpolation —
		// `go run …@{{version}}` dumps as ["go run …@", [["variable",
		// "version"]]]. recipeBodies keeps the strings and drops the rest.
		Body [][]any `json:"body"`
	} `json:"recipes"`
}

// priorDependencies reduces a dump to the dependencies just runs before each
// recipe's body, which is the reading parseJustfile is compared against.
//
// It is a function rather than a few lines in the test above because the
// truncation at Priors is what makes the two readings agree about `&&`, and the
// justfile in this repo does not exercise it: measured on the 29 recipes at this
// commit, every one has Priors equal to its whole dependency list, so a run that
// ignored Priors would read the same answer.
func priorDependencies(dumped justDump) (map[string][]string, error) {
	declared := make(map[string][]string, len(dumped.Recipes))
	for name, r := range dumped.Recipes {
		if r.Priors > len(r.Dependencies) || r.Priors < 0 {
			return nil, fmt.Errorf(
				"just reports recipe %s with %d dependencies running before its body out of "+
					"%d it lists, which this reading cannot make sense of", name, r.Priors, len(r.Dependencies))
		}
		deps := make([]string, 0, r.Priors)
		for _, d := range r.Dependencies[:r.Priors] {
			deps = append(deps, d.Recipe)
		}
		declared[name] = deps
	}
	return declared, nil
}

// recipeBodies reduces a dump to the literal text of each recipe's body.
//
// It is the literal text and not the whole body: an interpolation dumps as a
// nested expression rather than as the `{{…}}` the file spells, and this reader
// keeps that spelling, so comparing the two whole would report every recipe
// using a variable. What the comparison asks of a body is whether it spells
// modscopePkg, and an interpolated path is a limit already recorded at
// modscopePkg (bd gqlc-wkio) rather than one this reduction introduces.
//
// The newline join reconstructs the body rather than a run-together of it, and
// on this repo's justfile the mention does not rest on it: every recipe whose
// body names modscopePkg names it inside a single body line, and no adjacent
// pair of lines runs together into a new one, so joining with "" selects the
// same four recipes. That is a fact about this justfile, not about the join.
//
// Two more measurements on this repo's justfile at just 1.57.0, which bear on a
// body comparison in opposite ways. The first is a further difference: the dump
// drops each body line's leading indentation, which this reader keeps, and that
// alone makes the text of all 29 recipes here differ, none of them having an
// empty body. The second is a sameness worth stating because a comment is a
// place a mention could hide — a commented-out `go run ./internal/tools/modscope`
// is a mention — and both sides keep those lines: the dump carries the 469 body
// lines opening with '#' and the '#!' of the 11 shebang recipes, as does this
// reader.
func recipeBodies(dumped justDump) map[string]string {
	out := make(map[string]string, len(dumped.Recipes))
	for name, r := range dumped.Recipes {
		lines := make([]string, 0, len(r.Body))
		for _, line := range r.Body {
			var b strings.Builder
			for _, frag := range line {
				if s, ok := frag.(string); ok {
					b.WriteString(s)
				}
			}
			lines = append(lines, b.String())
		}
		out[name] = strings.Join(lines, "\n")
	}
	return out
}

// TestJustDumpCountsPriorsSeparatelyFromLaterDependencies asks just, rather than
// this file's reading of its documentation, what `priors` counts. Everything
// priorDependencies does rests on the answer, and the justfile in this repo
// cannot supply it: no recipe here writes a dependency after `&&`, so on real
// data Priors and the dependency list are the same length and the two readings
// of the field are indistinguishable. This one writes a justfile that tells them
// apart.
func TestJustDumpCountsPriorsSeparatelyFromLaterDependencies(t *testing.T) {
	dir := t.TempDir()
	src := "before:\n    @true\n\nafter:\n    @true\n\nvuln: before && after\n    @true\n"
	if err := os.WriteFile(filepath.Join(dir, "justfile"), []byte(src), 0o644); err != nil {
		t.Fatalf("write the fixture justfile: %v", err)
	}

	dumped := dumpJustfile(t, dir, "justfile")
	vuln, found := dumped.Recipes["vuln"]
	if !found {
		t.Fatalf("just read no recipe vuln out of %q", src)
	}
	if len(vuln.Dependencies) != 2 {
		t.Fatalf("just lists %d dependencies for vuln, want both of them", len(vuln.Dependencies))
	}
	if vuln.Priors != 1 {
		t.Fatalf("just reports priors=%d for `vuln: before && after`, and this file reads "+
			"that field as the count of dependencies running before the body, which is 1 "+
			"here. parseJustfile drops what runs after, so a different meaning makes the "+
			"comparison against it wrong in the fail-open direction", vuln.Priors)
	}
	if got := vuln.Dependencies[0].Recipe; got != "before" {
		t.Fatalf("just lists %s first among vuln's dependencies, and this file takes the "+
			"first Priors entries as the ones that run first, so the order is not "+
			"incidental", got)
	}

	declared, err := priorDependencies(dumped)
	if err != nil {
		t.Fatalf("priorDependencies over the fixture: %v", err)
	}
	if want := []string{"before"}; !slices.Equal(declared["vuln"], want) {
		t.Errorf("priorDependencies read %v for vuln, want %v", declared["vuln"], want)
	}
	read, complaints := parseJustfile(src)
	if len(complaints) != 0 {
		t.Fatalf("complaints = %v, want none", complaints)
	}
	if d := justfileDisagreements(declared, recipeBodies(dumped), read); len(d) != 0 {
		t.Errorf("the two readings part over `vuln: before && after`: %v", d)
	}
}

// TestPriorDependenciesReadsTheDumpJustWrites cuts the reduction one way at a
// time. The live dump above reaches only the shape this repo's justfile has, so
// the truncation, the field names and the refusal below are each unexercised
// there.
func TestPriorDependenciesReadsTheDumpJustWrites(t *testing.T) {
	cases := []struct {
		name    string
		json    string
		want    map[string][]string
		wantErr string
	}{
		{
			name: "only the dependencies running before the body are taken",
			json: `{"recipes":{"vuln":{"priors":1,"dependencies":[{"recipe":"before"},{"recipe":"after"}]}}}`,
			want: map[string][]string{"vuln": {"before"}},
		},
		{
			// The shape this repo's justfile has, and the only one the live run
			// reaches.
			name: "every dependency runs before the body",
			json: `{"recipes":{"vuln":{"priors":2,"dependencies":[{"recipe":"a"},{"recipe":"b"}]}}}`,
			want: map[string][]string{"vuln": {"a", "b"}},
		},
		{
			name: "a recipe with no dependencies reads as an empty list, not a missing one",
			json: `{"recipes":{"vuln":{"priors":0,"dependencies":[]}}}`,
			want: map[string][]string{"vuln": {}},
		},
		{
			// Order is the answer, not the set: just runs them in the order it
			// lists and justfileDisagreements compares the sequences.
			name: "the listed order is kept",
			json: `{"recipes":{"vuln":{"priors":2,"dependencies":[{"recipe":"b"},{"recipe":"a"}]}}}`,
			want: map[string][]string{"vuln": {"b", "a"}},
		},
		{
			// Not a shape just is known to emit. Refused rather than clamped,
			// because clamping would silently shorten the closure this file
			// walks, which is the fail-open direction.
			name:    "more priors than dependencies is refused rather than clamped",
			json:    `{"recipes":{"vuln":{"priors":3,"dependencies":[{"recipe":"a"}]}}}`,
			wantErr: "3 dependencies running before its body out of 1",
		},
		{
			name:    "a negative prior count is refused",
			json:    `{"recipes":{"vuln":{"priors":-1,"dependencies":[{"recipe":"a"}]}}}`,
			wantErr: "-1 dependencies running before its body",
		},
		{
			// A dump with none of the field names this file reads unmarshals
			// without error and yields zero values, so the field names are part
			// of what these rows pin.
			name: "a recipe just names but says nothing else about",
			json: `{"recipes":{"vuln":{}}}`,
			want: map[string][]string{"vuln": {}},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var dumped justDump
			if err := json.Unmarshal([]byte(tc.json), &dumped); err != nil {
				t.Fatalf("unmarshal the fixture dump: %v", err)
			}
			got, err := priorDependencies(dumped)
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("priorDependencies = %v, want an error mentioning %q", got, tc.wantErr)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Errorf("error = %q, want it to contain %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("priorDependencies: %v", err)
			}
			if !maps.EqualFunc(got, tc.want, slices.Equal) {
				t.Errorf("priorDependencies = %v, want %v", got, tc.want)
			}
		})
	}
}

// justfileDisagreements compares the recipes just declares with the recipes
// parseJustfile read out of the same source and returns a line for each way the
// two answers differ. Nothing returned is agreement.
//
// declared maps a recipe name to the dependencies just runs before its body,
// and declaredBodies maps it to the literal text of that body. Both readings
// are compared because the file asks two separate things of a recipe: which
// dependencies it reaches, and whether its body names modscopePkg. Comparing
// only the first leaves the second reading unchecked, and the second is the one
// unsweptModscopeCallers selects callers with — a reader that reads a header
// correctly and then reads a short body drops a caller, and a comparison of
// names and edges alone agrees with it.
//
// The comparison is split out from the test above so that
// TestJustfileDisagreementsFindsEachWayTheTwoReadingsPart can cut each way of
// disagreeing on its own: over the justfile in this repo the two readings agree,
// so every clause here is one the live run never exercises, and a clause nothing
// exercises is a clause nothing would notice the removal of.
//
// Either side reading nothing is itself a disagreement rather than agreement,
// and it is reported instead of the comparison rather than alongside it. Two
// empty readings match each other, so a comparison that called that agreement
// would pass over any justfile at all — the shape unsweptModscopeCallers refuses
// one level down — and with one side empty every name on the other is reported
// as unmatched, which buries the one fact that matters.
func justfileDisagreements(
	declared map[string][]string, declaredBodies map[string]string, read []justRecipe,
) []string {
	var out []string
	if len(declared) == 0 {
		out = append(out, "just declares no recipe in this justfile, so agreeing with it says nothing")
	}
	if len(read) == 0 {
		out = append(out, "this reader read no recipe out of this justfile, so agreeing with just says nothing")
	}
	if len(out) > 0 {
		return out
	}

	byReader := make(map[string]justRecipe, len(read))
	for _, r := range read {
		byReader[r.name] = r
	}

	names := make([]string, 0, len(declared))
	for name := range declared {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		got, found := byReader[name]
		if !found {
			out = append(out, fmt.Sprintf(
				"just declares recipe %s and this reader does not find it, so the body of %s "+
					"is outside every answer this file gives — including the sweep requirement, "+
					"which is asked only of recipes it read (bd gqlc-6n9y)", name, name))
			continue
		}
		if !slices.Equal(got.deps, declared[name]) {
			out = append(out, fmt.Sprintf(
				"just runs %v before recipe %s's body and this reader reads %v, so the closure "+
					"this file walks is not the one just runs", declared[name], name, got.deps))
		}

		// The mention, not the text: modscopePkg in a body is the whole of what
		// unsweptModscopeCallers reads a body for, and the two sides spell the
		// same body differently in ways recipeBodies records above.
		//
		// The witness that the 24 pinned bodies in
		// TestParseJustfileReadsWhatJustReads do not already cover this: cap
		// the reader's body at 10 lines and every one of those rows stays
		// green, because the longest body they pin is 3 lines. vuln's body in
		// this repo's justfile runs 632 lines and names modscopePkg at line
		// 12, so the cap drops vuln from the caller set — while the three
		// other recipes naming it do so at line 2, which keeps
		// unsweptModscopeCallers' "no recipe body names" complaint quiet.
		// With that cap and without this clause the suite reports ok.
		justNames := strings.Contains(declaredBodies[name], modscopePkg)
		readerNames := strings.Contains(got.body, modscopePkg)
		switch {
		case justNames && !readerNames:
			out = append(out, fmt.Sprintf(
				"just reads a body for recipe %s that names %s and this reader reads one that "+
					"does not, so %s is a caller this file does not count and the sweep "+
					"dependency is not asked of it (bd gqlc-6n9y)", name, modscopePkg, name))
		case readerNames && !justNames:
			out = append(out, fmt.Sprintf(
				"this reader reads a body for recipe %s that names %s and just reads one that "+
					"does not, so %s can be reported unswept over text just does not run",
				name, modscopePkg, name))
		}
	}

	invented := make([]string, 0, len(byReader))
	for name := range byReader {
		invented = append(invented, name)
	}
	slices.Sort(invented)
	for _, name := range invented {
		if _, found := declared[name]; !found {
			out = append(out, fmt.Sprintf(
				"this reader reads a recipe %s that just does not declare, so it can be "+
					"reported as an unswept caller that does not exist", name))
		}
	}
	return out
}

// TestJustfileDisagreementsFindsEachWayTheTwoReadingsPart cuts the comparison
// one way at a time, because the live run above cannot: the two readings agree
// over this repo's justfile, so on this tree every clause returns nothing and a
// deleted clause would still return nothing. These rows are what stands between
// that and a comparison that has quietly stopped comparing.
func TestJustfileDisagreementsFindsEachWayTheTwoReadingsPart(t *testing.T) {
	sweep := justRecipe{name: probeSweep}
	caller := justRecipe{name: "vuln", deps: []string{probeSweep}}

	// The same body as the two sides spell it: `just --dump` drops the leading
	// indentation this reader keeps. Both spellings name modscopePkg.
	const justCallerBody = "go run ./" + modscopePkg + " modules"
	const readCallerBody = "    go run ./" + modscopePkg + " modules\n"

	cases := []struct {
		name     string
		declared map[string][]string
		bodies   map[string]string
		read     []justRecipe
		want     []string
	}{
		{
			name:     "the two readings agree",
			declared: map[string][]string{probeSweep: {}, "vuln": {probeSweep}},
			read:     []justRecipe{sweep, caller},
		},
		{
			name:     "a recipe just declares that this reader did not find",
			declared: map[string][]string{probeSweep: {}, "vuln": {probeSweep}},
			read:     []justRecipe{sweep},
			want:     []string{"just declares recipe vuln and this reader does not find it"},
		},
		{
			name:     "a recipe this reader read that just does not declare",
			declared: map[string][]string{probeSweep: {}},
			read:     []justRecipe{sweep, caller},
			want:     []string{"this reader reads a recipe vuln that just does not declare"},
		},
		{
			name:     "a dependency edge this reader did not read",
			declared: map[string][]string{probeSweep: {}, "vuln": {probeSweep}},
			read:     []justRecipe{sweep, {name: "vuln"}},
			want:     []string{"just runs [" + probeSweep + "] before recipe vuln's body and this reader reads []"},
		},
		{
			name:     "a dependency edge this reader invented",
			declared: map[string][]string{probeSweep: {}, "vuln": {}},
			read:     []justRecipe{sweep, caller},
			want:     []string{"just runs [] before recipe vuln's body and this reader reads [" + probeSweep + "]"},
		},
		{
			// The edge is there on both sides and the order is not, which just
			// runs differently. Reported, because a comparison that sorted first
			// would agree here.
			name:     "the same edges in a different order",
			declared: map[string][]string{"vuln": {probeSweep, "lint"}},
			read:     []justRecipe{{name: "vuln", deps: []string{"lint", probeSweep}}},
			want:     []string{"before recipe vuln's body and this reader reads"},
		},
		{
			// The fail-open this clause is here for. The header is read, the
			// name matches, the edges match — every other clause is silent —
			// and the body that selects vuln as a caller was read short.
			name:     "a body just reads as naming the path and this reader reads short",
			declared: map[string][]string{probeSweep: {}, "vuln": {probeSweep}},
			bodies:   map[string]string{probeSweep: "", "vuln": justCallerBody},
			read:     []justRecipe{sweep, caller},
			want: []string{
				"just reads a body for recipe vuln that names " + modscopePkg +
					" and this reader reads one that does not",
			},
		},
		{
			// The other direction, which reports a caller unswept over text
			// just does not run.
			name:     "a body this reader reads as naming the path and just does not",
			declared: map[string][]string{probeSweep: {}, "vuln": {probeSweep}},
			bodies:   map[string]string{probeSweep: "", "vuln": "echo nothing here"},
			read: []justRecipe{
				sweep, {name: "vuln", deps: []string{probeSweep}, body: readCallerBody},
			},
			want: []string{
				"this reader reads a body for recipe vuln that names " + modscopePkg +
					" and just reads one that does not",
			},
		},
		{
			// Both name it, spelled differently. The clause compares the
			// mention and not the text, so this is agreement — comparing the
			// text would report every recipe in the repo's justfile.
			name:     "both bodies name the path, spelled differently",
			declared: map[string][]string{probeSweep: {}, "vuln": {probeSweep}},
			bodies:   map[string]string{probeSweep: "", "vuln": justCallerBody},
			read: []justRecipe{
				sweep, {name: "vuln", deps: []string{probeSweep}, body: readCallerBody},
			},
		},
		{
			// Bodies that differ and neither names the path. Nothing this file
			// asks of a body tells these apart, so a report here would be a
			// disagreement over something no answer rests on.
			name:     "bodies differ in text and neither names the path",
			declared: map[string][]string{"vuln": {}},
			bodies:   map[string]string{"vuln": "echo one"},
			read:     []justRecipe{{name: "vuln", body: "    echo two\n"}},
		},
		{
			// Not agreement. Every name on the other side would otherwise be
			// reported unmatched, which buries this.
			name:     "just declared nothing",
			declared: map[string][]string{},
			read:     []justRecipe{sweep, caller},
			want:     []string{"just declares no recipe in this justfile"},
		},
		{
			name:     "this reader read nothing",
			declared: map[string][]string{probeSweep: {}},
			read:     nil,
			want:     []string{"this reader read no recipe out of this justfile"},
		},
		{
			// Two empty readings match, and this is the row that says matching
			// is not the question.
			name:     "neither side read anything",
			declared: map[string][]string{},
			read:     nil,
			want: []string{
				"just declares no recipe in this justfile",
				"this reader read no recipe out of this justfile",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := justfileDisagreements(tc.declared, tc.bodies, tc.read)
			if len(got) != len(tc.want) {
				t.Fatalf("disagreements = %v, want %d matching %v", got, len(tc.want), tc.want)
			}
			for i, want := range tc.want {
				if !strings.Contains(got[i], want) {
					t.Errorf("disagreement %d = %q, want it to contain %q", i, got[i], want)
				}
			}
		})
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
// WHAT THIS STILL DOES NOT REACH. These header shapes are known to be read
// differently by just and by this reader, and the useful thing about them is
// which way each one fails. The list is what somebody has looked for and found,
// not a boundary anybody has proved:
// TestParseJustfileAgreesWithJustOnThisJustfile is the check that does not
// depend on it, and a backslash-continued header was missing from this list
// until a review found it.
//
//   - A header whose parameter default opens a literal it does not close on
//     the same line. just accepts one, because a triple-quoted default may
//     span lines, and this reader is line-based, so it drops the recipe. It
//     fails open: a caller lost this way is reported by nothing that reads the
//     header set alone. Written into this repo's own justfile it would be named
//     by TestParseJustfileAgreesWithJustOnThisJustfile, and the last row below
//     pins the drop itself, so that it stays a choice.
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
			// Neither physical line carries the separating colon and the
			// parameter's literal is closed, so this is not the unclosed-literal
			// shape below: before the reader joined the two, it found no colon,
			// dropped the header, and said nothing. The body is asserted here
			// because a reader that joined the header but started the body at the
			// wrong line would take the continuation for the first body line.
			name: "a header continued with a backslash is still one header",
			src:  "vuln x=\"a\" \\\n  : sweep\n    echo hi\n",
			want: []justRecipe{{name: "vuln", deps: []string{"sweep"}, body: "    echo hi\n"}},
		},
		{
			// The continuation is whitespace to just, not glue: this is a recipe
			// named vu that takes a parameter ln. A reader joining the two halves
			// with nothing between them would answer vuln, which is a recipe just
			// does not have and a name any dependency on the real vu would miss.
			name: "a continuation separates tokens rather than joining them",
			src:  "vu\\\nln: sweep\n    echo hi\n",
			want: []justRecipe{{name: "vu", deps: []string{"sweep"}, body: "    echo hi\n"}},
		},
		{
			// just does not continue a comment, so the header below one is a
			// recipe it runs. This row is the guard on the repair rather than on
			// the reader: joining every line that ends in a backslash would fold
			// this header into the comment and lose the recipe silently, which is
			// the failure the join was added to stop.
			name: "a comment ending in a backslash does not swallow the header below",
			src:  "# note \\\nvuln: sweep\n    echo hi\n",
			want: []justRecipe{{name: "vuln", deps: []string{"sweep"}, body: "    echo hi\n"}},
		},
		{
			// A line holding only the backslash contributes no text to the
			// header, and just reads the header under it. A join that pasted the
			// two together would push the name behind a space and out of header
			// position, which drops the recipe without a word.
			name: "a line that is only a backslash leaves the header below readable",
			src:  "\\\nvuln: sweep\n    echo hi\n",
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

// TestHeaderColonTakesTheFirstSeparatingColon pins which colon headerColon
// answers with when a header carries more than one outside a parameter default.
// A trailing comment is where the second one turns up without anyone arranging
// it, and a URL inside such a comment carries one by construction.
//
// The choice decides whether the recipe is read at all. Taking the last colon
// leaves the separator inside the name half, `vuln:` is not a name isJustName
// accepts, and justHeader then answers that the line is no header — so the
// recipe goes, and with it every caller it stands for, with nothing said. Every
// other test in this package passed under a reader that took the last one.
func TestHeaderColonTakesTheFirstSeparatingColon(t *testing.T) {
	cases := []struct {
		name  string
		line  string
		colon int
	}{
		{
			name:  "a second colon inside a trailing comment",
			line:  "vuln: sweep # note: here",
			colon: 4,
		},
		{
			name:  "a URL inside a trailing comment",
			line:  "vuln: sweep # see http://x",
			colon: 4,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := headerColon(tc.line); got != tc.colon {
				t.Errorf("headerColon(%q) = %d, want %d", tc.line, got, tc.colon)
			}
			// The index is the mechanism; this is what it costs. A reader
			// answering with the later colon reads no recipe here at all.
			recipes, complaints := parseJustfile(tc.line + "\n    echo hi\n")
			if len(complaints) != 0 {
				t.Fatalf("complaints = %v, want none", complaints)
			}
			if len(recipes) != 1 || recipes[0].name != "vuln" {
				t.Fatalf("read %v, want the recipe vuln read rather than dropped", recipes)
			}
		})
	}
}

// TestParseJustfileRefusesADependencyShapeItCannotRead is separate from the
// rows above because the reader answers this shape with a complaint instead of
// a reading. just accepts `recipe: (dep arg)`; this reader would take `(dep`
// for a name, so it says so instead.
//
// The complaint is raised by a bracket anywhere after the separating colon, so
// it is not confined to the shape named here. `vuln: sweep # note (see docs)`
// takes the same path — measured: one parenthesised-dependency complaint and no
// recipe read — where just runs vuln with the sweep and ignores the comment. So
// this test pins the answer for a dependency the reader cannot read, not the
// full set of lines that reach the answer.
func TestParseJustfileRefusesADependencyShapeItCannotRead(t *testing.T) {
	got, complaints := parseJustfile("vuln: (sweep \"arg\")\n    echo hi\n")
	if len(got) != 0 {
		t.Errorf("read %v, want the recipe left unread", got)
	}
	if len(complaints) != 1 || !strings.Contains(complaints[0], "parenthesised dependency") {
		t.Fatalf("complaints = %v, want one naming a parenthesised dependency", complaints)
	}
}

// TestParseJustfileMisreadsATrailingCommentLoudly pins how this reader answers
// a trailing comment on a header. just takes a `# …` after a dependency list as
// a comment and ignores it — measured, `just --dump` on
// `vuln: sweep-discovery-probes # note` is rc=0 with vuln depending on the
// sweep alone — where this reader takes the words after the '#' for dependency
// names.
//
// It pins this shape, not a claim about how many shapes read differently. Other
// shapes do, they are listed on TestParseJustfileReadsWhatJustReads above, and
// they do not all answer the same way: a header whose default opens a literal
// it does not close is dropped in silence, and a comment carrying a bracket is
// refused outright. What is pinned here is a reading, loudly wrong.
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
