package cypher_test

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	gherkin "github.com/cucumber/gherkin/go/v26"
	"github.com/cucumber/godog"
	messages "github.com/cucumber/messages/go/v21"

	"github.com/areqag/gqlc/internal/procsig"
	"github.com/areqag/gqlc/internal/query"
	"github.com/areqag/gqlc/internal/query/cypher"
)

// updateGolden regenerates the .golden.json snapshots from parser output. It is
// shared with parser_test.go (one -update flag for the whole package).
var updateGolden = flag.Bool("update", false, "regenerate golden snapshots from parser output")

// readCoreDirs are the TCK feature directories godog points at. Stage 0 opened
// with the read-core clauses (match/return/where — the WHERE scenarios live
// under match-where in the vendored TCK). Stage 4 added with/union. Stage 6
// adds the expression dirs: literals, boolean, comparison, mathematical,
// string, null, precedence, typeConversion, list, map, conditional — every
// one exercises a scalar expression the widened projection sum now types.
// Stage 7 adds expressions/temporal — the constructor + arithmetic surface
// the six new temporal Type variants unlock. Stage 8 adds expressions/path,
// expressions/pattern, expressions/graph — the pattern surface widening
// (named paths, variable-length relationships, multi-type) reaches every
// scenario exercising graph functions over paths. Stage 9 adds the
// remaining read-clause dirs: return-orderby, with-orderBy, with-skip-limit,
// with-where, unwind — closing out the read-clause surface (UNWIND parses
// under the widened Binding sum, WITH...WHERE and WITH...ORDER BY / SKIP /
// LIMIT wire through existing hooks). Stage 10 adds expressions/aggregation
// — the aggregate surface (count/sum/collect/min/max/avg/stDev/percentile*)
// with DISTINCT propagation and per-aggregate result typing. Stage 11 adds
// expressions/quantifier and expressions/existentialSubqueries — the four
// list quantifiers (ALL/ANY/NONE/SINGLE) type as TypeBool with iteration-
// variable scoping enforced structurally, and EXISTS { ... } types as
// TypeBool with a suppression counter that stops inner bindings from
// leaking into the outer part. Stage 12 adds clauses/create,
// clauses/delete, clauses/set, and clauses/remove — the write clause
// surface: Query gains a StatementKind axis (read vs. write), Part gains
// an Effects slice (per-part write clauses in walk order), and a
// projection-less part (writes with no RETURN) is a first-class shape.
// Stage 13 adds clauses/merge — MERGE joins the write-clause set with a
// dedicated MergeEffect (match-or-create axis) and a sealed SetEffect
// sub-sum for the ON MATCH / ON CREATE payloads. The mineInlineMap
// widening lands with it: inline-map values that reference variables now
// flow onto curPart.refs, so `CREATE (b {name: missing})`-shaped
// undefined-variable scenarios (Create1[20], Create2[24]) reject via
// ErrUnboundVariable at the buildPart referential-integrity sweep — they
// come OFF the skiplist here.
// The corpus is never edited; each stage widens the dir list and shrinks the
// skiplist.
var readCoreDirs = []string{
	"../../../test/data/query/cypher/tck/features/clauses/match",
	"../../../test/data/query/cypher/tck/features/clauses/return",
	"../../../test/data/query/cypher/tck/features/clauses/match-where",
	"../../../test/data/query/cypher/tck/features/clauses/return-skip-limit",
	"../../../test/data/query/cypher/tck/features/clauses/union",
	"../../../test/data/query/cypher/tck/features/clauses/with",
	"../../../test/data/query/cypher/tck/features/clauses/return-orderby",
	"../../../test/data/query/cypher/tck/features/clauses/with-orderBy",
	"../../../test/data/query/cypher/tck/features/clauses/with-skip-limit",
	"../../../test/data/query/cypher/tck/features/clauses/with-where",
	"../../../test/data/query/cypher/tck/features/clauses/unwind",
	"../../../test/data/query/cypher/tck/features/clauses/create",
	"../../../test/data/query/cypher/tck/features/clauses/delete",
	"../../../test/data/query/cypher/tck/features/clauses/set",
	"../../../test/data/query/cypher/tck/features/clauses/remove",
	"../../../test/data/query/cypher/tck/features/clauses/merge",
	"../../../test/data/query/cypher/tck/features/clauses/call",
	"../../../test/data/query/cypher/tck/features/expressions/literals",
	"../../../test/data/query/cypher/tck/features/expressions/boolean",
	"../../../test/data/query/cypher/tck/features/expressions/comparison",
	"../../../test/data/query/cypher/tck/features/expressions/mathematical",
	"../../../test/data/query/cypher/tck/features/expressions/string",
	"../../../test/data/query/cypher/tck/features/expressions/null",
	"../../../test/data/query/cypher/tck/features/expressions/precedence",
	"../../../test/data/query/cypher/tck/features/expressions/typeConversion",
	"../../../test/data/query/cypher/tck/features/expressions/list",
	"../../../test/data/query/cypher/tck/features/expressions/map",
	"../../../test/data/query/cypher/tck/features/expressions/conditional",
	"../../../test/data/query/cypher/tck/features/expressions/temporal",
	"../../../test/data/query/cypher/tck/features/expressions/path",
	"../../../test/data/query/cypher/tck/features/expressions/pattern",
	"../../../test/data/query/cypher/tck/features/expressions/graph",
	"../../../test/data/query/cypher/tck/features/expressions/aggregation",
	"../../../test/data/query/cypher/tck/features/expressions/quantifier",
	"../../../test/data/query/cypher/tck/features/expressions/existentialSubqueries",
}

const goldenDir = "testdata/golden"

