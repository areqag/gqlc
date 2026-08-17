// This file is an external test package on purpose: the claim under test is
// about what a caller OUTSIDE internal/query can construct. Inside the package
// every marker method is writable, so an in-package witness would measure
// nothing.
package query_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/areqag/gqlc/internal/query"
)

// Each embedder promotes exactly one variant's methods, the unexported marker
// included, and declares no method of its own. The compile-time assignments
// the forms constructor performs are the first half of the measurement — this
// file failing to build is itself the claim being falsified.
type (
	embedNodeBinding   struct{ query.NodeBinding }
	embedEdgeBinding   struct{ query.EdgeBinding }
	embedPathBinding   struct{ query.PathBinding }
	embedUnwindBinding struct{ query.UnwindBinding }
	embedCallBinding   struct{ query.CallBinding }

	embedNamedNodeMember struct{ query.NamedNodeMember }
	embedNamedEdgeMember struct{ query.NamedEdgeMember }
	embedAnonEdgeMember  struct{ query.AnonEdgeMember }
	embedAnonNodeMember  struct{ query.AnonNodeMember }

	embedVarEndpoint    struct{ query.VarEndpoint }
	embedInlineEndpoint struct{ query.InlineEndpoint }

	embedRefProjection       struct{ query.RefProjection }
	embedLiteralProjection   struct{ query.LiteralProjection }
	embedFuncProjection      struct{ query.FuncProjection }
	embedAggregateProjection struct{ query.AggregateProjection }
	embedExprProjection      struct{ query.ExprProjection }

	embedPropertyUse   struct{ query.PropertyUse }
	embedClauseSlotUse struct{ query.ClauseSlotUse }
	embedExprUse       struct{ query.ExprUse }

	embedCreateEffect         struct{ query.CreateEffect }
	embedMergeEffect          struct{ query.MergeEffect }
	embedDeleteEffect         struct{ query.DeleteEffect }
	embedSetPropertyEffect    struct{ query.SetPropertyEffect }
	embedSetEntityEffect      struct{ query.SetEntityEffect }
	embedSetLabelsEffect      struct{ query.SetLabelsEffect }
	embedRemovePropertyEffect struct{ query.RemovePropertyEffect }
	embedRemoveLabelsEffect   struct{ query.RemoveLabelsEffect }

	embedTypeBool          struct{ query.TypeBool }
	embedTypeInt           struct{ query.TypeInt }
	embedTypeFloat         struct{ query.TypeFloat }
	embedTypeString        struct{ query.TypeString }
	embedTypeNull          struct{ query.TypeNull }
	embedTypeMap           struct{ query.TypeMap }
	embedTypeNode          struct{ query.TypeNode }
	embedTypeEdge          struct{ query.TypeEdge }
	embedTypeList          struct{ query.TypeList }
	embedTypeUnknown       struct{ query.TypeUnknown }
	embedTypeDate          struct{ query.TypeDate }
	embedTypeTime          struct{ query.TypeTime }
	embedTypeLocalTime     struct{ query.TypeLocalTime }
	embedTypeDateTime      struct{ query.TypeDateTime }
	embedTypeLocalDateTime struct{ query.TypeLocalDateTime }
	embedTypeDuration      struct{ query.TypeDuration }
	embedTypePath          struct{ query.TypePath }
)

// inhabitant holds the three forms of one variant that satisfy its sum's
// interface. value is the form the switches under audit enumerate; pointer and
// embedded are the two an out-of-package caller reaches without declaring the
// marker.
type inhabitant struct {
	value    any
	pointer  any
	embedded any
}

// forms records one variant's three forms. The type argument is the sum's
// interface, so the compiler rejects a row whose pointer or embedding form
// does not satisfy it — that rejection, not a runtime assertion, is what
// proves the two constructions inhabit the sum. The fields are `any` because
// one table holds rows for eight different interfaces.
func forms[I any](value, pointer, embedded I) inhabitant {
	return inhabitant{value: value, pointer: pointer, embedded: embedded}
}

// armMatch is what one form measured against one sum reports: whether it
// satisfies the interface at all, and which arm of a value-form type switch it
// matched ("" for the default). Both halves are needed — a form that did not
// satisfy the interface would also match no arm, and reading only the arm
// would let that read as the claim being made.
type armMatch struct {
	satisfies bool
	arm       string
}

