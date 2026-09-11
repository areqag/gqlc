package age_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/areqag/gqlc/internal/codegen"
	"github.com/areqag/gqlc/internal/codegen/age"
	"github.com/areqag/gqlc/internal/graph"
	"github.com/areqag/gqlc/internal/schema"
)

// The fence over generate's render phase: a codegen bug raised while
// rendering becomes generate's own refusal, and a fault of any other
// species still takes the caller.
//
// WHY THE TRIGGER IS A HOOK AND NOT A FIXTURE. The fault this seam converts
// is decodeFunc meeting a carrier it has no arm for, and no assembled
// codegen.Input reaches it: the carrier set is a property of the backend's
// type table, so producing an untaught one means editing the table, not
// writing a schema or a query. A guard with no reachable trigger is a guard
// nobody can falsify — delete generate's recover and, without the hook,
// nothing in this package reddens. That is the exact disease bd gqlc-h0vqx
// is about, so the trigger is scaffolded rather than left absent.
//
// WHAT PINS THE OTHER HALF. These rows pin the recover. That decodeFunc
// raises the type the recover looks for is pinned separately, by
// TestDecodeFuncRefusesACarrierItWasNotTaught's PanicsWithValue rows, which
// compare a typed age.CodegenBug. Neither half is sufficient: a panic
// re-spelled as a bare string would satisfy these rows and still cross the
// seam, and a recover deleted here would satisfy those.
//
// NEITHER ROW CALLS t.Parallel, and that is load-bearing rather than
// oversight. The hook is package-level state, so a Generate call overlapping
// one of these rows would meet a fault meant for this test. Go resumes a
// paused parallel test only after the sequential phase it was declared in
// has finished, so a non-parallel row here cannot overlap one of the
// package's 21 parallel tests; making these rows parallel would break that.

// faultProbeInput is a query-free batch. Nothing below reads the emitted
// bytes — only whether the render phase was entered and how it ended — so
// the smallest schema that survives validation is the right one.
func faultProbeInput() codegen.Input {
	return codegen.Input{Schema: schema.Schema{Name: "Probe"}}
}

// withRenderFault arms generate's render-phase fault hook for one row and
// restores it, on the panicking path too.
func withRenderFault(t *testing.T, fault func()) {
	t.Helper()
	prev := *age.TestRenderFault
	*age.TestRenderFault = fault
	t.Cleanup(func() { *age.TestRenderFault = prev })
}

// TestGenerateConvertsACodegenBugPanicIntoItsOwnRefusal is the standing
// witness for the recover. Deleting the defer in generate reddens this row
// by killing the test binary — which is the failure mode this whole seam
// exists to end, so the row is written to be read that way: a binary that
// dies here is the guard gone, not a flake.
func TestGenerateConvertsACodegenBugPanicIntoItsOwnRefusal(t *testing.T) {
	const sentence = "age codegen bug: probe"
	withRenderFault(t, func() { panic(age.CodegenBug(sentence)) })

	files, err := age.New().Generate(faultProbeInput())

	require.Error(t, err, "a codegen bug in the render phase must refuse the batch")
	// The panic's own sentence, byte for byte: this is what the CLI puts
	// in front of an author, and it is the only thing naming which
	// carrier lost its arm.
	require.EqualError(t, err, sentence)
	// Generate's documented contract is (nil, err) on failure, never a
	// partial slice, and this is a live assertion rather than a
	// restatement: the hook fires with four files already in the slice
	// and the per-source emission still to come, so a recover that set
	// only err would return those four beside the error. That is the
	// shape a real fault takes — renderCypherFile reaches decodeFunc and
	// runs inside the append loop.
	require.Nil(t, files, "a refused batch must emit nothing, not the files built before the fault")
}

// TestGenerateRepanicsAFaultThatIsNotItsOwn pins the type check inside the
// recover. Dropping it would convert every panic crossing generate into an
// error, which would silently swallow faults this package deliberately lets
// through — unserved_nil_type_test.go's generateUnserved takes no recover of
// its own precisely so that a fault inside the render surfaces as that
// test's own panic, naming the site.
//
// require.PanicsWithValue's own recover is what contains the re-panic, so
// the binary survives this row.
func TestGenerateRepanicsAFaultThatIsNotItsOwn(t *testing.T) {
	// A bare string, not a CodegenBug: the species differ by TYPE alone,
	// which is what makes this row fail if the recover compares text or
	// stops checking.
	const foreign = "age codegen bug: probe"
	withRenderFault(t, func() { panic(foreign) })

	var (
		files []codegen.File
		err   error
	)
	require.PanicsWithValue(t, foreign, func() {
		files, err = age.New().Generate(faultProbeInput())
	})
	// Neither return was ever assigned, because generate did not return.
	// This is what separates a re-panic from a recover that converted the
	// fault and then panicked anyway.
	require.Nil(t, files)
	require.NoError(t, err)
}

