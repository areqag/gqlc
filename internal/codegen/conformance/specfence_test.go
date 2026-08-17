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

// docRoots are the trees the fence sweeps, relative to repoRoot.
// Everything under docs/ rather than only docs/specs/: the drift reached
// C1, C3, C4 and C5, and an ADR or a design note prints the same
// signatures.
//
// A root is held here by what the censuses below name beneath it, and by
// nothing else. Deleting `docs` outright is red: every document they
// name is under it, so all of them are lost at once and each is reported
// by name. Nothing else about this list is held. Deleting `README.md` or
// `CONTEXT.md` is green — no census names a document under either.
// Narrowing `docs` to `docs/specs` is green too, for the same reason
// from the other side: every censused document is under `docs/specs`,
// so the ADRs and design notes leave the sweep and no census misses
// them. A root that stops existing on disk is the one case that is
// caught whichever entry it is, and it fails in docFiles by name.
//
// That shrinkage is not closed here, and not by anything this file could
// reconcile. Everything it observes is walked out of docRoots, so a
// census over what a sweep produced cannot notice docRoots losing or
// narrowing an entry: the observation is derived from the list being
// audited, which is the failure the censuses below exist to stop
// repeating. Catching it needs a set of candidate roots from outside the
// list — the repository's own markdown — and that is the same check that
// would catch the list failing to grow, which is gqlc-jfwo. `AGENTS.md`,
// `CLAUDE.md` and `CONTRIBUTING.md` sit beside the two files named here,
// are not swept, and print no query method and no driver-binding literal
// today; a new document at the repository root would join them
// unnoticed.
var docRoots = []string{"docs", "README.md", "CONTEXT.md"}

// repoRoot is where this package sits relative to the tree above. Swept
// documents are named relative to it — a failure that says
// `docs/specs/codegen-stage-c1.md` names a path the reader can open.
const repoRoot = "../../../"

// --- the censuses -----------------------------------------------------------
//
// Which documents each sweep is answerable to is written down here, by
// name, and reconciled against what the sweep actually graded — in both
// directions, failing with the document rather than with a count.
//
// This was a set of marker regexps reading the requirement off the
// documents themselves, and that could not hold: the markers were the
// same shapes as the grading, so one edit to a graded site removed the
// document from the requirement and from the grading at the same
// instant. Moving the comma out of a template's placeholder
// (`(ctx context.Context, <param-list>)`) unselected C1 from the marker
// and unread it from the scanner together, and the capture vector went
// back into §5.3 green. Defeating the signal defeated both sides of it,
// which is what a derivation cannot avoid: it is the scanner auditing
// itself. A tighter regexp only moves the hole to the next spelling.
//
// So the expectation is written rather than derived, and deliberately in
// this file rather than beside the text it names. It costs one line when
// a document starts or stops printing the surface, and the failure
// prints the line to add or remove. Removing an entry *and* gutting the
// document it names is still possible — but it is now a visible two-part
// edit that deletes a named document from a list in a test file, which
// is the honest floor for any check that reads only these documents.
//
// Deliberately not a count of sites. A site count sits beside the thing
// it counts, carries silent slack for every site it is under, and fails
// as `13 != 14`, which names nothing to go and look at. A census fails
// as a set difference with a name in it. What a census is, though, is a
// count of one per document, and that count has slack of its own — the
// third omission below is exactly it, stated rather than argued away.
//
// The three sets come out different, and the difference is a fact about
// the documents: C3's width and nullability bullets print the binding
// literal without printing the method around it, so C3 owes a binding
// and owes no signature.
//
// Three things these censuses do not reach.
//
// Deleting the documented surface outright is the first. That is a
// two-part edit here — the document and its line below — and the second
// part is the record, which is the most any check reading only the
// documents it is pointed at can offer.
//
// A document these censuses are never pointed at is the second, and it
// leaves no record at all: the sets below name documents, not roots, so
// they are silent about anything docRoots does not reach (gqlc-jfwo).
//
// The third is the largest, and unlike the first it is a one-part edit.
// A listed document keeps its entry on one surviving graded site, so
// every site past the first can leave the sweep with nothing said. Not
// by being corrected — by ceasing to print `ctx context.Context`, which
// is what the scanners anchor on, so a site that drifts in the anchor
// itself drifts off an axis this file does not grade and takes its
// argument name out of reach on the way. C4 §3.2's WriteQuerier member
// `RemovePerson(ctx context.Context, arg int64)` — the declared
// interface surface gqlc-rz0l singled out as drift in a surface rather
// than in prose — is one of ten graded signatures in that document, and
// rewriting its context parameter leaves every sweep in this file green.
// So the honest floor is one graded site per listed document, not one
// document per list.
//
// Left open on purpose, and the alternatives are the reason. A per-site
// census keyed on the method name does not close it: C4 grades
// `RemovePerson` at three separate sites, so its name survives any one
// of them leaving. A per-site count does close it and is the shape
// rejected above, failing as `9 != 10`. A per-site census of the graded
// text closes it too, at the price of making this file a copy of the
// documents — red on every honest edit to an example, and a fence that
// reddens on honest edits is one whose census gets bulk-updated without
// being read, which buys less than a floor of one that is written down.
// This is where it is written down.
const (
	specC1 = "docs/specs/codegen-stage-c1.md"
	specC3 = "docs/specs/codegen-stage-c3.md"
	specC4 = "docs/specs/codegen-stage-c4.md"
	specC5 = "docs/specs/codegen-stage-c5.md"
)

