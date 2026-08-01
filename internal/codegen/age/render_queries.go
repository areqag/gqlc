package age

import (
	"fmt"
	"path/filepath"
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
// first-appearance order (spec §5.5).
func groupBySource(prepared []codegen.Query) []sourceGroup {
	seen := make(map[string]int)
	var groups []sourceGroup
	for _, p := range prepared {
		base := filepath.Base(p.SourceFile)
		stem := strings.TrimSuffix(base, filepath.Ext(base))
		if idx, ok := seen[stem]; ok {
			groups[idx].queries = append(groups[idx].queries, p)
			continue
		}
		seen[stem] = len(groups)
		groups = append(groups, sourceGroup{
			filename: stem + ".cypher.go",
			queries:  []codegen.Query{p},
		})
	}
	return groups
}

// dollarTag picks the delimiter that carries a query's text through the
// SQL parser to AGE, which reads cypher()'s query argument as a
// dollar-quoted constant and nothing else. The chosen tag closes the
// literal exactly once, at the far end, so the text can hold any quote,
// backslash or dollar sign an author writes.
//
// A candidate is judged on the text with the candidate appended, because
// what the scanner matches is the closing delimiter, and an occurrence
// of it can begin in the text's final bytes and finish in the tag: text
// ending $gqlc composes to a body ending $gqlc$gqlc$, whose first match
// is five bytes early. A first match at exactly len(text) is the
// delimiter the emission placed and no other.
func dollarTag(text string) string {
	for i := 0; ; i++ {
		tag := "$gqlc$"
		if i > 0 {
			tag = fmt.Sprintf("$gqlc%d$", i)
		}
		if strings.Index(text+tag, tag) == len(text) {
			return tag
		}
	}
}

// recordShape is the record definition a set-returning cypher() call
// declares: one agtype column per projected column, positionally named.
// The names are positional because a projection's own column name is an
// expression like p.name, which is not an SQL identifier.
func recordShape(p codegen.Query) string {
	cols := make([]string, len(p.RowFields))
	for i := range p.RowFields {
		cols[i] = fmt.Sprintf("v%d ag_catalog.agtype", i)
	}
	return strings.Join(cols, ", ")
}

// renderCypherFile emits one <name>.cypher.go file (spec §5.5). Per
// query in order: query-text const, Params struct (if any), Row struct
// (if any), method.
func renderCypherFile(pkg string, queries []codegen.Query) []byte {
	var b strings.Builder
	b.WriteString(codegen.Header())
	b.WriteString("package " + pkg + "\n\n")
	b.WriteString("import (\n\t\"context\"\n\t\"fmt\"\n)\n\n")

	for i, p := range queries {
		if i > 0 {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "const %sQueryText = `%s`\n\n", p.Bare, p.SourceText)
		if len(p.ParamFields) >= 2 {
			fmt.Fprintf(&b, "type %sParams struct {\n", p.MethodName)
			for _, f := range p.ParamFields {
				b.WriteString("\t" + f.Field + " ")
				if f.Nullable {
					b.WriteString("*")
				}
				b.WriteString(f.GoType + "\n")
			}
			b.WriteString("}\n\n")
		}
		if len(p.RowFields) >= 2 {
			fmt.Fprintf(&b, "type %sRow struct {\n", p.MethodName)
			for _, f := range p.RowFields {
				b.WriteString("\t" + f.Field + " ")
				if f.Nullable {
					b.WriteString("*")
				}
				b.WriteString(f.GoType + "\n")
			}
			b.WriteString("}\n\n")
		}
		writeMethod(&b, p)
	}
	return []byte(b.String())
}

// writeMethodSignature writes one `MethodName(ctx context.Context, ...)
// (Return, error)` line — used both by the interface entry in querier.go
// and by the method definition in <name>.cypher.go.
func writeMethodSignature(b *strings.Builder, p codegen.Query) {
	b.WriteString(p.MethodName)
	b.WriteString("(ctx context.Context")
	switch len(p.ParamFields) {
	case 0:
	case 1:
		fmt.Fprintf(b, ", %s ", codegen.LowerFirstRune(p.ParamFields[0].Field))
		if p.ParamFields[0].Nullable {
			b.WriteString("*")
		}
		b.WriteString(p.ParamFields[0].GoType)
	default:
		fmt.Fprintf(b, ", arg %sParams", p.MethodName)
	}
	b.WriteString(") (" + returnTypeText(p) + ", error)")
}

// returnTypeText composes the return-type text for a prepared query.
// :one → T or MethodRow; :many → []T or []MethodRow.
func returnTypeText(p codegen.Query) string {
	elem := rowElemText(p)
	if p.Cardinality == codegen.CardinalityMany {
		return "[]" + elem
	}
	return elem
}

// rowElemText is the Go type of one decoded row: the column's own type
// for a single-column projection, the derived Row struct otherwise.
func rowElemText(p codegen.Query) string {
	if len(p.RowFields) != 1 {
		return p.MethodName + "Row"
	}
	var elem string
	if p.RowFields[0].Nullable {
		elem = "*"
	}
	return elem + p.RowFields[0].GoType
}

// zeroValueText composes the zero-value expression for a prepared
// query's return type, matching the emitted method signature (§5.3).
func zeroValueText(p codegen.Query) string {
	if p.Cardinality == codegen.CardinalityMany {
		return "nil"
	}
	if len(p.RowFields) != 1 {
		return p.MethodName + "Row{}"
	}
	f := p.RowFields[0]
	if f.Nullable {
		return "nil"
	}
	switch f.GoType {
	case "string":
		return `""`
	case "bool":
		return "false"
	default:
		return "0"
	}
}

// writeMethod writes the method definition + body (spec §5.3 / §5.5).
func writeMethod(b *strings.Builder, p codegen.Query) {
	writeDocComment(b, p)
	b.WriteString("func (q *Queries) ")
	writeMethodSignature(b, p)
	b.WriteString(" {\n")
	writeQueryCall(b, p)
	if p.Cardinality == codegen.CardinalityOne {
		writeOneBody(b, p)
	} else {
		writeManyBody(b, p)
	}
	b.WriteString("}\n")
}

// writeDocComment emits the per-method doc comment: the method name and
// the first 3 lines of the query text, prefixed //   .
func writeDocComment(b *strings.Builder, p codegen.Query) {
	fmt.Fprintf(b, "// %s executes the %s query.\n//\n", p.MethodName, p.MethodName)
	lines := strings.Split(strings.TrimRight(p.SourceText, "\n"), "\n")
	limit := min(len(lines), 3)
	for i := range limit {
		fmt.Fprintf(b, "//   %s\n", lines[i])
	}
	if len(lines) > 3 {
		b.WriteString("//   ...\n")
	}
}

// writeQueryCall emits the statement composition, the parameter
// encoding, and the q.db.Query call every body opens with.
func writeQueryCall(b *strings.Builder, p codegen.Query) {
	zero := zeroValueText(p)
	fmt.Fprintf(b, "\tstmt, err := q.cypherStmt(%q, %sQueryText, %q)\n", dollarTag(p.SourceText), p.Bare, recordShape(p))
	fmt.Fprintf(b, "\tif err != nil {\n\t\treturn %s, err\n\t}\n", zero)

	argsExpr := `"{}"`
	if len(p.ParamFields) > 0 {
		fmt.Fprintf(b, "\targs, err := agtypeArgs(%s)\n", argsMapText(p))
		fmt.Fprintf(b, "\tif err != nil {\n\t\treturn %s, err\n\t}\n", zero)
		argsExpr = "args"
	}
	fmt.Fprintf(b, "\trows, err := q.db.Query(ctx, stmt, %s)\n", argsExpr)
	fmt.Fprintf(b, "\tif err != nil {\n\t\treturn %s, fmt.Errorf(%q, err)\n\t}\n", zero, p.MethodName+": %w")
	b.WriteString("\tdefer rows.Close()\n")
}

// argsMapText composes the map literal agtypeArgs encodes. Keys are the
// names the query text writes after the dollar sign, so what AGE
// substitutes is what the author wrote.
func argsMapText(p codegen.Query) string {
	var b strings.Builder
	b.WriteString("map[string]any{")
	for i, f := range p.ParamFields {
		if i > 0 {
			b.WriteString(", ")
		}
		access := "arg." + f.Field
		if len(p.ParamFields) == 1 {
			access = codegen.LowerFirstRune(f.Field)
		}
		fmt.Fprintf(&b, "%q: %s", f.RawName, access)
	}
	b.WriteString("}")
	return b.String()
}

// writeOneBody emits the :one arity check, the single row's decode, and
// the return. The arity check is two Next calls rather than a count of
// buffered rows: a second row is a broken assumption about the query,
// and reporting it is worth one more round of the cursor.
func writeOneBody(b *strings.Builder, p codegen.Query) {
	zero := zeroValueText(p)
	fmt.Fprintf(b, "\tif !rows.Next() {\n\t\tif err := rows.Err(); err != nil {\n\t\t\treturn %s, fmt.Errorf(%q, err)\n\t\t}\n\t\treturn %s, ErrNoRows\n\t}\n",
		zero, p.MethodName+": %w", zero)
	writeScan(b, p, "\t", zero)
	fmt.Fprintf(b, "\tif rows.Next() {\n\t\treturn %s, ErrMultipleResults\n\t}\n", zero)
	fmt.Fprintf(b, "\tif err := rows.Err(); err != nil {\n\t\treturn %s, fmt.Errorf(%q, err)\n\t}\n", zero, p.MethodName+": %w")
	for i, f := range p.RowFields {
		writeColumnDecode(b, p, i, f, "\t", zero)
	}
	if len(p.RowFields) == 1 {
		fmt.Fprintf(b, "\treturn %s, nil\n", valueExpr(0, p.RowFields[0]))
		return
	}
	fmt.Fprintf(b, "\treturn %sRow{\n", p.MethodName)
	for i, f := range p.RowFields {
		fmt.Fprintf(b, "\t\t%s: %s,\n", f.Field, valueExpr(i, f))
	}
	b.WriteString("\t}, nil\n")
}

// writeManyBody emits the :many cursor walk, the per-row decode, and the
// return. The slice is allocated empty rather than left nil so a query
// that matched nothing returns the same shape as one that matched.
func writeManyBody(b *strings.Builder, p codegen.Query) {
	fmt.Fprintf(b, "\tout := make([]%s, 0)\n", rowElemText(p))
	b.WriteString("\tfor rows.Next() {\n")
	writeScan(b, p, "\t\t", "nil")
	for i, f := range p.RowFields {
		writeColumnDecode(b, p, i, f, "\t\t", "nil")
	}
	if len(p.RowFields) == 1 {
		fmt.Fprintf(b, "\t\tout = append(out, %s)\n", valueExpr(0, p.RowFields[0]))
	} else {
		fmt.Fprintf(b, "\t\tout = append(out, %sRow{\n", p.MethodName)
		for i, f := range p.RowFields {
			fmt.Fprintf(b, "\t\t\t%s: %s,\n", f.Field, valueExpr(i, f))
		}
		b.WriteString("\t\t})\n")
	}
	b.WriteString("\t}\n")
	fmt.Fprintf(b, "\tif err := rows.Err(); err != nil {\n\t\treturn nil, fmt.Errorf(%q, err)\n\t}\n", p.MethodName+": %w")
	b.WriteString("\treturn out, nil\n")
}

// writeScan emits the row scan. Every column lands in a []byte because
// that is the shape that tells a SQL NULL apart from every agtype value:
// agtype's text is never empty, so a nil slice is the null and nothing
// else is.
func writeScan(b *strings.Builder, p codegen.Query, indent, zero string) {
	targets := make([]string, len(p.RowFields))
	for i := range p.RowFields {
		fmt.Fprintf(b, "%svar %s []byte\n", indent, rawName(i))
		targets[i] = "&" + rawName(i)
	}
	fmt.Fprintf(b, "%sif err := rows.Scan(%s); err != nil {\n%s\treturn %s, fmt.Errorf(%q, err)\n%s}\n",
		indent, strings.Join(targets, ", "), indent, zero, p.MethodName+": scan row: %w", indent)
}

// rawName is the scan target for the column at index i.
func rawName(i int) string { return fmt.Sprintf("raw%d", i) }

// valueName is the decoded local for the column at index i, positional
// so it matches its scan target.
func valueName(i int) string { return fmt.Sprintf("value%d", i) }

// valueExpr is what a column contributes to the returned row. A narrow
// width rides its wide carrier through the decode and converts here; a
// nullable column already holds a pointer of the declared width.
func valueExpr(i int, f codegen.Row) string {
	if !f.Nullable && agtypeCarrier(f.GoType) != f.GoType {
		return f.GoType + "(" + valueName(i) + ")"
	}
	return valueName(i)
}

// writeColumnDecode emits one column's null handling and decode. A
// non-nullable column that arrives null fails the row: the schema says
// the value is there, and a Go zero would report absence as a value the
// graph holds.
func writeColumnDecode(b *strings.Builder, p codegen.Query, idx int, f codegen.Row, indent, zero string) {
	raw, value := rawName(idx), valueName(idx)
	carrier := agtypeCarrier(f.GoType)
	decodeErr := fmt.Sprintf("%s: decode column %%q: %%w", p.MethodName)

	if !f.Nullable {
		fmt.Fprintf(b, "%sif %s == nil {\n%s\treturn %s, fmt.Errorf(%q, %q)\n%s}\n",
			indent, raw, indent, zero, fmt.Sprintf("%s: column %%q is non-nullable but arrived null", p.MethodName), f.ColumnName, indent)
		fmt.Fprintf(b, "%s%s, err := %s(%s)\n", indent, value, decodeFunc(carrier), raw)
		fmt.Fprintf(b, "%sif err != nil {\n%s\treturn %s, fmt.Errorf(%q, %q, err)\n%s}\n",
			indent, indent, zero, decodeErr, f.ColumnName, indent)
		return
	}

	fmt.Fprintf(b, "%svar %s *%s\n", indent, value, f.GoType)
	fmt.Fprintf(b, "%sif %s != nil {\n", indent, raw)
	fmt.Fprintf(b, "%s\tdecoded, err := %s(%s)\n", indent, decodeFunc(carrier), raw)
	fmt.Fprintf(b, "%s\tif err != nil {\n%s\t\treturn %s, fmt.Errorf(%q, %q, err)\n%s\t}\n",
		indent, indent, zero, decodeErr, f.ColumnName, indent)
	if carrier != f.GoType {
		fmt.Fprintf(b, "%s\tnarrowed := %s(decoded)\n%s\t%s = &narrowed\n", indent, f.GoType, indent, value)
	} else {
		fmt.Fprintf(b, "%s\t%s = &decoded\n", indent, value)
	}
	fmt.Fprintf(b, "%s}\n", indent)
}

// decodeFunc names the models.go helper for a carrier type.
func decodeFunc(carrier string) string {
	switch carrier {
	case "bool":
		return "agtypeBool"
	case "int64":
		return "agtypeInt64"
	case "float64":
		return "agtypeFloat64"
	default:
		return "agtypeString"
	}
}
