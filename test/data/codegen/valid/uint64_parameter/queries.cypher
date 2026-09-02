// A UINT64 parameter is the one width whose values the wire cannot all
// hold: an agtype integer scalar and the Bolt packer's integer are both
// signed 64-bit, so [MaxInt64+1, MaxUint64] has no representation on
// either. What the two backends do about that differs, and both answers
// are pinned here rather than in one place, because each is invisible in
// the other's goldens.
//
// NEO4J BINDS IT BARE. The driver already refuses it — packX and packV
// route reflect.Uint64 to packer.Uint64, whose checkOverflowInt raises an
// OverflowError above MaxInt64, and packer.End feeds that to onPackErr,
// which fails the send. gqlc used to emit int64(arg) here, which reached
// that check as an already-wrapped negative int64 and so disarmed it: the
// generated code was the reason the guard could not fire. The goldens for
// these four parameters must therefore read `arg.Hits`, never
// `int64(arg.Hits)`, and a widen reappearing is the defect returning
// (bd gqlc-tzjqu).
//
// AGE REFUSES IT AT BIND. Nothing downstream would: the args map goes
// through json.Marshal, which writes a uint64 exactly and errors on
// nothing, so the value would reach a server that cannot store it. The
// emission binds each of these to a local through agtypeUnsigned and
// returns a wrapped error naming the parameter.
//
// Four parameters for the four shapes the encode composition takes, since
// nullability and nesting are carried by combinators wrapped around the
// leaf encoder rather than by a variant of it: non-nullable (hits),
// nullable (misses), list (runs), and nullable list (spans) — the last
// being agtypeEncodedNullable over agtypeEncodedList, the only one whose
// inner signature has to be spelled out and so the only one a wrong
// encodedParamText entry would break. These goldens are a package of the
// test/data/codegen module, which `just test-codegen-fence` builds and
// vets, so a composition that renders but does not compile is caught here
// and nowhere else.

// name: CountersMatching :many
MATCH (c:Counter)
WHERE c.hits = $hits AND c.misses = $misses AND c.runs = $runs AND c.spans = $spans
RETURN c.id AS id
