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
//
// "Carries" is the witness's CODE and not the bytes its body occupies: a
// row commented out carries nothing, which took a fix (review mutation
// M15). An ANSWER is held tighter than that, and the difference is worth
// knowing before adding a gap: a probe or a served text has only to
// appear in the witness, an answer has to appear in what one of its
// assertions reads, so a wantMessage column no assertion selects is not
// a recorded answer (mutation M18, bd gqlc-35yu.17).
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
	// served are texts the gap's witness requires the server to ACCEPT
	// and that find must stay silent on. They are not decoration: a find
	// that refused everything would satisfy the refused half alone, and
	// the whole hazard of a text gate is the false positive — ADR 0005
	// leaves the author no way to route around one.
	//
	// Unlike refused, not every one is transcribed from a hand-run
	// session — timestamp() is (gqlc-35yu.5), the rest are first measured
	// by the witness. The direction is what makes that sound: a served
	// text asserts the gate must NOT fire, so one that is wrong about the
	// server reddens the live run instead of refusing an author.
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
		// No temporal constructor this project has measured is defined
		// on Apache AGE 1.7.0. The hand-run spike behind that
		// (gqlc-35yu.5) found agtype with no temporal value type, no
		// cast to or from one in either direction, and one temporal
		// name among the 348 functions then in ag_catalog:
		// age_timestamp, reached from cypher as timestamp(), which
		// returns epoch MILLISECONDS as an integer. No test re-measures
		// the catalogue, so the refused set is the probes below and the
		// spike is provenance rather than a closed set.
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
				"(ADR 0005) and Apache AGE 1.7.0 defines no temporal constructor this project has "+
				"measured, so every call on %d %s would answer \"function <name> does not exist\" — "+
				"timestamp() is the one that answered, returning epoch milliseconds as an integer, so "+
				"compute the value in Go and bind it as a parameter, or generate against a neo4j "+
				"target: %s", count, noun, dropped)
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
	{
		// point() is refused by the pinned image the same way the
		// temporal names are — SQLSTATE 42883, `function point does
		// not exist` — and it is a gap of its own rather than a name
		// added to the one above. The gap above says AGE defines no
		// temporal constructor this project has measured, and point is
		// not a temporal, so admitting it there would answer a spatial
		// call with a sentence that is false about it and hand a
		// caller branching on ErrUndefinedFunction a remedy for a
		// different fix.
		//
		// It answers LAST because it is the narrowest: one name,
		// measured once. A query spelling a temporal constructor and
		// point() is told about the temporal one, which is arbitrary
		// as between two true answers but is not arbitrary as a rule —
		// the table answers in order so that the answer is stable, and
		// TestRejectsTheSpatialConstructor pins which one comes back.
		sentinel: ErrUndefinedSpatialFunction,
		find:     findUndefinedSpatialFunctions,
		diagnose: func(count int, noun, dropped string) string {
			return fmt.Sprintf("generated code runs the author's query text verbatim "+
				"(ADR 0005) and Apache AGE 1.7.0 does not define the spatial constructor this "+
				"project has measured, so every call on %d %s would answer "+
				"\"function <name> does not exist\" (SQLSTATE 42883) — store the coordinates as "+
				"ordinary properties and compute the geometry in Go: %s", count, noun, dropped)
		},
		witness: "TestAGERefusesTheSpatialConstructor",
		refused: spatialFunctionProbes,
		served: []string{
			// The false positive a scan for `point(` would take. There
			// is no served CALL to put beside it: the one spatial name
			// this project has run is refused, so the accepting half of
			// this gap's bound is the property lookup alone.
			"MATCH (p:Person) RETURN p.point",
		},
	},
	{
		// The fourth gap, and the only one here that does not refuse a
		// function. A namespaced call — Cypher.g4 §oC_FunctionName is
		// `oC_Namespace oC_SymbolicName` — reaches PostgreSQL as a
		// SCHEMA-qualified name, and PostgreSQL resolves the qualifier
		// before it looks for any function. So the pinned image answers
		// duration.between with SQLSTATE 3F000, `schema "duration" does
		// not exist`, and the answer names the namespace and no function
		// at all.
		//
		// That is why it is not a name in the temporal gap. A row there
		// has to be named by its own probe's answer
		// (TestEveryRefusedFunctionNameIsNamedByItsProbeAnswer), which
		// is what keeps that catalogue honest, and no answer of this
		// shape can satisfy it. Widening that guard to take a second
		// answer shape would have bought this row at the price of the
		// property the guard exists for.
		//
		// The catalogue is of NAMESPACES rather than of full names, and
		// that is the server's own reading rather than a convenience:
		// the qualifier fails to resolve, so EVERY function under it is
		// refused by the same mechanism, and a per-full-name catalogue
		// would refuse `duration.between` while serving
		// `duration.inSeconds` — a distinction the server does not make.
		// Both spellings were measured on the pinned image and both
		// answer 3F000 with the same message (bd gqlc-dy40s), so the
		// widening is witnessed and not inferred; the witness carries
		// the second one for exactly that reason.
		//
		// It answers LAST, so a query spelling a bare constructor and a
		// namespaced one is told about the bare one. That is not
		// arbitrary between two true answers: the bare call is the older
		// and better-measured refusal, and the table answers in order so
		// the answer is stable.
		//
		// The refusal is narrower than the server is, deliberately. An
		// unprobed namespace is refused by the image too — `foo.bar(1)`
		// answers 3F000 the same way — and is served by this gate
		// regardless, because a gap is what a probe witnessed and ADR
		// 0005 leaves an author no way around a false positive.
		sentinel: ErrUndefinedNamespace,
		find:     findUndefinedNamespaces,
		diagnose: func(count int, noun, dropped string) string {
			return fmt.Sprintf("generated code runs the author's query text verbatim "+
				"(ADR 0005) and Apache AGE 1.7.0 has no schema for the namespace this project has "+
				"measured, so every call on %d %s would answer \"schema <namespace> does not exist\" "+
				"(SQLSTATE 3F000) — PostgreSQL resolves the namespace as a schema qualifier before it "+
				"looks for any function, so no function under that namespace resolves whatever it is "+
				"called: compute the value in Go and bind it as a parameter, or generate against a "+
				"neo4j target: %s", count, noun, dropped)
		},
		witness: "TestAGERefusesTheNamespaceItHasNoSchemaFor",
		refused: namespaceProbes,
		served: []string{
			// A qualified call that DOES resolve, which is what bounds
			// the false positive: this gate refuses a namespace, so a
			// served namespace is the half that says it refuses the
			// measured one rather than the call shape. ag_catalog is
			// AGE's own schema and age_timestamp() is the function
			// behind the bare timestamp() the temporal gap serves.
			"RETURN ag_catalog.age_timestamp()",
			// The false positive a scan for `duration.` would take, and
			// the accessor/property distinction CONTEXT.md draws: a
			// property lookup spells the namespace and calls nothing.
			"MATCH (p:Person) RETURN p.duration",
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
// time() and localtime() were the last two temporal constructors
// openCypher spells that this list did not hold. Both were run against
// the pinned image on 2026-08-29 (bd gqlc-osf1, workflow run
// 33268424367) and both are refused, so the temporal constructor set is
// now closed by measurement rather than left short on suspicion.
//
// Two constructs measured in that same run are still NOT here, and each
// is absent for a reason rather than an oversight:
//
//   - point({x: 1, y: 2}) is refused, SQLSTATE 42883, but it is not a
//     temporal, and this gap's prose scopes itself to temporals. It is
//     the third gap below, with its own sentinel and its own message
//     (bd gqlc-l8e2n).
//   - duration.between(null, null) is refused under a DIFFERENT error
//     class: SQLSTATE 3F000, `schema "duration" does not exist`. Postgres
//     reads the openCypher namespace as a schema qualifier and fails on
//     the qualifier before looking for any function, so the answer names
//     no function at all — which is precisely what
//     TestEveryRefusedFunctionNameIsNamedByItsProbeAnswer requires of a
//     row here. It could not join this gap even once a namespaced
//     scanner existed, so it is the FOURTH gap above, catalogued by
//     namespace and carrying its own sentinel (bd gqlc-dy40s).
var undefinedFunctionProbes = []dialectProbe{
	{text: "RETURN datetime()", answer: "function datetime does not exist"},
	{text: "RETURN date()", answer: "function date does not exist"},
	{text: "RETURN localdatetime()", answer: "function localdatetime does not exist"},
	{text: "RETURN duration({days:1})", answer: "function duration does not exist"},
	{text: "RETURN toTimestamp('2024-01-01')", answer: "function toTimestamp does not exist"},
	{text: "RETURN time()", answer: "function time does not exist"},
	{text: "RETURN localtime()", answer: "function localtime does not exist"},
}

// undefinedFunctions is the lowercased name of every function the probes
// called, read out of the probe texts by the same parse the gate runs on
// the author's query. Deriving it rather than writing it down is what
// makes the witness compulsory: there is no literal here for a name to
// be added to.
var undefinedFunctions = calledFunctionNames(undefinedFunctionProbes)

// spatialFunctionProbes is the third gap's evidence, on the same terms
// as undefinedFunctionProbes: the ONLY source of the name it refuses,
// parsed for the name rather than told it.
//
// One row, because one spatial constructor has been run. point() was
// measured against apache/age@sha256:4241e2d8… (PostgreSQL 18.1, AGE
// 1.7.0) on 2026-08-29, workflow run 33268424367, and answered SQLSTATE
// 42883 (bd gqlc-l8e2n). openCypher spells others — point() with a
// spatial reference system, distance(), and the point accessors — and
// none of them is here, for the reason the temporal gap gives: a false
// positive costs the author a query that would have worked and ADR 0005
// leaves them no way around it, while a false negative costs a runtime
// error they were going to get anyway.
var spatialFunctionProbes = []dialectProbe{
	{text: "RETURN point({x: 1, y: 2})", answer: "function point does not exist"},
}

// undefinedSpatialFunctions is the third gap's catalogue, derived from
// its probes by the same parse for the same reason: there is no literal
// here for a name to be added to without a measurement.
var undefinedSpatialFunctions = calledFunctionNames(spatialFunctionProbes)

// namespaceProbes is the fourth gap's evidence. One row, because one
// namespace has been measured — and one row is also all this gap's
// catalogue can carry per namespace, since
// TestEveryRefusedNamespaceIsNamedByItsProbeAnswer requires the probes
// and the namespaces to line up one for one.
//
// duration.between(null, null) was run against apache/age@sha256:4241e2d8…
// (PostgreSQL 18.1, AGE 1.7.0) on 2026-08-29 by Արամազդ, workflow run
// 33268424367, and re-measured locally against the same image digest the
// same day (bd gqlc-dy40s). Both runs answered SQLSTATE 3F000 with the
// message below. The arguments are null because what is being measured
// is the NAME: the qualifier fails to resolve before anything is
// evaluated, so no argument could change the answer.
//
// The answer is the server's own words and quotes the namespace in the
// case the AUTHOR wrote it — `Duration.Between` answers `schema
// "Duration" does not exist`, measured. That is why the guard compares
// case-insensitively, and why the catalogue lowercases what it stores.
var namespaceProbes = []dialectProbe{
	{text: "RETURN duration.between(null, null)", answer: `schema "duration" does not exist`},
}

// undefinedNamespaces is the fourth gap's catalogue: the lowercased
// namespace of every qualified call its probes make, read out of the
// probe TEXTS by the same parse the gate runs on the author's query.
// Derived rather than written down for the reason the other two are —
// there is no literal here for a namespace to be added to without a
// measurement — and the guard that closes the loop is
// TestEveryRefusedNamespaceIsNamedByItsProbeAnswer.
var undefinedNamespaces = calledNamespaces(namespaceProbes)

// calledNamespaces is the lowercased namespace of every qualified
// function call a set of probes makes.
//
// It is a separate reading from calledFunctionNames and not a widening
// of it, because the two catalogues hold different things: that one
// holds function names read off unqualified calls, this one holds
// namespaces read off qualified ones. The scans they read through
// partition the calls (internal/query/cypher), so nothing is counted by
// both and nothing escapes both.
func calledNamespaces(probes []dialectProbe) map[string]struct{} {
	out := make(map[string]struct{})
	for _, p := range probes {
		for _, c := range cypher.QualifiedFunctionCalls(p.text) {
			// Already lowercased by the scan, which is where the
			// case-folding belongs: the resolver folds it, so every
			// reader of a namespace has to.
			out[c.Namespace] = struct{}{}
		}
	}
	return out
}

// calledFunctionNames is the lowercased name of every unqualified
// function a set of probes calls, read out of the probe TEXTS by the
// parse the gate runs on the author's query.
//
// Shared by both function gaps rather than written twice, because the
// property it carries — a refused name exists only where a probe called
// it — is the same property in both, and two copies of it can drift into
// one gap deriving and the other listing.
func calledFunctionNames(probes []dialectProbe) map[string]struct{} {
	out := make(map[string]struct{})
	for _, p := range probes {
		for _, name := range cypher.UnqualifiedFunctionCalls(p.text) {
			out[strings.ToLower(name)] = struct{}{}
		}
	}
	return out
}

// findUndefinedFunctions is the calls in a query text that name a
// function AGE does not define, quoted as the author wrote them.
//
// A namespaced call is not one of these: Cypher.g4 §oC_FunctionName is
// `oC_Namespace oC_SymbolicName`, and a server resolving duration.between
// resolves nothing about duration, so the two are different names and
// only the one that was probed is refused.
func findUndefinedFunctions(src string) []string {
	return findCalls(undefinedFunctions, src)
}

// findUndefinedSpatialFunctions is the same reading against the spatial
// catalogue. The two are separate functions over separate catalogues
// rather than one over their union, because the gap a name lands in
// decides the sentinel and the prose the author gets, and a union would
// answer both with whichever gap ran first.
func findUndefinedSpatialFunctions(src string) []string {
	return findCalls(undefinedSpatialFunctions, src)
}

// findUndefinedNamespaces is the QUALIFIED calls in a query text whose
// namespace names something the pinned image has no schema for, quoted
// whole — namespace included — as the author wrote them.
//
// It reads the other scan, and that is the partition the four gaps rest
// on: an invocation carries a namespace or it does not, so this and
// findCalls cannot both claim one call and no call escapes both. That is
// also why the namespace catalogue is exempt from
// TestTheFunctionCataloguesAreDisjoint — the disjointness it would test
// for is a property of the call SHAPE here, not of the two name sets,
// and `duration` being in the temporal catalogue and `duration` being a
// refused namespace are facts about two different spellings.
func findUndefinedNamespaces(src string) []string {
	var found []string
	for _, c := range cypher.QualifiedFunctionCalls(src) {
		if _, undefined := undefinedNamespaces[c.Namespace]; !undefined {
			continue
		}
		found = append(found, c.Text)
	}
	return found
}

// findCalls is the calls in a query text naming something in a
// catalogue, quoted as the author wrote them.
func findCalls(catalogue map[string]struct{}, src string) []string {
	var found []string
	for _, name := range cypher.UnqualifiedFunctionCalls(src) {
		if _, undefined := catalogue[strings.ToLower(name)]; !undefined {
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
// here, ahead of the portable temporal refusal", and — at the CLI seam,
// through the real front end —
// TestRunApacheAgeAnswersAnAlternationAheadOfSharedAdmission and
// TestRunApacheAgeRefusesUndefinedFunctions) rather than left to the
// reading order of generate.go.
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
