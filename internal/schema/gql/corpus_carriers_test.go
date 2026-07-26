package gql

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestCorpusAreaParseCarriers gates the *parse-time* incidental-carrier class
// the corpus-wide coverage test cannot see by construction: the wide test asks
// whether *some* file reached a name, never *which*. So a name whose owning
// area no longer carries a file for it silently reads green as long as a file
// from an unrelated area happened to reach it in passing during parsing. One of
// these was fixed by hand on PR #392 (`localdatetimeType#2` carried only by
// five 18.3-edge-type files spelling TIMESTAMP incidentally), and no test
// stopped it from being reverted.
//
// The measurement runs the coverage gate once per area over just that area's
// files, subtracts the red set from the obligation, and calls the result
// carried(area). A required name whose owning area is not among its carriers is
// an orphan. Ownership is derived from the ISO clause heading the grammar rule
// (or the rule first naming a token or alternative) is declared under — the
// directory prefixes an area owns already carry that clause number, so the two
// register the same partition. This is the mechanism the out-of-tree detector
// used (/tmp/lead/orphans.sh, never checked in) — moving it here is what makes
// the check reproducible from the tree.
//
// Names whose section number falls outside every area's prefixes are cross-cutting
// (identifier lexis, and grammar utility rules under section 21). These are the
// carrierExemptions list, mirroring alternativeExemption: an exemption is
// preferable to widening the ownership rule, because widening is invisible and an
// exemption is a reviewer's checklist item with a stated reason.
//
// LIMIT: this gate measures *parse-time* carriage only. A name carried
// exclusively by files whose outcome is `unsupported` (the listener rejects
// them after the parse tree is complete) still reads as carried here, because
// the tokens and rules were entered by the parser before the listener
// rejected. Non-vacuity check at SHA 22e02c02 (which has the TIMESTAMP fix but
// lacks the NODE-synonym fix 7217b180) passes this gate: `NODE` is entered by
// unsupported files in areas B and D2, so parse coverage is satisfied while
// the resolving path is not. That resolving-time class is gqlc-h9n.26, which
// needs a distinct instrument (run over files whose outcome is `resolves`
// only). Do NOT extend this test to try to catch it — the two want different
// inputs.
func TestCorpusAreaParseCarriers(t *testing.T) {
	obligation := grammarObligation(t)
	sections := scanRuleSections(t)

	owners := areaOwners(t, sections, obligation)

	filesByArea := corpusFilesByArea(t)
	carried := carriedByArea(t, filesByArea, obligation)

	// The exemption list is a claim about names outside every area's prefixes, so
	// it can only shelter names whose owners set is empty. An exemption over a
	// name any area owns would hide a real orphan, which is exactly what the
	// out-of-tree detector was written to find. requireCarrierExemptions verifies
	// this shape and the metadata on each entry.
	exempt := requireCarrierExemptions(t, owners, obligation)

	orphans := findOrphans(obligation, owners, carried)
	unowned := findUnowned(owners, exempt, obligation)

	if len(orphans) == 0 && len(unowned) == 0 {
		return
	}
	t.Fatalf("%d orphan names (no owning area carries them) and %d unowned names (no area owns and no exemption covers):\n%s\n%s",
		len(orphans), len(unowned),
		orphanReport(orphans, filesByArea),
		unownedReport(unowned))
}

// carrierExemption records a name that falls outside every area's prefix rule,
// which is either grammar utility (identifier, nonReservedWords, regularIdentifier)
// or under an ISO section none of the areas own. Rather than widen the ownership
// rule and hide the fact, each such name is written down with a reason: a widened
// rule is invisible and an exemption is a reviewer's checklist.
//
// kind is the sort of name being exempted ("rule", "token", "alt"). Redundant with
// what a lookup could tell you, and kept so a reader of this list does not have to
// cross-reference the three obligation sets to know which they are being asked to
// endorse.
type carrierExemption struct {
	name string
	kind string
	bead string
	why  string
}