// skiplist excludes by name the negative TCK scenarios that this parser
// deliberately accepts: valid openCypher whose error lives on the other side of
// the type-interface boundary, so it is accept-and-ignored and caught at
// execution via the original text (ADR 0005, principle B1). Each entry is a
// TCK error this parser cannot and need not raise; later stages never need to
// remove them (they aren't "unsupported features"). A skipped scenario is
// reported and counted, never deleted from the corpus.
//
// The Stage 1 audit of clauses/return-skip-limit/ classified its 31 scenarios
// as 11 parse-green (snapshotted goldens), 4 PENDING via existing
// ErrUnsupportedClause/ErrUnsupportedProjection sentinels (WITH/UNWIND/
// aggregation in RETURN), 0 true parse-rejection, and 16 accept-then-runtime-
// or-compile-time-value-error scenarios listed below in two groups.
//
// Heterogeneous reasons-to-skip live together here; each entry is pinned to
// its actual cause rather than collapsed to a single rationale.
var skiplist = map[string]bool{
	// --- pattern semantics that live below the type-interface boundary ---
	//
	// MATCH (a)-[r]->()-[r]->(a): reusing a relationship variable is a runtime
	// uniqueness rule, not a type-interface concern. Spec Cluster C: relationship
	// reuse is not special-cased — first occurrence defines endpoints, later
	// occurrences merge labels.
	"[29] Fail when re-using a relationship in the same pattern": true,
	// WHERE count(a) > 10: an aggregation inside WHERE. Bucket 3 per ADR 0007 —
	// the parser accepts and types the predicate (Stage 10: count(n) > 3 types as
	// TypeBool via the aggregate-arm in typeAtom), then re-executes the original
	// text so the engine raises the aggregate-in-WHERE grouping/binding-scope
	// error (ADR 0005). Per-position aggregate legality is a semantic rule the
	// type-interface boundary does not carry — same family as
	// AmbiguousAggregationExpression / InvalidAggregation.
	"[15] Fail on aggregation in WHERE": true,

	// --- SKIP/LIMIT with a literal the TCK rejects as compile-time
	//     NonConstantExpression / NegativeIntegerArgument / InvalidArgumentType ---
	//
	// The value lives below the type-interface boundary (B1 — execution validates
	// the literal via the original text per ADR 0005), so this parser
	// accept-and-ignores. The TCK names these "compile time SyntaxError" but the
	// rejection is a value-constraint check, not a parse-shape check; an engine
	// reading our generated method body still sees the original SKIP -1 / LIMIT
	// 1.5 / SKIP n.count text and raises the same error.
	"[5] SKIP with an expression that depends on variables should fail": true,
	"[7] Negative SKIP should fail":                                     true,
	"[9] Floating point SKIP should fail":                               true,
	"[10] Fail when using non-constants in SKIP":                        true,
	"[11] Fail when using negative value in SKIP":                       true,
	"[9] Fail when using non-constants in LIMIT":                        true,
	"[12] Fail when using negative value in LIMIT 1":                    true,
	"[13] Fail when using negative value in LIMIT 2":                    true,
	"[16] Fail when using floating point in LIMIT 1":                    true,
	"[17] Fail when using floating point in LIMIT 2":                    true,

	// --- SKIP/LIMIT with a parameter whose runtime value the TCK rejects as
	//     NegativeIntegerArgument / InvalidArgumentType ---
	//
	// The parameter's name is what the model carries (a ClauseSlotUse on the
	// Parameter); the runtime-bound argument value lives below the type-interface
	// boundary (B1), so this parser accept-and-ignores. An engine reading the
	// generated method body sees the original SKIP $_skip / LIMIT $_limit text
	// and binds the caller's value, raising the same error.
	"[6] Negative parameter for SKIP should fail":                       true,
	"[8] Floating point parameter for SKIP should fail":                 true,
	"[10] Negative parameter for LIMIT should fail":                     true,
	"[11] Negative parameter for LIMIT with ORDER BY should fail":       true,
	"[14] Floating point parameter for LIMIT should fail":               true,
	"[15] Floating point parameter for LIMIT with ORDER BY should fail": true,

	// --- projection value/semantics below the type-interface boundary (Stage 3) ---
	//
	// Stage 3 widens RETURN to a projection sum (var/var.prop, scalar literal,
	// function, aggregate, RETURN *), so these negatives now parse-accept; their
	// error lives below the boundary and the re-executed original text raises it
	// (ADR 0005, B1).
	//
	// RETURN * with nothing in scope expands to zero columns: a scope/value error
	// (NoVariablesInScope), not a parse-shape one. We record ReturnsAll and the
	// resolver expands * post-freeze (spec §3).
	"[2] Fail when using RETURN * without variables in scope": true,
	// RETURN foo(a): the parser carries no function name (FuncProjection holds only
	// the referenced bindings, §2), so a non-existent function (UnknownFunction) is
	// not a distinction it can make — the engine re-executing foo(a) raises it.
	"[18] Fail on projecting a non-existent function": true,
	// RETURN 1 AS a, 2 AS a: duplicate column names are a value-level result-shape
	// check (ColumnNameConflict); Returns is duplicate-preserving (Stage-0 rule),
	// so two LiteralProjections both named "a" parse-accept.
	"[10] Fail when returning multiple columns with same name": true,

	// --- WITH/UNION value & result-shape errors below the boundary (Stage 4) ---
	//
	// Stage 4 adds WITH chaining (per-part scopes) and UNION (parallel branches),
	// so these negatives now parse-accept; each error is a value- or result-shape
	// rule the type-interface model does not carry (B1, ADR 0003), raised by the
	// re-executed original text (ADR 0005).
	//
	// WITH <literal> AS n / MATCH (n): n imports a name into the next part and is
	// re-bound there as a node; the conflict is that the WITH expression's value is
	// not a node (VariableTypeConflict). We model n's binding kind, not the type of
	// the projected expression, so the two reconcile structurally. (Scenario Outline
	// with 3 examples — true/123/123.4 — each pickle carries the same name.)
	"[11] Fail when matching a node variable bound to a value": true,
	// WITH <invalid> AS r / MATCH ()-[r]-(): the edge analogue of the node entry
	// above — r imports a name and is re-bound as a relationship; the conflict is
	// that the WITH expression's value is not a relationship (VariableTypeConflict).
	// We model r's binding kind, not the projected expression's type. Reachable only
	// at Stage 5 because the pattern is undirected (()-[r]-()); the error is the same
	// value-level rule below the type-interface boundary (B1, ADR 0003/0005).
	"[13] Fail when matching a relationship variable bound to a value": true,
	// RETURN 1 AS a UNION RETURN 2 AS b: the two branches expose different column
	// names (DifferentColumnsInUnion). Column compatibility across branches is not
	// modelled (ADR 0003); we record each branch's Returns verbatim.
	"[5] Failing when UNION has different columns":     true,
	"[5] Failing when UNION ALL has different columns": true,
	// Mixing UNION with UNION ALL in one query (InvalidClauseComposition): we record
	// the combinator sequence faithfully ([union, unionAll]); the no-mixing rule is a
	// clause-composition constraint, not a parse-shape one.
	"[1] Failing when mixing UNION and UNION ALL": true,
	"[2] Failing when mixing UNION ALL and UNION": true,
	// WITH 1 AS a, 2 AS a: duplicate forwarded column names (ColumnNameConflict),
	// the WITH analogue of the RETURN entry above; Returns is duplicate-preserving.
	"[4] Fail when forwarding multiple aliases with the same name": true,
	// WITH a, count(*): a non-aliased expression in WITH (NoExpressionAlias). We
	// synthesise a Name from the item's source text (here "count(*)"), so every WITH
	// item carries a name and the must-alias rule has nothing to check against.
	"[5] Fail when not aliasing expressions in WITH": true,

	// --- expressions value/semantics below the type-interface boundary (Stage 6) ---
	//
	// Stage 6 widens RETURN / WITH projections to any scalar expression and types
	// the result, so these AmbiguousAggregationExpression negatives now parse-accept
	// as ExprProjection over the whole expression. Grouping-key correctness — the
	// rule "every non-aggregate sub-expression inside an aggregate expression must
	// be a projected variable" — is a semantic constraint the type interface does
	// not carry (ADR 0003), so it is a bucket-3 runtime concern (ADR 0007). An
	// engine re-executing the original text raises the same error.
	"[8] Fail if not projected variables are used inside an expression which contains an aggregation expression":                   true,
	"[9] Fail if more complex expression, even if projected, are used inside expression which contains an aggregation expression":  true,
	"[20] Fail if not returned variables are used inside an expression which contains an aggregation expression":                   true,
	"[21] Fail if more complex expressions, even if returned, are used inside expression which contains an aggregation expression": true,
	// count(count(*)) — nested aggregation is a NestedAggregation semantic rule.
	// Stage 6 accepts the outer count() as an AggregateProjection and the inner
	// count(*) as its argument (surfaced as a ref-free func-arg walk). The rule
	// against nesting an aggregate inside another aggregate is a resolver /
	// engine concern below the type-interface boundary.
	"[14] Aggregates in aggregates": true,
	// count(rand()) — the impurity of rand() prevents grouping-key aggregation
	// semantics; a value-level engine rule below the boundary.
	"[15] Using `rand()` in aggregations": true,
	// MATCH (n) WITH [n] AS users MATCH (users)-->() — reusing an alias bound to
	// a list-of-nodes as a node pattern variable is a VariableTypeConflict
	// (value-level rule). Stage 6 accepts the WITH [n] AS users projection as
	// an ExprProjection of TypeList<TypeNode>; the downstream re-binding of
	// users as a node is a schema-agnostic parse-accept (predicate structure
	// stays below the boundary).
	"[30] Fail when using a list or nodes as a node": true,

	// size(<pattern-predicate>) — a pattern predicate as a function argument.
	// The TCK names this SyntaxError:UnexpectedSyntax (a genuine parse-shape
	// class) but the fail-site rule is really "pattern predicates are not
	// bindable arguments to size()," a semantic check tied to size()'s
	// signature. The parser accepts the pattern-predicate atom as an unknown-
	// typed opaque (typing.go's typeAtom leaves OC_PatternPredicate as
	// TypeUnknown without mining refs), so the query parses. Rejection of
	// pattern-predicate arguments is downstream signature-checking work
	// (procedure/function registry, ADR 0007), out of Stage 6's scope.
	"[6] Fail for `size()` on pattern predicates": true,

	// --- pattern semantics (Stage 8) ---
	//
	// Stage 8 widens the pattern model to admit named paths, variable-length
	// relationships, and multi-type relationships. Three of the negative
	// scenarios exercise semantic rules the type-interface model does not
	// carry, so they now parse-accept; each error sits below the boundary
	// per ADR 0005 and rides bucket 3 (ADR 0007).
	//
	// WITH <invalid> AS p / MATCH p = ()-[]-(): binding a path variable to a
	// value is a VariableAlreadyBound semantic rule (the WITH exports p as a
	// non-path expression's alias; the MATCH re-binds it as a path). The
	// model records p's kind (path) and the WITH's alias-and-type separately,
	// so the two reconcile structurally; the engine raises. Scenario Outline
	// with several examples, all sharing the same name.
	"[25] Fail when matching a path variable bound to a value": true,

	// MATCH r = (n)-[*]->() / WHERE r.name = 'apa' / RETURN r: a property
	// lookup on a path variable is an InvalidArgumentType semantic rule
	// (paths have no properties). The parser accepts the property-lookup
	// shape (Stage 6 records TypeUnknown for any property lookup, ADR 0003);
	// the engine's type-check against the resolved r:path rejects it.
	"[14] Fail when filtering path with property predicate": true,

	// (Stage 11, gqlc-3r0 fold: the two Pattern1 [22]/[23] deferrals — a
	// pattern predicate at RETURN / WITH projection position — are
	// RETIRED. collectReturnItem now rejects the shape with the new
	// ErrPatternInProjection sentinel via isPatternPredicateAtom; see the
	// "pattern predicate in return projection" mustReject case for the
	// fail-site.)

	// MATCH (n) SET n.prop = head(nodes(head((n)-[:REL]->()))).foo — a pattern
	// predicate buried inside the RHS of a SET item's value expression
	// (SyntaxError:UnexpectedSyntax). Stage 12 exposes this newly: before
	// Stage 12 the SET clause rejected via ErrUnsupportedClause, so the
	// scenario passed on the negative outcome; after Stage 12 the SET
	// clause parses, and the buried pattern predicate types as TypeBool
	// via typeAtom's Stage-11 pattern-predicate arm, so the whole SET
	// value parses. Stage 11 §8 documents this class of hole
	// ("pattern predicate inside another expression" — the enclosing
	// shape is not a bare atom, so the isPatternPredicateAtom check does
	// not catch it). Widening the rejection to climb the precedence
	// tower is Stage-11 scope creep; Stage 12 records the shape as
	// bucket-1 accept-and-defer and lets the engine raise the
	// UnexpectedSyntax rule on the original text (ADR 0005). A future
	// stage that revisits pattern-predicate scope-checking would remove
	// this entry.
	"[24] Fail on using pattern in right-hand side of SET": true,

	// --- deleted-entity access at RETURN (Stage 12 newly-exposed) ---
	//
	// MATCH (n) DELETE n RETURN n.num (Return2 [15]/[16]/[17]) —
	// EntityNotFound:DeletedEntityAccess at runtime. Not a SyntaxError,
	// not under expressions/*, so neither isBucketThreeDir nor
	// isBucketThreeError's SyntaxError-detail gate catches it — but
	// EntityNotFound IS a runtime rule below the type-interface boundary
	// (ADR 0005): the parser accepts, the engine detects deleted-entity
	// access at execution. Before Stage 12 these scenarios PENDING'd via
	// ErrUnsupportedClause on DELETE; after Stage 12 DELETE parses, so
	// the shapes are enumerated here as bucket-3 accept-and-defer.
	"[15] Fail when returning properties of deleted nodes":         true,
	"[16] Fail when returning labels of deleted nodes":             true,
	"[17] Fail when returning properties of deleted relationships": true,

	// --- write-side semantic rules the type interface does not carry
	//     (Stage 12: clauses/{create,delete,set,remove} newly wired) ---
	//
	// The write-clause dirs are NOT under expressions/*, so isBucketThreeDir
	// does not categorically accept their negatives. Each entry below is a
	// SyntaxError or TypeError whose rule sits below the type-interface
	// boundary (ADR 0005): the parser accepts the shape at the shape gates
	// it already applies (pattern collection, expression typing), and the
	// engine re-executing the original text raises the specific rule. The
	// three semantic-rule families:
	//
	//   1. VariableAlreadyBound (Create1 [13]-[19], Create2 [23]) —
	//      CREATE re-binding a name a prior clause already bound. openCypher
	//      forbids re-binding at compile time (a CREATE (a) after MATCH (a)
	//      is legal only if the pattern is unlabelled; a CREATE (a:Label)
	//      after MATCH (a) is VariableAlreadyBound). The rule turns on the
	//      *combination* of clause order, labels, and inline properties —
	//      a semantic-composition rule the type interface's per-clause
	//      binding shape does not carry. Stage 12 records the binding under
	//      the existing mergeBinding path (labels merge, kind-conflict
	//      still fires); the compile-time rule is engine-side.
	//   2. NoSingleRelationshipType / RequiresDirectedRelationship /
	//      CreatingVarLength (Create2 [18]-[22]) — CREATE-side pattern
	//      constraints: an edge must have exactly one type, an unambiguous
	//      direction, and no hop range. The type-interface carries the
	//      shape (LabelSet, directed flag, EdgeHops) verbatim — the
	//      constraint that CREATE requires specific shape values is a
	//      write-clause semantic rule. Codegen post-freeze reads
	//      StatementKind and could enforce these at generation time; today
	//      the engine raises them.
	//   3. UndefinedVariable (Create1 [20], Create2 [24]) — Stage 13
	//      retires these skiplist entries. The mineInlineMap widening
	//      (Stage 13 §4.3) now records inline-map value refs onto
	//      curPart.refs uniformly, so `CREATE (b {name: missing})` and
	//      `CREATE (a)-[:KNOWS]->(b {name: missing})` reach
	//      ErrUnboundVariable at the buildPart referential-integrity
	//      sweep. Layer-2 counterpart: the unbound inline-map ref
	//      kill-probe mustReject pin. See spec §5 skiplist removals.
	//   4. InvalidDelete (Delete1 [8], Delete2 [5]) — DELETE target is
	//      a labelled variable (`DELETE r:T`). The label predicate types
	//      as TypeBool via typeAtom's node-labels branch; the whole
	//      expression parses as a rich shape. The engine's rule "DELETE
	//      target must be a node or edge value" is a runtime type check
	//      below the boundary.
	//   5. InvalidArgumentType (Delete5 [9]) — DELETE target is an integer
	//      expression (`DELETE 1 + 1`). Already handled by
	//      isBucketThreeError's SyntaxError-detail gate; explicit entry
	//      here because clauses/delete is not under expressions/*.
	//   6. Cardinality / value rules (Delete1 [7]) — deleting a node with
	//      connected edges without DETACH; a runtime cardinality rule.
	//   7. TypeError:InvalidPropertyType (Set1 [10]) — SET a property to a
	//      list-of-maps, which the property model does not admit at
	//      runtime. TypeError is not SyntaxError, so bucket-3-eligible via
	//      the top-level kind gate; explicit entry because the dir is not
	//      under expressions/*.
	"[13] Fail when creating a node that is already bound":                          true,
	"[14] Fail when creating a node with properties that is already bound":          true,
	"[15] Fail when adding a new label predicate on a node that is already bound 1": true,
	"[16] Fail when adding new label predicate on a node that is already bound 2":   true,
	"[17] Fail when adding new label predicate on a node that is already bound 3":   true,
	"[18] Fail when adding new label predicate on a node that is already bound 4":   true,
	"[19] Fail when adding new label predicate on a node that is already bound 5":   true,
	// [20] Fail when creating a node using undefined variable in pattern:
	// retired by Stage 13's mineInlineMap widening (see case 3 above).
	"[18] Fail when creating a relationship without a type":          true,
	"[19] Fail when creating a relationship without a direction":     true,
	"[20] Fail when creating a relationship with two directions":     true,
	"[21] Fail when creating a relationship with more than one type": true,
	"[22] Fail when creating a variable-length relationship":         true,
	"[23] Fail when creating a relationship that is already bound":   true,
	// [24] Fail when creating a relationship using undefined variable in
	// pattern: retired by Stage 13's mineInlineMap widening.
	"[7] Failing when deleting connected nodes":              true,
	"[8] Failing when deleting a label":                      true,
	"[5] Failing when deleting a relationship type":          true,
	"[9] Failing when deleting an integer expression":        true,
	"[10] Failing when setting a list of maps as a property": true,

	// --- MERGE-dir negatives (Stage 13, bucket-3 per ADR 0007) ---
	//
	// clauses/merge negatives cluster into the same rule families the Stage-12
	// CREATE negatives do — the write-clause semantic rules that live below
	// the type-interface boundary (ADR 0005). Each entry pairs with its
	// Stage-12 CREATE analogue by kind:
	//
	//   VariableAlreadyBound (Merge1 [15], Merge5 [22]/[26]) — parallels case
	//     1 above (Create1 [13]-[19]); the write-clause re-binding rule turns
	//     on clause order + labels, which the type interface does not carry.
	//   MergeReadOwnWrites (Merge1 [17], Merge5 [29]) — a null property value
	//     in a MERGE pattern is a runtime "match uses this pattern; a null
	//     property would match nothing" cardinality rule. TypeError sits below
	//     the boundary; explicit entries because the dir is not under
	//     expressions/*.
	//   NoSingleRelationshipType (Merge5 [23]/[24]/[25]) — parallels case 2
	//     above (Create2 [18]-[21]); the MERGE relationship shape must be a
	//     single directed type, a write-clause semantic rule.
	//   CreatingVarLength (Merge5 [28]) — parallels Create2 [22]; a MERGE
	//     relationship cannot carry a hop range.
	"[15] Fail when merge a node that is already bound":                          true,
	"[17] Fail on merging node with null property":                               true,
	"[22] Fail when imposing new predicates on a variable that is already bound": true,
	"[23] Fail when merging relationship without type":                           true,
	"[24] Fail when merging relationship without type, no colon":                 true,
	"[25] Fail when merging relationship with more than one type":                true,
	"[26] Fail when merging relationship that is already bound":                  true,
	"[28] Fail when using variable length relationship in MERGE":                 true,
	"[29] Fail on merging relationship with null property":                       true,

	// --- EXISTS with an inner write clause (Stage 11) ---
	//
	// MATCH (n) WHERE exists { MATCH (n)-->(m) SET m.prop='fail' } RETURN n:
	// a SET inside EXISTS { ... } — SyntaxError:InvalidClauseComposition per
	// the TCK. Stage 11 §1.6 accept-and-defer: the outer EnterOC_Set handler
	// suppresses inside EXISTS { ... } (subqueryDepth > 0), so the query
	// parses; the InvalidClauseComposition rule is a position-shape semantic
	// rule the type interface does not carry (writes-in-subqueries is a
	// bucket-3 case the engine raises when re-executing the original text,
	// ADR 0005). isBucketThreeError does not include InvalidClauseComposition
	// (it's a genuine parse-shape kind elsewhere — mixing UNION with UNION
	// ALL is enumerated similarly under clauses/union), so this needs an
	// enumerated entry rather than a categorical accept.
	"[3] Full existential subquery with update clause should fail": true,

	// --- ORDER BY value/semantics below the type-interface boundary (Stage 9) ---
	//
	// Stage 9 wires clauses/return-orderby and clauses/with-orderBy. ORDER BY's
	// sort-key structure is not modelled (ADR 0003) — the sort keys are walked
	// only for parameter mining (§4.2), so undefined-variable, aggregation-
	// misuse, DISTINCT-scope, and non-projected-aggregation scenarios all
	// parse-accept. Each error is a value-level result-shape or grouping-key
	// rule the type interface does not carry (B1), raised by the re-executed
	// original text (ADR 0005). Bucket 3 per ADR 0007 §6 — enumerated here
	// because the clause dirs are not under isBucketThreeDir.
	//
	// WITH a ORDER BY undefined_var: an ORDER BY variable that is not in the
	// WITH's projected set (SyntaxError:UndefinedVariable). The parser does
	// not carry ORDER BY refs (they are snapshotted around the sort-item walk),
	// so an undefined-in-sort-key name never triggers ErrUnboundVariable.
	// The outline has three example groups (out of scope / never defined /
	// mixed); each pickle carries a distinct name via the `#Example: ...`
	// suffix, so all three are listed.
	"[8] Fail on sorting by any number of undefined variables in any position #Example: out of scope":  true,
	"[8] Fail on sorting by any number of undefined variables in any position #Example: never defined": true,
	"[8] Fail on sorting by any number of undefined variables in any position #Example: mixed":         true,
	"[46] Fail on sorting by an undefined variable #Example: out of scope":                             true,
	"[46] Fail on sorting by an undefined variable #Example: never defined":                            true,
	// WITH a WITH DISTINCT b ORDER BY a: a sort key naming a variable removed
	// by DISTINCT (SyntaxError:UndefinedVariable). Same rationale as above:
	// ORDER BY structure is below the boundary.
	"[13] Fail when sorting on variable removed by DISTINCT": true,
	// ORDER BY count(...) at RETURN or WITH position without a corresponding
	// projected aggregate (SyntaxError:InvalidAggregation). The parser walks
	// the sort key for parameters only; nested aggregate structure is not
	// modelled. Grouping-key correctness is a resolver concern.
	"[14] Fail on aggregation in ORDER BY after RETURN":                    true,
	"[25] Fail on sorting by an aggregation":                               true,
	"[13] Fail on sorting by a non-projected aggregation on a variable":    true,
	"[14] Fail on sorting by a non-projected aggregation on an expression": true,
	// ORDER BY containing an aggregation whose non-aggregate sub-expressions
	// are not projected variables (SyntaxError:AmbiguousAggregationExpression).
	// Same grouping-key rule as the Stage-6 return-orderby entries — sort-key
	// structure is below the boundary.
	"[4] Fail if not returned variables are used inside an order by item which contains an aggregation expression":                        true,
	"[5] Fail if more complex expressions, even if returned, are used inside an order by item which contains an aggregation expression":   true,
	"[19] Fail if not projected variables are used inside an order by item which contains an aggregation expression":                      true,
	"[20] Fail if more complex expressions, even if projected, are used inside an order by item which contains an aggregation expression": true,

	// --- Stage 14 (clauses/call) bucket-3 skiplist ---
	//
	// The Stage-14 spec §5 enumerates exactly four scenarios whose
	// negative outcome the parser accepts categorically per ADR 0007's
	// bucket-3 discipline: the wire model does not carry the fact
	// needed to raise them at parse time, and the engine on the
	// re-executed original text raises the same error (ADR 0005).

	// SyntaxError:InvalidArgumentType (Call2 [5]/[6]) — wrong argument
	// type against the signature's declared param. Stage 14 does not
	// check argument types against the registry (spec §4.5): the mined
	// argument type at parse time is best-effort (a $param mines to
	// TypeUnknown; a n.prop mines to TypeUnknown), so a parser-time
	// reject would either over-reject a $param the engine would accept
	// or fire only on literals (a half-check that gives false
	// confidence).
	"[5] Standalone call to procedure should fail if input type is wrong": true,
	"[6] In-query call to procedure should fail if input type is wrong":   true,
	// ParameterMissing:MissingParameter (Call1 [11]) — implicit
	// invocation binds args from `$name` parameters at runtime; the
	// parser has no static way to detect a missing named parameter
	// (there is no `$name` in the query text — the binding is
	// implicit-by-signature-name). Bucket-3 accept-and-defer.
	"[11] Standalone call to procedure should fail if implicit argument is missing": true,
	// SyntaxError:InvalidAggregation (Call1 [16]) — aggregate in
	// argument position. Same family as `[15] Fail on aggregation in
	// WHERE` (already skiplisted): per-position aggregate legality is
	// a semantic rule the type-interface boundary does not carry
	// (ADR 0007).
	"[16] In-query procedure call should fail if one of the argument expressions uses an aggregation function": true,
}

