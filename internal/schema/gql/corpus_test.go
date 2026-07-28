package gql

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/antlr4-go/antlr/v4"
	"github.com/stretchr/testify/require"

	"github.com/areqag/gqlc/internal/grammar/gql/gen"
	"github.com/areqag/gqlc/internal/schema"
	"github.com/areqag/gqlc/internal/schema/gql/annexd"
)

// The corpus is a third fixture category beside valid/ and invalid/: every file
// in it is syntactically valid ISO GQL, and the manifest below states, for each,
// whether this parser resolves it today or rejects it with a named sentinel. A
// syntax error in a corpus file is a test failure, not an outcome — syntax
// negatives belong in invalid/.
//
// It exists because three separate defects shared one shape: a grammar
// alternative the listener neither handled nor rejected, which parsed, collected
// nothing, and returned an empty schema with a nil error. Per-bug tests catch
// none of that, because nobody knows to write them. So the obligation is
// enumerated from the grammar instead (see corpus_grammar_test.go): every
// reachable rule, every reachable token, and every alternative those two cannot
// demand. TestCorpusGrammarCoverage fails while any of the three is undischarged.
// Those gates are what make ISO surface coverage complete and non-drifting — an
// unclassified grammar branch is a build failure.
//
// The two counts below do something narrower, and it is worth being exact about
// which: the gates are green with every entry unsupported, so the ratio is not a
// coverage measure. It makes support progress legible — implementing a construct
// moves entries from unsupported to resolves and bumps wantCorpusResolving.
//
// It does not make a support regression fail the build. Both counts are computed
// from the outcomes the manifest declares and never run the resolver, so no change
// to resolving code can move either one: forcing the edge type guard in resolve.go
// to reject, or making propertytype.go accept the RECORD value type it declines,
// leaves TestCorpusSize green and reddens TestCorpusOutcomes, which is the test that
// executes each file against the outcome its entry declares. A drop here is an
// author demoting an entry, not the resolver losing a construct.
//
// What wantCorpusEntries does catch, and nothing else does, is a file deleted from
// disk and from the manifest in one change. TestCorpusManifest's bijection is an
// ElementsMatch over the two sides, so it stays satisfied when both shrink together,
// and the coverage gates are content to be discharged by whatever files remain. The
// count is then the only assertion left that the corpus was ever larger — the same
// argument wantSemanticCases makes below, holding here for the same reason.
//
// Two spellings are traps when authoring files, both pinned by
// TestCorpusSpellingTraps: a COPY OF source takes no AS, and a parameter reference
// carries two sigils. Working COPY OF sources are "CURRENT_SCHEMA/gt",
// "HOME_SCHEMA/gt", "./gt", "/a/b/gt", "../a/gt" and "$$gt"; a bare "s/gt" is a
// syntax error, because an identifier is not a schemaReference (GQL.g4:1469).
//
// wantSemanticCases is the same kind of pin for the third count, and it is the only
// thing that makes the number of declared blind spots anything other than
// self-declared: deleting a semanticCase row along with its entry's bead and reason
// is otherwise green, so a case that was never written and one that was quietly
// removed look identical. A drop is legitimate exactly once per case — when the bead
// lands, TestSemanticCaseCollisions goes red, and the row is deleted with this number
// — which is why it is a pin to repin rather than a lower bound.
//
// What the manifest deliberately does NOT do (gqlc-h9n.24). `resolves` says the
// file produced a schema and no error. It does not say which schema, so a change
// that makes a fixture resolve to a different but still plausible model — INT(8)
// moving off TypeInt8, an alias repointed — leaves every count here green. Two
// beads have now independently expected otherwise, so this is written down: the
// corpus is a coverage instrument, not a model-identity instrument, and that is
// on purpose rather than unfinished.
//
// The alternative was a `model:` field, or golden model snapshots per file.
// Golden snapshots are ruled out on evidence, not cost — gqlc-exl's standing
// argument is that round-tripping launders current behaviour into "expected", and
// -update silently rebases thereafter, which is a worse instrument than none. A
// `model:` field is opt-in, so it is only as good as the rows that opt in, and the
// row that matters is the one whose author did not think identity was at stake.
//
// So model identity is pinned where the model lives, by hand-written tables next
// to the code that produces it, and the discipline that used to rely on the author
// remembering is mechanical: TestTypeSpellingsEveryRowPinned fails if a
// typeSpellings row is added or repointed with no end-to-end pin behind it.
// semanticCase covers the other half — a fixture that resolves to a model known to
// be wrong — and TestSemanticCaseCollisions asserts those. Neither reads its
// expectations off the map under test, which is what keeps them evidence.
const (
	wantCorpusEntries   = 115
	wantCorpusResolving = 58
	wantSemanticCases   = 17
)

// isValidFeature reports whether v is an accepted value of corpusEntry.feature:
// one of the two special-case tokens, or an Annex D optional-feature code from
// the vendored ISO list. A closed check rather than a documented one, because
// "do not cite an Annex D id" was a rule with no mechanism, and it was violated
// three times in nine entries before anyone noticed. Now an id that is not in
// annexd.Codes is rejected mechanically, so a fabricated one (e.g. GG99, which
// is not in the vendored snapshot) cannot land whatever the author intended.
func isValidFeature(v string) bool {
	return v == "mandatory" || v == "unsourced" || annexd.Has(v)
}

const corpusDir = fixtureDir + "/corpus"

// outcome is what Parse does with a corpus file. It has exactly two values, and
// the zero value is neither, so an entry that omits it fails the manifest check
// rather than defaulting into the weaker assertion.
type outcome int

const (
	// resolves: Parse returns a model and a nil error.
	resolves outcome = iota + 1
	// unsupported: Parse returns the entry's sentinel.
	unsupported
)

// corpusEntry classifies one corpus file. Nothing here declares what the file
// covers: coverage is measured from the parse tree, because a declared list drifts
// from the grammar exactly the way the listener did.
type corpusEntry struct {
	// file is the path under corpusDir, e.g. "18.2-node-type/pattern_bare.gql".
	file    string
	outcome outcome
	// sentinel is required iff outcome is unsupported, and must be one of
	// allSentinels so the corpus cannot pin to an ad-hoc error.
	sentinel error
	// feature classifies the construct's Annex D status. Three accepted shapes:
	//
	//   - "mandatory": declining this is non-conformant. On a resolves entry it
	//     is free; on an unsupported entry it declares a known conformance gap,
	//     and bead is what closes it.
	//   - an Annex D optional-feature code (e.g. "GG02", "GH02"): the construct
	//     is an ISO GQL optional feature, so declining it is conformant. The
	//     accepted set is annexd.Codes, sourced from ISO's normative XML
	//     digital-artefact — see internal/schema/gql/annexd/SOURCE.md.
	//   - "unsourced": we do not know. This is honest ONLY for a construct
	//     declined permanently, where the claim being made is "declining this
	//     is still conformant" — the one thing no snapshot of the standard can
	//     support without knowing the construct.
	//
	// isValidFeature is the mechanism. An id not in annexd.Codes (e.g. a
	// plausible but fabricated "GG99") is rejected, which closes the class where
	// the fabricator invents a code that does not exist at all. It does NOT
	// close the class where a real ISO code is applied to the wrong construct —
	// the earlier fabrications caught both: GG02 was cited on LIKE and GE03 on
	// undirected patterns; the codes are real but they name different
	// constructs, and only per-file research against the standard catches that.
	// So citing a real code on the right construct is on the author, not on
	// this guard.
	feature string
	// bead is the issue that will make an unsupported entry resolve. A construct
	// declined permanently names gqlc-0ri, the epic's ADR bead, rather than a
	// magic string: that bead cannot close without accounting for every entry
	// pointing at it, which is what stops a decline from becoming the
	// undocumented rejection this epic exists to close. A resolving entry names a
	// bead only when it resolves to a model that is known to be wrong.
	bead string
	// reason is one line on why an unsupported entry is not supported, or on what
	// a resolving entry gets wrong.
	reason string
}