// carrierExemptions lists the names whose ownership does not fall to any area by
// the section-prefix rule. Every entry needs a bead pointing at the work that
// would let the entry be deleted, and a reason a reviewer can weigh; the question
// the reader must answer is "would I rather do that work than keep this here".
// Structural entries (section 21 identifier lexis; the section-11.1 query surface
// cut) have no retirement path by design — the `why:` must say so, or a future
// reader burns time hunting for the bead that removes them.
//
// If this list grows past a handful the ownership model is wrong, not the
// exemptions — file the finding rather than adding a tenth entry.
var carrierExemptions = []carrierExemption{
	// Identifier lexis, section 21.1, unowned by any of the ISO 18.x construct
	// areas — every corpus file names an identifier somewhere, so requiring a
	// specific area to carry these adds nothing. STRUCTURAL: no retirement path;
	// ISO clause 21 has no corpus directory by design.
	{name: "identifier", kind: "rule", bead: "gqlc-h9n.17", why: "STRUCTURAL: section 21.1 identifier lexis; reached from every construct in every area, so ownership is spurious. No retirement path — clause 21 has no corpus directory by design."},
	{name: "regularIdentifier", kind: "rule", bead: "gqlc-h9n.17", why: "STRUCTURAL: section 21.1 identifier lexis; reached from every construct in every area. No retirement path — clause 21 has no corpus directory by design."},
	{name: "nonReservedWords", kind: "rule", bead: "gqlc-h9n.17", why: "STRUCTURAL: section 21.1 identifier lexis; a coverage cut in coverageCuts, referenced by regularIdentifier. No retirement path — clause 21 has no corpus directory by design."},
	{name: "objectName", kind: "rule", bead: "gqlc-h9n.17", why: "STRUCTURAL: section 21.1 identifier lexis; naming productions reach it from every area. No retirement path — clause 21 has no corpus directory by design."},
	{name: "schemaName", kind: "rule", bead: "gqlc-h9n.17", why: "STRUCTURAL: section 21.1 identifier lexis; the schema-reference chain reaches it. No retirement path — clause 21 has no corpus directory by design."},
	{name: "labelName", kind: "rule", bead: "gqlc-h9n.17", why: "STRUCTURAL: section 21.1 identifier lexis; every label set in every area reaches it. No retirement path — clause 21 has no corpus directory by design."},
	{name: "propertyName", kind: "rule", bead: "gqlc-h9n.17", why: "STRUCTURAL: section 21.1 identifier lexis; every property spelling in every area reaches it. No retirement path — clause 21 has no corpus directory by design."},
	{name: "fieldName", kind: "rule", bead: "gqlc-h9n.17", why: "STRUCTURAL: section 21.1 identifier lexis; reached by 18.10-field-type field lists. No retirement path — clause 21 has no corpus directory by design."},
	{name: "directoryName", kind: "rule", bead: "gqlc-h9n.17", why: "STRUCTURAL: section 21.1 identifier lexis; reached by 17-references directory paths. No retirement path — clause 21 has no corpus directory by design."},
	{name: "graphTypeName", kind: "rule", bead: "gqlc-h9n.17", why: "STRUCTURAL: section 21.1 identifier lexis; every CREATE GRAPH TYPE names one, so every area reaches it. No retirement path — clause 21 has no corpus directory by design."},
	{name: "edgeTypeName", kind: "rule", bead: "gqlc-h9n.17", why: "STRUCTURAL: section 21.1 identifier lexis; edge type declarations name it. No retirement path — clause 21 has no corpus directory by design."},
	{name: "nodeTypeName", kind: "rule", bead: "gqlc-h9n.17", why: "STRUCTURAL: section 21.1 identifier lexis; node type declarations name it. No retirement path — clause 21 has no corpus directory by design."},
	{name: "unsignedInteger", kind: "rule", bead: "gqlc-h9n.17", why: "STRUCTURAL: section 21.2 literal; length/precision/scale spellings reach it from every area with sized types. No retirement path — clause 21 has no corpus directory by design."},
	{name: "unsignedDecimalInteger", kind: "rule", bead: "gqlc-h9n.17", why: "STRUCTURAL: section 21.2 literal; alias of unsignedInteger. No retirement path — clause 21 has no corpus directory by design."},
	{name: "unsignedInteger#1", kind: "alt", bead: "gqlc-h9n.17", why: "STRUCTURAL: section 21.2 literal; alternative of unsignedInteger, above. No retirement path — clause 21 has no corpus directory by design."},
	{name: "identifier#1", kind: "alt", bead: "gqlc-h9n.17", why: "STRUCTURAL: section 21.1 identifier lexis; alternative of identifier, above. No retirement path — clause 21 has no corpus directory by design."},
	{name: "REGULAR_IDENTIFIER", kind: "token", bead: "gqlc-h9n.17", why: "STRUCTURAL: section 21.1 identifier lexis; identifier terminal. No retirement path — clause 21 has no corpus directory by design."},
	{name: "ACCENT_QUOTED_CHARACTER_SEQUENCE", kind: "token", bead: "gqlc-h9n.17", why: "STRUCTURAL: section 21.1 identifier lexis; delimited-identifier terminal. No retirement path — clause 21 has no corpus directory by design."},
	{name: "DOUBLE_QUOTED_CHARACTER_SEQUENCE", kind: "token", bead: "gqlc-h9n.17", why: "STRUCTURAL: section 21.1 identifier lexis; delimited-identifier terminal. No retirement path — clause 21 has no corpus directory by design."},
	{name: "UNSIGNED_DECIMAL_INTEGER", kind: "token", bead: "gqlc-h9n.17", why: "STRUCTURAL: section 21.2 literal; unsigned integer terminal. No retirement path — clause 21 has no corpus directory by design."},
	{name: "UNSIGNED_BINARY_INTEGER", kind: "token", bead: "gqlc-h9n.17", why: "STRUCTURAL: section 21.2 literal; unsigned integer alternate radix. No retirement path — clause 21 has no corpus directory by design."},
	{name: "UNSIGNED_HEXADECIMAL_INTEGER", kind: "token", bead: "gqlc-h9n.17", why: "STRUCTURAL: section 21.2 literal; unsigned integer alternate radix. No retirement path — clause 21 has no corpus directory by design."},
	{name: "UNSIGNED_OCTAL_INTEGER", kind: "token", bead: "gqlc-h9n.17", why: "STRUCTURAL: section 21.2 literal; unsigned integer alternate radix. No retirement path — clause 21 has no corpus directory by design."},

	// Section-heading misfits: declared under a section number that no area owns
	// by prefix, but semantically belong to one that does. Moving the declarations
	// under the right heading in GQL.g4 retires these — that is gqlc-h9n.27.
	{name: "graphTypeLikeGraph", kind: "rule", bead: "gqlc-h9n.27", why: "section 12.4 in the grammar for typesetting reasons, but the LIKE alternative belongs to 12.6 area A and 12.6-graph-type-statement/like_graph.gql carries it. Retire by moving the declaration under // 12.6."},
	{name: "isOrColon", kind: "rule", bead: "gqlc-h9n.27", why: "section 16.7 pattern lexis; reached by every area's label spelling and by the phrase forms in 18.2 and 18.3. Retire when 16.7 declarations move to the area that owns them."},
	{name: "IS", kind: "token", bead: "gqlc-h9n.27", why: "section 16.7 pattern lexis; label-set prefix keyword, alternate of COLON. Retire when 16.7 declarations move to the area that owns them."},
	{name: "COLON", kind: "token", bead: "gqlc-h9n.27", why: "section 16.7 pattern lexis; label-set prefix. Retire when 16.7 declarations move to the area that owns them."},
	{name: "LIKE", kind: "token", bead: "gqlc-h9n.27", why: "section 12.4 in the grammar but reachable from 12.6 through graphTypeLikeGraph, above. Retire by moving the declaration under // 12.6."},

	// Resolving-path-only carriers: parse-time coverage is satisfied by files
	// whose outcome is `unsupported` (the listener rejects them post-parse), so
	// this parse-time gate cannot see whether any resolving path exercises them.
	// gqlc-h9n.26 is the resolving-path gate that WOULD see it — retire these
	// once it lands and has its own owner-area check.
	{name: "nodeSynonym", kind: "rule", bead: "gqlc-h9n.26", why: "section 21.1 vocabulary; the NODE/VERTEX synonym pair. Parse-time carried by unsupported files in areas B and D2 (phrase_form.gql, constructed_node_closed.gql), so this gate cannot distinguish resolving-path presence. Retire once the resolving-path gate lands."},
	{name: "edgeSynonym", kind: "rule", bead: "gqlc-h9n.26", why: "section 21.1 vocabulary; the EDGE/RELATIONSHIP/DIRECTED synonym set. Same shape as nodeSynonym — parse-time carried by unsupported files. Retire once the resolving-path gate lands."},
	{name: "NODE", kind: "token", bead: "gqlc-h9n.26", why: "section 21.1 vocabulary; keyword synonym of VERTEX. Parse-time carried by unsupported files; resolving path is what needs the gate. Retire once the resolving-path gate lands."},
	{name: "VERTEX", kind: "token", bead: "gqlc-h9n.26", why: "section 21.1 vocabulary; keyword synonym of NODE. Parse-time carried by unsupported files; resolving path is what needs the gate. Retire once the resolving-path gate lands."},
	{name: "EDGE", kind: "token", bead: "gqlc-h9n.26", why: "section 21.1 vocabulary; keyword synonym of RELATIONSHIP. Parse-time carried by unsupported files; resolving path is what needs the gate. Retire once the resolving-path gate lands."},
	{name: "RELATIONSHIP", kind: "token", bead: "gqlc-h9n.26", why: "section 21.1 vocabulary; keyword synonym of EDGE. Parse-time carried by unsupported files; resolving path is what needs the gate. Retire once the resolving-path gate lands."},

	// The query-surface cut has no retirement path from within this gate — the
	// DDL areas are not meant to own query-side productions.
	{name: "graphExpression", kind: "rule", bead: "gqlc-h9n.17", why: "STRUCTURAL: section 11.1 query surface; a coverageCut and unowned by any of the DDL areas by design. No retirement path — the DDL areas do not own the query surface."},
}

