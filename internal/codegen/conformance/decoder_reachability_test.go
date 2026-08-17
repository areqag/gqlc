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
	"testing"

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
// It is what makes the sweep's classification total. Every receiver-less
// function an emission writes at package level is either an entity decoder,
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
//
// # Hand-written, and the asymmetry that leaves
//
// This list is written out rather than derived from agtypeBool and
// agtypeValue, and the consequence is worth stating rather than papering
// over: the two error directions are not symmetric.
//
// Too narrow reddens, promptly and loudly. A spelling one of those helpers
// compares and this map does not hold makes that helper an ungraded site on
// the very next emission, which is the behaviour the paragraph above
// describes.
//
// Too wide is undetected. A spurious entry is a string no helper tests, so
// nothing here reads it and nothing reports it. The bound on that is exact,
// and it is why the machinery to close it is not worth building: this map
// is consulted on one arm only — the arm for a function whose results name
// no entity — so a spurious entry can widen only what a *non-decoder* may
// compare. A decoder's guards are graded against its own axis's label
// alphabet (schemaLabelAlphabet) and are never looked up here, so no entry
// in this map, however wrong, can admit an unsatisfiable label guard into a
// decoder. The undetected direction costs a helper that may compare one
// string too many; it cannot cost the reachability claim itself.
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
// So the sweep classifies every receiver-less function the emission writes
// at package level, and it finds one by walking rather than by matching a
// spelling: a func declaration, and every function literal any other
// package-level declaration holds at any depth — bound to a name, behind
// parentheses, inside a dispatch table, under a conversion. Those differ in
// syntax and in nothing this gate cares about. Over that set the
// classification is total:
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
// The net is exactly that wide and the claim is not wider. A method is not
// swept and is not claimed: a receiver form belongs to the emitted query
// surface, whose label sites are held to an alphabet chosen per call rather
// than per decoder, so grading them here would put a large amount of
// legitimate code under the wrong rule — see "What it does not decide"
// below, and gqlc-9xy0, which owns that site.
//
// Note what the first bullet costs, because it is a constraint on the
// emitters and not only a reading of them. Inside a decoder every compared
// string is graded as a label, with no exemption for wireScalarSpellings:
// a decoder that inlined `if s == "null"` is refused, though that
// comparison is one a value can satisfy. That is the contract rather than
// an accident — the wire's scalar handling lives in helpers the second
// bullet covers (agtypeInt64, agtypeString), so no emission carries such a
// comparison in a decoder body today, and exempting the spellings here
// would let a label guard spelled "null" pass the one check this gate
// exists to make. There is no sound way to tell the two apart at this
// remove: the inlined scalar and the label guard are the same *ast.Binary
// Expr, and discriminating on the operand's form would exempt a backend
// that wrote `if string(raw) == "Person"`, which is the dangerous
// direction.
//
// So the cost is a refusal an author has to work around, and it is paid
// where a refusal is paid — by writing the comparison in a helper. It is
// not paid in the report. The diagnostic for that decoder names the
// finding the sweep actually made and draws no consequence it cannot
// support: unstampableBecause has an arm for a wire spelling that says the
// string is not a label at all and offers both readings, and the frame
// around it asserts only what was compared and what the axis carries. A
// red that states a checkable falsehood about the emission — that a
// decoder returns an error, that its struct is unreachable — is the one
// failure mode a gate is least allowed to have, and this one used to have
// it.
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
// emitted *query method* carries inline sits in no decoder body and is not
// read here, because the sweep skips a function with a receiver. The
// edge-union dispatch is that shape, and it is neo4j's alone:
// walkListElemBody and writeEdgeUnionDispatchBody
// (internal/codegen/neo4j/render_queries.go) both write
// `switch rel.Type { case "<relationship type>": … }` into the query
// method, while Apache AGE emits no such site at any width — it refuses an
// edge-union column whole, on codegen.ErrUnrepresentableEdgeUnion, so no
// AGE emission carries one. Same defect class, different site. Under
// `just test` the only thing standing beneath an unsatisfiable case label
// there is a golden byte diff, which is to say nothing; the run that would
// catch it is edgeUnionDispatch in test/data/codegen/live_test.go, behind
// the codegen_live build tag and outside the default suite. That is
// gqlc-9xy0, filed rather than fixed here: widening the extractor to query
// bodies is a larger claim than this gate makes and wants its own verdict
// on what alphabet each site is held to.
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
						// The frame states what was observed and nothing
						// that follows from it. Which branch the guard
						// selects, whether the other one returns an error,
						// and whether any value can take it are all
						// properties of the emission this sweep did not
						// read: it reads that a string was compared and
						// that the axis cannot carry it. The consequence
						// is the reason clause's to draw, because it is
						// the only part that knows which kind of string
						// this is.
						s.Require().True(alphabet[d.shape][guard],
							"%s emits %s in %s for fixture %s. It fills %s, which is %s, so the only value that "+
								"ever reaches it is %s, and its body tests that value for equality against %q, "+
								"which is not one of the labels this axis can carry. %s",
							target, d.fn, d.file, fixture,
							d.entity, entityShapeWords[d.shape].typeText, entityShapeWords[d.shape].valueText,
							guard, unstampableBecause(guard, d.shape, alphabet))
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
				// before, so the floor below reports only emissions this
				// test ruled legitimate.
				//
				// This cannot change a verdict and is not claimed to. The
				// s.Require().Equal above is FailNow, so any input that
				// would make the floor lie has already reddened the suite;
				// measured with neo4j-go-v5 flipped to false, counting
				// after reports two failures where counting before reports
				// one, and both are red. It buys the second message, not
				// the red.
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
	// Both callers draw target from s.backends.Keys() and both hold that
	// key set equal to a ledger's before they sweep, so the lookup cannot
	// miss and an assertion on it would be a check no input can fail.
	newGen, _ := s.backends.Lookup(target)
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
//
// The duplicate-name refusal below cannot fire while codegen.Prepare's
// §4.6 identifier sweep stands: sweepIdentifiers (internal/codegen/
// prepare.go) inserts every entity struct name as its first source and
// returns ErrIdentifierCollision on the second, so a colliding batch
// reaches the NoError above instead. Unlike the arms of
// emittedEntityDecoders, which are unreachable only because no fixture
// writes their shape and are held by a synthetic witness, this one is
// unreachable by a guarantee upstream — so there is no emission that
// would exercise it and no test here pins it.
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
// decoder's own axis does not hold. It carries the diagnosis *and* the
// consequence, because the two do not travel together: three of its four
// arms answer a string that was meant to be a label, and one answers a
// string that was never a label at all.
//
// That is the split the assertion above cannot make. Its frame says only
// what the sweep observed — this decoder compares the wire value against
// this string, and this axis cannot carry it — and every arm here says what
// follows from that, so no sentence in the report asserts a consequence
// that does not hold on the emission it is reporting.
//
// Two arms are called out ahead of the general one. The join spelling is
// the case whose unsatisfiability is mechanical rather than corpus-
// relative: graph.LabelSetKey joins a label set with "&", and a single
// wire label cannot equal the join of two whatever the schema declares.
// The cross-axis case is the one a reader is most likely to argue with,
// because the string *is* in the schema — so the clause says which axis
// declares it and why that does not help.
//
// The wire-spelling arm is the fourth, and it is the one that stops the
// report lying. Inside a decoder every compared string is graded as a
// label, with no exemption for wireScalarSpellings — see the gate's
// docstring for why exempting them is not on offer — so a decoder that
// inlined `if string(raw) == "null"` is refused. But that comparison is
// satisfiable: the wire really does spell a null that way, and telling its
// author the branch is dead would be a falsehood they can check in one
// reading. So this arm reports the finding, which is true (a decoder
// compared something that is not a label), names both readings, and claims
// no deadness. It comes after the cross-axis arm because a schema is free
// to declare a label spelled like a wire scalar, and there the cross-axis
// clause is the more informative answer.
func unstampableBecause(guard string, shape codegen.EntityKind, alphabet labelAlphabet) string {
	declared := slices.Sorted(maps.Keys(alphabet[shape]))
	// dead is the consequence the three label arms share and the wire
	// spelling arm must not draw.
	// Which of the two is dead depends on how the emission wrote the
	// comparison, and the sweep does not read that: it collects an operand
	// of == or != and a case value alike, so naming a side would be a claim
	// about the source that this has not looked at.
	const dead = " So no value that reaches this decoder carries it: the comparison answers the same way on every " +
		"call, and one of the two branches it decides between is dead."
	if parts := graph.LabelSetKey(guard).Split(); len(parts) > 1 {
		return fmt.Sprintf("That is graph.LabelSetKey's join spelling of the %d-label set %v — a label set, not a "+
			"label — and a wire value carries one label, so no single label equals it. This axis declares %v.",
			len(parts), []string(parts), declared) + dead
	}
	for _, other := range slices.Sorted(maps.Keys(entityShapeWords)) {
		if other == shape || !alphabet[other][guard] {
			continue
		}
		return fmt.Sprintf("The schema does declare that label — but on the other axis, where it belongs to %s and "+
			"%s carries it. A node carries a node type's key label and an edge carries its relationship type, "+
			"and no statement gqlc writes puts either on the other. This axis declares %v.",
			entityShapeWords[other].typeText, entityShapeWords[other].valueText, declared) + dead
	}
	if wireScalarSpellings[guard] {
		return fmt.Sprintf("That is one of the wire's own fixed scalar spellings %v and not a label at all, so "+
			"either this decoder inlined scalar handling that belongs in one of the wire's helpers, where this "+
			"gate grades a comparison against those spellings, or it is a label guard nothing satisfies. This "+
			"axis declares %v.", slices.Sorted(maps.Keys(wireScalarSpellings)), declared)
	}
	return fmt.Sprintf("This axis declares the labels %v and no others, so nothing writes that one.", declared) + dead
}

