package resolver

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/areqag/gqlc/internal/graph"
	"github.com/areqag/gqlc/internal/procsig"
	"github.com/areqag/gqlc/internal/query"
	"github.com/areqag/gqlc/internal/schema"
)

// resolve is the R5 kernel: it walks q.Branches left-to-right, resolves each
// branch's Part chain via resolveBranch, certifies branch-0 column compatibility
// against every other branch (§4.3), witnesses each parameter Use against the
// exact lexical Part's scope in its lexical branch (fvo per ADR 0008 amendment
// 2026-07-06; the pre-fvo any-valid-witness rule at §4.2.4 retires), and folds
// Part.Distinct + UnionKind into ValidatedQuery.Distinct (§3.2/§4.7).
func resolve(q query.Query, s schema.Schema, r procsig.Registry) (ValidatedQuery, error) {
	if len(q.Branches) == 0 {
		// Defensive tripwire; the parser's buildBranch guarantees >= 1
		// (Query is a builder-maintained product type). Unreachable via parse.
		return ValidatedQuery{}, fmt.Errorf("%w: empty branches", ErrOutOfR0Scope)
	}

	branchCols := make([][]Column, len(q.Branches))
	branchScopeTables := make([][]partScope, len(q.Branches))

	for b, branch := range q.Branches {
		cols, uses, err := resolveBranch(branch, s, r)
		if err != nil {
			return ValidatedQuery{}, err
		}
		branchCols[b] = cols
		branchScopeTables[b] = useSitesToScopes(uses)
	}

	if err := compareBranchColumns(branchCols); err != nil {
		return ValidatedQuery{}, err
	}

	params, err := unifyParameterUsesAcrossBranches(q.Parameters, branchScopeTables, s)
	if err != nil {
		return ValidatedQuery{}, err
	}

	return ValidatedQuery{
		Columns:    branchCols[0],
		Parameters: params,
		Statement:  StatementKind(q.StatementKind),
		Distinct:   computeDistinct(q),
	}, nil
}

// partScope captures the resolver-typed binding tables in effect at one Part —
// enough for the top-level parameter walker to witness every Use against the
// Part whose Ref names an in-scope binding. Threaded out of resolveBranch via
// parameterUseSite (one site per Part; the caller reconstructs scopes at
// walk time).
//
// Methods (Contains, PropertyUseWitness, WitnessUse) live in scope.go
// alongside partScope's producer (scope.Snapshot).
type partScope struct {
	nodeTypes       map[string]schema.NodeType
	nodeCands       map[string][]schema.NodeType
	edgeTypes       map[string]schema.EdgeType
	edgeCands       map[string][]schema.EdgeKey
	edgeBindings    map[string]query.EdgeBinding
	nullableBinding map[string]bool
}

// useSitesToScopes is the adapter from resolveBranch's []parameterUseSite (its
// pinned second return) to a []partScope the top-level walker consumes. Every
// parameterUseSite in R5 wraps one Part's scope snapshot.
func useSitesToScopes(sites []parameterUseSite) []partScope {
	out := make([]partScope, 0, len(sites))
	for _, s := range sites {
		out = append(out, s.scope)
	}
	return out
}

// branchState is the resolver-typed carry from Part K to Part K+1 within one
// branch (§4.2.1). All maps nil for Part 0 (empty carry).
//
// exportedOptionalGroup carries the OPTIONAL-group id of a WITH-carried
// binding across the Part boundary. Group ids are per-query and unique across
// the whole parse (§3.3), so a carried id cannot collide with a local id in
// the downstream Part. Demotion of a member proven in Part K+1 pulls the
// whole carried group via the ay9 fixed-point in demoteNullableInPlace.
type branchState struct {
	exportedNodeTypes       map[string]schema.NodeType
	exportedNodeCands       map[string][]schema.NodeType
	exportedEdgeTypes       map[string]schema.EdgeType
	exportedEdgeKeys        map[string]schema.EdgeKey
	exportedEdgeCands       map[string][]schema.EdgeKey
	exportedEdgeBindings    map[string]query.EdgeBinding
	exportedNullableBinding map[string]bool
	exportedOptionalGroup   map[string]int
	exportedResolvedTypes   map[string]ResolvedType
	exportedCallTypes       map[string]callBindingSlot
	exportedOrder           []string
}

// callBindingSlot carries the resolver-side view of a CallBinding at a Part's
// Phase A1: the bridged Stage-6 result type (spec §4.1).
type callBindingSlot struct {
	resultType query.Type
}

// parameterUseSite is resolveBranch's second-return element (pinned type per
// R5 §2.2). In this implementation each site carries one Part's resolved-scope
// snapshot — enough for the top-level unifier to witness every Use against
// the scope its emission-time (branch, part) attribution selects (§4.2.4).
// Witnessing runs at the top-level resolve() after every branch has resolved
// its Parts, because a Use may live in any branch.
type parameterUseSite struct {
	scope partScope
}

// resolveBranch walks a branch's Parts left-to-right, threading a branchState
// carry. Returns the final Part's resolved column list (with grouping-key bits
// filled), the aggregated parameter-Use witnesses collected across every Part,
// and the first failure encountered.
//
// Pinned signature per R5 §2.2 / team-lead brief.
func resolveBranch(branch query.Branch, s schema.Schema, r procsig.Registry) ([]Column, []parameterUseSite, error) {
	if len(branch.Parts) == 0 {
		// Defensive tripwire; parser's buildBranch guarantees >= 1.
		return nil, nil, fmt.Errorf("%w: empty parts", ErrOutOfR0Scope)
	}

	var carry branchState
	var allUses []parameterUseSite
	var finalCols []Column
	var finalPart query.Part
	lastIdx := len(branch.Parts) - 1

	for k, part := range branch.Parts {
		cols, exported, uses, err := resolvePart(part, carry, s, r)
		if err != nil {
			return nil, nil, err
		}
		allUses = append(allUses, uses...)
		carry = exported
		if k == lastIdx {
			finalCols = cols
			finalPart = part
		}
	}

	// Grouping-key discovery runs only for the final Part (§4.5). The
	// per-column bit lives on ValidatedQuery.Columns (§3.2.1); no other
	// consumer reads it. Non-final Parts fold via exportedResolvedTypes.
	fillGroupingKeys(finalCols, finalPart)
	return finalCols, allUses, nil
}

// resolvePart runs the per-Part kernel: R4's Phase A/B/C for the local
// bindings, R4's Phase D nullability with a carry-seed extension (§4.6),
// carried-scope-seeded binding tables (§4.2.3), projection walk with
// AggregateProjection support (§4.5) and RETURN * / WITH * expansion (§4.4),
// and per-Part parameter-Use witness collection (§4.2.4). Returns the Part's
// column list (unfilled GroupingKey; filled by resolveBranch on the final
// Part), the branchState exported to Part K+1 (§4.2.2), and the parameter-Use
// witnesses collected inside this Part.
func resolvePart(part query.Part, carry branchState, s schema.Schema, r procsig.Registry) ([]Column, branchState, []parameterUseSite, error) {
	sc := newScope(carry)
	sc.Ingest(part)

	// Phase A1: local labelled-node / edge / call admission. The
	// unlabelled-node arm defers to Phase B (InferUnlabelled reads
	// s.bindings and picks them up). Bind* runs the cross-lane shadow
	// cascade and the R5/R7 conflict checks.
	for _, b := range sc.bindings {
		switch bb := b.(type) {
		case query.NodeBinding:
			if len(bb.Labels()) == 0 {
				continue
			}
			nts, err := resolveNodeLabels(bb.Labels(), s)
			if err != nil {
				return nil, branchState{}, nil, err
			}
			if len(nts) == 1 {
				if err := sc.BindNode(bb, nts[0]); err != nil {
					return nil, branchState{}, nil, err
				}
			} else {
				if err := sc.BindNodeCands(bb, nts); err != nil {
					return nil, branchState{}, nil, err
				}
			}
		case query.EdgeBinding:
			if err := sc.BindEdge(bb); err != nil {
				return nil, branchState{}, nil, err
			}
		case query.CallBinding:
			if err := sc.BindCall(bb, r); err != nil {
				return nil, branchState{}, nil, err
			}
		default:
			return nil, branchState{}, nil, fmt.Errorf("%w: %s binding", ErrOutOfR0Scope, b.Kind())
		}
	}

	// Phase A2 (defer unfulfilled endpoint edges) + Phase B (infer
	// unlabelled nodes) + Phase C (retry deferred edges), all
	// scope-internal per spec §2.2.
	if err := sc.CloseEdges(s); err != nil {
		return nil, branchState{}, nil, err
	}

	// Phase D (§4.6): seed with carry, override with local, then demote.
	// Nullability seed already ran at newScope (the carry's nullable
	// bindings landed in sc.nullableBinding). Local Bindings override
	// the carry before demotion runs so Part K+1 that re-MATCHes an
	// OPTIONAL-carried `b` sees sc.nullableBinding["b"] = false.
	sc.SeedLocalNullability()
	sc.DemoteNullability()

	// Phase E (R6 §4.1): effect validation. Runs after Phase D so effect
	// targets see the same schema-committed binding tables and
	// effective-nullability map that the projection walk sees. First
	// failure short-circuits.
	if err := sc.ValidateEffects(s); err != nil {
		return nil, branchState{}, nil, err
	}

	// Projection walk (§4.4): scopeOrder + materialiseReturns +
	// per-item projectionType, all on scope. Populates sc.scopeOrder,
	// sc.items, sc.columns for Export.
	if err := sc.ResolveProjections(s); err != nil {
		return nil, branchState{}, nil, err
	}

	// Emit this Part's scope snapshot as one parameterUseSite. The
	// top-level unifier walks every parameter's Uses against every
	// scope; a PropertyUse witnesses at the scope whose tables contain
	// its Ref's binding (§4.2.4).
	site := parameterUseSite{scope: sc.Snapshot()}

	// Build the exported branchState for Part K+1. R7 §4.6 adds the
	// exportedCallTypes lane for CALL YIELD carry-forward. Names
	// surviving WITH keep their Part-K OPTIONAL group id via
	// exportedOptionalGroup, so downstream Parts can close cross-Part
	// group demotion via the ay9 fixed point.
	return sc.columns, sc.Export(), []parameterUseSite{site}, nil
}

// fillGroupingKeys populates Column.GroupingKey for branch 0's final Part per
// §4.5.2. Grouping mode is entered when Returns contains at least one
// aggregate — either as a top-level AggregateProjection OR embedded inside
// an ExprProjection (ContainsAggregate() == true). In grouping mode,
// ExprProjection is a grouping key iff ContainsAggregate() == false
// (ADR 0008 amendment 2026-07-06).
func fillGroupingKeys(cols []Column, part query.Part) {
	// ReturnsAll and a non-empty Returns are NOT mutually exclusive: a
	// standalone CALL ... YIELD sets both, with Returns holding the yielded
	// items index-aligned to cols. What does hold is the weaker claim —
	// neither a `RETURN *` expansion (Returns empty) nor a YIELD item list
	// can carry an AggregateProjection, so the hasAggregate gate below could
	// not fire for a ReturnsAll Part regardless.
	if part.ReturnsAll {
		return
	}
	hasAggregate := false
	for _, item := range part.Returns {
		switch v := item.Value.(type) {
		case query.AggregateProjection:
			hasAggregate = true
		case query.ExprProjection:
			if v.ContainsAggregate() {
				hasAggregate = true
			}
		}
		if hasAggregate {
			break
		}
	}
	if !hasAggregate {
		return
	}
	// Grouping applies. Non-aggregate items are keys; ExprProjection is a
	// key iff it does NOT contain a nested aggregate.
	for i, item := range part.Returns {
		switch v := item.Value.(type) {
		case query.RefProjection, query.LiteralProjection, query.FuncProjection:
			cols[i].GroupingKey = true
		case query.ExprProjection:
			if !v.ContainsAggregate() {
				cols[i].GroupingKey = true
			}
		}
		// AggregateProjection stays false (the aggregate itself is not a key).
	}
}