// matcher adapts one sum's interface-typed switch to the table, whose rows are
// `any` because one table holds eight interfaces. The assertion is the checked
// form: an unchecked one would turn a row that does not satisfy its interface
// into a panic instead of a named failure.
func matcher[I any](f func(I) string) func(any) armMatch {
	return func(v any) armMatch {
		iv, ok := v.(I)
		if !ok {
			return armMatch{}
		}
		return armMatch{satisfies: true, arm: f(iv)}
	}
}

// docPolicy says how a sum's interface doc comment states its membership. The
// zero value is not one of the two policies: a sum added to the table without
// choosing one fails the doc row rather than skipping it, which is the failure
// mode a plain bool would hide.
type docPolicy int

const (
	docPolicyUnset docPolicy = iota
	// docEnumeratesVariants: the doc names every declared variant and spells
	// the count. Both are compared against the package's own declarations.
	docEnumeratesVariants
	// docDescribesByStage: the doc introduces the variants by the stage that
	// landed them and states no count. The row holds it to stating none, so
	// adding a count later fails here rather than drifting unread.
	docDescribesByStage
)

// sealedSum is one interface in internal/query carrying an unexported marker.
type sealedSum struct {
	// iface is the interface's name, and the name of the subtest.
	iface string
	// marker is the unexported method the variants declare. The declared set
	// is read from the package sources by this name.
	marker string
	// match reports whether the argument satisfies iface and which arm of a
	// value-form type switch it matches.
	match func(any) armMatch
	// docs is how iface's doc comment states its membership.
	docs docPolicy
	// variants is keyed by the variant name the package declares.
	variants map[string]inhabitant
}

// matchBinding has the shape every audited Binding switch has: one arm per
// declared variant in its value form, and a default. It returns the name of
// the arm that matched, or "" for the default. The audited switches differ in
// what their arms do, not in which types they name, so what this reports about
// a given input holds for all of them. The same holds for the seven matchers
// below it.
func matchBinding(b query.Binding) string {
	switch b.(type) {
	case query.NodeBinding:
		return "NodeBinding"
	case query.EdgeBinding:
		return "EdgeBinding"
	case query.PathBinding:
		return "PathBinding"
	case query.UnwindBinding:
		return "UnwindBinding"
	case query.CallBinding:
		return "CallBinding"
	default:
		return ""
	}
}

func matchPathMember(m query.PathMember) string {
	switch m.(type) {
	case query.NamedNodeMember:
		return "NamedNodeMember"
	case query.NamedEdgeMember:
		return "NamedEdgeMember"
	case query.AnonEdgeMember:
		return "AnonEdgeMember"
	case query.AnonNodeMember:
		return "AnonNodeMember"
	default:
		return ""
	}
}

func matchEndpoint(e query.Endpoint) string {
	switch e.(type) {
	case query.VarEndpoint:
		return "VarEndpoint"
	case query.InlineEndpoint:
		return "InlineEndpoint"
	default:
		return ""
	}
}

func matchProjection(p query.Projection) string {
	switch p.(type) {
	case query.RefProjection:
		return "RefProjection"
	case query.LiteralProjection:
		return "LiteralProjection"
	case query.FuncProjection:
		return "FuncProjection"
	case query.AggregateProjection:
		return "AggregateProjection"
	case query.ExprProjection:
		return "ExprProjection"
	default:
		return ""
	}
}

func matchUse(u query.Use) string {
	switch u.(type) {
	case query.PropertyUse:
		return "PropertyUse"
	case query.ClauseSlotUse:
		return "ClauseSlotUse"
	case query.ExprUse:
		return "ExprUse"
	default:
		return ""
	}
}

func matchEffect(e query.Effect) string {
	switch e.(type) {
	case query.CreateEffect:
		return "CreateEffect"
	case query.MergeEffect:
		return "MergeEffect"
	case query.DeleteEffect:
		return "DeleteEffect"
	case query.SetPropertyEffect:
		return "SetPropertyEffect"
	case query.SetEntityEffect:
		return "SetEntityEffect"
	case query.SetLabelsEffect:
		return "SetLabelsEffect"
	case query.RemovePropertyEffect:
		return "RemovePropertyEffect"
	case query.RemoveLabelsEffect:
		return "RemoveLabelsEffect"
	default:
		return ""
	}
}

func matchSetEffect(e query.SetEffect) string {
	switch e.(type) {
	case query.SetPropertyEffect:
		return "SetPropertyEffect"
	case query.SetEntityEffect:
		return "SetEntityEffect"
	case query.SetLabelsEffect:
		return "SetLabelsEffect"
	default:
		return ""
	}
}

