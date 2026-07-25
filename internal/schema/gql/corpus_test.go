package gql

import (
	"bytes"
	"encoding/json"
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
// enumerated from the grammar instead (see corpus_grammar_test.go), and
// TestCorpusGrammarCoverage fails while any reachable rule or token is entered by
// no corpus file. An unclassified grammar branch is a build failure.
//
// Implementing a construct therefore means moving entries from unsupported to
// resolves and bumping wantCorpusResolving. The two numbers below make
// "coverage is increasing" a mechanically checkable claim rather than a feeling.
//
// One trap when authoring files, because it costs nothing at parse time: a COPY OF
// source must be spelled without AS. "CREATE GRAPH TYPE t AS COPY OF other" has no
// syntax error but enters createGraphTypeStatement zero times, because
// graphSource : AS COPY OF graphExpression (GQL.g4:331) matches it as
// createGraphStatement (GQL.g4:313) instead — so the file exercises none of the
// graph type grammar and the corpus asserts nothing about it. Working spellings:
// "COPY OF CURRENT_SCHEMA/gt", "COPY OF HOME_SCHEMA/gt", "COPY OF ./gt",
// "COPY OF /a/b/gt", "COPY OF ../a/gt", "COPY OF $$gt". A bare "COPY OF s/gt" is a
// syntax error either way: an identifier is not a schemaReference (GQL.g4:1469).
//
// Note the doubled sigil. A parameter reference is spelled $$name here, because
// only SUBSTITUTED_PARAMETER_REFERENCE (GQL.g4:3604) is reachable from a schema
// reference; "$name" lexes as GENERAL_PARAMETER_REFERENCE and is a syntax error.
const (
	wantCorpusEntries   = 5
	wantCorpusResolving = 3
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

// corpusEntry classifies one corpus file. There is deliberately no field listing
// the constructs a file covers: coverage is measured from the parse tree, and a
// declared list drifts from the grammar exactly the way the listener did.
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
	// bead is the issue that will make an unsupported entry resolve, or "wontfix".
	// A resolving entry names a bead only when it resolves to a model that is
	// known to be wrong, and then it must pin that model with a golden.
	bead string
	// reason is one line on why an unsupported entry is not supported, or on what
	// a resolving entry's golden gets wrong.
	reason string
	// golden pins the resolved model to a .golden.json beside the file, for the
	// entries where the mapping rather than the acceptance is the point. Worth
	// setting when today's mapping is known to be wrong: the fix's diff then
	// shows the semantic change. Regenerate with -update.
	golden bool
}

// corpusAreas maps each corpus subdirectory to the entries declared for it, one
// area variable per ISO clause so that authors working on different clauses never
// edit the same Go file. A key is the only place a clause directory is named:
// TestCorpusManifest requires every entry in an area to live under its key.
var corpusAreas = map[string][]corpusEntry{
	"12.6-graph-type-statement": corpusArea126GraphTypeStatement,
	"17-references":             corpusArea17References,
	"18.1-nested-graph-type":    corpusArea181NestedGraphType,
	"18.2-node-type":            corpusArea182NodeType,
	"18.3-edge-type":            corpusArea183EdgeType,
	"18.4-label-set":            corpusArea184LabelSet,
	"18.5-property-types":       corpusArea185PropertyTypes,
	"18.6-property-type":        corpusArea186PropertyType,
	"18.7-property-value-type":  corpusArea187PropertyValueType,
	"18.8-binding-table-type":   corpusArea188BindingTableType,
	"18.9-value-type":           corpusArea189ValueType,
	"18.10-field-type":          corpusArea1810FieldType,
}

// corpusManifest flattens corpusAreas into one file-ordered list.
func corpusManifest(t *testing.T) []corpusEntry {
	t.Helper()

	var entries []corpusEntry
	for clause, area := range corpusAreas {
		for _, entry := range area {
			require.Equal(t, clause, path.Dir(entry.file),
				"entry declared in the %q area must live in that directory", clause)
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

	files := make([]string, 0, len(entries))
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
			// wrong. Requiring a golden is what makes the eventual fix show the
			// semantic change as a diff instead of a silent pass.
			if entry.bead != "" {
				require.True(t, entry.golden, "%s: a resolving entry with an open bead must pin its model with a golden", entry.file)
				require.NotEmpty(t, entry.reason, "%s: say what the pinned model gets wrong", entry.file)
			}
		case unsupported:
			require.Error(t, entry.sentinel, "%s: an unsupported entry must name the sentinel Parse returns", entry.file)
			require.Contains(t, allSentinels, entry.sentinel, "%s: sentinel is not one of the parser's sentinels", entry.file)
			require.NotEmpty(t, entry.bead, `%s: an unsupported entry needs the bead that will fix it, or "wontfix"`, entry.file)
			require.NotEmpty(t, entry.reason, "%s: an unsupported entry needs a reason", entry.file)
		default:
			t.Fatalf("%s: outcome is %d, must be resolves or unsupported", entry.file, entry.outcome)
		}
	}

	require.ElementsMatch(t, corpusFiles(t), files,
		"every corpus file needs a manifest entry, and every manifest entry needs a file")

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

			got, parseErr := New().Parse(bytes.NewReader(src))

			switch entry.outcome {
			case resolves:
				require.NoError(t, parseErr)
				assertNothingDropped(t, measureCoverage(t, string(src)), got)
				if entry.golden {
					assertGolden(t, filepath.Join(corpusDir, entry.file)+".golden.json", got)
				}
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

// assertGolden compares a resolved model against its golden file, regenerating
// it under -update like the valid fixtures do.
func assertGolden(t *testing.T, goldenPath string, got schema.Schema) {
	t.Helper()

	want, err := json.MarshalIndent(got, "", "  ")
	require.NoError(t, err)

	if *update {
		require.NoError(t, os.WriteFile(goldenPath, want, 0o644))
		return
	}

	expected, err := os.ReadFile(goldenPath)
	require.NoError(t, err, "missing golden file; run go test -update")
	require.JSONEq(t, string(expected), string(want))
}

// TestCorpusGrammarCoverage is the gate: every parser rule and token reachable
// from a CREATE GRAPH TYPE statement must be entered by some corpus file. It
// makes an unclassified grammar branch a build failure, and its failure message
// is the authoring worklist.
func TestCorpusGrammarCoverage(t *testing.T) {
	want := grammarObligation(t)

	got := newCoverage(parserNameTables())
	for _, file := range corpusFiles(t) {
		src, err := os.ReadFile(filepath.Join(corpusDir, file))
		require.NoError(t, err)
		got.merge(measureCoverage(t, string(src)))
	}

	uncoveredRules := uncovered(want.rules, got.rules)
	uncoveredTokens := uncovered(want.tokens, got.tokens)
	t.Logf("grammar coverage: rules %d/%d, tokens %d/%d",
		len(want.rules)-len(uncoveredRules), len(want.rules),
		len(want.tokens)-len(uncoveredTokens), len(want.tokens))

	if len(uncoveredRules) == 0 && len(uncoveredTokens) == 0 {
		return
	}
	t.Fatalf("%d rules and %d tokens are entered by no corpus file:\n%s",
		len(uncoveredRules), len(uncoveredTokens),
		worklist(t, want, uncoveredRules, uncoveredTokens))
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
func worklist(t *testing.T, o obligation, uncoveredRules, uncoveredTokens []string) string {
	t.Helper()

	sections := scanRuleSections(t)
	const unknownSection = "(no clause heading)"
	section := func(rule string) string {
		if s := sections[rule]; s != "" {
			return s
		}
		return unknownSection
	}

	type bucket struct{ rules, tokens []string }
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
	}
	return out.String()
}
