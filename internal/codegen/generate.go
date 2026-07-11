package codegen

import (
	"cmp"
	"fmt"
	"go/format"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"unicode"

	"github.com/areqag/gqlc/internal/graph"
	"github.com/areqag/gqlc/internal/resolver"
)

// version is the version stamp embedded in every generated file's header.
// Default "dev" (C0); a -ldflags -X override wires with C6 per ADR 0010
// D7. The value is a package-level constant so double-run determinism
// holds across arbitrary invocations of the same binary (§2.3).
const version = "dev"

// packageIdent is the Go package-identifier grammar (spec §5.1). Digits
// inside are legal; underscores are legal; digit-leading is not; non-ASCII
// is not.
var packageIdent = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// rowBareIdent matches column text of shape "name" — a bare identifier
// projection like RETURN n or RETURN name (spec §4.3 shape 1). Anchored so
// substring matches are impossible.
var rowBareIdent = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// rowPropAccess matches column text of shape "n.name" — a single-dot
// property access projection like RETURN p.name (spec §4.3 shape 2).
// Anchored so substring matches are impossible.
var rowPropAccess = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*\.[A-Za-z_][A-Za-z0-9_]*$`)

// reservedIdentifiers is the C1 exported-identifier reserved set (spec
// §4.1). A NamedQuery.Name matching any of these routes to
// ErrIdentifierCollision at Phase A. ErrNoRows / ErrMultipleResults are
// included even in batches that would not emit them so the check stays
// uniform — a rename that works in one batch but not another is exactly
// the "renaming scheme" D2 Resolved refused.
var reservedIdentifiers = map[string]struct{}{
	"Queries":            {},
	"New":                {},
	"WithTx":             {},
	"ReadQuerier":        {},
	"WriteQuerier":       {},
	"Querier":            {},
	"ErrNoRows":          {},
	"ErrMultipleResults": {},
}

// preparedQuery bundles the per-query derivations produced by Phase B —
// the derived method surface, the Params/Row shapes, and the resolved
// axes Phase A already gate-checked. Kept together so the per-source
// emission walk reads one struct per query in order (spec §5.5) rather
// than re-deriving each field from NamedQuery.Validated.
type preparedQuery struct {
	NamedQuery
	MethodName  string          // verbatim NamedQuery.Name
	Bare        string          // lowerCamel first rune of MethodName
	ParamFields []preparedParam // in Validated.Parameters order
	RowFields   []preparedRow   // in Validated.Columns order
}

type preparedParam struct {
	RawName  string // ResolvedParameter.Name
	Field    string // mangle §4.2
	GoType   string // §5.1
	Nullable bool
}

type preparedRow struct {
	ColumnName string // resolver Column.Name — the driver record key
	Field      string // mangle §4.3
	GoType     string // §5.1
	Nullable   bool
}

// generate is the pure emission kernel. Determinism per §2.3: input
// slices are walked in their author-defined order; the output slice is
// sorted by Path before return. First-error short-circuit: (nil, err)
// on failure.
func generate(in Input) ([]File, error) {
	pkg, err := derivePackage(in.Schema.Name)
	if err != nil {
		return nil, err
	}

	if err := validateQueries(in.Queries); err != nil {
		return nil, err
	}

	// Phase A — batch admission: for each query in slice order, gate on
	// resolved type / cardinality / reserved-identifier. First offender
	// wins (spec §2.1).
	if err := phaseAAdmit(in.Queries); err != nil {
		return nil, err
	}

	// Phase B — per-query name derivation. Row-field text-shape analysis,
	// Params-field mangle, per-query collision checks. First offender
	// wins.
	prepared, err := phaseBDerive(in.Queries)
	if err != nil {
		return nil, err
	}

	// Cross-query package-level exported-identifier collision sweep
	// (§4.4).
	if err := sweepIdentifiers(prepared); err != nil {
		return nil, err
	}

	hasOne := false
	for _, p := range prepared {
		if p.Cardinality == CardinalityOne {
			hasOne = true
			break
		}
	}

	files := []File{
		{Path: "db.go", Contents: renderDB(pkg, hasOne)},
		{Path: "querier.go", Contents: renderQuerier(pkg, prepared)},
		{Path: "models.go", Contents: renderModels(pkg)},
	}

	// Per-source `<name>.cypher.go` file emission — grouped by
	// SourceFile basename in first-appearance order (§5.5). Basename
	// stripped of extension.
	for _, group := range groupBySource(prepared) {
		files = append(files, File{
			Path:     group.filename,
			Contents: renderCypherFile(pkg, group.queries),
		})
	}

	for i, f := range files {
		formatted, err := format.Source(f.Contents)
		if err != nil {
			return nil, fmt.Errorf("%w: %s: %w", ErrFormatFailure, f.Path, err)
		}
		files[i].Contents = formatted
	}

	slices.SortFunc(files, func(a, b File) int { return cmp.Compare(a.Path, b.Path) })
	return files, nil
}

// derivePackage lowers Schema.Name into the emitted package identifier
// (spec §5.1). The mangle is deterministic: verbatim → ToLower → grammar
// check. A non-conforming result is ErrInvalidPackageName naming the
// mangled token, not the source; the caller's fix is at the schema
// layer.
func derivePackage(schemaName string) (string, error) {
	mangled := strings.ToLower(schemaName)
	if !packageIdent.MatchString(mangled) {
		return "", fmt.Errorf("%w: derived package %q from schema name %q", ErrInvalidPackageName, mangled, schemaName)
	}
	return mangled, nil
}

// validateQueries runs the batch-level checks (spec §4.6). C0 does not
// project queries but the sentinels fire uniformly regardless of stage
// so a fixture that fails here at C0 stays failing at C5.
//
// ErrDuplicateSourceFile fires when two DISTINCT SourceFile paths share
// a basename (e.g. "a/queries.cypher" and "b/queries.cypher"). Multiple
// queries from the same file are legitimate — they share both full path
// and basename by construction — and never trigger the sentinel.
func validateQueries(queries []NamedQuery) error {
	seenName := make(map[string]int, len(queries))
	seenFile := make(map[string]int, len(queries)) // basename -> first-appearance query index
	basenameToPath := make(map[string]string, len(queries))
	for i, q := range queries {
		if q.Cardinality == 0 {
			return fmt.Errorf("%w: query %q at position %d", ErrInvalidCardinality, q.Name, i)
		}
		if first, dup := seenName[q.Name]; dup {
			return fmt.Errorf("%w: %q at positions %d and %d", ErrDuplicateQueryName, q.Name, first, i)
		}
		seenName[q.Name] = i
		if q.SourceFile != "" {
			base := filepath.Base(q.SourceFile)
			if firstPath, seen := basenameToPath[base]; seen {
				if firstPath != q.SourceFile {
					return fmt.Errorf("%w: %q shared by queries at positions %d and %d", ErrDuplicateSourceFile, base, seenFile[base], i)
				}
			} else {
				basenameToPath[base] = q.SourceFile
				seenFile[base] = i
			}
		}
	}
	return nil
}

// phaseAAdmit is spec §2.1's Phase A: gates every query on axes Phase B
// depends on for name derivation. First offender in slice order wins.
func phaseAAdmit(queries []NamedQuery) error {
	for i, q := range queries {
		if _, reserved := reservedIdentifiers[q.Name]; reserved {
			return fmt.Errorf("%w: query %q at position %d collides with reserved identifier", ErrIdentifierCollision, q.Name, i)
		}
		if q.Cardinality == CardinalityExec {
			return fmt.Errorf("%w: query %q at position %d has cardinality :exec (C4 owns writes)", ErrOutOfC1Scope, q.Name, i)
		}
		if q.Cardinality != CardinalityOne && q.Cardinality != CardinalityMany {
			return fmt.Errorf("%w: query %q at position %d has unrecognised cardinality %d", ErrInvalidCardinality, q.Name, i, q.Cardinality)
		}
		if len(q.Validated.Columns) == 0 {
			// C1 admissibility requires a non-empty projection (§7).
			return fmt.Errorf("%w: query %q at position %d has no projected columns", ErrOutOfC1Scope, q.Name, i)
		}
		if strings.ContainsRune(q.SourceText, '`') {
			return fmt.Errorf("%w: query %q at position %d has a backtick in its source text", ErrOutOfC1Scope, q.Name, i)
		}
		for ci, col := range q.Validated.Columns {
			// Shape check first (spec §4.3, §6.4): count(*), arithmetic
			// expressions, and other non-clean shapes route to
			// ErrAliasRequired regardless of their resolved type — the fix
			// is an AS alias, not a scope change. Only after the column's
			// text is a known shape do we check its resolved type.
			if _, ok := rowFieldName(col.Name); !ok {
				return fmt.Errorf("%w: query %q column %d %q is neither a bare identifier nor a property access — add an explicit AS alias", ErrAliasRequired, q.Name, ci, col.Name)
			}
			prop, ok := col.Type.(resolver.ResolvedProperty)
			if !ok {
				return fmt.Errorf("%w: query %q column %d %q resolved as %s (C1 projects ResolvedProperty only)", ErrOutOfC1Scope, q.Name, ci, col.Name, col.Type.String())
			}
			if _, ok := goType(prop.Type); !ok {
				return fmt.Errorf("%w: query %q column %d %q has unrepresentable property width %s (C3 owns)", ErrOutOfC1Scope, q.Name, ci, col.Name, prop.Type)
			}
		}
		for pi, p := range q.Validated.Parameters {
			prop, ok := p.Type.(resolver.ResolvedProperty)
			if !ok {
				return fmt.Errorf("%w: query %q parameter %d $%s resolved as %s (C1 projects ResolvedProperty only)", ErrOutOfC1Scope, q.Name, pi, p.Name, p.Type.String())
			}
			if _, ok := goType(prop.Type); !ok {
				return fmt.Errorf("%w: query %q parameter %d $%s has unrepresentable property width %s (C3 owns)", ErrOutOfC1Scope, q.Name, pi, p.Name, prop.Type)
			}
		}
	}
	return nil
}

