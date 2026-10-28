package gen

import (
	"fmt"
	"strings"

	"github.com/areqag/gqlc/internal/fuzzkit"
)

// literals holds a Cypher literal for the scalar spellings that have one
// whose type the resolver reads without a parameter. A property whose type
// is not here is compared against a parameter instead.
var literals = map[string][]string{
	"INT64": {"42", "0", "-7"}, "INT32": {"42", "1"}, "INT": {"42"}, "INT8": {"3"}, "INT16": {"300"},
	"SMALLINT": {"5"}, "BIGINT": {"99"},
	"UINT8": {"7"}, "UINT16": {"7"}, "UINT32": {"7"}, "UINT64": {"7"}, "UINT": {"7"},
	"FLOAT64": {"1.5", "0.0"}, "FLOAT32": {"2.5"}, "FLOAT": {"1.5"}, "REAL": {"1.5"}, "DOUBLE": {"1.5"},
	"STRING": {"'x'", "''"}, "VARCHAR(64)": {"'y'"},
	"BOOL": {"true", "false"}, "BOOLEAN": {"true"},
}

// numeric is the spellings arithmetic and sum/avg are drawn over.
var numeric = map[string]bool{
	"INT64": true, "INT32": true, "INT": true, "INT8": true, "INT16": true, "SMALLINT": true, "BIGINT": true,
	"UINT8": true, "UINT16": true, "UINT32": true, "UINT64": true, "UINT": true,
	"FLOAT64": true, "FLOAT32": true, "FLOAT": true, "REAL": true, "DOUBLE": true,
}

// binding is one name in scope: an entity variable (node or edge index into
// the Vocab) or a scalar alias a WITH projected.
type binding struct {
	name string
	node int // index into Vocab.Nodes, or -1
	edge int // index into Vocab.Edges, or -1
	// varLen marks an edge bound by a variable-length hop, whose properties
	// are not projectable.
	varLen bool
}

type qgen struct {
	s      *fuzzkit.Stream
	v      Vocab
	b      strings.Builder
	scope  []binding
	nVar   int
	nParam int
	nAlias int
}

// Query writes one Cypher query over v.
//
// Its shape is a chain of 1–3 parts. Each part reads with MATCH or OPTIONAL
// MATCH over a pattern of 0–3 hops along v's declared edges (variable-length
// at a low rate), filters with WHERE over typed properties compared to
// parameters or literals, and ends in WITH (aliases, DISTINCT, aggregates,
// ORDER BY, SKIP/LIMIT) or, in the last part, RETURN with entity refs,
// aliased properties, expressions and aggregates. UNWIND, a write arm
// (CREATE / SET / DELETE / DETACH DELETE) and a UNION of two branches each
// appear at a low rate.
//
// Every identifier comes from v, so a query fails to resolve on purpose
// rather than by accident. The controlled mutations, each at a low rate: a
// label v does not declare, a property its owner does not declare, a hop
// written against its declared direction, an untyped edge, and one parameter
// compared against two properties.
func Query(s *fuzzkit.Stream, v Vocab) string {
	g := &qgen{s: s, v: v}
	g.branch()
	first := strings.TrimSpace(g.b.String())
	if !rare(s, 12) {
		return first
	}
	op := " UNION "
	if s.Bool() {
		op = " UNION ALL "
	}
	if !rare(s, 4) {
		return first + op + first // the same branch twice, so the columns agree
	}
	// An independent branch, whose columns rarely agree with the first's.
	g.b.Reset()
	g.scope, g.nVar, g.nAlias = nil, 0, 0
	g.branch()
	return first + op + strings.TrimSpace(g.b.String())
}

func (g *qgen) w(format string, args ...any) {
	fmt.Fprintf(&g.b, format, args...)
}

func (g *qgen) branch() {
	parts := 1 + g.s.Int(3)
	for i := range parts {
		last := i == parts-1
		g.reads(i == 0)
		if last {
			if rare(g.s, 8) {
				g.write()
				if g.s.Bool() {
					return // a write-only query: no RETURN
				}
			}
			g.ret()
		} else {
			g.with()
		}
	}
}

// reads writes the part's reading clauses: one or two MATCH / OPTIONAL MATCH
// clauses, each with an optional WHERE, and UNWIND at a low rate.
func (g *qgen) reads(first bool) {
	n := 1 + g.s.Int(2)
	if !first && g.s.Bool() {
		n = g.s.Int(2) // a later part may only read what it carried
	}
	for range n {
		if rare(g.s, 5) {
			g.w(" OPTIONAL MATCH ")
		} else {
			g.w(" MATCH ")
		}
		g.pattern()
		if g.s.Int(3) == 1 {
			g.where()
		}
	}
	if rare(g.s, 60) {
		name := g.fresh("u")
		g.w(" UNWIND [1, 2, 3] AS %s", name)
		g.scope = append(g.scope, binding{name: name, node: -1, edge: -1})
	} else if n == 0 && len(g.entities()) == 0 {
		g.w(" MATCH ")
		g.pattern()
	}
}