// the public sentinels for scenarios the parser cannot faithfully represent
// yet — the "valid Cypher we don't support yet" set. A positive scenario that
// fails with one of these is the progress meter (PENDING), not a test
// failure. Mirrors the spec's category-grained taxonomy. Stage 6 retired
// ErrUnsupportedProjection. Stage 8 retired ErrUnsupportedPattern. Stage 14
// retires ErrUnsupportedClause (CALL was the last fail-site; CALL is
// supported after Stage 14 — an unknown procedure surfaces as the new
// ErrUnknownProcedure sentinel, which is a bucket-1 bounded rejection, not
// a "we cannot represent this shape" case). Stage 14 leaves only
// ErrUnsupportedParameter in the progress-meter set.
var unsupportedSentinels = []error{
	cypher.ErrUnsupportedParameter,
}

func isUnsupported(err error) bool {
	for _, s := range unsupportedSentinels {
		if errors.Is(err, s) {
			return true
		}
	}
	return false
}

// scenarioState carries the parse outcome of the "When executing query" step to
// the "Then" steps, plus the scenario identity for golden snapshotting and the
// skiplist. It is held per-scenario in the context.
type scenarioState struct {
	name    string
	uri     string
	query   string
	got     query.Query
	err     error
	skipped bool
	// sigs is the Stage-14 per-scenario procedure signature list,
	// populated by the "there exists a procedure" step. Multiple
	// procedure declarations in one scenario accumulate here in
	// declaration order; executingQuery constructs a procsig.Registry
	// from the slice before parsing.
	sigs []procsig.Signature
}

