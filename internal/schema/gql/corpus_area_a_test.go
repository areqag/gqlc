package gql_test

import "github.com/areqag/gqlc/internal/schema/gql"

// corpusAreaA holds the corpus entries for the graph type statement itself, the
// references that name a source graph type, and the identifier and nested-body
// grammar every other area builds on. One area variable per author so that two
// authors never edit the same Go file; corpusAreas fixes the directories these
// entries may live in.
var corpusAreaA = []corpusEntry{
	{
		file:    "12.6-graph-type-statement/nested_body.gql",
		outcome: resolves,
		feature: "mandatory",
	},
	{
		file:    "12.6-graph-type-statement/create_if_not_exists.gql",
		outcome: resolves,
		feature: "mandatory",
	},
	{
		file:    "12.6-graph-type-statement/create_or_replace.gql",
		outcome: resolves,
		feature: "mandatory",
	},
	{
		file:    "12.6-graph-type-statement/synonyms.gql",
		outcome: resolves,
		feature: "mandatory",
	},
	{
		file:    "12.6-graph-type-statement/synonyms_node_edge.gql",
		outcome: resolves,
		feature: "mandatory",
	},
	{
		file:    "12.6-graph-type-statement/delimited_identifiers.gql",
		outcome: resolves,
		feature: "mandatory",
	},
	{
		file:    "18.1-nested-graph-type/element_types.gql",
		outcome: resolves,
		feature: "mandatory",
	},
	{
		file:     "12.6-graph-type-statement/like_graph.gql",
		outcome:  unsupported,
		sentinel: gql.ErrLikeGraphSource,
		feature:  "GG04",
		bead:     "gqlc-0ri",
		reason:   "LIKE takes a graphExpression, which reaches CURRENT_GRAPH and binding variables — session state a static generator has none of, so no catalogue would make this resolvable; declined permanently",
	},
	{
		file:    "12.6-graph-type-statement/copy_of_source.gql",
		outcome: resolves,
		feature: "mandatory",
	},
	{
		file:    "12.6-graph-type-statement/schemas/base/Source.gql",
		outcome: resolves,
		feature: "mandatory",
	},
	{
		file:    "17-references/copy_of_graph_type_bare.gql",
		outcome: resolves,
		feature: "mandatory",
	},
	{
		file:    "17-references/copy_of_absolute_bare.gql",
		outcome: resolves,
		feature: "mandatory",
	},
	{
		file:    "17-references/copy_of_predefined_current.gql",
		outcome: resolves,
		feature: "mandatory",
	},
	{
		file:    "17-references/copy_of_current_schema.gql",
		outcome: resolves,
		feature: "mandatory",
	},
	{
		file:    "17-references/copy_of_chain_climb.gql",
		outcome: resolves,
		feature: "mandatory",
	},
	{
		file:    "17-references/gt.gql",
		outcome: resolves,
		feature: "mandatory",
	},
	{
		file:    "17-references/Source.gql",
		outcome: resolves,
		feature: "mandatory",
	},
	{
		file:    "17-references/s/base.gql",
		outcome: resolves,
		feature: "mandatory",
	},
	{
		file:    "17-references/liar.gql",
		outcome: resolves,
		feature: "mandatory",
	},
	{
		file:     "17-references/copy_of_param_graph_type.gql",
		outcome:  unsupported,
		sentinel: gql.ErrReferenceParameter,
		feature:  "mandatory",
		bead:     "gqlc-0ri",
		reason:   "graphTypeReference alternative 2 — a parameter reference where the graph type name goes. A parameter is bound at execution time and a build-time catalogue has no parameter values, so no catalogue would make this resolvable; declined permanently",
	},
	{
		file:     "17-references/copy_of_param_schema.gql",
		outcome:  unsupported,
		sentinel: gql.ErrReferenceParameter,
		feature:  "mandatory",
		bead:     "gqlc-0ri",
		reason:   "schemaReference alternative 3 — the same parameter decline one position further left, where the schema name goes rather than the graph type name; neither spelling discharges the other",
	},
	{
		file:     "17-references/copy_of_qualified.gql",
		outcome:  unsupported,
		sentinel: gql.ErrObjectParentReference,
		feature:  "mandatory",
		bead:     "gqlc-0ri",
		reason:   "catalogObjectParentReference alternative 2 — a dotted-name parent with no schema reference. An object parent is a catalogue object containing other objects, and a directory-backed catalogue has no container between a directory and a file",
	},
	{
		file:     "17-references/copy_of_schema_and_object.gql",
		outcome:  unsupported,
		sentinel: gql.ErrObjectParentReference,
		feature:  "mandatory",
		bead:     "gqlc-0ri",
		reason:   "catalogObjectParentReference alternative 1 taking its (objectName PERIOD)* repetition after a schema reference, which alternative 2 reaches without one — same decline, a second alternative",
	},
	{
		file:     "17-references/copy_of_home_schema.gql",
		outcome:  unsupported,
		sentinel: gql.ErrHomeSchemaReference,
		feature:  "mandatory",
		bead:     "gqlc-0ri",
		reason:   "HOME_SCHEMA keyword form of predefinedSchemaReference. It is a property of a session and gqlc has none; unlike CURRENT_SCHEMA, which resolves in this directory, it has no static referent to translate to",
	},
	{
		file:     "17-references/copy_of_delimited.gql",
		outcome:  unsupported,
		sentinel: gql.ErrDelimitedReferenceSegment,
		feature:  "mandatory",
		bead:     "gqlc-0ri",
		reason:   "a delimited identifier where a path segment goes: it may contain a solidus, a full stop, or nothing at all, so it is not one safe path element. Accepting more later is non-breaking",
	},
	{
		file:     "17-references/copy_of_relative_up.gql",
		outcome:  unsupported,
		sentinel: gql.ErrReferenceOutsideCatalogue,
		feature:  "mandatory",
		bead:     "gqlc-0ri",
		reason:   "relativeDirectoryPath form of relativeCatalogSchemaReference — ../s reaches DOUBLE_PERIOD, and bare ../gt does not parse. This file is its own catalogue root, so the climb has nowhere to pop to; copy_of_chain_climb.gql is the same climb succeeding one directory down",
	},
	{
		file:     "17-references/copy_of_relative_up_twice.gql",
		outcome:  unsupported,
		sentinel: gql.ErrReferenceOutsideCatalogue,
		feature:  "mandatory",
		bead:     "gqlc-0ri",
		reason:   "relativeDirectoryPath climbing two levels — the repetition copy_of_relative_up.gql takes zero times. The second .. is the whole difference, and it escapes for the same reason",
	},
	{
		file:     "17-references/sub/climber.gql",
		outcome:  unsupported,
		sentinel: gql.ErrReferenceOutsideCatalogue,
		feature:  "mandatory",
		bead:     "gqlc-0ri",
		reason:   "the same ../s/base that resolves when copy_of_chain_climb.gql reaches this file as a hop. Here the file IS the root, so the pop leaves the catalogue — identical bytes, opposite outcomes, which is what pins the climb to the referencing file's directory",
	},
	{
		file:     "17-references/copy_of_dangling.gql",
		outcome:  unsupported,
		sentinel: gql.ErrDanglingReference,
		feature:  "mandatory",
		bead:     "gqlc-0ri",
		reason:   "a supported spelling naming no file. Reachable from the same reference text as the escape above, which is why the two are separate sentinels rather than one resolution failure",
	},
	{
		file:     "17-references/copy_of_name_mismatch.gql",
		outcome:  unsupported,
		sentinel: gql.ErrReferenceNameMismatch,
		feature:  "mandatory",
		bead:     "gqlc-0ri",
		reason:   "liar.gql was found as `liar` and declares NotLiar. A referenced file promises that its declaration matches the name that found it; only the root of a load is exempt, being reached by a configured path",
	},
	{
		file:     "17-references/cycle_self.gql",
		outcome:  unsupported,
		sentinel: gql.ErrReferenceCycle,
		feature:  "mandatory",
		bead:     "gqlc-0ri",
		reason:   "the one-element cycle. A declaration has one source and a COPY OF source one reference, so chains are linear and a cycle is the only way one fails to terminate",
	},
	{
		file:     "17-references/cycle_a.gql",
		outcome:  unsupported,
		sentinel: gql.ErrReferenceCycle,
		feature:  "mandatory",
		bead:     "gqlc-0ri",
		reason:   "half of the two-element cycle, which unlike the self-copy is caught only by remembering every file already visited rather than by the first hop returning to its start",
	},
	{
		file:     "17-references/cycle_b.gql",
		outcome:  unsupported,
		sentinel: gql.ErrReferenceCycle,
		feature:  "mandatory",
		bead:     "gqlc-0ri",
		reason:   "the other half, entered from the far side so that the cycle is reported whichever of the two a reader loads first",
	},
}

// semanticAreaA holds this area's semantic cases: files above that resolve to a
// model known to be wrong. Empty, and declared anyway so that recording one is an
// edit here rather than to the shared corpus_test.go. If the linter reports this
// unused, corpusAreas has lost its `semantic:` entry — TestCorpusManifest says so
// too, by area name. Wire it back rather than deleting this, which is the only
// thing standing between an author and that edit. Keep the []semanticCase{}
// spelling: the manifest requires non-nil, so `var x []semanticCase` reads as a
// lost wiring.
var semanticAreaA = []semanticCase{}
