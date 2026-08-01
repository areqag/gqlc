package age

import (
	"github.com/areqag/gqlc/internal/codegen"
)

// renderModels emits models.go (spec §5.2). C0 emits no entity structs:
// the decode helpers they exist for arrive with the read path.
func renderModels(pkg string) []byte {
	return []byte(codegen.Header() + `package ` + pkg + `
`)
}
