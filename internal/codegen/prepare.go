package codegen

import (
	"cmp"
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/areqag/gqlc/internal/graph"
	"github.com/areqag/gqlc/internal/queryfile"
	"github.com/areqag/gqlc/internal/resolver"
	"github.com/areqag/gqlc/internal/schema"
)

// identifierScope is the Go scope a reserved declaration occupies in the
// emitted package. A method on *Queries and a package-level declaration
// of the same name coexist in Go, so a name fixed at method scope is not
// one a package-level type redeclares — which is what sweepIdentifiers
// reads the scope for.
type identifierScope uint8

const (
	// scopePackage is a package-level declaration: the Queries handle and
	// New in db.go, the DBTX seam and SessionInit in the Apache AGE
	// emission, the three Querier interfaces in querier.go, and the two
	// :one sentinels.
	scopePackage identifierScope = iota
	// scopeMethod is a method, on whichever receiver: WithTx and Begin on
	// *Queries, the Apache AGE graph lifecycle pair on *Queries, and
	// Commit and Rollback on *Tx. The receiver is not part of the
	// distinction — what this value says is that the name occupies no
	// package block, so a package-level declaration may take it.
	scopeMethod
)

// reservedIdentifiers is the C1 exported-identifier reserved set (spec
// §4.1): the exported names a generated package declares because the
// emitter fixes them, whatever the batch contains, each paired with the
// scope it occupies. A NamedQuery.Name matching any of them routes to
// ErrIdentifierCollision at Phase A on membership alone; the scope column
// narrows sweepIdentifiers' source 0 and nothing else. Exported names
// derived from the batch — entity structs, method names, <Method>Params,
// <Method>Row, edgeUnion interfaces — vary with the input and are caught
// by sweepIdentifiers instead.
//
// The set is the union across backends and batches: ErrNoRows /
// ErrMultipleResults are reserved in batches that would not emit them,
// DBTX / SessionInit / EnsureGraph / DropGraph in batches targeting a
// backend with neither a connection seam nor a graph lifecycle, and the
// five temporal carriers (ADR 0033) in batches whose surface names no
// temporal width and so emits no temporal.go. A rename that works in one
// batch or against one backend but not another is exactly the "renaming
// scheme" D2 Resolved refused.
//
// Every scopeMethod row stands on promotion, not on redeclaration
// (docs/specs/codegen-tx-embedded-querier.md §5, superseding
// docs/specs/codegen-tx-object.md §9.1, which read them as collisions).
// Query methods are emitted on the unexported core `*queries`; the fixed
// methods are on *Queries or on *Tx, both of which embed the core. So no
// query name redeclares anything — measured: a package declaring both
// `func (q *queries) Commit` and `func (tx *Tx) Commit` builds and vets
// clean. What Phase A prevents is not a collision but a SUCCESS.
//
// The outcome is the same shape at either receiver, and it is silent both
// ways. A query taking a name fixed on *Queries — WithTx, Begin,
// EnsureGraph, DropGraph — is shadowed at depth 0 on the root handle and
// promotes into *Tx unshadowed, so tx.Begin(ctx) compiles again and runs
// a user query inside the open transaction. A query taking a name fixed
// on *Tx — Commit, Rollback — is the mirror: reachable on a *Queries,
// shadowed on a *Tx. Either way one of the two is silently unreachable,
// with no diagnostic from the compiler or from vet, which is why the
// refusal reads membership and not the receiver.
//
// Queries occupies the package block alone: the handle type, and since
// gqlc-f4hf no accessor on *Tx. The row records scopePackage, which is
// where source 0 seeds.
var reservedIdentifiers = map[string]identifierScope{
	"Queries":            scopePackage,
	"New":                scopePackage,
	"WithTx":             scopeMethod,
	"ReadQuerier":        scopePackage,
	"WriteQuerier":       scopePackage,
	"Querier":            scopePackage,
	"ErrNoRows":          scopePackage,
	"ErrMultipleResults": scopePackage,
	"DBTX":               scopePackage,
	"SessionInit":        scopePackage,
	"EnsureGraph":        scopeMethod,
	"DropGraph":          scopeMethod,
	"Tx":                 scopePackage,
	"ErrTxDone":          scopePackage,
	"Begin":              scopeMethod,
	"Commit":             scopeMethod,
	"Rollback":           scopeMethod,
	"Date":               scopePackage,
	"Time":               scopePackage,
	"LocalTime":          scopePackage,
	"LocalDateTime":      scopePackage,
	"Duration":           scopePackage,
}

// Prepared is the batch derivation the shared phases commit: the emitted
// package identifier, the schema's entities in Phase Z order, and the
// batch's queries in Input order. A backend's render layer walks this
// alone — it never reaches back into resolver types.
type Prepared struct {
	Package  string
	Entities []Entity
	Queries  []Query
}

// Query bundles the per-query derivations produced by Phase B — the
// derived method surface, the Params/Row shapes, and the resolved axes
// Phase A already gate-checked. Kept together so the per-source emission
// walk reads one struct per query in order (spec §5.5) rather than
// re-deriving each field from NamedQuery.Validated.
type Query struct {
	NamedQuery
	MethodName  string       // verbatim NamedQuery.Name
	Bare        string       // lowerCamel first rune of MethodName
	IsWrite     bool         // §1.2 — Validated.Statement == StatementWrite
	ParamFields []Param      // in Validated.Parameters order
	RowFields   []Row        // in Validated.Columns order
	EdgeUnions  []*EdgeUnion // in Validated.Columns order (sub-ordered by column position); one per ColumnEdgeUnion Row field (C5). Pointer-stable so a ListElem's UnionIdx into this slice survives slice growth (spec §3.1).
}

// Param is one derived Params-struct field: the raw driver-binding key
// the query text names, the mangled Go field, and the emitted Go type.
type Param struct {
	RawName  string // ResolvedParameter.Name
	Field    string // mangle §4.2
	GoType   string // §5.1
	Nullable bool
}

// Row is one derived Row-struct field: the driver record key, the
// mangled Go field, the emitted Go type, and the committed decode arm
// the emission dispatches on.
type Row struct {
	ColumnName string // resolver Column.Name — the driver record key
	Field      string // mangle §4.3
	GoType     string // §5.1 — a Go type text; for entity columns the entity struct name; for edgeUnion columns the synthesised interface name
	Nullable   bool
	Kind       ColumnKind       // property (C1) or entity — property/node/edge (C2); temporal/list/scalar/any (C3); edgeUnion (C5)
	ListElem   *ListElem        // non-nil iff Kind == ColumnList — the committed element decode plan (spec §1.3)
	EdgeKeys   []schema.EdgeKey // populated when Kind == ColumnEdgeUnion — the candidate edge keys in resolver-canonical order (§5.5)
}

