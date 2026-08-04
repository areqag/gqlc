package conformance_test

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/areqag/gqlc/internal/codegen"
)

// The specs describe an emitted method surface, and every
// `Name(ctx context.Context, …)` they print is a claim about bytes this
// package produces. Nothing held those claims against the emitter, and
// they drifted: C1 §5.3 specified the single-parameter argument as a
// mangle of the query author's parameter name (`paramFieldName("minAge")`
// → `MinAge` → `minAge`), which is the capture vector gqlc-lhs3 removed
// from the emitter and replaced with codegen.ParamArg. A reader
// implementing to that spec reintroduced the vulnerability (gqlc-rz0l).
//
// This file is the fence. It reads the argument name from the emitter's
// own constant, so the specs cannot disagree with codegen.ParamArg
// without going red, and a future rename of that constant reddens the
// specs rather than silently widening the gap.

// docRoots are the trees the fence sweeps, relative to this package.
// Everything under docs/ rather than only docs/specs/: the drift reached
// C1, C3, C4 and C5, and an ADR or a design note prints the same
// signatures.
var docRoots = []string{"../../../docs", "../../../README.md", "../../../CONTEXT.md"}

// ctxAnchor is the prefix every emitted query method's parameter list
// opens with. Anchoring on it rather than on `func (q *Queries)` reaches
// the interface members too — C4 §3.2's WriteQuerier block is a declared
// surface, and its drift was the same drift.
const ctxAnchor = "(ctx context.Context"

// mapAnchor opens the driver-binding map literal an emitted method body
// passes to the run seam.
const mapAnchor = "map[string]any{"

// specSigFloor and specBindFloor are the counts below which each sweep is
// presumed broken rather than clean. A scanner that stops matching — a
// changed anchor spelling, a doc reorganisation that moves the specs out
// from under docRoots — grades zero sites and passes vacuously, which is
// the failure mode this project keeps finding. Both floors sit under the
// counts at the time of writing (17 signatures, 12 bindings) so ordinary
// doc edits do not churn them, and far enough over zero to trip a dead
// scanner.
const (
	specSigFloor  = 12
	specBindFloor = 8
)

// sigAnchorDocs must each contribute at least one graded signature. The
// floors above catch a scanner that matches nothing anywhere; these
// catch the narrower case of one document falling out of the sweep while
// the others carry the count. C1 is the authoritative section for
// method-signature naming (codegen-stage-c0.md §10 routes it there); C4
// declares the same signatures inside the WriteQuerier interface, and C5
// prints them for the edgeUnion surface. C2 and C3 are absent on
// purpose: every method they illustrate takes zero parameters.
var sigAnchorDocs = []string{
	"../../../docs/specs/codegen-stage-c1.md",
	"../../../docs/specs/codegen-stage-c4.md",
	"../../../docs/specs/codegen-stage-c5.md",
}

// specSig is one graded site: a documented method signature taking one
// argument after the context, or one documented driver-binding entry.
// Both reduce to the same question — which identifier the document says
// the emitted Go reads — so `arg` carries the method argument's name for
// the first and the binding expression's root for the second, and `text`
// carries the source line so a failure names it rather than describes it.
type specSig struct {
	file string
	line int
	arg  string
	text string
}

func (s specSig) String() string {
	return fmt.Sprintf("%s:%d: %s", s.file, s.line, s.text)
}

