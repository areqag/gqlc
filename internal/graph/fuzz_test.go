package graph_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/areqag/gqlc/internal/fuzzkit"
	"github.com/areqag/gqlc/internal/graph"
)

// scalars is every scalar constant the package declares, ANY among them: the
// leaves a generated tree draws from.
var scalars = []graph.PropertyType{
	graph.TypeString, graph.TypeBytes, graph.TypeBool, graph.TypeDate, graph.TypeTime,
	graph.TypeLocalTime, graph.TypeTimestamp, graph.TypeDuration,
	graph.TypeInt, graph.TypeInt8, graph.TypeInt16, graph.TypeInt32, graph.TypeInt64, graph.TypeInt128, graph.TypeInt256,
	graph.TypeUint, graph.TypeUint8, graph.TypeUint16, graph.TypeUint32, graph.TypeUint64, graph.TypeUint128, graph.TypeUint256,
	graph.TypeFloat, graph.TypeFloat16, graph.TypeFloat32, graph.TypeFloat64, graph.TypeFloat128, graph.TypeFloat256,
	graph.TypeDecimal, graph.TypeUUID, graph.TypeAnyPropertyValue,
}

// fieldNames are names the encoding has to quote or could mistake for its own
// structure; Ident draws the rest from fieldAlphabet.
var (
	fieldNames    = []string{"", "a", "b", "ANY", "NOT NULL", " NOT NULL", "a NOT NULL", "x>", "<", "a,b", "a|b", "`", "``", "a`b", "LIST<INT>"}
	fieldAlphabet = "abZ_9 ,|<>`"
)

// coordinates are the vector elements ADR 0051 admits (Cypher 25's coordinate
// types), written out here rather than read from graph.VectorElements.
var coordinates = []graph.PropertyType{
	graph.TypeInt8, graph.TypeInt16, graph.TypeInt32, graph.TypeInt64, graph.TypeFloat32, graph.TypeFloat64,
}

// vectorFold is VectorOf's documented widening of INT and FLOAT.
var vectorFold = map[graph.PropertyType]graph.PropertyType{graph.TypeInt: graph.TypeInt64, graph.TypeFloat: graph.TypeFloat64}

const (
	maxDepth = 4
	maxWidth = 4
)

// A node is one constructor call in a generated tree.
type node struct {
	kind    graph.PropertyTypeKind
	scalar  graph.PropertyType // KindScalar, and KindVector's element
	elem    *node              // KindList
	notNull bool               // KindList: the element's qualifier
	fields  []field            // KindRecord, names unique
	members []member           // KindUnion
}

type field struct {
	name    string
	n       *node
	notNull bool
}

type member struct {
	n       *node
	notNull bool
}

func gen(s *fuzzkit.Stream, depth int) *node {
	k := graph.KindScalar
	if depth < maxDepth {
		k = graph.PropertyTypeKind(s.Int(5))
	}
	n := &node{kind: k}
	switch k {
	case graph.KindScalar, graph.KindVector:
		n.scalar = scalars[s.Int(len(scalars))]
	case graph.KindList:
		n.elem, n.notNull = gen(s, depth+1), s.Bool()
	case graph.KindRecord:
		for range s.Int(maxWidth + 1) {
			name := fieldNames[s.Int(len(fieldNames))]
			if s.Bool() {
				name = s.Ident(fieldAlphabet, 4)
			}
			// RecordOf's precondition: the parser refuses a duplicate name
			// before it reaches the constructor.
			if slices.ContainsFunc(n.fields, func(f field) bool { return f.name == name }) {
				continue
			}
			n.fields = append(n.fields, field{name: name, n: gen(s, depth+1), notNull: s.Bool()})
		}
	case graph.KindUnion:
		for range s.Int(maxWidth + 1) {
			n.members = append(n.members, member{n: gen(s, depth+1), notNull: s.Bool()})
		}
	}
	return n
}