// phaseBDerive is spec §2.1's Phase B: derives names for the method,
// Params fields, and Row fields; runs per-query collision checks. Every
// column and parameter is already known-property from Phase A, so
// goType() cannot fail here.
func phaseBDerive(queries []NamedQuery) ([]preparedQuery, error) {
	out := make([]preparedQuery, 0, len(queries))
	for qi, q := range queries {
		p := preparedQuery{NamedQuery: q, MethodName: q.Name, Bare: lowerFirstRune(q.Name)}

		// Params field derivation.
		seenParam := make(map[string]int, len(q.Validated.Parameters))
		for pi, param := range q.Validated.Parameters {
			field := paramFieldName(param.Name)
			if first, dup := seenParam[field]; dup {
				return nil, fmt.Errorf("%w: query %q parameters $%s (position %d) and $%s (position %d) both mangle to %q", ErrParamNameCollision, q.Name, q.Validated.Parameters[first].Name, first, param.Name, pi, field)
			}
			seenParam[field] = pi

			// Phase A guaranteed ResolvedProperty + representable width.
			prop, ok := param.Type.(resolver.ResolvedProperty)
			if !ok {
				return nil, fmt.Errorf("%w: query %q parameter %d $%s: internal invariant — Phase A missed non-property type %s", ErrOutOfC1Scope, q.Name, pi, param.Name, param.Type.String())
			}
			ty, _ := goType(prop.Type)
			p.ParamFields = append(p.ParamFields, preparedParam{
				RawName:  param.Name,
				Field:    field,
				GoType:   ty,
				Nullable: prop.Nullable,
			})
		}

		// Row field derivation.
		seenRow := make(map[string]int, len(q.Validated.Columns))
		for ci, col := range q.Validated.Columns {
			field, ok := rowFieldName(col.Name)
			if !ok {
				return nil, fmt.Errorf("%w: query %q column %d %q is neither a bare identifier nor a property access — add an explicit AS alias", ErrAliasRequired, q.Name, ci, col.Name)
			}
			if first, dup := seenRow[field]; dup {
				return nil, fmt.Errorf("%w: query %q columns %d (%q) and %d (%q) both derive to %q — add an explicit AS alias to disambiguate", ErrRowFieldCollision, q.Name, first, q.Validated.Columns[first].Name, ci, col.Name, field)
			}
			seenRow[field] = ci

			prop, ok := col.Type.(resolver.ResolvedProperty)
			if !ok {
				return nil, fmt.Errorf("%w: query %q column %d %q: internal invariant — Phase A missed non-property type %s", ErrOutOfC1Scope, q.Name, ci, col.Name, col.Type.String())
			}
			ty, _ := goType(prop.Type)
			p.RowFields = append(p.RowFields, preparedRow{
				ColumnName: col.Name,
				Field:      field,
				GoType:     ty,
				Nullable:   prop.Nullable,
			})
		}

		out = append(out, p)
		_ = qi
	}
	return out, nil
}

