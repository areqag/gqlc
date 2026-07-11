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
	"github.com/areqag/gqlc/internal/schema"
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

// exportedGoIdentRe is the ASCII exported Go identifier grammar (spec §4.5
// Rule 1). Explicit entity Names must satisfy it; the single-label mangle
// (Rule 2 / Rule 3) also lands its result on this predicate. C1's queryfile
// front end uses the same grammar for method names — deliberately, so a
// schema-side identifier reads the same as a query-side one.
var exportedGoIdentRe = regexp.MustCompile(`^[A-Z][A-Za-z0-9]*$`)

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
	MethodName  string              // verbatim NamedQuery.Name
	Bare        string              // lowerCamel first rune of MethodName
	ParamFields []preparedParam     // in Validated.Parameters order
	RowFields   []preparedRow       // in Validated.Columns order
	EdgeUnions  []preparedEdgeUnion // in Validated.Columns order (sub-ordered by column position); one per columnEdgeUnion Row field (C5)
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
	GoType     string // §5.1 — a Go type text; for entity columns the entity struct name; for edgeUnion columns the synthesised interface name
	Nullable   bool
	Kind       columnKind            // property (C1) or entity — property/node/edge (C2); temporal/list/scalar/any (C3); edgeUnion (C5)
	ListElem   resolver.ResolvedType // populated when Kind == columnList — the element leaf, driving the loop body
	EdgeKeys   []schema.EdgeKey      // populated when Kind == columnEdgeUnion — the candidate edge keys in resolver-canonical order (§5.5)
}

// columnKind discriminates the row-assembly template arm to run for a
// given RowField. C1 always emitted a property arm; C2 adds node/edge
// entity arms; C3 adds temporal / list / scalar / any arms. The kind is
// derived once at Phase B and carried onto preparedRow so the row-
// assembly template (§5.5) needs no per-emission re-derivation.
type columnKind int

const (
	// columnProperty is C1's property arm: neo4j.GetRecordValue[<carrier>]
	// with a narrow-carrier + convert dance. Extended at C3 to include
	// DATE / TIMESTAMP passthrough (carrier = Go type).
	columnProperty columnKind = iota
	// columnNode is C2's node-entity arm: neo4j.GetRecordValue[dbtype.Node]
	// followed by a decode<EntityName>(node) call.
	columnNode
	// columnEdge is C2's edge-entity arm: neo4j.GetRecordValue[dbtype.Relationship]
	// followed by a decode<EntityName>(rel) call.
	columnEdge
	// columnTemporal is C3's temporal-expression arm:
	// neo4j.GetRecordValue[<dbtype.Kind or time.Time>] with a passthrough
	// assign — the carrier is already the emitted Go type.
	columnTemporal
	// columnScalar is C3's scalar-expression arm:
	// neo4j.GetRecordValue[<bool|int64|float64|string|map[string]any>]
	// with a passthrough assign. ScalarNull and ScalarMap have the same
	// decode shape (map's carrier is map[string]any; null decodes via
	// columnAny below).
	columnScalar
	// columnList is C3's list-column arm:
	// neo4j.GetRecordValue[[]any] followed by a per-element loop whose
	// body dispatches on the element type.
	columnList
	// columnAny is C3's honest-any arm: record.Get(key) returning (any,
	// bool). Used for ResolvedUnknown, ScalarNull, and (per §5.5) any
	// leaf whose emitted Go type is `any`.
	columnAny
	// columnEdgeUnion is C5's multi-candidate-edge arm: record.Get(key)
	// returning (any, bool) followed by a dbtype.Relationship assertion +
	// type-switch dispatch on rel.Type in resolver-canonical EdgeKeys
	// order (§5.5). The row-field GoType carries the synthesised
	// interface name; each candidate satisfies the interface via a
	// marker method emitted in models.go (§5.2).
	columnEdgeUnion
)

// entityKind discriminates node from edge in the entity-naming and
// emission passes. Node reads NodeType.Labels; edge reads EdgeType.EdgeKey.
type entityKind int

const (
	entityNode entityKind = iota
	entityEdge
)

// preparedEdgeUnion carries one per-query-column edgeUnion synthesis
// result (§4.10). The InterfaceName is the emitted sealed-marker
// interface's Go identifier (<QueryName><RowFieldName>); Candidates is
// the ordered slice of entity struct names each candidate schema edge
// maps to (via entityIndex), matched positionally against the resolver's
// EdgeKeys slice. Emission walks §5.2 to write the interface + marker
// methods, and §5.5 to write the type-switch dispatch body. Introduced
// at C5.
type preparedEdgeUnion struct {
	QueryName     string           // owning query's method name
	ColumnPos     int              // 0-based column index in Validated.Columns
	ColumnName    string           // Column.Name
	FieldName     string           // row-field mangle (§4.3), also used as the interface suffix
	InterfaceName string           // <QueryName><FieldName>
	EdgeKeys      []schema.EdgeKey // resolver-canonical order (R3 spec §4.4)
	Candidates    []string         // entity struct names, len == len(EdgeKeys); positional
}

// preparedEntity is Phase Z's per-entity result: struct name plus ordered
// field list plus the source-axis text for the doc comment. Cached in a
// slice the emission walk (§5.2) reads in insertion order.
type preparedEntity struct {
	Kind       entityKind
	Name       string            // derived struct name (spec §4.5)
	Labels     graph.LabelSetKey // node-only source axis (empty for edge)
	EdgeKey    schema.EdgeKey    // edge-only source axis (zero for node)
	DocAxis    string            // "<labels>" or "<label> edge (<src> -> <tgt>)" for doc
	Fields     []preparedEntityField
	AnyProp    bool // any property emits (⇒ fmt used)
	AnyNonNull bool // any non-nullable property emits (⇒ neo4j.GetProperty[T] used)
	AnyTime    bool // any property emits as time.Time (⇒ time used in models.go); introduced at C3
}

// preparedEntityField carries one property's derived struct field name
// and its Go type text. Property source name is retained for the driver
// property-map key.
type preparedEntityField struct {
	PropName string // Property.Name — the driver's Props map key
	Field    string // paramFieldName(PropName)
	GoType   string // §5.1 property-side row (unchanged from C1)
	Nullable bool
}

