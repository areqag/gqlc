package resolver

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/areqag/gqlc/internal/procsig"
	"github.com/areqag/gqlc/internal/query"
	"github.com/areqag/gqlc/internal/query/cypher"
	"github.com/areqag/gqlc/internal/schema"
	"github.com/areqag/gqlc/internal/schema/gql"
)

const (
	sweepManifestPath = fixtureDir + "/sweep.manifest.tsv"

	verdictAccept = "accept"
	verdictRefuse = "refuse"

	// sentinelNone fills the sentinel column on an accepting cell.
	sentinelNone = "-"
	// sentinelUnmatched marks a refusal satisfying no member of allSentinels.
	// The manifest is keyed by errors.Is against that closed set, so a refusal
	// outside it needs a value of its own rather than an empty column.
	sentinelUnmatched = "(unmatched)"

	// sweepDigestHex is the width of a detail digest. 48 bits over a corpus
	// whose distinct details number in the hundreds; runSweep asserts against a
	// collision rather than assuming it away.
	sweepDigestHex = 12

	// sweepReportExamples caps how many cells each disagreement category
	// prints. A resolver change can move thousands of cells at once.
	sweepReportExamples = 10

	// sweepRegenCmd is the sanctioned way to absorb a diff, and the one string
	// the manifest header, the missing-file message and the failure hint are
	// all written from.
	//
	// -v is load-bearing: go test discards a passing package's output whole —
	// t.Log, stdout and stderr alike — so without it the -update run prints
	// only "ok" and the breakdown of what it absorbed reaches nobody. -run is
	// anchored because it is an unanchored regexp by default.
	sweepRegenCmd = "go test ./internal/resolver/ -run '^TestCorpusSweepManifest$' -update -v"

	// sweepDeltaPath holds the cells whose outcome moves when the corpus is
	// swept under regR7Alt instead of regR7 — the registry axis the manifest
	// holds constant (gqlc-2xkf). Only the differing cells are committed, and
	// only their regR7Alt side: the regR7 side of every one of them is already
	// a row of the manifest under the same key, so carrying it here would
	// duplicate it and move this file every time the manifest moved.
	sweepDeltaPath = fixtureDir + "/sweep.registry-delta.tsv"

	sweepDeltaRegenCmd = "go test ./internal/resolver/ -run '^TestSweepRegistryDelta$' -update -v"
)

// sentinelNames renders each sentinel as the identifier a reader greps for.
// An errors.New value has no name at runtime, and its message text is exactly
// what a diagnostic change is free to rewrite, so the manifest's sentinel
// column carries the identifier instead. Totality against allSentinels runs in
// both directions in TestSweepSentinelNamesAreTotal.
var sentinelNames = map[error]string{
	ErrUnknownLabel:             "ErrUnknownLabel",
	ErrAmbiguousLabel:           "ErrAmbiguousLabel",
	ErrUnknownProperty:          "ErrUnknownProperty",
	ErrOutOfR0Scope:             "ErrOutOfR0Scope",
	ErrUnknownEdge:              "ErrUnknownEdge",
	ErrAmbiguousBinding:         "ErrAmbiguousBinding",
	ErrParameterTypeConflict:    "ErrParameterTypeConflict",
	ErrAmbiguousEdgeOrientation: "ErrAmbiguousEdgeOrientation",
	ErrUnionColumnMismatch:      "ErrUnionColumnMismatch",
	ErrPartBindingTypeConflict:  "ErrPartBindingTypeConflict",
	ErrInvalidEffectTarget:      "ErrInvalidEffectTarget",
	ErrCallArgAssignability:     "ErrCallArgAssignability",
}

// cellKey identifies one cell of the query x schema cross product.
type cellKey struct{ query, schema string }

func (k cellKey) String() string { return k.query + " x " + k.schema }

// sweepCell is one cell's recorded outcome. detail digests the refusal message
// on a refusing cell and the marshalled ValidatedQuery on an accepting one, so
// the two quadrants are compared on their content and not only on their
// verdict.
type sweepCell struct {
	key      cellKey
	verdict  string
	sentinel string
	detail   string
}

// marshalErrPrefix labels the detail of a cell Resolve accepted whose model
// then failed to marshal. Such a cell is recorded and reported as a changed
// detail; the alternative is a harness assertion, which would abort the sweep
// on resolver output and read as a detection.
const marshalErrPrefix = "MARSHAL ERROR: "

// sweepManifest is the committed baseline: one cell per cross-product pair,
// plus the refusal text each refusing cell's detail digest stands for.
type sweepManifest struct {
	cells    map[cellKey]sweepCell
	order    []cellKey
	messages map[string]string
}

func sweepDigest(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])[:sweepDigestHex]
}

// sentinelsOf names every sentinel the error satisfies, joined by "+" in
// allSentinels order. Reporting all of them rather than the first hit means a
// refusal that starts satisfying a second sentinel reads as a changed value.
//
// Every refusing cell of this corpus satisfies exactly one sentinel, and an
// accepting cell is never asked, so the corpus run never reaches the join and
// cannot hold that property.
// TestSentinelsOfNamesEverySentinelSatisfied drives this function directly and
// is what a first-hit-only rewrite dies against.
func sentinelsOf(err error) string {
	var hit []string
	for _, s := range allSentinels {
		if errors.Is(err, s) {
			hit = append(hit, sentinelNames[s])
		}
	}
	if len(hit) == 0 {
		return sentinelUnmatched
	}
	return strings.Join(hit, "+")
}

// sweepCorpus enumerates every fixture query and every fixture schema under
// test/data/resolver, from the valid/ and invalid/ subdirs both. Schemas are
// keyed by subdir-qualified path: the two schemas/ dirs share ten basenames
// and four of those ten differ in bytes — satisfy_singular.gql, social.gql,
// satisfy_plural_edges_reversed_subtype.gql and
// satisfy_plural_edges_inline_subtype.gql — so a basename key would fold two
// different schemas onto one cell. Both counts re-derive, from
// test/data/resolver, with:
//
//	comm -12 <(ls valid/schemas) <(ls invalid/schemas) | tee /dev/stderr |
//		while read -r b; do
//			cmp -s "valid/schemas/$b" "invalid/schemas/$b" || echo "DIFFER $b"
//		done
func sweepCorpus(t *testing.T) (queries, schemas []string) {
	t.Helper()
	for _, sub := range []string{"valid", "invalid"} {
		q, err := filepath.Glob(filepath.Join(fixtureDir, sub, "*.cypher"))
		require.NoError(t, err)
		require.NotEmpty(t, q, "no queries under %s/", sub)
		for _, p := range q {
			queries = append(queries, sub+"/"+filepath.Base(p))
		}
		s, err := filepath.Glob(filepath.Join(fixtureDir, sub, "schemas", "*.gql"))
		require.NoError(t, err)
		require.NotEmpty(t, s, "no schemas under %s/schemas/", sub)
		for _, p := range s {
			schemas = append(schemas, sub+"/schemas/"+filepath.Base(p))
		}
	}
	sort.Strings(queries)
	sort.Strings(schemas)
	return queries, schemas
}

