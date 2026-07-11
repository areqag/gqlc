package queryfile

import (
	"bufio"
	"fmt"
	"io"
	"regexp"
	"strings"
)

// annotationLine is the full annotation grammar (§4.1): "//" then optional
// whitespace, "name:", the identifier, at least one whitespace character,
// then ":cardinality". Whitespace between "//" and "name:" is permitted; a
// single space between the ident and the ":cardinality" is standard but
// any run of whitespace is tolerated.
var annotationLine = regexp.MustCompile(`^//\s*name:\s*(\S+)\s+:(\S+)\s*$`)

// annotationPrefix detects lines that begin like an annotation but do not
// match the full grammar. A line matching this prefix but failing
// annotationLine is malformed (a typo the author almost certainly means as
// an annotation), not a comment.
var annotationPrefix = regexp.MustCompile(`^//\s*name:`)

// identName is the exported-Go-identifier grammar the annotation's name
// must satisfy (spec §4.1: "^[A-Z][A-Za-z0-9]*$"). Enforced at parse time
// so codegen consumes NamedQuery.Name verbatim (ADR 0010 D2 Q5.1).
var identName = regexp.MustCompile(`^[A-Z][A-Za-z0-9]*$`)

// parse walks r line by line, flushing an [AnnotatedQuery] on each
// annotation-line hit. Short-circuits on the first grammar violation.
func parse(r io.Reader) ([]AnnotatedQuery, error) {
	scanner := bufio.NewScanner(r)
	// Query bodies can be arbitrarily long in principle; the default
	// bufio.Scanner buffer (64K per line) is generous but not open-ended.
	// Grow to 1 MiB so a pathological single-line query does not surprise
	// the parser. Users pathological beyond this are outside C0 scope.
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var (
		out         []AnnotatedQuery
		curName     string
		curCard     Cardinality
		curBody     strings.Builder
		haveCurrent bool
		lineno      int
	)

	flush := func() {
		out = append(out, AnnotatedQuery{
			Name:        curName,
			Cardinality: curCard,
			Text:        strings.TrimSpace(curBody.String()),
		})
		curBody.Reset()
	}

	for scanner.Scan() {
		lineno++
		line := scanner.Text()

		if annotationLine.MatchString(line) {
			m := annotationLine.FindStringSubmatch(line)
			name, cardTok := m[1], m[2]

			card, ok := parseCardinality(cardTok)
			if !ok {
				return nil, fmt.Errorf("%w: line %d: %q", ErrUnknownCardinality, lineno, cardTok)
			}
			if !identName.MatchString(name) {
				return nil, fmt.Errorf("%w: line %d: %q", ErrInvalidQueryName, lineno, name)
			}

			if haveCurrent {
				if strings.TrimSpace(curBody.String()) == "" {
					return nil, fmt.Errorf("%w: line %d: %q has no body", ErrMissingAnnotation, lineno, curName)
				}
				flush()
			}
			curName = name
			curCard = card
			haveCurrent = true
			continue
		}

		if annotationPrefix.MatchString(line) {
			return nil, fmt.Errorf("%w: line %d: %q", ErrMalformedAnnotation, lineno, line)
		}

		if !haveCurrent {
			// Pre-first-annotation: only comments and blank lines allowed
			// (spec §4.1 file-header comments). "//" any content is a
			// comment; blank / whitespace-only is fine.
			trimmed := strings.TrimSpace(line)
			if trimmed == "" || strings.HasPrefix(trimmed, "//") {
				continue
			}
			return nil, fmt.Errorf("%w: line %d", ErrTextBeforeAnnotation, lineno)
		}

		curBody.WriteString(line)
		curBody.WriteByte('\n')
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	if haveCurrent {
		if strings.TrimSpace(curBody.String()) == "" {
			return nil, fmt.Errorf("%w: %q has no body", ErrMissingAnnotation, curName)
		}
		flush()
	}
	if len(out) == 0 {
		return nil, ErrNoQueries
	}

	// Duplicate-name detection runs after emission (spec §4.4): a linear
	// pass, no set carried through the walk.
	seen := make(map[string]int, len(out))
	for i, q := range out {
		if first, dup := seen[q.Name]; dup {
			return nil, fmt.Errorf("%w: %q at positions %d and %d", ErrDuplicateQueryName, q.Name, first, i)
		}
		seen[q.Name] = i
	}

	return out, nil
}

// parseCardinality lowers the annotation's raw token into the enum, or
// reports the token was not one of the three C0-accepted values.
func parseCardinality(tok string) (Cardinality, bool) {
	switch tok {
	case "one":
		return CardinalityOne, true
	case "many":
		return CardinalityMany, true
	case "exec":
		return CardinalityExec, true
	default:
		return 0, false
	}
}
