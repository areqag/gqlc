package conformance_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	"github.com/areqag/gqlc/internal/cli/backends"
	"github.com/areqag/gqlc/internal/codegen"
	"github.com/areqag/gqlc/internal/graph"
	"github.com/areqag/gqlc/internal/resolver"
	"github.com/areqag/gqlc/internal/schema"
)

// AssembledInputSuite covers the §2 rows whose refused construct has no
// on-disk form: a codegen.Input a consumer of the package assembles and
// hands to Generate directly, carrying a value no .gql schema, no
// .cypher query and no CLI option produces.
//
// These rows sat in §3 until gqlc-h4ug, most of them argued as "the
// resolver would never build this". That argument is about the pipeline,
// and §5.1's criterion is about the contract: Input, NamedQuery,
// ValidatedQuery, Column and every resolver.Resolved* variant are
// exported structs with exported fields, so what the resolver builds does
// not bound what a caller can hand over. The last two rows to move were
// argued the same way one level down — a switch over
// resolver.ResolvedType is total because the interface is sealed. It is
// not sealed: the unexported marker stops another package writing an
// implementation from scratch, but Go promotes an embedded type's
// unexported methods, so struct{ resolver.ResolvedNode } satisfies it
// from here and matches no case arm. The pointer forms below are the
// cheapest witness of the same opening. Each case is one such hand-off,
// and every one fired the first time it was written.
//
// The suite is the coverage §5 step 3 asks for on those rows, and it is
// half of what TestSentinelTaxonomy measures: it links internal/codegen
// from outside the package, so the fence's corpus sweep counts it.
//
// Dropping a case is not reliably caught, and the reason is worth
// stating rather than discovering. Coverage is a union over the whole
// corpus, so a case is only load-bearing while it is the sole reach of
// its fail-site. TestMarkerSealDoesNotCloseTheSum reaches the two
// unknown-variant sites by a second route, so deleting
// column-unknown-variant or list-elem-unknown-variant here leaves the
// fence green. Deleting edge-union-arity, whose site nothing else
// reaches, reddens naming prepare.go:660. What the fence guards is that
// each site has *a* witness, not that this file is it.
type AssembledInputSuite struct {
	suite.Suite

	backends codegen.Registry
}

func TestAssembledInputSuite(t *testing.T) {
	suite.Run(t, new(AssembledInputSuite))
}

func (s *AssembledInputSuite) SetupSuite() {
	reg, err := backends.Registry()
	s.Require().NoError(err)
	s.backends = reg
}

// assembledTarget is the backend these cases generate through. neo4j is
// the one that hands its Input to codegen.Prepare unfiltered — the AGE
// backend pre-gates with rejectUnservedQueries before Prepare runs, so
// several of these constructs would be answered by that gate instead and
// the case would measure the wrong refusal.
const assembledTarget = "neo4j-go-v5"

// resolverDir is the package declaring resolver.Temporal. This suite reads
// it off disk because the fact it needs is the one compilation erases: a Go
// constant has no run-time footprint, so a kind the source names and
// Temporal.String has no arm for is, to anything running, identical to a
// value nobody ever declared.
//
// That is not a hypothetical. The derivation this replaces scanned for
// the first value String answered with its default arm and called it the
// end of the enum, which is the same thing only while every declared kind
// has an arm. A seventh constant added with no arm left the scan
// answering 6, TestTemporalScanFindsTheEnumEnd comparing 6 to 6, and the
// list-elem-temporal case below feeding a kind the resolver declares
// while its `why` still said otherwise — the whole repo green. Behaviour
// cannot see a missing switch arm, and a missing switch arm is the
// commonest Go enum bug there is.
const resolverDir = "../../resolver"

// resolverAnchorFile declares the type Temporal, its iota run and
// Temporal.String. It is where this suite expects the enum to live, and
// what the sweep below requires to be among the files it read — but it is
// no longer the only file the sweep looks in, so it is not what failures
// name. Each names the file its kind was found in.
const resolverAnchorFile = "validated.go"

// resolverPath is the swept file called name, spelled the way a reader of
// a failure message opens it.
func resolverPath(name string) string { return filepath.Join(resolverDir, name) }

// temporalMember is one constant of type resolver.Temporal: the name it
// is declared under, the value it holds, and the file of resolverDir it is
// declared in.
//
// The file belongs to the member rather than to the suite because the
// sweep reads the whole package. A failure has to send its reader to the
// file the kind is actually in, and a message that reasons correctly to
// the wrong file is worse than one that names none.
type temporalMember struct {
	Name  string
	Value int
	File  string
}

// temporalKinds is resolver.Temporal's vocabulary as this suite believes
// it stands.
//
// Written out, values included, and that is the point: declaredTemporals
// derives the same list from the source, the two are held equal, and they
// can disagree. The list this replaces was read through len() and could
// not — six was six whatever the enum said. A member added, removed,
// renamed or renumbered moves the derivation and not this, and the
// failure names which.
//
// The file is written out on the same argument. All six live in the anchor
// today; a kind that appears anywhere else in the package is the escape
// the sweep was widened to catch, and pinning the file is what makes the
// diff say where it appeared rather than only that it did.
var temporalKinds = []temporalMember{
	{"TemporalDate", 0, resolverAnchorFile},
	{"TemporalTime", 1, resolverAnchorFile},
	{"TemporalLocalTime", 2, resolverAnchorFile},
	{"TemporalDateTime", 3, resolverAnchorFile},
	{"TemporalLocalDateTime", 4, resolverAnchorFile},
	{"TemporalDuration", 5, resolverAnchorFile},
}

