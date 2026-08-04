package conformance_test

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
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
//
// It grades three shapes, because the specs write the name in three
// places: signatures printed whole, the `<param-list>` bullets that
// expand the placeholder those signatures print instead of a list, and
// the values of the driver-binding map literals. The middle one is the
// normative rule and the exact text that drifted, so it is graded
// rather than skipped as prose.

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

// paramListTerm opens the bullet each spec uses to define what fills the
// `<param-list>` placeholder its method-shape template prints. That
// bullet is the normative rule: the template shows only the placeholder,
// so the bullet is the one place either document says which identifier
// the emitted signature binds — and it is prose, not a parameter list
// the paren walk below can reach. It is the exact text gqlc-rz0l
// corrected, so a fence that does not grade it fences everything except
// the site it exists for.
const paramListTerm = "**`<param-list>`**"

// Anchors are matched with whitespace read the way Go's tokeniser reads
// it rather than as one fixed spelling — see anchorPattern.
var (
	ctxAnchorRe = anchorPattern(ctxAnchor)
	mapAnchorRe = anchorPattern(mapAnchor)
)

// specSigFloor and specBindFloor are the counts below which each sweep is
// presumed broken rather than clean. A scanner that stops matching — a
// changed anchor spelling, a doc reorganisation that moves the specs out
// from under docRoots — grades zero sites and passes vacuously, which is
// the failure mode this project keeps finding. Both floors sit under the
// live counts so ordinary doc edits do not churn them, and far enough
// over zero to trip a dead scanner. Deliberately not written as "the
// count today minus a margin": a number in this file's prose rots
// silently, and the enumerating failure output below is the real record
// of what is graded.
const (
	specSigFloor  = 12
	specBindFloor = 8
)

// sigAnchorDocs, paramListAnchorDocs and bindAnchorDocs must each
// contribute at least one graded site to their sweep. The floors above
// catch a scanner that matches nothing anywhere; these catch the
// narrower case of one document falling out of the sweep while the
// others carry the count — including the case where the others are
// padded to carry it, which a bare floor cannot tell from health.
//
// C1 is the authoritative section for method-signature naming
// (codegen-stage-c0.md §10 routes it there); C4 declares the same
// signatures inside the WriteQuerier interface, and C5 prints them for
// the edgeUnion surface. C2 and C3 print no such signature on purpose:
// every method they illustrate takes zero parameters. C1 and C4 are the
// two documents that define `<param-list>`; C5 defers to C1's rule.
// Bindings reach one document further than signatures do, because C3's
// width and nullability bullets print the map literal without printing
// the method around it.
var (
	sigAnchorDocs = []string{
		"../../../docs/specs/codegen-stage-c1.md",
		"../../../docs/specs/codegen-stage-c4.md",
		"../../../docs/specs/codegen-stage-c5.md",
	}
	paramListAnchorDocs = []string{
		"../../../docs/specs/codegen-stage-c1.md",
		"../../../docs/specs/codegen-stage-c4.md",
	}
	bindAnchorDocs = []string{
		"../../../docs/specs/codegen-stage-c1.md",
		"../../../docs/specs/codegen-stage-c3.md",
		"../../../docs/specs/codegen-stage-c4.md",
		"../../../docs/specs/codegen-stage-c5.md",
	}
)

