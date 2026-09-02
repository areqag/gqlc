package age

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/areqag/gqlc/internal/codegen"
)

// namesInstant reports whether one query's emitted surface spells the
// instant type, which is the whole of what decides whether the file it
// lands in imports "time". A parameter or a projected column is enough:
// both put the type text into a signature, and the Row and Params
// structs are built from the same fields.
func namesInstant(p codegen.Query) bool {
	return slices.ContainsFunc(p.ParamFields, func(f codegen.Param) bool { return f.GoType == goInstant }) ||
		slices.ContainsFunc(p.RowFields, func(f codegen.Row) bool { return f.GoType == goInstant })
}

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
	b.WriteString("import (\n\t\"context\"\n\t\"fmt\"\n")
	if slices.ContainsFunc(queries, namesInstant) {
		b.WriteString("\t\"time\"\n")
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
// pointer to carry its absence. Every column kind this backend serves
// does: the one exception on the other backends is the sealed interface
// of a multi-candidate edge column, which holds nil already, and this
// backend refuses that column ahead of Prepare (edgeUnionReason).
func pointerWrapped(f codegen.Row) bool {
	return f.Nullable
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
	case goInstant, goDate, goLocalTime, goTime, goDuration:
		// The instant and the neutral carriers are all structs, so their
		// zero is the composite literal and not a numeric one.
		return goType + "{}"
	default:
		return "0"
	}
}

// writeMethod writes the method definition + body (spec §5.3 / §5.5).
func writeMethod(b *strings.Builder, p codegen.Query) {
	writeDocComment(b, p)
	b.WriteString("func (q *queries) ")
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

	// A parameter whose encoding can fail is bound to a local first: an
	// expression that returns an error has no form inside the map literal
	// agtypeArgs takes. The failure names the parameter the author wrote,
	// because the helper it comes out of sees only a value.
	for i, f := range p.ParamFields {
		encode, ok := fallibleParamEncoder(f, paramAccess(p, f))
		if !ok {
			continue
		}
		fmt.Fprintf(b, "\t%s, err := %s\n", boundParamName(i), encode)
		fmt.Fprintf(b, "\tif err != nil {\n\t\treturn %sfmt.Errorf(%q, err)\n\t}\n",
			fail, p.MethodName+": parameter $"+f.RawName+": %w")
	}

	argsExpr := `"{}"`
	if len(p.ParamFields) > 0 {
		fmt.Fprintf(b, "\targs, err := agtypeArgs(%s)\n", argsMapText(p))
		fmt.Fprintf(b, "\tif err != nil {\n\t\treturn %serr\n\t}\n", fail)
		argsExpr = "args"
	}
	return argsExpr
}

// boundParamName is the local one fallibly-encoded parameter's result is
// bound to. Indexed rather than derived from the parameter name, which is
// the author's and could mangle to a Go keyword or to another local's
// name; nothing else in an emitted method body is spelled this way.
func boundParamName(i int) string { return fmt.Sprintf("param%d", i) }

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
		value := encodeParam(f, paramAccess(p, f))
		if _, fallible := fallibleParamEncoder(f, ""); fallible {
			// Already encoded, into the local writeStatement bound it to.
			value = boundParamName(i)
		}
		fmt.Fprintf(&b, "%q: %s", f.RawName, value)
	}
	b.WriteString("}")
	return b.String()
}

// paramAccess is the expression reading one bound parameter out of the
// method's argument. A query binding one parameter takes it bare; two or
// more arrive in a Params struct.
func paramAccess(p codegen.Query, f codegen.Param) string {
	if len(p.ParamFields) > 1 {
		return codegen.ParamArg + "." + f.Field
	}
	return codegen.ParamArg
}

// encodeParam wraps one parameter's access in the encoder its Go type
// crosses the wire through, for the encodings that cannot fail. Every
// emitted type but a temporal is already a shape the JSON encoder writes
// as the agtype scalar it rides; an instant is not, and left alone would
// cross as a formatted string agtype orders by collation rather than by
// time. The three neutral carriers are not either, and left alone would
// cross as the JSON object their fields spell — which no decode in the
// emitted package reads and no ORDER BY sorts.
//
// Those three are encoded by fallibleParamEncoder instead, and reach the
// args map through a local. This answers for the instant and for
// everything that needs no encoder at all.
func encodeParam(f codegen.Param, access string) string {
	if f.GoType != goInstant {
		return access
	}
	if f.Nullable {
		return "agtypeNullableMicros(" + access + ")"
	}
	return "agtypeMicros(" + access + ")"
}

// encodedParamText is the agtype-side Go type one carrier encodes to: the
// string a DATE is stored as, and the microsecond count the other two
// are. Named because the nullable-list composition below has to spell the
// inner encoder's signature.
var encodedParamText = map[string]string{
	goDate:      "string",
	goLocalTime: "int64",
	goTime:      "int64",
	goDuration:  "int64",
}

