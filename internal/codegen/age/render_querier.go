package age

import (
	"slices"
	"strings"

	"github.com/areqag/gqlc/internal/codegen"
)

// renderQuerier emits querier.go (spec §5.4). ReadQuerier lists every
// method whose codegen.Query.IsWrite is false in Input.Queries order;
// WriteQuerier lists every IsWrite==true method in the same filtered
// order. A method belongs to exactly one interface — the partition is on
// Statement, not on Cardinality. The compile-time assertion on the last
// line catches method-name drift.
func renderQuerier(pkg string, prepared []codegen.Query) []byte {
	var b strings.Builder
	b.WriteString(codegen.Header())
	b.WriteString("package " + pkg + "\n\n")
	// An interface entry repeats the signature its method declares, so a
	// signature spelling the instant or the sequence puts that import in
	// this file as well as in the one the method lands in. Built as a list
	// rather than as one case per combination: the two conditions are
	// independent, so enumerating them would need a case per subset and a
	// third carrier would need eight.
	if len(prepared) > 0 {
		extra := ""
		if slices.ContainsFunc(prepared, namesIter) {
			extra += "\t\"iter\"\n"
		}
		if slices.ContainsFunc(prepared, namesInstant) {
			extra += "\t\"time\"\n"
		}
		if extra == "" {
			b.WriteString("import \"context\"\n\n")
		} else {
			b.WriteString("import (\n\t\"context\"\n" + extra + ")\n\n")
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
	b.WriteString("var _ Querier = (*Tx)(nil)\n")
	return []byte(b.String())
}
