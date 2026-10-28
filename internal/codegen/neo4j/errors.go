package neo4j

import (
	"errors"
	"fmt"
	"strings"

	"github.com/areqag/gqlc/internal/codegen"
)

// ErrUnrepresentableOnDriverVersion is returned when a batch declares a
// property width this backend carries, but not on the driver major this
// target names. Package-level so callers branch with errors.Is; the
// fail-site wraps it with the position and the major that does carry it.
//
// Today that is VECTOR alone: dbtype.Vector is a neo4j-go-driver v6 type
// and v5 has no counterpart, so neo4j-go-v5 refuses the width and
// neo4j-go-v6 carries it (ADR 0050). UUID raised this once too, until ADR
// 0047 moved it to a string wire both majors carry.
//
// DELIBERATELY NOT codegen.ErrUnrepresentableWidth, which is the sentinel
// every other refused width raises. That one says the width has no
// faithful Go carrier here, and the author's move on reading it is to
// change the declaration or change backend. Both are wrong answers to this
// refusal: the carrier exists, and the move is to point the target at the
// newer major, a fix inside the same backend that the coarse sentinel
// cannot suggest. A caller branching on the two gets two answers because
// there are two.
//
// It is this package's sentinel rather than a codegen one because the
// claim is about neo4j's driver majors, which the shared front end has no
// notion of; codegen's sentinel taxonomy is scoped to allSentinels and
// takes no view on a backend-local refusal. Published through Sentinels
// below, so an invalid fixture can name it.
var ErrUnrepresentableOnDriverVersion = errors.New("unrepresentable property width on this driver version")

// Sentinels publishes this backend's user-reachable refusals under the
// spelling a conformance manifest's expectedError carries,
// "<package>.<ExportedVar>".
//
// One entry, and the three Err* names in render_db.go are not it: those are
// TEMPLATE TEXT inside string literals, declared in the package this
// backend GENERATES and never returned by this one.
//
// A name published here is one the corpus is required to witness with an
// invalid fixture, so this is a promise and not an inventory.
func Sentinels() map[string]error {
	return map[string]error{
		"neo4j.ErrUnrepresentableOnDriverVersion": ErrUnrepresentableOnDriverVersion,
	}
}

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
// Width refusals that survive driverVersionRefusal below are returned
// unwrapped BY that rule rather than by an exception to it. Every width
// that reaches this point is one NO enrolled target carries — the eight
// oversized numerics, permanent under spec §9, and a vector inside a
// container (ADR 0050) — so a suffix would send an author looking for a
// target that carries INT128. AGE's twin does attribute its width refusals
// and is right to: its refused set additionally holds BYTES, VECTOR and the
// zoned-element lists, which this table accepts on at least one major. That premise is measured rather
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

// driverVersionRefusal re-raises a width refusal that another enrolled
// driver major would have carried, under this package's own sentinel. nil
// leaves the error to nameBackend above.
//
// It reads the refusal rather than re-walking the batch, which is why it
// covers every front-end raise site — the entity sweep, the query-column
// sweep, the query-parameter sweep and the list-element chain — with no
// walk of its own. codegen.RefusedWidth exists for exactly this caller.
// ok=false from it keeps the front end's error, which is the conservative
// direction: the coarse sentinel is true of every width no major carries,
// and merely incomplete about one a newer major does.
//
// The message inherits the front end's position text with the sentinel
// prefix removed, so the author still reads which entity, column or
// parameter is at fault. strings.TrimPrefix is total — a message this
// package did not predict passes through unchanged and reads redundantly
// rather than wrongly.
func driverVersionRefusal(err error, target driverTarget) error {
	width, ok := codegen.RefusedWidth(err)
	if !ok {
		return nil
	}
	for _, other := range driverTargets {
		if other.key == target.key {
			continue
		}
		if _, carried := other.types().Property(width); !carried {
			continue
		}
		site := strings.TrimPrefix(err.Error(), codegen.ErrUnrepresentableWidth.Error()+": ")
		return fmt.Errorf("%w: %s, which the neo4j backend carries only on the %s target",
			ErrUnrepresentableOnDriverVersion, site, other.key)
	}
	return nil
}

// refuse is the one place a refusal leaves this backend, so the two rules
// above are asked in one order rather than at each call site.
func refuse(err error, target driverTarget) error {
	if refusal := driverVersionRefusal(err, target); refusal != nil {
		return refusal
	}
	return nameBackend(err)
}
