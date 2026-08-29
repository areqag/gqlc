package neo4j

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/areqag/gqlc/internal/codegen"
)

// sourceGroup carries one <name>.cypher.go file's worth of prepared
// queries in emission order. Grouping is by SourceFile basename minus
// extension, in first-appearance order.
type sourceGroup struct {
	filename string
	queries  []codegen.Query
}

// groupBySource groups prepared queries by SourceFile basename in
// first-appearance order (spec §5.5). A query with no SourceFile is
// unreachable at C1 (queryfile always records one) but defensively
// grouped under "queries" so the emission is uniform.
func groupBySource(prepared []codegen.Query) []sourceGroup {
	seen := make(map[string]int)
	var groups []sourceGroup
	for _, p := range prepared {
		base := p.SourceFile
		if base == "" {
			base = "queries"
		}
		base = filepath.Base(base)
		stem := strings.TrimSuffix(base, filepath.Ext(base))
		key := stem
		if idx, ok := seen[key]; ok {
			groups[idx].queries = append(groups[idx].queries, p)
			continue
		}
		seen[key] = len(groups)
		groups = append(groups, sourceGroup{
			filename: stem + ".cypher.go",
			queries:  []codegen.Query{p},
		})
	}
	return groups
}

// groupImports computes the C3 per-file import gates for one
// <name>.cypher.go source group. dbtype fires when any column or
// parameter decodes / encodes through a dbtype.<Kind> carrier
// (entity, DATE property, six temporal-column kinds except
// TemporalDateTime, or a list column whose leaf uses dbtype.<Kind>).
// time fires when any column or parameter uses time.Time (TIMESTAMP
// property, TemporalDateTime column, or a list column whose leaf is
// either). fmt fires when any method's body emits a decode wrapper
// (`fmt.Errorf`) — every :one / :many method does, and every write-
// with-projection method does; the C4 :exec three-line body does not
// (spec §5.5).
func groupImports(queries []codegen.Query) (needDbtype, needTime, needFmt bool) {
	for _, p := range queries {
		if p.Cardinality != codegen.CardinalityExec {
			// Row-assembly bodies emit fmt.Errorf decode wrappers.
			needFmt = true
		}
		for _, f := range p.RowFields {
			nd, nt := columnNeedsImports(f)
			if nd {
				needDbtype = true
			}
			if nt {
				needTime = true
			}
		}
		for _, f := range p.ParamFields {
			nd, nt := goTypeNeedsImports(f.GoType)
			if nd {
				needDbtype = true
			}
			if nt {
				needTime = true
			}
		}
	}
	return needDbtype, needTime, needFmt
}

// columnNeedsImports reports whether one prepared row needs dbtype /
// time in the enclosing file's import block. The list arm walks the
// row's committed element plan recursively; every other arm delegates
// to a per-kind test on the row's emitted Go type.
func columnNeedsImports(f codegen.Row) (needDbtype, needTime bool) {
	switch f.Kind {
	case codegen.ColumnNode, codegen.ColumnEdge, codegen.ColumnEdgeUnion:
		// edgeUnion decode type-asserts dbtype.Relationship (§5.5); the
		// column's emitted Go type is the sealed interface (not a
		// dbtype.* text), so goTypeNeedsImports does not fire and the
		// need is declared here.
		return true, false
	case codegen.ColumnTemporal, codegen.ColumnProperty:
		return decodeNeedsImports(f.GoType)
	case codegen.ColumnScalar, codegen.ColumnScalarNull, codegen.ColumnAny:
		return false, false
	case codegen.ColumnList:
		return listElemNeedsImports(f.ListElem)
	}
	return false, false
}

// listElemNeedsImports walks a codegen.ListElem tree recursively,
// reporting whether the element decode uses dbtype / time carriers.
// Called by columnNeedsImports for codegen.ColumnList rows; render never sees
// a resolver type. Node / Edge / EdgeUnion arms need dbtype
// unconditionally (dbtype.Node / dbtype.Relationship carriers, §5.5);
// Property / Temporal delegate to goTypeNeedsImports on the arm's
// emitted GoType; List recurses; every other arm needs neither.
func listElemNeedsImports(e *codegen.ListElem) (needDbtype, needTime bool) {
	if e == nil {
		return false, false
	}
	switch e.Kind {
	case codegen.ColumnNode, codegen.ColumnEdge, codegen.ColumnEdgeUnion:
		return true, false
	case codegen.ColumnProperty, codegen.ColumnTemporal:
		return decodeNeedsImports(e.GoType)
	case codegen.ColumnList:
		return listElemNeedsImports(e.Nested)
	case codegen.ColumnScalar, codegen.ColumnScalarNull, codegen.ColumnAny:
		// bare `any` / plain scalar carriers — no dbtype / time.
		return false, false
	}
	return false, false
}

// goTypeNeedsImports reports whether a Go type text names dbtype or
// time. Both are single-string prefix checks; list types are walked
// element-wise by stripping the leading "[]".
func goTypeNeedsImports(ty string) (bool, bool) {
	if elem := strings.TrimPrefix(ty, "[]"); elem != ty {
		return goTypeNeedsImports(elem)
	}
	needDbtype := strings.HasPrefix(ty, "dbtype.")
	needTime := ty == "time.Time"
	return needDbtype, needTime
}

// decodeNeedsImports is goTypeNeedsImports for a decode position, where
// the imports follow the carrier the driver hands over rather than the
// type text the column is emitted as. A neutral temporal carrier (ADR
// 0033) names no package, and yet the site asserts or requests its
// dbtype counterpart before converting.
//
// A bind position takes the plain test instead: a parameter of a
// neutral carrier reaches the driver through from<X>, whose dbtype
// mention lives in temporal_neo4j.go. Answering true for it would put
// an import in the file that nothing there names.
func decodeNeedsImports(ty string) (bool, bool) {
	needDbtype, needTime := goTypeNeedsImports(ty)
	return needDbtype || isTemporalCarrier(leafType(ty)), needTime
}