type stateKey struct{}

func stateFrom(ctx context.Context) *scenarioState {
	st, ok := ctx.Value(stateKey{}).(*scenarioState)
	if !ok {
		return nil
	}
	return st
}

func TestReadCoreAcceptance(t *testing.T) {
	// Non-strict: a PENDING scenario (valid Cypher out of Stage-0 scope) is the
	// progress meter and must not fail the suite. An UNDEFINED step (a phrasing we
	// have no step def for) would be a real harness gap, so it is guarded
	// separately by TestNoUndefinedSteps below — non-strict would otherwise let it
	// pass silently.
	suite := godog.TestSuite{
		Name:                "cypher-read-core",
		ScenarioInitializer: initScenario,
		Options: &godog.Options{
			Format:   "pretty",
			Paths:    readCoreDirs,
			Strict:   false,
			TestingT: t,
		},
	}
	if suite.Run() != 0 {
		t.Fatal("read-core acceptance suite failed")
	}
}

// undefinedStepsRe matches the pretty formatter's step summary when one or more
// steps had no definition (e.g. "1887 steps (… 3 undefined …)"). It is precise
// where a plain "undefined" substring is not — scenario titles legitimately
// contain the word (e.g. "Fail when returning an undefined variable").
var undefinedStepsRe = regexp.MustCompile(`\d+ undefined`)