func matchType(t query.Type) string {
	switch t.(type) {
	case query.TypeBool:
		return "TypeBool"
	case query.TypeInt:
		return "TypeInt"
	case query.TypeFloat:
		return "TypeFloat"
	case query.TypeString:
		return "TypeString"
	case query.TypeNull:
		return "TypeNull"
	case query.TypeMap:
		return "TypeMap"
	case query.TypeNode:
		return "TypeNode"
	case query.TypeEdge:
		return "TypeEdge"
	case query.TypeList:
		return "TypeList"
	case query.TypeUnknown:
		return "TypeUnknown"
	case query.TypeDate:
		return "TypeDate"
	case query.TypeTime:
		return "TypeTime"
	case query.TypeLocalTime:
		return "TypeLocalTime"
	case query.TypeDateTime:
		return "TypeDateTime"
	case query.TypeLocalDateTime:
		return "TypeLocalDateTime"
	case query.TypeDuration:
		return "TypeDuration"
	case query.TypePath:
		return "TypePath"
	default:
		return ""
	}
}

// sealedSums is the table of every interface internal/query declares with an
// unexported marker of its own. Each row's variant keys are checked against
// the package's own sources by
// TestQuerySumsAreNotClosed/<iface>/declared_variants, so a variant landing or
// leaving without an edit here fails rather than silently narrowing the rows
// below it. That the table names every interface declaring a marker of
// its own — the claim this comment opens with, which the per-sum rows'
// comparisons do not reach — is checked by
// TestQuerySumsAreNotClosed/table_covers_every_interface_with_its_own_marker.
//
// The type argument on each forms call is the sum's interface: it is the
// compile-time half of the claim, and dropping a row's pointer or embedded
// entry is a build failure, not a quiet pass.
var sealedSums = []sealedSum{
	{
		iface:  "Binding",
		marker: "isBinding",
		match:  matcher(matchBinding),
		docs:   docEnumeratesVariants,
		variants: map[string]inhabitant{
			"NodeBinding":   forms[query.Binding](query.NodeBinding{}, &query.NodeBinding{}, embedNodeBinding{}),
			"EdgeBinding":   forms[query.Binding](query.EdgeBinding{}, &query.EdgeBinding{}, embedEdgeBinding{}),
			"PathBinding":   forms[query.Binding](query.PathBinding{}, &query.PathBinding{}, embedPathBinding{}),
			"UnwindBinding": forms[query.Binding](query.UnwindBinding{}, &query.UnwindBinding{}, embedUnwindBinding{}),
			"CallBinding":   forms[query.Binding](query.CallBinding{}, &query.CallBinding{}, embedCallBinding{}),
		},
	},
	{
		iface:  "PathMember",
		marker: "isPathMember",
		match:  matcher(matchPathMember),
		docs:   docEnumeratesVariants,
		variants: map[string]inhabitant{
			"NamedNodeMember": forms[query.PathMember](query.NamedNodeMember{}, &query.NamedNodeMember{}, embedNamedNodeMember{}),
			"NamedEdgeMember": forms[query.PathMember](query.NamedEdgeMember{}, &query.NamedEdgeMember{}, embedNamedEdgeMember{}),
			"AnonEdgeMember":  forms[query.PathMember](query.AnonEdgeMember{}, &query.AnonEdgeMember{}, embedAnonEdgeMember{}),
			"AnonNodeMember":  forms[query.PathMember](query.AnonNodeMember{}, &query.AnonNodeMember{}, embedAnonNodeMember{}),
		},
	},
	{
		iface:  "Endpoint",
		marker: "isEndpoint",
		match:  matcher(matchEndpoint),
		docs:   docEnumeratesVariants,
		variants: map[string]inhabitant{
			"VarEndpoint":    forms[query.Endpoint](query.VarEndpoint{}, &query.VarEndpoint{}, embedVarEndpoint{}),
			"InlineEndpoint": forms[query.Endpoint](query.InlineEndpoint{}, &query.InlineEndpoint{}, embedInlineEndpoint{}),
		},
	},
	{
		iface:  "Projection",
		marker: "isProjection",
		match:  matcher(matchProjection),
		docs:   docEnumeratesVariants,
		variants: map[string]inhabitant{
			"RefProjection":       forms[query.Projection](query.RefProjection{}, &query.RefProjection{}, embedRefProjection{}),
			"LiteralProjection":   forms[query.Projection](query.LiteralProjection{}, &query.LiteralProjection{}, embedLiteralProjection{}),
			"FuncProjection":      forms[query.Projection](query.FuncProjection{}, &query.FuncProjection{}, embedFuncProjection{}),
			"AggregateProjection": forms[query.Projection](query.AggregateProjection{}, &query.AggregateProjection{}, embedAggregateProjection{}),
			"ExprProjection":      forms[query.Projection](query.ExprProjection{}, &query.ExprProjection{}, embedExprProjection{}),
		},
	},
	{
		iface:  "Use",
		marker: "isUse",
		match:  matcher(matchUse),
		docs:   docEnumeratesVariants,
		variants: map[string]inhabitant{
			"PropertyUse":   forms[query.Use](query.PropertyUse{}, &query.PropertyUse{}, embedPropertyUse{}),
			"ClauseSlotUse": forms[query.Use](query.ClauseSlotUse{}, &query.ClauseSlotUse{}, embedClauseSlotUse{}),
			"ExprUse":       forms[query.Use](query.ExprUse{}, &query.ExprUse{}, embedExprUse{}),
		},
	},
	{
		iface:  "Effect",
		marker: "isEffect",
		match:  matcher(matchEffect),
		docs:   docEnumeratesVariants,
		variants: map[string]inhabitant{
			"CreateEffect":         forms[query.Effect](query.CreateEffect{}, &query.CreateEffect{}, embedCreateEffect{}),
			"MergeEffect":          forms[query.Effect](query.MergeEffect{}, &query.MergeEffect{}, embedMergeEffect{}),
			"DeleteEffect":         forms[query.Effect](query.DeleteEffect{}, &query.DeleteEffect{}, embedDeleteEffect{}),
			"SetPropertyEffect":    forms[query.Effect](query.SetPropertyEffect{}, &query.SetPropertyEffect{}, embedSetPropertyEffect{}),
			"SetEntityEffect":      forms[query.Effect](query.SetEntityEffect{}, &query.SetEntityEffect{}, embedSetEntityEffect{}),
			"SetLabelsEffect":      forms[query.Effect](query.SetLabelsEffect{}, &query.SetLabelsEffect{}, embedSetLabelsEffect{}),
			"RemovePropertyEffect": forms[query.Effect](query.RemovePropertyEffect{}, &query.RemovePropertyEffect{}, embedRemovePropertyEffect{}),
			"RemoveLabelsEffect":   forms[query.Effect](query.RemoveLabelsEffect{}, &query.RemoveLabelsEffect{}, embedRemoveLabelsEffect{}),
		},
	},
	{
		// The SET-family sub-sum. Its embedders are the same types the Effect
		// row uses: a struct embedding SetPropertyEffect promotes isEffect and
		// isSetEffect alike, so one embedder inhabits both sums.
		iface:  "SetEffect",
		marker: "isSetEffect",
		match:  matcher(matchSetEffect),
		docs:   docEnumeratesVariants,
		variants: map[string]inhabitant{
			"SetPropertyEffect": forms[query.SetEffect](query.SetPropertyEffect{}, &query.SetPropertyEffect{}, embedSetPropertyEffect{}),
			"SetEntityEffect":   forms[query.SetEffect](query.SetEntityEffect{}, &query.SetEntityEffect{}, embedSetEntityEffect{}),
			"SetLabelsEffect":   forms[query.SetEffect](query.SetLabelsEffect{}, &query.SetLabelsEffect{}, embedSetLabelsEffect{}),
		},
	},
	{
		iface:  "Type",
		marker: "isType",
		match:  matcher(matchType),
		docs:   docDescribesByStage,
		variants: map[string]inhabitant{
			"TypeBool":          forms[query.Type](query.TypeBool{}, &query.TypeBool{}, embedTypeBool{}),
			"TypeInt":           forms[query.Type](query.TypeInt{}, &query.TypeInt{}, embedTypeInt{}),
			"TypeFloat":         forms[query.Type](query.TypeFloat{}, &query.TypeFloat{}, embedTypeFloat{}),
			"TypeString":        forms[query.Type](query.TypeString{}, &query.TypeString{}, embedTypeString{}),
			"TypeNull":          forms[query.Type](query.TypeNull{}, &query.TypeNull{}, embedTypeNull{}),
			"TypeMap":           forms[query.Type](query.TypeMap{}, &query.TypeMap{}, embedTypeMap{}),
			"TypeNode":          forms[query.Type](query.TypeNode{}, &query.TypeNode{}, embedTypeNode{}),
			"TypeEdge":          forms[query.Type](query.TypeEdge{}, &query.TypeEdge{}, embedTypeEdge{}),
			"TypeList":          forms[query.Type](query.TypeList{}, &query.TypeList{}, embedTypeList{}),
			"TypeUnknown":       forms[query.Type](query.TypeUnknown{}, &query.TypeUnknown{}, embedTypeUnknown{}),
			"TypeDate":          forms[query.Type](query.TypeDate{}, &query.TypeDate{}, embedTypeDate{}),
			"TypeTime":          forms[query.Type](query.TypeTime{}, &query.TypeTime{}, embedTypeTime{}),
			"TypeLocalTime":     forms[query.Type](query.TypeLocalTime{}, &query.TypeLocalTime{}, embedTypeLocalTime{}),
			"TypeDateTime":      forms[query.Type](query.TypeDateTime{}, &query.TypeDateTime{}, embedTypeDateTime{}),
			"TypeLocalDateTime": forms[query.Type](query.TypeLocalDateTime{}, &query.TypeLocalDateTime{}, embedTypeLocalDateTime{}),
			"TypeDuration":      forms[query.Type](query.TypeDuration{}, &query.TypeDuration{}, embedTypeDuration{}),
			"TypePath":          forms[query.Type](query.TypePath{}, &query.TypePath{}, embedTypePath{}),
		},
	},
}

