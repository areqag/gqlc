package conformance_test

import (
	"bytes"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/areqag/gqlc/internal/codegen"
	"github.com/areqag/gqlc/internal/query/cypher"
	"github.com/areqag/gqlc/internal/queryfile"
)

// ageTarget is the registry wire key a manifest enrols the Apache AGE
// backend under. Spelled here rather than imported from config so this
// file states the target the rule below is about, not the target list
// the loader happens to accept.
const ageTarget = "apache-age-pgx-v5"

// The rule this file asserts, over the corpus rather than over one
// fixture: no fixture enrolled for apache-age-pgx-v5 carries a
// relationship-type alternation in its query text. AGE 1.7.0's parser
// has no production for `|` in a relationship detail, and the backend
// runs the author's text verbatim (ADR 0005), so the backend refuses
// every such query with age.ErrRelationshipTypeAlternation (ADR 0028
// §1).
//
// Before this file the rule lived in a reviewer's head and in the commit
// message that un-enrolled invalid/unrepresentable_edge_union_shared_label
// from AGE (gqlc-35yu.14). The failure a re-enrolment produced was
// TestInvalid's "expected sentinel X, got Y", which is what a plain
// manifest typo produces too, and whose obvious repair — renaming the
// manifest's expectedError — is exactly the wrong edit. Worse, that
// diagnosis was only available for an INVALID fixture: enrolling AGE on
// a valid/ fixture carrying `|` reds TestValid on a missing golden tree,
// which names the rule not at all.
//
// UN-ENROLMENT is the polarity chosen, over "enrolled with
// ErrRelationshipTypeAlternation named". The alternative is not
// expressible today: a manifest names its sentinel through
// sentinelByName, whose lanes are codegen., queryfile. and cypher. only,
// and this package deliberately imports no single backend, so
// age.ErrRelationshipTypeAlternation has no spelling a manifest can
// carry (gqlc-rv0h, ADR 0027). Should that bead land a registry-published
// sentinel lane, this rule becomes the disjunction the bead describes and
// this comment is the place to say so.
//
// The complement — that AGE has not quietly STOPPED refusing alternation,
// which would leave the un-enrolments above holding nothing and nothing
// red — is TestAgeStillRefusesAnAlternationTheCorpusUnenrolledItFrom.

// corpusAlternation is one fixture's alternation-carrying query text.
type corpusAlternation struct {
	fixture      string
	file         string
	alternations []string
}

// alternationSweep is one pass over the whole corpus. The three
// populations are recorded separately, and each is asserted separately,
// because a single "the sweep read something" arm over their sum is
// answered by any one of them being non-empty: the rule below is
// vacuous unless the corpus holds BOTH an AGE-enrolled fixture and an
// alternation-carrying one, and an aggregate floor cannot tell the two
// silences apart (measured elsewhere in this repo as
// require.NotZero(total) over three sources staying green with two of
// them silent).
type alternationSweep struct {
	// fixtures is every fixture directory read, AGE-enrolled or not.
	fixtures []string
	// ageEnrolled is every fixture whose manifest names ageTarget.
	ageEnrolled []string
	// carriers is every (fixture, query file) whose text spells at
	// least one relationship-type alternation, AGE-enrolled or not.
	carriers []corpusAlternation
	// violations is the subset of carriers whose fixture is
	// AGE-enrolled — the rule's negative.
	violations []corpusAlternation
}

