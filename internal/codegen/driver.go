package codegen

// driverTarget owns the neo4j-go-driver module import paths written into
// generated files. Only the module paths vary across driver major
// versions; the emitted package identifiers (neo4j, dbtype) do not, so
// the templates keep those inline.
type driverTarget struct {
	neo4jImport  string
	dbtypeImport string
}

var driverV5 = driverTarget{
	neo4jImport:  "github.com/neo4j/neo4j-go-driver/v5/neo4j",
	dbtypeImport: "github.com/neo4j/neo4j-go-driver/v5/neo4j/dbtype",
}
