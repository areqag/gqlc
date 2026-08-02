package age

import (
	"fmt"
	"strings"

	"github.com/areqag/gqlc/internal/codegen"
	"github.com/areqag/gqlc/internal/graph"
)

// The annotation AGE suffixes onto every entity it renders. A vertex and
// an edge are the same object otherwise, so this is the only thing on the
// wire that tells them apart.
const (
	vertexAnnotation = "::vertex"
	edgeAnnotation   = "::edge"
)

// helpers records which agtype encode / decode helpers a batch reaches
// for. Each is emitted only when something calls it: an unreferenced
// unexported function is a lint failure in the generated module, so the
// set the batch uses is the set the file carries.
type helpers struct {
	args     bool // agtypeArgs — some query binds at least one parameter
	str      bool
	boolean  bool
	integer  bool
	float    bool
	entity   bool // agtypeSpan / agtypeObject / agtypeEntity — the schema has an entity type
	prop     bool // agtypeProperty — some entity declares a non-nullable property
	nullProp bool // agtypeNullableProperty — some entity declares a nullable property
}

// any reports whether the batch reaches for any helper at all, which is
// what decides whether models.go carries an import block.
func (h helpers) any() bool {
	return h.args || h.str || h.boolean || h.integer || h.float || h.entity || h.prop || h.nullProp
}

// forEntities marks the helpers an entity emission reaches. Every entity
// decoder splits the wire value and reads the label out of it, so one
// entity anywhere in the schema puts the whole entity trio and the string
// helper in the file.
func (h *helpers) forEntities(entities []wiredEntity) {
	if len(entities) == 0 {
		return
	}
	h.entity = true
	h.str = true
	for _, e := range entities {
		for _, f := range e.Fields {
			h.need(f.GoType)
			if f.Nullable {
				h.nullProp = true
			} else {
				h.prop = true
			}
		}
	}
}

// need marks the helper one emitted Go type decodes through. Narrow
// integer and float widths ride the wide carrier and narrow through a Go
// conversion at the call site, so they mark the same helper their
// carrier does.
func (h *helpers) need(goType string) {
	switch agtypeCarrier(goType) {
	case "string":
		h.str = true
	case "bool":
		h.boolean = true
	case "int64":
		h.integer = true
	case "float64":
		h.float = true
	}
}

