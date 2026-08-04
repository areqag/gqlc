package age

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/areqag/gqlc/internal/codegen"
	"github.com/areqag/gqlc/internal/query/cypher"
)

// dialectProbe is one query text a live session against the AGE image
// this repo pins was measured on, paired with the answer the server gave
// back. It is the unit of evidence this file is built out of: a probe is
// what a construct's refusal rests on, and there is no other way to put
// one here.
type dialectProbe struct {
	// text is the cypher the session ran, spelled exactly as the live
	// test runs it — the sweep compares the two as bytes.
	text string
	// answer is the substring of the server's error message the live
	// test asserts. It is quoted back to the author by the refusal, so
	// it has to be the server's own words.
	answer string
}

// dialectGap is one construct Apache AGE 1.7.0 will not accept, together
// with the evidence that it will not.
//
// Generated code runs the author's query text verbatim (ADR 0005), so a
// construct the server's parser or its function catalogue refuses is a
// package that compiles, passes the fence, and fails on every call. The
// only place gqlc can answer is generate time, and the only thing it can
// read there is the text.
//
// The fields are in two halves and the split is the point. find is what
// the gate does; refused, served and witness are why it is allowed to.
// witnessGaps (dialect_test.go) holds them together — it requires every
// gap to carry probes, every probe to fire find, every served text not
// to, and every one of them to appear in the live test the gap names,
// which the AGE live recipes must run. A gap added with no probe, or
// with a probe no live test carries, reddens the sweep. That is the
// property this whole file exists for: adding a refusal costs a witness.
// Without it the table grows on suspicion, which is the same guess as a
// hardcoded list with more ceremony around it.
type dialectGap struct {
	// sentinel is what a caller branches on with errors.Is. One per gap,
	// because two gaps are two different fixes and a caller that cannot
	// tell them apart is being told less than the gate knows.
	sentinel error
	// find is the offending spellings in a query text, quoted as the
	// author wrote them, or nil where the text carries none. Every one
	// of these reads the grammar (internal/query/cypher) rather than
	// scanning for characters, because the characters are ambiguous:
	// '|' is spelled by three Cypher.g4 productions and a name followed
	// by '(' is spelled by procedure invocation too.
	find func(src string) []string
	// diagnose is the prose the refusal carries, given the arithmetic
	// and the per-query list. Per gap, because the way out differs.
	diagnose func(count int, noun, dropped string) string
	// witness names the live test that measures this gap against the
	// pinned image on every AGE live run.
	witness string
	// refused are the probes the server rejected, which is where the
	// gap's authority comes from.
	refused []dialectProbe
	// served are texts the same session accepted that find must stay
	// silent on. They are not decoration: a find that refused
	// everything would satisfy the refused half alone, and the whole
	// hazard of a text gate is the false positive — ADR 0005 leaves the
	// author no way to route around one.
	served []string
}

