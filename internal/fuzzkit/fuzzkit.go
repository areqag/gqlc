// Package fuzzkit holds the rules every fuzz target in this repository shares,
// so that no target re-implements them: the input size cap, the slow-input
// detector, error classification, the byte stream structured generators read
// their decisions from, and corpus seeding. The plan that fixes this surface is
// docs/specs/nightly-fuzzing-plan.md (§5, §6.1); other packages are written
// against it, so a signature here moves only when that document does.
//
// It is a non-test package that imports "testing", and it is meant to be
// imported from _test.go files only.
//
// What it does not do:
//   - Guard does not interrupt the call it times; it reports it. A call that
//     returns late fails the test. A call still running at WatchdogFactor
//     times the limit ends the whole process, which is the only way to stop a
//     goroutine Go offers, so a test binary running seeds dies with it and a
//     fuzzing worker dies with the engine holding the input.
//   - Skip drops inputs over MaxInput, so a defect that needs a larger input to
//     show is not found by any target that calls it.
//   - Determinism (§5) is not a helper here: what "the same result" means is
//     each target's own — reflect.DeepEqual for a model, byte equality for
//     emitted files — so each target compares its two calls itself.
package fuzzkit

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// MaxInput is the largest input, in bytes, a target examines.
const MaxInput = 256 << 10

// Skip calls t.Skip when len(b) > MaxInput.
func Skip(t *testing.T, b []byte) {
	t.Helper()
	if len(b) > MaxInput {
		t.Skipf("fuzzkit: input is %d bytes, over the %d-byte cap", len(b), MaxInput)
	}
}

// WatchdogFactor is how many times its limit Guard lets a call run before it
// stops waiting for the call to return.
const WatchdogFactor = 5

// Guard runs fn on the calling goroutine and t.Fatalf's, naming the elapsed
// time, when it takes longer than limit; the engine then records the input as a
// crasher.
//
// A call that never returns would never reach that report: measured
// 2026-10-27, a worker spinning on one input left `go test -fuzz` green at
// -fuzztime with no crasher. So if fn is still running at WatchdogFactor ×
// limit, a watchdog panics on its own goroutine, which ends the process. The
// fuzzing engine reports that as a worker that terminated unexpectedly and
// writes the input it was running as a crasher; outside fuzzing it is a
// failed test binary naming t.Name().
func Guard(t *testing.T, limit time.Duration, fn func()) {
	t.Helper()
	name := t.Name()
	watchdog := time.AfterFunc(WatchdogFactor*limit, func() {
		panic(fmt.Sprintf("fuzzkit.Guard: watchdog: %s has not returned after %s (%d× its %s limit); ending the process so the input is recorded",
			name, WatchdogFactor*limit, WatchdogFactor, limit))
	})
	// Deferred, not called after fn: a t.Fatal or a recovered panic inside fn
	// leaves Guard without returning normally, and a timer still armed then
	// kills the process later with a false "has not returned".
	defer watchdog.Stop()
	start := time.Now()
	fn()
	if elapsed := time.Since(start); elapsed > limit {
		t.Fatalf("fuzzkit.Guard: slow input: the call took %s, over the %s limit", elapsed, limit)
	}
}

// ErrClass is the class ClassifyError assigns to an error.
type ErrClass int

// The classes. ClassifyError tests them in the order its doc gives, which is
// not this order.
const (
	ClassNil ErrClass = iota
	ClassSentinel
	ClassSyntax
	ClassBug
	ClassPlain
)

func (c ErrClass) String() string {
	switch c {
	case ClassNil:
		return "nil"
	case ClassSentinel:
		return "sentinel"
	case ClassSyntax:
		return "syntax"
	case ClassBug:
		return "bug"
	case ClassPlain:
		return "plain"
	default:
		return "ErrClass(?)"
	}
}

