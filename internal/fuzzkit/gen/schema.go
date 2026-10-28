package gen

import (
	"fmt"
	"strings"

	"github.com/areqag/gqlc/internal/fuzzkit"
	"github.com/areqag/gqlc/internal/graph"
)

// Vocab is what Schema declared, in the form Query draws its identifiers
// from. Labels, RelTypes and Props are the flat lists the plan names (§6.2);
// Nodes and Edges keep the structure a pattern needs, so that a hop is drawn
// from a declared (source, label, target) triple rather than from three
// independent lists.
//
// A Vocab built by hand rather than by Schema is fine: Query reads only these
// fields, and an empty Vocab still yields a query (one over no labels, which
// the resolver refuses).
type Vocab struct {
	// Labels is every node label, each once, in declaration order.
	Labels []string
	// RelTypes is every edge label, each once, in declaration order.
	RelTypes []string
	// Props is every (owner, property, type) triple: the node types' and then
	// the edge types' properties.
	Props []Prop
	// Nodes is the node types in declaration order.
	Nodes []Node
	// Edges is the edge types in declaration order.
	Edges []Edge
}

// Node is one declared node type.
type Node struct {
	// Labels is the key label set, in declaration order.
	Labels []string
	Props  []Prop
}

// Edge is one declared edge type, From and To indexing Vocab.Nodes. The
// declared direction is From to To whichever arc spelling wrote it.
type Edge struct {
	Type     string
	From, To int
	Props    []Prop
}

// Prop is one declared property. Owner is the label set of its node type
// joined by "&", or its edge type's label. Type is the value type exactly as
// the schema text spells it, NOT NULL excluded; NotNull is the property's own
// qualifier.
type Prop struct {
	Owner   string
	Name    string
	Type    string
	NotNull bool
}

var (
	nodeLabels = []string{"Person", "Post", "Tag", "Company", "Comment", "Forum", "City", "Employee"}
	relTypes   = []string{"KNOWS", "AUTHORED", "LIKES", "TAGGED", "WORKS_AT", "REPLY_OF", "LOCATED_IN", "FOLLOWS"}
	propNames  = []string{"id", "name", "age", "title", "score", "rank", "since", "weight", "tags", "email", "born", "rating", "views", "active", "nick", "meta"}
	fieldNames = []string{"a", "b", "c", "d"}

	// scalarTypes is the scalar half of the lattice, weighted towards the
	// spellings every backend serves by listing them first: an exhausted
	// stream picks the first.
	scalarTypes = []string{
		"INT64", "STRING", "INT32", "BOOL", "FLOAT64", "INT", "DATE", "TIMESTAMP",
		"INT8", "INT16", "UINT8", "UINT16", "UINT32", "UINT64", "UINT",
		"FLOAT32", "FLOAT", "REAL", "DOUBLE", "SMALLINT", "BIGINT",
		"ZONED DATETIME", "LOCAL DATETIME", "ZONED TIME", "LOCAL TIME",
		"DURATION(DAY TO SECOND)", "UUID", "VARCHAR(64)", "BOOLEAN",
	}

	// richScalarTypes adds the scalars a backend has no carrier for (BYTES
	// on AGE), drawn only where constructed types are.
	richScalarTypes = append(append([]string(nil), scalarTypes...), "BYTES")
)

// rare reports a 1-in-n event. It is false on an exhausted stream, so the
// mutation and low-rate arms all sit behind decisions a run of zero bytes
// never takes.
func rare(s *fuzzkit.Stream, n int) bool {
	return s.Int(n) == n-1
}

// Schema writes one CREATE PROPERTY GRAPH TYPE statement and returns the
// Vocab it declares.
//
// What it writes: 1–6 node types keyed on label sets drawn from a fixed
// alphabet, multi-label at a low rate and named with NODE TYPE when they are;
// 0–6 properties each over the type lattice, with NOT NULL flags; 0–6 edge
// types between declared node types, written with one of five arc spellings
// (pattern `-[]->`, pattern `<-[]-`, phrase `->`, phrase `<-`, phrase `TO`)
// and with aliased or inline endpoints; edge labels shared across endpoint
// pairs at a low rate, named or not, which is what reaches codegen's Rule 4
// (ErrUnnamedMultiLabelType) and ADR 0027.
//
// The controlled mutations, each at a low rate: an undirected edge
// (gql.ErrUndirectedEdge), a property declared twice
// (gql.ErrDuplicatePropertyName), and an edge endpoint naming an undeclared
// alias (gql.ErrUnknownEndpoint). A mutated schema is still returned with the
// Vocab of what it declared, so a query against it reaches the parser's
// refusal rather than nothing.
func Schema(s *fuzzkit.Stream) (string, Vocab) {
	var v Vocab
	// Constructed types (LIST, RECORD, closed unions, VECTOR) and BYTES are
	// drawn only in a schema that opts in, at a low rate: one RECORD or
	// numeric union anywhere makes the neo4j backend refuse the whole batch,
	// and BYTES the AGE one, so spreading them over every schema would keep
	// most pairs out of codegen.
	rich := rare(s, 4)
	decls := declareNodes(s, &v, rich)
	decls = append(decls, declareEdges(s, &v, rich)...)
	return "CREATE PROPERTY GRAPH TYPE G AS {\n    " + strings.Join(decls, ",\n    ") + "\n}\n", v
}

