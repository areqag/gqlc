package age

import (
	"strings"

	"github.com/areqag/gqlc/internal/codegen"
)

// helpers records which agtype encode / decode helpers a batch reaches
// for. Each is emitted only when something calls it: an unreferenced
// unexported function is a lint failure in the generated module, so the
// set the batch uses is the set the file carries.
type helpers struct {
	args    bool // agtypeArgs — some query binds at least one parameter
	str     bool
	boolean bool
	integer bool
	float   bool
}

// any reports whether the batch reaches for any helper at all, which is
// what decides whether models.go carries an import block.
func (h helpers) any() bool {
	return h.args || h.str || h.boolean || h.integer || h.float
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

// renderModels emits models.go (spec §5.2): the agtype helpers the query
// methods encode parameters and decode columns through.
func renderModels(pkg string, h helpers) []byte {
	var b strings.Builder
	b.WriteString(codegen.Header())
	b.WriteString("package " + pkg + "\n")
	if !h.any() {
		return []byte(b.String())
	}

	b.WriteString("\nimport (\n")
	if h.args || h.str {
		b.WriteString("\t\"encoding/json\"\n")
	}
	b.WriteString("\t\"fmt\"\n")
	if h.integer || h.float {
		b.WriteString("\t\"strconv\"\n\t\"strings\"\n")
	}
	b.WriteString(")\n")

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
	return []byte(b.String())
}