// runSweep resolves every query against every schema under reg. Each query and
// schema is parsed once and reused across its row and column, which is sound
// only while Resolve leaves its arguments alone — TestSweepIsIndependentOfCellOrder
// is what holds that.
//
// reg is handed to the parser and to the resolver both, because the registry
// reaches a cell's outcome by both routes: the parser types a YIELD column from
// it, the resolver checks argument assignability against it. The parse
// assertion below names only the fixture, which is enough while every registry
// this is called with agrees with regR7 on procedure names, arities and result
// field names — the three things collectCall fails on. regR7Alt is derived
// from signaturesR7 so that it does (resolver_test.go).
func runSweep(t *testing.T, queries, schemas []string, reg procsig.Registry) (map[cellKey]sweepCell, []cellKey, map[string]string) {
	t.Helper()

	parsedQ := make(map[string]query.Query, len(queries))
	for _, name := range queries {
		src, err := os.ReadFile(filepath.Join(fixtureDir, name))
		require.NoError(t, err)
		q, err := cypher.New(cypher.WithRegistry(reg)).Parse(bytes.NewReader(src))
		require.NoError(t, err, "query fixture %s must parse", name)
		parsedQ[name] = q
	}

	parsedS := make(map[string]schema.Schema, len(schemas))
	for _, name := range schemas {
		src, err := os.ReadFile(filepath.Join(fixtureDir, name))
		require.NoError(t, err)
		sch, err := gql.New().Parse(bytes.NewReader(src))
		require.NoError(t, err, "schema fixture %s must parse", name)
		parsedS[name] = sch
	}

	cells := make(map[cellKey]sweepCell, len(queries)*len(schemas))
	order := make([]cellKey, 0, len(queries)*len(schemas))
	messages := map[string]string{}
	// record digests text and holds the digest injective over it, so a changed
	// message is a changed detail rather than a silent alias.
	record := func(k cellKey, text string) string {
		id := sweepDigest(text)
		if prev, ok := messages[id]; ok {
			require.Equal(t, prev, text, "digest collision between two texts at %s", k)
		}
		messages[id] = text
		return id
	}
	for _, qn := range queries {
		for _, sn := range schemas {
			k := cellKey{query: qn, schema: sn}
			vq, err := New(parsedS[sn], WithRegistry(reg)).Resolve(parsedQ[qn])
			cells[k] = recordCell(k, vq, err, record)
			order = append(order, k)
		}
	}
	return cells, order, messages
}

// recordCell renders one cell's outcome, taking nothing from the resolver but
// its two return values. Every caller records through this one function, so a
// resolver output that only some of them could serialise cannot make them
// disagree on the same cell.
//
// model is marshalled only on the accepting arm, and a marshal failure becomes
// the cell's detail. Asserting on it instead would abort on the first affected
// cell, which exits nonzero with no category table — indistinguishable at the
// exit code from the sentinel change this sweep exists to report.
func recordCell(k cellKey, model any, err error, record func(cellKey, string) string) sweepCell {
	c := sweepCell{key: k}
	if err != nil {
		c.verdict, c.sentinel = verdictRefuse, sentinelsOf(err)
		c.detail = record(k, err.Error())
		return c
	}
	c.verdict, c.sentinel = verdictAccept, sentinelNone
	b, merr := json.Marshal(model)
	if merr != nil {
		c.detail = record(k, marshalErrPrefix+merr.Error())
		return c
	}
	c.detail = sweepDigest(string(b))
	return c
}

// digestOnly is the recorder for a caller that compares cells against a sweep
// rather than serialising one. It yields the digest runSweep's recorder yields
// for the same text and keeps no copy of it.
func digestOnly(_ cellKey, text string) string { return sweepDigest(text) }

// renderSweepManifest serialises the sweep. The msg block expands each refusal
// digest into the text it stands for, so a reader diffing the file sees what a
// changed digest means without re-running anything. An accepting cell's digest
// stands for the marshalled model and has no msg line unless the model failed
// to marshal: the model is recovered by resolving, not from this file.
//
// msg text is written strconv.Quote'd. The row separators are tab and newline
// and a refusal message is free to contain either, so quoting is what keeps a
// reworded message a data change here instead of a parse failure.
//
// The header's denominator is counted off the rows it heads rather than
// written as prose, so adding a fixture restates it on the next regeneration.
func renderSweepManifest(cells map[cellKey]sweepCell, order []cellKey, messages map[string]string) []byte {
	seenQ, seenS := map[string]bool{}, map[string]bool{}
	var accept, refuse int
	for _, k := range order {
		seenQ[k.query], seenS[k.schema] = true, true
		if cells[k].verdict == verdictAccept {
			accept++
		} else {
			refuse++
		}
	}

	var b strings.Builder
	b.WriteString("# resolver corpus sweep baseline: every fixture query under\n")
	b.WriteString("# test/data/resolver/{valid,invalid}/*.cypher resolved against every fixture\n")
	b.WriteString("# schema under test/data/resolver/{valid,invalid}/schemas/*.gql. Generated\n")
	b.WriteString("# file: edit internal/resolver/sweep_test.go, not this.\n")
	b.WriteString("#\n")
	fmt.Fprintf(&b, "# corpus: %d queries, %d schemas. %d cells: %d accept, %d refuse.\n",
		len(seenQ), len(seenS), len(order), accept, refuse)
	b.WriteString("#\n")
	b.WriteString("# cell rows: query, schema, verdict, sentinel, detail. A refusing cell's\n")
	b.WriteString("# detail digests the error text and is expanded by the strconv.Quote'd msg\n")
	b.WriteString("# row carrying the same digest; an accepting cell's detail digests the\n")
	b.WriteString("# marshalled ValidatedQuery. Comparing this file against a fresh sweep sorts\n")
	b.WriteString("# every disagreement into a category, including the one a verdict-only diff\n")
	b.WriteString("# misses: a cell that refuses before and after under a different sentinel.\n")
	b.WriteString("#\n")
	b.WriteString("# what a row does not carry: the resolved model itself, which an accepting\n")
	b.WriteString("# cell digests and a reader recovers by resolving; the error's dynamic type,\n")
	b.WriteString("# since two errors sharing a text and an errors.Is behaviour are one value\n")
	b.WriteString("# here; and any query or schema outside test/data/resolver.\n")
	b.WriteString("#\n")
	fmt.Fprintf(&b, "# regenerate: %s\n", sweepRegenCmd)

	writeSweepRows(&b, cells, order, messages)
	return []byte(b.String())
}

// writeSweepRows writes the msg block and then the cell block. Both committed
// files are written through it, so they share one row grammar and one parser
// (parseSweepManifest); a second row writer is what would let the registry
// delta drift into a format that file's guards no longer hold.
func writeSweepRows(b *strings.Builder, cells map[cellKey]sweepCell, order []cellKey, messages map[string]string) {
	ids := make([]string, 0, len(messages))
	for id := range messages {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		fmt.Fprintf(b, "msg\t%s\t%s\n", id, strconv.Quote(messages[id]))
	}
	for _, k := range order {
		c := cells[k]
		fmt.Fprintf(b, "cell\t%s\t%s\t%s\t%s\t%s\n", c.key.query, c.key.schema, c.verdict, c.sentinel, c.detail)
	}
}

// sweepT is what the manifest parser reports through. The corpus run passes a
// *testing.T; TestSweepManifestRejectsHandEdits passes a probe that records the
// diagnosis instead of failing, because a row there has to assert which guard
// rejected it and not merely that some guard did.
type sweepT interface {
	require.TestingT
	Helper()
	Fatalf(format string, args ...any)
}