// semanticCase is a corpus file that parses, resolves, and is still wrong. No
// grammar gate can catch one, and whether a gate demands the file is beside the
// point: the undirected-arc case is undemandable, being a wrong combination of
// alternatives each fine on its own, while the DECIMAL case is demanded loudly,
// since precision and scale are reachable through no other construct. Coverage
// proves which alternatives were taken, never that the model they resolved to is
// right, so both are equally invisible to it.
//
// What makes them invisible is that the model has no field for what was discarded,
// so the wrong result is byte-identical to the right one. EdgeType has no
// undirectedness field, so an UNDIRECTED arc resolves as DIRECTED; PropertyType has
// no length field, so DECIMAL(10,2) resolves as bare DECIMAL.
//
// These are hand-maintained — there is no mechanical source for them, which is
// exactly why they are few and each cites the bead that gives the model somewhere
// to record the difference. Each is declared in the area that owns its file rather
// than in one shared list here: TestCorpusManifest requires the two to agree in
// both directions, so a row landing before its .gql file turns the manifest red in
// every author's worktree at once, and a shared list would make every semantic case
// an edit to a file its author does not own.
//
// spelling is what keeps a case honest. file, bead and why are three strings that
// describe a construct without being tied to one, so deleting the construct the case
// exists to record — rewriting CHAR(4) to STRING and leaving the header comment
// claiming otherwise — leaves the whole package green. Coverage cannot object, since
// being invisible to coverage is what makes it a semantic case. Requiring the source
// to contain the spelling is the only thing here that reads the file's content.
//
// siblings is what makes the spelling's job checkable rather than merely present.
// The spelling pin only asks that the construct still appear, so `spelling: "A"` on
// the CHAR(4) case passes while discriminating nothing; and a case could be added
// whose collision was never real. siblings names the other spellings this one must
// currently resolve identically to, and TestSemanticCaseCollisions asserts it. That
// inverts the signal: today a semantic case is a promise that something is broken,
// checked by nobody; asserted, it goes red the day the bead lands and the model
// gains the field, which is when the case should be deleted.
//
// It is a slice, not a scalar, because these are equivalence classes rather than
// pairs. The pre-DURATION rows all pair X(n) against a bare X, which reads as "the
// un-annotated spelling" and would fit a scalar; DURATION does not, since bare
// DURATION is a syntax error (TestPropertyBareDurationRejectedAtParse) and the
// collision is between two qualified spellings. DURATION(DAY TO HOUR) will be a
// third member of that class, and adding it must not mean revisiting the field's
// shape.
//
// The rule for populating it: every spelling the why claims this one collides with
// gets a row here, or the why says why it cannot. The why is the claim, siblings is
// the machine-checkable form of it, and the two drifting apart is the failure mode
// both exist to prevent. The escape is not hypothetical — the DECIMAL row's bare
// sibling is unreachable by substitution because ISO puts notNull inside the
// parenthesised group.
type semanticCase struct {
	file     string
	bead     string
	why      string
	spelling string
	siblings []string
}

// corpusArea is one author's share of the corpus: the path prefixes they own and the
// entries they have declared. prefixes is what makes the areas file-disjoint, and it
// is the only place a clause directory is named, so a typo in an entry's path fails
// instead of quietly creating a directory.
//
// Prefixes rather than whole directories because an oversized clause has to be split
// between two authors, and a prefix set can be checked for disjointness where a
// convention on filenames cannot. Nothing in the corpus nests, so the extra reach of
// HasPrefix over path.Dir equality costs nothing.
//
// The prefixes do a second job, and it runs through a different file, which is what
// makes it easy to miss: areaPrefixNumbers reduces each prefix to its ISO section
// number, and areaOwners hands every grammar name in that section to the areas holding
// it. A prefix is therefore a claim on a clause of the specification and not only on a
// directory, so one that matches no file is not necessarily dead — dropping it hands
// that clause's names to nobody, which the carriage gates report as `unowned` rather
// than as a gap. ownershipOnlyPrefixes registers the prefixes held for that job alone.
type corpusArea struct {
	prefixes []string
	entries  []corpusEntry
	semantic []semanticCase
}

// corpusAreas partitions the corpus so that authors never edit the same Go file.
// The split follows the grammar: each of the reachable rules and tokens belongs to
// the clause of the rule that first names it, and the areas are those clauses
// grouped into roughly equal shares. D1 and D2 split 18.9-value-type/ between them,
// because the value type grammar is half the obligation on its own.
var corpusAreas = map[string]corpusArea{
	"A": {
		prefixes: []string{"12.6-graph-type-statement/", "17-references/", "18.1-nested-graph-type/"},
		entries:  corpusAreaA,
		semantic: semanticAreaA,
	},
	"B": {
		prefixes: []string{"18.2-node-type/", "18.4-label-set/", "18.5-property-types/", "18.6-property-type/", "18.7-property-value-type/"},
		entries:  corpusAreaB,
		semantic: semanticAreaB,
	},
	"C": {
		prefixes: []string{"18.3-edge-type/"},
		entries:  corpusAreaC,
		semantic: semanticAreaC,
	},
	"D1": {
		prefixes: []string{"18.9-value-type/scalar_"},
		entries:  corpusAreaD1,
		semantic: semanticAreaD1,
	},
	"D2": {
		prefixes: []string{"18.8-binding-table-type/", "18.9-value-type/constructed_", "18.10-field-type/"},
		entries:  corpusAreaD2,
		semantic: semanticAreaD2,
	},
}

// requireOwnedByArea fails unless file matches one of the area's prefixes. Entries
// and semantic cases share it because an area's claim on a file is one claim, and a
// semantic case naming a file outside the area is the case nothing else catches: the
// manifest checks it against the entry that names the same bead, which the owning
// author wrote in their own area.
func requireOwnedByArea(t *testing.T, name string, area corpusArea, kind, file string) {
	t.Helper()

	require.True(t, slices.ContainsFunc(area.prefixes, func(prefix string) bool {
		return strings.HasPrefix(file, prefix)
	}), "%s %s declared in area %s must match one of that area's prefixes %v", kind, file, name, area.prefixes)
}

// corpusManifest flattens corpusAreas into one file-ordered list.
func corpusManifest(t *testing.T) []corpusEntry {
	t.Helper()

	var entries []corpusEntry
	for name, area := range corpusAreas {
		for _, entry := range area.entries {
			requireOwnedByArea(t, name, area, "entry", entry.file)
			entries = append(entries, entry)
		}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].file < entries[j].file })
	return entries
}

// semanticCases flattens the areas' semantic cases the same way, so that the
// manifest sees one list regardless of how many areas contributed to it.
func semanticCases(t *testing.T) []semanticCase {
	t.Helper()

	var cases []semanticCase
	for name, area := range corpusAreas {
		// An area whose `semantic:` is missing gets the zero value, and its cases are
		// dropped with nothing to notice: unlike `entries:`, whose absence the
		// file-against-entry match catches because the .gql files are still on disk,
		// a semantic case is anchored only by the bead on its entry, so an area
		// carrying no bead leaves nothing on the other side to differ from. Requiring
		// non-nil costs the nil spelling of an empty list, which is why all five say
		// []semanticCase{}.
		require.NotNil(t, area.semantic, "area %s has no `semantic:` entry in corpusAreas, so its semantic cases are silently dropped", name)
		for _, sc := range area.semantic {
			requireOwnedByArea(t, name, area, "semantic case", sc.file)
			cases = append(cases, sc)
		}
	}
	sort.Slice(cases, func(i, j int) bool { return cases[i].file < cases[j].file })
	return cases
}

// corpusFiles lists every .gql file under corpusDir, relative to it.
func corpusFiles(t *testing.T) []string {
	t.Helper()

	var files []string
	err := filepath.WalkDir(corpusDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || filepath.Ext(p) != ".gql" {
			return nil
		}
		rel, err := filepath.Rel(corpusDir, p)
		if err != nil {
			return err
		}
		files = append(files, filepath.ToSlash(rel))
		return nil
	})
	require.NoError(t, err)
	return files
}

