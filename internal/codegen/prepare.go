package codegen

import (
	"cmp"
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/areqag/gqlc/internal/graph"
	"github.com/areqag/gqlc/internal/resolver"
	"github.com/areqag/gqlc/internal/schema"
)

// packageIdent is the Go package-identifier grammar (spec §5.1). Digits
// inside are legal; underscores are legal; digit-leading is not; non-ASCII
// is not.
var packageIdent = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

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

// accessMode is the closed prepare-side enum committing the neo4j
// session AccessMode axis (spec §1.1). Two values only. Kept
// prepare-local and target-independent — render maps to the emitted
// token at the single accessModeText call site.
type accessMode int

const (
	accessModeRead accessMode = iota
	accessModeWrite
)

// preparedQuery bundles the per-query derivations produced by Phase B —
// the derived method surface, the Params/Row shapes, and the resolved
// axes Phase A already gate-checked. Kept together so the per-source
// emission walk reads one struct per query in order (spec §5.5) rather
// than re-deriving each field from NamedQuery.Validated.
type preparedQuery struct {
	NamedQuery
	MethodName  string               // verbatim NamedQuery.Name
	Bare        string               // lowerCamel first rune of MethodName
	AccessMode  accessMode           // §1.1 — closed enum committed at Phase B
	IsWrite     bool                 // §1.2 — Validated.Statement == StatementWrite
	ParamFields []preparedParam      // in Validated.Parameters order
	RowFields   []preparedRow        // in Validated.Columns order
	EdgeUnions  []*preparedEdgeUnion // in Validated.Columns order (sub-ordered by column position); one per columnEdgeUnion Row field (C5). Pointer-stable so a preparedListElem's UnionIdx into this slice survives slice growth (spec §3.1).
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
	Kind       columnKind        // property (C1) or entity — property/node/edge (C2); temporal/list/scalar/any (C3); edgeUnion (C5)
	ListElem   *preparedListElem // non-nil iff Kind == columnList — the committed element decode plan (spec §1.3)
	EdgeKeys   []schema.EdgeKey  // populated when Kind == columnEdgeUnion — the candidate edge keys in resolver-canonical order (§5.5)
}