// unionColumnTypeArm is the phrase that separates ErrUnionColumnMismatch's type
// arm from its count and name arms. All three carry the same sentinel, so
// errors.Is cannot tell them apart, and the resolver test derives which invalid
// fixtures reach the type arm by matching this phrase in the message. It is
// spliced into the format string below rather than transcribed into the sweep's
// regexp, so the two cannot drift apart and leave the coverage sweep matching
// nothing.
//
// That same splice is why the sweep cannot also be what pins the phrase: built
// from this constant, it admits the same fixture set whatever the phrase says,
// and rewriting this line to " zzz " once left the entire suite green. The one
// literal copy is invalidFixtureContains["union_column_type_mismatch.cypher"] —
// an invalid fixture, so it carries no golden and -update cannot reach it.
const unionColumnTypeArm = " projects "

// compareBranchColumns runs the R5 UNION column compatibility check (§4.3).
// Every branch's column list must equal branch 0's index-wise on count, name,
// and type (strict Go-value equality; no lattice widening across branches).
//
// All three arms name the failing side of the comparison before branch 0. The
// count and name arms lead with it outright; the type arm leads with the column,
// which is the one thing the two branches agree on, and then gives the failing
// branch's projection first. The direction is arbitrary; the consistency is not,
// because one error that reads in two directions makes the reader re-derive
// which number is the culprit on every arm.
func compareBranchColumns(branchCols [][]Column) error {
	if len(branchCols) < 2 {
		return nil
	}
	base := branchCols[0]
	for b := 1; b < len(branchCols); b++ {
		other := branchCols[b]
		if len(other) != len(base) {
			return fmt.Errorf("%w: branch %d has %d columns; branch 0 has %d", ErrUnionColumnMismatch, b, len(other), len(base))
		}
		for i := range base {
			if other[i].Name != base[i].Name {
				return fmt.Errorf("%w: branch %d column %d named %q; branch 0 column %d named %q", ErrUnionColumnMismatch, b, i, other[i].Name, i, base[i].Name)
			}
			if !resolvedTypeEqual(other[i].Type, base[i].Type) {
				return fmt.Errorf("%w: column %q"+unionColumnTypeArm+"%s in branch %d but %s in branch 0", ErrUnionColumnMismatch, base[i].Name, describeColumnType(other[i].Type), b, describeColumnType(base[i].Type))
			}
		}
	}
	return nil
}

// describeColumnType renders a resolved column type for a fail-message that
// has to tell two of them apart: the variant's tag, plus every axis
// resolvedTypeEqual compares it on.
//
// The Stringers alone cannot do this. They are wire tags — the "kind"
// discriminator each MarshalJSON emits — so every ResolvedNode renders "node"
// whichever type it holds and every ResolvedEdgeUnion renders "edgeUnion"
// whichever keys it committed, and a message built from them prints the same
// text on both sides of a mismatch.
//
// The braces around the union key list are readability, not distinctness: a
// strict prefix is never equal to its extension, and nullabilityNote already
// terminates the list, so removing them collides nothing. They are here so the
// reader can see where one branch's key list ends.
func describeColumnType(t ResolvedType) string {
	switch v := t.(type) {
	case ResolvedNode:
		return v.String() + " " + string(v.Labels) + nullabilityNote(v.Nullable)
	case ResolvedEdge:
		return v.String() + " " + formatEdgeKey(v.EdgeKey) + nullabilityNote(v.Nullable)
	case ResolvedEdgeUnion:
		return v.String() + " {" + formatEdgeKeys(v.EdgeKeys) + "}" + nullabilityNote(v.Nullable)
	case ResolvedProperty:
		return v.String() + nullabilityNote(v.Nullable)
	case ResolvedList:
		return v.String() + " of " + describeColumnType(v.Element)
	default:
		return t.String()
	}
}

// nullabilityNote spells the nullability axis on both settings rather than
// annotating only the nullable one: two column types that differ on nothing
// else must render as different text.
func nullabilityNote(nullable bool) string {
	if nullable {
		return " (nullable)"
	}
	return " (not null)"
}

// resolvedTypeEqual is Go-value equality for ResolvedType. Rendering to their
// stable MarshalJSON output would work too, but a variant-by-variant check is
// direct and avoids the allocation.
func resolvedTypeEqual(a, b ResolvedType) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	switch aa := a.(type) {
	case ResolvedNode:
		bb, ok := b.(ResolvedNode)
		return ok && aa == bb
	case ResolvedProperty:
		bb, ok := b.(ResolvedProperty)
		return ok && aa == bb
	case ResolvedEdge:
		bb, ok := b.(ResolvedEdge)
		return ok && aa == bb
	case ResolvedEdgeUnion:
		bb, ok := b.(ResolvedEdgeUnion)
		if !ok || aa.Nullable != bb.Nullable || len(aa.EdgeKeys) != len(bb.EdgeKeys) {
			return false
		}
		for i := range aa.EdgeKeys {
			if aa.EdgeKeys[i] != bb.EdgeKeys[i] {
				return false
			}
		}
		return true
	case ResolvedScalar:
		bb, ok := b.(ResolvedScalar)
		return ok && aa == bb
	case ResolvedTemporal:
		bb, ok := b.(ResolvedTemporal)
		return ok && aa == bb
	case ResolvedList:
		bb, ok := b.(ResolvedList)
		return ok && resolvedTypeEqual(aa.Element, bb.Element)
	case ResolvedUnknown:
		_, ok := b.(ResolvedUnknown)
		return ok
	default:
		return false
	}
}

// computeDistinct folds Part.Distinct across every branch × Part with the
// UnionKind ∈ Combinators fold (§3.2 / §4.7).
func computeDistinct(q query.Query) bool {
	for _, branch := range q.Branches {
		for _, part := range branch.Parts {
			if part.Distinct {
				return true
			}
		}
	}
	for _, c := range q.Combinators {
		if c == query.UnionDistinct {
			return true
		}
	}
	return false
}

// unifyParameterUsesAcrossBranches witnesses each parameter Use against its
// emission-attributed branch's Part-indexed scope table — Use.Branch selects
// the branch (gqlc-qcc per ADR 0008 amendment 2026-07-12), Use.Part the Part
// within it (fvo per ADR 0008 amendment 2026-07-06). Witnesses are unified
// via R2's lattice; the first conflict fires ErrParameterTypeConflict.
func unifyParameterUsesAcrossBranches(params []query.Parameter, tables [][]partScope, s schema.Schema) ([]ResolvedParameter, error) {
	if len(params) == 0 {
		return []ResolvedParameter{}, nil
	}
	if len(tables) == 0 {
		return nil, fmt.Errorf("%w: no branch scope tables", ErrOutOfR0Scope)
	}
	out := make([]ResolvedParameter, 0, len(params))
	for _, p := range params {
		var unified ResolvedType
		seen := false
		for _, u := range p.Uses {
			b := u.Branch()
			if b < 0 || b >= len(tables) {
				// Defensive: the parser attributes to a valid query-level
				// branch index by construction — same posture as the Part
				// guard in witnessAcrossScopes.
				return nil, fmt.Errorf("%w: Use Branch index %d out of range for query with %d branches", ErrOutOfR0Scope, b, len(tables))
			}
			sc, err := selectPartScope(u, tables[b])
			if err != nil {
				return nil, err
			}
			ws, err := sc.WitnessUse(u, s)
			if err != nil {
				return nil, err
			}
			for _, w := range ws {
				if !seen {
					unified = w
					seen = true
					continue
				}
				merged, ok := unify(unified, w)
				if !ok {
					return nil, fmt.Errorf("%w: parameter %q: %s vs %s", ErrParameterTypeConflict, p.Name, unified.String(), w.String())
				}
				unified = merged
			}
		}
		if !seen {
			unified = ResolvedUnknown{}
		}
		out = append(out, ResolvedParameter{Name: p.Name, Type: unified})
	}
	return out, nil
}

// selectPartScope resolves which partScope the unifier should dispatch to
// for a given Use. PropertyUse needs the Part-attributed scope so its Ref
// hits the correct binding tables; the range check that used to live inside
// witnessAcrossScopes moves here (parser attribution is authoritative — an
// out-of-range index is decoder / model corruption). ClauseSlotUse and
// ExprUse are Part-agnostic in their type witness (the Part axis is a
// lexical-attribution property for future consumer stages, not a witness
// discriminator today), so any scope value works — a zero partScope keeps
// the WitnessUse receiver argument-typed without touching branchScopes.
func selectPartScope(u query.Use, branchScopes []partScope) (partScope, error) {
	pu, ok := u.(query.PropertyUse)
	if !ok {
		return partScope{}, nil
	}
	idx := pu.Part()
	if idx < 0 || idx >= len(branchScopes) {
		return partScope{}, fmt.Errorf("%w: PropertyUse Part index %d out of range for branch with %d Parts", ErrOutOfR0Scope, idx, len(branchScopes))
	}
	return branchScopes[idx], nil
}

// r3EdgeAdmissible screens an EdgeBinding against R3's edge shape predicate:
// labelled (at least one type). Every R3 shape — directed or undirected,
// single-hop or var-length, single-type or multi-type — is admitted; untyped
// edges route to ErrOutOfR0Scope (R-later takes them up).
func r3EdgeAdmissible(e query.EdgeBinding) error {
	if len(e.Labels()) == 0 {
		return fmt.Errorf("%w: untyped edge", ErrOutOfR0Scope)
	}
	return nil
}

// edgeProbes enumerates the EdgeKeys one edge binding could name: its
// labels × the endpoint cross-product × the orientations its Directed()
// marker admits. srcs and tgts are slices to support plural node
// satisfaction (ADR 0022).
//
// The result is a set, not a bag. Distinct probes can name one key — for
// an undirected edge with src == tgt the reversed orientation reproduces
// the forward one — and both consumers read the result as "the distinct
// edge types in play": §4.6's verdict table dispatches on the count, and
// the fail-message enumerates them for a reader who must tell them apart.
// A repeat makes the first claim a second edge type and the second claim
// that an edge is ambiguous against itself.
//
// Order is first occurrence. The probe order is fixed by the query's label
// order, the endpoint slices, and the constant orientation pair; the seen
// set is membership-only and never iterated, so the result is
// deterministic across runs (§4.4).
func edgeProbes(e query.EdgeBinding, srcs, tgts []graph.LabelSetKey) []schema.EdgeKey {
	out := make([]schema.EdgeKey, 0, len(e.Labels()))
	seen := make(map[schema.EdgeKey]struct{}, len(e.Labels()))
	for _, L := range e.Labels() {
		labelKey := graph.LabelSet{L}.Key()
		for _, src := range srcs {
			for _, tgt := range tgts {
				orientations := [][2]graph.LabelSetKey{{src, tgt}}
				if !e.Directed() {
					orientations = append(orientations, [2]graph.LabelSetKey{tgt, src})
				}
				for _, o := range orientations {
					k := schema.EdgeKey{Source: o[0], KeyLabels: labelKey, Target: o[1]}
					if _, dup := seen[k]; dup {
						continue
					}
					seen[k] = struct{}{}
					out = append(out, k)
				}
			}
		}
	}
	return out
}

