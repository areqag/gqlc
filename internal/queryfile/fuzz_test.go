package queryfile_test

import (
	"bufio"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"
	"unicode"

	"github.com/areqag/gqlc/internal/fuzzkit"
	"github.com/areqag/gqlc/internal/queryfile"
)

// exportedIdent is the name grammar of codegen-stage-c0.md §4.1, written out
// here rather than read from the package so the property is not the parser's
// own regexp checking itself.
var exportedIdent = regexp.MustCompile(`^[A-Z][A-Za-z0-9]*$`)

// FuzzQueryfileParse is target T4 of docs/specs/nightly-fuzzing-plan.md. On
// every input it checks:
//
//   - an error comes with a nil slice and is one of the package's sentinels;
//   - a success names every query with an exported Go identifier and a
//     declared Cardinality;
//   - decomposition: the input, split into lines as bufio.Scanner splits them,
//     is header lines (blank or comment), then per query its annotation line
//     followed by a body from which Text was cut by the rule bodyText documents
//     (see checkDecomposition);
//   - determinism: two parses of the same input are reflect.DeepEqual.
func FuzzQueryfileParse(f *testing.F) {
	fuzzkit.SeedFiles(f,
		"testdata/valid/*.cypher",
		"testdata/invalid/*.cypher",
		"../../test/data/codegen/valid/*/queries.cypher",
		"../../test/data/codegen/invalid/*/queries.cypher",
	)
	f.Fuzz(func(t *testing.T, src string) {
		fuzzkit.Skip(t, []byte(src))
		var first, second []queryfile.AnnotatedQuery
		var err1, err2 error
		fuzzkit.Guard(t, 2*time.Second, func() {
			first, err1 = queryfile.New().Parse(strings.NewReader(src))
			second, err2 = queryfile.New().Parse(strings.NewReader(src))
		})
		if !reflect.DeepEqual(first, second) || errText(err1) != errText(err2) {
			t.Fatalf("property determinism: two parses disagree:\n%#v, %v\n%#v, %v", first, err1, second, err2)
		}
		if err1 != nil {
			if first != nil {
				t.Fatalf("property error-nil-slice: Parse returned %d queries alongside %v", len(first), err1)
			}
			if c := classify(err1, src); c != fuzzkit.ClassSentinel {
				t.Fatalf("property error-class: %v is class %s, want sentinel", err1, c)
			}
			return
		}
		for i, q := range first {
			if !exportedIdent.MatchString(q.Name) {
				t.Fatalf("property name: query %d is named %q", i, q.Name)
			}
			if q.Cardinality.String() == "invalid" {
				t.Fatalf("property cardinality: query %d (%s) has undeclared Cardinality %d", i, q.Name, q.Cardinality)
			}
		}
		if msg := checkDecomposition(src, first); msg != "" {
			t.Fatal("property decomposition: " + msg)
		}
	})
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// classify is fuzzkit.ClassifyError over the package's sentinels, with one
// correction. queryfile never writes "bug:" itself, but its messages quote the
// offending line or token, so an input carrying "bug:" can put the marker into
// a sentinel error; ClassifyError tests the marker first and would call that a
// bug. Only in that case, and only when the error does wrap a sentinel, is it
// read as the sentinel it is.
func classify(err error, src string) fuzzkit.ErrClass {
	c := fuzzkit.ClassifyError(err, nil, queryfile.AllSentinels()...)
	if c == fuzzkit.ClassBug && strings.Contains(src, "bug:") {
		for _, s := range queryfile.AllSentinels() {
			if errors.Is(err, s) {
				return fuzzkit.ClassSentinel
			}
		}
	}
	return c
}

// annotationLike is the prefix test the grammar gives a line that is meant as
// an annotation (codegen-stage-c0.md §4.1). On a successful parse every line
// carrying it is one of the annotations, because a line that carries it and
// does not parse as one is ErrMalformedAnnotation.
var annotationLike = regexp.MustCompile(`^//\s*name:`)

// checkDecomposition holds the claim that Text is cut from the input and from
// nowhere else. codegen-stage-c0.md §3.2 calls SourceText "the exact query
// text between this annotation and the next"; the code is narrower than that,
// and this checks what the code documents (bodyText, bd gqlc-kc5w): Text is
// the body with leading and trailing whitespace removed and, after its last
// query line, a run of comment lines that begins with a blank line dropped.
// Lines are what bufio.Scanner yields, so a CR before a newline is not part of
// any line, which is the scanner's behaviour and not a claim of this property.
func checkDecomposition(src string, qs []queryfile.AnnotatedQuery) string {
	var lines []string
	sc := bufio.NewScanner(strings.NewReader(src))
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	if err := sc.Err(); err != nil {
		return "the input does not split into lines: " + err.Error()
	}

	var annots []int
	for i, l := range lines {
		if annotationLike.MatchString(l) {
			annots = append(annots, i)
		}
	}
	if len(annots) != len(qs) {
		return fmt.Sprintf("the input has %d annotation lines but Parse returned %d queries", len(annots), len(qs))
	}
	for i := range annots[0] {
		if !blankOrComment(lines[i]) {
			return fmt.Sprintf("header line %d is neither blank nor a comment: %q", i+1, lines[i])
		}
	}
	for k, q := range qs {
		if fields := annotationFields(lines[annots[k]]); len(fields) != 2 || fields[0] != q.Name || fields[1] != ":"+q.Cardinality.String() {
			return fmt.Sprintf("query %d (%s :%s) does not match its annotation line %q", k, q.Name, q.Cardinality, lines[annots[k]])
		}
		end := len(lines)
		if k+1 < len(annots) {
			end = annots[k+1]
		}
		if msg := checkBody(strings.Join(lines[annots[k]+1:end], "\n"), q.Text); msg != "" {
			return fmt.Sprintf("query %d (%s): %s", k, q.Name, msg)
		}
	}
	return ""
}

// annotationFields splits the part of an annotation line after "name:" on the
// whitespace RE2's \s names, which is not unicode.IsSpace's set.
func annotationFields(line string) []string {
	_, rest, _ := strings.Cut(line, "name:")
	return strings.FieldsFunc(rest, func(r rune) bool {
		return r == ' ' || r == '\t' || r == '\n' || r == '\f' || r == '\r'
	})
}

// checkBody holds Text against the body it was cut from: body = prefix + Text +
// suffix, prefix all whitespace, Text not empty and not starting or ending in
// whitespace, and suffix the rest of Text's last line (whitespace) followed by
// lines that are blank or comments, the first of them blank when any is a
// comment — a comment abutting the last query line stays in Text.
func checkBody(body, text string) string {
	if text == "" {
		return "Text is empty"
	}
	if strings.TrimSpace(text) != text {
		return fmt.Sprintf("Text carries leading or trailing whitespace: %q", text)
	}
	start := len(body) - len(strings.TrimLeftFunc(body, unicode.IsSpace))
	if !strings.HasPrefix(body[start:], text) {
		return fmt.Sprintf("Text is not the body from its first non-space byte on:\nbody: %q\ntext: %q", body, text)
	}
	suffix := strings.Split(body[start+len(text):], "\n")
	if strings.TrimSpace(suffix[0]) != "" {
		return fmt.Sprintf("Text stops inside a line of the body:\nbody: %q\ntext: %q", body, text)
	}
	rest := suffix[1:]
	for _, l := range rest {
		if !blankOrComment(l) {
			return fmt.Sprintf("a query line after Text was dropped: %q", l)
		}
	}
	if len(rest) > 0 && strings.TrimSpace(rest[0]) != "" {
		return fmt.Sprintf("a comment abutting the query's last line was dropped: %q", rest[0])
	}
	return ""
}

func blankOrComment(line string) bool {
	t := strings.TrimSpace(line)
	return t == "" || strings.HasPrefix(t, "//")
}
