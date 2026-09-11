package conformance_test

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/areqag/gqlc/internal/codegen"
)

// This file is the SPEC fence, and it is not what the required CI
// context named `codegen-fence` runs. That context runs `just
// test-codegen-fence`, the nested-module COMPILE fence over
// test/data/codegen (docs/specs/codegen-stage-c1.md §8). This file is
// an ordinary Go test and is PR-blocking through `test`. Two gates, two
// contexts, and the name matches the other one (bd gqlc-gmz6).
//
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
// has been measured. Prose beside an intact graded span can say the
// opposite of it (gqlc-e143); a binding stated with no map literal
// around it is unread (gqlc-offa); a parenthesis-less parameter list on
// its own line inside a code block, fenced or indented, is unread too —
// as is one inside a code span whose backtick run the block rule takes
// for a fence, since that rule tests bytes rather than parsing markdown
// — while the paren anchor reaches a block like any other text
// (gqlc-cgat); and a listed document keeps its census entry on one
// surviving site (gqlc-0rjn).
//
// A list replacing one of the exhibits specBareListExhibits names,
// spelled the same way, no longer takes that entry's exemption: an
// exemption is claimed by exhibitMarker on the site's line rather than
// by the site coming first (gqlc-x2sg). Neither does a claim replacing
// one of the non-method quotes specNonMethodSites exempts: that census
// is keyed by the NAME the document prints before the parameter list as
// well as by the list, so a replacement has to keep both (gqlc-yn2l).
//
// A site hidden inside an HTML comment no longer pays a census either.
// Four of the five anchors are refused there outright rather than read
// there, which is an absence check and not a scanner (gqlc-jnsk, ADR
// 0029 decision 15). The fifth, scanBareBinds, has no anchor to refuse,
// so a commented brace-less binding span is still read and still counts.
//
// A signature carrying the author's parameter names as separate
// arguments is no longer past the arity read here: the emitted list is a
// closed shape at every arity, so anything longer is graded as drift,
// and the functions in the corpus that open the same anchor without
// being query methods are exempted per document by an exact census
// (gqlc-vu7z).
//
// That this file scans bytes rather than parsing markdown is a decision
// and not an oversight: ADR 0042 rules that no CommonMark parser enters
// this package, prices the two options against the corpus, and
// dispositions the three beads that were waiting on the answer
// (gqlc-r8na3). The limits above are what that ruling accepts. Each was
// measured unoccupied on the day it was taken and nothing here re-reads
// those counts, so a limit that becomes occupied does so silently —
// which is why the ruling prefers an absence check to a parser wherever
// one of them is worth closing at all.

// docRoots are the trees the fence sweeps, relative to repoRoot. The
// drift reached C1, C3, C4 and C5, and an ADR or a design note prints
// the same signatures, so the whole of docs/ is in scope.
//
// A root is held here only by what the censuses below name beneath it.
// Deleting `docs` is red — every censused document is under it, and each
// is reported by name. So is narrowing it to `docs/specs`, because
// specBareListExhibits names a document under `docs/adr/`. A root that
// stops existing on disk fails in docFiles.
//
// Deleting `README.md` or `CONTEXT.md` is red as well, but from nothing
// below: no census names a document under either, so the sweep would
// produce exactly what it produced before. It is caught one level out, by
// TestEveryRootDocIsSweptOrDeclaredOutOfScope, whose candidate set is the
// repository root's own markdown rather than whatever this list reached.
// That direction is the whole point — a census over what a sweep produced
// is derived from the list being audited, so it cannot see the list
// shrink: a root the walk stops reaching contributes no document there is
// then anything to miss (gqlc-jfwo).
//
// A top-level DIRECTORY of markdown nobody added here is caught the same
// way, by TestEveryDocTreeIsSweptOrDeclaredOutOfScope: `docs` is the only
// tree this list sweeps, and dropping it leaves that tree's documents
// tracked and declared by nothing. The trees deliberately left unswept are
// named in nonSpecDocTrees, with the reason for each; all 56 documents
// between them print no query method signature and no driver-binding
// literal, measured 2026-09-05.
var docRoots = []string{"docs", "README.md", "CONTEXT.md"}

// nonSpecRootDocs are the repository-root markdown documents deliberately
// left unswept, each mapped to why. A root document is red until it is
// named either here or in docRoots, so leaving one out stops being
// something that happens and becomes something somebody writes down.
//
// The reason is required rather than conventional: a census of bare names
// records that a decision was taken and none of what it was taken on, and
// an exemption whose argument is not written cannot be checked when the
// document changes under it. Nothing here re-reads these documents, so a
// reason that goes false goes false silently — which is the price of the
// exemption and the argument for keeping this map short.
var nonSpecRootDocs = map[string]string{
	"AGENTS.md":       "instructions to coding agents about this repository; states nothing about the emitted surface",
	"CLAUDE.md":       "the same, addressed to Claude Code",
	"CONTRIBUTING.md": "contribution process; prints no generated-code example",
}

// nonSpecDocTrees are the top-level directories holding markdown that the
// fence deliberately does not sweep, each mapped to why. nonSpecRootDocs
// answers for the repository root's own files; this answers for the trees
// standing beside them, so a `spec/` or `guides/` tree added tomorrow is
// red until somebody decides which of the two states it is in.
//
// Only TRACKED markdown reaches the census below, and that is what makes
// this list checkable in every clone rather than in some of them. Agents
// working here leave untracked scratch trees at the repository root, so a
// candidate set read off the filesystem would redden in the worktree that
// happens to hold one and nowhere else — a failure only its owner can see,
// and the reason this census was not written alongside its sibling
// (gqlc-tn88a, gqlc-jfwo).
//
// The reason is required on the same terms as nonSpecRootDocs, and expires
// the same way: nothing here re-reads these trees, so a reason that goes
// false goes false silently.
var nonSpecDocTrees = map[string]string{
	".beads":   "the beads tracker's own README, written by `bd init`; it describes the issue tracker and states nothing about this repository's emitted surface",
	"internal": "three SOURCE.md provenance notes for the vendored GQL grammar and the two ISO BNF extracts; they record where those artefacts came from, not what codegen emits from them",
	"test":     "one README stating what the resolver corpus's valid/ claim covers; it is explicit that codegen is outside that claim, and prints no emitted signature or binding",
}

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
// documents, not roots, so they stay silent about anything docRoots does
// not reach. What answers for the repository root's own markdown is
// TestEveryRootDocIsSweptOrDeclaredOutOfScope, one level out from these,
// and what answers for an undeclared top-level directory of it is
// TestEveryDocTreeIsSweptOrDeclaredOutOfScope beside it (gqlc-jfwo,
// gqlc-tn88a).
//
// A site swapped for another inside one document. specSigDocs and
// specBindDocs carry a per-document FLOOR — how many graded sites the
// document owes, not merely that it owes one — so a site that stops
// printing its anchor takes its document under its floor and is named
// there. What a floor does not distinguish is a document that loses one
// site and gains another: a count is a size, not a membership. The
// membership reading is a per-site verbatim census, priced and refused
// on gqlc-0rjn because it makes this file a copy of the documents, red
// on every honest edit to an example — which is how a census gets
// bulk-updated without being read.
//
// Until those floors, one graded site per listed document was the whole
// requirement, and every site past the first could leave the sweep with
// nothing said — not by being corrected, but by ceasing to print an
// anchor, each of which is a delimiter as well as the text after it:
// `(ctx context.Context` or a code span's opening backticks before it
// for a signature, `map[string]any` with its opening brace for a
// binding. C4 §3.2's WriteQuerier member
// `RemovePerson(ctx context.Context, arg int64)` was one of ten graded
// signatures in that document, and rewriting its context parameter left
// every sweep here green (gqlc-0rjn, ADR 0029 decision 3;
// docs/specs/codegen-stage-c1.md §5.3 states it to the reader). Under
// the floors that edit takes C4's signature count below what specSigDocs
// declares, and is reported by document name with both numbers.
const (
	specC0        = "docs/specs/codegen-stage-c0.md"
	specC1        = "docs/specs/codegen-stage-c1.md"
	specC3        = "docs/specs/codegen-stage-c3.md"
	specC4        = "docs/specs/codegen-stage-c4.md"
	specC5        = "docs/specs/codegen-stage-c5.md"
	specGodogDocs = "docs/specs/cypher-golden-test-migration.md"
	adrFence      = "docs/adr/0029-the-codegen-spec-fence.md"
)

// specSigDocs are the documents that print an emitted query method
// signature whole, mapped to how many graded argument names each owes.
//
// The number is a floor, not an equality: a document that grows a
// signature is green, and only a document that loses one is red. That
// asymmetry is deliberate. An equality reddens on every honest addition,
// and a census that reddens on honest edits is a census maintainers bump
// without reading — which is the failure mode the per-site verbatim
// census was refused for (gqlc-0rjn). Adding a signature and leaving the
// floor where it is costs nothing and protects the sites already
// counted; only a REMOVAL has to be written down here, which is the same
// price the membership half of this census already charges.
var specSigDocs = map[string]int{specC1: 5, specC4: 10, specC5: 2}

// specBindDocs are the documents that print a `map[string]any` driver
// binding, mapped to how many graded binding values each owes. A floor,
// on the same terms as specSigDocs.
var specBindDocs = map[string]int{specC1: 3, specC3: 3, specC4: 3, specC5: 1}

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

// The four parameter lists the documentation prints that open the
// emitted query method's anchor and belong to something else: the
// driver/transaction `run` seam in both its spellings, the neo4j
// driver's own ExecuteWrite, and the godog step handler the golden-test
// migration note quotes. Each is spelled here once and counted per
// document in specNonMethodSites, so a document that respells one is red
// on its text rather than silently exempted under the old spelling.
const (
	runSeamList      = "ctx context.Context, cypher string, params map[string]any, access neo4j.AccessMode"
	runSeamTxList    = "ctx context.Context, cypher string, params map[string]any, _ neo4j.AccessMode"
	executeWriteList = "ctx context.Context, session SessionWithContext, " +
		"work ManagedTransactionWorkT[T], configurers ...func(*TransactionConfig)"
	godogStepList = "ctx context.Context, sigText string, _ *godog.Table"
)

// nonMethodSite spells one exempted site the way specNonMethodSites keys
// it: the name the document prints immediately before the parameter
// list, then the list itself back inside its parentheses.
//
// The name is half the key, and it is the half gqlc-yn2l is about. Keyed
// by the list alone, a document could replace one of these quotes IN
// PLACE with a claim about the emitted surface spelled identically — the
// count stayed satisfied and the claim went ungraded. It cannot now: an
// emitted query method's name is the query author's, so a claim carries
// that name where the quote carried `run`, `ExecuteWrite` or `func`, and
// the replacement is red twice over — once as an overlong list no entry
// covers, once as an entry whose count fell (ADR 0029 decision 14).
func nonMethodSite(name, list string) string { return name + "(" + list + ")" }

// The four sites above under the name each document prints them with.
// `func` is not an omission: the godog handler is quoted as an anonymous
// function literal, so `func` is what stands in the name position, and
// keying on it holds a replacement to still being one.
var (
	runSeamSite      = nonMethodSite("run", runSeamList)
	runSeamTxSite    = nonMethodSite("run", runSeamTxList)
	executeWriteSite = nonMethodSite("ExecuteWrite", executeWriteList)
	godogStepSite    = nonMethodSite("func", godogStepList)
)

// specNonMethodSites are the sites whose parameter list is longer than
// any the emitter renders, mapped per document to how many times each is
// printed there.
//
// The emitted query method's parameter list is a closed shape at every
// arity — `(ctx context.Context)` or `(ctx context.Context, arg <T>)`,
// never more — so ANY longer list opening that anchor is drift, and is
// graded as drift (gqlc-vu7z). What this census holds is the other
// population the same anchor reaches: functions the documentation prints
// that are not emitted query methods at all. Thirteen sites, four
// distinct lists, measured 2026-09-11 over the swept corpus.
//
// Telling those apart from a drifted claim cannot be done from the
// bytes: an emitted query method's name is the query author's, so there
// is no receiver, no keyword and no shape that a drifted four-parameter
// claim could not also wear. That is ADR 0042's finding for gqlc-x2sg
// restated one construct over, and the answer is the same — a census
// written down, reconciled in BOTH directions.
//
// What the name in the key buys is the OTHER half of gqlc-x2sg, which a
// general discriminator was never going to answer. No name tells an
// emitted query method from something else in the abstract; but these
// four sites are not in the abstract, and the names they are printed
// under — `run`, `ExecuteWrite`, `func` — are recorded here beside the
// lists. So an in-place replacement has to keep the name as well as the
// list, and a claim about the emitted surface does not (gqlc-yn2l, ADR
// 0029 decision 14).
//
// The count is exact rather than a floor, and that asymmetry is the
// opposite of specSigDocs' for the opposite reason. specSigDocs bounds a
// REQUIREMENT, where growing is honest; this bounds an EXEMPTION, where
// growing is the failure — an unrecorded fourteenth site spelled like
// one of these four would be waved through by a floor. So a document
// that starts printing the run seam once more is red until the number
// beside it moves, and a document that stops printing one is red too,
// because the exemption is then holding nothing.
//
// What it does NOT carry is the exhibitMarker half of gqlc-x2sg's
// mechanism, and the reason is measured rather than assumed: 11 of these
// 13 sites sit inside fenced Go code blocks, where `**exhibit**` is not
// emphasis but literal text corrupting the example (measured 2026-09-11;
// the two in prose are C4's ExecuteWrite span and the godog handler).
// The name in the key is what stands in for it, and it costs no byte in
// any document because every one of these sites already prints one.
//
// The residual after that is narrower than x2sg's and is stated rather
// than assumed: a replacement keeping BOTH halves is still exempted, so
// what it has to claim is that an emitted query method is named `run`,
// or `ExecuteWrite`, or is an anonymous `func` literal, AND takes a
// `cypher string` beside a driver `params map[string]any`.
var specNonMethodSites = map[string]map[string]int{
	specC0:        {runSeamSite: 2, runSeamTxSite: 1},
	specC1:        {runSeamSite: 6, runSeamTxSite: 1},
	specC4:        {runSeamSite: 1, executeWriteSite: 1},
	specGodogDocs: {godogStepSite: 1},
}