// requireCarrierExemptions rejects entries that shelter something an area owns, or
// that name something not in the obligation, or that are missing metadata. The two
// registers must stay in step: an exemption that matches nothing silently stops
// excusing whatever it was written for.
func requireCarrierExemptions(t *testing.T, owners map[string][]string, obligation obligation) map[string]bool {
	t.Helper()

	inObligation := func(name, kind string) bool {
		switch kind {
		case "rule":
			return obligation.rules[name]
		case "token":
			return obligation.tokens[name]
		case "alt":
			return slices.Contains(obligation.required, name)
		default:
			return false
		}
	}

	exempt := make(map[string]bool, len(carrierExemptions))
	seen := make(map[string]bool, len(carrierExemptions))
	for _, ex := range carrierExemptions {
		require.NotEmpty(t, ex.name, "carrier exemption needs a name")
		require.Contains(t, []string{"rule", "token", "alt"}, ex.kind,
			"carrier exemption %q has invalid kind %q", ex.name, ex.kind)
		require.NotEmpty(t, ex.bead, "carrier exemption %q needs the bead recording it", ex.name)
		require.NotEmpty(t, ex.why, "carrier exemption %q needs a reason a reviewer can weigh", ex.name)
		require.False(t, seen[ex.name], "duplicate carrier exemption %q", ex.name)
		require.True(t, inObligation(ex.name, ex.kind),
			"carrier exemption %q (%s) is not in the obligation; delete it", ex.name, ex.kind)
		require.Empty(t, owners[ex.name],
			"carrier exemption %q is owned by area(s) %v; this exemption would hide a real orphan", ex.name, owners[ex.name])
		exempt[ex.name] = true
		seen[ex.name] = true
	}
	return exempt
}

