package conformance_test

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"maps"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/stretchr/testify/require"

	"github.com/areqag/gqlc/internal/codegen"
	"github.com/areqag/gqlc/internal/graph"
	"github.com/areqag/gqlc/internal/schema"
)

// wireScalarSpellings is the closed vocabulary of strings a package-level
// function that decodes no entity may compare for equality: the spellings
// the wire format fixes for its own scalars. AGE renders a boolean and a
// null as these three words and agtypeBool and agtypeValue switch on them,
// so they are the wire's, not the schema's — no label can be one and no
// author picks one.
//
// It is what makes the sweep's classification total. Every package-level,
// receiver-less function an emission writes is either an entity decoder,
// which is graded against the labels its own axis can carry, or a function
// whose every string comparison is in here. A third kind — a function
// testing a string this gate has no verdict about — is an *ungraded* site
// and fails, rather than being passed over. Passing it over is what made
// the extractor's blind spot invisible: a decoder it did not recognise
// dropped out of the census, out of the guard count and out of the
// alphabet check at once, and the only thing left standing under it was a
// golden byte diff. A backend that grows a new wire spelling reddens until
// it is named here, which makes the widening a decision someone makes
// rather than one already made.
var wireScalarSpellings = map[string]bool{
	"true":  true,
	"false": true,
	"null":  true,
}

// labelGuardingTargets records, per registered target, whether that
// backend's entity decoders gate on the wire label.
//
// apache-age-pgx-v5 does: a vertex and an edge are the same agtype object
// but for the annotation, so the label is the only thing in the value that
// says which entity type it is, and every decoder checks it. The neo4j
// targets do not: their decoders take a dbtype.Node or a
// dbtype.Relationship and read properties off it without consulting its
// labels at all. That asymmetry is why the invariant surface cannot be the
// gate here — it is exactly the encode/decode divergence the corpus exists
// to permit.
//
// Both halves are checked. A target recorded true must guard *every*
// entity decoder it emits anywhere in the corpus, so an emission that
// stopped writing the check reddens rather than passing a sweep that now
// finds nothing to look at. A target recorded false must guard none, so a
// backend that grows a label guard reddens until it is enrolled here and
// its guards come under the reachability rule. The key set is held equal
// to the registry's, so a newly registered backend cannot join the corpus
// without a verdict.
var labelGuardingTargets = map[string]bool{
	"apache-age-pgx-v5": true,
	"neo4j-go-v5":       false,
	"neo4j-go-v6":       false,
}

// multiLabelEmittingTargets records, per registered target, whether that
// backend emits for a schema declaring an entity keyed on more than one
// label, or refuses it.
//
// This ledger is what stops the reachability sweep's quiet arm from being
// unexamined. A backend that refuses a schema emits no decoder, so it
// contributes nothing to check, and "nothing to check" and "checked and
// clean" are the same green. Recording the verdict makes the silence an
// assertion: apache-age-pgx-v5 is recorded as refusing, so the day it
// starts emitting for a two-label node type this reddens whether or not
// the guard it writes happens to be one the extractor below understands.
//
// It is per target and not per fixture because the posture is a property
// of the backend's wire, not of the schema: AGE stamps exactly one label
// per vertex and per edge whatever the schema says. A multi-label fixture
// some backend treated differently from the others would redden here and
// force the ledger to be refined rather than silently averaging them.
var multiLabelEmittingTargets = map[string]bool{
	"apache-age-pgx-v5": false,
	"neo4j-go-v5":       true,
	"neo4j-go-v6":       true,
}

// shapeWords is what to call one entity axis in a diagnostic: what the
// schema calls the type, and what to call a value that carries its label.
// Both are backend-neutral — a node rather than AGE's vertex — because
// every registered target's decoders are swept and the wording has to
// read for the one whose carrier is a dbtype.Node too.
type shapeWords struct{ typeText, valueText string }

// entityShapeWords is the sweep's ledger of entity axes. It is what makes
// the per-shape alphabet decidable: a codegen.EntityKind this map does
// not hold is one the sweep can neither describe nor assign an alphabet
// to, so a prepared entity carrying an unnamed kind reddens in
// preparedEntityShapes rather than being held to a nil set.
//
// The census iterates its keys, so a third axis added here without an
// alphabet arm in schemaLabelAlphabet fails loudly on the first decoder
// that fills it rather than going unaudited.
var entityShapeWords = map[codegen.EntityKind]shapeWords{
	codegen.EntityNode: {typeText: "a node type", valueText: "a node"},
	codegen.EntityEdge: {typeText: "an edge type", valueText: "an edge"},
}