func (g *qgen) fresh(prefix string) string {
	g.nVar++
	return fmt.Sprintf("%s%d", prefix, g.nVar-1)
}

// nodeRef writes one node of a pattern: a carried variable of type ni when
// one is in scope and the stream says so, else a fresh labelled one.
func (g *qgen) nodeRef(ni int) {
	if ni < 0 || ni >= len(g.v.Nodes) {
		name := g.fresh("n")
		g.w("(%s:Ghost)", name)
		return
	}
	for _, b := range g.scope {
		if b.node == ni && g.s.Int(3) == 1 {
			g.w("(%s)", b.name)
			return
		}
	}
	name := g.fresh("n")
	labels := g.v.Nodes[ni].Labels
	if rare(g.s, 80) {
		labels = []string{"Ghost"} // mutation: a label v does not declare
	}
	g.w("(%s:%s", name, strings.Join(labels, ":"))
	if rare(g.s, 10) {
		if p, ok := g.pickProp(g.v.Nodes[ni].Props); ok {
			g.w(" {%s: %s}", p.Name, g.param())
		}
	}
	g.w(")")
	g.scope = append(g.scope, binding{name: name, node: ni, edge: -1})
}

func (g *qgen) pattern() {
	if len(g.v.Nodes) == 0 {
		g.nodeRef(-1)
		return
	}
	cur := g.s.Int(len(g.v.Nodes))
	g.nodeRef(cur)
	for range g.s.Int(4) {
		if cur = g.hop(cur); cur < 0 {
			return
		}
	}
}

// hop writes one relationship leaving or entering node type cur along a
// declared edge, and the node at its far end, and returns that node's type;
// -1 when no declared edge touches cur.
func (g *qgen) hop(cur int) int {
	var cands []int
	for i, e := range g.v.Edges {
		if e.From == cur || e.To == cur {
			cands = append(cands, i)
		}
	}
	if len(cands) == 0 {
		return -1
	}
	ei := cands[g.s.Int(len(cands))]
	e := g.v.Edges[ei]
	other, forward := e.To, true
	switch {
	case e.From == cur && e.To == cur:
		forward = g.s.Bool()
	case e.To == cur:
		other, forward = e.From, false
	}
	if rare(g.s, 40) {
		forward = !forward // mutation: against the declared direction
	}
	body := g.relBody(ei)
	if forward {
		g.w("-[%s]->", body)
	} else {
		g.w("<-[%s]-", body)
	}
	g.nodeRef(other)
	return other
}

// relBody writes the inside of a relationship pattern over edge type ei: an
// optional fresh variable, the type, a variable-length range at a low rate,
// and an inline property parameter at a low rate.
func (g *qgen) relBody(ei int) string {
	e := g.v.Edges[ei]
	rel := ":" + e.Type
	if rare(g.s, 150) {
		rel = "" // mutation: an untyped edge
	}
	varLen := ""
	if rare(g.s, 10) {
		varLen = g.s.Pick([]string{"*1..3", "*", "*2", "*0..2"})
	}
	if g.s.Int(3) == 2 {
		return rel + varLen // anonymous
	}
	name := g.fresh("r")
	g.scope = append(g.scope, binding{name: name, node: -1, edge: ei, varLen: varLen != ""})
	props := ""
	if varLen == "" && rare(g.s, 12) {
		if p, ok := g.pickProp(e.Props); ok {
			props = fmt.Sprintf(" {%s: %s}", p.Name, g.param())
		}
	}
	return name + rel + varLen + props
}

func (g *qgen) pickProp(props []Prop) (Prop, bool) {
	if len(props) == 0 {
		return Prop{}, false
	}
	p := props[g.s.Int(len(props))]
	if rare(g.s, 80) {
		p.Name = "ghost" // mutation: a property its owner does not declare
	}
	return p, true
}

func (g *qgen) param() string {
	if g.nParam > 0 && rare(g.s, 30) {
		return fmt.Sprintf("$p%d", g.s.Int(g.nParam)) // mutation: one parameter, two uses
	}
	g.nParam++
	return fmt.Sprintf("$p%d", g.nParam-1)
}

