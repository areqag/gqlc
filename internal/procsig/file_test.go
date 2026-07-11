package procsig_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/areqag/gqlc/internal/procsig"
)

// canonicalPath is the sole on-disk fixture; every round-trip and
// canonical-form test consumes it, so any drift in the emitted form (a
// re-ordered key, a signature emitted before its predecessor) fails the
// byte-equality assertion in TestJSONRoundTripCanonicalBytes.
const canonicalPath = "testdata/canonical.procsig.json"

// TestLoadCanonical asserts the fixture parses into a Registry whose
// Lookup surface matches the four signatures the file declares.
func TestLoadCanonical(t *testing.T) {
	reg, err := procsig.Load(canonicalPath)
	if err != nil {
		t.Fatalf("Load(%q): unexpected error %v", canonicalPath, err)
	}
	for _, name := range []string{
		"test.doNothing",
		"test.my.numberProc",
		"test.my.proc",
		"test.strict",
	} {
		if _, ok := reg.Lookup(name); !ok {
			t.Errorf("Lookup(%q) missed after loading canonical fixture", name)
		}
	}
	if _, ok := reg.Lookup("absent.proc"); ok {
		t.Errorf("Lookup on unregistered name hit — loader must not fabricate signatures")
	}
}

// TestLoadCanonicalShape drills into one signature's fields to catch
// silent losses: a loader that dropped `nullable`, mis-ordered params,
// or coerced INTEGER->STRING would pass TestLoadCanonical but fail here.
func TestLoadCanonicalShape(t *testing.T) {
	reg, err := procsig.Load(canonicalPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	sig, ok := reg.Lookup("test.my.proc")
	if !ok {
		t.Fatal("test.my.proc missing")
	}
	if got, want := len(sig.Params), 1; got != want {
		t.Fatalf("params count = %d; want %d", got, want)
	}
	if p := sig.Params[0]; p.Name != "in" || p.Token != procsig.TokenInteger || !p.Nullable {
		t.Errorf("param[0] = %+v; want {in INTEGER true}", p)
	}
	if got, want := len(sig.Results), 2; got != want {
		t.Fatalf("results count = %d; want %d", got, want)
	}
	if r := sig.Results[0]; r.Name != "a" || r.Token != procsig.TokenString || !r.Nullable {
		t.Errorf("result[0] = %+v; want {a STRING true}", r)
	}
	if r := sig.Results[1]; r.Name != "b" || r.Token != procsig.TokenString || !r.Nullable {
		t.Errorf("result[1] = %+v; want {b STRING true}", r)
	}

	strict, ok := reg.Lookup("test.strict")
	if !ok {
		t.Fatal("test.strict missing")
	}
	if strict.Params[0].Nullable {
		t.Errorf("test.strict param nullable = true; want false — nullable:false must round-trip explicitly")
	}
	if strict.Results[0].Nullable {
		t.Errorf("test.strict result nullable = true; want false — nullable:false must round-trip explicitly")
	}
}

// TestJSONRoundTripCanonicalBytes is the canonical-form byte-identity
// invariant (spec §5.2): a file that is already in canonical form must
// round-trip byte-identically through Decode -> MarshalJSON.
func TestJSONRoundTripCanonicalBytes(t *testing.T) {
	src, err := os.ReadFile(canonicalPath)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	reg, err := procsig.Decode(bytes.NewReader(src))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	got, err := json.MarshalIndent(reg, "", "  ")
	if err != nil {
		t.Fatalf("MarshalIndent: %v", err)
	}
	// Fixture ends in a newline; MarshalIndent does not. Normalise the
	// trailing newline so the byte compare focuses on structural drift.
	want := bytes.TrimRight(src, "\n")
	if !bytes.Equal(got, want) {
		t.Fatalf("canonical form drift:\n--- want ---\n%s\n--- got ---\n%s", want, got)
	}
}

// TestSemanticRoundTrip is the in-memory lossless invariant (spec §5.1).
// The from-Go path is the codegen ingress: a Go-constructed Registry
// must serialise + deserialise into a Registry with identical Lookup
// behaviour, else the file format silently loses information.
func TestSemanticRoundTrip(t *testing.T) {
	src := []procsig.Signature{
		{Name: "test.a", Params: []procsig.Param{{Name: "n", Token: procsig.TokenInteger, Nullable: true}}, Results: []procsig.Result{{Name: "s", Token: procsig.TokenString, Nullable: true}}},
		{Name: "test.b", Params: nil, Results: []procsig.Result{{Name: "v", Token: procsig.TokenFloat, Nullable: false}}},
		{Name: "test.c", Params: []procsig.Param{{Name: "x", Token: procsig.TokenNumber, Nullable: true}}, Results: nil},
	}
	orig, err := procsig.NewRegistry(src)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	blob, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	got, err := procsig.Decode(bytes.NewReader(blob))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	for _, s := range src {
		g, ok := got.Lookup(s.Name)
		if !ok {
			t.Errorf("Lookup(%q) missed after round trip", s.Name)
			continue
		}
		if g.Name != s.Name {
			t.Errorf("name mismatch: got %q want %q", g.Name, s.Name)
		}
		if len(g.Params) != len(s.Params) {
			t.Errorf("%s: params count = %d; want %d", s.Name, len(g.Params), len(s.Params))
			continue
		}
		for i, p := range s.Params {
			if g.Params[i] != p {
				t.Errorf("%s: param[%d] = %+v; want %+v", s.Name, i, g.Params[i], p)
			}
		}
		if len(g.Results) != len(s.Results) {
			t.Errorf("%s: results count = %d; want %d", s.Name, len(g.Results), len(s.Results))
			continue
		}
		for i, r := range s.Results {
			if g.Results[i] != r {
				t.Errorf("%s: result[%d] = %+v; want %+v", s.Name, i, g.Results[i], r)
			}
		}
	}
}

// TestMarshalDeterministicOrder locks the canonical order (signatures
// sorted by Name). A Registry built from an un-sorted slice must emit
// JSON in sorted order — the same round-trip stability discipline
// schema.Schema follows.
func TestMarshalDeterministicOrder(t *testing.T) {
	unsorted := []procsig.Signature{
		{Name: "test.z", Params: nil, Results: []procsig.Result{{Name: "r", Token: procsig.TokenInteger}}},
		{Name: "test.a", Params: nil, Results: []procsig.Result{{Name: "r", Token: procsig.TokenInteger}}},
		{Name: "test.m", Params: nil, Results: []procsig.Result{{Name: "r", Token: procsig.TokenInteger}}},
	}
	reg, err := procsig.NewRegistry(unsorted)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	blob, err := json.Marshal(reg)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	// Cheap indexed check: "test.a" must appear before "test.m" which
	// must appear before "test.z". Robust to any indenting or key order
	// inside the signature object; only the outer signature order matters.
	sa := bytes.Index(blob, []byte(`"test.a"`))
	sm := bytes.Index(blob, []byte(`"test.m"`))
	sz := bytes.Index(blob, []byte(`"test.z"`))
	if sa < 0 || sa >= sm || sm >= sz {
		t.Fatalf("signatures not sorted by name: %s", string(blob))
	}
}

// TestDecodeRejects walks the file format's rejection surface. Each
// case names a class of malformed input; the loader must return a
// wrapping error whose text mentions the offending token so the user
// can locate and fix the file.
func TestDecodeRejects(t *testing.T) {
	cases := []struct {
		name    string
		body    string
		wantSub string
	}{
		{
			name:    "empty file",
			body:    "",
			wantSub: "empty",
		},
		{
			name:    "malformed JSON",
			body:    `{"signatures": [`,
			wantSub: "decode",
		},
		{
			name:    "unknown top-level key",
			body:    `{"signaturs": []}`,
			wantSub: "unknown",
		},
		{
			name:    "unknown signature key",
			body:    `{"signatures": [{"name": "test.p", "params": [], "results": [], "extra": 1}]}`,
			wantSub: "unknown",
		},
		{
			name:    "unknown column key",
			body:    `{"signatures": [{"name": "test.p", "params": [{"name":"in","type":"INTEGER","nullable":true,"extra":1}], "results": []}]}`,
			wantSub: "unknown",
		},
		{
			name:    "unknown type token",
			body:    `{"signatures": [{"name": "test.p", "params": [{"name":"in","type":"BOOLEAN","nullable":true}], "results": []}]}`,
			wantSub: "BOOLEAN",
		},
		{
			name:    "unqualified name delegated to NewRegistry",
			body:    `{"signatures": [{"name": "doNothing", "params": [], "results": []}]}`,
			wantSub: "fully-qualified",
		},
		{
			name:    "duplicate name delegated to NewRegistry",
			body:    `{"signatures": [{"name": "test.dup", "params": [], "results": []}, {"name": "test.dup", "params": [], "results": []}]}`,
			wantSub: "duplicate signature name",
		},
		{
			name:    "duplicate column name delegated to NewRegistry",
			body:    `{"signatures": [{"name": "test.p", "params": [{"name":"in","type":"INTEGER","nullable":true},{"name":"in","type":"STRING","nullable":true}], "results": []}]}`,
			wantSub: "duplicate param name",
		},
		{
			name:    "unknown version",
			body:    `{"version": 99, "signatures": []}`,
			wantSub: "version",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := procsig.Decode(strings.NewReader(tc.body))
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.wantSub)
			}
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Fatalf("error %q does not contain %q", err.Error(), tc.wantSub)
			}
		})
	}
}

