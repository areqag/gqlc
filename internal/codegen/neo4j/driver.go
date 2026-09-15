package neo4j

// driverTarget owns every text fragment written into generated files
// that varies across neo4j-go-driver major versions: the module import
// paths, the driver-handle interface name (v6 renamed DriverWithContext
// back to Driver) and the session interface name.
//
// The session name is asymmetric, and only one direction is load-bearing.
// v6 keeps SessionWithContext as an alias for Session (the
// `type SessionWithContext = Session` declaration in v6's neo4j/session.go),
// so either spelling compiles there; v5's Session is the deprecated
// non-context session, a different interface entirely (the `type Session
// interface` declaration in v5's neo4j/session.go, with v5's context
// session declared separately in neo4j/session_with_context.go), so v5
// must be spelled SessionWithContext. A single inline spelling would
// therefore have to be the v5 one, and this field exists so the emission
// says what it means on each major rather than relying on that alias.
//
// The emitted package identifiers
// (neo4j, dbtype) are name-identical across the two majors, as is every
// other API name the templates reach for — BeginTransaction,
// ExplicitTransaction, SessionConfig and the ExecuteRead/ExecuteWrite
// pair among them — so the templates keep those inline.
type driverTarget struct {
	neo4jImport  string
	dbtypeImport string
	driverIface  string
	sessionIface string
}

var (
	driverV5 = driverTarget{
		neo4jImport:  "github.com/neo4j/neo4j-go-driver/v5/neo4j",
		dbtypeImport: "github.com/neo4j/neo4j-go-driver/v5/neo4j/dbtype",
		driverIface:  "neo4j.DriverWithContext",
		sessionIface: "neo4j.SessionWithContext",
	}
	driverV6 = driverTarget{
		neo4jImport:  "github.com/neo4j/neo4j-go-driver/v6/neo4j",
		dbtypeImport: "github.com/neo4j/neo4j-go-driver/v6/neo4j/dbtype",
		driverIface:  "neo4j.Driver",
		sessionIface: "neo4j.Session",
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