// dialectGaps is the whole of what this backend refuses on the text, in
// the order it answers. A query tripping two gaps is told about the
// first, because two constructs are two rewrites and one message that
// merged them would be true of neither — and a caller branching on the
// sentinel would get an answer belonging to a fix it did not ask about.
//
// The alternation goes first because it is the older, wider refusal:
// it is what the edge-union column gate defers to, and a pattern that
// does not parse is a pattern whose function calls never get evaluated.
var dialectGaps = []dialectGap{
	{
		// Apache AGE 1.7.0's parser has no '|' in a relationship detail
		// — it answers one with `ERROR: syntax error at or near "|"`,
		// SQLSTATE 42601, whatever surrounds it.
		//
		// Most of what this catches reaches no column, which is why it
		// is a text gate and not a column reason. A query may bind an
		// alternation and project something else entirely
		// (`-[r:A|B]->(p) RETURN p.id`); it may bind one the resolver
		// narrowed to a single declared candidate, because a type the
		// schema does not declare is dropped during resolution
		// (internal/resolver, edgeCandidates), which leaves a
		// ResolvedEdge the emission serves; and a re-bound relationship
		// variable's occurrences intersect (internal/query/cypher,
		// mergeBinding), which narrows the binding while leaving the
		// text alone. Every one of those is a package that compiles and
		// whose every call is a server-side syntax error.
		//
		// The alternations are quoted as the parser read them and are
		// not reconstructed from anything: every character printed is a
		// character the author wrote, whitespace inside the alternation
		// included — SP is a default-channel token in this grammar, so
		// the context's text concatenation keeps it and `-[r:A | B]->`
		// is quoted ":A | B". That is the axis the column refusal
		// cannot have, since its label list is a subset of what the
		// pattern names.
		sentinel: ErrRelationshipTypeAlternation,
		find:     cypher.RelationshipTypeAlternations,
		diagnose: func(count int, noun, dropped string) string {
			return fmt.Sprintf("generated code runs the author's query text verbatim (ADR 0005) and "+
				"Apache AGE 1.7.0's parser has no \"|\" in a relationship pattern, so every call on %d %s "+
				"would answer %q (SQLSTATE 42601) — write each relationship type as its own query: %s",
				count, noun, `syntax error at or near "|"`, dropped)
		},
		witness: "TestAGERefusesRelationshipTypeAlternation",
		refused: []dialectProbe{
			{
				text:   "MATCH (:Person)-[r:ACTED_IN|DIRECTED]->(:Movie) RETURN r",
				answer: `syntax error at or near "|"`,
			},
			{
				// The pre-openCypher-9 spelling of the same thing.
				text:   "MATCH (:Person)-[r:ACTED_IN|:DIRECTED]->(:Movie) RETURN r",
				answer: `syntax error at or near "|"`,
			},
			{
				// The variable-length form the _list edge-union
				// fixtures reach the same leaf through.
				text:   "MATCH (:Person)-[r:ACTED_IN|DIRECTED*]->(:Movie) RETURN r",
				answer: `syntax error at or near "|"`,
			},
		},
		served: []string{
			"MATCH (:Person)-[r]->(:Movie) RETURN r",
			"MATCH (:Person)-[r:DIRECTED]->(:Movie) RETURN r",
		},
	},
	{
		// Apache AGE 1.7.0 defines no temporal constructor. agtype has
		// no temporal value type, there is no cast to or from one in
		// either direction, and of 348 functions in ag_catalog exactly
		// one has a temporal name: age_timestamp, reached from cypher
		// as timestamp(), which returns epoch MILLISECONDS as an
		// integer (gqlc-35yu.5).
		//
		// So this is not the temporal refusal ADR 0025 already makes.
		// That one is about the carrier: a projected temporal column
		// has no Go type here, and codegen.Prepare answers it with
		// ErrUnrepresentableTemporal. This one is about the statement,
		// and it fires where no column exists to ask about — a
		// constructor in a WHERE predicate reaches no column at all,
		// because the query model drops predicate structure by design
		// (ADR 0003), and a write clause projects nothing while
		// shipping its whole text to the server.
		//
		// The names are matched case-insensitively, which is what
		// openCypher function resolution is, and quoted back in the
		// author's own case, which is what the author has to find in
		// their file.
		sentinel: ErrUndefinedFunction,
		find:     findUndefinedFunctions,
		diagnose: func(count int, noun, dropped string) string {
			return fmt.Sprintf("generated code runs the author's query text verbatim "+
				"(ADR 0005) and Apache AGE 1.7.0 defines no temporal constructor at all, so every call on "+
				"%d %s would answer \"function <name> does not exist\" — AGE's whole temporal surface is "+
				"timestamp(), which returns epoch milliseconds as an integer, so compute the value in Go "+
				"and bind it as a parameter, or generate against a neo4j target: %s", count, noun, dropped)
		},
		witness: "TestAGERefusesTheFunctionsItDoesNotDefine",
		refused: undefinedFunctionProbes,
		served: []string{
			// The one that works, and the reason the catalogue is a
			// measured set rather than "anything temporal-looking".
			"RETURN timestamp()",
			// The false positive a scan for the name would take.
			"MATCH (p:Person) RETURN p.datetime",
		},
	},
}

// undefinedFunctionProbes are the constructor calls a live session ran
// against the pinned image, each with the answer it got. This slice is
// the ONLY source of the refused name set: findUndefinedFunctions parses
// these texts for the names they call, so a name cannot be refused
// without a probe that called it and an answer that names it back.
//
// Every entry is from gqlc-35yu.5, run by hand against
// apache/age@sha256:4241e2d8… (PostgreSQL 18.1, AGE 1.7.0), and
// TestAGERefusesTheFunctionsItDoesNotDefine re-measures all of them on
// every AGE live run.
//
// openCypher spells three more constructors this list does NOT hold —
// time(), localtime() and point() — and a namespaced duration.between().
// Every one of them is suspect for the same reason as the five here and
// none was ever run, so none is refused: a false positive costs the
// author a query that would have worked and ADR 0005 leaves them no way
// around it, while a false negative costs a runtime error they were
// going to get anyway. Verifying them needs a container, which needs CI
// (bd gqlc-osf1).
var undefinedFunctionProbes = []dialectProbe{
	{text: "RETURN datetime()", answer: "function datetime does not exist"},
	{text: "RETURN date()", answer: "function date does not exist"},
	{text: "RETURN localdatetime()", answer: "function localdatetime does not exist"},
	{text: "RETURN duration({days: 1})", answer: "function duration does not exist"},
	{text: "RETURN toTimestamp('2024-01-01')", answer: "function toTimestamp does not exist"},
}