// entityDecoder is one emitted entity decoder: the function and file it
// is written as, the entity it fills, the axis that entity sits on, and
// the string literals its body tests the wire value for equality against.
//
// fn is carried rather than re-derived from entity because the sweep does
// not decide what a decoder is called — it decides what one returns — so
// the diagnostic has to name the function the emission actually wrote.
type entityDecoder struct {
	fn     string
	file   string
	entity string
	shape  codegen.EntityKind
	guards []string
}

// TestEmittedDecodersGuardOnlyOnStampableLabels is the reachability gate:
// no decoder a backend emits may carry a label guard that no value that
// backend can produce could satisfy.
//
// # What it decides
//
// Exactly one shape, and it is worth being precise about which, because a
// gate that claims more than it decides is worse than a narrow one that is
// honest. For every valid fixture and every registered backend — not only
// the targets the fixture's manifest enrols, see below — it takes the
// emitted package, finds every entity decoder in it, and collects the
// string literals that function compares for equality (an == or != operand,
// or a switch case value). Every such literal must be a label some value
// that decoder can be handed carries.
//
// # How it finds a decoder, and why not by name
//
// By the entity type the function returns, not by what it is called. A
// function fills an entity struct by returning one, so the emission's own
// signature says which entity a package-level, receiver-less function
// decodes, and codegen.Prepare says which axis that entity sits on.
//
// The recognition used to be the name pattern decode<T> for a declared T,
// and that was the gate's own vacuity. A function the pattern failed to
// resolve was not merely unread: it left the per-axis census, the guard
// count and the alphabet check *simultaneously*, so the extractor's
// failure to recognise a decoder removed it from the numerator, the
// denominator and the correctness check at once. Emitting one entity's
// decoder as decodePostRecord with a guard on a label no backend stamps
// left this gate green; the only red was a TestValid golden byte diff,
// which go test -update blesses away. Broadening the pattern until that
// name matched would have been the same shape one rename later.
//
// So the sweep classifies *every* package-level, receiver-less function
// the emission writes, and the classification is total:
//
//   - one that returns an entity codegen.Prepare names is that entity's
//     decoder, and its guards are graded;
//   - one that returns none may compare only wireScalarSpellings, the
//     wire's own fixed spellings;
//   - anything else is an ungraded site and fails. A function returning
//     two entities has no single axis to be graded against; a function
//     returning none while testing some other string is a guard no verdict
//     covers.
//
// Both directions are reconciled per emission: the entities decoded must
// be exactly the entities codegen.Prepare names, one decoder each. So a
// decoder rendered under any name at all is still read, and a decoder
// rendered in a shape the sweep cannot read leaves its entity undecoded
// and reddens by that absence. An unrecognised decoder is an ungraded
// site, not an absent one.
//
// # Why the alphabet is per axis and not per schema
//
// A vertex carries a node type's key label; an edge carries its
// relationship type. They are disjoint alphabets on the wire and on AGE
// there is no way to put one on the other — a MATCH binding an edge can
// only ever hand the decoder a value labelled with a relationship type.
// So the alphabet a decoder is held to is the one for the axis its own
// entity sits on, read off codegen.Prepare, and not the union of the two.
// A union would pass decodeCompanyKnows guarding on "Company": a node
// label, on a decoder no vertex ever reaches, unsatisfiable at exit 0 and
// invisible to a gate that only asks whether the schema mentions the
// string somewhere.
//
// A literal outside its axis's alphabet is a guard the decoder can never
// pass. The other concrete instance is graph.LabelSetKey's own join
// spelling: a node type keyed on Employee and Person has key
// "Employee&Person", and a decoder guarding on that string is asking a
// single wire label to equal the join of two. No vertex and no edge
// carries it, and on Apache AGE none could — AGE stamps exactly one label
// and has no syntax for a second — so the decoder is dead on arrival while
// gqlc generate still exits 0.
//
// # What it does not decide
//
// It is not a reachability analysis. It says nothing about guards that are
// not string equality — no arithmetic, no length, no nil check, no
// interprocedural condition. It says nothing about a conjunction: two
// guards each in the axis's alphabet can still be jointly unsatisfiable,
// and this reads them one at a time (gqlc-eo74). And it decides
// satisfiability against the labels gqlc's own write path stamps for that
// axis; a graph a foreign writer populated is outside its terms.
//
// The swap it detects is the *cross-axis* one, and it is worth saying so
// next to the one it does not. A node label on an edge decoder — the
// decodeCompanyKnows-guards-on-"Company" case the per-axis alphabet was
// built for — is unreachable, because no statement gqlc writes puts a node
// type's key label on an edge, and it reddens. A node label on a *different
// node type's* decoder does not: decodePerson guarding on "Post" is asking
// for a label the node alphabet holds, so a vertex satisfying it exists and
// the decoder does fire. It is reachable and pointed at the wrong entity,
// which is a claim about which values a decoder *accepts* rather than about
// whether any value reaches it, and this gate makes no claim of that kind.
// gqlc-2h9w holds that class: the neo4j decoders carry no label guard at
// all, so every one of them accepts a node of any label, and the within-axis
// swap is the same defect with a guard written.
//
// The carrier annotation is outside its terms for a related reason. An AGE
// decoder names the carrier it wants as an *argument* —
// agtypeEntity(raw, "::vertex") — and an edge decoder emitted with
// "::vertex" is as dead as one guarding on an unstampable label, since no
// edge value carries the vertex suffix. comparedStrings takes only what a
// body compares, deliberately: an argument literal is otherwise a property
// key or a format string, and reading arguments would hold those to a
// label alphabet. So the annotation swap is not read here. It is not
// unheld — internal/codegen/age's TestEmittedHelpersDecodeTheAgtypeCorpus
// compiles the emitted helpers and runs them over captured agtype bytes,
// and a swapped annotation fails there with "does not carry the ::edge
// annotation" — but it is held there, behaviourally, and not here.
//
// Its subject is also the decoders and only the decoders. A label guard an
// emitted *query method* carries inline — AGE's edge-union dispatch writes
// one, a switch over the wire label with a case per candidate edge — sits
// in no decoder body and is not read here, because the sweep skips a
// function with a receiver. Same defect class, different site; today the
// only thing standing under an unsatisfiable case label there is a golden
// byte diff, which is to say nothing. That is gqlc-9xy0, filed rather than
// fixed here: widening the extractor to query bodies is a larger claim than
// this gate makes and wants its own verdict on what alphabet each site is
// held to.
//
// # Why it sweeps every backend, not the enrolled ones
//
// Enrolment is what hid the defect. A fixture reaches a backend only by
// naming it in its manifest, and the multi-label fixture names only
// neo4j-go-v5 precisely because AGE refuses the schema — so the emission
// that would carry the unfillable guard is one no enrolled pairing ever
// asks for. Running every registered backend over every fixture and letting
// a refusal pass silently is what makes the gate independent of the
// enrolment matrix: the day a backend stops refusing, the sweep is already
// pointed at it.
//
// # Why it generates rather than reading the goldens
//
// The bytes come from Generate, so no golden regeneration stands between
// the emission and the verdict. A gate whose only red is a golden byte diff
// is not a gate: -update blesses it away.
func (s *ConformanceSuite) TestEmittedDecodersGuardOnlyOnStampableLabels() {
	targets := s.backends.Keys()
	s.Require().Equal(targets, slices.Sorted(maps.Keys(labelGuardingTargets)),
		"the label-guard ledger no longer names exactly the registered backends: a backend it does not name emits "+
			"decoders no verdict covers, and a name the registry does not hold is a verdict over nothing")

	// The census is per target *and per axis*. Summed over both axes it
	// would be satisfied by the node decoders alone, so a rename that
	// blinded the extractor to every edge decoder — and with them to any
	// unstampable relationship-type guard one carried — would leave a
	// non-zero total and pass. What the sweep reads is a claim about each
	// axis separately, so it is recorded that way.
	shapes := slices.Sorted(maps.Keys(entityShapeWords))
	accepted := make(map[string]int, len(targets))
	decoders := make(map[string]map[codegen.EntityKind]int, len(targets))
	guarded := make(map[string]map[codegen.EntityKind]int, len(targets))
	for _, target := range targets {
		decoders[target] = make(map[codegen.EntityKind]int, len(shapes))
		guarded[target] = make(map[codegen.EntityKind]int, len(shapes))
	}
	// graded counts the guards actually held to an alphabet, incremented
	// where one is checked and nowhere else. A count of targets the ledger
	// records as guarding would be the same number by consequence and not
	// by construction: it is reached whether or not any guard was read, so
	// it would report "this gate examined a guard" on the strength of an
	// entry in a map rather than of a comparison this sweep performed.
	graded := 0

	for _, dir := range s.validFixtures() {
		fixture := filepath.Base(dir)
		s.Run(fixture, func() {
			m := s.loadManifest(dir)
			sch := s.loadSchema(dir)
			in := codegen.Input{Schema: sch, Queries: s.loadNamedQueries(dir, m, sch)}
			alphabet := schemaLabelAlphabet(sch)
			decoderShapes := preparedEntityShapes(s.Require(), in)

			for _, target := range targets {
				files, ok := s.emitOrRefuse(target, in)
				if !ok {
					// The backend cannot represent this schema, so it emits
					// no decoder and there is nothing here to reach. Its
					// refusals are not left unexamined: the multi-label
					// posture ledger asserts the shape that matters.
					continue
				}
				accepted[target]++
				for _, d := range emittedEntityDecoders(s.Require(), target, files, decoderShapes) {
					decoders[target][d.shape]++
					if len(d.guards) > 0 {
						guarded[target][d.shape]++
					}
					for _, guard := range d.guards {
						graded++
						s.Require().True(alphabet[d.shape][guard],
							"%s emits %s in %s for fixture %s. It fills %s, which is %s, so the only value that "+
								"ever reaches it is %s, and it returns an error unless that value's label equals "+
								"%q. %s Every call to %s therefore fails and the struct it fills is unreachable.",
							target, d.fn, d.file, fixture,
							d.entity, entityShapeWords[d.shape].typeText, entityShapeWords[d.shape].valueText,
							guard, unstampableBecause(guard, d.shape, alphabet), d.fn)
					}
				}
			}
		})
	}

	for _, target := range targets {
		// The floor on accepted is what makes every count below a
		// statement about emissions rather than about an empty loop: a
		// target that generated for nothing graded nothing and would
		// otherwise pass on zeroes. It is worth more than it was, because
		// each accepted emission is now reconciled entity by entity
		// against codegen.Prepare rather than sampled.
		s.Require().NotZero(accepted[target],
			"%s generated for no fixture in the corpus, so this sweep read none of its emissions", target)
		for _, shape := range shapes {
			words := entityShapeWords[shape]
			s.Require().NotZero(decoders[target][shape],
				"%s emitted no decoder filling %s over the whole corpus, so it emits nothing that decodes %s at "+
					"all: the corpus no longer holds a fixture declaring one, or the emission stopped rendering "+
					"them. It is not that the sweep failed to recognise them — a decoder it cannot recognise "+
					"leaves its entity undecoded and reddens per emission",
				target, words.typeText, words.valueText)
			if labelGuardingTargets[target] {
				s.Require().Equal(decoders[target][shape], guarded[target][shape],
					"%s is recorded as gating its entity decoders on the wire label, but %d of the %d it emits "+
						"for %s over the corpus test no string for equality: either the label check has been "+
						"dropped from the emission, or it is written in a shape this sweep does not read, and "+
						"either way the guards this gate believes it is checking are not being checked",
					target, decoders[target][shape]-guarded[target][shape], decoders[target][shape], words.typeText)
				continue
			}
			s.Require().Zero(guarded[target][shape],
				"%s is recorded as emitting no label guard, but %d of the %d decoders it emits for %s test a "+
					"string for equality; enrol it in labelGuardingTargets so its guards are held to the labels "+
					"that axis can carry",
				target, guarded[target][shape], decoders[target][shape], words.typeText)
		}
	}
	s.Require().NotZero(graded,
		"no emitted decoder in the whole corpus compared a string for equality, so this gate held nothing to an "+
			"alphabet: every assertion above is about counts and ledgers, and the reachability claim itself was "+
			"never exercised")
}

