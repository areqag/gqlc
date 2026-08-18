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
// so they are the wire's, not the schema's.
//
// No schema label is spelled like one of them, and that is a property of
// the front end rather than a convention, so it is worth saying where it
// lives. A bare true, false or null does not parse as a labelName: GQL.g4
// is declared caseInsensitive, so BOOLEAN_LITERAL and NULL_KW take those
// spellings in any case, and neither is in nonReservedWords, which is the
// only keyword route into regularIdentifier. The delimited spellings do
// parse, and they carry their delimiters into the label, because labelSet
// reads a labelName with GetText() (internal/schema/gql/nodetype.go): the
// label declared by (:`true`) is `true` with the backticks in it. Change
// either half — a keyword leaves the reserved set, or the front end starts
// unquoting a delimited identifier — and a label can be spelled like a
// wire scalar, which is what the wire-spelling arm of unstampableBecause
// says it relies on.
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
// Recognising by the name pattern decode<T> for a declared T is the gate's
// own vacuity. A function the pattern fails to resolve is not merely
// unread: it leaves the per-axis census, the guard count and the alphabet
// check *simultaneously*, so the extractor's failure to recognise a decoder
// removes it from the numerator, the denominator and the correctness check
// at once. One entity's decoder emitted as decodePostRecord, with a guard
// on a label no backend stamps, leaves such a gate green; the only red is a
// TestValid golden byte diff, which go test -update blesses away.
// Broadening the pattern until that name matches is the same shape one
// rename later.
//
// So the sweep classifies every receiver-less function declaration with a
// body that the emission writes, and it finds one by walking rather than by
// matching a spelling: a func declaration, every function literal any other
// package-level declaration holds at any depth — bound to a name, behind
// parentheses, inside a dispatch table, under a conversion — and, inside
// any body it reads, every function literal at any depth, whatever its
// results name. Those differ in syntax and in nothing this gate cares
// about. Over that set the classification is total:
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
// classified and is not claimed: a receiver form belongs to the emitted
// query surface, whose label sites are held to an alphabet chosen per call
// rather than per decoder, so grading them here would put a large amount of
// legitimate code under the wrong rule — see "What it does not decide"
// below, and gqlc-9xy0, which owns that site. Its *body* is still walked,
// for every function literal at any depth in it, whatever that literal's
// results name. A receiver-less function literal is not a receiver form
// however it is reached, and skipping the method whole leaves a dispatch
// table returned from one in nobody's guard list — a second decoder for an
// entity, absent from the census, on an emission that goes green.
//
// The exclusion is syntactic: it is over the receiver form, not over what
// its results name. A decoder moved onto a receiver is excluded with every
// other method — it leaves this census, and the only thing that reddens is
// the reconciliation against codegen.Prepare, on that entity's absence. Put
// a live package-level decoder for the same entity beside it and even that
// is satisfied, and the method's own guards are read by nothing here. A
// receiver-less helper written beside a decoder is yielded, because it is
// not a receiver form.
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
// around it, unstampableReport, says only that a string was tested for
// equality somewhere in that body and that this axis cannot carry it. It
// does not say which value the string was compared with, because
// comparedStrings does not read that, and every consequence the report
// draws is conditional on the comparison being the label guard. A red that
// states a checkable falsehood about the emission — that a decoder returns
// an error, that its struct is unreachable, that a comparison on a
// property answers the same way on every call — is the one failure mode a
// gate is least allowed to have.
//
// Both directions are reconciled per emission: the entities decoded must
// be exactly the entities codegen.Prepare names, one decoder each. So a
// decoder rendered under any function name at all is still read, and one
// whose result type is spelled in a way the sweep cannot resolve to a
// prepared entity — an alias, a wrapper — is not recognised as that
// entity's decoder. It is still *classified*: every receiver-less function
// declaration with a body, and every function literal at any depth inside
// any declaration, is yielded, so such a decoder lands on the arm for a
// function whose results name nothing prepared, and the first label guard
// it writes refuses the emission. If it is the only
// decoder for its entity the reconciliation refuses the emission too, on
// the entity's absence. An unrecognised decoder is an ungraded site, not a
// silent one.
//
// The limit that leaves is stated rather than papered over: an unresolvable
// decoder that compares no string, or only the wire's own spellings, and
// whose entity some other decoder does fill, is accepted. It carries no
// guard, so there is no reachability claim in it for this gate to be wrong
// about.
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
// read here, because a function with a receiver is classified by nothing
// and the strings its own body compares — outside any function literal
// written in it — are collected by nothing. The
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
						s.Require().True(alphabet[d.shape][guard],
							unstampableReport(target, fixture, d, guard, alphabet))
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
					"them. It is not that the sweep silently failed to recognise them — every emission is "+
					"reconciled entity by entity, so a decoder it cannot recognise leaves its entity undecoded "+
					"and reddens per emission unless a second, recognised decoder fills it",
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

