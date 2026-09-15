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

// Exit opens failExit to the external test package, which needs to name it
// to hold one in a helper's signature. An alias rather than exported fields:
// how a decoder lane leaves its enclosing function is the emitter's own
// business, and a test only ever passes through a value PairExit or
// YieldExit built.
type Exit = failExit

var (
	PairExit  = pairExit
	YieldExit = yieldExit
)

// DriverTarget is one driver major's emission target. The type is
// unexported, so an external test cannot build one; these two are the
// only ones there are, and a test names the major it is asking about.
//
// V6 is not decoration on V5: it is the only major whose type table
// carries a UUID, so a row that wants dbtype.UUID anywhere in an
// emission has to ask for it here.
type DriverTarget = driverTarget

var (
	TargetV5 = driverV5
	TargetV6 = driverV6
)

// Types is the Go-type table a target answers width questions with. A
// test holding a target uses it to reach the same table the emission
// walks ask, rather than building a second one that could disagree.
func (t DriverTarget) Types() TypeMap { return t.types() }

var (
	AccessModeText       = accessModeText
	DriverCarrier        = driverCarrier
	NarrowsANumericWidth = narrowsANumericWidth
	ParamBindExpr        = paramBindExpr
	// WriteMethod rather than renderCypherFile, which the AGE side's
	// equivalent test calls: neo4j's takes a driverTarget, whose type is
	// unexported, so an external test package cannot supply one.
	WriteMethod                   = writeMethod
	WriteSingleColumnDecodeIndent = writeSingleColumnDecodeIndent
)

// RenderQuerier emits querier.go for a batch. The v5 target, because the
// only thing the target decides in this file is the dbtype import path, and
// which import paths appear is the whole subject here rather than a
// difference between the majors — the corpus already holds the two equal
// modulo that path.
func RenderQuerier(pkg string, prepared []codegen.Query) []byte {
	return renderQuerier(pkg, prepared, driverV5)
}

// RenderRecordHelpers emits record_neo4j.go for a chosen encoding set and
// use record. The use map's element type is unexported, so a test states
// the directions as CarrierUseFlags and this converts.
//
// The target is the caller's since stage 2 of bd gqlc-eg4b. It used to be
// v5 unconditionally, on the reading that the only thing a target decides
// in this file is the dbtype import path — true while the two majors
// carried the same widths, and false now that one of them carries UUID
// and the other does not. A record with a UUID field emits on V6 and is
// skipped whole on V5, and that is the difference no golden of this file
// covers, because neo4j refuses a record as a stored property and no
// on-disk fixture can put one on a neo4j target.
func RenderRecordHelpers(pkg string, encodings []graph.PropertyType, uses map[graph.PropertyType]CarrierUseFlags, target DriverTarget) []byte {
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
	return renderRecordHelpers(pkg, encodings, inner, target)
}

// RenderModels emits models.go for a chosen entity set. The target is
// the caller's for RenderRecordHelpers' reason: it decides the dbtype
// import path AND, since stage 2, which widths the type table carries.
func RenderModels(pkg string, entities []codegen.Entity, prepared []codegen.Query, target DriverTarget) []byte {
	return renderModels(pkg, entities, prepared, target)
}

// CarrierUseFlags projects one carrier's use record for the external test
// package, which cannot read carrierUse's unexported fields. A projection
// rather than exported fields on the production type: which directions a
// batch reaches is the emitter's own bookkeeping and has no caller
// outside this package.
type CarrierUseFlags struct {
	Decode, Encode, EncodePtr, List, ListPtr bool
	ListElem, ListElemPtr                    bool
}

func flagsOf(u carrierUse) CarrierUseFlags {
	return CarrierUseFlags{
		Decode:      u.decode,
		Encode:      u.encode,
		EncodePtr:   u.encodePtr,
		List:        u.list,
		ListPtr:     u.listPtr,
		ListElem:    u.listElem,
		ListElemPtr: u.listElemPtr,
	}
}

// TemporalUseOf and RecordUseOf read one carrier out of a conversionUses
// answer. The maps' value type is unexported, so a test can hold a map
// but cannot spell its element; these are how it asks. Absent reads as
// the zero flags, which is what an unused carrier means.
func TemporalUseOf(prepared codegen.Prepared, name string, tm TypeMap) CarrierUseFlags {
	temporal, _, _ := conversionUses(prepared, tm)
	return flagsOf(temporal[name])
}

func RecordUseOf(prepared codegen.Prepared, encoding graph.PropertyType, tm TypeMap) CarrierUseFlags {
	_, records, _ := conversionUses(prepared, tm)
	return flagsOf(records[encoding])
}

// RecordUseEncodings is the key set of the record half, in canonical
// order, so a test can compare it against codegen.RecordEncodings without
// spelling the map's element type.
func RecordUseEncodings(prepared codegen.Prepared, tm TypeMap) []graph.PropertyType {
	_, records, _ := conversionUses(prepared, tm)
	out := make([]graph.PropertyType, 0, len(records))
	for pt := range records {
		out = append(out, pt)
	}
	slices.Sort(out)
	return out
}

// TemporalUseNames is the key set of the temporal half, sorted, so a test
// can assert that a batch reaching no temporal carrier marks none.
func TemporalUseNames(prepared codegen.Prepared, tm TypeMap) []string {
	temporal, _, _ := conversionUses(prepared, tm)
	out := make([]string, 0, len(temporal))
	for name := range temporal {
		out = append(out, name)
	}
	slices.Sort(out)
	return out
}
