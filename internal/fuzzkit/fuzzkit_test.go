package fuzzkit_test

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/areqag/gqlc/internal/fuzzkit"
)

// helperEnv selects a scenario when this binary is re-executed by a test
// below. Guard and Skip act on a *testing.T, and the only way to watch one
// fail or skip without failing the test doing the watching is to run it in a
// child process and read what the child reports.
const helperEnv = "GQLC_FUZZKIT_HELPER"

func TestHelperProcess(t *testing.T) {
	switch os.Getenv(helperEnv) {
	case "":
		t.Skip("run only as a child of the tests below")
	case "guard-slow":
		fuzzkit.Guard(t, 20*time.Millisecond, func() { time.Sleep(50 * time.Millisecond) })
		t.Log("REACHED: Guard returned")
	case "guard-fatal-then-later":
		t.Run("fatal", func(t *testing.T) {
			fuzzkit.Guard(t, 20*time.Millisecond, func() { t.Fatal("planted fatal inside Guard") })
		})
		t.Run("later", func(t *testing.T) {
			time.Sleep(300 * time.Millisecond)
			t.Log("REACHED: later test")
		})
	case "guard-recovered-panic":
		func() {
			defer func() {
				if recover() == nil {
					t.Error("the planted panic did not reach the recover")
				}
			}()
			fuzzkit.Guard(t, 20*time.Millisecond, func() { panic("planted panic inside Guard") })
		}()
		time.Sleep(300 * time.Millisecond)
		t.Log("REACHED: after the recovered panic")
	case "guard-never":
		fuzzkit.Guard(t, 20*time.Millisecond, func() { select {} })
		t.Log("REACHED: Guard returned")
	case "guard-fast":
		fuzzkit.Guard(t, 2*time.Second, func() {})
		t.Log("REACHED: Guard returned")
	case "skip-over":
		fuzzkit.Skip(t, make([]byte, fuzzkit.MaxInput+1))
		t.Log("REACHED: Skip returned")
	case "skip-at":
		fuzzkit.Skip(t, make([]byte, fuzzkit.MaxInput))
		t.Log("REACHED: Skip returned")
	default:
		t.Fatalf("unknown scenario %q", os.Getenv(helperEnv))
	}
}

// child runs TestHelperProcess under scenario and returns its combined output
// and its exit error.
func child(t *testing.T, scenario string) (string, error) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestHelperProcess$", "-test.v", "-test.count=1")
	cmd.Env = append(os.Environ(), helperEnv+"="+scenario)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func TestGuardReportsASlowCall(t *testing.T) {
	out, err := child(t, "guard-slow")
	var exit *exec.ExitError
	if !errors.As(err, &exit) {
		t.Fatalf("child: want a non-zero exit, got %v\n%s", err, out)
	}
	if !strings.Contains(out, "fuzzkit.Guard: slow input: the call took ") {
		t.Fatalf("child failed, but not through Guard's Fatalf:\n%s", out)
	}
	if strings.Contains(out, "REACHED") {
		t.Fatalf("Guard reported the slow call but did not stop the test:\n%s", out)
	}
}

func TestGuardWatchdogEndsACallThatNeverReturns(t *testing.T) {
	out, err := child(t, "guard-never")
	var exit *exec.ExitError
	if !errors.As(err, &exit) {
		t.Fatalf("child: want a non-zero exit, got %v\n%s", err, out)
	}
	want := "fuzzkit.Guard: watchdog: TestHelperProcess has not returned after 100ms (5× its 20ms limit)"
	if !strings.Contains(out, want) {
		t.Fatalf("child died, but not through Guard's watchdog; want %q in:\n%s", want, out)
	}
}

// A Guard left by t.Fatal must disarm its watchdog: the later test sleeps past
// 5× the limit, and an armed timer would kill the binary under it.
func TestGuardDisarmsAfterFatal(t *testing.T) {
	out, err := child(t, "guard-fatal-then-later")
	var exit *exec.ExitError
	if !errors.As(err, &exit) || !strings.Contains(out, "--- FAIL: TestHelperProcess/fatal") {
		t.Fatalf("child: want the planted Fatal to fail its subtest, got %v\n%s", err, out)
	}
	if strings.Contains(out, "watchdog") || !strings.Contains(out, "REACHED: later test") ||
		!strings.Contains(out, "--- PASS: TestHelperProcess/later") {
		t.Fatalf("the watchdog outlived Guard's Fatal exit, or the later test did not report:\n%s", out)
	}
}

// Likewise a panic in fn that a caller recovers: the test passes, and stays
// passing past 5× the limit.
func TestGuardDisarmsAfterARecoveredPanic(t *testing.T) {
	out, err := child(t, "guard-recovered-panic")
	if err != nil || strings.Contains(out, "watchdog") || !strings.Contains(out, "REACHED: after the recovered panic") {
		t.Fatalf("child: want a pass with no watchdog, got %v\n%s", err, out)
	}
}

func TestGuardPassesAFastCall(t *testing.T) {
	out, err := child(t, "guard-fast")
	if err != nil || !strings.Contains(out, "REACHED: Guard returned") {
		t.Fatalf("child: want a pass that reaches past Guard, got %v\n%s", err, out)
	}
}

