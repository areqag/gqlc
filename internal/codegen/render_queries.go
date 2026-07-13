package codegen

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/areqag/gqlc/internal/resolver"
	"github.com/areqag/gqlc/internal/schema"
)

// sourceGroup carries one <name>.cypher.go file's worth of prepared
// queries in emission order. Grouping is by SourceFile basename minus
// extension, in first-appearance order.
type sourceGroup struct {
	filename string
	queries  []preparedQuery
}

// groupBySource groups prepared queries by SourceFile basename in
// first-appearance order (spec §5.5). A query with no SourceFile is
// unreachable at C1 (queryfile always records one) but defensively
// grouped under "queries" so the emission is uniform.
func groupBySource(prepared []preparedQuery) []sourceGroup {
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
			queries:  []preparedQuery{p},
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
func groupImports(queries []preparedQuery) (needDbtype, needTime, needFmt bool) {
	for _, p := range queries {
		if p.Cardinality != CardinalityExec {
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

// renderCypherFile emits one <name>.cypher.go file (spec §5.5). Per
// query in order: query-text const, Params struct (if any), Row struct
// (if any), method. The withDbtype flag toggles the dbtype import; the
// withTime flag toggles the time-stdlib import (C3, for TIMESTAMP /
// TemporalDateTime carriers). The withFmt flag toggles the fmt import
// (C4: a write-only file whose queries are all :exec emits no
// fmt.Errorf wrapper, so fmt is elided). The row-assembly template
// inlines the per-kind decode arm.
func renderCypherFile(pkg string, queries []preparedQuery, withDbtype, withTime, withFmt bool, target driverTarget) []byte {
	var b strings.Builder
	b.WriteString(header())
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
		fmt.Fprintf(&b, "const %sQueryText = `%s`\n\n", p.Bare, p.SourceText)
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
				if f.Nullable && f.Kind != columnEdgeUnion {
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
func writeMethodSignature(b *strings.Builder, p preparedQuery) {
	b.WriteString(p.MethodName)
	b.WriteString("(ctx context.Context")
	switch len(p.ParamFields) {
	case 0:
		// bare arg
	case 1:
		fmt.Fprintf(b, ", %s ", lowerFirstRune(p.ParamFields[0].Field))
		if p.ParamFields[0].Nullable {
			b.WriteString("*")
		}
		b.WriteString(p.ParamFields[0].GoType)
	default:
		fmt.Fprintf(b, ", arg %sParams", p.MethodName)
	}
	if p.Cardinality == CardinalityExec {
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
func returnTypeText(p preparedQuery) string {
	var elem string
	if len(p.RowFields) == 1 {
		elem = ""
		// Nullable columns wrap the emitted Go type in a pointer, EXCEPT
		// edgeUnion columns whose emitted type is a sealed interface —
		// nil is the natural absence value for an interface, and
		// pointer-to-interface is the Go anti-pattern ADR 0010 D3
		// Resolved (lines 343–345) forbids (§3.3).
		if p.RowFields[0].Nullable && p.RowFields[0].Kind != columnEdgeUnion {
			elem = "*"
		}
		elem += p.RowFields[0].GoType
	} else {
		elem = p.MethodName + "Row"
	}
	if p.Cardinality == CardinalityMany {
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
func zeroValueText(p preparedQuery) string {
	if p.Cardinality == CardinalityMany {
		return "nil"
	}
	if len(p.RowFields) == 1 {
		f := p.RowFields[0]
		if f.Nullable {
			return "nil"
		}
		switch f.Kind {
		case columnNode, columnEdge:
			return f.GoType + "{}"
		case columnTemporal:
			return f.GoType + "{}"
		case columnList:
			return "nil"
		case columnAny, columnEdgeUnion:
			// edgeUnion single-column return type is the interface; its
			// zero value is nil (§3.1 / §5.5).
			return "nil"
		case columnProperty, columnScalar:
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
		default:
			return "0"
		}
	}
	return p.MethodName + "Row{}"
}

// writeMethod writes the method definition + body (spec §5.3 / §5.5).
// C4 adds the :exec arm: three-line body (run, discard rows, return
// error) with no Row-struct decoding.
func writeMethod(b *strings.Builder, p preparedQuery) {
	// Doc comment: first 3 lines of query text, prefixed "//   ".
	writeDocComment(b, p)
	b.WriteString("func (q *Queries) ")
	writeMethodSignature(b, p)
	b.WriteString(" {\n")

	if p.Cardinality == CardinalityExec {
		fmt.Fprintf(b, "\t_, err := q.db.run(ctx, %sQueryText, %s, %s)\n", p.Bare, paramsMapText(p), accessModeText(p.AccessMode))
		b.WriteString("\treturn err\n")
		b.WriteString("}\n")
		return
	}

	// Body: build the params map, call run, decode.
	writeRunCall(b, p)

	if p.Cardinality == CardinalityOne {
		writeOneBody(b, p)
	} else {
		writeManyBody(b, p)
	}
	b.WriteString("}\n")
}

// accessModeText picks the fourth q.db.run argument from the prepare-
// side closed enum (spec §1.1). Dispatch is on committed data —
// preparedQuery.AccessMode — never on Validated.Statement.
func accessModeText(m accessMode) string {
	if m == accessModeWrite {
		return "neo4j.AccessModeWrite"
	}
	return "neo4j.AccessModeRead"
}

// writeDocComment emits the per-method doc comment: the method name
// and the first 3 lines of the query text, prefixed //   .
func writeDocComment(b *strings.Builder, p preparedQuery) {
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
func writeRunCall(b *strings.Builder, p preparedQuery) {
	fmt.Fprintf(b, "\trecords, err := q.db.run(ctx, %sQueryText, %s, %s)\n", p.Bare, paramsMapText(p), accessModeText(p.AccessMode))
	fmt.Fprintf(b, "\tif err != nil {\n\t\treturn %s, err\n\t}\n", zeroValueText(p))
}

// paramsMapText composes the driver-binding map literal. C3 extends
// the per-field expression with the FLOAT32 encode-widen contract:
// map[string]any{"x": float64(x)} for a float32 parameter, symmetric
// with the decode-narrow site (§5.5). Narrow-integer parameters keep
// the widen pattern (int64(v)) — the driver accepts the wider carrier.
// Nullable parameters go through binParamExpr, which handles the
// nil-pointer case by binding a bare nil literal.
func paramsMapText(p preparedQuery) string {
	if len(p.ParamFields) == 0 {
		return "nil"
	}
	var b strings.Builder
	b.WriteString("map[string]any{")
	for i, f := range p.ParamFields {
		if i > 0 {
			b.WriteString(", ")
		}
		var access string
		if len(p.ParamFields) == 1 {
			access = lowerFirstRune(f.Field)
		} else {
			access = "arg." + f.Field
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
func paramBindExpr(f preparedParam, access string) string {
	if f.Nullable {
		// Uniform: pass the pointer through as-is. A nil pointer binds
		// Cypher null via the driver's parameter marshalling.
		return access
	}
	carrier := driverCarrier(f.GoType)
	if carrier != f.GoType {
		return fmt.Sprintf("%s(%s)", carrier, access)
	}
	return access
}

// writeOneBody emits the :one arity-check + per-column decode + return.
func writeOneBody(b *strings.Builder, p preparedQuery) {
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
func writeManyBody(b *strings.Builder, p preparedQuery) {
	var elem string
	if len(p.RowFields) == 1 {
		// EdgeUnion columns emit the bare interface, never
		// pointer-to-interface — even when nullable (§3.3).
		if p.RowFields[0].Nullable && p.RowFields[0].Kind != columnEdgeUnion {
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
func writeSingleColumnDecode(b *strings.Builder, p preparedQuery, f preparedRow, recordExpr, zero, assignPrefix, assignSuffix string) {
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
func writeSingleColumnDecodeIndent(b *strings.Builder, p preparedQuery, f preparedRow, recordExpr, zero, assignPrefix, assignSuffix, indent string) {
	varName := "value"
	if len(p.RowFields) > 1 {
		for i, r := range p.RowFields {
			if r.ColumnName == f.ColumnName && r.Field == f.Field {
				varName = fmt.Sprintf("value%d", i)
				break
			}
		}
	}
	switch f.Kind {
	case columnNode, columnEdge:
		writeEntityColumnDecodeIndent(b, p, f, recordExpr, zero, assignPrefix, assignSuffix, indent, varName)
		return
	case columnAny:
		writeAnyColumnDecodeIndent(b, p, f, recordExpr, zero, assignPrefix, assignSuffix, indent, varName)
		return
	case columnList:
		writeListColumnDecodeIndent(b, p, f, recordExpr, zero, assignPrefix, assignSuffix, indent, varName)
		return
	case columnEdgeUnion:
		writeEdgeUnionColumnDecodeIndent(b, p, f, recordExpr, zero, assignPrefix, assignSuffix, indent, varName)
		return
	case columnProperty, columnTemporal, columnScalar:
		// Fall through to the GetRecordValue + narrow-convert path below.
	}
	// columnProperty / columnTemporal / columnScalar all use GetRecordValue
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
		valueExpr = fmt.Sprintf("%s(%s)", f.GoType, varName)
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

// writeAnyColumnDecodeIndent emits the record.Get lane for a column
// whose emitted Go type is `any` — ResolvedUnknown or ResolvedScalar
// {Null} (spec §5.5). The driver's Get returns (any, bool) where bool
// is "found" (not "null"). The "not-found" branch is a decode error
// (the resolver committed the column, so the driver must produce it);
// the "found" branch assigns the value verbatim (a nil value satisfies
// the `any` field's zero — no pointer wrap per §5.1's table).
func writeAnyColumnDecodeIndent(b *strings.Builder, p preparedQuery, f preparedRow, recordExpr, zero, assignPrefix, assignSuffix, indent, varName string) {
	fmt.Fprintf(b, "%s%s, ok := %s.Get(%q)\n", indent, varName, recordExpr, f.ColumnName)
	fmt.Fprintf(b, "%sif !ok {\n%s\treturn %s, fmt.Errorf(\"%s: decode column %%q: key not found\", %q)\n%s}\n", indent, indent, zero, p.MethodName, f.ColumnName, indent)
	b.WriteString(indent)
	b.WriteString(assignPrefix[len(indent):])
	b.WriteString(varName)
	b.WriteString(assignSuffix)
}

// writeListColumnDecodeIndent emits the list-column arm (spec §5.5):
// neo4j.GetRecordValue[[]any] followed by a per-element loop that
// dispatches on the element type. The loop body is derived by
// writeListElementDecode, which recurses for nested list elements.
// Nullable list column produces *[]T via the standard pointer-wrap.
func writeListColumnDecodeIndent(b *strings.Builder, p preparedQuery, f preparedRow, recordExpr, zero, assignPrefix, assignSuffix, indent, varName string) {
	fmt.Fprintf(b, "%s%s, isNil, err := neo4j.GetRecordValue[[]any](%s, %q)\n", indent, varName, recordExpr, f.ColumnName)
	fmt.Fprintf(b, "%sif err != nil {\n%s\treturn %s, fmt.Errorf(\"%s: decode column %%q: %%w\", %q, err)\n%s}\n", indent, indent, zero, p.MethodName, f.ColumnName, indent)
	// GoType is "[]<inner>"; strip the leading "[]" to get the slice
	// element Go type.
	elemGoType := strings.TrimPrefix(f.GoType, "[]")
	if f.Nullable {
		// Nullable list: build a *[]T. Nil pointer on null; otherwise
		// address of the accumulated slice.
		fmt.Fprintf(b, "%svar %sPtr *%s\n", indent, varName, f.GoType)
		fmt.Fprintf(b, "%sif !isNil {\n", indent)
		fmt.Fprintf(b, "%s\tacc := make(%s, 0, len(%s))\n", indent, f.GoType, varName)
		writeListElementDecode(b, p, f, f.ListElem, elemGoType, "acc", varName, zero, indent+"\t")
		fmt.Fprintf(b, "%s\t%sPtr = &acc\n", indent, varName)
		fmt.Fprintf(b, "%s}\n", indent)
		b.WriteString(indent)
		b.WriteString(assignPrefix[len(indent):])
		b.WriteString(varName)
		b.WriteString("Ptr")
		b.WriteString(assignSuffix)
		return
	}
	// Non-nullable: error if isNil; else build acc slice + assign.
	fmt.Fprintf(b, "%sif isNil {\n%s\treturn %s, fmt.Errorf(\"%s: column %%q is non-nullable but arrived null\", %q)\n%s}\n", indent, indent, zero, p.MethodName, f.ColumnName, indent)
	fmt.Fprintf(b, "%sacc := make(%s, 0, len(%s))\n", indent, f.GoType, varName)
	writeListElementDecode(b, p, f, f.ListElem, elemGoType, "acc", varName, zero, indent)
	b.WriteString(indent)
	b.WriteString(assignPrefix[len(indent):])
	b.WriteString("acc")
	b.WriteString(assignSuffix)
}

// writeListElementDecode emits the per-element loop for a list column
// (spec §5.5). The loop iterates the driver's []any slice one element
// at a time; the body dispatches on the element's ResolvedType:
//
//   - ResolvedProperty leaf → type-assert the driver carrier + narrow
//   - ResolvedTemporal leaf → type-assert dbtype.<Kind> / time.Time
//   - ResolvedScalar leaf → type-assert the carrier (map is a legit assert)
//   - ResolvedUnknown / ScalarNull leaf → append elem directly (any)
//   - ResolvedNode / ResolvedEdge leaf → type-assert dbtype.Node /
//     Relationship + decode<EntityName> helper call
//   - Nested ResolvedList → recurse with a new inner loop
//
// The accumulator name (accVar) accumulates elements at this depth;
// the source slice name (srcVar) is the raw driver []any at this depth.
func writeListElementDecode(b *strings.Builder, p preparedQuery, f preparedRow, elem resolver.ResolvedType, elemGoType, accVar, srcVar, zero, indent string) {
	iterVar := "elem"
	if strings.Contains(indent, "\t\t\t\t") { // three levels deep — disambiguate
		iterVar = "elem" + fmt.Sprint(strings.Count(indent, "\t"))
	}
	// The index variable is only used by the element-type-assertion fail
	// message; a bare-append arm (ResolvedUnknown / ScalarNull) does not
	// use i. Suppress the unused-var warning by ranging with `_` when the
	// element decode is one of those two arms.
	indexVar := "i"
	if listElemUsesBareAppend(elem) {
		indexVar = "_"
	}
	fmt.Fprintf(b, "%sfor %s, %s := range %s {\n", indent, indexVar, iterVar, srcVar)
	inner := indent + "\t"
	writeListElementBody(b, p, f, elem, elemGoType, accVar, iterVar, zero, inner)
	fmt.Fprintf(b, "%s}\n", indent)
}

// listElemUsesBareAppend reports whether the list-element decode arm
// for elem emits a bare `acc = append(acc, elem)` (no type assertion,
// no error path). Applies to ResolvedUnknown and ResolvedScalar{Null} —
// both surface `any` at the leaf.
func listElemUsesBareAppend(elem resolver.ResolvedType) bool {
	switch tt := elem.(type) {
	case resolver.ResolvedUnknown:
		return true
	case resolver.ResolvedScalar:
		return tt.Kind == resolver.ScalarNull
	}
	return false
}

// writeListElementBody emits the body of one list-element loop
// iteration. Called by writeListElementDecode with the element's
// resolved type, its emitted Go type, the accumulator name (into which
// the decoded element is appended), the loop variable name (the raw
// `elem` from the driver []any), the enclosing method's zero-return
// expression, and the current indent (already deepened by one level
// relative to the loop head).
func writeListElementBody(b *strings.Builder, p preparedQuery, f preparedRow, elem resolver.ResolvedType, elemGoType, accVar, iterVar, zero, indent string) {
	switch tt := elem.(type) {
	case resolver.ResolvedProperty:
		carrier := driverCarrier(elemGoType)
		fmt.Fprintf(b, "%sv, ok := %s.(%s)\n", indent, iterVar, carrier)
		fmt.Fprintf(b, "%sif !ok {\n%s\treturn %s, fmt.Errorf(\"%s: decode column %%q element %%d: expected %s, got %%T\", %q, i, %s)\n%s}\n", indent, indent, zero, p.MethodName, carrier, f.ColumnName, iterVar, indent)
		if carrier != elemGoType {
			fmt.Fprintf(b, "%s%s = append(%s, %s(v))\n", indent, accVar, accVar, elemGoType)
		} else {
			fmt.Fprintf(b, "%s%s = append(%s, v)\n", indent, accVar, accVar)
		}
	case resolver.ResolvedTemporal:
		fmt.Fprintf(b, "%sv, ok := %s.(%s)\n", indent, iterVar, elemGoType)
		fmt.Fprintf(b, "%sif !ok {\n%s\treturn %s, fmt.Errorf(\"%s: decode column %%q element %%d: expected %s, got %%T\", %q, i, %s)\n%s}\n", indent, indent, zero, p.MethodName, elemGoType, f.ColumnName, iterVar, indent)
		fmt.Fprintf(b, "%s%s = append(%s, v)\n", indent, accVar, accVar)
	case resolver.ResolvedScalar:
		if tt.Kind == resolver.ScalarNull {
			// Bare append — the element is `any`.
			fmt.Fprintf(b, "%s%s = append(%s, %s)\n", indent, accVar, accVar, iterVar)
			return
		}
		fmt.Fprintf(b, "%sv, ok := %s.(%s)\n", indent, iterVar, elemGoType)
		fmt.Fprintf(b, "%sif !ok {\n%s\treturn %s, fmt.Errorf(\"%s: decode column %%q element %%d: expected %s, got %%T\", %q, i, %s)\n%s}\n", indent, indent, zero, p.MethodName, elemGoType, f.ColumnName, iterVar, indent)
		fmt.Fprintf(b, "%s%s = append(%s, v)\n", indent, accVar, accVar)
	case resolver.ResolvedUnknown:
		fmt.Fprintf(b, "%s%s = append(%s, %s)\n", indent, accVar, accVar, iterVar)
	case resolver.ResolvedNode:
		fmt.Fprintf(b, "%snode, ok := %s.(dbtype.Node)\n", indent, iterVar)
		fmt.Fprintf(b, "%sif !ok {\n%s\treturn %s, fmt.Errorf(\"%s: decode column %%q element %%d: expected dbtype.Node, got %%T\", %q, i, %s)\n%s}\n", indent, indent, zero, p.MethodName, f.ColumnName, iterVar, indent)
		fmt.Fprintf(b, "%sdecoded, err := decode%s(node)\n", indent, elemGoType)
		fmt.Fprintf(b, "%sif err != nil {\n%s\treturn %s, fmt.Errorf(\"%s: decode column %%q element %%d: %%w\", %q, i, err)\n%s}\n", indent, indent, zero, p.MethodName, f.ColumnName, indent)
		fmt.Fprintf(b, "%s%s = append(%s, decoded)\n", indent, accVar, accVar)
	case resolver.ResolvedEdge:
		fmt.Fprintf(b, "%srel, ok := %s.(dbtype.Relationship)\n", indent, iterVar)
		fmt.Fprintf(b, "%sif !ok {\n%s\treturn %s, fmt.Errorf(\"%s: decode column %%q element %%d: expected dbtype.Relationship, got %%T\", %q, i, %s)\n%s}\n", indent, indent, zero, p.MethodName, f.ColumnName, iterVar, indent)
		fmt.Fprintf(b, "%sdecoded, err := decode%s(rel)\n", indent, elemGoType)
		fmt.Fprintf(b, "%sif err != nil {\n%s\treturn %s, fmt.Errorf(\"%s: decode column %%q element %%d: %%w\", %q, i, err)\n%s}\n", indent, indent, zero, p.MethodName, f.ColumnName, indent)
		fmt.Fprintf(b, "%s%s = append(%s, decoded)\n", indent, accVar, accVar)
	case resolver.ResolvedEdgeUnion:
		// C5 list-of-edgeUnion element arm (§5.5). The element type is
		// the sealed interface (elemGoType); dispatch on rel.Type in
		// EdgeKeys slice order, matching the top-level column's
		// preparedEdgeUnion candidates positionally.
		candidates := findEdgeUnionCandidates(p, f, tt.EdgeKeys)
		fmt.Fprintf(b, "%srel, ok := %s.(dbtype.Relationship)\n", indent, iterVar)
		fmt.Fprintf(b, "%sif !ok {\n%s\treturn %s, fmt.Errorf(\"%s: decode column %%q element %%d: expected dbtype.Relationship, got %%T\", %q, i, %s)\n%s}\n", indent, indent, zero, p.MethodName, f.ColumnName, iterVar, indent)
		fmt.Fprintf(b, "%sswitch rel.Type {\n", indent)
		for i, ek := range tt.EdgeKeys {
			fmt.Fprintf(b, "%scase %q:\n", indent, string(ek.Label))
			fmt.Fprintf(b, "%s\tentity, err := decode%s(rel)\n", indent, candidates[i])
			fmt.Fprintf(b, "%s\tif err != nil {\n%s\t\treturn %s, fmt.Errorf(\"%s: decode column %%q element %%d: %%w\", %q, i, err)\n%s\t}\n", indent, indent, zero, p.MethodName, f.ColumnName, indent)
			fmt.Fprintf(b, "%s\t%s = append(%s, entity)\n", indent, accVar, accVar)
		}
		fmt.Fprintf(b, "%sdefault:\n%s\treturn %s, fmt.Errorf(\"%s: decode column %%q element %%d: unexpected relationship type %%q\", %q, i, rel.Type)\n%s}\n", indent, indent, zero, p.MethodName, f.ColumnName, indent)
	case resolver.ResolvedList:
		// Nested list: type-assert to []any, then recurse.
		innerGoType := strings.TrimPrefix(elemGoType, "[]")
		fmt.Fprintf(b, "%sinner, ok := %s.([]any)\n", indent, iterVar)
		fmt.Fprintf(b, "%sif !ok {\n%s\treturn %s, fmt.Errorf(\"%s: decode column %%q element %%d: expected []any, got %%T\", %q, i, %s)\n%s}\n", indent, indent, zero, p.MethodName, f.ColumnName, iterVar, indent)
		fmt.Fprintf(b, "%sinnerAcc := make(%s, 0, len(inner))\n", indent, elemGoType)
		writeListElementDecode(b, p, f, tt.Element, innerGoType, "innerAcc", "inner", zero, indent)
		fmt.Fprintf(b, "%s%s = append(%s, innerAcc)\n", indent, accVar, accVar)
	}
}

// writeEdgeUnionColumnDecodeIndent emits the edgeUnion-column arm of
// the row assembly (spec §5.5, C5). The column is decoded via
// record.Get returning (any, bool) — no neo4j.GetRecordValue[T]
// overload exists for a dbtype.Relationship-or-nil result — then
// type-asserted to dbtype.Relationship, then dispatched through a
// type-switch on rel.Type in resolver-canonical EdgeKeys order. Each
// case calls the entity's decode<Name> helper and either returns the
// entity (single-column :one), appends to out (single-column :many),
// or assigns to the Row field (multi-column). Nullable columns skip
// the raw==nil non-null gate and let the nil interface propagate as
// the natural absence value (§3.3, ADR 0010 D3 Resolved lines 343–345).
func writeEdgeUnionColumnDecodeIndent(b *strings.Builder, p preparedQuery, f preparedRow, recordExpr, zero, assignPrefix, assignSuffix, indent, varName string) {
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
// (skipping the non-null raw gate). The dispatch keys are EdgeKey.Label
// strings — the driver's wire labels — not the mangled entity struct
// names. assignBody writes one `<indent><extraIndent><prefix><value><suffix>`
// assignment line; the callback keeps the raw assignPrefix / assignSuffix
// out of the dispatch-body inner loop so the indent arithmetic is done
// in exactly one place.
func writeEdgeUnionDispatchBody(b *strings.Builder, p preparedQuery, f preparedRow, rawLocal, relLocal, okLocal, entityLocal, zero string, assignBody func(extraIndent, valueExpr string), indent, extraIndent string) {
	dispatchIndent := indent + extraIndent
	fmt.Fprintf(b, "%s%s, %s := %s.(dbtype.Relationship)\n", dispatchIndent, relLocal, okLocal, rawLocal)
	fmt.Fprintf(b, "%sif !%s {\n%s\treturn %s, fmt.Errorf(\"%s: column %%q: expected dbtype.Relationship, got %%T\", %q, %s)\n%s}\n", dispatchIndent, okLocal, dispatchIndent, zero, p.MethodName, f.ColumnName, rawLocal, dispatchIndent)
	fmt.Fprintf(b, "%sswitch %s.Type {\n", dispatchIndent, relLocal)
	for i, ek := range f.EdgeKeys {
		entityName := edgeKeyToEntityName(p, f, i)
		fmt.Fprintf(b, "%scase %q:\n", dispatchIndent, string(ek.Label))
		fmt.Fprintf(b, "%s\t%s, err := decode%s(%s)\n", dispatchIndent, entityLocal, entityName, relLocal)
		fmt.Fprintf(b, "%s\tif err != nil {\n%s\t\treturn %s, fmt.Errorf(\"%s: decode column %%q: %%w\", %q, err)\n%s\t}\n", dispatchIndent, dispatchIndent, zero, p.MethodName, f.ColumnName, dispatchIndent)
		assignBody(extraIndent+"\t", entityLocal)
	}
	fmt.Fprintf(b, "%sdefault:\n%s\treturn %s, fmt.Errorf(\"%s: column %%q: unexpected relationship type %%q\", %q, %s.Type)\n%s}\n", dispatchIndent, dispatchIndent, zero, p.MethodName, f.ColumnName, relLocal, dispatchIndent)
}

// edgeKeyToEntityName resolves an EdgeKey position in a preparedRow's
// EdgeKeys slice to the emitted entity struct name. The lookup walks
// the owning query's preparedEdgeUnion entries, matching on ColumnName
// (unique per query), then indexes Candidates by the position. Every
// call site has a Phase B guarantee that the row's edgeUnion entry
// exists.
func edgeKeyToEntityName(p preparedQuery, f preparedRow, i int) string {
	for _, u := range p.EdgeUnions {
		if u.ColumnName == f.ColumnName && u.FieldName == f.Field {
			return u.Candidates[i]
		}
	}
	// Unreachable: Phase B guarantees a matching preparedEdgeUnion for
	// every columnEdgeUnion Row field. Returning the bare label keeps
	// the emission textually distinct so a regression surfaces at the
	// nested-module compile fence rather than silently miscompiling.
	return string(f.EdgeKeys[i].Label)
}

// writeEntityColumnDecodeIndent emits the entity-column arm of the row
// assembly (spec §5.5). Carrier is dbtype.Node for node columns, dbtype.
// Relationship for edge columns; the decode helper takes the driver
// value and returns the entity struct. Nullable columns produce a
// *EntityName pointer field via a local +address-of; non-nullable
// columns are a decode error when the driver value arrived null.
func writeEntityColumnDecodeIndent(b *strings.Builder, p preparedQuery, f preparedRow, recordExpr, zero, assignPrefix, assignSuffix, indent, varName string) {
	var carrier, decodeArg string
	if f.Kind == columnNode {
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

// findEdgeUnionCandidates resolves a list-of-edgeUnion leaf's EdgeKeys
// to the emitted entity struct names by looking up the owning query's
// preparedEdgeUnion entries. Phase B ensures every list-of-edgeUnion
// column has a preparedEdgeUnion entry with matching keys. Callers
// pass the leaf's EdgeKeys to disambiguate against different edgeUnion
// columns on the same query.
func findEdgeUnionCandidates(p preparedQuery, f preparedRow, keys []schema.EdgeKey) []string {
	for _, u := range p.EdgeUnions {
		if u.ColumnName != f.ColumnName || u.FieldName != f.Field {
			continue
		}
		if len(u.EdgeKeys) != len(keys) {
			continue
		}
		match := true
		for i := range keys {
			if u.EdgeKeys[i] != keys[i] {
				match = false
				break
			}
		}
		if match {
			return u.Candidates
		}
	}
	// Unreachable: Phase B synthesises a matching preparedEdgeUnion.
	out := make([]string, len(keys))
	for i, k := range keys {
		out[i] = string(k.Label)
	}
	return out
}