// TestCorpusManifest checks the manifest against the files on disk in both
// directions, and checks each entry states everything its outcome requires.
// Without the file -> manifest half, dropping in an unclassified file would
// silently widen the corpus without asserting anything about it.
// TestCorpusAreasAreDisjoint pins that no area can own a file another area owns. The
// per-entry check in corpusManifest only says an entry matches its own area, which
// would still hold if two areas' prefixes overlapped: both authors would pass locally
// and the collision would surface only when the areas are merged, where the only
// recovery is discarding one author's work. Overlap is a prefix relation between the
// prefixes themselves, so it is checkable without reference to any file.
func TestCorpusAreasAreDisjoint(t *testing.T) {
	type owned struct{ area, prefix string }

	var all []owned
	for name, area := range corpusAreas {
		require.NotEmpty(t, area.prefixes, "area %s owns nothing", name)
		for _, prefix := range area.prefixes {
			require.NotEmpty(t, prefix, "area %s: an empty prefix owns the whole corpus", name)
			all = append(all, owned{area: name, prefix: prefix})
		}
	}

	for i, a := range all {
		for _, b := range all[i+1:] {
			if a.area == b.area {
				continue
			}
			require.False(t, strings.HasPrefix(a.prefix, b.prefix) || strings.HasPrefix(b.prefix, a.prefix),
				"areas %s and %s both own files under %q / %q", a.area, b.area, a.prefix, b.prefix)
		}
	}
}

// ownershipOnlyPrefix is a prefix held for the ownership half of a prefix's job and
// not the file half: it owns its clause's grammar names, and holds no corpus file.
type ownershipOnlyPrefix struct {
	prefix string
	area   string
	why    string
}

// ownershipOnlyPrefixes are the prefixes that own a clause without owning a file. They
// are not a backlog (gqlc-h9n.20). Each names a clause whose productions have no
// surface syntax of their own to write a file against: the syntax is spelled by a
// parent construct that lives in another clause and is covered there, so a file added
// here would parse bytes an existing file already parses, and its manifest row would
// assert nothing the existing row does not.
//
// Deleting these four and running TestCorpusAreaParseCarriers reports fieldType,
// propertyType, propertyTypeList, propertyTypesSpecification and propertyValueType as
// unowned — names no area answers for. That is strictly weaker than the state this
// register describes, which is why the empty prefixes stay.
//
// The register is not a permanent exemption. An entry whose prefix gains a file fails,
// and the fix is to delete the entry rather than to widen it, because a file under the
// prefix is the direct refutation of the reason the entry states.
var ownershipOnlyPrefixes = []ownershipOnlyPrefix{
	{
		prefix: "18.5-property-types/",
		area:   "B",
		why: "The braced block itself: propertyTypesSpecification is LEFT_BRACE propertyTypeList? RIGHT_BRACE, and propertyTypeList the comma-separated repetition inside it (GQL.g4:1691-1697). " +
			"Every element type that declares properties spells both, so 18.2's files carry them already; a file here would be a node type declaration with the clause number changed.",
	},
	{
		prefix: "18.6-property-type/",
		area:   "B",
		why: "propertyType is propertyName typed? propertyValueType (GQL.g4:1701-1703) — one row of the block above, and unspellable outside it. " +
			"Its two interesting choices, the typed? spelling and the value type, are owned by 17-references and by the value-type areas respectively.",
	},
	{
		prefix: "18.7-property-value-type/",
		area:   "B",
		why: "propertyValueType : valueType (GQL.g4:1707-1709) is a pure alias with no token of its own. " +
			"Every spelling that could go in a file here is a value type, and value-type spellings are areas D1 and D2's whole subject.",
	},
	{
		prefix: "18.10-field-type/",
		area:   "D2",
		why: "fieldType is fieldName typed? valueType (GQL.g4:1996-1998), reachable only through binding table types and record types. " +
			"The resolver declines both with ErrUnsupportedType — declinedCarriers files fieldType under that sentinel — so this clause cannot have a resolving carrier at all, and a rejecting file would pin a decline 18.8's files pin already.",
	},
}

// TestEveryAreaPrefixIsBacked pins that a prefix matching no corpus file is a decision
// someone wrote down. The two halves of a prefix's job come apart here: an unbacked
// prefix is invisible to every file-side check, because those all start from a file and
// ask which area owns it, and it is invisible to the carriage gates too, because from
// their side it looks like ordinary ownership. So it reads as an unfinished directory
// to whoever finds it next, and the cheap repair — delete it — is the one that silently
// unowns its clause.
//
// Registering rather than commenting because the register expires on its own: the gate
// below fails the day a file lands under a registered prefix, which is exactly the day
// the entry's reasoning stopped holding.
func TestEveryAreaPrefixIsBacked(t *testing.T) {
	files := corpusFiles(t)

	registered := make(map[string]bool, len(ownershipOnlyPrefixes))
	for _, entry := range ownershipOnlyPrefixes {
		require.NotEmpty(t, entry.why, "ownership-only prefix %q must state why its clause has no file to write", entry.prefix)
		area, ok := corpusAreas[entry.area]
		require.True(t, ok, "ownership-only prefix %q names area %s, which does not exist", entry.prefix, entry.area)
		require.Contains(t, area.prefixes, entry.prefix,
			"ownership-only prefix %q is not one of area %s's prefixes, so it owns nothing and registers nothing", entry.prefix, entry.area)
		require.False(t, registered[entry.prefix], "prefix %q is registered twice", entry.prefix)
		registered[entry.prefix] = true
	}

	for name, area := range corpusAreas {
		for _, prefix := range area.prefixes {
			backing := 0
			for _, file := range files {
				if strings.HasPrefix(file, prefix) {
					backing++
				}
			}
			if registered[prefix] {
				require.Zero(t, backing,
					"prefix %q is registered ownership-only but now backs %d file(s): delete its ownershipOnlyPrefixes entry, because a file under the prefix refutes the reason it states",
					prefix, backing)
				continue
			}
			require.NotZero(t, backing,
				"area %s's prefix %q matches no corpus file: either add one, or register it in ownershipOnlyPrefixes with the reason its clause has no surface syntax of its own",
				name, prefix)
		}
	}
}

// uncommented blanks GQL comments, preserving every other byte and the offsets of
// what is left, so a spelling stays matchable exactly as written — including the
// space in STRING(2, 5).
//
// Via the lexer rather than a scan for "--" because GQL has three comment forms
// (GQL.g4:3746-3750) and the other two are one keystroke away from the one the
// corpus happens to use. Asking the lexer which spans are comments cannot drift
// from the grammar; a hand-rolled scan silently stops discriminating the day
// someone writes // instead.
func uncommented(src string) string {
	lex := gen.NewGQLLexer(antlr.NewInputStream(src))
	lex.RemoveErrorListeners()

	out := []rune(src)
	for _, tok := range lex.GetAllTokens() {
		switch tok.GetTokenType() {
		case gen.GQLLexerBRACKETED_COMMENT, gen.GQLLexerSIMPLE_COMMENT_SOLIDUS, gen.GQLLexerSIMPLE_COMMENT_MINUS:
			for i := tok.GetStart(); i <= tok.GetStop() && i < len(out); i++ {
				out[i] = ' '
			}
		}
	}
	return string(out)
}

