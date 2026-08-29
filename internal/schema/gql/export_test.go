package gql

// Opens the property-type spellings, the label-set key derivation and the parse
// listener to the external test package. The whole suite moved to package
// gql_test to get inside govulncheck's call graph, which discards the
// in-package test variant and everything only it imports (bd gqlc-m5rc).
//
// This file imports nothing, and that is load-bearing rather than incidental:
// `vuln-root-residual` measures a package's blindness from the imports of its
// in-package test files, so a single third-party import here would return
// internal/schema/gql to the blind set and undo the conversion. It is why the
// listener is reached through NewListener — whose signature is inferred from
// the production constructor — rather than through a constructor written out
// here, which would have had to name *antlr.CommonTokenStream.
type Listener = listener

var (
	CanonicalSpelling = canonicalSpelling
	LabelSets         = labelSets
	NewListener       = newListener
	// A method expression rather than a method: walk's parameter is antlr.Tree,
	// which a declared method here would have to name.
	ListenerWalk          = (*listener).walk
	Property              = property
	TruncateParenthetical = truncateParenthetical
	TypeSpellings         = typeSpellings
)

// The listener's fields stay unexported: production code does not change shape
// to suit a scanner, so the reads the tests need are accessors declared here.
func (l *listener) Err() error          { return l.err }
func (l *listener) RawNodes() []rawNode { return l.raw.nodes }
func (l *listener) RawEdges() []rawEdge { return l.raw.edges }