// ListElem is Phase B's committed list-element decode plan (spec §1.3).
// Every list column's ListElem is non-nil; the element's arm — Kind, from
// the same closed ColumnKind enum as the top-level row's Kind — plus the
// derived entity / union coordinates let the render-side loop body walk
// one struct per element, never a resolver type. Nested lists carry a
// Nested plan for the inner iteration.
type ListElem struct {
	// Kind is the same closed ColumnKind used at the top level. A future
	// resolver variant lands at two Phase B assignment sites, this one
	// and the top level's, and nothing at build time demands either:
	// both are type switches on resolver.ResolvedType, Go does not
	// require a switch to be exhaustive, and golangci-lint's exhaustive
	// cannot read a type switch at all — its `check` setting accepts
	// only `switch` and `map`. A missed arm is reported by the
	// ErrOutOfC6Scope fallthrough instead, at generation time. Measured
	// by deleting an arm from each: both build clean and lint green
	// (gqlc-7hp5g). The render-side ColumnKind switches are guarded
	// differently; see walkListElemPlan.
	Kind ColumnKind
	// GoType is the emitted Go type text for one element — a TypeMap
	// entry, a schema-derived entity struct name, or a synthesised
	// edgeUnion interface name.
	GoType string
	// EntityName is the schema-derived struct name for the Node / Edge
	// arms — feeds the `decode<EntityName>` helper call.
	EntityName string
	// UnionIdx is the index into the owning Query.EdgeUnions slice for
	// the EdgeUnion arm. Index is chosen over a pointer so a future
	// Phase B edit that reorders EdgeUnions appends around the
	// plan-build call cannot leave a stale pointer behind (spec §5.2).
	// Zero for every non-EdgeUnion arm.
	UnionIdx int
	// Nested is the inner element plan for a nested list. Non-nil iff
	// Kind == ColumnList.
	Nested *ListElem
}

// ColumnKind discriminates the row-assembly arm a backend runs for a
// given Row: which member of the resolved type surface the column landed
// on. The kind is derived once at Phase B and carried onto Row so the
// row-assembly template (§5.5) needs no per-emission re-derivation.
type ColumnKind int

const (
	// ColumnProperty is a schema property of scalar width, including the
	// temporal widths a property may declare.
	ColumnProperty ColumnKind = iota
	// ColumnNode is a whole-node projection, decoded into its entity
	// struct.
	ColumnNode
	// ColumnEdge is a whole-edge projection, decoded into its entity
	// struct.
	ColumnEdge
	// ColumnTemporal is a temporal-valued expression — a projection whose
	// type the resolver derived rather than read off a property.
	ColumnTemporal
	// ColumnScalar is a scalar-valued expression: bool, integer, float,
	// string, or map.
	ColumnScalar
	// ColumnScalarNull is the list-element split of the null scalar off
	// ColumnScalar (spec §1.3). At the top level a null scalar continues
	// to route through ColumnAny; inside a list-element plan the arm
	// distinguishes an untyped element from a typed-scalar one. Kept on
	// the same closed enum so a new resolver variant lands in exactly one
	// place.
	ColumnScalarNull
	// ColumnList is a list-valued column, whose elements are decoded one
	// at a time through the arm its ListElem plan commits.
	ColumnList
	// ColumnAny is the honest-any arm: a column whose emitted Go type is
	// `any` because the resolver could not narrow it (§5.5).
	ColumnAny
	// ColumnEdgeUnion is C5's multi-candidate-edge arm: the column may
	// carry any of several schema edge types, dispatched on the wire
	// label in resolver-canonical EdgeKeys order (§5.5). The row-field
	// GoType carries the synthesised interface name; each candidate
	// satisfies the interface via a marker method emitted in models.go
	// (§5.2).
	ColumnEdgeUnion
)

// EntityKind discriminates node from edge in the entity-naming and
// emission passes. Node reads NodeType.KeyLabels; edge reads EdgeType.EdgeKey.
type EntityKind int

const (
	// EntityNode is a schema node type, keyed on its KEY label set.
	EntityNode EntityKind = iota
	// EntityEdge is a schema edge type, keyed on its source / label /
	// target triple.
	EntityEdge
)

// EdgeUnion carries one per-query-column edgeUnion synthesis result
// (§4.10). The InterfaceName is the emitted sealed-marker interface's Go
// identifier (<QueryName><RowFieldName>); Candidates is the ordered slice
// of entity struct names each candidate schema edge maps to (via the
// Phase Z index), matched positionally against the resolver's EdgeKeys
// slice. Emission walks §5.2 to write the interface + marker methods, and
// §5.5 to write the type-switch dispatch body. Introduced at C5.
type EdgeUnion struct {
	QueryName     string           // owning query's method name
	ColumnPos     int              // 0-based column index in Validated.Columns
	ColumnName    string           // Column.Name
	FieldName     string           // row-field mangle (§4.3), also used as the interface suffix
	InterfaceName string           // <QueryName><FieldName>
	EdgeKeys      []schema.EdgeKey // resolver-canonical order (R3 spec §4.4)
	Candidates    []string         // entity struct names, len == len(EdgeKeys); positional
}

// Entity is Phase Z's per-entity result: struct name plus ordered field
// list plus the source-axis text for the doc comment. Cached in a slice
// the emission walk (§5.2) reads in insertion order.
type Entity struct {
	Kind    EntityKind
	Name    string            // derived struct name (spec §4.5)
	Labels  graph.LabelSetKey // node-only source axis: the KEY label set, its identity (empty for edge)
	EdgeKey schema.EdgeKey    // edge-only source axis (zero for node)
	DocAxis string            // "<complete labels>" or "<label> edge (<src> -> <tgt>)" for doc
	Fields  []EntityField
}

// EntityField carries one property's derived struct field name and its Go
// type text. Property source name is retained for the driver property-map
// key.
type EntityField struct {
	PropName string // Property.Name — the driver's Props map key
	Field    string // paramFieldName(PropName)
	GoType   string // §5.1 property-side row (unchanged from C1)
	Nullable bool
}

// Prepare runs every shared phase over one batch and returns the
// backend-neutral derivation its render layer consumes: package-
// identifier derivation, Phase Z (schema-shape admission + entity
// naming), batch admission, Phase A (per-query gates), Phase B (name
// derivation + committed decode plans), and the package-level
// exported-identifier sweep. First offender wins across every axis;
// the returned error wraps one of this package's sentinels.
//
// packageName overrides the Schema.Name derivation when non-empty.
func Prepare(in Input, tm TypeMap, packageName string) (Prepared, error) {
	pkg, err := emittedPackage(in.Schema.Name, packageName)
	if err != nil {
		return Prepared{}, err
	}

	// Phase Z — schema-shape admission and entity naming (§2.1, §4.5,
	// §5.2). Eagerly walks every NodeType and EdgeType, deriving the
	// entity struct name via the entity-naming rules and the per-entity
	// property field list. First offender wins across the schema-shape
	// axis. Runs before Phase A because Phase A's ResolvedNode /
	// ResolvedEdge admission reads Phase Z's cache to type-check the Go
	// type text.
	entities, entityIndex, err := phaseZAdmit(in.Schema, tm)
	if err != nil {
		return Prepared{}, err
	}

	if err := validateQueries(in.Queries); err != nil {
		return Prepared{}, err
	}

	// Phase A — batch admission: for each query in slice order, gate on
	// resolved type / cardinality / reserved-identifier. C3 widens the
	// admissible column shape to the full closed ResolvedType sum minus
	// ResolvedEdgeUnion; parameter admission stays property-only,
	// extended to temporal-property widths. First offender wins (spec
	// §2.1).
	if err := phaseAAdmit(in.Queries, entities, entityIndex, tm); err != nil {
		return Prepared{}, err
	}

	// Phase B — per-query name derivation. Row-field text-shape analysis,
	// Params-field mangle, per-query collision checks. C2 extends the
	// row-field type mapping with entity-column lookup into Phase Z's
	// cache. First offender wins.
	prepared, err := phaseBDerive(in.Queries, entities, entityIndex, tm)
	if err != nil {
		return Prepared{}, err
	}

	// Cross-query package-level exported-identifier collision sweep
	// (§4.6). C2 adds entity struct names as the fourth identifier
	// source, swept first.
	if err := sweepIdentifiers(entities, prepared); err != nil {
		return Prepared{}, err
	}

	return Prepared{Package: pkg, Entities: entities, Queries: prepared}, nil
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
	Kind    EntityKind
	Labels  graph.LabelSetKey // node axis: the type's KEY label set, its identity; zero for edge
	EdgeKey schema.EdgeKey    // edge axis; zero for node
}

