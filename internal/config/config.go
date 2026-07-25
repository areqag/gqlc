// Package config loads and saves the gqlc config file: the hand-written
// YAML manifest that declares a project's generation targets, each one a
// schema path, a query directory, the three tool axes and a generated Go
// package. See docs/specs/config-file-format.md. The package is
// CLI-agnostic: it returns raw values and never touches the filesystem
// beyond the config file itself.
package config

import (
	"bytes"
	"fmt"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// DefaultFilename is the canonical config file name (§2). There are no
// .yml or .json variants and no search logic; callers that want another
// path pass it explicitly.
const DefaultFilename = "gqlc.yaml"

// fileVersion is the on-disk format version this loader accepts and
// Save emits. The version key is wire-only: a loaded Config is always
// the latest in-memory shape, whatever version the file declared.
const fileVersion = 1

// SchemaLang is the closed vocabulary of the wire key "schema_language":
// the language the schema file is written in.
type SchemaLang string

// SchemaLangGQL is the ISO GQL graph-type schema language.
const SchemaLangGQL SchemaLang = "gql"

// SchemaLangValues lists every valid SchemaLang. Loader errors and
// future `gqlc init` prompts both derive their choices from this slice.
func SchemaLangValues() []SchemaLang { return []SchemaLang{SchemaLangGQL} }

// UnmarshalYAML validates vocabulary membership at decode time so the
// error carries the offending node's line.
func (s *SchemaLang) UnmarshalYAML(value *yaml.Node) error {
	v, err := enumFromNode(value, "schema_language", SchemaLangValues())
	if err != nil {
		return err
	}
	*s = v
	return nil
}

// QueryLang is the closed vocabulary of the wire key "query_language":
// the language the query files are written in.
type QueryLang string

// QueryLangOpenCypher is the openCypher query language.
const QueryLangOpenCypher QueryLang = "opencypher"

// QueryLangValues lists every valid QueryLang. Loader errors and future
// `gqlc init` prompts both derive their choices from this slice.
func QueryLangValues() []QueryLang { return []QueryLang{QueryLangOpenCypher} }

// UnmarshalYAML validates vocabulary membership at decode time so the
// error carries the offending node's line.
func (q *QueryLang) UnmarshalYAML(value *yaml.Node) error {
	v, err := enumFromNode(value, "query_language", QueryLangValues())
	if err != nil {
		return err
	}
	*q = v
	return nil
}

// Driver is the closed vocabulary of the wire key "driver": the client
// library the generated code targets.
type Driver string

// DriverNeo4jGoV5 is the official Neo4j Go driver, major version 5.
const DriverNeo4jGoV5 Driver = "neo4j-go-v5"

// DriverNeo4jGoV6 is the official Neo4j Go driver, major version 6.
const DriverNeo4jGoV6 Driver = "neo4j-go-v6"

// DriverValues lists every valid Driver. Loader errors and future
// `gqlc init` prompts both derive their choices from this slice.
func DriverValues() []Driver { return []Driver{DriverNeo4jGoV5, DriverNeo4jGoV6} }

// UnmarshalYAML validates vocabulary membership at decode time so the
// error carries the offending node's line.
func (d *Driver) UnmarshalYAML(value *yaml.Node) error {
	v, err := enumFromNode(value, "driver", DriverValues())
	if err != nil {
		return err
	}
	*d = v
	return nil
}

// enumFromNode resolves a YAML scalar into a member of valid, or
// reports the line, the offending value, and the whole vocabulary. A
// non-scalar node is named as such — its Value is the empty string,
// which would otherwise misreport a sequence as `invalid driver ""`.
func enumFromNode[T ~string](value *yaml.Node, wireKey string, valid []T) (T, error) {
	var zero T
	if value.Kind != yaml.ScalarNode {
		return zero, fmt.Errorf("line %d: invalid %s: expected a scalar value, got a YAML %s", value.Line, wireKey, kindName(value.Kind))
	}
	for _, v := range valid {
		if value.Value == string(v) {
			return v, nil
		}
	}
	return zero, fmt.Errorf("line %d: invalid %s %q (valid values: %s)", value.Line, wireKey, value.Value, joinValues(valid))
}

// kindName names a yaml.Node kind for error messages.
func kindName(k yaml.Kind) string {
	switch k {
	case yaml.DocumentNode:
		return "document"
	case yaml.SequenceNode:
		return "sequence"
	case yaml.MappingNode:
		return "mapping"
	case yaml.ScalarNode:
		return "scalar"
	case yaml.AliasNode:
		return "alias"
	}
	return fmt.Sprintf("node kind %d", k)
}

// joinValues renders an enum vocabulary for error messages.
func joinValues[T ~string](valid []T) string {
	parts := make([]string, len(valid))
	for i, v := range valid {
		parts[i] = string(v)
	}
	return strings.Join(parts, ", ")
}

// Config is the canonical in-memory form of a config file, always the
// latest shape regardless of the on-disk version it was loaded from.
// There is deliberately no Version field: version is wire-only, and
// Save always writes the latest format.
//
// Relative paths are relative to the config file's directory — that is
// format semantics the CLI implements; the loader returns raw strings
// and never resolves or checks paths (config-file-format §4).
type Config struct {
	// Targets are the generation targets the file declares, in document
	// order (wire key "graph").
	Targets []Target
}

// Target is one generation target: one schema and query directory front-
// ended into one generated Go package. Field order is wire order.
type Target struct {
	// SchemaPath locates this target's schema file (wire key "schema").
	SchemaPath string
	// SchemaLang is the language the schema file is written in (wire
	// key "schema_language").
	SchemaLang SchemaLang
	// QueryDir locates the directory holding this target's query files
	// (wire key "queries").
	QueryDir string
	// QueryLang is the language the query files are written in (wire
	// key "query_language").
	QueryLang QueryLang
	// ProcsigPath locates the optional procedure-signature registry
	// file (wire key "procsig"). Empty means the key was omitted.
	ProcsigPath string
	// Go is this target's code-generation block (wire key "gen.go").
	Go GoGen
}

// GoGen is a target's gen.go block.
type GoGen struct {
	// Package names the generated Go package (wire key
	// "gen.go.package"); it must be a valid Go identifier.
	Package string
	// Out locates the directory generated code is written to (wire key
	// "gen.go.out"), owned exclusively by gqlc (ADR 0012).
	Out string
	// Driver is the client library the generated code targets (wire key
	// "gen.go.driver").
	Driver Driver
}

// wireV1 mirrors the multi-target spec §2.1. Graph is a value slice, not
// a pointer: the scalar keys need pointers because the strict decode is
// the only pass that sees them, but graph's absent, null, non-sequence
// and empty cases are all settled by the document scan before the strict
// decode runs. The value type is also what makes a zero Config marshal
// to "graph: []" — the empty-sequence complaint — where a nil pointer
// would marshal to "graph: null" and Load would claim the key is absent
// (§5).
//
// These type names reach users through yaml's unknown-key messages.
type wireV1 struct {
	Version *int         `yaml:"version"`
	Graph   []wireTarget `yaml:"graph"`
}

// wireTarget mirrors §2.2; every field is a pointer so an omitted key is
// distinguishable from an explicit empty value.
type wireTarget struct {
	SchemaPath  *string     `yaml:"schema"`
	SchemaLang  *SchemaLang `yaml:"schema_language"`
	QueryDir    *string     `yaml:"queries"`
	QueryLang   *QueryLang  `yaml:"query_language"`
	ProcsigPath *string     `yaml:"procsig,omitempty"`
	Gen         *wireGen    `yaml:"gen"`
}

// wireGen mirrors §2.3. The level exists because a second generated
// language would be a sibling key of "go"; today any other key rejects.
type wireGen struct {
	Go *wireGo `yaml:"go"`
}

// wireGo mirrors §2.3's gen.go table.
type wireGo struct {
	Package *string `yaml:"package"`
	Out     *string `yaml:"out"`
	Driver  *Driver `yaml:"driver"`
}

// strictInt decodes only a true YAML integer scalar (tag !!int).
// Without it the version probe would inherit yaml.v3's numeric
// coercion — `version: 1.5` truncating to 1, `version: 0.9` to 0 — at
// the one field that guards format evolution.
type strictInt int

// UnmarshalYAML enforces the !!int tag before decoding the value.
func (i *strictInt) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind != yaml.ScalarNode {
		return fmt.Errorf("line %d: field \"version\" must be a YAML integer (got a YAML %s)", value.Line, kindName(value.Kind))
	}
	if value.Tag != "!!int" {
		return fmt.Errorf("line %d: field \"version\" must be a YAML integer (got %s %q)", value.Line, value.Tag, value.Value)
	}
	var n int
	if err := value.Decode(&n); err != nil {
		return fmt.Errorf("field \"version\": %w", err)
	}
	*i = strictInt(n)
	return nil
}

