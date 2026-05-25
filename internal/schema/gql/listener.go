package gql

import (
	"fmt"

	"github.com/antlr4-go/antlr/v4"

	"github.com/antranig-yeretzian/gqlc/internal/grammar/gql/gen"
	"github.com/antranig-yeretzian/gqlc/internal/schema"
)

// listener is the single error sink and collector for a parse: it captures the
// first lexer/parser syntax error (SyntaxError) and collects the parse tree into
// a rawSchema as the ParseTreeWalker descends, both funnelling into l.err. The walk
// cannot be stopped mid-traversal (ADR 0001), but it needs no per-rule error
// guard: fail() keeps the first error, and Parse discards the result — never
// calling resolve — once an error is set. So an Enter* that runs after the first
// error is harmless; there is nothing to remember to add. The listener's job ends
// at producing l.raw; rawSchema.resolve() turns that into the final model
// afterward, in plain Go (see resolve.go).
type listener struct {
	*gen.BaseGQLListener
	*antlr.DefaultErrorListener

	ts *antlr.CommonTokenStream

	// seenGraphType records whether a CREATE PROPERTY GRAPH TYPE statement has
	// been seen, enforcing the exactly-one input scope: a second one is rejected
	// (ErrMultipleGraphTypes), and none at all is rejected (ErrNoGraphType).
	seenGraphType bool

	// raw is the unresolved schema the walk builds up; rawSchema.resolve() turns
	// it into the final model.
	raw rawSchema

	err error
}

// fail records the first error and is idempotent thereafter: the error found
// first in walk order is the one Parse returns, and later failures are dropped.
func (l *listener) fail(err error) {
	if l.err == nil {
		l.err = err
	}
}

// SyntaxError records the first lexer/parser syntax error onto the same l.err
// channel as every collection error. ANTLR keeps reporting after the first error,
// so fail() (idempotent) keeps only the first — matching the fail-fast contract.
// Naming the offending token alongside line:column makes the location concrete
// for a schema author scanning their source.
func (l *listener) SyntaxError(_ antlr.Recognizer, offendingSymbol any, line, column int, msg string, _ antlr.RecognitionException) {
	if tok, ok := offendingSymbol.(antlr.Token); ok && tok.GetText() != "" {
		l.fail(fmt.Errorf("syntax error at %d:%d near %q: %s", line, column, tok.GetText(), msg))
		return
	}
	l.fail(fmt.Errorf("syntax error at %d:%d: %s", line, column, msg))
}

// walk drives the ParseTreeWalker over the tree and returns the first error the
// listener recorded — turning ANTLR's void, side-effecting walk into an ordinary
// error-returning call so the caller never reaches into l.err. A syntax error
// recorded during lexing/parsing means the tree is unreliable, so we surface it
// and never walk.
func (l *listener) walk(tree antlr.Tree) error {
	if l.err != nil {
		return l.err
	}
	antlr.NewParseTreeWalker().Walk(l, tree)
	return l.err
}

func (l *listener) EnterCreateGraphTypeStatement(c *gen.CreateGraphTypeStatementContext) {
	// A second graph type is caught right here: entering one while seenGraphType
	// is already set *is* "more than one". No count is needed — we record the
	// error the moment the second one appears.
	if l.seenGraphType {
		l.fail(ErrMultipleGraphTypes)
		return
	}
	l.seenGraphType = true

	// A LIKE source (graphTypeLikeGraph) reaches this rule; COPY OF is parsed
	// as a different statement and never gets here. Only an inline AS { ... }
	// body (nestedGraphTypeSpecification) is supported.
	if src := c.GraphTypeSource(); src != nil && src.GraphTypeLikeGraph() != nil {
		l.fail(ErrUnsupportedSource)
		return
	}

	l.raw.name = c.CatalogGraphTypeParentAndName().GraphTypeName().Identifier().GetText()
}

// ExitGqlProgram fires once, at the end of the walk, when graphTypes is final.
// "No graph type at all" is the absence of a rule, so it can't be caught by any
// Enter* — only here, at the program root, once everything has been seen. Doing
// it in the listener keeps the whole input-scope check on the l.err channel
// instead of a separate return in Parse.
func (l *listener) ExitGqlProgram(c *gen.GqlProgramContext) {
	if !l.seenGraphType {
		l.fail(ErrNoGraphType)
	}
}

func (l *listener) EnterNodeTypePattern(c *gen.NodeTypePatternContext) {
	n := rawNode{}
	if name := c.NodeTypeName(); name != nil {
		n.name = name.Identifier().GetText()
	}
	// The alias is optional: `(p :Person)` binds `p`, `(:Person)` binds nothing.
	// A node without an alias is fully supported — it just can't be referenced by
	// alias from an edge, only by its inline label set. So when there is none we
	// leave n.alias empty and carry on.
	if alias := c.LocalNodeTypeAlias(); alias != nil {
		n.alias = alias.GetText()
	}

	labels, props, err := l.nodeContent(c.NodeTypeFiller())
	if err != nil {
		l.fail(err)
		return
	}
	n.labels = labels
	n.props = props

	l.raw.nodes = append(l.raw.nodes, n)
}

