package age_test

import (
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/areqag/gqlc/internal/codegen"
	"github.com/areqag/gqlc/internal/codegen/age"
	"github.com/areqag/gqlc/internal/graph"
	"github.com/areqag/gqlc/internal/schema"
)

// multiLabelOffender is one type a schema keys on more than one label,
// read off the parsed schema rather than off the emission. The two
// derivations are independent, so a diagnostic that agrees with this one
// is not agreeing with a copy of itself.
type multiLabelOffender struct {
	name   string
	labels []string
}

// multiLabelOffenders partitions a schema's node types into those keyed
// on more than one label and those keyed on exactly one. Edge types are
// not partitioned here: the GQL grammar admits no `&` in an edge's key
// label set — `-[:WORKED_FOR&FOR]->` fails to parse — so no schema can
// present a multi-label edge to be classified.
func multiLabelOffenders(sch schema.Schema) (offenders []multiLabelOffender, single []string) {
	for key, nt := range sch.Nodes {
		labels := key.Split()
		name := nt.Name
		if name == "" {
			name = string(key)
		}
		if len(labels) > 1 {
			offenders = append(offenders, multiLabelOffender{name: name, labels: labels})
			continue
		}
		single = append(single, name)
	}
	slices.SortFunc(offenders, func(a, b multiLabelOffender) int { return strings.Compare(a.name, b.name) })
	slices.Sort(single)
	return offenders, single
}

// requireLabelsDistinguishable holds the fixtures to the property that
// makes the label sweep mean anything: a type whose name spells its own
// labels — PersonEmployee over Person and Employee — satisfies a
// substring search for each of them whether or not the diagnostic prints
// a single label, so the sweep would pass a message that dropped them.
// Asserting the disjointness is what keeps that from being a fixture
// detail a later rename can quietly undo.
func (s *EmissionSuite) requireLabelsDistinguishable(fixture string, offenders []multiLabelOffender) {
	for _, o := range offenders {
		for _, label := range o.labels {
			s.Require().NotContains(o.name, label,
				"fixture %s names a type %s that spells its own label %s, so a diagnostic omitting the label would still be found in the name",
				fixture, o.name, label)
		}
	}
}

// teamQueries is a batch of n read queries over the single-label type
// every multi-label fixture below declares beside its offenders. None of
// them projects an offending type, which is the whole point: the batch
// names only columns this backend serves.
func teamQueries(n int) []codegen.NamedQuery {
	out := make([]codegen.NamedQuery, 0, n)
	for i := range n {
		out = append(out, readQuery("TeamNames"+strconv.Itoa(i), scalarColumn("t.name", graph.TypeString)))
	}
	return out
}

// TestMultiLabelRefusalReachesPastTheColumnsTheBatchProjects is the
// posture this backend takes, asserted as behaviour rather than as
// prose: a schema carrying one multi-label type refuses generation for
// the whole batch, including queries that project only types this
// backend represents perfectly well.
//
// The control arm is what stops the assertion being "AGE refuses this
// input for some reason": the same batch against the same schema with
// the multi-label type dropped generates. So the refusal is caused by
// the type the batch never names, which is the claim (ADR 0027).
func (s *EmissionSuite) TestMultiLabelRefusalReachesPastTheColumnsTheBatchProjects() {
	in := s.inputFrom(filepath.Join("testdata", "multi_label_with_collateral.gql"))
	in.Queries = teamQueries(3)

	files, err := age.New().Generate(in)
	s.Require().Error(err)
	s.Require().Nil(files, "a refused schema must not return a partial file set")
	s.Require().ErrorIs(err, age.ErrUnsupportedSchema)

	served := in
	served.Schema = withoutMultiLabelTypes(in.Schema)
	s.Require().NotEqual(len(in.Schema.Nodes), len(served.Schema.Nodes),
		"the control schema must differ from the refused one, or it proves nothing")
	_, servedErr := age.New().Generate(served)
	s.Require().NoError(servedErr,
		"the same batch generates once the type it never projects is gone, so the multi-label type is what refused it")
}

// withoutMultiLabelTypes is the schema with every node type keyed on
// more than one label removed, leaving the batch and every other type
// untouched.
func withoutMultiLabelTypes(sch schema.Schema) schema.Schema {
	out := sch
	out.Nodes = make(map[graph.LabelSetKey]schema.NodeType, len(sch.Nodes))
	for key, nt := range sch.Nodes {
		if len(key.Split()) > 1 {
			continue
		}
		out.Nodes[key] = nt
	}
	return out
}