// nonMethodCensus flattens specNonMethodSites into the one-entry-per
// (document, list) form the census helpers reconcile, on exhibitEntry's
// terms.
func nonMethodCensus() map[string]int {
	out := map[string]int{}
	for doc, lists := range specNonMethodSites {
		for list, n := range lists {
			out[exhibitEntry(doc, list)] = n
		}
	}
	return out
}

// exhibitMarker is what a document writes on the line carrying a site
// this census exempts, and it is the whole of what tells an exhibit from
// a claim spelled the same way (gqlc-x2sg).
//
// Nothing about the BYTES of a quoted shape says whether the document is
// exhibiting drift or asserting the emitted surface — an exhibit and a
// claim are the same construct, so no scanner and no markdown parse can
// separate them (ADR 0042). What separates them is the author's
// intent, and this is that intent written where the fence can read it.
//
// Before it, the exemption went to whichever matching site came FIRST in
// the document, so replacing an exhibit in place with a claim spelled the
// way that exhibit was kept the census satisfied and left the claim
// ungraded. The exemption now follows the marker rather than the
// position: an unmarked site is graded whatever the census says, and an
// entry no marked site carries is red in the `lost` direction.
//
// Two costs, both accepted rather than overlooked. It puts fence syntax
// into prose a reader sees, which is the price of writing intent down at
// all. And it is read on the site's own LINE, so reflowing a paragraph
// until the marker and the span it marks land on different lines reddens
// the fence — fails closed, names the line, and the remedy is to write
// the marker back beside the span, which is to re-assert the intent
// rather than to bump a number. There are six marked sites today; the
// falsifier for calling that cost small is the census growing long
// enough that re-marking becomes rote.
const exhibitMarker = "**exhibit**"

// specBareListExhibits are the parenthesis-less parameter lists a
// document prints as exhibits of what the fence catches rather than as
// claims about the emitted surface, and so are read but not graded (ADR
// 0029 decision 10).
//
// The exemption is per list, spelled verbatim: a parenthesis-less list
// a listed document prints that is not written down here is read on the
// same terms as any other document's, so one document can quote a
// drifted shape as an exhibit and state the emitted shape as a claim.
// Each entry exempts one site, so a second list spelled the same way is
// graded.
//
// Naming the list here is necessary and not sufficient: the site must
// also carry exhibitMarker on its line. Both halves are load-bearing in
// opposite directions — the census bounds WHICH shapes may be exempted
// at all, and the marker says WHICH occurrence of one is the exhibit.
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

// specBareBindExhibits is the same census for the binding half: the
// brace-less `"key": value` spans a document prints as exhibits of what
// the fence catches rather than as claims about the emitted surface.
//
// Both documents named here print the spelling gqlc-offa was filed
// about, inside the sentence recording that it was unread. Closing that
// bead turns those sentences into graded sites, and a limit's own
// statement of itself is not a claim about the emitter — so each is
// exempted by text, per site, on exactly the terms specBareListExhibits
// sets for the signature half.
//
// The exemption is per site and spelled verbatim, so a second span
// spelled the same way in the same document is graded, and an entry the
// document stopped printing is red by its text. It is claimed by
// exhibitMarker on the site's line, not by position, on exactly the
// terms specBareListExhibits sets (gqlc-x2sg).
var specBareBindExhibits = map[string][]string{
	specC1:   {`"minAge": minAge`, `"key": value`},
	adrFence: {`"key": value`},
}

// exhibitCensus flattens specBareListExhibits to one entry per exempted
// list, so that the reconciliation names the list to add or remove
// rather than the document holding it.
func exhibitCensus() []string { return flattenExhibits(specBareListExhibits) }

// bindExhibitCensus is the same flattening for specBareBindExhibits.
func bindExhibitCensus() []string { return flattenExhibits(specBareBindExhibits) }

func flattenExhibits(m map[string][]string) []string {
	var out []string
	for doc, lists := range m {
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

// marked reports whether a site's line claims the exemption its census
// entry offers. The line is the collapsed source line the site opens on,
// which is what every scanner here already carries for its failures, so
// the marker travels with the span under a reflow that keeps them on one
// line and is lost by one that does not.
func marked(line string) bool { return strings.Contains(line, exhibitMarker) }

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
// the parens off, where the run of backticks closing the span ends the
// list (ADR 0029 decision 10).
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

	sweep := sweepSigs(
		files,
		func(file string) string { return readDoc(t, file) },
		specBareListExhibits,
		specNonMethodSites,
	)

	var bad []specSig
	for _, sig := range sweep.graded {
		if sig.arg != codegen.ParamArg {
			bad = append(bad, sig)
		}
	}

	requireClean(t, sweep.unclosed, "documented parameter list does not close",
		"these documents open a parameter list on `ctx context.Context` and never close the delimiter that\n"+
			"ends it — the parenthesis, or a run of backticks as long as the one the span opened on — so the\n"+
			"fence cannot read the argument out of them and silently graded nothing there; fix the text rather\n"+
			"than the fence — an unreadable site is an ungraded site")

	requireClean(t, bad, "documented method argument is not generator-owned",
		fmt.Sprintf("these documented signatures name the emitted method argument after the query author's parameter\n"+
			"instead of after the generator; the emitter binds codegen.ParamArg (%q) at every arity, precisely so\n"+
			"that no author-chosen identifier reaches the scope the method body resolves in (gqlc-lhs3, gqlc-rz0l)",
			codegen.ParamArg))

	requireClean(t, sweep.overlong, "documented method takes more parameters than the emitter renders",
		"these documented signatures open `(ctx context.Context` and then carry more than the one argument\n"+
			"the emitter binds. The emitted parameter list is a closed shape at every arity — one query\n"+
			"parameter renders as `arg <T>`, two or more render as a single `arg <MethodName>Params` struct —\n"+
			"so a list naming the query author's parameters as separate arguments is the capture vector\n"+
			"gqlc-lhs3 removed, written in a second spelling (gqlc-vu7z).\n\n"+
			"If the function above is not an emitted query method — the driver/transaction `run` seam, the\n"+
			"driver's own API, a test harness handler — write its NAME and parameter list into\n"+
			"specNonMethodSites beside its document, with the number of times that document prints it.\n"+
			"The name is part of the key, so a site that kept the list and changed the name is reported\n"+
			"here rather than exempted: that is the in-place replacement gqlc-yn2l is about")

	requireCensusExact(t, nonMethodCensus(), sweep.nonMethod, "specNonMethodSites", "exempted signature",
		"each entry above is one site a document prints that opens the emitted query method's anchor and\n"+
			"belongs to something else — spelled as the name before the parameter list, then the list — and\n"+
			"the number beside it is how many times that document prints it. The count is exact, not a\n"+
			"floor: this census bounds an EXEMPTION, so a site it has not been told about is the failure it\n"+
			"exists to prevent, and a site that went away means the exemption is holding nothing\n"+
			"(gqlc-vu7z). An entry reported missing here with no matching arrival may instead be a quote\n"+
			"whose NAME was rewritten in place; the list is then unchanged and the new name is graded as\n"+
			"drift by the failure above (gqlc-yn2l)")

	requireCensusFloors(t, specSigDocs, sweep.sigDocs, "specSigDocs", "graded signature",
		"each document on this list prints emitted query methods whose arguments this fence reads, and the\n"+
			"number beside it is how many it owes; contributing fewer means one of its signatures has moved out\n"+
			"of the sweep — stopped printing `(ctx context.Context`, or the code span around it — or the scanner\n"+
			"no longer reads it. Contributing none means the whole surface went")

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
			"a list it prints that is not spelled above is read on the same terms as any other document's.\n"+
			"An entry is claimed by the site writing "+exhibitMarker+" on its own line and not by the site\n"+
			"coming first, so an entry reported missing here may instead be a marker a reflow carried onto\n"+
			"another line — the site is then graded, and named by the failure above (gqlc-x2sg)")

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

	sweep := sweepBinds(files, func(file string) string { return readDoc(t, file) }, specBareBindExhibits)

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
	requireCensusFloors(t, specBindDocs, sweep.bindDocs, "specBindDocs", "graded binding",
		"each document on this list prints the `map[string]any` literals the emitted body passes to the run\n"+
			"seam, and the number beside it is how many bindings it owes; contributing fewer means one literal\n"+
			"has stopped printing `map[string]any{` — the anchor is the type with its opening brace, so rewriting\n"+
			"it to any other literal type unreads every binding inside it. Contributing none means they all went")

	// The brace-less spans a document prints as exhibits of what the
	// binding sweep used to miss, reconciled in both directions
	// (gqlc-offa). An entry whose span the document stopped printing is
	// an exemption outliving the exhibit it covers; a bare binding this
	// census does not name is graded rather than exempted, so the sweep
	// produces no entry the census lacks.
	requireCensus(t, bindExhibitCensus(), sweep.bareExhibits, "specBareBindExhibits",
		"a `\"key\": value` span with no `map[string]any{` around it is a documented binding the sweep now\n"+
			"reads (gqlc-offa). The entries here are the spans printed as exhibits of that limit rather than as\n"+
			"claims about the emitted surface, and each covers exactly one site — a second span spelled the same\n"+
			"way in the same document is graded. An entry is claimed by "+exhibitMarker+" on the site's own\n"+
			"line, not by the site coming first (gqlc-x2sg)")
}

// TestSpecDocumentsCarryNoGradedAnchorInsideAnHTMLComment refuses the one
// construct that lets a document's rendered text and its swept bytes
// disagree.
//
// Both sweeps above read bytes, so an HTML comment is invisible to the
// reader and fully present to them. That cuts two ways and only one of
// them is a nuisance. A commented site whose text has DRIFTED reddens the
// fence over a line no reader can see: annoying, loud, self-correcting. A
// commented site that is CORRECT quietly pays a census — specSigDocs and
// specBindDocs are per-document floors, so a document can meet its whole
// obligation on signatures nobody reads, and the floor that exists to
// prove the surface is still documented proves nothing (bd gqlc-jnsk).
// The second is the direction that matters, and it cannot be caught by
// reading the site harder, because there is nothing wrong with the site.
//
// So this is an absence check, not a scanner: the anchors may not appear
// inside a comment at all, in either condition. ADR 0042 prefers refusing
// a construct over interpreting it, and that preference is what makes
// this affordable — deciding whether a comment's contents would have
// rendered is a markdown parse, while deciding whether a byte run sits
// between `<!--` and `-->` is not (ADR 0029 decision 15).
//
// The corpus passes today: the one real HTML comment in it is in
// docs/bd-ledger-writes.md and carries no anchor.
func TestSpecDocumentsCarryNoGradedAnchorInsideAnHTMLComment(t *testing.T) {
	files := docFiles(t)
	require.NotEmpty(t, files, "the fence swept no documents; docRoots is stale")

	var hidden []specSig
	for _, file := range files {
		hidden = append(hidden, commentedAnchors(file, readDoc(t, file))...)
	}

	requireClean(t, hidden, "documented anchor is buried in an HTML comment",
		"each line above names an anchor one of this fence's sweeps reads, sitting inside an HTML comment.\n"+
			"A renderer hides that text from every reader; a byte scan does not, so the two disagree about\n"+
			"what the document says. The harm runs in the quiet direction: specSigDocs and specBindDocs are\n"+
			"per-document floors, and a commented site satisfies its document's floor while showing the\n"+
			"reader nothing — the census then vouches for a surface the documentation has stopped\n"+
			"describing (gqlc-jnsk).\n\n"+
			"The remedy is in the text: delete the commented block, or uncomment it so the claim it makes\n"+
			"is one a reader can check. There is no exemption list here on purpose — an exemption would be\n"+
			"a second invisible place for a signature to live")
}

// TestEveryRootDocIsSweptOrDeclaredOutOfScope reconciles the repository
// root's own markdown against the two lists that may account for it:
// docRoots, which sweeps a document, and nonSpecRootDocs, which exempts
// one with a reason. A document in neither is red, and the failure names
// both remedies.
//
// It is the only check in this file whose candidate set is not derived
// from docRoots, and that is what it is for. Every other assertion here
// reads what the sweep produced, and a root dropped from the list
// produces nothing — so the evidence a shrinking walk would be caught by
// is exactly what the shrinking removes. Measured on gqlc-jfwo: a
// markdown document added at the repository root printing both graded
// anchors, `(ctx context.Context, id int64)` and a `map[string]any{`
// binding, left all of this package green before this test existed.
//
// The reconciliation runs in both directions, so it also refuses a
// declared document that has left the disk, and — through requireCensus'
// duplicate arm — one named as swept and exempt at once, where either
// line could later be deleted under cover of the other.
func TestEveryRootDocIsSweptOrDeclaredOutOfScope(t *testing.T) {
	// A docRoots entry accounts for a root document only by being that
	// document, so the tree entries answer for nothing here. A nested
	// entry naming a document rather than a tree would be reported below
	// as declared and unobserved; none exists, and `docs` covers the
	// case that would motivate one.
	var declared []string
	for _, root := range docRoots {
		if strings.HasSuffix(root, ".md") {
			declared = append(declared, root)
		}
	}
	declared = append(declared, reasonedNames(t, nonSpecRootDocs, "nonSpecRootDocs")...)
	sort.Strings(declared)

	entries, err := os.ReadDir(repoRoot)
	require.NoError(t, err, "the repository root is unreadable, so this census has no candidate set")

	observed := make(map[string]bool)
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".md") {
			observed[entry.Name()] = true
		}
	}

	requireCensus(t, declared, observed, "docRoots and nonSpecRootDocs together",
		"a markdown document at the repository root is either swept for spec drift — name it in docRoots —\n"+
			"or deliberately out of scope — name it in nonSpecRootDocs, with the reason. A document in neither\n"+
			"list is in a third state nobody chose: unswept because nobody was asked. That is how README.md and\n"+
			"CONTEXT.md came to be swept while AGENTS.md, CLAUDE.md and CONTRIBUTING.md came not to be, and it\n"+
			"is the state this test exists to make impossible to reach quietly")
}