// sweepIdentifiers runs spec §4.4's exported-identifier collision sweep
// across every emitted top-level identifier: method names, Params
// struct names, Row struct names. First insertion-order duplicate wins.
// C0 skeleton identifiers (Queries / New / WithTx / ReadQuerier /
// WriteQuerier / Querier) and the ErrNoRows / ErrMultipleResults
// sentinels are already gate-checked by Phase A's reserved-identifier
// match, so they never appear here.
func sweepIdentifiers(prepared []preparedQuery) error {
	seen := make(map[string]string, len(prepared)*3)
	insert := func(ident, source string) error {
		if first, dup := seen[ident]; dup {
			return fmt.Errorf("%w: identifier %q emitted by both %s and %s", ErrIdentifierCollision, ident, first, source)
		}
		seen[ident] = source
		return nil
	}
	for _, p := range prepared {
		if err := insert(p.MethodName, fmt.Sprintf("query %q method", p.Name)); err != nil {
			return err
		}
		if len(p.ParamFields) >= 2 {
			if err := insert(p.MethodName+"Params", fmt.Sprintf("query %q Params struct", p.Name)); err != nil {
				return err
			}
		}
		if len(p.RowFields) >= 2 {
			if err := insert(p.MethodName+"Row", fmt.Sprintf("query %q Row struct", p.Name)); err != nil {
				return err
			}
		}
	}
	return nil
}