// parseQueryPackage parses the package query files in this directory,
// in-package test files included: one of them declaring a marker would add a
// variant the rows below claim does not exist. Files here whose package clause
// is query_test are read past, since the markers are unexported and a method
// of that name in another package does not satisfy the interface.
//
// It walks the AST rather than grepping the sources because a commented-out
// declaration satisfies a grep, which would let the enumeration above drift
// behind a comment.
func parseQueryPackage(t *testing.T) (*token.FileSet, []*ast.File) {
	t.Helper()
	entries, err := os.ReadDir(".")
	require.NoError(t, err)

	fset := token.NewFileSet()
	var files []*ast.File
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Join(".", name), nil, parser.ParseComments)
		require.NoErrorf(t, err, "parsing %s", name)
		if f.Name.Name != "query" {
			continue
		}
		files = append(files, f)
	}
	// A filter that matched nothing would return empty sets, which agree with
	// an empty enumeration rather than contradicting it.
	require.NotEmpty(t, files, "no sources parsed from the package directory")
	return fset, files
}

// declaredMarkers returns one sorted entry per declaration of marker: the
// receiver type's name for a value receiver, and that name prefixed with "*"
// for a pointer receiver. Encoding the receiver form into the entry rather
// than asserting it separately keeps both drift modes — a variant appearing or
// disappearing, and a marker moving to a pointer receiver — on the single
// comparison below.
func declaredMarkers(t *testing.T, marker string) []string {
	t.Helper()
	fset, files := parseQueryPackage(t)

	var got []string
	for _, f := range files {
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv == nil || len(fn.Recv.List) != 1 || fn.Name.Name != marker {
				continue
			}
			recv := fn.Recv.List[0].Type
			prefix := ""
			if star, isPointer := recv.(*ast.StarExpr); isPointer {
				recv, prefix = star.X, "*"
			}
			id, ok := recv.(*ast.Ident)
			require.Truef(t, ok, "%s: unexpected receiver shape %T on %s", fset.Position(fn.Pos()), recv, marker)
			got = append(got, prefix+id.Name)
		}
	}
	sort.Strings(got)
	return got
}

