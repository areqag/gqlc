package gql

import (
	"fmt"

	"github.com/antlr4-go/antlr/v4"

	"github.com/areqag/gqlc/internal/grammar/gql/gen"
	"github.com/areqag/gqlc/internal/graph"
	"github.com/areqag/gqlc/internal/schema"
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

	// An inline `AS { ... }` body (nestedGraphTypeSpecification) is the only
	// supported source. Testing for it rather than enumerating LIKE and COPY OF
	// means a source alternative added to the grammar later is rejected rather
	// than silently dropped. Both LIKE and the AS-less `COPY OF` spelling reach
	// this rule and are rejected here. Of the two COPY OF spellings only the
	// AS-less one gets this far: `AS COPY OF` matches createGraphStatement
	// instead, so it surfaces as ErrNoGraphType (both spellings are pinned by
	// TestCorpusSpellingTraps).
	if src := c.GraphTypeSource(); src == nil || src.NestedGraphTypeSpecification() == nil {
		l.fail(unsupportedSource(src))
		return
	}

	// The parent path of a catalogue-qualified name is discarded: Schema.Name's
	// one consumer is derivePackage, which needs a Go identifier, and /a/b/G is
	// not one. ADR 0018 decides that; it also records that COPY OF (gqlc-h9n.1)
	// references a graph type *by* catalogue path, at which point /a/b/G and
	// /c/d/G stop being distinguishable here.
	l.raw.name = c.CatalogGraphTypeParentAndName().GraphTypeName().Identifier().GetText()
}

// unsupportedSource names which rejected graphTypeSource alternative was written.
// The guard above still tests for the supported one, so which error this returns
// never decides whether to reject — only what the rejection is called.
//
// src is nil only under error recovery, and an alternative matching neither LIKE
// nor COPY OF is one added to the grammar since this was written; both get the
// bare class error, because neither of the two justifications is theirs.
func unsupportedSource(src gen.IGraphTypeSourceContext) error {
	if src == nil {
		return ErrUnsupportedSource
	}
	switch {
	case src.GraphTypeLikeGraph() != nil:
		return ErrLikeGraphSource
	case src.CopyOfGraphType() != nil:
		return ErrCopyOfSource
	}
	return ErrUnsupportedSource
}

// ExitGqlProgram fires once, at the end of the walk, when graphTypes is final.
// "No graph type at all" is the absence of a rule, so it can't be caught by any
// Enter* — only here, at the program root, once everything has been seen. Doing
// it in the listener keeps the whole input-scope check on the l.err channel
// instead of a separate return in Parse.
func (l *listener) ExitGqlProgram(_ *gen.GqlProgramContext) {
	if !l.seenGraphType {
		l.fail(ErrNoGraphType)
	}
}

func (l *listener) EnterNodeTypePattern(c *gen.NodeTypePatternContext) {
	if nestedDepth(c) > 1 {
		return
	}
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

	fc, err := l.nodeContent(c.NodeTypeFiller())
	if err != nil {
		l.fail(err)
		return
	}
	n.hasKeyLabelSet = fc.hasKeyLabelSet
	n.keyLabels = fc.keyLabels
	n.impliedLabels = fc.impliedLabels
	n.props = fc.props

	l.raw.nodes = append(l.raw.nodes, n)
}

// EnterNodeTypePhrase collects the phrase form (`NODE TYPE Person :Person { ... }
// AS p`), the other alternative of nodeTypeSpecification. It carries the same
// parts as the pattern form in different places: the name and the filler are both
// under nodeTypePhraseFiller, and the alias follows AS rather than opening the
// parens.
func (l *listener) EnterNodeTypePhrase(c *gen.NodeTypePhraseContext) {
	if nestedDepth(c) > 1 {
		return
	}
	filler := c.NodeTypePhraseFiller()

	n := rawNode{}
	if name := filler.NodeTypeName(); name != nil {
		n.name = name.Identifier().GetText()
	}
	// As in the pattern form the alias is optional. A node type without one is
	// still fully supported; it just leaves a CONNECTING endpoint nothing to name
	// it by, those being alias-only.
	if alias := c.LocalNodeTypeAlias(); alias != nil {
		n.alias = alias.GetText()
	}

	fc, err := l.nodeContent(filler.NodeTypeFiller())
	if err != nil {
		l.fail(err)
		return
	}
	n.hasKeyLabelSet = fc.hasKeyLabelSet
	n.keyLabels = fc.keyLabels
	n.impliedLabels = fc.impliedLabels
	n.props = fc.props

	l.raw.nodes = append(l.raw.nodes, n)
}

