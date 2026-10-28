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
//   - Guard does not interrupt the call it times. A call that never returns is
//     never reported by Guard; -fuzztime and the job's timeout are what end it.
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

// Guard runs fn on the calling goroutine and t.Fatalf's, naming the elapsed
// time, when it takes longer than limit. It does not interrupt fn: the report
// comes after fn returns, so the engine records the input as a crasher.
func Guard(t *testing.T, limit time.Duration, fn func()) {
	t.Helper()
	start := time.Now()
	fn()
	if elapsed := time.Since(start); elapsed > limit {
		t.Fatalf("fuzzkit.Guard: slow input: the call took %s, over the %s limit", elapsed, limit)
	}
}

// ErrClass is the class ClassifyError assigns to an error.
type ErrClass int

// The classes, in the order ClassifyError tests for them.
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

// ClassifyError reads err as: nil → ClassNil; errors.Is any of sentinels →
// ClassSentinel; isSyntax(err) → ClassSyntax; text containing "bug:" →
// ClassBug; else ClassPlain. The first match wins, so an error that wraps a
// sentinel AND carries "bug:" is ClassSentinel.
//
// isSyntax is the caller's because what a syntax error looks like is each
// parser's own. A nil isSyntax matches nothing.
func ClassifyError(err error, isSyntax func(error) bool, sentinels ...error) ErrClass {
	if err == nil {
		return ClassNil
	}
	for _, s := range sentinels {
		if errors.Is(err, s) {
			return ClassSentinel
		}
	}
	if isSyntax != nil && isSyntax(err) {
		return ClassSyntax
	}
	if strings.Contains(err.Error(), "bug:") {
		return ClassBug
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
	if msg := seed(globs, func(s string) { f.Add(s) }); msg != "" {
		f.Fatal(msg)
	}
}

// seed is SeedFiles without the *testing.F, so its refusals can be tested: a
// *testing.F cannot be built outside a fuzz target, and a second fuzz target
// would be one more name for `just fuzz-list` to discover. It returns the
// refusal, or "" once every glob has added at least one file.
func seed(globs []string, add func(string)) string {
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
			add(string(b))
			n++
		}
		if n == 0 {
			return fmt.Sprintf("fuzzkit.SeedFiles: glob %q matched no file, so it seeds nothing", g)
		}
	}
	return ""
}
