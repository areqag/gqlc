// gqlc-dvd1 / ruling-4w5 §6 row L1. A RENAMED entity carry. Master refused this
// at parse with `unbound variable: b`; post-swap it is admitted with $p
// `unknown`. The `unknown` is a PRE-EXISTING and ORTHOGONAL gap, not something
// the attribution move introduced (ruling §5, the renamed-carry bullet):
// `MATCH (a:Post) WITH a AS z RETURN z.id` already fails on master with
// ErrOutOfR0Scope, and `WITH a AS z WITH z WHERE z.id = $p` already commits
// `unknown` there. The move routes this WHERE's parameter into a gap the
// projection path already had; it does not create it.
MATCH (a:Post) WITH a AS b WHERE b.title = $p RETURN b