func TestCorpusManifest(t *testing.T) {
	entries := corpusManifest(t)

	cases := semanticCases(t)
	semanticBeads := make(map[string]string, len(cases))
	semanticFiles := make([]string, 0, len(cases))
	for _, sc := range cases {
		require.NotEmpty(t, sc.bead, "%s: a semantic case needs the bead that will fix it", sc.file)
		require.NotEmpty(t, sc.why, "%s: a semantic case needs the reason no gate can catch it", sc.file)
		require.NotEmpty(t, sc.spelling, "%s: a semantic case needs the spelling it exists to record", sc.file)
		require.NotContains(t, semanticBeads, sc.file, "duplicate semantic case")

		// Non-empty rather than optional: an empty slice is the escape hatch that
		// puts a case back to prose nothing checks, which is what this field exists
		// to close. Every case today is an X-resolves-equal-to-Y; the one that was
		// not — kind_undirected_arc_directed, accepted where it should be rejected —
		// stopped being a semantic case when gqlc-h9n.3 gave it ErrEdgeKindArcMismatch,
		// so it is now an unsupported entry. Should a sibling-less case ever be
		// wanted again, it needs a second mode here and a reason for it, not a nil.
		require.NotEmpty(t, sc.siblings, "%s: a semantic case needs the spellings its model cannot be told apart from", sc.file)
		for _, sibling := range sc.siblings {
			require.NotEqual(t, sc.spelling, sibling, "%s: a spelling does not collide with itself", sc.file)
		}

		// The only check here that reads the file's GQL. Everything else about a
		// semantic case is prose, so without this the construct can be edited out
		// from under a row that still claims it and nothing goes red. Comments are
		// stripped first because a header quoting the construct would otherwise
		// satisfy the pin on behalf of GQL that no longer contains it — and 7 of the
		// 86 corpus files already quote a spelling verbatim in their header, so that
		// is a convention away, not a contrivance.
		src, err := os.ReadFile(filepath.Join(corpusDir, sc.file))
		require.NoError(t, err, "%s: semantic case names a file that does not exist", sc.file)
		require.Contains(t, uncommented(string(src)), sc.spelling,
			"%s: a semantic case is about a construct the model gets wrong, so the file must still spell %q", sc.file, sc.spelling)

		semanticBeads[sc.file] = sc.bead
		semanticFiles = append(semanticFiles, sc.file)
	}

	files := make([]string, 0, len(entries))
	var wrongModel []string
	resolving := 0
	for _, entry := range entries {
		require.NotContains(t, files, entry.file, "duplicate manifest entry")
		files = append(files, entry.file)

		require.True(t, isValidFeature(entry.feature),
			`%s: feature %q is not accepted; must be "mandatory", "unsourced", or an Annex D optional-feature code from annexd.Codes (see internal/schema/gql/annexd/SOURCE.md)`, entry.file, entry.feature)

		switch entry.outcome {
		case resolves:
			resolving++
			require.NoError(t, entry.sentinel, "%s: a resolving entry has no sentinel", entry.file)
			// An entry that resolves and still names a bead resolves to something
			// wrong, and semanticCases is the only place that is recorded, so the two
			// must agree in both directions.
			if entry.bead != "" {
				require.NotEmpty(t, entry.reason, "%s: say what the resolved model gets wrong", entry.file)
				require.Equal(t, entry.bead, semanticBeads[entry.file],
					"%s: resolves to a wrong model under bead %s, so semanticCases must carry the same bead", entry.file, entry.bead)
				wrongModel = append(wrongModel, entry.file)
			}
		case unsupported:
			require.Error(t, entry.sentinel, "%s: an unsupported entry must name the sentinel Parse returns", entry.file)
			require.True(t, isSentinel(entry.sentinel), "%s: sentinel is not one of the parser's sentinels", entry.file)
			require.NotEmpty(t, entry.bead, "%s: an unsupported entry needs the bead that will fix it, or gqlc-0ri if it is declined permanently", entry.file)
			require.NotEmpty(t, entry.reason, "%s: an unsupported entry needs a reason", entry.file)
		default:
			t.Fatalf("%s: outcome is %d, must be resolves or unsupported", entry.file, entry.outcome)
		}
	}

	require.ElementsMatch(t, corpusFiles(t), files,
		"every corpus file needs a manifest entry, and every manifest entry needs a file")
	require.ElementsMatch(t, semanticFiles, wrongModel,
		"a semantic case is a file that resolves to a wrong model, so it must be a resolving entry naming that bead, and nothing else may be")

	t.Logf("corpus: %d entries, %d resolving", len(entries), resolving)
}

// TestCorpusSize is separate from TestCorpusManifest because the two have different
// lifetimes. The manifest checks are invariants — every file has an entry, every
// unsupported entry names a real sentinel — and hold no matter how the corpus grows.
// These three are pins to repin, stale by design the moment anyone adds a file, so an
// author who adds one must not be told that the manifest is broken. They are not
// regression pins: each reads the manifest, so each moves only when someone edits it.
func TestCorpusSize(t *testing.T) {
	entries := corpusManifest(t)

	resolving := 0
	for _, entry := range entries {
		if entry.outcome == resolves {
			resolving++
		}
	}

	require.Len(t, entries, wantCorpusEntries,
		"corpus size changed; repin wantCorpusEntries (a drop is the one deletion the manifest bijection cannot see, so name the file that went and why)")
	require.Equal(t, wantCorpusResolving, resolving,
		"declared resolving count changed; repin wantCorpusResolving (a drop is an entry demoted in the manifest — TestCorpusOutcomes is what catches the resolver losing a construct)")
	require.Len(t, semanticCases(t), wantSemanticCases,
		"semantic case count changed; repin wantSemanticCases (a drop means a blind spot closed, so say which bead closed it)")
}

// TestSemanticCaseCollisions asserts the thing a semantic case is about, rather than
// the spelling it is written in. A case says the model has no field for what the
// parse discarded, and the observable form of that is a collision: the file and each
// of its siblings differ in source and not in model. TestCorpusManifest already pins
// that the file still spells the construct; this pins that spelling it still makes no
// difference.
//
// The sibling source is the case's own file with the spelling substituted, so the two
// differ in exactly the construct under test and in nothing else — no second corpus
// file to keep in step, and no bare-STRING carrier to invent for the four byte-string
// rows that have none. Substituting into the comment-blanked copy rather than the raw
// bytes keeps a header quoting the spelling from being rewritten into GQL's place;
// blanked comments are spaces, so the base still resolves to what the file does.
//
// Whole-schema equality rather than reaching for the one Property: the claim is that
// the discarded qualifier is unrecoverable downstream, and downstream sees the model,
// not the field someone thought to compare. It is also what makes the failure useful
// when it comes — the diff names the field the model gained.
func TestSemanticCaseCollisions(t *testing.T) {
	for _, sc := range semanticCases(t) {
		t.Run(sc.file, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join(corpusDir, sc.file))
			require.NoError(t, err)
			src := uncommented(string(raw))

			want, err := New().Parse(strings.NewReader(src))
			require.NoError(t, err, "a semantic case is a file that resolves")

			for _, sibling := range sc.siblings {
				t.Run(sibling, func(t *testing.T) {
					variant := strings.ReplaceAll(src, sc.spelling, sibling)
					require.NotEqual(t, src, variant,
						"the spelling is not in the file's GQL, so this compares the file with itself")

					got, err := New().Parse(strings.NewReader(variant))
					require.NoError(t, err, "the sibling spelling must resolve for there to be a collision")
					require.Equal(t, want, got,
						"%s still resolves differently from %s, so the model can tell them apart and %s has closed this blind spot",
						sc.spelling, sibling, sc.bead)
				})
			}
		})
	}
}

// typeArgument matches a parenthesised argument on an uppercase type keyword —
// STRING(0xFF), DEC(8), DURATION(DAY TO SECOND). It deliberately does not try to know
// which keywords are types: a stray match is harmless, because the verdict below comes
// from resolving the file rather than from this pattern. Nested parens are excluded so
// that a match ends at the first close, which keeps element type patterns like
// `(:X)-[:R]->(:Y)` from being read as one enormous argument.
var typeArgument = regexp.MustCompile(`\b([A-Z][A-Z_]*)\(([^()]*)\)`)