// temporalTypeName is the type a constant must carry to be a kind. It is
// the whole of what separates the enum from the successor sentinel
// declared beside it: `TemporalCount int = iota` closes the same const
// block and is an int, so it is a count rather than a value,
// Temporal.String owes it no arm, and an exhaustive switch over Temporal
// may not have one.
const temporalTypeName = "Temporal"

// temporalScanLimit bounds the sweep over values the package names no
// constant at in TestTemporalStringerAnswersForDeclaredKindsAlone. Nothing
// derives from it; it is how far that test looks for an arm that should
// not be there.
const temporalScanLimit = 64

// temporalFallback is what resolver.Temporal.String renders a value the
// package names no constant at as. Asserted in both directions — no
// declared kind may render it, every undeclared one must — so this
// mirroring the resolver's format is what the assertions are for rather
// than something they rest on.
func temporalFallback(v int) string { return "Temporal(" + strconv.Itoa(v) + ")" }

// declaredTemporals returns every constant of type resolver.Temporal the
// package declares, with the value each holds and the file each is
// declared in.
//
// Every const declaration is swept, not just the iota run: a
// `TemporalWeek Temporal = 6` written below the block is as much a kind as
// the six inside it, and a derivation that read only the block would leave
// it with no arm in Temporal.String and nothing to say so.
//
// Every non-test source file of the package is swept, not just the anchor,
// and for the same reason one word further out. Nothing stops a kind being
// declared beside the code that switches on it, and a sweep that read one
// file would call such a kind undeclared and pass — which is what
// internal/codegen's sentinel sweep already says about reading only
// errors.go. It was not hypothetical here either: appending
// `TemporalWeek Temporal = 6` to scope.go with no arm in Temporal.String
// left the whole repo green, firstUndeclaredTemporal handing the
// list-elem-temporal case a value the resolver declares.
//
// Order is the anchor file's members in declaration order, then every
// other file's, in the filename order os.ReadDir returns. Two rules
// because there are two things to order: inside a file the source has a
// declaration order and it is the one a reader sees, while between files
// no such order exists and only the sweep's own does. Anchoring first
// rather than sorting the lot by filename is what keeps the enum's own run
// at the front — validated.go sorts last of this package's files — so a
// stray kind reads against temporalKinds as one appended member instead of
// displacing all six.
func (s *AssembledInputSuite) declaredTemporals() []temporalMember {
	return declaredTemporalsIn(s.Require(), resolverDir)
}

// sweptFile is one non-test source file of the swept package, parsed once.
// The sweep reads the package twice — once for the names that spell
// resolver.Temporal, once for the constants carrying them — and a file
// parsed twice could differ between the passes.
type sweptFile struct {
	Name string
	File *ast.File
}

// declaredTemporalsIn is the derivation above, over the package in dir.
//
// The directory is a parameter so that the sweep's own reading can be put to
// source written for the purpose. Every escape this fence has had was a
// package read wrongly rather than one it failed to find, and none of those
// is reachable through internal/resolver: pinning them there would mean
// committing a stray kind to the resolver and leaving it in the tree.
func declaredTemporalsIn(req *require.Assertions, dir string) []temporalMember {
	entries, err := os.ReadDir(dir)
	req.NoError(err,
		"cannot read %s, the package declaring resolver.Temporal; this suite derives the enum's members from its source because compilation erases them",
		dir)

	var scanned []string
	var files []sweptFile
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		scanned = append(scanned, name)

		path := filepath.Join(dir, name)
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, nil, 0)
		req.NoError(err,
			"cannot parse %s, a source file of the package declaring resolver.Temporal; this suite derives the enum's members from that source because compilation erases them",
			path)
		files = append(files, sweptFile{Name: name, File: file})
	}

	spellings := temporalSpellings(files)

	var anchorMembers, otherMembers []temporalMember
	anchored := false
	for _, swept := range files {
		var found []temporalMember
		for _, decl := range swept.File.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.CONST {
				continue
			}
			members, isRun := temporalsOf(req, gen, dir, swept.Name, spellings)
			anchored = anchored || isRun
			found = append(found, members...)
		}
		if swept.Name == resolverAnchorFile {
			anchorMembers = append(anchorMembers, found...)
			continue
		}
		otherMembers = append(otherMembers, found...)
	}
	// resolverAnchorFile is named rather than counted, on internal/codegen's
	// argument for naming errors.go in its own sweep: a sweep that lost it
	// still finds files, still finds const declarations, and — the moment
	// any second file of the package declares a kind — is still anchored and
	// still passes, reporting an enum with its whole run missing. Nothing
	// else here is specific enough to notice. It is also what separates this
	// failure from the anchored one below, which the two fixes are opposite
	// for: swept the wrong directory, versus swept the right one and found
	// no enum in it.
	req.Contains(scanned, resolverAnchorFile,
		"the temporal sweep read %v under %s, which does not include %s; the sweep ran against the wrong directory, and one that cannot see the file declaring resolver.Temporal derives the enum from whatever it did see",
		scanned, dir, resolverAnchorFile)
	req.True(anchored,
		"no file of %s declares a `<name> Temporal = iota` const block; this suite derives the enum's members from that block, and a derivation that found nothing would report an empty enum",
		dir)

	members := make([]temporalMember, 0, len(anchorMembers)+len(otherMembers))
	members = append(members, anchorMembers...)
	members = append(members, otherMembers...)

	held := make(map[int]temporalMember, len(members))
	for _, m := range members {
		first, dup := held[m.Value]
		req.False(dup,
			"%s: resolver.%s and %s: resolver.%s are both Temporal(%d), so a value no longer names one kind",
			filepath.Join(dir, first.File), first.Name, filepath.Join(dir, m.File), m.Name, m.Value)
		held[m.Value] = m
	}
	return members
}

