package codegen

// driverTarget owns every text fragment written into generated files
// that varies across neo4j-go-driver major versions: the module import
// paths and the driver-handle interface name (v6 renamed
// DriverWithContext back to Driver). The emitted package identifiers
// (neo4j, dbtype) and the rest of the API surface the templates use are
// name-identical across the two majors, so the templates keep those
// inline.
type driverTarget struct {
	neo4jImport  string
	dbtypeImport string
	driverIface  string
}

var (
	driverV5 = driverTarget{
		neo4jImport:  "github.com/neo4j/neo4j-go-driver/v5/neo4j",
		dbtypeImport: "github.com/neo4j/neo4j-go-driver/v5/neo4j/dbtype",
		driverIface:  "neo4j.DriverWithContext",
	}
	driverV6 = driverTarget{
		neo4jImport:  "github.com/neo4j/neo4j-go-driver/v6/neo4j",
		dbtypeImport: "github.com/neo4j/neo4j-go-driver/v6/neo4j/dbtype",
		driverIface:  "neo4j.Driver",
	}
)

// target maps a DriverVersion to its emission target. DriverV5 is the
// zero value, so an unconfigured Codegen emits v5 — every non-DriverV6
// value takes the v5 arm.
func (v DriverVersion) target() driverTarget {
	if v == DriverV6 {
		return driverV6
	}
	return driverV5
}