// fallibleParamEncoder composes the encode expression for one parameter
// whose carrier's encoding can fail, with ok=false for every parameter
// that crosses as an expression inside the args map — which is what the
// call site in argsMapText reads it for, so access may be empty there.
//
// The encoder is chosen by the carrier at the leaf, and nullability and
// list nesting are carried by the two combinators wrapped around it, so
// the four shapes a parameter of one of these widths can take are four
// compositions of the same three names rather than four emitted helpers.
func fallibleParamEncoder(f codegen.Param, access string) (string, bool) {
	elem, list := strings.CutPrefix(f.GoType, "[]")
	var encoder string
	switch elem {
	case goDate:
		encoder = "agtypeDateText"
	case goLocalTime:
		encoder = "agtypeLocalTimeMicros"
	case goTime:
		encoder = "agtypeTimeMicros"
	case goDuration:
		encoder = "agtypeDurationMicros"
	default:
		return "", false
	}
	switch {
	case list && f.Nullable:
		return fmt.Sprintf("agtypeEncodedNullable(%s, func(in []%s) ([]%s, error) {\n\t\treturn agtypeEncodedList(in, %s)\n\t})",
			access, elem, encodedParamText[elem], encoder), true
	case list:
		return fmt.Sprintf("agtypeEncodedList(%s, %s)", access, encoder), true
	case f.Nullable:
		return fmt.Sprintf("agtypeEncodedNullable(%s, %s)", access, encoder), true
	default:
		return fmt.Sprintf("%s(%s)", encoder, access), true
	}
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

// writeNonNullGate emits the check that fails the row when a column the
// schema declares non-nullable arrives as SQL NULL, which is the shape
// an agtype null reaches the driver in.
func writeNonNullGate(b *strings.Builder, p codegen.Query, f codegen.Row, raw, indent, zero string) {
	fmt.Fprintf(b, "%sif %s == nil {\n%s\treturn %s, fmt.Errorf(%q, %q)\n%s}\n",
		indent, raw, indent, zero,
		fmt.Sprintf("%s: column %%q is non-nullable but arrived null", p.MethodName), f.ColumnName, indent)
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
// and a scalar through the helper for the agtype scalar its carrier is —
// the caller narrows.
//
// Every carrier the type table produces has an arm here, and a carrier
// with none panics. This is the first panic in the non-test code of
// internal/codegen/ — the conformance tests carry three of their own.
// The house shape for a broken internal invariant is a peer package's
// (internal/resolver/resolve.go, scope.go), and it is taken here because
// every caller renders into a []byte with no error to return, and every
// error generate can return is raised before the first render.
//
// What reaches here is a Go type text the table produced (types.go:
// Property, Scalar) or an element of one it composed, because every
// column kind whose text the table did NOT produce is taken before the
// call. A ColumnNode or ColumnEdge carries its entity struct name, and
// columnDecoder answers it with that entity's own decoder above. A
// ColumnEdgeUnion carries a synthesised interface name (prepare.go,
// phase B) and reaches no emission at all: edgeUnionReason refuses it
// ahead of Prepare wherever it binds two or more distinct labels, and
// Phase A's admitEdgeUnionCandidates refuses what that arm stands aside
// for — fewer than two candidates, or a label two candidates share —
// before any Row is derived. The table's third method contributes no
// text at all, which is why there is no arm for one: every arm of
// Temporal answers ok=false, so Prepare refuses a temporal column with
// ErrUnrepresentableTemporal and no ColumnTemporal Row is built here.
// That refusal is now reached by assembled input alone. Every spelling
// that puts a temporal into a query TEXT on this backend is a call into
// a constructor the server has no function for, and since bd gqlc-dy40s
// the namespaced spelling too, so the dialect gate answers on the text
// before Prepare is asked (ADR 0028 item 4). The invalid fixture that
// used to reach it went with that change; the witnesses that remain are
// the conformance suite's assembled column-temporal case and the
// declared-kind column this package assembles for itself.
//
// A list is not one of those bounds. A schema property of list width is
// SERVED: it arrives as a resolver.ResolvedProperty, typeMap.Property
// admits it at every nesting depth its element is admitted at — the
// instant excepted, for want of anywhere to put a per-element zone — and
// the emitted column decodes through the wrapper the slice arm below
// names, as the two_non_null_list_columns golden shows. What
// unservedColumn refuses is a list EXPRESSION column
// (resolver.ResolvedList), and that refusal is what keeps an entity or an
// edge union from arriving as a list element and reaching this through
// elemDecoder.
//
// So a carrier with no arm is a change to the table rather than anything
// an author wrote. Answering one with a helper picked for some other
// shape emits a package that compiles, because the field was typed from
// the same table, and reads the value as the wrong Go type at run time.
//
// TestDecodeFuncHasAnArmForEveryCarrierTheTypeTableProduces is what goes
// red when the table gains a carrier this switch was not taught: it walks
// the typeMap methods of every .go file in the package, so a new row that
// returns a literal fails a subtest of its own, and most other spellings
// fail the walk's refusal. The one spelling it reads without sweeping is
// named where the walk is, at typeTableGoTypes.
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
	carrier := agtypeCarrier(goType)
	switch carrier {
	case "bool":
		return "agtypeBool"
	case "int64":
		return "agtypeInt64"
	case "float64":
		return "agtypeFloat64"
	case "string":
		return "agtypeString"
	case goInstant:
		return "agtypeInstant"
	case goDate:
		return "agtypeDate"
	case goLocalTime:
		return "agtypeLocalTime"
	case goTime:
		return "agtypeTime"
	case goDuration:
		return "agtypeDuration"
	}
	panic(fmt.Sprintf("age codegen bug: Go type %q carries as %q, which decodeFunc has no arm for", goType, carrier))
}
