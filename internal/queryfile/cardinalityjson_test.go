package queryfile

import "encoding/json"

// Ensure Cardinality serialises as its wire tag so goldens read naturally
// (":one" / ":many" / ":exec" via the enum's String()). This lives in a test
// file rather than production code because JSON encoding is a test-only
// concern for queryfile — the codegen consumer passes AnnotatedQuery
// directly, no wire.
//
// It is in-package because a method cannot be declared on another package's
// type, and it is ALONE in this file, importing only the standard library,
// because the rest of the suite moved to queryfile_test to get inside
// govulncheck's call graph (bd gqlc-m5rc). `vuln-root-residual` measures
// blindness from a package's .TestImports — the in-package test files' imports
// — so this residue leaves the package scannable only for as long as nothing
// third-party is added here.
func (c Cardinality) MarshalJSON() ([]byte, error) {
	return json.Marshal(c.String())
}