// TestEveryDocTreeIsSweptOrDeclaredOutOfScope is the same reconciliation one
// level up: the repository's top-level DIRECTORIES holding markdown, against
// docRoots — which sweeps a tree — and nonSpecDocTrees, which exempts one
// with a reason.
//
// Its sibling above censuses root FILES and nothing else, so until this
// existed a whole TREE added beside them was unswept and silent about it.
// Measured 2026-09-05 on master 5aa2cdbd: a tracked `spec/drift.md` printing
// both graded anchors, `(ctx context.Context, id int64)` and a
// `map[string]any{` binding, left every test in this package green
// (gqlc-tn88a).
//
// The candidate set is the tracked paths rather than a directory listing,
// which is the whole of why this was not written with its sibling. The
// listing sees untracked scratch trees, and those exist in some checkouts
// and not others.
//
// A candidate set that arrives empty does not pass quietly, and needs no
// separate guard to say so: every declared name is then unobserved, and
// requireCensus reports each by name. So the reconciliation is its own
// control for having read anything at all — `docs` is tracked and full of
// markdown, so an index this cannot read cannot look like a clean census.
func TestEveryDocTreeIsSweptOrDeclaredOutOfScope(t *testing.T) {
	// A docRoots entry answers for a tree here only by being one. Its
	// `.md` entries are the sibling census's business, exactly as the
	// tree entries are none of that one's.
	var declared []string
	for _, root := range docRoots {
		if !strings.HasSuffix(root, ".md") {
			declared = append(declared, root)
		}
	}
	declared = append(declared, reasonedNames(t, nonSpecDocTrees, "nonSpecDocTrees")...)
	sort.Strings(declared)

	observed := make(map[string]bool)
	for _, path := range trackedFiles(t) {
		if dir, _, nested := strings.Cut(path, "/"); nested && strings.HasSuffix(path, ".md") {
			observed[dir] = true
		}
	}

	requireCensus(t, declared, observed, "docRoots and nonSpecDocTrees together",
		"a top-level directory holding markdown is either swept for spec drift — name it in docRoots —\n"+
			"or deliberately out of scope — name it in nonSpecDocTrees, with the reason. A tree in neither\n"+
			"list is unswept because nobody was asked, which is the state a `spec/` or `guides/` tree added\n"+
			"tomorrow arrives in: printing emitted signatures, graded by nothing, and saying so nowhere")
}

// trackedFiles lists the repository's tracked paths, named relative to
// repoRoot. It reads the index rather than the filesystem so that untracked
// scratch trees are invisible to the census above.
//
// git is a dependency the rest of this package does not have, and an absent
// or unreadable one fails the run rather than skipping it. A fence that
// could not read its candidate set has not established that anything is
// declared, and reporting that as a pass is the single answer it must not
// give — the same call os.ReadDir's error makes in the sibling above.
func trackedFiles(t *testing.T) []string {
	t.Helper()

	cmd := exec.CommandContext(t.Context(), "git", "ls-files", "-z")
	cmd.Dir = repoRoot
	out, err := cmd.Output()
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		err = fmt.Errorf("%w: %s", err, bytes.TrimSpace(exit.Stderr))
	}
	require.NoError(t, err, "the repository index is unreadable, so this census has no candidate set")

	return strings.FieldsFunc(string(out), func(r rune) bool { return r == 0 })
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
	graded   []specSig
	unclosed []specSig

	// overlong are the sites whose parameter list is longer than any the
	// emitter renders and which specNonMethodSites does not account for,
	// so they are drift rather than another function (gqlc-vu7z).
	overlong []specSig

	// nonMethod counts the overlong sites that census does account for,
	// per (document, list) entry, so the exemption is reconciled against
	// the corpus in both directions AND by count.
	nonMethod map[string]int

	sigDocs      map[string]int
	exemptDocs   map[string]bool
	ruleDocs     map[string]bool
	bareExhibits map[string]bool
	statedRules  map[string]map[string]bool
}

type bindSweep struct {
	graded   []specSig
	unclosed []specSig
	bindDocs map[string]int

	// bareExhibits are the brace-less binding spans a document prints as
	// exhibits of the limit rather than as claims, routed out of the
	// graded set and reconciled against specBareBindExhibits (gqlc-offa).
	bareExhibits map[string]bool
}

