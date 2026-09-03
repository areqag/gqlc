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
	// It is not where switches are scanned; see scanRoots.
	declRoot string
	// scanRoots are the directories switches over this sum are scanned in,
	// relative to this package. Separate from declRoot because the two
	// answer different questions — where the members are written, and
	// where a switch over them may appear — and for a sum declared
	// elsewhere the answers differ.
	//
	// Each root must yield at least one switch over the sum. A root that
	// silently went unscanned would take its switches out of the fence
	// while every remaining row still passed, which is the shape of
	// blindness this whole file exists to refuse.
	scanRoots []string
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
// Temporal are declared in internal/resolver, and switches over them are
// now scanned there as well as here (bd gqlc-8z5ap). The scan used to
// stop at internal/codegen, so a `default` added to any *other*
// resolver-side switch over either sum was unheld — and the sums are
// exported, so nothing confined such a switch to this package.
//
// The two the widened scan finds are the sums' own String methods, and
// both keep their default: the property `exhaustive` would give is
// already held there by name, by
// TestScalarStringAnswersForDeclaredKindsAlone in
// internal/resolver/sumdefaults_internal_test.go and by
// TestTemporalStringerAnswersForDeclaredKindsAlone in
// internal/codegen/conformance, each of which fails when a declared
// member loses its arm and starts rendering as the undeclared form. Both
// therefore carry a tag naming that test.
//
// So ../resolver holds no still-guarded switch and appears in no sum's
// dirs. What keeps the two tags honest is
// TestDeclaredDefaultsStillGuardADefault; what keeps the root itself from
// going quietly unscanned is scanRoots' own population check, and nothing
// else. Measured: a walk truncated to each sum's first root leaves all
// three tests green as soon as that check is weakened to speak per sum
// instead of per root, because a file the walk never reaches carries no
// tag the stray scan can report. Dropping a root from a list here is the
// other failure and is caught otherwise — the file is still walked for
// the sum that kept the root, so the orphaned tag turns up in
// TestStrayDefaultOKTagsAreNotSilent.
//
// THREE CLOSED SUMS ARE DELIBERATELY OUTSIDE THIS FENCE, on the ground that
// their defaults are loud or accounted rather than silent (bd gqlc-5225b,
// designed on gqlc-qr09l). The deciding line is what a default does for a
// member added later: invent a plausible answer nobody sees, or say the value
// is unhandled. Only the first is this fence's quarry.
//
//   - config.SchemaLang and config.QueryLang (internal/config/config.go).
//     Their one switch each, in runTarget in internal/cli/pipeline/pipeline.go,
//     keeps a default returning "internal: no pipeline mapping for ... %q" —
//     it names the unmapped value and fails the run. Member-add is also red
//     before that: SchemaLangValues/QueryLangValues are pinned by name in
//     internal/config/config_test.go.
//   - takeVerdict (internal/tools/tmpreap/archive.go). account()'s default
//     routes an unknown verdict into the `unreadable` bucket by design; its
//     own comment records that a silent default shipped the deleted-with-no-
//     record defect twice (bd gqlc-osuz). Deleting that default to satisfy
//     this fence would re-open the defect it exists to end.
//
// A fourth, graph.EntityKind, stays out on a different ground entirely — the
// two-types-one-name collision of bd gqlc-pw6yj — and not on this one.
//
// Adding a row here is the only way to bring such a sum in. A
// //gqlc:default-ok tag alone cannot do it: on a switch over a sum this list
// does not name, TestStrayDefaultOKTagsAreNotSilent reports the tag as stray.
var guardedSums = []guardedSum{
	{name: "ColumnKind", declRoot: ".", scanRoots: []string{"."}, sentinel: "ColumnProperty", dirs: []string{"neo4j"}},
	{name: "Scalar", declRoot: "../resolver", scanRoots: []string{".", "../resolver"}, sentinel: "ScalarNull", dirs: []string{"age", "neo4j"}},
	{name: "Temporal", declRoot: "../resolver", scanRoots: []string{".", "../resolver"}, sentinel: "TemporalDate", dirs: []string{"age", "neo4j"}},
	// Cardinality and TypeToken carry no tag between them: every switch
	// over either names its members outright and answers for an undeclared
	// value below the switch instead of in a `default`, which is what
	// cardinalityAnnotation and validToken already did in the two
	// declaring packages (bd gqlc-51l6m). So each names a dir per scan
	// root — every root here holds a still-guarded switch, and naming them
	// all is what makes a root that stopped being read fail rather than
	// lean on a sibling.
	//
	// Cardinality's dir "." is this package's own directory: the walk names
	// a file reached under root "." by the base of its parent, and for a
	// file directly in the root that is ".".
	//
	// The re-export that blinded `exhaustive` under "." — internal/codegen's
	// `type Cardinality = queryfile.Cardinality` and its three constant
	// mirrors — was deleted (bd gqlc-ptz4t), so the failure message below
	// now reads true under both roots; TestGuardedSumsAreNotAliased refuses
	// its return.
	{name: "Cardinality", declRoot: "../queryfile", scanRoots: []string{".", "../queryfile"}, sentinel: "CardinalityExec", dirs: []string{".", "queryfile"}},
	{name: "TypeToken", declRoot: "../procsig", scanRoots: []string{"../procsig", "../query", "../resolver"}, sentinel: "TokenNumber", dirs: []string{"procsig", "cypher", "resolver"}},
	// The five below arrive together (bd gqlc-5225b, designed on gqlc-qr09l).
	// Every switch over each defaulted, so `exhaustive` checked them nowhere
	// and this fence could not hold them: its dirs invariant demands a
	// still-guarded switch per sum and none existed. Each default was SILENT
	// — it returned the zero member's wire name for a member added later, the
	// drift this fence was built for (bd gqlc-7hp5g) — so each was deleted
	// and its answer hoisted below the switch unchanged, behaviour-identical
	// for every value in range and out. goFilesUnder recurses, so "../query"
	// reaches query/cypher, which is why AggregateFunc names dir "cypher".
	{name: "AggregateFunc", declRoot: "../query", scanRoots: []string{"../query"}, sentinel: "AggPercentile", dirs: []string{"cypher", "query"}},
	{name: "BindingKind", declRoot: "../query", scanRoots: []string{"../query"}, sentinel: "BindingUnwind", dirs: []string{"query"}},
	{name: "ClauseSlot", declRoot: "../query", scanRoots: []string{"../query"}, sentinel: "ClauseSlotLimit", dirs: []string{"query"}},
	{name: "ExprPosition", declRoot: "../query", scanRoots: []string{"../query", "../resolver"}, sentinel: "ExprInDeleteTarget", dirs: []string{"query", "resolver"}},
	{name: "UnionKind", declRoot: "../query", scanRoots: []string{"../query"}, sentinel: "UnionAll", dirs: []string{"query"}},
}