// sealedInterfaces returns, for each interface declared in the package query
// sources of this directory, the unexported methods that interface names in
// its own body, sorted. An interface naming none is absent from the result,
// which is what separates the marker-sealed sums from Parser.
//
// Methods reached only by embedding are not read here: the walk skips every
// interface field that is not a method declaration, named embed and anonymous
// interface literal alike. SetEffect has an entry of its own because it names
// isSetEffect in its own body, not because it embeds Effect. An interface that
// named no unexported method of its own and obtained one solely by embedding
// would therefore go unreported — a limit of this reading, not a claim that no
// such interface can be written.
//
// The same AST walk the rows below use, for the same reason: a commented-out
// declaration satisfies a grep.
func sealedInterfaces(t *testing.T) map[string][]string {
	t.Helper()
	_, files := parseQueryPackage(t)

	got := map[string][]string{}
	for _, f := range files {
		for _, decl := range f.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.TYPE {
				continue
			}
			for _, spec := range gen.Specs {
				ts, ok := spec.(*ast.TypeSpec)
				if !ok {
					continue
				}
				iface, ok := ts.Type.(*ast.InterfaceType)
				if !ok {
					continue
				}
				for _, field := range iface.Methods.List {
					if _, isMethod := field.Type.(*ast.FuncType); !isMethod {
						continue // an embedded interface, not a method
					}
					for _, name := range field.Names {
						if !name.IsExported() {
							got[ts.Name.Name] = append(got[ts.Name.Name], name.Name)
						}
					}
				}
			}
		}
	}
	for _, methods := range got {
		sort.Strings(methods)
	}
	return got
}

