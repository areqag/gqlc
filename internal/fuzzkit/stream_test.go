package fuzzkit_test

import (
	"fmt"
	"math"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/areqag/gqlc/internal/fuzzkit"
)

// The bounds the operations below hand to the stream: the edges of each
// decision's contract (non-positive, one, a byte's range and one past it) and
// the largest int, which makes Int read eight bytes.
var (
	bounds    = []int{-1, 0, 1, 2, 255, 256, 257, 65536, math.MaxInt}
	choices   = [][]string{nil, {}, {"only"}, {"a", "b", "c"}}
	alphabets = []string{"", "a", "xyz", "αβγ", "\xff"}
	maxLens   = []int{-1, 0, 1, 8}
)

// bound reads a bound from ctl: one of bounds, or a small value either side
// of zero so that coverage can steer it.
func bound(ctl *fuzzkit.Stream) int {
	if i := ctl.Int(len(bounds) + 1); i < len(bounds) {
		return bounds[i]
	}
	return ctl.Int(1<<12) - 1<<8
}

// An op makes one decision on s, with its arguments read from ctl, and returns
// the decision as a trace entry and the property it broke, or "".
type op func(ctl, s *fuzzkit.Stream) (string, string)

var ops = []op{
	func(_, s *fuzzkit.Stream) (string, string) {
		return fmt.Sprintf("Byte=%d", s.Byte()), ""
	},
	func(ctl, s *fuzzkit.Stream) (string, string) {
		n := bound(ctl)
		r := s.Int(n)
		if (n <= 0 && r != 0) || (n > 0 && (r < 0 || r >= n)) {
			return "", fmt.Sprintf("property Int range: Int(%d) = %d", n, r)
		}
		return fmt.Sprintf("Int(%d)=%d", n, r), ""
	},
	func(_, s *fuzzkit.Stream) (string, string) {
		return fmt.Sprintf("Bool=%t", s.Bool()), ""
	},
	func(ctl, s *fuzzkit.Stream) (string, string) {
		xs := choices[ctl.Int(len(choices))]
		r := s.Pick(xs)
		if len(xs) == 0 && r != "" {
			return "", fmt.Sprintf("property Pick refusal: Pick(%q) = %q, want \"\"", xs, r)
		}
		if len(xs) > 0 && !slices.Contains(xs, r) {
			return "", fmt.Sprintf("property Pick range: Pick(%q) = %q", xs, r)
		}
		return "Pick=" + r, ""
	},
	func(ctl, s *fuzzkit.Stream) (string, string) {
		alphabet, maxLen := alphabets[ctl.Int(len(alphabets))], maxLens[ctl.Int(len(maxLens))]
		r := s.Ident(alphabet, maxLen)
		if msg := identViolation(alphabet, maxLen, r); msg != "" {
			return "", msg
		}
		return "Ident=" + r, ""
	},
}

func identViolation(alphabet string, maxLen int, r string) string {
	n := utf8.RuneCountInString(r)
	if alphabet == "" || maxLen <= 0 {
		if r != "" {
			return fmt.Sprintf("property Ident refusal: Ident(%q, %d) = %q, want \"\"", alphabet, maxLen, r)
		}
		return ""
	}
	if n < 1 || n > maxLen {
		return fmt.Sprintf("property Ident length: Ident(%q, %d) = %q", alphabet, maxLen, r)
	}
	letters := []rune(alphabet)
	for _, c := range r {
		if !slices.Contains(letters, c) {
			return fmt.Sprintf("property Ident alphabet: Ident(%q, %d) = %q", alphabet, maxLen, r)
		}
	}
	return ""
}

// drive runs a sequence of decisions over b, chosen by a second stream over
// the same bytes, then reads past the end of the input and checks that every
// decision there is the zero one. It returns the trace of decisions.
func drive(t *testing.T, b []byte) []string {
	t.Helper()
	ctl, s := fuzzkit.NewStream(b), fuzzkit.NewStream(b)
	var trace []string
	for range len(b) + 8 {
		entry, broken := ops[ctl.Int(len(ops))](ctl, s)
		if broken != "" {
			t.Fatal(broken)
		}
		trace = append(trace, entry)
	}
	for range len(b) {
		s.Byte()
	}
	if msg := exhaustionViolation(s); msg != "" {
		t.Fatal(msg)
	}
	return trace
}

func exhaustionViolation(s *fuzzkit.Stream) string {
	got := []string{
		fmt.Sprint(s.Byte()), fmt.Sprint(s.Int(1000)), fmt.Sprint(s.Int(math.MaxInt)),
		fmt.Sprint(s.Bool()), s.Pick([]string{"first", "second"}), s.Ident("xyz", 8), s.Pick(nil),
	}
	want := []string{"0", "0", "0", "false", "first", "x", ""}
	if !slices.Equal(got, want) {
		return fmt.Sprintf("property exhaustion: decisions past the end read %q, want %q", got, want)
	}
	return ""
}

// FuzzStream is target T0 of docs/specs/nightly-fuzzing-plan.md: Stream never
// panics on any bytes, exhaustion is deterministic (zeros), Pick and Ident
// refuse empty arguments with "" rather than a panic, every decision is in its
// declared range, and the same bytes give the same decisions twice.
func FuzzStream(f *testing.F) {
	f.Add([]byte(nil))
	f.Add([]byte("MATCH (n:Person) RETURN n"))
	f.Add([]byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff})
	f.Fuzz(func(t *testing.T, b []byte) {
		fuzzkit.Skip(t, b)
		var first, second []string
		fuzzkit.Guard(t, 2*time.Second, func() { first = drive(t, b) })
		fuzzkit.Guard(t, 2*time.Second, func() { second = drive(t, b) })
		if !slices.Equal(first, second) {
			t.Fatalf("property determinism: two streams over %q decided differently:\n%s\n%s",
				b, strings.Join(first, " "), strings.Join(second, " "))
		}
	})
}

func TestStreamDecisions(t *testing.T) {
	s := fuzzkit.NewStream([]byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06})
	got := []any{s.Int(1), s.Int(257), s.Bool(), s.Pick([]string{"a", "b"}), s.Ident("xyz", 3), s.Byte()}
	// Int(1) reads nothing; Int(257) reads 0x0102 = 258 → 258 % 257; Bool
	// reads 0x03; Pick reads 0x04 % 2; Ident reads 0x05 % 3 + 1 = 3 runes,
	// the first from 0x06 % 3 and the other two from an exhausted stream.
	want := []any{0, 1, true, "a", "xxx", byte(0)}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}