// build constructs n through the package's constructors. A non-nil perm
// reorders every record's fields and every union's members first, so two
// builds of one tree differ only in the order the constructors are handed.
// A vector VectorOf refuses stands in as its element, so the tree stays
// buildable; check holds the refusal itself.
func build(n *node, perm *fuzzkit.Stream) graph.PropertyType {
	switch n.kind {
	case graph.KindList:
		return graph.ListOf(build(n.elem, perm), n.notNull)
	case graph.KindRecord:
		fs := make([]graph.RecordField, 0, len(n.fields))
		for _, f := range n.fields {
			fs = append(fs, graph.RecordField{Name: f.name, Type: build(f.n, perm), NotNull: f.notNull})
		}
		shuffle(fs, perm)
		return graph.RecordOf(fs)
	case graph.KindUnion:
		ms := make([]graph.UnionMember, 0, len(n.members))
		for _, m := range n.members {
			ms = append(ms, graph.UnionMember{Type: build(m.n, perm), NotNull: m.notNull})
		}
		shuffle(ms, perm)
		return graph.UnionOf(ms)
	case graph.KindVector:
		if v, ok := graph.VectorOf(n.scalar); ok {
			return v
		}
	case graph.KindScalar:
	}
	return n.scalar
}

func shuffle[T any](xs []T, perm *fuzzkit.Stream) {
	if perm == nil {
		return
	}
	for i := len(xs) - 1; i > 0; i-- {
		j := perm.Int(i + 1)
		xs[i], xs[j] = xs[j], xs[i]
	}
}

// check reads pt back through Kind, Elem, ElemNotNull, Fields and Members and
// holds it against n, children first, so a parent's expectation may lean on a
// child's read-back that has already been checked. It returns the first
// violation, as "property <name>: ...".
func check(n *node, pt graph.PropertyType) string {
	k := pt.Kind()
	if k == graph.KindScalar && strings.Contains(string(pt), "<") {
		return fmt.Sprintf("property scalar-no-angle: %q reads as KindScalar", pt)
	}
	if k != graph.KindRecord && pt.Fields() != nil {
		return fmt.Sprintf("property fields-off-family: %q (kind %d) has fields %v", pt, k, pt.Fields())
	}
	if k != graph.KindUnion && pt.Members() != nil {
		return fmt.Sprintf("property members-off-family: %q (kind %d) has members %v", pt, k, pt.Members())
	}
	switch n.kind {
	case graph.KindScalar:
		if pt != n.scalar || k != graph.KindScalar {
			return fmt.Sprintf("property read-back: scalar %q built as %q (kind %d)", n.scalar, pt, k)
		}
	case graph.KindVector:
		elem := n.scalar
		if wide, ok := vectorFold[elem]; ok {
			elem = wide
		}
		admitted := slices.Contains(coordinates, elem)
		if _, ok := graph.VectorOf(n.scalar); ok != admitted {
			return fmt.Sprintf("property vector-admission: VectorOf(%q) answered %t", n.scalar, ok)
		}
		if !admitted {
			if pt != n.scalar {
				return fmt.Sprintf("property read-back: refused vector of %q built as %q", n.scalar, pt)
			}
			return ""
		}
		if k != graph.KindVector || pt.Elem() != elem {
			return fmt.Sprintf("property read-back: vector of %q built as %q, Elem %q", n.scalar, pt, pt.Elem())
		}
	case graph.KindList:
		elem := build(n.elem, nil)
		if msg := check(n.elem, elem); msg != "" {
			return msg
		}
		if k != graph.KindList || pt.Elem() != elem || pt.ElemNotNull() != n.notNull {
			return fmt.Sprintf("property read-back: list of %q (not null %t) built as %q, read back Elem %q ElemNotNull %t",
				elem, n.notNull, pt, pt.Elem(), pt.ElemNotNull())
		}
	case graph.KindRecord:
		return checkRecord(n, pt)
	case graph.KindUnion:
		return checkUnion(n, pt)
	}
	return ""
}

func checkRecord(n *node, pt graph.PropertyType) string {
	want := make([]graph.RecordField, 0, len(n.fields))
	for _, f := range n.fields {
		ft := build(f.n, nil)
		if msg := check(f.n, ft); msg != "" {
			return msg
		}
		want = append(want, graph.RecordField{Name: f.name, Type: ft, NotNull: f.notNull})
	}
	slices.SortFunc(want, func(a, b graph.RecordField) int { return strings.Compare(a.Name, b.Name) })
	if len(want) == 0 {
		want = nil
	}
	if pt.Kind() != graph.KindRecord || !slices.Equal(pt.Fields(), want) {
		return fmt.Sprintf("property read-back: record built as %q reads back fields %#v, want %#v", pt, pt.Fields(), want)
	}
	return ""
}

