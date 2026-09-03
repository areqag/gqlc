package emitscan_test

import (
	"go/ast"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/areqag/gqlc/internal/codegen/emitscan"
)

// The fixtures below are synthetic emissions, not goldens. Sweep.Run
// takes map[string]string, so every arm can be driven from a few lines of
// Go with no backend, no corpus and no generation — which is the point:
// the backend sweeps run this analyser over emissions that are correct,
// so they exercise it in its passing direction only, and a detector that
// exits 0 is not a gate.
//
// Nothing here needs to compile or to have its imports satisfied. The
// analyser parses; it does not type-check.
const (
	// captureArgName is the name every emitted query method must bind its
	// argument as, whatever the query text said. codegen.ParamArg.
	captureArgName = "arg"
	// captureQuerySuffix selects the files carrying query methods.
	captureQuerySuffix = ".cypher.go"
	// captureCandidate is the parameter name the probe was emitted under,
	// which appears in two of the findings' messages.
	captureCandidate = "param"

	queryPath = "people" + captureQuerySuffix
	dbPath    = "db.go"

	// baseDB declares the two package-level names the base emission has:
	// a string-typed const, which is the declaration the capture class is
	// about, and a type.
	baseDB = `package p

const getPersonSQL = "MATCH (n) RETURN n"

type Queries struct{}
`

	// baseQuery is a well-formed query file: the argument is bound under
	// the generator's own name, the only body local is generator-owned,
	// and the body resolves the query-text const.
	baseQuery = `package p

func (q *Queries) GetPerson(ctx context.Context, arg GetPersonParams) error {
	stmt := getPersonSQL
	_ = stmt
	return nil
}
`

	// unparseable is a truncated file. An emission that does not parse is
	// a finding rather than a panic, because the sweep runs over
	// emissions generated under names the emitter has never seen.
	unparseable = `package p

func (
`
)

// baseEmission is the emission both sides of the sweep get when a row
// does not perturb them. On its own it is a clean sweep, which C0 below
// requires — every row that follows differs from it in one stated way,
// so a finding a row reports is attributable to that difference.
func baseEmission() map[string]string {
	return map[string]string{dbPath: baseDB, queryPath: baseQuery}
}

// baseSweep is the base emission held against itself.
func baseSweep() emitscan.Sweep {
	return emitscan.Sweep{
		Baseline:    baseEmission(),
		Probe:       baseEmission(),
		Name:        captureCandidate,
		ArgName:     captureArgName,
		QuerySuffix: captureQuerySuffix,
	}
}

// arm names one finding by a substring of its own message and the path
// it must carry. The substring rather than the whole line because two of
// the messages interpolate a name set whose order is the analyser's to
// choose; the part named here is the part that identifies which arm
// spoke.
type arm struct {
	detail string
	path   string
}

// requireArms holds a sweep's result to exactly the arms a row declares,
// matched one-to-one.
//
// Set equality rather than a count. A count agreeing is not the same as
// the right arms speaking, and one Empty() over the slice cannot tell
// which arm spoke at all — which is the whole reason this file exists,
// since the backend sweeps already assert exactly that and are green.
func requireArms(t *testing.T, found []emitscan.Finding, want ...arm) {
	t.Helper()

	left := slices.Clone(found)
	for _, w := range want {
		i := slices.IndexFunc(left, func(f emitscan.Finding) bool {
			return f.Path == w.path && strings.Contains(f.Detail, w.detail)
		})
		require.GreaterOrEqual(t, i, 0,
			"no finding at path %q contains %q, so that arm stayed silent under a perturbation written to reach it; what did fire:\n%s",
			w.path, w.detail, emitscan.Findings(found))
		left = slices.Delete(left, i, i+1)
	}
	require.Empty(t, left,
		"the sweep reported arms this row did not declare, so the perturbation reached more than the one it names:\n%s",
		emitscan.Findings(left))
}

