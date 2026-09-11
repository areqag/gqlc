package age

// The second half of the codegen-bug fence, for the callers generate does
// not sit above.
//
// generate recovers a codegenBug at its own boundary, which covers every
// production run and every one of this package's 66 .Generate( test call
// sites. It does not cover a test that enters the RENDER LAYER directly:
// twelve sites call age.RenderModels, age.RenderCypherFile or
// age.WriteEntityFieldDecode through the export_test bridge, below
// generate's seam, and each of those still took the whole binary. That is
// what the acceptance row for bd gqlc-h0vqx measured — delete decodeFunc's
// `any` arm and the run died in TestImportsTimeAgreesWithTheEmittedFile at
// a bare age.RenderModels, with 544 of 9834 tests reached and the pin whose
// job is to NAME the lost carrier never running.
//
// WHY THE BRIDGE AND NOT THE TWELVE SITES. Wrapping the sites is the sweep
// this bead rejected: it is correct on the day it is written and grows a
// blind spot with the next test that calls a renderer. The bridge is the
// only route from package age_test into the render layer — the renderers
// themselves are unexported — so fencing it covers the twelve sites that
// exist and every one written later, by construction rather than by anyone
// remembering. No call site changes, now or ever.
//
// WHY Goexit AND NOT AN ERROR RETURN. These renderers write into a
// *strings.Builder and return bytes; there is no error in their contract to
// widen, and widening it would be the ~20-function plumbing change the
// design (bd gqlc-qi4st) rejected on cost. runtime.Goexit fails exactly the
// test that made the call, runs its defers, and leaves the binary alive for
// every test after it — which is the whole property this bead is about. It
// fails the test rather than returning a zero value, so a site that asserts
// the ABSENCE of something cannot pass on empty output.
//
// NOT FENCED, deliberately: age.DecodeFunc and age.Generate stay bound to
// the bare functions. DecodeFunc's panic is pinned by
// TestDecodeFuncRefusesACarrierItWasNotTaught through PanicsWithValue, and
// fencing it would swallow the value that test exists to read; Generate
// already converts the panic into an error of its own.

import (
	"fmt"
	"os"
	"runtime"
)

// renderFault handles a panic that crossed a fenced render bridge. A fault
// that is not a codegenBug is re-panicked unchanged: this fence is as
// selective as generate's, so a nil-map dereference in a renderer still
// reaches the test's own stack and names its site.
func renderFault(v any) {
	bug, ok := v.(codegenBug)
	if !ok {
		panic(v)
	}
	// Stderr rather than a returned error because there is no t here and
	// no error in the signature. go test attributes this to the running
	// test, and it is the only place the lost carrier is NAMED — the
	// failure testing itself reports for a Goexit does not carry a
	// reason.
	fmt.Fprintf(os.Stderr, "\ncodegen bug reached a bare render call: %s\n", string(bug))
	// Fails this test and only this test. Deferred calls still run.
	runtime.Goexit()
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
		defer func() {
			if v := recover(); v != nil {
				renderFault(v)
			}
		}()
		return f(a, b)
	}
}

func fencedBytes3[A, B, C any](f func(A, B, C) []byte) func(A, B, C) []byte {
	return func(a A, b B, c C) []byte {
		defer func() {
			if v := recover(); v != nil {
				renderFault(v)
			}
		}()
		return f(a, b, c)
	}
}

func fencedVoid4[A, B, C, D any](f func(A, B, C, D)) func(A, B, C, D) {
	return func(a A, b B, c C, d D) {
		defer func() {
			if v := recover(); v != nil {
				renderFault(v)
			}
		}()
		f(a, b, c, d)
	}
}
