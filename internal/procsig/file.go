package procsig

import (
	"bytes"
	"cmp"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"slices"
)

// fileVersion is the on-disk schema version this loader accepts.
// Omitted in a file loads as v1; any other explicit value is rejected.
const fileVersion = 1

// wireFile mirrors docs/specs/procsig-file-format.md §3.1. Version is a
// pointer so an omitted key is distinguished from an explicit zero
// (the latter is rejected as an invalid version).
type wireFile struct {
	Version    *int            `json:"version,omitempty"`
	Signatures []wireSignature `json:"signatures"`
}

// wireSignature is the JSON shape of one Signature. The token is a
// string, not a TypeToken, so an unknown vocabulary member surfaces
// as a load error rather than a decode error.
type wireSignature struct {
	Name    string       `json:"name"`
	Params  []wireColumn `json:"params"`
	Results []wireColumn `json:"results"`
}

// wireColumn is one param or result on the wire. Nullable is always
// emitted (§3.3): omitted-means-false would make hand-edited files
// ambiguous about whether a missing key was a deliberate false.
type wireColumn struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Nullable bool   `json:"nullable"`
}

// Load reads the file at path, decodes it under the wire schema, and
// returns the equivalent Registry. Errors are wrapped with path.
func Load(path string) (Registry, error) {
	blob, err := os.ReadFile(path)
	if err != nil {
		return Registry{}, fmt.Errorf("procsig: open %s: %w", path, err)
	}
	return decode(bytes.NewReader(blob), path)
}

// Decode reads from r and returns the equivalent Registry, without
// touching the filesystem.
func Decode(r io.Reader) (Registry, error) {
	return decode(r, "<stream>")
}

// decode is the shared body of Load and Decode. src labels the origin
// (a file path or "<stream>") in error messages.
func decode(r io.Reader, src string) (Registry, error) {
	// A zero-byte input is a truncation, not the empty registry (§4.5):
	// json.Decoder would surface io.EOF here, which is less actionable
	// than a dedicated message.
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(r); err != nil {
		return Registry{}, fmt.Errorf("procsig: read %s: %w", src, err)
	}
	if buf.Len() == 0 {
		return Registry{}, fmt.Errorf("procsig: %s is empty (write {\"signatures\":[]} for the empty registry)", src)
	}
	body := buf.Bytes()

	// Typed decode catches unknown top-level keys, unknown signature
	// or column fields, and structural mismatches. Bare `null` and
	// `{"signatures":null}` are then caught explicitly below — the
	// typed decode accepts both as zero-value wireFile and would
	// silently produce the empty registry.
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	var wf wireFile
	if err := dec.Decode(&wf); err != nil {
		return Registry{}, fmt.Errorf("procsig: decode %s: %w", src, err)
	}
	if wf.Version != nil && *wf.Version != fileVersion {
		return Registry{}, fmt.Errorf("procsig: %s declares version %d; only version %d is supported", src, *wf.Version, fileVersion)
	}
	if err := requireSignaturesKey(body, src); err != nil {
		return Registry{}, err
	}

	sigs := make([]Signature, 0, len(wf.Signatures))
	for _, ws := range wf.Signatures {
		sig, err := ws.toSignature(src)
		if err != nil {
			return Registry{}, err
		}
		sigs = append(sigs, sig)
	}
	reg, err := NewRegistry(sigs)
	if err != nil {
		return Registry{}, fmt.Errorf("procsig: %s: %w", src, err)
	}
	return reg, nil
}

