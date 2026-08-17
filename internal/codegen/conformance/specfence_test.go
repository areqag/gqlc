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

// Every `Name(ctx context.Context, …)` the documentation prints is a
// claim about bytes this package emits. This file holds those claims to
// codegen.ParamArg, read from the emitter, so that renaming the constant
// reddens the documents instead of widening the gap between them.
//
// Three shapes are graded, because the documents write the name in three
// places: signatures printed whole, the `<param-list>` bullets that
// expand the placeholder those signatures print instead of a list, and
// the values of the driver-binding map literals. The middle one is the
// normative rule, and prose is where the drift gqlc-rz0l corrected sat.
//
// What is graded is a site, not a document, and the documents can still
// disagree with codegen.ParamArg and stay green in at least five
// places. The count is a floor rather than a census: it records what
// has been measured. A signature carrying the author's names as
// separate arguments is past the arity read here (gqlc-vu7z); prose
// beside an intact graded span can say the opposite of it (gqlc-e143);
// a binding stated with no map literal around it is unread
// (gqlc-offa); a listed document keeps its census entry on one
// surviving site (gqlc-0rjn); and a list replacing one of the exhibits
// specBareListExhibits names, spelled the same way, takes that entry's
// exemption (gqlc-x2sg).

// docRoots are the trees the fence sweeps, relative to repoRoot. The
// drift reached C1, C3, C4 and C5, and an ADR or a design note prints
// the same signatures, so the whole of docs/ is in scope.
//
// A root is held here only by what the censuses below name beneath it.
// Deleting `docs` is red — every censused document is under it, and each
// is reported by name. So is narrowing it to `docs/specs`, because
// specBareListExhibits names a document under `docs/adr/`. Deleting
// `README.md` or `CONTEXT.md` is green, because no census names a
// document under either. A root that stops existing on disk fails in
// docFiles.
//
// This file cannot close that: everything it observes is walked out of
// docRoots, so a census over what a sweep produced is derived from the
// list being audited. Catching a root that shrank, or one that should
// have grown, needs candidate roots from outside the list — the
// repository's own markdown — which is gqlc-jfwo. `AGENTS.md`,
// `CLAUDE.md` and `CONTRIBUTING.md` sit beside the two files named here,
// are not swept, and print no query method and no driver-binding literal
// today.
var docRoots = []string{"docs", "README.md", "CONTEXT.md"}

// repoRoot is where this package sits relative to the tree above. Swept
// documents are named relative to it — a failure that says
// `docs/specs/codegen-stage-c1.md` names a path the reader can open.
const repoRoot = "../../../"

// --- the censuses -----------------------------------------------------------
//
// Which documents each sweep is answerable to is written down here, by
// name, and reconciled against what the sweep graded — in both
// directions, naming the document in the failure (ADR 0029, decisions 1
// and 2). Adding or removing a document costs one line, and the failure
// prints the line to add or remove.
//
// The three sets differ because the documents do: C3's width and
// nullability bullets print the binding literal without the method
// around it, so C3 owes a binding and owes no signature.
//
// What these censuses do not reach, in ascending size:
//
// Deleting a documented surface and its line below together — a
// two-part edit whose second part is the record of it.
//
// A document these censuses are never pointed at: the sets below name
// documents, not roots, so they are silent about anything docRoots does
// not reach (gqlc-jfwo).
//
// One graded site per listed document is the floor, not one per site.
// Every site past the first can leave the sweep with nothing said — not
// by being corrected, but by ceasing to print an anchor, each of which
// is a delimiter as well as the text after it: `(ctx context.Context`
// or a code span's backtick before it for a signature,
// `map[string]any` with its opening brace for a binding. C4 §3.2's
// WriteQuerier member `RemovePerson(ctx context.Context, arg int64)` is
// one of ten graded signatures in that document, and rewriting its
// context parameter leaves every sweep here green (gqlc-0rjn, ADR 0029
// decision 3; docs/specs/codegen-stage-c1.md §5.3 states it to the
// reader).
const (
	specC1   = "docs/specs/codegen-stage-c1.md"
	specC3   = "docs/specs/codegen-stage-c3.md"
	specC4   = "docs/specs/codegen-stage-c4.md"
	specC5   = "docs/specs/codegen-stage-c5.md"
	adrFence = "docs/adr/0029-the-codegen-spec-fence.md"
)

// specSigDocs are the documents that print an emitted query method
// signature whole, and so must contribute at least one graded argument
// name.
var specSigDocs = []string{specC1, specC4, specC5}

// specBindDocs are the documents that print a `map[string]any` driver
// binding, and so must contribute at least one graded binding value.
var specBindDocs = []string{specC1, specC3, specC4, specC5}

// specListRuleDocs are the documents whose method-shape template prints
// a placeholder standing for the whole parameter list. Such a document
// names no argument in the template, so the bullet that expands the
// placeholder is the only place it states which identifier the emitted
// signature binds — and that bullet is the exact text gqlc-rz0l
// corrected.
//
// A document here must both state the expansions (specListRules) and be
// one whose signatures the fence let past ungraded on account of the
// placeholder; a document not here may not have a whole-list placeholder
// waved through (ADR 0029 decision 4).
var specListRuleDocs = []string{specC1, specC4}

// specBareListExhibits are the parenthesis-less parameter lists a
// document prints as exhibits of what the fence catches rather than as
// claims about the emitted surface, and so are read but not graded (ADR
// 0029 decision 10).
//
// The exemption is per list, spelled verbatim: a parenthesis-less list
// a listed document prints that is not written down here is read on the
// same terms as any other document's, so one document can quote a
// drifted shape as an exhibit and state the emitted shape as a claim. Each entry exempts one site,
// so a second list spelled the same way is graded. The first is not: a
// claim put in an exhibit's place, spelled the way that exhibit was,
// takes the entry (gqlc-x2sg).
//
// An entry the document stopped printing is red by its text, so the
// exemption cannot run ahead of the exhibit needing it. The other
// direction is reconciled too but cannot fire on a document's text: a
// list this census does not name is graded rather than recorded, so the
// sweep produces no entry the census lacks.
var specBareListExhibits = map[string][]string{
	adrFence: {
		"ctx context.Context, <bareParam> <T>",
		"ctx context.Context<bareParam> <T>",
		"ctx context.Context, minAge int64",
	},
}

// exhibitCensus flattens specBareListExhibits to one entry per exempted
// list, so that the reconciliation names the list to add or remove
// rather than the document holding it.
func exhibitCensus() []string {
	var out []string
	for doc, lists := range specBareListExhibits {
		for _, list := range lists {
			out = append(out, exhibitEntry(doc, list))
		}
	}
	return out
}

// exhibitEntry is how one exempted list is named, on both the written
// side and the observed one, so that a census entry and the site it
// exempts cannot drift apart in spelling.
func exhibitEntry(doc, list string) string { return doc + ": " + list }