// areaOwners maps every name in the obligation to the areas whose prefixes cover
// its ISO section. A name with no owning area is neither an orphan nor covered by
// this gate: it lands in `unowned` and requires a carrier exemption naming it.
//
// Tokens are attributed to the first rule referencing them (as in worklist), and
// alternatives to the rule they are numbered against. The obligation supplies both
// mappings, so nothing here re-derives them.
func areaOwners(t *testing.T, sections map[string]string, obligation obligation) map[string][]string {
	t.Helper()

	areaPrefixNums := areaPrefixNumbers(t)

	owners := make(map[string][]string)
	setOwners := func(name, sectionNum string) {
		var areas []string
		for area, nums := range areaPrefixNums {
			for _, num := range nums {
				if sectionMatchesArea(sectionNum, num) {
					areas = append(areas, area)
					break
				}
			}
		}
		sort.Strings(areas)
		owners[name] = areas
	}

	for rule := range obligation.rules {
		setOwners(rule, sectionNumber(sections[rule]))
	}
	for token, refs := range obligation.tokenRefs {
		setOwners(token, sectionNumber(sections[refs[0]]))
	}
	for _, tag := range obligation.required {
		setOwners(tag, sectionNumber(sections[ruleOf(tag)]))
	}
	return owners
}

// reSectionPrefix pulls the "N" or "N.M" number out of a section heading like
// "18.3 <edge type specification>" and out of an area prefix like
// "18.3-edge-type/". The two write the same partition; extracting it consistently
// is what makes the register agree.
var reSectionPrefix = regexp.MustCompile(`^(\d+(?:\.\d+)?)`)