// edgeCandidates is the closed candidate set for one edge binding whose
// endpoint keys are already committed: the probes the schema declares,
// in probe order.
func edgeCandidates(e query.EdgeBinding, srcs, tgts []graph.LabelSetKey, s schema.Schema) []schema.EdgeKey {
	out := make([]schema.EdgeKey, 0, len(e.Labels()))
	for _, k := range edgeProbes(e, srcs, tgts) {
		if _, ok := s.Edges[k]; ok {
			out = append(out, k)
		}
	}
	return out
}

func formatEdgeKey(k schema.EdgeKey) string {
	return fmt.Sprintf("%s-[%s]->%s", k.Source, k.KeyLabels, k.Target)
}

func formatEdgeKeys(keys []schema.EdgeKey) string {
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = formatEdgeKey(k)
	}
	return strings.Join(parts, ", ")
}

// endpointKeys is what endpointLabels knows about one edge endpoint: the key
// label sets to probe, and whether those keys COVER every key a matching row
// can put at this endpoint.
//
// The bit travels with the keys because the keys alone do not carry it, and the
// callers that read them need opposite halves of the same set relation. Every
// PROBED key being declared is the close's half and is free; every ATTAINABLE
// key being probed is the narrowing's half and is not. declared() and
// covering() are those two contracts, and there is no accessor handing over the
// keys without naming which one the caller is under. candidateTypes is the
// caller easiest to miss: it reads a far end under the narrowing's half, and a
// commitment it derives from an uncovered one is itself uncovered.
type endpointKeys struct {
	keys   []graph.LabelSetKey
	covers bool
}

// declared is the keys for a caller that needs each PROBED key to be declared
// and nothing more — closeEdge, which filters edgeProbes' box through s.Edges
// and refuses what the schema does not have. A probe the schema declares is a
// real edge type whether or not the box was complete, so an incomplete box
// costs that caller candidates it might have had, never a wrong answer.
func (k endpointKeys) declared() []graph.LabelSetKey { return k.keys }

// covering is the keys for a caller that needs every ATTAINABLE key to be in
// the box, and ok=false when they are not. That is NarrowPluralEndpoints'
// contract: it reads a committed candidate set as a complete statement about
// the pattern's two ends, so a key left out takes its whole declaration out
// with it and the contribution then omits a type the omitted rows really have.
// Committing on that is not a missed widening, it is a wrong answer.
func (k endpointKeys) covering() ([]graph.LabelSetKey, bool) { return k.keys, k.covers }

// nodeTable is the resolver's node-binding view at EdgeKey-formation time. The
// three lanes travel together because endpointLabels' answer needs all three:
// reading resolved without resolvedCovers is exactly what makes a Phase B
// closure commitment look like a set of satisfying types.
//
// The maps are the scope's own, by reference — inferUnlabelled writes through
// this value into the live table.
type nodeTable struct {
	resolved map[string]schema.NodeType
	cands    map[string][]schema.NodeType
	// resolvedCovers qualifies `resolved` alone, and holds the entries whose
	// type set covers every type a matching row can put at that variable. Three
	// writers can put an entry in it:
	//
	//   - Phase A1's BindNode arm, whose entry is the set of types SATISFYING
	//     the labels the query spelled;
	//   - the narrowing's own collapse of a plural binding, which only ever
	//     drops types no matching row can have;
	//   - Phase B's inferUnlabelled, but ONLY when candidateTypes reports its
	//     inference covering — every folded edge had an enumerated far end and
	//     was evidence about every returned row. That conditional is the whole
	//     point of the lane: it is what lets a covering inference be learned
	//     from without letting an uncovered one launder a strict subset into a
	//     VarEndpoint the narrowing trusts.
	//
	// It is deliberately a positive lane, so anything the resolver did not
	// itself derive that way defaults to uncovered: an uncovered Phase B
	// inference (which may be a strict subset of the attainable types), and a
	// carried entry (whose provenance this Part cannot see).
	//
	// `cands` needs no such lane. Its only writers are BindNodeCands, the carry
	// seed, and the narrowing's shrink, so a cands entry is a satisfying set by
	// construction; inferUnlabelled never writes it.
	resolvedCovers map[string]struct{}
}

// endpointLabels reads the key label set(s) identifying an edge endpoint's
// node type(s), at the point EdgeKey formation needs it, together with whether
// those keys cover every key a matching row can put there. EdgeKey.Source and
// .Target hold node type identities, so this must yield key label sets, never
// complete ones.
//
// Returns a slice to support plural node satisfaction (ADR 0022): both arms can
// name several types, and edgeCandidates expands them into a cross-product. A
// VarEndpoint contributes its binding's candidate key label sets; an
// InlineEndpoint contributes the identities of the types whose COMPLETE label
// set satisfies the expression written there, which is the same superset test
// resolveNodeLabels runs on a labelled binding.
//
// The spelled labels are not one of those identities. They are a label
// expression, and the set of types satisfying it is neither bounded above by it
// — two types satisfy `(:Company)` on a schema declaring Company and
// Company&Large — nor below — `(:Staff)` is satisfied by a type whose identity
// is Engineer.
func endpointLabels(e query.Endpoint, t nodeTable, s schema.Schema) (endpointKeys, bool) {
	switch ep := e.(type) {
	case query.VarEndpoint:
		v := ep.Variable()
		if nt, ok := t.resolved[v]; ok {
			_, covers := t.resolvedCovers[v]
			return endpointKeys{keys: []graph.LabelSetKey{nt.KeyLabels}, covers: covers}, true
		}
		if nts, ok := t.cands[v]; ok {
			keys := make([]graph.LabelSetKey, len(nts))
			for i, nt := range nts {
				keys[i] = nt.KeyLabels
			}
			return endpointKeys{keys: keys, covers: true}, true
		}
		return endpointKeys{}, false
	case query.InlineEndpoint:
		ls := ep.Labels()
		if len(ls) == 0 {
			return endpointKeys{}, false
		}
		if sat := satisfyingNodeTypes(ls, s); len(sat) > 0 {
			return endpointKeys{keys: sat, covers: true}, true
		}
		// Nothing declared satisfies the expression, so no row can stand here and
		// the keys below are reached only by the refusal that follows. The spelled
		// key is what that refusal has to name: describeTriedEdges renders the
		// probes, and an author who wrote `(:Foo)` needs to read Foo back.
		return endpointKeys{keys: []graph.LabelSetKey{ls.Key()}, covers: true}, true
	default:
		return endpointKeys{}, false
	}
}

// closeEdge applies edge-candidate closure to one already-endpoint-resolved
// edge and records the resolved shape. srcs and tgts are slices to support
// plural node satisfaction (ADR 0022).
func closeEdge(e query.EdgeBinding, srcs, tgts []graph.LabelSetKey, s schema.Schema, edgeTypes map[string]schema.EdgeType, edgeKeys map[string]schema.EdgeKey, edgeCands map[string][]schema.EdgeKey) error {
	cands := edgeCandidates(e, srcs, tgts, s)
	v := e.Variable()

	switch len(cands) {
	case 0:
		return fmt.Errorf("%w: %s", ErrUnknownEdge, describeTriedEdges(e, srcs, tgts))
	case 1:
		key := cands[0]
		et := s.Edges[key]
		if v != "" {
			edgeTypes[v] = et
			edgeKeys[v] = key
		}
		return nil
	default:
		if !e.Directed() && len(e.Labels()) == 1 {
			if fwd, rev, ambiguous := orientationDisagreement(cands, srcs, tgts); ambiguous {
				// One witness per side, never the whole candidate set: the set
				// can hold three or more (a plural endpoint widens a side that
				// is already represented) and the extras add nothing a reader
				// must tell apart. So the message names the two sides rather
				// than claiming to enumerate the matches — "matches both X, Y"
				// read as a totality claim, and was false of exactly the
				// three-candidate fixture that pins this arm.
				return fmt.Errorf("%w: %s matches %s left-to-right and %s right-to-left",
					ErrAmbiguousEdgeOrientation, describeEdgeBinding(e), formatEdgeKey(fwd), formatEdgeKey(rev))
			}
		}
		if v != "" {
			edgeCands[v] = cands
		}
		return nil
	}
}

// describeEdgeBinding names an edge binding in text the author can find in
// their own query. A named binding is its variable. An anonymous edge is still
// its own binding but has no name to quote, so it is placed by the label
// alternation it carries and the two ends it runs between — the quoted empty
// string reads as a defect in gqlc rather than as a description of the pattern.
func describeEdgeBinding(e query.EdgeBinding) string {
	if v := e.Variable(); v != "" {
		return fmt.Sprintf("edge %q", v)
	}
	return fmt.Sprintf("the [:%s] edge between %s and %s",
		strings.Join(e.Labels(), "|"), describeEndpoint(e.Source()), describeEndpoint(e.Target()))
}

// describeEndpoint names one end of a pattern as the author wrote it: the
// variable where one is bound, and the inline label expression otherwise.
func describeEndpoint(e query.Endpoint) string {
	switch ep := e.(type) {
	case query.VarEndpoint:
		return ep.Variable()
	case query.InlineEndpoint:
		if len(ep.Labels()) > 0 {
			return "(:" + string(ep.Labels().Key()) + ")"
		}
	}
	return "()"
}

