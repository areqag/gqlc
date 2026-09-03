package neo4j

// Opens the renderer's unexported helpers to the external test package. They
// are asserted directly because each is a small text decision — the carrier a
// Go type decodes through, the access-mode word, a parameter's bind
// expression — that a whole-file golden would only cover incidentally;
// asserting them from `package neo4j` put those rows, and the testify they use,
// outside govulncheck's call graph (bd gqlc-m5rc).
//
// No THIRD-PARTY imports here, deliberately: vuln-root-residual reads a
// package's blindness off its in-package test files' imports, so a testify
// import in this file would return internal/codegen/neo4j to the blind set. The
// first-party one below is what the gate does not read.
import "github.com/areqag/gqlc/internal/codegen"

type TypeMap = typeMap

// TemporalUseFlags is temporalUse with its decisions readable from the external
// test package, whose own fields are unexported.
type TemporalUseFlags struct {
	Decode, Encode, EncodePtr, List, ListPtr bool
}

// TemporalUses re-keys temporalUses' result so each decision can be asserted by
// name. The whole map is returned rather than one carrier's flags because its
// EMPTINESS is a claim too: a parameter whose leaf is not a temporal carrier
// must reach no site at all.
func TemporalUses(prepared codegen.Prepared) map[string]TemporalUseFlags {
	out := make(map[string]TemporalUseFlags)
	for name, u := range temporalUses(prepared) {
		out[name] = TemporalUseFlags{
			Decode:    u.decode,
			Encode:    u.encode,
			EncodePtr: u.encodePtr,
			List:      u.list,
			ListPtr:   u.listPtr,
		}
	}
	return out
}

var (
	AccessModeText       = accessModeText
	DriverCarrier        = driverCarrier
	IsDeclaredRecord     = isDeclaredRecord
	NarrowsANumericWidth = narrowsANumericWidth
	ParamBindExpr        = paramBindExpr
	// WriteMethod rather than renderCypherFile, which the AGE side's
	// equivalent test calls: neo4j's takes a driverTarget, whose type is
	// unexported, so an external test package cannot supply one.
	WriteMethod                   = writeMethod
	WriteSingleColumnDecodeIndent = writeSingleColumnDecodeIndent
)
