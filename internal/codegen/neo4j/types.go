package neo4j

import (
	"github.com/areqag/gqlc/internal/codegen"
	"github.com/areqag/gqlc/internal/graph"
	"github.com/areqag/gqlc/internal/resolver"
)

// typeMap is the driver's Go-type table (spec §5.1) the shared phases
// read. Stateless: every entry is a pure function of the resolved type.
type typeMap struct{}

// Property maps a resolved property type to its native Go emission (spec
// §5.1). Returns (typeText, ok): ok=false for the eight unrepresentable
// widths (INT128 / INT256 / UINT128 / UINT256 / FLOAT16 / FLOAT128 /
// FLOAT256 / DECIMAL) — caller routes to ErrUnrepresentableWidth naming
// the width.
//
// The two '*' positions belong to different owners. The WHOLE-VALUE star
// is the caller's: a nullable column, field or parameter gets its
// leading '*' at emission time, as before. The ELEMENT star is this
// table's own, applied by the list arm from the width's ElemNotNull
// qualifier, so every position carrying a list — Row, EntityField, Param
// and the nested text under each — reads one answer rather than four
// re-derivations of it. A nullable column of LIST<INT64> is therefore
// `*[]*int64`: one star from each owner.
//
// FLOAT32 returns "float32" (the
// carrier-widens-on-encode / narrow-on-decode contract is enforced at
// the emission sites, spec §5.5 / §5.7).
//
// DATE / TIME / LOCAL TIME / DURATION return the gqlc-owned neutral
// carriers "Date" / "Time" / "LocalTime" / "Duration", declared in the
// generated package's own temporal.go and bridged to dbtype by the
// unexported helpers in temporal_neo4j.go (ADR 0033). They name no
// package, because they are declared alongside the code that uses them.
// TIMESTAMP stays "time.Time": the stdlib type is already neutral and
// models an instant without residue. DURATION collapses its
// (YEAR TO MONTH) vs (DAY TO SECOND) qualifier onto a single Duration
// carrying Months / Days / Seconds / Nanos (see ADR 0002 Consequences).
//
// EXEMPT FROM gocyclo, NOT FROM gocognit. gocyclo counts this 31 because it
// increments once per `case` and the table has one per property width;
// gocognit counts it 10, charging the `switch` once and the nesting nothing.
// A 3x disagreement, and gocognit is the one describing what a reader faces:
// gocyclo alone ranked this the 6th-most-complex function in the repository
// when it is one flat table behind two container guards.
//
// Splitting it to satisfy the count is not available, and that is a fact
// about this method rather than a preference. ONE guard reads it by
// STRUCTURE: typescan.PropertyArms skips any decl whose `fn.Recv == nil` and
// takes the method name as an argument, so a table moved to a plain function
// is invisible to it — the walk is propertyArmNames in decoder_test.go, and
// it feeds TWO obligations: types_test.go:123 and decoder_test.go:206. That
// does not pass vacuously at either. types_test.go asserts
// `require.NotEmpty(t, arms, ...)` before ranging over them, precisely so a
// walk that read nothing cannot hold the table to nothing, and
// decoder_test.go's `require.Contains(t, arms, ...)` reds on an empty map
// too. It reds LOUDLY, and was measured doing so on 2026-09-10 when exactly
// that split was attempted.
//
// The age copy of this table is held by a SECOND structural guard that this
// package has no equivalent of: age's render_queries_test.go reads its
// typeMap's RETURN statements and refuses a return whose shape it cannot
// read. Nothing here walks returns, so the neo4j table rests on the arms
// walk alone — do not read the age comment as also describing this one.
//
//nolint:gocyclo // flat per-width dispatch table; gocognit scores it 10 and still gates it
func (t typeMap) Property(pt graph.PropertyType) (string, bool) {
	if pt.Kind() == graph.KindList {
		elemTy, ok := t.Property(pt.Elem())
		if !ok {
			return "", false
		}
		// An element the schema permits to be NULL carries the star, the
		// same rule every other nullable position obeys. `any` is the one
		// exemption: it already carries null as nil, and this backend
		// deliberately does not walk a []any at all (isSliceType), so a
		// star there would force the walk and add a second spelling of
		// the same null.
		//
		// Folded into the element text rather than written as
		// `"[]*" + elemTy` so this method keeps exactly one list return of
		// the form `"[]" + elem` — the only shape render_queries_test.go's
		// type-table walk can read, and it refuses what it cannot read
		// rather than skipping it.
		if !pt.ElemNotNull() && elemTy != "any" {
			elemTy = "*" + elemTy
		}
		return "[]" + elemTy, true
	}
	if pt.Kind() == graph.KindRecord {
		if pt == graph.TypeAnyRecord {
			// Fields undeclared, so there is no struct to build: the
			// record whose contents are unconstrained maps to Go's
			// unconstrained string-keyed product, exactly as ANY maps
			// to any and LIST<ANY> to []any (spec §3).
			return "map[string]any", true
		}
		// Threading this Property in as the field carrier is what makes
		// a record inherit neo4j's own refusals: a field of a width
		// this table has no case for refuses the whole record, through
		// the same ErrUnrepresentableWidth channel a bare property of
		// that width would.
		return codegen.RecordStructText(pt.Fields(), t.Property)
	}
	if pt.Kind() == graph.KindUnion {
		// The carrier is `any` and the admission rule is the whole of the
		// decision (spec §4): the members have to be pairwise distinct on
		// THIS driver's wire, because the emitted decode has the wire shape
		// and nothing else to narrow by. UNION<INT32|INT64> is refused
		// here, both widths arriving as int64.
		return codegen.UnionCarrier(pt, t.Property, wireFamily)
	}
	switch pt {
	case graph.TypeString:
		return "string", true
	case graph.TypeBytes:
		return "[]byte", true
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
		return "Date", true
	case graph.TypeTime:
		return "Time", true
	case graph.TypeLocalTime:
		return "LocalTime", true
	case graph.TypeTimestamp:
		return "time.Time", true
	case graph.TypeDuration:
		return "Duration", true
	case graph.TypeAnyPropertyValue:
		return "any", true
	case graph.TypeList:
		// Intercepted by the Kind() guard above; unreachable here.
		// Listed so the exhaustive linter sees the full constant set.
		return "[]any", true
	case graph.TypeAnyRecord:
		// RECORD<ANY> spelled out, so the Kind() guard above intercepts
		// it and this arm is unreachable. Listed so the exhaustive
		// linter sees the full constant set, and answering
		// "map[string]any" keeps it agreeing with the arm that does the
		// work — the arrangement graph.TypeList already has.
		return "map[string]any", true
	case graph.TypeUUID:
		// Refused on BOTH majors here, and unlike the eight below that
		// is a statement about this table today rather than a permanent
		// one. dbtype.UUID exists — it landed in neo4j-go-driver v6.2.0
		// and v5.28.4 has no counterpart, the two versions
		// test/data/codegen/go.mod pins — so the v6 answer is owed and
		// is not given here.
		//
		// Giving it is stage 2 of bd gqlc-eg4b and needs more than this
		// arm: a carrier that exists on one major and not the other is
		// the first of its kind in this table, so the table has to
		// learn which major it is answering for, and the v5 refusal
		// then needs a sentinel saying the width is representable but
		// not on this driver rather than ErrUnrepresentableWidth, which
		// says something false about it. Until that lands, both majors
		// refuse and the message names the width, which is true on v5
		// and merely incomplete on v6.
		//
		// test/data/codegen/invalid/uuid_width_unrepresentable is the
		// fixture that holds this, on all three enrolled targets.
		return "", false
	case graph.TypeInt128, graph.TypeInt256,
		graph.TypeUint128, graph.TypeUint256,
		graph.TypeFloat16, graph.TypeFloat128, graph.TypeFloat256,
		graph.TypeDecimal:
		// The eight unrepresentable widths — no faithful Go carrier on
		// neo4j-go-driver (v5 and v6 alike). Permanent, per §9 (spec).
		return "", false
	}
	return "", false
}

