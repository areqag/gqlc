package gql

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestCorpusResolvingCarriers gates the second carriage class, the one
// TestCorpusAreaParseCarriers cannot see: a grammar name reached only by files
// the listener rejects. Those files enter every rule and token before the
// rejection, so parse-time carriage is satisfied while nothing downstream of the
// parse tree ever sees the name. That is the more dangerous class, because "the
// only files exercising this construct are ones we reject" reads as coverage in
// every report the parse-time instruments produce.
//
// It was found by running the h9n.17 gate against the two historical orphans it
// was meant to prevent: it reproduced the TIMESTAMP one and did not reproduce
// the NODE-synonym one (7217b180, "no resolving path exercised an explicit node
// synonym in nodeTypePattern"). Two gates, two inputs — this one runs over
// entries whose outcome is `resolves` and nothing else.
//
// Two tiers, because the two questions have different answers when they fail:
//
//   - Tier 1, corpus-wide: does ANY resolving file reach the name? Ownership is
//     irrelevant to this question, which is why it also covers the section-21
//     vocabulary names (NODE, VERTEX, EDGE, RELATIONSHIP and their synonym
//     rules) that no area owns by prefix and that the parse-time gate can only
//     exempt. A name failing tier 1 must appear in declinedCarriers with the
//     sentinel accounting for it.
//   - Tier 2, per-area: for a name some resolving file does reach, does its
//     OWNING area reach it? This has no register and no excuse. A resolving path
//     demonstrably exists, so a decline cannot explain the absence, and the fix
//     is always a file in the owning area. This is the NODE defect's exact shape.
func TestCorpusResolvingCarriers(t *testing.T) {
	obligation := grammarObligation(t)
	sections := scanRuleSections(t)
	owners := areaOwners(t, sections, obligation)

	byArea := resolvingFilesByArea(t)
	perArea := carriedByArea(t, byArea, obligation)

	var everything []string
	for _, files := range byArea {
		everything = append(everything, files...)
	}
	wide := carriedByArea(t, map[string][]string{"": everything}, obligation)[""]

	var noResolvingCarrier []declinedName
	var areaOrphans []orphan

	check := func(name, kind string, sel func(carriedSets) map[string]bool) {
		if !sel(wide)[name] {
			noResolvingCarrier = append(noResolvingCarrier, declinedName{name, kind})
			return
		}
		areaOrphans = append(areaOrphans, unmetGroups(name, kind, owners[name], sel, perArea)...)
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

	requireDeclinedCarriers(t, obligation, noResolvingCarrier)

	sort.Slice(areaOrphans, func(i, j int) bool {
		if areaOrphans[i].name != areaOrphans[j].name {
			return areaOrphans[i].name < areaOrphans[j].name
		}
		return areaOrphans[i].group.section < areaOrphans[j].group.section
	})
	require.Empty(t, areaOrphans, "%s", resolvingOrphanReport(areaOrphans, byArea))
}

// resolvingFilesByArea is corpusFilesByArea restricted to entries the parser
// resolves. It reads the manifest rather than the directory, because the outcome
// is the filter and only the manifest knows it.
func resolvingFilesByArea(t *testing.T) map[string][]string {
	t.Helper()

	out := make(map[string][]string, len(corpusAreas))
	for name, area := range corpusAreas {
		for _, entry := range area.entries {
			if entry.outcome == resolves {
				out[name] = append(out[name], entry.file)
			}
		}
	}
	return out
}

// declinedName is one obligation name and which of the three obligation sets it
// belongs to. The kind is what makes the register readable without
// cross-referencing, and it is checked rather than trusted.
type declinedName struct {
	name string
	kind string
}

// declinedCarriage accounts for the obligation names no resolving corpus file
// reaches, keyed by the sentinel that explains why. The join is what makes this
// a register rather than a duplicate of the sentinel list: names are computed
// from the corpus and only the *account* is written here, one per decline.
type declinedCarriage struct {
	sentinel error
	// bead is the work that would let this group shrink or go away. A decline
	// with no retirement path says so in why, so a reader does not hunt for a
	// bead that will never move it.
	bead  string
	why   string
	names []declinedName
}

// declinedCarriers answers the open question gqlc-h9n.26 was filed with: should
// the obligation set be narrowed to resolvable constructs, or does every
// deliberate rejection need an exemption? Measured before deciding, as the bead
// asked. Roughly a quarter of the obligation has no resolving carrier — the
// names below — and every one of them turned out to be carried exclusively by
// files bearing one of just seven sentinels.
//
// Neither listed option survives that measurement.
//
// Narrowing the obligation to what the corpus resolves is VACUOUS on the
// motivating case, which is the decisive argument rather than a matter of taste.
// 7217b180's commit message records that before it landed, every corpus file
// spelling NODE was an unsupported entry — so NODE had no resolving carrier, a
// corpus-derived narrowing would have dropped it from the obligation, and the
// gate would not have caught the defect it exists to catch. An obligation set
// calibrated from the thing it measures cannot report a gap in it.
//
// Per-name exemptions are the register the bead feared: one entry per name below,
// carrying that many copies of seven reasons.
//
// So: keep the full obligation, and derive the excuse from the sentinel instead
// of writing it per name. Each group states one account; the names under it are
// still listed, because the list is the pin — a name that gains a resolving
// carrier must be deleted from here or the gate goes red, which is the red-green
// signal that a decline was lifted. requireDeclinedCarriers checks the account
// too, not just the membership: a name is filed under the most specific sentinel
// that accounts for every one of its carriers, so a name cannot be parked under a
// decline that does not actually explain it.
//
// "Most specific" became a real question when ADR 0019 split ErrUnsupportedType
// five ways. Most names have one family and are filed there. A few do not: ANY
// spells both ANY VALUE and ANY NODE, the field-type rules serve RECORD and
// BINDING TABLE alike, and the angle brackets are shared by LIST and the closed
// unions. Those are filed under the class, and the class is the honest answer —
// they are genuinely carried by more than one declined family, and saying so is
// the fact about the grammar that a per-family filing would suppress.
var declinedCarriers = []declinedCarriage{
	{
		sentinel: ErrUndirectedEdge,
		bead:     "gqlc-0ri",
		why:      "graph.EdgeType has no undirectedness field, so an undirected arc cannot resolve to anything but a lie. ADR 0016 declined it permanently rather than deferring it: an undirected edge is a distinct element kind and the distinction is observable through IS DIRECTED, so a canonical direction would answer those queries wrongly rather than imprecisely. No retirement path.",
		names: []declinedName{
			{"arcTypeUndirected", "rule"},
			{"edgeTypePatternUndirected", "rule"},
			{"RIGHT_BRACKET_TILDE", "token"},
			{"TILDE_LEFT_BRACKET", "token"},
		},
	},
	{
		sentinel: ErrEdgeKindArcMismatch,
		bead:     "gqlc-0ri",
		why:      "The undirected connector spellings are reachable only through an edge type whose declared kind contradicts the arc, or through ErrUndirectedEdge above; either way the listener rejects before a model exists. Inherits that group's permanence — these names have no resolving path while undirectedness is unmodelled, and ADR 0016 settled that it stays so.",
		names: []declinedName{
			{"connectorUndirected", "rule"},
			{"endpointPairUndirected", "rule"},
			{"TILDE", "token"},
			{"UNDIRECTED", "token"},
		},
	},
	{
		sentinel: ErrUnnamedNodeType,
		bead:     "gqlc-0ri",
		why:      "Same shape as the edge case below - a node type with no key label has no identity to record. The alternative below is reached in endpoint position too, where the same missing identity surfaces as ErrUnknownEndpoint instead; see declinedAlso.",
		names: []declinedName{
			{"nodeTypeImpliedContent#2", "alt"},
		},
	},
	{
		sentinel: ErrUnnamedEdgeType,
		bead:     "gqlc-0ri",
		why:      "The implied-content alternative with no label set cannot resolve: an edge type with no key label has no identity to record. The decline is the model's identity rule, not a gap.",
		names: []declinedName{
			{"edgeTypeImpliedContent#2", "alt"},
		},
	},
	{
		sentinel: ErrLikeGraphSource,
		bead:     "gqlc-0ri",
		why:      "LIKE reaches session state a static generator cannot see, so it is declined permanently (ADR 0016). No retirement path - this group is expected to stay.",
		names: []declinedName{
			{"graphExpression", "rule"},
			{"graphTypeLikeGraph", "rule"},
			{"LIKE", "token"},
		},
	},
	// These four were one 29-name group keyed to ErrCopyOfSource until gqlc-h9n.1
	// gave COPY OF a catalogue to resolve against (ADR 0034). Twenty of the
	// twenty-nine gained resolving carriers in that commit and left; the nine that
	// remain are the spellings gqlc declines rather than the construct it had not
	// built, and they no longer share a reason. Keying each to its own sentinel is
	// what makes that legible — and no name here spans two of them, so none needs
	// declinedAlso.
	{
		sentinel: ErrReferenceParameter,
		bead:     "gqlc-0ri",
		why:      "A substituted parameter is bound at execution time and a build-time catalogue has no parameter values, so no catalogue reaches these. The two alternatives are one character of position apart — the parameter where the graph type goes, and where the schema goes — and neither discharges the other. No retirement path; this group is expected to stay.",
		names: []declinedName{
			{"graphTypeReference#2", "alt"},
			{"schemaReference#3", "alt"},
			{"referenceParameterSpecification", "rule"},
			{"SUBSTITUTED_PARAMETER_REFERENCE", "token"},
		},
	},
	{
		sentinel: ErrObjectParentReference,
		bead:     "gqlc-0ri",
		why:      "An object parent is a catalogue object containing other objects, and a directory-backed catalogue has no container between a directory and a file. The rule catalogObjectParentReference itself left this register — it resolves whenever its (objectName PERIOD)* repetition is taken zero times — so what is declined is the repetition and the name it introduces, not the parent. No retirement path without a catalogue model richer than a filesystem.",
		names: []declinedName{
			{"catalogObjectParentReference#2", "alt"},
			{"objectName", "rule"},
		},
	},
	{
		sentinel: ErrHomeSchemaReference,
		bead:     "gqlc-0ri",
		why:      "HOME_SCHEMA is a property of a session and gqlc has none. Its sibling CURRENT_SCHEMA left this register in the same commit, which is the whole distinction: CURRENT_SCHEMA has a static referent to translate to — the referencing file's own directory — and HOME_SCHEMA has none. No retirement path.",
		names: []declinedName{
			{"HOME_SCHEMA", "token"},
		},
	},
	{
		sentinel: ErrReferenceOutsideCatalogue,
		bead:     "gqlc-0ri",
		why:      "A climb is supported and resolves inside a chain; what it cannot do is leave the catalogue root. Carriage is measured per entry file and every entry spelling a climb is loaded as its own root, so the three here escape — while the same ../s/base resolves when copy_of_chain_climb.gql reaches sub/climber.gql as a hop. So this group records where the corpus can witness the climb from, not that the construct is unimplemented. It retires if an entry ever resolves while spelling a climb, which no single file can do.",
		names: []declinedName{
			{"relativeDirectoryPath", "rule"},
			{"DOUBLE_PERIOD", "token"},
		},
	},
	{
		sentinel: ErrPathValueType,
		bead:     "gqlc-0ri",
		why:      "A path is a traversal a query produces, not a value an element stores, so no model or backend change reaches it (ADR 0019). No retirement path - this group is expected to stay.",
		names: []declinedName{
			{"pathValueType", "rule"},
			{"PATH", "token"},
		},
	},
	{
		sentinel: ErrReferenceValueType,
		bead:     "gqlc-0ri",
		why:      "A reference is a handle into a graph rather than a value, and a property holding one would be a relationship no traversal can follow - graph.EdgeType is where gqlc keeps those. The binding table is here because it is ISO's fourth referenceValueType alternative, and a query result rather than stored data either way. Permanent (ADR 0019).",
		names: []declinedName{
			{"bindingTableReferenceValueType", "rule"},
			{"bindingTableType", "rule"},
			{"closedEdgeReferenceValueType", "rule"},
			{"closedGraphReferenceValueType", "rule"},
			{"closedNodeReferenceValueType", "rule"},
			{"edgeReferenceValueType", "rule"},
			{"graphReferenceValueType", "rule"},
			{"nodeReferenceValueType", "rule"},
			{"openEdgeReferenceValueType", "rule"},
			{"openGraphReferenceValueType", "rule"},
			{"openNodeReferenceValueType", "rule"},
			{"referenceValueType", "rule"},
			{"BINDING", "token"},
			{"TABLE", "token"},
		},
	},
	{
		sentinel: ErrImmaterialValueType,
		bead:     "gqlc-0ri",
		why:      "NULL admits only null, which schema.Property.Nullable already records, and the empty type admits nothing at all - a property of it could never be written or read. Permanent on the shape of the types themselves (ADR 0019).",
		names: []declinedName{
			{"emptyType#1", "alt"},
			{"emptyType", "rule"},
			{"immaterialValueType", "rule"},
			{"nullType", "rule"},
			{"NOTHING", "token"},
		},
	},
	{
		sentinel: ErrRecordValueType,
		bead:     "gqlc-h9n.33",
		why:      "A record is structured and graph.PropertyType is a flat enum, so there is nowhere to put the fields. Unimplemented rather than declined: gqlc-h9n.33 retires this group, and would retire the closed unions with it.",
		names: []declinedName{
			{"recordType#1", "alt"},
			{"recordType#2", "alt"},
			{"recordType", "rule"},
			{"RECORD", "token"},
		},
	},
	{
		sentinel: ErrDynamicUnionType,
		bead:     "gqlc-h9n.33",
		why:      "The closed unions (#9, #10) need the enum to carry members and are gqlc-h9n.33's. The open ones (#7, #8) are now implemented (ADR 0020), so valueType#7, valueType#8 and VALUE gained resolving carriers and left this group.",
		names: []declinedName{
			{"valueType#10", "alt"},
			{"valueType#9", "alt"},
			{"VERTICAL_BAR", "token"},
		},
	},
	{
		sentinel: ErrUnsupportedType,
		bead:     "gqlc-h9n.33",
		why:      "The four field-type grammar rules are carried by two declined families at once — RECORD (ErrRecordValueType) and binding-table references (ErrReferenceValueType) — so no leaf accounts for all of their carriers and the class is the most specific sentinel that does. LIST/ARRAY names moved out when gqlc-h9n.5 gave them resolving carriers; the angle and square brackets left with them for the same reason.",
		names: []declinedName{
			{"fieldName", "rule"},
			{"fieldType", "rule"},
			{"fieldTypeList", "rule"},
			{"fieldTypesSpecification", "rule"},
		},
	},
}

// declinedAlso is the escape the paragraph above leaves open for a name whose
// carriers span two declined families with NO class wrapping them. The five
// value-type families have ErrUnsupportedType and are filed there; these two have
// nothing, because they are not two readings of one rejection — they are one
// rejection reported from the two sides it can be seen from.
//
// nodeTypeImpliedContent#2 is a filler that is a property block and nothing else.
// Written as a node type declaration (18.2-node-type/pattern_properties_only.gql)
// it declares a type with no key label set, and ErrUnnamedNodeType says an
// unlabelled node type has no identity. Written as an edge endpoint
// (18.3-edge-type/endpoint_property_block_only.gql) it REFERENCES the type with
// the empty key label set, which by ADR 0018 no declaration can ever create, and
// ErrUnknownEndpoint says the referent does not exist. Same missing identity, one
// sentinel per side of it.
//
// A map keyed by name rather than a field on declinedName, so the fifty-odd
// entries above stay two-field literals and this stays visibly an exception: it
// should have one row, and a second should be argued rather than appended.
// requireDeclinedCarriers demands every sentinel here be exercised by a carrier,
// so a row cannot launder a filing that is simply wrong.
var declinedAlso = map[string][]error{
	"nodeTypeImpliedContent#2": {ErrUnknownEndpoint},
}

// requireDeclinedCarriers checks the register against what was measured, in both
// directions, and checks each entry's account. Bidirectional because the two
// failures are opposite and both silent: a name missing from the register is a
// construct nothing resolving exercises and nobody endorsed, and a name lingering
// in it is a decline that has since been lifted with the register left claiming
// otherwise.
func requireDeclinedCarriers(t *testing.T, obligation obligation, measured []declinedName) {
	t.Helper()

	carriers := parseTimeCarriersByName(t, obligation)
	sentinels := corpusSentinelOf(t)

	seen := make(map[string]bool, len(measured))
	var declared []declinedName
	for _, group := range declinedCarriers {
		require.Error(t, group.sentinel, "declined carriage group needs the sentinel accounting for it")
		require.True(t, isSentinel(group.sentinel),
			"declined carriage group names %v, which is not in allSentinels", group.sentinel)
		require.NotEmpty(t, group.bead, "declined carriage group %v needs a bead", group.sentinel)
		require.NotEmpty(t, group.why, "declined carriage group %v needs a reason a reviewer can weigh", group.sentinel)
		require.NotEmpty(t, group.names, "declined carriage group %v accounts for nothing; delete it", group.sentinel)

		for _, entry := range group.names {
			require.Contains(t, []string{"rule", "token", "alt"}, entry.kind,
				"declined name %q has invalid kind %q", entry.name, entry.kind)
			require.False(t, seen[entry.name], "declined name %q is accounted for twice", entry.name)
			seen[entry.name] = true
			declared = append(declared, entry)

			files := carriers[entry.name]
			require.NotEmpty(t, files,
				"declined name %q is reached by no corpus file at all, so no sentinel accounts for it; the coverage gate is what should be failing", entry.name)

			also := declinedAlso[entry.name]
			for _, other := range also {
				require.True(t, isSentinel(other),
					"declined name %q lists %v in declinedAlso, which is not in allSentinels", entry.name, other)
				require.NotEqual(t, group.sentinel, other,
					"declined name %q lists its own group's sentinel in declinedAlso", entry.name)
			}

			exact := make(map[error]bool, len(files))
			for _, file := range files {
				got := sentinels[file]
				require.Error(t, got,
					"declined name %q is filed under %v, but %s carries it and resolves; a name with a resolving carrier belongs in neither this register nor any other",
					entry.name, group.sentinel, file)
				accounted := errors.Is(got, group.sentinel)
				for _, other := range also {
					accounted = accounted || errors.Is(got, other)
				}
				require.True(t, accounted,
					"declined name %q is filed under %v, but %s carries it and rejects with %v",
					entry.name, group.sentinel, file, got)
				exact[got] = true
			}

			// An unexercised declinedAlso row is the failure mode that register
			// invites: it would let any name be filed anywhere by naming enough
			// sentinels. Each row has to be a carrier's actual rejection.
			for _, other := range also {
				carried := false
				for got := range exact {
					if errors.Is(got, other) {
						carried = true
					}
				}
				require.True(t, carried,
					"declined name %q lists %v in declinedAlso, but no carrier rejects with it; delete the row",
					entry.name, other)
			}
			// A name every carrier rejects with the same sentinel must be filed
			// under that sentinel, not under a class wrapping it. Without this the
			// register would satisfy itself by parking everything under
			// ErrUnsupportedType, which is the undifferentiated state ADR 0019 was
			// filed against. A name carried by two families has no such home, and
			// the class is then the honest answer rather than a lazy one.
			if len(exact) == 1 {
				for got := range exact {
					require.Equal(t, group.sentinel, got,
						"declined name %q is carried only by files rejecting with %v, so file it there rather than under %v",
						entry.name, got, group.sentinel)
				}
			}
		}
	}

	// The per-name checks above only reach a declinedAlso row whose name the
	// register still lists, so without this a row for anything else would sit
	// there unread — including one left behind by a name that gained a resolving
	// carrier and was deleted, which is the moment its exemption stops being true.
	for name := range declinedAlso {
		require.True(t, seen[name],
			"declinedAlso has a row for %q, which no declined-carriage group names; it accounts for nothing", name)
	}

	require.ElementsMatch(t, measured, declared,
		"the declined-carriage register must name exactly the obligation names no resolving file reaches: "+
			"add a name under the sentinel that rejects every file carrying it, or delete one that has gained a resolving carrier")
}

// parseTimeCarriersByName inverts per-file coverage: obligation name to the
// corpus files whose parse tree enters it. Measured over every file rather than
// the resolving subset, because the register's claim is about which sentinel the
// carriers reject with, and a resolving carrier would put the name outside the
// register entirely.
func parseTimeCarriersByName(t *testing.T, obligation obligation) map[string][]string {
	t.Helper()

	ruleNames, symbolicNames := parserNameTables()
	out := make(map[string][]string)
	for _, file := range corpusFiles(t) {
		src, err := os.ReadFile(filepath.Join(corpusDir, file))
		require.NoError(t, err)
		cov := newCoverage(ruleNames, symbolicNames, obligation.alternatives)
		cov.merge(measureCoverage(t, string(src)))
		for _, set := range []map[string]bool{cov.rules, cov.tokens, cov.alternatives} {
			for name := range set {
				out[name] = append(out[name], file)
			}
		}
	}
	for name := range out {
		sort.Strings(out[name])
	}
	return out
}

// corpusSentinelOf maps each corpus file to the sentinel its entry pins, or nil
// for a resolving entry.
func corpusSentinelOf(t *testing.T) map[string]error {
	t.Helper()

	out := make(map[string]error)
	for _, area := range corpusAreas {
		for _, entry := range area.entries {
			out[entry.file] = entry.sentinel
		}
	}
	return out
}

// resolvingOrphanReport names the tier-2 failures and the areas that must gain a
// file. There is deliberately no "or add an exemption" branch in the message: a
// resolving file elsewhere already proves the construct resolves, so the only
// honest fix is a file in the owning area.
func resolvingOrphanReport(orphans []orphan, byArea map[string][]string) string {
	if len(orphans) == 0 {
		return ""
	}
	var out strings.Builder
	fmt.Fprintf(&out, "%d name(s) reached by a resolving file in some other area, but by no resolving file in the area that owns them.\n", len(orphans))
	out.WriteString("A resolving path exists, so no decline explains this: the owning area needs a file that exercises the construct and resolves.\n")
	for _, o := range orphans {
		fmt.Fprintf(&out, "  %s (%s) named under section %s, owned there by %s\n",
			o.name, o.kind, o.group.section, strings.Join(o.group.areas, ", "))
		for _, area := range o.group.areas {
			fmt.Fprintf(&out, "    area %s has %d resolving files under %v\n",
				area, len(byArea[area]), corpusAreas[area].prefixes)
		}
	}
	return out.String()
}