// TestMultiLabelSchemaPostureIsRecorded holds each backend to a declared
// verdict on a schema that declares an entity keyed on more than one label:
// emit for it, or refuse it whole.
//
// It is the anti-vacuity half of the reachability gate. That gate lets a
// refusal pass in silence, because a backend that emits nothing emits no
// unfillable decoder — but "nothing to check" and "checked and clean" are
// the same green, and the one backend whose wire cannot carry a label set
// is the one whose refusal is doing all the work. Recording the verdict
// turns that silence into an assertion, so a backend that starts emitting
// for a two-label entity reddens here whatever shape the guard it writes
// takes.
//
// The offending fixtures are found by walking the corpus for a node or edge
// type whose KeyLabels split into more than one label, not by naming a
// fixture: deleting the fixture that carries the shape reddens on the
// emptiness check rather than quietly leaving the ledger unexercised.
func (s *ConformanceSuite) TestMultiLabelSchemaPostureIsRecorded() {
	targets := s.backends.Keys()
	s.Require().Equal(targets, slices.Sorted(maps.Keys(multiLabelEmittingTargets)),
		"the multi-label posture ledger no longer names exactly the registered backends")

	swept, emitting := 0, 0
	for _, dir := range s.validFixtures() {
		fixture := filepath.Base(dir)
		sch := s.loadSchema(dir)
		offenders := multiLabelEntities(sch)
		if len(offenders) == 0 {
			continue
		}
		swept++
		s.Run(fixture, func() {
			m := s.loadManifest(dir)
			in := codegen.Input{Schema: sch, Queries: s.loadNamedQueries(dir, m, sch)}
			for _, target := range targets {
				_, emits := s.emitOrRefuse(target, in)
				s.Require().Equal(multiLabelEmittingTargets[target], emits,
					"%s %s fixture %s, which declares %s; the posture ledger records that it %s such a schema. "+
						"A backend that emits for one owes a decoder whose label guard some value it can stamp "+
						"satisfies, which is what TestEmittedDecodersGuardOnlyOnStampableLabels then holds it to",
					target, verdict(emits), fixture, strings.Join(offenders, " and "),
					verdict(multiLabelEmittingTargets[target]))
				// Counted after the ledger has been held to it, not
				// before. An emission the ledger refuses is a red, and
				// counting it first would let it satisfy the "some
				// emission for this shape was read" floor below on its
				// way past — the floor would be reporting an emission
				// this test has already ruled illegitimate.
				if emits {
					emitting++
				}
			}
		})
	}

	s.Require().NotZero(swept,
		"no valid fixture declares an entity keyed on more than one label, so this ledger records verdicts on a "+
			"shape the corpus no longer holds and the reachability gate's quiet arm is unexamined again")
	s.Require().NotZero(emitting,
		"every backend refuses every multi-label fixture, so no emission for one was read: the reachability gate "+
			"has nothing to say about this shape on any target")
}

