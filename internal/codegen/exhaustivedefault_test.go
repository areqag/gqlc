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

// A switch on codegen.ColumnKind is checked for a missing arm by
// golangci-lint's `exhaustive`, and only while it carries no `default`:
// .golangci.yml sets default-signifies-exhaustive, so a `default` tells
// the linter the switch is already complete and it stops looking.
// Adding one is a change that reads as defensive hygiene, builds clean,
// lints green and passes every gate in this repository — and retires the
// only thing standing between a new ColumnKind and an arm nobody wrote
// (bd gqlc-7hp5g).
//
// Until this fence that hazard was recorded in a doc comment asking a
// future author not to do it, which is the shape of claim that gets
// edited away by someone who has a reason. A `default` is also
// legitimately correct in some of these switches — carriesElemBare
// answers false for a kind it does not care about, and should — so the
// rule cannot be "no ColumnKind switch carries a default". It is: a
// `default` must declare itself, and the declaration is checked (bd
// gqlc-qf5az).
const defaultOKTag = "//gqlc:default-ok"

// namedGuardedSwitchDirs are package directories the walk must find at
// least one still-guarded ColumnKind switch in. The population is
// derived from the tree so it grows by itself, but a derivation that
// broke would report an empty set as compliance, and asserting on the
// total would let one directory go silent while another still spoke.
//
// Only neo4j is named, and that is a measurement rather than an
// oversight: the age driver has exactly one ColumnKind switch —
// zeroValueText — and its default is the correct answer there, so age
// has no exhaustive-guarded ColumnKind switch to name. Its other
// reference to the enum is an `if`, not a switch. A driver added later
// is held to the rule without being listed here; what naming buys is a
// walk that stops seeing neo4j failing loudly instead of passing.
var namedGuardedSwitchDirs = []string{"neo4j"}

// kindSwitch is one switch whose case expressions name ColumnKind
// constants: where it is, whether it carries a default, and the
// //gqlc:default-ok reason written above it if any.
type kindSwitch struct {
	pos        string // path:line, as a reader would grep for it
	dir        string
	hasDefault bool
	reason     string
}

// TestColumnKindDefaultsDeclareThemselves is the fence: a ColumnKind
// switch either carries no default, so `exhaustive` is live on it and a
// kind added later reds the lint, or carries one and says why.
func TestColumnKindDefaultsDeclareThemselves(t *testing.T) {
	switches, _ := scanKindSwitches(t)

	for _, sw := range switches {
		if !sw.hasDefault {
			continue
		}
		require.NotEmpty(t, sw.reason,
			"%s switches on ColumnKind and carries a `default`, so golangci-lint's exhaustive no longer checks it for a missing arm — .golangci.yml sets default-signifies-exhaustive — and a ColumnKind added later lands in that default silently, at run time, in generated code. If the default is wrong here, delete it and spell the arms out. If it is right, because a conservative answer suits every kind this switch does not name, write `%s <why>` on the line directly above the switch",
			sw.pos, defaultOKTag)
	}
}

// TestDeclaredDefaultsStillGuardADefault is the tripwire on the tag.
// The tag buys an exemption, so one left behind by an edit that removed
// the default is an exemption nothing pays for, and the next author to
// add a default under it inherits a fence that was already green.
func TestDeclaredDefaultsStillGuardADefault(t *testing.T) {
	switches, _ := scanKindSwitches(t)

	for _, sw := range switches {
		if sw.reason == "" {
			continue
		}
		require.True(t, sw.hasDefault,
			"%s carries `%s %s` but the switch below it has no `default`; exhaustive is live here already, so the tag exempts nothing and reads as though a default were sanctioned. Drop the tag",
			sw.pos, defaultOKTag, sw.reason)
	}

	for _, dir := range namedGuardedSwitchDirs {
		found := slices.ContainsFunc(switches, func(sw kindSwitch) bool {
			return sw.dir == dir && !sw.hasDefault
		})
		require.True(t, found,
			"the walk found no ColumnKind switch WITHOUT a `default` under internal/codegen/%s, so nothing in that package is being held to this rule. Either every switch there has taken a default, which is the thing this fence exists to notice, or the walk stopped seeing the directory",
			dir)
	}
}