// entities is the in-scope entity bindings whose properties can be read.
func (g *qgen) entities() []binding {
	var out []binding
	for _, b := range g.scope {
		if (b.node >= 0 || b.edge >= 0) && !b.varLen {
			out = append(out, b)
		}
	}
	return out
}

func (g *qgen) propsOf(b binding) []Prop {
	if b.node >= 0 {
		return g.v.Nodes[b.node].Props
	}
	if b.edge >= 0 {
		return g.v.Edges[b.edge].Props
	}
	return nil
}

// propRef draws an in-scope entity and one of its properties.
func (g *qgen) propRef() (binding, Prop, bool) {
	ents := g.entities()
	var withProps []binding
	for _, b := range ents {
		if len(g.propsOf(b)) > 0 {
			withProps = append(withProps, b)
		}
	}
	if len(withProps) == 0 {
		return binding{}, Prop{}, false
	}
	b := withProps[g.s.Int(len(withProps))]
	p, _ := g.pickProp(g.propsOf(b))
	return b, p, true
}

func (g *qgen) where() {
	for i := range 1 + g.s.Int(2) {
		b, p, ok := g.propRef()
		if !ok {
			return
		}
		if i == 0 {
			g.w(" WHERE ")
		} else {
			g.w("%s", g.s.Pick([]string{" AND ", " OR "}))
		}
		g.w("%s", g.predicate(b.name+"."+p.Name, p.Type))
	}
}

// predicate compares the property ref, of the spelled type typ, against a
// parameter or a literal of its type, or tests it for null.
func (g *qgen) predicate(ref, typ string) string {
	switch g.s.Int(5) {
	case 0, 1:
		return ref + " = " + g.param()
	case 2:
		lits := literals[typ]
		if len(lits) == 0 {
			return ref + " = " + g.param()
		}
		op := "="
		if numeric[typ] {
			op = g.s.Pick([]string{"=", "<", ">=", "<>"})
		}
		return ref + " " + op + " " + g.s.Pick(lits)
	case 3:
		return ref + " IS NULL"
	default:
		return ref + " IS NOT NULL"
	}
}

// item is one projection: its text and the column name it introduces.
type item struct {
	text, col string
	entity    binding
	isEntity  bool
}

// items draws 1–4 projection items over the scope. Column names are unique by
// construction: entity refs keep their variable's name and every other item
// is aliased to a fresh name.
func (g *qgen) items(allowAgg bool) []item {
	n := 1 + g.s.Int(4)
	var out []item
	used := map[string]bool{}
	for range n {
		it, ok := g.item(allowAgg)
		if !ok || used[it.col] {
			continue
		}
		used[it.col] = true
		out = append(out, it)
	}
	if len(out) == 0 {
		out = append(out, g.aliased("1"))
	}
	return out
}

func (g *qgen) alias() string {
	g.nAlias++
	return fmt.Sprintf("c%d", g.nAlias-1)
}

func (g *qgen) item(allowAgg bool) (item, bool) {
	switch k := g.s.Int(8); {
	case k <= 1:
		// an entity, or a scalar alias, by name
		if len(g.scope) == 0 {
			return item{}, false
		}
		b := g.scope[g.s.Int(len(g.scope))]
		return item{text: b.name, col: b.name, entity: b, isEntity: b.node >= 0 || b.edge >= 0}, true
	case k <= 4:
		b, p, ok := g.propRef()
		if !ok {
			return item{}, false
		}
		return g.aliased(b.name + "." + p.Name), true
	case k == 5:
		return g.exprItem()
	case allowAgg:
		return g.aggItem()
	default:
		return g.aliased(g.s.Pick([]string{"1", "'s'", "true", "null", "[1, 2]"})), true
	}
}

// aliased is the item `expr AS c<n>` under a fresh column name.
func (g *qgen) aliased(expr string) item {
	a := g.alias()
	return item{text: expr + " AS " + a, col: a}
}

// exprItem is an expression over one property: arithmetic on a numeric one,
// otherwise toString or a one-element list.
func (g *qgen) exprItem() (item, bool) {
	b, p, ok := g.propRef()
	if !ok {
		return item{}, false
	}
	ref := b.name + "." + p.Name
	switch {
	case numeric[p.Type]:
		return g.aliased(ref + " + 1"), true
	case g.s.Bool():
		return g.aliased("toString(" + ref + ")"), true
	default:
		return g.aliased("[" + ref + "]"), true
	}
}