// TestSpecMethodArgIsGeneratorOwned holds every emitted-method signature
// printed anywhere in the documentation to the argument name the emitter
// actually binds. The single- and multi-parameter forms share that name
// (codegen.ParamArg), so the rule is one comparison at both arities: the
// identifier after `ctx context.Context,` is the generator's, never the
// query author's.
func TestSpecMethodArgIsGeneratorOwned(t *testing.T) {
	files := docFiles(t)
	require.NotEmpty(t, files, "the fence swept no documents; docRoots is stale")

	perDoc := make(map[string]int, len(files))
	var graded, bad []specSig
	for _, file := range files {
		for _, sig := range scanSpecSigs(file, readDoc(t, file)) {
			perDoc[file]++
			graded = append(graded, sig)
			if sig.arg != codegen.ParamArg {
				bad = append(bad, sig)
			}
		}
	}

	require.GreaterOrEqualf(t, len(graded), specSigFloor,
		"the fence graded only %d method signatures across %d documents, under the floor of %d — "+
			"the scanner has stopped matching and this test is passing over nothing",
		len(graded), len(files), specSigFloor)
	for _, doc := range sigAnchorDocs {
		require.NotZerof(t, perDoc[doc],
			"%s contributed no graded method signature; either its method surface has moved out of "+
				"the sweep or the scanner no longer reads it", doc)
	}

	requireClean(t, bad, "documented method argument is not generator-owned",
		fmt.Sprintf("these documented signatures name the emitted method argument after the query author's parameter\n"+
			"instead of after the generator; the emitter binds codegen.ParamArg (%q) at every arity, precisely so\n"+
			"that no author-chosen identifier reaches the scope the method body resolves in (gqlc-lhs3, gqlc-rz0l)",
			codegen.ParamArg))
}

// TestSpecParamsMapBindsGeneratorOwnedValue is the same fence one step
// further into the body. The driver-binding map's key and value are
// separately owned, and only the value moved: the key stays the raw
// parameter name the query text writes after the dollar sign, because
// that is what the driver substitutes on, while the value is an
// expression in the emitted method's Go scope and so has to be
// codegen.ParamArg. A spec that reverts the value to the author's name
// documents the capture vector again just as loudly as the signature
// does — and correcting the value by also rewriting the key would break
// the binding outright, so the fence grades the value alone.
func TestSpecParamsMapBindsGeneratorOwnedValue(t *testing.T) {
	files := docFiles(t)
	require.NotEmpty(t, files, "the fence swept no documents; docRoots is stale")

	var graded, bad []specSig
	for _, file := range files {
		for _, bind := range scanSpecBinds(file, readDoc(t, file)) {
			graded = append(graded, bind)
			if bind.arg != codegen.ParamArg && !strings.HasPrefix(bind.arg, codegen.ParamArg+".") {
				bad = append(bad, bind)
			}
		}
	}

	require.GreaterOrEqualf(t, len(graded), specBindFloor,
		"the fence graded only %d parameter bindings across %d documents, under the floor of %d — "+
			"the scanner has stopped matching and this test is passing over nothing",
		len(graded), len(files), specBindFloor)

	requireClean(t, bad, "documented parameter binding is not generator-owned",
		fmt.Sprintf("these documented map[string]any entries bind a value that is not codegen.ParamArg (%q) or a\n"+
			"field selected from it; the emitter's paramsMapText / argsMapText compose every value from that\n"+
			"one identifier, and only the map key carries the author's parameter name (gqlc-lhs3, gqlc-rz0l)",
			codegen.ParamArg))
}

// requireClean fails with one message naming every offending site, so a
// reader fixing the drift sees all of it at once rather than the first.
func requireClean(t *testing.T, bad []specSig, headline, why string) {
	t.Helper()
	if len(bad) == 0 {
		return
	}
	lines := make([]string, 0, len(bad))
	for _, sig := range bad {
		lines = append(lines, "  "+sig.String())
	}
	require.Fail(t, headline, why+":\n"+strings.Join(lines, "\n"))
}

