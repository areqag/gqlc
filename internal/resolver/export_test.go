package resolver

// For FuzzResolve (fuzz_test.go), which is an external test package: the
// closed sentinel set its error-class property holds a refusal to, and the
// registry the corpus sweep resolves under, so a seed taken from
// sweep.manifest.tsv resolves as its cell says.
var (
	AllSentinels = allSentinels
	RegR7        = regR7
)