// EnterEdgeTypePhrase collects the phrase form of an edge type (`DIRECTED EDGE
// TYPE Wrote :WROTE { ... } CONNECTING (a -> b)`). Its endpoints are alias-only:
// endpointPairPointingRight and its siblings take a bare sourceNodeTypeAlias,
// where the pattern form's sourceNodeTypeReference also admits an inline
// nodeTypeFiller. An endpoint that names a node type instead is left for
// resolution to reject (ErrEndpointNotAlias).
//
// That rejection is a reading of a slot name, not a fact the grammar states.
// ISO's own production list distinguishes <source node type alias> from <source
// node type reference>, which supports it; the Syntax Rules that would settle it
// are prose we do not have, and TuGraph's draft grammar spells the same slot as
// a node type name. See internal/grammar/gql/SOURCE.md.
func (l *listener) EnterEdgeTypePhrase(c *gen.EdgeTypePhraseContext) {
	if nestedDepth(c) > 1 {
		return
	}
	directed := c.EndpointPairPhrase().EndpointPair().EndpointPairDirected()
	// <edge kind> is mandatory in <edge type phrase> (ISO/IEC 39075:2024 BNF,
	// standards.iso.org/iso-iec/39075/ed-1/en/), so it is always present here.
	// Checking kind against the connector direction *before* the directed-only
	// bail below means a mismatch reports ErrEdgeKindArcMismatch rather than
	// falling through to EnterEndpointPairUndirected's ErrUndirectedEdge — the
	// message names the actual authoring mistake (edgeKind vs. connector), not
	// the accepted-subset gap that both readings of a bare undirected connector
	// would hit anyway.
	if edgeKindArcMismatch(c.EdgeKind(), directed != nil) {
		l.fail(ErrEdgeKindArcMismatch)
		return
	}
	if directed == nil {
		// Not about errors, and the same shape as EnterEdgeTypePattern: this rule
		// fires for both endpoint pair directions and only the directed one
		// becomes an edge. The undirected pair is rejected by
		// EnterEndpointPairUndirected.
		return
	}

	e := rawEdge{}
	filler := c.EdgeTypePhraseFiller()
	if name := filler.EdgeTypeName(); name != nil {
		e.name = name.Identifier().GetText()
	}
	// Both directed alternatives name their ends by role, so `(b <- a)` needs no
	// swap here — the grammar already reports a as the source.
	if r := directed.EndpointPairPointingRight(); r != nil {
		e.source = rawEndpoint{alias: r.SourceNodeTypeAlias().GetText()}
		e.target = rawEndpoint{alias: r.DestinationNodeTypeAlias().GetText()}
	} else if lft := directed.EndpointPairPointingLeft(); lft != nil {
		e.source = rawEndpoint{alias: lft.SourceNodeTypeAlias().GetText()}
		e.target = rawEndpoint{alias: lft.DestinationNodeTypeAlias().GetText()}
	}

	fc, err := l.edgeContent(filler.EdgeTypeFiller())
	if err != nil {
		l.fail(err)
		return
	}
	e.hasKeyLabelSet = fc.hasKeyLabelSet
	e.keyLabels = fc.keyLabels
	e.impliedLabels = fc.impliedLabels
	e.props = fc.props

	l.raw.edges = append(l.raw.edges, e)
}

// edgeKindArcMismatch is true iff the declared edgeKind and the arc/connector
// direction disagree: kind=UNDIRECTED with a directed arc, or kind=DIRECTED with
// an undirected arc. The nil kind (absent in the pattern form's optional prefix)
// is not a mismatch — the author has made no claim.
//
// The ISO/IEC 39075 BNF (ISO/IEC 39075:2024, <edge type pattern> / <edge type
// phrase> / <edge kind>, standards.iso.org/iso-iec/39075/ed-1/en/) admits these
// spellings: <edge kind> and the arc/connector direction sit in independent
// slots with no cross-constraint in the grammar. Which of the two disagreeing
// signals prevails is a semantic question, and the normative prose that would
// answer it lives in the paid PDF, which we do not have. Rejecting rather than
// silently choosing one is our decision under the no-dialect principle: the
// alternative — resolving in favour of the arc — silently reinterprets an
// UNDIRECTED-marked edge as directed, which is the class-2 defect this bead
// exists to close. Recorded as a deviation under gqlc-0ri pending a citation
// from the normative prose.
func edgeKindArcMismatch(kind gen.IEdgeKindContext, arcDirected bool) bool {
	if kind == nil {
		return false
	}
	if kind.UNDIRECTED() != nil && arcDirected {
		return true
	}
	if kind.DIRECTED() != nil && !arcDirected {
		return true
	}
	return false
}