// emitOrRefuse generates in through the backend registered under target,
// reporting ok=false for a backend that refuses the schema. Unlike
// generate() it is reached with a target the fixture's manifest does not
// enrol, so a refusal is an answer rather than a failure.
func (s *ConformanceSuite) emitOrRefuse(target string, in codegen.Input) ([]codegen.File, bool) {
	newGen, ok := s.backends.Lookup(target)
	s.Require().True(ok, "no backend is registered under %q", target)
	files, err := newGen("").Generate(in)
	if err != nil {
		s.Require().Nil(files, "%s returned files alongside its refusal", target)
		return nil, false
	}
	s.Require().NotEmpty(files, "%s returned neither files nor an error", target)
	return files, true
}

// verdict spells an emit-or-refuse answer for a diagnostic.
func verdict(emits bool) string {
	if emits {
		return "emits for"
	}
	return "refuses"
}

// labelAlphabet is, per entity axis, the labels a value on that axis can
// carry — the whole set of strings a decoder filling that axis can have
// its guard satisfied by. Kept split rather than unioned because the two
// axes are disjoint on the wire: no node carries a relationship type and
// no edge carries a node label.
type labelAlphabet map[codegen.EntityKind]map[string]bool

// schemaLabelAlphabet is what one schema's write path can stamp, split by
// the axis that carries it: node key labels on a vertex, edge key labels
// — the relationship type — on an edge.
//
// Key labels and no others. A node type's identity is its key set and its
// CompleteLabels is that set plus any label a "=>" clause implies, but an
// implied label is a matching concept, not a writing one: gqlc's own
// emission stamps exactly the key set (codegen.Entity.Labels is
// NodeType.KeyLabels, and AGE's wireEntity takes its single label from
// there), so a value gqlc writes never carries an implied label and a
// guard naming one is a guard nothing satisfies. Folding CompleteLabels in
// would widen this set in the false-green direction — it would pass
// exactly that guard — which is the opposite of what this gate is for. No
// fixture in the corpus declares an implied label today, so the two sets
// coincide and nothing here is load-bearing yet; it is spelled this way so
// that the day one lands, the widening is a decision someone makes rather
// than one already made.
//
// A backend whose write path did stamp complete labels and whose decoders
// did guard would need this per target as well as per axis. None does:
// the only target recorded in labelGuardingTargets stamps the key set.
func schemaLabelAlphabet(sch schema.Schema) labelAlphabet {
	out := labelAlphabet{}
	for shape := range entityShapeWords {
		out[shape] = map[string]bool{}
	}
	add := func(shape codegen.EntityKind, k graph.LabelSetKey) {
		for _, label := range k.Split() {
			out[shape][label] = true
		}
	}
	for _, n := range sch.Nodes {
		add(codegen.EntityNode, n.KeyLabels)
	}
	for _, e := range sch.Edges {
		add(codegen.EntityEdge, e.KeyLabels)
	}
	return out
}