// renderCypherFile emits one <name>.cypher.go file (spec §5.5). Per
// query in order: query-text const, Params struct (if any), Row struct
// (if any), method. The withDbtype flag toggles the dbtype import; the
// withTime flag toggles the time-stdlib import (C3, for TIMESTAMP /
// TemporalDateTime carriers). The withFmt flag toggles the fmt import
// (C4: a write-only file whose queries are all :exec emits no
// fmt.Errorf wrapper, so fmt is elided). The row-assembly template
// inlines the per-kind decode arm.
func renderCypherFile(pkg string, queries []codegen.Query, withDbtype, withTime, withFmt bool, target driverTarget) []byte {
	var b strings.Builder
	b.WriteString(codegen.Header())
	b.WriteString("package ")
	b.WriteString(pkg)
	b.WriteString("\n\n")
	// Import order per goimports: stdlib first (context, fmt, time),
	// then third-party (neo4j, dbtype). A single grouped import ()
	// block keeps gofmt output stable.
	b.WriteString("import (\n\t\"context\"\n")
	if withFmt {
		b.WriteString("\t\"fmt\"\n")
	}
	if withTime {
		b.WriteString("\t\"time\"\n")
	}
	b.WriteString("\n\t\"" + target.neo4jImport + "\"\n")
	if withDbtype {
		b.WriteString("\t\"" + target.dbtypeImport + "\"\n")
	}
	b.WriteString(")\n\n")

	for i, p := range queries {
		if i > 0 {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "const %s = `%s`\n\n", codegen.QueryTextConst(p), p.SourceText)
		if len(p.ParamFields) >= 2 {
			fmt.Fprintf(&b, "type %sParams struct {\n", p.MethodName)
			for _, f := range p.ParamFields {
				b.WriteString("\t")
				b.WriteString(f.Field)
				b.WriteString(" ")
				if f.Nullable {
					b.WriteString("*")
				}
				b.WriteString(f.GoType)
				b.WriteString("\n")
			}
			b.WriteString("}\n\n")
		}
		if len(p.RowFields) >= 2 {
			fmt.Fprintf(&b, "type %sRow struct {\n", p.MethodName)
			for _, f := range p.RowFields {
				b.WriteString("\t")
				b.WriteString(f.Field)
				b.WriteString(" ")
				// EdgeUnion columns emit the bare interface, never
				// pointer-to-interface — even when nullable (§3.3).
				if f.Nullable && f.Kind != codegen.ColumnEdgeUnion {
					b.WriteString("*")
				}
				b.WriteString(f.GoType)
				b.WriteString("\n")
			}
			b.WriteString("}\n\n")
		}
		writeMethod(&b, p)
	}
	return []byte(b.String())
}

// writeMethodSignature writes one `MethodName(ctx context.Context,
// ...) (Return, error)` line — used both by the interface entry in
// querier.go and by the method definition in <name>.cypher.go. C4
// adds the :exec arm: the return list collapses to a bare `error`
// (no rows-to-decode).
func writeMethodSignature(b *strings.Builder, p codegen.Query) {
	b.WriteString(p.MethodName)
	b.WriteString("(ctx context.Context")
	switch len(p.ParamFields) {
	case 0:
		// bare arg
	case 1:
		fmt.Fprintf(b, ", %s ", codegen.ParamArg)
		if p.ParamFields[0].Nullable {
			b.WriteString("*")
		}
		b.WriteString(p.ParamFields[0].GoType)
	default:
		fmt.Fprintf(b, ", %s %sParams", codegen.ParamArg, p.MethodName)
	}
	if p.Cardinality == codegen.CardinalityExec {
		b.WriteString(") error")
		return
	}
	b.WriteString(") (")
	b.WriteString(returnTypeText(p))
	b.WriteString(", error)")
}

// returnTypeText composes the return-type text for a prepared query.
// :one → T or MethodRow; :many → []T or []MethodRow. Bare-value shape
// used for single-column projections; struct shape otherwise.
func returnTypeText(p codegen.Query) string {
	var elem string
	if len(p.RowFields) == 1 {
		elem = ""
		// Nullable columns wrap the emitted Go type in a pointer, EXCEPT
		// edgeUnion columns whose emitted type is a sealed interface —
		// nil is the natural absence value for an interface, and
		// pointer-to-interface is the Go anti-pattern ADR 0010 D3
		// Resolved (lines 343–345) forbids (§3.3).
		if p.RowFields[0].Nullable && p.RowFields[0].Kind != codegen.ColumnEdgeUnion {
			elem = "*"
		}
		elem += p.RowFields[0].GoType
	} else {
		elem = p.MethodName + "Row"
	}
	if p.Cardinality == codegen.CardinalityMany {
		return "[]" + elem
	}
	return elem
}

// zeroValueText composes the zero-value expression for a prepared
// query's return type, matching the emitted method signature (§5.3).
// :many always returns a slice, whose zero value is nil. :one returns
// T (single column) or MethodRow (multi-column) — the T's zero value
// (or nil for a nullable pointer T; entity struct's zero-composite for
// a bare-value entity column). C3 extends the switch to temporals
// (dbtype.Kind{} / time.Time{}), lists (nil), scalars (bool/int64/
// float64/string), map (nil), and any (nil).
func zeroValueText(p codegen.Query) string {
	if p.Cardinality == codegen.CardinalityMany {
		return "nil"
	}
	if len(p.RowFields) == 1 {
		f := p.RowFields[0]
		if f.Nullable {
			return "nil"
		}
		switch f.Kind {
		case codegen.ColumnNode, codegen.ColumnEdge:
			return f.GoType + "{}"
		case codegen.ColumnTemporal:
			return f.GoType + "{}"
		case codegen.ColumnList:
			return "nil"
		case codegen.ColumnAny, codegen.ColumnScalarNull, codegen.ColumnEdgeUnion:
			// edgeUnion single-column return type is the interface; its
			// zero value is nil (§3.1 / §5.5). codegen.ColumnScalarNull is
			// unreachable at the top level today (Phase B routes
			// ScalarNull to codegen.ColumnAny) but listed for exhaustive-switch
			// discipline.
			return "nil"
		case codegen.ColumnProperty, codegen.ColumnScalar:
			// Fall through to the per-Go-type dispatch below.
		}
		switch f.GoType {
		case "string":
			return `""`
		case "bool":
			return "false"
		case "float32", "float64":
			return "0"
		case "map[string]any":
			return "nil"
		case "any":
			return "nil"
		case "time.Time", "Date", "Time", "LocalTime",
			"LocalDateTime", "Duration":
			// The six temporal property widths carry a struct, whose zero
			// is a composite literal and not the numeric zero the default
			// arm spells. The ColumnTemporal arm above does not cover them:
			// that is the kind a temporal *expression* takes, and a
			// projection of a stored TIMESTAMP property is ColumnProperty.
			// Unreached until a fixture ran a :one over one, at which point
			// the emitted `return 0, err` did not compile.
			return f.GoType + "{}"
		default:
			return "0"
		}
	}
	return p.MethodName + "Row{}"
}

// writeMethod writes the method definition + body (spec §5.3 / §5.5).
// C4 adds the :exec arm: three-line body (run, discard rows, return
// error) with no Row-struct decoding.
func writeMethod(b *strings.Builder, p codegen.Query) {
	// Doc comment: first 3 lines of query text, prefixed "//   ".
	writeDocComment(b, p)
	b.WriteString("func (q *Queries) ")
	writeMethodSignature(b, p)
	b.WriteString(" {\n")

	if p.Cardinality == codegen.CardinalityExec {
		fmt.Fprintf(b, "\t_, err := q.db.run(ctx, %s, %s, %s)\n", codegen.QueryTextConst(p), paramsMapText(p), accessModeText(p.IsWrite))
		b.WriteString("\treturn err\n")
		b.WriteString("}\n")
		return
	}

	// Body: build the params map, call run, decode.
	writeRunCall(b, p)

	if p.Cardinality == codegen.CardinalityOne {
		writeOneBody(b, p)
	} else {
		writeManyBody(b, p)
	}
	b.WriteString("}\n")
}