// TestSpecSigScannerDetectsDrift is the fence's own witness. The sweep
// above is only worth its runtime if the scanner it runs can actually
// separate a drifted signature from a correct one, so each row here is a
// verbatim line that was in the specs before gqlc-rz0l corrected it,
// paired with what replaced it. Without this, a scanner that quietly
// matched nothing would leave the sweep green over any prose at all.
func TestSpecSigScannerDetectsDrift(t *testing.T) {
	for _, tc := range []struct {
		name    string
		text    string
		wantArg string
		wantAny bool
	}{{
		name:    "c1 §3.1, before",
		text:    "func (q *Queries) PersonById(ctx context.Context, id int64) (PersonRow, error)",
		wantArg: "id",
		wantAny: true,
	}, {
		name:    "c1 §3.1, after",
		text:    "func (q *Queries) PersonById(ctx context.Context, arg int64) (PersonRow, error)",
		wantArg: codegen.ParamArg,
		wantAny: true,
	}, {
		name:    "c1 §6.3 worked example, before",
		text:    "func (q *Queries) PersonName(ctx context.Context, id int64) (string, error) {",
		wantArg: "id",
		wantAny: true,
	}, {
		name:    "c4 §3.2 WriteQuerier member, before",
		text:    "    RemovePerson(ctx context.Context, id int64) error",
		wantArg: "id",
		wantAny: true,
	}, {
		name:    "c4 §3.2 WriteQuerier member, after",
		text:    "    RemovePerson(ctx context.Context, arg int64) error",
		wantArg: codegen.ParamArg,
		wantAny: true,
	}, {
		name:    "c5 §5.6 worked example, before",
		text:    "func (q *Queries) GetAction(ctx context.Context, since int64) (GetActionR, error) {",
		wantArg: "since",
		wantAny: true,
	}, {
		name:    "the multi-parameter form is graded at the same axis",
		text:    "func (q *Queries) PeopleOverAge(ctx context.Context, arg PeopleOverAgeParams) ([]string, error)",
		wantArg: codegen.ParamArg,
		wantAny: true,
	}, {
		name:    "an unnamed argument is drift too, not an exemption",
		text:    "func (q *Queries) PersonById(ctx context.Context, int64) (PersonRow, error)",
		wantArg: "",
		wantAny: true,
	}, {
		name:    "zero-parameter methods have no argument to grade",
		text:    "func (q *Queries) AllPeopleNames(ctx context.Context) ([]string, error)",
		wantAny: false,
	}, {
		name:    "the driverOrTx.run seam is not a query method",
		text:    "func (d driverDB) run(ctx context.Context, cypher string, params map[string]any, access neo4j.AccessMode) ([]*neo4j.Record, error) {",
		wantAny: false,
	}, {
		name:    "a signature wrapped across lines is still one signature",
		text:    "func (q *Queries) PersonById(ctx context.Context,\n    id int64) (PersonRow, error)",
		wantArg: "id",
		wantAny: true,
	}, {
		name:    "a template placeholder parameter list is not graded",
		text:    "func (q *Queries) <MethodName>(ctx context.Context<param-list>) (<return>, error) {",
		wantAny: false,
	}} {
		t.Run(tc.name, func(t *testing.T) {
			got := scanSpecSigs("witness.md", tc.text)
			if !tc.wantAny {
				require.Empty(t, got)
				return
			}
			require.Len(t, got, 1)
			require.Equal(t, tc.wantArg, got[0].arg)
		})
	}
}