// preparedEntityShapes is the axis each entity struct one batch derives
// sits on, keyed by the struct name a decoder returns.
//
// It is read off codegen.Prepare — the same derivation every backend's
// Generate runs before it renders a line — rather than re-derived from
// the schema here. That is what keeps a decoder's axis the emission's own
// answer: entity naming and the shape assignment move together, so this
// sweep cannot drift into holding a decoder to an axis the emitter does
// not put it on.
//
// Its key set is also the roll the emission is reconciled against, so a
// batch it under-names would leave decoders ungraded and one it
// over-names would demand decoders no backend renders. Both are the same
// derivation the backends run, which is why neither can drift alone.
//
// probeTypeMap admits every property width, so the derivation runs for
// every fixture any backend serves. A narrower table would refuse widths
// some backend has no carrier for, and a refusal here would blind the
// sweep to the very emission it is pointed at.
func preparedEntityShapes(r *require.Assertions, in codegen.Input) map[string]codegen.EntityKind {
	prepared, err := codegen.Prepare(in, probeTypeMap{}, "")
	r.NoError(err, "the shared derivation refuses a batch the corpus holds valid, so no decoder in it can be "+
		"assigned an axis and every guard it carries would go unread")
	out := make(map[string]codegen.EntityKind, len(prepared.Entities))
	for _, e := range prepared.Entities {
		r.Contains(entityShapeWords, e.Kind,
			"codegen.Prepare puts %s on an entity axis entityShapeWords does not name, so this sweep can say "+
				"neither which labels a value reaching its decoder carries nor what to call them: name the kind "+
				"there and give schemaLabelAlphabet an arm for it", e.Name)
		r.NotContains(out, e.Name,
			"codegen.Prepare names two entities %s, so which axis its decoder sits on is ambiguous", e.Name)
		out[e.Name] = e.Kind
	}
	return out
}

