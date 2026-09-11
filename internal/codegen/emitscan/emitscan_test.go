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

// iterEmission is the shape ruling-1a5-iter-streaming-cardinality §5
// schedules for :iter: a method returning iter.Seq2, whose body returns
// a closure binding `yield`. `yield` is written here ONLY by the
// literal's parameter list — the signature names it nowhere — so a test
// over this source reads DeclaredIdents' FuncLit arm and nothing else.
const iterEmission = `package p

func (q *Queries) GetPersonIter(ctx context.Context, arg GetPersonParams) iter.Seq2[GetPersonRow, error] {
	return func(yield func(GetPersonRow, error) bool) {
		var row GetPersonRow
		if !yield(row, nil) {
			return
		}
	}
}
`

// TestAClosureParameterIsNotFree holds the direction of error that
// FreeIdents' doc comment declares, at the one construct that broke it:
// a name a function literal binds is bound, not free.
//
// Which way it errs is the whole safety argument for the capture
// sweeps. A name reported free is a name the sweep checks for capture;
// a name wrongly reported free is only noise, but a bound name wrongly
// omitted is a capture that slips through. Before the FuncLit arm,
// `yield` was omitted from every bound set, so an emission that let a
// query author's chosen name reach a closure parameter was unpoliced.
func TestAClosureParameterIsNotFree(t *testing.T) {
	file, err := emitscan.Parse(queryPath, iterEmission)
	require.NoError(t, err)

	fn := onlyFuncDecl(t, file)
	free := emitscan.FreeIdents(fn)

	require.NotContains(t, free, "yield",
		"a closure parameter is reported free, so FreeIdents errs towards calling a name UNbound — the opposite of the direction its doc comment declares, and the direction in which a capture slips through the sweep instead of failing it")
	require.Contains(t, free, "GetPersonRow",
		"a type the closure names is resolved outside the function and must stay free, or the resolved set loses the row type an :iter emission hangs off")
}

// TestAClosureParameterIsABodyLocal holds the same arm through
// BodyLocals, which is a separate question from FreeIdents': its only
// caller asserts that the set of names an emitted body binds is
// invariant under renaming the query's columns. A closure parameter
// missing from that set makes the assertion pass over a name it never
// saw, so the omission is a vacuous pass rather than noise.
func TestAClosureParameterIsABodyLocal(t *testing.T) {
	file, err := emitscan.Parse(queryPath, iterEmission)
	require.NoError(t, err)

	require.Contains(t, emitscan.BodyLocals(file), "yield",
		"a name bound by a closure parameter is not reported as a body local, so a rename sweep over body locals never sees it")
	require.Contains(t, emitscan.BodyLocals(file), "row",
		"an ordinary body local is missing, so this test is not reading the set it claims to")
}

// genericHelperEmission is the one construct gqlc-db0e closed that is
// LIVE rather than scheduled: the signature the age emitter writes at
// internal/codegen/age/render_models.go:1515, inside the raw string
// opened at :1504. agtypeNullableElem, agtypeProperty,
// agtypeNullableProperty and render_record.go's agtypeRecordField are
// the same shape. Copied rather than generated here so the unit stays a
// unit; if the emitter drops the form entirely this exhibit outlives it,
// which costs a row rather than hiding one.
const genericHelperEmission = `package p

func agtypeList[T any](raw []byte, decode func([]byte) (T, error)) ([]T, error) {
	out := make([]T, 0)
	value, err := decode(raw)
	if err != nil {
		return nil, err
	}
	return append(out, value), nil
}
`

// TestALiveGenericHelperBindsItsTypeParameter is the live half of
// gqlc-db0e. Every other construct that bead closed is reachable only
// from an emission nobody writes yet; this one ships today, so `T` was
// in a real emitted function's free set until the bound set grew
// fn.Type.TypeParams.
func TestALiveGenericHelperBindsItsTypeParameter(t *testing.T) {
	file, err := emitscan.Parse(queryPath, genericHelperEmission)
	require.NoError(t, err)

	free := emitscan.FreeIdents(onlyFuncDecl(t, file))

	require.NotContains(t, free, "T",
		"a type parameter of a shipping emitted helper is reported free, so the analysis errs towards calling a name UNbound on a construct that is in the tree now")
	require.Contains(t, free, "make",
		"a universe name the body resolves is missing, so this test is not reading the set it claims to")
}