// header renders the "Code generated by gqlc ... DO NOT EDIT." header,
// byte-identical across files in a batch. The Go toolchain regex
// (^// Code generated .* DO NOT EDIT\.$) matches so gofmt / linters
// treat the files as generated.
func header() string {
	return fmt.Sprintf("// Code generated by gqlc %s. DO NOT EDIT.\n\n", version)
}

// renderDB emits db.go (spec §5.3, §5.6). The template is the spec's
// snippet verbatim; format.Source normalises whitespace on the way out.
// C1 revises the driverOrTx.run seam signature to []*neo4j.Record —
// self-contained snapshots that survive transaction close (§5.6).
// emitOneSentinels controls whether ErrNoRows / ErrMultipleResults are
// declared: true iff the batch contains at least one :one query.
func renderDB(pkg string, emitOneSentinels bool) []byte {
	var sentinelBlock string
	if emitOneSentinels {
		sentinelBlock = `
// ErrNoRows is returned by a :one method when the query produced zero
// rows. Callers branch with errors.Is.
var ErrNoRows = errors.New("gqlc: no rows in result set")

// ErrMultipleResults is returned by a :one method when the query
// produced more than one row. Callers branch with errors.Is.
var ErrMultipleResults = errors.New("gqlc: multiple rows in :one result set")
`
	}
	importsBlock := "import (\n\t\"context\"\n\t\"fmt\"\n"
	if emitOneSentinels {
		importsBlock += "\t\"errors\"\n"
	}
	importsBlock += "\n\t\"github.com/neo4j/neo4j-go-driver/v5/neo4j\"\n)\n"

	return []byte(header() + `package ` + pkg + `

` + importsBlock + sentinelBlock + `
type Queries struct {
	db driverOrTx
}

func New(driver neo4j.DriverWithContext) *Queries {
	return &Queries{db: driverDB{driver: driver}}
}

func (q *Queries) WithTx(tx neo4j.ManagedTransaction) *Queries {
	return &Queries{db: txDB{tx: tx}}
}

// driverOrTx is the unexported run indirection: every generated query
// body routes through it, dispatching between the per-call-session
// path (New) and the caller-owned managed-transaction path (WithTx).
// C1 returns []*neo4j.Record — driver-documented self-contained value
// snapshots safe to consume after the transaction closes (§5.6).
type driverOrTx interface {
	run(ctx context.Context, cypher string, params map[string]any, access neo4j.AccessMode) ([]*neo4j.Record, error)
}

type driverDB struct {
	driver neo4j.DriverWithContext
}

func (d driverDB) run(ctx context.Context, cypher string, params map[string]any, access neo4j.AccessMode) ([]*neo4j.Record, error) {
	session := d.driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: access})
	defer session.Close(ctx)
	switch access {
	case neo4j.AccessModeRead:
		return neo4j.ExecuteRead(ctx, session, func(tx neo4j.ManagedTransaction) ([]*neo4j.Record, error) {
			result, err := tx.Run(ctx, cypher, params)
			if err != nil {
				return nil, err
			}
			return result.Collect(ctx)
		})
	case neo4j.AccessModeWrite:
		// C4 populates the write arm.
		return nil, fmt.Errorf("gqlc: write path not implemented")
	default:
		return nil, fmt.Errorf("gqlc: unknown access mode %v", access)
	}
}

type txDB struct {
	tx neo4j.ManagedTransaction
}

func (t txDB) run(ctx context.Context, cypher string, params map[string]any, _ neo4j.AccessMode) ([]*neo4j.Record, error) {
	result, err := t.tx.Run(ctx, cypher, params)
	if err != nil {
		return nil, err
	}
	return result.Collect(ctx)
}
`)
}