// specListRules are the parameter-list tails every `<param-list>` bullet
// must spell out. Both arities are here because the emitted signature
// binds codegen.ParamArg at one query parameter and at two-plus, and an
// arity left to prose is an arity outside the fence.
//
// Membership is by identity, so a bullet's tail that is not one of these
// is an extra span, and an extra span is as red as a missing one (ADR
// 0029 decision 5).
//
// The name in each is codegen.ParamArg, so renaming the emitter's
// constant reddens every document that states these tails.
var specListRules = []string{
	", " + codegen.ParamArg + " <T>",
	", " + codegen.ParamArg + " <MethodName>Params",
}

// ctxParam is the first parameter of every emitted query method. Each
// anchor is that parameter behind a delimiter, and the delimiter is part
// of the anchor: dropping it takes the site out of that sweep.
//
// ctxAnchor's open paren reaches the interface members as well as the
// funcs — C4 §3.2's WriteQuerier block is a declared surface. tickAnchor
// reaches the lists a document prints inside an inline code span with
// the parens off, where the span's closing backtick ends the list (ADR
// 0029 decision 10).
const (
	ctxParam   = "ctx context.Context"
	ctxAnchor  = "(" + ctxParam
	tickAnchor = "`" + ctxParam
)

// mapAnchor opens the driver-binding map literal an emitted method body
// passes to the run seam.
const mapAnchor = "map[string]any{"

// paramListTerm opens the bullet each spec uses to define what fills the
// `<param-list>` placeholder its method-shape template prints. That
// bullet is the normative rule: the template shows only the placeholder,
// so the bullet is the one place either document says which identifier
// the emitted signature binds. It is prose, so no anchor above reaches
// it, and it is the text gqlc-rz0l corrected.
const paramListTerm = "**`<param-list>`**"

// Anchors are matched with whitespace read the way Go's tokeniser reads
// it — see anchorPattern.
var (
	ctxAnchorRe  = anchorPattern(ctxAnchor)
	mapAnchorRe  = anchorPattern(mapAnchor)
	tickAnchorRe = anchorPattern(tickAnchor)

	// ctxParamRe reads the context parameter off the head of a
	// declaration so whatever follows it inside the same parameter can be
	// examined — which is where `(ctx context.Context<param-list>)` puts
	// its placeholder, with no comma to split on.
	ctxParamRe = regexp.MustCompile(`^(?:` + anchorPattern(ctxParam).String() + `)`)
)

// paramListPlaceholder is the bare placeholder inside paramListTerm's
// markdown emphasis, derived from the term so the template's placeholder
// and the bullet the scanner reads cannot drift apart.
var paramListPlaceholder = strings.Trim(paramListTerm, "*`")

