package conformance_test

import (
	"bytes"
	"errors"
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

// alternationSentinelName is the published spelling of the refusal this
// file's sweep matches on. It is the same string a manifest writes in
// expectedError, and resolving it through sentinelByName is what makes
// the sweep fail loudly if the backend ever stops publishing it, rather
// than quietly matching nothing.
const alternationSentinelName = "age.ErrRelationshipTypeAlternation"

// The rule this file asserts, over the corpus rather than over one
// fixture: a fixture whose query text carries a relationship-type
// alternation is either un-enrolled from apache-age-pgx-v5, or an
// invalid fixture naming age.ErrRelationshipTypeAlternation as the
// refusal it expects. AGE 1.7.0's parser has no production for `|` in a
// relationship detail, and the backend runs the author's text verbatim
// (ADR 0005), so the backend refuses every such query with that sentinel
// (ADR 0028 §1). What the rule forbids is the third case: enrolled,
// carrying `|`, and expecting anything else.
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
// THE SECOND ARM IS NEW, and until gqlc-rv0h landed this rule was bare
// un-enrolment. The reason was not that naming the sentinel was the
// worse polarity but that it was not expressible: a manifest names its
// refusal through sentinelByName, whose lanes were codegen., queryfile.
// and cypher. only, and this package deliberately imports no single
// backend, so age.ErrRelationshipTypeAlternation had no spelling a
// manifest could carry (ADR 0027). The registry now publishes it with
// the value, so invalid/age_relationship_type_alternation is a fixture
// that enrols AGE, spells `|`, and is CORRECT — and the sweep below
// would have called it the very violation it was written to catch.
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
	// witnesses is the subset of carriers that is AGE-enrolled and
	// legitimately so: an invalid fixture expecting the alternation
	// sentinel. These are the rule's second arm, not its negative.
	witnesses []corpusAlternation
	// violations is the subset of carriers whose fixture is
	// AGE-enrolled and is not a witness — the rule's negative.
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
			// The rule's second arm, and it is deliberately narrow: an
			// invalid fixture, naming this exact sentinel. An invalid
			// fixture expecting some OTHER refusal is still a violation.
			//
			// NOT because AGE necessarily answers on the alternation
			// before that other gate is reached. It does not, and the
			// complement below records the measurement: an alternation
			// that also produces an edge-union COLUMN is refused with
			// age.ErrUnsupportedQuery, the column gate answering first.
			// The reason is narrower and is about the corpus rather than
			// about AGE: the exemption is for the fixture whose PURPOSE
			// is this refusal. A fixture that records some other refusal
			// while also carrying `|` leaves which gate answered
			// undetermined, and an exemption that wide is one the corpus
			// has no way to check.
			witness := arm == "invalid" && m.ExpectedError == alternationSentinelName
			for _, qf := range m.QueryFiles {
				src, err := os.ReadFile(filepath.Join(dir, qf))
				s.Require().NoError(err)
				found := alternationsIn(src)
				if len(found) == 0 {
					continue
				}
				carrier := corpusAlternation{fixture: name, file: qf, alternations: found}
				out.carriers = append(out.carriers, carrier)
				switch {
				case !enrolled:
				case witness:
					out.witnesses = append(out.witnesses, carrier)
				default:
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
			// The TEXT alone: this reads a fixture for the set of
			// spellings it carries, so where each one sits is not part
			// of the question.
			if seen[alt.Text] {
				continue
			}
			seen[alt.Text] = true
			out = append(out, alt.Text)
		}
	}
	sort.Strings(out)
	return out
}

// TestAgeIsUnenrolledFromEveryAlternationCarryingFixture is the rule.
//
// A future fixture that adds an apache-age-pgx-v5 target to a query
// spelling `|` reds here, with a message naming the rule and the three
// repairs it admits — drop the target, rewrite the query, or make it an
// invalid fixture expecting the alternation sentinel — rather than
// reading as a sentinel mismatch a manifest edit would "fix".
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
			"fixtures enrol it anyway, and expect some other refusal or none:\n\n"+strings.Join(lines, "\n")+"\n\n"+
			"Three repairs, and which one is right depends on what the fixture is for. If it is covering\n"+
			"something else and AGE was enrolled by habit, drop the "+ageTarget+" target, or rewrite the query\n"+
			"without the alternation. If the alternation IS the point, make it an invalid fixture whose\n"+
			"expectedError is "+alternationSentinelName+" — that spelling reaches the value since gqlc-rv0h,\n"+
			"and invalid/age_relationship_type_alternation is the one that does it. What is never the repair\n"+
			"is renaming expectedError to whatever sentinel the run happened to produce.")
}