// TestUnstampableReasonNamesTheRightObstacle holds unstampableBecause to one
// arm per obstacle.
//
// Only the general arm runs on a green corpus. The reason clause is built
// eagerly, as an argument to the assertion it explains, so every arm's
// *condition* is evaluated on every guard the sweep grades — but the two that
// answer a guard outside its axis are reached only under a failure, and their
// output is discarded on the pass. So nothing in the corpus distinguishes them
// from each other or from the general arm.
//
// Which arm answers is not cosmetic. The cross-axis clause carries the one
// thing a reader cannot get from the assertion itself: that the schema does
// declare the label, on the axis where it belongs. Degrade that to the general
// clause and the report says the schema never declares it, which is false and
// sends the reader to fix the wrong end.
func TestUnstampableReasonNamesTheRightObstacle(t *testing.T) {
	// The join spelling and the cross-axis label are both drawn from this
	// alphabet's own declarations, so a row cannot pass by naming a string
	// the schema never mentions.
	alphabet := labelAlphabet{
		codegen.EntityNode: {"Company": true, "Person": true},
		codegen.EntityEdge: {"KNOWS": true},
	}
	const (
		joinSpelling = "join spelling"
		otherAxis    = "on the other axis"
		wireSpelling = "fixed scalar spellings"
		undeclared   = "and no others"
	)

	for _, tc := range []struct {
		name, guard string
		shape       codegen.EntityKind
		want        string
	}{
		{
			name:  "a label set where a label is expected",
			guard: "Company&Person", shape: codegen.EntityNode, want: joinSpelling,
		},
		{
			name:  "a node label on an edge decoder",
			guard: "Company", shape: codegen.EntityEdge, want: otherAxis,
		},
		{
			name:  "one of the wire's own scalar spellings",
			guard: "null", shape: codegen.EntityNode, want: wireSpelling,
		},
		{
			name:  "a label the schema declares on neither axis",
			guard: "NoSuchLabelAnywhere", shape: codegen.EntityNode, want: undeclared,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			because := unstampableBecause(tc.guard, tc.shape, alphabet)

			require.Contains(t, because, tc.want,
				"the reason clause for %s does not reach the arm that explains it", tc.name)
			for _, other := range []string{joinSpelling, otherAxis, wireSpelling, undeclared} {
				if other == tc.want {
					continue
				}
				require.NotContains(t, because, other,
					"the reason clause for %s also answers as %q, so the arms do not decide one obstacle each",
					tc.name, other)
			}
		})
	}
}

