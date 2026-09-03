package codegen_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// A switch over a closed enum is checked for a missing arm by
// golangci-lint's `exhaustive`, and only while it carries no `default`:
// .golangci.yml sets default-signifies-exhaustive, so a `default` tells
// the linter the switch is already complete and it stops looking.
// Adding one is a change that reads as defensive hygiene, builds clean,
// lints green and passes every gate in this repository — and retires the
// only thing standing between a new member and an arm nobody wrote
// (bd gqlc-7hp5g).
//
// Until this fence that hazard was recorded in a doc comment asking a
// future author not to do it, which is the shape of claim that gets
// edited away by someone who has a reason. A `default` is also
// legitimately correct in some of these switches — carriesElemBare
// answers false for a kind it does not care about, and should — so the
// rule cannot be "no such switch carries a default". It is: a `default`
// must declare itself, and the declaration is checked (bd gqlc-qf5az,
// widened past ColumnKind by bd gqlc-f5onb).
const defaultOKTag = "//gqlc:default-ok"

// guardedSum is one closed enum this fence keeps the `exhaustive` check
// live over: where its constants are declared, and which package
// directories under internal/codegen must still hold a switch over it
// that carries no `default`.
type guardedSum struct {
	// name is the Go type the constant block is declared with.
	name string
	// declRoot is where to read that block, relative to this package.
	// It is not where switches are scanned; see codegenTreeGoFiles.
	declRoot string
	// sentinel is one member the derivation must find. Listed rather
	// than derived on purpose: it is the only thing that can tell a
	// broken derivation apart from a sum nothing switches on, and both
	// otherwise present as an empty population that every assertion
	// passes over.
	sentinel string
	// dirs are directories the walk must find at least one still-guarded
	// switch over this sum in. The population is derived from the tree so
	// it grows by itself, but a derivation that broke would report an
	// empty set as compliance, and asserting on the total would let one
	// directory go silent while another still spoke.
	//
	// These are measurements, not aspirations. ColumnKind names only
	// neo4j because the age driver's single ColumnKind switch —
	// zeroValueText — wants its default and has it; age has no
	// exhaustive-guarded ColumnKind switch to name. A driver added later
	// is held to the rule without being listed here; what naming buys is
	// a walk that stops seeing a directory failing loudly instead of
	// passing.
	dirs []string
}

// guardedSums is the whole population this fence holds. Scalar and
// Temporal are declared in internal/resolver, so their constants are
// read from there — but switches are still scanned under
// internal/codegen alone. That boundary is deliberate: the only
// resolver-side default over either sum is its own String method's, which
// is correct and is already fenced by name in
// internal/resolver/sumdefaults_internal_test.go
// (TestScalarStringAnswersForDeclaredKindsAlone and its Temporal twin).
// A default added to some *other* resolver-side switch would be unheld;
// that is bd gqlc-8z5ap, not a hole this file pretends to cover.
var guardedSums = []guardedSum{
	{name: "ColumnKind", declRoot: ".", sentinel: "ColumnProperty", dirs: []string{"neo4j"}},
	{name: "Scalar", declRoot: "../resolver", sentinel: "ScalarNull", dirs: []string{"age", "neo4j"}},
	{name: "Temporal", declRoot: "../resolver", sentinel: "TemporalDate", dirs: []string{"age", "neo4j"}},
}

// sumSwitch is one switch whose case expressions name members of one
// guarded sum: which sum, where it is, whether it carries a default, and
// the //gqlc:default-ok reason written above it if any. A switch naming
// two sums yields one of these per sum.
type sumSwitch struct {
	sum        string
	pos        string // path:line, as a reader would grep for it
	dir        string
	hasDefault bool
	reason     string
}

// TestGuardedSumDefaultsDeclareThemselves is the fence: a switch over a
// guarded sum either carries no default, so `exhaustive` is live on it
// and a member added later reds the lint, or carries one and says why.
func TestGuardedSumDefaultsDeclareThemselves(t *testing.T) {
	switches, _ := scanSumSwitches(t)

	for _, sw := range switches {
		if !sw.hasDefault {
			continue
		}
		require.NotEmpty(t, sw.reason,
			"%s switches on %s and carries a `default`, so golangci-lint's exhaustive no longer checks it for a missing arm — .golangci.yml sets default-signifies-exhaustive — and a %s added later lands in that default silently, at run time, in generated code. If the default is wrong here, delete it and spell the arms out. If it is right, because a conservative answer suits every member this switch does not name, write `%s <why>` on the line directly above the switch",
			sw.pos, sw.sum, sw.sum, defaultOKTag)
	}
}