// aggItem is an aggregate: count(*), count of an entity, collect of a
// property, or min / max / sum / avg over one, sum and avg only over a
// numeric one.
func (g *qgen) aggItem() (item, bool) {
	switch g.s.Int(5) {
	case 0:
		return g.aliased("count(*)"), true
	case 1:
		ents := g.entities()
		if len(ents) == 0 {
			return g.aliased("count(*)"), true
		}
		return g.aliased("count(" + ents[g.s.Int(len(ents))].name + ")"), true
	case 2:
		b, p, ok := g.propRef()
		if !ok {
			return item{}, false
		}
		return g.aliased("collect(" + b.name + "." + p.Name + ")"), true
	default:
		b, p, ok := g.propRef()
		if !ok {
			return item{}, false
		}
		fn := g.s.Pick([]string{"min", "max", "count"})
		if numeric[p.Type] {
			fn = g.s.Pick([]string{"sum", "avg", "min", "max"})
		}
		return g.aliased(fn + "(" + b.name + "." + p.Name + ")"), true
	}
}

// with writes a WITH that ends a non-final part and resets the scope to what
// it projected.
func (g *qgen) with() {
	if g.s.Int(3) == 0 && len(g.scope) > 0 {
		g.w(" WITH *")
		return
	}
	g.w(" WITH ")
	if rare(g.s, 6) {
		g.w("DISTINCT ")
	}
	its := g.items(g.s.Int(3) == 2)
	// An entity carried keeps its type; everything else becomes a scalar
	// alias, whose properties the next part cannot read.
	var next []binding
	texts := make([]string, len(its))
	for i, it := range its {
		texts[i] = it.text
		if it.isEntity {
			next = append(next, it.entity)
		} else {
			next = append(next, binding{name: it.col, node: -1, edge: -1})
		}
	}
	g.w("%s", strings.Join(texts, ", "))
	g.order(its)
	g.scope = next
	if g.s.Int(4) == 1 {
		g.where()
	}
}

func (g *qgen) ret() {
	g.w(" RETURN ")
	if rare(g.s, 8) {
		g.w("DISTINCT ")
	}
	its := g.items(g.s.Int(3) == 2)
	texts := make([]string, len(its))
	for i, it := range its {
		texts[i] = it.text
	}
	g.w("%s", strings.Join(texts, ", "))
	g.order(its)
}

// order writes ORDER BY over projected columns and SKIP / LIMIT, each at a
// modest rate.
func (g *qgen) order(its []item) {
	if g.s.Int(4) == 1 {
		var cols []string
		for _, it := range its {
			if !it.isEntity {
				cols = append(cols, it.col)
			}
		}
		if len(cols) > 0 {
			g.w(" ORDER BY %s", g.s.Pick(cols))
			if g.s.Bool() {
				g.w(" DESC")
			}
		}
	}
	if g.s.Int(5) == 1 {
		g.w(" SKIP %s", g.slot())
	}
	if g.s.Int(4) == 1 {
		g.w(" LIMIT %s", g.slot())
	}
}

func (g *qgen) slot() string {
	if rare(g.s, 4) {
		return g.param()
	}
	return fmt.Sprintf("%d", 1+g.s.Int(10))
}

// write writes one write clause after the reads: CREATE of a fresh node, SET
// of a property or a label, DELETE or DETACH DELETE. An arm with nothing in
// scope to act on writes CREATE () instead.
func (g *qgen) write() {
	g.w(" ")
	var wrote bool
	switch g.s.Int(4) {
	case 0:
		wrote = g.create()
	case 1:
		if b, p, ok := g.propRef(); ok {
			g.w("SET %s.%s = %s", b.name, p.Name, g.param())
			wrote = true
		}
	case 2:
		wrote = g.setLabel()
	default:
		wrote = g.del()
	}
	if !wrote {
		g.w("CREATE ()")
	}
}

func (g *qgen) create() bool {
	if len(g.v.Nodes) == 0 {
		return false
	}
	ni := g.s.Int(len(g.v.Nodes))
	name := g.fresh("n")
	g.w("CREATE (%s:%s)", name, strings.Join(g.v.Nodes[ni].Labels, ":"))
	g.scope = append(g.scope, binding{name: name, node: ni, edge: -1})
	return true
}

func (g *qgen) setLabel() bool {
	for _, b := range g.entities() {
		if b.node >= 0 && len(g.v.Nodes[b.node].Labels) > 0 && g.s.Bool() {
			g.w("SET %s:%s", b.name, g.v.Nodes[b.node].Labels[0])
			return true
		}
	}
	return false
}

func (g *qgen) del() bool {
	ents := g.entities()
	if len(ents) == 0 {
		return false
	}
	b := ents[g.s.Int(len(ents))]
	if b.node >= 0 && g.s.Bool() {
		g.w("DETACH DELETE %s", b.name)
	} else {
		g.w("DELETE %s", b.name)
	}
	return true
}
