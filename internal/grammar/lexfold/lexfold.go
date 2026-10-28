// Package lexfold keeps the ANTLR lexers linear-time with a small constant on
// non-ASCII input.
//
// The antlr4-go lexer caches a DFA edge only for a symbol no greater than
// antlr.LexerATNSimulatorMaxDFAEdge, 127 by default, and runs a full ATN
// closure for every rune above it. The closure's cost depends on the lexer
// context — about 13 us per rune inside a GQL quoted string, 11 us inside a
// Cypher comment — so 256 KiB of non-ASCII text took seconds where ASCII takes
// milliseconds (bd gqlc-jjot2.17).
//
// Two things remove that. This package raises the bound to 255 at init, so a
// Latin-1 rune is cached like an ASCII one. And Stream presents every rune
// above 255 to the lexer as a Latin-1 stand-in from the same lexer character
// class: a code point that matches exactly the same transitions of the
// grammar's lexer ATN, so the lexer takes the same path on it and every token
// keeps its type and its bounds. Token text is read from the original runes,
// never from the stand-ins.
//
// The tables are generated from the generated lexers' ATNs, not from Go's
// unicode package: the grammar's \p{...} sets are ANTLR's Unicode version, and
// Go's is newer. TestTablesAreCurrent re-derives them and fails on any drift,
// which is what a regenerated grammar must answer to; run it with -update.
//
// A rune with no stand-in in its class is passed through unchanged and is lexed
// on the slow path; the tables record which (Keep).
package lexfold

import (
	"io"
	"sort"

	"github.com/antlr4-go/antlr/v4"
)

// MaxDFAEdge is the bound this package sets. The stand-ins in the tables are
// all at or below it.
const MaxDFAEdge = 255

// antlr4-go v4.13.1 declares LexerATNSimulatorMaxDFAEdge as a package var in
// lexer_atn_simulator.go, and getExistingTargetState and addDFAEdge there skip
// the edge cache for any symbol above it. The runtime reads it on every lexer
// step, so it is set once here, before any lexer exists, rather than per Stream.
func init() {
	antlr.LexerATNSimulatorMaxDFAEdge = MaxDFAEdge
}

// Keep marks a class whose runes have no stand-in and reach the lexer as
// themselves.
const Keep rune = -1

// Range assigns the runes Lo..Hi, inclusive, to a class, an index into a
// Table. The ranges are shared by both grammars, since their lexer classes
// come from the same Unicode properties; only the stand-ins differ.
type Range struct {
	Lo, Hi rune
	Class  uint8
}

// Table is one grammar's stand-in for each class. Class 0 is every rune above
// MaxDFAEdge that no range covers.
type Table []rune

func (t Table) fold(r rune) rune {
	if r <= MaxDFAEdge {
		return r
	}
	class := uint8(0)
	i := sort.Search(len(ranges), func(i int) bool { return ranges[i].Hi >= r })
	if i < len(ranges) && ranges[i].Lo <= r {
		class = ranges[i].Class
	}
	if to := t[class]; to != Keep {
		return to
	}
	return r
}

// Stream is an antlr.CharStream that shows the lexer each rune folded through
// a Table and answers every text request from the original runes.
type Stream struct {
	*antlr.InputStream
	table Table
}

// NewStream reads r to the end through antlr.NewIoStream, so the input decodes
// exactly as it did before folding: an invalid byte is U+FFFD and a read error
// ends the input.
func NewStream(r io.Reader, t Table) *Stream {
	return &Stream{InputStream: antlr.NewIoStream(r), table: t}
}

// LA is the only method the lexer reads symbols through.
func (s *Stream) LA(offset int) int {
	c := s.InputStream.LA(offset)
	if c == antlr.TokenEOF {
		return c
	}
	return int(s.table.fold(rune(c)))
}