// emittedEntityDecoders extracts one backend's entity decoders out of one
// emission, and refuses the emission outright if it cannot classify every
// receiver-less function written at package level in it — however the
// emission spells one, see packageLevelFuncs.
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
// So the classification is total over what packageLevelFuncs yields, and
// its residue is a failure rather than an omission:
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
		for _, fn := range packageLevelFuncs(fset, file) {
			site := fmt.Sprintf("%s in %s", fn.name, f.Path)
			guards := comparedStrings(r, fn.body)
			entities := resultEntities(fn.typ, shapes)
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
				fn:     fn.name,
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

// TestSweepGradesTheNamedFunctionSpellingsAlike holds the two ways Go spells
// a package-level, receiver-less function *under a name* to one rule: they
// must be classified and graded identically, not merely both somehow
// noticed.
//
// The two named spellings are not all of them, and reading them as all of
// them is what B2 was — see TestSweepReadsAFunctionLiteralAtAnyDepthOfA
// Declaration for the literals no name is bound to. What this one adds and
// that one cannot is the word-for-word equality: a named literal reports
// under its declared name, so its refusal is comparable character by
// character with the func declaration's, and a var-spelled function that
// reddened with a different diagnostic would be classified by a second rule
// rather than by the same one.
//
// It exists because the sweep runs over emissions no backend writes this
// way. Nothing gqlc emits today binds a function to a package-level var, so
// that path is reached by no fixture, and a path no test reaches is exactly
// the kind of thing this gate was built to stop believing in. The witness is
// therefore synthetic, and it is the residual the fixture corpus cannot
// express rather than a second copy of what the corpus already checks.
//
// The two sources differ in one keyword. Both write a helper that compares
// a string no verdict here covers, so both must be refused as ungraded
// sites.
func TestSweepGradesTheNamedFunctionSpellingsAlike(t *testing.T) {
	// The decoder is identical in both and is beside the point: it is here
	// so the emission decodes exactly the entity the shape map names and
	// the reconciliation at the end of the sweep is not what fires.
	const emission = `package emitted

%s

func decodePerson(raw []byte) (Person, error) {
	label := string(raw)
	if label != "Person" {
		return Person{}, nil
	}
	if !personLabelOK(label) {
		return Person{}, nil
	}
	return Person{}, nil
}
`
	const (
		asFunc = `func personLabelOK(label string) bool { return label == "NoSuchLabelAnywhere" }`
		asVar  = `var personLabelOK = func(label string) bool { return label == "NoSuchLabelAnywhere" }`
	)
	shapes := map[string]codegen.EntityKind{"Person": codegen.EntityNode}

	sweepOf := func(helper string) string {
		files := []codegen.File{{Path: "models.go", Contents: []byte(fmt.Sprintf(emission, helper))}}
		msgs := recordedSweepRefusal(files, shapes)
		require.NotEmpty(t, msgs,
			"the sweep accepted an emission carrying a helper that compares a string no verdict covers, so a guard "+
				"written there is graded by nothing")
		return strings.Join(msgs, "\n")
	}

	// Both spellings are swept from one call site so that the recorded
	// failures are comparable whole: testify's report carries the caller's
	// line, and two calls written on two lines would differ in the trace
	// while agreeing on everything this test is about.
	refusals := make([]string, 0, 2)
	for _, helper := range []string{asFunc, asVar} {
		refusals = append(refusals, sweepOf(helper))
	}

	require.Contains(t, refusals[0], "personLabelOK",
		"the func spelling must be refused by name, since the diagnostic is what points at the ungraded site")

	require.Equal(t, refusals[0], refusals[1],
		"the same helper spelled as a package-level var bound to a function literal is classified differently from "+
			"the same helper spelled as a func declaration. A var is an *ast.GenDecl and a func is an *ast.FuncDecl, "+
			"but that is a difference in syntax and in nothing this gate decides: both are receiver-less functions "+
			"written at package level, both can hold a label guard, and a sweep that reads only one of them leaves "+
			"the other's guards ungraded while claiming the classification is total")
}

// TestSweepReadsAFunctionLiteralAtAnyDepthOfADeclaration holds the sweep to
// every function literal a package-level declaration holds, and not only to
// the one spelled as a declared name's whole value.
//
// The rows below are witnesses that the walk is wired up, and deliberately
// not an enumeration the totality rests on. Reading a fixed list of syntactic
// positions is the defect this replaces: the sweep used to type-assert
// *ast.FuncLit on ValueSpec.Values[i], so a literal one node deeper — behind
// parentheses, inside a composite literal, under a conversion — was neither
// graded nor reported, and a witness set drawn from the arms the code
// happened to have could not discover the arm it did not have. The walk now
// visits every node of the declaration, so there is no per-shape arm left to
// be missing, and the last row nests a literal in shapes no arm ever named.
//
// Each row writes a helper comparing a string no verdict here covers, so the
// answer for every one of them is the same refusal: an ungraded site. A row
// that stopped reddening would mean a guard written in that position is
// graded by nothing.
func TestSweepReadsAFunctionLiteralAtAnyDepthOfADeclaration(t *testing.T) {
	// The decoder is beside the point and identical in every row: it is here
	// so the emission decodes exactly the entity the shape map names and the
	// reconciliation at the end of the sweep is not what fires.
	const emission = `package emitted

%s

func decodePerson(raw []byte) (Person, error) {
	label := string(raw)
	if label != "Person" {
		return Person{}, nil
	}
	return Person{}, nil
}
`
	const helper = `func(label string) bool { return label == "NoSuchLabelAnywhere" }`
	shapes := map[string]codegen.EntityKind{"Person": codegen.EntityNode}

	for _, tc := range []struct{ name, decl string }{
		{name: "parenthesised", decl: `var personLabelOK = (` + helper + `)`},
		{name: "an element of a slice literal", decl: `var personLabelOKs = []func(string) bool{` + helper + `}`},
		{
			name: "a value in a dispatch table",
			decl: `var personLabelOKs = map[string]func(string) bool{"Person": ` + helper + `}`,
		},
		{
			name: "a field of a struct literal",
			decl: `var personLabelOK = struct{ ok func(string) bool }{ok: ` + helper + `}`,
		},
		{
			name: "the operand of a conversion",
			decl: "type pred func(string) bool\n\nvar personLabelOK = pred(" + helper + ")",
		},
		{
			name: "nested in shapes no arm of this walk names",
			decl: `var personLabelOKs = [1]struct{ fs []func(string) bool }{{fs: []func(string) bool{` + helper + `}}}`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			files := []codegen.File{{Path: "models.go", Contents: []byte(fmt.Sprintf(emission, tc.decl))}}

			msgs := recordedSweepRefusal(files, shapes)

			require.NotEmpty(t, msgs,
				"the sweep accepted an emission whose helper is written %s, so it read a package-level function "+
					"literal as nothing at all and every string that one compares is graded by nothing", tc.name)
			joined := strings.Join(msgs, "\n")
			require.Contains(t, joined, "NoSuchLabelAnywhere",
				"the sweep refused the emission, but on some other arm than the ungraded comparison written %s",
				tc.name)
			require.Contains(t, joined, "the function literal at line",
				"the refusal does not say where the literal is. A literal written %s carries no declared name of its "+
					"own, so its position is the only thing that points a reader at the site", tc.name)
		})
	}
}

