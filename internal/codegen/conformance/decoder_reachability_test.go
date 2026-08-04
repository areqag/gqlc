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

// decodePrefix is how the emission names an entity decoder: decode<T> for
// the struct T it fills. A backend that spelled it otherwise would
// contribute nothing to the sweep below, which is what the per-target
// decoder census refuses.
const decodePrefix = "decode"

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

// entityDecoder is one emitted decode<T> function: the file and entity it
// belongs to, and the string literals its body tests the wire value for
// equality against.
type entityDecoder struct {
	file   string
	entity string
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
// emitted package, finds every package-level func named decode<T> where T
// is a type that package declares, and collects the string literals that
// function compares for equality (an == or != operand, or a switch case
// value). Every such literal must be a label the fixture's schema declares:
// a member of the union of KeyLabels.Split() and CompleteLabels.Split()
// over its node and edge types.
//
// A literal outside that set is a guard the decoder can never pass. The
// concrete instance is graph.LabelSetKey's own join spelling: a node type
// keyed on Employee and Person has key "Employee&Person", and a decoder
// guarding on that string is asking a single wire label to equal the join
// of two. No vertex and no edge carries it, and on Apache AGE none could —
// AGE stamps exactly one label and has no syntax for a second — so the
// decoder is dead on arrival while gqlc generate still exits 0.
//
// # What it does not decide
//
// It is not a reachability analysis. It says nothing about guards that are
// not string equality — no arithmetic, no length, no nil check, no
// interprocedural condition. It says nothing about whether a guard is
// *precise*: a decoder for a two-label entity guarding on just one of its
// two labels is reachable, so this passes it, and nothing here notices that
// it would also accept the wrong entity. It says nothing about the neo4j
// targets' decoders, which carry no label guard at all and therefore accept
// a node of any label — a distinct defect on a distinct axis, filed rather
// than fixed here. And it decides satisfiability against the label alphabet
// the schema declares, which is the alphabet gqlc's own write path can
// stamp; a graph a foreign writer populated is outside its terms.
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

	accepted := make(map[string]int, len(targets))
	decoders := make(map[string]int, len(targets))
	guarded := make(map[string]int, len(targets))

	for _, dir := range s.validFixtures() {
		fixture := filepath.Base(dir)
		s.Run(fixture, func() {
			m := s.loadManifest(dir)
			sch := s.loadSchema(dir)
			in := codegen.Input{Schema: sch, Queries: s.loadNamedQueries(dir, m, sch)}
			alphabet := schemaLabelAlphabet(sch)

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
				for _, d := range emittedEntityDecoders(s.Require(), files) {
					decoders[target]++
					if len(d.guards) > 0 {
						guarded[target]++
					}
					for _, guard := range d.guards {
						s.Require().True(alphabet[guard],
							"%s emits %s in %s for fixture %s, which returns an error unless the wire value's "+
								"label equals %q. %s Nothing this backend can stamp carries that string, so every "+
								"call to %s fails and the struct it fills is unreachable.",
							target, decodePrefix+d.entity, d.file, fixture, guard,
							unstampableBecause(guard, alphabet), decodePrefix+d.entity)
					}
				}
			}
		})
	}

	guarding := 0
	for _, target := range targets {
		s.Require().NotZero(accepted[target],
			"%s generated for no fixture in the corpus, so this sweep read none of its emissions", target)
		s.Require().NotZero(decoders[target],
			"%s emitted no decode<T> function over the whole corpus: either it names its decoders otherwise, "+
				"in which case this sweep no longer finds them, or it emits none, in which case it decodes nothing",
			target)
		if labelGuardingTargets[target] {
			guarding++
			s.Require().Equal(decoders[target], guarded[target],
				"%s is recorded as gating its entity decoders on the wire label, but %d of the %d it emits over "+
					"the corpus test no string for equality: either the label check has been dropped from the "+
					"emission, or it is written in a shape this sweep does not read, and either way the guards "+
					"this gate believes it is checking are not being checked",
				target, decoders[target]-guarded[target], decoders[target])
			continue
		}
		s.Require().Zero(guarded[target],
			"%s is recorded as emitting no label guard, but %d of the %d decoders it emits test a string for "+
				"equality; enrol it in labelGuardingTargets so its guards are held to the schema's label alphabet",
			target, guarded[target], decoders[target])
	}
	s.Require().NotZero(guarding,
		"no registered backend is recorded as guarding on a label, so this gate examined no guard at all")
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
				if emits {
					emitting++
				}
				s.Require().Equal(multiLabelEmittingTargets[target], emits,
					"%s %s fixture %s, which declares %s; the posture ledger records that it %s such a schema. "+
						"A backend that emits for one owes a decoder whose label guard some value it can stamp "+
						"satisfies, which is what TestEmittedDecodersGuardOnlyOnStampableLabels then holds it to",
					target, verdict(emits), fixture, strings.Join(offenders, " and "),
					verdict(multiLabelEmittingTargets[target]))
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