// declareNodes draws 1–6 node types with distinct key label sets into v and
// returns their declarations. Node type i is declared under the alias n<i>.
func declareNodes(s *fuzzkit.Stream, v *Vocab, rich bool) []string {
	var decls []string
	seenSet := map[graph.LabelSetKey]bool{}
	seenLabel := map[string]bool{}
	for i := range 1 + s.Int(6) {
		labels := []string{nodeLabels[(i+s.Int(len(nodeLabels)))%len(nodeLabels)]}
		if extra := s.Pick(nodeLabels); rare(s, 12) && extra != labels[0] {
			labels = append(labels, extra)
		}
		key := graph.LabelSet(labels).Key()
		if seenSet[key] {
			continue
		}
		seenSet[key] = true
		owner := strings.Join(labels, "&")
		props := genProps(s, owner, rich)

		head := ""
		if len(labels) > 1 && !rare(s, 8) {
			head = "NODE TYPE " + strings.Join(labels, "") + " "
		}
		decls = append(decls, fmt.Sprintf("%s(n%d :%s%s)", head, len(v.Nodes), owner, propsText(s, props)))

		v.Nodes = append(v.Nodes, Node{Labels: labels, Props: props})
		v.Props = append(v.Props, props...)
		for _, l := range labels {
			if !seenLabel[l] {
				seenLabel[l] = true
				v.Labels = append(v.Labels, l)
			}
		}
	}
	return decls
}

// declareEdges draws 0–6 edge types between v's node types into v and returns
// their declarations. Each takes a label no earlier edge type carries, except
// at a low rate, when it shares one on purpose.
func declareEdges(s *fuzzkit.Stream, v *Vocab, rich bool) []string {
	seenEdge := map[string]bool{}
	uses := map[string]int{}
	for range s.Int(7) {
		from, to := s.Int(len(v.Nodes)), s.Int(len(v.Nodes))
		var unused []string
		for _, r := range relTypes {
			if uses[r] == 0 {
				unused = append(unused, r)
			}
		}
		rel := s.Pick(unused)
		if len(v.RelTypes) > 0 && (rel == "" || rare(s, 8)) {
			rel = s.Pick(v.RelTypes)
		}
		ek := fmt.Sprintf("%s|%d|%d", rel, from, to)
		if seenEdge[ek] {
			continue
		}
		seenEdge[ek] = true
		e := Edge{Type: rel, From: from, To: to, Props: genProps(s, rel, rich)}
		v.Edges = append(v.Edges, e)
		v.Props = append(v.Props, e.Props...)
		if uses[rel] == 0 {
			v.RelTypes = append(v.RelTypes, rel)
		}
		uses[rel]++
	}
	// Written after all are drawn, because whether a label is shared is a
	// fact about every edge type carrying it, the first included.
	decls := make([]string, 0, len(v.Edges))
	for _, e := range v.Edges {
		decls = append(decls, edgeDecl(s, *v, e, uses[e.Type] > 1))
	}
	return decls
}

// genProps draws 0–6 distinctly named properties. Distinct names are the
// valid shape; propsText re-emits one of them at a low rate as the
// duplicate-property mutation.
func genProps(s *fuzzkit.Stream, owner string, rich bool) []Prop {
	n := s.Int(7)
	seen := map[string]bool{}
	var out []Prop
	for range n {
		name := s.Pick(propNames)
		if seen[name] {
			continue
		}
		seen[name] = true
		t := s.Pick(scalarTypes)
		if rich {
			t = valueType(s, 0)
		}
		out = append(out, Prop{Owner: owner, Name: name, Type: t, NotNull: s.Bool() && qualifiable(t)})
	}
	return out
}

// propsText writes a property block, with a leading space, or "" for an
// owner with no properties (` { }` at a low rate).
func propsText(s *fuzzkit.Stream, props []Prop) string {
	if len(props) == 0 {
		if rare(s, 4) {
			return " { }"
		}
		return ""
	}
	var b strings.Builder
	b.WriteString(" {")
	for i, p := range props {
		if i > 0 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, " %s :: %s", p.Name, p.Type)
		if p.NotNull {
			b.WriteString(" NOT NULL")
		}
	}
	if rare(s, 150) {
		fmt.Fprintf(&b, ", %s :: %s", props[0].Name, props[0].Type) // mutation: duplicate property
	}
	b.WriteString(" }")
	return b.String()
}