// wireFamily folds one carrier text onto the equivalence class of declared
// widths this driver delivers as one indistinguishable shape (CONTEXT.md,
// "wire family"). It is asked of a closed union's members alone, because
// that is the one place two declared widths have to be told apart by their
// arrival rather than by the declaration that asked for them.
//
// It is driverCarrier's answer and not a second table, which is the point.
// driverCarrier already says which neo4j.GetRecordValue[T] a carrier is
// fetched through, and two widths fetched through one T are exactly two
// widths that arrive as one shape: every integer width comes back int64,
// every float float64, every list []any, and a record and a Cypher map
// both map[string]any. A separate table would be a second statement of the
// same fact with its own opportunity to disagree with the decode this
// package actually emits. The temporal widths separate here for free —
// driverCarrier answers each a distinct dbtype, and TIMESTAMP time.Time —
// which is why a zoned member rides a union on this backend while AGE
// refuses one.
//
// ANY is the one carrier driverCarrier's answer has to be overridden for.
// It arrives as whatever the writer wrote, so it owns no shape of its own
// and leaves none for a member beside it — WireFamilyIndistinct is the tag
// that says so. A nested UNION carries as `any` too and lands here for the
// same reason.
//
// A PLAIN FUNCTION and not a typeMap method, deliberately. age's census
// walk tells a carrier method from the rest by its declared result shape,
// and a (string, bool) method of this receiver would be swept as one and
// hold its family tags to decodeFunc arms. The tags are not Go types. The
// same reading would be wrong here, where decoder_test.go's arms walk
// takes a method name and would be widened by a second carrier-shaped
// method appearing beside Property.
func wireFamily(goType string) string {
	if goType == codegen.UnionCarrierText {
		return codegen.WireFamilyIndistinct
	}
	return driverCarrier(goType)
}

