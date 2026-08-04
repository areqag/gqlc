package age

import (
	"errors"
	"fmt"
	"strings"

	"github.com/areqag/gqlc/internal/codegen"
	"github.com/areqag/gqlc/internal/graph"
	"github.com/areqag/gqlc/internal/resolver"
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
// name this backend derives for a TIMESTAMP property's zone is already
// a property the author declared. The derived name is read out of the
// same property map the declared one is, so a schema holding both gives
// one key two readers: the declared property fills its own struct field
// and is then read a second time as an offset, re-zoning the instant by
// whatever it holds. Where the declared width is not an integer the
// second read fails outright and no vertex of that type ever decodes.
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
			if f.GoType != goInstant {
				continue
			}
			sidecar := offsetProperty(f.PropName)
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

// unservedColumn names why a resolved column type has no decode arm, or
// "" when it has one. Served are a schema property of any width the type
// table carries — which is every scalar width with an agtype scalar, a
// list of one at whatever depth, and a property of no declared shape — a
// bool / integer / float / string expression, and a whole vertex or edge,
// the last of these because its label and its properties are together
// enough to fill the entity struct the schema declares. What remains is a
// width no emitted helper can fill, or an expression the resolver typed
// as something other than a property.
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
	case resolver.ResolvedNode, resolver.ResolvedEdge, resolver.ResolvedEdgeUnion:
		return ""
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