// variantsDeclaringAField returns, sorted, the subset of names whose type
// declaration in the package query sources of this directory is a struct
// declaring at least one field. Embedded fields count: the parser lists them
// as fields with no name. The result is a subset rather than a per-name count
// because ast.StructType groups `a, b int` into one entry, so a count read
// here would not be a count of fields.
//
// A name the directory declares as something other than a struct, and a name
// it does not declare at all, fail here. Reporting either as "declares no
// field" would put a shape this walk cannot read on the no-parameter side of
// the caller's comparison.
//
// Reads the same sources through the same parseQueryPackage helper that
// declaredMarkers and sealedInterfaces read, and walks the AST for the reason
// that helper states: a commented-out field satisfies a grep.
func variantsDeclaringAField(t *testing.T, names []string) []string {
	t.Helper()
	fset, files := parseQueryPackage(t)

	wanted := map[string]bool{}
	for _, name := range names {
		wanted[name] = true
	}

	seen := map[string]bool{}
	var got []string
	for _, f := range files {
		for _, decl := range f.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.TYPE {
				continue
			}
			for _, spec := range gen.Specs {
				ts, ok := spec.(*ast.TypeSpec)
				if !ok || !wanted[ts.Name.Name] {
					continue
				}
				st, isStruct := ts.Type.(*ast.StructType)
				require.Truef(t, isStruct,
					"%s: %s is declared as %T rather than a struct, which this walk cannot read for fields", fset.Position(ts.Pos()), ts.Name.Name, ts.Type)
				seen[ts.Name.Name] = true
				if st.Fields != nil && len(st.Fields.List) > 0 {
					got = append(got, ts.Name.Name)
				}
			}
		}
	}
	for _, name := range names {
		require.Truef(t, seen[name],
			"%s declares a marker in this directory but no type declaration for it was found here, so whether it carries a field went unread", name)
	}
	sort.Strings(got)
	return got
}

// interfaceDoc returns the doc comment on the named interface declaration.
// Requiring exactly one declaration keeps a rename or a move from emptying the
// text the rows below read, which would satisfy those rows rather than fail
// them.
func interfaceDoc(t *testing.T, name string) string {
	t.Helper()
	fset, files := parseQueryPackage(t)

	var docs []string
	for _, f := range files {
		for _, decl := range f.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.TYPE {
				continue
			}
			for _, spec := range gen.Specs {
				ts, ok := spec.(*ast.TypeSpec)
				if !ok || ts.Name.Name != name {
					continue
				}
				doc := ts.Doc
				if doc == nil {
					doc = gen.Doc
				}
				require.NotNilf(t, doc, "%s: %s carries no doc comment", fset.Position(ts.Pos()), name)
				docs = append(docs, doc.Text())
			}
		}
	}
	require.Lenf(t, docs, 1, "expected one %s declaration in the package directory, found %d", name, len(docs))
	return docs[0]
}

// countWords spells a variant count the way the doc comments do. A count past
// the end of this table fails the row rather than skipping the comparison.
var countWords = []string{
	"zero", "one", "two", "three", "four", "five", "six", "seven", "eight",
	"nine", "ten", "eleven", "twelve", "thirteen", "fourteen", "fifteen",
	"sixteen", "seventeen", "eighteen", "nineteen", "twenty",
}

