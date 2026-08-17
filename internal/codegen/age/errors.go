package age

import (
	"errors"
	"fmt"
	"strings"

	"github.com/areqag/gqlc/internal/codegen"
	"github.com/areqag/gqlc/internal/graph"
	"github.com/areqag/gqlc/internal/resolver"
	"github.com/areqag/gqlc/internal/schema"
)

// ErrUnsupportedQuery is returned when a batch carries a query this
// backend has no emission for. Package-level so callers branch with
// errors.Is; the fail-site wraps it with the offending names — the
// schema/gql convention.
var ErrUnsupportedQuery = errors.New("unsupported query")

// ErrUnsupportedSchema is returned when a schema declares a shape this
// backend has no representation for. Package-level so callers branch
// with errors.Is; the fail-site wraps it with the offending names.
var ErrUnsupportedSchema = errors.New("unsupported schema")

// ErrRelationshipTypeAlternation is returned when a batch carries a
// query whose TEXT spells a relationship-type alternation, which Apache
// AGE 1.7.0's parser has no production for. It is deliberately not
// ErrUnsupportedQuery: that sentinel says this backend has no emission
// for a shape, and here the emission is not the obstacle — the text is,
// and the author's fix is a rewrite of the query rather than a different
// projection. A caller branching on the two gets two different answers
// because there are two.
var ErrRelationshipTypeAlternation = errors.New("relationship type alternation")

// ErrUndefinedFunction is returned when a batch carries a query whose
// TEXT calls a function Apache AGE 1.7.0 does not define. Separate from
// ErrRelationshipTypeAlternation for the same reason that one is
// separate from ErrUnsupportedQuery: the two are different obstacles
// with different fixes — one is rewritten as several queries, the other
// is computed in Go and bound — and a caller told only "the text is
// wrong" has been told less than the gate knows. Which names are refused
// is decided by the probe table in dialect.go, and only by it.
var ErrUndefinedFunction = errors.New("undefined function")

// wireEntities pairs every entity with the form it takes on the wire,
// failing a schema that declares a node or edge type under more than one
// label. The refusal is over the whole entity table rather than over the
// columns the batch projects, and takes the batch's queries with it
// (ADR 0027); queries is how many, which the diagnostic states.
func wireEntities(entities []codegen.Entity, queries int) ([]wiredEntity, error) {
	wired := make([]wiredEntity, 0, len(entities))
	var refused []codegen.Entity
	for _, e := range entities {
		w, ok := wireEntity(e)
		if !ok {
			refused = append(refused, e)
			continue
		}
		wired = append(wired, w)
	}
	if len(refused) == 0 {
		return wired, nil
	}
	return nil, fmt.Errorf("%w: %s", ErrUnsupportedSchema, multiLabelDiagnostic(refused, queries))
}

// multiLabelDiagnostic is what an author meets when a schema keys a type
// on more than one label and the target is AGE. It is where the posture
// is recorded for the person hitting it, so it carries four things: which
// types are refused and the labels each is keyed on, why AGE cannot hold
// them, that the refusal is the whole batch's rather than the projecting
// queries', and the two ways out.
//
// It names the refused types and nothing else. A message listing the
// whole entity table would say which types exist, not which need editing.
func multiLabelDiagnostic(refused []codegen.Entity, queries int) string {
	named := make([]string, 0, len(refused))
	for _, e := range refused {
		labels := keyLabels(e)
		named = append(named, fmt.Sprintf("%s type %s, keyed on %d labels (%s)",
			entityKindNoun(e.Kind), e.Name, len(labels), strings.Join(labels, ", ")))
	}
	queryNoun := "queries"
	if queries == 1 {
		queryNoun = "query"
	}
	return fmt.Sprintf(
		"Apache AGE stamps exactly one label on a vertex or an edge and its parser has no syntax for a second: "+
			"`CREATE (x:A:B)` is a syntax error, not a value AGE rejects. "+
			"So a type keyed on more than one label names an element no graph this backend can address ever holds, "+
			"and no value matching such a type can arrive to be decoded. "+
			"This schema declares %d of them: %s. "+
			"Refusing a type refuses the whole schema, and this batch's %d %s with it, "+
			"including every query that projects none of them — gqlc names an entity for every type a schema declares "+
			"and the generated Go surface does not vary by backend, so emitting here would declare each refused type "+
			"with a label check nothing could satisfy. "+
			"Give each a single key label, or generate this schema against a neo4j target.",
		len(refused), strings.Join(named, "; "), queries, queryNoun)
}

