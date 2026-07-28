package gql

import (
	"errors"
	"fmt"
)

// Errors returned by Parse for GQL this parser rejects: constructs outside the
// supported subset, and schemas that violate the node/edge identity rules. They
// are sentinels so callers can branch with errors.Is.
var (
	ErrImpliedLabelIsKeyLabel      = errors.New("implied label is also a declared element type's key label: whether it inherits that type's properties is not settled by the standard")
	ErrUndirectedEdge              = errors.New("undirected edges are a distinct element kind, which gqlc does not model")
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
)

// ErrUnsupportedSource is the class of rejected <graph type source> alternatives
// rather than a leaf sentinel: the two reachable rejections below wrap it, so a
// caller that only asks "was the source rejected" still matches with errors.Is,
// and splitting them did not narrow the public surface. It is also what an
// alternative added to the grammar after this was written would report, having
// no justification of its own to name yet.
//
// Hence its absence from allSentinels, which lists leaves — nothing produces it
// bare today. TestGraphTypeSourceErrorsWrapTheClass is what keeps that honest.
var ErrUnsupportedSource = errors.New("unsupported graph type source")

// ErrLikeGraphSource and ErrCopyOfSource are both rejections of a graph type
// source, and that is all they share. LIKE takes a graphExpression, which
// reaches CURRENT_GRAPH and binding variables — session state a static generator
// has no access to, so it is declined permanently. COPY OF names a graph type in
// the catalogue, which is statically resolvable and merely unimplemented.
//
// They were one error until gqlc-h9n.12, which is why the deviation record had
// to carry two incompatible justifications against a single sentinel and could
// not say which applied. See ADR 0016.
var (
	ErrLikeGraphSource = fmt.Errorf("%w: LIKE derives the graph type from a graph expression, which can name session state", ErrUnsupportedSource)
	ErrCopyOfSource    = fmt.Errorf("%w: COPY OF names a graph type this parser cannot reach, having no catalogue", ErrUnsupportedSource)
)