// checkSweepSentinelColumn holds the sentinel column to the names runSweep can
// write. Without it the column is free text on load, so a manifest hand-edited
// to name a sentinel that does not exist compares clean against a sweep that
// never produced it.
func checkSweepSentinelColumn(t sweepT, where, verdict, column string) {
	t.Helper()
	if verdict == verdictAccept {
		require.Equal(t, sentinelNone, column, "%s: accepting cell carries sentinel %q", where, column)
		return
	}
	require.NotEmpty(t, column, "%s: refusing cell carries an empty sentinel column", where)
	if column == sentinelUnmatched {
		return
	}
	known := map[string]bool{}
	for _, s := range allSentinels {
		known[sentinelNames[s]] = true
	}
	for _, name := range strings.Split(column, "+") {
		require.True(t, known[name], "%s: sentinel column names %q, which is no sentinel in allSentinels", where, name)
	}
}

// loadSweepManifest parses the committed baseline.
func loadSweepManifest(t *testing.T) sweepManifest {
	t.Helper()
	return loadSweepFile(t, sweepManifestPath, sweepRegenCmd)
}

// loadSweepFile parses one committed sweep file, naming in the missing-file
// message the command that writes THAT file. A shared message would send a
// reader regenerating the registry delta to the manifest's command, which
// rewrites the wrong file and leaves the failure exactly where it was.
func loadSweepFile(t *testing.T, path, regenCmd string) sweepManifest {
	t.Helper()
	raw, err := os.ReadFile(path)
	require.NoError(t, err, "missing %s; regenerate with %s", path, regenCmd)
	return parseSweepManifest(t, path, raw)
}

// parseSweepManifest re-derives each msg row's digest from its own text. A
// hand-edited message that kept its digest would otherwise read as unchanged
// while the report quoted text no run produced.
//
// The cell/msg correspondence is checked in both directions, as
// TestSweepSentinelNamesAreTotal checks its map: renderSweepManifest writes a
// msg row only for a digest some cell carries, so an uncited msg row is a row
// no sweep produced. Checking only that each refusal cites an existing row
// accepts a file carrying quotable text for cells that are not there.
func parseSweepManifest(t sweepT, path string, raw []byte) sweepManifest {
	t.Helper()
	m := sweepManifest{cells: map[cellKey]sweepCell{}, messages: map[string]string{}}
	for n, line := range strings.Split(strings.TrimRight(string(raw), "\n"), "\n") {
		where := fmt.Sprintf("%s:%d", path, n+1)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		switch {
		case strings.HasPrefix(line, "msg\t"):
			f := strings.SplitN(line, "\t", 3)
			require.Len(t, f, 3, "%s: msg row wants 3 fields", where)
			text, uerr := strconv.Unquote(f[2])
			require.NoError(t, uerr, "%s: msg text is not a quoted Go string", where)
			require.Equal(t, f[1], sweepDigest(text), "%s: msg digest does not match its own text", where)
			_, dup := m.messages[f[1]]
			require.False(t, dup, "%s: duplicate msg digest %s", where, f[1])
			m.messages[f[1]] = text
		case strings.HasPrefix(line, "cell\t"):
			f := strings.Split(line, "\t")
			require.Len(t, f, 6, "%s: cell row wants 6 fields", where)
			require.Contains(t, []string{verdictAccept, verdictRefuse}, f[3], "%s: unrecognised verdict %q", where, f[3])
			checkSweepSentinelColumn(t, where, f[3], f[4])
			k := cellKey{query: f[1], schema: f[2]}
			_, dup := m.cells[k]
			require.False(t, dup, "%s: duplicate cell %s", where, k)
			m.cells[k] = sweepCell{key: k, verdict: f[3], sentinel: f[4], detail: f[5]}
			m.order = append(m.order, k)
		default:
			t.Fatalf("%s: unrecognised row %q", where, line)
		}
	}
	cited := make(map[string]bool, len(m.messages))
	for _, c := range m.cells {
		cited[c.detail] = true
		if c.verdict != verdictRefuse {
			continue
		}
		_, ok := m.messages[c.detail]
		require.True(t, ok, "%s: refusing cell %s cites digest %s with no msg row", path, c.key, c.detail)
	}
	for id := range m.messages {
		require.True(t, cited[id], "%s: msg row %s is cited by no cell", path, id)
	}
	return m
}

// sweepComparison sorts each cell into one bucket. Verdict, then sentinel,
// then detail: the buckets are exclusive and coarsest-first, so a cell whose
// verdict moved is reported as that and not also as a changed sentinel.
//
// differentDetail exists because two refusals carrying one sentinel can still
// differ in which arm of that sentinel's validator spoke — the reason
// invalidFixtureContains pins message substrings that errors.Is cannot tell
// apart.
type sweepComparison struct {
	identical         int
	differentDetail   []string
	differentSentinel []string
	differentVerdict  []string
	onlyInManifest    []string
	onlyInSweep       []string
}

func (c sweepComparison) disagreements() int {
	return len(c.differentDetail) + len(c.differentSentinel) + len(c.differentVerdict) +
		len(c.onlyInManifest) + len(c.onlyInSweep)
}

// summary states the count in every category whether or not any fired, so a
// green run says what it compared rather than only that it found nothing.
//
// against is the file the counts were taken against. It is a parameter and not
// sweepManifestPath because two files are compared this way now, and a report
// naming the wrong one sends a reader to diff a file that did not move.
func (c sweepComparison) summary(against string) string {
	return fmt.Sprintf(
		"sweep vs %s:\n"+
			"  %6d  same verdict, same sentinel, same detail\n"+
			"  %6d  same verdict, same sentinel, DIFFERENT DETAIL (refusal text, or accepted model)\n"+
			"  %6d  same verdict, DIFFERENT SENTINEL\n"+
			"  %6d  DIFFERENT VERDICT\n"+
			"  %6d  cell in manifest, absent from sweep\n"+
			"  %6d  cell in sweep, absent from manifest",
		against, c.identical, len(c.differentDetail), len(c.differentSentinel),
		len(c.differentVerdict), len(c.onlyInManifest), len(c.onlyInSweep))
}

// describeCell quotes the cell's text when the run that produced the cell also
// recorded one. An accepting cell whose model marshalled carries no text, so it
// is described by its digest.
func describeCell(c sweepCell, messages map[string]string) string {
	if msg, ok := messages[c.detail]; ok {
		return fmt.Sprintf("%s %s %q", c.verdict, c.sentinel, msg)
	}
	return fmt.Sprintf("%s %s detail:%s", c.verdict, c.sentinel, c.detail)
}

func compareSweep(want sweepManifest, gotCells map[cellKey]sweepCell, gotOrder []cellKey, gotMsgs map[string]string) sweepComparison {
	var c sweepComparison
	for _, k := range gotOrder {
		got := gotCells[k]
		old, ok := want.cells[k]
		if !ok {
			c.onlyInSweep = append(c.onlyInSweep, fmt.Sprintf("%s: %s", k, describeCell(got, gotMsgs)))
			continue
		}
		line := fmt.Sprintf("%s\n      was: %s\n      now: %s", k,
			describeCell(old, want.messages), describeCell(got, gotMsgs))
		switch {
		case old.verdict != got.verdict:
			c.differentVerdict = append(c.differentVerdict, line)
		case old.sentinel != got.sentinel:
			c.differentSentinel = append(c.differentSentinel, line)
		case old.detail != got.detail:
			c.differentDetail = append(c.differentDetail, line)
		default:
			c.identical++
		}
	}
	for _, k := range want.order {
		if _, ok := gotCells[k]; !ok {
			c.onlyInManifest = append(c.onlyInManifest, fmt.Sprintf("%s: %s", k, describeCell(want.cells[k], want.messages)))
		}
	}
	return c
}