// cardinalityAnnotation renders a Cardinality as its ":one" / ":many" /
// ":exec" annotation text — the caller-visible form Phase A's fail
// messages use so the error line reads back the exact string the author
// typed on the // name: line.
func cardinalityAnnotation(c Cardinality) string {
	switch c {
	case CardinalityOne:
		return ":one"
	case CardinalityMany:
		return ":many"
	case CardinalityExec:
		return ":exec"
	}
	return "<invalid>"
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

	// Phase Z — schema-shape admission and entity naming (§2.1, §4.5,
	// §5.2). Eagerly walks every NodeType and EdgeType, deriving the
	// entity struct name via the entity-naming rules and the per-entity
	// property field list. First offender wins across the schema-shape
	// axis. Runs before Phase A because Phase A's ResolvedNode /
	// ResolvedEdge admission reads Phase Z's cache to type-check the Go
	// type text.
	entities, entityIndex, err := phaseZAdmit(in.Schema)
	if err != nil {
		return nil, err
	}

	if err := validateQueries(in.Queries); err != nil {
		return nil, err
	}

	// Phase A — batch admission: for each query in slice order, gate on
	// resolved type / cardinality / reserved-identifier. C3 widens the
	// admissible column shape to the full closed ResolvedType sum minus
	// ResolvedEdgeUnion; parameter admission stays property-only,
	// extended to temporal-property widths. First offender wins (spec
	// §2.1).
	if err := phaseAAdmit(in.Queries, entities, entityIndex); err != nil {
		return nil, err
	}

	// Phase B — per-query name derivation. Row-field text-shape analysis,
	// Params-field mangle, per-query collision checks. C2 extends the
	// row-field type mapping with entity-column lookup into Phase Z's
	// cache. First offender wins.
	prepared, err := phaseBDerive(in.Queries, entities, entityIndex)
	if err != nil {
		return nil, err
	}

	// Cross-query package-level exported-identifier collision sweep
	// (§4.6). C2 adds entity struct names as the fourth identifier
	// source, swept first.
	if err := sweepIdentifiers(entities, prepared); err != nil {
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
		{Path: "models.go", Contents: renderModels(pkg, entities, prepared)},
	}

	// Per-source `<name>.cypher.go` file emission — grouped by
	// SourceFile basename in first-appearance order (§5.5). Basename
	// stripped of extension.
	for _, group := range groupBySource(prepared) {
		needDbtype, needTime, needFmt := groupImports(group.queries)
		files = append(files, File{
			Path:     group.filename,
			Contents: renderCypherFile(pkg, group.queries, needDbtype, needTime, needFmt),
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

// entityLookupKey identifies a Phase Z cache entry: kind + the source-axis
// value (labels for a node, edge-key for an edge). Comparable so it lands
// in a Go map key directly.
type entityLookupKey struct {
	Kind    entityKind
	Labels  graph.LabelSetKey // node axis; zero for edge
	EdgeKey schema.EdgeKey    // edge axis; zero for node
}

// phaseZAdmit is spec §2.1's Phase Z: eagerly walks the schema's node and
// edge types deriving struct names + property field lists. First offender
// wins across the schema-shape axis. Every multi-label node type and every
// ambiguous edge label must carry an explicit Name — a lazy check would
// make output depend on the query set, which D3 Resolved rejects.
func phaseZAdmit(sch schema.Schema) ([]preparedEntity, map[entityLookupKey]int, error) {
	// Deterministic iteration: keys sorted lexically.
	nodeKeys := make([]graph.LabelSetKey, 0, len(sch.Nodes))
	for k := range sch.Nodes {
		nodeKeys = append(nodeKeys, k)
	}
	slices.Sort(nodeKeys)

	edgeKeys := make([]schema.EdgeKey, 0, len(sch.Edges))
	for k := range sch.Edges {
		edgeKeys = append(edgeKeys, k)
	}
	slices.SortFunc(edgeKeys, func(a, b schema.EdgeKey) int {
		return cmp.Or(
			cmp.Compare(a.Source, b.Source),
			cmp.Compare(a.Label, b.Label),
			cmp.Compare(a.Target, b.Target),
		)
	})

	// Ambiguity axis: an edge Label appearing on more than one EdgeKey is
	// ambiguous even when the two endpoint pairs differ (spec §4.5 Rule 4).
	labelCount := make(map[graph.LabelSetKey]int, len(sch.Edges))
	for _, k := range edgeKeys {
		labelCount[k.Label]++
	}

	entities := make([]preparedEntity, 0, len(sch.Nodes)+len(sch.Edges))
	index := make(map[entityLookupKey]int, len(sch.Nodes)+len(sch.Edges))

	for _, k := range nodeKeys {
		nt := sch.Nodes[k]
		name, err := entityStructName(entityNode, nt.Labels, schema.EdgeKey{}, nt.Name, false)
		if err != nil {
			return nil, nil, err
		}
		fields, anyProp, anyNonNull, anyTime, err := prepareEntityFields(name, nt.Properties)
		if err != nil {
			return nil, nil, err
		}
		labels := strings.Join(nt.Labels.Split(), "&")
		ent := preparedEntity{
			Kind:       entityNode,
			Name:       name,
			Labels:     nt.Labels,
			DocAxis:    labels,
			Fields:     fields,
			AnyProp:    anyProp,
			AnyNonNull: anyNonNull,
			AnyTime:    anyTime,
		}
		index[entityLookupKey{Kind: entityNode, Labels: nt.Labels}] = len(entities)
		entities = append(entities, ent)
	}

	for _, k := range edgeKeys {
		et := sch.Edges[k]
		ambig := labelCount[et.Label] > 1
		name, err := entityStructName(entityEdge, "", et.EdgeKey, et.Name, ambig)
		if err != nil {
			return nil, nil, err
		}
		fields, anyProp, anyNonNull, anyTime, err := prepareEntityFields(name, et.Properties)
		if err != nil {
			return nil, nil, err
		}
		docAxis := fmt.Sprintf("%s edge type (%s -> %s)", string(et.Label), string(et.Source), string(et.Target))
		ent := preparedEntity{
			Kind:       entityEdge,
			Name:       name,
			EdgeKey:    et.EdgeKey,
			DocAxis:    docAxis,
			Fields:     fields,
			AnyProp:    anyProp,
			AnyNonNull: anyNonNull,
			AnyTime:    anyTime,
		}
		index[entityLookupKey{Kind: entityEdge, EdgeKey: et.EdgeKey}] = len(entities)
		entities = append(entities, ent)
	}
	return entities, index, nil
}

// entityStructName derives the exported Go struct name for a schema node
// or edge type per spec §4.5's five rules. First failure wins in rule
// order: Rule 1 (explicit Name invalid) → ErrInvalidEntityName; Rule 4
// (multi-label / ambiguous without explicit Name) → ErrUnnamedMultiLabelType;
// Rule 2/3 (mangle result invalid) → ErrInvalidEntityName.
func entityStructName(kind entityKind, labels graph.LabelSetKey, edgeKey schema.EdgeKey, explicitName string, ambiguousEdgeLabel bool) (string, error) {
	if explicitName != "" {
		if exportedGoIdent(explicitName) {
			return explicitName, nil
		}
		return "", fmt.Errorf("%w: %s explicit Name %q is not a valid exported Go identifier", ErrInvalidEntityName, entityAxisText(kind, labels, edgeKey), explicitName)
	}

	if kind == entityNode {
		parts := labels.Split()
		if len(parts) > 1 {
			return "", fmt.Errorf("%w: node type with multi-label set %q requires an explicit Name", ErrUnnamedMultiLabelType, string(labels))
		}
		if len(parts) == 0 {
			return "", fmt.Errorf("%w: node type with empty label set requires an explicit Name", ErrUnnamedMultiLabelType)
		}
		name := paramFieldName(parts[0])
		if !exportedGoIdent(name) {
			return "", fmt.Errorf("%w: node type labels %q mangle to %q, not a valid exported Go identifier", ErrInvalidEntityName, string(labels), name)
		}
		return name, nil
	}

	// Edge.
	labelParts := edgeKey.Label.Split()
	if len(labelParts) > 1 {
		return "", fmt.Errorf("%w: multi-label edge type (%s -[:%s]-> %s) requires an explicit Name", ErrUnnamedMultiLabelType, string(edgeKey.Source), string(edgeKey.Label), string(edgeKey.Target))
	}
	if len(labelParts) == 0 {
		return "", fmt.Errorf("%w: edge type with empty label requires an explicit Name", ErrUnnamedMultiLabelType)
	}
	if ambiguousEdgeLabel {
		return "", fmt.Errorf("%w: edge label %q is shared across endpoint pairs — (%s -[:%s]-> %s) requires an explicit Name", ErrUnnamedMultiLabelType, string(edgeKey.Label), string(edgeKey.Source), string(edgeKey.Label), string(edgeKey.Target))
	}
	name := paramFieldName(labelParts[0])
	if !exportedGoIdent(name) {
		return "", fmt.Errorf("%w: edge type label %q mangles to %q, not a valid exported Go identifier", ErrInvalidEntityName, string(edgeKey.Label), name)
	}
	return name, nil
}

// exportedGoIdent reports whether s matches ^[A-Z][A-Za-z0-9]*$ — the
// exported-Go-identifier grammar spec §4.5 Rule 1 pins for entity names.
// ASCII-only; Unicode escape hatch lives on field-name mangle only.
func exportedGoIdent(s string) bool {
	return exportedGoIdentRe.MatchString(s)
}

// entityAxisText renders a human-readable source-axis fragment for a
// fail-message: "node type Person&Employee" or
// "edge type (Person -[:KNOWS]-> Company)".
func entityAxisText(kind entityKind, labels graph.LabelSetKey, edgeKey schema.EdgeKey) string {
	if kind == entityNode {
		return fmt.Sprintf("node type %q", string(labels))
	}
	return fmt.Sprintf("edge type (%s -[:%s]-> %s)", string(edgeKey.Source), string(edgeKey.Label), string(edgeKey.Target))
}

// prepareEntityFields derives an entity's per-property field list in
// map-key-sorted order (spec §5.2). Returns the fields, the anyProp bit
// (any property emits, ⇒ fmt used in decode helper), the anyNonNull bit
// (any non-nullable property emits, ⇒ neo4j.GetProperty[T] used), the
// anyTime bit (any property decodes as time.Time — TIMESTAMP), and a
// same-entity field-name collision as ErrPropertyFieldCollision. The
// C3 eager width sweep (§4.8) folds into this pass: a property whose
// width has no faithful Go carrier (INT128 / INT256 / UINT128 /
// UINT256 / FLOAT16 / FLOAT128 / FLOAT256 / DECIMAL) returns
// ErrUnrepresentableWidth naming the entity, property, and width.
// First offender wins across the schema-shape axis.
func prepareEntityFields(entityName string, props map[string]schema.Property) ([]preparedEntityField, bool, bool, bool, error) {
	keys := make([]string, 0, len(props))
	for k := range props {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	fields := make([]preparedEntityField, 0, len(props))
	seen := make(map[string]string, len(props))
	anyProp := false
	anyNonNull := false
	anyTime := false
	for _, k := range keys {
		p := props[k]
		field := paramFieldName(p.Name)
		if first, dup := seen[field]; dup {
			return nil, false, false, false, fmt.Errorf("%w: entity %q properties %q and %q both mangle to %q", ErrPropertyFieldCollision, entityName, first, p.Name, field)
		}
		seen[field] = p.Name
		ty, ok := goType(p.Type)
		if !ok {
			// C3 §4.8 eager width sweep: the eight unrepresentable widths
			// route through ErrUnrepresentableWidth at Phase Z regardless
			// of whether any query projects the offending property.
			return nil, false, false, false, fmt.Errorf("%w: entity %q property %q has %s", ErrUnrepresentableWidth, entityName, p.Name, p.Type)
		}
		fields = append(fields, preparedEntityField{
			PropName: p.Name,
			Field:    field,
			GoType:   ty,
			Nullable: p.Nullable,
		})
		anyProp = true
		if !p.Nullable {
			anyNonNull = true
		}
		if ty == "time.Time" {
			anyTime = true
		}
	}
	return fields, anyProp, anyNonNull, anyTime, nil
}

// phaseAAdmit is spec §2.1's Phase A: gates every query on axes Phase B
// depends on for name derivation. First offender in slice order wins.
// C4 widens cardinality admission to the full {One, Many, Exec} set and
// pairs it with a cardinality × shape gate (spec §4.9): :exec on a
// column-producing query routes through ErrExecOnProjection; :one or
// :many on a zero-column query routes through ErrCardinalityShapeMismatch.
// Column and parameter admission unchanged from C3 (property-widths on
// parameters, full closed sum minus ResolvedEdgeUnion on columns);
// unrepresentable widths route through ErrUnrepresentableWidth (Phase Z
// already caught schema-side offenders so a column projecting an
// unrepresentable-width property is unreachable unless the query declares
// an unrepresentable width on a parameter).
func phaseAAdmit(queries []NamedQuery, entities []preparedEntity, entityIndex map[entityLookupKey]int) error {
	for i, q := range queries {
		if _, reserved := reservedIdentifiers[q.Name]; reserved {
			return fmt.Errorf("%w: query %q at position %d collides with reserved identifier", ErrIdentifierCollision, q.Name, i)
		}
		if q.Cardinality != CardinalityOne && q.Cardinality != CardinalityMany && q.Cardinality != CardinalityExec {
			return fmt.Errorf("%w: query %q at position %d has unrecognised cardinality %d", ErrInvalidCardinality, q.Name, i, q.Cardinality)
		}
		// Cardinality × shape gate (spec §4.9). Runs before the column-type
		// sweep so a fixture combining :exec-on-projection with an
		// unrepresentable-width column fires ErrExecOnProjection first —
		// the caller fixes the cardinality axis before revisiting widths.
		if q.Cardinality == CardinalityExec && len(q.Validated.Columns) > 0 {
			return fmt.Errorf("%w: query %q at position %d has cardinality :exec but projects %d column(s) (first column %q) — drop :exec or drop RETURN", ErrExecOnProjection, q.Name, i, len(q.Validated.Columns), q.Validated.Columns[0].Name)
		}
		if (q.Cardinality == CardinalityOne || q.Cardinality == CardinalityMany) && len(q.Validated.Columns) == 0 {
			shape := "zero-column read"
			if q.Validated.Statement == resolver.StatementWrite {
				shape = "zero-column write"
			}
			return fmt.Errorf("%w: query %q at position %d has cardinality %s but the query is a %s — annotate :exec or add a RETURN clause", ErrCardinalityShapeMismatch, q.Name, i, cardinalityAnnotation(q.Cardinality), shape)
		}
		if strings.ContainsRune(q.SourceText, '`') {
			return fmt.Errorf("%w: query %q at position %d has a backtick in its source text", ErrOutOfC5Scope, q.Name, i)
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
			switch t := col.Type.(type) {
			case resolver.ResolvedProperty:
				if _, ok := goType(t.Type); !ok {
					return fmt.Errorf("%w: query %q column %d %q has %s", ErrUnrepresentableWidth, q.Name, ci, col.Name, t.Type)
				}
			case resolver.ResolvedNode:
				if _, ok := entityIndex[entityLookupKey{Kind: entityNode, Labels: t.Labels}]; !ok {
					// Unknown node type — the resolver's R0 gate should
					// have caught this; a synthetic test seam lands here.
					return fmt.Errorf("%w: query %q column %d %q references unknown node type %q", ErrOutOfC5Scope, q.Name, ci, col.Name, string(t.Labels))
				}
			case resolver.ResolvedEdge:
				if _, ok := entityIndex[entityLookupKey{Kind: entityEdge, EdgeKey: t.EdgeKey}]; !ok {
					return fmt.Errorf("%w: query %q column %d %q references unknown edge type %s -[:%s]-> %s", ErrOutOfC5Scope, q.Name, ci, col.Name, string(t.EdgeKey.Source), string(t.EdgeKey.Label), string(t.EdgeKey.Target))
				}
			case resolver.ResolvedEdgeUnion:
				// C5 admission (§2.1): defensive gates on the resolver-
				// guaranteed invariants. The resolver commits len(EdgeKeys)
				// >= 2 (single-candidate collapses to ResolvedEdge) per R3
				// spec §4.4; codegen reads defensively so a synthetic test
				// seam fails at generation, not downstream. Every candidate
				// must have a Phase Z schema-cache entry — a miss indicates
				// the resolver committed an edge the schema does not declare.
				if len(t.EdgeKeys) < 2 {
					return fmt.Errorf("%w: query %q column %d %q resolved as edgeUnion with only %d candidate(s) — resolver invariant violated (expected >= 2)", ErrOutOfC5Scope, q.Name, ci, col.Name, len(t.EdgeKeys))
				}
				for _, ek := range t.EdgeKeys {
					if _, ok := entityIndex[entityLookupKey{Kind: entityEdge, EdgeKey: ek}]; !ok {
						return fmt.Errorf("%w: query %q column %d %q edgeUnion candidate %s -[:%s]-> %s not declared by schema", ErrOutOfC5Scope, q.Name, ci, col.Name, string(ek.Source), string(ek.Label), string(ek.Target))
					}
				}
			case resolver.ResolvedTemporal:
				// Every temporal kind is representable; the closed enum
				// maps into the temporal Go type table (§5.1) without a
				// fallible dispatch.
			case resolver.ResolvedScalar:
				// Every scalar kind is representable at C3 — bool /
				// int64 / float64 / string / any / map[string]any.
			case resolver.ResolvedUnknown:
				// Honest-any leaf (§3.3). Fully in-scope; the emission
				// walks the record.Get path.
			case resolver.ResolvedList:
				// Recurse the list-element chain to find unrepresentable
				// leaves (§4.7). The Go type text is derived at Phase B;
				// here we only fail if the recursion itself rejects. C5
				// widens the recursion to synthesise the edgeUnion
				// interface name for a ResolvedEdgeUnion leaf, threading
				// the ambient query + column field through so the leaf's
				// synthesised name matches the top-level Row-field
				// interface. At Phase A we discard the derived text; the
				// call is a validity probe.
				rowField, _ := rowFieldName(col.Name)
				if _, err := resolvedListGoType(t.Element, entities, entityIndex, q.Name, rowField); err != nil {
					return fmt.Errorf("query %q column %d %q: %w", q.Name, ci, col.Name, err)
				}
			default:
				return fmt.Errorf("%w: query %q column %d %q resolved as %s", ErrOutOfC5Scope, q.Name, ci, col.Name, col.Type.String())
			}
		}
		for pi, p := range q.Validated.Parameters {
			prop, ok := p.Type.(resolver.ResolvedProperty)
			if !ok {
				return fmt.Errorf("%w: query %q parameter %d $%s resolved as %s (non-property parameters are post-v1)", ErrOutOfC5Scope, q.Name, pi, p.Name, p.Type.String())
			}
			if _, ok := goType(prop.Type); !ok {
				return fmt.Errorf("%w: query %q parameter %d $%s has %s", ErrUnrepresentableWidth, q.Name, pi, p.Name, prop.Type)
			}
		}
	}
	return nil
}

// phaseBDerive is spec §2.1's Phase B: derives names for the method,
// Params fields, and Row fields; runs per-query collision checks. Phase A
// guarantees columns are ResolvedProperty / ResolvedNode / ResolvedEdge
// with a resolved entity index entry (for the latter two), so lookups
// cannot fail here.
func phaseBDerive(queries []NamedQuery, entities []preparedEntity, entityIndex map[entityLookupKey]int) ([]preparedQuery, error) {
	out := make([]preparedQuery, 0, len(queries))
	for _, q := range queries {
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
				return nil, fmt.Errorf("%w: query %q parameter %d $%s: internal invariant — Phase A missed non-property type %s", ErrOutOfC5Scope, q.Name, pi, param.Name, param.Type.String())
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

			switch t := col.Type.(type) {
			case resolver.ResolvedProperty:
				ty, _ := goType(t.Type)
				p.RowFields = append(p.RowFields, preparedRow{
					ColumnName: col.Name,
					Field:      field,
					GoType:     ty,
					Nullable:   t.Nullable,
					Kind:       columnProperty,
				})
			case resolver.ResolvedNode:
				idx := entityIndex[entityLookupKey{Kind: entityNode, Labels: t.Labels}]
				p.RowFields = append(p.RowFields, preparedRow{
					ColumnName: col.Name,
					Field:      field,
					GoType:     entities[idx].Name,
					Nullable:   t.Nullable,
					Kind:       columnNode,
				})
			case resolver.ResolvedEdge:
				idx := entityIndex[entityLookupKey{Kind: entityEdge, EdgeKey: t.EdgeKey}]
				p.RowFields = append(p.RowFields, preparedRow{
					ColumnName: col.Name,
					Field:      field,
					GoType:     entities[idx].Name,
					Nullable:   t.Nullable,
					Kind:       columnEdge,
				})
			case resolver.ResolvedTemporal:
				p.RowFields = append(p.RowFields, preparedRow{
					ColumnName: col.Name,
					Field:      field,
					GoType:     temporalGoType(t.Kind),
					Kind:       columnTemporal,
				})
			case resolver.ResolvedScalar:
				ty := scalarGoType(t.Kind)
				kind := columnScalar
				// ScalarNull decodes through record.Get (§5.5) — no
				// GetRecordValue overload for a bare `any`. ScalarMap
				// has a legitimate GetRecordValue[map[string]any] arm.
				if t.Kind == resolver.ScalarNull {
					kind = columnAny
				}
				p.RowFields = append(p.RowFields, preparedRow{
					ColumnName: col.Name,
					Field:      field,
					GoType:     ty,
					Kind:       kind,
				})
			case resolver.ResolvedUnknown:
				p.RowFields = append(p.RowFields, preparedRow{
					ColumnName: col.Name,
					Field:      field,
					GoType:     "any",
					Kind:       columnAny,
				})
			case resolver.ResolvedEdgeUnion:
				// C5 edgeUnion synthesis (§4.10): interface name is
				// <QueryName><RowFieldName>; candidates are the schema's
				// entity struct names in resolver-canonical EdgeKeys order.
				// Every candidate has a Phase A guarantee of a schema-cache
				// entry (§2.1), so the lookup is infallible here.
				interfaceName := q.Name + field
				candidates := make([]string, len(t.EdgeKeys))
				for i, ek := range t.EdgeKeys {
					candidates[i] = entities[entityIndex[entityLookupKey{Kind: entityEdge, EdgeKey: ek}]].Name
				}
				p.EdgeUnions = append(p.EdgeUnions, preparedEdgeUnion{
					QueryName:     q.Name,
					ColumnPos:     ci,
					ColumnName:    col.Name,
					FieldName:     field,
					InterfaceName: interfaceName,
					EdgeKeys:      t.EdgeKeys,
					Candidates:    candidates,
				})
				p.RowFields = append(p.RowFields, preparedRow{
					ColumnName: col.Name,
					Field:      field,
					GoType:     interfaceName,
					Nullable:   t.Nullable,
					Kind:       columnEdgeUnion,
					EdgeKeys:   t.EdgeKeys,
				})
			case resolver.ResolvedList:
				inner, err := resolvedListGoType(t.Element, entities, entityIndex, q.Name, field)
				if err != nil {
					return nil, fmt.Errorf("query %q column %d %q: %w", q.Name, ci, col.Name, err)
				}
				// list-of-edgeUnion at a leaf synthesises a preparedEdgeUnion
				// so models.go emits the interface + marker methods (§5.2).
				// The leaf's synthesised interface name matches the top-level
				// column's field name — every element of the list satisfies
				// the same sealed sum.
				if leafEK, isEdgeUnion := findEdgeUnionLeaf(t.Element); isEdgeUnion {
					candidates := make([]string, len(leafEK))
					for i, ek := range leafEK {
						candidates[i] = entities[entityIndex[entityLookupKey{Kind: entityEdge, EdgeKey: ek}]].Name
					}
					p.EdgeUnions = append(p.EdgeUnions, preparedEdgeUnion{
						QueryName:     q.Name,
						ColumnPos:     ci,
						ColumnName:    col.Name,
						FieldName:     field,
						InterfaceName: q.Name + field,
						EdgeKeys:      leafEK,
						Candidates:    candidates,
					})
				}
				p.RowFields = append(p.RowFields, preparedRow{
					ColumnName: col.Name,
					Field:      field,
					GoType:     "[]" + inner,
					Kind:       columnList,
					ListElem:   t.Element,
				})
			default:
				return nil, fmt.Errorf("%w: query %q column %d %q: internal invariant — Phase A missed non-property type %s", ErrOutOfC5Scope, q.Name, ci, col.Name, col.Type.String())
			}
		}

		out = append(out, p)
	}
	return out, nil
}

// sweepIdentifiers runs spec §4.6's exported-identifier collision sweep
// across every emitted top-level identifier. Six sources at C5, in
// insertion order (§2.2 / §5.7):
//
//  1. entity struct names (C2)
//  2. entity decode helper names (`decode<Name>`, promoted to sweep at C5)
//  3. method names (C1)
//  4. `<Method>Params` for two-plus-param queries (C1)
//  5. `<Method>Row` for two-plus-column queries (C1)
//  6. edgeUnion interface names, per-query-column (C5)
//
// First insertion-order duplicate wins. C0 skeleton identifiers
// (Queries / New / WithTx / ReadQuerier / WriteQuerier / Querier) and
// the ErrNoRows / ErrMultipleResults sentinels are gate-checked by
// Phase A's reserved-identifier match, so they never appear here.
// Marker method names (source 6's per-candidate satisfier) and
// <methodName>QueryText consts are unexported and stay off the sweep
// (§4.6 defence): a marker collision is caught by the interface-name
// axis first, and a QueryText collision is caught by the method-name
// axis first.
func sweepIdentifiers(entities []preparedEntity, prepared []preparedQuery) error {
	seen := make(map[string]string, len(entities)*2+len(prepared)*3)
	insert := func(ident, source string) error {
		if first, dup := seen[ident]; dup {
			return fmt.Errorf("%w: identifier %q emitted by both %s and %s", ErrIdentifierCollision, ident, first, source)
		}
		seen[ident] = source
		return nil
	}
	// Source 1: entity struct names.
	for _, e := range entities {
		var srcAxis string
		if e.Kind == entityNode {
			srcAxis = fmt.Sprintf("entity struct %q (schema labels %q)", e.Name, string(e.Labels))
		} else {
			srcAxis = fmt.Sprintf("entity struct %q (schema edge %s -[:%s]-> %s)", e.Name, string(e.EdgeKey.Source), string(e.EdgeKey.Label), string(e.EdgeKey.Target))
		}
		if err := insert(e.Name, srcAxis); err != nil {
			return err
		}
	}
	// Source 2: entity decode helper names. Same insertion order as
	// entity structs. Unexported by construction but promoted to the
	// sweep at C5 so a future exported-decode-helper refactor cannot
	// blow past the invariant (§2.2 defence).
	for _, e := range entities {
		if err := insert("decode"+e.Name, fmt.Sprintf("entity decode helper %q for entity struct %q", "decode"+e.Name, e.Name)); err != nil {
			return err
		}
	}
	// Sources 3-5: method / Params / Row.
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
	// Source 6: edgeUnion interface names, per-query-column in
	// Input.Queries slice order sub-ordered by column position.
	for _, p := range prepared {
		for _, u := range p.EdgeUnions {
			if err := insert(u.InterfaceName, fmt.Sprintf("edgeUnion interface %q for query %q column %d %q", u.InterfaceName, p.Name, u.ColumnPos, u.ColumnName)); err != nil {
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
		return neo4j.ExecuteWrite(ctx, session, func(tx neo4j.ManagedTransaction) ([]*neo4j.Record, error) {
			result, err := tx.Run(ctx, cypher, params)
			if err != nil {
				return nil, err
			}
			return result.Collect(ctx)
		})
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

// renderQuerier emits querier.go (spec §5.4). ReadQuerier lists every
// method whose Validated.Statement == StatementRead in Input.Queries
// order; WriteQuerier lists every StatementWrite method in the same
// filtered order. A method belongs to exactly one interface — the
// partition is on Statement, not on Cardinality (a :one write-with-
// projection lands in WriteQuerier; a :exec on a call-with-no-yield
// lands in ReadQuerier). The compile-time assertion on the last line
// catches method-name drift.
func renderQuerier(pkg string, prepared []preparedQuery) []byte {
	var b strings.Builder
	b.WriteString(header())
	b.WriteString("package ")
	b.WriteString(pkg)
	b.WriteString("\n\n")
	// Import set: context always (for method signatures); dbtype iff a
	// method signature names a dbtype.<Kind>; time iff a signature names
	// time.Time. The signature-search runs over Params and Row types.
	needDbtype, needTime := querierImports(prepared)
	if len(prepared) > 0 {
		if needDbtype || needTime {
			b.WriteString("import (\n\t\"context\"\n")
			if needTime {
				b.WriteString("\t\"time\"\n")
			}
			if needDbtype {
				b.WriteString("\n\t\"github.com/neo4j/neo4j-go-driver/v5/neo4j/dbtype\"\n")
			}
			b.WriteString(")\n\n")
		} else {
			b.WriteString("import \"context\"\n\n")
		}
	}
	b.WriteString("type ReadQuerier interface {\n")
	for _, p := range prepared {
		if p.Validated.Statement != resolver.StatementRead {
			continue
		}
		b.WriteString("\t")
		writeMethodSignature(&b, p)
		b.WriteString("\n")
	}
	b.WriteString("}\n\n")
	b.WriteString("type WriteQuerier interface {\n")
	for _, p := range prepared {
		if p.Validated.Statement != resolver.StatementWrite {
			continue
		}
		b.WriteString("\t")
		writeMethodSignature(&b, p)
		b.WriteString("\n")
	}
	b.WriteString("}\n\n")
	b.WriteString("type Querier interface {\n\tReadQuerier\n\tWriteQuerier\n}\n\n")
	b.WriteString("var _ Querier = (*Queries)(nil)\n")
	return []byte(b.String())
}

// querierImports scans every prepared query's method signature (params
// + return type) for dbtype / time references. Multi-column returns
// use the MethodNameRow struct name (the struct itself lives in the
// .cypher.go file, whose imports carry any dbtype / time it needs);
// single-column returns and every parameter surface the carrier
// directly in the signature. The querier interface file needs an
// import when — and only when — its method signature strings contain
// the carrier.
func querierImports(prepared []preparedQuery) (needDbtype, needTime bool) {
	scan := func(ty string) {
		if strings.Contains(ty, "dbtype.") {
			needDbtype = true
		}
		if strings.Contains(ty, "time.Time") {
			needTime = true
		}
	}
	for _, p := range prepared {
		// Parameters appear verbatim in every method signature.
		for _, param := range p.ParamFields {
			scan(param.GoType)
		}
		// Return type: bare row Go type for single-column projections;
		// MethodNameRow (no import needed) otherwise.
		if len(p.RowFields) == 1 {
			scan(p.RowFields[0].GoType)
		}
	}
	return needDbtype, needTime
}

// renderModels emits models.go (spec §5.2). C2 emits one exported
// struct per schema NodeType and EdgeType (Phase Z order) plus an
// unexported decode<Name> helper. C5 adds two blocks:
//
//   - Marker methods on each candidate entity struct, one per edgeUnion
//     interface it participates in. Emitted between the struct
//     declaration and the decode helper so a reader following the entity
//     sees shape → sum-membership → decode.
//   - EdgeUnion interface declarations, per-query-column in
//     Input.Queries slice order sub-ordered by column position, with a
//     `//sumtype:decl` comment line above each.
//
// The import set is a template invariant on schema shape:
//
//   - dbtype: unconditional (decode helpers take dbtype.Node /
//     dbtype.Relationship)
//   - fmt iff any property is decoded (decode-error wrapping)
//   - neo4j iff any non-nullable property is decoded (neo4j.GetProperty[T])
//
// EdgeUnion emission adds no new import (the interface + marker methods
// live in this package; no cross-package reference emerges). A schema
// with zero entity types emits an empty body — package clause only —
// matching C1's byte-empty models.go (§7 "silently accepted").
func renderModels(pkg string, entities []preparedEntity, prepared []preparedQuery) []byte {
	if len(entities) == 0 {
		return []byte(header() + `package ` + pkg + `
`)
	}

	// Collect edgeUnion interfaces across every query, preserving
	// Input.Queries slice order sub-ordered by column position.
	// markersByEntity maps entity-struct name -> ordered interface
	// names it satisfies, deduplicated so an entity that appears twice
	// in an EdgeKeys slice (impossible — resolver commits distinct
	// candidates) or across two per-query columns projecting the same
	// interface (impossible — per-query-column naming) still emits one
	// marker per interface participation.
	var unions []preparedEdgeUnion
	markersByEntity := make(map[string][]string)
	seenMarker := make(map[string]struct{})
	for _, p := range prepared {
		for _, u := range p.EdgeUnions {
			unions = append(unions, u)
			for _, cand := range u.Candidates {
				key := cand + "\x00" + u.InterfaceName
				if _, dup := seenMarker[key]; dup {
					continue
				}
				seenMarker[key] = struct{}{}
				markersByEntity[cand] = append(markersByEntity[cand], u.InterfaceName)
			}
		}
	}

	anyProp := false
	anyNonNull := false
	anyTime := false
	for _, e := range entities {
		if e.AnyProp {
			anyProp = true
		}
		if e.AnyNonNull {
			anyNonNull = true
		}
		if e.AnyTime {
			anyTime = true
		}
	}

	var b strings.Builder
	b.WriteString(header())
	b.WriteString("package ")
	b.WriteString(pkg)
	b.WriteString("\n\n")

	// Imports: dbtype is unconditional (every helper's argument type);
	// fmt gates on anyProp; time gates on anyTime (TIMESTAMP property);
	// neo4j gates on anyNonNull. Alphabetical: fmt, time, then external
	// neo4j / dbtype.
	b.WriteString("import (\n")
	if anyProp {
		b.WriteString("\t\"fmt\"\n")
	}
	if anyTime {
		b.WriteString("\t\"time\"\n")
	}
	if anyProp || anyTime {
		b.WriteString("\n")
	}
	if anyNonNull {
		b.WriteString("\t\"github.com/neo4j/neo4j-go-driver/v5/neo4j\"\n")
	}
	b.WriteString("\t\"github.com/neo4j/neo4j-go-driver/v5/neo4j/dbtype\"\n")
	b.WriteString(")\n\n")

	for i, e := range entities {
		if i > 0 {
			b.WriteString("\n")
		}
		writeEntityStruct(&b, e)
		if markers := markersByEntity[e.Name]; len(markers) > 0 {
			b.WriteString("\n")
			for _, iface := range markers {
				fmt.Fprintf(&b, "func (%s) is%s() {}\n", e.Name, iface)
			}
		}
		b.WriteString("\n")
		writeEntityDecodeHelper(&b, e)
	}

	// EdgeUnion interface declarations, appended after entity blocks,
	// one per synthesised per-query-column interface in emission order.
	for _, u := range unions {
		b.WriteString("\n//sumtype:decl\n")
		fmt.Fprintf(&b, "type %s interface{ is%s() }\n", u.InterfaceName, u.InterfaceName)
	}
	return []byte(b.String())
}

// writeEntityStruct emits the exported struct declaration for one entity.
// Zero-property entities emit an empty struct declaration (§7 "silently
// accepted"). Doc comment names the source-side axis (labels or edge key).
func writeEntityStruct(b *strings.Builder, e preparedEntity) {
	if e.Kind == entityNode {
		fmt.Fprintf(b, "// %s corresponds to the %s node type.\n", e.Name, e.DocAxis)
	} else {
		fmt.Fprintf(b, "// %s corresponds to the %s.\n", e.Name, e.DocAxis)
	}
	fmt.Fprintf(b, "type %s struct {\n", e.Name)
	for _, f := range e.Fields {
		if f.Nullable {
			fmt.Fprintf(b, "\t%s *%s\n", f.Field, f.GoType)
		} else {
			fmt.Fprintf(b, "\t%s %s\n", f.Field, f.GoType)
		}
	}
	b.WriteString("}\n")
}

// writeEntityDecodeHelper emits the unexported decode<Name> helper for
// one entity. Nullable properties go through direct Props lookup + type
// assertion (three-way outcome); non-nullable properties go through
// neo4j.GetProperty[T] (missing key is a decode error).
func writeEntityDecodeHelper(b *strings.Builder, e preparedEntity) {
	var carrier, arg string
	if e.Kind == entityNode {
		carrier = "dbtype.Node"
		arg = "node"
	} else {
		carrier = "dbtype.Relationship"
		arg = "rel"
	}
	fmt.Fprintf(b, "// decode%s decodes a driver %s into a %s struct,\n", e.Name, carrier, e.Name)
	b.WriteString("// enforcing per-property nullability against the schema.\n")
	fmt.Fprintf(b, "func decode%s(%s %s) (%s, error) {\n", e.Name, arg, carrier, e.Name)
	fmt.Fprintf(b, "\tvar out %s\n", e.Name)
	for _, f := range e.Fields {
		writeEntityFieldDecode(b, e, f, arg)
	}
	b.WriteString("\treturn out, nil\n")
	b.WriteString("}\n")
}

// writeEntityFieldDecode emits one field's decode block. Nullable path:
// Props lookup + type assertion against the driver's carrier + narrow-
// convert into a local of the emitted Go type + address-of-local into
// the pointer field. Non-nullable path: neo4j.GetProperty[<carrier>] +
// narrow-convert. The property key is the source property name
// (Property.Name), not the derived field name — the driver map is
// keyed on the schema-side name. Extended at C3 to cover DATE
// (dbtype.Date carrier) and TIMESTAMP (time.Time carrier); FLOAT32's
// nullable arm now narrows correctly (was a latent bug, no fixture
// exercised it before C3).
func writeEntityFieldDecode(b *strings.Builder, e preparedEntity, f preparedEntityField, arg string) {
	carrier := driverCarrier(f.GoType)
	if f.Nullable {
		fmt.Fprintf(b, "\tif v, ok := %s.Props[%q]; ok {\n", arg, f.PropName)
		fmt.Fprintf(b, "\t\ts, ok := v.(%s)\n", carrier)
		b.WriteString("\t\tif !ok {\n")
		fmt.Fprintf(b, "\t\t\treturn %s{}, fmt.Errorf(\"decode %s.%s: property %%q: expected %s, got %%T\", %q, v)\n", e.Name, e.Name, f.Field, carrier, f.PropName)
		b.WriteString("\t\t}\n")
		if carrier != f.GoType {
			fmt.Fprintf(b, "\t\tnarrowed := %s(s)\n", f.GoType)
			fmt.Fprintf(b, "\t\tout.%s = &narrowed\n", f.Field)
		} else {
			fmt.Fprintf(b, "\t\tout.%s = &s\n", f.Field)
		}
		b.WriteString("\t}\n")
		return
	}
	local := lowerFirstRune(f.Field)
	fmt.Fprintf(b, "\t%s, err := neo4j.GetProperty[%s](%s, %q)\n", local, carrier, arg, f.PropName)
	b.WriteString("\tif err != nil {\n")
	fmt.Fprintf(b, "\t\treturn %s{}, fmt.Errorf(\"decode %s.%s: %%w\", err)\n", e.Name, e.Name, f.Field)
	b.WriteString("\t}\n")
	if carrier != f.GoType {
		fmt.Fprintf(b, "\tout.%s = %s(%s)\n", f.Field, f.GoType, local)
	} else {
		fmt.Fprintf(b, "\tout.%s = %s\n", f.Field, local)
	}
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

// columnNeedsImports reports whether one prepared row needs dbtype /
// time in the enclosing file's import block. The list arm walks the
// row's carried element type recursively; every other arm delegates to
// a per-kind test on the row's emitted Go type.
func columnNeedsImports(f preparedRow) (needDbtype, needTime bool) {
	switch f.Kind {
	case columnNode, columnEdge, columnEdgeUnion:
		// edgeUnion decode type-asserts dbtype.Relationship (§5.5); the
		// column's emitted Go type is the sealed interface (not a
		// dbtype.* text), so goTypeNeedsImports does not fire and the
		// need is declared here.
		return true, false
	case columnTemporal, columnProperty:
		return goTypeNeedsImports(f.GoType)
	case columnScalar, columnAny:
		return false, false
	case columnList:
		// Walk the element chain for any nested carrier requirement.
		var walk func(resolver.ResolvedType) (bool, bool)
		walk = func(t resolver.ResolvedType) (bool, bool) {
			switch tt := t.(type) {
			case resolver.ResolvedProperty:
				ty, ok := goType(tt.Type)
				if !ok {
					return false, false
				}
				return goTypeNeedsImports(ty)
			case resolver.ResolvedNode, resolver.ResolvedEdge, resolver.ResolvedEdgeUnion:
				return true, false
			case resolver.ResolvedTemporal:
				return goTypeNeedsImports(temporalGoType(tt.Kind))
			case resolver.ResolvedList:
				return walk(tt.Element)
			}
			return false, false
		}
		return walk(f.ListElem)
	}
	return false, false
}

// goTypeNeedsImports reports whether a Go type text names dbtype or
// time. Both are single-string prefix checks; the emitted type text
// never nests dbtype/time except through the list arm (which walks
// element-wise above).
func goTypeNeedsImports(ty string) (bool, bool) {
	needDbtype := strings.HasPrefix(ty, "dbtype.")
	needTime := ty == "time.Time"
	return needDbtype, needTime
}

// renderCypherFile emits one <name>.cypher.go file (spec §5.5). Per
// query in order: query-text const, Params struct (if any), Row struct
// (if any), method. The withDbtype flag toggles the dbtype import; the
// withTime flag toggles the time-stdlib import (C3, for TIMESTAMP /
// TemporalDateTime carriers). The withFmt flag toggles the fmt import
// (C4: a write-only file whose queries are all :exec emits no
// fmt.Errorf wrapper, so fmt is elided). The row-assembly template
// inlines the per-kind decode arm.
func renderCypherFile(pkg string, queries []preparedQuery, withDbtype, withTime, withFmt bool) []byte {
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
	b.WriteString("\n\t\"github.com/neo4j/neo4j-go-driver/v5/neo4j\"\n")
	if withDbtype {
		b.WriteString("\t\"github.com/neo4j/neo4j-go-driver/v5/neo4j/dbtype\"\n")
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
		fmt.Fprintf(b, "\t_, err := q.db.run(ctx, %sQueryText, %s, %s)\n", p.Bare, paramsMapText(p), accessModeText(p))
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

// accessModeText picks the fourth q.db.run argument for one prepared
// query — AccessModeWrite iff Validated.Statement == StatementWrite,
// AccessModeRead otherwise. The dispatch runs once per emitted method
// at generation time (spec §5.5's access-mode threading rule); the
// emitted body carries the constant, not a runtime branch.
func accessModeText(p preparedQuery) string {
	if p.Validated.Statement == resolver.StatementWrite {
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
	fmt.Fprintf(b, "\trecords, err := q.db.run(ctx, %sQueryText, %s, %s)\n", p.Bare, paramsMapText(p), accessModeText(p))
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
// §5.1). Returns (typeText, ok): ok=false for the eight unrepresentable
// widths (INT128 / INT256 / UINT128 / UINT256 / FLOAT16 / FLOAT128 /
// FLOAT256 / DECIMAL) — caller routes to ErrUnrepresentableWidth naming
// the width. Callers append a leading '*' for nullable columns and
// parameters at emission time. DATE / TIMESTAMP are in-scope at C3 and
// return "dbtype.Date" / "time.Time"; FLOAT32 returns "float32" (the
// carrier-widens-on-encode / narrow-on-decode contract is enforced at
// the emission sites, spec §5.5 / §5.7).
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
	case graph.TypeDate:
		return "dbtype.Date", true
	case graph.TypeTimestamp:
		return "time.Time", true
	case graph.TypeInt128, graph.TypeInt256,
		graph.TypeUint128, graph.TypeUint256,
		graph.TypeFloat16, graph.TypeFloat128, graph.TypeFloat256,
		graph.TypeDecimal:
		// The eight unrepresentable widths — no faithful Go carrier on
		// neo4j-go-driver/v5. Permanent, per §9 (spec).
		return "", false
	}
	return "", false
}

// temporalGoType maps a resolver Temporal kind to the Go type text C3
// emits (spec §5.1 column-shape table). Every result is a
// dbtype.<Kind> or time.Time — one dispatch on the closed enum.
func temporalGoType(k resolver.Temporal) string {
	switch k {
	case resolver.TemporalDate:
		return "dbtype.Date"
	case resolver.TemporalTime:
		return "dbtype.Time"
	case resolver.TemporalLocalTime:
		return "dbtype.LocalTime"
	case resolver.TemporalDateTime:
		return "time.Time"
	case resolver.TemporalLocalDateTime:
		return "dbtype.LocalDateTime"
	case resolver.TemporalDuration:
		return "dbtype.Duration"
	}
	// Unreachable: Temporal is a closed enum.
	return "any"
}

// scalarGoType maps a resolver Scalar kind to the Go type text C3
// emits (spec §5.1 column-shape table). Bool / Int / Float / String
// bridge to the driver's native carriers; Null → any (the openCypher
// null literal is legal-but-pointless projection); Map → map[string]any.
func scalarGoType(k resolver.Scalar) string {
	switch k {
	case resolver.ScalarBool:
		return "bool"
	case resolver.ScalarInt:
		return "int64"
	case resolver.ScalarFloat:
		return "float64"
	case resolver.ScalarString:
		return "string"
	case resolver.ScalarNull:
		return "any"
	case resolver.ScalarMap:
		return "map[string]any"
	}
	return "any"
}

// resolvedListGoType derives the Go type text for a ResolvedType leaf
// or nested ResolvedList (spec §2.2, §4.7). Returns (text, err):
// err wraps ErrUnrepresentableWidth for a leaf property width that is
// unrepresentable. A ResolvedList element recurses; every other leaf is
// one dispatch on the ResolvedType sum. C5 widens the sum to admit
// ResolvedEdgeUnion leaves — the derived text is the ambient query's
// synthesised interface name (<queryName><columnField>); the extra
// parameters are inert for every non-edgeUnion arm.
func resolvedListGoType(t resolver.ResolvedType, entities []preparedEntity, entityIndex map[entityLookupKey]int, queryName, columnField string) (string, error) {
	switch tt := t.(type) {
	case resolver.ResolvedProperty:
		ty, ok := goType(tt.Type)
		if !ok {
			return "", fmt.Errorf("%w: list element has unrepresentable property width %s", ErrUnrepresentableWidth, tt.Type)
		}
		return ty, nil
	case resolver.ResolvedNode:
		idx, ok := entityIndex[entityLookupKey{Kind: entityNode, Labels: tt.Labels}]
		if !ok {
			return "", fmt.Errorf("%w: list element references unknown node type %q", ErrOutOfC5Scope, string(tt.Labels))
		}
		return entities[idx].Name, nil
	case resolver.ResolvedEdge:
		idx, ok := entityIndex[entityLookupKey{Kind: entityEdge, EdgeKey: tt.EdgeKey}]
		if !ok {
			return "", fmt.Errorf("%w: list element references unknown edge type %s -[:%s]-> %s", ErrOutOfC5Scope, string(tt.EdgeKey.Source), string(tt.EdgeKey.Label), string(tt.EdgeKey.Target))
		}
		return entities[idx].Name, nil
	case resolver.ResolvedEdgeUnion:
		// C5 recursion arm: leaf synthesises the same interface name the
		// top-level Row field would emit (§4.7). Phase A already asserted
		// len(EdgeKeys) >= 2 and cache membership for the top-level column;
		// list-of-edgeUnion at Phase A calls into this recursion path only
		// for the validity probe, and Phase B repeats the derivation at
		// emission time — so a schema-cache miss at the leaf indicates a
		// resolver-produced foreign edge and routes through ErrOutOfC5Scope
		// for uniformity with the top-level edgeUnion admission arm.
		if len(tt.EdgeKeys) < 2 {
			return "", fmt.Errorf("%w: list element resolved as edgeUnion with only %d candidate(s) — resolver invariant violated (expected >= 2)", ErrOutOfC5Scope, len(tt.EdgeKeys))
		}
		for _, ek := range tt.EdgeKeys {
			if _, ok := entityIndex[entityLookupKey{Kind: entityEdge, EdgeKey: ek}]; !ok {
				return "", fmt.Errorf("%w: list element edgeUnion candidate %s -[:%s]-> %s not declared by schema", ErrOutOfC5Scope, string(ek.Source), string(ek.Label), string(ek.Target))
			}
		}
		return queryName + columnField, nil
	case resolver.ResolvedTemporal:
		return temporalGoType(tt.Kind), nil
	case resolver.ResolvedScalar:
		return scalarGoType(tt.Kind), nil
	case resolver.ResolvedUnknown:
		return "any", nil
	case resolver.ResolvedList:
		inner, err := resolvedListGoType(tt.Element, entities, entityIndex, queryName, columnField)
		if err != nil {
			return "", err
		}
		return "[]" + inner, nil
	}
	return "", fmt.Errorf("%w: list element has unknown resolved type %s", ErrOutOfC5Scope, t.String())
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

// findEdgeUnionLeaf walks a list-element chain looking for an
// edgeUnion leaf, returning the leaf's EdgeKeys and true when found.
// Nested lists recurse; anything else terminates the search. Called at
// Phase B to synthesise a preparedEdgeUnion (§4.7 recursion arm, §5.2
// emission) for a list-of-edgeUnion column. A list whose leaf is any
// non-edgeUnion type returns (nil, false) — no marker method emission
// is needed and the list arm decodes the leaf through its own arm.
func findEdgeUnionLeaf(t resolver.ResolvedType) ([]schema.EdgeKey, bool) {
	switch tt := t.(type) {
	case resolver.ResolvedEdgeUnion:
		return tt.EdgeKeys, true
	case resolver.ResolvedList:
		return findEdgeUnionLeaf(tt.Element)
	}
	return nil, false
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