// TestNonScopeOccurrencesAreExcludedPerOccurrence is what distinguishes
// gqlc-db0e's verdict from the alternative it declined.
//
// Three of the six constructs that bead closed write a name that binds
// nothing the body can read: a field name at its declaration site, a
// statement label, and a parameter name in a func TYPE. Either remedy
// empties the free set of that name, so TestFreeIdentsBoundSetLimits
// cannot tell them apart — but they differ on the source below, where
// the SAME name is also a genuine read of a package-level declaration.
// An exclusion is per occurrence and leaves the read free; adding the
// name to FreeIdents' bound set is keyed by name, applies to the whole
// function, and would swallow it. Swallowing it is the direction a
// capture slips through the sweep.
//
// So these rows do not fail before the fix — they failed under the
// remedy that was not taken, and that is what they are here to hold.
// What would have to change for the verdict to change: a caller that
// wanted the label or field namespace, which would need a set of its
// own rather than this one.
func TestNonScopeOccurrencesAreExcludedPerOccurrence(t *testing.T) {
	for _, tc := range []struct {
		name string
		read string
		src  string
	}{
		{
			name: "a field name declared locally does not bind the same name elsewhere",
			read: "col",
			src:  "func f() { type row struct{ col int }; var r row; _ = r.col; _ = col }",
		},
		{
			name: "a label does not bind the same name elsewhere",
			read: "Loop",
			src:  "func f() { Loop: for { break Loop }; _ = Loop }",
		},
		{
			name: "a func-type parameter name does not bind the same name elsewhere",
			read: "yield",
			src:  "func f(g func(yield int)) { _ = g; _ = yield }",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			file, err := emitscan.Parse(queryPath, "package p\n\n"+tc.src+"\n")
			require.NoError(t, err)

			require.Contains(t, emitscan.FreeIdents(onlyFuncDecl(t, file)), tc.read,
				"a genuine read of a package-level name is not reported free, because an occurrence that binds nothing was recorded as binding it for the whole function; the resolved set now loses a name the body really does resolve")
		})
	}
}

// TestFreeIdentsBoundSetLimits holds the enumeration in FreeIdents' doc
// comment: every construct that introduces a name inside a function, and
// the claim that none of them is reported free.
//
// It arrived under gqlc-9hrh in two halves — ten rows asserting a name
// was bound and six asserting it was NOT, the second half being the
// measured record of where the analysis erred the direction the doc
// comment forbids. gqlc-db0e closed those six, so the table is now one
// half, and a row that flips is a regression rather than a comment that
// has gone stale.
//
// Why a table rather than a list of positive cases: the six rows that
// once read the other way were found by enumerating constructs, not by
// noticing a bug, and the enumeration is the only thing that makes the
// doc comment's claim checkable. A construct added to the claim without
// a row here is a claim nobody measured.
func TestFreeIdentsBoundSetLimits(t *testing.T) {
	for _, tc := range []struct {
		name  string
		bound string
		src   string
	}{
		{name: "closure parameter", bound: "yield", src: "func f() any { return func(yield int) { _ = yield } }"},
		{name: "closure named result", bound: "cnt", src: "func f() any { return func() (cnt int) { _ = cnt; return } }"},
		{name: "short variable declaration", bound: "v", src: "func f() { v := 1; _ = v }"},
		{name: "var declaration", bound: "v", src: "func f() { var v int; _ = v }"},
		{name: "const declaration", bound: "v", src: "func f() { const v = 1; _ = v }"},
		{name: "range clause", bound: "v", src: "func f(xs []int) { for _, v := range xs { _ = v } }"},
		{name: "type switch binding", bound: "v", src: "func f(x any) { switch v := x.(type) { default: _ = v } }"},
		{name: "select comm clause", bound: "v", src: "func f(c chan int) { select { case v := <-c: _ = v } }"},
		{name: "own parameter", bound: "p", src: "func f(p int) { _ = p }"},
		{name: "own named result", bound: "r", src: "func f() (r int) { _ = r; return }"},

		// The six gqlc-db0e closed. Each was measured reporting its name
		// FREE before that bead; the four below that are not a plain
		// bound-set widening carry their verdict at the row.
		{name: "local type declaration", bound: "row", src: "func f() { type row struct{}; var r row; _ = r }"},

		// A FIELD name is not in the function's scope at all, so it is
		// excluded from ReferencedIdents rather than added to the bound
		// set — the same verdict, and for the same reason, as the
		// selector suffix in `r.col`, which that function has always
		// excluded. Binding it instead would also empty this row, which
		// is why the row cannot distinguish the two; the row that can is
		// in TestNonScopeOccurrencesAreExcludedPerOccurrence.
		{name: "local type field name", bound: "col", src: "func f() { type row struct{ col int }; var r row; _ = r.col }"},

		// A label lives in its own namespace in Go: `Loop:` cannot shadow
		// a package-level `Loop`, and `break Loop` does not resolve one.
		// So a label is not a reference, and it is excluded rather than
		// bound. This would have to change if FreeIdents ever grew a
		// caller that wanted the label namespace — nothing would then be
		// reading it, and it would need its own set rather than this one.
		{name: "statement label", bound: "Loop", src: "func f() { Loop: for { break Loop } }"},

		// A parameter name written into a func TYPE binds nothing: it is
		// documentation on the type, and the body cannot read it. Not a
		// reference either, so excluded rather than bound — the bead
		// (gqlc-db0e) reached the same verdict from the same premise.
		{name: "func-type parameter name in a signature", bound: "yield", src: "func f(g func(yield int)) { _ = g }"},

		// A type parameter IS in the function's scope and the body's `T`
		// genuinely resolves it, so these two are the bound-set widening
		// the bead names, not an exclusion.
		{name: "type parameter", bound: "T", src: "func f[T any](x T) { var y T; _ = y; _ = x }"},
		{name: "generic receiver type parameter", bound: "T", src: "func (q *Q[T]) f(x T) { _ = x }"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			file, err := emitscan.Parse(queryPath, "package p\n\n"+tc.src+"\n")
			require.NoError(t, err)

			free := emitscan.FreeIdents(onlyFuncDecl(t, file))

			require.NotContains(t, free, tc.bound,
				"the doc comment claims this construct's name is not reported free; it is, which is the direction a capture slips through the sweep rather than failing it")
		})
	}
}
