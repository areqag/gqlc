package fuzzkit

import "strings"

// Stream reads fuzz bytes as a sequence of small decisions. Exhaustion returns
// zero values forever, so a generator is total: every decision past the end of
// the input is the first choice, and the same bytes always yield the same
// decisions.
type Stream struct {
	b []byte
	i int
}

// NewStream returns a Stream over b. It does not copy b.
func NewStream(b []byte) *Stream {
	return &Stream{b: b}
}

// Byte returns the next input byte, or 0 once the input is exhausted.
func (s *Stream) Byte() byte {
	if s.i >= len(s.b) {
		return 0
	}
	c := s.b[s.i]
	s.i++
	return c
}

// Int returns r with 0 <= r < n, and 0 when n <= 0. It reads as many bytes as
// n-1 needs, big-endian, so n == 1 reads none and n <= 256 reads one. The
// result is that value modulo n, which is not uniform when n does not divide a
// power of 256 — a fuzzer's bytes are not uniform either.
func (s *Stream) Int(n int) int {
	if n <= 0 {
		return 0
	}
	var v uint64
	for span := uint64(n - 1); span > 0; span >>= 8 {
		v = v<<8 | uint64(s.Byte())
	}
	return int(v % uint64(n))
}

// Bool reads one byte and returns its low bit.
func (s *Stream) Bool() bool {
	return s.Byte()&1 == 1
}

// Pick returns one of xs, chosen by Int(len(xs)). On an empty xs it returns ""
// and reads nothing: that is a refusal the caller can see, not a panic.
func (s *Stream) Pick(xs []string) string {
	if len(xs) == 0 {
		return ""
	}
	return xs[s.Int(len(xs))]
}

// Ident returns a string of 1 to maxLen runes drawn from alphabet, read as
// runes (an invalid UTF-8 byte reads as U+FFFD). It returns "" when alphabet
// is empty or maxLen <= 0. On an exhausted stream it is the alphabet's first
// rune alone. maxLen is capped at MaxInput, because past the end of the input
// the length is still read and a caller's large bound would otherwise let a
// few 0xff bytes ask for a string of that size.
func (s *Stream) Ident(alphabet string, maxLen int) string {
	runes := []rune(alphabet)
	if len(runes) == 0 || maxLen <= 0 {
		return ""
	}
	maxLen = min(maxLen, MaxInput)
	n := 1 + s.Int(maxLen)
	var sb strings.Builder
	for range n {
		sb.WriteRune(runes[s.Int(len(runes))])
	}
	return sb.String()
}