// TestSweepReadsAClosureThroughTheFunctionThatHoldsIt holds the other edge of
// the walk: a function literal written inside a function's body is not a
// second function to classify, it is part of the one whose body holds it.
//
// The walk yields a literal and stops there, and this is what that costs and
// buys. Descending further would classify the closure below on its own, find
// that it fills no entity, and refuse the emission because it compares a
// string that is not one of the wire's spellings — a red on an emission whose
// decoder is perfectly satisfiable, drawn from a helper the decoder itself
// owns. Stopping instead leaves the comparison to be collected by the walk of
// the enclosing body, where it is graded as what it is: this decoder's guard.
//
// No emission writes a closure today, so nothing in the corpus decides this
// either way, which is exactly why the pin is here rather than assumed.
func TestSweepReadsAClosureThroughTheFunctionThatHoldsIt(t *testing.T) {
	const emission = `package emitted

var decodePerson = func(raw []byte) (Person, error) {
	label := string(raw)
	isPerson := func() bool { return label == "Person" }
	if !isPerson() {
		return Person{}, nil
	}
	return Person{}, nil
}
`
	shapes := map[string]codegen.EntityKind{"Person": codegen.EntityNode}
	files := []codegen.File{{Path: "models.go", Contents: []byte(emission)}}

	decoders := emittedEntityDecoders(require.New(t), "probe-backend", files, shapes)

	require.Len(t, decoders, 1,
		"the sweep read the closure inside decodePerson as a second package-level function, so a helper a decoder "+
			"owns is classified as though the emission had written it beside the decoder")
	require.Equal(t, "decodePerson", decoders[0].fn,
		"the sweep reported the decoder under something other than the name it is declared and called by")
	require.Equal(t, []string{"Person"}, decoders[0].guards,
		"the label the closure compares is not collected as the enclosing decoder's guard, so a decoder that moved "+
			"its guard into a closure would be graded against no alphabet at all")
}