// accessModeText picks the fourth q.db.run argument (spec §1.1).
// Dispatch is on committed data — codegen.Query.IsWrite — never on
// Validated.Statement.
func accessModeText(isWrite bool) string {
	if isWrite {
		return "neo4j.AccessModeWrite"
	}
	return "neo4j.AccessModeRead"
}

// writeDocComment emits the per-method doc comment: the method name
// and the first 3 lines of the query text, prefixed //   .
func writeDocComment(b *strings.Builder, p codegen.Query) {
	fmt.Fprintf(b, "// %s executes the %s query.\n//\n", p.MethodName, p.MethodName)
	lines := strings.Split(strings.TrimRight(p.SourceText, "\n"), "\n")
	limit := 3
	if len(lines) < limit {
		limit = len(lines)
	}
	for i := 0; i < limit; i++ {
		fmt.Fprintf(b, "//   %s\n", lines[i])
	}
	if len(lines) > 3 {
		b.WriteString("//   ...\n")
	}
}

// writeRunCall emits the `records, err := q.db.run(...)` prelude.
// C4 threads the access mode dispatch per Validated.Statement (§5.5);
// the C1 hardcoded neo4j.AccessModeRead retires.
func writeRunCall(b *strings.Builder, p codegen.Query) {
	fmt.Fprintf(b, "\trecords, err := q.db.run(ctx, %s, %s, %s)\n", codegen.QueryTextConst(p), paramsMapText(p), accessModeText(p.IsWrite))
	fmt.Fprintf(b, "\tif err != nil {\n\t\treturn %s, err\n\t}\n", zeroValueText(p))
}

// paramsMapText composes the driver-binding map literal. C3 extends
// the per-field expression with the FLOAT32 encode-widen contract:
// map[string]any{"x": float64(x)} for a float32 parameter, symmetric
// with the decode-narrow site (§5.5). Narrow-integer parameters keep
// the widen pattern (int64(v)) — the driver accepts the wider carrier.
// Nullable parameters go through binParamExpr, which handles the
// nil-pointer case by binding a bare nil literal.
func paramsMapText(p codegen.Query) string {
	if len(p.ParamFields) == 0 {
		return "nil"
	}
	var b strings.Builder
	b.WriteString("map[string]any{")
	for i, f := range p.ParamFields {
		if i > 0 {
			b.WriteString(", ")
		}
		access := codegen.ParamArg
		if len(p.ParamFields) > 1 {
			access = codegen.ParamArg + "." + f.Field
		}
		fmt.Fprintf(&b, "%q: %s", f.RawName, paramBindExpr(f, access))
	}
	b.WriteString("}")
	return b.String()
}

// paramBindExpr renders the driver-binding expression for one prepared
// parameter, given its access expression (a bare local for the single-
// param method form, or arg.Field for the multi-param form). Nullable
// parameters pass through unchanged (the driver accepts a nil pointer
// as SQL null). Non-nullable narrow-integer / float32 widen to their
// driver carrier via a Go conversion. Every other type binds bare.
//
// The neutral temporal carriers (ADR 0033) are the exception on both
// arms: they are gqlc's own structs, which the driver's reflective
// parameter marshalling has no encoding for, so each converts to its
// dbtype counterpart first. That includes the nullable arm, where the
// pass-the-pointer-through shape does not survive — *Date reaches the
// same reflective path as Date, one dereference later.
//
// Slices are asked separately, and must not be routed through
// driverCarrier: that function answers which neo4j.GetRecordValue[T] the
// driver hands a value back in, which is a decode question. Its answer
// for a list is []any, and rendering that as a widen produced
// `[]any(arg)` — not a Go conversion at all, so any query with a list
// parameter emitted a package that did not compile (bd gqlc-hrls).
func paramBindExpr(f codegen.Param, access string) string {
	if isSliceType(f.GoType) {
		return sliceParamBindExpr(f.GoType, f.Nullable, access)
	}
	if f.Nullable {
		if isTemporalCarrier(f.GoType) {
			return fmt.Sprintf("from%sPtr(%s)", f.GoType, access)
		}
		// Uniform: pass the pointer through as-is. A nil pointer binds
		// Cypher null via the driver's parameter marshalling.
		return access
	}
	carrier := driverCarrier(f.GoType)
	if carrier != f.GoType {
		return widenExpr(f.GoType, access)
	}
	return access
}

// sliceParamBindExpr renders the binding for a list parameter.
//
// Read off the driver's packer at v5.28.4 and v6.2.0
// (neo4j/internal/bolt/outgoing.go), which both have this shape: packX's
// reflect.Slice arm names five slice types it packs directly and ends in
// a default that walks ANY other slice element by element through packV,
// and packV handles bool, every int and uint width, both float widths,
// string, pointers and nested slices. So the widening the scalar arms
// here do per value, the packer already does per element, and a list
// reaches the wire needing no conversion at all. That covers the nullable
// case too: packX's reflect.Ptr arm indirects to the slice and packs it,
// and a nil pointer packs as the Cypher null the schema declared.
//
// The one value packV cannot reach is a struct the driver does not know.
// packStruct's cases are dbtype.Point2D/3D, time.Time and the five dbtype
// temporals, and its default raises UnsupportedTypeError. gqlc's neutral
// carriers are not among them, so a list whose leaf is one of those five
// is the sole list shape still owing a conversion — per element, into the
// driver's own array carrier, mirroring the per-element narrow the decode
// side has had since walkListElemBody.
func sliceParamBindExpr(goType string, nullable bool, access string) string {
	if !isTemporalCarrier(leafType(goType)) {
		return access
	}
	helper := temporalListHelper(leafType(goType), sliceDepth(goType))
	if nullable {
		helper += "Ptr"
	}
	return fmt.Sprintf("%s(%s)", helper, access)
}

// writeOneBody emits the :one arity-check + per-column decode + return.
func writeOneBody(b *strings.Builder, p codegen.Query) {
	zero := zeroValueText(p)
	fmt.Fprintf(b, "\tif len(records) == 0 {\n\t\treturn %s, ErrNoRows\n\t}\n", zero)
	fmt.Fprintf(b, "\tif len(records) > 1 {\n\t\treturn %s, ErrMultipleResults\n\t}\n", zero)

	if len(p.RowFields) == 1 {
		f := p.RowFields[0]
		writeSingleColumnDecode(b, p, f, "records[0]", zero, "\treturn ", ", nil\n")
		return
	}

	fmt.Fprintf(b, "\tvar row %sRow\n", p.MethodName)
	for _, f := range p.RowFields {
		writeSingleColumnDecode(b, p, f, "records[0]", zero, "\trow."+f.Field+" = ", "\n")
	}
	b.WriteString("\treturn row, nil\n")
}