// multiLabelFixtures is every fixture the multi-label sweeps run over,
// selected by the naming convention rather than listed. A third
// multi-label fixture is swept by having been named for what it is,
// which is the property a hand-maintained slice does not have.
//
// A bare glob over testdata would be wrong: the directory holds fixtures
// for other tests, and a schema with no multi-label type fails the
// sweep's own preconditions. So the convention carries the selection —
// and a convention is only as good as its exhaustiveness, because a glob
// cannot see the fixture that should have matched and does not. That
// half is TestEveryMultiLabelFixtureIsNamedByTheConvention, and neither
// is worth having without the other.
func (s *EmissionSuite) multiLabelFixtures() []string {
	matches, err := filepath.Glob(filepath.Join("testdata", "multi_label_*.gql"))
	s.Require().NoError(err)
	s.Require().NotEmpty(matches, "the multi-label sweeps would run over no fixture at all")
	names := make([]string, 0, len(matches))
	for _, m := range matches {
		names = append(names, filepath.Base(m))
	}
	slices.Sort(names)
	return names
}

// TestEveryMultiLabelFixtureIsNamedByTheConvention is what makes the glob
// in multiLabelFixtures safe to rely on: it holds the convention to a
// biconditional over the parsed schema, not over the file's name. Every
// .gql in testdata that declares a multi-label type must be named
// multi_label_*, and every fixture so named must declare one.
//
// The reverse direction is not decoration. A fixture renamed INTO the
// convention and then emptied of its offenders would leave the sweeps
// asserting over a schema that cannot refuse, and the sweeps' own
// NotEmpty precondition reports that as their failure rather than as the
// fixture's. Reading the schema rather than grepping the source is what
// lets this answer for a multi-label type spelled in some way a text
// search does not anticipate.
func (s *EmissionSuite) TestEveryMultiLabelFixtureIsNamedByTheConvention() {
	all, err := filepath.Glob(filepath.Join("testdata", "*.gql"))
	s.Require().NoError(err)
	s.Require().NotEmpty(all, "testdata holds no schema fixture, so this proves nothing")

	var named, plain int
	for _, path := range all {
		fixture := filepath.Base(path)
		offenders, _ := multiLabelOffenders(s.inputFrom(path).Schema)
		conforms := strings.HasPrefix(fixture, "multi_label_")

		if len(offenders) > 0 {
			s.Require().True(conforms,
				"fixture %s declares %d multi-label type(s) but is not named multi_label_*, so the sweeps never see it",
				fixture, len(offenders))
			named++
			continue
		}
		s.Require().False(conforms,
			"fixture %s is named multi_label_* but declares no multi-label type, so the sweeps assert over a schema that cannot refuse",
			fixture)
		plain++
	}

	// Both populations non-empty, asserted separately: a run where every
	// fixture landed in one arm would satisfy every assertion above while
	// testing only one direction of the biconditional.
	s.Require().NotZero(named, "no fixture exercises the conforming arm")
	s.Require().NotZero(plain, "no fixture exercises the non-conforming arm")
}

// TestMultiLabelDiagnosticNamesEveryOffenderAndItsLabels holds the
// diagnostic to the facts an author needs to act on it: which types are
// refused, and which labels each is keyed on. Both are read off the
// parsed schema, so the expectation is not a transcription of the
// message.
//
// The negative half carries as much as the positive one. A message that
// listed every type in the schema would name each offender and each of
// its labels and satisfy the sweep, so the representable type beside
// them must not appear.
func (s *EmissionSuite) TestMultiLabelDiagnosticNamesEveryOffenderAndItsLabels() {
	for _, fixture := range s.multiLabelFixtures() {
		s.Run(fixture, func() {
			in := s.inputFrom(filepath.Join("testdata", fixture))
			_, err := age.New().Generate(in)
			s.Require().ErrorIs(err, age.ErrUnsupportedSchema)
			msg := err.Error()

			offenders, single := multiLabelOffenders(in.Schema)
			s.Require().NotEmpty(offenders, "fixture %s declares no multi-label type", fixture)
			s.Require().NotEmpty(single, "fixture %s declares no representable type to be left out", fixture)
			s.requireLabelsDistinguishable(fixture, offenders)

			// The top-level count, not just the per-offender label counts
			// below. The two fixtures declare different numbers of
			// offenders, so a message spelling a constant cannot pass both
			// rows — the same thing that separates the query-count test
			// from asserting a literal the source and the test both write.
			s.Require().Contains(msg, strconv.Itoa(len(offenders))+" of them",
				"the diagnostic does not say how many types the schema declares")

			for _, o := range offenders {
				s.Require().Contains(msg, o.name, "the diagnostic does not name refused type %s", o.name)
				for _, label := range o.labels {
					s.Require().Contains(msg, label,
						"the diagnostic does not name label %s, which %s is keyed on", label, o.name)
				}
				s.Require().Contains(msg, strconv.Itoa(len(o.labels))+" labels",
					"the diagnostic does not say how many labels %s is keyed on", o.name)
			}
			for _, name := range single {
				s.Require().NotContains(msg, name,
					"the diagnostic names %s, which this backend represents, so it does not say what to fix", name)
			}
		})
	}
}

