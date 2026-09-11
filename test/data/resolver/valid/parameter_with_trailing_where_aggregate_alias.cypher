// gqlc-dvd1 / ruling-4w5 §6 row L2 — the headline repair. Master REFUSED this
// legal query at parse with `unbound variable: c`, because the WHERE was mined
// into the Part the WITH CLOSED and pairAddSub's appendRef put `c` into that
// Part's referential-integrity set, where the aggregate alias is not bound. The
// same query with a literal (`WHERE c > 1`) always parsed, which is why it read
// as a parameter bug. $p is `unknown`: Part 1 carries `c` as a carry-only lane
// that partScope does not expose (ruling §5.1).
MATCH (a:Post) WITH count(a) AS c WHERE c = $p RETURN c