// sweepSigs runs all three signature scanners over every named document,
// reading each one with read and treating the parenthesis-less
// parameter lists that exhibits names for a document as
// read-but-not-graded. Each name covers one site; every other bare list
// in the same document is read on the same terms as any other's.
//
// A list longer than any the emitter renders is drift, and is routed the
// other way round from an exhibit: the census is consulted by (document,
// list) and an accounted site is COUNTED rather than deleted from a
// pending set, because one entry there covers a stated number of sites
// rather than exactly one (gqlc-vu7z).
func sweepSigs(
	files []string,
	read func(string) string,
	exhibits map[string][]string,
	nonMethod map[string]map[string]int,
) sigSweep {
	out := sigSweep{
		nonMethod:    map[string]int{},
		sigDocs:      map[string]int{},
		exemptDocs:   map[string]bool{},
		ruleDocs:     map[string]bool{},
		bareExhibits: map[string]bool{},
		statedRules:  map[string]map[string]bool{},
	}
	for _, file := range files {
		text := read(file)

		sigs, exempt, long, broken := scanSpecSigs(file, text)
		out.unclosed = append(out.unclosed, broken...)
		if len(exempt) > 0 {
			out.exemptDocs[file] = true
		}

		bare, bareLong, bareBroken := scanBareSigs(file, text)
		out.unclosed = append(out.unclosed, bareBroken...)

		for _, sites := range [][]specSig{long, bareLong} {
			for _, site := range sites {
				if _, declared := nonMethod[file][site.list]; !declared {
					out.overlong = append(out.overlong, site)
					continue
				}
				out.nonMethod[exhibitEntry(file, site.list)]++
			}
		}
		unclaimed := map[string]bool{}
		for _, list := range exhibits[file] {
			unclaimed[list] = true
		}
		for _, sig := range bare {
			if !unclaimed[sig.list] || !marked(sig.text) {
				out.graded = append(out.graded, sig)
				continue
			}
			delete(unclaimed, sig.list)
			out.bareExhibits[exhibitEntry(file, sig.list)] = true
		}

		for _, sig := range sigs {
			out.sigDocs[file]++
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

// sweepBinds runs both binding scanners over every named document: the
// one reading a `map[string]any{…}` literal, and the one reading a
// binding stated as a bare code span with no literal around it
// (gqlc-offa). A bare span the exhibits census names for that document
// is routed out of the graded set, one entry per site, the way
// sweepSigs routes a parenthesis-less parameter list.
//
// A bare site does NOT set bindDocs. That census asks which documents
// print the map literal the emitted body passes to the run seam, and a
// document that stopped printing it while keeping a sentence about
// bindings would otherwise keep its entry on the sentence.
func sweepBinds(files []string, read func(string) string, exhibits map[string][]string) bindSweep {
	out := bindSweep{bindDocs: map[string]int{}, bareExhibits: map[string]bool{}}
	for _, file := range files {
		text := read(file)

		binds, broken := scanSpecBinds(file, text)
		out.unclosed = append(out.unclosed, broken...)
		for _, bind := range binds {
			out.bindDocs[file]++
			out.graded = append(out.graded, bind)
		}

		unclaimed := map[string]bool{}
		for _, span := range exhibits[file] {
			unclaimed[span] = true
		}
		for _, site := range scanBareBinds(file, text) {
			if !unclaimed[site.list] || !marked(site.text) {
				out.graded = append(out.graded, site.binds...)
				continue
			}
			delete(unclaimed, site.list)
			out.bareExhibits[exhibitEntry(file, site.list)] = true
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
// TestSpecBareBindScannerReadsOnlyABindingShapedSpan pins both halves of
// scanBareBinds' narrowing on synthetic input.
//
// The corpus cannot pin either. The reject rows are shapes the swept
// documents hold by the dozen, so a relaxation reddens the live run
// loudly — but it reddens it on a document, and what a reader then
// learns is that a spec is wrong rather than that this scanner widened.
// The accept rows are the reverse: the three bare bindings in the corpus
// today are all exempted exhibits, so a scanner that stopped reading
// them would be caught by the census reconciliation and by nothing that
// says what it stopped reading.
//
// The two narrowings are separable here and are separately mutated: drop
// the jsonScalars exclusion and the `"nullable": true` row goes graded;
// drop the requirement that the whole span be pairs and the JSON-object
// and prose rows do (gqlc-offa).
func TestSpecBareBindScannerReadsOnlyABindingShapedSpan(t *testing.T) {
	for _, row := range []struct {
		name  string
		span  string
		binds []string
		why   string
	}{{
		name:  "a bare driver binding",
		span:  `"minAge": minAge`,
		binds: []string{"minAge"},
		why:   "the shape gqlc-offa was filed about, and the whole reason this scanner exists",
	}, {
		name:  "a bare binding through a carrier conversion",
		span:  `"seenAt": agtypeNullableMicros(arg.SeenAt)`,
		binds: []string{"arg.SeenAt"},
		why:   "the widen is orthogonal to who owns the name, as it is inside a literal",
	}, {
		name:  "two pairs in one span",
		span:  `"id": arg.ID, "name": arg.Name`,
		binds: []string{"arg.ID", "arg.Name"},
		why:   "a binding list is still a binding list without the literal around it",
	}, {
		name: "a JSON scalar",
		span: `"nullable": true`,
		why: "the corpus states model shapes this way in dozens of files; a scanner reading " +
			"them reddens on prose, which is why ADR 0029 decision 10 declined the symmetric " +
			"move and gqlc-offa recorded the hole rather than closing it wide",
	}, {
		name: "a JSON type name",
		span: `"directed": bool`,
		why:  "same, and it is not an expression in the emitted method's Go scope either",
	}, {
		name: "a JSON object",
		span: `{"kind": "property", "type": "INT", "nullable": true}`,
		why:  "the brace makes the head of the span something other than a quoted key",
	}, {
		name: "a quoted value",
		span: `"kind": "expr"`,
		why:  "a string literal is not an identifier the emitted method could read",
	}, {
		name: "prose that happens to contain a pair",
		span: `config: <src>: field "version": yaml: unmarshal errors`,
		why:  "the span is read whole, so a pair buried in a message is not a binding",
	}, {
		name: "a Go composite literal of some other type",
		span: `exportedOptionalGroup{"a": g, "b": g}`,
		why:  "the head is a type name, so the span is not a bare pair list",
	}, {
		// This row is the one that separates the head-of-span test from
		// the value test. Measured: relax `bareBindValues` to seek the
		// first quote instead of requiring the span to open with one, and
		// every other reject row above stays rejected — splitTopLevel
		// keeps a braced span whole and the value test then throws it out
		// — so without this row the head test is an equivalent mutant.
		//
		// It is also the residual of gqlc-offa rather than a shape nobody
		// would write: a binding stated inside a sentence that is itself
		// one code span is read by nothing here. The hole is narrower than
		// the one that bead measured, and it is still a hole.
		name: "a pair at the tail of a prose span",
		span: `the driver binding is "minAge": minAge`,
		why: "the span is graded whole or not at all; a sentence carrying a pair is not a " +
			"pair list, and reading one would put every English clause ending in a colon " +
			"into the graded set",
	}} {
		t.Run(row.name, func(t *testing.T) {
			text := "prose `" + row.span + "` prose\n"
			var got []string
			for _, site := range scanBareBinds("synthetic.md", text) {
				for _, bind := range site.binds {
					got = append(got, bind.arg)
				}
			}
			require.Equalf(t, row.binds, got, "scanBareBinds read %v out of `%s`, want %v. %s",
				got, row.span, row.binds, row.why)
		})
	}
}

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
		sweep := sweepSigs(files, read, nil, nil)
		require.Len(t, sweep.unclosed, 1)
		require.Equal(t, "unreadable.md", sweep.unclosed[0].file)
		require.Equal(t, 2, sweep.unclosed[0].line)
		require.NotEmpty(t, sweep.graded, "the readable document is still swept")
		require.Positive(t, sweep.sigDocs["readable.md"])
	})

	t.Run("the binding sweep carries an unreadable literal out of the scanner", func(t *testing.T) {
		sweep := sweepBinds(files, read, nil)
		require.Len(t, sweep.unclosed, 1)
		require.Equal(t, "unreadable.md", sweep.unclosed[0].file)
		require.Equal(t, 4, sweep.unclosed[0].line)
		require.NotEmpty(t, sweep.graded, "the readable document is still swept")
		require.Positive(t, sweep.bindDocs["readable.md"])
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
//
// The last two are gqlc-x2sg. The sixth is that bead's reproduction
// reduced to one document: a listed list the document prints WITHOUT
// exhibitMarker is graded, so a claim put in an exhibit's place and
// spelled the way that exhibit was no longer inherits its exemption. The
// seventh separates the marker from the position, which is the whole of
// what changed — the census entry goes to the marked occurrence even
// when an unmarked one precedes it, and an ordering rule that merely
// looked at the census would take the first.
func TestSpecSweepRoutesBareSitesByExhibit(t *testing.T) {
	const drifted = "ctx context.Context, minAge int64"
	claim := "ctx context.Context, " + codegen.ParamArg + " int64"
	docs := map[string]string{
		"graded.md":  "the parameter list is `" + drifted + "`\n",
		"exhibit.md": "the " + exhibitMarker + " parameter list is `" + drifted + "`\n",
		"silent.md":  "this document quotes no parameter list at all\n",
		"mixed.md": "the " + exhibitMarker + " is `" + drifted + "` and the emitted list is `" +
			claim + "`\n",
		"twice.md": "the " + exhibitMarker + " is `" + drifted + "` and so is `" + drifted + "`\n",

		// gqlc-x2sg's reproduction: the census still names the list, and
		// the document prints it as a claim about the emitted surface
		// rather than as an exhibit, so it carries no marker.
		"unmarked.md": "the emitted parameter list is `" + drifted + "` at one query parameter\n",

		// The claim comes first and the exhibit second. Under the
		// positional rule the claim took the entry and went ungraded.
		"claimfirst.md": "the emitted parameter list is `" + drifted + "` at one query parameter\n" +
			"and the " + exhibitMarker + " it is spelled like is `" + drifted + "`\n",
	}
	read := func(file string) string { return docs[file] }
	listing := func(file string) map[string][]string {
		return map[string][]string{file: {drifted}}
	}

	t.Run("an unlisted document's bare list is graded", func(t *testing.T) {
		sweep := sweepSigs([]string{"graded.md"}, read, nil, nil)
		require.Len(t, sweep.graded, 1)
		require.Equal(t, "minAge", sweep.graded[0].arg)
		require.Empty(t, sweep.bareExhibits)
	})

	t.Run("a listed and marked bare list is censused instead of graded", func(t *testing.T) {
		sweep := sweepSigs([]string{"exhibit.md"}, read, listing("exhibit.md"), nil)
		require.Empty(t, sweep.graded)
		require.Equal(t, map[string]bool{"exhibit.md: " + drifted: true}, sweep.bareExhibits)
	})

	t.Run("a listed bare list the document stopped printing is not censused", func(t *testing.T) {
		sweep := sweepSigs([]string{"silent.md"}, read, listing("silent.md"), nil)
		require.Empty(t, sweep.bareExhibits, "the lost direction is what reports this")
	})

	t.Run("a listed document's unlisted bare list is graded", func(t *testing.T) {
		sweep := sweepSigs([]string{"mixed.md"}, read, listing("mixed.md"), nil)
		require.Len(t, sweep.graded, 1)
		require.Equal(t, codegen.ParamArg, sweep.graded[0].arg)
		require.Equal(t, map[string]bool{"mixed.md: " + drifted: true}, sweep.bareExhibits)
	})

	t.Run("a second copy of a listed bare list is graded", func(t *testing.T) {
		sweep := sweepSigs([]string{"twice.md"}, read, listing("twice.md"), nil)
		require.Len(t, sweep.graded, 1)
		require.Equal(t, "minAge", sweep.graded[0].arg)
		require.Equal(t, map[string]bool{"twice.md: " + drifted: true}, sweep.bareExhibits)
	})

	t.Run("a listed bare list with no marker on its line is graded", func(t *testing.T) {
		sweep := sweepSigs([]string{"unmarked.md"}, read, listing("unmarked.md"), nil)
		require.Len(t, sweep.graded, 1,
			"a claim spelled the way an exhibit is takes no exemption from the census naming that "+
				"spelling; only "+exhibitMarker+" on the site's line does (gqlc-x2sg)")
		require.Equal(t, "minAge", sweep.graded[0].arg)
		require.Empty(t, sweep.bareExhibits,
			"and the entry is then reported lost, so the census cannot go on covering nothing")
	})

	t.Run("the exemption follows the marker rather than the position", func(t *testing.T) {
		sweep := sweepSigs([]string{"claimfirst.md"}, read, listing("claimfirst.md"), nil)
		require.Len(t, sweep.graded, 1, "the unmarked occurrence is graded even though it comes first")
		require.Equal(t, 1, sweep.graded[0].line, "and it is the one on line 1 that is graded")
		require.Equal(t, map[string]bool{"claimfirst.md: " + drifted: true}, sweep.bareExhibits)
	})
}

// TestSpecSweepRoutesBareBindsByExhibit is the same witness for the
// binding half, which routes through its own lines in sweepBinds and so
// is not covered by the signature rows above. The exemption is claimed
// on identical terms, and the last row is gqlc-x2sg's reproduction
// restated over a binding span.
func TestSpecSweepRoutesBareBindsByExhibit(t *testing.T) {
	const drifted = `"minAge": minAge`
	docs := map[string]string{
		"exhibit.md":  "the " + exhibitMarker + " binding is `" + drifted + "`\n",
		"unmarked.md": "the emitted driver binding is `" + drifted + "`\n",
	}
	read := func(file string) string { return docs[file] }
	listing := func(file string) map[string][]string {
		return map[string][]string{file: {drifted}}
	}

	t.Run("a listed and marked bare binding is censused instead of graded", func(t *testing.T) {
		sweep := sweepBinds([]string{"exhibit.md"}, read, listing("exhibit.md"))
		require.Empty(t, sweep.graded)
		require.Equal(t, map[string]bool{"exhibit.md: " + drifted: true}, sweep.bareExhibits)
	})

	t.Run("a listed bare binding with no marker on its line is graded", func(t *testing.T) {
		sweep := sweepBinds([]string{"unmarked.md"}, read, listing("unmarked.md"))
		require.Len(t, sweep.graded, 1,
			"a binding claim spelled the way an exhibit is takes no exemption from the census (gqlc-x2sg)")
		require.Equal(t, "minAge", sweep.graded[0].arg)
		require.Empty(t, sweep.bareExhibits)
	})
}

// TestSpecSweepRoutesOverlongSitesByCensus is the witness for the other
// routing in sweepSigs: a parameter list longer than any the emitter
// renders is reported as drift unless specNonMethodSites accounts for it
// in the document printing it (gqlc-vu7z).
//
// The corpus cannot stand in for these rows. Every overlong site it
// holds today is accounted for, so the live run exercises the counting
// arm and never the reporting one; a sweep that routed EVERYTHING to the
// census would be green on the whole repository. The drifted row below
// is the bead's own reproduction, and it is the row that was measured
// green before this change.
//
// The counting arm is the half the exact census needs: one entry stands
// for a stated number of sites, so an occurrence has to add to a count
// rather than claim an entry the way an exhibit does.
func TestSpecSweepRoutesOverlongSitesByCensus(t *testing.T) {
	const (
		seam    = "ctx context.Context, cypher string, params map[string]any, access neo4j.AccessMode"
		drifted = "ctx context.Context, minAge int64, locale string"
	)
	var (
		seamSite  = nonMethodSite("run", seam)
		bareSite  = nonMethodSite("", seam)
		claimSite = nonMethodSite("PeopleOverAge", seam)
	)
	docs := map[string]string{
		"seam.md":    "the seam is `func (d driverDB) run(" + seam + ") error`\n",
		"twice.md":   "`run(" + seam + ") error`\nand again `run(" + seam + ") error`\n",
		"drift.md":   "`func (q *Queries) PeopleOverAge(" + drifted + ") ([]string, error)`\n",
		"bare.md":    "the seam takes `" + seam + "`\n",
		"wrapped.md": "```go\nfunc (d driverDB) run(\n\tctx context.Context,\n\tcypher string,\n\tparams map[string]any,\n\taccess neo4j.AccessMode,\n) error\n```\n",
		"claim.md":   "the emitted method is `func (q *Queries) PeopleOverAge(" + seam + ") error`\n",
		"generic.md": "`func ExecuteWrite[T any](" + executeWriteList + ") (T, error)`\n",
	}
	read := func(file string) string { return docs[file] }
	census := func(file, site string, n int) map[string]map[string]int {
		return map[string]map[string]int{file: {site: n}}
	}

	t.Run("a declared site is counted rather than reported", func(t *testing.T) {
		sweep := sweepSigs([]string{"seam.md"}, read, nil, census("seam.md", seamSite, 1))
		require.Empty(t, sweep.overlong)
		require.Empty(t, sweep.graded)
		require.Equal(t, map[string]int{"seam.md: " + seamSite: 1}, sweep.nonMethod)
	})

	t.Run("each occurrence adds to the count", func(t *testing.T) {
		sweep := sweepSigs([]string{"twice.md"}, read, nil, census("twice.md", seamSite, 2))
		require.Empty(t, sweep.overlong)
		require.Equal(t, map[string]int{"twice.md: " + seamSite: 2}, sweep.nonMethod,
			"an exact census needs the second occurrence counted, not absorbed by the first")
	})

	t.Run("an undeclared overlong list is reported", func(t *testing.T) {
		sweep := sweepSigs([]string{"drift.md"}, read, nil, census("drift.md", seamSite, 1))
		require.Empty(t, sweep.nonMethod)
		require.Len(t, sweep.overlong, 1,
			"the author's parameter names as separate arguments are drift, and were unswept (gqlc-vu7z)")
		require.Equal(t, nonMethodSite("PeopleOverAge", drifted), sweep.overlong[0].list)
	})

	// The gqlc-yn2l row. The document prints the declared list, verbatim,
	// the declared number of times — everything the census used to key on
	// — and has replaced the quote it stood for with a claim about the
	// emitted surface. Both arms have to move: the claim is reported as
	// drift, and the entry it displaced counts nothing, so the exact
	// census is red in the `lost` direction as well.
	t.Run("a claim replacing the quote in place keeps neither the exemption nor the count", func(t *testing.T) {
		sweep := sweepSigs([]string{"claim.md"}, read, nil, census("claim.md", seamSite, 1))
		require.Len(t, sweep.overlong, 1,
			"the name is half the census key, so a claim wearing the declared list is still graded (gqlc-yn2l)")
		require.Equal(t, claimSite, sweep.overlong[0].list)
		require.Empty(t, sweep.nonMethod,
			"the declared entry now covers nothing, and an exact census reports that as a loss")
	})

	t.Run("the exemption is scoped to the document that declared it", func(t *testing.T) {
		sweep := sweepSigs([]string{"seam.md"}, read, nil, census("elsewhere.md", seamSite, 1))
		require.Len(t, sweep.overlong, 1,
			"one document's entry does not exempt another document printing the same site")
		require.Empty(t, sweep.nonMethod)
	})

	t.Run("a bare span reaches the same census, under the empty name", func(t *testing.T) {
		sweep := sweepSigs([]string{"bare.md"}, read, nil, census("bare.md", bareSite, 1))
		require.Empty(t, sweep.overlong, "the code-span scanner's overlong sites route here too")
		require.Equal(t, map[string]int{"bare.md: " + bareSite: 1}, sweep.nonMethod)
	})

	t.Run("a bare span is not exempted by the named entry for the same list", func(t *testing.T) {
		sweep := sweepSigs([]string{"bare.md"}, read, nil, census("bare.md", seamSite, 1))
		require.Len(t, sweep.overlong, 1,
			"the parentheses are off, so the document printed no name and the site cannot claim a named entry")
		require.Empty(t, sweep.nonMethod)
	})

	t.Run("a wrapped list is the same census entry as an inline one", func(t *testing.T) {
		sweep := sweepSigs([]string{"wrapped.md"}, read, nil, census("wrapped.md", seamSite, 1))
		require.Empty(t, sweep.overlong,
			"gofmt's line breaks and trailing comma are formatting, not a different parameter list")
		require.Equal(t, map[string]int{"wrapped.md: " + seamSite: 1}, sweep.nonMethod)
	})

	// C4 quotes neo4j's ExecuteWrite with its type-parameter list, which
	// is the one site in the corpus where the byte before the paren is
	// not part of the name.
	t.Run("a type-parameter list is stepped over to reach the name", func(t *testing.T) {
		sweep := sweepSigs([]string{"generic.md"}, read, nil, census("generic.md", executeWriteSite, 1))
		require.Empty(t, sweep.overlong)
		require.Equal(t, map[string]int{"generic.md: " + executeWriteSite: 1}, sweep.nonMethod)
	})
}

// requireCensusFloors is requireCensus over a census whose entries carry
// a count: it reconciles membership in both directions exactly as
// requireCensus does, and then holds each declared document to the
// number of graded sites written beside it.
//
// The floor is what closes gqlc-0rjn. Membership alone gives a document
// an entry for as long as ONE graded site survives it, so C4 could go
// from ten graded signatures to one and stay declared, observed and
// green. The count is compared per document rather than in total,
// because a total is answered by any other document having grown: a
// sum over sources is satisfied while individual sources go silent, and
// the failure it prints names no document a reader can open.
//
// A floor of zero or less is refused. Writing one down is the same act
// as deleting the document's line, except that it leaves the line there
// looking like a guard, and the membership half of the reconciliation
// would then be satisfied by a document that grades nothing.
//
// The comparison is >=, not ==. See specSigDocs for why the asymmetry is
// deliberate rather than a weakening.
func requireCensusFloors(t fenceT, written map[string]int, observed map[string]int, census, unit, why string) {
	t.Helper()

	declared := make([]string, 0, len(written))
	for doc := range written {
		declared = append(declared, doc)
	}
	sort.Strings(declared)

	seen := make(map[string]bool, len(observed))
	for doc, n := range observed {
		if n > 0 {
			seen[doc] = true
		}
	}
	requireCensus(t, declared, seen, census, why)

	var zeroed, under []string
	for _, doc := range declared {
		floor := written[doc]
		if floor <= 0 {
			zeroed = append(zeroed, fmt.Sprintf("  %s: %d", doc, floor))
			continue
		}
		if got := observed[doc]; got < floor {
			under = append(under, fmt.Sprintf("  %s: %s declares %d %s(s), the sweep graded %d",
				doc, census, floor, unit, got))
		}
	}

	if len(zeroed) > 0 {
		require.Fail(t, census+" declares a floor of zero",
			"a document owing no graded site is a document this census is not guarding, and its line here reads\n"+
				"like a guard. Remove the line, or write down the number of sites the document actually owes:\n"+
				strings.Join(zeroed, "\n"))
	}
	if len(under) > 0 {
		require.Fail(t, census+" declares more graded sites than the sweep found",
			"each document below still contributes at least one site, so the membership reconciliation above is\n"+
				"satisfied; it is the count that fell. A site leaves the sweep by ceasing to print its anchor, not\n"+
				"by being corrected, so this is the arm that sees a claim quietly stop being graded:\n"+
				strings.Join(under, "\n")+"\n\n"+
				"Restore the site, or — if the document genuinely no longer states it — lower the number here in\n"+
				"the same commit, so that the removal is written down where the child text cannot edit it.\n\n"+why)
	}
}

// requireCensusExact is requireCensusFloors with the inequality closed:
// membership reconciled in both directions, then each declared entry
// held to its number in BOTH directions rather than only from below.
//
// The two helpers differ because the two kinds of census differ, and
// picking the wrong one is silent. A census of what a document OWES is a
// floor, because growing is honest and an equality reddens on every
// addition (specSigDocs). A census of what is EXEMPTED is not: growing
// is the failure mode, since an unrecorded site that matches a declared
// entry is exactly the site nobody wrote down. So specNonMethodSites is
// held to ==, and a document printing one more of the run seam is red
// until the number beside it moves.
//
// A count of zero or less is refused for the same reason a floor of zero
// is: it leaves a line that reads like a census entry while exempting
// nothing, and the membership half above would pass it.
func requireCensusExact(t fenceT, written map[string]int, observed map[string]int, census, unit, why string) {
	t.Helper()

	declared := make([]string, 0, len(written))
	for entry := range written {
		declared = append(declared, entry)
	}
	sort.Strings(declared)

	seen := make(map[string]bool, len(observed))
	for entry, n := range observed {
		if n > 0 {
			seen[entry] = true
		}
	}
	requireCensus(t, declared, seen, census, why)

	var zeroed, off []string
	for _, entry := range declared {
		want := written[entry]
		if want <= 0 {
			zeroed = append(zeroed, fmt.Sprintf("  %s: %d", entry, want))
			continue
		}
		if got := observed[entry]; got != want {
			off = append(off, fmt.Sprintf("  %s — %s declares %d %s(s), the sweep found %d",
				entry, census, want, unit, got))
		}
	}

	if len(zeroed) > 0 {
		require.Fail(t, census+" declares a count of zero",
			"an entry exempting no site is an entry guarding nothing, and its line here reads like a guard.\n"+
				"Remove the line, or write down the number of sites it actually covers:\n"+
				strings.Join(zeroed, "\n"))
	}
	if len(off) > 0 {
		require.Fail(t, census+" does not agree with the sweep on how many sites it covers",
			"each entry below is still observed, so the membership reconciliation above is satisfied; it is\n"+
				"the count that moved. Reading HIGH means a site this census covers stopped being printed and\n"+
				"the exemption now holds less than it claims; reading LOW means the document grew a site that\n"+
				"matched a declared entry and was exempted without anyone writing it down — which is the whole\n"+
				"failure this census is exact rather than a floor to catch:\n"+
				strings.Join(off, "\n")+"\n\n"+
				"Move the number here in the same commit as the document change, so that the exemption's size\n"+
				"is written down where the child text cannot edit it.\n\n"+why)
	}
}

// requireCensus reconciles a written census against what a sweep
// actually observed, naming the offending entry in the failure (ADR 0029
// decision 2). It refuses a census that declares nothing, a name declared
// twice, a declared entry the sweep did not produce, and an observed entry
// nobody declared.
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
//
// A census that declares nothing is refused before any of the other three
// is consulted, and require.Fail ends the run, so an empty census reports
// as one rather than as whatever it would have reconciled to. Each of the
// three is quantified over a set, so none has an entry to report when the
// written census and the sweep are empty together — emptying the census
// literals and blanking the swept documents in a single edit reconciles
// clean without this arm. The precedence is pinned as far as it can be:
// TestSpecFailuresAreWired's row "requireCensus refuses an empty census
// ahead of the undeclared arm" drives the only input that trips this arm
// and another at once. An empty census cannot also trip the duplicate or
// the lost arm — both are quantified over the declared names, and there
// are none — so their order against this one is not observable at all.
func requireCensus(t fenceT, written []string, observed map[string]bool, census, why string) {
	t.Helper()

	if len(written) == 0 {
		require.Fail(t, census+" declares no entry",
			"a census that declares nothing reconciles clean against a sweep that observed nothing, so every\n"+
				"comparison below is satisfied by a sweep that read none of the text it grades:\n\n"+why)
	}

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

// reasonedNames returns the names an exemption census declares, refusing
// any written down without the reason it rests on. A bare name records
// that somebody decided and not what they decided on, so the next reader
// has nothing to check the decision against — which is how an exemption
// outlives its argument.
//
// It returns the names rather than only inspecting them so that the
// caller must route the census through it. Written as its own statement
// beside the loop that reads the map, the check was a line whose deletion
// changed nothing else, and a mutation deleting it survived. Here that
// deletion takes the exempt names out of the reconciliation with it, and
// each is then reported as a root document nobody declared.
func reasonedNames(t fenceT, written map[string]string, census string) []string {
	t.Helper()

	names := make([]string, 0, len(written))
	var bare []string
	for name, why := range written {
		names = append(names, name)
		if strings.TrimSpace(why) == "" {
			bare = append(bare, "  "+name)
		}
	}
	sort.Strings(names)
	sort.Strings(bare)

	if len(bare) > 0 {
		require.Fail(t, census+" exempts an entry without saying why",
			"an exemption with no reason cannot be checked against the document later and cannot be seen to\n"+
				"expire, so it reads as a decision while recording none of it. Write down what it rests on:\n"+
				strings.Join(bare, "\n"))
	}
	return names
}

// requireSwept refuses a run that read nothing, ahead of any comparison
// quantified over what it read. requireCensus covers the case where one
// side is a written literal; this one covers the case where both sides are
// built at runtime, or where the written side is a literal the same edit
// can empty.
//
// The argument is the count rather than the collection because the callers
// hold three shapes — a []entityDecoder, a []string and a map[error]bool —
// and the length is what they have in common.
func requireSwept(t fenceT, read int, subject, why string) {
	t.Helper()

	if read == 0 {
		require.Fail(t, subject+" read nothing",
			"a comparison quantified over what a run read is answered by an empty list on both sides, so\n"+
				"emptying what the run reads and what the comparison expects in a single edit reconciles\n"+
				"clean without this arm:\n\n"+why)
	}
}

// fenceT is the part of *testing.T the three helpers above use, narrowed so
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
		name: "requireCensus fails on a census that declares nothing",
		call: func(ft fenceT) {
			requireCensus(ft, nil, map[string]bool{}, "census", "why")
		},
		wantFail: true,
		wantMsg:  []string{"census declares no entry"},
	}, {
		name: "requireCensus fails on a name declared twice",
		call: func(ft fenceT) {
			requireCensus(ft, []string{"a", "a"}, map[string]bool{"a": true}, "census", "why")
		},
		wantFail: true,
		wantMsg:  []string{"census names the same entry twice"},
	}, {
		// The empty-census arm's precedence, which its comment asserts and
		// nothing observed: require.Fail ends the run, so only the first arm
		// to fire is ever seen. This input trips two — the census is empty AND
		// the sweep produced an entry nobody declared — so the arm that
		// answers is the arm that ran first.
		//
		// It takes moving the empty check past EVERY other arm to redden this.
		// Moving it below reconcile alone does not: reconcile has no failure of
		// its own, so the arm still runs first among those that can fail.
		name: "requireCensus refuses an empty census ahead of the undeclared arm",
		call: func(ft fenceT) {
			requireCensus(ft, nil, map[string]bool{"b": true}, "census", "why")
		},
		wantFail: true,
		wantMsg:  []string{"census declares no entry"},
	}, {
		name: "reasonedNames passes an exemption that states its reason",
		call: func(ft fenceT) {
			require.Equal(t, []string{"a"},
				reasonedNames(ft, map[string]string{"a": "prints no emitted surface"}, "census"))
		},
	}, {
		// Whitespace is not a reason. A line kept syntactically populated
		// is the shape an exemption takes when its argument is deleted and
		// the entry is not.
		name: "reasonedNames fails an exemption whose reason is blank",
		call: func(ft fenceT) {
			reasonedNames(ft, map[string]string{"a": "why", "b": "  \n\t"}, "census")
		},
		wantFail: true,
		wantMsg:  []string{"census exempts an entry without saying why", "b"},
	}, {
		name: "requireCensusFloors passes a document at its floor",
		call: func(ft fenceT) {
			requireCensusFloors(ft, map[string]int{"a": 2}, map[string]int{"a": 2}, "census", "site", "why")
		},
	}, {
		name: "requireCensusFloors passes a document above its floor",
		call: func(ft fenceT) {
			requireCensusFloors(ft, map[string]int{"a": 2}, map[string]int{"a": 3}, "census", "site", "why")
		},
	}, {
		// The gqlc-0rjn shape, reduced: the document still contributes,
		// so the membership half above is satisfied and only the count
		// fell. Both numbers are in the message because "fewer than
		// declared" without them is the bare inequality this census was
		// not allowed to become.
		name: "requireCensusFloors fails a document that lost all but one site",
		call: func(ft fenceT) {
			requireCensusFloors(ft, map[string]int{"a": 10}, map[string]int{"a": 1}, "census", "site", "why")
		},
		wantFail: true,
		wantMsg:  []string{"census declares more graded sites than the sweep found", "a: census declares 10 site(s), the sweep graded 1", "why"},
	}, {
		name: "requireCensusFloors reports the document that fell, not one that held",
		call: func(ft fenceT) {
			requireCensusFloors(ft, map[string]int{"a": 2, "b": 2}, map[string]int{"a": 2, "b": 1}, "census", "site", "why")
		},
		wantFail: true,
		wantMsg:  []string{"b: census declares 2 site(s), the sweep graded 1"},
	}, {
		name: "requireCensusFloors fails a floor of zero",
		call: func(ft fenceT) {
			requireCensusFloors(ft, map[string]int{"a": 0}, map[string]int{"a": 1}, "census", "site", "why")
		},
		wantFail: true,
		wantMsg:  []string{"census declares a floor of zero", "a: 0"},
	}, {
		// The membership half is not lost to the floor half: a document
		// that grades nothing is reported as absent, by requireCensus,
		// before any count is consulted.
		name: "requireCensusFloors still fails a declared document the sweep did not grade",
		call: func(ft fenceT) {
			requireCensusFloors(ft, map[string]int{"a": 1, "b": 1}, map[string]int{"a": 1}, "census", "site", "why")
		},
		wantFail: true,
		wantMsg:  []string{"census declares an entry the sweep did not produce", "b"},
	}, {
		name: "requireCensusFloors still fails a document nobody declared",
		call: func(ft fenceT) {
			requireCensusFloors(ft, map[string]int{"a": 1}, map[string]int{"a": 1, "b": 1}, "census", "site", "why")
		},
		wantFail: true,
		wantMsg:  []string{"the sweep produced an entry census does not declare", "b"},
	}, {
		name: "requireCensusFloors fails a census that declares nothing",
		call: func(ft fenceT) {
			requireCensusFloors(ft, nil, map[string]int{}, "census", "site", "why")
		},
		wantFail: true,
		wantMsg:  []string{"census declares no entry"},
	}, {
		// A zero observation is an absence, not a graded site: without
		// this the floor arm would report "declares 1, graded 0" for a
		// document the membership arm owes a clearer failure for.
		name: "requireCensusFloors reads a zero observation as an ungraded document",
		call: func(ft fenceT) {
			requireCensusFloors(ft, map[string]int{"a": 1}, map[string]int{"a": 0}, "census", "site", "why")
		},
		wantFail: true,
		wantMsg:  []string{"census declares an entry the sweep did not produce", "a"},
	}, {
		name: "requireCensusExact passes an entry at its count",
		call: func(ft fenceT) {
			requireCensusExact(ft, map[string]int{"a": 2}, map[string]int{"a": 2}, "census", "site", "why")
		},
	}, {
		// The row that separates requireCensusExact from
		// requireCensusFloors, and the reason both exist: over this
		// input the floor helper PASSES. An exemption that has grown
		// covers a site nobody wrote down.
		name: "requireCensusExact fails an entry the sweep found more of",
		call: func(ft fenceT) {
			requireCensusExact(ft, map[string]int{"a": 2}, map[string]int{"a": 3}, "census", "site", "why")
		},
		wantFail: true,
		wantMsg: []string{
			"census does not agree with the sweep on how many sites it covers",
			"census declares 2 site(s), the sweep found 3", "why",
		},
	}, {
		name: "requireCensusExact fails an entry the sweep found fewer of",
		call: func(ft fenceT) {
			requireCensusExact(ft, map[string]int{"a": 2}, map[string]int{"a": 1}, "census", "site", "why")
		},
		wantFail: true,
		wantMsg:  []string{"census declares 2 site(s), the sweep found 1"},
	}, {
		name: "requireCensusExact reports the entry that moved, not one that held",
		call: func(ft fenceT) {
			requireCensusExact(ft, map[string]int{"a": 2, "b": 2}, map[string]int{"a": 2, "b": 3}, "census", "site", "why")
		},
		wantFail: true,
		wantMsg:  []string{"b — census declares 2 site(s), the sweep found 3"},
	}, {
		name: "requireCensusExact fails a count of zero",
		call: func(ft fenceT) {
			requireCensusExact(ft, map[string]int{"a": 0}, map[string]int{"a": 1}, "census", "site", "why")
		},
		wantFail: true,
		wantMsg:  []string{"census declares a count of zero", "a: 0"},
	}, {
		name: "requireCensusExact still fails a declared entry the sweep did not produce",
		call: func(ft fenceT) {
			requireCensusExact(ft, map[string]int{"a": 1, "b": 1}, map[string]int{"a": 1}, "census", "site", "why")
		},
		wantFail: true,
		wantMsg:  []string{"census declares an entry the sweep did not produce", "b"},
	}, {
		name: "requireCensusExact still fails an entry nobody declared",
		call: func(ft fenceT) {
			requireCensusExact(ft, map[string]int{"a": 1}, map[string]int{"a": 1, "b": 1}, "census", "site", "why")
		},
		wantFail: true,
		wantMsg:  []string{"the sweep produced an entry census does not declare", "b"},
	}, {
		name: "requireCensusExact fails a census that declares nothing",
		call: func(ft fenceT) {
			requireCensusExact(ft, nil, map[string]int{}, "census", "site", "why")
		},
		wantFail: true,
		wantMsg:  []string{"census declares no entry"},
	}, {
		name: "requireSwept passes over a run that read something",
		call: func(ft fenceT) { requireSwept(ft, 1, "the sweep", "why") },
	}, {
		name:     "requireSwept fails on a run that read nothing",
		call:     func(ft fenceT) { requireSwept(ft, 0, "the sweep", "why") },
		wantFail: true,
		wantMsg:  []string{"the sweep read nothing", "why"},
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
		name         string
		text         string
		wantArg      string
		wantAny      bool
		wantExempt   bool
		wantOverlong string
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
		// The seam is past every arity the emitter renders, so it is
		// REPORTED here rather than skipped, and sweepSigs is what
		// decides — against specNonMethodSites — that this particular
		// list belongs to something that is not a query method. Before
		// gqlc-vu7z the scanner dropped it silently, and dropped a
		// drifted `(ctx, minAge int64, locale string)` with it.
		name:         "the driverOrTx.run seam is reported as overlong, not dropped",
		text:         "func (d driverDB) run(ctx context.Context, cypher string, params map[string]any, access neo4j.AccessMode) ([]*neo4j.Record, error) {",
		wantOverlong: runSeamSite,
	}, {
		// gqlc-vu7z's own reproduction: the naive sqlc-shaped signature
		// a reader would plausibly write, carrying the query author's
		// parameter names as separate arguments. The emitter renders two
		// query parameters as one `arg <Method>Params` struct, so this
		// is drift, and it was green at every gate until the arity rule
		// widened.
		name:         "the author's names as separate arguments are drift",
		text:         "func (q *Queries) PeopleOverAge(ctx context.Context, minAge int64, locale string) ([]string, error)",
		wantOverlong: nonMethodSite("PeopleOverAge", "ctx context.Context, minAge int64, locale string"),
	}, {
		// The same list wrapped across lines is the same entry: the
		// paren walk reads through the breaks and the list is collapsed
		// before the census sees it, so a document cannot move a site
		// out of its census entry by reformatting it.
		name:         "a wrapped overlong list collapses to the same entry",
		text:         "func (d driverDB) run(\n    ctx context.Context,\n    cypher string,\n    params map[string]any,\n    access neo4j.AccessMode,\n) ([]*neo4j.Record, error) {",
		wantOverlong: runSeamSite,
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
		// The paren anchor needs no code span, so a code block reads
		// like the prose around it. What a block hides is the
		// parenthesis-less spelling alone (gqlc-cgat).
		name:    "a code block is read on its parentheses",
		text:    "```\nfunc (q *Queries) PersonById(ctx context.Context, id int64) error\n```\n",
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
		// The tolerance gqlc-dhm3 records: the suffix ranges over every
		// digit string, not only the -1 and -2 the documents print, so an
		// interface block showing a third member needs no fence edit.
		name:       "a higher-numbered whole-list placeholder is the same exemption",
		text:       "    <MethodName3>(ctx context.Context<param-list-3>) (<return-3>, error)",
		wantExempt: true,
	}, {
		// The other half of that tolerance: the exemption is the fixed
		// lowercase stem, so a differently-cased near-miss grades as a
		// declaration. Case-folding the match would turn this row red.
		name:    "a differently-cased whole-list spelling is graded, not exempted",
		text:    "func (q *Queries) <MethodName>(ctx context.Context, <Param-list>) (<return>, error) {",
		wantArg: "",
		wantAny: true,
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
			got, exempt, overlong, unclosed := scanSpecSigs("witness.md", tc.text)
			require.Empty(t, unclosed)
			require.Equal(t, tc.wantExempt, len(exempt) > 0, "exempt")
			if tc.wantOverlong == "" {
				require.Empty(t, overlong, "overlong")
			} else {
				require.Len(t, overlong, 1, "overlong")
				require.Equal(t, tc.wantOverlong, overlong[0].list,
					"the verbatim list specNonMethodSites reconciles by identity")
			}
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
		name         string
		text         string
		wantArg      string
		wantAny      bool
		wantOverlong string
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
		// ``x`` and `x` render identically, so the second tick is a
		// cosmetic edit the way dropping the parentheses was.
		name:    "a two-backtick span is the same span",
		text:    "- The parameter list is ``ctx context.Context, minAge int64``.",
		wantArg: "minAge",
		wantAny: true,
	}, {
		// The delimiter is a run of three here too. What keeps a block
		// out is where its run sits, not how long the run is.
		name:    "a three-backtick span inside a line is a span",
		text:    "- The parameter list is ```ctx context.Context, minAge int64```.",
		wantArg: "minAge",
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
		// The span scanner reports an overlong list the way the paren
		// walk does, so the two anchors reach the same population. No
		// bare site in the corpus is overlong today — all five are at
		// arity 1 or 2, measured 2026-09-11 — so this row is the whole
		// witness for that arm, and dropping it from scanBareSigs is
		// green on the corpus (gqlc-vu7z).
		name:         "an overlong list is reported here too, not dropped",
		text:         "the seam takes `ctx context.Context, cypher string, params map[string]any`",
		wantOverlong: nonMethodSite("", "ctx context.Context, cypher string, params map[string]any"),
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
		// The three rows below pin the skip (gqlc-cgat): these lists
		// carry no span of their own, and a block's own delimiter opens
		// none here. Written down rather than left implicit, so a
		// scanner that starts reading blocks fails on the pin.
		name: "a fenced block is not a span",
		text: "```\nctx context.Context, minAge int64\n```\n",
	}, {
		name: "an info string does not change that",
		text: "```go\nctx context.Context, minAge int64\n```\n",
	}, {
		name: "an indented block is not a span either",
		text: "prose:\n\n    ctx context.Context, minAge int64\n\nmore prose\n",
	}, {
		// A fence inside a list item is indented and still a fence.
		name: "an indented fence is still a fence",
		text: "- an example:\n\n  ```\n  ctx context.Context, minAge int64\n  ```\n",
	}, {
		// The block rule is a byte test and this row is the first of the
		// two spellings where it disagrees with cmark-gfm (gqlc-cgat).
		// A backtick fence's info string may not hold a backtick, so this
		// line opens no fence and renders identically to the same list in
		// one backtick — which the first rows above read. Pinned as it
		// stands so that closing the divergence is red here and has to
		// move the floor written in C1 §5.3 with it.
		name: "a line-opening run of three is skipped even when the line closes it",
		text: "```ctx context.Context, minAge int64```\n",
	}, {
		// A tab is indent, so this is an indented code block to a
		// renderer and its run is literal text, not a fence.
		name: "a tab-indented run opens no span either",
		text: "prose:\n\n\t```ctx context.Context, minAge int64\n\nmore prose\n",
	}, {
		// The second divergence, and the reason the first cannot be
		// closed by a better fence rule: the line-opening test is reached
		// only by a run of three or more, so this span is read although a
		// renderer shows it as the contents of an indented code block.
		name:    "a tab-indented single backtick is read regardless",
		text:    "prose:\n\n\t`ctx context.Context, minAge int64`\n\nmore prose\n",
		wantArg: "minAge",
		wantAny: true,
	}, {
		// The block rule takes the block's own delimiter, not what is
		// written inside it: a drifted list printed in a span there
		// grades rather than being skipped for being an example.
		name:    "a span inside a fenced block is read like any other",
		text:    "```\nthe list is `ctx context.Context, minAge int64`\n```\n",
		wantArg: "minAge",
		wantAny: true,
	}} {
		t.Run(tc.name, func(t *testing.T) {
			got, overlong, unclosed := scanBareSigs("witness.md", tc.text)
			require.Empty(t, unclosed)
			if tc.wantOverlong == "" {
				require.Empty(t, overlong, "overlong")
			} else {
				require.Len(t, overlong, 1, "overlong")
				require.Equal(t, tc.wantOverlong, overlong[0].list)
			}
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
		// Two backticks render as one span here too, and the tail inside
		// is the same tail specListRules holds by identity. Two of them
		// in one bullet: the second is only reached if the walk resumes
		// past the whole run that closed the first.
		name: "a two-backtick tail is the same tail",
		text: bullet("— ``, arg <T>`` if one parameter,\n" +
			"  ``, arg <MethodName>Params`` if two-plus."),
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
		sigs, exempt, overlong, unclosed := scanSpecSigs("witness.md", "prose\nRemovePerson(ctx context.Context, arg int64\nmore prose\n")
		require.Empty(t, sigs)
		require.Empty(t, exempt)
		require.Empty(t, overlong)
		require.Len(t, unclosed, 1)
		require.Equal(t, 2, unclosed[0].line)
	})
	t.Run("an unterminated code span is reported, not dropped", func(t *testing.T) {
		sigs, overlong, unclosed := scanBareSigs("witness.md", "prose\nthe list is `ctx context.Context, minAge int64\nmore prose\n")
		require.Empty(t, sigs)
		require.Empty(t, overlong)
		require.Len(t, unclosed, 1)
		require.Equal(t, 2, unclosed[0].line)
	})
	t.Run("a span whose closing run is the wrong length is reported too", func(t *testing.T) {
		sigs, overlong, unclosed := scanBareSigs("witness.md", "prose\nthe list is ``ctx context.Context, minAge int64`\nmore prose\n")
		require.Empty(t, sigs)
		require.Empty(t, overlong)
		require.Len(t, unclosed, 1)
		require.Equal(t, 2, unclosed[0].line)
	})
	// The row above is the shorter-run direction. Pairing by length is two
	// claims, and a longer run closing a shorter opener is the other one:
	// cmark-gfm leaves the backticks below as literal text, printing no
	// code element at all.
	t.Run("a longer closing run does not close a shorter opener", func(t *testing.T) {
		sigs, overlong, unclosed := scanBareSigs("witness.md", "prose\nthe list is `ctx context.Context, minAge int64``\nmore prose\n")
		require.Empty(t, sigs)
		require.Empty(t, overlong)
		require.Len(t, unclosed, 1)
		require.Equal(t, 2, unclosed[0].line)
	})
}

// TestDocFilesRefusesAMissingRoot is the witness for docFiles' existence
// guard, which ADR 0029 decision 7 leaves out of its enumeration of
// witnessed plumbing and which no other test stands in for.
//
// The guard is structurally unable to fail on a clean tree, so the
// corpus cannot pin it: an independent review of PR #804 replaced it
// with `continue` and the whole package stayed green. Synthetic roots
// are what make the failing direction reachable at all.
//
// Both directions are asserted, because the guard is only worth having
// if the walk it guards still does its job. A refusal that also stopped
// collecting the roots that DO exist would satisfy the first half alone.
func TestDocFilesRefusesAMissingRoot(t *testing.T) {
	base := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(base, "docs", "adr"), 0o755))
	for _, path := range []string{"README.md", "docs/one.md", "docs/adr/two.md", "docs/skipped.txt"} {
		require.NoError(t, os.WriteFile(filepath.Join(base, filepath.FromSlash(path)), []byte("x\n"), 0o644))
	}

	t.Run("a root that exists is walked", func(t *testing.T) {
		found, err := collectDocs(base, []string{"docs", "README.md"})
		require.NoError(t, err)
		require.Equal(t, []string{"README.md", "docs/adr/two.md", "docs/one.md"}, slashAll(found),
			"a directory root contributes its .md files at any depth and nothing else, "+
				"and a file root contributes itself")
	})

	t.Run("a root that does not exist is refused, by name", func(t *testing.T) {
		_, err := collectDocs(base, []string{"docs", "gone.md"})
		require.Error(t, err, "a docRoots entry that is not on disk was walked past in silence")
		// Either half of the refusal satisfies this: measured, dropping the
		// %q leaves the name in the wrapped os.Stat error. What must hold is
		// that a reader sees which entry went, not where the message got it.
		require.ErrorContains(t, err, "gone.md",
			"the refusal must name the root that is missing; a reader cannot repair a docRoots "+
				"list from a message that does not say which entry went")
	})

	t.Run("the refusal is not a blanket give-up", func(t *testing.T) {
		found, err := collectDocs(base, []string{"docs"})
		require.NoError(t, err)
		require.NotEmpty(t, found,
			"the walk collects nothing even for a root that exists, so the refusal above proves nothing")
	})
}

// slashAll rewrites collectDocs' output to forward slashes so the
// expectations above read as paths rather than as host separators.
func slashAll(paths []string) []string {
	out := make([]string, len(paths))
	for i, p := range paths {
		out[i] = filepath.ToSlash(p)
	}
	return out
}

// docFiles lists every markdown document under docRoots, named relative
// to repoRoot so the censuses and the failure output speak in paths a
// reader can open.
func docFiles(t *testing.T) []string {
	t.Helper()
	out, err := collectDocs(repoRoot, docRoots)
	require.NoError(t, err)
	return out
}

// collectDocs is docFiles' walk, over an arbitrary base and root list so
// the refusals below can be witnessed on synthetic input.
//
// A root that does not exist is refused rather than skipped. Skipping it
// would be silent in the direction that matters: the censuses redden when
// a root a census NAMES vanishes, so what a skip loses is exactly the
// roots no census names — README.md and CONTEXT.md today — which would
// stop being swept with nothing anywhere reporting it (bd gqlc-ipx6).
//
// Split out as a plain function rather than left as require calls on a
// *testing.T because a require cannot be observed failing: it is
// structurally unable to fail on a clean tree, and an independent review
// of PR #804 measured that replacing it with `continue` left the whole
// package green.
func collectDocs(base string, roots []string) ([]string, error) {
	var out []string
	for _, root := range roots {
		full := filepath.Join(base, root)
		info, err := os.Stat(full)
		if err != nil {
			return nil, fmt.Errorf("docRoots entry %q does not exist: %w", root, err)
		}
		if !info.IsDir() {
			out = append(out, root)
			continue
		}
		if err := filepath.WalkDir(full, func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !entry.IsDir() && strings.HasSuffix(path, ".md") {
				rel, relErr := filepath.Rel(base, path)
				if relErr != nil {
					return relErr
				}
				out = append(out, rel)
			}
			return nil
		}); err != nil {
			return nil, fmt.Errorf("walk docRoots entry %q: %w", root, err)
		}
	}
	sort.Strings(out)
	return out, nil
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
func scanSpecSigs(file, text string) (sigs, exempt, overlong, unclosed []specSig) {
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
		name, verdict := gradeParams(list)
		switch verdict {
		case paramPlaceholder:
			exempt = append(exempt, site)
		case paramNamed:
			site.arg = name
			sigs = append(sigs, site)
		case paramOverlong:
			site.list = nonMethodSite(declName(text, open), canonicalList(list))
			overlong = append(overlong, site)
		case paramUnread:
		}
	}
	return sigs, exempt, overlong, unclosed
}

// TestSpecDeclNameReadsTheNamePosition pins the name half of the
// specNonMethodSites key on synthetic input, because the corpus can only
// exercise the shapes it happens to print: every one of its 13 exempted
// sites is a plain identifier or `ExecuteWrite`'s type-parameter form,
// so the empty-name and unbalanced-bracket arms are unreachable live.
//
// Each row's text ends at the parenthesis the parameter list opens on,
// which is the offset scanSpecSigs reaches this with.
func TestSpecDeclNameReadsTheNamePosition(t *testing.T) {
	for _, tc := range []struct {
		name string
		text string
		want string
	}{
		{"an interface member", "    run(", "run"},
		{"a method with a receiver", "func (d driverDB) run(", "run"},
		{"a generic function's type parameters are stepped over", "func ExecuteWrite[T any](", "ExecuteWrite"},
		{"two type parameters are one list", "func Pick[K comparable, V any](", "Pick"},
		{"an anonymous function literal names the keyword", "a godog step handler (`func(", "func"},
		{"a claim about the emitted surface names the method", "func (q *Queries) PeopleOverAge(", "PeopleOverAge"},
		{"nothing in the name position reads as no name", "the list is (", ""},
		{"a bracket run that never opens fails closed", "T any](", ""},
		{"a parenthesis at the very start of a document", "(", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, declName(tc.text, len(tc.text)-1))
		})
	}
}

