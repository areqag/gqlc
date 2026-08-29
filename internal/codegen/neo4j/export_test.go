package neo4j

// Opens the renderer's unexported helpers to the external test package. They
// are asserted directly because each is a small text decision — the carrier a
// Go type decodes through, the access-mode word, a parameter's bind
// expression — that a whole-file golden would only cover incidentally;
// asserting them from `package neo4j` put those rows, and the testify they use,
// outside govulncheck's call graph (bd gqlc-m5rc).
//
// No imports here, deliberately: vuln-root-residual reads a package's blindness
// off its in-package test files' imports, so a third-party import in this file
// would return internal/codegen/neo4j to the blind set.
type TypeMap = typeMap

var (
	AccessModeText                = accessModeText
	DriverCarrier                 = driverCarrier
	ParamBindExpr                 = paramBindExpr
	WriteSingleColumnDecodeIndent = writeSingleColumnDecodeIndent
)
