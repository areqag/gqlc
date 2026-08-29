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
	ErrUnknownEndpoint             = errors.New("edge endpoint references an undeclared node type")
	ErrEndpointNotAlias            = errors.New("edge endpoint names a node type instead of a local alias bound to one")
	ErrEndpointFillerHasProperties = errors.New("edge endpoint filler carries properties: an inline endpoint may only name a node type by its label set")
	ErrEndpointFillerImpliesLabels = errors.New("edge endpoint filler implies labels: an inline endpoint may only name a node type by its key label set")
	ErrUnsupportedType             = errors.New("unsupported property value type")
	ErrUnnamedNodeType             = errors.New("node type has an empty key label set: node types are identified by that set, so an unlabelled one has no identity and no two of them could be told apart")
	ErrUnnamedEdgeType             = errors.New("edge type has an empty key label set: edge types are identified by that set together with their endpoints, so an unlabelled one has no identity of its own")
	ErrMultiLabelEdgeType          = errors.New("edge type has a multi-label key set: no Cypher conjunction syntax exists for edge labels, so no query can ever reference this type")
	ErrDuplicateNodeType           = errors.New("duplicate node type")
	ErrDuplicateEdgeType           = errors.New("duplicate edge type")
	ErrNoGraphType                 = errors.New("no graph type declaration")
	ErrMultipleGraphTypes          = errors.New("more than one graph type declaration")
)

// ErrEdgeKindArcMismatch is returned when a declaration's edgeKind
// (UNDIRECTED or DIRECTED) contradicts the arc connector direction
// (->, <-, ~, or TO). This is a provisional deviation: ISO/IEC 39075
// Syntax Rules may forbid the combination (outcome 1) or specify which
// wins (outcome 2); the implementation-defined.xml has no item for it.
// Rejection is the interim posture — it fails loudly rather than
// silently picking a reading. Revisit when the Syntax Rules are available.
// Tracked as gqlc-xtq.
var ErrEdgeKindArcMismatch = errors.New("edge kind contradicts arc direction")

// ErrDuplicatePropertyName is returned when one propertyTypeList declares the
// same property name twice. Like ErrEdgeKindArcMismatch this is a provisional
// deviation rather than a settled reading: the free ISO BNF admits the syntax
// (<property type list> ::= <property type> [{ <comma> <property type> }...]
// states no uniqueness constraint), so whether a repeat is an error lives in
// the Syntax Rules prose gqlc-lir declined to buy. Rejection is the interim
// posture, chosen because the alternative — keeping one declaration and
// discarding the other — is the only reading that can be wrong silently. See
// ADR 0030. Revisit when the Syntax Rules are available.
var ErrDuplicatePropertyName = errors.New("property name declared more than once in one property types specification")

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
//
// ErrCopyOfSource narrowed under ADR 0034: a catalogue exists now, but only a
// Loader holds one. So it is what Parse alone reports, and its message is
// literally true of the io.Reader Parse is handed — a reader has no directory to
// resolve against. Load reports it never.
var (
	ErrLikeGraphSource = fmt.Errorf("%w: LIKE derives the graph type from a graph expression, which can name session state", ErrUnsupportedSource)
	ErrCopyOfSource    = fmt.Errorf("%w: COPY OF names a graph type this parser cannot reach, having no catalogue", ErrUnsupportedSource)
)

// The four graph type reference spellings gqlc declines permanently, each
// wrapping ErrUnsupportedSource so a caller asking only "was the source
// rejected" keeps matching (the ADR 0016 pattern: a rejection carries its own
// justification). They are judgments about the spelling and not about any
// particular catalogue, so they fire in the lowering and Parse and Load report
// them identically. ADR 0034 §3.3 argues each.
var (
	ErrReferenceParameter        = fmt.Errorf("%w: a substituted parameter reference is bound at execution time, and a build-time catalogue has no parameter values", ErrUnsupportedSource)
	ErrHomeSchemaReference       = fmt.Errorf("%w: HOME_SCHEMA is a property of a session, and gqlc has none; unlike CURRENT_SCHEMA it has no static referent to translate to", ErrUnsupportedSource)
	ErrObjectParentReference     = fmt.Errorf("%w: an object parent is a catalogue object containing other objects, and a directory-backed catalogue has no container between a directory and a file", ErrUnsupportedSource)
	ErrDelimitedReferenceSegment = fmt.Errorf("%w: a delimited identifier may contain a solidus, a full stop, or nothing at all, so it is not one safe path element", ErrUnsupportedSource)
)

// The four ways resolving a SUPPORTED reference spelling fails. They stand alone
// rather than wrapping ErrUnsupportedSource: the source was accepted and the
// catalogue could not honour it, which is a different question from whether gqlc
// reads the construct at all. No class is minted over four leaves — ADR 0034
// §3.6.
var (
	ErrReferenceOutsideCatalogue = errors.New("graph type reference climbs above the catalogue root")
	ErrDanglingReference         = errors.New("graph type reference names no file in the catalogue")
	ErrReferenceCycle            = errors.New("graph type references form a cycle")
	ErrReferenceNameMismatch     = errors.New("referenced file declares a different graph type name than the reference that found it")
)

// The value-type families gqlc declines, each wrapping ErrUnsupportedType so a
// caller asking only "was the property type rejected" keeps matching. Unlike
// ErrUnsupportedSource, the class here is also produced bare and so stays in
// allSentinels: LIST/ARRAY reports it (gqlc-h9n.5 owns that decline and it has
// no justification of its own to name yet), as does any predefined scalar
// spelling typeSpellings does not carry.
//
// Each is named after the ISO production it declines, and
// TestValueTypeFamiliesAreIsoProductions checks those names against
// isobnf.DDLClosure — so the taxonomy is the standard's rather than one gqlc
// invented to suit its internals.
//
// The split that matters is not five ways but two, and ADR 0019 argues it: the
// first three say what the construct *is*, and no change to the model or the
// target store reaches them. The last two say what gqlc has not built, so the
// "yet" in their messages is load-bearing — gqlc-h9n.33 is the bead that
// deletes ErrRecordValueType; gqlc-h9n.34 (ADR 0020) already deleted the open
// halves of ErrDynamicUnionType, leaving it to cover only the closed unions.
var (
	ErrPathValueType       = fmt.Errorf("%w: PATH is a traversal a query produces, not a value an element stores", ErrUnsupportedType)
	ErrReferenceValueType  = fmt.Errorf("%w: a graph, node, edge or binding table reference is a handle into a graph rather than a value, and a property holding one would be a relationship no traversal can follow", ErrUnsupportedType)
	ErrImmaterialValueType = fmt.Errorf("%w: NULL admits only null, which the property's own nullability already records, and NOTHING (with NULL NOT NULL) admits nothing at all", ErrUnsupportedType)
	ErrRecordValueType     = fmt.Errorf("%w: RECORD needs a property type that carries its fields, which this model does not have yet", ErrUnsupportedType)
	ErrDynamicUnionType    = fmt.Errorf("%w: closed dynamic unions (ANY VALUE<A|B> and bare A|B) need a property type that carries their members, which this model does not have yet", ErrUnsupportedType)
)
