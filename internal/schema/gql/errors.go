package gql

import "errors"

// Errors returned by Parse for GQL this parser rejects: constructs outside the
// supported subset, and schemas that violate the node/edge identity rules. They
// are sentinels so callers can branch with errors.Is.
var (
	ErrImpliedLabelIsKeyLabel      = errors.New("implied label is also a declared element type's key label: whether it inherits that type's properties is not settled by the standard")
	ErrUndirectedEdge              = errors.New("undirected edges are not supported")
	ErrEdgeKindArcMismatch         = errors.New("edge kind contradicts arc direction")
	ErrUnknownEndpoint             = errors.New("edge endpoint references an undeclared node type")
	ErrEndpointNotAlias            = errors.New("edge endpoint names a node type instead of a local alias bound to one")
	ErrEndpointFillerHasProperties = errors.New("edge endpoint filler carries properties: an inline endpoint may only name a node type by its label set")
	ErrEndpointFillerImpliesLabels = errors.New("edge endpoint filler implies labels: an inline endpoint may only name a node type by its key label set")
	ErrUnsupportedType             = errors.New("unsupported property value type")
	ErrUnnamedNodeType             = errors.New("node type has an empty key label set")
	ErrUnnamedEdgeType             = errors.New("edge type has an empty key label set")
	ErrDuplicateNodeType           = errors.New("duplicate node type")
	ErrDuplicateEdgeType           = errors.New("duplicate edge type")
	ErrNoGraphType                 = errors.New("no graph type declaration")
	ErrMultipleGraphTypes          = errors.New("more than one graph type declaration")
	ErrUnsupportedSource           = errors.New("unsupported graph type source")
)