// schemaLabelAlphabet is every label one schema declares, split out of the
// key sets that hold them. This is the alphabet a backend's own write path
// can stamp on a vertex or an edge for this schema, and therefore the whole
// set of strings a decoder's label guard can be satisfied by.
//
// CompleteLabels is folded in alongside KeyLabels because a node type's
// identity is its key set while what a value carries is its complete set,
// so a guard naming an implied label is reachable and must not be read as
// an offender.
func schemaLabelAlphabet(sch schema.Schema) map[string]bool {
	out := make(map[string]bool)
	add := func(k graph.LabelSetKey) {
		for _, label := range k.Split() {
			out[label] = true
		}
	}
	for _, n := range sch.Nodes {
		add(n.KeyLabels)
		add(n.CompleteLabels)
	}
	for _, e := range sch.Edges {
		add(e.KeyLabels)
		add(e.CompleteLabels)
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

// unstampableBecause is the reason clause for a guard the schema's label
// alphabet does not hold. The join-spelling arm is called out separately
// because it is the one whose unsatisfiability is mechanical rather than
// corpus-relative: graph.LabelSetKey joins a label set with "&", and a
// single wire label cannot equal the join of two whatever the schema
// declares.
func unstampableBecause(guard string, alphabet map[string]bool) string {
	declared := slices.Sorted(maps.Keys(alphabet))
	if parts := graph.LabelSetKey(guard).Split(); len(parts) > 1 {
		return fmt.Sprintf("That is graph.LabelSetKey's join spelling of the %d-label set %v — a label set, not a "+
			"label — and a wire value carries one label, so no single label equals it. The schema declares %v.",
			len(parts), []string(parts), declared)
	}
	return fmt.Sprintf("The schema declares the labels %v and no others, so nothing writes that one.", declared)
}

// emittedEntityDecoders extracts one backend's entity decoders out of one
// emission: every package-level, receiver-less func named decode<T> where T
// is a type the same package declares, paired with the string literals its
// body tests for equality.
//
// Keying on a declared type is what separates an entity decoder from the
// backend's own scalar helpers, whose names (agtypeInt64, agtypeString)
// carry the wire vocabulary rather than a struct — those compare against
// agtype's fixed spellings of true and false, which are not labels and are
// not the emission's to choose.
func emittedEntityDecoders(r *require.Assertions, files []codegen.File) []entityDecoder {
	fset := token.NewFileSet()
	parsed := make(map[string]*ast.File, len(files))
	declared := make(map[string]bool)
	for _, f := range files {
		file, err := parser.ParseFile(fset, f.Path, f.Contents, parser.SkipObjectResolution)
		r.NoError(err, "parsing emitted %s", f.Path)
		parsed[f.Path] = file
		for _, decl := range file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.TYPE {
				continue
			}
			for _, spec := range gen.Specs {
				ts, ok := spec.(*ast.TypeSpec)
				r.True(ok, "%s: type declaration holds a %T", f.Path, spec)
				declared[ts.Name.Name] = true
			}
		}
	}

	var out []entityDecoder
	for _, f := range files {
		for _, decl := range parsed[f.Path].Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv != nil || fn.Body == nil {
				continue
			}
			entity, ok := strings.CutPrefix(fn.Name.Name, decodePrefix)
			if !ok || !declared[entity] {
				continue
			}
			out = append(out, entityDecoder{file: f.Path, entity: entity, guards: comparedStrings(r, fn.Body)})
		}
	}
	return out
}

// comparedStrings is every string literal a function body tests a value for
// equality against: an operand of == or !=, or a switch case value. Those
// are the shapes a guard takes; a literal passed as an argument is a key or
// a format string and decides nothing.
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