// versionProbe is the lenient first-pass decode target (§5): only the
// version key, read tag-strictly. A named type, so yaml's structural
// errors on a non-mapping document cite something readable instead of
// an anonymous struct literal.
type versionProbe struct {
	Version *strictInt `yaml:"version"`
}

// Load reads the config file at path and returns the canonical Config.
// Open failures wrap the underlying error, so
// errors.Is(err, fs.ErrNotExist) holds for a missing file — a future
// `gqlc init` branches on that to offer creation.
func Load(path string) (Config, error) {
	blob, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("config: open %s: %w", path, err)
	}
	return decode(bytes.NewReader(blob), path)
}

// Decode reads a config from r, without touching the filesystem.
// Errors label the source as "<stream>".
func Decode(r io.Reader) (Config, error) {
	return decode(r, "<stream>")
}

// decode is the shared body of Load and Decode. src labels the origin
// (a file path or "<stream>") in error messages.
func decode(r io.Reader, src string) (Config, error) {
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(r); err != nil {
		return Config{}, fmt.Errorf("config: read %s: %w", src, err)
	}
	// A zero-byte input is a truncation or a stub, never a valid
	// config: every field is required, so there is no meaningful empty
	// form to accept.
	if buf.Len() == 0 {
		return Config{}, fmt.Errorf("config: %s is empty (expected a gqlc config declaring version: 1)", src)
	}
	body := buf.Bytes()

	// Version probe, then dispatch — the versioning seam (§5). The
	// probe reads only the version key (other keys pass unexamined,
	// but the version itself is tag-strict); each accepted version
	// gets its own decoder that normalises into the one Config. There
	// is deliberately no version interface: one file format, one seam.
	var probe versionProbe
	if err := yaml.Unmarshal(body, &probe); err != nil {
		return Config{}, fmt.Errorf("config: %s: %w", src, err)
	}
	if probe.Version == nil {
		return Config{}, fmt.Errorf("config: %s: missing required field \"version\" (this gqlc supports version %d)", src, fileVersion)
	}
	if *probe.Version != fileVersion {
		return Config{}, fmt.Errorf("config: %s: declares version %d; only version %d is supported", src, *probe.Version, fileVersion)
	}
	return decodeV1(body, src)
}