// writeManyBody emits the :many loop + per-column decode + return.
func writeManyBody(b *strings.Builder, p codegen.Query) {
	var elem string
	if len(p.RowFields) == 1 {
		// EdgeUnion columns emit the bare interface, never
		// pointer-to-interface — even when nullable (§3.3).
		if p.RowFields[0].Nullable && p.RowFields[0].Kind != codegen.ColumnEdgeUnion {
			elem = "*"
		}
		elem += p.RowFields[0].GoType
	} else {
		elem = p.MethodName + "Row"
	}
	fmt.Fprintf(b, "\tout := make([]%s, 0, len(records))\n", elem)
	b.WriteString("\tfor _, record := range records {\n")

	if len(p.RowFields) == 1 {
		f := p.RowFields[0]
		writeSingleColumnDecode(b, p, f, "record", "nil", "\t\tout = append(out, ", ")\n")
	} else {
		fmt.Fprintf(b, "\t\tvar row %sRow\n", p.MethodName)
		for _, f := range p.RowFields {
			writeSingleColumnDecodeIndent(b, p, f, "record", "nil", "\t\trow."+f.Field+" = ", "\n", "\t\t")
		}
		b.WriteString("\t\tout = append(out, row)\n")
	}

	b.WriteString("\t}\n")
	b.WriteString("\treturn out, nil\n")
}

// writeSingleColumnDecode emits one column's GetRecordValue call + err
// handling + nullability check + assign/return line, at the standard
// method-body indent level.
func writeSingleColumnDecode(b *strings.Builder, p codegen.Query, f codegen.Row, recordExpr, zero, assignPrefix, assignSuffix string) {
	writeSingleColumnDecodeIndent(b, p, f, recordExpr, zero, assignPrefix, assignSuffix, "\t")
}

// writeSingleColumnDecodeIndent is writeSingleColumnDecode's inner
// variant, taking the block indent explicitly so the :many loop body
// can indent one level deeper.
//
// neo4j.GetRecordValue's T constraint is a narrow union (bool, int64,
// float64, string, plus driver types); Go's arbitrary numeric widths
// (int8..int32, int, uint*, float32) are NOT in it. C1's approach:
// decode via the driver's native carrier (int64 for every integer
// family, float64 for every float family), then narrow with a plain
// Go conversion. This matches sqlc's approach for narrow-width columns
// (its Int64 carrier + cast). Widening is safe; narrowing is the
// caller's contract per the schema author's declared width (FLOAT32
// schema-width contract is C3's business per §5.1).
func writeSingleColumnDecodeIndent(b *strings.Builder, p codegen.Query, f codegen.Row, recordExpr, zero, assignPrefix, assignSuffix, indent string) {
	varName := "value"
	if len(p.RowFields) > 1 {
		for i, r := range p.RowFields {
			if r.ColumnName == f.ColumnName && r.Field == f.Field {
				varName = valueName(i)
				break
			}
		}
	}
	switch f.Kind {
	case codegen.ColumnNode, codegen.ColumnEdge:
		writeEntityColumnDecodeIndent(b, p, f, recordExpr, zero, assignPrefix, assignSuffix, indent, varName)
		return
	case codegen.ColumnAny, codegen.ColumnScalarNull:
		// codegen.ColumnScalarNull at the top level is unreachable today (Phase B
		// routes ScalarNull to codegen.ColumnAny), but shares codegen.ColumnAny's
		// record.Get lane and is listed for exhaustive-switch discipline.
		writeAnyColumnDecodeIndent(b, p, f, recordExpr, zero, assignPrefix, assignSuffix, indent, varName)
		return
	case codegen.ColumnList:
		writeListColumnDecodeIndent(b, p, f, recordExpr, zero, assignPrefix, assignSuffix, indent, varName)
		return
	case codegen.ColumnEdgeUnion:
		writeEdgeUnionColumnDecodeIndent(b, p, f, recordExpr, zero, assignPrefix, assignSuffix, indent, varName)
		return
	case codegen.ColumnProperty:
		// A property of no declared shape rides no driver carrier, so
		// there is no neo4j.GetRecordValue[T] for it to go through:
		// `any` is a member of neither neo4j.PropertyValue nor
		// neo4j.RecordValue, and GetRecordValue[any] does not compile.
		// The same rule the entity path states as ridesADriverCarrier
		// and the element path as carriesElemBare, one axis up.
		if !ridesADriverCarrier(f.GoType) {
			writeAnyColumnDecodeIndent(b, p, f, recordExpr, zero, assignPrefix, assignSuffix, indent, varName)
			return
		}
	case codegen.ColumnTemporal, codegen.ColumnScalar:
		// Fall through to the GetRecordValue + narrow-convert path below.
	}
	// codegen.ColumnProperty / codegen.ColumnTemporal / codegen.ColumnScalar all use GetRecordValue
	// with the driver-carrier + narrow-convert pattern. Temporals /
	// scalars have carrier == GoType; property FLOAT32 narrows float64 →
	// float32; property narrow-int narrows int64 → intN.
	carrier := driverCarrier(f.GoType)
	fmt.Fprintf(b, "%s%s, isNil, err := neo4j.GetRecordValue[%s](%s, %q)\n", indent, varName, carrier, recordExpr, f.ColumnName)
	fmt.Fprintf(b, "%sif err != nil {\n%s\treturn %s, fmt.Errorf(\"%s: decode column %%q: %%w\", %q, err)\n%s}\n", indent, indent, zero, p.MethodName, f.ColumnName, indent)
	// Emit the value expression: bare varName if carrier == GoType, else a
	// Go conversion. Used both in the nullable and non-nullable arms.
	valueExpr := varName
	if carrier != f.GoType {
		valueExpr = narrowExpr(f.GoType, varName)
	}
	if f.Nullable {
		// Nullable: nil pointer when null, address of a narrowed local
		// otherwise.
		fmt.Fprintf(b, "%svar %sPtr *%s\n", indent, varName, f.GoType)
		fmt.Fprintf(b, "%sif !isNil {\n%s\tv := %s\n%s\t%sPtr = &v\n%s}\n", indent, indent, valueExpr, indent, varName, indent)
		b.WriteString(indent)
		b.WriteString(assignPrefix[len(indent):])
		b.WriteString(varName)
		b.WriteString("Ptr")
		b.WriteString(assignSuffix)
		return
	}
	// Non-nullable: error if isNil; else assign narrowed value.
	fmt.Fprintf(b, "%sif isNil {\n%s\treturn %s, fmt.Errorf(\"%s: column %%q is non-nullable but arrived null\", %q)\n%s}\n", indent, indent, zero, p.MethodName, f.ColumnName, indent)
	b.WriteString(indent)
	b.WriteString(assignPrefix[len(indent):])
	b.WriteString(valueExpr)
	b.WriteString(assignSuffix)
}