// specSig is one graded site: a documented method signature taking one
// argument after the context, the parameter-list tail a `<param-list>`
// bullet expands that placeholder to, or one documented driver-binding
// entry. All three reduce to the same question — which identifier the
// document says the emitted Go reads — so `arg` carries the argument's
// name for the first two and the binding expression's root for the
// third, and `text` carries the source line so a failure names the site.
//
// `rule` is the verbatim parameter-list tail a `<param-list>` bullet
// spelled, empty at every other site, and is what the rule census
// reconciles by identity (ADR 0029 decision 5). `list` is the verbatim
// parameter list a code span printed with the parentheses off, empty at
// every other site, and is what the exhibit census reconciles by
// identity (ADR 0029 decision 10).
type specSig struct {
	file string
	line int
	arg  string
	rule string
	list string
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
// Three scanners feed it. scanSpecSigs reads the signatures a document
// prints whole; scanBareSigs reads the parameter lists it prints inside
// a code span with the parens off; scanParamListRules reads the
// `<param-list>` bullets, where the signature carries a placeholder and
// the identifier is stated separately in prose. All three end in the
// same comparison, and the bullets are the site gqlc-rz0l corrected, so
// their tails are also reconciled against specListRules.
func TestSpecMethodArgIsGeneratorOwned(t *testing.T) {
	files := docFiles(t)
	require.NotEmpty(t, files, "the fence swept no documents; docRoots is stale")

	sweep := sweepSigs(files, func(file string) string { return readDoc(t, file) }, specBareListExhibits)

	var bad []specSig
	for _, sig := range sweep.graded {
		if sig.arg != codegen.ParamArg {
			bad = append(bad, sig)
		}
	}

	requireClean(t, sweep.unclosed, "documented parameter list does not close",
		"these documents open a parameter list on `ctx context.Context` and never close the delimiter that ends\n"+
			"it — the parenthesis, or the backtick of the code span it is printed in — so the fence cannot read\n"+
			"the argument out of them and silently graded nothing there; fix the text rather than the fence — an\n"+
			"unreadable site is an ungraded site")

	requireClean(t, bad, "documented method argument is not generator-owned",
		fmt.Sprintf("these documented signatures name the emitted method argument after the query author's parameter\n"+
			"instead of after the generator; the emitter binds codegen.ParamArg (%q) at every arity, precisely so\n"+
			"that no author-chosen identifier reaches the scope the method body resolves in (gqlc-lhs3, gqlc-rz0l)",
			codegen.ParamArg))

	requireCensus(t, specSigDocs, sweep.sigDocs, "specSigDocs",
		"each document on this list prints an emitted query method whose argument this fence reads, so it must\n"+
			"contribute at least one graded signature; contributing none means its method surface has moved out\n"+
			"of the sweep, or the scanner no longer reads it")

	requireCensus(t, specListRuleDocs, sweep.ruleDocs, "specListRuleDocs",
		"each document on this list prints a placeholder where its method-shape template's parameters go, so\n"+
			"the "+paramListTerm+" bullet expanding that placeholder is the only place it states which\n"+
			"identifier the emitted signature binds; an unread bullet is an unfenced one")

	requireCensus(t, specListRuleDocs, sweep.exemptDocs, "specListRuleDocs",
		"this list is also what a whole-list placeholder is exempted against, because the exemption and the\n"+
			"requirement have to be the same set or one of them is free. A document that prints such a\n"+
			"placeholder names no argument in its template and owes the bullet instead, so it belongs here; a\n"+
			"document here that exempts nothing is not printing the template it is listed for, and its parameter\n"+
			"lists belong to the signature sweep")

	requireCensus(t, exhibitCensus(), sweep.bareExhibits, "specBareListExhibits",
		"each entry above is one parameter list its document prints without the enclosing parentheses, as an\n"+
			"exhibit of what the fence catches rather than as a claim about the emitted surface, and is read but\n"+
			"not graded there; a list the document stopped printing means the exemption is holding nothing, and\n"+
			"a list it prints that is not spelled above is read on the same terms as any other document's")

	for _, doc := range specListRuleDocs {
		requireCensus(t, specListRules, sweep.statedRules[doc], "specListRules, in "+doc,
			"the emitted signature binds codegen.ParamArg at one query parameter and at two-plus, and the\n"+
				"entries above are the two tails the "+paramListTerm+" bullet states verbatim. A missing one is\n"+
				"an arity reworded back into prose where nothing grades it; an extra one is a spelling this list\n"+
				"does not recognise as a rule — a restatement or an illustration standing in for the arity it is\n"+
				"not, which is how both arities once read as stated while only one of them was")
	}
}

// TestSpecParamsMapBindsGeneratorOwnedValue is the same fence one step
// further into the body. The driver-binding map's key and value are
// separately owned, and only the value moved: the key stays the raw
// parameter name the query text writes after the dollar sign, because
// that is what the driver substitutes on, while the value is an
// expression in the emitted method's Go scope and so has to be
// codegen.ParamArg. Only the value is graded: rewriting the key to match
// would break the binding outright.
func TestSpecParamsMapBindsGeneratorOwnedValue(t *testing.T) {
	files := docFiles(t)
	require.NotEmpty(t, files, "the fence swept no documents; docRoots is stale")

	sweep := sweepBinds(files, func(file string) string { return readDoc(t, file) })

	var bad []specSig
	for _, bind := range sweep.graded {
		if bind.arg != codegen.ParamArg && !strings.HasPrefix(bind.arg, codegen.ParamArg+".") {
			bad = append(bad, bind)
		}
	}

	requireClean(t, sweep.unclosed, "documented map[string]any literal does not close",
		"these documents open a `map[string]any{` and never close the brace, so the fence cannot read the\n"+
			"bindings out of them and silently graded nothing there; fix the text rather than the fence — an\n"+
			"unreadable site is an ungraded site, and it is the shape a drifted binding hides behind")

	requireClean(t, bad, "documented parameter binding is not generator-owned",
		fmt.Sprintf("these documented map[string]any entries bind a value that is not codegen.ParamArg (%q) or a\n"+
			"field selected from it; the emitter's paramsMapText / argsMapText compose every value from that\n"+
			"one identifier, and only the map key carries the author's parameter name (gqlc-lhs3, gqlc-rz0l)",
			codegen.ParamArg))

	// A document quoting a signature owes a binding only while it is
	// listed here (ADR 0029 decision 9).
	requireCensus(t, specBindDocs, sweep.bindDocs, "specBindDocs",
		"each document on this list prints the `map[string]any` the emitted body passes to the run seam, so it\n"+
			"must contribute at least one graded binding; contributing none means its literals have moved out of\n"+
			"the sweep, or the scanner no longer reads them")
}

// sigSweep is one pass of the signature scanners over a set of
// documents: every site they graded, every site they could not read,
// and which documents produced each outcome a census reconciles.
//
// bindSweep is the same for the binding scanner.
//
// Both accumulations take their document set, reader and exhibits as
// parameters so a witness can supply all three and observe the judgement
// on a clean tree (ADR 0029 decision 7). Each line carrying a scanner's
// return into an accumulator is load-bearing and silent when dropped;
// TestSpecSweepsCarryUnreadableSites and
// TestSpecSweepRoutesBareSitesByExhibit are the witnesses.
type sigSweep struct {
	graded       []specSig
	unclosed     []specSig
	sigDocs      map[string]bool
	exemptDocs   map[string]bool
	ruleDocs     map[string]bool
	bareExhibits map[string]bool
	statedRules  map[string]map[string]bool
}

type bindSweep struct {
	graded   []specSig
	unclosed []specSig
	bindDocs map[string]bool
}

// sweepSigs runs all three signature scanners over every named document,
// reading each one with read and treating the parenthesis-less
// parameter lists that exhibits names for a document as
// read-but-not-graded. Each name covers one site; every other bare list
// in the same document is read on the same terms as any other's.
func sweepSigs(files []string, read func(string) string, exhibits map[string][]string) sigSweep {
	out := sigSweep{
		sigDocs:      map[string]bool{},
		exemptDocs:   map[string]bool{},
		ruleDocs:     map[string]bool{},
		bareExhibits: map[string]bool{},
		statedRules:  map[string]map[string]bool{},
	}
	for _, file := range files {
		text := read(file)

		sigs, exempt, broken := scanSpecSigs(file, text)
		out.unclosed = append(out.unclosed, broken...)
		if len(exempt) > 0 {
			out.exemptDocs[file] = true
		}

		bare, bareBroken := scanBareSigs(file, text)
		out.unclosed = append(out.unclosed, bareBroken...)
		unclaimed := map[string]bool{}
		for _, list := range exhibits[file] {
			unclaimed[list] = true
		}
		for _, sig := range bare {
			if !unclaimed[sig.list] {
				out.graded = append(out.graded, sig)
				continue
			}
			delete(unclaimed, sig.list)
			out.bareExhibits[exhibitEntry(file, sig.list)] = true
		}

		for _, sig := range sigs {
			out.sigDocs[file] = true
			out.graded = append(out.graded, sig)
		}
		for _, sig := range scanParamListRules(file, text) {
			out.ruleDocs[file] = true
			if out.statedRules[file] == nil {
				out.statedRules[file] = map[string]bool{}
			}
			out.statedRules[file][sig.rule] = true
			out.graded = append(out.graded, sig)
		}
	}
	return out
}

// sweepBinds runs the binding scanner over every named document.
func sweepBinds(files []string, read func(string) string) bindSweep {
	out := bindSweep{bindDocs: map[string]bool{}}
	for _, file := range files {
		binds, broken := scanSpecBinds(file, read(file))
		out.unclosed = append(out.unclosed, broken...)
		for _, bind := range binds {
			out.bindDocs[file] = true
			out.graded = append(out.graded, bind)
		}
	}
	return out
}

// TestSpecSweepsCarryUnreadableSites is the witness for that carrying —
// the join between the scanners reporting an unterminated span
// (TestSpecScannersReportUnreadableSites) and requireClean failing on a
// non-empty set (TestSpecFailuresAreWired).
//
// The document set is synthetic because a repository holding this input
// is a repository the fence is already failing on. The unreadable
// document is swept first, so that the readable one behind it still
// being graded is asserted: carrying the site correctly and giving up on
// the rest of the corpus would satisfy every other assertion here.
func TestSpecSweepsCarryUnreadableSites(t *testing.T) {
	docs := map[string]string{
		"readable.md": "func (q *Queries) PersonById(ctx context.Context, arg int64) (PersonRow, error)\n" +
			"- " + paramListTerm + " — `, " + codegen.ParamArg + " <T>` if one parameter.\n" +
			`map[string]any{"id": arg}` + "\n",
		"unreadable.md": "prose\n" +
			"RemovePerson(ctx context.Context, arg int64\n" +
			"more prose\n" +
			`map[string]any{"id": arg` + "\n",
	}
	files := []string{"unreadable.md", "readable.md"}
	read := func(file string) string { return docs[file] }

	t.Run("the signature sweep carries an unreadable parameter list out of the scanner", func(t *testing.T) {
		sweep := sweepSigs(files, read, nil)
		require.Len(t, sweep.unclosed, 1)
		require.Equal(t, "unreadable.md", sweep.unclosed[0].file)
		require.Equal(t, 2, sweep.unclosed[0].line)
		require.NotEmpty(t, sweep.graded, "the readable document is still swept")
		require.True(t, sweep.sigDocs["readable.md"])
	})

	t.Run("the binding sweep carries an unreadable literal out of the scanner", func(t *testing.T) {
		sweep := sweepBinds(files, read)
		require.Len(t, sweep.unclosed, 1)
		require.Equal(t, "unreadable.md", sweep.unclosed[0].file)
		require.Equal(t, 4, sweep.unclosed[0].line)
		require.NotEmpty(t, sweep.graded, "the readable document is still swept")
		require.True(t, sweep.bindDocs["readable.md"])
	})
}

// TestSpecSweepRoutesBareSitesByExhibit is the witness for the lines
// deciding where a parenthesis-less parameter list lands. The document
// set is synthetic: the repository's one listed document prints each of
// its three exhibits exactly once, so it exercises neither a second copy
// of one nor a declared exhibit that has gone missing.
//
// An exhibit is exempt only while the document prints it, which is what
// the third row holds — without it a census entry outlives the exhibit
// it covers. The fourth and fifth hold the exemption to the list the
// census names, and to one site of that list: a listed document states
// claims about the emitted surface too, and a claim can be spelled the
// way an exhibit is.
func TestSpecSweepRoutesBareSitesByExhibit(t *testing.T) {
	const drifted = "ctx context.Context, minAge int64"
	claim := "ctx context.Context, " + codegen.ParamArg + " int64"
	docs := map[string]string{
		"graded.md":  "the parameter list is `" + drifted + "`\n",
		"exhibit.md": "the parameter list is `" + drifted + "`\n",
		"silent.md":  "this document quotes no parameter list at all\n",
		"mixed.md":   "the exhibit is `" + drifted + "` and the emitted list is `" + claim + "`\n",
		"twice.md":   "the exhibit is `" + drifted + "` and so is `" + drifted + "`\n",
	}
	read := func(file string) string { return docs[file] }
	listing := func(file string) map[string][]string {
		return map[string][]string{file: {drifted}}
	}

	t.Run("an unlisted document's bare list is graded", func(t *testing.T) {
		sweep := sweepSigs([]string{"graded.md"}, read, nil)
		require.Len(t, sweep.graded, 1)
		require.Equal(t, "minAge", sweep.graded[0].arg)
		require.Empty(t, sweep.bareExhibits)
	})

	t.Run("a listed bare list is censused instead of graded", func(t *testing.T) {
		sweep := sweepSigs([]string{"exhibit.md"}, read, listing("exhibit.md"))
		require.Empty(t, sweep.graded)
		require.Equal(t, map[string]bool{"exhibit.md: " + drifted: true}, sweep.bareExhibits)
	})

	t.Run("a listed bare list the document stopped printing is not censused", func(t *testing.T) {
		sweep := sweepSigs([]string{"silent.md"}, read, listing("silent.md"))
		require.Empty(t, sweep.bareExhibits, "the lost direction is what reports this")
	})

	t.Run("a listed document's unlisted bare list is graded", func(t *testing.T) {
		sweep := sweepSigs([]string{"mixed.md"}, read, listing("mixed.md"))
		require.Len(t, sweep.graded, 1)
		require.Equal(t, codegen.ParamArg, sweep.graded[0].arg)
		require.Equal(t, map[string]bool{"mixed.md: " + drifted: true}, sweep.bareExhibits)
	})

	t.Run("a second copy of a listed bare list is graded", func(t *testing.T) {
		sweep := sweepSigs([]string{"twice.md"}, read, listing("twice.md"))
		require.Len(t, sweep.graded, 1)
		require.Equal(t, "minAge", sweep.graded[0].arg)
		require.Equal(t, map[string]bool{"twice.md: " + drifted: true}, sweep.bareExhibits)
	})
}

// requireCensus reconciles a written census against what a sweep
// actually observed, in both directions, naming the offending entry in
// the failure (ADR 0029 decision 2).
//
// Declared and unobserved is the case this file exists for one level up:
// a document that still prints the surface and no longer contributes to
// the sweep.
//
// Observed and undeclared is the census auditing its own scope. A
// document that starts printing the surface is red until it is written
// down, and the failure says which line to add.
//
// A name declared twice is refused: either copy could then be deleted
// under cover of the other.
func requireCensus(t fenceT, written []string, observed map[string]bool, census, why string) {
	t.Helper()

	lost, undeclared, duplicated := reconcile(written, observed)

	if len(duplicated) > 0 {
		require.Fail(t, census+" names the same entry twice",
			"either copy could be deleted under cover of the other, so the census would not notice losing one:\n"+
				indent(duplicated))
	}
	if len(lost) > 0 {
		require.Fail(t, census+" declares an entry the sweep did not produce",
			census+" declares these and the sweep produced none of them:\n"+indent(lost)+"\n\n"+why)
	}
	if len(undeclared) > 0 {
		require.Fail(t, "the sweep produced an entry "+census+" does not declare",
			"the sweep produced these and "+census+" does not declare them:\n"+indent(undeclared)+
				"\n\nadd each to "+census+", so that losing it later is noticed. The requirement there is\n"+
				"written down rather than read off the text being graded, precisely so that editing the text\n"+
				"cannot edit the requirement along with it.\n\n"+why)
	}
}

// fenceT is the part of *testing.T the two helpers above use, narrowed so
// a witness can stand in for it (ADR 0029 decision 7). The witnesses are
// TestSpecFailuresAreWired, TestSpecCensusReconcilesBothDirections,
// TestSpecSweepsCarryUnreadableSites and
// TestSpecSweepRoutesBareSitesByExhibit; the sweeps' own comparison
// bodies are outside that set.
type fenceT interface {
	require.TestingT
	Helper()
}

// fenceFailNow is how a witness unwinds a helper that has failed.
// require.Fail ends with FailNow, which must not return to its caller, so
// the recorder panics with this and captureFence recovers it.
type fenceFailNow struct{}

// fenceRecorder implements fenceT by recording the failure instead of
// ending the run.
type fenceRecorder struct {
	failed bool
	msg    string
}

func (r *fenceRecorder) Helper() {}

func (r *fenceRecorder) Errorf(format string, args ...any) {
	r.failed = true
	r.msg = fmt.Sprintf(format, args...)
}

func (r *fenceRecorder) FailNow() { panic(fenceFailNow{}) }

func captureFence(call func(fenceT)) (rec *fenceRecorder) {
	rec = &fenceRecorder{}
	defer func() {
		if p := recover(); p != nil {
			if _, ours := p.(fenceFailNow); !ours {
				panic(p)
			}
		}
	}()
	call(rec)
	return rec
}

// TestSpecFailuresAreWired is the witness for that wiring. Each row calls
// one helper directly and asserts whether it failed and which arm it
// failed on, because an arm that reports another arm's headline is a
// mutation the sweeps cannot distinguish from a correct one either.
func TestSpecFailuresAreWired(t *testing.T) {
	drift := specSig{
		file: "witness.md",
		line: 7,
		text: "func (q *Queries) PersonById(ctx context.Context, id int64) error",
	}
	for _, tc := range []struct {
		name     string
		call     func(fenceT)
		wantFail bool
		wantMsg  []string
	}{{
		name: "requireClean passes over an empty set",
		call: func(ft fenceT) { requireClean(ft, nil, "headline", "why") },
	}, {
		name:     "requireClean fails and names the site",
		call:     func(ft fenceT) { requireClean(ft, []specSig{drift}, "headline", "why") },
		wantFail: true,
		wantMsg:  []string{"headline", "witness.md:7"},
	}, {
		name: "requireCensus passes over an exact match",
		call: func(ft fenceT) {
			requireCensus(ft, []string{"a"}, map[string]bool{"a": true}, "census", "why")
		},
	}, {
		name: "requireCensus fails on a declared entry the sweep did not produce",
		call: func(ft fenceT) {
			requireCensus(ft, []string{"a", "b"}, map[string]bool{"a": true}, "census", "why")
		},
		wantFail: true,
		wantMsg:  []string{"census declares an entry the sweep did not produce", "b"},
	}, {
		name: "requireCensus fails on an entry the sweep produced and nobody declared",
		call: func(ft fenceT) {
			requireCensus(ft, []string{"a"}, map[string]bool{"a": true, "b": true}, "census", "why")
		},
		wantFail: true,
		wantMsg:  []string{"the sweep produced an entry census does not declare", "b"},
	}, {
		name: "requireCensus fails on a name declared twice",
		call: func(ft fenceT) {
			requireCensus(ft, []string{"a", "a"}, map[string]bool{"a": true}, "census", "why")
		},
		wantFail: true,
		wantMsg:  []string{"census names the same entry twice"},
	}} {
		t.Run(tc.name, func(t *testing.T) {
			rec := captureFence(tc.call)
			require.Equal(t, tc.wantFail, rec.failed, "failed")
			for _, want := range tc.wantMsg {
				require.Contains(t, rec.msg, want)
			}
		})
	}
}

// reconcile is requireCensus's whole judgement, extracted so a witness
// can drive it directly (ADR 0029 decision 7).
func reconcile(written []string, observed map[string]bool) (lost, undeclared, duplicated []string) {
	declared := make(map[string]bool, len(written))
	seen := make(map[string]bool, len(written))
	for _, name := range written {
		if seen[name] {
			duplicated = append(duplicated, name)
		}
		seen[name] = true
		declared[name] = true
	}
	for name := range declared {
		if !observed[name] {
			lost = append(lost, name)
		}
	}
	for name := range observed {
		if !declared[name] {
			undeclared = append(undeclared, name)
		}
	}
	sort.Strings(lost)
	sort.Strings(undeclared)
	sort.Strings(duplicated)
	return lost, undeclared, duplicated
}

// requireClean fails with one message naming every offending site, so a
// reader fixing the drift sees all of it at once.
func requireClean(t fenceT, bad []specSig, headline, why string) {
	t.Helper()
	if len(bad) == 0 {
		return
	}
	lines := make([]string, 0, len(bad))
	for _, sig := range bad {
		lines = append(lines, sig.String())
	}
	require.Fail(t, headline, why+":\n"+indent(lines))
}

func indent(lines []string) string {
	return "  " + strings.Join(lines, "\n  ")
}

// TestSpecCensusReconcilesBothDirections is the witness for the
// judgement every census in this file rests on, and one the sweeps above
// cannot exercise while the tree is clean (ADR 0029 decision 7).
func TestSpecCensusReconcilesBothDirections(t *testing.T) {
	observed := func(names ...string) map[string]bool {
		out := map[string]bool{}
		for _, n := range names {
			out[n] = true
		}
		return out
	}
	for _, tc := range []struct {
		name         string
		written      []string
		observed     map[string]bool
		lost, undecl []string
		duplicated   []string
	}{{
		name:     "an exact match is clean",
		written:  []string{"a", "b"},
		observed: observed("a", "b"),
	}, {
		name:     "a declared entry the sweep stopped producing is named",
		written:  []string{"a", "b"},
		observed: observed("a"),
		lost:     []string{"b"},
	}, {
		name:     "an entry the sweep produced and nobody declared is named",
		written:  []string{"a"},
		observed: observed("a", "b"),
		undecl:   []string{"b"},
	}, {
		// One lost and one gained: same size, different set.
		name:     "a swap is not a wash",
		written:  []string{"a", "b"},
		observed: observed("a", "c"),
		lost:     []string{"b"},
		undecl:   []string{"c"},
	}, {
		name:       "a name declared twice is refused",
		written:    []string{"a", "a"},
		observed:   observed("a"),
		duplicated: []string{"a"},
	}, {
		name:    "an empty sweep loses every declared entry",
		written: []string{"a", "b"},
		lost:    []string{"a", "b"},
	}} {
		t.Run(tc.name, func(t *testing.T) {
			lost, undecl, duplicated := reconcile(tc.written, tc.observed)
			require.Equal(t, tc.lost, lost, "lost")
			require.Equal(t, tc.undecl, undecl, "undeclared")
			require.Equal(t, tc.duplicated, duplicated, "duplicated")
		})
	}
}

// TestSpecSigScannerDetectsDrift pins scanSpecSigs's judgement. The
// early rows pair lines that were in the specs before gqlc-rz0l
// corrected them with what replaced them; the later rows are forms a
// spec could be rewritten into — reflowed by gofmt, respaced, or written
// against the method-shape template — and each holds one spelling of the
// anchor or one placeholder position readable.
//
// `wantExempt` is the third outcome. A whole-list placeholder is a site
// the fence declines to read a name out of and owes against
// specListRuleDocs; a zero-parameter method is a site with no name to
// read, owed against nothing. The two stay distinct (ADR 0029 decision
// 4).
func TestSpecSigScannerDetectsDrift(t *testing.T) {
	for _, tc := range []struct {
		name       string
		text       string
		wantArg    string
		wantAny    bool
		wantExempt bool
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
		name:    "c5 §5.5 worked example, before",
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
		// A lone `<bareParam>` is the author's name standing where a
		// declaration should be, and grades on the row above's arm (ADR
		// 0029 decision 6).
		name:    "a lone author-named placeholder is not a whole-list placeholder",
		text:    "func (q *Queries) PersonById(ctx context.Context, <bareParam>) (PersonRow, error)",
		wantArg: "",
		wantAny: true,
	}, {
		name: "zero-parameter methods have no argument to grade",
		text: "func (q *Queries) AllPeopleNames(ctx context.Context) ([]string, error)",
	}, {
		name: "the driverOrTx.run seam is not a query method",
		text: "func (d driverDB) run(ctx context.Context, cypher string, params map[string]any, access neo4j.AccessMode) ([]*neo4j.Record, error) {",
	}, {
		name:    "a signature wrapped after ctx is still one signature",
		text:    "func (q *Queries) PersonById(ctx context.Context,\n    id int64) (PersonRow, error)",
		wantArg: "id",
		wantAny: true,
	}, {
		// The wrap lands inside the anchor here, which is the arm
		// anchorPattern's `\s+` covers. gofmt writes this form for a
		// signature too long for one line.
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
		name:       "a template placeholder parameter list is exempted, and says so",
		text:       "func (q *Queries) <MethodName>(ctx context.Context<param-list>) (<return>, error) {",
		wantExempt: true,
	}, {
		// The reformat measured in ADR 0029 decision 1.
		name:       "a whole-list placeholder moved past the comma is the same exemption",
		text:       "func (q *Queries) <MethodName>(ctx context.Context, <param-list>) (<return>, error) {",
		wantExempt: true,
	}, {
		// The spelling the interface blocks use when they show two
		// members at once.
		name:       "the numbered whole-list placeholder is the same exemption",
		text:       "    <MethodName1>(ctx context.Context, <param-list-1>) (<return-1>, error)",
		wantExempt: true,
	}, {
		name:       "the numbered placeholder is the same exemption without the comma too",
		text:       "    <MethodName1>(ctx context.Context<param-list-1>) (<return-1>, error)",
		wantExempt: true,
	}, {
		// A placeholder in the type position is not an exemption for the
		// name beside it: the name is the one thing this sweep grades.
		name:    "a placeholder type does not exempt the name beside it",
		text:    "func (q *Queries) <MethodName>(ctx context.Context, <bareParam> <T>) (<return>, error) {",
		wantArg: "<bareParam>",
		wantAny: true,
	}, {
		name:    "the corrected template form passes on its name",
		text:    "func (q *Queries) <MethodName>(ctx context.Context, arg <T>) (<return>, error) {",
		wantArg: codegen.ParamArg,
		wantAny: true,
	}, {
		// The glued position is where C4's interface template writes its
		// placeholder (`(ctx context.Context<param-list-1>)`). The
		// exemption census cannot cover this row: C4 keeps a second
		// template whose placeholder is intact, and one exemption
		// satisfies the document (ADR 0029 decision 4).
		name:    "a declaration glued to the context parameter is graded, not dropped",
		text:    "    <WriteMethodName1>(ctx context.Context<bareParam1> <T1>) <return-1>",
		wantArg: "<bareParam1>",
		wantAny: true,
	}, {
		name:    "the corrected glued form passes on its name",
		text:    "    <WriteMethodName1>(ctx context.Context" + codegen.ParamArg + " <T1>) <return-1>",
		wantArg: codegen.ParamArg,
		wantAny: true,
	}, {
		// The same ruling as the lone `<bareParam>` row above, in the
		// glued position: one token is a declaration naming nothing, and
		// the only token exempted anywhere is one standing for a list.
		name:    "a lone glued token is a declaration naming nothing",
		text:    "    <WriteMethodName1>(ctx context.Context<bareParam1>) <return-1>",
		wantArg: "",
		wantAny: true,
	}} {
		t.Run(tc.name, func(t *testing.T) {
			got, exempt, unclosed := scanSpecSigs("witness.md", tc.text)
			require.Empty(t, unclosed)
			require.Equal(t, tc.wantExempt, len(exempt) > 0, "exempt")
			if !tc.wantAny {
				require.Empty(t, got)
				return
			}
			require.Len(t, got, 1)
			require.Equal(t, tc.wantArg, got[0].arg)
		})
	}
}