// StorableProperty refuses a record, a list of records, and a list whose
// element is itself a list, and admits everything else.
//
// This is the storage axis, not the carrier axis: Property answers
// "[][]int16" for LIST<LIST<INT16>> and is right to, and this backend
// emits a working recursive decode for a nested list arriving as a
// QUERY VALUE. What refuses it is the server, which stores a property
// value only if it is a scalar or a flat list of scalars and answers a
// nested write with "Collections containing collections can not be
// stored in properties" — measured against the pinned image by
// TestNeo4jRefusesANestedListStoredProperty (ADR 0035, bd gqlc-v0gk).
// Emitting for such a property would hand the author a struct field no
// write could ever fill, so it fails at generation, where it can name
// the property.
//
// Elem() strips the NOT NULL suffix, so LIST<LIST<INT16> NOT NULL> is
// caught the same as LIST<LIST<FLOAT32>>; a depth-3 list is caught at
// its outer level, its element being a list; LIST<LIST<ANY VALUE>> is
// caught for the same reason. LIST<ANY VALUE> is ADMITTED and can carry
// a nested list at runtime, which no static check can see — that write
// fails at the server as it does today (ADR 0035 names the limit).
//
// The RECORD arms rest on the same kind of measurement, taken against the
// pinned image by TestNeo4jRefusesAMapValuedStoredProperty rather than
// assumed. The server answered:
//
//	Neo.ClientError.Statement.TypeError (Property values can only be of
//	primitive types or arrays thereof. Encountered: Map{…}.)
//
// with two controls green in the same run — a scalar property on the same
// session was stored, and the identical map came back as a projected
// column — so the refusal is about the property slot rather than about
// maps in general. That asymmetry is why only this axis refuses a record
// while Property still carries one: a record arriving as a query VALUE
// decodes fine, and §6 of the spec turns on the difference.
//
// THE LIST ARM IS NOT AN INFERENCE FROM THE BARE ONE. The rule the server
// states admits "arrays thereof", and a flat list of scalars IS stored —
// ADR 0035 turns on exactly that — so an array of maps had to be asked
// about separately. It was, in the same test, and is refused by the same
// rule. Without this arm a LIST<RECORD<…>> would reach the server through
// the list arm above, which asks only whether the element is a list.
//
// KindRecord covers RECORD<ANY> and the fieldless RECORD<> too: Kind()
// tests the "RECORD<" prefix, which all three spellings share. A depth-3
// list of records is already refused one level out by the nested-list arm.
//
// THE UNION ARM IS THE LIST ARM ONLY, and it rests on its own measurement
// rather than on the record one next door. A BARE union property is a
// single primitive value and is STORED — the pinned image kept
// `{u: true}` — so the refusal cannot be hung on KindUnion at the top.
// What the server refuses is the ARRAY: a stored property array must be
// homogeneous in its storage type, and every union gqlc admits is
// heterogeneous by construction, because codegen.UnionMemberCollision
// already refuses at declaration any union whose members share a wire
// family. So a LIST<UNION<…>> that reaches here spans two or more
// families and is exactly the shape measured by
// TestNeo4jRefusesAHeterogeneousArrayStoredProperty, against Neo4j Kernel
// 5.26.28 community, which answered:
//
//	Neo4j only supports a subset of Cypher types for storage as
//	singleton or array properties.
//
// with three controls green in the same run — a homogeneous BOOL array
// and a homogeneous INT array were both stored on that session, and the
// identical heterogeneous list came back as a projected column. So the
// refusal is the property slot's, not the server's view of mixed lists.
//
// IT REFUSES THE WHOLE WIDTH THOUGH ONE PAIR IS ACCEPTED, and that is the
// deliberate part. The same run measured `{ns: [1, 1.5]}` STORED, and read
// it back as `[1.0, 1.5]` with both elements typed FLOAT: the server
// widens a long into a double rather than refusing the array. An
// INT64|FLOAT64 union is wire-distinct and would reach here, so a guard
// keyed on "the pairs the server rejects out loud" would admit the one
// width whose failure is SILENT — the emitted decode dispatch would
// resolve that element to the FLOAT64 member, the INT64 the writer stored
// having been destroyed in the store. A loud refusal at generation, where
// the property can be named, beats a lossy round-trip at runtime.
//
// LIST<ANY VALUE> stays admitted for the reason it is admitted above: it
// can carry a heterogeneous list at runtime, which no static check can
// see, and that write fails at the server as it does today.
func (typeMap) StorableProperty(pt graph.PropertyType) bool {
	if pt.Kind() == graph.KindRecord {
		return false
	}
	if pt.Kind() != graph.KindList {
		return true
	}
	elem := pt.Elem().Kind()
	return elem != graph.KindList && elem != graph.KindRecord && elem != graph.KindUnion
}