// TestNoUndeclaredLossiness closes the direction of the manifest that was open
// (gqlc-eh4): an entry that resolves and names a bead must carry a semanticCases row,
// and nothing else may, but an entry that resolved *lossily* and named no bead faced
// no assertion at all. "I did not think about it" and "I checked, it is lossless" were
// the same manifest, so silence was a valid answer where it should not have been. PR
// #439 shipped five entries through that gap in a single commit; this test, written
// after, found four more that were already in the corpus and had never been noticed.
//
// The check is a search, not a proof. For each parenthesised argument in the file it
// resolves the variant that spells the argument differently. An identical model proves
// the argument said nothing the model records, which is exactly lossiness and exactly
// what semanticCases exists to record. An argument with no rewrite, or one whose rewrite
// does not parse, yields no verdict, so this can never certify a file lossless. It can
// only refuse to let a demonstrated discard go unrecorded, which is the half that was
// missing.
//
// It rewrites rather than removes (gqlc-w2o). Removal was the first shape and it left a
// hole, because a bare spelling is not always grammatical and an ungrammatical variant
// is silence rather than a verdict: `DECIMAL NOT NULL` is not GQL, notNull? sitting
// inside the parenthesised group decimalExactNumericType spells (GQL.g4:1832), so a file
// of that shape could discard its precision indefinitely and never be asked about it.
// A rewrite leaves the argument's syntactic slot alone, so it is grammatical wherever
// the original was. The technique is not new, only mechanical now —
// scalar_decimal_precision_scale.gql's semanticCases row already carries DECIMAL(8) as
// its sibling, hand-picked for exactly this reason.
//
// Removal is gone rather than kept alongside: no type in GQL's vocabulary rejects a
// rewritten argument while accepting a bare one, so it could not reach a case a rewrite
// misses, and a branch that cannot fire is the theatre this manifest is meant to avoid.
//
// What is left is a non-numeric qualifier, whose rewrite nobody can derive — DURATION's
// field list is the one instance, and both its files are registered already.
//
// Entries already declaring a bead are skipped, TestSemanticCaseCollisions being the
// assertion they face instead. Nothing here reads bead or reason on the entries it does
// check: the verdict is a model comparison, so a wrong declaration cannot buy silence.
func TestNoUndeclaredLossiness(t *testing.T) {
	// Witnesses, because once the corpus is clean every file this test examines has
	// nothing to report, and a search that reports nothing looks identical to a search
	// that looks for nothing. Between them they fail if the search stops finding, if it
	// starts reporting whatever it sees, and if the rewrite it reports from stops being
	// a rewrite.
	t.Run("a discarded argument is reported", func(t *testing.T) {
		const src = "CREATE PROPERTY GRAPH TYPE t AS { (:Doc { s :: STRING(5) }) }"
		require.Equal(t, []string{"STRING(5)"}, discardedArguments(t, src),
			"STRING(5) still collides with STRING(1); if it has stopped, gqlc-5md landed and this witness needs a spelling that is still discarded")
	})

	t.Run("a discard only a rewrite can reach is reported", func(t *testing.T) {
		// The case removal could not reach: DECIMAL NOT NULL is a syntax error, so the
		// bare variant never got as far as a comparison. DECIMAL(1,1) NOT NULL is GQL,
		// and it resolves to the same model.
		const src = "CREATE PROPERTY GRAPH TYPE t AS { (:Doc { d :: DECIMAL(10,2) NOT NULL }) }"
		require.Equal(t, []string{"DECIMAL(10,2)"}, discardedArguments(t, src))
	})

	t.Run("an argument with no derivable rewrite yields no verdict", func(t *testing.T) {
		// No rewrite of DAY TO SECOND is derivable, so there is no second model to
		// compare against and no evidence either way. Reporting it would be reporting
		// a spelling never resolved.
		const src = "CREATE PROPERTY GRAPH TYPE t AS { (:Doc { d :: DURATION(DAY TO SECOND) }) }"
		require.Empty(t, discardedArguments(t, src))
	})

	// perturb is witnessed directly because the corpus cannot witness it. No type
	// argument reaches the model at all, so there is no source whose rewrite resolves
	// to a *different* model — a perturb that returned its input unchanged would report
	// every argument it saw and no end-to-end case would notice.
	t.Run("a perturbation differs from what it perturbs", func(t *testing.T) {
		for argument, want := range map[string]string{
			"10,2":  "1,1", // one digit throughout, so scale cannot come to exceed precision
			"1, 1":  "2, 2",
			"0xFF":  "0x1",
			"0b101": "0b1",
			"0o17":  "0o1",
			"0x1":   "0x2",
		} {
			got, ok := perturb(argument)
			require.True(t, ok, argument)
			require.Equal(t, want, got)
		}
	})

	// rewriteArguments is witnessed directly for the reason perturb is, and it is the
	// same reason: no verdict today distinguishes it from a plain string replacement.
	// No type argument reaches the model, so both halves of a collision are discarded
	// and the answer comes out right either way. The verdict starts depending on this
	// when one of them stops being discarded (gqlc-5md), which is also the first point
	// at which the gate has anything to distinguish.
	t.Run("a rewrite leaves a longer keyword ending in the same spelling alone", func(t *testing.T) {
		const src = "CREATE PROPERTY GRAPH TYPE t AS { (:Doc { a :: VARCHAR(10), b :: CHAR(10) }) }"
		require.Equal(t, "CREATE PROPERTY GRAPH TYPE t AS { (:Doc { a :: VARCHAR(10), b :: CHAR(1) }) }",
			rewriteArguments(src, "CHAR(10)", "CHAR(1)"),
			"CHAR(10) occurs inside VARCHAR(10), and only the one typeArgument reported is the argument under test")
	})

	t.Run("a rewrite reaches every occurrence of the spelling itself", func(t *testing.T) {
		const src = "CREATE PROPERTY GRAPH TYPE t AS { (:Doc { a :: CHAR(10), b :: CHAR(10) }) }"
		require.Equal(t, "CREATE PROPERTY GRAPH TYPE t AS { (:Doc { a :: CHAR(1), b :: CHAR(1) }) }",
			rewriteArguments(src, "CHAR(10)", "CHAR(1)"),
			"a copy the substitution did not reach would keep the models apart on its own")
	})

	t.Run("a partly non-numeric argument is declined", func(t *testing.T) {
		for _, argument := range []string{"DAY TO SECOND", "5 OCTETS", ""} {
			_, ok := perturb(argument)
			require.False(t, ok, "%q: rewriting the literals in an argument demonstrates "+
				"nothing about the rest of it, and the verdict is about the whole argument", argument)
		}
	})

	for _, entry := range corpusManifest(t) {
		if entry.outcome != resolves || entry.bead != "" {
			continue
		}

		t.Run(entry.file, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join(corpusDir, entry.file))
			require.NoError(t, err)

			require.Empty(t, discardedArguments(t, uncommented(string(raw))),
				"the model is the same with these arguments spelled differently, so it discards what they say. Give the entry the bead that will carry them and a reason, and add a semanticCases row for each")
		})
	}
}

// discardedArguments returns the parenthesised arguments in src that say nothing the
// resolved model records: rewriting the argument to a different one leaves the model
// identical. Every occurrence of a spelling is rewritten at once, so that a source
// spelling the same argument twice cannot keep the models apart on the copy the
// substitution did not reach. An argument with no derivable rewrite, or one whose
// rewrite does not parse, is skipped rather than reported: there is no model to compare,
// so there is no evidence either way.
func discardedArguments(t *testing.T, src string) []string {
	t.Helper()

	want, err := New().Parse(strings.NewReader(src))
	require.NoError(t, err, "only a source that resolves has a model to compare against")

	var discarded []string
	seen := make(map[string]bool)
	for _, match := range typeArgument.FindAllStringSubmatchIndex(src, -1) {
		spelling, keyword, argument := src[match[0]:match[1]], src[match[2]:match[3]], src[match[4]:match[5]]
		if seen[spelling] {
			continue
		}
		seen[spelling] = true

		// Declining here rather than resolving KEYWORD(): that spelling is not GQL
		// either, so the outcome would be the same today, but by accident of the
		// grammar rather than because there was nothing to compare.
		rewritten, ok := perturb(argument)
		if !ok {
			continue
		}

		variant := rewriteArguments(src, spelling, keyword+"("+rewritten+")")
		if got, err := New().Parse(strings.NewReader(variant)); err == nil && reflect.DeepEqual(want, got) {
			discarded = append(discarded, spelling)
		}
	}
	return discarded
}

// rewriteArguments replaces spelling at every offset the typeArgument scan reported
// it, and nowhere else.
//
// Every reported occurrence rather than the one under test, for the reason above: a
// source spelling the same argument twice would otherwise keep the models apart on
// the copy the substitution did not reach. Nowhere else, because a plain string
// replacement also rewrites the tail of a longer keyword ending in this one. GQL
// spells CHAR and VARCHAR, and BINARY and VARBINARY, and typeArgument's leading \b
// makes VARCHAR(10) one match rather than two — so in a file holding both, CHAR(10)
// occurs inside VARCHAR(10) and the verdict returned under the shorter name would be
// a verdict about both. The direction that bites is silence: if the neighbour records
// its argument and the one under test discards it, the variant's model differs and
// the discard goes unreported, which is the gap gqlc-eh4 opened this test to close.
func rewriteArguments(src, spelling, replacement string) string {
	var out strings.Builder
	end := 0
	for _, at := range typeArgument.FindAllStringIndex(src, -1) {
		if src[at[0]:at[1]] != spelling {
			continue
		}
		out.WriteString(src[end:at[0]])
		out.WriteString(replacement)
		end = at[1]
	}
	out.WriteString(src[end:])
	return out.String()
}