// keyLabels is the label set an entity is keyed on, which is the axis
// AGE cannot hold more than one member of.
func keyLabels(e codegen.Entity) []string {
	if e.Kind == codegen.EntityNode {
		return e.Labels.Split()
	}
	return e.EdgeKey.KeyLabels.Split()
}

// entityKindNoun names an entity kind as the schema's own vocabulary
// spells it, so the diagnostic points at a declaration the author wrote.
func entityKindNoun(k codegen.EntityKind) string {
	if k == codegen.EntityNode {
		return "node"
	}
	return "edge"
}

// rejectOffsetSidecarCollisions fails a schema in which the property
// name this backend derives for a field's zone is already a property the
// author declared. The derived name is read out of the same property map
// the declared one is, so a schema holding both gives one key two
// readers: the declared property fills its own struct field and is then
// read a second time as an offset, re-zoning the instant by whatever it
// holds. Where the declared width is not an integer the second read
// fails outright, so a vertex carrying the property does not decode at
// all.
//
// Which fields derive a name, and what that name is, are offsetSidecar's
// answers — the same call the decode makes — so this covers exactly the
// fields whose decode reads a sidecar.
//
// Reported as codegen.ErrPropertyFieldCollision because that is what it
// is — two properties on one entity contending for a single name — even
// though only one of the two appears in the schema text. Fields arrive
// map-key sorted, so the first offender is the same one on every run.
func rejectOffsetSidecarCollisions(entities []codegen.Entity) error {
	for _, e := range entities {
		declared := make(map[string]struct{}, len(e.Fields))
		for _, f := range e.Fields {
			declared[f.PropName] = struct{}{}
		}
		for _, f := range e.Fields {
			sidecar, derived := offsetSidecar(f)
			if !derived {
				continue
			}
			if _, taken := declared[sidecar]; !taken {
				continue
			}
			return fmt.Errorf("%w: entity %q declares property %q, which is where the Apache AGE backend stores property %q's zone — one key with two readers, so an instant would come back re-zoned by the declared value",
				codegen.ErrPropertyFieldCollision, e.Name, sidecar, f.PropName)
		}
	}
	return nil
}

// nameBackend adds this backend's name to a width or a temporal kind the
// shared phases refused. Both refusals come from asking this package's
// type table, so they are this backend's answer and not a property of the
// schema or of the query: a run emitting several targets has to say which
// of them has no carrier. Every other refusal follows from the input
// alone, so wrapping it here would misattribute it.
func nameBackend(err error) error {
	if !errors.Is(err, codegen.ErrUnrepresentableWidth) && !errors.Is(err, codegen.ErrUnrepresentableTemporal) {
		return err
	}
	return fmt.Errorf("%w, which the Apache AGE backend has no carrier for", err)
}

// rejectUnservedQueries fails a batch whose queries this backend cannot
// emit methods for, naming each one and the axis that dropped it. A
// generated package accounts for every query in its batch, and a query
// with no emission is one it cannot account for.
//
// The predicate keys on the backend's capability, not on the stage: the
// served set is exactly what the emitted decode and encode arms cover,
// so an arm added here widens it and an arm removed narrows it.
//
// It yields to rejectDialectGaps on a query whose text spells any of the
// constructs that gate reads, EXCEPT where the reason is the edge-union
// column. The exception is what the gate order is for: edgeUnionReason
// names the candidates the SCHEMA declares for the pattern, which the
// text cannot say, so it is the more informative of two answers to the
// same defect. Every OTHER reason answers a different defect — a list
// property, a map column, a list parameter — and printing it first costs
// the author a whole round: they fix the projection, regenerate, and
// only then learn the statement never parsed on this backend. The text
// is the obstacle underneath, because it has to be rewritten before any
// column question can be put to this server at all, so it goes first.
//
// Yielding is per query, not per batch: a sibling query whose reason
// this gate still owns is named here in the same run.
func rejectUnservedQueries(queries []codegen.NamedQuery) error {
	var dropped []string
	for _, q := range queries {
		reason, edgeUnion := unservedReason(q)
		if reason == "" {
			continue
		}
		if !edgeUnion && dialectGapFires(q.SourceText) {
			continue
		}
		dropped = append(dropped, q.Name+" ("+reason+")")
	}
	if len(dropped) == 0 {
		return nil
	}
	noun := "queries"
	if len(dropped) == 1 {
		noun = "query"
	}
	return fmt.Errorf("%w: the Apache AGE backend serves scalar and entity columns, so %d %s would be dropped: %s",
		ErrUnsupportedQuery, len(dropped), noun, strings.Join(dropped, ", "))
}