// recordedSweepRefusal runs the sweep over a synthetic emission and returns
// what it reported instead of failing the caller's test, empty for one it
// accepted.
//
// Every caller goes through this one call site on purpose. testify writes the
// caller's line into the message it reports, so two sweeps invoked from two
// lines would differ in the trace while agreeing on everything a test here
// asks about.
func recordedSweepRefusal(files []codegen.File, shapes map[string]codegen.EntityKind) []string {
	rec := &recordingT{}
	func() {
		defer func() {
			if r := recover(); r != nil {
				if _, ok := r.(failedNow); !ok {
					panic(r)
				}
			}
		}()
		emittedEntityDecoders(require.New(rec), "probe-backend", files, shapes)
	}()
	return rec.msgs
}

// TestSweepRefusesEveryEmissionItCannotClassify holds the three refusals that
// make the classification total rather than merely wide.
//
// Each is unreachable through the corpus: no backend renders a function
// returning two entity structs, none renders a second decoder for an entity,
// and none leaves an entity undecoded. So all three survive being neutered —
// the whole gate stays green with the one-entity check relaxed, with the
// duplicate check dropped, and with the reconciliation dropped — and each is a
// refusal the gate's docstring states as load-bearing.
//
// The one that decides reachability is the first. With it relaxed the sweep
// takes results[0] as the entity, so a function returning both a node and an
// edge has its guards graded against whichever axis happens to come first, and
// a relationship type read against the node alphabet passes. The other two are
// precision rather than vacuity, and are pinned because the emission is
// reconciled against codegen.Prepare in both directions and a direction nothing
// exercises is a direction nothing holds.
//
// The duplicate arm is exercised twice, on one emission written two ways: the
// second decoder beside the first, and the same second decoder as a value in a
// package-level dispatch table. That pair is a claim the arms alone do not
// make — that the refusal is a property of the emission and not of how it is
// spelled. It reddened when the sweep read only a declaration's top-level
// value, which is how a decoder no value can satisfy passed this gate green
// while its visible twin was refused. Its reconciliation cannot catch it
// either: the entity is decoded by the visible decoder, so the roll balances.
func TestSweepRefusesEveryEmissionItCannotClassify(t *testing.T) {
	const prologue = `package emitted

func decodePerson(raw []byte) (Person, error) { return Person{}, nil }

func decodeKnows(raw []byte) (Knows, error) { return Knows{}, nil }
`
	shapes := map[string]codegen.EntityKind{"Person": codegen.EntityNode, "Knows": codegen.EntityEdge}

	for _, tc := range []struct{ name, emission, want string }{
		{
			name:     "a function whose results name two entities",
			emission: prologue + "\nfunc decodeBoth(raw []byte) (Person, Knows, error) { return Person{}, Knows{}, nil }\n",
			want:     "cannot be assigned an axis to be graded against",
		},
		{
			name:     "a second decoder for one entity",
			emission: prologue + "\nfunc decodePersonAgain(raw []byte) (Person, error) { return Person{}, nil }\n",
			want:     "Which of the two a value reaches is not decidable here",
		},
		{
			name:     "an entity the emission never decodes",
			emission: "package emitted\n\nfunc decodePerson(raw []byte) (Person, error) { return Person{}, nil }\n",
			want:     "decodes a different set of entities than codegen.Prepare names for this fixture",
		},
		{
			name: "a second decoder for one entity, spelled as a value in a dispatch table",
			emission: prologue + `
var byLabel = map[string]func([]byte) (Person, error){
	"Person": func(raw []byte) (Person, error) {
		label := string(raw)
		if label != "NoSuchLabelAnywhere" {
			return Person{}, nil
		}
		return Person{}, nil
	},
}
`,
			want: "Which of the two a value reaches is not decidable here",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			files := []codegen.File{{Path: "models.go", Contents: []byte(tc.emission)}}

			msgs := recordedSweepRefusal(files, shapes)

			require.NotEmpty(t, msgs,
				"the sweep accepted an emission holding %s, so it classified something it has no verdict for", tc.name)
			require.Contains(t, strings.Join(msgs, "\n"), tc.want,
				"the sweep refused the emission, but on some other arm than the one %s is meant to reach", tc.name)
		})
	}
}