// preparedListElem is Phase B's committed list-element decode plan
// (spec §1.3). Every list column's ListElem is non-nil; the element's
// arm — Kind, from the same closed columnKind enum as the top-level
// row's Kind — plus the derived carrier / entity / union coordinates
// let the render-side loop body walk one struct per element, never a
// resolver type. Nested lists carry a Nested plan for the inner
// iteration.
type preparedListElem struct {
	// Kind is the same closed columnKind used at the top level. A future
	// resolver variant lands in exactly one place — here and at the top
	// level's Phase B assignment — and both switches fail to compile
	// until it is handled.
	Kind columnKind
	// GoType is the emitted Go type text for one element — a native Go
	// type (`int64`, `string`), a schema-derived entity struct name, or
	// a synthesised edgeUnion interface name.
	GoType string
	// Carrier is the driver's carry type for a Property arm's
	// GetRecordValue / type-assert (`int64` for narrow ints, `float64`
	// for narrow floats). Empty string when Carrier == GoType or the arm
	// does not use a carrier.
	Carrier string
	// UsesConvert reports whether the Property arm must emit a
	// `GoType(v)` narrow-convert after asserting the carrier.
	UsesConvert bool
	// EntityName is the schema-derived struct name for the Node / Edge
	// arms — feeds the `decode<EntityName>` helper call.
	EntityName string
	// UnionIdx is the index into the owning preparedQuery.EdgeUnions
	// slice for the EdgeUnion arm. Index is chosen over a pointer so a
	// future Phase B edit that reorders EdgeUnions appends around the
	// plan-build call cannot leave a stale pointer behind (spec §5.2).
	// Zero for every non-EdgeUnion arm.
	UnionIdx int
	// Nested is the inner element plan for a nested list. Non-nil iff
	// Kind == columnList.
	Nested *preparedListElem
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
	// columnScalarNull is the list-element split of ScalarNull off
	// columnScalar (spec §1.3). At the top level ScalarNull continues to
	// route through columnAny (unchanged bytes; both dispatch to
	// writeAnyColumnDecodeIndent). Inside a list-element plan the arm
	// distinguishes bare-append `any` from a typed-scalar assertion — the
	// former needs no index variable and no type check. Kept on the same
	// closed enum so a new resolver variant lands in exactly one place.
	columnScalarNull
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

// emittedPackage selects the emitted package identifier: a non-empty
// configured name (CLI-1 spec §3.4, WithPackageName) wins after
// validation against the same packageIdent grammar the derivation
// enforces; the empty string keeps the Schema.Name derivation.
func emittedPackage(schemaName, configured string) (string, error) {
	if configured == "" {
		return derivePackage(schemaName)
	}
	if !packageIdent.MatchString(configured) {
		return "", fmt.Errorf("%w: configured package %q", ErrInvalidPackageName, configured)
	}
	return configured, nil
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
			return fmt.Errorf("%w: query %q at position %d has a backtick in its source text", ErrOutOfC6Scope, q.Name, i)
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
					return fmt.Errorf("%w: query %q column %d %q references unknown node type %q", ErrOutOfC6Scope, q.Name, ci, col.Name, string(t.Labels))
				}
			case resolver.ResolvedEdge:
				if _, ok := entityIndex[entityLookupKey{Kind: entityEdge, EdgeKey: t.EdgeKey}]; !ok {
					return fmt.Errorf("%w: query %q column %d %q references unknown edge type %s -[:%s]-> %s", ErrOutOfC6Scope, q.Name, ci, col.Name, string(t.EdgeKey.Source), string(t.EdgeKey.Label), string(t.EdgeKey.Target))
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
					return fmt.Errorf("%w: query %q column %d %q resolved as edgeUnion with only %d candidate(s) — resolver invariant violated (expected >= 2)", ErrOutOfC6Scope, q.Name, ci, col.Name, len(t.EdgeKeys))
				}
				for _, ek := range t.EdgeKeys {
					if _, ok := entityIndex[entityLookupKey{Kind: entityEdge, EdgeKey: ek}]; !ok {
						return fmt.Errorf("%w: query %q column %d %q edgeUnion candidate %s -[:%s]-> %s not declared by schema", ErrOutOfC6Scope, q.Name, ci, col.Name, string(ek.Source), string(ek.Label), string(ek.Target))
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
				// leaves (§4.7). Phase B repeats the walk to commit the
				// plan; here the call is a validity probe — we discard
				// the returned plan. Threading unionIdx = -1 and an
				// empty interface name is inert: Phase A never emits,
				// so neither is read.
				if _, err := buildListElemPlan(t.Element, entities, entityIndex, -1, ""); err != nil {
					return fmt.Errorf("query %q column %d %q: %w", q.Name, ci, col.Name, err)
				}
			default:
				return fmt.Errorf("%w: query %q column %d %q resolved as %s", ErrOutOfC6Scope, q.Name, ci, col.Name, col.Type.String())
			}
		}
		for pi, p := range q.Validated.Parameters {
			prop, ok := p.Type.(resolver.ResolvedProperty)
			if !ok {
				return fmt.Errorf("%w: query %q parameter %d $%s resolved as %s (non-property parameters are post-v1)", ErrOutOfC6Scope, q.Name, pi, p.Name, p.Type.String())
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
		if q.Validated.Statement == resolver.StatementWrite {
			p.AccessMode = accessModeWrite
			p.IsWrite = true
		}

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
				return nil, fmt.Errorf("%w: query %q parameter %d $%s: internal invariant — Phase A missed non-property type %s", ErrOutOfC6Scope, q.Name, pi, param.Name, param.Type.String())
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
				p.EdgeUnions = append(p.EdgeUnions, &preparedEdgeUnion{
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
				// list-of-edgeUnion at a leaf synthesises a preparedEdgeUnion
				// so models.go emits the interface + marker methods (§5.2).
				// The leaf's synthesised interface name matches the top-level
				// column's field name — every element of the list satisfies
				// the same sealed sum. Append first so the plan builder
				// can carry the resolved UnionIdx and interface name
				// (§5.2 index-not-pointer).
				unionIdx := -1
				interfaceName := q.Name + field
				if leafEK, isEdgeUnion := findEdgeUnionLeaf(t.Element); isEdgeUnion {
					candidates := make([]string, len(leafEK))
					for i, ek := range leafEK {
						candidates[i] = entities[entityIndex[entityLookupKey{Kind: entityEdge, EdgeKey: ek}]].Name
					}
					unionIdx = len(p.EdgeUnions)
					p.EdgeUnions = append(p.EdgeUnions, &preparedEdgeUnion{
						QueryName:     q.Name,
						ColumnPos:     ci,
						ColumnName:    col.Name,
						FieldName:     field,
						InterfaceName: interfaceName,
						EdgeKeys:      leafEK,
						Candidates:    candidates,
					})
				}
				plan, err := buildListElemPlan(t.Element, entities, entityIndex, unionIdx, interfaceName)
				if err != nil {
					return nil, fmt.Errorf("query %q column %d %q: %w", q.Name, ci, col.Name, err)
				}
				p.RowFields = append(p.RowFields, preparedRow{
					ColumnName: col.Name,
					Field:      field,
					GoType:     "[]" + plan.GoType,
					Kind:       columnList,
					ListElem:   plan,
				})
			default:
				return nil, fmt.Errorf("%w: query %q column %d %q: internal invariant — Phase A missed non-property type %s", ErrOutOfC6Scope, q.Name, ci, col.Name, col.Type.String())
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

// buildListElemPlan commits one list-element decode step into a
// preparedListElem, walking the ResolvedType sum exactly once (spec
// §1.3). Every arm returns a non-nil plan whose Kind is one of the
// closed columnKind values; render walks the plan alone and never sees
// resolver.ResolvedType again. On an EdgeUnion arm the plan carries
// UnionIdx pointing at the entry the caller has already appended (or
// will append) to preparedQuery.EdgeUnions and GoType carries the
// synthesised sealed-interface name — Phase B threads both through so
// pointer stability across slice growth is not required (spec §3.1,
// §5.2).
//
// unrepresentable-width leaves surface ErrUnrepresentableWidth naming
// the offending width. Unknown resolver variants surface ErrOutOfC6Scope
// naming the type — the deletion-fence for the failure mode this bead
// closes (spec §4.1 synthetic-malformed-variant row).
//
// The unionInterfaceName argument carries the synthesised edgeUnion
// interface name (`<QueryName><RowField>`) the caller committed onto
// preparedQuery.EdgeUnions. Every arm except the EdgeUnion / List
// recursion ignores it.
func buildListElemPlan(t resolver.ResolvedType, entities []preparedEntity, entityIndex map[entityLookupKey]int, unionIdx int, unionInterfaceName string) (*preparedListElem, error) {
	switch tt := t.(type) {
	case resolver.ResolvedProperty:
		ty, ok := goType(tt.Type)
		if !ok {
			return nil, fmt.Errorf("%w: list element has unrepresentable property width %s", ErrUnrepresentableWidth, tt.Type)
		}
		carrier := driverCarrier(ty)
		convert := carrier != ty
		if !convert {
			carrier = ""
		}
		return &preparedListElem{Kind: columnProperty, GoType: ty, Carrier: carrier, UsesConvert: convert}, nil
	case resolver.ResolvedNode:
		idx, ok := entityIndex[entityLookupKey{Kind: entityNode, Labels: tt.Labels}]
		if !ok {
			return nil, fmt.Errorf("%w: list element references unknown node type %q", ErrOutOfC6Scope, string(tt.Labels))
		}
		name := entities[idx].Name
		return &preparedListElem{Kind: columnNode, GoType: name, EntityName: name}, nil
	case resolver.ResolvedEdge:
		idx, ok := entityIndex[entityLookupKey{Kind: entityEdge, EdgeKey: tt.EdgeKey}]
		if !ok {
			return nil, fmt.Errorf("%w: list element references unknown edge type %s -[:%s]-> %s", ErrOutOfC6Scope, string(tt.EdgeKey.Source), string(tt.EdgeKey.Label), string(tt.EdgeKey.Target))
		}
		name := entities[idx].Name
		return &preparedListElem{Kind: columnEdge, GoType: name, EntityName: name}, nil
	case resolver.ResolvedEdgeUnion:
		if len(tt.EdgeKeys) < 2 {
			return nil, fmt.Errorf("%w: list element resolved as edgeUnion with only %d candidate(s) — resolver invariant violated (expected >= 2)", ErrOutOfC6Scope, len(tt.EdgeKeys))
		}
		for _, ek := range tt.EdgeKeys {
			if _, ok := entityIndex[entityLookupKey{Kind: entityEdge, EdgeKey: ek}]; !ok {
				return nil, fmt.Errorf("%w: list element edgeUnion candidate %s -[:%s]-> %s not declared by schema", ErrOutOfC6Scope, string(ek.Source), string(ek.Label), string(ek.Target))
			}
		}
		return &preparedListElem{Kind: columnEdgeUnion, GoType: unionInterfaceName, UnionIdx: unionIdx}, nil
	case resolver.ResolvedTemporal:
		ty := temporalGoType(tt.Kind)
		return &preparedListElem{Kind: columnTemporal, GoType: ty}, nil
	case resolver.ResolvedScalar:
		if tt.Kind == resolver.ScalarNull {
			return &preparedListElem{Kind: columnScalarNull, GoType: "any"}, nil
		}
		return &preparedListElem{Kind: columnScalar, GoType: scalarGoType(tt.Kind)}, nil
	case resolver.ResolvedUnknown:
		return &preparedListElem{Kind: columnAny, GoType: "any"}, nil
	case resolver.ResolvedList:
		nested, err := buildListElemPlan(tt.Element, entities, entityIndex, unionIdx, unionInterfaceName)
		if err != nil {
			return nil, err
		}
		return &preparedListElem{Kind: columnList, GoType: "[]" + nested.GoType, Nested: nested}, nil
	}
	return nil, fmt.Errorf("%w: list element has unknown resolved type %s", ErrOutOfC6Scope, t.String())
}