// multiLabelEntities names every node or edge type one schema keys on more
// than one label, sorted, for a diagnostic and for the posture sweep's
// fixture selection.
func multiLabelEntities(sch schema.Schema) []string {
	var out []string
	for _, n := range sch.Nodes {
		if len(n.KeyLabels.Split()) > 1 {
			out = append(out, fmt.Sprintf("a node type keyed on %s", string(n.KeyLabels)))
		}
	}
	for _, e := range sch.Edges {
		if len(e.KeyLabels.Split()) > 1 {
			out = append(out, fmt.Sprintf("an edge type keyed on %s", string(e.KeyLabels)))
		}
	}
	slices.Sort(out)
	return out
}

// unstampableBecause is the reason clause for a guard the alphabet of the
// decoder's own axis does not hold.
//
// Two arms are called out ahead of the general one. The join spelling is
// the case whose unsatisfiability is mechanical rather than corpus-
// relative: graph.LabelSetKey joins a label set with "&", and a single
// wire label cannot equal the join of two whatever the schema declares.
// The cross-axis case is the one a reader is most likely to argue with,
// because the string *is* in the schema — so the clause says which axis
// declares it and why that does not help.
func unstampableBecause(guard string, shape codegen.EntityKind, alphabet labelAlphabet) string {
	declared := slices.Sorted(maps.Keys(alphabet[shape]))
	if parts := graph.LabelSetKey(guard).Split(); len(parts) > 1 {
		return fmt.Sprintf("That is graph.LabelSetKey's join spelling of the %d-label set %v — a label set, not a "+
			"label — and a wire value carries one label, so no single label equals it. This axis declares %v.",
			len(parts), []string(parts), declared)
	}
	for _, other := range slices.Sorted(maps.Keys(entityShapeWords)) {
		if other == shape || !alphabet[other][guard] {
			continue
		}
		return fmt.Sprintf("The schema does declare that label — but on the other axis, where it belongs to %s and "+
			"%s carries it. A node carries a node type's key label and an edge carries its relationship type, "+
			"and no statement gqlc writes puts either on the other. This axis declares %v.",
			entityShapeWords[other].typeText, entityShapeWords[other].valueText, declared)
	}
	return fmt.Sprintf("This axis declares the labels %v and no others, so nothing writes that one.", declared)
}