// EnterEdgeTypePatternUndirected and EnterEndpointPairUndirected reject an
// undirected edge written as the pattern form's `~[ ]~` arc or as the phrase
// form's `CONNECTING (a ~ b)`. Same reason for both: in GQL's data model an
// undirected edge is a distinct element kind, not a directed edge matched from
// either end, so there is nothing for gqlc's directed edge model to hold it in.
//
// Note the reason is *not* that EdgeKey wants a source->target triple. Both
// undirected productions keep their endpoint types in written order, so a
// canonical-direction encoding is mechanically available — and would be wrong
// rather than merely lossy, because `IS DIRECTED` (§19.8) and `IS SOURCE OF`
// (§19.10) make the distinction observable. ADR 0016 has the argument.
func (l *listener) EnterEdgeTypePatternUndirected(_ *gen.EdgeTypePatternUndirectedContext) {
	l.fail(ErrUndirectedEdge)
}

func (l *listener) EnterEndpointPairUndirected(_ *gen.EndpointPairUndirectedContext) {
	l.fail(ErrUndirectedEdge)
}

func (l *listener) EnterEdgeTypePattern(c *gen.EdgeTypePatternContext) {
	if nestedDepth(c) > 1 {
		return
	}
	directed := c.EdgeTypePatternDirected()
	// <edge kind> is optional in <edge type pattern> (ISO/IEC 39075:2024 BNF,
	// standards.iso.org/iso-iec/39075/ed-1/en/): the whole `[<edge kind>
	// <edge synonym> [TYPE] <edge type name>]` prefix is one optional group, and
	// a bare `(a)-[:E]->(b)` has no kind. The mismatch check reads that as
	// "no claim, no contradiction". When a kind IS present it must not
	// contradict the arc: fire the mismatch sentinel first so
	// `DIRECTED ... ~[:E]~ ...` reports the kind/arc conflict rather than
	// falling through to EnterEdgeTypePatternUndirected's ErrUndirectedEdge,
	// which names the accepted-subset gap instead of the author's actual mistake.
	if edgeKindArcMismatch(c.EdgeKind(), directed != nil) {
		l.fail(ErrEdgeKindArcMismatch)
		return
	}
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
	//
	// Reading the source first is what makes a defective pair report the source's
	// rejection, and the pick is by role rather than written position — for `<-`
	// the source is the rightmost endpoint — so respelling an edge in the other
	// direction does not move its diagnostic to the other end.
	// TestEndpointFillerMixedRejectionsReportTheSource goes red on either swap.
	var (
		filler gen.IEdgeTypeFillerContext
		src    rawEndpoint
		dst    rawEndpoint
		refErr error
	)
	if r := directed.EdgeTypePatternPointingRight(); r != nil {
		src, refErr = sourceRef(r.SourceNodeTypeReference())
		if refErr == nil {
			dst, refErr = destRef(r.DestinationNodeTypeReference())
		}
		filler = r.ArcTypePointingRight().EdgeTypeFiller()
	} else if lft := directed.EdgeTypePatternPointingLeft(); lft != nil {
		src, refErr = sourceRef(lft.SourceNodeTypeReference())
		if refErr == nil {
			dst, refErr = destRef(lft.DestinationNodeTypeReference())
		}
		filler = lft.ArcTypePointingLeft().EdgeTypeFiller()
	}
	if refErr != nil {
		l.fail(refErr)
		return
	}
	e.source = src
	e.target = dst

	fc, err := l.edgeContent(filler)
	if err != nil {
		l.fail(err)
		return
	}
	e.hasKeyLabelSet = fc.hasKeyLabelSet
	e.keyLabels = fc.keyLabels
	e.impliedLabels = fc.impliedLabels
	e.props = fc.props

	l.raw.edges = append(l.raw.edges, e)
}

// fillerContent is a node or edge type filler read off the parse tree, split at
// `=>` the way the grammar splits it. It is the return of nodeContent and
// edgeContent, which have four results between them and read better named than
// positional.
type fillerContent struct {
	hasKeyLabelSet bool
	keyLabels      graph.LabelSet
	impliedLabels  graph.LabelSet
	props          map[string]schema.Property
}