// temporalsOf returns the resolver.Temporal constants one const
// declaration of file holds, and reports whether it anchors the enum's
// iota run.
//
// A constant's type is what decides membership, and it is not always
// written on its own line: a spec carrying no type of its own inherits
// the last one an `= iota` line named. Tracking that inheritance is what
// tells the two kinds of constant in resolver.Temporal's block apart —
// every bare line under the anchor is still a Temporal, while
// TemporalCount re-anchors to int and is therefore no member of the enum.
//
// Three spec shapes are read, and a Temporal in any other shape fails the
// suite rather than being skipped, because a shape read wrong is a value
// read wrong and the value is what holds Temporal.String's arms below:
//
//	Name Type = iota    opens a run; the value is the spec's position
//	Name                inherits the run above, at its own position
//	Name Type = <int>   stands alone, at the value it names
//
// A fourth shape is refused outright wherever it shares the anchor's
// declaration: a line naming no type at all. Deciding membership on the
// written type is sound only while every line in the block writes one,
// and the untyped constant is where it stops being sound in the direction
// that costs something. `TemporalCount = iota` is not of type Temporal,
// so this derivation would wave it through exactly as it waves the int
// form through — but unlike the int form it is *assignable* to Temporal,
// so `case TemporalCount:` compiles inside a switch over the enum, which
// is the one thing validated.go says a successor sentinel must never be.
// No other assertion here can see it: the difference between the two is
// in no value, only in the type the line declines to write.
func temporalsOf(req *require.Assertions, gen *ast.GenDecl, dir, file string, spellings map[string]bool) (members []temporalMember, anchored bool) {
	path := filepath.Join(dir, file)
	runType, runFromIota := "", false
	var untyped []string
	for i, raw := range gen.Specs {
		spec, ok := raw.(*ast.ValueSpec)
		req.True(ok, "%s: const spec %d is a shape the derivation cannot read", path, i)

		declType, fromIota := runType, runFromIota
		if len(spec.Values) > 0 {
			declType, fromIota = typeName(spec.Type), isIdent(spec.Values[0], "iota")
			runType, runFromIota = declType, fromIota
		}
		if declType == "" {
			for _, ident := range spec.Names {
				untyped = append(untyped, ident.Name)
			}
		}
		if !spellings[declType] {
			continue
		}
		anchored = anchored || fromIota

		req.Len(spec.Names, 1,
			"%s: a Temporal is declared on a line holding %d names; the derivation reads each kind's value off its own line",
			path, len(spec.Names))
		name := spec.Names[0].Name
		if name == "_" {
			continue // names no constant, and still spends its position
		}
		val := i
		if !fromIota {
			req.NotEmpty(spec.Values,
				"%s: %s inherits its value from a line that is not `= iota`, so its position in the block is not its value",
				path, name)
			val = intValue(req, spec.Values[0], name, path)
		}
		members = append(members, temporalMember{Name: name, Value: val, File: file})
	}
	if anchored && len(untyped) > 0 {
		req.Empty(untyped,
			"%s: %v share resolver.Temporal's const block and name no type, so each is an untyped constant assignable to Temporal; `case %s:` would compile inside a switch over the enum while this derivation reads it as no member of one. Give the line a type — int if it is a count, Temporal if it is a kind",
			path, untyped, untyped[0])
	}
	return members, anchored
}

// temporalSpellings is every name in the swept package that means
// resolver.Temporal: the type's own name, and each alias that resolves to
// it.
func temporalSpellings(files []sweptFile) map[string]bool {
	return map[string]bool{temporalTypeName: true}
}

// typeName is the name of the type a const spec declares, or "" when it
// declares none or declares one the derivation cannot read. It answers
// rather than asserting because the sweep visits every const declaration
// of every file it reads, and one that is not about Temporal is none of
// this suite's business.
func typeName(expr ast.Expr) string {
	ident, ok := expr.(*ast.Ident)
	if !ok {
		return ""
	}
	return ident.Name
}

// isIdent reports whether expr is the bare identifier name.
func isIdent(expr ast.Expr, name string) bool {
	ident, ok := expr.(*ast.Ident)
	return ok && ident.Name == name
}

// intValue reads the integer literal the Temporal named name is declared
// at in the file at path.
func intValue(req *require.Assertions, expr ast.Expr, name, path string) int {
	lit, ok := expr.(*ast.BasicLit)
	req.True(ok && lit.Kind == token.INT,
		"%s: resolver.%s is a Temporal declared at neither iota nor an integer literal; the derivation reads a kind's value to ask whether Temporal.String has an arm for it",
		path, name)
	v, err := strconv.Atoi(lit.Value)
	req.NoError(err, "%s: resolver.%s is declared at %s, which the derivation cannot read as a value", path, name, lit.Value)
	return v
}