// valueName is the decoded local at position i. Positional, and so the
// generator's own: no identifier a schema or a query text chose reaches
// a body's scope through one.
func valueName(i int) string { return fmt.Sprintf("value%d", i) }

// writeAnyColumnDecodeIndent emits the record.Get lane for a column
// whose emitted Go type is `any` — ResolvedUnknown, ResolvedScalar
// {Null}, or a schema property whose declared width is ANY VALUE (spec
// §5.5). The driver's Get returns (any, bool) where bool is "found"
// (not "null"). The "not-found" branch is a decode error (the resolver
// committed the column, so the driver must produce it); the "found"
// branch assigns the value verbatim (a nil value satisfies the `any`
// field's zero — no pointer wrap per §5.1's table).
//
// A nullable one carries the graph's null in its pointer, the way every
// other nullable column on this path carries isNil. That is where this
// differs from writeShapelessFieldDecode, whose pointer carries a
// missing Props key instead: a record holds a key for every column the
// query projected, so the pointer here has no absence to spend itself
// on, and a pointer that is never nil is a null the caller cannot read.
func writeAnyColumnDecodeIndent(b *strings.Builder, p codegen.Query, f codegen.Row, recordExpr, zero, assignPrefix, assignSuffix, indent, varName string) {
	fmt.Fprintf(b, "%s%s, ok := %s.Get(%q)\n", indent, varName, recordExpr, f.ColumnName)
	fmt.Fprintf(b, "%sif !ok {\n%s\treturn %s, fmt.Errorf(\"%s: decode column %%q: key not found\", %q)\n%s}\n", indent, indent, zero, p.MethodName, f.ColumnName, indent)
	if f.Nullable {
		fmt.Fprintf(b, "%svar %sPtr *%s\n", indent, varName, f.GoType)
		fmt.Fprintf(b, "%sif %s != nil {\n%s\t%sPtr = &%s\n%s}\n", indent, varName, indent, varName, varName, indent)
		b.WriteString(indent)
		b.WriteString(assignPrefix[len(indent):])
		b.WriteString(varName)
		b.WriteString("Ptr")
		b.WriteString(assignSuffix)
		return
	}
	b.WriteString(indent)
	b.WriteString(assignPrefix[len(indent):])
	b.WriteString(varName)
	b.WriteString(assignSuffix)
}

// writeListColumnDecodeIndent emits the list-column arm (spec §5.5):
// neo4j.GetRecordValue[[]any] followed by a per-element loop that
// dispatches on the element type. The loop body is derived by
// walkListElemPlan, which recurses for nested list elements. Nullable
// list column produces *[]T via the standard pointer-wrap.
//
// The accumulator is positional, for the reason writeEntityFieldDecode
// gives about the local a value lands in: this one is declared at the
// row assembly's own indent, so a second list column in the same
// projection redeclares it — `no new variables on left side of :=`, plus
// whatever the two element types disagree about. A nullable list column
// hid that for a while by accident, its accumulator sitting inside the
// block its null gate opens; a second non-nullable one has nowhere to
// hide, and generation would still exit 0 because the format gate only
// parses.
func writeListColumnDecodeIndent(b *strings.Builder, p codegen.Query, f codegen.Row, recordExpr, zero, assignPrefix, assignSuffix, indent, varName string) {
	// varName is "value" for a single-column projection and "valueN" for
	// a row field, so the same suffix numbers the accumulator without
	// renaming the single-column shape spec §5.5 spells out.
	accVar := "acc" + strings.TrimPrefix(varName, "value")
	fmt.Fprintf(b, "%s%s, isNil, err := neo4j.GetRecordValue[[]any](%s, %q)\n", indent, varName, recordExpr, f.ColumnName)
	fmt.Fprintf(b, "%sif err != nil {\n%s\treturn %s, fmt.Errorf(\"%s: decode column %%q: %%w\", %q, err)\n%s}\n", indent, indent, zero, p.MethodName, f.ColumnName, indent)
	if f.Nullable {
		// Nullable list: build a *[]T. Nil pointer on null; otherwise
		// address of the accumulated slice.
		fmt.Fprintf(b, "%svar %sPtr *%s\n", indent, varName, f.GoType)
		fmt.Fprintf(b, "%sif !isNil {\n", indent)
		fmt.Fprintf(b, "%s\t%s := make(%s, 0, len(%s))\n", indent, accVar, f.GoType, varName)
		walkListElemPlan(b, p, f, f.ListElem, accVar, varName, zero, indent+"\t", 0)
		fmt.Fprintf(b, "%s\t%sPtr = &%s\n", indent, varName, accVar)
		fmt.Fprintf(b, "%s}\n", indent)
		b.WriteString(indent)
		b.WriteString(assignPrefix[len(indent):])
		b.WriteString(varName)
		b.WriteString("Ptr")
		b.WriteString(assignSuffix)
		return
	}
	// Non-nullable: error if isNil; else build the accumulator + assign.
	fmt.Fprintf(b, "%sif isNil {\n%s\treturn %s, fmt.Errorf(\"%s: column %%q is non-nullable but arrived null\", %q)\n%s}\n", indent, indent, zero, p.MethodName, f.ColumnName, indent)
	fmt.Fprintf(b, "%s%s := make(%s, 0, len(%s))\n", indent, accVar, f.GoType, varName)
	walkListElemPlan(b, p, f, f.ListElem, accVar, varName, zero, indent, 0)
	b.WriteString(indent)
	b.WriteString(assignPrefix[len(indent):])
	b.WriteString(accVar)
	b.WriteString(assignSuffix)
}

// walkListElemPlan emits the per-element loop for a list column
// (spec §1.3, §5.5). The loop iterates the driver's []any slice one
// element at a time; the body dispatches on the plan's committed Kind
// via walkListElemBody. Every future resolver variant lands as a new
// codegen.ColumnKind arm handled once — prepare's buildListElemPlan and the
// emission switch below both fail to compile until it is handled.
//
// The accumulator name (accVar) accumulates elements at this depth;
// the source slice name (srcVar) is the raw driver []any at this depth.
// depth is the list nesting level this loop iterates, counting the
// column's own elements as 0, and is what elemLocal suffixes by.
func walkListElemPlan(b *strings.Builder, p codegen.Query, f codegen.Row, e *codegen.ListElem, accVar, srcVar, zero, indent string, depth int) {
	// The index variable is only used by the element-type-assertion
	// fail message, so the arms that assert nothing never name it and
	// ranging with `i` would emit an unused variable.
	indexVar := "i"
	if carriesElemBare(e) {
		indexVar = "_"
	}
	iterVar := elemLocal("elem", depth)
	fmt.Fprintf(b, "%sfor %s, %s := range %s {\n", indent, indexVar, iterVar, srcVar)
	walkListElemBody(b, p, f, e, accVar, iterVar, zero, indent+"\t", depth)
	fmt.Fprintf(b, "%s}\n", indent)
}