// TestSpecBindScannerDetectsDrift is the binding sweep's witness, on the
// same terms: each row is a `map[string]any` literal that was in the
// specs before gqlc-rz0l corrected it, or the correction, or a form the
// sweep must leave alone.
func TestSpecBindScannerDetectsDrift(t *testing.T) {
	for _, tc := range []struct {
		name string
		text string
		want []string
	}{{
		name: "c1 §5.3 template, before",
		text: `map[string]any{"<rawName>": <bareParam>}`,
		want: []string{"<bareParam>"},
	}, {
		name: "c1 §5.3 template, after",
		text: `map[string]any{"<rawName>": arg}`,
		want: []string{"arg"},
	}, {
		name: "c1 §6.3 worked example, before",
		text: `records, err := q.db.run(ctx, personNameQueryText, map[string]any{"id": id}, neo4j.AccessModeRead)`,
		want: []string{"id"},
	}, {
		name: "c3 §5.7 FLOAT32 widen, before",
		text: `map[string]any{"x": float64(x)}`,
		want: []string{"x"},
	}, {
		name: "c3 §5.7 FLOAT32 widen, after",
		text: `map[string]any{"x": float64(arg)}`,
		want: []string{"arg"},
	}, {
		name: "c3 §5.7 nullable FLOAT32, before — a deref the emitter never writes",
		text: `map[string]any{"x": float64(*x)}`,
		want: []string{"x"},
	}, {
		name: "the multi-parameter form binds selectors off the same identifier",
		text: `map[string]any{"pid": arg.Pid, "oid": arg.Oid}`,
		want: []string{"arg.Pid", "arg.Oid"},
	}, {
		name: "the multi-parameter template keeps its field placeholder",
		text: `map[string]any{"<rawName1>": arg.<Field1>, ...}`,
		want: []string{"arg.<Field1>"},
	}, {
		name: "the AGE instant encoder is peeled to the identifier it reads",
		text: `map[string]any{"seenAt": agtypeNullableMicros(arg.SeenAt)}`,
		want: []string{"arg.SeenAt"},
	}, {
		name: "an elided literal is prose, not a binding",
		text: `map[string]any{...}`,
		want: nil,
	}, {
		name: "the run seam's parameter type is not a literal",
		text: `run(ctx context.Context, cypher string, params map[string]any, access neo4j.AccessMode) error`,
		want: nil,
	}} {
		t.Run(tc.name, func(t *testing.T) {
			got := scanSpecBinds("witness.md", tc.text)
			var values []string
			for _, bind := range got {
				values = append(values, bind.arg)
			}
			require.Equal(t, tc.want, values)
		})
	}
}

// docFiles lists every markdown document under docRoots.
func docFiles(t *testing.T) []string {
	t.Helper()
	var out []string
	for _, root := range docRoots {
		info, err := os.Stat(root)
		require.NoErrorf(t, err, "docRoots entry %q does not exist", root)
		if !info.IsDir() {
			out = append(out, root)
			continue
		}
		require.NoError(t, filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !entry.IsDir() && strings.HasSuffix(path, ".md") {
				out = append(out, path)
			}
			return nil
		}))
	}
	sort.Strings(out)
	return out
}

// scanSpecSigs extracts every documented single-argument method
// signature from one document's text.
//
// The shape it grades is the emitted query method's: a parameter list
// opening `(ctx context.Context` and holding exactly one more parameter,
// which is what every arity of the emitted surface produces (`arg <T>`
// for one query parameter, `arg <Method>Params` for two or more). The
// zero-parameter form has nothing after the context and nothing to
// grade; the `driverOrTx.run` seam and `ExecuteWrite` take three or more
// and are not query methods. Parameter lists are matched by balancing
// parentheses rather than by a line regexp, so a signature the prose
// wrapped across lines is graded rather than skipped.
func scanSpecSigs(file, text string) []specSig {
	var out []specSig
	for i := 0; ; {
		j := strings.Index(text[i:], ctxAnchor)
		if j < 0 {
			return out
		}
		open := i + j
		i = open + len(ctxAnchor)

		list, ok := parenSpan(text, open)
		if !ok {
			continue
		}
		params := splitTopLevel(list)
		if len(params) != 2 {
			continue
		}
		// A placeholder stands for a whole parameter list, not for one
		// argument's name; §5.3's `<param-list>` is prose the sweep has
		// no signature to compare.
		if strings.ContainsAny(params[1], "<>") {
			continue
		}
		out = append(out, specSig{
			file: file,
			line: 1 + strings.Count(text[:open], "\n"),
			arg:  argName(params[1]),
			text: strings.TrimSpace(collapse(lineAt(text, open))),
		})
	}
}

