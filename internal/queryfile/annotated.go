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
// a stream of rows, or no rows). Open enum: the members start at iota+1 and
// a new one appends, which is what let :iter land as a fourth member without
// churning the wire (ADR 0010 D8, gqlc-1a5). Zero value means "not set" and
// is a bug the front end never produces; codegen catches it as
// ErrInvalidCardinality.
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
	// CardinalityIter is the ":iter" annotation: the generated method
	// returns iter.Seq2[Row, error], streaming rows to the consumer's
	// range loop rather than materialising a slice. Read-only — codegen
	// refuses it on a write with ErrIterOnWrite (ADR 0010 D8).
	CardinalityIter
)

// String returns the wire tag ("one" / "many" / "exec" / "iter"), matching
// sqlc's tokens minus the leading colon. Used by both packages for error
// messages and by tests for golden encoding. Zero-value falls through to
// "invalid".
func (c Cardinality) String() string {
	switch c {
	case CardinalityOne:
		return "one"
	case CardinalityMany:
		return "many"
	case CardinalityExec:
		return "exec"
	case CardinalityIter:
		return "iter"
	}
	// Below the switch rather than in a `default`, so `exhaustive` still
	// checks this stringer for a missing arm — the members start at iota+1
	// and only the unset zero reaches here, so the answer is unchanged
	// (bd gqlc-51l6m).
	return "invalid"
}