// report is the summary plus every category's cells. Both the failing path and
// the -update path print it: regenerating the manifest absorbs the diff, and a
// regeneration that stated only its counts would absorb a sentinel change
// without ever naming the sentinel.
func (c sweepComparison) report(against string) string {
	return c.summary(against) +
		sweepCategoryDetail("DIFFERENT VERDICT", c.differentVerdict) +
		sweepCategoryDetail("DIFFERENT SENTINEL, same verdict", c.differentSentinel) +
		sweepCategoryDetail("DIFFERENT DETAIL, same verdict and sentinel", c.differentDetail) +
		sweepCategoryDetail("in manifest, absent from sweep", c.onlyInManifest) +
		sweepCategoryDetail("in sweep, absent from manifest", c.onlyInSweep)
}

func sweepCategoryDetail(label string, lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "\n%s (%d):\n", label, len(lines))
	for i, l := range lines {
		if i == sweepReportExamples {
			fmt.Fprintf(&b, "  ... %d more\n", len(lines)-i)
			break
		}
		fmt.Fprintf(&b, "  - %s\n", l)
	}
	return b.String()
}

// TestCorpusSweepManifest resolves the whole query x schema cross product and
// diffs it against the committed baseline, sorting every disagreement into a
// named category. A cell that refuses on both sides under a different sentinel
// gets its own category: comparing verdicts and goldens alone leaves that cell
// looking unchanged, since it moves no verdict and rewrites no golden byte.
//
// Nothing here parses the refusal text. The diff is keyed on errors.Is against
// allSentinels and on digests, so a reworded message is reported as a changed
// detail rather than raising a parse failure the report would be mistaken for.
//
// What a cell carries, and therefore what this bounds:
//   - refusing cell: the sentinel set errors.Is admits, and a digest of
//     err.Error(). Two errors with the same text and the same errors.Is
//     behaviour are one value here whatever their dynamic types.
//   - accepting cell: a digest of the marshalled ValidatedQuery. Each
//     ResolvedType writes its own MarshalJSON, so this column reaches a model
//     field when that MarshalJSON emits it — the same bound the
//     .validated.golden.json files carry (gqlc-gyp5).
//
// The corpus is the fixtures on disk, resolved under regR7, which is the only
// Option the resolver takes (gqlc-2xkf).
//
// What a sentinel SWAP does not add: TestSentinelReachability requires every
// sentinel to have a negative fixture and TestResolverSuite pins that
// fixture's sentinel, so a change swapping a sentinel at every one of its
// fail-sites goes red there too. Measured on this corpus, degrading
// ErrAmbiguousLabel at its only fail-site fails TestResolverSuite and moves
// 299 cells here.
//
// What a sentinel WIDENING adds is detection, and it is the case that argument
// does not cover. Make the same fail-site satisfy one additional sentinel with
// byte-identical text — an errors.Join, or a shared wrapper that starts folding
// in ErrOutOfR0Scope — and every errors.Is(err, ErrAmbiguousLabel) assertion
// stays true, because a widened set still satisfies it and nothing else in the
// package asserts the ABSENCE of a second sentinel. Measured, that mutation's
// complete failure list in this package is this test; TestResolverSuite and
// TestSentinelReachability both pass. The columns move 0 VERDICT, 299 SENTINEL,
// 0 DETAIL, so a cheaper design digesting only the refusal text calls the same
// run green. The sentinel column and the detail digest are independent axes,
// not one axis recorded twice.
//
// What both cases add is the extent. TestResolverSuite resolves the pairs
// schema.mapping.json names, one schema per query — 313 of these 12520 cells
// at this commit — leaving 12207 no other test holds to a committed value.
// Within this file the extent is not what the manifest adds:
// TestSweepReachesEverySentinel and TestSweepIsIndependentOfCellOrder resolve
// all 12520 too, but the first only counts sentinels per name and the second
// only compares a run against a re-parse of itself, so neither holds any cell
// to a committed value. The count is what the sweep this replaces could not
// state: verdicts alone put the same change at 0 differences.
//
// The sentinel-WIDENING mutation's two figures — 299 cells moved, of which 288
// fell outside TestResolverSuite's pairs — were measured on the 11780-cell
// corpus as it stood before gqlc-h6h7 added three queries and two schemas, and
// have NOT been re-run against 12520. What they witness does not depend on the
// total: that mutation rewrites no refusal text and flips no verdict by
// construction, so DETAIL and VERDICT are 0 at any corpus size and the
// sentinel column is the only axis left to move. The live corpus shape is in
// the manifest header, which is regenerated with the file.
func TestCorpusSweepManifest(t *testing.T) {
	queries, schemas := sweepCorpus(t)
	cells, order, msgs := runSweep(t, queries, schemas, regR7)

	if *update {
		if _, err := os.Stat(sweepManifestPath); err == nil {
			t.Log(compareSweep(loadSweepManifest(t), cells, order, msgs).report(sweepManifestPath))
		}
		require.NoError(t, os.WriteFile(sweepManifestPath, renderSweepManifest(cells, order, msgs), 0o644))
		return
	}

	cmp := compareSweep(loadSweepManifest(t), cells, order, msgs)
	t.Log(cmp.report(sweepManifestPath))
	if cmp.disagreements() == 0 {
		return
	}
	t.Errorf("%s\n\nif these changes are intended, regenerate with:\n  %s", cmp.report(sweepManifestPath), sweepRegenCmd)
}

// registryDelta keeps the regR7Alt side of every cell whose (verdict, sentinel,
// detail) triple moved between the two sweeps, in the corpus order, with the
// alt-side text of each digest it kept.
//
// The messages map is filtered to the digests the kept cells cite rather than
// passed through whole: parseSweepManifest rejects a msg row no cell cites, so
// carrying every alt message would make the file it renders unloadable.
func registryDelta(base, alt map[cellKey]sweepCell, order []cellKey, altMsgs map[string]string) (map[cellKey]sweepCell, []cellKey, map[string]string) {
	cells := map[cellKey]sweepCell{}
	kept := make([]cellKey, 0, len(order))
	msgs := map[string]string{}
	for _, k := range order {
		b, a := base[k], alt[k]
		if b.verdict == a.verdict && b.sentinel == a.sentinel && b.detail == a.detail {
			continue
		}
		cells[k] = a
		kept = append(kept, k)
		if text, ok := altMsgs[a.detail]; ok {
			msgs[a.detail] = text
		}
	}
	return cells, kept, msgs
}