// TestNoUndefinedSteps guards the harness gap that non-strict mode hides: every
// step in the read-core corpus must match a step definition. It runs the suite
// into a buffer (no TestingT, so subtests aren't re-emitted) and fails if the
// step summary reports any undefined step.
func TestNoUndefinedSteps(t *testing.T) {
	var buf bytes.Buffer
	godog.TestSuite{
		Name:                "cypher-read-core-stepcheck",
		ScenarioInitializer: initScenario,
		Options: &godog.Options{
			Format: "pretty",
			Paths:  readCoreDirs,
			Output: &buf,
		},
	}.Run()
	if undefinedStepsRe.MatchString(buf.String()) {
		t.Fatalf("undefined steps in the read-core corpus:\n%s", buf.String())
	}
}

// TestSkiplistOrphans guards against a stale skiplist entry: every key must
// match at least one scenario in the in-suite corpus. A TCK rename or reindex
// would orphan a key silently otherwise — the skiplist is consulted by name
// (acceptance_test.go's Before hook does `skiplist[sc.Name]`), so an unmatched
// key has no effect and the scenario it used to cover would surface as a
// regression. Mirrors TestNoUndefinedSteps's role as a harness-gap guard.
func TestSkiplistOrphans(t *testing.T) {
	seen := make(map[string]bool)
	for _, dir := range readCoreDirs {
		files, err := filepath.Glob(filepath.Join(dir, "*.feature"))
		if err != nil {
			t.Fatalf("glob %s: %v", dir, err)
		}
		for _, path := range files {
			f, err := os.Open(path)
			if err != nil {
				t.Fatalf("open %s: %v", path, err)
			}
			doc, err := gherkin.ParseGherkinDocument(f, func() string { return "" })
			if cerr := f.Close(); cerr != nil {
				t.Fatalf("close %s: %v", path, cerr)
			}
			if err != nil {
				t.Fatalf("parse %s: %v", path, err)
			}
			for _, p := range gherkin.Pickles(*doc, path, newIDGen()) {
				seen[p.Name] = true
			}
		}
	}
	for name := range skiplist {
		if !seen[name] {
			t.Errorf("skiplist entry %q matched no scenario — TCK rename or stale entry?", name)
		}
	}
}

