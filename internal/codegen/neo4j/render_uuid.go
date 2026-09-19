package neo4j

import (
	"fmt"
	"strings"

	"github.com/areqag/gqlc/internal/codegen"
)

// renderUUIDConversions emits uuid_neo4j.go: the unexported conversions
// between the UUID carrier uuid.go declares and the STRING a UUID is on
// this backend's wire (ADR 0047).
//
// No driver type appears here, on either major. The driver's own
// dbtype.UUID exists on v6 alone and is a Bolt 6.1 value its packer and
// hydrator both refuse on an older connection, so carrying the width on
// it would tie a schema's UUID property to a driver major and a server
// line. A string has neither tie: it is what Cypher's own randomUUID()
// returns, and what both majors pack unaided.
//
// A separate file from temporal_neo4j.go because the two are triggered
// independently: a batch declaring a UUID property and no temporal width
// emits this file and no temporal pair, and every temporal fixture in
// the corpus is the reverse.
//
// toUUID is the one conversion here that can fail, which is why decode
// sites reach it through narrowCall rather than narrowExpr: a property
// slot holds whatever string a writer put there, and one that is not a
// UUID fails the read the way an out-of-range integer does (ADR 0037).
// uuid.Parse also accepts the braced, URN and undashed spellings, so a
// value another writer stored in one of those reads back; fromUUID
// writes the canonical lowercase form alone.
//
// The list bodies are temporalListEncodeBody's, called with this
// carrier's name. They are already generic in the name — each one builds
// an []any and calls from<X> per element — and a []UUID needs the same
// per-element walk for its own reason: packed as it stands, each element
// is a byte array rather than the text the property holds.
func renderUUIDConversions(pkg string, uses map[string]carrierUse) []byte {
	name := codegen.UUIDCarrier

	var b strings.Builder
	b.WriteString(codegen.Header())
	b.WriteString("package ")
	b.WriteString(pkg)
	b.WriteString("\n")

	use := uses[name]
	if use.decode {
		b.WriteString("\nimport (\n\t\"fmt\"\n\t\"uuid\"\n)\n")
		fmt.Fprintf(&b, `
// to%[1]s parses the text a %[1]s property is stored as. A string that
// is not a UUID fails the read rather than arriving as a zero %[1]s.
func to%[1]s(v string) (%[1]s, error) {
	out, err := uuid.Parse(v)
	if err != nil {
		return %[1]s{}, fmt.Errorf("value %%q is not a UUID: %%w", v, err)
	}
	return out, nil
}
`, name)
	}
	if use.encode || use.encodePtr {
		fmt.Fprintf(&b, `
// from%[1]s renders a %[1]s as the RFC 9562 text it is stored as:
// lowercase, hyphenated, the one spelling every writer of this package
// produces.
func from%[1]s(v %[1]s) string {
	return v.String()
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