// orientationDisagreement reports whether the candidate set contains two
// candidates that disagree about which of the pattern's two endpoints the edge
// runs *from*, and returns one witness per side. srcs and tgts are the endpoint
// keys the pattern puts on the left and on the right, the same slices the
// candidates were enumerated from.
//
// Each candidate is classified by *both* its endpoints, against both endpoint
// slices. A candidate reads left-to-right when its Source is a key the pattern
// puts on the left and its Target one it puts on the right; right-to-left when
// the same holds with the slices exchanged. Both classes non-empty means the
// schema declares this edge type running both ways across the pattern, and an
// undirected pattern carries nothing that says which was meant — that is
// exactly what ErrAmbiguousEdgeOrientation names, and writing the arrow is a
// remedy that works, because a directed close drops one of the two classes
// entirely.
//
// Candidate-set size alone stopped being this question once a node binding
// could satisfy several declared types (ADR 0022): plural endpoints multiply
// the candidates *along one side*, and refusing those would report an
// orientation the schema never declared. An exact mirror — {A, L, B} alongside
// {B, L, A} — is one shape the disagreement takes, not the only one: when the
// reverse declaration lands on a subtype of the node type the forward one uses,
// the two candidates cross the pattern in opposite directions while being
// nobody's mirror.
//
// A candidate that reads both ways carries no orientation signal and is
// skipped: a self-loop key, or any key whose Source and Target each satisfy
// both of the pattern's endpoints, is its own counterparty, and reading it as a
// disagreement would name one key twice.
//
// Classifying on the Source alone is not that test and is strictly weaker. When
// the endpoints' satisfying sets overlap without being equal — an (:Employee)
// endpoint against a (:Person) one, the ordinary shape of a subtype schema —
// every candidate Source can sit in both slices while the Targets still
// separate the two readings, and a Source-only skip exempts the whole schema
// from the check. Corpus:
// invalid/ambiguous_edge_orientation_overlapping_endpoints and its _reversed
// twin, which cover both subset directions.
//
// Only candidates carrying the same label are compared. Two *different* edge
// types running opposite ways is the multi-type union of §4.6 case D, and the
// sentinel's message would be false of it. The single-type guard at the call
// site makes this vacuous today; the grouping is here so the answer is true of
// whatever set the function is handed rather than true only where it is called.
//
// Candidates are scanned in candidate order and the first witness on each side
// is kept, so the reported pair is deterministic (§4.4).
func orientationDisagreement(cands []schema.EdgeKey, srcs, tgts []graph.LabelSetKey) (schema.EdgeKey, schema.EdgeKey, bool) {
	type sides struct {
		fwd, rev       schema.EdgeKey
		hasFwd, hasRev bool
	}
	byLabel := make(map[graph.LabelSetKey]*sides, len(cands))
	for _, k := range cands {
		l2r, r2l := readsLeftToRight(k, srcs, tgts), readsRightToLeft(k, srcs, tgts)
		// Equal means either both readings hold (no orientation signal) or
		// neither, which edgeProbes cannot produce — every candidate comes from
		// an (src, tgt) pair drawn from the two slices in one order or the other.
		if l2r == r2l {
			continue
		}
		s, ok := byLabel[k.KeyLabels]
		if !ok {
			s = &sides{}
			byLabel[k.KeyLabels] = s
		}
		if l2r && !s.hasFwd {
			s.fwd, s.hasFwd = k, true
		}
		if r2l && !s.hasRev {
			s.rev, s.hasRev = k, true
		}
		if s.hasFwd && s.hasRev {
			return s.fwd, s.rev, true
		}
	}
	return schema.EdgeKey{}, schema.EdgeKey{}, false
}

// readsLeftToRight and readsRightToLeft are a candidate's two readings against
// the endpoint key slices the pattern put on the left (srcs) and the right
// (tgts). Each is a claim about BOTH the candidate's ends: a key reads
// left-to-right when its Source is a type the pattern admits on the left and
// its Target one it admits on the right, and right-to-left when the same holds
// with the slices exchanged. Both can be true of one key: a directed close
// probes only the left-to-right reading, but where srcs and tgts overlap a
// candidate it returns still answers to the other one. endpointContribution
// asks both questions of every candidate whatever the arrow said, so such a key
// contributes both of its ends and the result is a superset of what the arrow
// permits — widening in ADR 0006's safe direction, filed as gqlc-pv0u rather
// than narrowed here.
//
// Package-level rather than closures inside orientationDisagreement because the
// endpoint narrowing asks the same question of the same candidate set and must
// get the same answer: whether a candidate's Source or its Target is the type
// this end of the pattern gets is exactly which reading it is.
func readsLeftToRight(k schema.EdgeKey, srcs, tgts []graph.LabelSetKey) bool {
	return slices.Contains(srcs, k.Source) && slices.Contains(tgts, k.Target)
}

func readsRightToLeft(k schema.EdgeKey, srcs, tgts []graph.LabelSetKey) bool {
	return slices.Contains(tgts, k.Source) && slices.Contains(srcs, k.Target)
}

// patternEnd names which end of a pattern a contribution is being computed for.
// The two ends read the same candidate from opposite sides, so this is the only
// thing that differs between them.
type patternEnd bool

const (
	patternLeft  patternEnd = true
	patternRight patternEnd = false
)

// endpointContribution is the set of node types a committed candidate set puts
// on ONE end of the pattern — the per-edge contribution the endpoint narrowing
// intersects across touching edges, and the direct analogue of R3 §4.5.2's
// per-edge contribution for unlabelled bindings.
//
// A candidate contributes through each reading it has, and the contribution is
// the UNION of them. That is the whole subtlety: an undirected close probes both
// orientations, so one candidate set can put this end of the pattern on the
// Source of some candidates and on the Target of others, and a set can even hold
// a candidate that reads both ways alongside candidates that read one way.
// Taking a single reading — or intersecting the two — narrows away a node type
// the schema permits at this end, which the caller would then commit as a
// refusal or as the wrong type.
//
// Every element of the result is drawn from the slice this end contributed to
// the probe, because a reading is a claim about both of the candidate's ends and
// every candidate was probed from those two slices. So a single edge can never
// empty the set it is intersected into, and the narrowing is a refinement of
// label satisfaction rather than an independent answer.
func endpointContribution(cands []schema.EdgeKey, srcs, tgts []graph.LabelSetKey, end patternEnd) map[graph.LabelSetKey]struct{} {
	out := make(map[graph.LabelSetKey]struct{}, len(cands))
	for _, k := range cands {
		if readsLeftToRight(k, srcs, tgts) {
			if end == patternLeft {
				out[k.Source] = struct{}{}
			} else {
				out[k.Target] = struct{}{}
			}
		}
		if readsRightToLeft(k, srcs, tgts) {
			if end == patternLeft {
				out[k.Target] = struct{}{}
			} else {
				out[k.Source] = struct{}{}
			}
		}
	}
	return out
}

// describeTriedEdges is ErrUnknownEdge's fail-message body: every EdgeKey
// the binding could have named, none of which the schema declares. It reads
// the same enumeration the candidate set is filtered from, so the message
// cannot list a key the resolver did not try, nor omit one it did.
func describeTriedEdges(e query.EdgeBinding, srcs, tgts []graph.LabelSetKey) string {
	return formatEdgeKeys(edgeProbes(e, srcs, tgts))
}

// endpointNarrowing is the endpoint-narrowing rule itself, as a map from node
// variable to the key set the touching edges leave it: per touching edge, UNION
// the two readings' contributions (endpointContribution); across touching
// edges, INTERSECT. A variable absent from the result was narrowed by nothing.
//
// Two callers read it and they read it for different purposes.
// NarrowPluralEndpoints APPLIES it, shrinking a plural binding's own candidate
// list. candidateTypes READS it about a pending unlabelled binding's far end,
// so Phase B infers through the types that end can still have rather than
// through its whole satisfying set. Sharing one function is what makes those
// two answers the same answer: an unlabelled binding inferred through a far end
// and the far end's own narrowing are the same claim about that end, and
// deriving them twice is how they drift apart.
//
// The gates are gqlc-0tft's, unchanged and asked of every edge: it must
// witnessesItsEndpoints, and BOTH its ends must cover(). Skipping an edge only
// widens the result, which is the safe direction for both callers — the applier
// lands on the pre-narrowing candidate list, the Phase B reader on the far
// end's full satisfying set, which is what each had before this pass existed.
//
// Only plural bindings get an entry. A resolved singular endpoint contributes
// its one key to every reading it has, so an entry for it could only ever be
// that same key: including them would change no answer, and the restriction is
// written here so it is stated once rather than left to each caller.
func endpointNarrowing(edges []query.EdgeBinding, t nodeTable, s schema.Schema, written map[string]struct{}) map[string]map[graph.LabelSetKey]struct{} {
	acc := make(map[string]map[graph.LabelSetKey]struct{}, len(t.cands))
	for _, e := range edges {
		if !witnessesItsEndpoints(e, written) {
			continue
		}
		srcEnd, srcOK := endpointLabels(e.Source(), t, s)
		tgtEnd, tgtOK := endpointLabels(e.Target(), t, s)
		if !srcOK || !tgtOK {
			// Reached from Phase B, whose whole business is bindings not yet
			// typed: the edge holding the pending binding has an end
			// endpointLabels cannot read, and every edge deferred by Phase A2
			// does too until the binding at its far end commits. Contributing
			// nothing is the widening direction. From NarrowPluralEndpoints this
			// arm is dead — CloseEdges either resolved both ends or returned
			// ErrUnknownLabel before calling it.
			continue
		}
		// covering(), because everything below reads `cands` as a complete
		// statement about this edge's two ends. Either end failing it means
		// edgeProbes' box could be missing a declaration a matching row really
		// has, so the contribution would omit a type those rows carry.
		srcs, srcCovers := srcEnd.covering()
		tgts, tgtCovers := tgtEnd.covering()
		if !srcCovers || !tgtCovers {
			continue
		}
		cands := edgeCandidates(e, srcs, tgts, s)
		if len(cands) == 0 {
			// The schema declares no edge of this label between these ends. From
			// NarrowPluralEndpoints closeEdge has already refused that (§4.6 case
			// A); from Phase B it can be an edge Phase A2 deferred whose ends
			// have only just become readable, so its close is still ahead of it
			// and will fail there. Contributing nothing is the widening
			// direction, and it leaves the refusal to closeEdge, which is the
			// one that can name the keys it tried.
			continue
		}
		// Per-edge contributions first, so a binding sitting at BOTH ends of
		// one edge — a self-loop written on a single variable — unions its two
		// ends rather than intersecting them.
		//
		// No input can tell that union from an intersection, and that is a
		// property of the shape rather than a gap in the corpus: a variable
		// reaches both ends only by naming both, and endpointLabels then hands
		// both ends the same key slice, which makes a candidate's two readings
		// the same predicate and both ends' contributions the same set —
		// TestEndpointContributionUnionsTheTwoReadings' equal-slices row states
		// that directly. The union is written because it is the rule, not
		// because an input needs it.
		perEdge := make(map[string]map[graph.LabelSetKey]struct{}, 2)
		for _, side := range [2]struct {
			ep  query.Endpoint
			end patternEnd
		}{{e.Source(), patternLeft}, {e.Target(), patternRight}} {
			ve, isVar := side.ep.(query.VarEndpoint)
			if !isVar {
				continue
			}
			v := ve.Variable()
			if _, plural := t.cands[v]; !plural {
				continue
			}
			contrib := endpointContribution(cands, srcs, tgts, side.end)
			if prev, seen := perEdge[v]; seen {
				for k := range prev {
					contrib[k] = struct{}{}
				}
			}
			perEdge[v] = contrib
		}
		for v, contrib := range perEdge {
			if prev, seen := acc[v]; seen {
				acc[v] = intersect(prev, contrib)
				continue
			}
			acc[v] = contrib
		}
	}
	return acc
}