// numericLiteral matches a literal in every radix GQL spells one: 255, 0xFF, 0b101, 0o17.
var numericLiteral = regexp.MustCompile(`0[xXbBoO][0-9A-Fa-f]+|[0-9]+`)

// perturb rewrites a type argument into a different argument occupying the same
// syntactic slot, so that the variant is grammatical wherever the original was. Every
// literal becomes the same digit — DECIMAL(10,2) becomes DECIMAL(1,1), not
// DECIMAL(1,2) — because a per-literal rewrite could invert an ordering the type
// constrains, and DECIMAL(1,2) is rejected for a scale exceeding its precision. The
// second digit is for arguments already spelling the first, which would otherwise be
// rewritten to themselves and collide with the original for that reason alone.
//
// An argument that is not made only of literals and the separators between them is
// declined. Perturbing the literals in DURATION(DAY TO SECOND) would demonstrate
// something about those literals, and the verdict is about the whole argument.
func perturb(argument string) (string, bool) {
	if strings.Trim(numericLiteral.ReplaceAllString(argument, ""), ", \t\r\n") != "" {
		return "", false
	}

	for _, digit := range []string{"1", "2"} {
		rewritten := numericLiteral.ReplaceAllStringFunc(argument, func(literal string) string {
			return radixPrefix(literal) + digit
		})
		if rewritten != argument {
			return rewritten, true
		}
	}
	return "", false
}

func radixPrefix(literal string) string {
	if len(literal) > 2 && literal[0] == '0' && strings.ContainsRune("xXbBoO", rune(literal[1])) {
		return literal[:2]
	}
	return ""
}

// TestCorpusOutcomes asserts each entry's outcome, and for resolving entries that the
// element types the source declared all reached the model. It is a count, not a model
// comparison: what a property type discards is TestNoUndeclaredLossiness' subject.
func TestCorpusOutcomes(t *testing.T) {
	for _, entry := range corpusManifest(t) {
		t.Run(entry.file, func(t *testing.T) {
			src, err := os.ReadFile(filepath.Join(corpusDir, entry.file))
			require.NoError(t, err)

			cov := measureCoverage(t, string(src))

			got, parseErr := New().Parse(bytes.NewReader(src))

			switch entry.outcome {
			case resolves:
				require.NoError(t, parseErr)
				assertNothingDropped(t, cov, got)
			case unsupported:
				require.ErrorIs(t, parseErr, entry.sentinel)
				require.Equal(t, schema.Schema{}, got, "the model must be the zero value on error")
			default:
				t.Fatalf("outcome is %d, must be resolves or unsupported", entry.outcome)
			}
		})
	}
}

// assertNothingDropped is the direct check on the defect class this corpus exists
// for: a listener that ignores a grammar alternative drops the declaration and
// reports nothing, so a nil error alone proves nothing about the model.
//
// Which of the two forms applies is read off the parse tree rather than declared
// per entry, so an entry cannot opt itself into the weaker one.
func assertNothingDropped(t *testing.T, src *coverage, got schema.Schema) {
	t.Helper()

	elements := len(got.Nodes) + len(got.Edges)
	if !src.declaresElements {
		// COPY OF and LIKE name a graph whose element types are not in this file,
		// so there is no count to compare. The model must still be non-empty:
		// resolving one of these to an empty schema is the defect itself, and the
		// equality below would hold vacuously at 0 == 0 and pass it.
		require.NotZero(t, elements,
			"a source that inherits its element types resolved to an empty model")
		return
	}
	require.NotZero(t, src.elementTypes,
		"a nested graph type body cannot be empty; if this trips, the count is wrong")
	require.Equal(t, src.elementTypes, elements,
		"%d element type declarations in the source, %d node and edge types in the model",
		src.elementTypes, elements)
}

// TestCorpusSpellingTraps pins the two spellings described in the package comment,
// because both are silent: one produces a file that parses cleanly and exercises
// none of the graph type grammar, the other differs from the working spelling by a
// single character. A comment alone rots; these fail if the grammar moves.
func TestCorpusSpellingTraps(t *testing.T) {
	t.Run("AS before COPY OF is a different statement", func(t *testing.T) {
		got, errs := walkCoverage(t, "CREATE GRAPH TYPE t AS COPY OF other")
		require.Empty(t, errs, "the trap is that this spelling is valid GQL")
		require.False(t, got.rules[statementRule],
			"graphSource : AS COPY OF graphExpression no longer wins; if the ambiguity is gone, drop this case")
		require.True(t, got.rules["createGraphStatement"], "expected the CREATE GRAPH statement to match instead")
	})

	t.Run("COPY OF without AS is a graph type source", func(t *testing.T) {
		got, errs := walkCoverage(t, "CREATE GRAPH TYPE t COPY OF CURRENT_SCHEMA/gt")
		require.Empty(t, errs)
		require.True(t, got.rules[statementRule])
		require.True(t, got.rules["copyOfGraphType"])
	})

	t.Run("a parameter reference needs both sigils", func(t *testing.T) {
		_, errs := walkCoverage(t, "CREATE GRAPH TYPE t COPY OF $gt")
		require.NotEmpty(t, errs, "only SUBSTITUTED_PARAMETER_REFERENCE is reachable from a schema reference")

		got, errs := walkCoverage(t, "CREATE GRAPH TYPE t COPY OF $$gt")
		require.Empty(t, errs)
		require.True(t, got.tokens["SUBSTITUTED_PARAMETER_REFERENCE"])
	})
}

// TestFabricatedTokenDoesNotScore pins that a token ANTLR invents during error
// recovery is not counted as covered. This source omits its closing brace; recovery
// reports "missing '}'" and hangs an error node in the tree exactly where the
// RIGHT_BRACE belongs. Visiting that node like any other terminal would score
// RIGHT_BRACE for a file that never contained one, which is the gate closing on a
// token no corpus file spells. VisitErrorNode is a no-op for this reason.
//
// Blanking the child name in coverage.children does not cover this case, so the two
// guards are not redundant: nestedGraphTypeSpecification has a single alternative and
// so is absent from the alternative index, meaning nothing about it is attributed and
// the invented brace is invisible to attribution either way.
func TestFabricatedTokenDoesNotScore(t *testing.T) {
	got, errs := walkCoverage(t, "CREATE GRAPH TYPE t { (:A)")

	require.NotEmpty(t, errs, "the premise is that recovery had to invent the brace")
	require.True(t, got.tokens["LEFT_BRACE"], "the brace the source does spell must still score")
	require.False(t, got.tokens["RIGHT_BRACE"], "the invented brace must not score")
}

// TestFabricatedTokenDoesNotAttribute pins the other half of that defence, the half
// TestFabricatedTokenDoesNotScore says it cannot reach. This source omits the closing
// paren of its destination endpoint. Recovery invents a RIGHT_PAREN and hangs an error
// node where it belongs, which leaves destinationNodeTypeReference holding the child
// sequence LEFT_PAREN nodeTypeFiller RIGHT_PAREN — an exact match for its second
// alternative, and a required one. Naming that error node would score an alternative from
// a file that does not parse, and unlike an over-counted token it would score the very
// thing an author is being asked to cover.
//
// The source is chosen so that both endpoints reach the same shape and only one of them
// parses, which is what makes this a test of the blanking rather than of the rule being
// absent from the index.
func TestFabricatedTokenDoesNotAttribute(t *testing.T) {
	got, errs := walkCoverage(t, "CREATE GRAPH TYPE t { (:A), (:B), (:A)-[:R]->(:B }")

	require.NotEmpty(t, errs, "the premise is that recovery had to invent the paren")
	require.True(t, got.alternatives["sourceNodeTypeReference#2"], "the endpoint that does parse must still score")
	require.False(t, got.alternatives["destinationNodeTypeReference#2"], "the recovered endpoint must not score")
}