// specSig is one graded site: a documented method signature taking one
// argument after the context, the parameter-list tail a `<param-list>`
// bullet expands that placeholder to, or one documented driver-binding
// entry. All three reduce to the same question — which identifier the
// document says the emitted Go reads — so `arg` carries the argument's
// name for the first two and the binding expression's root for the
// third, and `text` carries the source line so a failure names the site
// rather than describes it.
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
//
// Two scanners feed it. scanSpecSigs reads signatures the documents
// print whole; scanParamListRules reads the `<param-list>` bullets,
// where the documents print the placeholder in the signature and state
// the identifier separately in prose. Both end in the same comparison,
// and the second is the site gqlc-rz0l corrected, so it is anchored
// per-document alongside the first.
func TestSpecMethodArgIsGeneratorOwned(t *testing.T) {
	files := docFiles(t)
	require.NotEmpty(t, files, "the fence swept no documents; docRoots is stale")

	perDoc := make(map[string]int, len(files))
	perParamListDoc := make(map[string]int, len(files))
	var graded, bad, unclosed []specSig
	for _, file := range files {
		text := readDoc(t, file)

		sigs, broken := scanSpecSigs(file, text)
		unclosed = append(unclosed, broken...)
		for _, sig := range sigs {
			perDoc[file]++
			graded = append(graded, sig)
		}
		for _, sig := range scanParamListRules(file, text) {
			perParamListDoc[file]++
			graded = append(graded, sig)
		}
	}
	for _, sig := range graded {
		if sig.arg != codegen.ParamArg {
			bad = append(bad, sig)
		}
	}

	requireClean(t, unclosed, "documented parameter list does not close",
		"these documents open a parameter list on `ctx context.Context` and never close the parenthesis, so the\n"+
			"fence cannot read the argument out of them and silently graded nothing there; fix the text rather than\n"+
			"the fence — an unreadable site is an ungraded site")

	require.GreaterOrEqualf(t, len(graded), specSigFloor,
		"the fence graded only %d method signatures across %d documents, under the floor of %d — "+
			"the scanner has stopped matching and this test is passing over nothing",
		len(graded), len(files), specSigFloor)
	for _, doc := range sigAnchorDocs {
		require.NotZerof(t, perDoc[doc],
			"%s contributed no graded method signature; either its method surface has moved out of "+
				"the sweep or the scanner no longer reads it", doc)
	}
	for _, doc := range paramListAnchorDocs {
		require.NotZerof(t, perParamListDoc[doc],
			"%s contributed no graded `<param-list>` rule; either its %s bullet has been reworded out of "+
				"the sweep or the scanner no longer reads it — that bullet is where this document states "+
				"which identifier the emitted signature binds, so an unread one is an unfenced one",
			doc, paramListTerm)
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

	perDoc := make(map[string]int, len(files))
	var graded, bad, unclosed []specSig
	for _, file := range files {
		binds, broken := scanSpecBinds(file, readDoc(t, file))
		unclosed = append(unclosed, broken...)
		for _, bind := range binds {
			perDoc[file]++
			graded = append(graded, bind)
			if bind.arg != codegen.ParamArg && !strings.HasPrefix(bind.arg, codegen.ParamArg+".") {
				bad = append(bad, bind)
			}
		}
	}

	requireClean(t, unclosed, "documented map[string]any literal does not close",
		"these documents open a `map[string]any{` and never close the brace, so the fence cannot read the\n"+
			"bindings out of them and silently graded nothing there; fix the text rather than the fence — an\n"+
			"unreadable site is an ungraded site, and it is the shape a drifted binding hides behind")

	require.GreaterOrEqualf(t, len(graded), specBindFloor,
		"the fence graded only %d parameter bindings across %d documents, under the floor of %d — "+
			"the scanner has stopped matching and this test is passing over nothing",
		len(graded), len(files), specBindFloor)
	for _, doc := range bindAnchorDocs {
		require.NotZerof(t, perDoc[doc],
			"%s contributed no graded parameter binding; either its map literals have moved out of the "+
				"sweep or the scanner no longer reads it — a floor the other documents can satisfy by "+
				"themselves is not a floor on this one", doc)
	}

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
// separate a drifted signature from a correct one, so the rows below
// pair lines that were in the specs before gqlc-rz0l corrected them with
// what replaced them. Without this, a scanner that quietly matched
// nothing would leave the sweep green over any prose at all.
//
// The later rows are not spec lines: they are forms a spec could be
// rewritten into — reflowed by gofmt, respaced, or written against the
// template rather than a concrete method — where a scanner that reads
// one spelling of the anchor, or that treats a placeholder as an
// exemption, stops seeing drift it was seeing a moment before.
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
		name:    "a signature wrapped after ctx is still one signature",
		text:    "func (q *Queries) PersonById(ctx context.Context,\n    id int64) (PersonRow, error)",
		wantArg: "id",
		wantAny: true,
	}, {
		// The arm above and this one are different arms, not one rule
		// seen twice: the anchor's own whitespace is what the wrap
		// moves here, and a literal anchor matches nothing at all.
		// This is the form gofmt writes for a signature too long for
		// one line, so it is the form a reformatted spec drifts in.
		name:    "a signature wrapped before ctx is still one signature",
		text:    "func (q *Queries) PersonById(\n    ctx context.Context,\n    id int64,\n) (PersonRow, error)",
		wantArg: "id",
		wantAny: true,
	}, {
		name:    "a second space inside the anchor is not an escape hatch",
		text:    "func (q *Queries) PersonById(ctx  context.Context, id int64) (PersonRow, error)",
		wantArg: "id",
		wantAny: true,
	}, {
		name:    "a template placeholder parameter list is not graded",
		text:    "func (q *Queries) <MethodName>(ctx context.Context<param-list>) (<return>, error) {",
		wantAny: false,
	}, {
		name:    "a whole-list placeholder in parameter position is not graded",
		text:    "func (q *Queries) <MethodName>(ctx context.Context, <param-list>) (<return>, error) {",
		wantAny: false,
	}, {
		// The blocker gqlc-rz0l's first fence let through: a
		// placeholder in the type position used to skip the whole
		// declaration, discarding the name beside it — which is the
		// one thing this file grades.
		name:    "a placeholder type does not exempt the name beside it",
		text:    "func (q *Queries) <MethodName>(ctx context.Context, <bareParam> <T>) (<return>, error) {",
		wantArg: "<bareParam>",
		wantAny: true,
	}, {
		name:    "the corrected template form passes on its name",
		text:    "func (q *Queries) <MethodName>(ctx context.Context, arg <T>) (<return>, error) {",
		wantArg: codegen.ParamArg,
		wantAny: true,
	}} {
		t.Run(tc.name, func(t *testing.T) {
			got, unclosed := scanSpecSigs("witness.md", tc.text)
			require.Empty(t, unclosed)
			if !tc.wantAny {
				require.Empty(t, got)
				return
			}
			require.Len(t, got, 1)
			require.Equal(t, tc.wantArg, got[0].arg)
		})
	}
}

