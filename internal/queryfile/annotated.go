package queryfile

// AnnotatedQuery is one annotated query in the caller's file: its author-
// declared name and cardinality, plus the verbatim query text executed by
// the driver (ADR 0005: generated code executes the original text; the
// model shapes signatures, never reconstructs the query). JSON tags are
// present so queryfile goldens serialise as readable, stable text.
type AnnotatedQuery struct {
	Name        string      `json:"name"`
	Cardinality Cardinality `json:"cardinality"`
	Text        string      `json:"text"`
}

// Cardinality is the author-declared consumer-side row axis of a
// [AnnotatedQuery] (CONTEXT.md Generation-language: one row, a list of rows,
// or no rows). Open enum: :iter is reserved for post-v1 (ADR 0010 D8,
// gqlc-1a5) so a future constant can be added without churning the wire.
// Zero value means "not set" and is a bug the front end never produces;
// codegen catches it as ErrInvalidCardinality.
type Cardinality int

const (
	// CardinalityOne is the ":one" annotation: the generated method returns
	// exactly one row (or ErrNoRows at C1+).
	CardinalityOne Cardinality = iota + 1
	// CardinalityMany is the ":many" annotation: the generated method
	// returns a slice of rows.
	CardinalityMany
	// CardinalityExec is the ":exec" annotation: the generated method
	// returns no rows — a projection-less write.
	CardinalityExec
)

// String returns the wire tag ("one" / "many" / "exec"), matching sqlc's
// tokens minus the leading colon. Used by both packages for error messages
// and by tests for golden encoding. Zero-value falls through to "invalid".
func (c Cardinality) String() string {
	switch c {
	case CardinalityOne:
		return "one"
	case CardinalityMany:
		return "many"
	case CardinalityExec:
		return "exec"
	default:
		return "invalid"
	}
}
