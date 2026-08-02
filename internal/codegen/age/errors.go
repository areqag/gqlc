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
// shared phases refused. The type table those phases consult is this
// package's, so the refusal is this backend's answer and not a property
// of the schema or of the query: a run emitting several targets has to
// say which of them has no carrier for the width or the kind it names.
//
// These two sentinels and no others, because they are the two the shared
// phases reach by asking this package's table. Every other refusal
// follows from the input alone and would read as this backend's opinion
// if it were wrapped here.
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
// "" when it has one. Served are a schema property of scalar width, a
// bool / integer / float / string expression, and a whole vertex or edge
// — the last of these because its label and its properties are together
// enough to fill the entity struct the schema declares. What remains
// arrives either as a shape whose members the emitted helpers have no
// declared widths for, or as a value whose shape is not known until it
// arrives.
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
	case resolver.ResolvedNode, resolver.ResolvedEdge, resolver.ResolvedEdgeUnion:
		return ""
	case resolver.ResolvedTemporal:
		// Not answered here. Whether a temporal kind has a carrier is the
		// type table's answer, and the shared phase asks it and refuses
		// with ErrUnrepresentableTemporal naming the kind — a sentinel
		// this gate has no way to raise, since it reports one reason for
		// a whole query. Answering it in both places would be two tables
		// to keep in step, and the arm that commits an encoding per kind
		// (bd gqlc-35yu.11) would have to move both.
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