// renderRegistryDelta serialises the delta in the manifest's row grammar, under
// a header of its own: the manifest's says the rows are the whole cross product
// and these are a subset of it under a second registry.
func renderRegistryDelta(cells map[cellKey]sweepCell, order []cellKey, messages map[string]string) []byte {
	var b strings.Builder
	b.WriteString("# resolver corpus sweep, registry axis: the cells whose outcome moves when\n")
	b.WriteString("# the cross product is resolved under regR7Alt instead of regR7. Generated\n")
	b.WriteString("# file: edit internal/resolver/sweep_test.go, not this.\n")
	b.WriteString("#\n")
	fmt.Fprintf(&b, "# %d cells differ. Absent from this file means the two registries agree\n", len(order))
	b.WriteString("# on that cell, which is every cell of a query with no CALL clause.\n")
	b.WriteString("#\n")
	b.WriteString("# cell rows carry the regR7Alt side only, in the manifest's grammar: query,\n")
	b.WriteString("# schema, verdict, sentinel, detail, with a strconv.Quote'd msg row per\n")
	b.WriteString("# refusal digest. The regR7 side is the row under the same key in\n")
	b.WriteString("# sweep.manifest.tsv.\n")
	b.WriteString("#\n")
	b.WriteString("# what this file adds over that manifest: regR7 is the only registry the\n")
	b.WriteString("# manifest is swept under, so a CALL diagnostic that fires only under some\n")
	b.WriteString("# other signature table moves no row there. It moves a row here.\n")
	b.WriteString("#\n")
	b.WriteString("# what it does not add: any registry beyond regR7Alt, which is signaturesR7\n")
	b.WriteString("# with every type token rotated and every nullability flipped. A diagnostic\n")
	b.WriteString("# needing a procedure name, an arity or a result field regR7 does not\n")
	b.WriteString("# declare is outside both files.\n")
	b.WriteString("#\n")
	fmt.Fprintf(&b, "# regenerate: %s\n", sweepDeltaRegenCmd)

	writeSweepRows(&b, cells, order, messages)
	return []byte(b.String())
}

// TestSweepRegistryDelta sweeps the corpus a second time under regR7Alt and
// holds the cells that move to a committed file. WithRegistry is the resolver's
// only Option, so the registry is the one axis TestCorpusSweepManifest holds
// constant, and a change to a CALL diagnostic visible only under another
// signature table moves nothing there (gqlc-2xkf).
//
// The non-degeneracy assertions run BEFORE the -update write, and they assert
// per route rather than on the size of the delta. A single NotEmpty over the
// delta is satisfied by either route alone, so it stays green when the registry
// stops reaching the parser — and the parser route is the one that types a
// YIELD column, which is most of what a CALL cell's model is. Their two counts
// are what a -update cannot absorb: regenerating writes whatever the sweep
// produced, so a delta gone empty or gone one-sided would otherwise be
// committed as the new baseline in silence.
//
//   - a moved VERDICT witnesses the resolver route: a rotated PARAM token
//     reaches argAssignable and refuses an argument regR7 admits.
//   - a cell accepting under both registries with a different detail witnesses
//     the parser route: a rotated RESULT token and its flipped nullability
//     change the CallBinding's result type and therefore the model, without
//     touching any verdict.
func TestSweepRegistryDelta(t *testing.T) {
	queries, schemas := sweepCorpus(t)
	base, order, _ := runSweep(t, queries, schemas, regR7)
	alt, _, altMsgs := runSweep(t, queries, schemas, regR7Alt)

	cells, deltaOrder, msgs := registryDelta(base, alt, order, altMsgs)

	var movedVerdict, movedModel int
	for _, k := range deltaOrder {
		switch {
		case base[k].verdict != alt[k].verdict:
			movedVerdict++
		case base[k].verdict == verdictAccept && base[k].detail != alt[k].detail:
			movedModel++
		}
	}
	require.NotZero(t, movedVerdict,
		"no cell of the %d-cell cross product changes verdict under regR7Alt: the registry does not reach the resolver's argument-assignability check", len(order))
	require.NotZero(t, movedModel,
		"no cell of the %d-cell cross product accepts under both registries with a different model: the registry does not reach the parser's YIELD column typing", len(order))

	if *update {
		if _, err := os.Stat(sweepDeltaPath); err == nil {
			t.Log(compareSweep(loadSweepFile(t, sweepDeltaPath, sweepDeltaRegenCmd), cells, deltaOrder, msgs).report(sweepDeltaPath))
		}
		require.NoError(t, os.WriteFile(sweepDeltaPath, renderRegistryDelta(cells, deltaOrder, msgs), 0o644))
		return
	}

	cmp := compareSweep(loadSweepFile(t, sweepDeltaPath, sweepDeltaRegenCmd), cells, deltaOrder, msgs)
	t.Log(cmp.report(sweepDeltaPath))
	if cmp.disagreements() == 0 {
		return
	}
	t.Errorf("%s\n\nif these changes are intended, regenerate with:\n  %s", cmp.report(sweepDeltaPath), sweepDeltaRegenCmd)
}

// TestSweepComparisonSortsEachDisagreement drives compareSweep with synthetic
// cells. On an unmutated tree every cell is identical, so the corpus run
// exercises the identical arm and nothing else — the arm that sorts a sentinel
// change away from a verdict change is the fix, and it needs cells that
// disagree.
//
// The buckets are exclusive and coarsest-first, which is what keeps a cell
// whose verdict moved out of the sentinel category even though its sentinel
// moved too.
func TestSweepComparisonSortsEachDisagreement(t *testing.T) {
	sch := "valid/schemas/s.gql"
	cell := func(q, verdict, sentinel, detail string) sweepCell {
		return sweepCell{key: cellKey{query: q, schema: sch}, verdict: verdict, sentinel: sentinel, detail: detail}
	}
	was := map[cellKey]sweepCell{}
	now := map[cellKey]sweepCell{}
	var order []cellKey
	add := func(old, fresh sweepCell) {
		was[old.key] = old
		now[fresh.key] = fresh
		order = append(order, fresh.key)
	}
	add(cell("same.cypher", verdictRefuse, "ErrUnknownEdge", "d1"),
		cell("same.cypher", verdictRefuse, "ErrUnknownEdge", "d1"))
	add(cell("detail.cypher", verdictRefuse, "ErrUnknownEdge", "d1"),
		cell("detail.cypher", verdictRefuse, "ErrUnknownEdge", "d2"))
	// The blind-quadrant cell: refuses on both sides, no golden byte moves.
	add(cell("sentinel.cypher", verdictRefuse, "ErrAmbiguousLabel", "d1"),
		cell("sentinel.cypher", verdictRefuse, "ErrUnknownEdge", "d1"))
	// Sentinel and detail both moved; coarsest-first puts it in sentinel only.
	add(cell("both.cypher", verdictRefuse, "ErrAmbiguousLabel", "d1"),
		cell("both.cypher", verdictRefuse, "ErrUnknownEdge", "d2"))
	add(cell("verdict.cypher", verdictRefuse, "ErrUnknownEdge", "d1"),
		cell("verdict.cypher", verdictAccept, sentinelNone, "d3"))

	gone := cell("gone.cypher", verdictRefuse, "ErrUnknownEdge", "d1")
	was[gone.key] = gone
	fresh := cell("fresh.cypher", verdictRefuse, "ErrUnknownEdge", "d1")
	now[fresh.key] = fresh
	order = append(order, fresh.key)

	wantOrder := make([]cellKey, 0, len(was))
	for k := range was {
		wantOrder = append(wantOrder, k)
	}
	sort.Slice(wantOrder, func(i, j int) bool { return wantOrder[i].query < wantOrder[j].query })

	cmp := compareSweep(
		sweepManifest{cells: was, order: wantOrder, messages: map[string]string{"d1": "was text"}},
		now, order, map[string]string{"d1": "was text", "d2": "now text"})

	require.Equal(t, 1, cmp.identical)
	require.Len(t, cmp.differentDetail, 1)
	require.Len(t, cmp.differentSentinel, 2)
	require.Len(t, cmp.differentVerdict, 1)
	require.Len(t, cmp.onlyInManifest, 1)
	require.Len(t, cmp.onlyInSweep, 1)
	require.Equal(t, 6, cmp.disagreements())

	// A path no constant in this file carries. The report names the file the
	// counts were taken against, and two files are compared this way now, so a
	// summary that reached for sweepManifestPath instead of its argument would
	// send a reader diffing the registry delta to a manifest that did not move.
	report := cmp.report("synthetic/against.tsv")
	for _, want := range []string{
		"sweep vs synthetic/against.tsv:",
		"DIFFERENT SENTINEL, same verdict (2)",
		"DIFFERENT VERDICT (1)",
		"DIFFERENT DETAIL, same verdict and sentinel (1)",
		"in manifest, absent from sweep (1)",
		"in sweep, absent from manifest (1)",
		"sentinel.cypher x valid/schemas/s.gql",
		`was: refuse ErrAmbiguousLabel "was text"`,
		`now: refuse ErrUnknownEdge "was text"`,
		`gone.cypher x valid/schemas/s.gql: refuse ErrUnknownEdge "was text"`,
	} {
		require.Contains(t, report, want)
	}
	require.NotContains(t, strings.SplitN(report, "DIFFERENT DETAIL, same verdict and sentinel", 2)[1],
		"both.cypher", "a cell whose sentinel and detail both moved belongs to one category")
}

