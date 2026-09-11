package resolver

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/areqag/gqlc/internal/graph"
)

// This file holds orderableSelection's verdict for EVERY property type
// internal/graph declares, and it exists because the resolver corpus cannot.
//
// The corpus reaches exactly two of this table's answers — one orderable family
// (INT32, via certified_min_width_preserved) and one refusal (ANY, via
// certified_min_unorderable_stays_any) — and a corpus row is expensive: it needs
// a schema property, and that schema has to stay codegen-admissible on BOTH
// backends or TestResolverValidCorpusStageBoundary reds. BYTES, the refusal spec
// ruling-p9qgu §3.1 actually names, cannot be a corpus row at all: AGE refuses a
// BYTES property at SCHEMA admission ("BYTES has no agtype scalar at all",
// age/types.go), so the fixture would be rejected before its query was looked at
// and would witness nothing about min.
//
// That is the gap the ruling predicted. §5's mutation row 4 — "admit every
// graph.PropertyType in the orderable table" — is called out there as the row
// most likely to come back SURVIVED, because the table has many arms and one
// fixture blinds one of them, with the remedy stated as "add the row per family
// or say which families ship unwitnessed". This is that remedy taken in the
// first form: every arm is named here, so no family ships unwitnessed and the
// blanket mutation is killed by whichever rows it moves rather than by luck
// about which one fixture happened to cover.
func TestOrderableSelectionNamesEveryPropertyType(t *testing.T) {
	// want is the verdict for every constant propertytype.go declares. A
	// selection preserves a family iff that family carries a total order the
	// aggregate's result stays inside.
	want := map[graph.PropertyType]bool{
		// Scalars with a total order.
		graph.TypeString: true,
		graph.TypeBool:   true,

		// The temporal families. Driver-neutral carriers (ADR 0033), and
		// ordered by construction.
		graph.TypeDate:      true,
		graph.TypeTime:      true,
		graph.TypeLocalTime: true,
		graph.TypeTimestamp: true,
		graph.TypeDuration:  true,

		// Every integer width, signed and unsigned. The widths are listed one
		// by one rather than folded, because the whole reason this predicate
		// ranges over graph.PropertyType instead of calling the parser's
		// width-free table is that the widths are distinguishable here
		// (ADR 0002) — a test that could not tell INT32 from INT64 would not
		// be testing the thing that made this a separate table.
		graph.TypeInt:    true,
		graph.TypeInt8:   true,
		graph.TypeInt16:  true,
		graph.TypeInt32:  true,
		graph.TypeInt64:  true,
		graph.TypeInt128: true,
		graph.TypeInt256: true,

		graph.TypeUint:    true,
		graph.TypeUint8:   true,
		graph.TypeUint16:  true,
		graph.TypeUint32:  true,
		graph.TypeUint64:  true,
		graph.TypeUint128: true,
		graph.TypeUint256: true,

		graph.TypeFloat:    true,
		graph.TypeFloat16:  true,
		graph.TypeFloat32:  true,
		graph.TypeFloat64:  true,
		graph.TypeFloat128: true,
		graph.TypeFloat256: true,

		// BYTES — the ruling's named refusal (§3.1), and the row this file was
		// written to be able to make at all.
		graph.TypeBytes: false,

		// DECIMAL is refused not because it lacks an order but because the
		// ruling does not enumerate it. Refusing degrades min(p.d) to any,
		// which costs a column its type and cannot be WRONG; admitting it
		// would be a decision nobody has made. If a later bead wants it, this
		// row is where the decision gets recorded.
		graph.TypeDecimal: false,

		// UUID is refused for DECIMAL's reason and not for BYTES': it carries
		// a total byte order, so this row says "the ruling does not enumerate
		// it", not "it is not ordered". ruling-p9qgu predates the constant —
		// gqlc-eg4b added it in PR #2842, after the ruling was written — so
		// there was no enumeration for it to be left out of, and the
		// least/greatest of a set of opaque identifiers is not a question
		// anyone has asked. Refusing degrades min(p.u) to any, which costs a
		// column its type and cannot be WRONG. If a later bead wants it
		// orderable, this row is where the decision gets recorded.
		//
		// This row exists because master went red without either PR being
		// wrong: #2842 added the constant and #2838 added this census, and
		// neither merge ref contained the other. That is a semantic conflict
		// no text-level merge can surface, and this table is the thing that
		// caught it — which is the case for keeping it exhaustive.
		graph.TypeUUID: false,

		// The open/composite families. Ordering an ANY is engine-dependent at
		// best — it holds whatever the writer wrote — and "can min/max over a
		// list be typed" is a question the ruling explicitly does not open
		// (§7); the depth-0 mint condition keeps it closed on the parser side
		// and this keeps it closed on ours, for an operand that is a declared
		// list PROPERTY rather than a list literal.
		graph.TypeAnyPropertyValue: false,
		graph.TypeList:             false,
		graph.TypeAnyRecord:        false,
	}

	for _, name := range declaredPropertyTypeConsts(t) {
		t.Run(name.ident, func(t *testing.T) {
			verdict, named := want[name.value]
			require.Truef(t, named,
				"graph.%s is declared in propertytype.go and this table does not name it. A property type nobody assigned a verdict to is admitted or refused by whichever way orderableSelection's allow-list happens to fall, which is not a decision anyone took: add the row",
				name.ident)
			require.Equalf(t, verdict, orderableSelection(name.value),
				"graph.%s: orderableSelection disagrees with the verdict this file records", name.ident)
		})
	}

	t.Run("the table names nothing that is not declared", func(t *testing.T) {
		declared := map[graph.PropertyType]struct{}{}
		for _, c := range declaredPropertyTypeConsts(t) {
			declared[c.value] = struct{}{}
		}
		var stale []string
		for pt := range want {
			if _, ok := declared[pt]; !ok {
				stale = append(stale, string(pt))
			}
		}
		sort.Strings(stale)
		require.Emptyf(t, stale, "this table holds verdicts for property types propertytype.go no longer declares, so those rows assert nothing: %v", stale)
	})
}