// sweepAlternations reads every valid/ and invalid/ fixture and grades
// its query text with cypher.RelationshipTypeAlternations rather than
// grepping for '|': the character is spelled by the list and pattern
// comprehension productions too, so a grep would red on
// `[x IN xs | x.n]`, which names no relationship type and which AGE
// parses.
//
// Query text is taken through the queryfile front end where that
// succeeds, so annotation lines are not graded. An invalid fixture whose
// whole point is that queryfile refuses it has no such split available,
// and there the raw file bytes are graded instead. That errs toward
// over-reading — a `|` inside an annotation comment would be read — and
// that is the safe direction: a false red names a rule and is repaired
// by looking, a false green is the defect this file exists to remove.
func (s *ConformanceSuite) sweepAlternations() alternationSweep {
	var out alternationSweep
	for _, arm := range []string{"valid", "invalid"} {
		dirs, err := filepath.Glob(filepath.Join(fixtureRoot(), arm, "*"))
		s.Require().NoError(err)
		for _, dir := range dirs {
			name := arm + "/" + filepath.Base(dir)
			m := s.loadManifest(dir)
			out.fixtures = append(out.fixtures, name)
			enrolled := false
			for _, target := range m.Targets {
				if target == ageTarget {
					enrolled = true
				}
			}
			if enrolled {
				out.ageEnrolled = append(out.ageEnrolled, name)
			}
			for _, qf := range m.QueryFiles {
				src, err := os.ReadFile(filepath.Join(dir, qf))
				s.Require().NoError(err)
				found := alternationsIn(src)
				if len(found) == 0 {
					continue
				}
				carrier := corpusAlternation{fixture: name, file: qf, alternations: found}
				out.carriers = append(out.carriers, carrier)
				if enrolled {
					out.violations = append(out.violations, carrier)
				}
			}
		}
	}
	return out
}

// alternationsIn grades one query file's bytes, splitting them into
// annotated queries first where the front end can and reading them whole
// where it cannot. The results are de-duplicated so a fixture repeating
// one alternation across two queries reports it once.
func alternationsIn(src []byte) []string {
	texts := []string{string(src)}
	if parsed, err := queryfile.New().Parse(bytes.NewReader(src)); err == nil && len(parsed) > 0 {
		texts = texts[:0]
		for _, aq := range parsed {
			texts = append(texts, aq.Text)
		}
	}
	seen := map[string]bool{}
	var out []string
	for _, text := range texts {
		for _, alt := range cypher.RelationshipTypeAlternations(text) {
			if seen[alt] {
				continue
			}
			seen[alt] = true
			out = append(out, alt)
		}
	}
	sort.Strings(out)
	return out
}

// TestAgeIsUnenrolledFromEveryAlternationCarryingFixture is the rule.
//
// A future fixture that adds an apache-age-pgx-v5 target to a query
// spelling `|` reds here, with a message naming the rule and the two
// repairs it admits — drop the target, or rewrite the query — rather
// than reading as a sentinel mismatch a manifest edit would "fix".
func (s *ConformanceSuite) TestAgeIsUnenrolledFromEveryAlternationCarryingFixture() {
	sweep := s.sweepAlternations()

	requireSwept(s.T(), len(sweep.fixtures), "the conformance corpus",
		"the rule below is quantified over the fixtures this run read, so a corpus glob that matched nothing\n"+
			"satisfies it with no fixture graded")
	requireSwept(s.T(), len(sweep.ageEnrolled), "the AGE-enrolled population",
		"no fixture in the corpus enrols "+ageTarget+", so the rule below has no fixture it could refuse and\n"+
			"is satisfied by a corpus that stopped covering this backend entirely. Either AGE has been\n"+
			"un-enrolled corpus-wide — in which case delete this rule and say why — or the manifest key has\n"+
			"been renamed out from under the constant above")
	requireSwept(s.T(), len(sweep.carriers), "the alternation-carrying population",
		"no fixture in the corpus spells a relationship-type alternation, so the rule below is satisfied by a\n"+
			"scanner that reads nothing: blanking cypher.RelationshipTypeAlternations, or losing every `|` from\n"+
			"the corpus, both land here rather than on a green run")

	if len(sweep.violations) == 0 {
		return
	}
	var lines []string
	for _, v := range sweep.violations {
		lines = append(lines, "  "+v.fixture+"/"+v.file+": "+strings.Join(v.alternations, ", "))
	}
	s.Require().Fail("a fixture enrolled for "+ageTarget+" carries a relationship-type alternation",
		"Apache AGE 1.7.0's parser has no production for `|` in a relationship detail, and the backend runs\n"+
			"the author's query text verbatim (ADR 0005, ADR 0028 §1), so it refuses every such query. These\n"+
			"fixtures enrol it anyway:\n\n"+strings.Join(lines, "\n")+"\n\n"+
			"The repair is to the fixture, never to the manifest's expectedError: either drop the "+ageTarget+"\n"+
			"target from the fixture, or rewrite the query without the alternation. Naming AGE's own sentinel\n"+
			"in the manifest is not an option and is not merely unspelt — see gqlc-rv0h.")
}

