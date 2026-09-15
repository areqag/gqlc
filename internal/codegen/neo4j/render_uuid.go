package neo4j

import (
	"fmt"
	"strings"

	"github.com/areqag/gqlc/internal/codegen"
)

// renderUUIDConversions emits uuid_neo4j.go: the unexported bridge
// between the neutral UUID carrier uuid.go declares and the dbtype.UUID
// this driver reads and writes. Entirely unexported, so the
// driver-freedom sweep over the emitted public surface does not see it —
// this file is where the driver is allowed to appear, the same division
// temporal.go and temporal_neo4j.go stand on (ADR 0033).
//
// A separate file from temporal_neo4j.go rather than a sixth carrier
// inside it, because the two are triggered independently: a batch
// declaring a UUID property and no temporal width emits this file and no
// temporal pair, and every temporal fixture in the corpus is the
// reverse. Folding them together would emit both for either, which is
// the unreferenced-declaration cost ADR 0033's placement argument
// weighed and declined.
//
// Every body here is a Go conversion, which the five temporal bodies
// are not: dbtype.UUID is `type UUID [16]byte` (v6.2.0
// neo4j/dbtype/uuid.go) and codegen.RenderUUID emits the same underlying
// type, so neither direction takes the value apart. isNeutralCarrier
// records why the pair is emitted at all rather than converted inline at
// each site — a list parameter cannot be, since the driver packs an
// array by type-switching on dbtype.UUID itself.
//
// The list bodies are temporalListEncodeBody's, called with this
// carrier's name. They are already generic in the name — each one builds
// an []any and calls from<X> per element — and the reason the element
// conversion is needed is the same reason on both families: the driver
// marshals no gqlc type, so each element converts before the list
// reaches the wire.
//
// Reached on the v6 target alone. On v5 the width has no carrier, so
// Prepare refuses the batch and no emission follows.
func renderUUIDConversions(pkg string, uses map[string]carrierUse, target driverTarget) []byte {
	name := codegen.UUIDCarrier

	var b strings.Builder
	b.WriteString(codegen.Header())
	b.WriteString("package ")
	b.WriteString(pkg)
	b.WriteString("\n\n")
	b.WriteString("import (\n")
	b.WriteString("\t\"" + target.dbtypeImport + "\"\n")
	b.WriteString(")\n")

	use := uses[name]
	if use.decode {
		fmt.Fprintf(&b, `
// to%[1]s narrows the driver's UUID onto the neutral carrier. Both are
// [16]byte holding the RFC 9562 value in wire order, so the conversion
// is the whole of it — nothing is reinterpreted and nothing can fail.
func to%[1]s(v dbtype.%[1]s) %[1]s {
	return %[1]s(v)
}
`, name)
	}
	if use.encode || use.encodePtr {
		fmt.Fprintf(&b, `
// from%[1]s widens the neutral carrier back to the driver's UUID, which
// is what the packer's array arm type-switches on.
func from%[1]s(v %[1]s) dbtype.%[1]s {
	return dbtype.%[1]s(v)
}
`, name)
	}
	if use.encodePtr {
		fmt.Fprintf(&b, `
// from%[1]sPtr binds a nullable %[1]s parameter: a nil pointer is the
// Cypher null the schema's nullability declared, not a zero %[1]s.
func from%[1]sPtr(v *%[1]s) any {
	if v == nil {
		return nil
	}
	return from%[1]s(*v)
}
`, name)
	}
	if use.list {
		b.WriteString("\n")
		b.WriteString(temporalListEncodeBody(name, false))
		if use.listPtr {
			b.WriteString("\n")
			b.WriteString(temporalListEncodePtrBody(name, false))
		}
	}
	if use.listElem {
		b.WriteString("\n")
		b.WriteString(temporalListEncodeBody(name, true))
		if use.listElemPtr {
			b.WriteString("\n")
			b.WriteString(temporalListEncodePtrBody(name, true))
		}
	}
	return []byte(b.String())
}