// scanSpecBinds extracts every documented driver-binding entry from one
// document's text: the value half of each `"<rawName>": <expr>` pair
// inside a `map[string]any{…}` literal, reduced to the identifier the
// expression ultimately reads. A carrier conversion is peeled off
// (`float64(arg)` binds `arg`, `agtypeNullableMicros(arg.SeenAt)` binds
// `arg.SeenAt`), because the widen is orthogonal to who owns the name.
// Entries with no `:` — the `...` and `map[string]any{...}` elisions —
// are prose, not bindings, and are not graded.
func scanSpecBinds(file, text string) []specSig {
	var out []specSig
	for i := 0; ; {
		j := strings.Index(text[i:], mapAnchor)
		if j < 0 {
			return out
		}
		anchor := i + j
		open := anchor + len(mapAnchor) - 1
		i = open + 1

		body, ok := span(text, open, '{', '}')
		if !ok {
			continue
		}
		for _, entry := range splitTopLevel(body) {
			colon := topLevelColon(entry)
			if colon < 0 {
				continue
			}
			value := unwrapConversions(strings.TrimSpace(entry[colon+1:]))
			// `arg.<Field1>` is the template form of a real selector; its
			// prefix is what this fence grades, so it is kept.
			out = append(out, specSig{
				file: file,
				line: 1 + strings.Count(text[:anchor], "\n"),
				arg:  value,
				text: strings.TrimSpace(collapse(lineAt(text, anchor))),
			})
		}
	}
}

// unwrapConversions peels carrier conversions and pointer operators off
// a binding expression, leaving the identifier or selector underneath.
func unwrapConversions(value string) string {
	for {
		value = strings.TrimLeft(strings.TrimSpace(value), "*&")
		open := strings.IndexByte(value, '(')
		if open <= 0 || !strings.HasSuffix(value, ")") || !isConversionName(value[:open]) {
			return value
		}
		value = value[open+1 : len(value)-1]
	}
}

// isConversionName reports whether s spells a type or helper being
// applied — an identifier, a qualified identifier, or a slice type.
func isConversionName(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if !isIdentByte(s[i]) && s[i] != '.' && s[i] != '[' && s[i] != ']' {
			return false
		}
	}
	return true
}

// topLevelColon is the index of the entry's key/value separator, or -1.
func topLevelColon(entry string) int {
	depth := 0
	for i := 0; i < len(entry); i++ {
		switch entry[i] {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
		case '"':
			if next := strings.IndexByte(entry[i+1:], '"'); next >= 0 {
				i += next + 1
			}
		case ':':
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

// parenSpan returns the contents of the parenthesised span opening at
// text[open], with newlines collapsed to spaces. Reports false when the
// span does not close.
func parenSpan(text string, open int) (string, bool) {
	return span(text, open, '(', ')')
}

// span returns the contents of the bracketed span opening at text[open],
// with newlines collapsed to spaces. Reports false when it does not
// close.
func span(text string, open int, opener, closer byte) (string, bool) {
	depth := 0
	for i := open; i < len(text); i++ {
		switch text[i] {
		case opener:
			depth++
		case closer:
			depth--
			if depth == 0 {
				return collapse(text[open+1 : i]), true
			}
		}
	}
	return "", false
}

// readDoc reads one swept document.
func readDoc(t *testing.T, file string) string {
	t.Helper()
	body, err := os.ReadFile(file)
	require.NoError(t, err)
	return string(body)
}

// splitTopLevel splits a parameter list at its depth-zero commas.
func splitTopLevel(list string) []string {
	var out []string
	depth, start := 0, 0
	for i := 0; i < len(list); i++ {
		switch list[i] {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
		case ',':
			if depth == 0 {
				out = append(out, strings.TrimSpace(list[start:i]))
				start = i + 1
			}
		}
	}
	return append(out, strings.TrimSpace(list[start:]))
}

// argName is the identifier a parameter declaration binds, or "" when it
// binds none. An unnamed parameter is drift in its own right — a
// documented signature that names no argument is not the one the emitter
// writes — so it is graded rather than skipped.
func argName(param string) string {
	fields := strings.Fields(param)
	if len(fields) < 2 {
		return ""
	}
	return fields[0]
}

func isIdentByte(c byte) bool {
	return c == '_' ||
		('0' <= c && c <= '9') ||
		('a' <= c && c <= 'z') ||
		('A' <= c && c <= 'Z')
}

// lineAt returns the whole line containing text[i].
func lineAt(text string, i int) string {
	start := strings.LastIndexByte(text[:i], '\n') + 1
	end := strings.IndexByte(text[i:], '\n')
	if end < 0 {
		return text[start:]
	}
	return text[start : i+end]
}

func collapse(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
