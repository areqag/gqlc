package age

// Bridges this package's unexported surface to its external test package.
//
// THIS FILE DECLARES NO IMPORTS, and that is load-bearing rather than tidy.
// vuln-root-residual reads a package's blindness off its .TestImports walked
// transitively, so an import here whose closure reaches third-party code puts
// internal/codegen/age straight back into govulncheck's blind set and undoes
// the conversion this file exists to serve (bd gqlc-5utdh, ADR 0026).
//
// Type inference is what makes that possible: `var X = x` and `type X = x`
// carry a function or a type without naming the packages in its signature.
// Where a test needs an unexported FIELD, the accessor below spells only
// builtin and own-package types for the same reason.

type (
	DialectGap   = dialectGap
	DialectProbe = dialectProbe
	Finding      = finding
	Helpers      = helpers
	TypeMap      = typeMap
	WiredEntity  = wiredEntity
)

var (
	DecodeFunc                       = decodeFunc
	DialectGaps                      = dialectGaps
	DollarTag                        = dollarTag
	EdgeUnionReason                  = edgeUnionReason
	FindRelationshipTypeAlternations = findRelationshipTypeAlternations
	FindUndefinedFunctions           = findUndefinedFunctions
	FindUndefinedNamespaces          = findUndefinedNamespaces
	FindUndefinedSpatialFunctions    = findUndefinedSpatialFunctions
	Generate                         = generate
	NamespaceProbes                  = namespaceProbes
	RejectOffsetSidecarCollisions    = rejectOffsetSidecarCollisions
	RenderCypherFile                 = renderCypherFile
	RenderModels                     = renderModels
	SpatialFunctionProbes            = spatialFunctionProbes
	UndefinedFunctionProbes          = undefinedFunctionProbes
	UndefinedFunctions               = undefinedFunctions
	UndefinedNamespaces              = undefinedNamespaces
	UndefinedSpatialFunctions        = undefinedSpatialFunctions
	UnservedColumn                   = unservedColumn
	UnservedReason                   = unservedReason
	WriteEntityFieldDecode           = writeEntityFieldDecode
)

const (
	GoInstant        = goInstant
	VertexAnnotation = vertexAnnotation
)

// An external test cannot name an unexported field, so the accessors below
// carry the ones its tests read and write. Every signature here spells only
// builtin and own-package types, which is what keeps the no-import rule
// above intact; where a signature would have to name another package —
// forParams takes []codegen.Param — the bridge is a method expression
// instead, and the call site spells the type it already imports.

// DialectGapFields is the external test package's spelling of a gap's
// unexported fields. It exists so a test can keep writing a labelled
// composite literal rather than a positional constructor of six arguments,
// which is the form the sweep's rows are read in.
type DialectGapFields struct {
	Sentinel error
	Find     func(src string) []Finding
	Diagnose func(count int, noun, dropped string) string
	Witness  string
	Refused  []dialectProbe
	Served   []string
}

func (f DialectGapFields) Build() dialectGap {
	return dialectGap{
		sentinel: f.Sentinel,
		find:     f.Find,
		diagnose: f.Diagnose,
		witness:  f.Witness,
		refused:  f.Refused,
		served:   f.Served,
	}
}

func NewDialectProbe(text, answer string) dialectProbe {
	return dialectProbe{text: text, answer: answer}
}

func (f finding) Text() string { return f.text }
func (f finding) Line() int    { return f.line }
func (f finding) Column() int  { return f.column }

func (p dialectProbe) Text() string   { return p.text }
func (p dialectProbe) Answer() string { return p.answer }

func (g dialectGap) Sentinel() error                            { return g.sentinel }
func (g dialectGap) Find() func(src string) []Finding           { return g.find }
func (g dialectGap) Diagnose() func(int, string, string) string { return g.diagnose }
func (g dialectGap) Witness() string                            { return g.witness }
func (g dialectGap) Refused() []dialectProbe                    { return g.refused }
func (g dialectGap) Served() []string                           { return g.served }

func (g *dialectGap) SetSentinel(s error)                            { g.sentinel = s }
func (g *dialectGap) SetFind(f func(src string) []Finding)           { g.find = f }
func (g *dialectGap) SetDiagnose(d func(int, string, string) string) { g.diagnose = d }
func (g *dialectGap) SetWitness(w string)                            { g.witness = w }
func (g *dialectGap) SetRefused(p []dialectProbe)                    { g.refused = p }
func (g *dialectGap) SetServed(s []string)                           { g.served = s }

func (h helpers) Zone() bool        { return h.zone }
func (h helpers) Instant() bool     { return h.instant }
func (h helpers) TimeZone() bool    { return h.timeZone }
func (h helpers) ImportsTime() bool { return h.importsTime() }

func (h *helpers) ForEntities(entities []wiredEntity) { h.forEntities(entities) }

// HelpersForParams is a method expression rather than a method: forParams
// takes []codegen.Param, and spelling that here would import
// internal/codegen into this file.
var HelpersForParams = (*helpers).forParams

// WithLabels returns a copy carrying the wire label and annotation, so a
// test can build one inside a slice literal.
func (w wiredEntity) WithLabels(label, annotation string) wiredEntity {
	w.label, w.annotation = label, annotation
	return w
}