// recordingT collects what a require reported instead of failing the test,
// so that a helper asserting on its caller's behalf can be tested rather
// than only run. It is the same device internal/schema/gql's
// TestMeasureCoverageRejects uses, for the same reason.
//
// FailNow must not return. It panics rather than calling runtime.Goexit
// because Goexit needs the call on a goroutine of its own, and an assertion
// inside a goroutine is indistinguishable — to a reader and to testifylint —
// from the mistake where a failure is reported to a test that has finished.
type recordingT struct{ msgs []string }

// failedNow is the panic value, distinct so that a panic from anywhere else
// in the sweep is re-raised rather than read as a refusal.
type failedNow struct{}

func (r *recordingT) Errorf(format string, args ...any) {
	r.msgs = append(r.msgs, fmt.Sprintf(format, args...))
}

func (r *recordingT) FailNow() { panic(failedNow{}) }

// emittedFunc is one receiver-less function an emission writes at package
// level, reduced to the three things the classification reads: what it is
// called, for the diagnostic; its signature, which says what it can fill;
// and its body, which holds the strings it compares.
//
// It exists so the sweep reads a function rather than a *declaration*. The
// two spellings arrive as different nodes — an *ast.FuncDecl and a value
// inside an *ast.GenDecl — and every question this gate asks is answered
// identically for both, so they are flattened here and nowhere else has to
// know there were two.
type emittedFunc struct {
	name string
	typ  *ast.FuncType
	body *ast.BlockStmt
}

