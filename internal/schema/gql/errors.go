package gql

import "errors"

// Errors returned by Parse for GQL this parser rejects: constructs outside the
// supported subset, and schemas that violate the node/edge identity rules. They
// are sentinels so callers can branch with errors.Is.
var (
	ErrLabelImplication   = errors.New(`label implication ("=>") is not supported`)
	ErrUndirectedEdge     = errors.New("undirected edges are not supported")
	ErrUnknownEndpoint    = errors.New("edge endpoint references an undeclared node type")
	ErrUnsupportedType    = errors.New("unsupported property value type")
	ErrUnnamedNodeType    = errors.New("node type has no label")
	ErrUnnamedEdgeType    = errors.New("edge type has no label")
	ErrDuplicateNodeType  = errors.New("duplicate node type")
	ErrDuplicateEdgeType  = errors.New("duplicate edge type")
	ErrNoGraphType        = errors.New("no graph type declaration")
	ErrMultipleGraphTypes = errors.New("more than one graph type declaration")
	ErrUnsupportedSource  = errors.New("unsupported graph type source")
)
