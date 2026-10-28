package gen

import (
	"fmt"
	"strings"

	"github.com/areqag/gqlc/internal/fuzzkit"
)

// QueryFile writes 1–4 annotated queries over v, each under a
// `// name: Ident :one|:many|:exec|:iter` line, with Query's text as its body.
//
// The names are distinct exported identifiers and :many is the first choice,
// so an exhausted stream yields a file the queryfile front end admits. The
// controlled mutations, each at a low rate: a lower-case name
// (queryfile.ErrInvalidQueryName), a name used twice
// (queryfile.ErrDuplicateQueryName) and an unknown cardinality token
// (queryfile.ErrUnknownCardinality).
func QueryFile(s *fuzzkit.Stream, v Vocab) string {
	var b strings.Builder
	n := 1 + s.Int(4)
	for i := range n {
		name := fmt.Sprintf("Q%d", i)
		switch {
		case rare(s, 40):
			name = fmt.Sprintf("q%d", i)
		case i > 0 && rare(s, 40):
			name = "Q0"
		}
		card := s.Pick([]string{"many", "one", "exec", "iter"})
		if rare(s, 40) {
			card = "all"
		}
		if i > 0 {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "// name: %s :%s\n%s\n", name, card, Query(s, v))
	}
	return b.String()
}