// TestRecordCellReportsAMarshalFailure drives the accepting arm with a model
// json.Marshal refuses, which is the arm no fixture reaches: on this corpus
// every accepted model marshals, so the corpus run exercises the other branch
// 3763 times and this one never.
//
// The cell has to come back as data and reach the report. An assertion in its
// place aborts on the first affected cell, and the resulting nonzero exit with
// no category table is what a sentinel change also looks like from outside.
func TestRecordCellReportsAMarshalFailure(t *testing.T) {
	k := cellKey{query: "valid/q.cypher", schema: "valid/schemas/s.gql"}
	texts := map[string]string{}
	record := func(_ cellKey, text string) string {
		id := sweepDigest(text)
		texts[id] = text
		return id
	}

	broken := recordCell(k, make(chan int), nil, record)
	require.Equal(t, verdictAccept, broken.verdict, "a cell Resolve accepted stays accepted")
	require.Equal(t, sentinelNone, broken.sentinel)
	require.Contains(t, texts[broken.detail], marshalErrPrefix)

	// Same cell, a model that marshals. Without a different detail the failure
	// lands in the identical bucket and the report never mentions it.
	fine := recordCell(k, struct{}{}, nil, record)
	require.NotEqual(t, fine.detail, broken.detail)

	cmp := compareSweep(
		sweepManifest{cells: map[cellKey]sweepCell{k: fine}, order: []cellKey{k}, messages: texts},
		map[cellKey]sweepCell{k: broken}, []cellKey{k}, texts)
	require.Len(t, cmp.differentDetail, 1)
	require.Empty(t, cmp.differentVerdict)
	require.Contains(t, cmp.report(sweepManifestPath), marshalErrPrefix)

	// The refusing arm goes through the same function, and a refusal is
	// recorded on its sentinel even when a model was returned alongside it.
	refused := recordCell(k, struct{}{}, fmt.Errorf("wrapped: %w", ErrUnknownEdge), record)
	require.Equal(t, verdictRefuse, refused.verdict)
	require.Equal(t, "ErrUnknownEdge", refused.sentinel)
	require.Equal(t, "wrapped: unknown edge", texts[refused.detail])
}

// TestSweepManifestRejectsHandEdits feeds parseSweepManifest rows a sweep does
// not produce. Regenerating the manifest is the sanctioned way to absorb a diff
// and it leaves a reviewable one; editing the file to make a category go quiet
// leaves none, so the parser refuses the edits that would do it.
func TestSweepManifestRejectsHandEdits(t *testing.T) {
	digest := sweepDigest("unknown edge: x")
	msgRow := "msg\t" + digest + "\t" + strconv.Quote("unknown edge: x") + "\n"
	cellRow := func(verdict, sentinel string) string {
		return "cell\tvalid/q.cypher\tvalid/schemas/s.gql\t" + verdict + "\t" + sentinel + "\t" + digest + "\n"
	}
	good := cellRow(verdictRefuse, "ErrUnknownEdge") + msgRow
	require.Len(t, parseSweepManifest(t, "good.tsv", []byte(good)).cells, 1)
	// A probe that answered with one fixed diagnosis is caught by the rows
	// themselves, because each asserts its own guard's text. What the rows do
	// not catch is a probe answering with every guard's diagnosis at once: with
	// this line removed, such a probe passes all fourteen of them.
	require.Empty(t, sweepParseError(good), "the probe must accept a row a sweep does produce")

	// Every row is good with one thing broken, and asserts the diagnosis that
	// one thing earns rather than that some diagnosis came back. A row that
	// dropped good's msg row too can draw the cell/msg correspondence's
	// diagnosis instead of its own guard's — that correspondence reaches a
	// refusing cell whose msg row is gone — and a table asserting only that
	// some diagnosis came back reads that as the row's own guard still
	// working. Not every such row: the correspondence skips a cell that is not
	// refusing.
	//
	// Measured against the table this one replaces, which asserted only that
	// some guard had rejected the row: neutering each of the 13 guards in turn
	// — compile-screened with `go test -c -o /dev/null ./internal/resolver`,
	// run with -count=1, each restored by md5 — left the whole package green
	// for eight of them. For five of those eight the cell/msg correspondence
	// was the last guard standing: the allSentinels-name guard, msg digest
	// re-derivation and duplicate msg digest reach it in one step, while the
	// empty-sentinel guard reaches it behind the allSentinels-name guard and
	// msg text quoting behind the digest guard, so those two need three
	// deletions together before their row is accepted. The other three of the
	// eight were masked by something that is not the correspondence: both
	// field-count guards by the index-out-of-range panic their absence causes,
	// which that table's probe scored as a rejection, and the verdict guard by
	// the allSentinels-name guard, because that table's verdict row carried
	// "-" in the sentinel column where the row below carries a real one.
	for name, tc := range map[string]struct{ row, want string }{
		"sentinel that is no sentinel":     {cellRow(verdictRefuse, "ErrTypo") + msgRow, `sentinel column names "ErrTypo"`},
		"sentinel on an accepting cell":    {cellRow(verdictAccept, "ErrUnknownEdge") + msgRow, "accepting cell carries sentinel"},
		"verdict that is neither":          {cellRow("maybe", "ErrUnknownEdge") + msgRow, `unrecognised verdict "maybe"`},
		"one sentinel of a + join unknown": {cellRow(verdictRefuse, "ErrUnknownEdge+ErrTypo") + msgRow, `sentinel column names "ErrTypo"`},
		"empty sentinel on a refusal":      {cellRow(verdictRefuse, "") + msgRow, "refusing cell carries an empty sentinel column"},
		"msg text edited under its digest": {cellRow(verdictRefuse, "ErrUnknownEdge") + "msg\t" + digest + "\t" + strconv.Quote("unknown edge: y") + "\n", "msg digest does not match its own text"},
		"msg text not quoted":              {cellRow(verdictRefuse, "ErrUnknownEdge") + "msg\t" + digest + "\tunknown edge: x\n", "msg text is not a quoted Go string"},
		"duplicate msg digest":             {good + msgRow, "duplicate msg digest"},
		"duplicate cell":                   {good + cellRow(verdictRefuse, "ErrUnknownEdge"), "duplicate cell"},
		"refusing cell with no msg row":    {cellRow(verdictRefuse, "ErrUnknownEdge"), "with no msg row"},
		"msg row no cell cites":            {good + "msg\t" + sweepDigest("unknown edge: y") + "\t" + strconv.Quote("unknown edge: y") + "\n", "is cited by no cell"},
		"row that is neither kind":         {good + "cells\tvalid/q.cypher\n", `unrecognised row "cells`},
		"cell row short a field":           {good + "cell\tvalid/r.cypher\tvalid/schemas/s.gql\trefuse\tErrUnknownEdge\n", "cell row wants 6 fields"},
		"msg row short a field":            {good + "msg\t" + sweepDigest("unknown edge: y") + "\n", "msg row wants 3 fields"},
	} {
		t.Run(name, func(t *testing.T) {
			require.Contains(t, sweepParseError(tc.row), tc.want,
				"parseSweepManifest did not reject this row for the reason the row is named for")
		})
	}
}