type propertyTypeConst struct {
	ident string
	value graph.PropertyType
}

// declaredPropertyTypeConsts reads the PropertyType constants out of
// propertytype.go's source rather than taking a list written here.
//
// That is the half that makes this file a gate instead of a second opinion.
// graph.PropertyType is a STRING type with an open composite grammar
// (LIST<INT32>, RECORD{...}), not a closed enum, so `exhaustive` cannot hold
// orderableSelection's switch the way it holds the parser's — there is no
// constant set for it to check against. A hand-written list here would go stale
// silently on the day someone adds a width, and the new width would be refused
// by default with nobody having said so.
func declaredPropertyTypeConsts(t *testing.T) []propertyTypeConst {
	t.Helper()

	path := filepath.Join("..", "graph", "propertytype.go")
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, 0)
	require.NoError(t, err, "propertytype.go is the artefact this file reads its constant set from; without it there is no claim to check")

	var out []propertyTypeConst
	for _, decl := range f.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.CONST {
			continue
		}
		for _, spec := range gd.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			// Only `Name PropertyType = "..."` — an untyped const in the same
			// block is not one of these.
			id, ok := vs.Type.(*ast.Ident)
			if !ok || id.Name != "PropertyType" {
				continue
			}
			for i, n := range vs.Names {
				if i >= len(vs.Values) {
					continue
				}
				lit, ok := vs.Values[i].(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					continue
				}
				out = append(out, propertyTypeConst{
					ident: n.Name,
					value: graph.PropertyType(strings.Trim(lit.Value, `"`)),
				})
			}
		}
	}

	// A reader that silently found nothing would make every row above vacuous
	// and the suite would pass by asserting about an empty set.
	require.NotEmptyf(t, out, "read no PropertyType constants out of %s, so every row in this file would assert nothing", path)
	return out
}