// TestStrayDefaultOKTagsAreNotSilent catches a tag the switch scan never
// reads. Such a tag exempts nothing, so no assertion above touches it,
// and it sits in the tree looking like a satisfied fence — the same
// failure the tag was introduced to remove.
func TestStrayDefaultOKTagsAreNotSilent(t *testing.T) {
	_, stray := scanKindSwitches(t)

	for _, loc := range stray {
		require.Fail(t, "inert //gqlc:default-ok tag",
			"%s carries `%s` but no ColumnKind switch begins on the line below it. The fence recognises a switch by a case expression naming a ColumnKind constant, so a tag above anything else is read by nothing",
			loc, defaultOKTag)
	}
}

// scanKindSwitches walks every non-test Go file under internal/codegen
// and returns the ColumnKind switches it found, plus the location of
// every //gqlc:default-ok tag that sat above something else. One pass
// returns both so the tag scan and the switch scan cannot disagree about
// which tags were read.
func scanKindSwitches(t *testing.T) (switches []kindSwitch, stray []string) {
	t.Helper()
	kinds := columnKindNames(t)

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
			if !ok || !namesAColumnKind(sw, kinds) {
				return true
			}
			line := fset.Position(sw.Pos()).Line
			read[line] = true
			switches = append(switches, kindSwitch{
				pos:        path + ":" + strconv.Itoa(line),
				dir:        dir,
				hasDefault: carriesDefault(sw),
				reason:     reasonFor[line],
			})
			return true
		})

		for governs, line := range tagLine {
			if !read[governs] {
				stray = append(stray, path+":"+strconv.Itoa(line))
			}
		}
	}

	require.NotEmpty(t, switches,
		"the walk found no ColumnKind switch anywhere under internal/codegen; every assertion reading this would pass over an empty set")
	slices.Sort(stray)
	return switches, stray
}

// columnKindNames reads the ColumnKind constants off the package's own
// sources. Listed here instead, the set would go stale in the direction
// that matters: a kind this fence does not know reads as an ordinary
// identifier, so the switch enumerating it stops looking like a
// ColumnKind switch and leaves the population unmeasured.
func columnKindNames(t *testing.T) []string {
	t.Helper()

	var names []string
	for _, path := range codegenTreeGoFiles(t) {
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
			// resets when another typed spec appears.
			var typed bool
			for _, spec := range gen.Specs {
				value, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				if ident, ok := value.Type.(*ast.Ident); ok {
					typed = ident.Name == "ColumnKind"
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
	require.Contains(t, names, "ColumnProperty",
		"the ColumnKind constant scan found no ColumnProperty, so it is reading the wrong declaration or none. Every switch would then classify as not-a-ColumnKind-switch and this whole fence would pass on an empty population")
	return names
}

// namesAColumnKind reports whether any case expression of sw names a
// ColumnKind constant, qualified or bare. The drivers write
// codegen.ColumnNode and package codegen writes ColumnNode; both are the
// same switch to this fence.
func namesAColumnKind(sw *ast.SwitchStmt, kinds []string) bool {
	for _, stmt := range sw.Body.List {
		clause, ok := stmt.(*ast.CaseClause)
		if !ok {
			continue
		}
		for _, expr := range clause.List {
			switch e := expr.(type) {
			case *ast.Ident:
				if slices.Contains(kinds, e.Name) {
					return true
				}
			case *ast.SelectorExpr:
				if slices.Contains(kinds, e.Sel.Name) {
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
// directory, drivers included. Derived rather than listed so a driver
// added later joins the fence by existing.
func codegenTreeGoFiles(t *testing.T) []string {
	t.Helper()

	var out []string
	err := filepath.WalkDir(".", func(path string, entry fs.DirEntry, err error) error {
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
	require.NoError(t, err, "walking the codegen tree")
	require.NotEmpty(t, out, "the codegen tree walk returned no Go files")
	slices.Sort(out)
	return out
}