// TestSpecCommentedAnchorsReadOnlyWhatARendererHides witnesses the
// absence check gqlc-jnsk asks for on synthetic input, because the
// corpus cannot exercise it: its one real HTML comment carries no
// anchor, so on a clean tree every arm of this reader is unreached and
// TestSpecDocumentsCarryNoGradedAnchorInsideAnHTMLComment passes over an
// empty set whatever the reader does.
//
// Each row states the sites expected as `line:anchor`, so a row that
// reports the right count on the wrong offset is a failure rather than a
// pass.
func TestSpecCommentedAnchorsReadOnlyWhatARendererHides(t *testing.T) {
	for _, tc := range []struct {
		name string
		text string
		want []string
	}{{
		name: "a signature inside a comment is hidden from the reader and reported",
		text: "intro\n<!--\nfunc (q *Queries) A" + ctxAnchor + ", arg int64) error\n-->\n",
		want: []string{"3:" + ctxAnchor},
	}, {
		name: "the same signature outside a comment is the sweeps' business, not this check's",
		text: "intro\nfunc (q *Queries) A" + ctxAnchor + ", arg int64) error\n",
		want: nil,
	}, {
		name: "a comment opener inside a code span opens nothing",
		text: "an HTML comment opens on `" + commentOpen + "` and closes on `" + commentClose + "`.\n" +
			"func (q *Queries) A" + ctxAnchor + ", arg int64) error\n",
		want: nil,
	}, {
		name: "an unclosed comment hides every anchor after it",
		text: "intro\n<!--\nnotes\n\nfunc (q *Queries) A" + ctxAnchor + ", arg int64) error\n",
		want: []string{"5:" + ctxAnchor},
	}, {
		name: "the binding anchor is hidden on the same terms as the signature",
		text: "<!-- params := " + mapAnchor + "\"minAge\": minAge} -->\n",
		want: []string{"1:" + mapAnchor},
	}, {
		name: "a parenthesis-less list inside a comment is reported on its backtick anchor",
		text: "<!-- the list is `" + ctxParam + ", arg int64` -->\n",
		want: []string{"1:" + tickAnchor},
	}, {
		name: "the rule bullet is an anchor too, so a commented bullet cannot pay its census",
		text: "<!--\n- " + paramListTerm + " — `, " + codegen.ParamArg + " <T>` if one parameter.\n-->\n",
		want: []string{"2:" + paramListTerm},
	}, {
		name: "a comment carrying no anchor is left alone",
		text: "<!-- TODO: rewrite this section once the seam lands. -->\n",
		want: nil,
	}, {
		name: "one comment naming two anchors reports both",
		text: "<!--\nfunc (q *Queries) A" + ctxAnchor + ", arg int64) error\nparams := " + mapAnchor + "\"minAge\": minAge}\n-->\n",
		want: []string{"2:" + ctxAnchor, "3:" + mapAnchor},
	}, {
		name: "a closed comment releases the text after it",
		text: "<!-- notes -->\nfunc (q *Queries) A" + ctxAnchor + ", arg int64) error\n<!--\nparams := " + mapAnchor + "\"minAge\": minAge}\n-->\n",
		want: []string{"4:" + mapAnchor},
	}} {
		t.Run(tc.name, func(t *testing.T) {
			var got []string
			for _, sig := range commentedAnchors("doc.md", tc.text) {
				got = append(got, fmt.Sprintf("%d:%s", sig.line, sig.arg))
			}
			require.Equal(t, tc.want, got)
		})
	}
}

