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
// Since bd gqlc-oxgyt it asserts the converse too, on the rows where the
// converse is well posed: a width every target refuses FOR THE SAME
// REASON carries no name anywhere. That is the half the rule needs to be
// a rule rather than a one-way obligation — without it, a backend that
// suffixed its name to every refusal it ever raised would pass.
//
// "For the same reason" is the sentinel each refusal cites, and it is a
// real restriction rather than a hedge. Nine of this sweep's widths are
// refused by every target under two DIFFERENT sentinels —
// LIST<LIST<BYTES>> and its record-shaped siblings, where AGE has no
// carrier for BYTES and neo4j will not store a nested value. Each of
// those two refusals is itself contingent: neo4j carries BYTES, and AGE
// stores nested lists. So both names are owed, and it is only their
// CONJUNCTION over one width that is unanimous — a shape the converse as
// stated would wrongly call a shared refusal. Those rows are logged and
// left unasserted; asserting them needs a per-refusal notion of
// contingency this layer cannot compute (bd gqlc-r0yy).
func TestAContingentRefusalNamesItsBackend(t *testing.T) {
	reg, err := backends.Registry()
	require.NoError(t, err)

	keys := reg.Keys()
	require.NotEmpty(t, keys, "an empty roster makes every width unanimous, so the sweep below asserts nothing")
	for _, key := range keys {
		require.Contains(t, selfName, key,
			"registry key %q declares no phrase to call itself by, so a refusal of its own could not be told from a shared one", key)
	}

	var contested, shared, divergent []graph.PropertyType
	for _, pt := range declaredWidths() {
		accepted, refused := partitionByVerdict(t, reg, keys, pt)
		switch {
		case len(refused) == 0:
			// Every target emitted it. There is no refusal to attribute.
		case len(accepted) > 0:
			contested = append(contested, pt)
			for key, err := range refused {
				require.ErrorContains(t, err, selfName[key],
					"%s is refused by %s and accepted by %v, so the message has to say which target refused; it reads %q",
					pt, key, accepted, err.Error())
			}
		case len(citedSentinels(t, pt, refused)) == 1:
			shared = append(shared, pt)
			for key, err := range refused {
				require.NotContains(t, err.Error(), selfName[key],
					"%s is refused by every enrolled target under one sentinel, so the declaration is the obstacle "+
						"and naming %s tells the author another target may differ when none does; it reads %q",
					pt, key, err.Error())
			}
		default:
			divergent = append(divergent, pt)
		}
	}

	require.NotEmpty(t, contested,
		"no declared width divides the roster, so every row above was unanimous and the naming obligation certified nothing")
	require.NotEmpty(t, shared,
		"no declared width is refused by every target under one sentinel, so the converse above certified nothing")
	t.Logf("widths dividing the roster: %v", contested)
	t.Logf("widths every target refuses for the same reason: %v", shared)
	t.Logf("widths every target refuses for DIFFERENT reasons, unasserted per bd gqlc-r0yy: %v", divergent)
}

// citedSentinels reports the distinct sentinel sets the refusals cite, so
// the caller can tell one shared reason from several coincident ones. The
// set rather than a single sentinel because errors.Is is not exclusive,
// and sorted so two keys citing the same pair in either order agree.
//
// A refusal citing NO sentinel fails here rather than being folded in
// with the others. It would otherwise compare equal to every other
// sentinel-less refusal and let a coincidence read as a shared reason —
// and the sweep declares no queries, so every refusal it can raise comes
// from a schema phase, all of which the taxonomy (docs/specs/
// codegen-sentinel-taxonomy.md) requires to carry one.
func citedSentinels(t *testing.T, pt graph.PropertyType, refused map[string]error) []string {
	t.Helper()

	seen := make(map[string]struct{})
	for key, err := range refused {
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
		seen[strings.Join(cited, "+")] = struct{}{}
	}
	return slices.Sorted(maps.Keys(seen))
}

// partitionByVerdict generates a one-property schema carrying pt for
// every enrolled target, returning the keys that emitted it and the
// errors of those that refused.
func partitionByVerdict(t *testing.T, reg codegen.Registry, keys []string, pt graph.PropertyType) ([]string, map[string]error) {
	t.Helper()

	var accepted []string
	refused := make(map[string]error)
	for _, key := range keys {
		newGen, ok := reg.Lookup(key)
		require.True(t, ok, "registry key %q reported by Keys does not resolve through Lookup", key)

		files, err := newGen("widths").Generate(codegen.Input{Schema: schemaWithPayload(pt)})
		if err != nil {
			refused[key] = err
			continue
		}
		require.NotEmpty(t, files, "%s emitted no files at %s and returned no error, so neither verdict is recorded", pt, key)
		accepted = append(accepted, key)
	}
	return accepted, refused
}

// declaredWidths is graph's property-type vocabulary, each width also in
// its flat-list and nested-list forms. The nesting is not decoration:
// LIST<LIST<T>> is where neo4j's storage refusal lives, and it is what
// puts a second sentinel under the one rule above.
//
// Hand-enumerated because graph exports no vocabulary; the backend type
// tables' own tests enumerate it the same way. A width added to graph
// without a row here is swept by nothing, which is a gap this sweep
// shares with them rather than one it introduces.
//
// TypeList is spelled LIST<ANY>, so it and ListOf(TypeAnyPropertyValue)
// are the same width reached two ways and a couple of rows repeat. The
// duplication is the constant block's, not a miscount here.
func declaredWidths() []graph.PropertyType {
	scalars := []graph.PropertyType{
		graph.TypeString, graph.TypeBytes, graph.TypeBool,
		graph.TypeDate, graph.TypeTime, graph.TypeLocalTime,
		graph.TypeTimestamp, graph.TypeDuration,
		graph.TypeInt, graph.TypeInt8, graph.TypeInt16, graph.TypeInt32,
		graph.TypeInt64, graph.TypeInt128, graph.TypeInt256,
		graph.TypeUint, graph.TypeUint8, graph.TypeUint16, graph.TypeUint32,
		graph.TypeUint64, graph.TypeUint128, graph.TypeUint256,
		graph.TypeFloat, graph.TypeFloat16, graph.TypeFloat32,
		graph.TypeFloat64, graph.TypeFloat128, graph.TypeFloat256,
		graph.TypeDecimal, graph.TypeAnyPropertyValue, graph.TypeList,
	}

	widths := make([]graph.PropertyType, 0, 5*len(scalars)+2)
	for _, pt := range scalars {
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
	// The two records with no declared fields. Neither has a field to
	// inherit a refusal from, so what divides the roster over them is
	// whatever a backend says about records AS SUCH — which is where
	// neo4j's storage answer lands.
	widths = append(widths, graph.RecordOf(nil), graph.TypeAnyRecord)
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
