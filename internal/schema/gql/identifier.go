package gql

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/areqag/gqlc/internal/grammar/gql/gen"
)

// identifierName reads one identifier as the name it denotes.
//
// A delimited identifier's delimiters are syntax and not content (GQL 18.9), so
// city, `city` and "city" are three spellings of ONE name and must reach the
// model as the same bytes. Before gqlc-tzu9r every one of the five identifier
// positions carried them through, so a schema that quoted a name and a query
// that did not declared two types that never unified.
//
// Decoding here rather than in the model constructors is forced, not preferred:
// a doubled delimiter denotes one, so a decoded name can hold the very byte that
// delimited it, and a bare string arriving at graph.RecordOf cannot be asked
// whether it is still quoted. Only the token knows its own delimiter kind.
// Design: bd gqlc-935qa.
//
// Case is untouched in both arms: a regular identifier reaches the model as its
// source bytes and so does a decoded delimited one, so "person" and Person stay
// two names. Folding either needs Syntax Rules prose gqlc has not bought.
func identifierName(id gen.IIdentifierContext) (string, error) {
	var delim byte
	switch {
	case id.ACCENT_QUOTED_CHARACTER_SEQUENCE() != nil:
		delim = '`'
	case id.DOUBLE_QUOTED_CHARACTER_SEQUENCE() != nil:
		delim = '"'
	default:
		// regularIdentifier: REGULAR_IDENTIFIER or one of the 47 nonReservedWords
		// (GQL.g4:2963-2966). Neither class can carry a delimiter or an escape, so
		// the source bytes are already the name — which is what every one of these
		// sites did unconditionally before.
		return id.GetText(), nil
	}

	text := id.GetText()
	if strings.HasPrefix(text, "@") {
		return "", fmt.Errorf("%w: %s", ErrNoEscapeIdentifier, text)
	}

	name, err := decodeDelimited(text[1:len(text)-1], delim)
	if err != nil {
		return "", fmt.Errorf("%w: %s", err, text)
	}
	if name == "" {
		return "", fmt.Errorf("%w: %s", ErrEmptyIdentifier, text)
	}
	return name, nil
}

// decodeDelimited resolves the body of a delimited identifier — the text between
// the delimiters — to the characters it denotes: a doubled delimiter is one, and
// a reverse solidus opens an escape (GQL.g4:3145-3186).
//
// Only the identifier's OWN delimiter doubles. The other two quote characters are
// ordinary content here, matched by the negated set in the character
// representation fragment, so `""` inside an accent-quoted name is two double
// quotes and not one.
//
// Every byte that triggers a decision is ASCII, and UTF-8 never encodes an ASCII
// byte inside a multi-byte rune, so a byte walk cannot cut a rune in half and
// non-ASCII content copies through verbatim.
func decodeDelimited(body string, delim byte) (string, error) {
	var b strings.Builder
	b.Grow(len(body))
	for i := 0; i < len(body); {
		switch c := body[i]; {
		case c == delim && i+1 < len(body) && body[i+1] == delim:
			b.WriteByte(delim)
			i += 2
		case c == '\\':
			r, width, err := unescape(body[i:])
			if err != nil {
				return "", err
			}
			b.WriteRune(r)
			i += width
		default:
			b.WriteByte(c)
			i++
		}
	}
	return b.String(), nil
}

// unescape decodes the escape sequence at the head of s, returning the character
// it denotes and the bytes consumed.
//
// The malformed arms are unreachable through the lexer: an escape it does not
// recognise ends the token, so ESCAPED_CHARACTER's eleven forms are the only ones
// that arrive here. They refuse rather than copy the bytes through, because a
// decoder that passes an escape it could not read mints a name from a shape it
// did not understand.
//
// The \u/\U range check is not of that kind, and is why this can fail at all:
// \uD800 is four hex digits, so it lexes, and it denotes an unpaired surrogate —
// no character. Writing it out would give U+FFFD, a name shared with every other
// unreadable escape and equal to none of them.
func unescape(s string) (rune, int, error) {
	if len(s) < 2 {
		return 0, 0, ErrIdentifierEscape
	}
	switch s[1] {
	case '\\', '\'', '"', '`':
		return rune(s[1]), 2, nil
	case 't':
		return '\t', 2, nil
	case 'b':
		return '\b', 2, nil
	case 'n':
		return '\n', 2, nil
	case 'r':
		return '\r', 2, nil
	case 'f':
		return '\f', 2, nil
	case 'u':
		return unescapeHex(s, 4)
	case 'U':
		return unescapeHex(s, 6)
	}
	return 0, 0, ErrIdentifierEscape
}

// unescapeHex decodes \uXXXX (digits == 4) or \UXXXXXX (digits == 6). The lexer
// spells both markers case-sensitively (START_UNICODE4 / START_UNICODE6 set
// caseInsensitive=false), so the two lengths cannot be confused for one another;
// the hex digits themselves are case-insensitive and ParseUint takes either.
func unescapeHex(s string, digits int) (rune, int, error) {
	width := 2 + digits
	if len(s) < width {
		return 0, 0, ErrIdentifierEscape
	}
	v, err := strconv.ParseUint(s[2:width], 16, 32)
	if err != nil {
		return 0, 0, ErrIdentifierEscape
	}
	if r := rune(v); utf8.ValidRune(r) {
		return r, width, nil
	}
	return 0, 0, ErrIdentifierEscape
}
