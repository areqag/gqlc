package backends_test

import (
	"errors"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/areqag/gqlc/internal/cli/backends"
	"github.com/areqag/gqlc/internal/codegen"
	"github.com/areqag/gqlc/internal/codegen/typescan"
	"github.com/areqag/gqlc/internal/graph"
	"github.com/areqag/gqlc/internal/schema"
)

// selfName is the phrase a backend must call itself by when it refuses a
// declared property some other enrolled backend accepts. Keyed by
// registry key, and the sweep below fails on a key with no row, so a new
// backend declares its name here rather than joining the roster unnamed.
//
// Two keys share one phrase because they share one backend package: the
// v5 and v6 neo4j entries differ in driver surface, not in what they
// refuse.
var selfName = map[string]string{
	"neo4j-go-v5":       "the neo4j backend",
	"neo4j-go-v6":       "the neo4j backend",
	"apache-age-pgx-v5": "the Apache AGE backend",
}

// TestAContingentRefusalNamesItsBackend holds the rule that decides which
// refusals carry a backend name (bd gqlc-fkdwq, ADR 0035): a backend
// names itself exactly when another enrolled backend answers the same
// declaration differently.
//
// Attribution implicates contingency. Naming a backend tells the author
// "this is this backend's answer, and another may differ", so it is owed
// wherever the targets disagree — a run emitting several of them fails on
// some and succeeds on others, and the name is the only thing that says
// which. Where every target refuses, the declaration is the obstacle and
// no name is owed.
//
// This is the composition root because it is the only layer that knows
// the enrolled roster. The backend packages cannot see each other, and
// the conformance suite deliberately imports no single backend, so
// neither can hold a claim about how two of them differ.
//
// The sweep runs over user-facing generation rather than over the type
// tables, which is what lets one assertion cover refusals from unrelated
// sentinels: today it holds ErrUnrepresentableWidth on AGE's BYTES and
// ErrUnstorableProperty on neo4j's nested list with the same line.
//
// Since bd gqlc-oxgyt it asserts the converse too: a refusal no other
// enrolled target answers differently carries no name. That is the half
// the rule needs to be a rule rather than a one-way obligation — without
// it, a backend that suffixed its name to every refusal it ever raised
// would pass.
//
// CONTINGENCY IS A PROPERTY OF A REFUSAL, NOT OF A WIDTH (bd gqlc-r0yy).
// The converse was first stated over widths — a width every target
// refuses CITING ONE SENTINEL carries no name — and that scoping left
// nine of this sweep's widths asserted by neither half. LIST<LIST<BYTES>>
// and its record-shaped siblings are refused by every target under two
// different sentinels: AGE has no carrier for BYTES, neo4j will not store
// a nested value. Each of those two refusals is itself contingent, so
// each name is owed — neo4j carries BYTES, and AGE stores nested lists —
// and it is only their CONJUNCTION over one width that is unanimous. A
// width-scoped converse can only call that a shared refusal or say
// nothing, and it said nothing.
//
// So the question is asked once per refusal instead: did any other
// enrolled target answer THIS declaration with something other than THIS
// refusal's sentinel set? That covers every unanimously-refused width
// uniformly and subsumes the sentinel-agreement scoping, which is its
// contraction — where every target refuses under one sentinel no target
// dissents, and the converse fires exactly as before.
//
// It needs nothing new from codegen. The claim a refusal makes is the
// sentinel it cites, which errors.Is already exposes; codegen.RefusedWidth
// answers a different question (which WIDTH), for a different caller
// (a backend deciding at emit time whether to name itself).
func TestAContingentRefusalNamesItsBackend(t *testing.T) {
	reg, err := backends.Registry()
	require.NoError(t, err)

	keys := reg.Keys()
	require.NotEmpty(t, keys, "an empty roster leaves every refusal without a dissenter, so the sweep below asserts nothing")
	for _, key := range keys {
		require.Contains(t, selfName, key,
			"registry key %q declares no phrase to call itself by, so a refusal of its own could not be told from a shared one", key)
	}

	var contested, divergent, shared []graph.PropertyType
	for _, pt := range declaredWidths(t) {
		answers := answersByTarget(t, reg, keys, pt)
		accepted, sentinels := tallyAnswers(answers)
		switch {
		case len(sentinels) == 0:
			// Every target emitted it. There is no refusal to attribute.
			continue
		case len(accepted) > 0:
			contested = append(contested, pt)
		case len(sentinels) > 1:
			divergent = append(divergent, pt)
		default:
			shared = append(shared, pt)
		}
		requireAttribution(t, pt, answers)
	}

	// The three buckets are bookkeeping, not three rules — every row above
	// went through the one requireAttribution call. They are guarded
	// separately because they are the three distinct ways the rule can
	// reach a verdict, and a bucket that empties takes its evidence path
	// out of the sweep without failing anything.
	require.NotEmpty(t, contested,
		"no declared width divides the roster, so no refusal above owed its name because another target ACCEPTED the declaration")
	require.NotEmpty(t, divergent,
		"no declared width is refused by every enrolled target under two sentinels, so no refusal above owed its name "+
			"because another target REFUSED the declaration for a different reason — the evidence path bd gqlc-r0yy "+
			"was filed for, and the one a width-scoped converse could not see. If a type table legitimately closed "+
			"that gap, delete this guard; it is here so the gap cannot close silently")
	require.NotEmpty(t, shared,
		"no declared width is refused by every enrolled target with no dissenter, so the converse — that such a refusal "+
			"carries NO name — certified nothing")
	t.Logf("widths dividing the roster: %v", contested)
	t.Logf("widths every target refuses, under two or more sentinels: %v", divergent)
	t.Logf("widths every target refuses under one sentinel: %v", shared)
}

