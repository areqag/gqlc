package gql

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/areqag/gqlc/internal/schema"
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
// coverage measure. It makes a support regression fail the build, and support
// progress legible. Implementing a construct moves entries from unsupported to
// resolves and bumps wantCorpusResolving.
//
// Two spellings are traps when authoring files, both pinned by
// TestCorpusSpellingTraps: a COPY OF source takes no AS, and a parameter reference
// carries two sigils. Working COPY OF sources are "CURRENT_SCHEMA/gt",
// "HOME_SCHEMA/gt", "./gt", "/a/b/gt", "../a/gt" and "$$gt"; a bare "s/gt" is a
// syntax error, because an identifier is not a schemaReference (GQL.g4:1469).
const (
	wantCorpusEntries   = 9
	wantCorpusResolving = 4
)

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
	// feature is the ISO GQL Annex D conformance feature id (GG01, GV50, ...), or
	// "mandatory" for a construct outside the optional features.
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

// semanticCase is a construct no grammar gate can demand, because what is wrong is
// a combination of alternatives rather than any one of them, and the resolved model
// has nowhere to record the difference. This list is hand-maintained — there is no
// mechanical source for it, which is exactly why it is small and each member cites
// the bead that must change the entry's outcome.
type semanticCase struct {
	file string
	bead string
	why  string
}

var semanticCases = []semanticCase{
	{
		file: "18.3-edge-type/kind_undirected_arc_directed.gql",
		bead: "gqlc-h9n.3",
		why:  "an UNDIRECTED edge kind on a directed arc resolves to the same EdgeType as DIRECTED, because EdgeType has no undirectedness field; the corpus cannot detect the reinterpretation",
	},
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
type corpusArea struct {
	prefixes []string
	entries  []corpusEntry
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
	},
	"B": {
		prefixes: []string{"18.2-node-type/", "18.4-label-set/", "18.5-property-types/", "18.6-property-type/", "18.7-property-value-type/"},
		entries:  corpusAreaB,
	},
	"C": {
		prefixes: []string{"18.3-edge-type/"},
		entries:  corpusAreaC,
	},
	"D1": {
		prefixes: []string{"18.9-value-type/scalar_"},
		entries:  corpusAreaD1,
	},
	"D2": {
		prefixes: []string{"18.8-binding-table-type/", "18.9-value-type/constructed_", "18.10-field-type/"},
		entries:  corpusAreaD2,
	},
}

// corpusManifest flattens corpusAreas into one file-ordered list.
func corpusManifest(t *testing.T) []corpusEntry {
	t.Helper()

	var entries []corpusEntry
	for name, area := range corpusAreas {
		for _, entry := range area.entries {
			require.True(t, slices.ContainsFunc(area.prefixes, func(prefix string) bool {
				return strings.HasPrefix(entry.file, prefix)
			}), "entry %s declared in area %s must match one of that area's prefixes %v", entry.file, name, area.prefixes)
			entries = append(entries, entry)
		}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].file < entries[j].file })
	return entries
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

func TestCorpusManifest(t *testing.T) {
	entries := corpusManifest(t)

	semanticBeads := make(map[string]string, len(semanticCases))
	semanticFiles := make([]string, 0, len(semanticCases))
	for _, sc := range semanticCases {
		require.NotEmpty(t, sc.bead, "%s: a semantic case needs the bead that will fix it", sc.file)
		require.NotEmpty(t, sc.why, "%s: a semantic case needs the reason no gate can demand it", sc.file)
		require.NotContains(t, semanticBeads, sc.file, "duplicate semantic case")
		semanticBeads[sc.file] = sc.bead
		semanticFiles = append(semanticFiles, sc.file)
	}

	files := make([]string, 0, len(entries))
	var wrongModel []string
	resolving := 0
	for _, entry := range entries {
		require.NotContains(t, files, entry.file, "duplicate manifest entry")
		files = append(files, entry.file)

		require.NotEmpty(t, entry.feature, `%s: feature is required (Annex D id, or "mandatory")`, entry.file)

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
			require.Contains(t, allSentinels, entry.sentinel, "%s: sentinel is not one of the parser's sentinels", entry.file)
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
	require.Len(t, entries, wantCorpusEntries, "corpus size changed; repin wantCorpusEntries")
	require.Equal(t, wantCorpusResolving, resolving,
		"resolving count changed; repin wantCorpusResolving (a drop is a regression)")
}

// TestCorpusOutcomes asserts each entry's outcome, and for resolving entries that
// nothing the source declared was dropped from the model.
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

// TestCorpusShapes reports the direct-child sequences the corpus produced per rule.
// It does not gate: while the corpus is being authored in parallel a new shape is
// the expected outcome of a new file, so a pinned set would fail on every commit.
// Promoting it to a gate with a checked-in artefact is gqlc-h9n.13.
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