// declName is the identifier a document prints immediately before a
// parameter list's opening parenthesis, empty when the list opens after
// something that is not one. It is the name half of the key
// specNonMethodSites exempts a site under (gqlc-yn2l).
//
// A type-parameter list between the name and the paren is stepped over,
// because the corpus prints one: C4 quotes neo4j's
// `func ExecuteWrite[T any](…)`, and reading `]` as the end of the name
// would key that site under the empty name instead of under
// `ExecuteWrite`. A bracket run that never opens leaves the name empty
// rather than guessing, which fails closed — the site is then keyed
// under a name no census entry carries and is reported as drift.
//
// Only the name position is read. A receiver, a `func` keyword and the
// rest of the line are deliberately left out: they are what ADR 0029
// decision 13 measured and declined as a general DISCRIMINATOR, and
// nothing here is discriminating. This narrows a census key.
func declName(text string, open int) string {
	end := open
	if end > 0 && text[end-1] == ']' {
		depth := 0
		for end > 0 {
			end--
			switch text[end] {
			case ']':
				depth++
			case '[':
				depth--
			}
			if depth == 0 {
				break
			}
		}
		if depth != 0 {
			return ""
		}
	}
	start := end
	for start > 0 && isIdentByte(text[start-1]) {
		start--
	}
	return text[start:end]
}