// TestAgeStillRefusesAnAlternationTheCorpusUnenrolledItFrom is the
// complement, and the half the rule above cannot state.
//
// TestAgeIsUnenrolledFromEveryAlternationCarryingFixture is satisfied by
// an AGE that has stopped refusing alternation, because it grades
// manifests and query text and never runs the backend. If the gate in
// AGE were weakened or deleted, every un-enrolment the rule protects
// would silently be holding nothing, and the fixtures un-enrolled for it
// would stay un-enrolled with no test saying they need not be.
//
// So this drives every alternation-carrying fixture the corpus does NOT
// enrol AGE in through the registry's AGE backend and requires a
// refusal. The registry is the seam — this package imports no single
// backend — so the sentinel is matched on its message rather than by
// identity; that is the same limit gqlc-rv0h records, and the message is
// age.ErrRelationshipTypeAlternation's own text.
//
// The REASON is asserted, not only the verdict, and it is asserted as a
// population rather than per fixture. Measured while writing this:
// valid/edge_union_many_two_candidates is refused with
// age.ErrUnsupportedQuery, because its alternation also produces an
// edge-union COLUMN and the column gate answers first. So a per-fixture
// "refused on the alternation" arm is false today, and a per-fixture
// "refused at all" arm is satisfied by a tree where the alternation gate
// has been deleted and only the column gate remains — which is exactly
// the weakening this test exists to catch. Both are asserted: every
// probe is refused, and at least one probe is refused ON the alternation.
const alternationRefusal = "relationship type alternation"

func (s *ConformanceSuite) TestAgeStillRefusesAnAlternationTheCorpusUnenrolledItFrom() {
	sweep := s.sweepAlternations()
	s.Require().Empty(sweep.violations, "run TestAgeIsUnenrolledFromEveryAlternationCarryingFixture for the detail")

	var probes []corpusAlternation
	for _, c := range sweep.carriers {
		if strings.HasPrefix(c.fixture, "valid/") {
			probes = append(probes, c)
		}
	}
	requireSwept(s.T(), len(probes), "the alternation-carrying valid fixtures",
		"this witness needs a fixture that reaches codegen and spells `|`; with none, the loop below runs the\n"+
			"backend zero times and the refusal it asserts is never asked for")

	var emitted, onAlternation []string
	for _, probe := range probes {
		dir := filepath.Join(fixtureRoot(), probe.fixture)
		m := s.loadManifest(dir)
		sch := s.loadSchema(dir)
		in := codegen.Input{Schema: sch, Queries: s.loadNamedQueries(dir, m, sch)}

		_, err := s.generate(ageTarget, in)
		switch {
		case err == nil:
			emitted = append(emitted, "  "+probe.fixture+": "+strings.Join(probe.alternations, ", "))
		case strings.Contains(err.Error(), alternationRefusal):
			onAlternation = append(onAlternation, probe.fixture)
		}
	}

	s.Require().Empty(emitted,
		"%s emitted for a fixture whose query text spells a relationship-type alternation:\n%s\n\n"+
			"AGE 1.7.0's parser answers `|` in a relationship pattern with a syntax error, and the backend "+
			"ships the author's text verbatim, so emitting here means shipping code that cannot run. If AGE "+
			"has genuinely gained a representation for alternation, every un-enrolment this corpus records "+
			"for that reason is now holding nothing and needs re-deciding (gqlc-vm6l)",
		ageTarget, strings.Join(emitted, "\n"))

	requireSwept(s.T(), len(onAlternation), "the alternation-reason refusals",
		"every alternation-carrying fixture was refused by "+ageTarget+", but not one of them on the\n"+
			"alternation: the refusals all came from another gate. That is the shape a deleted\n"+
			"age.ErrRelationshipTypeAlternation leaves behind, because an alternation in a projected column\n"+
			"is refused by the edge-union gate as well, and it reads identical to a healthy run if only the\n"+
			"verdict is asserted. A fixture spelling `|` outside a projected column is what distinguishes\n"+
			"them; valid/edge_union_list is the one that did so when this was written")
}