// decodeV1 decodes the version 1 wire shape in the stages the spec §4.6
// pins, reporting the first stage that fails.
func decodeV1(body []byte, src string) (Config, error) {
	// Stage 2 — the document scan. Safe without shape guards of its own
	// because it runs after the version check: the probe is a struct
	// decode, so a non-mapping root already failed it, and a document
	// with no content at all already failed the version-omitted check.
	// The parse cannot fail either — the probe ran the same bytes
	// through the same parser — but unreachable is not impossible.
	var doc yaml.Node
	if err := yaml.Unmarshal(body, &doc); err != nil {
		return Config{}, fmt.Errorf("config: %s: %w", src, err)
	}
	root := doc.Content[0]

	// Stage 3 — before the strict decode, so the targeted message beats
	// the unknown-key wall.
	if err := checkOldFlatShape(root); err != nil {
		return Config{}, fmt.Errorf("config: %s: %w", src, err)
	}

	// Stage 4.
	graphSeq, err := graphSequence(root)
	if err != nil {
		return Config{}, fmt.Errorf("config: %s: %w", src, err)
	}

	// Stage 5 — needs stage 4's verdict: there are no elements to index
	// until graph is known to be a sequence.
	if err := checkNullEntries(graphSeq); err != nil {
		return Config{}, fmt.Errorf("config: %s: %w", src, err)
	}

	// Stage 6 — the strict decode. Unknown keys reject, so typos surface
	// instead of silently dropping.
	dec := yaml.NewDecoder(bytes.NewReader(body))
	dec.KnownFields(true)
	var w wireV1
	if err := dec.Decode(&w); err != nil {
		return Config{}, fmt.Errorf("config: %s: %w", src, err)
	}

	// Stage 7 — every index stages 8 and 9 print refers to the entries
	// the file declares only if this holds.
	if err := checkEntryCount(graphSeq, len(w.Graph)); err != nil {
		return Config{}, fmt.Errorf("config: %s: %w", src, err)
	}

	// Stage 8.
	cfg := Config{Targets: make([]Target, 0, len(w.Graph))}
	for i, wt := range w.Graph {
		target, err := decodeTarget(wt)
		if err != nil {
			return Config{}, fmt.Errorf("config: %s: graph[%d]: %w", src, i, err)
		}
		cfg.Targets = append(cfg.Targets, target)
	}

	// Stage 9 — each entry against the entries before it, so the whole
	// rule lives in CheckOutAgainst and nowhere else.
	for i := range cfg.Targets {
		if err := (Config{Targets: cfg.Targets[:i]}).CheckOutAgainst(cfg.Targets[i].Go.Out); err != nil {
			return Config{}, fmt.Errorf("config: %s: graph[%d]: %w", src, i, err)
		}
	}
	return cfg, nil
}

