// Package graph holds the value vocabulary of the property-graph domain: the
// primitive types that describe graph elements — label sets and property value
// types — shared across the schema and query models.
package graph

import (
	"slices"
	"strings"
)

// LabelSet is a set of labels in source form. It is the input used to build a
// LabelSetKey; parsed models store identity as the key, not the slice.
type LabelSet []string

// LabelSetKey is the canonical, comparable form of a LabelSet, usable as a map
// key: labels sorted, deduplicated, and joined with "&", each label quoted iff
// its bytes would otherwise collide with the encoding's own.
//
// The key IS an element type's identity — it indexes schema.Schema.Nodes, fills
// all three fields of schema.EdgeKey, and reaches schema JSON and codegen — so
// two different label sets sharing one spelling is a type-identity forgery, not
// a cosmetic clash. That is why a label carrying "&" is quoted rather than
// refused: a refusal has to be copied into every producer of labels, and quoting
// at the single point every producer flows through removes the hazard once
// (bd gqlc-yd4ba, executed in gqlc-649co).
//
// The quoting is MINIMAL by design, not by accident: only a label whose bytes
// the encoding owns is quoted, so every key over ordinary labels is byte-for-byte
// what it was before quoting existed and no stored artefact was respelled.
type LabelSetKey string

// needsQuoting reports whether label must be quoted to survive the join.
//
// Three cases, each the encoding's own bytes coming back as data: "&" is the
// separator Key joins on and Split cuts at; a LEADING "`" is what Split reads as
// a quote opening; and the empty label is otherwise indistinguishable from the
// absence of a label, which is what makes Split total on everything Key emits.
//
// A backtick anywhere but the first byte is NOT a reason to quote — Split only
// opens a quote at a label boundary, so a`b keys as itself.
func needsQuoting(label string) bool {
	return strings.Contains(label, "&") || strings.HasPrefix(label, "`") || label == ""
}

// quoteLabel renders a label that needsQuoting reports on: backtick-delimited,
// internal backticks doubled. That is the lexer's own escape and the dialect
// quoteFieldName already speaks in this package, deliberately — a second escape
// dialect in one file would be one too many.
func quoteLabel(label string) string {
	return "`" + strings.ReplaceAll(label, "`", "``") + "`"
}

// Key canonicalises the set into its map key. The original slice is left
// unmodified.
//
// Sorting is on the RAW labels and quoting happens after, matching RecordOf's
// sort-then-encode. The order matters: "`" (0x60) sorts below "a" (0x61), so
// sorting the encoded forms would spell {"a","b&"} as "`b&`&a" instead of
// "a&`b&`" — the same set with a second identity.
func (ls LabelSet) Key() LabelSetKey {
	sorted := slices.Clone(ls)
	slices.Sort(sorted)
	sorted = slices.Compact(sorted)

	encoded := make([]string, len(sorted))
	for i, label := range sorted {
		if needsQuoting(label) {
			encoded[i] = quoteLabel(label)
			continue
		}
		encoded[i] = label
	}
	return LabelSetKey(strings.Join(encoded, "&"))
}

// Split returns the individual labels encoded in the key. It is the exact
// inverse of LabelSet.Key on every value Key produces, and the empty key yields
// no labels — the empty SET. A key of two backticks is a different value: the
// singleton holding the empty LABEL.
//
// On a string Key cannot produce — an unterminated quote, or bytes between a
// closing backtick and the next "&" — the result is unspecified but the call is
// total: no error return, because keys are produced only by Key and nothing
// unmarshals a schema back from JSON.
func (k LabelSetKey) Split() LabelSet {
	if k == "" {
		return nil
	}
	s := string(k)
	labels := make(LabelSet, 0, strings.Count(s, "&")+1)
	for i := 0; ; {
		var label string
		if i < len(s) && s[i] == '`' {
			label, i = readQuotedLabel(s, i)
		} else {
			label, i = readRawLabel(s, i)
		}
		labels = append(labels, label)
		if i >= len(s) {
			return labels
		}
		i++ // the separator, or an unspecified byte trailing a close
	}
}

// readRawLabel reads an unquoted label starting at pos, returning it with the
// index of the "&" that ended it (or len(s)). Backticks are ordinary bytes here:
// a quote opens only at a label boundary, which is what keeps a`b spelled as
// itself.
func readRawLabel(s string, pos int) (string, int) {
	if end := strings.IndexByte(s[pos:], '&'); end >= 0 {
		return s[pos : pos+end], pos + end
	}
	return s[pos:], len(s)
}

// readQuotedLabel reads a backtick-quoted label whose opening delimiter is at
// pos, returning it unquoted with the index just past its closing delimiter. A
// doubled backtick is one literal backtick; an undoubled one closes.
func readQuotedLabel(s string, pos int) (string, int) {
	var b strings.Builder
	for i := pos + 1; i < len(s); i++ {
		if s[i] != '`' {
			b.WriteByte(s[i])
			continue
		}
		if i+1 < len(s) && s[i+1] == '`' {
			b.WriteByte('`')
			i++
			continue
		}
		return b.String(), i + 1
	}
	return b.String(), len(s) // unterminated: unspecified, but total
}