// sectionNumber is the number heading a section like "18.3 <edge type ...>", or
// "" if the heading is empty or does not start with one. Rules under no section
// (the file preamble) get "", which sectionMatchesArea reports as unowned.
func sectionNumber(heading string) string {
	if m := reSectionPrefix.FindString(heading); m != "" {
		return m
	}
	return ""
}

// areaPrefixNumbers is each area's prefixes reduced to their leading section
// numbers. "18.9-value-type/scalar_" and "18.9-value-type/constructed_" both
// reduce to "18.9", so the D1/D2 split of the value-type clause shows up as two
// areas owning the same section — this is what makes notNull's carriers both.
func areaPrefixNumbers(t *testing.T) map[string][]string {
	t.Helper()

	out := make(map[string][]string, len(corpusAreas))
	for name, area := range corpusAreas {
		nums := make(map[string]bool)
		for _, prefix := range area.prefixes {
			num := reSectionPrefix.FindString(prefix)
			require.NotEmpty(t, num, "area %s prefix %q has no leading section number", name, prefix)
			nums[num] = true
		}
		out[name] = slices.Sorted(maps.Keys(nums))
	}
	return out
}

// sectionMatchesArea reports whether a section number lies within an area's number.
// "18.3" matches "18.3" exactly; "17.4" matches area A's "17" because "17" owns
// every 17.x subsection. Prefix on the dotted number rather than on the string,
// so "18.1" does not match "18.10".
func sectionMatchesArea(section, area string) bool {
	if section == "" || area == "" {
		return false
	}
	if section == area {
		return true
	}
	return strings.HasPrefix(section, area+".")
}

// corpusFilesByArea groups corpus files by the area whose prefix they match. A
// file matching no area is a test failure elsewhere (TestCorpusAreasAreDisjoint
// plus the per-entry check), so this only records the mapping.
func corpusFilesByArea(t *testing.T) map[string][]string {
	t.Helper()

	out := make(map[string][]string, len(corpusAreas))
	for _, file := range corpusFiles(t) {
		for name, area := range corpusAreas {
			for _, prefix := range area.prefixes {
				if strings.HasPrefix(file, prefix) {
					out[name] = append(out[name], file)
					break
				}
			}
		}
	}
	return out
}

// carriedByArea runs the coverage gate over each area's files independently and
// records which obligation names the area's files entered. The result maps area
// name to the three carried sets, so orphan detection is a set membership check
// with no re-parsing.
func carriedByArea(t *testing.T, filesByArea map[string][]string, obligation obligation) map[string]carriedSets {
	t.Helper()

	ruleNames, symbolicNames := parserNameTables()
	out := make(map[string]carriedSets, len(filesByArea))
	for area, files := range filesByArea {
		cov := newCoverage(ruleNames, symbolicNames, obligation.alternatives)
		for _, file := range files {
			src, err := os.ReadFile(filepath.Join(corpusDir, file))
			require.NoError(t, err)
			cov.merge(measureCoverage(t, string(src)))
		}
		out[area] = carriedSets{
			rules:        cov.rules,
			tokens:       cov.tokens,
			alternatives: cov.alternatives,
		}
	}
	return out
}