// elemLocal names a local of the element loop at the given nesting
// depth. A nested list's loop is emitted inside the enclosing list's
// loop body, so under a fixed name the inner declaration shadows the
// enclosing one at the point the emission still appends to it — the
// accumulator, the value appended to it and the inner accumulator become
// one identifier, and the emitted package does not compile from a
// nesting depth of three.
//
// Depth 0 is unsuffixed because spec §5.5 spells out the loop a
// single-level list column emits.
func elemLocal(name string, depth int) string {
	if depth == 0 {
		return name
	}
	return name + strconv.Itoa(depth)
}

// carriesElemBare reports whether the element loop appends what the
// driver handed it without asserting anything about it — which is also
// to say whether the loop body names the index at all. It is consulted
// by walkListElemPlan for the index variable and implemented by
// walkListElemBody, so that a body that stops asserting cannot leave a
// loop head declaring an index nothing reads.
//
// codegen.ColumnAny (Unknown) and codegen.ColumnScalarNull are bare
// because the plan says so. A codegen.ColumnProperty is bare when its
// element rides no driver carrier, for the reason isSliceType gives on
// the entity path: the carrier of an `any` element is `any`, and
// `elem.(any)` is false for a nil interface value — the null a list of
// no declared element shape exists to be allowed to hold. Asserting
// would fail the whole column on exactly that element, while AGE, whose
// agtypeValue maps null to nil, hands the same graph value back intact.
func carriesElemBare(e *codegen.ListElem) bool {
	switch e.Kind {
	case codegen.ColumnAny, codegen.ColumnScalarNull:
		return true
	case codegen.ColumnProperty:
		return !ridesADriverCarrier(e.GoType)
	default:
		return false
	}
}

// walkListElemBody emits the body of one list-element loop iteration
// (spec §5.5). Every arm is a case on the plan's committed codegen.ColumnKind
// — the render layer walks committed data only, never a resolver type.
// accVar is the accumulator to append into at this depth; iterVar is
// the raw `elem` from the driver []any; zero is the enclosing method's
// zero-return expression; indent is already deepened by one level
// relative to the loop head; depth is the loop's own nesting level, so
// the locals a nested list arm declares belong to depth+1.
func walkListElemBody(b *strings.Builder, p codegen.Query, f codegen.Row, e *codegen.ListElem, accVar, iterVar, zero, indent string, depth int) {
	switch e.Kind {
	case codegen.ColumnProperty:
		if carriesElemBare(e) {
			fmt.Fprintf(b, "%s%s = append(%s, %s)\n", indent, accVar, accVar, iterVar)
			return
		}
		carrier := driverCarrier(e.GoType)
		fmt.Fprintf(b, "%sv, ok := %s.(%s)\n", indent, iterVar, carrier)
		fmt.Fprintf(b, "%sif !ok {\n%s\treturn %s, fmt.Errorf(\"%s: decode column %%q element %%d: expected %s, got %%T\", %q, i, %s)\n%s}\n", indent, indent, zero, p.MethodName, carrier, f.ColumnName, iterVar, indent)
		if carrier != e.GoType {
			fmt.Fprintf(b, "%s%s = append(%s, %s)\n", indent, accVar, accVar, narrowExpr(e.GoType, "v"))
		} else {
			fmt.Fprintf(b, "%s%s = append(%s, v)\n", indent, accVar, accVar)
		}
	case codegen.ColumnTemporal:
		// The element arrives as the driver's carrier, which for the
		// four neutral temporal widths is not the emitted element type
		// (ADR 0033): assert against the carrier, then convert.
		elemCarrier := driverCarrier(e.GoType)
		fmt.Fprintf(b, "%sv, ok := %s.(%s)\n", indent, iterVar, elemCarrier)
		fmt.Fprintf(b, "%sif !ok {\n%s\treturn %s, fmt.Errorf(\"%s: decode column %%q element %%d: expected %s, got %%T\", %q, i, %s)\n%s}\n", indent, indent, zero, p.MethodName, elemCarrier, f.ColumnName, iterVar, indent)
		if elemCarrier != e.GoType {
			fmt.Fprintf(b, "%s%s = append(%s, %s)\n", indent, accVar, accVar, narrowExpr(e.GoType, "v"))
		} else {
			fmt.Fprintf(b, "%s%s = append(%s, v)\n", indent, accVar, accVar)
		}
	case codegen.ColumnScalar:
		fmt.Fprintf(b, "%sv, ok := %s.(%s)\n", indent, iterVar, e.GoType)
		fmt.Fprintf(b, "%sif !ok {\n%s\treturn %s, fmt.Errorf(\"%s: decode column %%q element %%d: expected %s, got %%T\", %q, i, %s)\n%s}\n", indent, indent, zero, p.MethodName, e.GoType, f.ColumnName, iterVar, indent)
		fmt.Fprintf(b, "%s%s = append(%s, v)\n", indent, accVar, accVar)
	case codegen.ColumnScalarNull, codegen.ColumnAny:
		fmt.Fprintf(b, "%s%s = append(%s, %s)\n", indent, accVar, accVar, iterVar)
	case codegen.ColumnNode:
		fmt.Fprintf(b, "%snode, ok := %s.(dbtype.Node)\n", indent, iterVar)
		fmt.Fprintf(b, "%sif !ok {\n%s\treturn %s, fmt.Errorf(\"%s: decode column %%q element %%d: expected dbtype.Node, got %%T\", %q, i, %s)\n%s}\n", indent, indent, zero, p.MethodName, f.ColumnName, iterVar, indent)
		fmt.Fprintf(b, "%sdecoded, err := decode%s(node)\n", indent, e.EntityName)
		fmt.Fprintf(b, "%sif err != nil {\n%s\treturn %s, fmt.Errorf(\"%s: decode column %%q element %%d: %%w\", %q, i, err)\n%s}\n", indent, indent, zero, p.MethodName, f.ColumnName, indent)
		fmt.Fprintf(b, "%s%s = append(%s, decoded)\n", indent, accVar, accVar)
	case codegen.ColumnEdge:
		fmt.Fprintf(b, "%srel, ok := %s.(dbtype.Relationship)\n", indent, iterVar)
		fmt.Fprintf(b, "%sif !ok {\n%s\treturn %s, fmt.Errorf(\"%s: decode column %%q element %%d: expected dbtype.Relationship, got %%T\", %q, i, %s)\n%s}\n", indent, indent, zero, p.MethodName, f.ColumnName, iterVar, indent)
		fmt.Fprintf(b, "%sdecoded, err := decode%s(rel)\n", indent, e.EntityName)
		fmt.Fprintf(b, "%sif err != nil {\n%s\treturn %s, fmt.Errorf(\"%s: decode column %%q element %%d: %%w\", %q, i, err)\n%s}\n", indent, indent, zero, p.MethodName, f.ColumnName, indent)
		fmt.Fprintf(b, "%s%s = append(%s, decoded)\n", indent, accVar, accVar)
	case codegen.ColumnEdgeUnion:
		// C5 list-of-edgeUnion element arm (§5.5). Plan carries an
		// index into the owning codegen.Query.EdgeUnions slice; the
		// dispatch keys are the committed EdgeKeys' Labels and the
		// candidates are the committed entity struct names — no
		// re-derivation.
		u := p.EdgeUnions[e.UnionIdx]
		fmt.Fprintf(b, "%srel, ok := %s.(dbtype.Relationship)\n", indent, iterVar)
		fmt.Fprintf(b, "%sif !ok {\n%s\treturn %s, fmt.Errorf(\"%s: decode column %%q element %%d: expected dbtype.Relationship, got %%T\", %q, i, %s)\n%s}\n", indent, indent, zero, p.MethodName, f.ColumnName, iterVar, indent)
		fmt.Fprintf(b, "%sswitch rel.Type {\n", indent)
		for i, ek := range u.EdgeKeys {
			fmt.Fprintf(b, "%scase %q:\n", indent, string(ek.KeyLabels))
			fmt.Fprintf(b, "%s\tentity, err := decode%s(rel)\n", indent, u.Candidates[i])
			fmt.Fprintf(b, "%s\tif err != nil {\n%s\t\treturn %s, fmt.Errorf(\"%s: decode column %%q element %%d: %%w\", %q, i, err)\n%s\t}\n", indent, indent, zero, p.MethodName, f.ColumnName, indent)
			fmt.Fprintf(b, "%s\t%s = append(%s, entity)\n", indent, accVar, accVar)
		}
		fmt.Fprintf(b, "%sdefault:\n%s\treturn %s, fmt.Errorf(\"%s: decode column %%q element %%d: unexpected relationship type %%q\", %q, i, rel.Type)\n%s}\n", indent, indent, zero, p.MethodName, f.ColumnName, indent)
	case codegen.ColumnList:
		// Nested list: type-assert to []any, then recurse.
		inner := elemLocal("inner", depth+1)
		innerAcc := elemLocal("innerAcc", depth+1)
		fmt.Fprintf(b, "%s%s, ok := %s.([]any)\n", indent, inner, iterVar)
		fmt.Fprintf(b, "%sif !ok {\n%s\treturn %s, fmt.Errorf(\"%s: decode column %%q element %%d: expected []any, got %%T\", %q, i, %s)\n%s}\n", indent, indent, zero, p.MethodName, f.ColumnName, iterVar, indent)
		fmt.Fprintf(b, "%s%s := make(%s, 0, len(%s))\n", indent, innerAcc, e.GoType, inner)
		walkListElemPlan(b, p, f, e.Nested, innerAcc, inner, zero, indent, depth+1)
		fmt.Fprintf(b, "%s%s = append(%s, %s)\n", indent, accVar, accVar, innerAcc)
	}
}