// sumSwitch is one switch whose case expressions name members of one
// guarded sum: which sum, where it is, whether it carries a default, and
// the //gqlc:default-ok reason written above it if any. A switch naming
// two sums yields one of these per sum.
type sumSwitch struct {
	sum        string
	pos        string // path:line, as a reader would grep for it
	root       string // the scanRoot the file was reached under
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
			"%s switches on %s and carries a `default`, so golangci-lint's exhaustive no longer checks it for a missing arm — .golangci.yml sets default-signifies-exhaustive — and a %s added later lands in that default silently, at run time — and for a switch under internal/codegen, in the generated output. If the default is wrong here, delete it and spell the arms out. If it is right, because a conservative answer suits every member this switch does not name, write `%s <why>` on the line directly above the switch",
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
				"the walk found no %s switch WITHOUT a `default` in a directory named %q under this sum's scan roots (%s), so nothing there is being held to this rule for that sum. Either every such switch has taken a default, which is the thing this fence exists to notice, or the walk stopped seeing the directory",
				sum.name, dir, strings.Join(sum.scanRoots, ", "))
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

// TestGuardedSumsAreNotAliased refuses a type alias to a guarded sum.
//
// `exhaustive` v0.12.0 resolves a switch's tag type by type-switching on
// *types.Named with no *types.Alias arm and no types.Unalias call, and
// go/types materialises aliases by default since Go 1.23. So a switch
// whose tag type is spelled through an alias is skipped WHOLESALE — zero
// issues, not every-member-missing — and every row in guardedSums above
// keeps passing while the linter half of the property is gone. That is
// what internal/codegen's `type Cardinality = queryfile.Cardinality` did
// until bd gqlc-ptz4t deleted it; measured then, deleting a whole case
// arm left the linter reporting nothing.
//
// The bound: this sees alias DECLARATIONS inside the scan roots. An alias
// declared outside them and used inside is beyond a per-file AST scan,
// and so is one that renames through an intermediate.
func TestGuardedSumsAreNotAliased(t *testing.T) {
	sums := map[string]bool{}
	roots := map[string]bool{}
	for _, sum := range guardedSums {
		sums[sum.name] = true
		for _, root := range sum.scanRoots {
			roots[root] = true
		}
	}

	for root := range roots {
		for _, path := range goFilesUnder(t, root) {
			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, path, nil, 0)
			require.NoError(t, err, "parsing %s", path)

			for _, decl := range file.Decls {
				gen, ok := decl.(*ast.GenDecl)
				if !ok || gen.Tok != token.TYPE {
					continue
				}
				for _, spec := range gen.Specs {
					ts, ok := spec.(*ast.TypeSpec)
					if !ok || ts.Assign == token.NoPos {
						continue
					}
					if name, ok := aliasedTypeName(ts.Type); ok && sums[name] {
						require.Fail(t, "alias to a guarded sum",
							"%s declares `%s` as a type alias of the guarded sum %s, and `exhaustive` skips a switch whose tag type is spelled through an alias — it reads *types.Alias, matches no case, and reports nothing at all. Every %s row above would keep passing over a switch the linter no longer checks. Name the sum through its declaring package instead (bd gqlc-ptz4t)",
							fset.Position(ts.Pos()), ts.Name.Name, name, name)
					}
				}
			}
		}
	}
}

