package age

// The second half of the codegen-bug fence, for the callers generate does
// not sit above.
//
// generate recovers a codegenBug at its own boundary, which covers every
// production run and every one of this package's 69 .Generate( test call
// sites. It does not cover a test that enters the RENDER LAYER directly:
// twelve sites call age.RenderModels, age.RenderCypherFile or
// age.WriteEntityFieldDecode through the export_test bridge, below
// generate's seam, and each of those still took the whole binary. That is
// what the acceptance row for bd gqlc-h0vqx measured — with generate's
// recover alone, deleting decodeFunc's `any` arm still died in
// TestImportsTimeAgreesWithTheEmittedFile at a bare age.RenderModels, 544
// of 9834 RUN lines reached and the pin whose job is to NAME the lost
// carrier never running.
//
// WHY THE BRIDGE AND NOT THE TWELVE SITES. Wrapping the sites is the sweep
// this bead rejected: correct the day it is written, and one blind spot
// wider with the next test that calls a renderer. The bridge is the only
// route from package age_test into the render layer — the renderers
// themselves are unexported — so fencing it covers the twelve sites that
// exist and every one written later, by construction rather than by anyone
// remembering. No call site changes, now or ever.
//
// WHY NOT runtime.Goexit, which is what a helper would use to fail one
// test. Goexit is what testing itself calls under t.FailNow, but only
// after bookkeeping this code cannot do without a *testing.T: a bare
// Goexit out of a test goroutine makes tRunner panic with "test executed
// panic(nil) or runtime.Goexit", which is unrecovered and takes the binary
// — the very failure being fenced, wearing a different message. Measured
// both ways while building this: called from inside the deferred recover
// it re-raised the panic it had just caught ("panic: ... [recovered]"),
// and called cleanly after it, it died in tRunner instead. Failing exactly
// one test needs that test's own t, which a bridge binding does not have.
//
// SO THE FENCE RECORDS AND RETURNS THE ZERO VALUE, and the caller fails on
// its own assertions. THE LIMIT OF THAT, stated rather than left to be
// discovered: a site whose assertion is SATISFIED BY EMPTY OUTPUT is not a
// witness. That is wider than the obvious NotContains/NotRegexp — it also
// takes an equality whose expected side is itself computed from the
// rendered bytes. Measured on the acceptance row rather than reasoned:
// with decodeFunc's `any` arm deleted,
// TestImportsTimeAgreesWithTheEmittedFile PASSES, because it compares
// importsTime() against whether the rendered source spells a time
// qualifier, and an empty render spells none while that carrier wants
// none either. That site is the one that used to take the whole binary,
// so this is still the better trade — but it is not a witness and must
// not be read as one.
//
// What does report: every assertion that needs bytes fails, the fault is
// named on stderr against the running test, and the pin whose job is to
// name the carrier
// (TestDecodeFuncHasAnArmForEveryCarrierTheTypeTableProduces) reaches its
// own subtests and fails there, which is the property the bead asked for.
//
// NOT FENCED, deliberately: age.DecodeFunc and age.Generate stay bound to
// the bare functions. DecodeFunc's panic is pinned by
// TestDecodeFuncRefusesACarrierItWasNotTaught through PanicsWithValue, and
// fencing it would swallow the value that test exists to read; Generate
// already converts the panic into an error of its own.

import (
	"fmt"
	"os"
	"sync"
)

// renderFaults is the fence's record of every codegen bug that reached a
// bare render call. Guarded because this package runs 21 parallel tests and
// any of them may render.
var renderFaults struct {
	sync.Mutex
	seen []string
}

// RecordedRenderFaults is the external test package's view of that record.
// It spells only builtin types, so binding it costs export_test.go's
// zero-import rule nothing.
func RecordedRenderFaults() []string {
	renderFaults.Lock()
	defer renderFaults.Unlock()
	return append([]string(nil), renderFaults.seen...)
}

// ResetRecordedRenderFaults drops the record, so a test that provokes a
// fault on purpose does not leave one behind for a later reader.
func ResetRecordedRenderFaults() {
	renderFaults.Lock()
	defer renderFaults.Unlock()
	renderFaults.seen = nil
}

// fenced runs call and converts a codegenBug it raises into a recorded,
// reported fault, leaving the binary alive for every test after this one.
//
// THE TWO STAGES ARE NOT A STYLE CHOICE. The recover completes and the
// inner function RETURNS before anything else happens, because acting on
// the fault from inside the deferred function that caught it is what
// re-raises it.
//
// A fault that is not a codegenBug is re-panicked from inside the defer,
// which is the ordinary re-raise and is safe. That keeps this fence as
// selective as generate's, so a nil dereference in a renderer still reaches
// the test's own stack and names its site — unserved_nil_type_test.go
// depends on exactly that.
func fenced(call func()) {
	var bug codegenBug
	caught := func() (caught bool) {
		defer func() {
			v := recover()
			if v == nil {
				return
			}
			cb, ok := v.(codegenBug)
			if !ok {
				panic(v)
			}
			bug, caught = cb, true
		}()
		call()
		return false
	}()
	if !caught {
		return
	}
	renderFaults.Lock()
	renderFaults.seen = append(renderFaults.seen, string(bug))
	renderFaults.Unlock()
	// Stderr rather than a returned error: there is no t here and no
	// error in these signatures. go test attributes this to the running
	// test, and it is where the lost carrier is NAMED for a reader whose
	// test failed on an empty render.
	fmt.Fprintf(os.Stderr, "\ncodegen bug reached a bare render call: %s\n", string(bug))
}

// fencedBytes2 and fencedBytes3 wrap a renderer returning []byte;
// fencedVoid4 wraps one that writes into a builder and returns nothing.
//
// Every parameter is a type parameter and the only named type is the
// builtin []byte, which is what lets export_test.go bind these without
// spelling — and therefore without importing — codegen or strings. That
// file's zero-import rule is load-bearing for vuln-root-residual; this file
// imports stdlib only, which the guard's own third-party test (a dot in the
// first path segment) does not match.
func fencedBytes2[A, B any](f func(A, B) []byte) func(A, B) []byte {
	return func(a A, b B) []byte {
		var out []byte
		fenced(func() { out = f(a, b) })
		return out
	}
}

func fencedBytes3[A, B, C any](f func(A, B, C) []byte) func(A, B, C) []byte {
	return func(a A, b B, c C) []byte {
		var out []byte
		fenced(func() { out = f(a, b, c) })
		return out
	}
}

func fencedVoid4[A, B, C, D any](f func(A, B, C, D)) func(A, B, C, D) {
	return func(a A, b B, c C, d D) {
		fenced(func() { f(a, b, c, d) })
	}
}