// answer is what one enrolled target said about one declaration: either
// it emitted, or it refused with an error citing a sentinel set.
//
// The sentinel set rather than a single sentinel because errors.Is is not
// exclusive, and joined from a sorted slice so two refusals citing the
// same pair in either order compare equal.
type answer struct {
	accepted  bool
	err       error
	sentinels string
}

// differsFrom reports whether a answers the declaration with something
// other than what b said. This is the whole of ADR 0035's contingency
// test, asked between two targets.
func (a answer) differsFrom(b answer) bool {
	if a.accepted != b.accepted {
		return true
	}
	return a.sentinels != b.sentinels
}

// describe renders an answer for a failure message, which is the only
// caller: a row that fails wants to print the dissent it turned on.
func (a answer) describe() string {
	if a.accepted {
		return "emitted it"
	}
	return "refused it citing " + a.sentinels
}

// requireAttribution holds ADR 0035's rule over every refusal of one
// declaration: a refusal names its backend exactly when some other
// enrolled target answered that declaration with something other than
// this refusal's sentinel set.
func requireAttribution(t *testing.T, pt graph.PropertyType, answers map[string]answer) {
	t.Helper()

	for _, key := range slices.Sorted(maps.Keys(answers)) {
		mine := answers[key]
		if mine.accepted {
			continue
		}
		other, dissents := dissenter(answers, key)
		if dissents {
			require.ErrorContains(t, mine.err, selfName[key],
				"%s is refused by %s citing %s, and %s %s, so another target does differ and the message has to say "+
					"which one refused; it reads %q",
				pt, key, mine.sentinels, other, answers[other].describe(), mine.err.Error())
			continue
		}
		require.NotContains(t, mine.err.Error(), selfName[key],
			"%s is refused by every enrolled target citing %s, so the declaration is the obstacle and naming %s tells "+
				"the author another target may differ when none does; it reads %q",
			pt, mine.sentinels, selfName[key], mine.err.Error())
	}
}

