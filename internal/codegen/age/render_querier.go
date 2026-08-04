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
	switch {
	case len(prepared) == 0:
	case slices.ContainsFunc(prepared, namesInstant):
		// An interface entry repeats the signature its method declares,
		// so a signature spelling the instant puts "time" in this file
		// as well as in the one the method lands in.
		b.WriteString("import (\n\t\"context\"\n\t\"time\"\n)\n\n")
	default:
		b.WriteString("import \"context\"\n\n")
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