// TestAlternationSweepChargesAnEnrolledFixtureExpectingAnotherSentinel
// holds the narrow arm of sweepAlternations, which the tracked corpus
// cannot hold on its own: no committed fixture is in the forbidden state,
// so widening the arm to admit every invalid fixture changes nothing the
// corpus can see. The forbidden state is reachable rather than
// hypothetical — the discriminator below is a fixture TestInvalid PASSES,
// because AGE really does refuse it with the sentinel its manifest names —
// so the sweep is the only thing in the suite that refuses it, and an arm
// that admitted it would leave it admitted with nothing red.
//
// The sweep is run over a copy of the corpus with one fixture added
// rather than over a synthetic pair, so what is measured is the rule
// answering about a real corpus. Both directions are asserted: the
// discriminator is charged, and the corpus's own legitimate witness is
// still exempt.
func (s *ConformanceSuite) TestAlternationSweepChargesAnEnrolledFixtureExpectingAnotherSentinel() {
	// The alternation in this fixture's query also produces an edge-union
	// column, and the column gate answers before the alternation gate, so
	// its refusal is a sentinel other than the one the arm exempts.
	const (
		donor         = "valid/edge_union_many_two_candidates"
		discriminator = "invalid/age_edge_union_column_alternation"
		otherSentinel = "age.ErrUnsupportedQuery"
		corpusWitness = "invalid/age_relationship_type_alternation"
	)

	root := s.T().TempDir()
	s.Require().NoError(os.CopyFS(root, os.DirFS(trackedFixtureDir)))
	dir := filepath.Join(root, discriminator)
	s.Require().NoError(os.MkdirAll(dir, 0o755))
	for _, f := range []string{"schema.gql", "queries.cypher"} {
		src, err := os.ReadFile(filepath.Join(root, donor, f))
		s.Require().NoError(err)
		s.Require().NoError(os.WriteFile(filepath.Join(dir, f), src, 0o644))
	}
	s.Require().NoError(os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(
		`{"package":"ageedgeunioncolumnalternation","queryFiles":["queries.cypher"],`+
			`"targets":["`+ageTarget+`"],"expectedError":"`+otherSentinel+`"}`+"\n"), 0o644))

	s.T().Setenv(childRootEnv, root)

	// What makes the discriminator an admitted state rather than a
	// fiction: the refusal its manifest names is the refusal it gets, so
	// TestInvalid would grade it correct.
	refusal, published := sentinelByName[otherSentinel]
	s.Require().Truef(published, "the AGE backend publishes no %q, so this fixture names a refusal "+
		"no manifest can carry and the state it is here to represent is not reachable after all", otherSentinel)
	m := s.loadManifest(dir)
	sch := s.loadSchema(dir)
	_, err := s.generate(ageTarget, codegen.Input{Schema: sch, Queries: s.loadNamedQueries(dir, m, sch)})
	s.Require().ErrorIs(err, refusal, "%s is refused with some other sentinel than the %q its manifest names, "+
		"so TestInvalid would red on it and this test no longer witnesses a state only the sweep refuses",
		discriminator, otherSentinel)

	sweep := s.sweepAlternations()

	var charged, exempt []string
	for _, v := range sweep.violations {
		charged = append(charged, v.fixture)
	}
	for _, w := range sweep.witnesses {
		exempt = append(exempt, w.fixture)
	}
	s.Require().Contains(charged, discriminator,
		"the sweep left an AGE-enrolled invalid fixture carrying `|` and expecting %s uncharged. That is what "+
			"the arm looks like once it stops reading expectedError: every invalid fixture becomes a witness, "+
			"and the rule then forbids nothing an invalid fixture can do", otherSentinel)
	s.Require().NotContains(exempt, discriminator,
		"the sweep exempted it as a legitimate witness instead")
	s.Require().Contains(exempt, corpusWitness,
		"the corpus's own legitimate witness stopped being exempt, so the arm now charges the fixture whose "+
			"whole purpose is the refusal it names")
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
// backend — and since gqlc-rv0h the sentinel travels through it WITH the
// value, so the refusal is matched by identity. Until then it was
// matched on the diagnostic's message text, for want of any spelling
// that reached the value: rewording the sentence AGE prints was enough
// to stop that match firing, and this sweep stayed green reporting a
// healthy gate.
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
func (s *ConformanceSuite) TestAgeStillRefusesAnAlternationTheCorpusUnenrolledItFrom() {
	sweep := s.sweepAlternations()
	s.Require().Empty(sweep.violations, "run TestAgeIsUnenrolledFromEveryAlternationCarryingFixture for the detail")

	alternationRefusal, published := sentinelByName[alternationSentinelName]
	s.Require().Truef(published,
		"the AGE backend publishes no %q, so this sweep has no sentinel to match and would count every "+
			"refusal as coming from another gate", alternationSentinelName)

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
		case errors.Is(err, alternationRefusal):
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