// TestSweepArms drives each of Sweep.Run's findings from a synthetic
// emission and holds the result to that arm alone.
//
// Two arms cannot be driven alone, and the rows say so rather than
// asserting a tidier result than the code allows:
//
//   - the per-file parse failure necessarily co-fires with the probe
//     Scope error, because Scope parses every probe file including the
//     one the per-file half already failed on;
//   - an empty declared set necessarily co-fires with an empty resolved
//     set, because resolved is intersected with declared, so it cannot be
//     the larger of the two.
//
// Every other arm is reached on its own.
func TestSweepArms(t *testing.T) {
	t.Run("C0 the base emission sweeps clean", func(t *testing.T) {
		requireArms(t, baseSweep().Run())
	})

	t.Run("no files to sweep", func(t *testing.T) {
		s := baseSweep()
		s.Probe = map[string]string{}
		requireArms(t, s.Run(), arm{detail: "the emission has no files to sweep"})
	})

	t.Run("the signature took its name from the query text", func(t *testing.T) {
		s := baseSweep()
		s.Probe[queryPath] = strings.ReplaceAll(baseQuery, "arg GetPersonParams", "param GetPersonParams")
		requireArms(t, s.Run(), arm{
			path:   queryPath,
			detail: `method GetPerson took its argument name from the query text: bound "param" under parameter "param", want "arg"`,
		})
	})

	t.Run("a body local shadows the argument", func(t *testing.T) {
		s := baseSweep()
		s.Probe[queryPath] = strings.ReplaceAll(baseQuery, "stmt := getPersonSQL\n\t_ = stmt", "arg := getPersonSQL\n\t_ = arg")
		requireArms(t, s.Run(), arm{
			path:   queryPath,
			detail: `a body local named "arg" shadows the caller's argument`,
		})
	})

	// The parse failure and the probe Scope error are one perturbation
	// reported twice, from the two places that read the same file.
	t.Run("a query file does not parse", func(t *testing.T) {
		s := baseSweep()
		s.Probe[queryPath] = unparseable
		requireArms(t, s.Run(),
			arm{path: queryPath, detail: "the emitted file does not parse"},
			arm{detail: "probe: the emitted file does not parse"},
		)
	})

	// Reached with a probe whose query file is present but not named for
	// the suffix, so Scope sees the same two files as the baseline and
	// only the per-file half notices — filenames are the suffix half's
	// business alone.
	t.Run("no query file was swept", func(t *testing.T) {
		s := baseSweep()
		s.Probe = map[string]string{dbPath: baseDB, "people.go": baseQuery}
		requireArms(t, s.Run(), arm{
			detail: "no " + captureQuerySuffix + " file was swept, so the per-file half ran on nothing",
		})
	})

	t.Run("the baseline does not parse", func(t *testing.T) {
		s := baseSweep()
		s.Baseline["broken.go"] = unparseable
		requireArms(t, s.Run(), arm{detail: "baseline: the emitted file does not parse"})
	})

	// The unparseable probe file is deliberately not named for the query
	// suffix here, which is what separates this row from the one above:
	// the per-file half skips it, so the probe Scope error is alone.
	t.Run("the probe does not parse", func(t *testing.T) {
		s := baseSweep()
		s.Probe["broken.go"] = unparseable
		requireArms(t, s.Run(), arm{detail: "probe: the emitted file does not parse"})
	})

	// Both sides declare nothing, so the two non-degeneracy arms fire and
	// the two differentials stay silent — an empty set equals an empty
	// set, which is exactly the collapse those arms exist to catch.
	t.Run("the baseline declares nothing", func(t *testing.T) {
		empty := map[string]string{
			dbPath: "package p\n\nfunc helper() {}\n",
			queryPath: `package p

func (q *Queries) GetPerson(ctx context.Context, arg GetPersonParams) error {
	stmt := 1
	_ = stmt
	return nil
}
`,
		}
		s := baseSweep()
		s.Baseline, s.Probe = empty, copyEmission(empty)
		requireArms(t, s.Run(),
			arm{detail: "the baseline package declares nothing at package level"},
			arm{detail: "no method in the baseline package resolves a package-level name"},
		)
	})

	// Declared is non-empty and resolved is not, which is the one of the
	// two that can be reached alone: a package-level name no function
	// reads.
	t.Run("the baseline resolves nothing", func(t *testing.T) {
		unread := map[string]string{
			dbPath: "package p\n\nconst unread = \"x\"\n",
			queryPath: `package p

func (q *Queries) GetPerson(ctx context.Context, arg GetPersonParams) error {
	stmt := 1
	_ = stmt
	return nil
}
`,
		}
		s := baseSweep()
		s.Baseline, s.Probe = unread, copyEmission(unread)
		requireArms(t, s.Run(), arm{detail: "no method in the baseline package resolves a package-level name"})
	})

	// A declaration the probe grew and nobody reads: the declared sets
	// differ and the resolved sets do not.
	t.Run("the probe declares a name the baseline does not", func(t *testing.T) {
		s := baseSweep()
		s.Probe[dbPath] = baseDB + "\nconst extra = \"y\"\n"
		requireArms(t, s.Run(), arm{
			detail: `renaming a query parameter to "param" changed what the emitted package declares`,
		})
	})

	// Capture in miniature, and the arm the whole package exists for: the
	// declared sets agree, and a method that used to resolve the
	// query-text const now reads its argument instead.
	t.Run("the probe stopped resolving a declaration", func(t *testing.T) {
		s := baseSweep()
		s.Probe[queryPath] = strings.ReplaceAll(baseQuery, "stmt := getPersonSQL", "stmt := arg")
		requireArms(t, s.Run(), arm{
			detail: "changed which package-level names the emitted package's methods resolve, so the caller's argument captured one",
		})
	})
}