// emittedEntityDecoders extracts one backend's entity decoders out of one
// emission, and refuses the emission outright if it cannot classify every
// package-level, receiver-less function in it.
//
// A decoder is recognised by what it returns: a function whose results
// name an entity codegen.Prepare derived is that entity's decoder. Filling
// an entity struct means returning one, so the recognition rests on the
// emission's own signature and on the shared derivation, and no naming
// convention has to hold for the sweep to see a decoder. Renaming one
// moves nothing here.
//
// That is the point, because the previous recognition was the name pattern
// decode<T>, and a function it failed to resolve was silently omitted —
// omitted from the per-axis census, from the guard count and from the
// alphabet check at the same time, so the extractor's own blind spot
// removed the numerator, the denominator and the correctness check
// together. The floor left standing was "some decoder was found", which
// any one surviving decoder satisfies.
//
// So the classification is total, and its residue is a failure rather than
// an omission:
//
//   - results naming exactly one prepared entity: that entity's decoder;
//   - results naming none: not a decoder, and then it may compare only
//     wireScalarSpellings, since any other string it tests is a guard this
//     gate has no verdict about;
//   - results naming two or more: no single axis to grade it against.
//
// Then both directions are reconciled. The entities decoded must be
// exactly the entities the shared derivation names, one decoder each:
// every backend renders one decoder per entity, so an entity left
// undecoded is either an emission that stopped filling that struct or a
// decoder written in a shape this sweep cannot read — and the second is
// precisely the ungraded site the classification exists to refuse.
//
// Naming the wire's scalar helpers is what this replaces. Keying on
// decode<T> separated a decoder from agtypeInt64 and agtypeString by their
// names; keying on the returned entity separates them by what they build,
// which no rename can move, and their comparisons against agtype's fixed
// spellings of true, false and null are held by wireScalarSpellings
// instead.
func emittedEntityDecoders(
	r *require.Assertions, target string, files []codegen.File, shapes map[string]codegen.EntityKind,
) []entityDecoder {
	fset := token.NewFileSet()
	var out []entityDecoder
	decoded := make(map[string]string, len(shapes))
	for _, f := range files {
		file, err := parser.ParseFile(fset, f.Path, f.Contents, parser.SkipObjectResolution)
		r.NoError(err, "parsing emitted %s", f.Path)
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv != nil || fn.Body == nil {
				continue
			}
			site := fmt.Sprintf("%s in %s", fn.Name.Name, f.Path)
			guards := comparedStrings(r, fn.Body)
			entities := resultEntities(fn, shapes)
			if len(entities) == 0 {
				for _, guard := range guards {
					r.True(wireScalarSpellings[guard],
						"%s emits %s, whose results name no entity type, and it tests %q for equality. A "+
							"package-level function in an emission is either a decoder — recognised here by the "+
							"entity it returns and held to the labels that entity's axis can carry — or one of the "+
							"wire's own scalar helpers, whose comparisons are against the fixed spellings %v. This "+
							"is neither, so %q is a guard nothing in this gate grades, and an unsatisfiable one "+
							"written there would pass unseen",
						target, site, guard, slices.Sorted(maps.Keys(wireScalarSpellings)), guard)
				}
				continue
			}
			r.Len(entities, 1,
				"%s emits %s, whose results name the entity types %v. A decoder's guards are graded against the "+
					"alphabet of the one axis its entity sits on, and a function returning entities from more "+
					"than one cannot be assigned an axis to be graded against",
				target, site, entities)
			entity := entities[0]
			prior, dup := decoded[entity]
			r.False(dup,
				"%s emits both %s and %s, and both return %s. Which of the two a value reaches is not decidable "+
					"here, so one of them is a decoder no call site need use and its guards are a claim about "+
					"nothing",
				target, prior, site, entity)
			decoded[entity] = site
			out = append(out, entityDecoder{
				fn:     fn.Name.Name,
				file:   f.Path,
				entity: entity,
				shape:  shapes[entity],
				guards: guards,
			})
		}
	}

	r.Equal(slices.Sorted(maps.Keys(shapes)), slices.Sorted(maps.Keys(decoded)),
		"%s decodes a different set of entities than codegen.Prepare names for this fixture. Every backend "+
			"renders one decoder per entity, and this sweep recognises one by the entity type it returns, so an "+
			"entity the derivation names and the emission does not decode is either a struct nothing fills or a "+
			"decoder written in a shape this sweep cannot read — and the guards of a decoder it cannot read are "+
			"graded by nothing at all", target)
	return out
}

