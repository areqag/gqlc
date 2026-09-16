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

	var acc queryAccumulator
	lineno := 0
	for scanner.Scan() {
		lineno++
		if err := acc.line(lineno, scanner.Text()); err != nil {
			return nil, err
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	out, err := acc.finish()
	if err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, ErrNoQueries
	}

	// Duplicate-name detection runs after emission (spec §4.4): a linear
	// pass, no set carried through the walk.
	if err := checkDuplicateQueryNames(out); err != nil {
		return nil, err
	}
	return out, nil
}

// queryAccumulator is parse's walk state: the queries flushed so far and
// the one whose body is still being collected.
type queryAccumulator struct {
	out         []AnnotatedQuery
	curName     string
	curCard     Cardinality
	curBody     strings.Builder
	haveCurrent bool
}

func (a *queryAccumulator) flush() {
	a.out = append(a.out, AnnotatedQuery{
		Name:        a.curName,
		Cardinality: a.curCard,
		Text:        bodyText(a.curBody.String()),
	})
	a.curBody.Reset()
}

// line consumes one input line: an annotation opens the next query, a
// malformed one is rejected, and anything else is body text once the
// first annotation has been seen.
func (a *queryAccumulator) line(lineno int, line string) error {
	if m := annotationLine.FindStringSubmatch(line); m != nil {
		return a.startQuery(lineno, m[1], m[2])
	}

	if annotationPrefix.MatchString(line) {
		return fmt.Errorf("%w: line %d: %q", ErrMalformedAnnotation, lineno, line)
	}

	if !a.haveCurrent {
		// Pre-first-annotation: only comments and blank lines allowed
		// (spec §4.1 file-header comments). "//" any content is a
		// comment; blank / whitespace-only is fine.
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "//") {
			return nil
		}
		return fmt.Errorf("%w: line %d", ErrTextBeforeAnnotation, lineno)
	}

	a.curBody.WriteString(line)
	a.curBody.WriteByte('\n')
	return nil
}

// startQuery validates an annotation line's two captures, flushes the
// query in progress, and opens the one the annotation names.
func (a *queryAccumulator) startQuery(lineno int, name, cardTok string) error {
	card, ok := parseCardinality(cardTok)
	if !ok {
		return fmt.Errorf("%w: line %d: %q", ErrUnknownCardinality, lineno, cardTok)
	}
	if !identName.MatchString(name) {
		return fmt.Errorf("%w: line %d: %q", ErrInvalidQueryName, lineno, name)
	}

	if a.haveCurrent {
		if bodyText(a.curBody.String()) == "" {
			return fmt.Errorf("%w: line %d: %q has no body", ErrMissingAnnotation, lineno, a.curName)
		}
		a.flush()
	}
	a.curName = name
	a.curCard = card
	a.haveCurrent = true
	return nil
}

// finish flushes the last query at end of input and returns everything
// collected.
func (a *queryAccumulator) finish() ([]AnnotatedQuery, error) {
	if a.haveCurrent {
		if bodyText(a.curBody.String()) == "" {
			return nil, fmt.Errorf("%w: %q has no body", ErrMissingAnnotation, a.curName)
		}
		a.flush()
	}
	return a.out, nil
}

// checkDuplicateQueryNames reports the first name declared twice, by the
// positions of its first two declarations.
func checkDuplicateQueryNames(out []AnnotatedQuery) error {
	seen := make(map[string]int, len(out))
	for i, q := range out {
		if first, dup := seen[q.Name]; dup {
			return fmt.Errorf("%w: %q at positions %d and %d", ErrDuplicateQueryName, q.Name, first, i)
		}
		seen[q.Name] = i
	}
	return nil
}

// bodyText renders a query's accumulated lines into the statement text handed
// verbatim to the driver. A run of comment lines separated from the query's
// last real line by a blank line is free-standing prose — typically written to
// introduce the NEXT query — and is dropped: Text crosses the wire as part of
// the statement, and while neo4j reads "//" as a Cypher line comment, the AGE
// backend composes Text into a SQL literal and nothing establishes it tolerates
// arbitrary prose (bd gqlc-kc5w).
//
// Deliberately NOT dropped: a comment abutting a query line with no blank
// separator, and a comment between two query lines. Those are the author
// annotating THAT query, and removing them would change statements that work
// today.
func bodyText(raw string) string {
	lines := strings.Split(raw, "\n")

	// Walk back over the trailing run of blank and comment-only lines. The
	// first blank line inside that run is the separator; everything from it
	// onward is free-standing.
	i := len(lines)
	for i > 0 && isBlankOrComment(lines[i-1]) {
		i--
	}
	for ; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "" {
			lines = lines[:i]
			break
		}
	}

	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func isBlankOrComment(line string) bool {
	trimmed := strings.TrimSpace(line)
	return trimmed == "" || strings.HasPrefix(trimmed, "//")
}

// parseCardinality lowers the annotation's raw token into the enum, or
// reports the token was not one of the accepted values.
func parseCardinality(tok string) (Cardinality, bool) {
	switch tok {
	case "one":
		return CardinalityOne, true
	case "many":
		return CardinalityMany, true
	case "exec":
		return CardinalityExec, true
	case "iter":
		return CardinalityIter, true
	default:
		return 0, false
	}
}