// agtypeCarrier picks the decode helper's return type for a Go type the
// emission wants to produce. Integer widths widen to int64 and float
// widths to float64, matching agtype's own two numeric scalars; the
// caller narrows with a Go conversion.
func agtypeCarrier(goType string) string {
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

// renderModels emits models.go (spec §5.2): one exported struct per
// schema node and edge type with the decoder that fills it, and the
// agtype helpers those and the query methods are built on.
//
// The entity surface is the same surface every other backend emits for
// the same schema — same struct names, same fields, same nullability.
// Only what the decoders read differs, because only the wire differs.
// The sealed edge-union interfaces those backends also emit have no
// counterpart here: this one refuses the column that would name them.
func renderModels(pkg string, entities []wiredEntity, h helpers) []byte {
	var b strings.Builder
	b.WriteString(codegen.Header())
	b.WriteString("package " + pkg + "\n")
	if !h.any() {
		return []byte(b.String())
	}

	b.WriteString("\nimport (\n")
	if h.entity {
		b.WriteString("\t\"bytes\"\n")
	}
	if h.args || h.str {
		b.WriteString("\t\"encoding/json\"\n")
	}
	b.WriteString("\t\"fmt\"\n")
	if h.integer || h.float {
		b.WriteString("\t\"strconv\"\n\t\"strings\"\n")
	}
	b.WriteString(")\n")

	writeEntities(&b, entities)

	if h.args {
		b.WriteString(`
// agtypeArgs renders a query's bound parameters as the single agtype
// argument cypher() takes. The value crosses as a Go string: agtype's
// OID is assigned per database, so pgx holds no registered codec for it
// and sends a string as an untyped literal, which is the form agtype's
// input function reads. JSON is a subset of agtype's own text syntax, so
// what the encoder produces is text AGE parses.
func agtypeArgs(args map[string]any) (string, error) {
	out, err := json.Marshal(args)
	if err != nil {
		return "", fmt.Errorf("gqlc: encode query parameters: %w", err)
	}
	return string(out), nil
}
`)
	}
	if h.str {
		b.WriteString(`
// agtypeString decodes an agtype string scalar. AGE renders one as a
// JSON string, escapes included, so the JSON decoder reads it back
// exactly; it also refuses every other agtype scalar, which is what
// stops a mis-shaped projection arriving as a plausible value.
//
// The target is a pointer because that is what separates the two shapes
// the decoder would otherwise agree on: unmarshalling a JSON null into a
// string succeeds and leaves it empty, so an agtype null would reach a
// non-nullable column as "".
func agtypeString(raw []byte) (string, error) {
	var out *string
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", fmt.Errorf("gqlc: %q is not an agtype string: %w", raw, err)
	}
	if out == nil {
		return "", fmt.Errorf("gqlc: %q is not an agtype string", raw)
	}
	return *out, nil
}
`)
	}
	if h.boolean {
		b.WriteString(`
// agtypeBool decodes an agtype boolean scalar. The two spellings are the
// whole vocabulary, so comparing against them refuses the numeric and
// abbreviated forms a general boolean parser accepts and agtype never
// emits.
func agtypeBool(raw []byte) (bool, error) {
	switch string(raw) {
	case "true":
		return true, nil
	case "false":
		return false, nil
	}
	return false, fmt.Errorf("gqlc: %q is not an agtype boolean", raw)
}
`)
	}
	if h.integer {
		b.WriteString(`
// agtypeInt64 decodes an agtype integer scalar, whose range is Go's
// int64. A value carrying the ::numeric annotation is one AGE evaluated
// in arbitrary precision: it is an integer only if it survives the parse
// as one, so a fractional part fails the decode rather than truncating
// quietly.
func agtypeInt64(raw []byte) (int64, error) {
	out, err := strconv.ParseInt(strings.TrimSuffix(string(raw), "::numeric"), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("gqlc: %q is not an agtype integer: %w", raw, err)
	}
	return out, nil
}
`)
	}
	if h.float {
		b.WriteString(`
// agtypeFloat64 decodes an agtype float scalar. The float parser reads
// it rather than the JSON decoder because agtype's float vocabulary is
// IEEE 754's: NaN, Infinity and -Infinity are values AGE emits and JSON
// has no spelling for. A value carrying the ::numeric annotation is one
// AGE evaluated in arbitrary precision, and lands on the nearest
// float64.
func agtypeFloat64(raw []byte) (float64, error) {
	out, err := strconv.ParseFloat(strings.TrimSuffix(string(raw), "::numeric"), 64)
	if err != nil {
		return 0, fmt.Errorf("gqlc: %q is not an agtype float: %w", raw, err)
	}
	return out, nil
}
`)
	}
	if h.entity {
		b.WriteString(`
// agtypeSpan reports where the value at the front of b ends: the offset
// of the first stop byte outside any nested structure, or len(b) when
// there is none. A string, a nested map and a nested list are each
// stepped over whole, so a delimiter one of them contains ends nothing.
func agtypeSpan(b []byte, stop byte) (int, error) {
	depth := 0
	for i := 0; i < len(b); i++ {
		if b[i] == '"' {
			for i++; i < len(b) && b[i] != '"'; i++ {
				if b[i] == '\\' {
					i++
				}
			}
			if i >= len(b) {
				return 0, fmt.Errorf("gqlc: %q leaves a string unterminated", b)
			}
			continue
		}
		if depth == 0 && b[i] == stop {
			return i, nil
		}
		switch b[i] {
		case '{', '[':
			depth++
		case '}', ']':
			depth--
			if depth < 0 {
				return 0, fmt.Errorf("gqlc: %q closes a structure it never opened", b)
			}
		}
	}
	if depth != 0 {
		return 0, fmt.Errorf("gqlc: %q leaves a structure open", b)
	}
	return len(b), nil
}

// agtypeObject splits an agtype map into its members, each key holding
// the undecoded text of its value. A map carries more than the schema
// declares: AGE stores whatever a writer wrote, so a value here may be of
// a shape no helper in this package reads, and only the ones a field asks
// for are ever decoded.
func agtypeObject(raw []byte) (map[string][]byte, error) {
	body := bytes.TrimSpace(raw)
	if len(body) < 2 || body[0] != '{' || body[len(body)-1] != '}' {
		return nil, fmt.Errorf("gqlc: %q is not an agtype map", raw)
	}
	body = bytes.TrimSpace(body[1 : len(body)-1])
	out := make(map[string][]byte)
	for len(body) > 0 {
		end, err := agtypeSpan(body, ',')
		if err != nil {
			return nil, err
		}
		member := bytes.TrimSpace(body[:end])
		body = bytes.TrimSpace(body[min(end+1, len(body)):])

		at, err := agtypeSpan(member, ':')
		if err != nil {
			return nil, err
		}
		if at == len(member) {
			return nil, fmt.Errorf("gqlc: %q is not a key and a value", member)
		}
		key, err := agtypeString(bytes.TrimSpace(member[:at]))
		if err != nil {
			return nil, err
		}
		out[key] = bytes.TrimSpace(member[at+1:])
	}
	return out, nil
}

// agtypeEntity splits an agtype vertex or edge into the label it carries
// and the undecoded text of each of its properties. A vertex and an edge
// are the same object but for the annotation, so requiring the one the
// caller named is what stands between an edge's decoder and a vertex
// whose label happens to match it.
func agtypeEntity(raw []byte, annotation string) (string, map[string][]byte, error) {
	body, ok := bytes.CutSuffix(bytes.TrimSpace(raw), []byte(annotation))
	if !ok {
		return "", nil, fmt.Errorf("gqlc: %q does not carry the %s annotation", raw, annotation)
	}
	fields, err := agtypeObject(body)
	if err != nil {
		return "", nil, err
	}
	rawLabel, ok := fields["label"]
	if !ok {
		return "", nil, fmt.Errorf("gqlc: %q carries no label", raw)
	}
	label, err := agtypeString(rawLabel)
	if err != nil {
		return "", nil, err
	}
	rawProps, ok := fields["properties"]
	if !ok {
		return "", nil, fmt.Errorf("gqlc: %q carries no properties", raw)
	}
	props, err := agtypeObject(rawProps)
	if err != nil {
		return "", nil, err
	}
	return label, props, nil
}
`)
	}
	if h.prop {
		b.WriteString(`
// agtypeProperty reads one property the schema declares NOT NULL out of a
// split entity. AGE drops a property whose value is null, so an absent
// key is how a null arrives, and taking the Go zero for one would report
// absence as a value the graph holds.
func agtypeProperty[T any](props map[string][]byte, key string, decode func([]byte) (T, error)) (T, error) {
	raw, ok := props[key]
	if !ok {
		var zero T
		return zero, fmt.Errorf("gqlc: property %q is absent", key)
	}
	return decode(raw)
}
`)
	}
	if h.nullProp {
		b.WriteString(`
// agtypeNullableProperty reads one nullable property out of a split
// entity, where the absence agtypeProperty refuses is the schema's null
// and reaches the caller as the nil pointer.
func agtypeNullableProperty[T any](props map[string][]byte, key string, decode func([]byte) (T, error)) (*T, error) {
	raw, ok := props[key]
	if !ok {
		return nil, nil
	}
	out, err := decode(raw)
	if err != nil {
		return nil, err
	}
	return &out, nil
}
`)
	}
	return []byte(b.String())
}

// writeEntities emits the schema's entity surface: one exported struct
// per node and edge type in Phase Z order, followed by the decoder that
// fills it.
//
// No sealed edge-union interface comes out here, and no marker method.
// Those belong to a column binding more than one candidate edge type,
// which this backend refuses ahead of Prepare (edgeUnionReason), so a
// batch reaching emission carries none: a prepared query's EdgeUnions
// is filled only from a ResolvedEdgeUnion column or from a list with
// one at its leaf, and unservedColumn refuses both.
func writeEntities(b *strings.Builder, entities []wiredEntity) {
	for _, e := range entities {
		b.WriteString("\n")
		writeEntityStruct(b, e.Entity)
		b.WriteString("\n")
		writeEntityDecoder(b, e)
	}
}

// writeEntityStruct emits one entity's exported struct declaration. A
// nullable property is a pointer field, which is what carries the
// difference between a property the graph holds and one it does not.
func writeEntityStruct(b *strings.Builder, e codegen.Entity) {
	if e.Kind == codegen.EntityNode {
		fmt.Fprintf(b, "// %s corresponds to the %s node type.\n", e.Name, e.DocAxis)
	} else {
		fmt.Fprintf(b, "// %s corresponds to the %s.\n", e.Name, e.DocAxis)
	}
	fmt.Fprintf(b, "type %s struct {\n", e.Name)
	for _, f := range e.Fields {
		b.WriteString("\t" + f.Field + " ")
		if f.Nullable {
			b.WriteString("*")
		}
		b.WriteString(f.GoType + "\n")
	}
	b.WriteString("}\n")
}

// writeEntityDecoder emits one entity's decode<Name> helper. It takes the
// undecoded column text a query method scanned, and holds the label check
// every call site goes through.
func writeEntityDecoder(b *strings.Builder, e wiredEntity) {
	// A zero-property entity reads nothing out of the map, and an
	// unused local does not compile.
	props := "props"
	if len(e.Fields) == 0 {
		props = "_"
	}
	fmt.Fprintf(b, "// decode%s decodes an agtype %s into a %s struct, enforcing\n", e.Name, strings.TrimPrefix(e.annotation, "::"), e.Name)
	b.WriteString("// the label and the per-property nullability the schema declares.\n")
	fmt.Fprintf(b, "func decode%s(raw []byte) (%s, error) {\n", e.Name, e.Name)
	fmt.Fprintf(b, "\tlabel, %s, err := agtypeEntity(raw, %q)\n", props, e.annotation)
	fmt.Fprintf(b, "\tif err != nil {\n\t\treturn %s{}, fmt.Errorf(%q, err)\n\t}\n", e.Name, "decode "+e.Name+": %w")
	fmt.Fprintf(b, "\tif label != %q {\n", e.label)
	fmt.Fprintf(b, "\t\treturn %s{}, fmt.Errorf(%q, %q, label)\n\t}\n",
		e.Name, "decode "+e.Name+": expected label %q, got %q", e.label)
	fmt.Fprintf(b, "\tvar out %s\n", e.Name)
	for i, f := range e.Fields {
		writeEntityFieldDecode(b, e.Entity, i, f)
	}
	b.WriteString("\treturn out, nil\n}\n")
}

// writeEntityFieldDecode emits the read of the property at index i. A
// width narrower than the agtype scalar it rides in converts at the field,
// so the struct carries the width the schema declared and not the
// carrier's.
func writeEntityFieldDecode(b *strings.Builder, e codegen.Entity, i int, f codegen.EntityField) {
	value := valueName(i)
	carrier := agtypeCarrier(f.GoType)
	reader := "agtypeProperty"
	if f.Nullable {
		reader = "agtypeNullableProperty"
	}
	fmt.Fprintf(b, "\t%s, err := %s(props, %q, %s)\n", value, reader, f.PropName, decodeFunc(carrier))
	fmt.Fprintf(b, "\tif err != nil {\n\t\treturn %s{}, fmt.Errorf(%q, err)\n\t}\n",
		e.Name, "decode "+e.Name+"."+f.Field+": %w")
	switch {
	case carrier == f.GoType:
		fmt.Fprintf(b, "\tout.%s = %s\n", f.Field, value)
	case f.Nullable:
		fmt.Fprintf(b, "\tif %s != nil {\n\t\tnarrowed := %s(*%s)\n\t\tout.%s = &narrowed\n\t}\n",
			value, f.GoType, value, f.Field)
	default:
		fmt.Fprintf(b, "\tout.%s = %s(%s)\n", f.Field, f.GoType, value)
	}
}

// wiredEntity is an entity together with the form it takes on the wire:
// the single label AGE stamps on every vertex and edge, and the
// annotation that says which of the two it is. The two fields are
// unexported and wireEntity is the only thing that fills them, so a
// populated value has been past the single-label check; the zero value
// carries no label to check against and belongs to no entity.
type wiredEntity struct {
	codegen.Entity

	label      string
	annotation string
}

// wireEntity is an entity's wire form, with ok=false for a node or edge
// type declared under any number of labels but one.
func wireEntity(e codegen.Entity) (wiredEntity, bool) {
	labels, annotation := e.EdgeKey.KeyLabels, edgeAnnotation
	if e.Kind == codegen.EntityNode {
		labels, annotation = e.Labels, vertexAnnotation
	}
	label, ok := single(labels)
	if !ok {
		return wiredEntity{}, false
	}
	return wiredEntity{Entity: e, label: label, annotation: annotation}, true
}

// single is the one label a key label set holds, with ok=false for any
// other number.
func single(k graph.LabelSetKey) (string, bool) {
	parts := k.Split()
	if len(parts) != 1 {
		return "", false
	}
	return parts[0], true
}