// resultEntities names the prepared entities one function's results carry.
// It is how the sweep recognises a decoder, and it is deliberately blind
// to the function's name: what a function returns is what it can fill.
//
// A qualified type is another package's (dbtype.Node) and a type parameter
// is the caller's, so neither can be an entity this emission declares;
// both are stepped over rather than matched by spelling, so a schema that
// happened to name a node type Node or T could not be confused for one.
func resultEntities(fn *ast.FuncDecl, shapes map[string]codegen.EntityKind) []string {
	if fn.Type.Results == nil {
		return nil
	}
	params := map[string]bool{}
	if fn.Type.TypeParams != nil {
		for _, field := range fn.Type.TypeParams.List {
			for _, name := range field.Names {
				params[name.Name] = true
			}
		}
	}
	var out []string
	for _, field := range fn.Type.Results.List {
		ast.Inspect(field.Type, func(n ast.Node) bool {
			ident, ok := n.(*ast.Ident)
			if !ok {
				_, qualified := n.(*ast.SelectorExpr)
				return !qualified
			}
			if _, ok := shapes[ident.Name]; ok && !params[ident.Name] && !slices.Contains(out, ident.Name) {
				out = append(out, ident.Name)
			}
			return true
		})
	}
	return out
}

// comparedStrings is every string literal a function body tests a value for
// equality against: an operand of == or !=, or a switch case value. Those
// are the shapes a label guard takes; a literal passed as an argument is
// almost always a property key or a format string, and holding those to a
// label alphabet would redden on every emission.
//
// The exception is deliberate and stated where the gate states its scope:
// AGE's carrier annotation rides in as an argument — agtypeEntity(raw,
// "::vertex") — so swapping it makes a decoder as dead as an unstampable
// guard would and nothing here reads it. That one is held behaviourally by
// internal/codegen/age's TestEmittedHelpersDecodeTheAgtypeCorpus.
func comparedStrings(r *require.Assertions, body *ast.BlockStmt) []string {
	var out []string
	add := func(expr ast.Expr) {
		lit, ok := expr.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return
		}
		text, err := strconv.Unquote(lit.Value)
		r.NoError(err, "emitted string literal %s does not unquote", lit.Value)
		out = append(out, text)
	}
	ast.Inspect(body, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.BinaryExpr:
			if node.Op == token.EQL || node.Op == token.NEQ {
				add(node.X)
				add(node.Y)
			}
		case *ast.CaseClause:
			for _, expr := range node.List {
				add(expr)
			}
		}
		return true
	})
	return out
}