// ClassifyError reads err as: nil → ClassNil; text containing "bug:" →
// ClassBug; errors.Is any of sentinels → ClassSentinel; isSyntax(err) →
// ClassSyntax; else ClassPlain. The first match wins, so an internal bug a
// fail site wrapped in a sentinel (a gofmt failure under
// codegen.ErrFormatFailure, say) is ClassBug, not a sentinel the target
// accepts.
//
// isSyntax is the caller's because what a syntax error looks like is each
// parser's own. A nil isSyntax matches nothing.
//
// A call that quotes its input in its messages can echo a "bug:" no fail site
// wrote; ClassifyInputError is this function for such a call.
func ClassifyError(err error, isSyntax func(error) bool, sentinels ...error) ErrClass {
	return classify(err, true, isSyntax, sentinels)
}

// ClassifyInputError is ClassifyError for an error produced from input, by a
// call that may quote input in its messages: a parser naming the offending
// token, a resolver naming a variable or a property. When input itself
// contains "bug:", an error carrying the marker may only be echoing it, so the
// marker is not read and the error is classified by the remaining tests.
//
// What that misses: a fail site's own "bug:" on an input that spells the
// marker. The cost is confined to such inputs; on every other input this is
// ClassifyError exactly.
func ClassifyInputError(input string, err error, isSyntax func(error) bool, sentinels ...error) ErrClass {
	return classify(err, !strings.Contains(input, bugMarker), isSyntax, sentinels)
}

const bugMarker = "bug:"

func classify(err error, readMarker bool, isSyntax func(error) bool, sentinels []error) ErrClass {
	if err == nil {
		return ClassNil
	}
	if readMarker && strings.Contains(err.Error(), bugMarker) {
		return ClassBug
	}
	for _, s := range sentinels {
		if errors.Is(err, s) {
			return ClassSentinel
		}
	}
	if isSyntax != nil && isSyntax(err) {
		return ClassSyntax
	}
	return ClassPlain
}

// SeedFiles adds every regular file matching the globs (relative to the test's
// cwd, filepath.Glob syntax, so no `**`) to f with f.Add(string(contents)).
//
// It fails f when any one glob matches no regular file, not only when all of
// them together match nothing: a target seeded from three directories, one of
// which moved, would otherwise go on seeding from two and say nothing.
func SeedFiles(f *testing.F, globs ...string) {
	f.Helper()
	if msg := seed(globs, func(b []byte) { f.Add(string(b)) }); msg != "" {
		f.Fatal(msg)
	}
}

// SeedFilesBytes is SeedFiles for a target whose input is []byte: it adds
// each file with f.Add(contents), under the same refusals.
func SeedFilesBytes(f *testing.F, globs ...string) {
	f.Helper()
	if msg := seed(globs, func(b []byte) { f.Add(b) }); msg != "" {
		f.Fatal(msg)
	}
}

// seed is SeedFiles and SeedFilesBytes without the *testing.F, so its refusals can be tested: a
// *testing.F cannot be built outside a fuzz target, and a second fuzz target
// would be one more name for `just fuzz-list` to discover. It returns the
// refusal, or "" once every glob has added at least one file.
func seed(globs []string, add func([]byte)) string {
	if len(globs) == 0 {
		return "fuzzkit.SeedFiles: no glob given, so nothing would be seeded"
	}
	for _, g := range globs {
		paths, err := filepath.Glob(g)
		if err != nil {
			return fmt.Sprintf("fuzzkit.SeedFiles: glob %q: %v", g, err)
		}
		n := 0
		for _, p := range paths {
			if info, err := os.Stat(p); err != nil || !info.Mode().IsRegular() {
				continue
			}
			b, err := os.ReadFile(p)
			if err != nil {
				return fmt.Sprintf("fuzzkit.SeedFiles: %v", err)
			}
			add(b)
			n++
		}
		if n == 0 {
			return fmt.Sprintf("fuzzkit.SeedFiles: glob %q matched no file, so it seeds nothing", g)
		}
	}
	return ""
}
