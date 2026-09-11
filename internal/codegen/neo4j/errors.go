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
// DELIBERATELY NOT codegen.ErrUnrepresentableWidth, which is the
// sentinel every other refused width raises. That one says the width has
// no faithful Go carrier here, and the author's move on reading it is to
// change the declaration or change backend. Both are wrong answers to
// this refusal: the carrier exists — dbtype.UUID, v6.2.0 — and the move
// is to point the target at the newer major, a fix inside the same
// backend that the coarse sentinel cannot suggest and would send the
// author away from. A caller branching on the two gets two answers
// because there are two.
//
// It is this package's sentinel rather than a codegen one because the
// claim is about neo4j's driver majors, which the shared front end has
// no notion of; codegen's sentinel taxonomy (docs/specs/
// codegen-sentinel-taxonomy.md) is scoped to allSentinels and takes no
// view on a backend-local refusal. Published through Sentinels below, so
// an invalid fixture can name it.
var ErrUnrepresentableOnDriverVersion = errors.New("unrepresentable property width on this driver version")

// Sentinels publishes this backend's user-reachable refusals under the
// spelling a conformance manifest's expectedError carries,
// "<package>.<ExportedVar>".
//
// One entry, and the three Err* names in render_db.go are not it: those
// are TEMPLATE TEXT inside string literals, declared in the package this
// backend GENERATES and never returned by this one. Until the entry
// below existed this map did not, and the registry comment in
// internal/cli/backends said so.
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
// oversized numerics, permanent under spec §9 — so a suffix would send
// an author looking for a target that carries INT128. AGE's twin does
// attribute its width refusals and is right to: its refused set
// additionally holds BYTES, UUID and the zoned-element lists, which this
// table accepts on at least one major. That premise is measured rather
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
// driver major would have carried, under this package's own sentinel.
// (nil, false) leaves the error to nameBackend above.
//
// It reads the refusal rather than re-walking the batch, which is why it
// covers all four of the front end's raise sites — the entity sweep, the
// query-column sweep, the query-parameter sweep and the list-element
// chain — with no walk of its own and no claim about which positions a
// width can reach. codegen.RefusedWidth exists for exactly this caller:
// a backend deciding at emit time what to say about a width the shared
// phases refused on its behalf.
//
// ok=false from RefusedWidth keeps the front end's error, which is the
// conservative direction: the coarse sentinel is true of every width no
// major carries, and merely incomplete about one a newer major does.
//
// The message inherits the front end's position text with the sentinel
// prefix removed, so the author still reads which entity, column or
// parameter is at fault. strings.TrimPrefix is total — a message this
// package did not predict passes through unchanged and reads redundantly
// rather than wrongly — which is why this is not a parse of a text this
// package has no contract with.
func driverVersionRefusal(err error, target driverTarget) (error, bool) {
	width, ok := codegen.RefusedWidth(err)
	if !ok {
		return nil, false
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
			ErrUnrepresentableOnDriverVersion, site, other.key), true
	}
	return nil, false
}

// refuse is the one place a refusal leaves this backend, so the two
// rules above are asked in one order rather than at each call site.
func refuse(err error, target driverTarget) error {
	if refusal, ok := driverVersionRefusal(err, target); ok {
		return refusal
	}
	return nameBackend(err)
}