// resolveNode follows an alias chain to the node it names. Every kind or
// tag test the document scan makes runs on the result and reports the
// alias node's own Line: an alias carries an empty Tag and an AliasNode
// Kind of its own, so a test on the alias itself sees neither the tag
// nor the kind the document actually supplies — an unresolved !!null
// test passes `- *none`, which yaml.v3 then drops, renumbering every
// later entry index. Anchoring an alias is a parse error in every YAML
// syntax, so the loop never runs twice; it is a loop rather than a
// single dereference so that is not a silent bet.
func resolveNode(n *yaml.Node) *yaml.Node {
	for n.Kind == yaml.AliasNode && n.Alias != nil {
		n = n.Alias
	}
	return n
}

// flatKeys is the frozen version-1-flat wire vocabulary (§4.2): the
// top-level keys of the format that preceded the graph sequence, in the
// order the loader reports them. Nothing will be added to it.
var flatKeys = []string{"schema", "queries", "output", "package", "schema_language", "query_language", "driver", "procsig"}

// mappingValue returns the value node of key in a mapping node, and the
// key's own node. Lookup is literal: a merge key (`<<:`) supplies keys
// yaml.v3 resolves at decode time and the scan cannot see (§4).
func mappingValue(mapping *yaml.Node, key string) (value, keyNode *yaml.Node) {
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			return mapping.Content[i+1], mapping.Content[i]
		}
	}
	return nil, nil
}

// checkOldFlatShape reports the §4.2 message for a file written in the
// format that preceded the graph sequence. Such a file declares
// version 1 truthfully, so without this it reaches the strict decode and
// gets one unknown-key line per key it happens to carry and no mention
// that the format changed. A document carrying both graph and a former
// key is not this case — that is a new-format file with a leftover key,
// and the unknown-key error says so.
func checkOldFlatShape(root *yaml.Node) error {
	if v, _ := mappingValue(root, "graph"); v != nil {
		return nil
	}
	for _, key := range flatKeys {
		_, keyNode := mappingValue(root, key)
		if keyNode == nil {
			continue
		}
		// The key node's line, not the value's, so a block or multi-line
		// value still points at the key the user must remove.
		return fmt.Errorf("line %d: %q is not a top-level key; version 1 declares a \"graph\" sequence of generation targets, each carrying its own schema, queries, and gen.go block", keyNode.Line, key)
	}
	return nil
}