// TestLoadFileErrors covers the file-path front door: a non-existent
// path must surface an open error naming the path, not fall through
// silently. Load and Decode share the same wire path, so the non-open
// cases are covered by TestDecodeRejects.
func TestLoadFileErrors(t *testing.T) {
	_, err := procsig.Load("testdata/does-not-exist.json")
	if err == nil {
		t.Fatal("expected error opening missing file")
	}
	if !strings.Contains(err.Error(), "testdata/does-not-exist.json") {
		t.Fatalf("error %q does not mention the offending path", err.Error())
	}
}

// TestVersionOnePermitted asserts the version:1 forward-compat hook
// works: a file that pins the version explicitly loads identically to
// one that omits it.
func TestVersionOnePermitted(t *testing.T) {
	body := `{"version": 1, "signatures": [{"name":"test.p","params":[],"results":[{"name":"out","type":"STRING","nullable":true}]}]}`
	reg, err := procsig.Decode(strings.NewReader(body))
	if err != nil {
		t.Fatalf("Decode with version:1: %v", err)
	}
	if _, ok := reg.Lookup("test.p"); !ok {
		t.Fatal("version:1 fixture did not populate the registry")
	}
}

// TestEmptySignaturesArrayValid asserts the intentional zero (§4.5):
// {"signatures": []} yields the empty registry without error.
func TestEmptySignaturesArrayValid(t *testing.T) {
	reg, err := procsig.Decode(strings.NewReader(`{"signatures": []}`))
	if err != nil {
		t.Fatalf("empty signatures array: %v", err)
	}
	if _, ok := reg.Lookup("anything"); ok {
		t.Fatal("empty-signatures registry must miss on every lookup")
	}
	// And the same-shaped file loads via Load too.
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.procsig.json")
	if err := os.WriteFile(path, []byte(`{"signatures": []}`), 0o644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	reg2, err := procsig.Load(path)
	if err != nil {
		t.Fatalf("Load empty: %v", err)
	}
	if _, ok := reg2.Lookup("anything"); ok {
		t.Fatal("empty-signatures registry (from file) must miss on every lookup")
	}
}

// TestSaveRoundTrip covers the Save convenience wrapper. The output
// must be canonical (byte-identical to a fresh MarshalIndent), so a
// Save + Load cycle is idempotent.
func TestSaveRoundTrip(t *testing.T) {
	src := []procsig.Signature{
		{Name: "test.b", Results: []procsig.Result{{Name: "out", Token: procsig.TokenInteger, Nullable: true}}},
		{Name: "test.a", Results: []procsig.Result{{Name: "out", Token: procsig.TokenInteger, Nullable: true}}},
	}
	reg, err := procsig.NewRegistry(src)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "out.procsig.json")
	if err := reg.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := procsig.Load(path)
	if err != nil {
		t.Fatalf("Load(saved): %v", err)
	}
	for _, s := range src {
		if _, ok := got.Lookup(s.Name); !ok {
			t.Errorf("Lookup(%q) missed after Save+Load", s.Name)
		}
	}
	// Bytes on disk must be canonical form (sorted signatures).
	blob, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read saved file: %v", err)
	}
	sa := bytes.Index(blob, []byte(`"test.a"`))
	sb := bytes.Index(blob, []byte(`"test.b"`))
	if sa < 0 || sa >= sb {
		t.Fatalf("Save did not emit canonical (sorted) form:\n%s", blob)
	}
}