// phaseZAdmit is spec §2.1's Phase Z: eagerly walks the schema's node and
// edge types deriving struct names + property field lists. First offender
// wins across the schema-shape axis. Every multi-label node type and every
// ambiguous edge label must carry an explicit Name — a lazy check would
// make output depend on the query set, which D3 Resolved rejects.
func phaseZAdmit(sch schema.Schema, tm TypeMap) ([]Entity, map[entityLookupKey]int, error) {
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
			cmp.Compare(a.KeyLabels, b.KeyLabels),
			cmp.Compare(a.Target, b.Target),
		)
	})

	// Ambiguity axis: an edge Label appearing on more than one EdgeKey is
	// ambiguous even when the two endpoint pairs differ (spec §4.5 Rule 4).
	labelCount := make(map[graph.LabelSetKey]int, len(sch.Edges))
	for _, k := range edgeKeys {
		labelCount[k.KeyLabels]++
	}

	entities := make([]Entity, 0, len(sch.Nodes)+len(sch.Edges))
	index := make(map[entityLookupKey]int, len(sch.Nodes)+len(sch.Edges))

	for _, k := range nodeKeys {
		nt := sch.Nodes[k]
		name, err := entityStructName(EntityNode, nt.KeyLabels, schema.EdgeKey{}, nt.Name, false)
		if err != nil {
			return nil, nil, err
		}
		fields, err := prepareEntityFields(name, nt.Properties, tm)
		if err != nil {
			return nil, nil, err
		}
		labels := strings.Join(nt.CompleteLabels.Split(), "&")
		ent := Entity{
			Kind:    EntityNode,
			Name:    name,
			Labels:  nt.KeyLabels,
			DocAxis: labels,
			Fields:  fields,
		}
		index[entityLookupKey{Kind: EntityNode, Labels: nt.KeyLabels}] = len(entities)
		entities = append(entities, ent)
	}

	for _, k := range edgeKeys {
		et := sch.Edges[k]
		ambig := labelCount[et.KeyLabels] > 1
		name, err := entityStructName(EntityEdge, "", et.EdgeKey, et.Name, ambig)
		if err != nil {
			return nil, nil, err
		}
		fields, err := prepareEntityFields(name, et.Properties, tm)
		if err != nil {
			return nil, nil, err
		}
		docAxis := fmt.Sprintf("%s edge type (%s -> %s)", string(et.KeyLabels), string(et.Source), string(et.Target))
		ent := Entity{
			Kind:    EntityEdge,
			Name:    name,
			EdgeKey: et.EdgeKey,
			DocAxis: docAxis,
			Fields:  fields,
		}
		index[entityLookupKey{Kind: EntityEdge, EdgeKey: et.EdgeKey}] = len(entities)
		entities = append(entities, ent)
	}
	return entities, index, nil
}