// TestSpecParamListRuleScannerDetectsDrift is the witness for the
// bullet scanner. The `<param-list>` bullet is where both specs state
// which identifier the emitted signature binds — the template above it
// prints only the placeholder — so it is the site gqlc-rz0l corrected
// and the site a revert lands on. Each row is a bullet body: the text
// before that commit, the text after, and the forms the scanner must
// leave alone.
func TestSpecParamListRuleScannerDetectsDrift(t *testing.T) {
	bullet := func(body string) string {
		return "- " + paramListTerm + " " + body + "\n- **`<return>`** — the return type.\n"
	}
	for _, tc := range []struct {
		name string
		text string
		want []string
	}{{
		name: "c1 §5.3, before",
		text: bullet("— empty if zero parameters, `, <bareParam> <T>` if one\n" +
			"  parameter, `, arg <MethodName>Params` if two-plus. `<bareParam>` is the\n" +
			"  single parameter's field-name mangle (§4.2), but lowercase-initial."),
		want: []string{"<bareParam>", codegen.ParamArg},
	}, {
		name: "c1 §5.3, after",
		text: bullet("— empty if zero parameters, `, arg <T>` if one\n" +
			"  parameter, `, arg <MethodName>Params` if two-plus. The argument\n" +
			"  name is the literal `arg` at both arities."),
		want: []string{codegen.ParamArg, codegen.ParamArg},
	}, {
		name: "the bullet ends at the next list item",
		text: bullet("— `, arg <T>` if one parameter.") +
			"- **`<paramsMap>`** — `, minAge int64` is not this bullet's text.\n",
		want: []string{codegen.ParamArg},
	}, {
		name: "prose code spans in the bullet are not parameter lists",
		text: bullet("— the C1 rule. `paramFieldName(\"minAge\")` → `MinAge` is what it\n" +
			"  is not; `$err`, `$q` and `$_` are why."),
		want: nil,
	}, {
		name: "a document with no such bullet contributes nothing",
		text: "The `<param-list>` placeholder is described in C1 §5.3.\n",
		want: nil,
	}} {
		t.Run(tc.name, func(t *testing.T) {
			var got []string
			for _, sig := range scanParamListRules("witness.md", tc.text) {
				got = append(got, sig.arg)
			}
			require.Equal(t, tc.want, got)
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
	}, {
		name: "a trailing comma closes the last entry rather than opening an empty one",
		text: "map[string]any{\n    \"pid\": arg.Pid,\n    \"oid\": arg.Oid,\n}",
		want: []string{"arg.Pid", "arg.Oid"},
	}} {
		t.Run(tc.name, func(t *testing.T) {
			got, unclosed := scanSpecBinds("witness.md", tc.text)
			require.Empty(t, unclosed)
			var values []string
			for _, bind := range got {
				values = append(values, bind.arg)
			}
			require.Equal(t, tc.want, values)
		})
	}
}

// TestSpecScannersReportUnreadableSites pins the two silences the
// scanners used to keep. A span that opens and never closes is not a
// site with no drift in it; it is a site the sweep could not read, and
// a sweep that drops it quietly loses coverage without the floors or
// the anchors moving. Both are surfaced so the failure names the text
// to fix rather than the fence.
func TestSpecScannersReportUnreadableSites(t *testing.T) {
	t.Run("an unterminated map literal is reported, not dropped", func(t *testing.T) {
		binds, unclosed := scanSpecBinds("witness.md", "prose\nmap[string]any{\"id\": arg\nmore prose\n")
		require.Empty(t, binds)
		require.Len(t, unclosed, 1)
		require.Equal(t, 2, unclosed[0].line)
	})
	t.Run("an unterminated parameter list is reported, not dropped", func(t *testing.T) {
		sigs, unclosed := scanSpecSigs("witness.md", "prose\nRemovePerson(ctx context.Context, arg int64\nmore prose\n")
		require.Empty(t, sigs)
		require.Len(t, unclosed, 1)
		require.Equal(t, 2, unclosed[0].line)
	})
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
// signature from one document's text, and separately every parameter
// list that opens but never closes.
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
func scanSpecSigs(file, text string) (sigs, unclosed []specSig) {
	for _, loc := range ctxAnchorRe.FindAllStringIndex(text, -1) {
		open := loc[0]
		site := specSig{
			file: file,
			line: 1 + strings.Count(text[:open], "\n"),
			text: strings.TrimSpace(collapse(lineAt(text, open))),
		}

		list, ok := parenSpan(text, open)
		if !ok {
			unclosed = append(unclosed, site)
			continue
		}
		params := splitTopLevel(list)
		if len(params) != 2 {
			continue
		}
		name, gradable := paramName(params[1])
		if !gradable {
			continue
		}
		site.arg = name
		sigs = append(sigs, site)
	}
	return sigs, unclosed
}

// scanParamListRules extracts the argument name from every
// `<param-list>` definition bullet in one document's text.
//
// The method-shape templates in C1 §5.3 and C4 §5.3 print the
// placeholder, not the list, so the paren walk above reads
// `(ctx context.Context<param-list>)` as a one-parameter list with
// nothing to grade — correctly, because a placeholder standing for a
// whole list carries no argument name. The name lives in the bullet
// that expands the placeholder, as inline code spelling the list's tail
// from its leading comma: `, arg <T>` and `, arg <MethodName>Params`.
// Those are ordinary parameter-list tails, so they split and grade on
// the same axis as a whole signature does, and reverting the bullet
// alone — which is exactly the drift gqlc-rz0l corrected — is red.
//
// Only code spans inside such a bullet are read, and only those opening
// with a comma. Prose commas in backticks are everywhere in these
// documents; a parameter-list tail inside the bullet that defines the
// parameter list is not ambiguous.
func scanParamListRules(file, text string) []specSig {
	var out []specSig
	for i := 0; ; {
		j := strings.Index(text[i:], paramListTerm)
		if j < 0 {
			return out
		}
		start := i + j + len(paramListTerm)
		i = start

		end := len(text)
		if k := strings.Index(text[start:], "\n- "); k >= 0 {
			end = start + k
		}
		for _, code := range inlineCodeSpans(text, start, end) {
			if !strings.HasPrefix(code.text, ",") {
				continue
			}
			params := splitTopLevel(code.text)
			if len(params) != 2 {
				continue
			}
			name, gradable := paramName(params[1])
			if !gradable {
				continue
			}
			out = append(out, specSig{
				file: file,
				line: 1 + strings.Count(text[:code.at], "\n"),
				arg:  name,
				text: strings.TrimSpace(collapse(lineAt(text, code.at))),
			})
		}
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
//
// A literal whose brace never closes is returned separately rather than
// dropped. Dropping it is how the sweep loses a site without saying so,
// and the site it loses is one that prints bindings.
func scanSpecBinds(file, text string) (binds, unclosed []specSig) {
	for _, loc := range mapAnchorRe.FindAllStringIndex(text, -1) {
		anchor := loc[0]
		open := loc[1] - 1
		site := specSig{
			file: file,
			line: 1 + strings.Count(text[:anchor], "\n"),
			text: strings.TrimSpace(collapse(lineAt(text, anchor))),
		}

		body, ok := span(text, open, '{', '}')
		if !ok {
			unclosed = append(unclosed, site)
			continue
		}
		for _, entry := range splitTopLevel(body) {
			colon := topLevelColon(entry)
			if colon < 0 {
				continue
			}
			// `arg.<Field1>` is the template form of a real selector; its
			// prefix is what this fence grades, so it is kept.
			bind := site
			bind.arg = unwrapConversions(strings.TrimSpace(entry[colon+1:]))
			binds = append(binds, bind)
		}
	}
	return binds, unclosed
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

// splitTopLevel splits a parameter list or composite literal at its
// depth-zero commas. A trailing comma closes the last entry rather than
// opening an empty one — gofmt writes one on every list it wraps across
// lines — so the empty tail it would otherwise produce is dropped.
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
	out = append(out, strings.TrimSpace(list[start:]))
	if len(out) > 1 && out[len(out)-1] == "" {
		out = out[:len(out)-1]
	}
	return out
}

// paramName is the identifier a parameter declaration binds, together
// with whether the declaration is one this fence can grade at all.
//
// A declaration is a name position and a type position, and only the
// name is the subject here, so the two are decided separately. Two
// tokens are a name and a type: the name is graded whatever the type
// says, which is what makes `arg <T>` and `arg <MethodName>Params` pass
// on their name while `<bareParam> <T>` — the capture vector gqlc-lhs3
// removed from the emitter — fails on its. A placeholder in the type
// position is not an exemption for the name beside it.
//
// One token is a type with no name, which is drift in its own right: a
// documented signature that names no argument is not the one the
// emitter writes.
//
// The one ungradable form is a single token that is itself a whole
// placeholder — `<param-list>`, which stands for the entire list rather
// than for one declaration and so has no name position to read. What
// that placeholder expands to is graded where the documents say it,
// in the `<param-list>` bullet (scanParamListRules).
func paramName(param string) (name string, gradable bool) {
	fields := strings.Fields(param)
	switch {
	case len(fields) == 0:
		return "", false
	case len(fields) == 1 && isWholePlaceholder(fields[0]):
		return "", false
	case len(fields) == 1:
		return "", true
	default:
		return fields[0], true
	}
}

// isWholePlaceholder reports whether a token is a spec placeholder in
// its entirety, as `<param-list>` is. `<MethodName>Params` is not one:
// it is a type built around a placeholder, and it sits in the type
// position beside a name the fence does grade.
func isWholePlaceholder(token string) bool {
	return len(token) > 2 && strings.HasPrefix(token, "<") && strings.HasSuffix(token, ">")
}

// anchorPattern compiles one anchor literal into a matcher that reads
// whitespace the way Go's tokeniser does — required between two
// identifier characters, optional at every other boundary — so the
// fence anchors on the anchor's tokens rather than on one spelling of
// the spacing between them. gofmt wraps a long parameter list after the
// open paren, which puts `ctx` on its own line and leaves a literal
// match finding nothing; prose reflows the same way, and a second space
// is invisible in rendered markdown.
//
// The normalisation is compiled into the pattern rather than applied to
// the documents, because collapsing a document's whitespace would move
// every byte in it and the failure messages here are only useful while
// they can still name a line. Deriving the pattern from the one literal
// rather than writing a second spelling beside it leaves nothing for
// the two to drift apart on.
func anchorPattern(anchor string) *regexp.Regexp {
	var b strings.Builder
	for i := 0; i < len(anchor); i++ {
		c := anchor[i]
		if c == ' ' {
			b.WriteString(`\s+`)
			continue
		}
		if i > 0 && anchor[i-1] != ' ' && isIdentByte(c) != isIdentByte(anchor[i-1]) {
			b.WriteString(`\s*`)
		}
		b.WriteString(regexp.QuoteMeta(string(c)))
	}
	return regexp.MustCompile(b.String())
}

// codeSpan is one inline `code` run, carrying the offset it opened at so
// a failure can name the line it sits on.
type codeSpan struct {
	text string
	at   int
}

// inlineCodeSpans returns every single-backtick code run in
// text[from:to], contents collapsed. Backticks are paired in order, so
// the prose between two runs is skipped the way a markdown renderer
// skips it.
func inlineCodeSpans(text string, from, to int) []codeSpan {
	var out []codeSpan
	for i := from; i < to; {
		open := strings.IndexByte(text[i:to], '`')
		if open < 0 {
			return out
		}
		open += i
		closing := strings.IndexByte(text[open+1:to], '`')
		if closing < 0 {
			return out
		}
		closing += open + 1
		out = append(out, codeSpan{text: collapse(text[open+1 : closing]), at: open})
		i = closing + 1
	}
	return out
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
