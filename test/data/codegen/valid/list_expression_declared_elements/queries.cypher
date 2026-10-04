// A list EXPRESSION column — a list literal or collect() — whose element is a
// property of a width the element's Go text does not identify (bd gqlc-k1dg).
// A closed union carries as `any`, exactly as ANY VALUE does, and a record as
// an anonymous struct; only the width tells which decoder the element needs.
// A list expression column carries no width of its own (codegen.Row.Width is
// set for a schema property), so the element's width has to be read off its
// ListElem plan. Before that, the union columns decoded through
// agtypeListOfAny / agtypeValue — a DATE member came back as its ISO string
// and an INT32 member as int64 — and the record columns failed generation
// with an "age codegen bug" naming the struct text.
//
// A list wrapper's doc line named a nested record element by its raw struct
// text, which split the comment and failed formatting. [a.moves] reached that
// through the fix, and a stored LIST<LIST<RECORD {…}>> — nest — through no
// list expression at all.
//
// n32s is the control: a narrow scalar's Go text names its decoder, so it was
// narrowed before the fix and is unchanged by it.
//
// AGE alone, because the two neo4j majors refuse to store a RECORD property.

// name: AccountLiterals :many
MATCH (a:Account) RETURN [a.either] AS eithers, [a.narrow] AS narrows, [a.n32] AS n32s, [a.home] AS homes

// name: AccountCollected :many
MATCH (a:Account) RETURN collect(a.either) AS eithers, collect(a.home) AS homes

// name: AccountNested :many
MATCH (a:Account) RETURN [a.dates] AS dates, [a.moves] AS moves, a.nest AS nest

// name: CreateAccount :exec
CREATE (a:Account {id: $id, either: $either, narrow: $narrow, n32: $n32, home: $home, moves: $moves, dates: $dates, nest: $nest})
