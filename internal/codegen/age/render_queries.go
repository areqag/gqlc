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
//
// A body that projects nothing still declares exactly one agtype column.
// Measured against AGE 1.7.0: omitting the AS clause is PostgreSQL's "a
// column definition list is required for functions returning record",
// writing AS () is a SQL syntax error, and any list that is not a single
// agtype attribute is AGE's own 42804. One column is the only shape the
// server accepts, and it yields no rows.
func recordShape(p codegen.Query) string {
	if len(p.RowFields) == 0 {
		return "v0 ag_catalog.agtype"
	}
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
		fmt.Fprintf(&b, "const %s = `%s`\n\n", codegen.QueryTextConst(p), p.SourceText)
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
				if pointerWrapped(f) {
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
	if pointerWrapped(p.RowFields[0]) {
		elem = "*"
	}
	return elem + p.RowFields[0].GoType
}

// pointerWrapped reports whether a nullable column's Go type takes a
// pointer to carry its absence. A multi-candidate edge column's type is
// a sealed interface, which holds nil already; pointer-to-interface is
// the shape ADR 0010 D3 forbids.
func pointerWrapped(f codegen.Row) bool {
	return f.Nullable && f.Kind != codegen.ColumnEdgeUnion
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
	if pointerWrapped(f) {
		return "nil"
	}
	switch f.Kind {
	case codegen.ColumnNode, codegen.ColumnEdge:
		return f.GoType + "{}"
	case codegen.ColumnEdgeUnion:
		return "nil"
	default:
		return zeroLiteral(f.GoType)
	}
}

// zeroLiteral is the zero value of a non-entity Go type the emission
// produces. A slice, an interface and a map are all nil; what is left is
// a Go scalar, and the numeric widths share a literal.
func zeroLiteral(goType string) string {
	if strings.HasPrefix(goType, "[]") {
		return "nil"
	}
	switch goType {
	case "string":
		return `""`
	case "bool":
		return "false"
	case "any", "map[string]any":
		return "nil"
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
	switch p.Cardinality {
	case codegen.CardinalityExec:
		writeExecBody(b, p)
	case codegen.CardinalityOne:
		writeQueryCall(b, p)
		writeOneBody(b, p)
	default:
		writeQueryCall(b, p)
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

// failPrefix is what a body's error returns place before the error
// itself. An :exec method returns error alone and so has nothing before
// it; every other cardinality returns the zero of its row type first.
func failPrefix(p codegen.Query) string {
	if p.Cardinality == codegen.CardinalityExec {
		return ""
	}
	return zeroValueText(p) + ", "
}

// writeStatement emits the statement composition and the parameter
// encoding both bodies open with, returning the expression holding the
// agtype argument object. Shared so a read and a write reach the server
// through the same composed text and the same bound argument: the graph
// name is the only thing this backend ever interpolates, and it is
// escaped and length-checked inside cypherStmt.
func writeStatement(b *strings.Builder, p codegen.Query) string {
	fail := failPrefix(p)
	fmt.Fprintf(b, "\tstmt, err := q.cypherStmt(%q, %s, %q)\n", dollarTag(p.SourceText), codegen.QueryTextConst(p), recordShape(p))
	fmt.Fprintf(b, "\tif err != nil {\n\t\treturn %serr\n\t}\n", fail)

	argsExpr := `"{}"`
	if len(p.ParamFields) > 0 {
		fmt.Fprintf(b, "\targs, err := agtypeArgs(%s)\n", argsMapText(p))
		fmt.Fprintf(b, "\tif err != nil {\n\t\treturn %serr\n\t}\n", fail)
		argsExpr = "args"
	}
	return argsExpr
}

// writeQueryCall emits the statement composition, the parameter
// encoding, and the q.db.Query call every decoding body opens with.
func writeQueryCall(b *strings.Builder, p codegen.Query) {
	argsExpr := writeStatement(b, p)
	zero := zeroValueText(p)
	fmt.Fprintf(b, "\trows, err := q.db.Query(ctx, stmt, %s)\n", argsExpr)
	fmt.Fprintf(b, "\tif err != nil {\n\t\treturn %s, fmt.Errorf(%q, err)\n\t}\n", zero, p.MethodName+": %w")
	b.WriteString("\tdefer rows.Close()\n")
}

// writeExecBody emits the whole of an :exec method: compose, encode,
// execute, report. The command tag is discarded rather than reported.
// Measured against AGE 1.7.0: the tag a cypher() call returns is the
// enclosing SELECT's, so its RowsAffected counts projected rows and is
// zero for every write that projects nothing — a CREATE of three
// vertices and a DELETE that matched none both report "SELECT 0". A
// count that cannot tell those apart is not a count of anything, and
// returning error alone is also what keeps this method's signature
// identical to the one the Neo4j targets emit.
func writeExecBody(b *strings.Builder, p codegen.Query) {
	argsExpr := writeStatement(b, p)
	fmt.Fprintf(b, "\tif _, err := q.db.Exec(ctx, stmt, %s); err != nil {\n\t\treturn fmt.Errorf(%q, err)\n\t}\n",
		argsExpr, p.MethodName+": %w")
	b.WriteString("\treturn nil\n")
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
		access := codegen.ParamArg
		if len(p.ParamFields) > 1 {
			access = codegen.ParamArg + "." + f.Field
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
	fmt.Fprintf(b, "\tif err := rows.Err(); err != nil {\n\t\treturn %s, fmt.Errorf(%q, err)\n\t}\n",
		zero, p.MethodName+": %w")
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
	fmt.Fprintf(b, "\tif err := rows.Err(); err != nil {\n\t\treturn nil, fmt.Errorf(%q, err)\n\t}\n",
		p.MethodName+": %w")
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

// valueName is the decoded local at index i — a projected column in a
// query method, a property in an entity decoder. Every local an emitted
// body declares is positional: a name taken from the query text or the
// schema is any Go identifier the author chose, including one the body
// already holds.
func valueName(i int) string { return fmt.Sprintf("value%d", i) }

// labelName is the local holding the label read off the column at index
// i, positional so it matches its scan target.
func labelName(i int) string { return fmt.Sprintf("label%d", i) }

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
	if f.Kind == codegen.ColumnEdgeUnion {
		writeEdgeUnionDecode(b, p, idx, f, indent, zero)
		return
	}
	raw, value := rawName(idx), valueName(idx)
	decodeErr := fmt.Sprintf("%s: decode column %%q: %%w", p.MethodName)

	if !f.Nullable {
		writeNonNullGate(b, p, f, raw, indent, zero)
		fmt.Fprintf(b, "%s%s, err := %s(%s)\n", indent, value, columnDecoder(f), raw)
		fmt.Fprintf(b, "%sif err != nil {\n%s\treturn %s, fmt.Errorf(%q, %q, err)\n%s}\n",
			indent, indent, zero, decodeErr, f.ColumnName, indent)
		return
	}

	fmt.Fprintf(b, "%svar %s *%s\n", indent, value, f.GoType)
	fmt.Fprintf(b, "%sif %s != nil {\n", indent, raw)
	fmt.Fprintf(b, "%s\tdecoded, err := %s(%s)\n", indent, columnDecoder(f), raw)
	fmt.Fprintf(b, "%s\tif err != nil {\n%s\t\treturn %s, fmt.Errorf(%q, %q, err)\n%s\t}\n",
		indent, indent, zero, decodeErr, f.ColumnName, indent)
	if carrier := agtypeCarrier(f.GoType); carrier != f.GoType {
		fmt.Fprintf(b, "%s\tnarrowed := %s(decoded)\n%s\t%s = &narrowed\n", indent, f.GoType, indent, value)
	} else {
		fmt.Fprintf(b, "%s\t%s = &decoded\n", indent, value)
	}
	fmt.Fprintf(b, "%s}\n", indent)
}

// writeEdgeUnionDecode emits a multi-candidate edge column's decode. The
// label is read off the wire value first and chooses which of the
// candidate decoders reads the whole of it; a label outside the
// candidate set fails the row, because the sealed interface has no
// member to carry it.
func writeEdgeUnionDecode(b *strings.Builder, p codegen.Query, idx int, f codegen.Row, indent, zero string) {
	raw, value, label := rawName(idx), valueName(idx), labelName(idx)
	decodeErr := fmt.Sprintf("%s: decode column %%q: %%w", p.MethodName)

	fmt.Fprintf(b, "%svar %s %s\n", indent, value, f.GoType)
	body := indent
	if f.Nullable {
		fmt.Fprintf(b, "%sif %s != nil {\n", indent, raw)
		body = indent + "\t"
	} else {
		writeNonNullGate(b, p, f, raw, indent, zero)
	}

	fmt.Fprintf(b, "%s%s, _, err := agtypeEntity(%s, %q)\n", body, label, raw, edgeAnnotation)
	fmt.Fprintf(b, "%sif err != nil {\n%s\treturn %s, fmt.Errorf(%q, %q, err)\n%s}\n",
		body, body, zero, decodeErr, f.ColumnName, body)
	fmt.Fprintf(b, "%sswitch %s {\n", body, label)
	for i, ek := range f.EdgeKeys {
		fmt.Fprintf(b, "%scase %q:\n", body, string(ek.KeyLabels))
		fmt.Fprintf(b, "%s\tdecoded, err := decode%s(%s)\n", body, edgeKeyToEntityName(p, f, i), raw)
		fmt.Fprintf(b, "%s\tif err != nil {\n%s\t\treturn %s, fmt.Errorf(%q, %q, err)\n%s\t}\n",
			body, body, zero, decodeErr, f.ColumnName, body)
		fmt.Fprintf(b, "%s\t%s = decoded\n", body, value)
	}
	fmt.Fprintf(b, "%sdefault:\n%s\treturn %s, fmt.Errorf(%q, %q, %s)\n%s}\n",
		body, body, zero, fmt.Sprintf("%s: column %%q: unexpected edge label %%q", p.MethodName),
		f.ColumnName, label, body)
	if f.Nullable {
		fmt.Fprintf(b, "%s}\n", indent)
	}
}

// writeNonNullGate emits the check that fails the row when a column the
// schema declares non-nullable arrives as SQL NULL, which is the shape
// an agtype null reaches the driver in.
func writeNonNullGate(b *strings.Builder, p codegen.Query, f codegen.Row, raw, indent, zero string) {
	fmt.Fprintf(b, "%sif %s == nil {\n%s\treturn %s, fmt.Errorf(%q, %q)\n%s}\n",
		indent, raw, indent, zero,
		fmt.Sprintf("%s: column %%q is non-nullable but arrived null", p.MethodName), f.ColumnName, indent)
}

// edgeKeyToEntityName resolves one of a column's candidate edge keys to
// the entity struct name emitted for it. The owning query's EdgeUnion
// entry holds the names in the same order as the keys, and Phase B
// guarantees one exists for every multi-candidate edge column.
func edgeKeyToEntityName(p codegen.Query, f codegen.Row, i int) string {
	for _, u := range p.EdgeUnions {
		if u.ColumnName == f.ColumnName && u.FieldName == f.Field {
			return u.Candidates[i]
		}
	}
	// Unreachable while that guarantee holds. Naming the label keeps the
	// emission textually distinct, so a regression surfaces as a compile
	// failure of the generated package.
	return string(f.EdgeKeys[i].KeyLabels)
}

// columnDecoder names the models.go helper that turns one column's
// undecoded text into its Go value. A whole vertex or edge decodes
// through the entity's own helper, which is where the label check lives;
// every other served column rides a single agtype scalar.
func columnDecoder(f codegen.Row) string {
	if f.Kind == codegen.ColumnNode || f.Kind == codegen.ColumnEdge {
		return "decode" + f.GoType
	}
	return decodeFunc(f.GoType)
}

// decodeFunc names the models.go helper that decodes one value of an
// emitted Go type. A slice goes through the named wrapper emitted for
// it, a type of no declared shape through the agtype value vocabulary,
// and everything else through the helper for the agtype scalar its
// carrier is — the caller narrows.
func decodeFunc(goType string) string {
	if strings.HasPrefix(goType, "[]") {
		return listHelperName(goType)
	}
	switch goType {
	case "any":
		return "agtypeValue"
	case "map[string]any":
		return "agtypeMap"
	}
	switch agtypeCarrier(goType) {
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