// dissenter returns an enrolled target that answered this declaration
// with something other than key's refusal, and whether there is one.
//
// Its identity is returned rather than a bool alone because it is the
// whole of the evidence the rule turns on, and a failing row that says
// which target dissented is the difference between a diagnosis and a
// re-measurement. Iterated in sorted key order so the witness a given
// roster names does not move between runs.
func dissenter(answers map[string]answer, key string) (string, bool) {
	mine := answers[key]
	for _, other := range slices.Sorted(maps.Keys(answers)) {
		if other != key && answers[other].differsFrom(mine) {
			return other, true
		}
	}
	return "", false
}

// tallyAnswers reports the keys that emitted, and the distinct sentinel
// sets cited among those that refused. Both are for bucketing and for the
// reach guards; the rule itself is asked per refusal by requireAttribution.
func tallyAnswers(answers map[string]answer) (accepted, sentinels []string) {
	seen := make(map[string]struct{})
	for key, a := range answers {
		if a.accepted {
			accepted = append(accepted, key)
			continue
		}
		seen[a.sentinels] = struct{}{}
	}
	slices.Sort(accepted)
	return accepted, slices.Sorted(maps.Keys(seen))
}

// answersByTarget generates a one-property schema carrying pt for every
// enrolled target and records what each one answered.
func answersByTarget(t *testing.T, reg codegen.Registry, keys []string, pt graph.PropertyType) map[string]answer {
	t.Helper()

	answers := make(map[string]answer, len(keys))
	for _, key := range keys {
		newGen, ok := reg.Lookup(key)
		require.True(t, ok, "registry key %q reported by Keys does not resolve through Lookup", key)

		files, err := newGen("widths").Generate(codegen.Input{Schema: schemaWithPayload(pt)})
		if err != nil {
			answers[key] = answer{err: err, sentinels: citedSentinels(t, pt, key, err)}
			continue
		}
		require.NotEmpty(t, files, "%s emitted no files at %s and returned no error, so neither verdict is recorded", pt, key)
		answers[key] = answer{accepted: true}
	}
	return answers
}

// citedSentinels renders the sentinel set one refusal cites, as the
// joined key two refusals are compared by.
//
// A refusal citing NO sentinel fails here rather than yielding an empty
// key. It would otherwise compare equal to every other sentinel-less
// refusal and let a coincidence read as agreement — and the sweep
// declares no queries, so every refusal it can raise comes from a schema
// phase, all of which the taxonomy (docs/specs/
// codegen-sentinel-taxonomy.md) requires to carry one.
func citedSentinels(t *testing.T, pt graph.PropertyType, key string, err error) string {
	t.Helper()

	var cited []string
	for _, sentinel := range codegen.AllSentinels() {
		if errors.Is(err, sentinel) {
			cited = append(cited, sentinel.Error())
		}
	}
	require.NotEmpty(t, cited,
		"%s is refused by %s citing none of the codegen sentinels, so its reason cannot be compared with the "+
			"other targets'; it reads %q", pt, key, err.Error())
	slices.Sort(cited)
	return strings.Join(cited, "+")
}

// graphPropertyTypeSource is where internal/graph declares the property
// types this sweep ranges over. The vocabulary is read off that
// declaration rather than restated here, which is the same gate
// internal/codegen/age, internal/codegen/neo4j and internal/resolver
// hold their own tables with.
const graphPropertyTypeSource = "../../graph/propertytype.go"