// firstUndeclaredTemporal is the lowest non-negative resolver.Temporal
// the source names no constant at. A gap rather than a count: a kind
// declared outside the iota run holds a value the run's length does not
// account for, and handing a declared kind to the list-elem-temporal case
// would make it prove the opposite of what it claims.
func (s *AssembledInputSuite) firstUndeclaredTemporal() resolver.Temporal {
	held := make(map[int]bool)
	for _, m := range s.declaredTemporals() {
		held[m.Value] = true
	}
	for v := 0; ; v++ {
		if !held[v] {
			return resolver.Temporal(v)
		}
	}
}

// probeSchema is the smallest schema that indexes one node type and one
// edge type, so a case can name a type the schema does not declare
// without the entity index being empty for an unrelated reason.
func probeSchema() schema.Schema {
	ek := schema.EdgeKey{Source: "Person", KeyLabels: "KNOWS", Target: "Person"}
	return schema.Schema{
		Name:  "Probe",
		Nodes: map[graph.LabelSetKey]schema.NodeType{"Person": {KeyLabels: "Person", CompleteLabels: "Person"}},
		Edges: map[schema.EdgeKey]schema.EdgeType{ek: {EdgeKey: ek, CompleteLabels: "KNOWS"}},
	}
}

// ghostEdge is an edge key probeSchema does not declare.
func ghostEdge() schema.EdgeKey {
	return schema.EdgeKey{Source: "Ghost", KeyLabels: "GHOSTED", Target: "Ghost"}
}

// probeQuery wraps one column in an otherwise admissible query, so the
// column's own resolved type is the only axis that can refuse.
func probeQuery(col resolver.Column) codegen.NamedQuery {
	return codegen.NamedQuery{
		Name:        "Fetch",
		Cardinality: codegen.CardinalityMany,
		SourceFile:  "probe.cypher",
		SourceText:  "MATCH (n) RETURN n",
		Validated:   resolver.ValidatedQuery{Columns: []resolver.Column{col}},
	}
}

// probeParamQuery wraps one parameter beside one admissible column, so
// the parameter is the only axis that can refuse. A column is required:
// a :many query with none is refused by the cardinality-shape gate,
// which runs first.
func probeParamQuery(param resolver.ResolvedParameter) codegen.NamedQuery {
	q := probeQuery(resolver.Column{Name: "n", Type: resolver.ResolvedNode{Labels: "Person"}})
	q.Validated.Parameters = []resolver.ResolvedParameter{param}
	return q
}

