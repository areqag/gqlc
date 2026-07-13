package codegen

import (
	"strings"
)

// renderQuerier emits querier.go (spec §5.4). ReadQuerier lists every
// method whose preparedQuery.IsWrite is false in Input.Queries order;
// WriteQuerier lists every IsWrite==true method in the same filtered
// order. A method belongs to exactly one interface — the
// partition is on Statement, not on Cardinality (a :one write-with-
// projection lands in WriteQuerier; a :exec on a call-with-no-yield
// lands in ReadQuerier). The compile-time assertion on the last line
// catches method-name drift.
func renderQuerier(pkg string, prepared []preparedQuery, target driverTarget) []byte {
	var b strings.Builder
	b.WriteString(header())
	b.WriteString("package ")
	b.WriteString(pkg)
	b.WriteString("\n\n")
	// Import set: context always (for method signatures); dbtype iff a
	// method signature names a dbtype.<Kind>; time iff a signature names
	// time.Time. The signature-search runs over Params and Row types.
	needDbtype, needTime := querierImports(prepared)
	if len(prepared) > 0 {
		if needDbtype || needTime {
			b.WriteString("import (\n\t\"context\"\n")
			if needTime {
				b.WriteString("\t\"time\"\n")
			}
			if needDbtype {
				b.WriteString("\n\t\"" + target.dbtypeImport + "\"\n")
			}
			b.WriteString(")\n\n")
		} else {
			b.WriteString("import \"context\"\n\n")
		}
	}
	b.WriteString("type ReadQuerier interface {\n")
	for _, p := range prepared {
		if p.IsWrite {
			continue
		}
		b.WriteString("\t")
		writeMethodSignature(&b, p)
		b.WriteString("\n")
	}
	b.WriteString("}\n\n")
	b.WriteString("type WriteQuerier interface {\n")
	for _, p := range prepared {
		if !p.IsWrite {
			continue
		}
		b.WriteString("\t")
		writeMethodSignature(&b, p)
		b.WriteString("\n")
	}
	b.WriteString("}\n\n")
	b.WriteString("type Querier interface {\n\tReadQuerier\n\tWriteQuerier\n}\n\n")
	b.WriteString("var _ Querier = (*Queries)(nil)\n")
	return []byte(b.String())
}

// querierImports scans every prepared query's method signature (params
// + return type) for dbtype / time references. Multi-column returns
// use the MethodNameRow struct name (the struct itself lives in the
// .cypher.go file, whose imports carry any dbtype / time it needs);
// single-column returns and every parameter surface the carrier
// directly in the signature. The querier interface file needs an
// import when — and only when — its method signature strings contain
// the carrier.
func querierImports(prepared []preparedQuery) (needDbtype, needTime bool) {
	scan := func(ty string) {
		if strings.Contains(ty, "dbtype.") {
			needDbtype = true
		}
		if strings.Contains(ty, "time.Time") {
			needTime = true
		}
	}
	for _, p := range prepared {
		// Parameters appear verbatim in every method signature.
		for _, param := range p.ParamFields {
			scan(param.GoType)
		}
		// Return type: bare row Go type for single-column projections;
		// MethodNameRow (no import needed) otherwise.
		if len(p.RowFields) == 1 {
			scan(p.RowFields[0].GoType)
		}
	}
	return needDbtype, needTime
}