// valueType draws a value type spelling. Depth bounds the nesting of LIST,
// RECORD and closed unions so a type stays small.
func valueType(s *fuzzkit.Stream, depth int) string {
	if depth >= 2 || !rare(s, 5) {
		return s.Pick(richScalarTypes)
	}
	switch s.Int(4) {
	case 0:
		return "LIST<" + qualified(s, valueType(s, depth+1)) + ">"
	case 1:
		n := 1 + s.Int(3)
		fields := make([]string, 0, n)
		for i := range n {
			fields = append(fields, fieldNames[i]+" :: "+qualified(s, valueType(s, depth+1)))
		}
		return "RECORD { " + strings.Join(fields, ", ") + " }"
	case 2:
		n := 2 + s.Int(2)
		members := make([]string, 0, n)
		for range n {
			members = append(members, s.Pick(richScalarTypes))
		}
		return "ANY<" + strings.Join(members, " | ") + ">"
	default:
		return fmt.Sprintf("VECTOR<%s>(%d)", s.Pick([]string{"FLOAT32", "FLOAT64", "INT8", "INT32"}), 1+s.Int(16))
	}
}

// qualified appends NOT NULL to t on a coin flip where the grammar allows it.
func qualified(s *fuzzkit.Stream, t string) string {
	if s.Bool() && qualifiable(t) {
		return t + " NOT NULL"
	}
	return t
}

// qualifiable reports whether a NOT NULL may follow the spelling t. A closed
// union admits none of its own (GQL.g4 closedDynamicUnionType); a trailing one
// would be read as the last member's, and the grammar then refuses it.
func qualifiable(t string) bool {
	return !strings.HasPrefix(t, "ANY<")
}

// edgeDecl writes one edge type in one of the five arc spellings. shared says
// e's label also labels another edge type; such an edge is named unless a
// low-rate draw leaves it unnamed for codegen to refuse (Rule 4).
func edgeDecl(s *fuzzkit.Stream, v Vocab, e Edge, shared bool) string {
	src, dst := fmt.Sprintf("n%d", e.From), fmt.Sprintf("n%d", e.To)
	if rare(s, 150) {
		dst = "ghost" // mutation: an endpoint naming no node type
	}
	props := propsText(s, e.Props)
	name := ""
	if shared && !rare(s, 8) {
		name = edgeName(e)
	}
	from, to := endpoint(s, v, src, e.From), endpoint(s, v, dst, e.To)
	if rare(s, 150) {
		// mutation: an undirected edge, which gqlc does not model
		return fmt.Sprintf("%s ~[:%s%s]~ %s", from, e.Type, props, to)
	}
	switch s.Int(5) {
	case 0, 1:
		return fmt.Sprintf("%s%s -[:%s%s]-> %s", edgeHead(name), from, e.Type, props, to)
	case 2:
		return fmt.Sprintf("%s%s <-[:%s%s]- %s", edgeHead(name), to, e.Type, props, from)
	default:
		if !shared && !rare(s, 4) {
			name = edgeName(e)
		}
		return phraseDecl(s, e, name, src, dst, props)
	}
}

// phraseDecl writes the DIRECTED EDGE TYPE … CONNECTING form, whose arc is
// spelled `->`, `<-` or TO over the endpoints' aliases.
func phraseDecl(s *fuzzkit.Stream, e Edge, name, src, dst, props string) string {
	label := ":" + e.Type
	if s.Bool() || name == "" {
		label = "LABEL " + e.Type // an unnamed phrase needs the keyword
	}
	arc := src + " -> " + dst
	switch s.Int(3) {
	case 1:
		arc = dst + " <- " + src
	case 2:
		arc = src + " TO " + dst
	}
	head := "DIRECTED EDGE TYPE "
	if name != "" {
		head += name + " "
	}
	return fmt.Sprintf("%s%s%s CONNECTING (%s)", head, label, props, arc)
}

func edgeName(e Edge) string {
	return fmt.Sprintf("%s%d%d", strings.ReplaceAll(e.Type, "_", ""), e.From, e.To)
}

func edgeHead(name string) string {
	if name == "" {
		return ""
	}
	return "DIRECTED EDGE TYPE " + name + " "
}

// endpoint writes a pattern-form endpoint: the alias, or at a low rate the
// node type's key label set inline.
func endpoint(s *fuzzkit.Stream, v Vocab, alias string, i int) string {
	if alias == "ghost" || !rare(s, 3) {
		return "(" + alias + ")"
	}
	return "(:" + strings.Join(v.Nodes[i].Labels, "&") + ")"
}