// requireSignaturesKey is the null-shape gate: the typed wireFile
// decode accepts `null` and `{"signatures":null}` as a zero-value
// wireFile, silently producing the empty registry. Re-decoding through
// a raw-object map catches both by requiring a top-level object with a
// non-null "signatures" key.
func requireSignaturesKey(body []byte, src string) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return fmt.Errorf("procsig: decode %s: %w", src, err)
	}
	if raw == nil {
		return fmt.Errorf("procsig: %s top-level must be a JSON object, got null", src)
	}
	sigsRaw, ok := raw["signatures"]
	if !ok {
		return fmt.Errorf("procsig: %s top-level object missing required \"signatures\" key", src)
	}
	if bytes.Equal(bytes.TrimSpace(sigsRaw), []byte("null")) {
		return fmt.Errorf("procsig: %s \"signatures\" is null; write [] for the empty registry", src)
	}
	return nil
}

// toSignature lifts a wire signature into a procsig.Signature.
// Error messages carry the enclosing signature name and column name so
// the reader can locate the offending token without line-numbering.
func (ws wireSignature) toSignature(src string) (Signature, error) {
	sig := Signature{Name: ws.Name}
	for _, c := range ws.Params {
		tok, err := parseWireToken(c.Type)
		if err != nil {
			return Signature{}, fmt.Errorf("procsig: %s: signature %q param %q: %w", src, ws.Name, c.Name, err)
		}
		sig.Params = append(sig.Params, Param{Name: c.Name, Token: tok, Nullable: c.Nullable})
	}
	for _, c := range ws.Results {
		tok, err := parseWireToken(c.Type)
		if err != nil {
			return Signature{}, fmt.Errorf("procsig: %s: signature %q result %q: %w", src, ws.Name, c.Name, err)
		}
		sig.Results = append(sig.Results, Result{Name: c.Name, Token: tok, Nullable: c.Nullable})
	}
	return sig, nil
}

// parseWireToken maps the uppercase wire vocabulary (the TCK
// declaration syntax) to a TypeToken.
func parseWireToken(s string) (TypeToken, error) {
	switch s {
	case "INTEGER":
		return TokenInteger, nil
	case "FLOAT":
		return TokenFloat, nil
	case "STRING":
		return TokenString, nil
	case "NUMBER":
		return TokenNumber, nil
	default:
		return 0, fmt.Errorf("unknown type token %q (want one of INTEGER, FLOAT, STRING, NUMBER)", s)
	}
}

// MarshalJSON renders a Registry in canonical form (§3.2 / §3.3):
// signatures sorted by Name so the output is stable under Go's
// randomised map iteration. Mirrors schema.Schema's discipline in
// internal/schema/schema.go.
func (r Registry) MarshalJSON() ([]byte, error) {
	sigs := make([]wireSignature, 0, len(r.byName))
	for _, s := range r.byName {
		sigs = append(sigs, toWire(s))
	}
	slices.SortFunc(sigs, func(a, b wireSignature) int { return cmp.Compare(a.Name, b.Name) })
	return json.Marshal(wireFile{Signatures: sigs})
}

// toWire mirrors toSignature in the other direction. The version field
// is never emitted (omission defaults to v1 in the loader).
func toWire(s Signature) wireSignature {
	ws := wireSignature{
		Name:    s.Name,
		Params:  make([]wireColumn, 0, len(s.Params)),
		Results: make([]wireColumn, 0, len(s.Results)),
	}
	for _, p := range s.Params {
		ws.Params = append(ws.Params, wireColumn{Name: p.Name, Type: p.Token.String(), Nullable: p.Nullable})
	}
	for _, res := range s.Results {
		ws.Results = append(ws.Results, wireColumn{Name: res.Name, Type: res.Token.String(), Nullable: res.Nullable})
	}
	return ws
}

// Save writes r's canonical JSON encoding to path with mode 0o644,
// two-space indented, with a trailing newline. Matches the fixture
// under testdata/canonical.procsig.json exactly.
func (r Registry) Save(path string) error {
	blob, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return fmt.Errorf("procsig: marshal for save: %w", err)
	}
	blob = append(blob, '\n')
	if err := os.WriteFile(path, blob, 0o644); err != nil {
		return fmt.Errorf("procsig: write %s: %w", path, err)
	}
	return nil
}