func (s *AssembledInputSuite) TestAssembledInput() {
	cases := []struct {
		// name is the §3-style fail-site name the case reaches. It is
		// not a tag any more — these sites carry no //gqlc:unreachable —
		// but naming the site keeps a failing subtest pointing at one
		// return statement rather than at one sentinel shared by eight.
		name string
		in   codegen.Input
		is   error
		msg  string
		why  string
	}{
		{
			name: "entity-empty-node-labels",
			why:  "Schema.Nodes keyed by the empty LabelSetKey. schema/gql refuses one with ErrUnnamedNodeType, so no parse carries it.",
			in: codegen.Input{Schema: schema.Schema{
				Name:  "Probe",
				Nodes: map[graph.LabelSetKey]schema.NodeType{"": {}},
			}},
			is:  codegen.ErrUnnamedMultiLabelType,
			msg: "unnamed multi-label type: node type with empty label set requires an explicit Name",
		},
		{
			name: "entity-multi-label-edge",
			why:  "An EdgeKey whose KeyLabels is a conjunction. Cypher has no conjunction syntax for edge labels and schema/gql refuses the key with ErrMultiLabelEdgeType.",
			in: func() codegen.Input {
				sc := probeSchema()
				ek := schema.EdgeKey{Source: "Person", KeyLabels: "Aye&Bee", Target: "Person"}
				sc.Edges = map[schema.EdgeKey]schema.EdgeType{ek: {EdgeKey: ek, CompleteLabels: "Aye&Bee"}}
				return codegen.Input{Schema: sc}
			}(),
			is:  codegen.ErrUnnamedMultiLabelType,
			msg: "unnamed multi-label type: multi-label edge type (Person -[:Aye&Bee]-> Person) requires an explicit Name",
		},
		{
			name: "entity-empty-edge-label",
			why:  "An EdgeKey whose KeyLabels is empty; schema/gql refuses one with ErrUnnamedEdgeType.",
			in: func() codegen.Input {
				sc := probeSchema()
				ek := schema.EdgeKey{Source: "Person", KeyLabels: "", Target: "Person"}
				sc.Edges = map[schema.EdgeKey]schema.EdgeType{ek: {EdgeKey: ek}}
				return codegen.Input{Schema: sc}
			}(),
			is:  codegen.ErrUnnamedMultiLabelType,
			msg: "unnamed multi-label type: edge type with empty label requires an explicit Name",
		},
		{
			name: "cardinality-not-in-set",
			why:  "A Cardinality outside its own constant set. queryfile.parseCardinality yields the three members or refuses the annotation, so no parse produces a fourth.",
			in: codegen.Input{
				Schema: probeSchema(),
				Queries: []codegen.NamedQuery{{
					Name:        "Fetch",
					Cardinality: codegen.Cardinality(7),
					SourceFile:  "probe.cypher",
					SourceText:  "MATCH (n) RETURN n",
				}},
			},
			is:  codegen.ErrInvalidCardinality,
			msg: `invalid cardinality: query "Fetch" at position 0 has unrecognised cardinality 7`,
		},
		{
			name: "column-width",
			why:  "A column carrying a width no schema property declares. Phase Z walks the schema, so a column backed by a declared property loses there first.",
			in: codegen.Input{
				Schema:  probeSchema(),
				Queries: []codegen.NamedQuery{probeQuery(resolver.Column{Name: "x", Type: resolver.ResolvedProperty{Type: graph.TypeInt128}})},
			},
			is:  codegen.ErrUnrepresentableWidth,
			msg: `unrepresentable property width: query "Fetch" column 0 "x" has INT128`,
		},
		{
			name: "column-unknown-node",
			why:  "A ResolvedNode naming labels the schema does not declare. The resolver resolves against the same schema Phase Z indexed.",
			in: codegen.Input{
				Schema:  probeSchema(),
				Queries: []codegen.NamedQuery{probeQuery(resolver.Column{Name: "n", Type: resolver.ResolvedNode{Labels: "Ghost"}})},
			},
			is:  codegen.ErrOutOfC6Scope,
			msg: `out of C6 scope: query "Fetch" column 0 "n" references unknown node type "Ghost"`,
		},
		{
			name: "column-unknown-edge",
			why:  "A ResolvedEdge naming an edge key the schema does not declare, on the same argument.",
			in: codegen.Input{
				Schema:  probeSchema(),
				Queries: []codegen.NamedQuery{probeQuery(resolver.Column{Name: "e", Type: resolver.ResolvedEdge{EdgeKey: ghostEdge()}})},
			},
			is:  codegen.ErrOutOfC6Scope,
			msg: `out of C6 scope: query "Fetch" column 0 "e" references unknown edge type Ghost -[:GHOSTED]-> Ghost`,
		},
		{
			name: "column-unknown-variant",
			why:  "The pointer form of a variant. Every variant declares the marker and String with value receivers, so *ResolvedNode satisfies resolver.ResolvedType while `case resolver.ResolvedNode:` does not match it. The same labels in their value form are admitted. The pointer is the cheapest witness rather than the only one: the unexported marker seals the interface against nothing, since an out-of-package struct{ resolver.ResolvedNode } promotes it and reaches this same arm.",
			in: codegen.Input{
				Schema:  probeSchema(),
				Queries: []codegen.NamedQuery{probeQuery(resolver.Column{Name: "n", Type: &resolver.ResolvedNode{Labels: "Person"}})},
			},
			is:  codegen.ErrOutOfC6Scope,
			msg: `out of C6 scope: query "Fetch" column 0 "n" resolved as node`,
		},
		{
			name: "param-width",
			why:  "A parameter carrying a width no schema property declares. The resolver draws a parameter's ResolvedProperty from a schema property or from callProjectionType, and both yield widths Phase Z has passed.",
			in: codegen.Input{
				Schema: probeSchema(),
				Queries: []codegen.NamedQuery{probeParamQuery(resolver.ResolvedParameter{
					Name: "p",
					Type: resolver.ResolvedProperty{Type: graph.TypeInt128},
				})},
			},
			is:  codegen.ErrUnrepresentableWidth,
			msg: `unrepresentable property width: query "Fetch" parameter 0 $p has INT128`,
		},
		{
			name: "edge-union-arity",
			why:  "A ResolvedEdgeUnion with one candidate. The resolver collapses a lone candidate to ResolvedEdge (R3 spec §4.4).",
			in: codegen.Input{
				Schema: probeSchema(),
				Queries: []codegen.NamedQuery{probeQuery(resolver.Column{
					Name: "e",
					Type: resolver.ResolvedEdgeUnion{EdgeKeys: []schema.EdgeKey{{Source: "Person", KeyLabels: "KNOWS", Target: "Person"}}},
				})},
			},
			is:  codegen.ErrOutOfC6Scope,
			msg: `out of C6 scope: query "Fetch" column 0 "e" resolved as edgeUnion with only 1 candidate(s) — resolver invariant violated (expected >= 2)`,
		},
		{
			name: "edge-union-undeclared",
			why:  "A ResolvedEdgeUnion naming a candidate the schema does not declare. The resolver commits only declared edges.",
			in: codegen.Input{
				Schema: probeSchema(),
				Queries: []codegen.NamedQuery{probeQuery(resolver.Column{
					Name: "e",
					Type: resolver.ResolvedEdgeUnion{EdgeKeys: []schema.EdgeKey{
						{Source: "Person", KeyLabels: "KNOWS", Target: "Person"},
						ghostEdge(),
					}},
				})},
			},
			is:  codegen.ErrOutOfC6Scope,
			msg: `out of C6 scope: query "Fetch" column 0 "e" edgeUnion candidate Ghost -[:GHOSTED]-> Ghost not declared by schema`,
		},
		{
			name: "column-temporal",
			why:  "A top-level temporal column, the site the list case below mirrors one level down. It had a fixture until bd gqlc-dy40s: unrepresentable_temporal_duration_column projected duration.between(...) at apache-age-pgx-v5, the last temporal spelling AGE's dialect gate did not hold. Closing that gap refuses the text ahead of the carrier, so no query an enrolled backend parses reaches this line any more and the sentinel is in assembledOnlySentinels. Unlike that fixture this row's kind names no member of the constant block, so it is refused on every backend rather than on the one with no carrier for a duration — the LINE is the same, the argument for reaching it is not. Measured, and stated because the suite comment above says a case is only load-bearing while it is the sole reach of its site: this one is not. internal/codegen/age assembles a declared-kind temporal column of its own, so deleting this row leaves the fence green, exactly as column-unknown-variant does.",
			in: codegen.Input{
				Schema: probeSchema(),
				Queries: []codegen.NamedQuery{probeQuery(resolver.Column{
					Name: "t",
					Type: resolver.ResolvedTemporal{Kind: s.firstUndeclaredTemporal()},
				})},
			},
			is:  codegen.ErrUnrepresentableTemporal,
			msg: `unrepresentable temporal kind: query "Fetch" column 0 "t" projects temporal(Temporal(6))`,
		},
		{
			name: "list-elem-width",
			why:  "A ResolvedList over a ResolvedProperty, a shape resolveType has no arm for. The one ResolvedProperty element that a schema produces is the one Phase B splits off a LIST property, whose width Phase Z has passed.",
			in: codegen.Input{
				Schema: probeSchema(),
				Queries: []codegen.NamedQuery{probeQuery(resolver.Column{
					Name: "xs",
					Type: resolver.ResolvedList{Element: resolver.ResolvedProperty{Type: graph.TypeInt128}},
				})},
			},
			is:  codegen.ErrUnrepresentableWidth,
			msg: `query "Fetch" column 0 "xs": unrepresentable property width: list element has unrepresentable property width INT128`,
		},
		{
			name: "list-elem-temporal",
			why:  "A resolver.Temporal outside its own constant set, the same shape as the out-of-set Cardinality above. The kind is derived from that block rather than written down, so a member added upstream moves it and TestTemporalEnumIsTheOneThisSuiteKnows says so. The query path to the fail-site itself is OPEN: only AGE refuses a kind, and since bd gqlc-p6cb its pre-gate judges a list column by its ELEMENT and stands aside on a temporal one, so a projected collect(date()) reaches Prepare and is answered here (TestAgeGateAnswersBeforePrepare). What no query text produces is THIS row's kind — a resolver.Temporal naming no member of the constant block — so the row is still assembled from outside.",
			in: codegen.Input{
				Schema: probeSchema(),
				Queries: []codegen.NamedQuery{probeQuery(resolver.Column{
					Name: "xs",
					Type: resolver.ResolvedList{Element: resolver.ResolvedTemporal{Kind: s.firstUndeclaredTemporal()}},
				})},
			},
			is: codegen.ErrUnrepresentableTemporal,
			// The kind renders as Temporal(6) because it names no member of
			// the constant block, which is the whole of what this case
			// claims. The two temporal tests below are what hold that
			// rendering to the block; here it is pinned as user-facing text.
			msg: `query "Fetch" column 0 "xs": unrepresentable temporal kind: list element projects temporal(Temporal(6))`,
		},
		{
			name: "list-elem-unknown-node",
			why:  "A list element naming a node type the schema does not declare.",
			in: codegen.Input{
				Schema: probeSchema(),
				Queries: []codegen.NamedQuery{probeQuery(resolver.Column{
					Name: "xs",
					Type: resolver.ResolvedList{Element: resolver.ResolvedNode{Labels: "Ghost"}},
				})},
			},
			is:  codegen.ErrOutOfC6Scope,
			msg: `query "Fetch" column 0 "xs": out of C6 scope: list element references unknown node type "Ghost"`,
		},
		{
			name: "list-elem-unknown-edge",
			why:  "A list element naming an edge type the schema does not declare.",
			in: codegen.Input{
				Schema: probeSchema(),
				Queries: []codegen.NamedQuery{probeQuery(resolver.Column{
					Name: "xs",
					Type: resolver.ResolvedList{Element: resolver.ResolvedEdge{EdgeKey: ghostEdge()}},
				})},
			},
			is:  codegen.ErrOutOfC6Scope,
			msg: `query "Fetch" column 0 "xs": out of C6 scope: list element references unknown edge type Ghost -[:GHOSTED]-> Ghost`,
		},
		{
			name: "list-elem-unknown-variant",
			why:  "The same pointer form one level down: buildListElemPlan's switch names the same eight value forms, so the element falls past every arm — as does an embedded variant, on the same argument as the column case above.",
			in: codegen.Input{
				Schema: probeSchema(),
				Queries: []codegen.NamedQuery{probeQuery(resolver.Column{
					Name: "xs",
					Type: resolver.ResolvedList{Element: &resolver.ResolvedNode{Labels: "Person"}},
				})},
			},
			is:  codegen.ErrOutOfC6Scope,
			msg: `query "Fetch" column 0 "xs": out of C6 scope: list element has unknown resolved type node`,
		},
	}

	newGen, ok := s.backends.Lookup(assembledTarget)
	s.Require().True(ok, "no backend registered under %q", assembledTarget)

	for _, tc := range cases {
		s.Run(tc.name, func() {
			files, err := newGen("").Generate(tc.in)
			s.Nil(files, "a refused Input must emit nothing")
			s.Require().Error(err, "reaching input: %s", tc.why)
			s.Require().ErrorIs(err, tc.is)
			// Change-detection on the user-facing text, and not what
			// tells these cases apart: eight of them share
			// ErrOutOfC6Scope, and the discriminator is
			// TestReachableBranchesAreReached's line-level measurement,
			// which names a distinct prepare.go line per case and
			// reddens when one goes dark. What this pins is the wording,
			// which is contract — down to how an undeclared temporal kind
			// renders, since list-elem-temporal's whole claim is that its
			// value names no member of the enum.
			s.EqualError(err, tc.msg)
		})
	}
}