func TestSkipDropsOnlyInputsOverTheCap(t *testing.T) {
	out, err := child(t, "skip-over")
	if err != nil || !strings.Contains(out, "--- SKIP: TestHelperProcess") || strings.Contains(out, "REACHED") {
		t.Fatalf("over the cap: want a skip, got %v\n%s", err, out)
	}
	out, err = child(t, "skip-at")
	if err != nil || !strings.Contains(out, "REACHED: Skip returned") {
		t.Fatalf("at the cap: want no skip, got %v\n%s", err, out)
	}
}

var (
	errSentinel = errors.New("sentinel")
	errOther    = errors.New("other sentinel")
	errSyntax   = errors.New("syntax error at 1:1")
)

func isSyntax(err error) bool { return strings.Contains(err.Error(), "syntax error") }

func TestClassifyErrorCoversEveryClass(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want fuzzkit.ErrClass
	}{
		{"nil", nil, fuzzkit.ClassNil},
		{"sentinel, wrapped", fmt.Errorf("%w: detail", errSentinel), fuzzkit.ClassSentinel},
		{"second sentinel", errOther, fuzzkit.ClassSentinel},
		{"syntax", errSyntax, fuzzkit.ClassSyntax},
		{"bug", errors.New("age codegen bug: no arm"), fuzzkit.ClassBug},
		{"plain", errors.New("registry miss"), fuzzkit.ClassPlain},
		// The first match wins, in the documented order: a bug a fail site
		// wrapped in a sentinel, or one whose text a syntax predicate
		// matches, is still a bug.
		{"bug before sentinel", fmt.Errorf("%w: resolver bug: x", errSentinel), fuzzkit.ClassBug},
		{"bug before syntax", errors.New("syntax error near bug: x"), fuzzkit.ClassBug},
		{"sentinel before syntax", fmt.Errorf("%w: %w", errSentinel, errSyntax), fuzzkit.ClassSentinel},
	}
	for _, tc := range cases {
		if got := fuzzkit.ClassifyError(tc.err, isSyntax, errSentinel, errOther); got != tc.want {
			t.Errorf("%s: ClassifyError(%v) = %s, want %s", tc.name, tc.err, got, tc.want)
		}
	}
}

// TestClassifyInputErrorDefusesAnEchoedMarker: the message below is the shape
// gqlc-jjot2.18 measured, a sentinel whose detail quotes a name from the input.
func TestClassifyInputErrorDefusesAnEchoedMarker(t *testing.T) {
	echoed := fmt.Errorf("%w: bug:x", errSentinel)
	cases := []struct {
		name  string
		input string
		err   error
		want  fuzzkit.ErrClass
	}{
		{"echoed marker reads as its sentinel", "RETURN `bug:x`", echoed, fuzzkit.ClassSentinel},
		{"echoed marker on a plain error is plain", "bug:", errors.New("bug: x"), fuzzkit.ClassPlain},
		{"echoed marker on a syntax error is syntax", "bug:", errors.New("syntax error near bug:"), fuzzkit.ClassSyntax},
		{"nil", "bug:", nil, fuzzkit.ClassNil},
		// Without the marker in the input, it is ClassifyError exactly.
		{"marker not in input is a bug", "RETURN x", echoed, fuzzkit.ClassBug},
		{"no marker anywhere", "RETURN x", errSentinel, fuzzkit.ClassSentinel},
	}
	for _, tc := range cases {
		if got := fuzzkit.ClassifyInputError(tc.input, tc.err, isSyntax, errSentinel); got != tc.want {
			t.Errorf("%s: ClassifyInputError(%q, %v) = %s, want %s", tc.name, tc.input, tc.err, got, tc.want)
		}
	}
}

func TestClassifyErrorNilPredicateMatchesNothing(t *testing.T) {
	if got := fuzzkit.ClassifyError(errSyntax, nil); got != fuzzkit.ClassPlain {
		t.Fatalf("nil isSyntax: got %s, want plain", got)
	}
}

func TestSeedAddsEveryMatchedFile(t *testing.T) {
	var got []string
	if msg := fuzzkit.Seed([]string{"testdata/seedfiles/*.txt"}, func(b []byte) { got = append(got, string(b)) }); msg != "" {
		t.Fatal(msg)
	}
	if want := []string{"one\n", "two\n"}; strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestSeedRefusesAGlobThatMatchesNothing(t *testing.T) {
	cases := map[string][]string{
		"no globs":              nil,
		"one glob of two empty": {"testdata/seedfiles/*.txt", "testdata/seedfiles/*.absent"},
		"directories only":      {"testdata/seed*"},
		"bad pattern":           {"testdata/["},
	}
	for name, globs := range cases {
		if msg := fuzzkit.Seed(globs, func([]byte) {}); msg == "" {
			t.Errorf("%s: want a refusal, got none", name)
		}
	}
	if _, err := fs.Stat(os.DirFS("testdata"), "seedfiles"); err != nil {
		t.Fatalf("the fixture the refusals are measured against is missing: %v", err)
	}
}