// bareRenderOutcome is how a call through a fenced render bridge ended.
// The call runs on its own goroutine because the fence ends it with
// runtime.Goexit, which terminates whichever goroutine makes the call — on
// the test's own goroutine that is the failure it is meant to be, and there
// would be nothing left to assert from.
type bareRenderOutcome struct {
	returned bool // the renderer ran to completion
	panicked any  // a fault crossed the bridge unhandled
}

// callBareRender runs fn on its own goroutine and reports how it ended.
func callBareRender(fn func()) bareRenderOutcome {
	var out bareRenderOutcome
	done := make(chan struct{})
	go func() {
		// Runs on the Goexit path too, which is what makes the
		// abandoned case observable rather than a hang.
		defer close(done)
		defer func() { out.panicked = recover() }()
		fn()
		out.returned = true
	}()
	<-done
	return out
}

// untaughtField is an entity field whose Go type no arm of decodeFunc
// answers. complex128 is not a carrier this backend's type table can
// produce, which is the point: it stands in for a carrier whose arm was
// deleted, without this test having to edit the table.
func untaughtField() codegen.EntityField {
	return codegen.EntityField{PropName: "p", Field: "P", GoType: "complex128"}
}

// TestABareRenderCallFailsOnlyItsOwnTest is the standing witness for the
// export_test render bridges being fenced, and it is the row that would
// have caught what generate's recover alone did not: twelve tests call the
// render layer directly, below generate's seam, and a codegen bug reaching
// one of them used to take the whole binary.
//
// Two ways to break the fence, one assertion each. Rebind the bridge to the
// bare renderer and the fault arrives here as panicked. Change the fence to
// swallow and return a zero value and it arrives as returned — which would
// be worse than the panic, since a site asserting the ABSENCE of something
// would then pass on empty output.
func TestABareRenderCallFailsOnlyItsOwnTest(t *testing.T) {
	f := untaughtField()
	e := codegen.Entity{Name: "E", Kind: codegen.EntityNode, Fields: []codegen.EntityField{f}}

	var b strings.Builder
	out := callBareRender(func() { age.WriteEntityFieldDecode(&b, e, 0, f) })

	require.Nil(t, out.panicked, "a codegen bug must not cross the bridge as a panic — it takes the binary, and with it every pin after it")
	require.False(t, out.returned, "the fence must abandon the call, not return a zero value a caller could assert against")
}

// TestAFaultThatIsNotACodegenBugStillCrossesTheRenderBridge is the other
// half: the render fence is as selective as generate's, so an ordinary bug
// in a renderer still reaches the caller's own stack and names its site.
// unserved_nil_type_test.go depends on that, and a fence that caught
// everything would swallow it.
func TestAFaultThatIsNotACodegenBugStillCrossesTheRenderBridge(t *testing.T) {
	// A SERVED carrier with a nil builder. The carrier has to be served:
	// an empty EntityField carries the Go type "", which decodeFunc
	// refuses as a codegen bug before the builder is ever written to, so
	// that probe would measure the arm above a second time rather than
	// this one.
	f := codegen.EntityField{PropName: "p", Field: "P", GoType: "string", Width: graph.TypeString}
	e := codegen.Entity{Name: "E", Kind: codegen.EntityNode, Fields: []codegen.EntityField{f}}

	out := callBareRender(func() { age.WriteEntityFieldDecode(nil, e, 0, f) })

	require.NotNil(t, out.panicked, "a fault that is not a codegen bug must reach the caller unchanged")
	require.False(t, out.returned)
}

// TestAFencedBridgeIsTransparentWhenNothingFaults is the negative control
// for the two rows above: a served carrier renders through the same fenced
// bridge and returns normally, so their verdicts are the fence acting and
// not the bridge being broken for every input.
func TestAFencedBridgeIsTransparentWhenNothingFaults(t *testing.T) {
	f := codegen.EntityField{PropName: "p", Field: "P", GoType: "string", Width: graph.TypeString}
	e := codegen.Entity{Name: "E", Kind: codegen.EntityNode, Fields: []codegen.EntityField{f}}

	var b strings.Builder
	out := callBareRender(func() { age.WriteEntityFieldDecode(&b, e, 0, f) })

	require.Nil(t, out.panicked)
	require.True(t, out.returned, "a served carrier must render through the bridge untouched")
	require.NotEmpty(t, b.String(), "the control must actually emit, or it screens nothing")
}

// TestGenerateWithNoRenderFaultIsUnaffected is the negative control for the
// two rows above: it asserts the hook is inert when unarmed, so a pass there
// is the recover doing the work and not the probe input failing on its own.
func TestGenerateWithNoRenderFaultIsUnaffected(t *testing.T) {
	require.Nil(t, *age.TestRenderFault, "the fault hook must be nil unless a row arms it")

	files, err := age.New().Generate(faultProbeInput())

	require.NoError(t, err)
	require.NotEmpty(t, files, "the probe input must reach the render phase, or the rows above prove nothing")
}