// writeEdgeUnionColumnDecodeIndent emits the edgeUnion-column arm of
// the row assembly (spec §5.5, C5). The column is decoded via
// record.Get returning (any, bool), then type-asserted to
// dbtype.Relationship, then dispatched through a type-switch on
// rel.Type in resolver-canonical EdgeKeys order. Each case calls the
// entity's decode<Name> helper and either returns the entity
// (single-column :one), appends to out (single-column :many), or
// assigns to the Row field (multi-column). Nullable columns skip the
// raw==nil non-null gate and let the nil interface propagate as the
// natural absence value (§3.3, ADR 0010 D3 Resolved lines 343–345).
//
// Get is not forced here: neo4j.RecordValue admits Relationship, so
// neo4j.GetRecordValue[dbtype.Relationship] instantiates — it is what
// writeEntityColumnDecodeIndent below binds for an edge column. Taking
// the untyped any instead is what keeps a missing key and a wrong
// dynamic type as this arm's own refusals rather than as wrapped
// driver errors, and it is the same honest-any carrier
// writeAnyColumnDecodeIndent takes.
func writeEdgeUnionColumnDecodeIndent(b *strings.Builder, p codegen.Query, f codegen.Row, recordExpr, zero, assignPrefix, assignSuffix, indent, varName string) {
	// Distinct per-column locals for the raw / rel bindings so
	// multi-column Row-assembly bodies never shadow. Single-column
	// projections keep the bare "raw" / "rel" locals matching spec
	// §5.5's snippets.
	rawLocal := "raw"
	relLocal := "rel"
	okLocal := "ok"
	entityLocal := "entity"
	if len(p.RowFields) > 1 {
		suffix := strings.TrimPrefix(varName, "value")
		rawLocal = "raw" + suffix
		relLocal = "rel" + suffix
		okLocal = "ok" + suffix
		entityLocal = "entity" + suffix
	}
	// assignBody carries the caller-supplied `<prefix><value><suffix>`
	// pattern with the outer indent already baked in. Every arm of the
	// dispatch appends one such body at `indent + extraIndent`.
	assignBody := func(extraIndent, valueExpr string) {
		b.WriteString(indent)
		b.WriteString(extraIndent)
		b.WriteString(assignPrefix[len(indent):])
		b.WriteString(valueExpr)
		b.WriteString(assignSuffix)
	}
	fmt.Fprintf(b, "%s%s, %s := %s.Get(%q)\n", indent, rawLocal, okLocal, recordExpr, f.ColumnName)
	fmt.Fprintf(b, "%sif !%s {\n%s\treturn %s, fmt.Errorf(\"%s: column %%q missing from record\", %q)\n%s}\n", indent, okLocal, indent, zero, p.MethodName, f.ColumnName, indent)
	if f.Nullable {
		// Nullable: nil raw propagates as the nil interface value. The
		// dispatch body sits inside an `else` block, indented one tab
		// deeper than the caller's baseline.
		fmt.Fprintf(b, "%sif %s == nil {\n", indent, rawLocal)
		assignBody("\t", "nil")
		fmt.Fprintf(b, "%s} else {\n", indent)
		writeEdgeUnionDispatchBody(b, p, f, rawLocal, relLocal, okLocal, entityLocal, zero, assignBody, indent, "\t")
		fmt.Fprintf(b, "%s}\n", indent)
		return
	}
	// Non-nullable: nil raw is a decode error.
	fmt.Fprintf(b, "%sif %s == nil {\n%s\treturn %s, fmt.Errorf(\"%s: column %%q is non-nullable but arrived null\", %q)\n%s}\n", indent, rawLocal, indent, zero, p.MethodName, f.ColumnName, indent)
	writeEdgeUnionDispatchBody(b, p, f, rawLocal, relLocal, okLocal, entityLocal, zero, assignBody, indent, "")
}