// inferUnlabelled is Phase B. `written` is the caller's scope.writtenBindings
// set, which candidateTypes needs to ask witnessesItsEndpoints of each edge it
// folds in — see there.
func inferUnlabelled(pending []query.NodeBinding, edges []query.EdgeBinding, s schema.Schema, t nodeTable, callTypes map[string]callBindingSlot, written map[string]struct{}) error {
	resolved, nodeCands := t.resolved, t.cands
	if len(pending) == 0 {
		return nil
	}
	// R5 §4.2.3 N1 posture: CARRY WINS. An unlabelled binding whose
	// variable was already typed by the carry seed at Phase A1 is a JOIN
	// on the same node identity (openCypher semantics for `WITH a MATCH
	// (a)-[...]->…`), not a redeclaration; skip Phase B inference for it
	// entirely so the carry-seeded type stays authoritative. Doing this
	// here also erases the order-dependence Linus observed in the raw
	// per-Part inference (before this guard, whether an unlabelled `(a)`
	// after `WITH a` got reinferred depended on whether the enclosing
	// edge's other endpoint had already committed).
	if len(resolved) > 0 || len(nodeCands) > 0 {
		filtered := pending[:0]
		for _, n := range pending {
			if _, carried := resolved[n.Variable()]; carried {
				continue
			}
			if _, carriedCands := nodeCands[n.Variable()]; carriedCands {
				continue
			}
			filtered = append(filtered, n)
		}
		pending = filtered
	}
	for len(pending) > 0 {
		next, committed, err := commitUnlabelledRound(pending, edges, s, t, callTypes, written, nil)
		if err != nil {
			return err
		}
		if committed == 0 {
			// Master's answer for this round is the ErrAmbiguousBinding below, so
			// this is the one place a second lane can widen without moving any
			// query the resolver already accepts: reaching here means every
			// pending binding was plural on its far ends' full satisfying sets,
			// and master returns from here. The retry asks the same question with
			// each far end narrowed by the edges that pin IT, which is what makes
			// a bare binding answer to a hop it does not itself touch.
			//
			// Recomputed per round rather than hoisted: a round that commits
			// changes the binding tables endpointNarrowing reads, so a hoisted
			// answer would be the one a stale table gave.
			narrowing := endpointNarrowing(edges, t, s, written)
			next, committed, err = commitUnlabelledRound(pending, edges, s, t, callTypes, written, narrowing)
			if err != nil {
				return err
			}
			if committed == 0 {
				n := next[0]
				cands, _ := candidateTypes(n, edges, s, t, written, narrowing)
				return fmt.Errorf("%w: cannot uniquely infer type of unlabelled binding %q — candidate types: %s", ErrAmbiguousBinding, n.Variable(), joinCandidates(cands))
			}
		}
		pending = next
	}
	return nil
}

// commitUnlabelledRound is one pass of Phase B's fixed point: every pending
// binding whose candidate set is a singleton commits, and the rest come back as
// `next` for the following round. `narrowing` is endpointNarrowing's answer, or
// nil for the lane that reads far ends exactly as master does.
//
// The two lanes differ in nothing but that argument, so the widened lane cannot
// drift from master's on any question other than which types a far end can
// still have.
func commitUnlabelledRound(pending []query.NodeBinding, edges []query.EdgeBinding, s schema.Schema, t nodeTable, callTypes map[string]callBindingSlot, written map[string]struct{}, narrowing map[string]map[graph.LabelSetKey]struct{}) ([]query.NodeBinding, int, error) {
	resolved := t.resolved
	var next []query.NodeBinding
	committed := 0
	for _, n := range pending {
		cands, covered := candidateTypes(n, edges, s, t, written, narrowing)
		switch len(cands) {
		case 0:
			return nil, 0, fmt.Errorf("%w: cannot infer type of unlabelled binding %q — no edge in the pattern reaches a compatible schema node type", ErrUnknownLabel, n.Variable())
		case 1:
			var only graph.LabelSetKey
			for k := range cands {
				only = k
			}
			// R7 §4.1.2.1 addendum: an inferred unlabelled node whose
			// name collides with a carried CALL YIELD scalar fails at
			// commit — the shape-posture check the labelled arm runs
			// at Phase A1 fires here for the unlabelled path.
			if _, seenCall := callTypes[n.Variable()]; seenCall {
				return nil, 0, fmt.Errorf("%w: variable %q carried as CALL YIELD scalar, re-bound as %s", ErrPartBindingTypeConflict, n.Variable(), only)
			}
			resolved[n.Variable()] = s.Nodes[only]
			// A Phase B commitment covers only when every edge it read from
			// could enumerate its far end AND was evidence about every
			// returned row — see candidateTypes for both conjuncts. Marking
			// it lets a later binding infer through this one and lets the
			// narrowing learn from it; NOT marking it is what keeps a far end
			// that could not enumerate itself, or an edge no returned row has,
			// from laundering a subset into a VarEndpoint the narrowing trusts.
			//
			// This is also how the property becomes transitive without a
			// fixed point: a variable committed uncovered is in `resolved` and
			// absent from resolvedCovers, so the next binding to read it
			// through endpointLabels gets covers=false and commits uncovered
			// in turn.
			//
			// That transitivity is the absence, not the delete. The else arm
			// changes no answer and no test can pin it: an unlabelled pending
			// binding is never already in the lane — the lane's other writers
			// are BindNode, which never sees an unlabelled binding, and the
			// narrowing's collapse, which cannot run before Phase B; newScope
			// does not seed the lane from the carry; and a name leaves
			// `pending` on the iteration it commits, so there is no second
			// commit to overwrite. It is written so the absence is asserted
			// rather than assumed: seeding the lane from the carry is the
			// change newScope names as its alternative, and it would make this
			// arm live.
			if covered {
				t.resolvedCovers[n.Variable()] = struct{}{}
			} else {
				delete(t.resolvedCovers, n.Variable())
			}
			committed++
		default:
			next = append(next, n)
		}
	}
	return next, committed, nil
}

// candidateTypes is the per-edge intersection Phase B infers an unlabelled
// binding's type from, and whether that answer COVERS every type a matching row
// can put at the binding.
//
// It reads each far end's keys whether or not that end covers, and infers from
// them: refusing an uncovered far end here would turn queries origin/master
// resolves into ErrUnknownLabel, and the wrong type it infers from one is
// gqlc-3uof — pre-existing on master and deliberately left alone. What the
// second return adds is the admission, so the wrong answer stops here instead
// of being read downstream as a complete statement about a pattern's ends.
//
// The rule for the second return is a CONJUNCTION over the edges that
// contributed, and each conjunct is a different way the intersection can drop a
// type the returned rows really have:
//
//   - the far end covered — otherwise the keys probed against are a strict
//     subset of the ones a matching row can put there, and the declarations
//     reachable only through the missing keys never enter the intersection;
//   - the edge witnessesItsEndpoints — otherwise the edge is not evidence about
//     every returned row, so intersecting against it drops a type the SURVIVING
//     rows carry even with both far ends enumerated perfectly. An OPTIONAL hop
//     is the demonstrated case: it is an outer join, so the rows that lack it
//     come back anyway and this binding is whatever those rows put here.
//
// The second conjunct does not skip the edge, only un-covers the answer. The
// inferred type is left exactly as master infers it — skipping would widen acc
// and turn a unique inference into ErrAmbiguousBinding, a refusal master does
// not make. An edge skipped above (an endpoint endpointLabels cannot read yet)
// contributes no constraint at all, which only widens acc, so skipping cannot
// break the superset property either.
//
// `narrowing` is endpointNarrowing's answer, or nil. Non-nil makes each far end
// contribute through the types the edges that pin IT leave it, rather than
// through its whole satisfying set — the difference between a bare binding
// reading the union of everything its edges could reach and reading what they
// prove (gqlc-h6h7). The narrowed answer is returned only when it is non-empty;
// an empty one falls back to the unnarrowed acc, so this function never refuses
// where the nil lane would not, and never turns that lane's
// ErrAmbiguousBinding into case 0's ErrUnknownLabel.
//
// `covered` is computed off the unnarrowed reading and the narrowing does not
// touch it. endpointNarrowing only ever drops types that no matching row can
// put at that end — it folds in an edge only when the edge witnesses its
// endpoints and both of its ends cover — which is the same argument
// NarrowPluralEndpoints makes when it keeps resolvedCovers on a collapse.
func candidateTypes(n query.NodeBinding, edges []query.EdgeBinding, s schema.Schema, t nodeTable, written map[string]struct{}, narrowing map[string]map[graph.LabelSetKey]struct{}) (map[graph.LabelSetKey]struct{}, bool) {
	var acc, narrowedAcc map[graph.LabelSetKey]struct{}
	covered := true
	for _, e := range edges {
		side, touches := touchingSide(e, n.Variable())
		if !touches {
			continue
		}
		other := e.Source()
		if side == "source" {
			other = e.Target()
		}
		otherEnd, ok := endpointLabels(other, t, s)
		if !ok {
			continue
		}
		otherKeys, otherCovers := otherEnd.covering()
		if !otherCovers {
			covered = false
		}
		// The same question NarrowPluralEndpoints asks of the edge it narrows
		// from, asked here of every edge folded in — a commitment derived from
		// an edge some returned row does not have is not a statement about that
		// row's type, however well enumerated the edge's far end was.
		if !witnessesItsEndpoints(e, written) {
			covered = false
		}
		acc = foldEdgeContribution(acc, e, side, otherKeys, s)
		if narrowing != nil {
			narrowedAcc = foldEdgeContribution(narrowedAcc, e, side, narrowedEndpointKeys(other, otherKeys, narrowing), s)
		}
	}
	if len(narrowedAcc) > 0 {
		return narrowedAcc, covered
	}
	if acc == nil {
		return map[graph.LabelSetKey]struct{}{}, covered
	}
	return acc, covered
}

// narrowedEndpointKeys filters a far end's keys through what endpointNarrowing
// leaves the binding at that end. A far end that is not a variable, or one the
// narrowing gave no entry, is returned unchanged.
//
// An empty filter result returns the keys unchanged as well, matching
// NarrowPluralEndpoints' empty arm: the edges pin this end to types disjoint
// from the ones satisfying it, so the pattern matches nothing, and under ADR
// 0006 that is a fact about which rows come back rather than a licence to
// refuse.
func narrowedEndpointKeys(other query.Endpoint, keys []graph.LabelSetKey, narrowing map[string]map[graph.LabelSetKey]struct{}) []graph.LabelSetKey {
	ve, isVar := other.(query.VarEndpoint)
	if !isVar {
		return keys
	}
	keep, ok := narrowing[ve.Variable()]
	if !ok {
		return keys
	}
	out := make([]graph.LabelSetKey, 0, len(keys))
	for _, k := range keys {
		if _, in := keep[k]; in {
			out = append(out, k)
		}
	}
	if len(out) == 0 {
		return keys
	}
	return out
}

// foldEdgeContribution intersects one edge's contribution into the accumulator,
// seeding it on the first contributing edge. A nil accumulator is "no edge has
// contributed yet"; an empty non-nil one is "an edge contributed nothing", and
// the two are not the same — the second is an answer, and every later
// intersection keeps it empty.
//
// The contribution is every node type the schema puts at this binding's side of
// a declaration the edge could name, unioned over the edge's labels, over both
// orientations when the pattern is undirected, and over the far end's keys.
func foldEdgeContribution(acc map[graph.LabelSetKey]struct{}, e query.EdgeBinding, side string, otherKeys []graph.LabelSetKey, s schema.Schema) map[graph.LabelSetKey]struct{} {
	cand := make(map[graph.LabelSetKey]struct{})
	orientations := []bool{true}
	if !e.Directed() {
		orientations = []bool{true, false}
	}
	for _, L := range e.Labels() {
		labelKey := graph.LabelSet{L}.Key()
		for _, forward := range orientations {
			for k := range s.Edges {
				if k.KeyLabels != labelKey {
					continue
				}
				nAtSource := (side == "source") == forward
				for _, otherKey := range otherKeys {
					if nAtSource && k.Target == otherKey {
						cand[k.Source] = struct{}{}
					}
					if !nAtSource && k.Source == otherKey {
						cand[k.Target] = struct{}{}
					}
				}
			}
		}
	}
	if acc == nil {
		return cand
	}
	return intersect(acc, cand)
}