// packageLevelFuncs is every receiver-less function one emitted file
// writes at package level: every func declaration, and every function
// literal any other declaration holds, at whatever depth it holds it.
//
//	func decodePost(raw []byte) (Post, error) { … }
//	var decodePost = func(raw []byte) (Post, error) { … }
//	var byLabel = map[string]func([]byte) (Post, error){"Post": func…}
//
// The third line is why this walks rather than matches. Reading only the
// first is what the totality claim originally overclaimed; reading the
// first two is what it overclaimed next, and that one was worse, because
// the correction was mistaken for the whole of it. A sweep that
// type-asserts *ast.FuncLit on the top-level value of a ValueSpec sees a
// literal behind parentheses, inside a slice, map or struct literal, or
// under a conversion as nothing at all — five positions, none reported —
// and a package-level dispatch table is the shape a decoder is most
// plausibly relocated into (gqlc-9xy0). A second decoder hidden in one
// left this gate green while the same emission spelled visibly was
// refused: the refusal did not survive a change of spelling.
//
// So the rule is not a list of positions. Every node of a declaration that
// is not a func declaration is visited, and every function literal found
// there is a function this gate classifies — which is a claim about the
// walk rather than about the shapes anyone thought to enumerate, and there
// is no per-shape arm left that could be the missing one.
//
// Descent stops at each literal that is yielded. A literal nested inside a
// function's body is covered exactly as one nested in a func declaration's
// body is: the enclosing body is walked whole and its comparisons are
// collected with the rest.
//
// A method is not yielded, and that exclusion is the claim's edge rather
// than a blind spot of the same kind: a receiver form belongs to the
// emitted query surface, which the gate states outright it holds nothing
// about (gqlc-9xy0). The net and the claim are the same width on purpose.
func packageLevelFuncs(fset *token.FileSet, file *ast.File) []emittedFunc {
	var out []emittedFunc
	for _, decl := range file.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok {
			// A nil body is a declaration whose implementation is
			// elsewhere (assembly, a linkname); it compares nothing and
			// there is no body to walk.
			if fn.Recv != nil || fn.Body == nil {
				continue
			}
			out = append(out, emittedFunc{name: fn.Name.Name, typ: fn.Type, body: fn.Body})
			continue
		}
		names := boundLiteralNames(decl)
		ast.Inspect(decl, func(n ast.Node) bool {
			lit, ok := n.(*ast.FuncLit)
			if !ok {
				return true
			}
			name, named := names[lit]
			if !named {
				name = fmt.Sprintf("the function literal at line %d", fset.Position(lit.Pos()).Line)
			}
			out = append(out, emittedFunc{name: name, typ: lit.Type, body: lit.Body})
			return false
		})
	}
	return out
}

// boundLiteralNames is the declared name of each function literal one
// declaration binds to one directly, so that a var spelled as a function
// reports under the name it is called by rather than under its position.
//
// A literal any deeper — an element of a table, the operand of a
// conversion — has no name of its own to report, and inventing one out of
// the declaration that encloses it would name something a reader cannot
// grep for. Those are reported by position instead.
func boundLiteralNames(decl ast.Decl) map[*ast.FuncLit]string {
	names := map[*ast.FuncLit]string{}
	ast.Inspect(decl, func(n ast.Node) bool {
		spec, ok := n.(*ast.ValueSpec)
		if !ok {
			return true
		}
		// Names and Values line up index for index whenever a value is a
		// function literal: the only ValueSpec shape where they do not is
		// the multi-return `var a, b = f()`, whose single value is a call
		// and never a literal.
		for i, v := range spec.Values {
			if lit, ok := v.(*ast.FuncLit); ok {
				names[lit] = spec.Names[i].Name
			}
		}
		return true
	})
	return names
}