// renderQuerier emits querier.go (spec §5.4). ReadQuerier is populated
// with one method signature per read query in Input.Queries order.
// WriteQuerier stays empty (C4 populates). The compile-time assertion
// on the last line catches method-name drift.
func renderQuerier(pkg string, prepared []preparedQuery) []byte {
	var b strings.Builder
	b.WriteString(header())
	b.WriteString("package ")
	b.WriteString(pkg)
	b.WriteString("\n\n")
	if len(prepared) > 0 {
		b.WriteString("import \"context\"\n\n")
	}
	b.WriteString("type ReadQuerier interface {\n")
	for _, p := range prepared {
		b.WriteString("\t")
		writeMethodSignature(&b, p)
		b.WriteString("\n")
	}
	b.WriteString("}\n\n")
	b.WriteString("type WriteQuerier interface {\n}\n\n")
	b.WriteString("type Querier interface {\n\tReadQuerier\n\tWriteQuerier\n}\n\n")
	b.WriteString("var _ Querier = (*Queries)(nil)\n")
	return []byte(b.String())
}

// renderModels emits models.go (spec §5.5). Empty at C0 — schema-shaped
// structs land at C2. The file exists so the golden tree carries a
// stable file set from day one; later stages fill it in place.
func renderModels(pkg string) []byte {
	return []byte(header() + `package ` + pkg + `
`)
}

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

