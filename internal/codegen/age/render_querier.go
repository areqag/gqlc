package age

import (
	"github.com/areqag/gqlc/internal/codegen"
)

// renderQuerier emits querier.go (spec §5.4). C0 emits no query methods,
// so both partitions are empty; the compile-time assertion on the last
// line is what catches method-name drift once C1 fills them.
func renderQuerier(pkg string) []byte {
	return []byte(codegen.Header() + `package ` + pkg + `

type ReadQuerier interface {
}

type WriteQuerier interface {
}

type Querier interface {
	ReadQuerier
	WriteQuerier
}

var _ Querier = (*Queries)(nil)
`)
}
