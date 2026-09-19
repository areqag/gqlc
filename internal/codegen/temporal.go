package codegen

import (
	"go/ast"
	"go/parser"

	"github.com/areqag/gqlc/internal/graph"
)

// TemporalCarriers is the set of exported names temporal.go declares
// (ADR 0033) — the gqlc-owned neutral carriers for the four temporal
// widths whose driver representations differ across backends. TIMESTAMP
// is absent by design: it stays time.Time, which is already neutral.
//
// The slice is the emission order of the declarations and the order the
// reserved-set rows are read in; it is not sorted alphabetically.
var TemporalCarriers = []string{"Date", "LocalTime", "Time", "LocalDateTime", "Duration"}

// temporalCarrierSet is TemporalCarriers as a lookup, built here so the
// two cannot drift.
var temporalCarrierSet = func() map[string]struct{} {
	set := make(map[string]struct{}, len(TemporalCarriers))
	for _, name := range TemporalCarriers {
		set[name] = struct{}{}
	}
	return set
}()

// RenderTemporal emits temporal.go: the five neutral temporal carriers,
// byte-identical across every backend (ADR 0033 "Placement"). The
// declarations are flat component structs, so == is value equality and
// the zero value is inspectable — the property a time.Time-backed
// newtype cannot have, because it carries a clock and a Location the
// width does not.
func RenderTemporal(pkg string) []byte {
	return []byte(Header() + `package ` + pkg + `

// Date is a calendar date: the three components a DATE carries, and
// nothing else — no clock reading, no zone.
type Date struct {
	Year, Month, Day int
}

// LocalTime is a clock reading with no date and no zone.
type LocalTime struct {
	Hour, Minute, Second, Nanosecond int
}

// Time is a clock reading together with the UTC offset it was read at.
// OffsetSeconds is east-positive, matching both the bolt wire encoding
// and time.Time.Zone.
type Time struct {
	Hour, Minute, Second, Nanosecond int
	OffsetSeconds                    int
}

// LocalDateTime is a date and a clock reading with no zone — a wall
// time, not an instant.
type LocalDateTime struct {
	Year, Month, Day                 int
	Hour, Minute, Second, Nanosecond int
}

// Duration is a DURATION: months, days, seconds and nanoseconds held
// apart. A month has no fixed length in seconds, so flattening the
// components onto one count would invent a length the value does not
// carry.
type Duration struct {
	Months, Days, Seconds int64
	Nanos                 int
}
`)
}

// ReferencesTemporalCarrier reports whether the emitted package names any
// carrier — the emission trigger for temporal.go (ADR 0033: emitted only
// when the generated package references at least one). Two places can
// name one: the public surface, and the member arms of a closed union's
// helper pair (see unionMembersNameCarrier). Every Go type text at either is
// parsed and walked for an identifier equal to a carrier name.
//
// Parsed rather than substring-matched: "Date" is a substring of
// "LocalDateTime" and of user-derived entity names, and a nested type
// text (map[string]Date, [][]Date) hides its leaf from a prefix strip.
// A user identifier that equals a carrier name cannot reach here — the
// names are in reservedIdentifiers, so Phase A already refused the
// batch with ErrIdentifierCollision.
//
// carrier is the member carrier the backend's own union emission hands
// UnionMembers, threaded in for the reason it is threaded in there.
func ReferencesTemporalCarrier(p Prepared, carrier func(graph.PropertyType) (string, bool)) bool {
	return referencesCarrier(p, carrier, temporalCarrierSet)
}

// referencesCarrier is the walk both emission triggers ask, over the set
// its caller owns. Shared rather than written twice because the walk is
// a claim about WHERE the public surface is — entity fields, query
// parameters, row fields and every nested list element — and a second
// copy would be a second answer to that question, free to drift from
// this one the next time a position is added to the prepared surface.
//
// The surface is not the whole of it: unionMembersNameCarrier reads the
// one place a carrier is named that no surface text holds.
func referencesCarrier(p Prepared, carrier func(graph.PropertyType) (string, bool), set map[string]struct{}) bool {
	for _, e := range p.Entities {
		for _, f := range e.Fields {
			if typeTextNamesCarrier(f.GoType, set) {
				return true
			}
		}
	}
	for _, q := range p.Queries {
		if queryNamesCarrier(q, set) {
			return true
		}
	}
	return unionMembersNameCarrier(p, carrier, set)
}

