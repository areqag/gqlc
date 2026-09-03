package codegen

import (
	"go/ast"
	"go/parser"
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

// ReferencesTemporalCarrier reports whether the prepared batch's public
// surface names any carrier — the emission trigger for temporal.go
// (ADR 0033: emitted only when the generated surface references at
// least one). Every Go type text reaching an exported position is
// parsed and walked for an identifier equal to a carrier name.
//
// Parsed rather than substring-matched: "Date" is a substring of
// "LocalDateTime" and of user-derived entity names, and a nested type
// text (map[string]Date, [][]Date) hides its leaf from a prefix strip.
// A user identifier that equals a carrier name cannot reach here — the
// names are in reservedIdentifiers, so Phase A already refused the
// batch with ErrIdentifierCollision.
func ReferencesTemporalCarrier(p Prepared) bool {
	for _, e := range p.Entities {
		for _, f := range e.Fields {
			if typeTextNamesCarrier(f.GoType) {
				return true
			}
		}
	}
	for _, q := range p.Queries {
		for _, param := range q.ParamFields {
			if typeTextNamesCarrier(param.GoType) {
				return true
			}
		}
		for _, row := range q.RowFields {
			if typeTextNamesCarrier(row.GoType) {
				return true
			}
		}
		for _, row := range q.RowFields {
			for elem := row.ListElem; elem != nil; elem = elem.Nested {
				if typeTextNamesCarrier(elem.GoType) {
					return true
				}
			}
		}
	}
	return false
}

// typeTextNamesCarrier reports whether one emitted Go type text names a
// carrier. A qualified type is not descended into: its Sel is another
// package's identifier, and two of the carrier names collide there —
// time.Time is the TIMESTAMP carrier and dbtype.Date is what a neo4j
// conversion still names internally, so a walk that read Sel would
// report every batch as carrier-bearing.
//
// A text go/parser rejects is an emitter bug, and this answers true for
// it: emitting a temporal.go nothing references still compiles, while
// omitting one something references does not, so the unparseable case
// takes the side that cannot break the generated package.
func typeTextNamesCarrier(text string) bool {
	expr, err := parser.ParseExpr(text)
	if err != nil {
		return true
	}
	return exprNamesCarrier(expr)
}

// exprNamesCarrier is typeTextNamesCarrier's walk, named so the *ast.Field
// arm can re-enter it for one field's type alone.
//
// A field's Names are declarations, never type references, so walking them
// reads an identifier that names nothing. The arm keys on *ast.Field rather
// than on *ast.StructType because ast.Inspect reaches that node kind in
// three places — StructType.Fields, InterfaceType.Methods and
// FuncType.Params/Results — and the property is the same at all three.
func exprNamesCarrier(expr ast.Expr) bool {
	found := false
	ast.Inspect(expr, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.SelectorExpr:
			return false
		case *ast.Field:
			if node.Type != nil && exprNamesCarrier(node.Type) {
				found = true
			}
			return false
		case *ast.Ident:
			if _, isCarrier := temporalCarrierSet[node.Name]; isCarrier {
				found = true
			}
		}
		return !found
	})
	return found
}