// aliasedTypeName returns the name an alias RHS refers to, for the two
// spellings a guarded sum can take: bare within its declaring package,
// qualified from outside it.
func aliasedTypeName(expr ast.Expr) (string, bool) {
	switch rhs := expr.(type) {
	case *ast.Ident:
		return rhs.Name, true
	case *ast.SelectorExpr:
		return rhs.Sel.Name, true
	default:
		return "", false
	}
}

// scanTarget is one sum to look for in one file, and the scan root the
// file was reached under. Attribution to a root is what lets the
// population check below speak per root rather than per sum, so a root
// that stopped yielding files fails instead of being covered by another.
type scanTarget struct {
	sum  string
	root string
}

// scanSumSwitches walks every non-test Go file under the union of the
// scan roots and returns the guarded-sum switches it found, plus the
// location of every //gqlc:default-ok tag that sat above something else.
// One pass returns both so the tag scan and the switch scan cannot
// disagree about which tags were read.
//
// A file is only examined for the sums whose scanRoots reach it, so a sum
// scanned narrowly does not acquire the wider sums' territory by sharing
// this walk.
func scanSumSwitches(t *testing.T) (switches []sumSwitch, stray []string) {
	t.Helper()

	members := make(map[string][]string, len(guardedSums))
	targets := map[string][]scanTarget{}
	var paths []string
	for _, sum := range guardedSums {
		members[sum.name] = sumMemberNames(t, sum)

		require.NotEmpty(t, sum.scanRoots,
			"guardedSums names %s but lists no scan root, so no file is examined for it and every row for that sum would pass on an empty set",
			sum.name)

		for _, root := range sum.scanRoots {
			for _, path := range goFilesUnder(t, root) {
				if targets[path] == nil {
					paths = append(paths, path)
				}
				targets[path] = append(targets[path], scanTarget{sum: sum.name, root: root})
			}
		}
	}
	slices.Sort(paths)

	for _, path := range paths {
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
			for _, target := range targets[path] {
				if !namesASumMember(sw, members[target.sum]) {
					continue
				}
				read[line] = true
				switches = append(switches, sumSwitch{
					sum:        target.sum,
					pos:        path + ":" + strconv.Itoa(line),
					root:       target.root,
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

	// Per sum AND per root, not on the total and not on the sum alone:
	// a sum still switched on in one of its roots would satisfy a count
	// while another root went unread, and that root's switches would
	// leave the fence with every remaining row still green. Which is the
	// aggregate reading this file exists to refuse — and, before bd
	// gqlc-8z5ap, exactly what ../resolver was.
	for _, sum := range guardedSums {
		for _, root := range sum.scanRoots {
			require.True(t,
				slices.ContainsFunc(switches, func(sw sumSwitch) bool {
					return sw.sum == sum.name && sw.root == root
				}),
				"the walk found no %s switch anywhere under %s, so every assertion reading this would pass over an empty set for that sum in that root. Either the switches there were deleted, or the walk stopped reaching the root — and if the sum genuinely is no longer switched on there, drop the root rather than leaving a check that passes on nothing",
				sum.name, root)
		}
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
