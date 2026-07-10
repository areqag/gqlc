package cypher

import "errors"

// The category-grained sentinels Parse returns for valid openCypher that
// affects the type interface but the current model cannot faithfully
// represent (spec §3/B3), or for a bucket-1 parse-shape rejection the
// type-interface boundary owns. They are category-grained, not per-
// construct: when a later stage supports a construct we delete one Enter*
// handler, never rename a sentinel. Each is wrapped with text naming the
// offending construct at the failing site, so callers branch with
// errors.Is while reading a concrete message. A sentinel-reachability
// sweep (parser_test.go) guards the set. Stage 6 retired
// ErrUnsupportedProjection: rich scalar expressions at RETURN / WITH
// position now parse to an ExprProjection, so the sentinel has no
// fail-site. Stage 8 retired ErrUnsupportedPattern. Stage 11 added
// ErrPatternInProjection for pattern predicates at projection position.
// Stage 14 retires ErrUnsupportedClause entirely (its last fail-site was
// CALL, which is supported after Stage 14) and adds two new sentinels
// covering CALL: ErrUnknownProcedure (registry miss on procedure name
// OR YIELD result field) and ErrProcedureArity (explicit invocation
// with a statically-wrong argument count).
var (
	// ErrUnsupportedParameter rejects a parameter that cannot be bound to a
	// binding property or a clause slot: SKIP/LIMIT $n non-bare, bare-predicate
	// params that mine can't approve, and params nested in lists or maps that
	// escape the mining sweep. Stage 6 accepts a $p inside a rich projection or
	// WHERE expression as an ExprUse on the parameter, so this sentinel no
	// longer fires on those shapes.
	ErrUnsupportedParameter = errors.New("unsupported parameter")

	// ErrUnboundVariable rejects a variable that reaches the model — a return item,
	// a parameter use, or an edge endpoint — without a binding in any MATCH. It
	// does not cover a variable that appears only inside an ignored WHERE predicate
	// (e.g. b in WHERE a.x = b.y with no parameter): predicate structure is not
	// modelled (B1), so such a variable is never tracked.
	ErrUnboundVariable = errors.New("unbound variable")

	// ErrVariableKindConflict rejects a variable bound once as a node and once as
	// an edge (or as a path). Stage 8 extends the check to the three-way kind
	// space (node/edge/path). Stage 14 extends the check to CALL YIELD binding
	// collisions (intra-YIELD name reuse and cross-scope shadowing of an
	// imported name) — the operative semantic is "binding collision in scope,"
	// not strictly node/edge/path (Q4 ruling, matching the Stage-9 unwind-vs-
	// unwind precedent in build.go:114-121). The message text mirrors that
	// broader semantic; the sentinel identity (not the text) is the frozen
	// contract.
	ErrVariableKindConflict = errors.New("variable bound with conflicting kinds")

	// ErrPatternInProjection rejects a pattern predicate at RETURN or WITH
	// projection position — a bucket-1 parse-shape rule (ADR 0007 §I). Pattern
	// predicates are legal openCypher only inside a boolean position
	// (WHERE / EXISTS / rich-expression predicate); using one as a scalar
	// column (MATCH (n) RETURN (n)-[]->()) is SyntaxError:UnexpectedSyntax per
	// the TCK (Pattern1 [22]/[23]). Stage 11 adds the fail-site; the sentinel
	// is freeze-durable — pattern predicates never become legal projection
	// atoms.
	ErrPatternInProjection = errors.New("pattern predicate in projection position")

	// ErrNestedPropertyTarget rejects a SET or REMOVE whose propertyExpression
	// target is nested (n.a.b instead of n.a). The write model's Ref carries a
	// single Property, so a nested LHS has no honest single-Ref shape — accept-
	// and-truncate would claim SET target n.a when the query says n.a.b. Real
	// engines reject this at parse ("only directly attached properties can be
	// set"), so the fail-site aligns parser semantics with runtime semantics.
	// The pinned-tag TCK exercises zero such shapes (grep on
	// SET .*\.\w+\.\w+ / REMOVE .*\.\w+\.\w+ returned nothing), so the
	// rejection is a bucket-1 posture with zero corpus fallout. Fail-sites are
	// EnterOC_Set / EnterOC_Remove via propertyExpressionRef.
	ErrNestedPropertyTarget = errors.New("nested property target")

	// ErrUnknownProcedure rejects a CALL clause whose procedure name is not
	// declared in the procedure signature registry (procsig.Registry supplied
	// via cypher.WithRegistry). Stage 14 introduces the sentinel as CALL's
	// registry-miss category — one sentinel covers both sub-cases:
	//
	//   - Procedure-name miss: "unknown procedure: <name>". The registry has
	//     no signature under <name> (or the registry is empty).
	//   - Unknown YIELD result field: "unknown procedure result field: <field>
	//     on <procedure>". The signature is registered but does not declare
	//     the field the YIELD item references.
	//
	// The two sub-cases share the sentinel identity because both are
	// "registry miss" semantically; the wrapped message disambiguates. Codegen
	// callers that want to distinguish read the message text. Q1/Q4 ruling.
	ErrUnknownProcedure = errors.New("unknown procedure")

	// ErrProcedureArity rejects an explicit CALL invocation (parens present)
	// whose argument count does not match the signature's declared parameter
	// count. Wrapped message: "procedure arity mismatch: <name> expects
	// <expected> arguments, got <actual>". Fires only on explicit
	// invocations — implicit invocations (`CALL foo` without parens) bind
	// arguments from parameters at runtime, so their arity is uncountable at
	// parse time (Q4 ruling). Stage 14 introduces the sentinel as a bucket-1
	// rejection: the mismatch is statically provable from the registry, so
	// accepting-and-deferring would drop a fact the parser can honestly
	// detect. Call1 [7]-[10] exercise the sentinel across the standalone-vs-
	// in-query axis and the too-few / too-many arity axis.
	ErrProcedureArity = errors.New("procedure arity mismatch")
)
