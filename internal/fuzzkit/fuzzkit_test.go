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
		fuzzkit.Guard(t, 10*time.Millisecond, func() { time.Sleep(200 * time.Millisecond) })
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
		// The first match wins, in the documented order.
		{"sentinel before bug", fmt.Errorf("%w: resolver bug: x", errSentinel), fuzzkit.ClassSentinel},
		{"syntax before bug", errors.New("syntax error near bug: x"), fuzzkit.ClassSyntax},
	}
	for _, tc := range cases {
		if got := fuzzkit.ClassifyError(tc.err, isSyntax, errSentinel, errOther); got != tc.want {
			t.Errorf("%s: ClassifyError(%v) = %s, want %s", tc.name, tc.err, got, tc.want)
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
	if msg := fuzzkit.Seed([]string{"testdata/seedfiles/*.txt"}, func(s string) { got = append(got, s) }); msg != "" {
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
		if msg := fuzzkit.Seed(globs, func(string) {}); msg == "" {
			t.Errorf("%s: want a refusal, got none", name)
		}
	}
	if _, err := fs.Stat(os.DirFS("testdata"), "seedfiles"); err != nil {
		t.Fatalf("the fixture the refusals are measured against is missing: %v", err)
	}
}