// unservedReason names the axis on which a query falls outside what this
// backend emits, or "" when every axis is served. Reading and writing
// both have an emission, and so does every cardinality either of them
// carries, so what is left to ask about is what the query carries across
// its columns and its parameters. Reported in the resolver's own type
// vocabulary, so the reason names the shape the author will find in
// their query.
//
// edgeUnion reports whether the reason came from an edge-union column,
// which is the one axis whose answer outranks the text gate's — see
// rejectUnservedQueries. It is read off the column's resolved type
// rather than off the reason string, because a reason is prose and
// matching prose would make the gate order turn on the wording.
// edgeUnionReason returns "" wherever it stands aside, so a non-empty
// reason from a ResolvedEdgeUnion column can only be its own.
//
// The assertion names the VALUE form only, and that is the answer this
// flag's contract wants rather than a form it forgets. What earns the rank
// is edgeUnionReason's text, which names the candidates the schema
// declares; unservedColumn reaches it from `case
// resolver.ResolvedEdgeUnion:`, which matches the value form and nothing
// else, so the assertion and that arm agree exactly. A pointer or embedded
// edge union — which satisfies resolver.ResolvedType without matching any
// arm, see unservedColumn's fall-through — is refused there instead, with
// "projects edgeUnion". That names no candidate, so it says strictly less
// than the alternation the text gate quotes back, and ranking it above the
// text would make the author worse off in exactly the trade the exception
// below exists to avoid. TestEdgeUnionRankingFlagNamesTheValueFormOnly
// holds both halves, so widening the assertion reddens it.
//
// The parameter arm answers false without asking: a parameter is a schema
// property or it is nothing this backend encodes, and neither is an edge
// union, so a reason from that arm is always one the text outranks.
//
// The columns are read BEFORE the parameters, and that order is a
// decision, not a reading order. One reason is reported per query (ADR
// 0025), so a query unserved on both axes reports one of them — and only
// the column axis can carry the edge union, the single reason that
// outranks the text. Reading the parameters first would hand such a query
// its parameter's reason with edgeUnion=false, which makes the query yield
// to the text, and the author would get the alternation quoted back where
// the candidates the schema declares for the pattern were available:
// strictly less about the very same fix, which is the one trade the
// exception to the yield exists to avoid. The parameter's reason is not
// lost, only deferred — it is what the author is told once the column is
// repaired.
//
// Both halves are pinned by what the author is told:
// TestRejectsRelationshipTypeAlternation's "an edge-union column outranks
// an unserved parameter" for the order, and "an unserved parameter yields
// to the text" for the arm's false.
func unservedReason(q codegen.NamedQuery) (reason string, edgeUnion bool) {
	for _, col := range q.Validated.Columns {
		if r := unservedColumn(col.Type); r != "" {
			_, union := col.Type.(resolver.ResolvedEdgeUnion)
			return fmt.Sprintf("column %q %s", col.Name, r), union
		}
	}
	for _, param := range q.Validated.Parameters {
		if r := unservedParam(param.Type); r != "" {
			return "parameter $" + param.Name + " " + r, false
		}
	}
	return "", false
}