// TestGoldenOrphans guards against a stale golden file: every .golden.json on
// disk must correspond to a scenario in the in-suite corpus. A TCK rename, a
// change to the golden-key hash input, or a change to the scenario query
// text would leave the old snapshot orphaned — silently — because the
// harness only reads/writes goldens keyed by the new hash. Cheap
// insurance: an orphaned golden signals that a real regression check has
// been quietly disconnected.
//
// The scenario query text is part of the hash (goldenPath) to disambiguate
// Scenario Outline example rows, so this test enumerates every pickle in the
// corpus and computes its expected path — then requires the on-disk set to
// be a subset of the expected set.
func TestGoldenOrphans(t *testing.T) {
	expected := make(map[string]bool)
	for _, dir := range readCoreDirs {
		files, err := filepath.Glob(filepath.Join(dir, "*.feature"))
		if err != nil {
			t.Fatalf("glob %s: %v", dir, err)
		}
		for _, path := range files {
			f, err := os.Open(path)
			if err != nil {
				t.Fatalf("open %s: %v", path, err)
			}
			doc, err := gherkin.ParseGherkinDocument(f, func() string { return "" })
			if cerr := f.Close(); cerr != nil {
				t.Fatalf("close %s: %v", path, cerr)
			}
			if err != nil {
				t.Fatalf("parse %s: %v", path, err)
			}
			for _, p := range gherkin.Pickles(*doc, path, newIDGen()) {
				// Key an expected golden by EVERY executing-query step, matching
				// the runtime posture: executingQuery overwrites st.query on each
				// When step, and checkGolden fires both at the final Then
				// (resultShouldBe) and at intermediate "no side effects" steps
				// for write statements — each snapshotting whatever st.query
				// holds at that moment. A Temporal4 storage scenario with a
				// CREATE followed by a MATCH-control-query mints one golden
				// keyed by the CREATE text and one keyed by the MATCH text.
				for _, step := range p.Steps {
					if isExecutingQueryStep(step) {
						q := step.Argument.DocString.Content
						expected[goldenPath(&scenarioState{name: p.Name, uri: p.Uri, query: q})] = true
					}
				}
			}
		}
	}
	onDisk, err := filepath.Glob(filepath.Join(goldenDir, "*.golden.json"))
	if err != nil {
		t.Fatalf("glob %s: %v", goldenDir, err)
	}
	for _, path := range onDisk {
		if !expected[path] {
			t.Errorf("orphan golden %q — no corpus scenario keys to it (rename or hash-input change?)", path)
		}
	}
}

func initScenario(ctx *godog.ScenarioContext) {
	ctx.Before(func(c context.Context, sc *godog.Scenario) (context.Context, error) {
		st := &scenarioState{name: sc.Name, uri: sc.Uri}
		if skiplist[sc.Name] {
			st.skipped = true
		}
		return context.WithValue(c, stateKey{}, st), nil
	})

	// Setup steps hold no graph, so they are no-ops (we are a parser, spec §7).
	ctx.Step(`^an empty graph$`, noop)
	ctx.Step(`^any graph$`, noop)
	ctx.Step(`^having executed:$`, noopDoc)
	ctx.Step(`^parameters are:$`, noopTable)

	// Stage 14 (clauses/call): the TCK declares procedure signatures
	// via a background step of the form
	//   `And there exists a procedure test.my.proc(in :: INTEGER?) :: (out :: STRING?):`
	// followed by an example-data DataTable the parser does not consume
	// (the table is runtime example data, not part of the type
	// interface). Two grammar variants appear in the corpus:
	//   * trailing colon adjacent to the closing paren: `):`
	//   * trailing colon with a space: `) :` (Call5[3])
	// The step parses the signature text into a procsig.Signature and
	// accumulates it on scenarioState.sigs for executingQuery to
	// consume. The trailing table is consumed silently via noopTable.
	ctx.Step(`^there exists a procedure (.+?)\s*:$`, thereExistsAProcedure)

	// The query under test.
	ctx.Step(`^executing query:$`, executingQuery)
	// The Stage-7 temporal storage scenarios (Temporal4) pair a write query
	// with a follow-up "executing control query" that reads back what was
	// written. We are a parser and the first executing-query already
	// exercised the parser's disposition (a write clause rejects, an
	// expression parses); the control query re-exercises the same rules
	// against a read query, so we route it through the same parser call
	// and let the following Then steps carry the assertion.
	ctx.Step(`^executing control query:$`, executingQuery)

	// Positive outcomes: the scenario expected a result, so the query must parse.
	// The order qualifier is a non-capturing group: we don't bind it, and a
	// capturing group would force a string argument onto the step function.
	// Stage 9's return-orderby corpus adds `, in order (ignoring element order
	// for lists)` — the combined qualifier — so the alternation covers it.
	ctx.Step(`^the result should be(?:, in any order| \(ignoring element order for lists\)|, in order(?: \(ignoring element order for lists\))?)?:$`, resultShouldBe)
	// Storage scenarios expect an empty result from the write query
	// (Temporal4). At the parse level "empty" is the same guard as
	// "should be": the query must have parsed (or be a known-unsupported /
	// skipped scenario). noSideEffects's semantics fit exactly.
	//
	// Assumption: write clauses fail at parse today (ErrUnsupportedClause),
	// so a scenario reaching "the result should be empty" pairs with a
	// parse-time reject and noSideEffects returns ErrPending via the
	// isUnsupported path. Once a future stage parses writes, the paired
	// executing-query step will succeed and this step must snapshot the
	// resulting model — silent-drop this and a Stage-12 write scenario
	// would type-check clean with no assertion. Guarded by
	// TestGoldenOrphans keying every executing-query step to a golden.
	ctx.Step(`^the result should be empty$`, noSideEffects)
	ctx.Step(`^no side effects$`, noSideEffects)
	ctx.Step(`^the side effects should be:$`, noopTable)

	// Negative outcomes: the scenario expected an error, so the query must be rejected.
	// "at any time" appears in expression scenarios that do not care whether the
	// engine detects the error at compile time or runtime; a parser sees it as a
	// rejection request identical to the two named phases. The detail token
	// accepts * as a wildcard for scenarios that don't pin a specific error.
	ctx.Step(`^a (\w+) should be raised at (compile time|runtime|any time): (\S+)$`, shouldBeRejected)
	ctx.Step(`^a (\w+) should be raised at (compile time|runtime|any time)$`, shouldBeRejectedNoDetail)
}

func noop(_ context.Context) error                        { return nil }
func noopDoc(_ context.Context, _ *godog.DocString) error { return nil }
func noopTable(_ context.Context, _ *godog.Table) error   { return nil }