// copyEmission copies an emission, so a row can hand the same fixture to
// both sides of a sweep without the two aliasing.
func copyEmission(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// onlyFuncDecl returns the single function an exhibit declares, and
// requires there to be exactly one so a test cannot silently read a
// different function than the one it was written about.
func onlyFuncDecl(t *testing.T, file *ast.File) *ast.FuncDecl {
	t.Helper()
	var out []*ast.FuncDecl
	for _, decl := range file.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok {
			out = append(out, fn)
		}
	}
	require.Len(t, out, 1, "the exhibit must declare exactly one function")
	return out[0]
}

// TestFindingRendering pins the two shapes a finding renders as, because
// every assertion above reads a path and the rows that carry none say so
// by carrying "".
func TestFindingRendering(t *testing.T) {
	withPath := emitscan.Finding{Path: queryPath, Detail: "something"}
	require.Equal(t, queryPath+": something", withPath.String())

	aboutPackage := emitscan.Finding{Detail: "something"}
	require.Equal(t, "something", aboutPackage.String(),
		"a finding about the package rather than a file must not render a bare separator where a path would be")

	require.Equal(t, queryPath+": something\nsomething",
		emitscan.Findings([]emitscan.Finding{withPath, aboutPackage}),
		"findings render one per line, so a sweep reporting several does not run them together")
}

// TestPackageDeclsReadsValuesNotOnlyTypes witnesses the claim
// PackageDecls' own doc comment makes and nothing held: that narrowing
// its ValueSpec arm away — leaving the TypeSpec arm alone, which is what
// conformance's declaredSurface keeps — would let an emitter deriving a
// package-level CONST name from a query author's parameter pass.
//
// The capture class is precisely about consts and vars, so the const
// below is the load-bearing row here and is asserted on its own rather
// than only as a member of the set.
func TestPackageDeclsReadsValuesNotOnlyTypes(t *testing.T) {
	file, err := emitscan.Parse("db.go", `package p

import "context"

const getPersonSQL = "MATCH (n) RETURN n"

var registry = map[string]string{}

var _ = registry

type Queries struct{}

func New(ctx context.Context) *Queries { return nil }
`)
	require.NoError(t, err)

	decls := emitscan.PackageDecls(file)

	require.Contains(t, decls, "getPersonSQL",
		"a const is not read as a package-level declaration, so an emitter naming its query-text const after a query author's parameter would move a declaration this sweep cannot see")
	require.Contains(t, decls, "registry", "a var is not read as a package-level declaration")
	require.Contains(t, decls, "Queries", "a type is not read as a package-level declaration")

	require.NotContains(t, decls, "_",
		"the blank identifier declares no name and cannot be captured, so admitting it would put a name in the declared set that no body can resolve")
	require.NotContains(t, decls, "context",
		"an import path's local name is not a declaration this sweep is about; admitting it would move the differential whenever an unrelated import did")
	require.NotContains(t, decls, "New",
		"funcs are not GenDecls and are not read here; the age suite's topLevelDecls adds them separately, and it can only do that if this does not")
}

