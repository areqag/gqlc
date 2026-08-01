package age

import (
	"errors"
	"fmt"
	"strings"

	"github.com/areqag/gqlc/internal/codegen"
)

// ErrUnsupportedQuery is returned when a batch carries a query this
// backend has no emission for. Package-level so callers branch with
// errors.Is; the fail-site wraps it with the offending names — the
// schema/gql convention.
var ErrUnsupportedQuery = errors.New("unsupported query")

// rejectUnservedQueries fails a batch whose queries this backend cannot
// emit methods for. Emitting anyway would hand the author a Querier that
// silently omits queries they wrote — the generated package would be
// wrong about its own input, and nothing downstream could tell.
//
// The predicate is the backend's capability, not the stage: C0 serves no
// query at all, so it rejects every one. A stage that serves some
// narrows the predicate here.
func rejectUnservedQueries(queries []codegen.Query) error {
	if len(queries) == 0 {
		return nil
	}
	names := make([]string, len(queries))
	for i, q := range queries {
		names[i] = q.MethodName
	}
	noun := "queries"
	if len(queries) == 1 {
		noun = "query"
	}
	return fmt.Errorf("%w: the Apache AGE backend emits no query methods yet, so %d %s would be dropped: %s",
		ErrUnsupportedQuery, len(queries), noun, strings.Join(names, ", "))
}
