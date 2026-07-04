package resolver

import (
	"fmt"

	"github.com/areqag/gqlc/internal/procsig"
	"github.com/areqag/gqlc/internal/query"
	"github.com/areqag/gqlc/internal/schema"
)

// resolve is the R0 resolution kernel: pure, deterministic, short-circuit.
// It walks a query.Query and produces a ValidatedQuery for the R0 capability
// scope (§7 of the R0 spec): one branch, one part, one labelled node binding,
// only RefProjection items, no writes, no CALL, no WITH, no UNION, no
// RETURN DISTINCT, no RETURN *. Everything else routes to ErrOutOfR0Scope.
func resolve(q query.Query, s schema.Schema, _ procsig.Registry) (ValidatedQuery, error) {
	if len(q.Branches) != 1 || len(q.Combinators) != 0 {
		return ValidatedQuery{}, fmt.Errorf("%w: UNION / multi-branch query", ErrOutOfR0Scope)
	}
	branch := q.Branches[0]
	if len(branch.Parts) != 1 {
		return ValidatedQuery{}, fmt.Errorf("%w: WITH / multi-part query", ErrOutOfR0Scope)
	}
	part := branch.Parts[0]

	if len(part.Effects) != 0 {
		return ValidatedQuery{}, fmt.Errorf("%w: write clause", ErrOutOfR0Scope)
	}
	if part.Distinct {
		return ValidatedQuery{}, fmt.Errorf("%w: RETURN DISTINCT / WITH DISTINCT", ErrOutOfR0Scope)
	}
	if part.ReturnsAll {
		return ValidatedQuery{}, fmt.Errorf("%w: RETURN * / WITH *", ErrOutOfR0Scope)
	}
	if len(part.Bindings) != 1 {
		return ValidatedQuery{}, fmt.Errorf("%w: expected exactly one node binding, got %d bindings", ErrOutOfR0Scope, len(part.Bindings))
	}
	node, ok := part.Bindings[0].(query.NodeBinding)
	if !ok {
		return ValidatedQuery{}, fmt.Errorf("%w: non-node binding %s", ErrOutOfR0Scope, part.Bindings[0].Kind())
	}
	labels := node.Labels()
	if len(labels) == 0 {
		return ValidatedQuery{}, fmt.Errorf("%w: unlabelled node binding %q", ErrOutOfR0Scope, node.Variable())
	}

	key := labels.Key()
	nodeType, ok := s.Nodes[key]
	if !ok {
		return ValidatedQuery{}, fmt.Errorf("%w: %s", ErrUnknownLabel, key)
	}

	if len(part.Returns) == 0 {
		return ValidatedQuery{}, fmt.Errorf("%w: empty projection", ErrOutOfR0Scope)
	}

	columns := make([]Column, 0, len(part.Returns))
	for _, item := range part.Returns {
		ref, err := r0RefProjection(item.Value)
		if err != nil {
			return ValidatedQuery{}, err
		}
		colType, err := r0ColumnType(ref, nodeType)
		if err != nil {
			return ValidatedQuery{}, err
		}
		columns = append(columns, Column{Name: item.Name, Type: colType})
	}

	params := make([]ResolvedParameter, 0, len(q.Parameters))
	for _, p := range q.Parameters {
		if len(p.Uses) != 1 {
			return ValidatedQuery{}, fmt.Errorf("%w: parameter %q has %d uses (R2 unifies)", ErrOutOfR0Scope, p.Name, len(p.Uses))
		}
		use, ok := p.Uses[0].(query.PropertyUse)
		if !ok {
			return ValidatedQuery{}, fmt.Errorf("%w: parameter %q uses a %T", ErrOutOfR0Scope, p.Name, p.Uses[0])
		}
		useRef := use.Ref()
		prop, ok := nodeType.Properties[useRef.Property]
		if !ok {
			return ValidatedQuery{}, fmt.Errorf("%w: %s.%s", ErrUnknownProperty, useRef.Variable, useRef.Property)
		}
		params = append(params, ResolvedParameter{
			Name: p.Name,
			Type: ResolvedProperty{Type: prop.Type, Nullable: prop.Nullable},
		})
	}

	return ValidatedQuery{
		Columns:    columns,
		Parameters: params,
		Statement:  StatementKind(q.StatementKind),
	}, nil
}

// r0RefProjection extracts the Ref from a projection, rejecting anything but
// a RefProjection at R0.
func r0RefProjection(p query.Projection) (query.Ref, error) {
	rp, ok := p.(query.RefProjection)
	if !ok {
		return query.Ref{}, fmt.Errorf("%w: non-Ref projection (%T)", ErrOutOfR0Scope, p)
	}
	return rp.Ref(), nil
}

// r0ColumnType upgrades a RefProjection's Ref into a ResolvedType against the
// resolved node type: whole-entity (Property == "") becomes ResolvedNode;
// property lookup (Property != "") becomes ResolvedProperty via the schema
// witness.
func r0ColumnType(ref query.Ref, nodeType schema.NodeType) (ResolvedType, error) {
	if ref.Property == "" {
		return ResolvedNode{Labels: nodeType.Labels}, nil
	}
	prop, ok := nodeType.Properties[ref.Property]
	if !ok {
		return nil, fmt.Errorf("%w: %s.%s", ErrUnknownProperty, ref.Variable, ref.Property)
	}
	return ResolvedProperty{Type: prop.Type, Nullable: prop.Nullable}, nil
}