// embeddedNode and embeddedProperty implement resolver.ResolvedType from
// outside internal/resolver by embedding a variant. Go promotes an
// embedded type's unexported methods, so isResolvedType() comes along
// with ResolvedNode and the interface is satisfied in one line — from
// this package, or from any other in this module. The opening stops at
// the module edge, since internal/resolver is unimportable outside it.
// That does not narrow the switch's problem: the switch lives in
// internal/codegen, so every package that can reach it can also write
// this type.
//
// The compile-time assertions below are half the point. If they ever
// stop compiling, resolver.ResolvedType has become genuinely closed and
// §5.1 step 5 needs rewriting again, in the other direction.
type embeddedNode struct{ resolver.ResolvedNode }

type embeddedProperty struct{ resolver.ResolvedProperty }

var (
	_ resolver.ResolvedType = embeddedNode{}
	_ resolver.ResolvedType = embeddedProperty{}
)

// TestMarkerSealDoesNotCloseTheSum pins the fact §5.1 step 5 rests on and
// two rounds of review got wrong: an unexported marker method does not
// seal an interface whose implementations are exported.
//
// The taxonomy called the switches over resolver.ResolvedType total,
// counting eight variants. A review round found the pointer forms and
// made it sixteen. Both numbers were answers to the wrong question —
// there is no count, because an out-of-package caller reaches these arms
// with a struct literal and an embedded field, and can write as many as
// it likes. That is why §3 has no **Total** row and why step 5 tells a
// classifier not to count but to name the check that answers first.
//
// The three sites are the ones §2 and §3 argue over: Phase A's column
// switch, buildListElemPlan's element switch, and Phase A's parameter
// type assertion. Each answers with its own message, so a switch that
// grew an arm matching the interface itself — the shape of somebody
// deciding the sum is closed after all — reddens here by name.
func (s *AssembledInputSuite) TestMarkerSealDoesNotCloseTheSum() {
	newGen, ok := s.backends.Lookup(assembledTarget)
	s.Require().True(ok, "no backend registered under %q", assembledTarget)

	cases := []struct {
		name string
		col  resolver.Column
		par  *resolver.ResolvedParameter
		msg  string
	}{
		{
			name: "column-unknown-variant",
			col:  resolver.Column{Name: "n", Type: embeddedNode{resolver.ResolvedNode{Labels: "Person"}}},
			msg:  `out of C6 scope: query "Fetch" column 0 "n" resolved as node`,
		},
		{
			name: "list-elem-unknown-variant",
			col: resolver.Column{
				Name: "xs",
				Type: resolver.ResolvedList{Element: embeddedNode{resolver.ResolvedNode{Labels: "Person"}}},
			},
			msg: `query "Fetch" column 0 "xs": out of C6 scope: list element has unknown resolved type node`,
		},
		{
			name: "param-non-property",
			par:  &resolver.ResolvedParameter{Name: "p", Type: embeddedProperty{}},
			msg:  `out of C6 scope: query "Fetch" parameter 0 $p resolved as property: (non-property parameters are post-v1)`,
		},
	}

	for _, tc := range cases {
		s.Run(tc.name, func() {
			q := probeQuery(tc.col)
			if tc.par != nil {
				q = probeParamQuery(*tc.par)
			}
			files, err := newGen("").Generate(codegen.Input{Schema: probeSchema(), Queries: []codegen.NamedQuery{q}})
			s.Nil(files, "a refused Input must emit nothing")
			s.Require().Error(err, "an embedded variant satisfies resolver.ResolvedType and must reach this fail-site")
			s.Require().ErrorIs(err, codegen.ErrOutOfC6Scope)
			s.EqualError(err, tc.msg)
		})
	}
}