// declaredWidths is graph's property-type vocabulary, each width also in
// its flat-list and nested-list forms. The nesting is not decoration:
// LIST<LIST<T>> is where neo4j's storage refusal lives, and it is what
// puts a second sentinel under the one rule above.
//
// The vocabulary is READ rather than listed, because graph exports none
// at run time — PropertyType is an open string type, so nothing
// enumerates its constants but a walk over the source that declares
// them. It was listed by hand until bd gqlc-tn96, and that list went
// stale the day PR #2842 added graph.TypeUUID: the sweep ran 32 of 33
// declared widths for a fortnight and nothing said so, because a
// hand-written list cannot notice what it omits. Now a width added
// upstream joins the sweep with no edit here, or the read fails and
// takes the test with it.
//
// TypeList is spelled LIST<ANY>, so it and ListOf(TypeAnyPropertyValue)
// are the same width reached two ways and a couple of rows repeat. The
// duplication is the constant block's, not a miscount here.
func declaredWidths(t *testing.T) []graph.PropertyType {
	t.Helper()

	vocabulary, err := typescan.PropertyTypes(graphPropertyTypeSource)
	require.NoError(t, err,
		"%s is the artefact this sweep reads its vocabulary from; without it there is nothing to sweep", graphPropertyTypeSource)
	require.NotEmptyf(t, vocabulary,
		"read no PropertyType constants out of %s, so this sweep would range over the four hand-built widths below "+
			"and assert nothing about any declared one", graphPropertyTypeSource)

	// Sorted so the widths a failing run names, and the three bucket
	// logs below, do not reorder between runs over one vocabulary.
	declared := slices.Sorted(maps.Keys(vocabulary))

	widths := make([]graph.PropertyType, 0, 5*len(declared)+4)
	for _, pt := range declared {
		flat := graph.ListOf(pt, false)
		widths = append(widths, pt, flat, graph.ListOf(flat, false))
		// Each scalar also as the single field of a record, and that
		// record as a list element. A record inherits its fields'
		// refusals, so these carry the same contest the bare width does
		// — and they carry one the bare width does NOT: the widths AGE
		// admits as a property but refuses in a container, which is
		// every zoned temporal one. Before records emitted, LIST<TIME>
		// was the only encoding that divided the roster that way.
		rec := graph.RecordOf([]graph.RecordField{{Name: "f", Type: pt, NotNull: true}})
		widths = append(widths, rec, graph.ListOf(rec, false))
	}
	// The field-less record. It has no field to inherit a refusal from, so
	// what divides the roster over it is whatever a backend says about
	// records AS SUCH — which is where neo4j's storage answer lands.
	// RECORD<> is built rather than declared, so the read above cannot
	// reach it. Its sibling RECORD<ANY> is not named here, because the
	// loop does reach that one: it is a declared constant, and until bd
	// gqlc-9xiz it was held out of the loop's container expansion — two
	// of its container forms made AGE emit Go that did not parse.
	widths = append(widths, graph.RecordOf(nil))

	// Two closed unions, and they are here for opposite reasons.
	//
	// UNION<DATE|STRING> is the width spec §8 names as the third
	// falsifier: neo4j admits it because the driver hands back a
	// dbtype.Date that no string can be mistaken for, and AGE refuses it
	// because agtype has no date scalar and a DATE is ISO text
	// indistinguishable on the wire from a STRING. So it divides the
	// roster, and AGE's refusal of it is contingent — the rule above
	// obliges that message to name AGE, which is the ADR 0035 half the
	// tripwire in union_test.go stood in for while nothing generated a
	// union at all.
	//
	// UNION<BOOL|INT64> is the control that makes the first row's dissent
	// mean something: its members land in distinct wire families on BOTH
	// backends, so both emit it and there is no refusal to attribute.
	// Without it a reader could not tell whether AGE refuses this union's
	// members or refuses closed unions as such.
	widths = append(widths,
		graph.UnionOf([]graph.UnionMember{{Type: graph.TypeDate}, {Type: graph.TypeString}}),
		graph.UnionOf([]graph.UnionMember{{Type: graph.TypeBool}, {Type: graph.TypeInt64}}),
	)
	return widths
}

// schemaWithPayload is a one-node schema whose single property carries
// pt, so the entity sweep is the only thing that can refuse.
func schemaWithPayload(pt graph.PropertyType) schema.Schema {
	labels := graph.LabelSetKey("Blob")
	return schema.Schema{
		Name: "Widths",
		Nodes: map[graph.LabelSetKey]schema.NodeType{
			labels: {
				KeyLabels:      labels,
				CompleteLabels: labels,
				Name:           "Blob",
				Properties: map[string]schema.Property{
					"payload": {Name: "payload", Type: pt},
				},
			},
		},
	}
}