// entityStructName derives the exported Go struct name for a schema node
// or edge type per spec §4.5's five rules. First failure wins in rule
// order: Rule 1 (explicit Name invalid) → ErrInvalidEntityName; Rule 4
// (multi-label / ambiguous without explicit Name) → ErrUnnamedMultiLabelType;
// Rule 2/3 (mangle result invalid) → ErrInvalidEntityName.
func entityStructName(kind EntityKind, labels graph.LabelSetKey, edgeKey schema.EdgeKey, explicitName string, ambiguousEdgeLabel bool) (string, error) {
	if explicitName != "" {
		if exportedGoIdent(explicitName) {
			return explicitName, nil
		}
		return "", fmt.Errorf("%w: %s explicit Name %q is not a valid exported Go identifier", ErrInvalidEntityName, entityAxisText(kind, labels, edgeKey), explicitName)
	}

	if kind == EntityNode {
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
	labelParts := edgeKey.KeyLabels.Split()
	if len(labelParts) > 1 {
		return "", fmt.Errorf("%w: multi-label edge type (%s -[:%s]-> %s) requires an explicit Name", ErrUnnamedMultiLabelType, string(edgeKey.Source), string(edgeKey.KeyLabels), string(edgeKey.Target))
	}
	if len(labelParts) == 0 {
		return "", fmt.Errorf("%w: edge type with empty label requires an explicit Name", ErrUnnamedMultiLabelType)
	}
	if ambiguousEdgeLabel {
		return "", fmt.Errorf("%w: edge label %q is shared across endpoint pairs — (%s -[:%s]-> %s) requires an explicit Name", ErrUnnamedMultiLabelType, string(edgeKey.KeyLabels), string(edgeKey.Source), string(edgeKey.KeyLabels), string(edgeKey.Target))
	}
	name := edgeLabelFieldName(labelParts[0])
	if !exportedGoIdent(name) {
		return "", fmt.Errorf("%w: edge type label %q mangles to %q, not a valid exported Go identifier", ErrInvalidEntityName, string(edgeKey.KeyLabels), name)
	}
	return name, nil
}

// entityAxisText renders a human-readable source-axis fragment for a
// fail-message: "node type Person&Employee" or
// "edge type (Person -[:KNOWS]-> Company)".
func entityAxisText(kind EntityKind, labels graph.LabelSetKey, edgeKey schema.EdgeKey) string {
	if kind == EntityNode {
		return fmt.Sprintf("node type %q", string(labels))
	}
	return fmt.Sprintf("edge type (%s -[:%s]-> %s)", string(edgeKey.Source), string(edgeKey.KeyLabels), string(edgeKey.Target))
}

// unimplementedTypeKind reports the outermost sub-type of pt whose KIND
// gqlc has built no emission for, and whether pt carries one at all. It
// is asked at every position that asks a TypeMap for a property carrier,
// and asked FIRST — see ErrUnimplementedTypeKind for why the table's own
// ok=false is the wrong answer here.
//
// It also reports the dotted path of RECORD FIELD names the unbuilt
// kind sits under, empty when it sits under none.
//
// The recursion is through list elements and record fields. ADR 0039
// walked list elements alone and said so: a record was refused at its
// own node, so nothing below one was reachable and a Fields descent
// would have been an arm no input could enter — not a guard, and nothing
// to test it with. Stage 1 of gqlc-x9tg7 emits records, which makes
// their fields reachable and makes the descent owed; ADR 0039 named this
// as the expected reversal. A union is still refused at its own node,
// so Members stays undescended for exactly the reason Fields used to be.
//
// Descending is the only way to tell RECORD<a INT32> from RECORD<u
// UNION<…>>: without it the second reaches the table, which has no case
// for the union nested inside the struct text it is asked to build, and
// comes back a width error — the same confusion the list descent exists
// to forbid, one kind over.
func unimplementedTypeKind(pt graph.PropertyType) (graph.PropertyType, string, bool) {
	switch pt.Kind() {
	case graph.KindUnion:
		return pt, "", true
	case graph.KindRecord:
		for _, f := range pt.Fields() {
			kind, path, unbuilt := unimplementedTypeKind(f.Type)
			if !unbuilt {
				continue
			}
			// The field names accumulate outward, so a record nested
			// in a record reports "at.zone" rather than the innermost
			// name alone — the author has to be told which declaration
			// to open, and the leaf name alone can appear at several.
			if path != "" {
				return kind, f.Name + "." + path, true
			}
			return kind, f.Name, true
		}
		return "", "", false
	case graph.KindList:
		return unimplementedTypeKind(pt.Elem())
	case graph.KindScalar:
		// Every scalar has an emission on some backend, so the walk stops
		// and the carrier question below decides. Named rather than left
		// to the default so a fourth kind cannot be added silently.
	}
	return "", "", false
}

// unimplementedKindDetail renders the tail every ErrUnimplementedTypeKind
// message shares, so the four fail-sites differ only in how they name
// themselves. For a bare union the two type arguments are the same
// string and it renders as the plain `has %s` the width refusals use;
// under a list or inside a record they differ, and both are named
// because neither alone tells the reader what to edit — the declared
// width does not say which level is unbuilt, and the sub-type alone
// cannot be found in the schema.
//
// field is the dotted record-field path the unbuilt kind sits under, and
// is named when there is one for the same reason: inside a record the
// sub-type is not enough to locate, because one width can be declared at
// several fields and the reader has to be told which declaration to open.
func unimplementedKindDetail(declared, kind graph.PropertyType, field string) string {
	if declared == kind {
		return string(declared)
	}
	if field != "" {
		return string(declared) + ", whose field " + strconv.Quote(field) + " has " + string(kind) + ", which has no emission"
	}
	return string(declared) + ", whose " + string(kind) + " has no emission"
}

// prepareEntityFields derives an entity's per-property field list in
// map-key-sorted order (spec §5.2), reporting a same-entity field-name
// collision as ErrPropertyFieldCollision. The C3 eager width sweep (§4.8)
// folds into this pass: a property whose width the TypeMap has no
// faithful carrier for returns ErrUnrepresentableWidth naming the entity,
// property, and width. First offender wins across the schema-shape axis.
//
// The storage sweep folds in beside it, and only here: a width with a
// carrier that the backend's STORE will not hold returns
// ErrUnstorableProperty (ADR 0035). Declared entity properties are the
// only positions that store, so the query-column and query-parameter
// sweeps below do not ask — a value the store would refuse to keep is
// still one the server will happily project or bind.
func prepareEntityFields(entityName string, props map[string]schema.Property, tm TypeMap) ([]EntityField, error) {
	keys := make([]string, 0, len(props))
	for k := range props {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	fields := make([]EntityField, 0, len(props))
	seen := make(map[string]string, len(props))
	for _, k := range keys {
		p := props[k]
		field := paramFieldName(p.Name)
		if first, dup := seen[field]; dup {
			return nil, fmt.Errorf("%w: entity %q properties %q and %q both mangle to %q", ErrPropertyFieldCollision, entityName, first, p.Name, field)
		}
		seen[field] = p.Name
		if kind, field, unbuilt := unimplementedTypeKind(p.Type); unbuilt {
			return nil, fmt.Errorf("%w: entity %q property %q has %s", ErrUnimplementedTypeKind, entityName, p.Name, unimplementedKindDetail(p.Type, kind, field))
		}
		ty, ok := tm.Property(p.Type)
		if !ok {
			return nil, fmt.Errorf("%w: entity %q property %q has %s", ErrUnrepresentableWidth, entityName, p.Name, p.Type)
		}
		if !tm.StorableProperty(p.Type) {
			return nil, fmt.Errorf("%w: entity %q property %q has %s", ErrUnstorableProperty, entityName, p.Name, p.Type)
		}
		fields = append(fields, EntityField{
			PropName: p.Name,
			Field:    field,
			GoType:   ty,
			Nullable: p.Nullable,
		})
	}
	return fields, nil
}

// cardinalityAnnotation renders a Cardinality as its ":one" / ":many" /
// ":exec" annotation text — the caller-visible form Phase A's fail
// messages use so the error line reads back the exact string the author
// typed on the // name: line.
func cardinalityAnnotation(c queryfile.Cardinality) string {
	switch c {
	case queryfile.CardinalityOne:
		return ":one"
	case queryfile.CardinalityMany:
		return ":many"
	case queryfile.CardinalityExec:
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
// unrepresentable widths route through ErrUnrepresentableWidth. Phase Z
// has already refused every width the schema declares, so what reaches
// the column and parameter gates here is a width no schema property
// carries — which is to say a Validated shape the resolver did not build.
func phaseAAdmit(queries []NamedQuery, entities []Entity, entityIndex map[entityLookupKey]int, tm TypeMap) error {
	for i, q := range queries {
		if _, reserved := reservedIdentifiers[q.Name]; reserved {
			return fmt.Errorf("%w: query %q at position %d collides with reserved identifier", ErrIdentifierCollision, q.Name, i)
		}
		if q.Cardinality != queryfile.CardinalityOne && q.Cardinality != queryfile.CardinalityMany && q.Cardinality != queryfile.CardinalityExec {
			return fmt.Errorf("%w: query %q at position %d has unrecognised cardinality %d", ErrInvalidCardinality, q.Name, i, q.Cardinality)
		}
		// Cardinality × shape gate (spec §4.9). Runs before the column-type
		// sweep so a fixture combining :exec-on-projection with an
		// unrepresentable-width column fires ErrExecOnProjection first —
		// the caller fixes the cardinality axis before revisiting widths.
		if q.Cardinality == queryfile.CardinalityExec && len(q.Validated.Columns) > 0 {
			return fmt.Errorf("%w: query %q at position %d has cardinality :exec but projects %d column(s) (first column %q) — drop :exec or drop RETURN", ErrExecOnProjection, q.Name, i, len(q.Validated.Columns), q.Validated.Columns[0].Name)
		}
		if (q.Cardinality == queryfile.CardinalityOne || q.Cardinality == queryfile.CardinalityMany) && len(q.Validated.Columns) == 0 {
			shape := "zero-column read"
			if q.Validated.Statement == resolver.StatementWrite {
				shape = "zero-column write"
			}
			return fmt.Errorf("%w: query %q at position %d has cardinality %s but the query is a %s — annotate :exec or add a RETURN clause", ErrCardinalityShapeMismatch, q.Name, i, cardinalityAnnotation(q.Cardinality), shape)
		}
		// Both backends emit SourceText through a Go RAW string literal, so the
		// bytes such a literal cannot carry are refused here rather than
		// emitted. A backtick cannot appear in one at all; a carriage return
		// can, and is DISCARDED from the literal's value (Go spec, "String
		// literals"). The CR is the worst of the set because the loss is
		// silent: generate exits 0 having written a constant whose value is not
		// the text that was parsed and resolved. For AGE that forges the
		// dollar-quote delimiter, dollarTag having scanned the bytes before
		// emission and the SQL parser the bytes after; for neo4j the discarded
		// byte glues the tokens it separated (bd gqlc-7f9a).
		//
		// Every CR reaching here is content, never a line ending, so this
		// refuses no authoring convention: queryfile reads with
		// bufio.ScanLines, which takes a CRLF's CR before SourceText exists,
		// and a lone-CR file is one line the annotation grammar already
		// rejects.
		//
		// The last two are not carried differently but make the emitted file
		// unparseable: Go source must be valid UTF-8 and, by a documented
		// implementation restriction, may not hold a NUL. So nothing ships
		// wrong and these are a diagnostic remedy rather than a correctness
		// one — without them the user is handed a go/format failure citing a
		// line of a GENERATED file, naming neither the query nor the byte
		// (bd gqlc-32n53).
		//
		// They are refused HERE, and not further upstream where a refusal
		// could name the file and line, because neither byte is a defect in
		// the query — only in its emission. Measured 2026-09-03 over
		// cmd/gqlc: the openCypher lexer already refuses both where they
		// stand in a token slot, and what survives to this point is the
		// positions it has no reason to scan — inside a line comment, and
		// inside a string literal, where a NUL is a value the server itself
		// accepts. Refusing them in the grammar would refuse a query neo4j
		// runs.
		if strings.ContainsRune(q.SourceText, '`') {
			return fmt.Errorf("%w: query %q at position %d has a backtick in its source text", ErrOutOfC6Scope, q.Name, i)
		}
		if strings.ContainsRune(q.SourceText, '\r') {
			return fmt.Errorf("%w: query %q at position %d has a carriage return in its source text", ErrOutOfC6Scope, q.Name, i)
		}
		if strings.ContainsRune(q.SourceText, '\x00') {
			return fmt.Errorf("%w: query %q at position %d has a NUL in its source text", ErrOutOfC6Scope, q.Name, i)
		}
		if !utf8.ValidString(q.SourceText) {
			return fmt.Errorf("%w: query %q at position %d is not valid UTF-8", ErrOutOfC6Scope, q.Name, i)
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
				if kind, field, unbuilt := unimplementedTypeKind(t.Type); unbuilt {
					return fmt.Errorf("%w: query %q column %d %q has %s", ErrUnimplementedTypeKind, q.Name, ci, col.Name, unimplementedKindDetail(t.Type, kind, field))
				}
				if _, ok := tm.Property(t.Type); !ok {
					return fmt.Errorf("%w: query %q column %d %q has %s", ErrUnrepresentableWidth, q.Name, ci, col.Name, t.Type)
				}
			case resolver.ResolvedNode:
				if _, ok := entityIndex[entityLookupKey{Kind: EntityNode, Labels: t.Labels}]; !ok {
					return fmt.Errorf("%w: query %q column %d %q references unknown node type %q", ErrOutOfC6Scope, q.Name, ci, col.Name, string(t.Labels))
				}
			case resolver.ResolvedEdge:
				if _, ok := entityIndex[entityLookupKey{Kind: EntityEdge, EdgeKey: t.EdgeKey}]; !ok {
					return fmt.Errorf("%w: query %q column %d %q references unknown edge type %s -[:%s]-> %s", ErrOutOfC6Scope, q.Name, ci, col.Name, string(t.EdgeKey.Source), string(t.EdgeKey.KeyLabels), string(t.EdgeKey.Target))
				}
			case resolver.ResolvedEdgeUnion:
				if err := admitEdgeUnionCandidates(t.EdgeKeys, entities, entityIndex, columnSite(q.Name, ci, col.Name)); err != nil {
					return err
				}
			case resolver.ResolvedTemporal:
				// Every temporal kind is representable; the closed enum
				// maps into the TypeMap's temporal table (§5.1) without a
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
				if _, err := buildListElemPlan(t.Element, entities, entityIndex, tm, -1, ""); err != nil {
					return fmt.Errorf("query %q column %d %q: %w", q.Name, ci, col.Name, err)
				}
			default:
				return fmt.Errorf("%w: query %q column %d %q resolved as %s", ErrOutOfC6Scope, q.Name, ci, col.Name, ResolvedTypeName(col.Type))
			}
		}
		for pi, p := range q.Validated.Parameters {
			prop, ok := p.Type.(resolver.ResolvedProperty)
			if !ok {
				return fmt.Errorf("%w: query %q parameter %d $%s resolved as %s (non-property parameters are post-v1)", ErrOutOfC6Scope, q.Name, pi, p.Name, ResolvedTypeName(p.Type))
			}
			if kind, field, unbuilt := unimplementedTypeKind(prop.Type); unbuilt {
				return fmt.Errorf("%w: query %q parameter %d $%s has %s", ErrUnimplementedTypeKind, q.Name, pi, p.Name, unimplementedKindDetail(prop.Type, kind, field))
			}
			if _, ok := tm.Property(prop.Type); !ok {
				return fmt.Errorf("%w: query %q parameter %d $%s has %s", ErrUnrepresentableWidth, q.Name, pi, p.Name, prop.Type)
			}
		}
	}
	return nil
}

// listElemSite is the fail-site text for an edge union reached through a
// list-element chain. The chain's own recursion carries no position, and
// the caller wraps the message with the query and column the chain hangs
// off (§4.7).
const listElemSite = "list element"

func columnSite(queryName string, pos int, columnName string) string {
	return fmt.Sprintf("query %q column %d %q", queryName, pos, columnName)
}

// ResolvedTypeName renders t for the refusals that name a type no arm
// matched. Five calls in THIS FILE render through it, three reachable
// and two not: Phase A's column switch, Phase A's parameter type
// assertion and buildListElemPlan's element switch, plus the two sites
// behind Phase A's shadow that §3 of
// docs/specs/codegen-sentinel-taxonomy.md carries as
// param-type-invariant and column-type-invariant. The shadowed pair
// renders the same way so all five answer alike if an edit ever removes
// the shadow. What they hold is by construction a value this package has
// no case for. That count is re-derivable, and the paren is escaped so
// this line is not one of the hits it reports:
// `grep -cE 'ResolvedTypeName\(' internal/codegen/prepare.go` = 6, being
// those five calls and this declaration.
//
// The count is scoped to this file because callers are no longer only
// here. internal/codegen/age's unserved-query gate renders the same kind
// of refusal on the same kind of value, from rejectUnservedQueries,
// which its generate() runs AHEAD of Prepare rather than inside it. So
// nothing on this side stood in front of those renders, and they faulted
// on shapes the five below had stopped faulting on (gqlc-aefe). They
// call this rather than spell a second answer for the same value. Being
// in another file, they are outside that grep, and a count written
// across files would move on every caller added anywhere.
//
// Exporting costs no public surface: a path under internal/ is
// importable only from inside this module, so the name is reachable by
// the callers that need it and by nothing outside.
//
// t.String() there is a call into code this package does not own.
// resolver.ResolvedType's unexported marker seals which types may
// DECLARE it, not which may satisfy it: every variant gives both the
// marker and String a value receiver, so each of the eight pointer forms
// carries them, and Go promotes an embedded type's methods — the marker
// included, and from an embedded interface as readily as from an
// embedded variant — so a struct embedding either can satisfy the
// interface from any package in the module (AGENTS.md, "Closed sum
// types"; internal/resolver's TestResolvedTypeSumIsNotClosed). Can,
// not does: a struct embedding two variants at equal depth promotes
// neither's methods, so struct{resolver.ResolvedNode;
// resolver.ResolvedEdge} satisfies the interface in neither its value
// nor its pointer form and reaches no fail-site here.
//
// Four shapes in that set are witnessed to fault on the call rather than
// answer it: the nil interface, which has no method to reach; any of the
// eight typed-nil pointer forms, because Go emits a nil check before a
// value method reached through a pointer, so even the zero-sized
// (*resolver.ResolvedUnknown)(nil) faults where its body dereferences
// nothing; a struct whose embedded pointer to a variant is nil; and a
// struct embedding resolver.ResolvedType itself, which carries no
// variant at all. The last two are neither a nil interface nor a nil
// pointer to look at, which is why neither a t == nil test nor a
// reflect nil-pointer check would do instead.
//
// Four is what is witnessed, not what the set holds. Whether a value
// faults here is a fact about the String() it ends up dispatching, and
// the interface is satisfiable by types this package never sees, so no
// enumeration written here closes the set. Promotion composes, and
// composing it reaches faulting shapes none of the four covers: a nil
// pointer to a struct that embeds a variant by value and declares no
// String() of its own faults, because the promoted value method must
// dereference the pointer to reach its receiver, and that struct is not
// one of the eight variants. Composing
// is not itself what faults, though — the same struct held by value
// answers, which is the value-embedder case in
// TestUnmatchedResolvedTypeKeepsTheWireTagWhereThereIsOne. Nothing below
// depends on the set being closed: the render enumerates no shape and no
// variant.
//
// A refusal that faults is not a refusal, so the tag is asked for under
// a recover and the dynamic type name answers when asking fails. The tag
// is preferred rather than skipped because §2 of
// docs/specs/codegen-sentinel-taxonomy.md pins these messages as
// contract and the conformance suite asserts them: an implementation
// that can name itself is still named by its own answer.
//
// An empty answer is treated as no answer, and falls back the same way.
// A String() returning "" does not fault, so the recover does not see it,
// and the refusal it produced ended mid-sentence — `resolved as ` with
// nothing after it, or AGE's `column "n" projects ` (bd gqlc-sv61). That
// is worse than the fault it sits beside: a panic announces itself, while
// this reaches the reader as a truncated log line rather than as a type
// that declined to name itself. Emptiness is the whole of the test, and
// deliberately so — a String() answering " " or "???" is passed through,
// since this package can tell whether another package's tag said
// anything, but not whether what it said was any use.
//
// The recover is not how the refusal travels. AGENTS.md's Errors
// convention asks for package-level sentinels matched with errors.Is,
// and names one channel it rules panic/recover out of: syntax errors,
// which come from a custom antlr.ErrorListener instead. This is not that
// channel. The sentinel still leaves through fmt.Errorf and still
// answers errors.Is; what is caught here is a fault in a call this
// package makes to render a value, on the one path where that call is
// into code it does not own.
//
// What this bounds is the panic. A String() that blocks, or that calls
// runtime.Goexit, or that trips a fault the runtime declines to make
// recoverable, still takes the caller with it — an unbounded set of
// implementations admits an unbounded set of ways to misbehave, and this
// addresses the one the sum's own inhabitants exhibit.
func ResolvedTypeName(t resolver.ResolvedType) (name string) {
	defer func() {
		// recover() is called for its effect and not its value: it stops
		// the panic either way, but under GODEBUG=panicnil=1 a panic(nil)
		// makes it return nil, so a `recover() != nil` test would read a
		// faulted call as an answered one, and this helper would hand its
		// callers the empty name it exists to prevent.
		//
		// The emptiness of name is what decides the fallback, and it needs
		// no flag beside it to say whether String() returned: a call that
		// faults never reaches its assignment, so name is still "" here.
		// One test therefore covers both ways of arriving without a name,
		// and it is this function's postcondition stated where it is
		// enforced rather than a second thing to keep in step with it.
		recover() //nolint:errcheck // called for its effect; the value is deliberately unread, per the comment above.
		if name == "" {
			// %T reads the dynamic type through reflection and never
			// dispatches a method, so it answers for the values whose
			// String() just did not. A nil interface renders "<nil>".
			name = fmt.Sprintf("%T", t)
		}
	}()
	return t.String()
}

// admitEdgeUnionCandidates gates one resolved edge-union candidate set,
// naming site in whatever it refuses. Shared by the two fail-sites so
// their answers cannot drift.
//
// The first two gates hold the resolver's invariants at this package's
// boundary: the resolver commits at least two candidates (a single one
// collapses to ResolvedEdge, R3 spec §4.4) and commits only edges the
// schema declares, so a Validated shape it did not build fails at
// generation rather than downstream. The third follows from what arrives
// — the emitted dispatch reads the value's label to pick a candidate,
// which two candidates carrying one label give it no way to do. First
// offender in candidate order wins across all three.
func admitEdgeUnionCandidates(edgeKeys []schema.EdgeKey, entities []Entity, entityIndex map[entityLookupKey]int, site string) error {
	if len(edgeKeys) < 2 {
		return fmt.Errorf("%w: %s resolved as edgeUnion with only %d candidate(s) — resolver invariant violated (expected >= 2)", ErrOutOfC6Scope, site, len(edgeKeys))
	}
	firstByLabel := make(map[graph.LabelSetKey]string, len(edgeKeys))
	for _, ek := range edgeKeys {
		idx, ok := entityIndex[entityLookupKey{Kind: EntityEdge, EdgeKey: ek}]
		if !ok {
			return fmt.Errorf("%w: %s edgeUnion candidate %s -[:%s]-> %s not declared by schema", ErrOutOfC6Scope, site, string(ek.Source), string(ek.KeyLabels), string(ek.Target))
		}
		name := entities[idx].Name
		if first, dup := firstByLabel[ek.KeyLabels]; dup {
			return fmt.Errorf("%w: %s candidates %s and %s both carry edge label %q — an edge value carries its label and its properties, not its endpoint types, so nothing in it tells the two apart; constrain the pattern's endpoints or direction so that at most one candidate carries the label", ErrUnrepresentableEdgeUnion, site, first, name, string(ek.KeyLabels))
		}
		firstByLabel[ek.KeyLabels] = name
	}
	return nil
}

// phaseBDerive is spec §2.1's Phase B: derives names for the method,
// Params fields, and Row fields; runs per-query collision checks. Phase A
// guarantees columns are ResolvedProperty / ResolvedNode / ResolvedEdge
// with a resolved entity index entry (for the latter two), so lookups
// cannot fail here. Temporal columns are the exception: Phase A does not
// consult the TypeMap, so a kind it has no carrier for is refused here
// with ErrUnrepresentableTemporal (ADR 0025).
func phaseBDerive(queries []NamedQuery, entities []Entity, entityIndex map[entityLookupKey]int, tm TypeMap) ([]Query, error) {
	out := make([]Query, 0, len(queries))
	for _, q := range queries {
		p := Query{NamedQuery: q, MethodName: q.Name, Bare: LowerFirstRune(q.Name)}
		if q.Validated.Statement == resolver.StatementWrite {
			p.IsWrite = true
		}

		// Params field derivation.
		seenParam := make(map[string]int, len(q.Validated.Parameters))
		for pi, param := range q.Validated.Parameters {
			field := paramFieldName(param.Name)
			// A name of nothing but underscores mangles to the empty
			// string, which is not a Go field name. Refused only where
			// the emission spells one: the one-parameter and no-parameter
			// forms take the bare typed argument and derive no identifier
			// from the parameter name at all, so $_ is served there and
			// has to stay served (TestBlankParameterReachesOnlyThe-
			// SingleParameterForm pins both halves).
			//
			// Before this, the two-or-more form emitted a struct field
			// with no name and a bind expression reading `arg.,`, which
			// left go/format to refuse the emission as ErrFormatFailure —
			// a sentinel naming a template bug, handed to an author whose
			// query is the thing at fault. Deferred rather than
			// permanent: a future stage that spells Params fields
			// positionally admits this, which is what puts it under
			// ErrOutOfC6Scope rather than an unrepresentability.
			if field == "" && len(q.Validated.Parameters) > 1 {
				return nil, fmt.Errorf("%w: query %q parameter %d $%s mangles to no Go field name, and a query binding %d parameters spells one per parameter; rename it",
					ErrOutOfC6Scope, q.Name, pi, param.Name, len(q.Validated.Parameters))
			}
			if first, dup := seenParam[field]; dup {
				return nil, fmt.Errorf("%w: query %q parameters $%s (position %d) and $%s (position %d) both mangle to %q", ErrParamNameCollision, q.Name, q.Validated.Parameters[first].Name, first, param.Name, pi, field)
			}
			seenParam[field] = pi

			// Phase A guaranteed ResolvedProperty + representable width.
			prop, ok := param.Type.(resolver.ResolvedProperty)
			if !ok {
				//gqlc:unreachable param-type-invariant
				return nil, fmt.Errorf("%w: query %q parameter %d $%s: internal invariant — Phase A missed non-property type %s", ErrOutOfC6Scope, q.Name, pi, param.Name, ResolvedTypeName(param.Type))
			}
			ty, _ := tm.Property(prop.Type)
			p.ParamFields = append(p.ParamFields, Param{
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
				//gqlc:unreachable row-field-alias
				return nil, fmt.Errorf("%w: query %q column %d %q is neither a bare identifier nor a property access — add an explicit AS alias", ErrAliasRequired, q.Name, ci, col.Name)
			}
			if first, dup := seenRow[field]; dup {
				return nil, fmt.Errorf("%w: query %q columns %d (%q) and %d (%q) both derive to %q — add an explicit AS alias to disambiguate", ErrRowFieldCollision, q.Name, first, q.Validated.Columns[first].Name, ci, col.Name, field)
			}
			seenRow[field] = ci

			switch t := col.Type.(type) {
			case resolver.ResolvedProperty:
				if t.Type.Kind() == graph.KindList {
					// Schema list property: build a ColumnList plan so the
					// render layer uses the element-by-element decode path
					// rather than a whole-slice carrier (§4.7).
					elemResolved := resolver.ResolvedProperty{
						Type:     t.Type.Elem(),
						Nullable: !t.Type.ElemNotNull(),
					}
					plan, err := buildListElemPlan(elemResolved, entities, entityIndex, tm, -1, "")
					if err != nil {
						return nil, fmt.Errorf("query %q column %d %q: %w", q.Name, ci, col.Name, err)
					}
					p.RowFields = append(p.RowFields, Row{
						ColumnName: col.Name,
						Field:      field,
						GoType:     "[]" + plan.GoType,
						Nullable:   t.Nullable,
						Kind:       ColumnList,
						ListElem:   plan,
					})
					break
				}
				ty, _ := tm.Property(t.Type)
				p.RowFields = append(p.RowFields, Row{
					ColumnName: col.Name,
					Field:      field,
					GoType:     ty,
					Nullable:   t.Nullable,
					Kind:       ColumnProperty,
				})
			case resolver.ResolvedNode:
				idx := entityIndex[entityLookupKey{Kind: EntityNode, Labels: t.Labels}]
				p.RowFields = append(p.RowFields, Row{
					ColumnName: col.Name,
					Field:      field,
					GoType:     entities[idx].Name,
					Nullable:   t.Nullable,
					Kind:       ColumnNode,
				})
			case resolver.ResolvedEdge:
				idx := entityIndex[entityLookupKey{Kind: EntityEdge, EdgeKey: t.EdgeKey}]
				p.RowFields = append(p.RowFields, Row{
					ColumnName: col.Name,
					Field:      field,
					GoType:     entities[idx].Name,
					Nullable:   t.Nullable,
					Kind:       ColumnEdge,
				})
			case resolver.ResolvedTemporal:
				ty, ok := tm.Temporal(t.Kind)
				if !ok {
					return nil, fmt.Errorf("%w: query %q column %d %q projects %s", ErrUnrepresentableTemporal, q.Name, ci, col.Name, t)
				}
				p.RowFields = append(p.RowFields, Row{
					ColumnName: col.Name,
					Field:      field,
					GoType:     ty,
					Kind:       ColumnTemporal,
				})
			case resolver.ResolvedScalar:
				ty := tm.Scalar(t.Kind)
				kind := ColumnScalar
				// A null scalar has no narrowed carrier to assert against,
				// so it shares ColumnAny's untyped lane at the top level
				// (§5.5); a map scalar has a legitimate typed one.
				if t.Kind == resolver.ScalarNull {
					kind = ColumnAny
				}
				p.RowFields = append(p.RowFields, Row{
					ColumnName: col.Name,
					Field:      field,
					GoType:     ty,
					Kind:       kind,
				})
			case resolver.ResolvedUnknown:
				p.RowFields = append(p.RowFields, Row{
					ColumnName: col.Name,
					Field:      field,
					GoType:     "any",
					Kind:       ColumnAny,
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
					candidates[i] = entities[entityIndex[entityLookupKey{Kind: EntityEdge, EdgeKey: ek}]].Name
				}
				p.EdgeUnions = append(p.EdgeUnions, &EdgeUnion{
					QueryName:     q.Name,
					ColumnPos:     ci,
					ColumnName:    col.Name,
					FieldName:     field,
					InterfaceName: interfaceName,
					EdgeKeys:      t.EdgeKeys,
					Candidates:    candidates,
				})
				p.RowFields = append(p.RowFields, Row{
					ColumnName: col.Name,
					Field:      field,
					GoType:     interfaceName,
					Nullable:   t.Nullable,
					Kind:       ColumnEdgeUnion,
					EdgeKeys:   t.EdgeKeys,
				})
			case resolver.ResolvedList:
				// list-of-edgeUnion at a leaf synthesises an EdgeUnion so
				// models.go emits the interface + marker methods (§5.2).
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
						candidates[i] = entities[entityIndex[entityLookupKey{Kind: EntityEdge, EdgeKey: ek}]].Name
					}
					unionIdx = len(p.EdgeUnions)
					p.EdgeUnions = append(p.EdgeUnions, &EdgeUnion{
						QueryName:     q.Name,
						ColumnPos:     ci,
						ColumnName:    col.Name,
						FieldName:     field,
						InterfaceName: interfaceName,
						EdgeKeys:      leafEK,
						Candidates:    candidates,
					})
				}
				plan, err := buildListElemPlan(t.Element, entities, entityIndex, tm, unionIdx, interfaceName)
				if err != nil {
					return nil, fmt.Errorf("query %q column %d %q: %w", q.Name, ci, col.Name, err)
				}
				p.RowFields = append(p.RowFields, Row{
					ColumnName: col.Name,
					Field:      field,
					GoType:     "[]" + plan.GoType,
					Kind:       ColumnList,
					ListElem:   plan,
				})
			default:
				//gqlc:unreachable column-type-invariant
				return nil, fmt.Errorf("%w: query %q column %d %q: internal invariant — Phase A missed non-property type %s", ErrOutOfC6Scope, q.Name, ci, col.Name, ResolvedTypeName(col.Type))
			}
		}

		out = append(out, p)
	}
	return out, nil
}

// sweepIdentifiers runs spec §4.6's exported-identifier collision sweep
// across every emitted top-level identifier. Eight sources, in insertion
// order (§2.2 / §5.7):
//
//  0. the emitter's own scopePackage declarations
//  1. entity struct names (C2)
//  2. entity decode helper names (`decode<Name>`, promoted to sweep at C5)
//  3. method names (C1)
//  4. `<Method>Params` for two-plus-param queries (C1)
//  5. `<Method>Row` for two-plus-column queries (C1)
//  6. edgeUnion interface names, per-query-column (C5)
//  7. `<bareMethod>QueryText` consts, one per query (C6)
//
// First insertion-order duplicate wins, so a batch-derived name that
// lands on a fixed declaration reports the fixed declaration. Source 0
// carries the scopePackage subset alone: a method on *Queries shares no
// scope with sources 1-6, all of which are package-level types, and
// reserving one against them refuses a schema the emitter serves.
// Source 3 reaches source 0 only after Phase A has refused a
// NamedQuery.Name in the reserved set, so the fail it reports is the
// Phase A one.
// Marker method names (source 6's per-candidate satisfier) are
// unexported and stay off the sweep (§4.6 defence), because a marker
// collision is caught by the interface-name axis first.
//
// Source 7 is unexported too and is swept anyway, because nothing else
// catches it. The QueryText const shares the unexported namespace with
// source 2's decode<Entity> helpers, and the two derive from different
// author text — method names on one side, schema labels on the other — so
// a node label FooQueryText alongside a query named DecodeFoo produces
// decodeFooQueryText twice. Both names are generator-owned, which is why
// the capture guards are structurally blind to the pair: those police
// author-chosen identifiers against generator-owned ones, and here
// neither side is author-chosen. Until gqlc-igs4 that meant generate
// exited 0 and go build reported the redeclaration.
//
// Swept rather than made disjoint by a reserved suffix, because what
// this sweep asserts is that the generator-owned package-level names are
// pairwise distinct. That is an equality check over the names actually
// emitted, so a later source added without a matching insert here fails
// loudly rather than resting on a spelling convention nobody re-derives.
func sweepIdentifiers(entities []Entity, prepared []Query) error {
	seen := make(map[string]string, len(reservedIdentifiers)+len(entities)*2+len(prepared)*3)
	insert := func(ident, source string) error {
		if first, dup := seen[ident]; dup {
			return fmt.Errorf("%w: identifier %q emitted by both %s and %s", ErrIdentifierCollision, ident, first, source)
		}
		seen[ident] = source
		return nil
	}
	// Source 0: the emitter's own package-scope declarations. Seeded
	// rather than inserted — the set is a map, so it holds no duplicate
	// to report, and seeding keeps the fail message's "first" side on the
	// fixed declaration.
	for ident, scope := range reservedIdentifiers {
		if scope != scopePackage {
			continue
		}
		seen[ident] = fmt.Sprintf("the generated package's fixed declaration %q", ident)
	}
	// Source 1: entity struct names.
	for _, e := range entities {
		var srcAxis string
		if e.Kind == EntityNode {
			srcAxis = fmt.Sprintf("entity struct %q (schema labels %q)", e.Name, string(e.Labels))
		} else {
			srcAxis = fmt.Sprintf("entity struct %q (schema edge %s -[:%s]-> %s)", e.Name, string(e.EdgeKey.Source), string(e.EdgeKey.KeyLabels), string(e.EdgeKey.Target))
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
	// Source 7: query-text consts, one per query, emitted unconditionally
	// by every backend. Last, so the sources that were here before it
	// keep reporting the same side of a collision as "first".
	for _, p := range prepared {
		ident := QueryTextConst(p)
		if err := insert(ident, fmt.Sprintf("query %q query-text const %q", p.Name, ident)); err != nil {
			return err
		}
	}
	return nil
}

// findEdgeUnionLeaf walks a list-element chain looking for an
// edgeUnion leaf, returning the leaf's EdgeKeys and true when found.
// Nested lists recurse; anything else terminates the search. Called at
// Phase B to synthesise an EdgeUnion (§4.7 recursion arm, §5.2
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
// ListElem, walking the ResolvedType sum exactly once (spec §1.3).
// Every arm returns a non-nil plan whose Kind is one of the closed
// ColumnKind values; render walks the plan alone and never sees
// resolver.ResolvedType again. On an EdgeUnion arm the plan carries
// UnionIdx pointing at the entry the caller has already appended (or
// will append) to Query.EdgeUnions and GoType carries the synthesised
// sealed-interface name — Phase B threads both through so pointer
// stability across slice growth is not required (spec §3.1, §5.2).
//
// Widths the TypeMap has no carrier for surface ErrUnrepresentableWidth
// naming the offending width; temporal kinds it has no carrier for
// surface ErrUnrepresentableTemporal naming the offending kind. Unknown
// resolver variants surface ErrOutOfC6Scope naming the type — the
// deletion-fence for the failure mode this bead closes (spec §4.1
// synthetic-malformed-variant row).
//
// The unionInterfaceName argument carries the synthesised edgeUnion
// interface name (`<QueryName><RowField>`) the caller committed onto
// Query.EdgeUnions. Every arm except the EdgeUnion / List recursion
// ignores it.
func buildListElemPlan(t resolver.ResolvedType, entities []Entity, entityIndex map[entityLookupKey]int, tm TypeMap, unionIdx int, unionInterfaceName string) (*ListElem, error) {
	switch tt := t.(type) {
	case resolver.ResolvedProperty:
		if kind, field, unbuilt := unimplementedTypeKind(tt.Type); unbuilt {
			return nil, fmt.Errorf("%w: list element has %s", ErrUnimplementedTypeKind, unimplementedKindDetail(tt.Type, kind, field))
		}
		ty, ok := tm.Property(tt.Type)
		if !ok {
			return nil, fmt.Errorf("%w: list element has unrepresentable property width %s", ErrUnrepresentableWidth, tt.Type)
		}
		// A list element that is itself a list gets a nested plan, the same
		// shape the ResolvedList arm below builds. Without it the element
		// carries the whole slice type on a ColumnProperty plan, and the
		// render layer's scalar arm asserts a driver value straight to it —
		// `elem.([]float32)` for LIST<LIST<FLOAT32>>, which no Bolt driver
		// ever satisfies. The width is the same either way, so this changes
		// what the decode walks and not what the caller is handed.
		if tt.Type.Kind() == graph.KindList {
			nested, err := buildListElemPlan(resolver.ResolvedProperty{
				Type:     tt.Type.Elem(),
				Nullable: !tt.Type.ElemNotNull(),
			}, entities, entityIndex, tm, unionIdx, unionInterfaceName)
			if err != nil {
				return nil, err
			}
			return &ListElem{Kind: ColumnList, GoType: ty, Nested: nested}, nil
		}
		return &ListElem{Kind: ColumnProperty, GoType: ty}, nil
	case resolver.ResolvedNode:
		idx, ok := entityIndex[entityLookupKey{Kind: EntityNode, Labels: tt.Labels}]
		if !ok {
			return nil, fmt.Errorf("%w: list element references unknown node type %q", ErrOutOfC6Scope, string(tt.Labels))
		}
		name := entities[idx].Name
		return &ListElem{Kind: ColumnNode, GoType: name, EntityName: name}, nil
	case resolver.ResolvedEdge:
		idx, ok := entityIndex[entityLookupKey{Kind: EntityEdge, EdgeKey: tt.EdgeKey}]
		if !ok {
			return nil, fmt.Errorf("%w: list element references unknown edge type %s -[:%s]-> %s", ErrOutOfC6Scope, string(tt.EdgeKey.Source), string(tt.EdgeKey.KeyLabels), string(tt.EdgeKey.Target))
		}
		name := entities[idx].Name
		return &ListElem{Kind: ColumnEdge, GoType: name, EntityName: name}, nil
	case resolver.ResolvedEdgeUnion:
		if err := admitEdgeUnionCandidates(tt.EdgeKeys, entities, entityIndex, listElemSite); err != nil {
			return nil, err
		}
		return &ListElem{Kind: ColumnEdgeUnion, GoType: unionInterfaceName, UnionIdx: unionIdx}, nil
	case resolver.ResolvedTemporal:
		ty, ok := tm.Temporal(tt.Kind)
		if !ok {
			return nil, fmt.Errorf("%w: list element projects %s", ErrUnrepresentableTemporal, tt)
		}
		return &ListElem{Kind: ColumnTemporal, GoType: ty}, nil
	case resolver.ResolvedScalar:
		if tt.Kind == resolver.ScalarNull {
			return &ListElem{Kind: ColumnScalarNull, GoType: "any"}, nil
		}
		return &ListElem{Kind: ColumnScalar, GoType: tm.Scalar(tt.Kind)}, nil
	case resolver.ResolvedUnknown:
		return &ListElem{Kind: ColumnAny, GoType: "any"}, nil
	case resolver.ResolvedList:
		nested, err := buildListElemPlan(tt.Element, entities, entityIndex, tm, unionIdx, unionInterfaceName)
		if err != nil {
			return nil, err
		}
		return &ListElem{Kind: ColumnList, GoType: "[]" + nested.GoType, Nested: nested}, nil
	}
	return nil, fmt.Errorf("%w: list element has unknown resolved type %s", ErrOutOfC6Scope, ResolvedTypeName(t))
}