// unionMembersNameCarrier is referencesCarrier's reading of the closed
// unions the batch reaches. A closed union carries as `any` at every
// position, its own and a record field's alike, so a carrier named only
// by a union MEMBER is on no surface text — while the union's emitted
// helper pair spells each member's carrier in a type-switch arm and
// calls its conversion by name. Read off the surface alone, a batch
// whose one DATE sat inside ANY<DATE | INT64> emitted a package that
// named Date and declared none (bd gqlc-o8p3).
//
// The members are read from the two functions the helper emission
// itself is built on: UnionEncodings is the set a helper pair is emitted
// for, already closed over list elements, record fields and union
// members, and UnionMembers is the text each arm is spelled from. A
// member's text names what is inside it — a record member its fields, a
// list member its element — and a union nested below one is an entry of
// UnionEncodings in its own right, so no descent is written here.
//
// A union one of whose members carrier refuses fails preparation and
// cannot reach here. It answers true, on the ground typeTextNamesCarrier
// gives for a text that does not parse.
func unionMembersNameCarrier(p Prepared, carrier func(graph.PropertyType) (string, bool), set map[string]struct{}) bool {
	for _, pt := range UnionEncodings(p.Entities, p.Queries) {
		members, ok := UnionMembers(pt, carrier)
		if !ok {
			return true
		}
		for _, m := range members {
			if typeTextNamesCarrier(m.GoType, set) {
				return true
			}
		}
	}
	return false
}

// queryNamesCarrier is referencesCarrier's walk over one query's
// positions: parameters, row fields and every nested list element.
func queryNamesCarrier(q Query, set map[string]struct{}) bool {
	for _, param := range q.ParamFields {
		if typeTextNamesCarrier(param.GoType, set) {
			return true
		}
	}
	for _, row := range q.RowFields {
		if typeTextNamesCarrier(row.GoType, set) {
			return true
		}
	}
	for _, row := range q.RowFields {
		for elem := row.ListElem; elem != nil; elem = elem.Nested {
			if typeTextNamesCarrier(elem.GoType, set) {
				return true
			}
		}
	}
	return false
}

// typeTextNamesCarrier reports whether one emitted Go type text names a
// carrier in set. A qualified type is not descended into: its Sel is
// another package's identifier, and three of the carrier names collide
// there — time.Time is the TIMESTAMP carrier, dbtype.Date is what a neo4j
// conversion still names internally, and uuid.UUID is the standard
// library type the UUID carrier aliases, so a walk that read Sel would
// report every batch as carrier-bearing.
//
// A text go/parser rejects is an emitter bug, and this answers true for
// it: emitting a carrier file nothing references still compiles, while
// omitting one something references does not, so the unparseable case
// takes the side that cannot break the generated package.
func typeTextNamesCarrier(text string, set map[string]struct{}) bool {
	expr, err := parser.ParseExpr(text)
	if err != nil {
		return true
	}
	return exprNamesCarrier(expr, set)
}

// exprNamesCarrier is typeTextNamesCarrier's walk, named so the *ast.Field
// arm can re-enter it for one field's type alone.
//
// A field's Names are declarations, never type references, so walking them
// reads an identifier that names nothing. The arm keys on *ast.Field rather
// than on *ast.StructType because ast.Inspect reaches that node kind in
// three places — StructType.Fields, InterfaceType.Methods and
// FuncType.Params/Results — and the property is the same at all three.
func exprNamesCarrier(expr ast.Expr, set map[string]struct{}) bool {
	found := false
	ast.Inspect(expr, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.SelectorExpr:
			return false
		case *ast.Field:
			if node.Type != nil && exprNamesCarrier(node.Type, set) {
				found = true
			}
			return false
		case *ast.Ident:
			if _, isCarrier := set[node.Name]; isCarrier {
				found = true
			}
		}
		return !found
	})
	return found
}