// carriedSets is what one area's files entered: the intersection with the
// obligation is what makes a name "carried by this area".
type carriedSets struct {
	rules        map[string]bool
	tokens       map[string]bool
	alternatives map[string]bool
}

// findOrphans returns required names whose owning areas exist but none carry the
// name. An empty owners set is skipped — that is either an exemption (which
// requireCarrierExemptions already rejects unless owners is empty) or an unowned
// name findUnowned reports separately.
func findOrphans(obligation obligation, owners map[string][]string, carried map[string]carriedSets) []orphan {
	var orphans []orphan

	check := func(name, kind string, sel func(carriedSets) map[string]bool) {
		areas := owners[name]
		if len(areas) == 0 {
			return
		}
		for _, area := range areas {
			if sel(carried[area])[name] {
				return
			}
		}
		orphans = append(orphans, orphan{name: name, kind: kind, owners: areas})
	}

	for rule := range obligation.rules {
		check(rule, "rule", func(c carriedSets) map[string]bool { return c.rules })
	}
	for token := range obligation.tokens {
		check(token, "token", func(c carriedSets) map[string]bool { return c.tokens })
	}
	for _, tag := range obligation.required {
		check(tag, "alt", func(c carriedSets) map[string]bool { return c.alternatives })
	}
	sort.Slice(orphans, func(i, j int) bool { return orphans[i].name < orphans[j].name })
	return orphans
}

// orphan is a required name whose owning area does not carry it.
type orphan struct {
	name   string
	kind   string
	owners []string
}

// findUnowned returns obligation names with no owning area that are also not
// exempted. Every such name needs an entry in carrierExemptions or the ownership
// model is wrong — see the note on that list. The exemption list rejects entries
// naming owned names, so the two failure modes cannot coincide.
func findUnowned(owners map[string][]string, exempt map[string]bool, obligation obligation) []string {
	var unowned []string
	consider := func(name string) {
		if exempt[name] {
			return
		}
		if len(owners[name]) == 0 {
			unowned = append(unowned, name)
		}
	}
	for rule := range obligation.rules {
		consider(rule)
	}
	for token := range obligation.tokens {
		consider(token)
	}
	for _, tag := range obligation.required {
		consider(tag)
	}
	sort.Strings(unowned)
	return unowned
}

// orphanReport lists each orphan with its owning areas and what those areas'
// carried sets look like for the name, so an author can see whether the fix is a
// file in their area or an ownership move. Grouped by area name so the report can
// be split by author the same way the worklist is.
func orphanReport(orphans []orphan, filesByArea map[string][]string) string {
	var out strings.Builder
	out.WriteString("orphans (a required name whose owning area carries no file for it):\n")
	for _, o := range orphans {
		fmt.Fprintf(&out, "  %s (%s) owned by %s\n", o.name, o.kind, strings.Join(o.owners, ", "))
		for _, area := range o.owners {
			fmt.Fprintf(&out, "    area %s has %d files under %v\n", area, len(filesByArea[area]), corpusAreas[area].prefixes)
		}
	}
	return out.String()
}

// unownedReport lists names whose section falls outside every area's prefixes and
// that no carrier exemption covers. Each needs an entry in carrierExemptions with
// a stated reason, or the ownership model needs to widen — the choice is a review
// question, so the report only names what needs answering.
func unownedReport(unowned []string) string {
	if len(unowned) == 0 {
		return ""
	}
	var out strings.Builder
	out.WriteString("unowned (no area's prefix covers the section, and no carrier exemption names it):\n")
	for _, name := range unowned {
		fmt.Fprintf(&out, "  %s\n", name)
	}
	return out.String()
}