// TestDeclaredDefaultsStillGuardADefault is the tripwire on the tag.
// The tag buys an exemption, so one left behind by an edit that removed
// the default is an exemption nothing pays for, and the next author to
// add a default under it inherits a fence that was already green.
func TestDeclaredDefaultsStillGuardADefault(t *testing.T) {
	switches, _ := scanSumSwitches(t)

	for _, sw := range switches {
		if sw.reason == "" {
			continue
		}
		require.True(t, sw.hasDefault,
			"%s carries `%s %s` but the switch below it has no `default`; exhaustive is live here already, so the tag exempts nothing and reads as though a default were sanctioned. Drop the tag",
			sw.pos, defaultOKTag, sw.reason)
	}

	for _, sum := range guardedSums {
		require.NotEmpty(t, sum.dirs,
			"guardedSums names %s but lists no directory that must hold a still-guarded switch over it, so nothing in the tree is held to the rule for that sum and its rows would pass on an empty set",
			sum.name)

		for _, dir := range sum.dirs {
			found := slices.ContainsFunc(switches, func(sw sumSwitch) bool {
				return sw.sum == sum.name && sw.dir == dir && !sw.hasDefault
			})
			require.True(t, found,
				"the walk found no %s switch WITHOUT a `default` under internal/codegen/%s, so nothing in that package is being held to this rule for that sum. Either every such switch there has taken a default, which is the thing this fence exists to notice, or the walk stopped seeing the directory",
				sum.name, dir)
		}
	}
}

// TestStrayDefaultOKTagsAreNotSilent catches a tag the switch scan never
// reads. Such a tag exempts nothing, so no assertion above touches it,
// and it sits in the tree looking like a satisfied fence — the same
// failure the tag was introduced to remove.
func TestStrayDefaultOKTagsAreNotSilent(t *testing.T) {
	_, stray := scanSumSwitches(t)

	for _, loc := range stray {
		require.Fail(t, "inert //gqlc:default-ok tag",
			"%s carries `%s` but no switch over a guarded sum begins on the line below it. The fence recognises such a switch by a case expression naming one of the sum's constants, so a tag above anything else is read by nothing",
			loc, defaultOKTag)
	}
}

// scanSumSwitches walks every non-test Go file under internal/codegen
// and returns the guarded-sum switches it found, plus the location of
// every //gqlc:default-ok tag that sat above something else. One pass
// returns both so the tag scan and the switch scan cannot disagree about
// which tags were read.
func scanSumSwitches(t *testing.T) (switches []sumSwitch, stray []string) {
	t.Helper()

	members := make(map[string][]string, len(guardedSums))
	for _, sum := range guardedSums {
		members[sum.name] = sumMemberNames(t, sum)
	}

	for _, path := range codegenTreeGoFiles(t) {
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
		require.NoError(t, err, "parsing %s", path)

		// Keyed by the line the tag governs — the one after it.
		reasonFor := make(map[int]string)
		tagLine := make(map[int]int)
		for _, group := range file.Comments {
			for _, comment := range group.List {
				text := strings.TrimSpace(comment.Text)
				if !strings.HasPrefix(text, defaultOKTag) {
					continue
				}
				governs := fset.Position(comment.End()).Line + 1
				reasonFor[governs] = strings.TrimSpace(strings.TrimPrefix(text, defaultOKTag))
				tagLine[governs] = fset.Position(comment.Pos()).Line
			}
		}

		dir := filepath.Base(filepath.Dir(path))
		read := make(map[int]bool, len(reasonFor))

		ast.Inspect(file, func(n ast.Node) bool {
			sw, ok := n.(*ast.SwitchStmt)
			if !ok {
				return true
			}
			line := fset.Position(sw.Pos()).Line
			for _, sum := range guardedSums {
				if !namesASumMember(sw, members[sum.name]) {
					continue
				}
				read[line] = true
				switches = append(switches, sumSwitch{
					sum:        sum.name,
					pos:        path + ":" + strconv.Itoa(line),
					dir:        dir,
					hasDefault: carriesDefault(sw),
					reason:     reasonFor[line],
				})
			}
			return true
		})

		for governs, line := range tagLine {
			if !read[governs] {
				stray = append(stray, path+":"+strconv.Itoa(line))
			}
		}
	}

	// Per sum, not on the total: one sum still switched on somewhere
	// would satisfy a count and let another go unmeasured, which is the
	// aggregate reading this fence exists to refuse.
	for _, sum := range guardedSums {
		require.True(t,
			slices.ContainsFunc(switches, func(sw sumSwitch) bool { return sw.sum == sum.name }),
			"the walk found no %s switch anywhere under internal/codegen, so every assertion reading this would pass over an empty set for that sum",
			sum.name)
	}

	slices.Sort(stray)
	return switches, stray
}