// EnterNodeTypeKeyLabelSet and EnterEdgeTypeKeyLabelSet reject the
// label-implication form of a key label set; see rejectLabelImplication.
func (l *listener) EnterNodeTypeKeyLabelSet(c *gen.NodeTypeKeyLabelSetContext) {
	l.rejectLabelImplication(c.IMPLIES())
}

func (l *listener) EnterEdgeTypeKeyLabelSet(c *gen.EdgeTypeKeyLabelSetContext) {
	l.rejectLabelImplication(c.IMPLIES())
}

// rejectLabelImplication fails on the label-implication form of a key label set
// (`=> :Label`, the "implied label" syntax). We support only the plain key label
// set (`:Label`); the IMPLIES token ("=>") appears only in the rejected form, so
// its presence is the whole signal. Node and edge key label sets share this.
func (l *listener) rejectLabelImplication(implies antlr.TerminalNode) {
	if implies != nil {
		l.fail(ErrLabelImplication)
	}
}

func (l *listener) EnterEdgeTypePatternUndirected(c *gen.EdgeTypePatternUndirectedContext) {
	l.fail(ErrUndirectedEdge)
}

func (l *listener) EnterEdgeTypePattern(c *gen.EdgeTypePatternContext) {
	directed := c.EdgeTypePatternDirected()
	if directed == nil {
		// Not about errors: this rule fires for both directions, and we only
		// build an edge from the directed form. An undirected pattern has no
		// directed child and is rejected by EnterEdgeTypePatternUndirected.
		return
	}

	e := rawEdge{}
	if name := c.EdgeTypeName(); name != nil {
		e.name = name.Identifier().GetText()
	}

	// The edge type filler is the bracketed arc content `[:LABEL { props }]`: it
	// carries the edge's label set and properties. Both directed alternatives
	// expose canonical source->target via these accessors (the grammar already
	// swaps a left-pointing arc's endpoints).
	var filler gen.IEdgeTypeFillerContext
	if r := directed.EdgeTypePatternPointingRight(); r != nil {
		e.source = sourceRef(r.SourceNodeTypeReference())
		e.target = destRef(r.DestinationNodeTypeReference())
		filler = r.ArcTypePointingRight().EdgeTypeFiller()
	} else if lft := directed.EdgeTypePatternPointingLeft(); lft != nil {
		e.source = sourceRef(lft.SourceNodeTypeReference())
		e.target = destRef(lft.DestinationNodeTypeReference())
		filler = lft.ArcTypePointingLeft().EdgeTypeFiller()
	}

	labels, props, err := l.edgeContent(filler)
	if err != nil {
		l.fail(err)
		return
	}
	e.labels = labels
	e.props = props

	l.raw.edges = append(l.raw.edges, e)
}

// nodeContent reads the label set and property types carried by a node type
// filler — the `:Label { ... }` after an optional alias. A node with no filler or
// no implied content contributes neither labels nor properties.
func (l *listener) nodeContent(f gen.INodeTypeFillerContext) (schema.LabelSet, map[string]schema.Property, error) {
	if f == nil {
		return nil, nil, nil
	}
	ic := f.NodeTypeImpliedContent()
	if ic == nil {
		return nil, nil, nil
	}

	var labels schema.LabelSet
	if ls := ic.NodeTypeLabelSet(); ls != nil {
		labels = labelSet(ls.LabelSetPhrase())
	}
	var spec gen.IPropertyTypesSpecificationContext
	if pts := ic.NodeTypePropertyTypes(); pts != nil {
		spec = pts.PropertyTypesSpecification()
	}
	props, err := l.properties(spec)
	return labels, props, err
}

// edgeContent is the edge-type counterpart of nodeContent: it reads the label set
// and property types from an edge type filler. The two cannot share one helper
// because the grammar gives node and edge fillers distinct generated types.
func (l *listener) edgeContent(f gen.IEdgeTypeFillerContext) (schema.LabelSet, map[string]schema.Property, error) {
	if f == nil {
		return nil, nil, nil
	}
	ic := f.EdgeTypeImpliedContent()
	if ic == nil {
		return nil, nil, nil
	}

	var labels schema.LabelSet
	if ls := ic.EdgeTypeLabelSet(); ls != nil {
		labels = labelSet(ls.LabelSetPhrase())
	}
	var spec gen.IPropertyTypesSpecificationContext
	if pts := ic.EdgeTypePropertyTypes(); pts != nil {
		spec = pts.PropertyTypesSpecification()
	}
	props, err := l.properties(spec)
	return labels, props, err
}

// properties lowers a property types specification into a map keyed by property
// name. A nil spec (a type with no properties) yields a nil map. The same rule
// shape backs both node and edge property types, so both paths reuse this.
func (l *listener) properties(spec gen.IPropertyTypesSpecificationContext) (map[string]schema.Property, error) {
	if spec == nil {
		return nil, nil
	}
	list := spec.PropertyTypeList()
	if list == nil {
		return nil, nil
	}

	out := make(map[string]schema.Property)
	for _, pt := range list.AllPropertyType() {
		p, err := property(pt, l.ts)
		if err != nil {
			return nil, err
		}
		out[p.Name] = p
	}
	return out, nil
}
