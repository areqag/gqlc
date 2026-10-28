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
//
// The vectors field is the one entry that is not emitted text but a
// TYPE-TABLE answer: dbtype.Vector exists in neo4j-go-driver v6 and has no
// counterpart in v5, so the two majors carry different sets of property
// widths (ADR 0050). false is the v5 answer and the zero value, so a
// typeMap built with no target refuses VECTOR, which is the direction that
// cannot emit code a driver will not pack.
type driverTarget struct {
	// key is the driver wire key this target is enrolled under
	// (internal/cli/backends). It is not emitted into generated code: it
	// is what a refusal calls the target by, so an author reading
	// "carried only on the neo4j-go-v6 target" reads back the string they
	// would write in their config.
	key string

	neo4jImport  string
	dbtypeImport string
	driverIface  string
	sessionIface string
	vectors      bool
}

var (
	driverV5 = driverTarget{
		key:          "neo4j-go-v5",
		neo4jImport:  "github.com/neo4j/neo4j-go-driver/v5/neo4j",
		dbtypeImport: "github.com/neo4j/neo4j-go-driver/v5/neo4j/dbtype",
		driverIface:  "neo4j.DriverWithContext",
		sessionIface: "neo4j.SessionWithContext",
	}
	driverV6 = driverTarget{
		key:          "neo4j-go-v6",
		neo4jImport:  "github.com/neo4j/neo4j-go-driver/v6/neo4j",
		dbtypeImport: "github.com/neo4j/neo4j-go-driver/v6/neo4j/dbtype",
		driverIface:  "neo4j.Driver",
		sessionIface: "neo4j.Session",
		vectors:      true,
	}
)

// driverTargets is every major this backend emits for, which is the
// roster the driver-version refusal is decided against. Listed rather
// than derived because DriverVersion is a closed enum of two and target()
// maps onto exactly these values.
var driverTargets = []driverTarget{driverV5, driverV6}

// types is the Go-type table for this major. Every phase and every walk
// that asks a width question asks it through here, so a width one major
// carries and the other does not is answered consistently or not at all.
func (t driverTarget) types() typeMap { return typeMap{vectors: t.vectors} }

// target maps a DriverVersion to its emission target. DriverV5 is the
// zero value, so an unconfigured Codegen emits v5 — every non-DriverV6
// value takes the v5 arm.
func (v DriverVersion) target() driverTarget {
	if v == DriverV6 {
		return driverV6
	}
	return driverV5
}