// Temporal maps a resolver Temporal kind to the Go type text C3 emits
// (spec §5.1 column-shape table). Returns (typeText, ok): ok=false
// routes the caller to ErrUnrepresentableTemporal naming the kind.
// Every kind of the enum has a carrier — the four neutral ones plus
// time.Time for the zoned datetime (ADR 0033) — so every arm answers
// ok=true and this backend never takes that channel.
func (typeMap) Temporal(k resolver.Temporal) (string, bool) {
	switch k {
	case resolver.TemporalDate:
		return "Date", true
	case resolver.TemporalTime:
		return "Time", true
	case resolver.TemporalLocalTime:
		return "LocalTime", true
	case resolver.TemporalDateTime:
		return "time.Time", true
	case resolver.TemporalLocalDateTime:
		return "LocalDateTime", true
	case resolver.TemporalDuration:
		return "Duration", true
	}
	// Only a value converted in from outside resolver.Temporal's
	// vocabulary reaches here; refusing beats guessing a carrier for a
	// kind the resolver never named.
	return "", false
}

// Scalar maps a resolver Scalar kind to the Go type text C3 emits (spec
// §5.1 column-shape table). Bool / Int / Float / String bridge to the
// driver's native carriers; Null → any (the openCypher null literal is
// legal-but-pointless projection); Map → map[string]any. Every arm is the
// Go shape of a value the driver's record vocabulary already carries.
func (typeMap) Scalar(k resolver.Scalar) string {
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
	// Only a value converted in from outside resolver.Scalar's vocabulary
	// reaches here; projecting it undecoded beats guessing a Go type for
	// a kind the resolver never named.
	return "any"
}

// driverCarrier picks the neo4j.GetRecordValue[T] type for a Go type
// the emission wants to produce. Integer widths widen to int64; float
// widths widen to float64; string / bool pass through. The caller
// narrows via a Go conversion.
//
// A slice widens to []any, which is the only shape a Bolt driver has for
// one. neo4j.PropertyValue admits []byte and []any and no other slice,
// GetProperty's own doc says "any property array value other than byte
// array is typed as []any", and the hydrator builds exactly that:
// `func (h *hydrator) array() []any`. So the element widths the schema
// declared are not on the wire to be asserted — a LIST<STRING> arrives
// as []any holding strings, and narrowing it is per element rather than
// whole. []byte is the exception because BYTES is the one width the
// driver does hand back as a Go slice of its own.
//
// A declared record widens to map[string]any, the shape the driver
// already hands a Cypher map back as, and the struct is built from it
// field by field — the same relationship a slice has to []any, and for
// the same reason: the driver has no narrower carrier to offer, so the
// declared shape is this package's to build. RECORD<ANY> needs no arm
// because map[string]any is what it already carries as, and the default
// arm answering it with itself is what tells every decode site to assign
// it bare.
func driverCarrier(goType string) string {
	if isSliceType(goType) {
		return "[]any"
	}
	if codegen.IsRecordStruct(goType) {
		return "map[string]any"
	}
	switch goType {
	case "int", "int8", "int16", "int32", "int64",
		"uint", "uint8", "uint16", "uint32", "uint64":
		return "int64"
	case "float32", "float64":
		return "float64"
	case "Date", "Time", "LocalTime", "LocalDateTime", "Duration":
		// The neutral temporal carriers (ADR 0033). The driver still
		// speaks dbtype on both wires, so the carrier is the dbtype
		// counterpart — but unlike every other arm here the two are not
		// conversion-compatible, and narrowExpr / widenExpr route them
		// through the emitted to<X> / from<X> helpers instead of a Go
		// conversion.
		return "dbtype." + goType
	default:
		return goType
	}
}
