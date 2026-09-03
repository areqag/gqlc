package codegen

// Opens the Phase Z / Phase B internals to the external test package. These
// tests drive the phases directly rather than through Generate, because that
// is the only way to hand a phase an input the schema front end would have
// refused; running them from `package codegen` put them, and the testify they
// assert with, outside govulncheck's call graph (bd gqlc-m5rc).
//
// Nothing here is imported, and that is load-bearing rather than incidental:
// `vuln-root-residual` measures a package's blindness from the imports of its
// in-package test files, so a bridge that names no third-party package leaves
// internal/codegen scannable. Adding an import to this file undoes the
// conversion the rest of the package just went through.
type (
	EntityLookupKey = entityLookupKey
	IdentifierScope = identifierScope
)

const (
	ScopePackage = scopePackage
	ScopeMethod  = scopeMethod
)

var (
	BuildListElemPlan    = buildListElemPlan
	PhaseBDerive         = phaseBDerive
	PhaseZAdmit          = phaseZAdmit
	RecordStructText     = recordStructText
	ReservedIdentifiers  = reservedIdentifiers
	TypeTextNamesCarrier = typeTextNamesCarrier
)
