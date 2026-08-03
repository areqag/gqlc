package age

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/areqag/gqlc/internal/codegen"
	"github.com/areqag/gqlc/internal/graph"
	"github.com/areqag/gqlc/internal/query/cypher"
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

// wireEntities pairs every entity with the form it takes on the wire,
// failing a schema that declares a node or edge type under more than one
// label. AGE stamps exactly one label on a vertex or an edge and its
// parser has no syntax for a second, so a graph matching such a type
// cannot be written through this backend and no value matching it can
// arrive to be decoded.
//
// It runs over the whole entity surface: Phase Z names an entity for
// every type in the schema and the emitted surface is backend-invariant,
// so a struct that no query could ever fill is still a struct this
// package declares.
func wireEntities(entities []codegen.Entity) ([]wiredEntity, error) {
	wired := make([]wiredEntity, 0, len(entities))
	var multi []string
	for _, e := range entities {
		w, ok := wireEntity(e)
		if !ok {
			multi = append(multi, fmt.Sprintf("%s (%s)", e.Name, e.DocAxis))
			continue
		}
		wired = append(wired, w)
	}
	if len(multi) == 0 {
		return wired, nil
	}
	noun := "types"
	if len(multi) == 1 {
		noun = "type"
	}
	return nil, fmt.Errorf("%w: Apache AGE stamps one label on a vertex or an edge, so %d %s cannot be represented: %s",
		ErrUnsupportedSchema, len(multi), noun, strings.Join(multi, ", "))
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
func rejectUnservedQueries(queries []codegen.NamedQuery) error {
	var dropped []string
	for _, q := range queries {
		if reason := unservedReason(q); reason != "" {
			dropped = append(dropped, q.Name+" ("+reason+")")
		}
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

// rejectRelationshipTypeAlternation fails a batch carrying a query whose
// TEXT spells a relationship-type alternation, naming each one and the
// alternations it spells.
//
// This is the refusal that keys on the hazard itself. Apache AGE 1.7.0's
// parser has no '|' in a relationship detail — it answers one with
// `ERROR: syntax error at or near "|"`, SQLSTATE 42601, whatever
// surrounds it (verified against the image test/data/codegen pins;
// TestAGERefusesRelationshipTypeAlternation re-measures it on every live
// run and fails when it stops holding) — and generated code runs the
// author's query text verbatim (ADR 0005). So the offending thing is the
// text, and the predicate reads the text.
//
// It is a separate gate from rejectUnservedQueries and not a column
// reason inside it, because most of what it catches reaches no column.
// A query may bind an alternation and project something else entirely
// (`-[r:A|B]->(p) RETURN p.id`); it may bind one the resolver narrowed to
// a single declared candidate, because a type the schema does not declare
// is dropped during resolution (internal/resolver, edgeCandidates), which
// leaves a ResolvedEdge the emission serves; and a re-bound relationship
// variable's occurrences intersect (internal/query/cypher, mergeBinding),
// which narrows the binding while leaving the text alone. Every one of
// those is a package that compiles and whose every call is a server-side
// syntax error, and the column shape is blind to all three.
//
// It runs AFTER rejectUnservedQueries rather than before, so a query that
// trips both gets the column's answer. That is the more informative of
// the two: it names the candidates the SCHEMA declares for the pattern,
// which the text cannot say. Where the column gate stands aside — a
// single surviving candidate, or candidates repeating a label — this one
// is what answers.
//
// The alternations are quoted as the parser read them and are not
// reconstructed from anything: every character printed is a character the
// author wrote (whitespace inside the alternation excepted, which the
// token concatenation drops). That is the axis the column refusal cannot
// have, since its label list is a subset of what the pattern names.
func rejectRelationshipTypeAlternation(queries []codegen.NamedQuery) error {
	var dropped []string
	for _, q := range queries {
		alts := cypher.RelationshipTypeAlternations(q.SourceText)
		if len(alts) == 0 {
			continue
		}
		quoted := make([]string, len(alts))
		for i, a := range alts {
			quoted[i] = strconv.Quote(a)
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
	return fmt.Errorf("%w: generated code runs the author's query text verbatim (ADR 0005) and "+
		"Apache AGE 1.7.0's parser has no \"|\" in a relationship pattern, so every call on %d %s "+
		"would answer %q (SQLSTATE 42601) — write each relationship type as its own query: %s",
		ErrRelationshipTypeAlternation, len(dropped), noun, `syntax error at or near "|"`,
		strings.Join(dropped, ", "))
}

// unservedReason names the axis on which a query falls outside what this
// backend emits, or "" when every axis is served. Reading and writing
// both have an emission, and so does every cardinality either of them
// carries, so what is left to ask about is what the query carries across
// its columns and its parameters. Reported in the resolver's own type
// vocabulary, so the reason names the shape the author will find in
// their query.
func unservedReason(q codegen.NamedQuery) string {
	for _, col := range q.Validated.Columns {
		if reason := unservedColumn(col.Type); reason != "" {
			return fmt.Sprintf("column %q %s", col.Name, reason)
		}
	}
	for _, param := range q.Validated.Parameters {
		if reason := unservedParam(param.Type); reason != "" {
			return "parameter $" + param.Name + " " + reason
		}
	}
	return ""
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
// TestAGERefusesRelationshipTypeAlternation re-measures it on every live
// run and fails when it stops holding). Generated code runs the author's
// query text verbatim (ADR 0005), so emitting for the column would
// produce a package that compiles and whose every call is a server-side
// syntax error. Refusing here turns that into an answer the author gets
// from `gqlc generate`.
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
// answered by rejectRelationshipTypeAlternation instead, and rightly:
// the text has to be rewritten before the column question can even be
// asked of this server.
//
// The split here is read off the resolved candidates rather than off the
// query text, because a bare '|' is not a witness to anything: Cypher.g4
// spells it in three productions — oC_RelationshipTypes (line 255), the
// list/filter comprehension (395) and the pattern comprehension (398) —
// so `[x IN xs | x.n]` carries one and names no relationship type.
// Candidate labels are downstream of the parse and say which production
// ran; so is the parse rejectRelationshipTypeAlternation runs, which asks
// the same question of the text the emission will ship.
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
// "" when it has one. Served are a schema property of scalar width, a
// bool / integer / float / string expression, and a whole vertex or edge
// — the last of these because its label and its properties are together
// enough to fill the entity struct the schema declares. What remains
// arrives either as a shape whose members the emitted helpers have no
// declared widths for, as a value whose shape is not known until it
// arrives, or — for the edge union — in answer to a statement this
// server will not parse.
func unservedColumn(t resolver.ResolvedType) string {
	switch ct := t.(type) {
	case resolver.ResolvedProperty:
		if ct.Type.Kind() == graph.KindList {
			return "projects a list property"
		}
		if !scalarWidth(ct.Type) {
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
	// ResolvedType is a sealed interface, so the switch above is its
	// whole membership; a variant added to the resolver lands here and is
	// dropped rather than emitted through an arm chosen for some other
	// shape.
	return "projects " + t.String()
}

// unservedParam names why a resolved parameter type cannot be encoded
// into the agtype argument, or "" when it can. Parameters reach here as
// schema properties only — the shared admission phase rejects every
// other resolved type before emission — so width is the whole question.
func unservedParam(t resolver.ResolvedType) string {
	prop, ok := t.(resolver.ResolvedProperty)
	if !ok {
		return "is " + t.String()
	}
	if prop.Type.Kind() == graph.KindList {
		return "is a list"
	}
	if !scalarWidth(prop.Type) {
		return "is " + prop.String()
	}
	return ""
}

// scalarWidth reports whether a property width lands on a Go scalar the
// emitted helpers encode and decode. The type table admits exactly those
// widths, so asking it is the whole check, and asking it here is what
// keeps the two answers from drifting apart.
func scalarWidth(pt graph.PropertyType) bool {
	_, ok := typeMap{}.Property(pt)
	return ok
}