func touchingSide(e query.EdgeBinding, v string) (string, bool) {
	if src, ok := e.Source().(query.VarEndpoint); ok && src.Variable() == v {
		return "source", true
	}
	if tgt, ok := e.Target().(query.VarEndpoint); ok && tgt.Variable() == v {
		return "target", true
	}
	return "", false
}

func intersect(a, b map[graph.LabelSetKey]struct{}) map[graph.LabelSetKey]struct{} {
	out := make(map[graph.LabelSetKey]struct{})
	for k := range a {
		if _, ok := b[k]; ok {
			out[k] = struct{}{}
		}
	}
	return out
}

func joinCandidates(c map[graph.LabelSetKey]struct{}) string {
	keys := make([]string, 0, len(c))
	for k := range c {
		keys = append(keys, string(k))
	}
	sort.Strings(keys)
	out := ""
	for i, k := range keys {
		if i > 0 {
			out += ", "
		}
		out += k
	}
	return out
}

// callProjectionType maps a Ref against a CALL YIELD variable's callBindingSlot
// to its resolved column type (spec §4.2.1). Bare Ref bridges the bridged
// query.Type to ResolvedProperty (INT / FLOAT / STRING) or ResolvedUnknown
// (NUMBER-marker → TypeUnknown wire form). Property lookup on a scalar
// (Ref.Property != "") fires ErrUnknownProperty with a widened message set —
// §4.2.2, one of the R7 §5.3 message-set widenings.
func callProjectionType(slot callBindingSlot, ref query.Ref, nullableBinding map[string]bool) (ResolvedType, error) {
	if ref.Property != "" {
		return nil, fmt.Errorf("%w: %s.%s (CALL YIELD variable %q is a scalar)", ErrUnknownProperty, ref.Variable, ref.Property, ref.Variable)
	}
	nullable := nullableBinding[ref.Variable]
	switch slot.resultType.(type) {
	case query.TypeInt:
		return ResolvedProperty{Type: graph.TypeInt, Nullable: nullable}, nil
	case query.TypeFloat:
		return ResolvedProperty{Type: graph.TypeFloat, Nullable: nullable}, nil
	case query.TypeString:
		return ResolvedProperty{Type: graph.TypeString, Nullable: nullable}, nil
	case query.TypeUnknown:
		return ResolvedUnknown{}, nil
	default:
		return ResolvedUnknown{}, nil
	}
}

func unionProperty(cands []schema.EdgeKey, s schema.Schema, refVar, refProp string, bindingNullable bool) (ResolvedType, error) {
	var first ResolvedProperty
	for i, k := range cands {
		et := s.Edges[k]
		prop, ok := et.Properties[refProp]
		if !ok {
			return nil, fmt.Errorf("%w: property %s.%s missing on union member %s", ErrUnknownProperty, refVar, refProp, formatEdgeKey(k))
		}
		hit := ResolvedProperty{Type: prop.Type, Nullable: prop.Nullable}
		if i == 0 {
			first = hit
			continue
		}
		if hit.Type != first.Type || hit.Nullable != first.Nullable {
			return nil, fmt.Errorf("%w: property %s.%s type differs across union members: %s vs %s", ErrUnknownProperty, refVar, refProp, first.String(), hit.String())
		}
	}
	first.Nullable = first.Nullable || bindingNullable
	return first, nil
}

// unionNodeProperty resolves a property reference against a plural node
// candidate set. The property must exist on every candidate with identical type
// and nullability — the intersection rule for plural label satisfaction (ADR 0022).
func unionNodeProperty(nts []schema.NodeType, refVar, refProp string, bindingNullable bool) (ResolvedType, error) {
	var first ResolvedProperty
	for i, nt := range nts {
		prop, ok := nt.Properties[refProp]
		if !ok {
			return nil, fmt.Errorf("%w: %s.%s missing on plural-satisfying type %s", ErrUnknownProperty, refVar, refProp, nt.KeyLabels)
		}
		hit := ResolvedProperty{Type: prop.Type, Nullable: prop.Nullable}
		if i == 0 {
			first = hit
			continue
		}
		if hit.Type != first.Type || hit.Nullable != first.Nullable {
			return nil, fmt.Errorf("%w: %s.%s type differs across plural-satisfying types: %s vs %s", ErrUnknownProperty, refVar, refProp, first.String(), hit.String())
		}
	}
	first.Nullable = first.Nullable || bindingNullable
	return first, nil
}

func formatNodeTypeKeys(nts []schema.NodeType) string {
	parts := make([]string, len(nts))
	for i, nt := range nts {
		parts[i] = string(nt.KeyLabels)
	}
	return strings.Join(parts, ", ")
}

// resolveType maps a parser Type into its ResolvedType. R5 is unchanged from
// R4 in mechanic — the AggregateProjection.Type() dispatch (per §4.5.1) rides
// this table for its result-type emission.
func resolveType(t query.Type) (ResolvedType, error) {
	switch tt := t.(type) {
	case query.TypeBool:
		return ResolvedScalar{Kind: ScalarBool}, nil
	case query.TypeInt:
		return ResolvedScalar{Kind: ScalarInt}, nil
	case query.TypeFloat:
		return ResolvedScalar{Kind: ScalarFloat}, nil
	case query.TypeString:
		return ResolvedScalar{Kind: ScalarString}, nil
	case query.TypeNull:
		return ResolvedScalar{Kind: ScalarNull}, nil
	case query.TypeMap:
		return ResolvedScalar{Kind: ScalarMap}, nil
	case query.TypeDate:
		return ResolvedTemporal{Kind: TemporalDate}, nil
	case query.TypeTime:
		return ResolvedTemporal{Kind: TemporalTime}, nil
	case query.TypeLocalTime:
		return ResolvedTemporal{Kind: TemporalLocalTime}, nil
	case query.TypeDateTime:
		return ResolvedTemporal{Kind: TemporalDateTime}, nil
	case query.TypeLocalDateTime:
		return ResolvedTemporal{Kind: TemporalLocalDateTime}, nil
	case query.TypeDuration:
		return ResolvedTemporal{Kind: TemporalDuration}, nil
	case query.TypeList:
		switch tt.Element().(type) {
		case query.TypeNode:
			return nil, fmt.Errorf("%w: list-of-nodes projection", ErrOutOfR0Scope)
		case query.TypeEdge:
			return nil, fmt.Errorf("%w: list-of-edges projection", ErrOutOfR0Scope)
		}
		el, err := resolveType(tt.Element())
		if err != nil {
			return nil, err
		}
		return ResolvedList{Element: el}, nil
	case query.TypeUnknown:
		return ResolvedUnknown{}, nil
	case query.TypeNode:
		panic("resolver bug: resolveType reached bare TypeNode (RefProjection bypasses this mapper)")
	case query.TypeEdge:
		panic("resolver bug: resolveType reached bare TypeEdge (RefProjection bypasses this mapper)")
	case query.TypePath:
		panic("resolver bug: resolveType reached TypePath (R5 does not admit path bindings)")
	default:
		panic(fmt.Sprintf("resolver bug: resolveType reached unhandled query.Type %T", t))
	}
}

func bindingVariable(b query.Binding) (string, bool) {
	switch bb := b.(type) {
	case query.NodeBinding:
		return bb.Variable(), true
	case query.EdgeBinding:
		return bb.Variable(), true
	case query.CallBinding:
		return bb.Variable(), true
	default:
		return "", false
	}
}

func qualifiedDemoter(e query.EdgeBinding) bool {
	h := e.Hops()
	if h == nil {
		return true
	}
	lower := h.Min()
	if lower == nil {
		return true
	}
	return *lower >= 1
}

// singleHopPattern reports whether an edge binding's pattern IS one declared
// edge: no quantifier at all, or a quantifier whose range admits exactly one
// hop and no other count.
//
// This is a STRICTER question than qualifiedDemoter's, and deliberately not the
// same one. qualifiedDemoter asks whether the edge is guaranteed to exist on a
// surviving row, which a zero lower bound is the only quantifier to break; that
// answer is all DemoteNullability needs, because nullability is a property of
// the endpoints' existence and a longer path still ends on real nodes. The
// narrowing needs more: it reads the closed candidate set as naming the types
// at the PATTERN's two ends, and under `*2` the closure describes the last hop
// while the pattern's near end sits two hops back. There, "an edge exists" and
// "these are its ends" come apart, so a lower-bound test is the wrong test.
//
// `*` is why the upper bound is read rather than the lower. openCypher reads an
// absent lower bound as one, so `*` passes every lower-bound test there is
// while admitting paths of any length — the widest shape the grammar has.
func singleHopPattern(e query.EdgeBinding) bool {
	h := e.Hops()
	if h == nil {
		return true
	}
	upper := h.Max()
	if upper == nil || *upper != 1 {
		return false
	}
	lower := h.Min()
	if lower == nil {
		// Absent lower bound is one (§4.4.3), so `*..1` is the closed [1,1].
		return true
	}
	// Two spellings other than one reach here, and the equality refuses both.
	// A zero lower bound is `*0..1`, whose empty path degenerates to
	// source == target and declares nothing about either end. A lower bound
	// above one is an EMPTY range such as `*2..1` — the grammar does not
	// require min <= max — and matching no path length is not matching one
	// declared edge. TestAnEmptyHopRangeIsNotAWitness is why this is `== 1`
	// rather than `>= 1`; nothing else in the corpus separates the two.
	return *lower == 1
}

// witnessesItsEndpoints reports whether a closed edge binding is EVIDENCE about
// the node types at its two ends — the precondition NarrowPluralEndpoints reads
// before letting an edge constrain a plural endpoint. `written` is the caller's
// scope.writtenBindings set.
//
// An edge binding names a relationship the schema declares between two node
// types, but that declaration only pins the ends of the rows the query returns
// when the pattern is that one relationship AND every returned row is
// guaranteed to have it. Three shapes break the conjunction, and on each the
// narrowing would commit a type the projection then names for rows that do not
// have it — a NOT NULL column that is null:
//
//   - Nullable(): an OPTIONAL MATCH is an outer join. `MATCH (p:Person)
//     OPTIONAL MATCH (p)-[:WORKS_AT]->(:Company)` returns a bare Person with no
//     WORKS_AT, and that row's p is not an Employee.
//   - !singleHopPattern(): a quantifier admitting any count but one. A zero
//     lower bound (`*0`, `*0..2`) admits the empty path, which degenerates to
//     source == target and declares nothing about either end. Any count above
//     one (`*2`, `*1..2`, `*`) chains several declared edges, and the closure
//     then names the ends of ONE of them rather than the ends of the pattern —
//     so the committed type is not a coarser answer but the wrong one, a hop or
//     more away from the truth. No OPTIONAL MATCH is needed to reach either: a
//     plain MATCH with the quantifier is enough.
//   - written: a CREATE or MERGE edge is CAUSED by the query rather than
//     observed by it, so it filters no row of the MATCH that fed it. Both
//     clauses leave every input row in the result, whatever its type.
//
// The first arm is §4.4.3's demotion gate, spelled the same way here and read
// in DemoteNullability for the same reason: both ask "is this edge guaranteed
// on a surviving row". The second is strictly narrower than §4.4.3's — see
// singleHopPattern for why the extra question is this pass's and not
// DemoteNullability's. The third is asked only here; DemoteNullability's answer
// for a written edge is master's and is not this function's to change.
//
// The OPTIONAL arm is deliberately blunter than DemoteNullability's, which
// exempts an OPTIONAL edge whose group is already proven (ay9). Such an edge
// genuinely is a witness, so honouring demotedGroups would narrow more — but
// that is a further widening, and it is filed rather than taken here.
func witnessesItsEndpoints(e query.EdgeBinding, written map[string]struct{}) bool {
	if e.Nullable() || !singleHopPattern(e) {
		return false
	}
	_, isWritten := written[e.Variable()]
	return !isWritten
}