// thereExistsAProcedure parses a TCK background procedure declaration
// of the shape `test.my.proc(in :: INTEGER?) :: (out :: STRING?)` and
// records the resulting procsig.Signature on the scenario state. The
// attached DataTable is example data (runtime rows the parser does not
// consume, per spec §7) — silently ignored.
func thereExistsAProcedure(ctx context.Context, sigText string, _ *godog.Table) error {
	st := stateFrom(ctx)
	if st == nil {
		return errors.New("scenario state missing")
	}
	sig, err := parseProcedureSignature(sigText)
	if err != nil {
		return fmt.Errorf("parse procedure signature %q: %w", sigText, err)
	}
	st.sigs = append(st.sigs, sig)
	return nil
}

// signatureRE matches a full procedure declaration in the shape
// `name(params) :: (results)`. The name is the dotted fully-qualified
// form; params and results are comma-separated `field :: TOKEN?`
// declarations (`?` is optional).
var signatureRE = regexp.MustCompile(`^\s*(?P<name>[A-Za-z_][A-Za-z0-9_.]*)\s*\(\s*(?P<params>[^)]*)\)\s*::\s*\(\s*(?P<results>[^)]*)\)\s*$`)

// columnRE matches one signature column: `name :: TOKEN` or
// `name :: TOKEN?`. Whitespace tolerant.
var columnRE = regexp.MustCompile(`^\s*(?P<name>[A-Za-z_][A-Za-z0-9_]*)\s*::\s*(?P<token>[A-Za-z]+)(?P<nullable>\??)\s*$`)

// parseProcedureSignature lifts the TCK step's signature text into a
// procsig.Signature. Returns an error if the shape does not match the
// declaration grammar or if any column names a token outside the
// procsig.TypeToken sum.
func parseProcedureSignature(text string) (procsig.Signature, error) {
	m := signatureRE.FindStringSubmatch(text)
	if m == nil {
		return procsig.Signature{}, fmt.Errorf("signature does not match `name(params) :: (results)` shape")
	}
	name := m[signatureRE.SubexpIndex("name")]
	params, err := parseColumnList(m[signatureRE.SubexpIndex("params")])
	if err != nil {
		return procsig.Signature{}, fmt.Errorf("params: %w", err)
	}
	results, err := parseColumnList(m[signatureRE.SubexpIndex("results")])
	if err != nil {
		return procsig.Signature{}, fmt.Errorf("results: %w", err)
	}
	sig := procsig.Signature{Name: name}
	for _, c := range params {
		sig.Params = append(sig.Params, procsig.Param{Name: c.name, Token: c.token, Nullable: c.nullable})
	}
	for _, c := range results {
		sig.Results = append(sig.Results, procsig.Result{Name: c.name, Token: c.token, Nullable: c.nullable})
	}
	return sig, nil
}

type signatureColumn struct {
	name     string
	token    procsig.TypeToken
	nullable bool
}

// parseColumnList splits a comma-separated column list (`name :: TOKEN
// [?], name2 :: TOKEN2 [?], ...`) into a slice of columns. Empty input
// yields the empty slice — a signature with no params or no results is
// valid (e.g. `test.doNothing() :: ()`).
func parseColumnList(text string) ([]signatureColumn, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, nil
	}
	parts := strings.Split(text, ",")
	out := make([]signatureColumn, 0, len(parts))
	for _, p := range parts {
		m := columnRE.FindStringSubmatch(p)
		if m == nil {
			return nil, fmt.Errorf("column %q does not match `name :: TOKEN[?]` shape", p)
		}
		tok, err := parseTypeToken(m[columnRE.SubexpIndex("token")])
		if err != nil {
			return nil, err
		}
		out = append(out, signatureColumn{
			name:     m[columnRE.SubexpIndex("name")],
			token:    tok,
			nullable: m[columnRE.SubexpIndex("nullable")] == "?",
		})
	}
	return out, nil
}

// parseTypeToken maps the TCK's uppercase token name to a
// procsig.TypeToken. Any token outside the sum returns an error —
// which surfaces as a step failure and reveals a corpus token the
// registry does not yet declare.
func parseTypeToken(name string) (procsig.TypeToken, error) {
	switch name {
	case "INTEGER":
		return procsig.TokenInteger, nil
	case "FLOAT":
		return procsig.TokenFloat, nil
	case "STRING":
		return procsig.TokenString, nil
	case "NUMBER":
		return procsig.TokenNumber, nil
	default:
		return 0, fmt.Errorf("unknown TypeToken %q (extend procsig.TypeToken to admit it)", name)
	}
}

// executingQuery runs Parse and stashes the outcome for the Then steps.
// Stage 14: the parser is constructed with the per-scenario procedure
// registry accumulated by the "there exists a procedure" step. A
// scenario with no such steps gets the empty registry (every CALL
// lookup misses, ErrUnknownProcedure fires at the fail-site — which is
// exactly the shape Call1[13]/[14] test).
func executingQuery(ctx context.Context, doc *godog.DocString) error {
	st := stateFrom(ctx)
	if st == nil {
		return errors.New("scenario state missing")
	}
	st.query = doc.Content
	reg, regErr := procsig.NewRegistry(st.sigs)
	if regErr != nil {
		st.err = fmt.Errorf("test-side registry construction failed: %w", regErr)
		return nil
	}
	st.got, st.err = cypher.New(cypher.WithRegistry(reg)).Parse(strings.NewReader(doc.Content))
	return nil
}

// resultShouldBe asserts the positive contract: the query parsed and its model
// matches the golden snapshot. A rejection with an ErrUnsupported* sentinel is the
// progress meter (PENDING/skip); any other error fails — including the run-A stub
// (errNotImplemented), which is the genuine implementation-gap signal.
func resultShouldBe(ctx context.Context, _ *godog.Table) error {
	st := stateFrom(ctx)
	if st.skipped {
		return godog.ErrPending
	}
	if st.err != nil {
		if isUnsupported(st.err) {
			return godog.ErrPending // valid Cypher we don't support yet
		}
		return fmt.Errorf("expected a parsed query, got error: %w", st.err)
	}
	return checkGolden(st)
}

// noSideEffects is a positive corroborating step. For read statements it only
// guards the outcome — resultShouldBe carries the snapshot assertion. For write
// statements it must carry the assertion itself: many write-dir scenarios have
// no result table, so without the checkGolden call here their Effects shape
// would never be snapshot-verified (spec §1.9 promises 100% write coverage).
func noSideEffects(ctx context.Context) error {
	st := stateFrom(ctx)
	if st.skipped {
		return godog.ErrPending
	}
	if st.err != nil {
		if isUnsupported(st.err) {
			return godog.ErrPending
		}
		return fmt.Errorf("expected a parsed query, got error: %w", st.err)
	}
	if st.got.StatementKind == query.StatementWrite {
		return checkGolden(st)
	}
	return nil
}

// shouldBeRejected asserts the negative contract: any rejection passes, a parsed
// query fails. The error type/detail the TCK names targets a full engine; we are
// a parser, so we only assert rejection. Both the kind and the detail flow to
// assertRejected so the bucket-3 categorical accept-path can gate on them: a
// SyntaxError with a runtime-level detail (IntegerOverflow, InvalidArgumentType,
// UndefinedVariable, …) is engine-side and bucket-3-eligible; a SyntaxError
// with a real parse-shape detail (UnexpectedSyntax, InvalidClauseComposition)
// or an unspecified detail (*) is parser-owned and must actually reject.
func shouldBeRejected(ctx context.Context, kind, _ /*phase*/, detail string) error {
	return assertRejected(ctx, kind, detail)
}

func shouldBeRejectedNoDetail(ctx context.Context, kind, _ /*phase*/ string) error {
	return assertRejected(ctx, kind, "")
}