// graphSequence answers stage 4 from the scan: graph present, non-null,
// a sequence, and non-empty, in that order. Kind and tag tests run on
// the resolved node so `graph: *g` naming a scalar is reported as a
// scalar; the line reported is the node's as written, so the alias's own
// rather than the anchor's.
func graphSequence(root *yaml.Node) (*yaml.Node, error) {
	value, _ := mappingValue(root, "graph")
	if value == nil {
		return nil, fmt.Errorf("missing required field %q", "graph")
	}
	resolved := resolveNode(value)
	if resolved.Tag == "!!null" {
		return nil, fmt.Errorf("missing required field %q", "graph")
	}
	if resolved.Kind != yaml.SequenceNode {
		return nil, fmt.Errorf("line %d: field %q must be a sequence of generation targets (got a YAML %s)", value.Line, "graph", kindName(resolved.Kind))
	}
	if len(resolved.Content) == 0 {
		return nil, fmt.Errorf("line %d: field %q must not be empty; declare at least one generation target", value.Line, "graph")
	}
	return resolved, nil
}

// checkNullEntries rejects a null sequence element (§4.4). yaml.v3 drops
// one rather than decoding it into the zero struct, so a document with
// one decodes to a shorter slice and every later entry's index in Config
// shifts away from the index the messages print. The sequence node's
// Content preserves every element, so the offender still has an index
// and a line here.
func checkNullEntries(graphSeq *yaml.Node) error {
	for i, element := range graphSeq.Content {
		if resolveNode(element).Tag == "!!null" {
			// The element's own line, not the anchor's: the anchor may be
			// a legitimate value used elsewhere, and the fault is the
			// element that names it here.
			return fmt.Errorf("graph[%d]: line %d: entry is null", i, element.Line)
		}
	}
	return nil
}

// checkEntryCount reports the §4 count invariant: the decoded target
// count must equal the number of elements the scan saw. A mismatch means
// yaml.v3 dropped an element §4.4 did not catch, which would renumber
// every later index. No input is known to violate it — enumerating
// yaml.v3's drop paths is an enumeration that is correct until the next
// one is found, and counting is a property that cannot go stale — so the
// test drives this directly.
func checkEntryCount(graphSeq *yaml.Node, decoded int) error {
	if len(graphSeq.Content) == decoded {
		return nil
	}
	return fmt.Errorf("internal: %q declares %d entries but %d decoded; the entry indices in any further message would be wrong",
		"graph", len(graphSeq.Content), decoded)
}