// sweepProbe is a sweepT that records what the parser said instead of failing a
// test with it. Recording the text rather than a bool is what lets a row assert
// which guard fired: a bool cannot distinguish a row rejected by the guard it
// is named for from a row rejected by some other guard first.
type sweepProbe struct{ said []string }

func (p *sweepProbe) Helper() {}

func (p *sweepProbe) Errorf(format string, args ...any) {
	p.said = append(p.said, fmt.Sprintf(format, args...))
}

// FailNow runs on the goroutine sweepParseError starts, and Goexit there is
// what stops the parser mid-file the way t.FailNow stops it under a real test.
func (p *sweepProbe) FailNow() { runtime.Goexit() }

func (p *sweepProbe) Fatalf(format string, args ...any) {
	p.Errorf(format, args...)
	p.FailNow()
}

// sweepParseError returns what parseSweepManifest said about raw, or "" if it
// accepted raw. FailNow exits the calling goroutine, hence the goroutine and
// the wait.
//
// A panic is reported under its own text. It is not the diagnosis the parser's
// own guards give, but a row that crashes the parser is not a row it accepted,
// and returning "" for it would report the opposite.
func sweepParseError(raw string) string {
	probe := &sweepProbe{}
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer func() {
			if r := recover(); r != nil {
				probe.said = append(probe.said, fmt.Sprintf("panic: %v", r))
			}
		}()
		parseSweepManifest(probe, "edited.tsv", []byte(raw))
	}()
	<-done
	return strings.Join(probe.said, "\n")
}

// TestSweepManifestSurvivesHostileRefusalText renders and re-parses a manifest
// whose refusal texts carry the manifest's own row and field separators, plus
// the quoting characters, plus a digest-shaped substring.
//
// This is what separates the two failure modes the sweep can exhibit. A sweep
// that aborts because a changed message broke its serialisation exits nonzero
// exactly as a sweep that detected a changed sentinel does; the categories in
// the report are the only thing that tells them apart, and a report is only
// reached if the round trip survives the message.
func TestSweepManifestSurvivesHostileRefusalText(t *testing.T) {
	hostile := []string{
		"unknown label: two\nlines",
		"unknown edge: a\tb",
		`ambiguous label: quoted "witness" and a \backslash`,
		"unknown property: msg\t00e5b117fa25\tnot a row",
		"invalid effect target: trailing newline\n",
		"call argument assignability: \x00\x7f control bytes",
		"",
	}

	cells := map[cellKey]sweepCell{}
	order := make([]cellKey, 0, len(hostile))
	messages := map[string]string{}
	for i, text := range hostile {
		k := cellKey{query: fmt.Sprintf("valid/q%02d.cypher", i), schema: "valid/schemas/s.gql"}
		id := sweepDigest(text)
		messages[id] = text
		cells[k] = sweepCell{key: k, verdict: verdictRefuse, sentinel: "ErrUnknownLabel", detail: id}
		order = append(order, k)
	}
	accepted := cellKey{query: "valid/accepted.cypher", schema: "valid/schemas/s.gql"}
	cells[accepted] = sweepCell{key: accepted, verdict: verdictAccept, sentinel: sentinelNone, detail: sweepDigest("{}")}
	order = append(order, accepted)

	raw := renderSweepManifest(cells, order, messages)
	require.Contains(t, string(raw), "# regenerate: "+sweepRegenCmd,
		"the header must name the invocation that prints what a regeneration absorbed")
	require.Contains(t, string(raw), fmt.Sprintf("%d cells: 1 accept, %d refuse", len(order), len(hostile)),
		"the header must count the rows it heads")

	var cellRows, msgRows int
	for _, line := range strings.Split(strings.TrimRight(string(raw), "\n"), "\n") {
		switch {
		case strings.HasPrefix(line, "cell\t"):
			cellRows++
		case strings.HasPrefix(line, "msg\t"):
			msgRows++
		}
	}
	require.Equal(t, len(order), cellRows, "every cell must occupy exactly one row")
	require.Equal(t, len(messages), msgRows, "every message must occupy exactly one row")

	got := parseSweepManifest(t, "hostile.tsv", raw)
	require.Equal(t, cells, got.cells)
	require.Equal(t, messages, got.messages)

	// The round trip is what makes a reworded message a reported disagreement
	// rather than an aborted parse: reword one and the comparison names it.
	reworded := map[cellKey]sweepCell{}
	rewordedMsgs := map[string]string{}
	for k, c := range cells {
		if c.verdict == verdictRefuse {
			id := sweepDigest(messages[c.detail] + "\nrewritten")
			rewordedMsgs[id] = messages[c.detail] + "\nrewritten"
			c.detail = id
		}
		reworded[k] = c
	}
	cmp := compareSweep(got, reworded, order, rewordedMsgs)
	require.Len(t, cmp.differentDetail, len(hostile))
	require.Empty(t, cmp.differentSentinel)
	require.Empty(t, cmp.differentVerdict)
	require.Equal(t, 1, cmp.identical)
	require.Contains(t, cmp.report(sweepManifestPath), "rewritten")
}

// multiSentinel satisfies every error it was built from. It is the shape a
// refusal takes when a shared wrapper or an errors.Join starts folding a second
// sentinel into an existing one, which is the accident the sentinel column
// exists to name.
type multiSentinel struct{ errs []error }

func (m multiSentinel) Error() string   { return m.errs[0].Error() }
func (m multiSentinel) Unwrap() []error { return m.errs }