// unify agrees two ResolvedTypes iff they are structurally equal or one side
// is ResolvedUnknown. Returns the agreed type and true on success, (nil, false)
// on conflict.
func unify(a, b ResolvedType) (ResolvedType, bool) {
	if _, ok := a.(ResolvedUnknown); ok {
		return b, true
	}
	if _, ok := b.(ResolvedUnknown); ok {
		return a, true
	}
	switch aa := a.(type) {
	case ResolvedProperty:
		bb, ok := b.(ResolvedProperty)
		if !ok || bb.Type != aa.Type || bb.Nullable != aa.Nullable {
			return nil, false
		}
		return aa, true
	case ResolvedScalar:
		bb, ok := b.(ResolvedScalar)
		if !ok || bb.Kind != aa.Kind {
			return nil, false
		}
		return aa, true
	case ResolvedTemporal:
		bb, ok := b.(ResolvedTemporal)
		if !ok || bb.Kind != aa.Kind {
			return nil, false
		}
		return aa, true
	case ResolvedList:
		bb, ok := b.(ResolvedList)
		if !ok {
			return nil, false
		}
		el, ok := unify(aa.Element, bb.Element)
		if !ok {
			return nil, false
		}
		return ResolvedList{Element: el}, true
	case ResolvedNode:
		bb, ok := b.(ResolvedNode)
		if !ok || bb.Labels != aa.Labels {
			return nil, false
		}
		return aa, true
	case ResolvedEdge:
		bb, ok := b.(ResolvedEdge)
		if !ok || bb.EdgeKey != aa.EdgeKey {
			return nil, false
		}
		return aa, true
	default:
		return nil, false
	}
}

// validateEffect dispatches one Effect through its per-variant validator against
// the scope's committed binding tables. The dispatch is a type switch on the
// closed Effect sum (query.go:1631-1660); the default arm is a defensive
// tripwire for a future Effect variant landing without an R6 refresh.
func validateEffect(sc *scope, e query.Effect, s schema.Schema) error {
	switch ee := e.(type) {
	case query.CreateEffect:
		return validateCreateEffect(sc, ee)
	case query.MergeEffect:
		return validateMergeEffect(sc, ee, s)
	case query.SetPropertyEffect:
		return validateSetPropertyEffect(sc, ee, s)
	case query.SetEntityEffect:
		return validateSetEntityEffect(sc, ee)
	case query.SetLabelsEffect:
		return validateSetLabelsEffect(sc, ee, s)
	case query.RemovePropertyEffect:
		return validateRemovePropertyEffect(sc, ee, s)
	case query.RemoveLabelsEffect:
		return validateRemoveLabelsEffect(sc, ee, s)
	case query.DeleteEffect:
		return validateDeleteEffect(sc, ee, s)
	default:
		return fmt.Errorf("%w: unknown Effect variant (%T)", ErrOutOfR0Scope, e)
	}
}

// validateCreateEffect walks e.Variables() and confirms each non-empty name is
// present in sc.nodeTypes OR sc.nodeCands OR sc.edgeBindings. Anonymous edges
// (v == "") skip per listener.go:349-350. Reachability of the tripwire is zero
// from parser input.
func validateCreateEffect(sc *scope, e query.CreateEffect) error {
	for _, v := range e.Variables() {
		if v == "" {
			continue
		}
		if _, ok := sc.nodeTypes[v]; ok {
			continue
		}
		if _, ok := sc.nodeCands[v]; ok {
			continue
		}
		if _, ok := sc.edgeBindings[v]; ok {
			continue
		}
		return fmt.Errorf("%w: CREATE variable %q not bound after phase C", ErrInvalidEffectTarget, v)
	}
	return nil
}

// validateMergeEffect runs the CREATE variable-presence check and routes each
// SetEffect in OnMatch / OnCreate through the SET-family validators. Sub-sum
// type-safety is guaranteed by query.go:1651-1660 (only Set-family effects can
// appear inside).
func validateMergeEffect(sc *scope, e query.MergeEffect, s schema.Schema) error {
	for _, v := range e.Variables() {
		if v == "" {
			continue
		}
		if _, ok := sc.nodeTypes[v]; ok {
			continue
		}
		if _, ok := sc.nodeCands[v]; ok {
			continue
		}
		if _, ok := sc.edgeBindings[v]; ok {
			continue
		}
		return fmt.Errorf("%w: MERGE variable %q not bound after phase C", ErrInvalidEffectTarget, v)
	}
	for _, se := range e.OnMatch() {
		if err := validateEffect(sc, se, s); err != nil {
			return err
		}
	}
	for _, se := range e.OnCreate() {
		if err := validateEffect(sc, se, s); err != nil {
			return err
		}
	}
	return nil
}

// validateSetPropertyEffect resolves the target Ref against the binding tables
// and looks up the property on the resolved schema entity. Rejects var-length
// edge targets (a var-length binding is a list of edges, not one edge).
// Rejects projection-alias targets and out-of-scope names (defensive tripwire)
// with ErrInvalidEffectTarget.
func validateSetPropertyEffect(sc *scope, e query.SetPropertyEffect, s schema.Schema) error {
	v := e.Target().Variable
	p := e.Target().Property
	if nt, ok := sc.nodeTypes[v]; ok {
		if _, ok := nt.Properties[p]; !ok {
			return fmt.Errorf("%w: %s.%s", ErrUnknownProperty, v, p)
		}
		return nil
	}
	if nts, ok := sc.nodeCands[v]; ok {
		if _, err := unionNodeProperty(nts, v, p, false); err != nil {
			return err
		}
		return nil
	}
	if et, ok := sc.edgeTypes[v]; ok {
		if sc.edgeBindings[v].Hops() != nil {
			return fmt.Errorf("%w: SET on variable-length edge %q", ErrInvalidEffectTarget, v)
		}
		if _, ok := et.Properties[p]; !ok {
			return fmt.Errorf("%w: %s.%s", ErrUnknownProperty, v, p)
		}
		return nil
	}
	if cands, ok := sc.edgeCands[v]; ok {
		if sc.edgeBindings[v].Hops() != nil {
			return fmt.Errorf("%w: SET on variable-length edge %q", ErrInvalidEffectTarget, v)
		}
		if _, err := unionProperty(cands, s, v, p, false); err != nil {
			return err
		}
		return nil
	}
	if _, ok := sc.carriedResolvedTypes[v]; ok {
		return fmt.Errorf("%w: SET %s.%s: %q resolves to a projection alias, not an entity binding", ErrInvalidEffectTarget, v, p, v)
	}
	return fmt.Errorf("%w: SET %s.%s: %q not in any Part scope", ErrInvalidEffectTarget, v, p, v)
}

// validateSetEntityEffect resolves the target variable against the entity
// binding tables. Rejects var-length edge targets and projection-alias / out-
// of-scope targets with ErrInvalidEffectTarget. No property-existence check —
// the RHS map's keys are runtime (per §4.3.2 in the R6 spec).
func validateSetEntityEffect(sc *scope, e query.SetEntityEffect) error {
	v := e.TargetVariable()
	if _, ok := sc.nodeTypes[v]; ok {
		return nil
	}
	if _, ok := sc.nodeCands[v]; ok {
		return nil
	}
	if _, ok := sc.edgeTypes[v]; ok {
		if sc.edgeBindings[v].Hops() != nil {
			return fmt.Errorf("%w: SET on variable-length edge %q", ErrInvalidEffectTarget, v)
		}
		return nil
	}
	if _, ok := sc.edgeCands[v]; ok {
		if sc.edgeBindings[v].Hops() != nil {
			return fmt.Errorf("%w: SET on variable-length edge %q", ErrInvalidEffectTarget, v)
		}
		return nil
	}
	if _, ok := sc.carriedResolvedTypes[v]; ok {
		return fmt.Errorf("%w: SET %s = ...: %q resolves to a projection alias, not an entity binding", ErrInvalidEffectTarget, v, v)
	}
	return fmt.Errorf("%w: SET %s = ...: %q not in any Part scope", ErrInvalidEffectTarget, v, v)
}

// validateSetLabelsEffect verifies the target is a node binding (edges reject
// with ErrInvalidEffectTarget since labels are node-only), then confirms each
// label individually appears in at least one declared NodeType's LabelSet.
// Missing labels surface ErrUnknownLabel per §4.3.3.
func validateSetLabelsEffect(sc *scope, e query.SetLabelsEffect, s schema.Schema) error {
	v := e.TargetVariable()
	if _, ok := sc.nodeTypes[v]; !ok {
		if _, isCand := sc.nodeCands[v]; !isCand {
			if _, ok := sc.edgeBindings[v]; ok {
				return fmt.Errorf("%w: SET labels on edge binding %q", ErrInvalidEffectTarget, v)
			}
			if _, ok := sc.carriedResolvedTypes[v]; ok {
				return fmt.Errorf("%w: SET labels on projection alias %q", ErrInvalidEffectTarget, v)
			}
			return fmt.Errorf("%w: SET %s: %q not in any Part scope", ErrInvalidEffectTarget, v, v)
		}
	}
	for _, L := range e.Labels() {
		if !labelDeclared(L, s) {
			return fmt.Errorf("%w: SET %s:%s: label %q not declared on any node type", ErrUnknownLabel, v, L, L)
		}
	}
	return nil
}

// validateRemovePropertyEffect mirrors validateSetPropertyEffect: same target
// resolution, same property-existence check. No value side to check.
func validateRemovePropertyEffect(sc *scope, e query.RemovePropertyEffect, s schema.Schema) error {
	v := e.Target().Variable
	p := e.Target().Property
	if nt, ok := sc.nodeTypes[v]; ok {
		if _, ok := nt.Properties[p]; !ok {
			return fmt.Errorf("%w: %s.%s", ErrUnknownProperty, v, p)
		}
		return nil
	}
	if nts, ok := sc.nodeCands[v]; ok {
		if _, err := unionNodeProperty(nts, v, p, false); err != nil {
			return err
		}
		return nil
	}
	if et, ok := sc.edgeTypes[v]; ok {
		if sc.edgeBindings[v].Hops() != nil {
			return fmt.Errorf("%w: REMOVE on variable-length edge %q", ErrInvalidEffectTarget, v)
		}
		if _, ok := et.Properties[p]; !ok {
			return fmt.Errorf("%w: %s.%s", ErrUnknownProperty, v, p)
		}
		return nil
	}
	if cands, ok := sc.edgeCands[v]; ok {
		if sc.edgeBindings[v].Hops() != nil {
			return fmt.Errorf("%w: REMOVE on variable-length edge %q", ErrInvalidEffectTarget, v)
		}
		if _, err := unionProperty(cands, s, v, p, false); err != nil {
			return err
		}
		return nil
	}
	if _, ok := sc.carriedResolvedTypes[v]; ok {
		return fmt.Errorf("%w: REMOVE %s.%s: %q resolves to a projection alias, not an entity binding", ErrInvalidEffectTarget, v, p, v)
	}
	return fmt.Errorf("%w: REMOVE %s.%s: %q not in any Part scope", ErrInvalidEffectTarget, v, p, v)
}