// spelledCount matches a spelled number qualifying the variants, or the arms
// that name them one apiece — the two phrasings the doc comments state a count
// in. Any other phrasing goes unmatched, so the enumerating rows require a
// match rather than reading zero of them as agreement.
var spelledCount = regexp.MustCompile(`\b(` + strings.Join(countWords, "|") + `)\s+(?:declared\s+)?(?:variants?|arms?)\b`)

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// TestQuerySumsAreNotClosed measures what the unexported markers on
// internal/query's interfaces do and do not buy, and is what those interfaces'
// doc comments cite.
//
// What they buy: a method named isBinding (isType, isEffect, …) in another
// package does not satisfy the interface, so this package's variants are the
// whole set of types that DECLARE the marker. That half is enforced by the
// compiler and has no row here.
//
// What they do not buy: two constructions inhabit each sum without declaring
// the marker — the pointer form of a variant, and a struct embedding one.
// Neither matches any arm, so a switch over the declared variants reaches its
// default. Counts live in the interfaces' doc comments, which the doc row
// holds to the declared set; stating one here would drift unread.
func TestQuerySumsAreNotClosed(t *testing.T) {
	// Every row below measures one sum against that sum's own declarations.
	// This row is the one that holds the set of rows to the package: without it
	// a marker-sealed interface reaches master read by nothing here, doc comment
	// included. That is not hypothetical — gqlc-1vkb's own site list names seven
	// interfaces (Binding, PathMember, Endpoint, Projection, Use, Effect, Type)
	// where the package declares eight, and SetEffect was found by hand while
	// the other seven were being rewritten.
	//
	// The name says "its own marker" rather than "marker-sealed" because that is
	// what sealedInterfaces reads; see the limit stated there.
	t.Run("table covers every interface with its own marker", func(t *testing.T) {
		declared := sealedInterfaces(t)
		// An empty ground truth agrees with any table that is also empty, so the
		// comparison below would hold without measuring anything. This is the
		// guard that makes it measure.
		require.NotEmpty(t, declared,
			"no interface in the package directory names an unexported method of its own; the comparison below would hold against an empty table rather than measure it")

		enumerated := map[string][]string{}
		for _, sum := range sealedSums {
			enumerated[sum.iface] = append(enumerated[sum.iface], sum.marker)
		}
		for _, markers := range enumerated {
			sort.Strings(markers)
		}

		require.Equal(t, enumerated, declared,
			"sealedSums (expected) and the interfaces naming an unexported method in the package sources (actual) have diverged. A name present only in actual is a marker-sealed interface with no row, so nothing here reads its doc comment or its variants — add a row. A name present only in expected is a row naming an interface this directory no longer declares. A name in both with differing methods means the row's marker field is not the method the interface names")
	})

	for _, sum := range sealedSums {
		t.Run(sum.iface, func(t *testing.T) {
			t.Run("declared variants", func(t *testing.T) {
				require.Equal(t, sortedKeys(sum.variants), declaredMarkers(t, sum.marker),
					"the set of %s declarations and the set sealedSums enumerates for %s have diverged. A bare name added or removed means a variant landed or left: extend the row and its matcher, or drop the stale entries. A name reported with a leading \"*\" means that marker moved to a pointer receiver, which is the mechanism the pointer-form row below and the interface's doc comment both rest on", sum.marker, sum.iface)
			})

			// The doc comments enumerate each sum's members and state their
			// count in separate sentences; this row is what holds both to the
			// declared set. Both policies are checked — a sum whose row
			// declares neither fails here rather than skipping the comparison.
			t.Run("doc comment states membership", func(t *testing.T) {
				doc := interfaceDoc(t, sum.iface)
				declared := declaredMarkers(t, sum.marker)

				require.Containsf(t, doc, sum.marker,
					"%s's doc comment does not name %s. The rows here rest on the marker being the thing that seals declaration, and a doc that never names it cannot be saying so", sum.iface, sum.marker)

				switch sum.docs {
				case docEnumeratesVariants:
					for _, entry := range declared {
						name := strings.TrimPrefix(entry, "*")
						require.Regexpf(t, `\b`+regexp.QuoteMeta(name)+`\b`, doc,
							"%s's doc comment does not name %s, which the package declares. The name is matched on word boundaries, so a longer name containing it does not stand in for it", sum.iface, name)
					}

					require.Lessf(t, len(declared), len(countWords),
						"%d declared variants is past the end of countWords; extend it rather than leaving the count unread", len(declared))
					want := countWords[len(declared)]
					spelled := spelledCount.FindAllStringSubmatch(doc, -1)
					require.NotEmptyf(t, spelled,
						`%s's doc comment states no count next to "variants" or "arms". The count is read from those two phrasings only, so rewording past them retires this comparison rather than failing it`, sum.iface)
					for _, match := range spelled {
						require.Equalf(t, want, match[1],
							"%s's doc comment says %q where the package declares %d variants", sum.iface, match[0], len(declared))
					}
				case docDescribesByStage:
					require.Emptyf(t, spelledCount.FindAllStringSubmatch(doc, -1),
						`%s's doc comment now spells a count next to "variants" or "arms", but its row says it describes the sum by stage, so nothing compares that count to the %d declarations. Move the row to docEnumeratesVariants, which names every declared variant and checks the count`, sum.iface, len(declared))
				case docPolicyUnset:
					t.Fatalf("%s's row declares no docPolicy; pick the one its doc comment follows rather than leaving the comment unread", sum.iface)
				default:
					// docPolicy is an int constant, so a third policy added
					// without an arm here would be read by nothing and its rows
					// would pass on the strength of no comparison. Reaching the
					// default is that omission.
					t.Fatalf("%s's row declares docPolicy %d, which this switch has no arm for; add the arm that reads its doc comment rather than leaving the comment unread", sum.iface, sum.docs)
				}
			})

			// The ALLOW half. Without it every REFUSE row below is satisfied
			// by a matcher that returns "" for everything.
			t.Run("value form matches its own arm", func(t *testing.T) {
				for _, name := range sortedKeys(sum.variants) {
					got := sum.match(sum.variants[name].value)
					require.Truef(t, got.satisfies, "%s in its value form does not satisfy %s", name, sum.iface)
					require.Equalf(t, name, got.arm,
						"%s in its value form must match the arm naming it", name)
				}
			})

			t.Run("pointer form inhabits but does not match", func(t *testing.T) {
				for _, name := range sortedKeys(sum.variants) {
					got := sum.match(sum.variants[name].pointer)
					require.Truef(t, got.satisfies,
						"*%s does not satisfy %s; the pointer form carries the value methods, so it inhabits the sum without declaring %s", name, sum.iface, sum.marker)
					require.Emptyf(t, got.arm,
						"*%s reached the arm naming %s; a pointer form matching the value arm would make the default narrower than this test claims", name, name)
				}
			})

			t.Run("embedded form inhabits but does not match", func(t *testing.T) {
				for _, name := range sortedKeys(sum.variants) {
					got := sum.match(sum.variants[name].embedded)
					require.Truef(t, got.satisfies,
						"a struct embedding %s does not satisfy %s; Go promotes an embedded type's unexported methods, so it inhabits the sum without declaring %s", name, sum.iface, sum.marker)
					require.Emptyf(t, got.arm,
						"a struct embedding %s reached the arm naming it; the embedder promotes the marker but is a distinct type, so it is expected at the default", name)
				}
			})
		})
	}
}