// nodeContent reads a node type filler — the `:Key => :Implied { ... }` after an
// optional alias — into its key label set, its implied label set and its property
// types. A node with no filler contributes none of the three.
//
// The two label sets are kept apart rather than unioned here because resolve()
// needs both: the key one becomes the type's identity and the union becomes its
// complete label set, and inferring an absent key label set (GG22) is a semantic
// rule that belongs in the pure-Go pass, not in a tree read.
//
// Note the property types live under the *implied* content in the grammar
// (nodeTypeImpliedContent alternatives 2 and 3), so there is no such thing as a
// property declared on the key side. A type's properties are its own either way.
func (l *listener) nodeContent(f gen.INodeTypeFillerContext) (fillerContent, error) {
	if f == nil {
		return fillerContent{}, nil
	}

	var fc fillerContent
	if kls := f.NodeTypeKeyLabelSet(); kls != nil {
		fc.hasKeyLabelSet = true
		fc.keyLabels = labelSet(kls.LabelSetPhrase())
	}

	ic := f.NodeTypeImpliedContent()
	if ic == nil {
		return fc, nil
	}
	if ls := ic.NodeTypeLabelSet(); ls != nil {
		fc.impliedLabels = labelSet(ls.LabelSetPhrase())
	}
	var spec gen.IPropertyTypesSpecificationContext
	if pts := ic.NodeTypePropertyTypes(); pts != nil {
		spec = pts.PropertyTypesSpecification()
	}
	props, err := l.properties(spec)
	if err != nil {
		return fillerContent{}, err
	}
	fc.props = props
	return fc, nil
}

// edgeContent is the edge-type counterpart of nodeContent, splitting an edge type
// filler at `=>` on the same terms. The two cannot share one helper because the
// grammar gives node and edge fillers distinct generated types.
func (l *listener) edgeContent(f gen.IEdgeTypeFillerContext) (fillerContent, error) {
	if f == nil {
		return fillerContent{}, nil
	}

	var fc fillerContent
	if kls := f.EdgeTypeKeyLabelSet(); kls != nil {
		fc.hasKeyLabelSet = true
		fc.keyLabels = labelSet(kls.LabelSetPhrase())
	}

	ic := f.EdgeTypeImpliedContent()
	if ic == nil {
		return fc, nil
	}
	if ls := ic.EdgeTypeLabelSet(); ls != nil {
		fc.impliedLabels = labelSet(ls.LabelSetPhrase())
	}
	var spec gen.IPropertyTypesSpecificationContext
	if pts := ic.EdgeTypePropertyTypes(); pts != nil {
		spec = pts.PropertyTypesSpecification()
	}
	props, err := l.properties(spec)
	if err != nil {
		return fillerContent{}, err
	}
	fc.props = props
	return fc, nil
}

// nestedDepth is the number of enclosing nestedGraphTypeSpecification contexts
// above ctx. It gates the four element-type collectors so that a node or edge
// declared inside a closedGraphReferenceValueType body (GQL.g4:1926, which nests
// a whole graph type as a property value type) is not collected as an element of
// the outer graph type. Depth 1 is an element of the outer graph body itself
// (every valid element type has one enclosing nestedGraphTypeSpecification —
// the outer body; AS is optional in GQL.g4:350 so the threshold does not depend
// on the keyword being present); depth > 1 means the element sits inside a
// graph-typed property.
// Coverage has the same notion (nestingDepth in corpus_coverage_test.go), and
// the two are deliberately not shared: the corpus coverage gate must measure the
// parse tree independently of what this listener computed from it, so a bug in a
// shared helper would silently hide from both.
func nestedDepth(ctx antlr.ParserRuleContext) int {
	depth := 0
	for parent := ctx.GetParent(); parent != nil; parent = parent.GetParent() {
		if _, ok := parent.(gen.INestedGraphTypeSpecificationContext); ok {
			depth++
		}
	}
	return depth
}

// properties lowers a property types specification into a map keyed by property
// name. A nil spec (a type with no properties) yields a nil map. The same rule
// shape backs both node and edge property types, so both paths reuse this.
//
// The repeated-name check is here rather than in resolve() because this is the
// only place the LIST is still visible: the map is the thing that loses the
// collision, so a guard downstream of it has nothing left to compare. Per ADR
// 0030 a repeat is rejected — the map cannot hold two declarations under one
// name, and the previous behaviour (last write wins, by accident of assignment
// order) discarded the first declaration's type and its NOT NULL with nothing
// said.
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
		if _, seen := out[p.Name]; seen {
			return nil, fmt.Errorf("%w: %q", ErrDuplicatePropertyName, p.Name)
		}
		out[p.Name] = p
	}
	return out, nil
}
