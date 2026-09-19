package neo4j

import (
	"errors"
	"fmt"

	"github.com/areqag/gqlc/internal/codegen"
)

// nameBackend attributes a storage refusal to this backend.
//
// ErrUnstorableProperty ONLY, under a rule that governs both backends: a
// backend names itself exactly when another enrolled backend answers the
// same declaration differently. Attribution implicates contingency —
// naming one tells the author "this is this backend's answer, and another
// may differ" — so the name is owed where the targets disagree and
// misleads where they do not. Apache AGE stores the nested list this
// sentinel refuses, so it is owed here.
//
// Width refusals are returned unwrapped BY that rule rather than by an
// exception to it. Every width this table refuses is one no backend
// carries — the eight oversized numerics, permanent under spec §9 — so a
// suffix would send an author looking for a target that carries INT128.
// AGE's twin does attribute its width refusals and is right to: its
// refused set additionally holds BYTES, UUID and the zoned-element lists,
// which this table accepts. That premise is measured rather
// than assumed, and TestAContingentRefusalNamesItsBackend
// (internal/cli/backends) reddens the day this table refuses a width
// another target accepts — which is the day the question is re-opened
// (bd gqlc-fkdwq).
//
// The storage wording could not be reused for the width channel, which is
// what made this a question rather than a copy. ErrUnrepresentableWidth is
// raised by three sweeps — the entity sweep, the query-column sweep, and
// the query-parameter sweep — and this suffix is a claim about STORAGE. On
// the latter two it would be false: a projected INT128 column is not a
// stored property, and it is refused for want of a carrier. Attributing the
// storage sentinel alone is what keeps the sentence true wherever it can
// appear (ADR 0035).
func nameBackend(err error) error {
	if !errors.Is(err, codegen.ErrUnstorableProperty) {
		return err
	}
	return fmt.Errorf("%w, which the neo4j backend cannot store as a property", err)
}
