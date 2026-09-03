package neo4j

// Opens the renderer's unexported helpers to the external test package. They
// are asserted directly because each is a small text decision — the carrier a
// Go type decodes through, the access-mode word, a parameter's bind
// expression — that a whole-file golden would only cover incidentally;
// asserting them from `package neo4j` put those rows, and the testify they use,
// outside govulncheck's call graph (bd gqlc-m5rc).
//
// NO THIRD-PARTY IMPORT MAY BE ADDED HERE: vuln-root-residual reads a package's
// blindness off its in-package test files' imports, so a testify import in this
// file would return internal/codegen/neo4j to the blind set. Stdlib and gqlc's
// own packages are outside that rule and are imported below, because the
// projections need the argument and result types they name.
import (
	"slices"

	"github.com/areqag/gqlc/internal/codegen"
	"github.com/areqag/gqlc/internal/graph"
)

type TypeMap = typeMap

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

// RenderRecordHelpers emits record_neo4j.go for a chosen encoding set and
// use record. The use map's element type is unexported, so a test states
// the directions as CarrierUseFlags and this converts.
//
// The v5 target, because the only thing the target decides in this file is
// the dbtype import path — which the corpus test already holds equal
// across the two majors modulo that path.
func RenderRecordHelpers(pkg string, encodings []graph.PropertyType, uses map[graph.PropertyType]CarrierUseFlags) []byte {
	inner := make(map[graph.PropertyType]carrierUse, len(uses))
	for pt, f := range uses {
		inner[pt] = carrierUse{
			decode:    f.Decode,
			encode:    f.Encode,
			encodePtr: f.EncodePtr,
			list:      f.List,
			listPtr:   f.ListPtr,
		}
	}
	return renderRecordHelpers(pkg, encodings, inner, driverV5)
}

// CarrierUseFlags projects one carrier's use record for the external test
// package, which cannot read carrierUse's unexported fields. A projection
// rather than exported fields on the production type: which directions a
// batch reaches is the emitter's own bookkeeping and has no caller
// outside this package.
type CarrierUseFlags struct{ Decode, Encode, EncodePtr, List, ListPtr bool }

func flagsOf(u carrierUse) CarrierUseFlags {
	return CarrierUseFlags{u.decode, u.encode, u.encodePtr, u.list, u.listPtr}
}

// TemporalUseOf and RecordUseOf read one carrier out of a conversionUses
// answer. The maps' value type is unexported, so a test can hold a map
// but cannot spell its element; these are how it asks. Absent reads as
// the zero flags, which is what an unused carrier means.
func TemporalUseOf(prepared codegen.Prepared, name string) CarrierUseFlags {
	temporal, _ := conversionUses(prepared)
	return flagsOf(temporal[name])
}

func RecordUseOf(prepared codegen.Prepared, encoding graph.PropertyType) CarrierUseFlags {
	_, records := conversionUses(prepared)
	return flagsOf(records[encoding])
}

// RecordUseEncodings is the key set of the record half, in canonical
// order, so a test can compare it against codegen.RecordEncodings without
// spelling the map's element type.
func RecordUseEncodings(prepared codegen.Prepared) []graph.PropertyType {
	_, records := conversionUses(prepared)
	out := make([]graph.PropertyType, 0, len(records))
	for pt := range records {
		out = append(out, pt)
	}
	slices.Sort(out)
	return out
}

// TemporalUseNames is the key set of the temporal half, sorted, so a test
// can assert that a batch reaching no temporal carrier marks none.
func TemporalUseNames(prepared codegen.Prepared) []string {
	temporal, _ := conversionUses(prepared)
	out := make([]string, 0, len(temporal))
	for name := range temporal {
		out = append(out, name)
	}
	slices.Sort(out)
	return out
}