// resultEntities names the prepared entities one function's results carry.
// It is how the sweep recognises a decoder, and it is deliberately blind
// to the function's name: what a function returns is what it can fill.
// It takes the signature rather than the declaration so that the two
// spellings packageLevelFuncs yields are recognised by one rule.
//
// A qualified type is another package's (dbtype.Node) and a type parameter
// is the caller's, so neither can be an entity this emission declares;
// both are stepped over rather than matched by spelling, so a schema that
// happened to name a node type Node or T could not be confused for one.
func resultEntities(typ *ast.FuncType, shapes map[string]codegen.EntityKind) []string {
	if typ.Results == nil {
		return nil
	}
	params := map[string]bool{}
	if typ.TypeParams != nil {
		for _, field := range typ.TypeParams.List {
			for _, name := range field.Names {
				params[name.Name] = true
			}
		}
	}
	var out []string
	for _, field := range typ.Results.List {
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

// TestSweepReadsAForeignOrGenericResultAsNoEntity holds resultEntities to the
// two shapes it steps over by construction rather than by spelling.
//
// Neither is reachable through the corpus: no fixture declares an entity named
// after a type another package exports, and nothing gqlc emits is generic. So
// removing either step leaves the whole gate green, and the claim that a schema
// naming a node type Node or T cannot be mistaken for one rests on reading the
// code. Each helper below returns a Node that is not the emission's Node, and
// the emission also carries the real decoder for it, so a step that stopped
// being taken makes the helper a second decoder for the same entity.
//
// The refusal is what reports that, and it is worth saying which assertion
// does the work here rather than leaving it to look like the ones below.
// Dropping either step reddens inside emittedEntityDecoders, on the duplicate
// arm — measured: "probe-backend emits both driverNode in models.go and
// decodeNode in models.go, and both return Node" — because the sweep is handed
// this test's own require and refuses before returning. The Len below states
// the shape that refusal is the absence of; it is not a second kill, and an
// Equal naming decodeNode beside it was a third statement of the same thing,
// unreachable behind both.
func TestSweepReadsAForeignOrGenericResultAsNoEntity(t *testing.T) {
	const emission = `package emitted

%s

func decodeNode(raw []byte) (Node, error) {
	label := string(raw)
	if label != "Node" {
		return Node{}, nil
	}
	return Node{}, nil
}
`
	shapes := map[string]codegen.EntityKind{"Node": codegen.EntityNode}

	for _, tc := range []struct{ name, helper string }{
		{
			name:   "a qualified type another package declares",
			helper: `func driverNode(raw []byte) (dbtype.Node, error) { return dbtype.Node{}, nil }`,
		},
		{
			name:   "a type parameter the call site binds",
			helper: `func zeroNode[Node any]() (Node, error) { var out Node; return out, nil }`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			files := []codegen.File{{Path: "models.go", Contents: []byte(fmt.Sprintf(emission, tc.helper))}}

			decoders := emittedEntityDecoders(require.New(t), "probe-backend", files, shapes)

			require.Len(t, decoders, 1,
				"the sweep read %s as decoding the emission's own Node, so a helper that cannot fill an entity "+
					"struct is graded as though it did", tc.name)
		})
	}
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

// TestSweepReadsEveryGuardShapeItClaims holds comparedStrings to all three
// shapes it says it reads, one witness each.
//
// Only one of the three is reachable through the corpus. Every label guard any
// backend emits today is written `if label != "X"`, so the literal is always the
// right operand: dropping the left operand and dropping the switch case arm both
// leave the whole gate green.
//
// Neither omission is merely uncovered. The per-axis census counts a decoder
// that compared *some* string, so a decoder keeping its real guard and growing a
// second one in an unread shape holds that count where it was, and the extra
// guard is then graded by nothing — a decoder that can never fire passing the
// gate built to refuse exactly that.
func TestSweepReadsEveryGuardShapeItClaims(t *testing.T) {
	const emission = `package emitted

func decodePerson(raw []byte) (Person, error) {
	label := string(raw)
%s
	return Person{}, nil
}
`
	shapes := map[string]codegen.EntityKind{"Person": codegen.EntityNode}

	for _, tc := range []struct{ name, guard string }{
		{name: "the right operand of !=", guard: "\tif label != \"Wanted\" {\n\t\treturn Person{}, nil\n\t}"},
		{name: "the left operand of ==", guard: "\tif \"Wanted\" == label {\n\t\treturn Person{}, nil\n\t}"},
		{name: "a switch case value", guard: "\tswitch label {\n\tcase \"Wanted\":\n\t\treturn Person{}, nil\n\t}"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			files := []codegen.File{{Path: "models.go", Contents: []byte(fmt.Sprintf(emission, tc.guard))}}

			decoders := emittedEntityDecoders(require.New(t), "probe-backend", files, shapes)

			require.Len(t, decoders, 1,
				"the emission writes one decoder and the sweep read %d, so the guard shape below is being read "+
					"off something other than the decoder this row wrote it into", len(decoders))
			require.Equal(t, []string{"Wanted"}, decoders[0].guards,
				"a decoder testing the wire label against a literal written as %s is read as guarding on nothing, "+
					"so a literal no value on its axis can carry is never held to that axis's alphabet", tc.name)
		})
	}
}