// undefinedFunctions is the lowercased name of every function the probes
// called, read out of the probe texts by the same parse the gate runs on
// the author's query. Deriving it rather than writing it down is what
// makes the witness compulsory: there is no literal here for a name to
// be added to.
var undefinedFunctions = func() map[string]struct{} {
	out := make(map[string]struct{})
	for _, p := range undefinedFunctionProbes {
		for _, name := range cypher.UnqualifiedFunctionCalls(p.text) {
			out[strings.ToLower(name)] = struct{}{}
		}
	}
	return out
}()

// findUndefinedFunctions is the calls in a query text that name a
// function AGE does not define, quoted as the author wrote them.
//
// A namespaced call is not one of these: Cypher.g4 §oC_FunctionName is
// `oC_Namespace oC_SymbolicName`, and a server resolving duration.between
// resolves nothing about duration, so the two are different names and
// only the one that was probed is refused.
func findUndefinedFunctions(src string) []string {
	var found []string
	for _, name := range cypher.UnqualifiedFunctionCalls(src) {
		if _, undefined := undefinedFunctions[strings.ToLower(name)]; !undefined {
			continue
		}
		found = append(found, name)
	}
	return found
}

// rejectDialectGaps fails a batch carrying a query whose TEXT spells
// something Apache AGE 1.7.0 will not accept, naming each query and what
// it spells.
//
// It reads the query text and nothing else about it — not its columns,
// not its cardinality, not whether it reads or writes. A :exec write
// projects no columns at all and its whole statement still reaches the
// server, so narrowing the loop to queries that project, or to queries
// that read, leaves a DELETE spelling '|' generating cleanly and failing
// on every call.
//
// It runs AFTER rejectUnservedQueries, but that gate yields to this one
// on every reason except the edge-union column, so what the ordering
// really says is: an edge-union column outranks the text, and nothing
// else does. The exception earns its place — edgeUnionReason names the
// candidates the SCHEMA declares for the pattern, which the text cannot
// say, and it is an answer to the SAME defect. A list property, a map
// column or a list parameter is a different defect, and printing it
// first sends the author round twice for one query: fix the projection,
// regenerate, then learn the statement never parsed here.
//
// It runs BEFORE codegen.Prepare, which answers a repeating edge label
// with the portable ErrUnrepresentableEdgeUnion and a projected temporal
// with ErrUnrepresentableTemporal. A query doing both gets this one,
// because the text has to be rewritten before the column question can be
// put to this server at all. That ordering is what puts
// invalid/unrepresentable_edge_union_shared_label out of reach of an
// apache-age-pgx-v5 enrolment, so it is pinned by the answer it produces
// (TestRejectsRelationshipTypeAlternation's "a column shared admission
// refuses is answered here, because this runs first",
// TestRejectsUndefinedFunctions' "a projected constructor is answered
// here, ahead of the portable temporal refusal", and
// TestRunApacheAgeAnswersAnAlternationAheadOfSharedAdmission at the CLI
// seam) rather than left to the reading order of generate.go.
func rejectDialectGaps(queries []codegen.NamedQuery) error {
	for _, g := range dialectGaps {
		if err := g.reject(queries); err != nil {
			return err
		}
	}
	return nil
}

// reject is one gap's answer over a batch, or nil where no query in it
// spells the gap's construct.
func (g dialectGap) reject(queries []codegen.NamedQuery) error {
	var dropped []string
	for _, q := range queries {
		found := g.find(q.SourceText)
		if len(found) == 0 {
			continue
		}
		quoted := make([]string, len(found))
		for i, f := range found {
			quoted[i] = strconv.Quote(f)
		}
		dropped = append(dropped, q.Name+" ("+strings.Join(quoted, ", ")+")")
	}
	if len(dropped) == 0 {
		return nil
	}
	noun := "queries"
	if len(dropped) == 1 {
		noun = "query"
	}
	return fmt.Errorf("%w: %s", g.sentinel, g.diagnose(len(dropped), noun, strings.Join(dropped, ", ")))
}

// dialectGapFires reports whether any gap in the table reads something
// in a query text. It is what rejectUnservedQueries yields on, and it is
// deliberately the whole table rather than one construct: the yield is
// to the TEXT, so a gap added here inherits it and the author is not
// sent to fix a projection sitting behind a statement that never parsed.
func dialectGapFires(src string) bool {
	for _, g := range dialectGaps {
		if len(g.find(src)) > 0 {
			return true
		}
	}
	return false
}