// specSigDocs are the documents that print an emitted query method
// signature whole, and so must contribute at least one graded argument
// name.
var specSigDocs = []string{specC1, specC4, specC5}

// specBindDocs are the documents that print a `map[string]any` driver
// binding, and so must contribute at least one graded binding value.
var specBindDocs = []string{specC1, specC3, specC4, specC5}

// specListRuleDocs are the documents whose method-shape template prints
// a placeholder standing for the whole parameter list instead of a list.
// Such a document names no argument in the template, so the bullet that
// expands the placeholder is the only place it states which identifier
// the emitted signature binds — and that bullet is the exact text
// gqlc-rz0l corrected.
//
// This list does double duty, which is what closes the gap between what
// the fence declines to grade and what it requires instead. A document
// here must both state the expansions (specListRules) and be one whose
// signatures the fence let past ungraded on account of the placeholder;
// a document not here may not have a whole-list placeholder waved
// through. Exemption and requirement are the same list, so neither can
// be had without the other.
var specListRuleDocs = []string{specC1, specC4}

// specListRules are the parameter-list tails every `<param-list>` bullet
// must spell out, graded by identity rather than by shape.
//
// Both are here because both are live: the emitted signature binds
// codegen.ParamArg at one query parameter and at two-plus, and a bullet
// that spells one and leaves the other to prose puts that other one back
// outside the fence — which is how §5.3 came to specify the capture
// vector for the single-parameter form while the two-plus form sat
// correct beside it.
//
// Identity, not shape. Reading "which arity is this?" off the type
// position — a placeholder standing alone against a placeholder with a
// generated suffix on it — was an inference, and an inference is
// satisfiable by a spelling that means something else: `, arg <ParamsType>`
// is the *two-plus* rule and reads as the single-parameter one, so
// stating the two-plus rule twice in two spellings satisfied both arities
// while the single-parameter rule sat in prose with the capture vector in
// it. An illustration (`, arg int64`) did the same. Neither is on this
// list, so neither is a rule any more: it is an extra span, and an extra
// span is as red as a missing one.
//
// The name in each is codegen.ParamArg rather than a literal `arg`, so
// renaming the emitter's constant reddens these documents instead of
// leaving them agreeing with a name the emitter no longer writes.
var specListRules = []string{
	", " + codegen.ParamArg + " <T>",
	", " + codegen.ParamArg + " <MethodName>Params",
}

// ctxParam is the first parameter of every emitted query method.
// ctxAnchor is that parameter with its opening paren: anchoring on it
// rather than on `func (q *Queries)` reaches the interface members too —
// C4 §3.2's WriteQuerier block is a declared surface, and its drift was
// the same drift.
const (
	ctxParam  = "ctx context.Context"
	ctxAnchor = "(" + ctxParam
)

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

	// ctxParamRe reads the context parameter off the head of a
	// declaration so whatever follows it inside the same parameter can be
	// examined — which is where `(ctx context.Context<param-list>)` puts
	// its placeholder, with no comma to split on.
	ctxParamRe = regexp.MustCompile(`^(?:` + anchorPattern(ctxParam).String() + `)`)
)