// decodeTarget runs one entry's post-decode checks — required keys in
// §2.2/§2.3 wire order, then the value checks in the same order — and
// normalises the wire struct into a Target. Errors are unprefixed; the
// caller adds the graph[i] prefix (§4.1).
func decodeTarget(w wireTarget) (Target, error) {
	// A nil pointer means the key was omitted (or explicitly null —
	// treated the same). Enum messages carry the vocabulary so a missing
	// axis is fixable without opening the spec.
	switch {
	case w.SchemaPath == nil:
		return Target{}, missingField("schema", "")
	case w.SchemaLang == nil:
		return Target{}, missingField("schema_language", joinValues(SchemaLangValues()))
	case w.QueryDir == nil:
		return Target{}, missingField("queries", "")
	case w.QueryLang == nil:
		return Target{}, missingField("query_language", joinValues(QueryLangValues()))
	case w.Gen == nil:
		return Target{}, missingField("gen", "")
	case w.Gen.Go == nil:
		return Target{}, missingField("gen.go", "")
	}
	g := w.Gen.Go
	switch {
	case g.Package == nil:
		return Target{}, missingField("gen.go.package", "")
	case g.Out == nil:
		return Target{}, missingField("gen.go.out", "")
	case g.Driver == nil:
		return Target{}, missingField("gen.go.driver", joinValues(DriverValues()))
	}

	for _, f := range []struct{ key, val string }{
		{key: "schema", val: *w.SchemaPath},
		{key: "queries", val: *w.QueryDir},
	} {
		if f.val == "" {
			return Target{}, fmt.Errorf("field %q must not be empty", f.key)
		}
	}
	// procsig is optional, but an explicit empty string is ambiguous
	// (a placeholder? a deliberate "none"?) — reject, don't guess.
	if w.ProcsigPath != nil && *w.ProcsigPath == "" {
		return Target{}, fmt.Errorf("field %q is empty; omit the key when no procsig file is used", "procsig")
	}
	for _, f := range []struct{ key, val string }{
		{key: "gen.go.package", val: *g.Package},
		{key: "gen.go.out", val: *g.Out},
	} {
		if f.val == "" {
			return Target{}, fmt.Errorf("field %q must not be empty", f.key)
		}
	}
	// token.IsIdentifier also rejects Go keywords, which are valid
	// identifiers lexically but unusable as package names.
	if !token.IsIdentifier(*g.Package) {
		return Target{}, fmt.Errorf("package %q is not a valid Go identifier", *g.Package)
	}

	t := Target{
		SchemaPath: *w.SchemaPath,
		SchemaLang: *w.SchemaLang,
		QueryDir:   *w.QueryDir,
		QueryLang:  *w.QueryLang,
		Go:         GoGen{Package: *g.Package, Out: *g.Out, Driver: *g.Driver},
	}
	if w.ProcsigPath != nil {
		t.ProcsigPath = *w.ProcsigPath
	}
	return t, nil
}

// missingField renders the §4.5 missing-key message; values is the enum
// vocabulary, empty for non-enum keys.
func missingField(key, values string) error {
	if values != "" {
		return fmt.Errorf("missing required field %q (valid values: %s)", key, values)
	}
	return fmt.Errorf("missing required field %q", key)
}

// CheckOutAgainst reports whether out may be added to c as a new
// generation target's output directory. It returns nil when out overlaps
// no existing target's, and otherwise an error naming the first target
// it overlaps and which way — the §4.5 cross-entry text, unprefixed, so
// the loader can prefix it and a huh Validate hook can render it bare.
//
// Overlapping output directories destroy each other's output: the later
// target's ADR 0012 wipe deletes what the earlier one just wrote. The
// comparison is lexical and filesystem-free (filepath.Rel cleans both
// operands and stats nothing), so config-file-format §4's rule survives
// and what escapes is exactly the pairs whose relation depends on a
// name the loader cannot see: an absolute path against a relative one,
// and an escaping path against one that re-enters through the working
// directory's own name ("../b/db" and "db" are one directory when the
// working directory is named b). Both are accepted (§4.3).
func (c Config) CheckOutAgainst(out string) error {
	for i, t := range c.Targets {
		switch compareOuts(t.Go.Out, out) {
		case outSame:
			return fmt.Errorf("out %q is already graph[%d]'s output directory", out, i)
		case outInside:
			return fmt.Errorf("out %q is inside graph[%d]'s output directory %q", out, i, t.Go.Out)
		case outContains:
			return fmt.Errorf("out %q contains graph[%d]'s output directory %q", out, i, t.Go.Out)
		case outDisjoint:
		}
	}
	return nil
}

// outRelation is how two output directories sit relative to each other.
type outRelation int

const (
	outDisjoint outRelation = iota
	outSame
	outInside   // later is inside earlier
	outContains // later contains earlier
)

// compareOuts runs the §4.3 rule in both directions, so containment is
// caught whichever operand is the parent.
func compareOuts(earlier, later string) outRelation {
	earlier, later = anchorRelative(earlier, later)
	if rel, err := filepath.Rel(earlier, later); err == nil {
		if rel == "." {
			return outSame
		}
		if !escapesBase(rel) {
			return outInside
		}
	}
	if rel, err := filepath.Rel(later, earlier); err == nil {
		if rel == "." {
			return outSame
		}
		if !escapesBase(rel) {
			return outContains
		}
	}
	return outDisjoint
}