// TestSpecBareSigScannerDetectsDrift is the witness for the code-span
// scanner. Its first row is the line that sat green in C1 §5.3 — the
// section gqlc-rz0l corrected — while spelling the capture vector
// gqlc-lhs3 removed, because the parens were off it.
//
// The rows the scanner must leave alone are the load-bearing half. Any
// span it reads has its second field graded as a parameter, so a span
// opening on anything but the context parameter has to fall out first.
func TestSpecBareSigScannerDetectsDrift(t *testing.T) {
	for _, tc := range []struct {
		name    string
		text    string
		wantArg string
		wantAny bool
	}{{
		name:    "the capture vector with the parens left off",
		text:    "- The parameter list is `ctx context.Context, minAge int64`.",
		wantArg: "minAge",
		wantAny: true,
	}, {
		name:    "the corrected form passes on its name",
		text:    "- The parameter list is `ctx context.Context, arg int64`.",
		wantArg: codegen.ParamArg,
		wantAny: true,
	}, {
		name:    "the glued position is read here too",
		text:    "the glued `ctx context.Context<bareParam> <T>` was green.",
		wantArg: "<bareParam>",
		wantAny: true,
	}, {
		name: "the context parameter alone declares no argument",
		text: "a document whose only surviving `ctx context.Context` sits in a comment",
	}, {
		name: "a whole-list placeholder names nothing here either",
		text: "either `ctx context.Context, <param-list>` or `ctx context.Context<param-list>`",
	}, {
		name: "the run seam is past the arity this scanner reads",
		text: "the seam takes `ctx context.Context, cypher string, params map[string]any`",
	}, {
		// A span the paren walk already grades, reached from inside. Read
		// twice it would be graded twice and reported twice, so the anchor
		// is the backtick and this span does not open on one.
		name: "a parenthesised signature inside a span is left to the paren walk",
		text: "`func (q *Queries) PersonById(ctx context.Context, id int64) error`",
	}, {
		name: "a prose span that splits in two is not a parameter list",
		text: "the fields are `MinAge, Locale` and the keys are raw",
	}, {
		// The opening fence of a ```-delimited block is three backticks,
		// and the third would anchor a span running to the closing fence.
		name: "a fenced block's delimiter does not open a span",
		text: "```\nctx context.Context, minAge int64\n```\n",
	}} {
		t.Run(tc.name, func(t *testing.T) {
			got, unclosed := scanBareSigs("witness.md", tc.text)
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
//
// Each row asserts the tail verbatim as well as the name read out of it,
// because the verbatim tail is what specListRules reconciles by identity
// (ADR 0029 decision 5).
func TestSpecParamListRuleScannerDetectsDrift(t *testing.T) {
	bullet := func(body string) string {
		return "- " + paramListTerm + " " + body + "\n- **`<return>`** — the return type.\n"
	}
	for _, tc := range []struct {
		name      string
		text      string
		wantArgs  []string
		wantRules []string
	}{{
		name: "c1 §5.3, before",
		text: bullet("— empty if zero parameters, `, <bareParam> <T>` if one\n" +
			"  parameter, `, arg <MethodName>Params` if two-plus. `<bareParam>` is the\n" +
			"  single parameter's field-name mangle (§4.2), but lowercase-initial."),
		wantArgs:  []string{"<bareParam>", codegen.ParamArg},
		wantRules: []string{", <bareParam> <T>", ", arg <MethodName>Params"},
	}, {
		name: "c1 §5.3, after",
		text: bullet("— empty if zero parameters, `, arg <T>` if one\n" +
			"  parameter, `, arg <MethodName>Params` if two-plus. The argument\n" +
			"  name is the literal `arg` at both arities."),
		wantArgs:  []string{codegen.ParamArg, codegen.ParamArg},
		wantRules: []string{", arg <T>", ", arg <MethodName>Params"},
	}, {
		name: "the bullet ends at the next list item",
		text: bullet("— `, arg <T>` if one parameter.") +
			"- **`<paramsMap>`** — `, minAge int64` is not this bullet's text.\n",
		wantArgs:  []string{codegen.ParamArg},
		wantRules: []string{", arg <T>"},
	}, {
		// A restatement is read as the text it is, and that text is not
		// one of the two rules (ADR 0029 decision 5).
		name:      "a second spelling of the two-plus tail is its own tail, not an arity",
		text:      bullet("— two-plus is `, arg <MethodName>Params`, equivalently `, arg <ParamsType>`."),
		wantArgs:  []string{codegen.ParamArg, codegen.ParamArg},
		wantRules: []string{", arg <MethodName>Params", ", arg <ParamsType>"},
	}, {
		// A concrete type is not the rule the census declares, whatever
		// arity it depicts.
		name:      "an illustration with a concrete type is its own tail too",
		text:      bullet("— `, arg <T>` if one parameter — for `$minAge INT`, `, arg int64`."),
		wantArgs:  []string{codegen.ParamArg, codegen.ParamArg},
		wantRules: []string{", arg <T>", ", arg int64"},
	}, {
		name: "prose code spans in the bullet are not parameter lists",
		text: bullet("— the C1 rule. `paramFieldName(\"minAge\")` → `MinAge` is what it\n" +
			"  is not; `$err`, `$q` and `$_` are why."),
	}, {
		// The leading-comma test is what keeps prose spans out, and only
		// a span that splits into two depth-zero fields reaches it. The
		// row above holds no depth-zero comma and is dropped a step
		// earlier, so this row is the one covering the test itself.
		name:      "a prose span that splits in two is still not a parameter-list tail",
		text:      bullet("— `, arg <T>` if one parameter; the fields are `MinAge, Locale`."),
		wantArgs:  []string{codegen.ParamArg},
		wantRules: []string{", arg <T>"},
	}, {
		name: "a document with no such bullet contributes nothing",
		text: "The `<param-list>` placeholder is described in C1 §5.3.\n",
	}} {
		t.Run(tc.name, func(t *testing.T) {
			var args, rules []string
			for _, sig := range scanParamListRules("witness.md", tc.text) {
				args = append(args, sig.arg)
				rules = append(rules, sig.rule)
			}
			require.Equal(t, tc.wantArgs, args)
			require.Equal(t, tc.wantRules, rules)
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
		name: "a trailing comma closes the last entry",
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

// TestSpecScannersReportUnreadableSites pins every unreadable-site
// return. A span that opens and never closes is a site the sweep could
// not read, and dropping it quietly loses coverage without the censuses
// moving, so each scanner surfaces it and the failure names the text to
// fix.
func TestSpecScannersReportUnreadableSites(t *testing.T) {
	t.Run("an unterminated map literal is reported, not dropped", func(t *testing.T) {
		binds, unclosed := scanSpecBinds("witness.md", "prose\nmap[string]any{\"id\": arg\nmore prose\n")
		require.Empty(t, binds)
		require.Len(t, unclosed, 1)
		require.Equal(t, 2, unclosed[0].line)
	})
	t.Run("an unterminated parameter list is reported, not dropped", func(t *testing.T) {
		sigs, exempt, unclosed := scanSpecSigs("witness.md", "prose\nRemovePerson(ctx context.Context, arg int64\nmore prose\n")
		require.Empty(t, sigs)
		require.Empty(t, exempt)
		require.Len(t, unclosed, 1)
		require.Equal(t, 2, unclosed[0].line)
	})
	t.Run("an unterminated code span is reported, not dropped", func(t *testing.T) {
		sigs, unclosed := scanBareSigs("witness.md", "prose\nthe list is `ctx context.Context, minAge int64\nmore prose\n")
		require.Empty(t, sigs)
		require.Len(t, unclosed, 1)
		require.Equal(t, 2, unclosed[0].line)
	})
}

// docFiles lists every markdown document under docRoots, named relative
// to repoRoot so the censuses and the failure output speak in paths a
// reader can open.
func docFiles(t *testing.T) []string {
	t.Helper()
	var out []string
	for _, root := range docRoots {
		full := filepath.Join(repoRoot, root)
		info, err := os.Stat(full)
		require.NoErrorf(t, err, "docRoots entry %q does not exist", root)
		if !info.IsDir() {
			out = append(out, root)
			continue
		}
		require.NoError(t, filepath.WalkDir(full, func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !entry.IsDir() && strings.HasSuffix(path, ".md") {
				rel, relErr := filepath.Rel(repoRoot, path)
				if relErr != nil {
					return relErr
				}
				out = append(out, rel)
			}
			return nil
		}))
	}
	sort.Strings(out)
	return out
}

// scanSpecSigs extracts every documented single-argument method
// signature from one document's text, every site where a placeholder
// stood in for the whole parameter list, and every parameter list that
// opens but never closes.
//
// The graded shape is the emitted query method's: a parameter list
// opening `(ctx context.Context` and holding exactly one more parameter.
// Lists are read by balancing parentheses, so a signature the prose
// wrapped across lines is one site.
//
// The exempt sites are what callers owe against specListRuleDocs. A
// template writes the whole list as a placeholder, in either of two
// positions — `(ctx context.Context<param-list>)` glued to the context
// parameter, or `(ctx context.Context, <param-list>)` past a comma — and
// either way there is no name in the declaration to read. Both positions
// are read on identical terms (ADR 0029 decision 4).
func scanSpecSigs(file, text string) (sigs, exempt, unclosed []specSig) {
	for _, loc := range ctxAnchorRe.FindAllStringIndex(text, -1) {
		open := loc[0]
		site := specSig{
			file: file,
			line: 1 + strings.Count(text[:open], "\n"),
			text: strings.TrimSpace(collapse(lineAt(text, open))),
		}

		list, ok := span(text, open, '(', ')')
		if !ok {
			unclosed = append(unclosed, site)
			continue
		}
		name, isExempt, gradable := gradeParams(list)
		switch {
		case isExempt:
			exempt = append(exempt, site)
		case gradable:
			site.arg = name
			sigs = append(sigs, site)
		}
	}
	return sigs, exempt, unclosed
}

// gradeParams reads the argument name out of a parameter list's
// contents, reporting separately whether a whole-list placeholder stood
// in for the parameters. It is the one grading step both signature
// anchors share (ADR 0029 decision 10).
//
// The emitted query method takes two parameters at most, at every arity:
// `arg <T>` for one query parameter, `arg <Method>Params` for two or
// more. A longer list — the `driverOrTx.run` seam, ExecuteWrite — is not
// a query method and is not graded.
func gradeParams(list string) (name string, exempt, gradable bool) {
	params := splitTopLevel(list)
	if len(params) > 2 {
		return "", false, false
	}

	// The parameter the placeholder would be in: whatever trails the
	// context parameter when the template glued it on, or the second
	// parameter when a comma separates them.
	tail := params[len(params)-1]
	if len(params) == 1 {
		tail = ctxParamTail(tail)
		if tail == "" {
			return "", false, false // a zero-parameter method has no name to read.
		}
	}
	if listPlaceholderRe.MatchString(tail) {
		return "", true, false
	}
	name, gradable = paramName(tail)
	return name, false, gradable
}

// scanBareSigs extracts every parameter list a document prints inside an
// inline code span with its enclosing parentheses left off, and every
// such span that never closes.
//
// The span's backtick is the delimiter at both ends: it opens the anchor
// and it terminates the list (ADR 0029 decision 10). The opening backtick
// must not itself follow a backtick, which keeps a fenced block's ```
// delimiter from anchoring a span that would run to the closing fence.
//
// A parameter list stated in prose with no code span around it is not
// read here (gqlc-e143).
func scanBareSigs(file, text string) (sigs, unclosed []specSig) {
	for _, loc := range tickAnchorRe.FindAllStringIndex(text, -1) {
		open := loc[0]
		if open > 0 && text[open-1] == '`' {
			continue
		}
		site := specSig{
			file: file,
			line: 1 + strings.Count(text[:open], "\n"),
			text: strings.TrimSpace(collapse(lineAt(text, open))),
		}

		closing := strings.IndexByte(text[open+1:], '`')
		if closing < 0 {
			unclosed = append(unclosed, site)
			continue
		}
		list := collapse(text[open+1 : open+1+closing])
		name, _, gradable := gradeParams(list)
		if !gradable {
			continue
		}
		site.arg = name
		site.list = list
		sigs = append(sigs, site)
	}
	return sigs, unclosed
}

// ctxParamTail splits the context parameter off the head of a parameter
// declaration, returning whatever follows it — empty when nothing does.
//
// A non-match returns empty as well: every caller reaches this past an
// anchor compiled from the same ctxParam literal, and either answer means
// "no name here".
func ctxParamTail(param string) string {
	loc := ctxParamRe.FindStringIndex(param)
	if loc == nil {
		return ""
	}
	return strings.TrimSpace(param[loc[1]:])
}

// scanParamListRules extracts every parameter-list tail the
// `<param-list>` definition bullets spell out, verbatim, along with the
// argument name each one binds.
//
// The method-shape templates in C1 §5.3 and C4 §5.3 print the
// placeholder, so the paren walk above reads
// `(ctx context.Context<param-list>)` as an exemption with no name in it.
// The name lives in the bullet that expands the placeholder, as inline
// code spelling the list's tail from its leading comma: `, arg <T>` and
// `, arg <MethodName>Params`. Those are ordinary parameter-list tails,
// so they split and grade on the same axis a whole signature does.
//
// The tail is kept verbatim because specListRules grades the two rules by
// identity (ADR 0029 decision 5).
//
// Only code spans inside such a bullet are read, and only those opening
// with a comma: prose commas in backticks are everywhere in these
// documents.
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
				rule: code.text,
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
// A literal whose brace never closes is returned separately, so that the
// sweep reports a site it could not read.
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

// readDoc reads one swept document, named relative to repoRoot.
func readDoc(t *testing.T, file string) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(repoRoot, file))
	require.NoError(t, err)
	return string(body)
}

// splitTopLevel splits a parameter list or composite literal at its
// depth-zero commas. A trailing comma closes the last entry: gofmt writes
// one on every list it wraps across lines, so the empty tail past it is
// dropped.
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
// Only the name position is read. Two tokens are a name and a type, and
// the name is graded whatever the type says: `arg <T>` and
// `arg <MethodName>Params` pass on their name, `<bareParam> <T>` — the
// capture vector gqlc-lhs3 removed from the emitter — fails on its. A
// placeholder in the type position is not an exemption for the name
// beside it. The `<param-list>` bullets are the one place the fence reads
// a type, in scanParamListRules (ADR 0029 decision 5).
//
// One token grades as drift, empty-named: a documented signature that
// names no argument is not the one the emitter writes (ADR 0029 decision
// 6). That covers a lone `<bareParam>` on the same terms as a lone
// `int64`. The whole-list placeholder is the one token the caller decides
// before reaching here, as an exemption owed against specListRuleDocs.
func paramName(param string) (name string, gradable bool) {
	fields := strings.Fields(param)
	switch {
	case len(fields) == 0:
		return "", false
	case len(fields) == 1:
		return "", true
	default:
		return fields[0], true
	}
}

// listPlaceholderRe matches the placeholders that stand for a whole
// parameter list: `<param-list>` and the numbered `<param-list-1>` form
// the interface blocks print when they show two members at once. Built
// from paramListPlaceholder so the exemption and the term the bullet
// scanner reads cannot drift apart.
//
// The enumeration is exhaustive by construction: a spelling not listed
// here is graded as a declaration, and `<bareParam>` — an angle-bracketed
// placeholder standing for the query author's parameter name — must stay
// outside it (ADR 0029 decision 4).
//
// Every spelling it matches is exempted from grading, and every document
// taking that exemption is reconciled against specListRuleDocs, so the
// numbered form owes the bullet on the same terms as the bare one.
var listPlaceholderRe = regexp.MustCompile(
	`^` + regexp.QuoteMeta(strings.TrimSuffix(paramListPlaceholder, ">")) + `(-\d+)?>$`)

// anchorPattern compiles one anchor literal into a matcher that reads
// whitespace the way Go's tokeniser does — required between two
// identifier characters, optional at every other boundary. gofmt wraps a
// long parameter list after the open paren, which puts `ctx` on its own
// line; prose reflows the same way, and a second space is invisible in
// rendered markdown.
//
// The documents themselves are never renormalised, so byte offsets stay
// usable and a failure can name a line (ADR 0029 decision 8).
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