// paramListPlaceholder is the bare placeholder inside paramListTerm's
// markdown emphasis. Derived from the term rather than written beside
// it, so the template's placeholder and the bullet the scanner reads
// cannot drift apart.
var paramListPlaceholder = strings.Trim(paramListTerm, "*`")

// specSig is one graded site: a documented method signature taking one
// argument after the context, the parameter-list tail a `<param-list>`
// bullet expands that placeholder to, or one documented driver-binding
// entry. All three reduce to the same question — which identifier the
// document says the emitted Go reads — so `arg` carries the argument's
// name for the first two and the binding expression's root for the
// third, and `text` carries the source line so a failure names the site
// rather than describes it.
//
// `rule` is the verbatim parameter-list tail a `<param-list>` bullet
// spelled, empty at every other site. It is what the rule census
// reconciles: the bullet's claim is graded as the text it wrote, not as
// a property inferred from it.
type specSig struct {
	file string
	line int
	arg  string
	rule string
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
// and the second is the site gqlc-rz0l corrected, so its rules are also
// reconciled by identity against specListRules.
func TestSpecMethodArgIsGeneratorOwned(t *testing.T) {
	files := docFiles(t)
	require.NotEmpty(t, files, "the fence swept no documents; docRoots is stale")

	sweep := sweepSigs(files, func(file string) string { return readDoc(t, file) })

	var bad []specSig
	for _, sig := range sweep.graded {
		if sig.arg != codegen.ParamArg {
			bad = append(bad, sig)
		}
	}

	requireClean(t, sweep.unclosed, "documented parameter list does not close",
		"these documents open a parameter list on `ctx context.Context` and never close the parenthesis, so the\n"+
			"fence cannot read the argument out of them and silently graded nothing there; fix the text rather than\n"+
			"the fence — an unreadable site is an ungraded site")

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
// codegen.ParamArg. A spec that reverts the value to the author's name
// documents the capture vector again just as loudly as the signature
// does — and correcting the value by also rewriting the key would break
// the binding outright, so the fence grades the value alone.
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

	// One census, no union. The requirement used to be the map anchor
	// unioned with the query-method anchor, so that gutting the literal
	// out of a document still declaring the method could not take its
	// requirement with it — and that union over-reached in the other
	// direction, making any new document that quoted a signature owe a
	// map literal it had no reason to carry. A written list needs neither
	// half: gutting C1's literals fails the first direction below by
	// name, and a document quoting a signature owes a binding only if it
	// is listed here.
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
// Both are the accumulation the two sweeps above used to do inline, and
// they are functions for the reason reconcile is: the line carrying a
// scanner's unreadable-site return into the accumulator is load-bearing
// and was structurally unable to fail. Dropping it (`sigs, exempt, _ :=`)
// is invisible on a clean tree — there is nothing unreadable to lose —
// and it is invisible on a drifted one too, because the drift it drops
// is the only thing that would have reported it. Inline, the only
// witness available was a swept document with an unterminated span in
// it, which is not a state the repository is ever in. As a function it
// takes a document set and a reader, so a witness supplies both.
type sigSweep struct {
	graded      []specSig
	unclosed    []specSig
	sigDocs     map[string]bool
	exemptDocs  map[string]bool
	ruleDocs    map[string]bool
	statedRules map[string]map[string]bool
}

type bindSweep struct {
	graded   []specSig
	unclosed []specSig
	bindDocs map[string]bool
}

// sweepSigs runs both signature scanners over every named document,
// reading each one with read.
func sweepSigs(files []string, read func(string) string) sigSweep {
	out := sigSweep{
		sigDocs:     map[string]bool{},
		exemptDocs:  map[string]bool{},
		ruleDocs:    map[string]bool{},
		statedRules: map[string]map[string]bool{},
	}
	for _, file := range files {
		text := read(file)

		sigs, exempt, broken := scanSpecSigs(file, text)
		out.unclosed = append(out.unclosed, broken...)
		if len(exempt) > 0 {
			out.exemptDocs[file] = true
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

// TestSpecSweepsCarryUnreadableSites is the witness for that carrying.
// The scanners report an unterminated span rather than dropping it
// (TestSpecScannersReportUnreadableSites) and requireClean fails on a
// non-empty set naming the site (TestSpecFailuresAreWired); this is the
// join between those two, and it was the one part of the path with no
// witness on it.
//
// The document set is synthetic because the repository's is not
// allowed to contain the input this needs. The unreadable document is
// swept first and the readable one after it, so that the rest of the
// corpus still being graded is asserted rather than assumed: a sweep
// that gave up at the first unreadable site would carry it correctly
// and lose every document behind it, which is the failure mode one
// step over from the one being fixed.
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
		sweep := sweepSigs(files, read)
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

// requireCensus reconciles a written census against what a sweep
// actually observed, in both directions, failing with the name rather
// than the count.
//
// Declared and unobserved is the case this file exists for one level up:
// a document that still prints the surface and no longer contributes to
// the sweep, which every derived requirement so far has agreed with
// rather than objected to, because the edit that hid the site from the
// scanner hid it from the marker in the same stroke.
//
// Observed and undeclared is the census auditing its own scope. A
// document that starts printing the surface is red until it is written
// down, and the failure says which line to add. That is the price of not
// deriving the requirement, and it is one line.
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

// fenceT is the part of *testing.T the two helpers above use, named so a
// witness can stand in for it.
//
// reconcile is a function precisely so its judgement can be witnessed on
// a clean tree, and every other line between a scanner's reading and an
// actual failure needed the same treatment for the same reason. Six of
// them did not have it, and each was structurally unable to fail:
// requireClean's empty-set guard, requireCensus's lost, undeclared and
// duplicated arms, and the two lines carrying a scanner's unreadable-site
// return into the sweep that fails on it. Neuter any one, then revert a
// signature, add a signature to an undeclared document, grow a declared
// document's signatures past what the scanner reads, name a document
// twice, or leave a parameter list or a map literal unterminated, and
// every sweep in this file stayed green. All six are witnessed now —
// four by TestSpecFailuresAreWired and TestSpecCensusReconcilesBothDirections,
// two by TestSpecSweepsCarryUnreadableSites.
//
// What is left is not wiring. The sweeps' own comparison bodies
// (`sig.arg != codegen.ParamArg` and the binding's prefix test) and
// their calls to these helpers are the assertions, and deleting an
// assertion passes any test in any suite; there is no arrangement of
// this file that changes that. The line drawn here is between an
// assertion, whose deletion is a deletion a reader sees, and plumbing,
// whose neutering reads as bookkeeping and takes a failure with it.
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

// reconcile is requireCensus's whole judgement, as a function, so the
// judgement itself can be witnessed rather than only exercised through a
// green sweep.
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
// reader fixing the drift sees all of it at once rather than the first.
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
// judgement every census in this file rests on. A reconciliation that
// only reported one direction, or that compared sizes, would be green
// over exactly the edits the censuses exist to catch, and the sweeps
// above cannot show that while the tree is clean.
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
		// The shape a count is green over: one entry lost and one gained
		// leaves the two sets the same size and neither the same set.
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
		name:    "an empty sweep loses every declared entry rather than passing over nothing",
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
//
// `wantExempt` is the third outcome, and it is not the same as grading
// nothing. A whole-list placeholder standing in for the parameters is a
// site the fence declines to read a name out of, and specListRuleDocs is
// what it is declined against; a zero-parameter method is a site with no
// name to read. Collapsing the two is how the exemption came to be
// wider than anything requiring it.
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
		// The same ruling as the row above, and it has to be: the skip
		// for a whole-list placeholder is a test for `<param-list>`, not
		// for angle brackets, or the author's own parameter name gets
		// waved through by wearing the exemption's clothes. A lone
		// `<bareParam>` is a name standing where a declaration should be,
		// which is drift on exactly the axis this file grades.
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
		name:       "a template placeholder parameter list is exempted, and says so",
		text:       "func (q *Queries) <MethodName>(ctx context.Context<param-list>) (<return>, error) {",
		wantExempt: true,
	}, {
		// The reformat that used to unselect a document from the
		// requirement and from the grading in one edit. It still grades
		// no name — a placeholder standing for a list carries none — but
		// it is now reported as an exemption, and the exemption is
		// reconciled against the document list that requires the bullet.
		name:       "a whole-list placeholder moved past the comma is the same exemption",
		text:       "func (q *Queries) <MethodName>(ctx context.Context, <param-list>) (<return>, error) {",
		wantExempt: true,
	}, {
		// The spelling the interface blocks use when they show two
		// members at once, in both positions. It stands for a whole list
		// on the same terms, so it is exempted on the same terms — and
		// owed against the same list, which is what it was not before.
		name:       "the numbered whole-list placeholder is the same exemption",
		text:       "    <MethodName1>(ctx context.Context, <param-list-1>) (<return-1>, error)",
		wantExempt: true,
	}, {
		name:       "the numbered placeholder is the same exemption without the comma too",
		text:       "    <MethodName1>(ctx context.Context<param-list-1>) (<return-1>, error)",
		wantExempt: true,
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
	}, {
		// The glued position is where C4's interface template writes its
		// placeholder (`(ctx context.Context<param-list-1>)`), and a
		// declaration written there rather than past a comma used to be
		// dropped: neither graded, nor exempted, nor reported. The
		// exemption census could not notice, because C4 keeps a second
		// template whose placeholder is intact and one exemption satisfies
		// the document. So the drift the comma'd form is red for was green
		// one character away.
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

// TestSpecParamListRuleScannerDetectsDrift is the witness for the
// bullet scanner. The `<param-list>` bullet is where both specs state
// which identifier the emitted signature binds — the template above it
// prints only the placeholder — so it is the site gqlc-rz0l corrected
// and the site a revert lands on. Each row is a bullet body: the text
// before that commit, the text after, and the forms the scanner must
// leave alone.
//
// Each row asserts the tail verbatim as well as the name read out of it,
// because the verbatim tail is what specListRules reconciles. A row's
// second column is therefore both what the fence grades and what it
// declares: `, arg <T>` is the single-parameter rule because that is the
// text, not because of anything inferred from the type beside the name.
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
		// The restatement that used to satisfy the two-plus arity twice
		// over while the single-parameter rule sat in prose: it is read
		// as the text it is, and that text is not one of the two rules.
		name:      "a second spelling of the two-plus tail is its own tail, not an arity",
		text:      bullet("— two-plus is `, arg <MethodName>Params`, equivalently `, arg <ParamsType>`."),
		wantArgs:  []string{codegen.ParamArg, codegen.ParamArg},
		wantRules: []string{", arg <MethodName>Params", ", arg <ParamsType>"},
	}, {
		// The illustration that used to do the same. A concrete type is
		// not the rule the census declares, whatever arity it depicts.
		name:      "an illustration with a concrete type is its own tail too",
		text:      bullet("— `, arg <T>` if one parameter — for `$minAge INT`, `, arg int64`."),
		wantArgs:  []string{codegen.ParamArg, codegen.ParamArg},
		wantRules: []string{", arg <T>", ", arg int64"},
	}, {
		name: "prose code spans in the bullet are not parameter lists",
		text: bullet("— the C1 rule. `paramFieldName(\"minAge\")` → `MinAge` is what it\n" +
			"  is not; `$err`, `$q` and `$_` are why."),
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
// a sweep that drops it quietly loses coverage without the censuses
// moving. Both are surfaced so the failure names the text to fix rather
// than the fence.
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
// The shape it grades is the emitted query method's: a parameter list
// opening `(ctx context.Context` and holding exactly one more parameter,
// which is what every arity of the emitted surface produces (`arg <T>`
// for one query parameter, `arg <Method>Params` for two or more). The
// zero-parameter form has nothing after the context and nothing to
// grade; the `driverOrTx.run` seam and `ExecuteWrite` take three or more
// and are not query methods. Parameter lists are matched by balancing
// parentheses rather than by a line regexp, so a signature the prose
// wrapped across lines is graded rather than skipped.
//
// The exempt return is the third outcome, and it is separate from
// grading nothing because it is the one the fence owes something for. A
// template writes the whole list as a placeholder, in either of two
// positions — `(ctx context.Context<param-list>)` glued to the context
// parameter, or `(ctx context.Context, <param-list>)` past a comma —
// and either way there is no name in the declaration to read. Reporting
// those sites is what lets specListRuleDocs require the bullet from
// exactly the documents that took the exemption, rather than from the
// documents some regexp happened to still be selecting.
//
// The two positions are read on identical terms, and that is the point.
// The glued one used to have a drop beside its exemption: a declaration
// written there rather than past a comma was skipped as "something else
// glued to the context parameter", so `(ctx context.Context, <bareParam>
// <T>)` was red and `(ctx context.Context<bareParam> <T>)` was green.
// The exemption census could not see the difference, because C4 writes
// the placeholder in both positions and one intact template satisfies
// the document. Whichever position it is written in, the tail is either
// a whole-list placeholder — exempted, and owed against
// specListRuleDocs — or a declaration, and graded.
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
		params := splitTopLevel(list)
		if len(params) > 2 {
			continue
		}

		// The parameter the placeholder would be in: whatever trails the
		// context parameter when the template glued it on, or the second
		// parameter when a comma separates them.
		tail := params[len(params)-1]
		if len(params) == 1 {
			tail = ctxParamTail(tail)
			if tail == "" {
				continue // a zero-parameter method has no name to read.
			}
		}
		if listPlaceholderRe.MatchString(tail) {
			exempt = append(exempt, site)
			continue
		}

		name, gradable := paramName(tail)
		if !gradable {
			continue
		}
		site.arg = name
		sigs = append(sigs, site)
	}
	return sigs, exempt, unclosed
}

// ctxParamTail splits the context parameter off the head of a parameter
// declaration, returning whatever follows it — empty when nothing does.
//
// The head is always there when the caller reaches this: ctxAnchor is
// ctxParam with the open paren prepended, both patterns are compiled
// from that one literal by anchorPattern, and span collapses the
// contents it returns the same way either pattern reads them. A
// non-match and an empty tail would both mean "no name here" in any
// case, so the two are not told apart.
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
// placeholder, not the list, so the paren walk above reads
// `(ctx context.Context<param-list>)` as an exemption with no name in
// it. The name lives in the bullet that expands the placeholder, as
// inline code spelling the list's tail from its leading comma:
// `, arg <T>` and `, arg <MethodName>Params`. Those are ordinary
// parameter-list tails, so they split and grade on the same axis as a
// whole signature does, and reverting the bullet alone — which is
// exactly the drift gqlc-rz0l corrected — is red.
//
// The tail is kept verbatim as well as graded, because which rules a
// bullet states is a question about its text and not about its shape.
// Inferring the arity from the type position instead let a second
// spelling of the two-plus tail stand in for the single-parameter one.
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
// position is not an exemption for the name beside it, and nothing is
// read off the type at all: reading the arity off it was an inference a
// second spelling of one arity could satisfy in place of the other.
//
// One token is a type with no name, which is drift in its own right: a
// documented signature that names no argument is not the one the emitter
// writes. Go's grammar would read that lone token as a type and call the
// parameter legitimately unnamed, and this fence deliberately does not,
// because the emitter names the argument at every arity. That ruling is
// applied whole: a lone `<bareParam>` is not a type either — it is the
// author's name standing where a whole declaration should be — so it is
// graded on the same arm as a lone `int64` rather than waved through for
// wearing angle brackets. The one token that is not graded here is the
// one that stands for a whole list, and that is decided by the caller
// (scanSpecSigs), which reports it as an exemption rather than dropping
// it — because an exemption is owed against specListRuleDocs and a drop
// is owed against nothing.
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
// Deliberately an enumeration rather than a test for `<…>`. A generic
// placeholder test cannot tell `<param-list>`, which stands for a list,
// from `<bareParam>`, which stands for the query author's parameter
// name — and exempting the second is the drift this whole file exists to
// catch, wearing the first one's clothes.
//
// Every spelling it matches is exempted from grading, and every document
// that takes that exemption is reconciled against specListRuleDocs, so
// the numbered form owes the bullet on the same terms as the bare one.
// It used to owe nothing: the exemption admitted the numbered spelling
// and the requirement did not, so a template rewritten to `<param-list-1>`
// dropped its bullet and stayed green.
var listPlaceholderRe = regexp.MustCompile(
	`^` + regexp.QuoteMeta(strings.TrimSuffix(paramListPlaceholder, ">")) + `(-\d+)?>$`)

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