// edgeUnionReason is why an edge column with more than one candidate is
// not served, or "" when the reason is not this backend's to give. It is
// the one refusal here that is not about the wire: an agtype edge carries
// its label spelled exactly as the schema spells it, so a dispatch on
// that label would pick the right candidate. What fails is the
// statement, before any value exists to decode.
//
// Which half applies is read off the candidates, because a refusal has
// to describe the column in front of it, and a single sentence about '|'
// printed over both halves was false of the second.
//
// Candidates carrying DISTINCT labels can only have come from a pattern
// naming more than one relationship type, and openCypher writes that
// exactly one way. Cypher.g4's oC_RelationshipTypes admits a second type
// only after '|', and a re-bound relationship variable's occurrences
// intersect rather than accumulate (internal/query/cypher/pattern.go), so
// no run of single-type occurrences widens one either. Apache AGE 1.7.0
// has no '|' in its relationship detail — it answers such a pattern with
// `ERROR: syntax error at or near "|"`, SQLSTATE 42601, whatever
// surrounds it (verified against the image test/data/codegen pins;
// TestAGERefusesRelationshipTypeAlternation re-measures it on every run
// of the AGE live half and fails when it stops holding). Generated code
// runs the author's query text verbatim (ADR 0005), so emitting for the
// column would produce a package that compiles and whose every call is a
// server-side syntax error. Refusing here turns that into an answer the
// author gets from `gqlc generate` — for this column shape. The text
// shapes that reach no such column belong to the alternation gap in
// dialectGaps, which rejectDialectGaps runs (dialect.go).
//
// The labels are named as what they are — the candidates the schema
// declares for the pattern — and no alternation is quoted back, because
// the candidate set is a SUBSET of the types the pattern names: a type
// the schema does not declare is dropped during resolution
// (internal/resolver, edgeCandidates) and never reaches here (gqlc-1dmu
// is the missing diagnostic for that). Reconstructing ":A|B" from the
// survivors and calling it the author's alternation printed a query
// nobody wrote — the same defect as the sentence above, one layer in.
//
// A DUPLICATE label is a different failure and not this backend's. Two
// candidates under one label leave the emitted dispatch nothing to tell
// them apart, because an edge value carries its label and its properties
// and never its endpoint types; that is true on every backend alike and
// is what codegen.ErrUnrepresentableEdgeUnion says, so standing aside
// lets the portable answer through. What it stands aside on is the
// duplicate, and the duplicate is reachable with no alternation at all —
// one relationship type over a plural endpoint (ADR 0022), which is
// invalid/plural_endpoint_edge_union_shared_label, enrolled for this
// backend. A repeated label under a pattern that DOES spell one is
// answered by rejectDialectGaps instead — by its alternation gap — and
// rightly: the text has to be rewritten before the column question can
// even be asked of this server.
//
// The split here is read off the resolved candidates rather than off the
// query text, because a bare '|' is not a witness to anything: Cypher.g4
// spells it in three productions — oC_RelationshipTypes (line 255), the
// list/filter comprehension (395) and the pattern comprehension (398) —
// so `[x IN xs | x.n]` carries one and names no relationship type.
// Candidate labels are downstream of the parse and say which production
// ran; so is the parse the alternation gap's find runs —
// cypher.RelationshipTypeAlternations, reached through rejectDialectGaps
// — which asks the same question of the text the emission will ship.
//
// Nothing after this point catches a re-admission. Loosening the gate
// compile-validly leaves `gqlc generate` exiting 0 over Go that does not
// build: this package emits no sealed interface and no marker method for
// the column (render_models.go, writeEntities), so the querier references
// a type nothing declares. The corpus cannot see it either — the fence
// module compiles goldens, and with AGE un-enrolled from every
// edge_union_* fixture there is no AGE golden here to compile. What
// holds the refusal instead is the refusal itself, asserted at three
// seams: TestRejectsEdgeUnionColumns and TestRejectsQueriesItCannotServe
// on the gate, and TestRunApacheAgeRefusesEdgeUnions through the CLI.
// Restoring the emission means restoring a golden with it, or the
// compile fence stays out of reach.
func edgeUnionReason(u resolver.ResolvedEdgeUnion) string {
	labels := distinctEdgeLabels(u.EdgeKeys)
	// A duplicate label, or a candidate count the resolver never emits,
	// belongs to shared admission: it refuses both and names the pair or
	// the invariant, which this gate has nothing to add to.
	if len(u.EdgeKeys) < 2 || len(labels) != len(u.EdgeKeys) {
		return ""
	}
	return fmt.Sprintf(
		`binds more than one relationship type — %s, the candidates the schema declares for `+
			`its pattern — which openCypher spells only as an alternation, and Apache AGE `+
			`1.7.0's parser has no "|" in a relationship pattern: it answers one with %q `+
			`(SQLSTATE 42601)`,
		formatLabelList(labels[0], labels[1], labels[2:]...), `syntax error at or near "|"`)
}

// distinctEdgeLabels is the candidates' edge labels in candidate order —
// which is probe order, which is the order the pattern names them (R3
// §4.4) — with repeats dropped so the caller can compare the count
// against the candidate count.
func distinctEdgeLabels(keys []schema.EdgeKey) []string {
	out := make([]string, 0, len(keys))
	seen := make(map[graph.LabelSetKey]struct{}, len(keys))
	for _, k := range keys {
		if _, dup := seen[k.KeyLabels]; dup {
			continue
		}
		seen[k.KeyLabels] = struct{}{}
		out = append(out, string(k.KeyLabels))
	}
	return out
}

// formatLabelList renders two or more labels as English prose: "A and
// B", "A, B and C", "A, B, C and D". The first two are named parameters
// because one label is the input this has no rendering for and the
// refusal it serves has no meaning for.
//
// The signature refuses a caller holding no labels at all; it does not
// refuse one holding a single-element slice, which spreads into the
// first parameter and panics on the second. What keeps that away is
// edgeUnionReason's own `len(u.EdgeKeys) < 2` arm, and the subtest that
// exercises it is TestRejectsEdgeUnionColumns/a_single_candidate.
func formatLabelList(first, second string, rest ...string) string {
	all := append([]string{first, second}, rest...)
	return strings.Join(all[:len(all)-1], ", ") + " and " + all[len(all)-1]
}