// TestTypeListIsTheOnlyVariantDeclaringAField holds the premise
// internal/query/cypher's commonType rests on. That function compares two
// Types by String rather than by concrete Go type, and cites TypeList as the
// variant that carries a parameter: TypeList renders its element into String,
// so the parameter reaches that comparison. A second variant carrying a
// parameter whose String did not render it would compare equal there across
// values this package had built as distinct.
//
// "Carries a parameter" is read here as "declares a struct field", which is
// what an AST walk can see. The two part company for a variant that reaches
// state some other way — through a method over package-level state, say —
// which this reading would report as carrying none.
//
// The domain is declaredMarkers' own output rather than a list written here,
// so a variant landing in the sum is read by this test without an edit.
// TestQuerySumsAreNotClosed/Type/declared_variants is what holds that domain
// to the sealedSums table; this test does not check the census and would pass
// against a gutted one. It cannot pass against an empty one: the expected set
// below is non-empty, so an empty domain fails here rather than agreeing.
func TestTypeListIsTheOnlyVariantDeclaringAField(t *testing.T) {
	var names []string
	for _, entry := range declaredMarkers(t, "isType") {
		// A marker on a pointer receiver is reported with a leading "*"; the
		// field declaration it points at is on the named type either way.
		names = append(names, strings.TrimPrefix(entry, "*"))
	}

	require.Equal(t, []string{"TypeList"}, variantsDeclaringAField(t, names),
		`the Type variants declaring a struct field are no longer TypeList alone. internal/query/cypher's commonType compares Types by String, which distinguishes two values of a parameterised variant only where that variant renders its parameter into String — TypeList does ("list<list<int>>"). A name added here is a variant that now carries one: render it into that variant's String, or change commonType to stop comparing by String, and amend commonType's comment either way. TypeList's absence means it no longer carries its element, which that comment also names`)
}