// checkUnion holds the reductions UnionOf documents (ADR 0039): unqualified
// nested unions flatten, an unqualified ANY absorbs the union, duplicates
// collapse, members sort by encoded spelling, and a lone unqualified member is
// that member's own type.
func checkUnion(n *node, pt graph.PropertyType) string {
	var flat []graph.UnionMember
	for _, m := range n.members {
		mt := build(m.n, nil)
		if msg := check(m.n, mt); msg != "" {
			return msg
		}
		if mt.Kind() == graph.KindUnion && !m.notNull {
			flat = append(flat, mt.Members()...)
			continue
		}
		flat = append(flat, graph.UnionMember{Type: mt, NotNull: m.notNull})
	}
	if slices.Contains(flat, graph.UnionMember{Type: graph.TypeAnyPropertyValue}) {
		if pt != graph.TypeAnyPropertyValue {
			return fmt.Sprintf("property read-back: union with an unqualified ANY built as %q", pt)
		}
		return ""
	}
	spell := func(m graph.UnionMember) string {
		if m.NotNull {
			return string(m.Type) + " NOT NULL"
		}
		return string(m.Type)
	}
	slices.SortFunc(flat, func(a, b graph.UnionMember) int { return strings.Compare(spell(a), spell(b)) })
	flat = slices.Compact(flat)
	if len(flat) == 1 && !flat[0].NotNull {
		if pt != flat[0].Type {
			return fmt.Sprintf("property read-back: union of the lone member %q built as %q", flat[0].Type, pt)
		}
		return ""
	}
	if len(flat) == 0 {
		flat = nil
	}
	if pt.Kind() != graph.KindUnion || !slices.Equal(pt.Members(), flat) {
		return fmt.Sprintf("property read-back: union built as %q reads back members %#v, want %#v", pt, pt.Members(), flat)
	}
	return ""
}

// FuzzPropertyType is target T6 of docs/specs/nightly-fuzzing-plan.md. The
// input steers a random tree of scalar, ListOf, RecordOf, UnionOf and VectorOf
// calls (record field names unique, RecordOf's stated precondition). It checks:
//
//   - read-back: Kind, Elem, ElemNotNull, Fields and Members recover what was
//     built, record fields in name order and union members reduced as UnionOf
//     documents;
//   - canonicalisation: the same tree built with every record's fields and
//     every union's members in another order is == to the first build, which
//     is what lets the resolver unify on ==;
//   - a KindScalar type never contains "<", and Fields and Members are nil off
//     their family;
//   - determinism: two builds in the same order are ==.
func FuzzPropertyType(f *testing.F) {
	f.Add([]byte{})
	// UNION<LIST<BYTES NOT NULL>|RECORD<a STRING,b INT NOT NULL> NOT NULL|VECTOR<INT64>>,
	// the record's fields handed over as b, a.
	f.Add([]byte{3, 3, 1, 0, 1, 1, 0, 2, 2, 2, 0, 0, 8, 1, 1, 0, 0, 0, 0, 1, 4, 8, 0})
	// A record of three fields: "x>" holding UNION<BOOL|STRING>, the quoted
	// "` |" holding a union whose unqualified nested union flattens, and ANY
	// holding LIST<ANY>.
	f.Add([]byte{2, 3, 7, 0, 3, 2, 0, 0, 0, 0, 2, 0, 0, 0, 1, 2, 10, 5, 7, 3, 2, 3, 2, 0, 0, 0, 0, 8, 0, 0, 0, 0, 1, 1, 3, 0, 1, 0, 30, 0, 0})
	// A union that an unqualified ANY absorbs.
	f.Add([]byte{3, 2, 0, 30, 0, 0, 1, 1})
	f.Fuzz(func(t *testing.T, b []byte) {
		fuzzkit.Skip(t, b)
		fuzzkit.Guard(t, 2*time.Second, func() {
			tree := gen(fuzzkit.NewStream(b), 0)
			first, second := build(tree, nil), build(tree, nil)
			if first != second {
				t.Fatalf("property determinism: one tree built as %q and %q", first, second)
			}
			// The permutation is read from the input backwards, so it is
			// steered by the same bytes but not the same decisions.
			rev := slices.Clone(b)
			slices.Reverse(rev)
			if permuted := build(tree, fuzzkit.NewStream(rev)); permuted != first {
				t.Fatalf("property canonicalisation: one tree built as %q and, reordered, as %q", first, permuted)
			}
			if msg := check(tree, first); msg != "" {
				t.Fatal(msg)
			}
		})
	})
}
