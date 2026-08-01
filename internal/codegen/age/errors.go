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
	return fmt.Errorf("%w: the Apache AGE backend serves scalar reads, so %d %s would be dropped: %s",
		ErrUnsupportedQuery, len(dropped), noun, strings.Join(dropped, ", "))
}

// unservedReason names the axis on which a query falls outside the read
// path, or "" when every axis is served. A served query only reads, has
// a row axis with rows on it, and carries nothing across its columns and
// parameters that the scalar arms cannot encode or decode. Reported in
// the resolver's own type vocabulary, so the reason names the shape the
// author will find in their query.
func unservedReason(q codegen.NamedQuery) string {
	if q.Validated.Statement == resolver.StatementWrite {
		return "writes to the graph"
	}
	if q.Cardinality == codegen.CardinalityExec {
		return ":exec returns no rows to decode"
	}
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
// "" when it has one. Served are a schema property of scalar width and a
// bool / integer / float / string expression: one agtype scalar token
// each. Every other member of the surface arrives as a shape with
// internal structure — an annotated vertex or edge, a bracketed list, a
// brace-delimited map — or as a value whose shape is not known until it
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
	case resolver.ResolvedNode, resolver.ResolvedEdge, resolver.ResolvedEdgeUnion,
		resolver.ResolvedTemporal, resolver.ResolvedList, resolver.ResolvedUnknown:
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
// emitted helpers encode and decode. The type table is the authority on
// which widths have a Go carrier at all; this narrows that to the ones
// whose carrier is a single agtype scalar. ANY is the width it excludes:
// its value can be any agtype shape, so `any` is the only carrier it has
// and no scalar helper fills one.
func scalarWidth(pt graph.PropertyType) bool {
	ty, ok := typeMap{}.Property(pt)
	return ok && ty != "any"
}