// TestTemporalEnumIsTheOneThisSuiteKnows holds the resolver.Temporal
// constants the source declares against the members written out above. It
// is what makes list-elem-temporal's Kind an undeclared one: that case's
// value is the first gap in the derived set, so a kind added upstream
// moves it silently unless something notices the enum grew.
//
// Set equality on name and value together, not a count and not the names
// alone. A seventh member appended and a member inserted in the middle are
// the same number and not the same enum — the second renumbers every kind
// after it, so every value this suite hands over means something else —
// and a kind declared outside the iota run has a value its position does
// not give. A count tells none of them apart; this names the member that
// moved and what it moved to.
//
// The hint reads the gap rather than the count. They agree only while the
// kinds run 0..n-1 with nothing outside the run, which is the very
// assumption a member declared away from the iota run breaks — and such a
// member is the case this test exists for, so a hint sized by len() would
// print the wrong rendering exactly when it is read.
func (s *AssembledInputSuite) TestTemporalEnumIsTheOneThisSuiteKnows() {
	s.Equal(temporalKinds, s.declaredTemporals(),
		"resolver.Temporal's constants are no longer the enum this suite was written against; update temporalKinds, then check that list-elem-temporal still reaches its fail-site and that its expected message still names %q",
		temporalFallback(int(s.firstUndeclaredTemporal())))
}