// renderCypherFile emits one <name>.cypher.go file (spec §5.5). Per
// query in order: query-text const, Params struct (if any), Row struct
// (if any), method.
func renderCypherFile(pkg string, queries []preparedQuery) []byte {
	var b strings.Builder
	b.WriteString(header())
	b.WriteString("package ")
	b.WriteString(pkg)
	b.WriteString("\n\n")
	b.WriteString("import (\n\t\"context\"\n\t\"fmt\"\n\n\t\"github.com/neo4j/neo4j-go-driver/v5/neo4j\"\n)\n\n")

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
				if f.Nullable {
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
// querier.go and by the method definition in <name>.cypher.go.
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
		if p.RowFields[0].Nullable {
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
// (or nil for a nullable pointer T).
func zeroValueText(p preparedQuery) string {
	if p.Cardinality == CardinalityMany {
		return "nil"
	}
	if len(p.RowFields) == 1 {
		if p.RowFields[0].Nullable {
			return "nil"
		}
		switch p.RowFields[0].GoType {
		case "string":
			return `""`
		case "bool":
			return "false"
		default:
			return "0"
		}
	}
	return p.MethodName + "Row{}"
}

// writeMethod writes the method definition + body (spec §5.3 / §5.5).
func writeMethod(b *strings.Builder, p preparedQuery) {
	// Doc comment: first 3 lines of query text, prefixed "//   ".
	writeDocComment(b, p)
	b.WriteString("func (q *Queries) ")
	writeMethodSignature(b, p)
	b.WriteString(" {\n")

	// Body: build the params map, call run, decode.
	writeRunCall(b, p)

	if p.Cardinality == CardinalityOne {
		writeOneBody(b, p)
	} else {
		writeManyBody(b, p)
	}
	b.WriteString("}\n")
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
func writeRunCall(b *strings.Builder, p preparedQuery) {
	fmt.Fprintf(b, "\trecords, err := q.db.run(ctx, %sQueryText, %s, neo4j.AccessModeRead)\n", p.Bare, paramsMapText(p))
	fmt.Fprintf(b, "\tif err != nil {\n\t\treturn %s, err\n\t}\n", zeroValueText(p))
}

// paramsMapText composes the driver-binding map literal.
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
		if len(p.ParamFields) == 1 {
			fmt.Fprintf(&b, "%q: %s", f.RawName, lowerFirstRune(f.Field))
		} else {
			fmt.Fprintf(&b, "%q: arg.%s", f.RawName, f.Field)
		}
	}
	b.WriteString("}")
	return b.String()
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
		if p.RowFields[0].Nullable {
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

// driverCarrier picks the neo4j.GetRecordValue[T] type for a Go type
// that C1 wants to emit. Integer widths widen to int64; float widths
// widen to float64; string / bool pass through. The caller narrows via
// a Go conversion.
func driverCarrier(goType string) string {
	switch goType {
	case "int", "int8", "int16", "int32", "int64",
		"uint", "uint8", "uint16", "uint32", "uint64":
		return "int64"
	case "float32", "float64":
		return "float64"
	default:
		return goType
	}
}

// paramFieldName derives the Params-struct field name for a parameter
// whose annotation was $<raw> (spec §4.2). Splits on '_', capitalises
// the first rune of each non-empty segment, preserves internal case of
// non-ALL-CAPS segments; ALL-CAPS segments stay ALL-CAPS.
func paramFieldName(raw string) string {
	if raw == "" {
		return ""
	}
	var b strings.Builder
	segments := strings.Split(raw, "_")
	for _, seg := range segments {
		if seg == "" {
			continue
		}
		if isAllCaps(seg) {
			b.WriteString(seg)
			continue
		}
		runes := []rune(seg)
		runes[0] = unicode.ToUpper(runes[0])
		b.WriteString(string(runes))
	}
	return b.String()
}

// isAllCaps reports whether every letter rune in s is uppercase (and s
// contains at least one letter). ALL-CAPS segments preserve their case
// under §4.2 so acronyms like API / URL / ID keep their form.
func isAllCaps(s string) bool {
	hasLetter := false
	for _, r := range s {
		if unicode.IsLetter(r) {
			hasLetter = true
			if !unicode.IsUpper(r) {
				return false
			}
		}
	}
	return hasLetter
}

// rowFieldName derives the Row-struct field name for a column whose
// text is one of the two clean shapes (spec §4.3). Returns "", false
// for anything else — the caller emits ErrAliasRequired.
func rowFieldName(colText string) (string, bool) {
	if rowBareIdent.MatchString(colText) {
		return paramFieldName(colText), true
	}
	if rowPropAccess.MatchString(colText) {
		dot := strings.IndexByte(colText, '.')
		return paramFieldName(colText[dot+1:]), true
	}
	return "", false
}

// goType maps a resolved property type to its native Go emission (spec
// §5.1). Returns (typeText, ok): ok=false for the widths C3 owns —
// caller routes to ErrOutOfC1Scope naming the width. Callers append a
// leading '*' for nullable columns/parameters at emission time.
func goType(pt graph.PropertyType) (string, bool) {
	switch pt {
	case graph.TypeString:
		return "string", true
	case graph.TypeBool:
		return "bool", true
	case graph.TypeInt:
		return "int", true
	case graph.TypeInt8:
		return "int8", true
	case graph.TypeInt16:
		return "int16", true
	case graph.TypeInt32:
		return "int32", true
	case graph.TypeInt64:
		return "int64", true
	case graph.TypeUint:
		return "uint", true
	case graph.TypeUint8:
		return "uint8", true
	case graph.TypeUint16:
		return "uint16", true
	case graph.TypeUint32:
		return "uint32", true
	case graph.TypeUint64:
		return "uint64", true
	case graph.TypeFloat, graph.TypeFloat64:
		return "float64", true
	case graph.TypeFloat32:
		return "float32", true
	case graph.TypeDate, graph.TypeTimestamp,
		graph.TypeInt128, graph.TypeInt256,
		graph.TypeUint128, graph.TypeUint256,
		graph.TypeFloat16, graph.TypeFloat128, graph.TypeFloat256,
		graph.TypeDecimal:
		// C3 owns temporals and unrepresentable widths (spec §5.1).
		return "", false
	}
	return "", false
}

// lowerFirstRune lowercases the first rune of s. Used for the
// package-internal query-text const name (Bare in preparedQuery) and
// for single-parameter argument names.
func lowerFirstRune(s string) string {
	if s == "" {
		return ""
	}
	runes := []rune(s)
	runes[0] = unicode.ToLower(runes[0])
	return string(runes)
}