// TestSentinelsOfNamesEverySentinelSatisfied drives sentinelsOf directly,
// because the corpus cannot. Every one of the 8017 refusing cells satisfies
// exactly one sentinel, so a rewrite of sentinelsOf that stopped at the first
// hit renders every one of them identically and the manifest compares clean —
// silently deleting the sentinel-widening class the column is here for.
//
// The order rows join their errors in reverse allSentinels order and require
// the rendered join in allSentinels order, so the output is a function of the
// sentinel set and not of how the error was assembled: two refusals satisfying
// the same pair must produce the same column whichever wrapper built them.
func TestSentinelsOfNamesEverySentinelSatisfied(t *testing.T) {
	all := make([]string, 0, len(allSentinels))
	for _, s := range allSentinels {
		all = append(all, sentinelNames[s])
	}

	for name, tc := range map[string]struct {
		err  error
		want string
	}{
		"one sentinel, wrapped": {
			fmt.Errorf("ambiguous label: p: %w", ErrAmbiguousLabel),
			"ErrAmbiguousLabel",
		},
		"two sentinels, joined in reverse order": {
			multiSentinel{[]error{
				fmt.Errorf("out of R0 scope: %w", ErrOutOfR0Scope),
				fmt.Errorf("ambiguous label: p: %w", ErrAmbiguousLabel),
			}},
			"ErrAmbiguousLabel+ErrOutOfR0Scope",
		},
		"two sentinels adjacent in allSentinels": {
			multiSentinel{[]error{ErrUnknownLabel, ErrAmbiguousLabel}},
			"ErrUnknownLabel+ErrAmbiguousLabel",
		},
		"first and last": {
			multiSentinel{[]error{ErrCallArgAssignability, ErrUnknownLabel}},
			"ErrUnknownLabel+ErrCallArgAssignability",
		},
		"every sentinel at once": {
			multiSentinel{slices.Clone(allSentinels)},
			strings.Join(all, "+"),
		},
		"no sentinel falls through": {
			errors.New("some refusal outside allSentinels"),
			sentinelUnmatched,
		},
	} {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, tc.want, sentinelsOf(tc.err))
		})
	}

	// Stated separately from the table because it is the property, not a row:
	// widening the set has to move the column. A first-hit-only sentinelsOf
	// returns "ErrAmbiguousLabel" for both of these and this goes red.
	narrow := fmt.Errorf("ambiguous label: p: %w", ErrAmbiguousLabel)
	widened := multiSentinel{[]error{narrow, ErrOutOfR0Scope}}
	require.Equal(t, narrow.Error(), widened.Error(),
		"the widened error must carry byte-identical text, or the detail digest moves instead")
	require.NotEqual(t, sentinelsOf(narrow), sentinelsOf(widened),
		"a refusal that starts satisfying a second sentinel must read as a changed column")
}

// TestSweepSentinelNamesAreTotal holds the map the manifest's sentinel column
// is written through. Both directions, per element rather than by length: a
// length check passes a map that swapped one sentinel's entry for another's.
func TestSweepSentinelNamesAreTotal(t *testing.T) {
	for _, s := range allSentinels {
		name, ok := sentinelNames[s]
		require.True(t, ok, "sentinel %q has no entry in sentinelNames", s)
		require.NotEmpty(t, name)
	}
	seen := map[string]bool{}
	for s, name := range sentinelNames {
		require.True(t, slices.Contains(allSentinels, s),
			"sentinelNames names %q, which is not in allSentinels", name)
		require.False(t, seen[name], "two sentinels share the name %q", name)
		seen[name] = true
	}
}

// sentinelMembership counts refusing cells per sentinel over the members of
// each cell's column, not over the joined string it renders as. A cell
// refusing with ErrAmbiguousLabel+ErrOutOfR0Scope does refuse with
// ErrAmbiguousLabel, and counting the join reports that no cell reaches it —
// a true failure carrying a false reason.
//
// Every cell of this corpus renders a single-member column, so the corpus run
// never distinguishes this from counting the join;
// TestSentinelMembershipCountsEachMember is what holds it.
func sentinelMembership(cells map[cellKey]sweepCell) map[string]int {
	count := map[string]int{}
	for _, c := range cells {
		if c.verdict != verdictRefuse {
			continue
		}
		for _, name := range strings.Split(c.sentinel, "+") {
			count[name]++
		}
	}
	return count
}

// TestSentinelMembershipCountsEachMember drives sentinelMembership with the
// joined column the corpus does not produce. Counting the column string whole
// would leave every member at zero, which is how a widened sentinel turns a
// reachability check into a false report of an unreachable one.
func TestSentinelMembershipCountsEachMember(t *testing.T) {
	sch := "valid/schemas/s.gql"
	cell := func(q, verdict, sentinel string) sweepCell {
		return sweepCell{key: cellKey{query: q, schema: sch}, verdict: verdict, sentinel: sentinel, detail: "d"}
	}
	cells := map[cellKey]sweepCell{}
	for _, c := range []sweepCell{
		cell("a.cypher", verdictRefuse, "ErrAmbiguousLabel"),
		cell("b.cypher", verdictRefuse, "ErrAmbiguousLabel+ErrOutOfR0Scope"),
		cell("c.cypher", verdictRefuse, "ErrOutOfR0Scope"),
		cell("d.cypher", verdictRefuse, sentinelUnmatched),
		// An accepting cell's "-" is no sentinel and must not be counted as one.
		cell("e.cypher", verdictAccept, sentinelNone),
	} {
		cells[c.key] = c
	}

	count := sentinelMembership(cells)
	require.Equal(t, 2, count["ErrAmbiguousLabel"], "the joined cell must count toward each member")
	require.Equal(t, 2, count["ErrOutOfR0Scope"])
	require.Equal(t, 1, count[sentinelUnmatched])
	require.Zero(t, count[sentinelNone], "an accepting cell carries no sentinel")
	require.Zero(t, count["ErrAmbiguousLabel+ErrOutOfR0Scope"], "the join is not itself a sentinel")
}

// TestSweepReachesEverySentinel asserts per sentinel, not over the set. A
// single NotZero across the corpus fires when every sentinel has gone silent
// together, so eleven of twelve going silent would keep it green.
//
// Reachability is asked of sentinelMembership, so a widened column answers for
// every sentinel it names rather than for the join alone.
//
// Regenerating the manifest absorbs a diff; it does not make an unreachable
// sentinel reachable, so this stays red across a -update that hides one.
func TestSweepReachesEverySentinel(t *testing.T) {
	queries, schemas := sweepCorpus(t)
	cells, _, _ := runSweep(t, queries, schemas, regR7)

	count := sentinelMembership(cells)
	require.Zero(t, count[sentinelUnmatched],
		"%d refusals satisfy no sentinel in allSentinels", count[sentinelUnmatched])
	for _, s := range allSentinels {
		name := sentinelNames[s]
		require.NotZero(t, count[name],
			"no cell of the %d-cell cross product refuses with %s", len(cells), name)
	}
}

// TestSweepIsIndependentOfCellOrder re-parses the query for every cell and
// requires the identical outcome. runSweep parses each query once and resolves
// it against every schema; that is sound while Resolve leaves its argument
// alone, and a Resolve that mutated it would make the manifest depend on the
// order cells run in. Regenerating the manifest does not settle that question
// either, so this asks it directly.
//
// Both sides record through recordCell. Reimplementing the recording here
// would make this test disagree with the sweep about any output the two chose
// to render differently, and report that disagreement as cell order.
func TestSweepIsIndependentOfCellOrder(t *testing.T) {
	queries, schemas := sweepCorpus(t)
	once, order, _ := runSweep(t, queries, schemas, regR7)

	parsedS := make(map[string]schema.Schema, len(schemas))
	for _, name := range schemas {
		src, err := os.ReadFile(filepath.Join(fixtureDir, name))
		require.NoError(t, err)
		sch, err := gql.New().Parse(bytes.NewReader(src))
		require.NoError(t, err)
		parsedS[name] = sch
	}
	for _, k := range order {
		src, err := os.ReadFile(filepath.Join(fixtureDir, k.query))
		require.NoError(t, err)
		q, err := cypher.New(cypher.WithRegistry(regR7)).Parse(bytes.NewReader(src))
		require.NoError(t, err)

		vq, rerr := New(parsedS[k.schema], WithRegistry(regR7)).Resolve(q)
		c := recordCell(k, vq, rerr, digestOnly)
		require.Equal(t, once[k], c, "cell %s differs between parse-once and parse-per-cell", k)
	}
}