// TestTemporalStringerAnswersForDeclaredKindsAlone holds Temporal.String's
// arms against the constants declared of that type, in both directions,
// and it is the half of this that behaviour can see.
//
// A declared kind that renders as the fallback has no arm — the seventh
// constant somebody adds and forgets to handle, wherever they write it.
// An undeclared value that renders as anything else has an arm it should
// not — a case left behind by a retired kind, or one written for a
// constant never declared. Both used to be invisible: the default arm
// returned "date", a declared member's own tag, so the two populations
// were indistinguishable by construction and every value off the end of
// the enum claimed to be TemporalDate.
//
// The undeclared sweep skips the values kinds hold rather than starting
// past the last of them, so a kind declared away from the iota run is
// measured as declared in both directions instead of being demanded to
// render as undeclared.
func (s *AssembledInputSuite) TestTemporalStringerAnswersForDeclaredKindsAlone() {
	declared := s.declaredTemporals()

	tagOf := make(map[string]temporalMember, len(declared))
	held := make(map[int]bool, len(declared))
	for _, m := range declared {
		held[m.Value] = true
		tag := resolver.Temporal(m.Value).String()
		s.NotEqual(temporalFallback(m.Value), tag,
			"%s: resolver.%s is declared but resolver.Temporal.String has no arm for it, so it renders as the form reserved for undeclared values; add the case",
			resolverPath(m.File), m.Name)
		first, dup := tagOf[tag]
		s.False(dup, "%s: resolver.%s and %s: resolver.%s both render %q, so the wire tag no longer identifies the kind",
			resolverPath(first.File), first.Name, resolverPath(m.File), m.Name, tag)
		tagOf[tag] = m
	}

	for v := range temporalScanLimit {
		if held[v] {
			continue
		}
		s.Equal(temporalFallback(v), resolver.Temporal(v).String(),
			"resolver.Temporal(%d) names no constant but resolver.Temporal.String answers for it; either the constant is missing or the arm is stale, and either way a value nothing declares is rendering as though something does",
			v)
	}
	s.Equal(temporalFallback(-1), resolver.Temporal(-1).String(),
		"no member of an iota run is negative, so resolver.Temporal(-1) must reach the default arm")
}

// TestAgeGateAnswersBeforePrepare measures which of the two gates answers
// a list column on the Apache AGE target, which is what §2's
// list-elem-temporal row rests on.
//
// The row once said Phase Z for three stages and Phase Z was never
// involved — it walks the widths of schema properties, and the element of
// a collect(...) column comes from an expression, so a schema declaring
// no LIST property gives Phase Z nothing to refuse. The gate that answers
// is age.rejectUnservedQueries, which generate calls before
// codegen.Prepare.
//
// What CHANGED under bd gqlc-p6cb is which list columns that gate keeps.
// It used to drop every one of them, which closed the query path to
// Prepare's list-element fail-sites on this target; it now judges a list
// column by its ELEMENT, so it keeps the ones whose element it has a
// decode arm for and stands aside on a temporal element — exactly as the
// top-level temporal arm does, and for the same reason: Prepare names the
// KIND and this gate reports one reason per query. So the second row
// below reaches the fail-site the first row's ancestor used to shadow,
// and the taxonomy row's "query path closed" claim went with it.
//
// Asserted on the message rather than on age.ErrUnsupportedQuery: this
// package sits above the backends and resolves them through the composed
// registry, so importing one would break the rule that keeps it there.
// The messages name their gate unambiguously — no phase inside codegen
// produces the first wording, and no backend gate produces the second.
func (s *AssembledInputSuite) TestAgeGateAnswersBeforePrepare() {
	const ageTarget = "apache-age-pgx-v5"
	newGen, ok := s.backends.Lookup(ageTarget)
	s.Require().True(ok, "no backend registered under %q", ageTarget)

	generate := func(elem resolver.ResolvedType) error {
		files, err := newGen("").Generate(codegen.Input{
			Schema: probeSchema(),
			Queries: []codegen.NamedQuery{probeQuery(resolver.Column{
				Name: "xs",
				Type: resolver.ResolvedList{Element: elem},
			})},
		})
		s.Nil(files)
		s.Require().Error(err)
		return err
	}

	s.Run("an element with no decode arm is dropped by the backend gate", func() {
		// A list of whole vertices. The gate is what answers, and it
		// answers before Prepare: Prepare's own refusal for this shape
		// would name the node type the probe schema does not declare.
		err := generate(resolver.ResolvedNode{Labels: "Ghost"})
		s.EqualError(err, `unsupported query: the Apache AGE backend serves scalar and entity columns, `+
			`so 1 query would be dropped: Fetch (column "xs" projects a list of node)`)
	})

	s.Run("a temporal element yields, so Prepare's list-element fail-site is reached", func() {
		// A collect(date()) column: a list whose element is a declared
		// temporal kind. AGE has no carrier for any temporal kind, and
		// the gate stands aside so that the answer names the kind.
		err := generate(resolver.ResolvedTemporal{Kind: resolver.TemporalDate})
		s.EqualError(err, `query "Fetch" column 0 "xs": unrepresentable temporal kind: `+
			`list element projects temporal(date), which the Apache AGE backend has no carrier for`)
	})
}
