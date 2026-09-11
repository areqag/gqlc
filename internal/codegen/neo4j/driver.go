package neo4j

import "github.com/areqag/gqlc/internal/codegen"

// driverTarget owns every text fragment written into generated files
// that varies across neo4j-go-driver major versions: the module import
// paths, the driver-handle interface name (v6 renamed DriverWithContext
// back to Driver) and the session interface name.
//
// The session name is asymmetric, and only one direction is load-bearing.
// v6 keeps SessionWithContext as an alias for Session (v6 session.go:81),
// so either spelling compiles there; v5's Session is the deprecated
// non-context session, a different interface entirely (v5 session.go:31),
// so v5 must be spelled SessionWithContext. A single inline spelling would
// therefore have to be the v5 one, and this field exists so the emission
// says what it means on each major rather than relying on that alias.
//
// The emitted package identifiers
// (neo4j, dbtype) are name-identical across the two majors, as is every
// other API name the templates reach for — BeginTransaction,
// ExplicitTransaction, SessionConfig and the ExecuteRead/ExecuteWrite
// pair among them — so the templates keep those inline.
// The uuidCarrier field is the first entry that is not emitted text at
// all but a TYPE-TABLE answer: dbtype.UUID landed in v6.2.0 and v5.28.4
// has no counterpart, so the two majors carry different sets of property
// widths. Empty means "this major has no carrier for UUID", which is the
// v5 answer and the zero value — so a typeMap built with no target still
// means what it always meant.
//
// Its non-empty value is codegen.UUIDCarrier and not "dbtype.UUID",
// which is the field holding a width's PUBLIC carrier rather than the
// driver's. ADR 0033 keeps the driver off the emitted public surface, so
// what typeMap.Property answers is the neutral name; driverCarrier maps
// that back to dbtype.UUID at the decode and encode sites. The field is
// a string rather than a bool for the reason the majors differ at all —
// a later major could carry the width under a different neutral name,
// and a bool would have to be re-widened to say so.
type driverTarget struct {
	// key is the driver wire key this target is enrolled under
	// (internal/cli/backends). It is not emitted into generated code:
	// it is what a refusal calls the target by, so an author reading
	// "carried only on neo4j-go-v6" reads back the string they would
	// write in their config.
	key string

	neo4jImport  string
	dbtypeImport string
	driverIface  string
	sessionIface string
	uuidCarrier  string
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
		uuidCarrier:  codegen.UUIDCarrier,
	}
)

// driverTargets is every major this backend emits for, which is the
// roster the driver-version refusal below is decided against. Listed
// rather than derived because DriverVersion is a closed enum of two and
// target() maps onto exactly these values.
var driverTargets = []driverTarget{driverV5, driverV6}

// types is the Go-type table for this major. Every phase and every walk
// that asks a width question asks it through here, so a width one major
// carries and the other does not is answered consistently or not at all.
func (t driverTarget) types() typeMap { return typeMap{uuidCarrier: t.uuidCarrier} }

// target maps a DriverVersion to its emission target. DriverV5 is the
// zero value, so an unconfigured Codegen emits v5 — every non-DriverV6
// value takes the v5 arm.
func (v DriverVersion) target() driverTarget {
	if v == DriverV6 {
		return driverV6
	}
	return driverV5
}