// TestMultiLabelDiagnosticStatesWhatRefusingTheSchemaCosts pins the
// clause that makes the posture legible where an author meets it: the
// refusal is not scoped to the queries that project the type, so the
// message states how many queries go down with it.
//
// The count is derived from the batch, and the rows vary it, so a
// message that spelled one number could not pass every row — which is
// what separates this from asserting a constant the source and the test
// both write.
func (s *EmissionSuite) TestMultiLabelDiagnosticStatesWhatRefusingTheSchemaCosts() {
	for _, queries := range []int{0, 1, 4} {
		s.Run(fmt.Sprintf("%d_queries", queries), func() {
			in := s.inputFrom(filepath.Join("testdata", "multi_label_with_collateral.gql"))
			in.Queries = teamQueries(queries)

			_, err := age.New().Generate(in)
			s.Require().ErrorIs(err, age.ErrUnsupportedSchema)
			// The whole noun, not the "quer" prefix both spellings share.
			// A prefix cannot see the singular/plural branch at all, so
			// deleting it survived; the rows carry 1 and not-1 precisely
			// so that this assertion has both sides to distinguish.
			noun := "queries"
			if queries == 1 {
				noun = "query"
			}
			s.Require().Contains(err.Error(), strconv.Itoa(queries)+" "+noun,
				"the diagnostic does not say how many queries the schema refusal takes with it")
		})
	}
}

// wantMechanism is what the diagnostic has to tell an author about AGE
// itself, beyond which type is refused. Each row is a fact the message
// must carry, not a phrase it must spell: the assertion is on a
// substring, so what holds this honest is that removing the clause from
// the emission removes the fact from the message.
//
// Verified against apache/age 1.7.0: a vertex or an edge carries exactly
// one label, and the parser has no production for a second, so
// `CREATE (x:A:B)` is a syntax error rather than a rejected value.
var wantMechanism = []struct {
	fact string
	sub  string
}{
	{"AGE stamps one label per element", "exactly one label"},
	{"the limit is the parser's, not a value check", "no syntax for a second"},
	{"the witness statement that does not parse", "CREATE (x:A:B)"},
	{"an entity is declared for every type in the schema", "every type"},
	{"the emitted Go surface does not vary by backend", "does not vary by backend"},
	{"the way out", "single key label"},
}

// TestMultiLabelDiagnosticExplainsTheBackendAndThePosture asserts the
// message carries the reasoning an author needs, since a generate-time
// error is where they meet this and a bead note is somewhere they will
// not look (ADR 0027).
//
// This is the weakest guard in the file — it compares against phrases
// this test and the emission both spell — so it asserts a set rather
// than one string, and reports which fact is missing rather than that a
// string differs. The claims behind the phrases are held by
// TestMultiLabelRefusalReachesPastTheColumnsTheBatchProjects for the
// posture and by ADR 0027 for the AGE facts.
func (s *EmissionSuite) TestMultiLabelDiagnosticExplainsTheBackendAndThePosture() {
	in := s.inputFrom(filepath.Join("testdata", "multi_label_with_collateral.gql"))
	_, err := age.New().Generate(in)
	s.Require().ErrorIs(err, age.ErrUnsupportedSchema)

	msg := err.Error()
	for _, want := range wantMechanism {
		s.Require().Contains(msg, want.sub, "the diagnostic does not say %s", want.fact)
	}
}