// writeEdgeUnionDispatchBody emits the type-assert + type-switch
// dispatch that owns the edgeUnion column's decode arm. Factored out
// so the nullable arm can reuse the same body inside an `else` branch
// (skipping the non-null raw gate). The dispatch keys are EdgeKey.KeyLabels
// strings — the driver's wire labels — not the mangled entity struct
// names. assignBody writes one `<indent><extraIndent><prefix><value><suffix>`
// assignment line; the callback keeps the raw assignPrefix / assignSuffix
// out of the dispatch-body inner loop so the indent arithmetic is done
// in exactly one place.
func writeEdgeUnionDispatchBody(b *strings.Builder, p codegen.Query, f codegen.Row, rawLocal, relLocal, okLocal, entityLocal, zero string, assignBody func(extraIndent, valueExpr string), indent, extraIndent string) {
	dispatchIndent := indent + extraIndent
	fmt.Fprintf(b, "%s%s, %s := %s.(dbtype.Relationship)\n", dispatchIndent, relLocal, okLocal, rawLocal)
	fmt.Fprintf(b, "%sif !%s {\n%s\treturn %s, fmt.Errorf(\"%s: column %%q: expected dbtype.Relationship, got %%T\", %q, %s)\n%s}\n", dispatchIndent, okLocal, dispatchIndent, zero, p.MethodName, f.ColumnName, rawLocal, dispatchIndent)
	fmt.Fprintf(b, "%sswitch %s.Type {\n", dispatchIndent, relLocal)
	for i, ek := range f.EdgeKeys {
		entityName := edgeKeyToEntityName(p, f, i)
		fmt.Fprintf(b, "%scase %q:\n", dispatchIndent, string(ek.KeyLabels))
		fmt.Fprintf(b, "%s\t%s, err := decode%s(%s)\n", dispatchIndent, entityLocal, entityName, relLocal)
		fmt.Fprintf(b, "%s\tif err != nil {\n%s\t\treturn %s, fmt.Errorf(\"%s: decode column %%q: %%w\", %q, err)\n%s\t}\n", dispatchIndent, dispatchIndent, zero, p.MethodName, f.ColumnName, dispatchIndent)
		assignBody(extraIndent+"\t", entityLocal)
	}
	fmt.Fprintf(b, "%sdefault:\n%s\treturn %s, fmt.Errorf(\"%s: column %%q: unexpected relationship type %%q\", %q, %s.Type)\n%s}\n", dispatchIndent, dispatchIndent, zero, p.MethodName, f.ColumnName, relLocal, dispatchIndent)
}

// edgeKeyToEntityName resolves an EdgeKey position in a codegen.Row's
// EdgeKeys slice to the emitted entity struct name. The lookup walks
// the owning query's codegen.EdgeUnion entries, matching on ColumnName
// (unique per query), then indexes Candidates by the position. Every
// call site has a Phase B guarantee that the row's edgeUnion entry
// exists.
func edgeKeyToEntityName(p codegen.Query, f codegen.Row, i int) string {
	for _, u := range p.EdgeUnions {
		if u.ColumnName == f.ColumnName && u.FieldName == f.Field {
			return u.Candidates[i]
		}
	}
	// Unreachable: Phase B guarantees a matching codegen.EdgeUnion for
	// every codegen.ColumnEdgeUnion Row field. Returning the bare label keeps
	// the emission textually distinct so a regression surfaces at the
	// nested-module compile fence rather than silently miscompiling.
	return string(f.EdgeKeys[i].KeyLabels)
}

// writeEntityColumnDecodeIndent emits the entity-column arm of the row
// assembly (spec §5.5). Carrier is dbtype.Node for node columns, dbtype.
// Relationship for edge columns; the decode helper takes the driver
// value and returns the entity struct. Nullable columns produce a
// *EntityName pointer field via a local +address-of; non-nullable
// columns are a decode error when the driver value arrived null.
func writeEntityColumnDecodeIndent(b *strings.Builder, p codegen.Query, f codegen.Row, recordExpr, zero, assignPrefix, assignSuffix, indent, varName string) {
	var carrier, decodeArg string
	if f.Kind == codegen.ColumnNode {
		carrier = "dbtype.Node"
		decodeArg = "node"
	} else {
		carrier = "dbtype.Relationship"
		decodeArg = "rel"
	}
	// Distinct local names per column position (numbered suffix) avoid
	// shadowing in multi-column rows; single-column projections use the
	// bare carrier local ("node" / "rel"), matching spec §5.5's shape.
	local := decodeArg
	if len(p.RowFields) > 1 {
		// varName is "value0", "value1", …; give the carrier a matching
		// numeric suffix so multi-column rows never shadow.
		suffix := strings.TrimPrefix(varName, "value")
		local = decodeArg + suffix
	}
	fmt.Fprintf(b, "%s%s, isNil, err := neo4j.GetRecordValue[%s](%s, %q)\n", indent, local, carrier, recordExpr, f.ColumnName)
	fmt.Fprintf(b, "%sif err != nil {\n%s\treturn %s, fmt.Errorf(\"%s: decode column %%q: %%w\", %q, err)\n%s}\n", indent, indent, zero, p.MethodName, f.ColumnName, indent)
	if f.Nullable {
		fmt.Fprintf(b, "%svar %sPtr *%s\n", indent, varName, f.GoType)
		fmt.Fprintf(b, "%sif !isNil {\n", indent)
		fmt.Fprintf(b, "%s\tv, err := decode%s(%s)\n", indent, f.GoType, local)
		fmt.Fprintf(b, "%s\tif err != nil {\n%s\t\treturn %s, fmt.Errorf(\"%s: decode column %%q: %%w\", %q, err)\n%s\t}\n", indent, indent, zero, p.MethodName, f.ColumnName, indent)
		fmt.Fprintf(b, "%s\t%sPtr = &v\n", indent, varName)
		fmt.Fprintf(b, "%s}\n", indent)
		b.WriteString(indent)
		b.WriteString(assignPrefix[len(indent):])
		b.WriteString(varName)
		b.WriteString("Ptr")
		b.WriteString(assignSuffix)
		return
	}
	fmt.Fprintf(b, "%sif isNil {\n%s\treturn %s, fmt.Errorf(\"%s: column %%q is non-nullable but arrived null\", %q)\n%s}\n", indent, indent, zero, p.MethodName, f.ColumnName, indent)
	fmt.Fprintf(b, "%s%s, err := decode%s(%s)\n", indent, varName, f.GoType, local)
	fmt.Fprintf(b, "%sif err != nil {\n%s\treturn %s, fmt.Errorf(\"%s: decode column %%q: %%w\", %q, err)\n%s}\n", indent, indent, zero, p.MethodName, f.ColumnName, indent)
	b.WriteString(indent)
	b.WriteString(assignPrefix[len(indent):])
	b.WriteString(varName)
	b.WriteString(assignSuffix)
}
