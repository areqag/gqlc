// Package config loads and saves the gqlc config file: the hand-written
// YAML manifest that declares a project's generation pipeline (schema
// path, query directory, output target, and the three tool axes). See
// docs/specs/config-file-format.md. The package is CLI-agnostic: it
// returns raw values and never touches the filesystem beyond the config
// file itself.
package config

import (
	"bytes"
	"fmt"
	"go/token"
	"io"
	"os"
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

// SchemaLangGQLC is gqlc's own graph-type schema language.
const SchemaLangGQLC SchemaLang = "gqlc"

// SchemaLangValues lists every valid SchemaLang. Loader errors and
// future `gqlc init` prompts both derive their choices from this slice.
func SchemaLangValues() []SchemaLang { return []SchemaLang{SchemaLangGQLC} }

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
// and never resolves or checks paths (§4).
type Config struct {
	// SchemaPath locates the schema file (wire key "schema").
	SchemaPath string
	// QueryDir locates the directory holding query files (wire key
	// "queries").
	QueryDir string
	// OutputDir locates the directory generated code is written to
	// (wire key "output").
	OutputDir string
	// OutputPackage names the generated Go package (wire key
	// "package"); it must be a valid Go identifier.
	OutputPackage string
	// ProcsigPath locates the optional procedure-signature registry
	// file (wire key "procsig"). Empty means the key was omitted.
	ProcsigPath string
	// SchemaLang is the language the schema file is written in (wire
	// key "schema_language").
	SchemaLang SchemaLang
	// QueryLang is the language the query files are written in (wire
	// key "query_language").
	QueryLang QueryLang
	// Driver is the client library the generated code targets (wire
	// key "driver").
	Driver Driver
}

// wireV1 mirrors docs/specs/config-file-format.md §3. Every field is a
// pointer so an omitted key is distinguishable from an explicit empty
// value; field order is the canonical Save order (§7).
type wireV1 struct {
	Version       *int        `yaml:"version"`
	SchemaPath    *string     `yaml:"schema"`
	QueryDir      *string     `yaml:"queries"`
	OutputDir     *string     `yaml:"output"`
	OutputPackage *string     `yaml:"package"`
	SchemaLang    *SchemaLang `yaml:"schema_language"`
	QueryLang     *QueryLang  `yaml:"query_language"`
	Driver        *Driver     `yaml:"driver"`
	ProcsigPath   *string     `yaml:"procsig,omitempty"`
}

// strictInt decodes only a true YAML integer scalar (tag !!int).
// Without it the version probe would inherit yaml.v3's numeric
// coercion — `version: 1.5` truncating to 1, `version: 0.9` to 0 — at
// the one field that guards format evolution.
type strictInt int

// UnmarshalYAML enforces the !!int tag before decoding the value.
func (i *strictInt) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind != yaml.ScalarNode {
		return fmt.Errorf("field \"version\" must be a YAML integer (got a YAML %s)", kindName(value.Kind))
	}
	if value.Tag != "!!int" {
		return fmt.Errorf("field \"version\" must be a YAML integer (got %s %q)", value.Tag, value.Value)
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

// decodeV1 decodes the version 1 wire shape: a strict decode (unknown
// keys reject, so typos surface instead of silently dropping), then
// required-field and value checks in wire-key order.
func decodeV1(body []byte, src string) (Config, error) {
	dec := yaml.NewDecoder(bytes.NewReader(body))
	dec.KnownFields(true)
	var w wireV1
	if err := dec.Decode(&w); err != nil {
		return Config{}, fmt.Errorf("config: %s: %w", src, err)
	}

	// A nil pointer means the key was omitted (or explicitly null —
	// treated the same). Enum messages carry the vocabulary so a
	// missing axis is fixable without opening the spec.
	required := []struct {
		key    string
		absent bool
		values string // non-empty only for enum keys
	}{
		{key: "schema", absent: w.SchemaPath == nil},
		{key: "queries", absent: w.QueryDir == nil},
		{key: "output", absent: w.OutputDir == nil},
		{key: "package", absent: w.OutputPackage == nil},
		{key: "schema_language", absent: w.SchemaLang == nil, values: joinValues(SchemaLangValues())},
		{key: "query_language", absent: w.QueryLang == nil, values: joinValues(QueryLangValues())},
		{key: "driver", absent: w.Driver == nil, values: joinValues(DriverValues())},
	}
	for _, f := range required {
		if !f.absent {
			continue
		}
		if f.values != "" {
			return Config{}, fmt.Errorf("config: %s: missing required field %q (valid values: %s)", src, f.key, f.values)
		}
		return Config{}, fmt.Errorf("config: %s: missing required field %q", src, f.key)
	}

	for _, f := range []struct {
		key string
		val string
	}{
		{key: "schema", val: *w.SchemaPath},
		{key: "queries", val: *w.QueryDir},
		{key: "output", val: *w.OutputDir},
		{key: "package", val: *w.OutputPackage},
	} {
		if f.val == "" {
			return Config{}, fmt.Errorf("config: %s: field %q must not be empty", src, f.key)
		}
	}
	// procsig is optional, but an explicit empty string is ambiguous
	// (a placeholder? a deliberate "none"?) — reject, don't guess.
	if w.ProcsigPath != nil && *w.ProcsigPath == "" {
		return Config{}, fmt.Errorf("config: %s: field \"procsig\" is empty; omit the key when no procsig file is used", src)
	}
	// token.IsIdentifier also rejects Go keywords, which are valid
	// identifiers lexically but unusable as package names.
	if !token.IsIdentifier(*w.OutputPackage) {
		return Config{}, fmt.Errorf("config: %s: package %q is not a valid Go identifier", src, *w.OutputPackage)
	}

	cfg := Config{
		SchemaPath:    *w.SchemaPath,
		QueryDir:      *w.QueryDir,
		OutputDir:     *w.OutputDir,
		OutputPackage: *w.OutputPackage,
		SchemaLang:    *w.SchemaLang,
		QueryLang:     *w.QueryLang,
		Driver:        *w.Driver,
	}
	if w.ProcsigPath != nil {
		cfg.ProcsigPath = *w.ProcsigPath
	}
	return cfg, nil
}

// Save writes c to path in canonical form (§7): version first, then the
// wire keys in canonical order, procsig omitted when empty, two-space
// indent, trailing newline, mode 0o644. Load(Save(c)) round-trips
// exactly; testdata/canonical.gqlc.yaml pins the bytes. Save exists to
// serve a future interactive `gqlc init`.
func (c Config) Save(path string) error {
	version := fileVersion
	w := wireV1{
		Version:       &version,
		SchemaPath:    &c.SchemaPath,
		QueryDir:      &c.QueryDir,
		OutputDir:     &c.OutputDir,
		OutputPackage: &c.OutputPackage,
		SchemaLang:    &c.SchemaLang,
		QueryLang:     &c.QueryLang,
		Driver:        &c.Driver,
	}
	if c.ProcsigPath != "" {
		w.ProcsigPath = &c.ProcsigPath
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(w); err != nil {
		return fmt.Errorf("config: marshal for save: %w", err)
	}
	if err := enc.Close(); err != nil {
		return fmt.Errorf("config: marshal for save: %w", err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		return fmt.Errorf("config: write %s: %w", path, err)
	}
	return nil
}