// TestCorpusGrammarCoverage is the gate: every parser rule and token reachable
// from a CREATE GRAPH TYPE statement must be entered by some corpus file, and every
// alternative those two cannot demand must be taken by one. It makes an
// unclassified grammar branch a build failure, and its failure message is the
// authoring worklist.
//
// The three obligations are one test on purpose. Discharging them means writing the
// same files, so splitting them would give two clause-grouped worklists to
// reconcile by hand.
func TestCorpusGrammarCoverage(t *testing.T) {
	want := grammarObligation(t)
	got := corpusCoverage(t)

	uncoveredRules := uncovered(want.rules, got.rules)
	uncoveredTokens := uncovered(want.tokens, got.tokens)
	var uncoveredAlts []string
	for _, tag := range want.required {
		if !got.alternatives[tag] {
			uncoveredAlts = append(uncoveredAlts, tag)
		}
	}
	t.Logf("grammar coverage: rules %d/%d, tokens %d/%d, alternatives %d/%d",
		len(want.rules)-len(uncoveredRules), len(want.rules),
		len(want.tokens)-len(uncoveredTokens), len(want.tokens),
		len(want.required)-len(uncoveredAlts), len(want.required))

	if len(uncoveredRules) == 0 && len(uncoveredTokens) == 0 && len(uncoveredAlts) == 0 {
		return
	}
	t.Fatalf("%d rules and %d tokens are entered by no corpus file, and %d alternatives are taken by none:\n%s",
		len(uncoveredRules), len(uncoveredTokens), len(uncoveredAlts),
		worklist(t, want, uncoveredRules, uncoveredTokens, uncoveredAlts))
}

// TestAlternativeExemptions sweeps the exemption list for staleness: an alternative
// claimed unreachable that a corpus file turns out to take is an exemption to delete,
// which is how a grammar fix reviving it gets noticed.
//
// The other direction — that the thief named by stolenBy is itself covered — is
// TestCorpusGrammarCoverage's, because requiredAlternatives puts every stolenBy in
// the required set and exemptionDemands gives it the exemption-specific message
// there. Asserting it here as well would fail a second test for the one missing
// file, and the demand belongs in the worklist authors read either way.
func TestAlternativeExemptions(t *testing.T) {
	got := corpusCoverage(t)

	for _, ex := range alternativeExemptions {
		require.False(t, got.alternatives[ex.tag],
			"%s is exempted as unreachable but a corpus file took it; delete the exemption (%s)", ex.tag, ex.bead)
	}
}

// TestExemptionDemands exercises the failure message against exemption list sizes the
// checked-in list does not have. There is one entry today, so the two-entry cases are
// the point: the message must name every affected entry rather than the first, and must
// say nothing at all when the thieves are covered. A second entry arrives with a grammar
// change adding another ordering conflict rather than during authoring, since every
// dischargeable alternative already has a probed spelling.
//
// notWants is what makes the selection tested rather than the formatting. An
// implementation naming every exemption as soon as any one thief is uncovered satisfies
// every positive assertion here, and it is the version with a cost: it reports
// alternatives as blocked that are not, which reads as a reason to widen the harness.
// Both single-thief cases are present so no assertion can be satisfied by position.
func TestExemptionDemands(t *testing.T) {
	two := []alternativeExemption{
		{tag: "a#1", stolenBy: "b#2", bead: "bd-1", why: "x"},
		{tag: "c#3", stolenBy: "d#4", bead: "bd-2", why: "y"},
	}

	for _, tc := range []struct {
		name       string
		exemptions []alternativeExemption
		uncovered  []string
		wants      []string
		notWants   []string
		empty      bool
	}{
		{name: "no exemptions", uncovered: []string{"b#2"}, empty: true},
		{name: "thieves all covered", exemptions: two, uncovered: []string{"e#5"}, empty: true},
		{
			name:       "first thief uncovered names only that entry",
			exemptions: two,
			uncovered:  []string{"b#2"},
			wants:      []string{"b#2", "a#1", "bd-1"},
			notWants:   []string{"c#3", "d#4", "bd-2"},
		},
		{
			name:       "second thief uncovered names only that entry",
			exemptions: two,
			uncovered:  []string{"d#4"},
			wants:      []string{"d#4", "c#3", "bd-2"},
			notWants:   []string{"a#1", "b#2", "bd-1"},
		},
		{
			name:       "both thieves uncovered names both",
			exemptions: two,
			uncovered:  []string{"b#2", "d#4"},
			wants:      []string{"b#2", "a#1", "bd-1", "d#4", "c#3", "bd-2"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := exemptionDemands(tc.exemptions, tc.uncovered)
			if tc.empty {
				require.Empty(t, got)
				return
			}
			for _, want := range tc.wants {
				require.Contains(t, got, want)
			}
			for _, notWant := range tc.notWants {
				require.NotContains(t, got, notWant)
			}
		})
	}
}

// TestRequiredAlternativesDemandsTheThief pins the append at the end of
// requiredAlternatives, which puts every exemption's stolenBy in the required set.
// Against the checked-in list that append is a no-op — today's one thief is itself
// a candidate, so it is already there — which left it deletable with no test
// noticing. That is the case it exists for: if a grammar change moves the thief out
// of the candidate set, the demand must survive the move, or an exemption would
// excuse a spelling nothing exercises at all.
//
// The two cases differ only in whether the thief is a candidate, and expect the same
// required set. That is the assertion: the demand does not depend on the thief being
// invisible, and it is made once either way.
func TestRequiredAlternativesDemandsTheThief(t *testing.T) {
	index := grammarObligation(t).alternatives
	exemptions := []alternativeExemption{{
		tag:      "connectorUndirected#1",
		stolenBy: "connectorPointingRight#1",
		bead:     "gqlc-h9n.15",
		why:      "the checked-in exemption, reused so the tags name real rules the index can resolve",
	}}

	for _, tc := range []struct {
		name      string
		invisible []string
	}{
		{"thief outside the candidate set", []string{"connectorUndirected#1"}},
		{"thief inside it", []string{"connectorUndirected#1", "connectorPointingRight#1"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := requiredAlternatives(tc.invisible, index, exemptions)
			require.NoError(t, err)
			require.Equal(t, []string{"connectorPointingRight#1"}, got,
				"the exempted tag drops out and its thief is demanded exactly once in its place")
		})
	}
}

// TestRequiredAlternativesRejectsMalformedExemptions is the rejection half of the
// test above, and the argument for it is TestIsValidFeature's: a guard on a
// hand-written registry exists for the entry nobody has written yet, so the
// rejection is the half that has to be pinned. All four branches were deletable
// with the suite green, the checked-in list having exactly one well-formed entry.
//
// The stale tag is the one that is a fail-open rather than an integrity check.
// Without it an exemption whose tag stops being a candidate — a grammar change, or
// a typo — marks something the loop over invisible never matches, so required is
// unchanged and the exemption quietly stops excusing what it was written for.
// TestAlternativeExemptions is not the backstop: it asserts no corpus file took the
// exempted tag, and a tag no file can take is uncovered for a stale entry too.
//
// The missing-field disjunction gets a case per disjunct. As one condition it is
// pinned by any one of them, and the fields are not interchangeable — the bead and
// the why are what make an exemption reviewable, which is the whole argument
// alternativeExemptions rests on.
func TestRequiredAlternativesRejectsMalformedExemptions(t *testing.T) {
	index := grammarObligation(t).alternatives

	// The checked-in pair, so every tag names a rule the index can resolve and a
	// case fails for the reason it is named after rather than for an unknown tag.
	const tag, thief = "connectorUndirected#1", "connectorPointingRight#1"
	invisible := []string{tag, thief}
	wellFormed := alternativeExemption{tag: tag, stolenBy: thief, bead: "gqlc-h9n.15", why: "the checked-in exemption"}

	withField := func(mutate func(*alternativeExemption)) []alternativeExemption {
		ex := wellFormed
		mutate(&ex)
		return []alternativeExemption{ex}
	}

	for _, tc := range []struct {
		name       string
		exemptions []alternativeExemption
		wantErr    string
	}{
		{
			name:       "tag is not a candidate",
			exemptions: withField(func(ex *alternativeExemption) { ex.tag = "valueType#3" }),
			wantErr:    "is not a candidate alternative",
		},
		{
			name:       "the same tag exempted twice",
			exemptions: []alternativeExemption{wellFormed, wellFormed},
			wantErr:    "duplicate exemption",
		},
		{
			name:       "no thief",
			exemptions: withField(func(ex *alternativeExemption) { ex.stolenBy = "" }),
			wantErr:    "needs the alternative that takes its input",
		},
		{
			name:       "no bead",
			exemptions: withField(func(ex *alternativeExemption) { ex.bead = "" }),
			wantErr:    "needs the alternative that takes its input",
		},
		{
			name:       "no why",
			exemptions: withField(func(ex *alternativeExemption) { ex.why = "" }),
			wantErr:    "needs the alternative that takes its input",
		},
		{
			name:       "stolen by itself",
			exemptions: withField(func(ex *alternativeExemption) { ex.stolenBy = ex.tag }),
			wantErr:    "cannot be stolen by itself",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := requiredAlternatives(invisible, index, tc.exemptions)
			require.ErrorContains(t, err, tc.wantErr)
			require.Nil(t, got, "a rejected exemption list must yield no required set to act on")
		})
	}

	// And the fixture itself is accepted, so no case above passes because the shape
	// they are all built from was already refused.
	got, err := requiredAlternatives(invisible, index, []alternativeExemption{wellFormed})
	require.NoError(t, err)
	require.Equal(t, []string{thief}, got)
}