// escapesBase reports whether a filepath.Rel result leaves its base
// directory. The test is on a path component, not a string prefix:
// filepath.Rel("internal/db", "internal/db/..foo") returns "..foo", a
// directory inside internal/db whose relative path begins with the two
// characters "..", and a string-prefix test would accept exactly the
// nested pair the overlap check exists to reject.
func escapesBase(rel string) bool {
	return rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// anchorSegment names a synthetic directory no configured path can
// collide with: YAML's printable character set excludes NUL, so a
// loaded `out` value cannot contain one.
const anchorSegment = "\x00gqlc-anchor-"

// anchorRelative rebases a pair of relative paths onto a shared
// synthetic absolute directory deep enough that neither's leading ".."
// components are cleaned away at the root.
//
// filepath.Rel refuses a base that escapes its own root — Rel("..",
// "a") errors — while the reverse direction returns an escaping
// "../..", so both arms of compareOuts fall through and plain
// containment reads as disjoint. Anchoring is pure string work: the
// base is fictional, nothing is resolved against the filesystem, and
// because neither operand can name an anchor segment, the relation
// between the joined paths is the relation between the originals under
// every real working directory. A mixed absolute/relative pair is left
// alone — relating those needs the working directory, which is the
// limit config-file-format §4 documents.
func anchorRelative(a, b string) (string, string) {
	if filepath.IsAbs(a) || filepath.IsAbs(b) {
		return a, b
	}
	depth := max(leadingParents(a), leadingParents(b))
	base := string(filepath.Separator)
	for i := range depth {
		base = filepath.Join(base, anchorSegment+strconv.Itoa(i))
	}
	return filepath.Join(base, a), filepath.Join(base, b)
}

// leadingParents counts the ".." components a cleaned relative path
// starts with — the depth above the working directory it reaches.
func leadingParents(p string) int {
	p = filepath.Clean(p)
	n := 0
	for p != ".." && strings.HasPrefix(p, ".."+string(filepath.Separator)) {
		n++
		p = p[3:]
	}
	if p == ".." {
		n++
	}
	return n
}

// Canonical returns the exact bytes Save writes: the §7 canonical
// form — version first, then the wire keys in canonical order,
// procsig omitted when empty, two-space indent, trailing newline.
// `gqlc init` previews these bytes before the confirm gate, so the
// preview/write identity is by construction, not parallel encoders.
func (c Config) Canonical() ([]byte, error) {
	version := fileVersion
	w := wireV1{Version: &version, Graph: make([]wireTarget, 0, len(c.Targets))}
	for _, t := range c.Targets {
		// Addresses of the loop variable's fields: Go 1.22+ gives each
		// iteration its own t, so these do not alias across entries.
		wt := wireTarget{
			SchemaPath: &t.SchemaPath,
			SchemaLang: &t.SchemaLang,
			QueryDir:   &t.QueryDir,
			QueryLang:  &t.QueryLang,
			Gen: &wireGen{Go: &wireGo{
				Package: &t.Go.Package,
				Out:     &t.Go.Out,
				Driver:  &t.Go.Driver,
			}},
		}
		if t.ProcsigPath != "" {
			wt.ProcsigPath = &t.ProcsigPath
		}
		w.Graph = append(w.Graph, wt)
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(w); err != nil {
		return nil, fmt.Errorf("config: marshal for save: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("config: marshal for save: %w", err)
	}
	return buf.Bytes(), nil
}

// Save writes c to path in canonical form (§7): Canonical's bytes,
// mode 0o644. Load(Save(c)) round-trips exactly;
// testdata/canonical.gqlc.yaml pins the bytes.
func (c Config) Save(path string) error {
	b, err := c.Canonical()
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		return fmt.Errorf("config: write %s: %w", path, err)
	}
	return nil
}