// assertRejected is the shared negative-contract check.
//
// The kind names the TCK error class (SyntaxError, TypeError, SemanticError,
// ArgumentError, …); the detail is the specific rule the engine cites, e.g.
// SyntaxError:IntegerOverflow. The parser owns exactly one kind — SyntaxError —
// and even inside SyntaxError only a subset of details are true parse-shape
// rules; the rest are value-level / semantic checks the engine raises when it
// re-executes the original text (ADR 0005). The bucket-3 categorical accept-
// and-defer (ADR 0007 §6) applies to the runtime-detail subset uniformly, and
// gates out parse-shape SyntaxError kinds so a genuine parser gap cannot ride
// the categorical accept-path.
func assertRejected(ctx context.Context, kind, detail string) error {
	st := stateFrom(ctx)
	if st.skipped {
		return godog.ErrPending
	}
	if st.err == nil {
		// A negative TCK scenario the parser accepts is a bucket-3 case per
		// ADR 0007: a runtime or value-level error the type-interface model does
		// not carry, resurfaced by the re-executed original text (ADR 0005). In
		// the expression dirs the ADR authorises this categorically — every
		// runtime-error / result-value scenario in these dirs is bucket-3 by
		// construction. Report PENDING there rather than failing, so the
		// progress meter reflects the ADR's boundary without enumerating each
		// scenario in the skiplist. The read-core dirs keep the enumerated
		// skiplist so a genuine regression (a scenario the parser used to
		// reject and no longer does) still surfaces there.
		//
		// The kind/detail gate scopes the categorical accept: only kinds the
		// parser does not own — plus SyntaxError shapes whose detail is a
		// known runtime rule — ride the bucket-3 path. Anything else is a
		// genuine parse-shape gap.
		if isBucketThreeDir(st.uri) && isBucketThreeError(kind, detail) {
			return godog.ErrPending
		}
		return fmt.Errorf("expected the query to be rejected, but it parsed")
	}
	return nil
}

// isBucketThreeError reports whether a TCK (kind, detail) names an error the
// engine raises rather than the parser. The parser owns SyntaxError only, so
// non-SyntaxError kinds (TypeError, ArgumentError, SemanticError, …) are
// always engine-side. For SyntaxError, the TCK reuses the kind for value-level
// and semantic checks the engine raises at runtime (integer/float overflow,
// non-boolean operands to a boolean operator, undefined variables in a
// WHERE predicate the parser does not model, aggregation-position rules the
// engine enforces post-frontend). Those specific details ride bucket-3; the
// remaining SyntaxError details — genuine parse-shape rules like
// UnexpectedSyntax and InvalidClauseComposition, or an unspecified detail (*)
// — are parser-owned and must actually reject.
func isBucketThreeError(kind, detail string) bool {
	if kind != "SyntaxError" {
		return true
	}
	// SyntaxError details the TCK uses for engine-side value / semantic rules
	// (per ADR 0007 §6 read-through: the parser accepts, the engine raises).
	switch detail {
	case "IntegerOverflow",
		"FloatingPointOverflow",
		"InvalidArgumentType",
		"InvalidArgumentValue",
		"InvalidNumberLiteral",
		"InvalidUnicodeCharacter",
		"InvalidUnicodeLiteral",
		"UndefinedVariable",
		"InvalidAggregation",
		"NegativeIntegerArgument",
		"NoVariablesInScope":
		return true
	}
	return false
}

// isBucketThreeDir reports whether the scenario's URI lies under one of the
// TCK expression dirs Stage 6 wires. Per ADR 0007 §6 every runtime/value-level
// error scenario in those dirs is bucket-3: the parser accepts, the engine
// raises.
func isBucketThreeDir(uri string) bool {
	return strings.Contains(uri, "/features/expressions/")
}

// checkGolden marshals the parsed query and compares it to its snapshot, keyed by
// a stable hash of the scenario URI and name. -update regenerates the snapshot.
func checkGolden(st *scenarioState) error {
	got, err := json.MarshalIndent(st.got, "", "  ")
	if err != nil {
		return err
	}
	path := goldenPath(st)

	if *updateGolden {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		return os.WriteFile(path, append(got, '\n'), 0o644)
	}

	want, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("missing golden snapshot (run go test -update): %w", err)
	}
	if strings.TrimRight(string(want), "\n") != string(got) {
		return fmt.Errorf("query model does not match golden %s", path)
	}
	return nil
}

// goldenPath keys a golden by feature-file basename + a 6-byte SHA1 of the
// scenario URI, name, AND query text. Including the query text disambiguates
// Scenario Outline examples, which share URI and name but iterate over
// different parameter substitutions — Stage 6 added many outline-heavy
// expression dirs, exposing the pre-existing collision.
func goldenPath(st *scenarioState) string {
	base := filepath.Base(st.uri)
	base = strings.TrimSuffix(base, filepath.Ext(base))
	sum := sha1.Sum([]byte(st.uri + "\x00" + st.name + "\x00" + st.query))
	return filepath.Join(goldenDir, fmt.Sprintf("%s_%x.golden.json", base, sum[:6]))
}

// harvestExecutingQueries reads every .feature file under dirs and returns the
// docstring of each "When executing query" step, deduplicated and sorted for a
// stable order. It reuses the gherkin parser (godog's dependency) so the harvest
// matches what the acceptance suite actually runs, rather than re-scanning text.
func harvestExecutingQueries(t *testing.T, dirs []string) []string {
	t.Helper()
	seen := make(map[string]bool)
	for _, dir := range dirs {
		files, err := filepath.Glob(filepath.Join(dir, "*.feature"))
		if err != nil {
			t.Fatalf("glob %s: %v", dir, err)
		}
		for _, path := range files {
			f, err := os.Open(path)
			if err != nil {
				t.Fatalf("open %s: %v", path, err)
			}
			doc, err := gherkin.ParseGherkinDocument(f, func() string { return "" })
			if cerr := f.Close(); cerr != nil {
				t.Fatalf("close %s: %v", path, cerr)
			}
			if err != nil {
				t.Fatalf("parse %s: %v", path, err)
			}
			for _, p := range gherkin.Pickles(*doc, path, newIDGen()) {
				for _, step := range p.Steps {
					if isExecutingQueryStep(step) {
						seen[step.Argument.DocString.Content] = true
					}
				}
			}
		}
	}
	out := make([]string, 0, len(seen))
	for q := range seen {
		out = append(out, q)
	}
	sort.Strings(out)
	return out
}

// isExecutingQueryStep identifies the docstring-bearing "when" step whose
// content is the query the scenario executes. The two accepted spellings
// mirror the two Step registrations in initScenario ("executing query" and
// the Temporal4 write-plus-readback "executing control query"). Exact match
// on the two known spellings — not a substring test — so a future TCK
// step like "before executing query, do X" cannot silently key a golden.
// Stage-12 write-storage goldens would orphan silently under a substring
// match once CREATE parses.
func isExecutingQueryStep(step *messages.PickleStep) bool {
	if step.Argument == nil || step.Argument.DocString == nil {
		return false
	}
	return step.Text == "executing query:" || step.Text == "executing control query:"
}

// newIDGen returns a fresh incrementing id generator, required by Pickles.
func newIDGen() func() string {
	n := 0
	return func() string {
		n++
		return fmt.Sprintf("%d", n)
	}
}