// validateRemoveLabelsEffect is the REMOVE analogue of validateSetLabelsEffect:
// same target discipline, same per-label declaration check.
func validateRemoveLabelsEffect(sc *scope, e query.RemoveLabelsEffect, s schema.Schema) error {
	v := e.TargetVariable()
	if _, ok := sc.nodeTypes[v]; !ok {
		if _, isCand := sc.nodeCands[v]; !isCand {
			if _, ok := sc.edgeBindings[v]; ok {
				return fmt.Errorf("%w: REMOVE labels on edge binding %q", ErrInvalidEffectTarget, v)
			}
			if _, ok := sc.carriedResolvedTypes[v]; ok {
				return fmt.Errorf("%w: REMOVE labels on projection alias %q", ErrInvalidEffectTarget, v)
			}
			return fmt.Errorf("%w: REMOVE %s: %q not in any Part scope", ErrInvalidEffectTarget, v, v)
		}
	}
	for _, L := range e.Labels() {
		if !labelDeclared(L, s) {
			return fmt.Errorf("%w: REMOVE %s:%s: label %q not declared on any node type", ErrUnknownLabel, v, L, L)
		}
	}
	return nil
}

// validateDeleteEffect walks e.Targets() for bare-shape checks (entity DELETE
// or bare-property DELETE) and e.Refs() as a defensive walk (parser
// referential integrity already covers them). See §4.4.
func validateDeleteEffect(sc *scope, e query.DeleteEffect, s schema.Schema) error {
	for _, t := range e.Targets() {
		v := t.Variable
		p := t.Property
		if p == "" {
			if _, ok := sc.nodeTypes[v]; ok {
				continue
			}
			if _, ok := sc.nodeCands[v]; ok {
				continue
			}
			if _, ok := sc.edgeTypes[v]; ok {
				continue
			}
			if _, ok := sc.edgeCands[v]; ok {
				continue
			}
			if _, ok := sc.carriedResolvedTypes[v]; ok {
				return fmt.Errorf("%w: DELETE %s: %q resolves to a projection alias, not an entity binding", ErrInvalidEffectTarget, v, v)
			}
			return fmt.Errorf("%w: DELETE %s: %q not in any Part scope", ErrInvalidEffectTarget, v, v)
		}
		if nt, ok := sc.nodeTypes[v]; ok {
			if _, ok := nt.Properties[p]; !ok {
				return fmt.Errorf("%w: %s.%s", ErrUnknownProperty, v, p)
			}
			continue
		}
		if nts, ok := sc.nodeCands[v]; ok {
			if _, err := unionNodeProperty(nts, v, p, false); err != nil {
				return err
			}
			continue
		}
		if et, ok := sc.edgeTypes[v]; ok {
			if sc.edgeBindings[v].Hops() != nil {
				return fmt.Errorf("%w: DELETE on variable-length edge %q", ErrInvalidEffectTarget, v)
			}
			if _, ok := et.Properties[p]; !ok {
				return fmt.Errorf("%w: %s.%s", ErrUnknownProperty, v, p)
			}
			continue
		}
		if cands, ok := sc.edgeCands[v]; ok {
			if sc.edgeBindings[v].Hops() != nil {
				return fmt.Errorf("%w: DELETE on variable-length edge %q", ErrInvalidEffectTarget, v)
			}
			if _, err := unionProperty(cands, s, v, p, false); err != nil {
				return err
			}
			continue
		}
		if _, ok := sc.carriedResolvedTypes[v]; ok {
			return fmt.Errorf("%w: DELETE %s.%s: %q resolves to a projection alias, not an entity binding", ErrInvalidEffectTarget, v, p, v)
		}
		return fmt.Errorf("%w: DELETE %s.%s: %q not in any Part scope", ErrInvalidEffectTarget, v, p, v)
	}
	// Refs walk: parser's referential-integrity sweep covers these; skip per
	// §4.4 step 2 ("R6 runs no additional check on e.Refs()").
	return nil
}

// resolveNodeLabels resolves a query node binding's label set to the set of
// declared node types that satisfy it (ISO 39075 §16.8 satisfaction). A type
// satisfies `labels` when its complete label set is a superset of `labels`
// — including the case where equality holds. The exact-match fast path that
// previously gave identity-match precedence is removed (ADR 0022): satisfaction
// is satisfaction, and every satisfying type is returned.
//
// Returns a non-empty slice on success. The caller dispatches:
//   - len == 1: singular, bind via BindNode.
//   - len > 1:  plural, bind via BindNodeCands; property projection uses
//     intersection (unionNodeProperty); whole-entity reference is refused
//     with ErrAmbiguousLabel.
//
// Zero satisfying types with all labels declared returns ErrUnknownLabel
// (no type carries the full combination). Any undeclared label also returns
// ErrUnknownLabel (per-label check fires first).
//
// The satisfying set tests against each declared type's COMPLETE label set,
// never its identity. An element carries every label its type implies, so
// implied labels satisfy a query expression exactly as key labels do; keying
// satisfaction on identity would make a `=>` declaration unmatchable by the
// labels it implies.
func resolveNodeLabels(labels graph.LabelSet, s schema.Schema) ([]schema.NodeType, error) {
	if undeclared := undeclaredLabels(labels, s); len(undeclared) > 0 {
		return nil, fmt.Errorf("%w: %s is not declared on any node type", ErrUnknownLabel, strings.Join(undeclared, ", "))
	}
	satisfying := satisfyingNodeTypes(labels, s)
	if len(satisfying) == 0 {
		return nil, fmt.Errorf("%w: no node type satisfies %s; declared types carrying these labels: %s", ErrUnknownLabel, labels.Key(), formatDeclaredCarrying(labels, s))
	}
	result := make([]schema.NodeType, len(satisfying))
	for i, id := range satisfying {
		result[i] = s.Nodes[id]
	}
	return result, nil
}

// undeclaredLabels returns the labels in `labels` carried by no declared node
// type. Order-preserving so the diagnostic reads in query-source order.
func undeclaredLabels(labels graph.LabelSet, s schema.Schema) []string {
	var out []string
	for _, l := range labels {
		if !labelDeclared(l, s) {
			out = append(out, l)
		}
	}
	return out
}

// satisfyingNodeTypes returns the identities of the declared node types whose
// complete label set is a superset of `labels`. It returns identities because
// the caller indexes s.Nodes with them; the superset test itself reads the
// complete label set.
//
// The superset is not required to be proper. It could not be reached by an
// equal set before gqlc-h9n.8, because the caller's identity fast path caught
// every equality; a `=>` type whose complete label set equals `labels` but whose
// identity does not arrives here, and admitting it is the point.
func satisfyingNodeTypes(labels graph.LabelSet, s schema.Schema) []graph.LabelSetKey {
	want := make(map[string]struct{}, len(labels))
	for _, l := range labels {
		want[l] = struct{}{}
	}
	var out []graph.LabelSetKey
	for id, nt := range s.Nodes {
		carried := make(map[string]struct{})
		for _, l := range nt.CompleteLabels.Split() {
			carried[l] = struct{}{}
		}
		ok := true
		for l := range want {
			if _, has := carried[l]; !has {
				ok = false
				break
			}
		}
		if ok {
			out = append(out, id)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// formatDeclaredCarrying lists the identities of declared node types that carry
// ANY of the queried labels — the "here are the near misses" hint. Used only
// when the satisfying set is empty but every queried label is declared
// somewhere.
func formatDeclaredCarrying(labels graph.LabelSet, s schema.Schema) string {
	want := make(map[string]struct{}, len(labels))
	for _, l := range labels {
		want[l] = struct{}{}
	}
	var keys []graph.LabelSetKey
	for id, nt := range s.Nodes {
		for _, l := range nt.CompleteLabels.Split() {
			if _, has := want[l]; has {
				keys = append(keys, id)
				break
			}
		}
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	return formatKeys(keys)
}

func formatKeys(keys []graph.LabelSetKey) string {
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = string(k)
	}
	return strings.Join(parts, ", ")
}

// labelDeclared reports whether label is carried by any declared NodeType —
// the R6 policy per §4.3.3 (per-label existence, not union-existence). It reads
// the complete label set, because an implied label is one an element carries
// and so is one a query may name. Naive O(|s.Nodes| × avg-arity) iteration;
// schemas are small.
func labelDeclared(label string, s schema.Schema) bool {
	for _, nt := range s.Nodes {
		for _, lbl := range nt.CompleteLabels.Split() {
			if lbl == label {
				return true
			}
		}
	}
	return false
}

// argAssignable reports whether an argument mined to argType at parse can be
// assigned to a signature parameter declared with token. The 0ig-adopted
// assignability lattice (spec §8.2):
//
//   - INTEGER: strict — TypeInt only.
//   - FLOAT:   loose  — TypeFloat OR TypeInt (TCK Call3 [5] admits INTEGER
//     at a FLOAT-typed position; ADR 0007 line 173 does not exclude this).
//   - STRING:  strict — TypeString only.
//   - NUMBER:  loose  — TypeInt OR TypeFloat (ADR 0007 line 172-174:
//     "assignable-from INTEGER-or-FLOAT at the argument site").
//
// TypeUnknown and TypeNull are resolver-side wildcards: TypeUnknown for a
// $param / n.name argument (the parser cannot type-narrow those at CALL-site),
// TypeNull for a bare null literal (shape.go:79 mines NULL to TypeNull, a
// distinct sum member from TypeUnknown per type.go:80). Admitting both
// preserves R7's parser-authoritative posture and validates TCK Call5 [4]
// (CALL test.my.proc(null) against nullable-typed params). A downstream
// $param whose enclosing type disagrees with the sig token is caught by the
// parameter-unification pass in ExprInProjection anyway.
func argAssignable(token procsig.TypeToken, argType query.Type) bool {
	if _, isUnknown := argType.(query.TypeUnknown); isUnknown {
		return true
	}
	if _, isNull := argType.(query.TypeNull); isNull {
		return true
	}
	switch token {
	case procsig.TokenInteger:
		_, ok := argType.(query.TypeInt)
		return ok
	case procsig.TokenFloat:
		_, isFloat := argType.(query.TypeFloat)
		_, isInt := argType.(query.TypeInt) // §8.2.1: TCK Call3 [5] admits INTEGER-at-FLOAT.
		return isFloat || isInt
	case procsig.TokenString:
		_, ok := argType.(query.TypeString)
		return ok
	case procsig.TokenNumber:
		_, isFloat := argType.(query.TypeFloat)
		_, isInt := argType.(query.TypeInt)
		return isFloat || isInt
	default:
		return false
	}
}