// TestReferencedIdentsDoesNotReadAFieldAsAScopeReference witnesses the
// defect mode ReferencedIdents' doc comment states and nothing held.
//
// Both exclusions recurse rather than sweeping their operand flat,
// because either can hold the other: `arg.MinAge` inside a composite
// literal is a selector under a key-value. Read flat, the field name
// would be called a scope reference — and a field name is not something
// a parameter can capture, because it resolves against a type.
func TestReferencedIdentsDoesNotReadAFieldAsAScopeReference(t *testing.T) {
	file, err := emitscan.Parse(queryPath, `package p

func (q *Queries) GetPerson(ctx context.Context, arg GetPersonParams) error {
	params := map[string]any{"minAge": arg.MinAge}
	filter := Filter{MinAge: arg.MinAge}
	_, _ = params, filter
	return nil
}
`)
	require.NoError(t, err)

	names := emitscan.ReferencedIdents(file)

	require.Contains(t, names, "arg",
		"the operand of a selector IS resolved in the scope the parameter is bound in, so excluding the suffix must not take the operand with it")
	require.Contains(t, names, "Filter",
		"a composite literal's type is resolved in package scope; excluding the literal's keys must not take its type with it")
	require.NotContains(t, names, "MinAge",
		"a field name is read as a scope reference — both as a selector suffix and as a composite-literal key, which is the case the doc says must recurse")
}

// TestAFieldSuffixDoesNotSatisfyTheResolvedSet is the consequence half of
// the test above, driven through the exported API a caller actually uses.
//
// The doc's stated harm is not noise: a package-level declaration is held
// to being resolvable from some function, so a name appearing only as a
// field suffix would satisfy that check while nothing resolved it. Here
// MinAge is declared at package level and mentioned only as a field, and
// the resolved set must not claim it.
func TestAFieldSuffixDoesNotSatisfyTheResolvedSet(t *testing.T) {
	declared, resolved, err := emitscan.Scope(map[string]string{
		dbPath: "package p\n\nconst MinAge = 18\n",
		queryPath: `package p

func (q *Queries) GetPerson(ctx context.Context, arg GetPersonParams) error {
	filter := Filter{MinAge: arg.MinAge}
	_ = filter
	return nil
}
`,
	})
	require.NoError(t, err)

	require.Contains(t, declared, "MinAge")
	require.NotContains(t, resolved, "MinAge",
		"a package-level name mentioned only as a field suffix is reported as resolved, so the reachability half would be satisfied by a body that never reads it")
}

// TestFreeIdentsIsFlatRatherThanBlockScoped pins the direction of the
// error FreeIdents' doc comment declares it makes.
//
// It is flat: a name bound anywhere in the function is treated as bound
// throughout, so a package-level name read outside an inner block that
// shadows it is not counted as free. A block-scoped reading would call
// that outer read free. The doc states the flat reading errs towards
// calling a name bound; what is pinned here is the reading itself, since
// the downstream claim about which way a sweep then fails is about
// Sweep.Run and is not measured by this test.
func TestFreeIdentsIsFlatRatherThanBlockScoped(t *testing.T) {
	file, err := emitscan.Parse(queryPath, `package p

func (q *Queries) GetPerson(ctx context.Context, arg GetPersonParams) error {
	if true {
		getPersonSQL := "shadow"
		_ = getPersonSQL
	}
	stmt := getPersonSQL
	_ = stmt
	return nil
}
`)
	require.NoError(t, err)

	fn := onlyFuncDecl(t, file)
	free := emitscan.FreeIdents(fn)

	require.NotContains(t, free, "getPersonSQL",
		"a name bound in an inner block is reported free where it is read outside that block, which is the block-scoped reading this helper deliberately does not do")
	require.NotContains(t, free, "stmt", "a body local is not free")
	require.NotContains(t, free, "arg", "a parameter is not free")
	require.Contains(t, free, "Queries",
		"the receiver's type is resolved outside the function and must stay free, or the resolved set loses every type an emitted method hangs off")
}
