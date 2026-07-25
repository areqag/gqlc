package gql

import (
	"bytes"
	"io/fs"
	"os"
	"path"
	"path/filepath"
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

// corpusEntry classifies one corpus file. Almost nothing here declares what the
// file covers: coverage is measured from the parse tree, because a declared list
// drifts from the grammar exactly the way the listener did. The one exception is
// covers, and its comment says why it has to be an exception.
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
	// covers tags the flagged alternatives (`nodeTypeImpliedContent#3`) this file
	// exercises. It is the one declared obligation in the manifest, and only
	// because the set of tags is derived from the grammar rather than hand-listed:
	// see flaggedAlternatives for why rule and token coverage cannot demand these.
	// The tags a file claims are checked against that derived set, and the file is
	// required to enter the tagged rule; which alternative of that rule it took is
	// not machine-checked, so a tag is a claim about a file the author has read.
	covers []string
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

// corpusArea is one author's share of the corpus: the ISO clause directories they
// own and the entries they have declared. dirs is what makes the areas
// file-disjoint, and it is the only place a clause directory is named, so a typo in
// an entry's path fails instead of quietly creating a directory.
type corpusArea struct {
	dirs    []string
	entries []corpusEntry
}

// corpusAreas partitions the corpus so that authors never edit the same Go file.
// The split follows the grammar: each of the reachable rules and tokens belongs to
// the clause of the rule that first names it, and the areas are those clauses
// grouped into roughly equal shares. D1 and D2 share 18.9-value-type/ because the
// value type grammar is half the obligation on its own; they are disjoint by file.
var corpusAreas = map[string]corpusArea{
	"A": {
		dirs:    []string{"12.6-graph-type-statement", "17-references", "18.1-nested-graph-type"},
		entries: corpusAreaA,
	},
	"B": {
		dirs:    []string{"18.2-node-type", "18.4-label-set", "18.5-property-types", "18.6-property-type", "18.7-property-value-type"},
		entries: corpusAreaB,
	},
	"C": {
		dirs:    []string{"18.3-edge-type"},
		entries: corpusAreaC,
	},
	"D1": {
		dirs:    []string{"18.9-value-type"},
		entries: corpusAreaD1,
	},
	"D2": {
		dirs:    []string{"18.8-binding-table-type", "18.9-value-type", "18.10-field-type"},
		entries: corpusAreaD2,
	},
}

// corpusManifest flattens corpusAreas into one file-ordered list.
func corpusManifest(t *testing.T) []corpusEntry {
	t.Helper()

	var entries []corpusEntry
	for name, area := range corpusAreas {
		for _, entry := range area.entries {
			require.Contains(t, area.dirs, path.Dir(entry.file),
				"entry declared in area %s must live in a directory that area owns", name)
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
func TestCorpusManifest(t *testing.T) {
	entries := corpusManifest(t)
	flagged := grammarObligation(t).alternatives

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

		tagged := make(map[string]bool, len(entry.covers))
		for _, tag := range entry.covers {
			require.False(t, tagged[tag], "%s: covers %q twice", entry.file, tag)
			tagged[tag] = true
			require.Contains(t, flagged, tag,
				"%s: covers %q, which is not an alternative rule and token coverage fails to demand; tagging one is pointless because the gates already require it",
				entry.file, tag)
		}

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
			assertCovers(t, entry, cov)

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

// assertCovers checks that a file enters the rule behind every alternative it
// tags. Which of that rule's alternatives the parse took is not checked — see
// corpusEntry.covers for why — so this catches only the cheap half of a wrong tag:
// a tag on a file that never reaches the rule at all.
func assertCovers(t *testing.T, entry corpusEntry, cov *coverage) {
	t.Helper()

	for _, tag := range entry.covers {
		rule, _, ok := strings.Cut(tag, "#")
		require.True(t, ok, "covers %q, which is not a rule#N tag", tag)
		require.True(t, cov.rules[rule], "covers %q but never enters %s", tag, rule)
	}
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

// TestCorpusGrammarCoverage is the gate: every parser rule and token reachable
// from a CREATE GRAPH TYPE statement must be entered by some corpus file, and every
// alternative those two cannot demand must be tagged by one. It makes an
// unclassified grammar branch a build failure, and its failure message is the
// authoring worklist.
//
// The three obligations are one test on purpose. Discharging them means writing the
// same files, so splitting them would give two clause-grouped worklists to
// reconcile by hand.
func TestCorpusGrammarCoverage(t *testing.T) {
	want := grammarObligation(t)

	got := newCoverage(parserNameTables())
	for _, file := range corpusFiles(t) {
		src, err := os.ReadFile(filepath.Join(corpusDir, file))
		require.NoError(t, err)
		got.merge(measureCoverage(t, string(src)))
	}

	tagged := make(map[string]bool)
	for _, entry := range corpusManifest(t) {
		for _, tag := range entry.covers {
			tagged[tag] = true
		}
	}

	uncoveredRules := uncovered(want.rules, got.rules)
	uncoveredTokens := uncovered(want.tokens, got.tokens)
	var uncoveredAlts []string
	for _, tag := range want.alternatives {
		if !tagged[tag] {
			uncoveredAlts = append(uncoveredAlts, tag)
		}
	}
	t.Logf("grammar coverage: rules %d/%d, tokens %d/%d, alternatives %d/%d",
		len(want.rules)-len(uncoveredRules), len(want.rules),
		len(want.tokens)-len(uncoveredTokens), len(want.tokens),
		len(want.alternatives)-len(uncoveredAlts), len(want.alternatives))

	if len(uncoveredRules) == 0 && len(uncoveredTokens) == 0 && len(uncoveredAlts) == 0 {
		return
	}
	t.Fatalf("%d rules and %d tokens are entered by no corpus file, and %d alternatives are tagged by none:\n%s",
		len(uncoveredRules), len(uncoveredTokens), len(uncoveredAlts),
		worklist(t, want, uncoveredRules, uncoveredTokens, uncoveredAlts))
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
	return out.String()
}