// paramVerdict is what gradeParams made of one parameter list.
type paramVerdict int

const (
	// paramUnread: nothing here to grade — a zero-parameter method, or a
	// span whose second field is not a parameter declaration at all.
	paramUnread paramVerdict = iota
	// paramPlaceholder: a whole-list placeholder stood in for the
	// parameters, so the declaration names no argument and the document
	// owes the `<param-list>` bullet instead.
	paramPlaceholder
	// paramNamed: one named argument after the context parameter, which
	// is the shape the emitter renders and the name is graded.
	paramNamed
	// paramOverlong: more parameters than the emitter renders at any
	// arity. Drift unless specNonMethodSites says the list belongs to
	// something that is not an emitted query method (gqlc-vu7z).
	paramOverlong
)

// gradeParams reads the argument name out of a parameter list's
// contents, reporting what it made of the list. It is the one grading
// step both signature anchors share (ADR 0029 decision 10).
//
// The emitted query method takes two parameters at most, at every arity:
// `arg <T>` for one query parameter, `arg <Method>Params` for two or
// more. That is a CLOSED shape — the emitter has no form taking the
// author's parameters as separate arguments — so a longer list is drift
// and says so, rather than being passed over for being past the arity
// this once read (gqlc-vu7z). The lists in the corpus that are longer
// and are not drift belong to functions that are not emitted query
// methods, and sweepSigs routes them out against specNonMethodSites.
func gradeParams(list string) (string, paramVerdict) {
	params := splitTopLevel(list)
	if len(params) > 2 {
		return "", paramOverlong
	}

	// The parameter the placeholder would be in: whatever trails the
	// context parameter when the template glued it on, or the second
	// parameter when a comma separates them.
	tail := params[len(params)-1]
	if len(params) == 1 {
		tail = ctxParamTail(tail)
		if tail == "" {
			return "", paramUnread // a zero-parameter method has no name to read.
		}
	}
	if listPlaceholderRe.MatchString(tail) {
		return "", paramPlaceholder
	}
	name, gradable := paramName(tail)
	if !gradable {
		return "", paramUnread
	}
	return name, paramNamed
}