// TestIsValidFeature pins the corpusEntry.feature guard: the two special-case
// tokens and at least one real Annex D code are accepted, and a plausible-but-
// fabricated id is rejected. The rejection half is the one that matters — that
// is the class this bead exists to close (bd gqlc-cfj) and a check that admits
// any /^G../ shape would restore the fabrication hole with a regex over it.
func TestIsValidFeature(t *testing.T) {
	for _, v := range []string{"mandatory", "unsourced", "GG02", "GH02", "G002", "GV90"} {
		require.True(t, isValidFeature(v), "isValidFeature(%q) is false; a special-case token or real Annex D code must be accepted", v)
	}

	for _, v := range []string{"GG99", "GX99", "gg02", "GG02 ", "", "Mandatory", "unsourced ", "annexd"} {
		require.False(t, isValidFeature(v), "isValidFeature(%q) is true; a value outside the accepted set must be rejected", v)
	}
}

// TestCorpusShapes reports the direct-child sequences the corpus produced per rule.
// It still does not gate, and gqlc-h9n.13 settled that it should not: a set of
// observed shapes grows with every file, so pinning it fails on every commit that
// adds one, and the artefact stays behind the opt-in flag for reading.
//
// What that bead gated instead is the grammar side. corpus_branch_test.go pins the
// optionality classes GQL.g4 defines — which no corpus file can move — and asks of
// each whether the shapes below exercise both of its branches. Same measurement,
// pinned from the end that holds still.
func TestCorpusShapes(t *testing.T) {
	got := corpusCoverage(t)

	total := 0
	for _, shapes := range got.shapes {
		total += len(shapes)
	}
	t.Logf("parse tree shapes: %d distinct child sequences across %d rules", total, len(got.shapes))

	path := os.Getenv("GQLC_CORPUS_SHAPES")
	if path == "" {
		return
	}
	rules := make([]string, 0, len(got.shapes))
	for rule := range got.shapes {
		rules = append(rules, rule)
	}
	sort.Strings(rules)

	var report strings.Builder
	for _, rule := range rules {
		shapes := make([]string, 0, len(got.shapes[rule]))
		for shape := range got.shapes[rule] {
			shapes = append(shapes, shape)
		}
		sort.Strings(shapes)
		for _, shape := range shapes {
			fmt.Fprintf(&report, "%s: %s\n", rule, shape)
		}
	}
	require.NoError(t, os.WriteFile(path, []byte(report.String()), 0o644))
}

// corpusCoverage is every corpus file's coverage merged.
func corpusCoverage(t *testing.T) *coverage {
	t.Helper()

	ruleNames, symbolicNames := parserNameTables()
	got := newCoverage(ruleNames, symbolicNames, grammarObligation(t).alternatives)
	for _, file := range corpusFiles(t) {
		src, err := os.ReadFile(filepath.Join(corpusDir, file))
		require.NoError(t, err)
		got.merge(measureCoverage(t, string(src)))
	}
	return got
}

// uncovered returns the sorted names in want that are absent from got.
func uncovered(want, got map[string]bool) []string {
	var names []string
	for name := range want {
		if !got[name] {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

// worklist groups uncovered names by the grammar clause that declares them
// (rules) or first names them (tokens), so the report can be split by clause
// across authors. A token is attributed to a rule that references it because a
// token is only reachable through one.
func worklist(t *testing.T, o obligation, uncoveredRules, uncoveredTokens, uncoveredAlts []string) string {
	t.Helper()

	sections := scanRuleSections(t)
	const unknownSection = "(no clause heading)"
	section := func(rule string) string {
		if s := sections[rule]; s != "" {
			return s
		}
		return unknownSection
	}

	type bucket struct{ rules, tokens, alts []string }
	buckets := make(map[string]*bucket)
	at := func(name string) *bucket {
		if buckets[name] == nil {
			buckets[name] = &bucket{}
		}
		return buckets[name]
	}

	for _, rule := range uncoveredRules {
		b := at(section(rule))
		b.rules = append(b.rules, rule)
	}
	for _, token := range uncoveredTokens {
		refs := o.tokenRefs[token]
		b := at(section(refs[0]))
		b.tokens = append(b.tokens, token+" ("+strings.Join(refs, ", ")+")")
	}
	for _, tag := range uncoveredAlts {
		rule, _, _ := strings.Cut(tag, "#")
		b := at(section(rule))
		b.alts = append(b.alts, tag)
	}

	names := make([]string, 0, len(buckets))
	for name := range buckets {
		names = append(names, name)
	}
	sort.Strings(names)

	var out strings.Builder
	for _, name := range names {
		out.WriteString(name + "\n")
		if b := buckets[name]; len(b.rules) > 0 {
			out.WriteString("    rules:  " + strings.Join(b.rules, ", ") + "\n")
		}
		if b := buckets[name]; len(b.tokens) > 0 {
			out.WriteString("    tokens: " + strings.Join(b.tokens, ", ") + "\n")
		}
		if b := buckets[name]; len(b.alts) > 0 {
			out.WriteString("    alts:   " + strings.Join(b.alts, ", ") + "\n")
		}
	}
	out.WriteString(exemptionDemands(alternativeExemptions, uncoveredAlts))
	out.WriteString(authoringGuidance)
	return out.String()
}

// exemptionDemands says why an uncovered alternative is load-bearing for an
// exemption, which the clause listing above cannot show: there the thief is one
// more tag among dozens. It loops rather than reporting the one entry that exists
// today so that a grammar change adding a second ordering conflict needs an entry
// and nothing else.
func exemptionDemands(exemptions []alternativeExemption, uncoveredAlts []string) string {
	uncovered := make(map[string]bool, len(uncoveredAlts))
	for _, tag := range uncoveredAlts {
		uncovered[tag] = true
	}

	var out strings.Builder
	for _, ex := range exemptions {
		if !uncovered[ex.stolenBy] {
			continue
		}
		fmt.Fprintf(&out, "    %s takes the input %s is exempted for (%s), so until a file spells it neither alternative is exercised at all\n",
			ex.stolenBy, ex.tag, ex.bead)
	}
	if out.Len() == 0 {
		return ""
	}
	return "exemptions whose thief is itself uncovered:\n" + out.String()
}

// authoringGuidance rides on the failure message rather than living only in the
// brief. An author reads the brief once and reads this at the moment the cheapest
// wrong move is available to them, which is widening the harness until the tag they
// believe they covered goes green.
const authoringGuidance = `
Each name above needs a corpus file that enters or takes it. Every alternative this
gate demands was probed one at a time before authoring began and has a spelling that
provably reaches it, so a name that stays red on a file you believe covers it is
first evidence about the file: the construct is likely spelled so that prediction
takes a neighbouring alternative instead. Check the file against the spelling in the
brief. If it still looks right, send the file and the tag to team-lead — an
alternative can be unreachable under ALL(*) prediction rather than merely uncovered,
and that is a grammar finding to record as an exemption naming the alternative that
takes its input. The harness is not the thing to change either way.`