// sumMemberNames reads one sum's constants off the sources that declare
// them. Listed here instead, the set would go stale in the direction that
// matters: a member this fence does not know reads as an ordinary
// identifier, so the switch enumerating it stops looking like a switch
// over that sum and leaves the population unmeasured.
func sumMemberNames(t *testing.T, sum guardedSum) []string {
	t.Helper()

	var names []string
	for _, path := range goFilesUnder(t, sum.declRoot) {
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, nil, 0)
		require.NoError(t, err, "parsing %s", path)

		for _, decl := range file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.CONST {
				continue
			}
			// Only the first spec of an iota block carries the type; the
			// rest inherit it, so the flag persists across the block and
			// resets when another typed spec appears. That reset is what
			// keeps the `<Sum>Count int = iota` sentinel out of the set:
			// it is a count, not a member, and no switch may have an arm
			// for it.
			var typed bool
			for _, spec := range gen.Specs {
				value, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				if ident, ok := value.Type.(*ast.Ident); ok {
					typed = ident.Name == sum.name
				}
				if !typed {
					continue
				}
				for _, name := range value.Names {
					names = append(names, name.Name)
				}
			}
		}
	}

	slices.Sort(names)
	require.Contains(t, names, sum.sentinel,
		"the %s constant scan under %s found no %s, so it is reading the wrong declaration or none. Every switch would then classify as not-a-%s-switch and every row for that sum would pass on an empty population",
		sum.name, sum.declRoot, sum.sentinel, sum.name)
	return names
}

// namesASumMember reports whether any case expression of sw names one of
// members, qualified or bare. The drivers write codegen.ColumnNode and
// package codegen writes ColumnNode; both are the same switch to this
// fence.
func namesASumMember(sw *ast.SwitchStmt, members []string) bool {
	for _, stmt := range sw.Body.List {
		clause, ok := stmt.(*ast.CaseClause)
		if !ok {
			continue
		}
		for _, expr := range clause.List {
			switch e := expr.(type) {
			case *ast.Ident:
				if slices.Contains(members, e.Name) {
					return true
				}
			case *ast.SelectorExpr:
				if slices.Contains(members, e.Sel.Name) {
					return true
				}
			}
		}
	}
	return false
}

// carriesDefault reports whether sw has a default clause, which the AST
// spells as a CaseClause with no expressions.
func carriesDefault(sw *ast.SwitchStmt) bool {
	for _, stmt := range sw.Body.List {
		if clause, ok := stmt.(*ast.CaseClause); ok && clause.List == nil {
			return true
		}
	}
	return false
}

// codegenTreeGoFiles returns every non-test Go file under this package's
// directory, drivers included. It is the switch-scanning population, and
// it is narrower than the set of directories constants are read from.
func codegenTreeGoFiles(t *testing.T) []string {
	t.Helper()
	return goFilesUnder(t, ".")
}

// goFilesUnder returns every non-test Go file under root. Derived rather
// than listed so a driver added later joins the fence by existing.
func goFilesUnder(t *testing.T, root string) []string {
	t.Helper()

	var out []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if entry.Name() == "testdata" {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		out = append(out, path)
		return nil
	})
	require.NoError(t, err, "walking %s", root)
	require.NotEmpty(t, out, "the walk of %s returned no Go files", root)
	slices.Sort(out)
	return out
}