// scanBareSigs extracts every parameter list a document prints inside an
// inline code span with its enclosing parentheses left off, and every
// such span that never closes.
//
// The span's backticks are the delimiter at both ends: they open the
// anchor and they terminate the list (ADR 0029 decision 10). The
// delimiter is the whole run of backticks the anchor's tick sits in, and
// only a run of that same length closes it, so a list written inside
// two backticks is read the way one written inside a single backtick is.
//
// A run of three or more opening its line is a fenced code block's
// delimiter, and opens no span here (gqlc-cgat). A code span written
// inside such a block does, so a drifted list printed there as an
// example grades rather than being skipped for being an example, and
// scanSpecSigs' paren walk needs no span at all to reach the block.
//
// So what a code block hides from this sweep is the parenthesis-less
// spelling, which is also what running prose hides (gqlc-e143,
// gqlc-cgat).
//
// That block rule is a byte test, not a parse, and it does not agree
// with a renderer on every spelling. Two divergences are measured,
// both against cmark-gfm, and both are gqlc-cgat:
//
//   - A run of three or more that opens its line but is closed by
//     another run on the same line is a span to CommonMark — a backtick
//     fence's info string may not contain a backtick, so the line opens
//     no fence — and is skipped here. It renders byte-identically to
//     the same list written in one backtick, which is read.
//   - The other direction is not reachable from this rule at all: the
//     line-opening test below is consulted only for a run of three or
//     more, so a list in a single backtick on a tab-indented line is
//     read here while a renderer shows it as the literal contents of an
//     indented code block.
//
// Neither divergence is closed here: this rule is a byte test rather
// than a markdown parse, so a better byte test cannot distinguish either
// spelling from what it already accepts or skips. A parse-based rule
// would close both, and ADR 0042 declines to buy one: telling prose from
// code is the service a parser sells, and no graded site in this corpus
// is in prose — 0 of 79 signature anchors and 0 of 17 binding anchors,
// measured against cmark-gfm — while all three spellings this rule skips
// are unoccupied. Where one of these limits is worth closing, that
// ruling prefers a check that the construct is ABSENT from the swept
// documents over a scanner that interprets it.
func scanBareSigs(file, text string) (sigs, overlong, unclosed []specSig) {
	for _, loc := range tickAnchorRe.FindAllStringIndex(text, -1) {
		open := loc[0]
		start := open
		for start > 0 && text[start-1] == '`' {
			start--
		}
		run := open - start + 1
		if run >= 3 && opensLine(text, start) {
			continue
		}
		site := specSig{
			file: file,
			line: 1 + strings.Count(text[:open], "\n"),
			text: strings.TrimSpace(collapse(lineAt(text, open))),
		}

		closing := closingTicks(text, open+1, run)
		if closing < 0 {
			unclosed = append(unclosed, site)
			continue
		}
		list := collapse(text[open+1 : closing])
		name, verdict := gradeParams(list)
		switch verdict {
		case paramNamed:
			site.arg = name
			site.list = list
			sigs = append(sigs, site)
		case paramOverlong:
			// The parentheses are off here, so there is no name position
			// to read and the key carries the empty one. That is the
			// honest reading — the document printed no name — and it
			// means such a site can only ever be exempted by a census
			// entry written with no name. None is today: all 13 sites the
			// corpus holds are parenthesised.
			site.list = nonMethodSite("", canonicalList(list))
			overlong = append(overlong, site)
		case paramUnread, paramPlaceholder:
		}
	}
	return sigs, overlong, unclosed
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

// jsonScalars are the value words a JSON model shape writes where a
// driver binding writes an expression. They are excluded by name, and
// that exclusion is what makes a brace-less binding scanner possible at
// all: the swept documents state JSON shapes as bare `"key": value`
// spans everywhere — `"nullable": true`, `"directed": bool`,
// `"labels": null` — and a recogniser that read those would redden on
// prose across the corpus, which is why ADR 0029 decision 10 declined
// the symmetric move for bindings and gqlc-offa recorded the hole.
//
// What makes the narrowing sound rather than convenient: a documented
// driver binding's value is an expression in the emitted method's Go
// scope, composed by paramsMapText / argsMapText from codegen.ParamArg,
// so it is an identifier or a selector rooted at one. None of these
// words can be that value, and a binding that drifted to one of them is
// past what this scanner claims to read — a limit narrower than the one
// it replaces, and stated rather than assumed.
//
// Measured over the corpus on this branch: with these excluded, the
// scanner grades three sites, and all three are the exhibits
// specBareBindExhibits names. Without them it grades 60.
var jsonScalars = map[string]bool{
	"true": true, "false": true, "null": true, "nil": true,
	"bool": true, "string": true, "number": true, "object": true, "array": true,
	"int": true, "int8": true, "int16": true, "int32": true, "int64": true,
	"uint": true, "uint8": true, "uint16": true, "uint32": true, "uint64": true,
	"float32": true, "float64": true, "byte": true, "rune": true, "any": true,
}

// scanBareBinds is the second binding anchor gqlc-offa asks for: a
// driver binding stated as a bare code span, with no `map[string]any{`
// around it.
//
// mapAnchor's opening brace is part of that anchor, so the whole of the
// binding sweep was blind to a bullet reading "the driver binding is
// `"minAge": minAge`" — measured live on PR #804, where inserting that
// line into C1 §5.3 left TestSpecParamsMapBindsGeneratorOwnedValue
// green. It documents the capture vector gqlc-lhs3 removed from the
// emitter, in the section gqlc-rz0l exists to correct, and the fence
// said nothing.
//
// The span has to be a pair list and NOTHING else — no `{` opening it,
// no prose around it — and every value in it has to be a Go identifier
// or selector that is not one of jsonScalars. That is what keeps it off
// the JSON model shapes: `{"kind": "expr", …}` opens with a brace,
// `config: <src>: field "version": yaml: …` has a head that is not a
// quoted key, and `"nullable": true` binds a scalar. Both narrowings are
// load-bearing and each is mutated in
// TestSpecBareBindScannerReadsOnlyABindingShapedSpan.
//
// `.list` carries the span verbatim, because specBareBindExhibits
// reconciles the exemptions by identity the way specBareListExhibits
// does for signatures.
func scanBareBinds(file, text string) []bareBindSite {
	var out []bareBindSite
	for _, code := range inlineCodeSpans(text, 0, len(text)) {
		values, ok := bareBindValues(code.text)
		if !ok {
			continue
		}
		site := bareBindSite{
			list: code.text,
			text: strings.TrimSpace(collapse(lineAt(text, code.at))),
		}
		for _, value := range values {
			site.binds = append(site.binds, specSig{
				file: file,
				line: 1 + strings.Count(text[:code.at], "\n"),
				arg:  value,
				list: code.text,
				text: site.text,
			})
		}
		out = append(out, site)
	}
	return out
}

// bareBindSite is one such span and the bindings read out of it. The
// span is the unit the exemption is granted in — a span carrying two
// pairs is one exhibit, not two — so the bindings are kept under it
// rather than flattened before the census sees them.
//
// text is the source line the span opened on, carried here rather than
// read back off the first binding because a span whose values are all
// unreadable still has to be able to claim its exemption.
type bareBindSite struct {
	list  string
	text  string
	binds []specSig
}

// bareBindValues reads a code span as a comma-separated list of
// `"key": <expr>` pairs, returning each value reduced the way
// scanSpecBinds reduces one inside a literal. It reports false unless
// every entry of the span is such a pair, which is the whole of what
// keeps this scanner off the corpus's JSON shapes.
func bareBindValues(span string) ([]string, bool) {
	entries := splitTopLevel(strings.TrimSpace(span))
	if len(entries) == 0 {
		return nil, false
	}
	values := make([]string, 0, len(entries))
	for _, entry := range entries {
		entry = strings.TrimSpace(entry)
		if !strings.HasPrefix(entry, `"`) {
			return nil, false
		}
		closing := strings.IndexByte(entry[1:], '"')
		if closing < 0 {
			return nil, false
		}
		rest := strings.TrimSpace(entry[closing+2:])
		if !strings.HasPrefix(rest, ":") {
			return nil, false
		}
		value := unwrapConversions(strings.TrimSpace(rest[1:]))
		if !isBindableExpr(value) {
			return nil, false
		}
		values = append(values, value)
	}
	return values, true
}

// isBindableExpr reports whether a value could be the expression an
// emitted method passes to the run seam: an identifier or a selector
// rooted at one, and not a JSON scalar.
func isBindableExpr(value string) bool {
	if value == "" || jsonScalars[value] {
		return false
	}
	c := value[0]
	isLetter := ('a' <= c && c <= 'z') || ('A' <= c && c <= 'Z')
	if !isLetter && c != '_' {
		return false
	}
	for i := 0; i < len(value); i++ {
		if !isIdentByte(value[i]) && value[i] != '.' {
			return false
		}
	}
	return true
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

// commentOpen and commentClose delimit an HTML comment. It is the one
// construct in this corpus that a markdown renderer hides from the
// reader while leaving its bytes in the file for a byte scanner to read,
// so it is the one place a document's rendered text and the text this
// fence sweeps can disagree (bd gqlc-jnsk).
const (
	commentOpen  = "<!--"
	commentClose = "-->"
)

// htmlCommentSpans returns the half-open byte ranges of text's HTML
// comments.
//
// A `<!--` inside an inline code span opens nothing: a renderer prints
// that run verbatim as content. Skipping those is not a refinement, it
// is what keeps this reader off `docs/adr/0042-the-spec-fence-stays-a-
// byte-scan.md`, which discusses the construct by quoting it three
// times in code spans; read naively, the first quote would open a
// "comment" running hundreds of lines to the second.
//
// An unclosed `<!--` runs to the end of the document. That is the
// renderer's own reading — it hides everything after — and it is also
// the fail-closed one here: the alternative, treating an unterminated
// opener as no comment at all, would mean a document could hide an
// arbitrary amount of graded text from both the reader and this check
// by omitting the terminator.
func htmlCommentSpans(text string) [][2]int {
	code := inlineCodeSpans(text, 0, len(text))
	var out [][2]int
	for i := 0; i < len(text); {
		j := strings.Index(text[i:], commentOpen)
		if j < 0 {
			return out
		}
		open := i + j
		if end, ok := spanCovering(code, open); ok {
			i = end
			continue
		}
		k := strings.Index(text[open:], commentClose)
		if k < 0 {
			return append(out, [2]int{open, len(text)})
		}
		i = open + k + len(commentClose)
		out = append(out, [2]int{open, i})
	}
	return out
}

// spanCovering reports the end of the inline code span containing the
// byte at `at`, if one does.
func spanCovering(spans []codeSpan, at int) (int, bool) {
	for _, s := range spans {
		if s.at <= at && at < s.end {
			return s.end, true
		}
	}
	return 0, false
}

// gradedAnchors are the byte runs that carry a site into one of this
// fence's sweeps: the signature anchor, the bare-list anchor, the
// driver-binding anchor, and the bullet term the rule scanner reads.
// They are the same constants the scanners match on, not copies, so an
// anchor that changes spelling changes here too.
//
// scanBareBinds has no entry because it has no anchor — it offers every
// inline code span in the document to bareBindValues — so a commented
// bare binding is the residual this list does not cover (ADR 0029
// decision 15).
var gradedAnchors = []struct {
	name string
	re   *regexp.Regexp
}{
	{ctxAnchor, ctxAnchorRe},
	{tickAnchor, tickAnchorRe},
	{mapAnchor, mapAnchorRe},
	{paramListTerm, anchorPattern(paramListTerm)},
}

// commentedAnchors reports every graded anchor a document buries inside
// an HTML comment, at most one site per anchor per comment: the comment
// is the unit a writer fixes, so naming each of its anchors once is the
// whole of what a failure has to say.
//
// `arg` carries the anchor's spelling rather than an argument name,
// because the finding is that a site exists where no reader can see it —
// nothing has been read out of it, and nothing should be.
func commentedAnchors(file, text string) []specSig {
	var out []specSig
	for _, comment := range htmlCommentSpans(text) {
		body := text[comment[0]:comment[1]]
		for _, anchor := range gradedAnchors {
			loc := anchor.re.FindStringIndex(body)
			if loc == nil {
				continue
			}
			at := comment[0] + loc[0]
			out = append(out, specSig{
				file: file,
				line: 1 + strings.Count(text[:at], "\n"),
				arg:  anchor.name,
				text: strings.TrimSpace(collapse(lineAt(text, at))),
			})
		}
	}
	return out
}

// readDoc reads one swept document, named relative to repoRoot.
func readDoc(t *testing.T, file string) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(repoRoot, file))
	require.NoError(t, err)
	return string(body)
}

// canonicalList is the spelling specNonMethodSites keys an overlong
// parameter list by: its depth-zero entries, each with its own
// whitespace collapsed, rejoined on `, `.
//
// The normalisation is what makes the census entry a claim about the
// PARAMETERS rather than about the formatting around them. gofmt wraps a
// long signature across lines and leaves a trailing comma, and the same
// seam is printed both ways in these documents; keyed on the raw bytes
// those are two entries, so a document reformatting a quote would read
// as one exempted site disappearing and an unrecorded one arriving. It
// is only the exemption census that is canonicalised — a bare list's own
// `list` field stays verbatim, because specBareListExhibits reconciles
// the text a document prints (ADR 0029 decision 10).
func canonicalList(list string) string {
	return strings.Join(splitTopLevel(collapse(list)), ", ")
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
// The numbered suffix ranges over every digit string, not just the -1
// and -2 the interface blocks print, so a third member needs no fence
// edit (gqlc-dhm3). The stem carries the safety: it is the fixed
// lowercase `<param-list`, so no camelCase placeholder — `<bareParam>`,
// the drift this fence exists to catch — can match it, whatever the
// suffix allows.
//
// Every spelling it matches is exempted from grading, and every document
// taking that exemption is reconciled against specListRuleDocs, so the
// numbered form owes the bullet on the same terms as the bare one.
var listPlaceholderRe = regexp.MustCompile(
	`^` + regexp.QuoteMeta(strings.TrimSuffix(paramListPlaceholder, ">")) + `(-\d+)?>$`)

// anchorPattern compiles one anchor literal into a matcher whose
// whitespace is required between two identifier characters and optional
// at every other boundary, so that a wrapped or reflowed parameter list
// still matches.
//
// Why the normalisation is compiled in here rather than applied to the
// documents is ADR 0029 decision 8 (`docs/adr/0029-the-codegen-spec-fence.md`).
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
// a failure can name the line it sits on, and the offset just past its
// closing delimiter so a caller can ask whether some other byte run
// falls inside it.
//
// `end` is past the closing backticks rather than at them, so [at, end)
// covers the delimiters as well as the body: what htmlCommentSpans needs
// to know is whether a run of bytes is inside a span a renderer prints
// verbatim, and the delimiters are part of what it prints.
type codeSpan struct {
	text string
	at   int
	end  int
}

// inlineCodeSpans returns the code runs in text[from:to] up to the first
// run nothing closes, contents collapsed. Runs are paired by length, so
// the prose between two spans is skipped the way a markdown renderer
// skips it.
//
// Stopping at an unpairable run drops the spans after it in that range.
// The one caller is scanParamListRules, whose range is a single bullet,
// so what is dropped is the rest of one bullet rather than the rest of a
// document.
func inlineCodeSpans(text string, from, to int) []codeSpan {
	var out []codeSpan
	bounded := text[:to]
	for i := from; i < to; {
		open := strings.IndexByte(bounded[i:], '`')
		if open < 0 {
			return out
		}
		open += i
		body := open
		for body < to && bounded[body] == '`' {
			body++
		}
		closing := closingTicks(bounded, body, body-open)
		if closing < 0 {
			return out
		}
		i = closing + (body - open)
		out = append(out, codeSpan{text: collapse(bounded[body:closing]), at: open, end: i})
	}
	return out
}

// closingTicks returns the offset of the first run of exactly n
// backticks at or after i, or -1 when nothing at or after i closes a
// span opened by a run of n. A longer or shorter run is content: a
// code span's delimiters pair by length, which is what lets a document
// print a backtick inside a span at all.
func closingTicks(text string, i, n int) int {
	for i < len(text) {
		j := strings.IndexByte(text[i:], '`')
		if j < 0 {
			return -1
		}
		j += i
		end := j
		for end < len(text) && text[end] == '`' {
			end++
		}
		if end-j == n {
			return j
		}
		i = end
	}
	return -1
}

// opensLine reports whether nothing but whitespace precedes text[i] on
// its line, which is where a code fence's delimiter sits. The indent is
// skipped because a fence inside a list item carries one.
//
// Tabs count as indent. A tab-indented run is never a fence to
// cmark-gfm — the line is an indented code block, so the run is literal
// text — and treating it as one keeps this sweep from reading a block
// and naming its delimiter line as the site.
func opensLine(text string, i int) bool {
	indent := text[strings.LastIndexByte(text[:i], '\n')+1 : i]
	return strings.TrimLeft(indent, " \t") == ""
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