// TestEmittedClosuresNameNoEntityAndCompareNoString is the corpus census of
// the function literals packageLevelFuncs yields, and it carries in
// measurement a claim prose cannot hold.
//
// Widening the sweep to every literal at every depth brought every closure in
// every emission under the classification at once. What that costs is a
// question about the emissions and not about the rule, and prose naming the
// closures one backend writes does not answer it: a second backend writes
// element decoders too, and it is the one target whose decoders are
// additionally held to guard on the wire label. So the answer is measured
// here: a literal yielded out of an emission names
// no prepared entity in its results and compares no string, which lands it on
// the results-name-nothing arm with no guard for an alphabet to reject.
//
// The string half of that is stricter than the sweep itself, which accepts a
// literal comparing one of the wire's own fixed spellings. An emitter that
// inlined a scalar narrowing as a closure, instead of passing the named
// helper it passes today, would redden this census on an emission the gate
// accepts. That is deliberate: what these sentences stand behind is what the
// backends write, not what the gate would tolerate.
//
// The per-target floor keeps the census off an empty loop. It is a presence
// bit, not the enumeration it replaced: it reddens when a target stops
// emitting closures altogether. A closure a target starts emitting is read by
// the two assertions above, and reddens here only if it names a prepared
// entity or compares a string.
func (s *ConformanceSuite) TestEmittedClosuresNameNoEntityAndCompareNoString() {
	targets := s.backends.Keys()
	closures := make(map[string]int, len(targets))

	for _, dir := range s.validFixtures() {
		fixture := filepath.Base(dir)
		s.Run(fixture, func() {
			m := s.loadManifest(dir)
			sch := s.loadSchema(dir)
			in := codegen.Input{Schema: sch, Queries: s.loadNamedQueries(dir, m, sch)}
			decoderShapes := preparedEntityShapes(s.Require(), in)

			for _, target := range targets {
				files, ok := s.emitOrRefuse(target, in)
				if !ok {
					continue
				}
				fset := token.NewFileSet()
				for _, f := range files {
					file, err := parser.ParseFile(fset, f.Path, f.Contents, parser.SkipObjectResolution)
					s.Require().NoError(err, "parsing emitted %s", f.Path)
					for _, fn := range packageLevelFuncs(fset, file) {
						if !fn.literal {
							continue
						}
						closures[target]++
						site := fmt.Sprintf("%s in %s", fn.name, f.Path)
						s.Require().Empty(resultEntities(fn.typ, decoderShapes),
							"%s emits %s for fixture %s, a function literal whose results name prepared entity "+
								"types. Both docstrings describing what widening this sweep to every literal "+
								"brought under the classification say the emitted closures fill nothing, and this "+
								"census is what stands behind them, so a closure that does fill one is a decoder "+
								"written where those sentences say there are none",
							target, site, fixture)
						s.Require().Empty(comparedStrings(s.Require(), fn.body),
							"%s emits %s for fixture %s, and it compares a string for equality. The sweep itself "+
								"accepts that only while the string is one of the wire's own fixed spellings %v "+
								"and refuses the emission otherwise; either way the docstrings saying the emitted "+
								"closures compare no string are describing a different emission",
							target, site, fixture, slices.Sorted(maps.Keys(wireScalarSpellings)))
					}
				}
			}
		})
	}

	for _, target := range targets {
		s.Require().NotZero(closures[target],
			"%s emitted no function literal anywhere in the corpus, so this census read none of its closures and "+
				"the sentences it stands behind are claims about an empty set. It replaced an enumeration in prose "+
				"that named one backend's closures and dropped another's; a backend dropping out of this set is the "+
				"same defect, recorded rather than written down",
			target)
	}
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
// reporting ok=false for a backend that refuses the schema. target must be
// one of s.backends.Keys(). Unlike generate() it is reached with a target
// the fixture's manifest does not enrol, so a refusal is an answer rather
// than a failure.
func (s *ConformanceSuite) emitOrRefuse(target string, in codegen.Input) ([]codegen.File, bool) {
	// A codegen.Registry is immutable after construction and its Keys
	// reports the map its Lookup reads, so under the precondition above the
	// lookup cannot miss and an assertion on it would be a check no input
	// can fail. Nothing here rests on a ledger holding that key set: a
	// caller that sweeps without one still cannot reach this with a target
	// the registry does not hold.
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

// unstampableReport is the whole red for one guard the decoder's axis
// cannot carry: the frame, which says what the sweep read off the emission,
// and then the reason clause, which says what follows.
//
// It is a function rather than a format string written into the assertion
// so that the frame is something a test can read. The reason clause was
// always testable and the sentence around it was not, and the sentence
// around it is where the report's last two falsehoods lived.
//
// The frame is careful about one thing in particular: which side of the
// comparison the string sits on. comparedStrings collects an operand of ==
// or != and a case value alike and never reads the other side, so the
// report cannot say the decoder tests *the wire value* against this string.
// It said exactly that, and on a decoder discriminating on a property —
// `if kind == "premium"` — every consequence that followed was false. What
// the sweep read is that a string appears in a comparison in this body, and
// that this axis cannot carry it. Which value it is compared with is the
// reader's to check.
//
// Its opening clause is still about the axis, because that part does follow
// from the signature alone: a function returning Person fills a node, so
// the alphabet it is held to is the node alphabet whatever its body does.
func unstampableReport(target, fixture string, d entityDecoder, guard string, alphabet labelAlphabet) string {
	words := entityShapeWords[d.shape]
	return fmt.Sprintf("%s emits %s in %s for fixture %s. It fills %s, which is %s, so the only value that ever "+
		"reaches it is %s and the labels it can be handed are that axis's. Somewhere in its body a string is "+
		"tested for equality against %q, which is not one of them. %s",
		target, d.fn, d.file, fixture, d.entity, words.typeText, words.valueText, guard,
		unstampableBecause(guard, d.shape, alphabet))
}

// unstampableBecause is the reason clause for a guard the alphabet of the
// decoder's own axis does not hold. It carries the diagnosis *and* the
// consequence, because the two do not travel together: three of its four
// arms answer a string that was meant to be a label, and one answers a
// string that was never a label at all.
//
// That is the split the frame cannot make. The frame says only what the
// sweep observed — a string compared somewhere in this body, on an axis
// that cannot carry it — and every arm here says what follows from that,
// under the one condition the sweep did not check.
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
// no deadness. Its position after the cross-axis arm is not load-bearing:
// no schema label is spelled like a wire scalar — see wireScalarSpellings
// for the two places the front end fixes that — so no guard satisfies this
// arm's test and that one at once, and the two cannot compete for a
// string.
func unstampableBecause(guard string, shape codegen.EntityKind, alphabet labelAlphabet) string {
	declared := slices.Sorted(maps.Keys(alphabet[shape]))
	// deadIfGuard is the consequence the three label arms share and the wire
	// spelling arm must not draw, and it is conditional because the sweep
	// does not read the condition. It collects an operand of == or != and a
	// case value alike without reading the other side, so it does not know
	// whether the string is being compared with the wire label or with
	// something the decoder computed. Stated flatly it was false of the
	// second shape: a decoder discriminating on a property compares a value
	// that varies, so the comparison does not answer the same way on every
	// call and neither branch is dead. Which of the two branches is the dead
	// one is unread for the same reason — naming a side would be a claim
	// about source this has not looked at.
	const deadIfGuard = " If that string is the wire label this decoder is guarding on, no value reaching it carries " +
		"the string: the guard answers the same way on every call and the branch behind it is dead. If it is " +
		"compared with something the decoder computed instead, it is not a label at all and this axis is the " +
		"wrong alphabet to have graded it against — which is a defect in this reading, and worth reporting as one."
	if parts := graph.LabelSetKey(guard).Split(); len(parts) > 1 {
		return fmt.Sprintf("That is graph.LabelSetKey's join spelling of the %d-label set %v — a label set, not a "+
			"label — and a wire value carries one label, so no single label equals it. This axis declares %v.",
			len(parts), []string(parts), declared) + deadIfGuard
	}
	for _, other := range slices.Sorted(maps.Keys(entityShapeWords)) {
		if other == shape || !alphabet[other][guard] {
			continue
		}
		return fmt.Sprintf("The schema does declare that label — but on the other axis, where it belongs to %s and "+
			"%s carries it. A node carries a node type's key label and an edge carries its relationship type, "+
			"and no statement gqlc writes puts either on the other. This axis declares %v.",
			entityShapeWords[other].typeText, entityShapeWords[other].valueText, declared) + deadIfGuard
	}
	if wireScalarSpellings[guard] {
		return fmt.Sprintf("That is one of the wire's own fixed scalar spellings %v and not a label at all, so "+
			"either this decoder inlined scalar handling that belongs in one of the wire's helpers, where this "+
			"gate grades a comparison against those spellings, or it is a label guard nothing satisfies. This "+
			"axis declares %v.", slices.Sorted(maps.Keys(wireScalarSpellings)), declared)
	}
	return fmt.Sprintf("This axis declares the labels %v and no others, so nothing writes that one.", declared) + deadIfGuard
}

// TestUnstampableReportSaysOnlyWhatTheSweepRead holds the whole red for one
// guard, word for word.
//
// It is an equality against a literal on purpose, and the brittleness is the
// point: every sentence here is a claim about an emission a reader can check
// in one reading, and this gate has twice shipped one that was false. First
// that the decoder returns an error and its struct is unreachable, then —
// after that was fixed for the shape that demonstrated it — that the decoder
// "tests that value for equality" against the string, and that the comparison
// therefore answers the same way on every call. Both are false of a decoder
// discriminating on a property (`if kind == "premium"`), which is a shape the
// sweep cannot distinguish from a label guard, because comparedStrings never
// reads the other side of the operator.
//
// So the frame is held to what was read and the consequence is held to the
// condition it needs. A rewording that reintroduces either binding has to
// come through here, and no other test can see this string: the row below is
// exactly the shape TestUnstampableReasonNamesTheRightObstacle's general arm
// answers, and that test calls the reason clause directly and never sees the
// sentence around it.
func TestUnstampableReportSaysOnlyWhatTheSweepRead(t *testing.T) {
	alphabet := labelAlphabet{
		codegen.EntityNode: {"Person": true},
		codegen.EntityEdge: {"KNOWS": true},
	}
	d := entityDecoder{
		fn: "decodePerson", file: "models.go", entity: "Person",
		shape: codegen.EntityNode, guards: []string{"premium"},
	}

	report := unstampableReport("apache-age-pgx-v5", "alias_bare_variable_ambiguity", d, "premium", alphabet)

	require.Equal(t,
		"apache-age-pgx-v5 emits decodePerson in models.go for fixture alias_bare_variable_ambiguity. It fills "+
			"Person, which is a node type, so the only value that ever reaches it is a node and the labels it can "+
			"be handed are that axis's. Somewhere in its body a string is tested for equality against \"premium\", "+
			"which is not one of them. This axis declares the labels [Person] and no others, so nothing writes "+
			"that one. If that string is the wire label this decoder is guarding on, no value reaching it carries "+
			"the string: the guard answers the same way on every call and the branch behind it is dead. If it is "+
			"compared with something the decoder computed instead, it is not a label at all and this axis is the "+
			"wrong alphabet to have graded it against — which is a defect in this reading, and worth reporting as "+
			"one.",
		report,
		"the red this gate prints has changed. Check the new text sentence by sentence against a decoder that "+
			"compares a property rather than the wire label — the sweep cannot tell the two apart — and update "+
			"this literal only for text that is true of both")
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
// convention over *functions* has to hold for the sweep to see a decoder.
// Renaming the function changes the site the diagnostic names and nothing
// about how the function is classified.
//
// It does rest on a spelling, and the limit is worth stating because the
// convention this replaced rested on one too. resultEntities matches the
// identifiers a result type names against the keys of the shape map, with
// no type resolution behind it, so a decoder declared to return an alias or
// a wrapper — `type knowsRow = Knows`, `type knowsRow struct{ Knows }` — is
// not recognised as that entity's decoder. What follows from that is a
// refusal and not silence: the function is still yielded by
// packageLevelFuncs and still classified here, so it lands on the
// results-name-nothing arm below, where any guard it writes that is not one
// of the wire's fixed spellings refuses the emission. The residue the
// spelling leaves is a decoder the gate declines to grade, not one whose
// guards it files under the body that happens to hold it.
//
// That is the point. Recognition by the name pattern decode<T> omits a
// function it cannot resolve silently — from the per-axis census, from the
// guard count and from the alphabet check at the same time, so the
// extractor's own blind spot removes the numerator, the denominator and
// the correctness check together. The floor left standing is "some decoder
// was found", which any one surviving decoder satisfies.
//
// So the classification is total over what packageLevelFuncs yields, and
// every arm of it is stated:
//
//   - results naming exactly one prepared entity: that entity's decoder;
//   - results naming none: not a decoder, and then it may compare only
//     wireScalarSpellings, since any other string it tests is a guard this
//     gate has no verdict about;
//   - results naming two or more: no single axis to grade it against.
//
// The second arm is what "a decoder it cannot classify is a failure rather
// than an omission" means precisely, and the precision matters: a function
// this sweep does not recognise as a decoder is refused for the guards it
// writes, not for existing. One that compares no string at all, or only the
// wire's own spellings, is accepted — it carries no guard for an alphabet
// to reject, so there is nothing here to be wrong about. The scope of
// "guard" is comparedStrings' three equality shapes and the scope of
// "function" is what packageLevelFuncs yields; a string compared in a
// method's own body, or passed as an argument, is outside both, and both
// limits are stated under "What it does not decide" above.
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
// which renaming the function does not move, and their comparisons against
// agtype's fixed spellings of true, false and null are held by
// wireScalarSpellings instead.
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
						"%s emits %s, whose results name none of the entity types the shared derivation "+
							"prepared %v, and it tests %q for equality. This sweep reads every receiver-less "+
							"function declaration with a body, and every function literal at any depth inside "+
							"a declaration, and classifies each by what its results name: one naming exactly "+
							"one prepared entity is that entity's decoder, held to the labels that entity's "+
							"axis can carry; one naming two or more is refused below; and one naming none may "+
							"compare only the wire's own fixed spellings %v. This names none, so %q is a "+
							"guard nothing in this gate grades, and an unsatisfiable one written there would "+
							"pass unseen",
						target, site, slices.Sorted(maps.Keys(shapes)), guard,
						slices.Sorted(maps.Keys(wireScalarSpellings)), guard)
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
				"%s emits both %s and %s, and both name %s in their results. This sweep recognises a decoder by "+
					"the prepared entity its results name and reads nothing about calls, so it holds two "+
					"candidates for one entity's decoder and nothing it read tells them apart: two independent "+
					"decoders means one of them is a decoder no call site need use and its guards are a claim "+
					"about nothing, and a factory returning the other means one decoder read twice, once through "+
					"the wrapper and once through the literal it returns",
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

// whyDecoderListEmpties is what every whole-decoder comparison in this file
// is held up by. Each states the classified list against a written literal,
// and both sides go empty on one emission.
//
// The shape map is part of that edit rather than a separate one: left as
// written, the reconciliation at the end of emittedEntityDecoders reds on
// the entity the emptied emission no longer decodes, so the comparison is
// reached with two empty sides only when the emission, the shape map and
// the expected list are emptied together. That configuration is what this
// refuses; the one where the shape map survives is already refused there.
const whyDecoderListEmpties = "the comparison below states the whole classified list against a written one, " +
	"so it is answered by an empty list on both sides. Emptying this emission, its shape map and that " +
	"written list in a single edit is the run this refuses"

// TestSweepGradesTheNamedFunctionSpellingsAlike holds the two ways Go spells
// a package-level, receiver-less function *under a name* to one rule: they
// must be classified and graded identically, not merely both somehow
// noticed.
//
// The two named spellings are not all of them — see
// TestSweepReadsAFunctionLiteralAtAnyDepthOfADeclaration for the literals
// no name is bound to. What this one adds and
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
// positions is the defect: a sweep that type-asserts *ast.FuncLit on
// ValueSpec.Values[i] neither grades nor reports a literal one node deeper —
// behind parentheses, inside a composite literal, under a conversion — and a
// witness set drawn from the arms such code happens to have cannot discover
// the arm it does not have. The walk visits every node of the declaration,
// so there is no per-shape arm left to be missing, and one row nests a
// literal in shapes no arm ever named.
//
// The last rows are witnesses to what the walk reaches beyond a
// declaration's values. One puts the helper inside a package-level literal's
// body. Two put it in a signature, which nothing reached while the walk
// pruned at each literal and handed on only that literal's body — the same
// helper one level down was read, so the classification turned on where its
// holder was written. A signature's only expression slot is an array length
// and that has to be constant, so no compiling Go writes a literal in one;
// the sweep parses rather than typechecks, which is what lets those rows be
// written at all, as it lets the last row bind fewer names than it has
// values.
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
		{
			name: "inside the body of a package-level literal",
			decl: `var boot = func() { _ = []func(string) bool{` + helper + `} }`,
		},
		{
			name: "in the signature of a package-level literal",
			decl: `var boot = func(a [` + helper + `("")]int) {}`,
		},
		{
			name: "in the signature of a func declaration",
			decl: `func boot(a [` + helper + `("")]int) {}`,
		},
		{
			// notALiteral is undeclared, and the emission is parsed rather
			// than typechecked, which is what lets a name list shorter than
			// its value list reach the binding at all.
			name: "a value of a declaration binding fewer names than values",
			decl: `var personLabelOK = notALiteral(), ` + helper,
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

// TestSweepClassifiesAFunctionAlikeAtEveryDepth holds the sweep to one
// classification per function, decided by the function and not by where the
// emission writes it: the same receiver-less helper, byte for byte, must be
// refused with the same words at package level, inside a decoder's body,
// inside a method's body and inside another literal's body.
//
// The opposite rule cannot be pinned into correctness. Classifying a
// body-nested literal only when its results name a prepared entity, and
// merging it into its holder otherwise, is wrong in the direction rows
// asserting that merge cannot see: "names a prepared entity" is a test on
// the *spelling* of the result type, so a dead edge decoder returning
// `knowsRow` where `type knowsRow = Knows` is merged into the node decoder
// holding it and its relationship-axis guard is graded against the node
// alphabet, green. The identical function at package level is refused.
// Position decides the axis, which is exactly what a totality claim may not
// let happen, and no mutation of that rule finds it — the hole is in the
// rule.
//
// So this asserts the invariant that rule denies. The four positions are
// not an enumeration the totality rests on; the totality rests on
// packageLevelFuncs yielding every literal at every depth. They are the four
// the defect distinguished, and word-for-word equality is what makes the
// claim "classified alike" rather than "reddened in all four".
//
// The cost that rule avoids is real and is paid here: a helper closure that
// compares a string this gate has no verdict about is refused wherever it is
// written, including in a method a query surface owns. That is the same
// price every package-level function in an emission has paid since this gate
// existed, and the price of not paying it is three dead decoders passing
// green. The closures the backends do emit today are yielded and pass, which
// TestEmittedClosuresNameNoEntityAndCompareNoString measures per emission
// rather than restating here: an enumeration written in prose names one
// backend's closures and drops another's.
func TestSweepClassifiesAFunctionAlikeAtEveryDepth(t *testing.T) {
	shapes := map[string]codegen.EntityKind{"Person": codegen.EntityNode}
	// Every row writes this helper under this name, so each refusal names the
	// same site and the messages are comparable character by character. A
	// literal reported by position could not be: the line differs per row.
	const helper = `helper = func(label string) bool { return label == "NoSuchLabelAnywhere" }`

	rows := []struct{ name, emission string }{
		{
			name: "at package level",
			emission: `package emitted

var ` + helper + `

func decodePerson(raw []byte) (Person, error) {
	label := string(raw)
	if label != "Person" {
		return Person{}, nil
	}
	return Person{}, nil
}
`,
		},
		{
			name: "inside a decoder's body",
			emission: `package emitted

func decodePerson(raw []byte) (Person, error) {
	label := string(raw)
	` + helper + `
	if !helper(label) {
		return Person{}, nil
	}
	return Person{}, nil
}
`,
		},
		{
			name: "inside a method's body",
			emission: `package emitted

func decodePerson(raw []byte) (Person, error) {
	label := string(raw)
	if label != "Person" {
		return Person{}, nil
	}
	return Person{}, nil
}

type queries struct{}

func (q queries) listPeople(rel string) bool {
	` + helper + `
	return helper(rel)
}
`,
		},
		{
			name: "inside another literal's body",
			emission: `package emitted

func decodePerson(raw []byte) (Person, error) {
	label := string(raw)
	outer := func() bool {
		` + helper + `
		return helper(label)
	}
	if !outer() {
		return Person{}, nil
	}
	return Person{}, nil
}
`,
		},
	}

	refusals := make([]string, len(rows))
	for i, tc := range rows {
		t.Run(tc.name, func(t *testing.T) {
			files := []codegen.File{{Path: "models.go", Contents: []byte(tc.emission)}}

			msgs := recordedSweepRefusal(files, shapes)

			require.NotEmpty(t, msgs,
				"the sweep accepted an emission whose helper is written %s, so a string comparison written there "+
					"is graded by nothing and a decoder relocated into that position carries guards no axis holds",
				tc.name)
			joined := strings.Join(msgs, "\n")
			require.Contains(t, joined, "NoSuchLabelAnywhere",
				"the sweep refused the emission, but on some other arm than the ungraded comparison written %s",
				tc.name)
			refusals[i] = joined
		})
	}

	for i := 1; i < len(rows); i++ {
		require.Equal(t, refusals[0], refusals[i],
			"the same helper written %s is classified differently from the same helper written %s. Depth is not "+
				"something this gate decides anything by: a function's axis comes from what its results name, so "+
				"a rule that reads one position differently from another lets a decoder be relocated out of the "+
				"grading it fails and into a holder whose axis it passes",
			rows[i].name, rows[0].name)
	}
}

// TestSweepClassifiesANestedDecoderOnItsOwnAxis holds the other half: a
// function literal whose results name an entity is that entity's decoder
// wherever it is written, and its guards are graded on that entity's axis
// rather than on its holder's.
//
// Reading it as part of its holder is not a gap, it is a mis-grading, and it
// passes. A dead edge decoder written as a closure inside a node decoder has
// its guard collected into the node decoder's guard list, where the node
// alphabet declares it and every check goes green — on a decoder no value can
// satisfy, which is the one thing this gate exists to refuse. The
// reconciliation cannot catch it either: the entity is decoded by the real
// decoder, so the roll balances. That is the cross-axis swap the per-axis
// alphabet was built for, arriving through a boundary a walk is most likely
// to document as safe.
//
// So each row asserts both directions of the attribution. The nested decoder
// carries its own guard, and the holder does not: a guard read twice is a
// guard graded against an axis that is not its own, and the second row is
// where that lands on a satisfiable emission — "KNOWS" collected into
// decodePerson would redden a node decoder for a label the node axis cannot
// carry. The last assertion replays the grading the gate itself performs, so
// a row states which guards the alphabet rejects and not merely where they
// were filed.
func TestSweepClassifiesANestedDecoderOnItsOwnAxis(t *testing.T) {
	const emission = `package emitted

func decodePerson(raw []byte) (Person, error) {
	label := string(raw)
	if label != "Person" {
		return Person{}, nil
	}
	decodeKnows := func(raw []byte) (Knows, error) {
		l := string(raw)
		if l != %q {
			return Knows{}, nil
		}
		return Knows{}, nil
	}
	_ = decodeKnows
	return Person{}, nil
}
`
	shapes := map[string]codegen.EntityKind{"Person": codegen.EntityNode, "Knows": codegen.EntityEdge}
	// The alphabet a schema declaring one node type and one edge type
	// produces, so that the replay below is the gate's own grading and not a
	// second rule about strings.
	alphabet := labelAlphabet{
		codegen.EntityNode: {"Person": true},
		codegen.EntityEdge: {"KNOWS": true},
	}

	for _, tc := range []struct {
		name, guard string
		unstampable []string
	}{
		{name: "a relationship type its own axis carries", guard: "KNOWS"},
		{
			name: "a node label no edge ever carries", guard: "Person",
			unstampable: []string{"decodeKnows guards on Person"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			files := []codegen.File{{Path: "models.go", Contents: []byte(fmt.Sprintf(emission, tc.guard))}}

			msgs, decoders := recordedSweep(files, shapes)

			require.Empty(t, msgs,
				"the sweep refused an emission that decodes each prepared entity exactly once: %s",
				strings.Join(msgs, "\n"))
			requireSwept(t, len(decoders), "the sweep of this emission", whyDecoderListEmpties)
			require.Equal(t, []entityDecoder{
				{
					fn: "decodePerson", file: "models.go", entity: "Person",
					shape: codegen.EntityNode, guards: []string{"Person"},
				},
				{
					fn: "decodeKnows", file: "models.go", entity: "Knows",
					shape: codegen.EntityEdge, guards: []string{tc.guard},
				},
			}, decoders,
				"the closure filling Knows is not classified as the Knows decoder carrying %q. One entry means it was "+
					"read as part of decodePerson; a guard list of two on decodePerson means its comparison was "+
					"collected into its holder as well, which is grading an edge decoder's guard against the node "+
					"alphabet", tc.guard)

			var unstampable []string
			for _, d := range decoders {
				for _, guard := range d.guards {
					if !alphabet[d.shape][guard] {
						unstampable = append(unstampable, fmt.Sprintf("%s guards on %s", d.fn, guard))
					}
				}
			}
			require.Equal(t, tc.unstampable, unstampable,
				"grading each decoder's guards against its own axis, as TestEmittedDecodersGuardOnlyOnStampable"+
					"Labels does, does not reach the verdict this row is about")
		})
	}
}

// TestSweepReadsADecoderInAPackageLevelLiteralOnce holds the sweep to a
// decoder written in the body of a package-level function literal: read once,
// under its own name, on its own axis.
//
// That position is where a seam falls. A declaration walk that pruned at
// each literal it found and handed that literal's body to a second walk
// would reach what is below a package-level literal through that hand-off
// or not at all — and the literal's signature, which such a hand-off does
// not carry, not at all. One walk per declaration leaves no seam to pin.
//
// What this holds is that the position is read, and read once. Stop walking
// below the literal and this decoder is yielded by
// nothing — since the sweep stopped collecting a nested literal's strings
// into its holder, nothing else reads it — so the entity it fills goes
// undecoded and the reconciliation against codegen.Prepare refuses the
// emission. Yield it twice and the duplicate arm refuses it as two candidates
// for one entity's decoder, which is what the prune was there to prevent. So
// what is asserted is that the emission is accepted, with exactly the one
// decoder classified.
func TestSweepReadsADecoderInAPackageLevelLiteralOnce(t *testing.T) {
	// The nested decoder is the only one here on purpose: a visible second
	// decoder for Person would let the reconciliation balance without it,
	// which is the shape that leaves a laundered decoder green.
	const emission = `package emitted

var boot = func() {
	decodePerson := func(raw []byte) (Person, error) {
		label := string(raw)
		if label != "Person" {
			return Person{}, nil
		}
		return Person{}, nil
	}
	_ = decodePerson
}
`
	shapes := map[string]codegen.EntityKind{"Person": codegen.EntityNode}
	files := []codegen.File{{Path: "models.go", Contents: []byte(emission)}}

	msgs, decoders := recordedSweep(files, shapes)

	require.Empty(t, msgs,
		"the sweep refused an emission whose one decoder is written in the body of a package-level literal: %s",
		strings.Join(msgs, "\n"))
	requireSwept(t, len(decoders), "the sweep of this emission", whyDecoderListEmpties)
	require.Equal(t, []entityDecoder{{
		fn: "decodePerson", file: "models.go", entity: "Person",
		shape: codegen.EntityNode, guards: []string{"Person"},
	}}, decoders,
		"the decoder written inside the package-level literal is not read once as Person's decoder carrying its "+
			"own guard. The refusal assertion above stops this test first, so the sweep accepted this emission "+
			"and the difference is in what it made of the decoder rather than in whether it read one; the diff "+
			"below is that difference")
}

// TestSweepMarksEachYieldedFunctionByItsSpelling reads what packageLevelFuncs
// yields, rather than what the classification later makes of it: which
// functions arrive, in what order, under what name, and marked as which
// spelling.
//
// The mark is the one field of a yield the classification does not read.
// What acts on it is the corpus census, which counts the literals and skips
// everything else, so a literal this marks otherwise is one the census skips
// in silence: not counted, and neither of its assertions asked. The census
// reads what the corpus happens to write, so the mark on a spelling no
// backend uses today is asserted here or nowhere.
//
// The whole list is compared, so a function yielded twice, yielded not at all,
// or reported under a name that points at no site is a difference here. The
// two exclusions are in it as absences: a func declaration with no body, and a
// receiver form, are walked for the literals they hold and are not yielded
// under their own names.
func TestSweepMarksEachYieldedFunctionByItsSpelling(t *testing.T) {
	// A literal no name is bound to is reported by its line, so the lines this
	// source puts them on are part of what is asserted. Two are written in a
	// signature, which is the child a per-literal hand-off does not carry, and
	// one of those is in a declaration with no body — walked, and not yielded
	// under its name.
	//
	// The last declaration binds more names than it has values, a shape
	// go/parser accepts and boundLiteralNames' length guard refuses to pair.
	// Its literal is therefore reported by line. A guard that paired name i
	// with value i there would report it as `unpaired`, which is a difference
	// in the list below.
	const emission = `package emitted

func decodePerson(raw []byte) (Person, error) { return Person{}, nil }

func linknamed(a [func() int { return 1 }()]int) (Person, error)

var boot = func(a [func() int { return 1 }()]int) {
	hold := func() {}
	_ = hold
	_ = []func(){func() {}}
}

func (q queries) run() { _ = func() {} }

var unpaired, alsoUnpaired = func() {}
`
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "models.go", emission, parser.SkipObjectResolution)
	require.NoError(t, err)

	var yielded []string
	for _, fn := range packageLevelFuncs(fset, file) {
		yielded = append(yielded, fmt.Sprintf("%s literal=%t", fn.name, fn.literal))
	}

	requireSwept(t, len(yielded), "the yield of this emission",
		"the comparison below states the whole yielded list against a written one, so it is answered by "+
			"an empty list on both sides. No shape map is consulted here and no reconciliation runs, so "+
			"emptying this emission and that written list is the whole of the edit this refuses")

	require.Equal(t, []string{
		"decodePerson literal=false",
		"the function literal at line 5 literal=true",
		"boot literal=true",
		"the function literal at line 7 literal=true",
		"hold literal=true",
		"the function literal at line 10 literal=true",
		"the function literal at line 13 literal=true",
		"the function literal at line 15 literal=true",
	}, yielded,
		"the sweep hands the classification a different set of functions than this source writes. Each entry is "+
			"one function the classification is handed: the name is what its refusal points a reader at, and the "+
			"mark is what the corpus census filters on, so an entry marked as no literal is graded by the sweep "+
			"and counted by nothing")
}

// TestSweepRefusesADecoderWhoseResultTypeItCannotResolve holds the negative
// arm of that recognition to fail-closed, which is where this gate shipped a
// hole and where the ADR's "a decoder it cannot classify is refused rather
// than skipped" is either true or prose.
//
// resultEntities matches identifiers against the prepared entity names and
// resolves no types, so a decoder returning an alias or a wrapper of an
// entity struct is not that entity's decoder as far as this sweep can tell.
// The question is what happens next. Merging it into the body that holds it
// grades a dead edge decoder's relationship-axis guard against the node
// alphabet of its holder, green, with the real decodeKnows beside it so the
// reconciliation balances too — an unfillable decoder invisible at exit 0,
// one identifier over. So it is yielded and the classification refuses it.
//
// Each row therefore carries a real decodeKnows, so the reconciliation is
// not what fires and a row that goes green is a laundered decoder rather
// than a missing entity. The dead decoder guards on "Person": a node label,
// on a function whose results the sweep cannot place on any axis, which is
// precisely the string an alphabet check must never be handed by the wrong
// holder.
func TestSweepRefusesADecoderWhoseResultTypeItCannotResolve(t *testing.T) {
	shapes := map[string]codegen.EntityKind{"Person": codegen.EntityNode, "Knows": codegen.EntityEdge}
	const prologue = `package emitted

%s

func decodeKnows(raw []byte) (Knows, error) {
	if string(raw) != "KNOWS" {
		return Knows{}, nil
	}
	return Knows{}, nil
}
`
	const deadBody = `		if string(raw) != "Person" {
			return knowsRow{}, nil
		}
		return knowsRow{}, nil`

	for _, tc := range []struct{ name, spelling, holder string }{
		{
			name:     "an alias of the entity, nested in a node decoder",
			spelling: "type knowsRow = Knows",
			holder: `
func decodePerson(raw []byte) (Person, error) {
	deadKnows := func(raw []byte) (knowsRow, error) {
` + deadBody + `
	}
	_ = deadKnows
	if string(raw) != "Person" {
		return Person{}, nil
	}
	return Person{}, nil
}
`,
		},
		{
			name:     "a struct embedding the entity, nested in a node decoder",
			spelling: "type knowsRow struct{ Knows }",
			holder: `
func decodePerson(raw []byte) (Person, error) {
	deadKnows := func(raw []byte) (knowsRow, error) {
` + deadBody + `
	}
	_ = deadKnows
	if string(raw) != "Person" {
		return Person{}, nil
	}
	return Person{}, nil
}
`,
		},
		{
			name:     "an alias of the entity, in a dispatch table a method returns",
			spelling: "type knowsRow = Knows",
			holder: `
func decodePerson(raw []byte) (Person, error) {
	if string(raw) != "Person" {
		return Person{}, nil
	}
	return Person{}, nil
}

type decoderSet struct{}

func (d decoderSet) byLabel() map[string]func([]byte) (knowsRow, error) {
	return map[string]func([]byte) (knowsRow, error){
		"k": func(raw []byte) (knowsRow, error) {
` + deadBody + `
		},
	}
}
`,
		},
		{
			name:     "an alias of the entity, at package level",
			spelling: "type knowsRow = Knows",
			holder: `
func deadKnows(raw []byte) (knowsRow, error) {
` + deadBody + `
}

func decodePerson(raw []byte) (Person, error) {
	if string(raw) != "Person" {
		return Person{}, nil
	}
	return Person{}, nil
}
`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := fmt.Sprintf(prologue, tc.spelling) + tc.holder
			files := []codegen.File{{Path: "models.go", Contents: []byte(source)}}

			msgs := recordedSweepRefusal(files, shapes)

			require.NotEmpty(t, msgs,
				"the sweep accepted an emission holding a dead edge decoder whose result type is %s. Every prepared "+
					"entity is decoded here, so nothing reddens by absence: accepting it means the decoder's "+
					"\"Person\" guard was collected into whatever body holds it and graded against that body's "+
					"axis, which is the cross-axis swap this gate exists to refuse", tc.name)
			joined := strings.Join(msgs, "\n")
			require.Contains(t, joined,
				"whose results name none of the entity types the shared derivation prepared [Knows Person]",
				"the sweep refused the emission, but on some other arm than the one an unresolvable result type "+
					"reaches, so this row is not witnessing what it says it witnesses")
			require.Contains(t, joined, `it tests "Person" for equality`,
				"the refusal does not name the guard it refused over. The string the dead decoder compares is the "+
					"whole finding: a refusal that omits it sends a reader looking for a different defect")
		})
	}
}

// recordedSweepRefusal runs the sweep over a synthetic emission and returns
// what it reported instead of failing the caller's test, empty for one it
// accepted.
func recordedSweepRefusal(files []codegen.File, shapes map[string]codegen.EntityKind) []string {
	msgs, _ := recordedSweep(files, shapes)
	return msgs
}

// recordedSweep runs the sweep over a synthetic emission and returns both
// what it refused and what it classified, so a test can assert on an
// emission the sweep accepts as well as on one it rejects. The decoders are
// what the sweep had built when it stopped, so they are meaningful only
// alongside an empty refusal.
//
// Every caller goes through this one call site on purpose. testify writes the
// caller's line into the message it reports, so two sweeps invoked from two
// lines would differ in the trace while agreeing on everything a test here
// asks about.
func recordedSweep(files []codegen.File, shapes map[string]codegen.EntityKind) ([]string, []entityDecoder) {
	rec := &recordingT{}
	var out []entityDecoder
	func() {
		defer func() {
			if r := recover(); r != nil {
				if _, ok := r.(failedNow); !ok {
					panic(r)
				}
			}
		}()
		out = emittedEntityDecoders(require.New(rec), "probe-backend", files, shapes)
	}()
	return rec.msgs, out
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
// The duplicate arm is exercised three times, on one emission written three
// ways: the second decoder beside the first, the same second decoder as a
// value in a package-level dispatch table, and the same table returned from a
// method. That set is a claim the arms alone do not make — that the refusal is
// a property of the emission and not of how it is spelled. Each of the last
// two are how a decoder no value can satisfy reaches green while its visible
// twin does not: a sweep reading only a declaration's top-level value misses
// the table, and one skipping a method before walking anything misses a
// receiver-less literal written inside one. The reconciliation cannot catch
// either: the entity is decoded by the visible decoder, so the roll balances.
//
// The method row is not the method exclusion being walked back. What escapes
// there has no receiver — it is exactly the literal the walk exists to
// classify — and a decoder is graded on the axis of what it fills wherever the
// emission writes it. The exclusion is over the receiver form itself, and the
// far side of that boundary — a decoder that *is* a method — is still out
// (gqlc-9xy0).
func TestSweepRefusesEveryEmissionItCannotClassify(t *testing.T) {
	const prologue = `package emitted

func decodePerson(raw []byte) (Person, error) { return Person{}, nil }

func decodeKnows(raw []byte) (Knows, error) { return Knows{}, nil }
`
	shapes := map[string]codegen.EntityKind{"Person": codegen.EntityNode, "Knows": codegen.EntityEdge}

	for _, tc := range []struct{ name, emission, want, absent string }{
		{
			name:     "a function whose results name two entities",
			emission: prologue + "\nfunc decodeBoth(raw []byte) (Person, Knows, error) { return Person{}, Knows{}, nil }\n",
			want:     "cannot be assigned an axis to be graded against",
		},
		{
			name:     "a second decoder for one entity",
			emission: prologue + "\nfunc decodePersonAgain(raw []byte) (Person, error) { return Person{}, nil }\n",
			want:     "holds two candidates for one entity's decoder",
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
			want: "holds two candidates for one entity's decoder",
		},
		{
			name: "a second decoder for one entity, spelled as a value in a dispatch table a method returns",
			emission: prologue + `
type decoderSet struct{}

func (d decoderSet) byLabel() map[string]func([]byte) (Person, error) {
	return map[string]func([]byte) (Person, error){
		"Person": func(raw []byte) (Person, error) {
			label := string(raw)
			if label != "NoSuchLabelAnywhere" {
				return Person{}, nil
			}
			return Person{}, nil
		},
	}
}
`,
			want: "holds two candidates for one entity's decoder",
		},
		{
			// A factory and the decoder it returns are one decoder, not two,
			// and this arm cannot tell. The refusal is the safe direction and
			// stands; what this row pins is that the words are true of the
			// emission in front of them. Saying "both return Person" of a
			// function returning func([]byte) (Person, error), or calling a
			// factory and its own returned literal "not decidable" when the
			// factory returns it, is a checkable falsehood — the failure mode
			// this gate's own docstring says it is least allowed to have.
			name: "a factory and the decoder it returns, which this arm cannot tell from two decoders",
			emission: prologue + `
func personDecoder() func([]byte) (Person, error) {
	return func(raw []byte) (Person, error) {
		label := string(raw)
		if label != "Person" {
			return Person{}, nil
		}
		return Person{}, nil
	}
}
`,
			want:   "holds two candidates for one entity's decoder",
			absent: "both return",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			files := []codegen.File{{Path: "models.go", Contents: []byte(tc.emission)}}

			msgs := recordedSweepRefusal(files, shapes)

			require.NotEmpty(t, msgs,
				"the sweep accepted an emission holding %s, so it classified something it has no verdict for", tc.name)
			joined := strings.Join(msgs, "\n")
			require.Contains(t, joined, tc.want,
				"the sweep refused the emission, but on some other arm than the one %s is meant to reach", tc.name)
			if tc.absent == "" {
				return
			}
			require.NotContains(t, joined, tc.absent,
				"the refusal for %s says %q of an emission where it is checkable and false: a factory's results "+
					"name Person, they do not return one. The arm may refuse — it cannot tell a wrapper from a "+
					"twin — but it may only say what the sweep read", tc.name, tc.absent)
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
//
// literal records which spelling it arrived as. Nothing in the classification
// reads it — that is the point of the flattening. It is carried for the
// corpus census, TestEmittedClosuresNameNoEntityAndCompareNoString, which is
// a claim about the literals alone.
type emittedFunc struct {
	name    string
	typ     *ast.FuncType
	body    *ast.BlockStmt
	literal bool
}

// packageLevelFuncs is every receiver-less function one emitted file
// writes at package level: every func declaration, and every function
// literal any other declaration holds, at whatever depth it holds it.
//
//	func decodePost(raw []byte) (Post, error) { … }
//	var decodePost = func(raw []byte) (Post, error) { … }
//	var byLabel = map[string]func([]byte) (Post, error){"Post": func…}
//
// The third line is why this walks rather than matches. Reading the first
// alone, or the first two, is a narrower claim than totality and does not
// look it: a sweep that type-asserts *ast.FuncLit on the top-level value
// of a ValueSpec sees a literal behind parentheses, inside a slice, map or
// struct literal, or under a conversion as nothing at all — five
// positions, none reported — and a package-level dispatch table is the
// shape a decoder is most plausibly relocated into (gqlc-9xy0). A second
// decoder hidden in one reaches green while the same emission spelled
// visibly is refused: the refusal does not survive a change of spelling.
//
// So the rule is not a list of positions. Every node of every declaration
// is visited by one walk, and every function literal found there is a
// function this gate classifies — which is a claim about the walk rather
// than about the shapes anyone thought to enumerate, and there is no
// per-shape arm left that could be the missing one. A func declaration's
// body is no different from a var's value: the walk reaches a literal at
// any depth of either, unfiltered.
//
// Classifying is not the same as grading. What a yielded literal is — an
// entity's decoder, or a function whose results name no prepared entity and
// which may therefore compare only the wire's own scalar spellings — is
// decided in emittedEntityDecoders by resultEntities and nowhere here. This
// function takes no shape map for that reason: what it yields cannot depend
// on what the derivation prepared, so there is no argument here that a
// decoder could be spelled around.
//
// Filtering the yield on "does this literal fill an entity" routes the
// totality claim around itself. Such a filter asks whether the literal's
// result type is *spelled* with a prepared entity's identifier, so
// `type knowsRow = Knows` or `type knowsRow struct{ Knows }` makes the
// answer no; the literal is then merged into its holder, its guards graded
// on the holder's axis, and a dead edge decoder guarding on a node label
// passes green. The identical function written at package level is
// refused. That asymmetry is the
// defect: a literal's axis has to come from the literal. Yielding every
// literal and letting the classification refuse what it cannot place is
// the rule that gets there, and it says nothing about nesting.
//
// The filter buys one thing — a query-side helper closure comparing a
// string this gate has no verdict about is refused here rather than left to
// its holder. That cost is real and it is the price of the totality claim,
// which is the same price every package-level function in an emission has
// paid since this gate existed. What the backends emit today does pay it
// without reddening, and that is measured rather than enumerated:
// TestEmittedClosuresNameNoEntityAndCompareNoString sweeps each emission the
// valid corpus produces and holds every literal yielded here to naming no
// prepared entity in its results and comparing no string, so each lands on
// the results-name-nothing arm with no guard to grade. An enumeration in
// prose here would name one backend's transaction callbacks and drop
// another backend's element decoders; the census cannot drop a backend
// without reddening its floor.
//
// A method is not yielded, and that exclusion is the claim's edge rather
// than a blind spot of the same kind: a receiver form belongs to the
// emitted query surface, which the gate states outright it holds nothing
// about (gqlc-9xy0). It is still walked, because a receiver-less function
// literal is not a receiver form however it is reached — skipping the
// method whole made a dispatch table returned from one invisible to the
// census, which is not the boundary this exclusion draws. The width is the
// same in both directions: a receiver form is read as a decoder's *holder*
// and is never graded as a decoder itself. That is an exclusion this gate
// draws, not a property it enforces — nothing here refuses a method whose
// results name a prepared entity. A backend that moved a decoder onto a
// receiver would take it out of this census, and what would redden is the
// reconciliation against codegen.Prepare, on that entity's absence, unless
// some yielded function also names it.
func packageLevelFuncs(fset *token.FileSet, file *ast.File) []emittedFunc {
	var out []emittedFunc
	litName := func(names map[*ast.FuncLit]string, lit *ast.FuncLit) string {
		if name, named := names[lit]; named {
			return name
		}
		return fmt.Sprintf("the function literal at line %d", fset.Position(lit.Pos()).Line)
	}
	// literals walks one declaration whole, yielding every function literal
	// written anywhere in it. The walk never stops descending, so a literal
	// wrapped in another literal is reached by the walk that reached its
	// holder, and ast.Inspect visits each node once, so each literal is
	// yielded once. It needs no recursion of its own and no hand-off.
	//
	// One walk per declaration, and the count is load-bearing in both
	// directions. Two walks that prune at each literal and hand that
	// literal's *body* to the second lose its signature, which no such
	// hand-off carries: a helper written inside one is yielded by neither
	// walk while the identical helper one level down is yielded, so the
	// classification turns on where the holder sits rather than on the
	// helper. Two walks over the whole declaration yield every literal twice
	// instead.
	literals := func(decl ast.Decl) {
		names := boundLiteralNames(decl)
		ast.Inspect(decl, func(n ast.Node) bool {
			lit, ok := n.(*ast.FuncLit)
			if !ok {
				return true
			}
			out = append(out, emittedFunc{name: litName(names, lit), typ: lit.Type, body: lit.Body, literal: true})
			return true
		})
	}
	for _, decl := range file.Decls {
		// A nil body is a declaration whose implementation is elsewhere
		// (assembly, a linkname): it holds no string to grade, and reading
		// its results as a decoder's would let a signature with no
		// implementation balance the reconciliation against codegen.Prepare.
		// It is not yielded under its own name; it is walked like every other
		// declaration.
		if fn, ok := decl.(*ast.FuncDecl); ok && fn.Recv == nil && fn.Body != nil {
			out = append(out, emittedFunc{name: fn.Name.Name, typ: fn.Type, body: fn.Body})
		}
		literals(decl)
	}
	return out
}

// boundLiteralNames is the declared name of each function literal one
// declaration binds to one directly, wherever inside it the binding is
// written, so that a function spelled as a var or assigned to a local
// reports under the name it is called by rather than under its position.
//
// A literal any deeper — an element of a table, the operand of a
// conversion — has no name of its own to report, and inventing one out of
// the declaration that encloses it would name something a reader cannot
// grep for. Those are reported by position instead, as is a literal bound
// to the blank identifier: `_` is a binding no call site can use, so
// reporting under it points a reader at no site in particular.
func boundLiteralNames(root ast.Node) map[*ast.FuncLit]string {
	names := map[*ast.FuncLit]string{}
	bind := func(lhs []ast.Expr, rhs []ast.Expr) {
		// This is a bounds guard. `var a = f(), g()` parses — go/parser does
		// not typecheck — and there the range below would index lhs past its
		// end. That is the one direction that can index out of range, since
		// the range is over rhs.
		//
		// Bounds alone would be `<`. It is `!=` so that the other direction —
		// more names than values — binds nothing rather than pairing name i
		// with value i and reporting a literal under lhs[i].
		// TestSweepMarksEachYieldedFunctionByItsSpelling writes that shape.
		if len(lhs) != len(rhs) {
			return
		}
		for i, v := range rhs {
			lit, isLit := v.(*ast.FuncLit)
			if !isLit {
				continue
			}
			if ident, ok := lhs[i].(*ast.Ident); ok && ident.Name != "_" {
				names[lit] = ident.Name
			}
		}
	}
	ast.Inspect(root, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.ValueSpec:
			lhs := make([]ast.Expr, 0, len(node.Names))
			for _, name := range node.Names {
				lhs = append(lhs, name)
			}
			bind(lhs, node.Values)
		case *ast.AssignStmt:
			bind(node.Lhs, node.Rhs)
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
// The refusal is what reports that, so the refusal is what this asserts on.
// Dropping either step reddens on the duplicate arm — measured: "probe-backend
// emits both driverNode in models.go and decodeNode in models.go, and both
// name Node in their results". Handing the sweep this test's own require would
// make that refusal the failure and leave anything written after it
// unreachable, which is what a Len(decoders, 1) here was: it could not fail,
// because len 2 is the duplicate arm and len 0 is the reconciliation, and both
// stop the sweep first. Recording the refusal instead puts the kill in this
// test, where the message names the step that stopped being taken.
//
// The two rows do not kill the same way and the quoted message is one row's.
// Dropping the qualified-type step reddens the qualified row and leaves the
// type-parameter row green; dropping the type-parameter step does the
// reverse. Each row kills its own step, which is the point — but a reader who
// took the quote for both rows' behaviour would be reading a claim neither
// row makes. Function literals cannot carry type parameters in Go, so the
// TypeParams arm is reached only through a func declaration, and the
// type-parameter row is written as one.
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

			msgs, decoders := recordedSweep(files, shapes)

			require.Empty(t, msgs,
				"the sweep read %s as decoding the emission's own Node, so a helper that cannot fill an entity "+
					"struct is graded as though it did: %s", tc.name, strings.Join(msgs, "\n"))
			requireSwept(t, len(decoders), "the sweep of this emission", whyDecoderListEmpties)
			require.Equal(t, []entityDecoder{{
				fn: "decodeNode", file: "models.go", entity: "Node",
				shape: codegen.EntityNode, guards: []string{"Node"},
			}}, decoders,
				"the emission's one decoder is not read as the sole decoder of Node, so what %s is stepped over "+
					"in favour of is not what this row means to assert", tc.name)
		})
	}
}

// comparedStrings is every string literal a function body tests for
// equality: an operand of == or !=, or a switch case value. What it is
// compared *with* is not read — the other operand may be the wire label,
// or a property the decoder pulled out of the map, and nothing here tells
// them apart, which is why the report says a string was compared rather
// than that the wire value was tested against it. Those are the shapes a
// label guard takes; a literal passed as an argument is
// almost always a property key or a format string, and holding those to a
// label alphabet would redden on every emission.
//
// The exception is deliberate and stated where the gate states its scope:
// AGE's carrier annotation rides in as an argument — agtypeEntity(raw,
// "::vertex") — so swapping it makes a decoder as dead as an unstampable
// guard would and nothing here reads it. That one is held behaviourally by
// internal/codegen/age's TestEmittedHelpersDecodeTheAgtypeCorpus.
//
// It stops at every function literal, which is exactly the set
// packageLevelFuncs yields out of a body. The two move together on purpose:
// a comparison is read by the innermost function this sweep yields that
// encloses it and by no other, so no string is read as two functions' guards
// at once. A string compared in a method's own body, outside any literal, is
// read by none of them — the method is not yielded, and that is the scope
// exclusion stated above rather than a second attribution.
//
// Collecting a nested decoder's guards into its holder as well
// is how a "Person" guard on a dead edge decoder came to be graded against
// the node alphabet and passed, and it is the same misattribution in the
// other direction that would redden a satisfiable one — read as the
// enclosing node decoder's, an edge decoder's "KNOWS" is a label the node
// axis cannot carry.
//
// Stopping at a literal that fills no entity is what a narrower rule got
// wrong in both halves at once. Yielding only entity-filling literals let a
// dead decoder whose result type is spelled through an alias be read as part
// of its holder, where its guard was graded on the holder's axis; stopping
// only at entity-filling literals would collect a nested decoder's guards
// into its holder as well. Neither half is safe without the other.
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
		case *ast.FuncLit:
			return false
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

			requireSwept(t, len(decoders), "the sweep of this emission", whyDecoderListEmpties)
			require.Equal(t, []entityDecoder{{
				fn: "decodePerson", file: "models.go", entity: "Person",
				shape: codegen.EntityNode, guards: []string{"Wanted"},
			}}, decoders,
				"a decoder testing the wire label against a literal written as %s is read as guarding on nothing, "+
					"so a literal no value on its axis can carry is never held to that axis's alphabet. The whole "+
					"decoder is stated rather than its guards alone because indexing one out of the slice needs a "+
					"length check that cannot fail: len 2 is the duplicate arm and len 0 is the reconciliation, and "+
					"both stop the sweep before it returns", tc.name)
		})
	}
}