// unservedColumn names why a resolved column type has no decode arm, or
// "" when it has one. Served are a schema property of any width the type
// table carries — which is every scalar width with an agtype scalar, a
// list of one at whatever depth, and a property of no declared shape — a
// bool / integer / float / string expression, and a whole vertex or edge,
// the last of these because its label and its properties are together
// enough to fill the entity struct the schema declares. What remains is a
// width no emitted helper can fill, an expression the resolver typed as
// something other than a property, or — for the edge union — a column
// that could only arrive in answer to a statement this server will not
// parse.
func unservedColumn(t resolver.ResolvedType) string {
	switch ct := t.(type) {
	case resolver.ResolvedProperty:
		if !carriedWidth(ct.Type) {
			return "projects " + ct.String()
		}
		return ""
	case resolver.ResolvedScalar:
		switch ct.Kind {
		case resolver.ScalarBool, resolver.ScalarInt, resolver.ScalarFloat, resolver.ScalarString:
			return ""
		case resolver.ScalarNull, resolver.ScalarMap:
			return "projects " + ct.String()
		}
		return "projects " + ct.String()
	case resolver.ResolvedNode, resolver.ResolvedEdge:
		return ""
	case resolver.ResolvedEdgeUnion:
		return edgeUnionReason(ct)
	case resolver.ResolvedTemporal:
		// Answered by the type table instead: the shared phase asks it and
		// refuses with ErrUnrepresentableTemporal naming the kind, which
		// this gate cannot do — it reports one reason per query (ADR 0025).
		return ""
	case resolver.ResolvedList, resolver.ResolvedUnknown:
		return "projects " + ct.String()
	}
	// Reached without a ninth variant. resolver.ResolvedType's unexported
	// marker stops another package DECLARING an implementation from
	// scratch, which is narrower than a closed sum: two constructions
	// obtain the marker without declaring it, and neither matches an arm
	// above. The pointer form of a variant — every marker and String takes
	// a value receiver, and a pointer's method set contains its value
	// methods, so *resolver.ResolvedEdgeUnion satisfies the interface while
	// `case resolver.ResolvedEdgeUnion:` does not match it. And a struct
	// embedding a variant — Go promotes an embedded type's unexported
	// methods, so `struct{ resolver.ResolvedNode }` declared anywhere
	// satisfies the interface without naming the marker at all. The two
	// nest, so the set of shapes that can arrive here has no bound.
	// Callers assemble both: Input, NamedQuery, ValidatedQuery, Column and
	// every variant are exported structs with exported fields, so what the
	// resolver builds does not bound what a caller hands over
	// (internal/codegen/errors.go, gqlc-h4ug).
	//
	// So the arms are not the interface's membership. What lands here is
	// either an existing variant in a form no arm spells, or a variant
	// genuinely added to the resolver that no arm yet names, and both are
	// refused on the same terms: dropped rather than emitted through an arm
	// chosen for some other shape, because a column no arm here recognises
	// has no decode arm either and serving it would emit a method that
	// cannot fill its row. The reason carries the wire tag and nothing
	// else, which is thinner than what the edge-union arm says and is what
	// the ranking in unservedReason turns on.
	// TestUnservedColumnFallThroughIsNotANinthVariant is the witness, for
	// all eight variants in both forms.
	return "projects " + t.String()
}

// unservedParam names why a resolved parameter type cannot be encoded
// into the agtype argument, or "" when it can. Parameters reach here as
// schema properties only — the shared admission phase rejects every
// other resolved type before emission — so width is the whole question.
// The argument object is JSON, whose syntax agtype's input function
// reads, so a width with a Go carrier has an encoding: a slice crosses
// as an agtype list and a value of no declared shape as whatever shape
// it holds.
func unservedParam(t resolver.ResolvedType) string {
	prop, ok := t.(resolver.ResolvedProperty)
	if !ok {
		return "is " + t.String()
	}
	if !carriedWidth(prop.Type) {
		return "is " + prop.String()
	}
	return ""
}

// carriedWidth reports whether a property width lands on a Go type the
// emitted helpers encode and decode. The type table admits exactly those
// widths, so asking it is the whole check, and asking it here is what
// keeps the two answers from drifting apart.
func carriedWidth(pt graph.PropertyType) bool {
	_, ok := typeMap{}.Property(pt)
	return ok
}
