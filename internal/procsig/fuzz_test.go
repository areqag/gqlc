package procsig_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/areqag/gqlc/internal/fuzzkit"
	"github.com/areqag/gqlc/internal/procsig"
)

// FuzzProcsigDecode is the procsig half of target T5 of
// docs/specs/nightly-fuzzing-plan.md. On every input it checks:
//
//   - an error comes with the zero Registry and names its source ("procsig:");
//   - agreement: on a success, the signatures the input declares — read here
//     by the key names docs/specs/procsig-file-format.md §3 fixes, exactly —
//     go through procsig.NewRegistry without error and give a Registry
//     reflect.DeepEqual to Decode's;
//   - every decoded signature's name is qualified and every column's token is
//     a member of the TypeToken sum;
//   - canonical round-trip: Decode of the Registry's own JSON is the same
//     Registry;
//   - determinism: two decodes of the same input are reflect.DeepEqual.
//
// The plan's row asks for "err class = sentinel". procsig-file-format.md §4.3
// says the loader "never uses errors.Is-style sentinels", so every refusal
// would classify plain; the provenance check stands in for the class.
func FuzzProcsigDecode(f *testing.F) {
	fuzzkit.SeedFiles(f, "testdata/canonical.procsig.json")
	f.Add(`{"version":1,"signatures":[]}`)
	f.Add(`{"signatures":null}`)
	f.Fuzz(func(t *testing.T, src string) {
		fuzzkit.Skip(t, []byte(src))
		var first, second procsig.Registry
		var err1, err2 error
		fuzzkit.Guard(t, 2*time.Second, func() {
			first, err1 = procsig.Decode(strings.NewReader(src))
			second, err2 = procsig.Decode(strings.NewReader(src))
		})
		if !reflect.DeepEqual(first, second) || fmt.Sprint(err1) != fmt.Sprint(err2) {
			t.Fatalf("property determinism: two decodes disagree: %v / %v", err1, err2)
		}
		if err1 != nil {
			if !reflect.DeepEqual(first, procsig.Registry{}) {
				t.Fatalf("property error-zero: Decode returned a non-zero Registry alongside %v", err1)
			}
			if !strings.HasPrefix(err1.Error(), "procsig: ") {
				t.Fatalf("property error-provenance: %q does not name its source", err1)
			}
			return
		}

		sigs, msg := declared(src)
		if msg != "" {
			t.Fatalf("property agreement: Decode accepted an input the format does not: %s", msg)
		}
		want, err := procsig.NewRegistry(sigs)
		if err != nil {
			t.Fatalf("property agreement: Decode accepted signatures NewRegistry refuses: %v", err)
		}
		if !reflect.DeepEqual(first, want) {
			t.Fatalf("property agreement: Decode and NewRegistry over the declared signatures differ:\nDecode:      %s\nNewRegistry: %s", canonical(first), canonical(want))
		}
		for _, s := range sigs {
			if msg := signatureViolation(s); msg != "" {
				t.Fatal(msg)
			}
		}

		canon, err := json.Marshal(first)
		if err != nil {
			t.Fatalf("property round-trip: MarshalJSON: %v", err)
		}
		back, err := procsig.Decode(bytes.NewReader(canon))
		if err != nil || !reflect.DeepEqual(first, back) {
			t.Fatalf("property round-trip: Decode(MarshalJSON(r)) is not r (%v):\n%s", err, canon)
		}
	})
}

func canonical(r procsig.Registry) string {
	b, err := json.Marshal(r)
	if err != nil {
		return "MarshalJSON: " + err.Error()
	}
	return string(b)
}

// The token vocabulary of procsig-file-format.md §3.4.
var wireTokens = map[string]procsig.TypeToken{
	"INTEGER": procsig.TokenInteger,
	"FLOAT":   procsig.TokenFloat,
	"STRING":  procsig.TokenString,
	"NUMBER":  procsig.TokenNumber,
}

// declared reads the signatures src declares by the format's own key names,
// compared exactly. encoding/json's struct decoding matches keys without
// regard to case, which is the loader's behaviour and so cannot also be the
// oracle for it. It returns a reason when src is not a file the format
// describes.
func declared(src string) ([]procsig.Signature, string) {
	top, msg := object([]byte(src), "signatures", "version")
	if msg != "" {
		return nil, "top level: " + msg
	}
	var raws []json.RawMessage
	if err := json.Unmarshal(top["signatures"], &raws); err != nil {
		return nil, fmt.Sprintf("signatures is not an array: %s", top["signatures"])
	} else if raws == nil {
		return nil, "signatures holds JSON null"
	}
	sigs := make([]procsig.Signature, 0, len(raws))
	for i, raw := range raws {
		obj, msg := object(raw, "name", "params", "results")
		if msg != "" {
			return nil, fmt.Sprintf("signatures[%d]: %s", i, msg)
		}
		var s procsig.Signature
		if err := json.Unmarshal(obj["name"], &s.Name); err != nil {
			return nil, fmt.Sprintf("signatures[%d].name: %v", i, err)
		}
		params, msg := columns(obj["params"])
		if msg != "" {
			return nil, fmt.Sprintf("signatures[%d].params: %s", i, msg)
		}
		results, msg := columns(obj["results"])
		if msg != "" {
			return nil, fmt.Sprintf("signatures[%d].results: %s", i, msg)
		}
		for _, c := range params {
			s.Params = append(s.Params, procsig.Param(c))
		}
		for _, c := range results {
			s.Results = append(s.Results, procsig.Result(c))
		}
		sigs = append(sigs, s)
	}
	return sigs, ""
}

type column struct {
	Name     string
	Token    procsig.TypeToken
	Nullable bool
}

func columns(raw json.RawMessage) ([]column, string) {
	var raws []json.RawMessage
	if err := json.Unmarshal(raw, &raws); err != nil {
		return nil, fmt.Sprintf("not an array: %s", raw)
	} else if raws == nil {
		return nil, "the column list is null"
	}
	var out []column
	for i, r := range raws {
		obj, msg := object(r, "name", "type", "nullable")
		if msg != "" {
			return nil, fmt.Sprintf("[%d]: %s", i, msg)
		}
		var c column
		var tok string
		if json.Unmarshal(obj["name"], &c.Name) != nil || json.Unmarshal(obj["type"], &tok) != nil || json.Unmarshal(obj["nullable"], &c.Nullable) != nil {
			return nil, fmt.Sprintf("[%d]: a field has the wrong JSON type: %s", i, r)
		}
		var ok bool
		if c.Token, ok = wireTokens[tok]; !ok {
			return nil, fmt.Sprintf("[%d]: type %q is not in the vocabulary", i, tok)
		}
		out = append(out, c)
	}
	return out, ""
}

// object decodes raw as a JSON object whose keys are exactly among allowed,
// each spelled as allowed spells it; every allowed key but "version" is
// required.
func object(raw []byte, allowed ...string) (map[string]json.RawMessage, string) {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil || m == nil {
		return nil, fmt.Sprintf("not a JSON object: %s", raw)
	}
	for k := range m {
		if slices.Contains(allowed, k) {
			continue
		}
		for _, a := range allowed {
			if strings.EqualFold(k, a) {
				return nil, fmt.Sprintf("key %q differs only in case from %q", k, a)
			}
		}
		return nil, fmt.Sprintf("key %q is not one the format defines (%s)", k, strings.Join(allowed, ", "))
	}
	for _, a := range allowed {
		if _, ok := m[a]; !ok && a != "version" {
			return nil, fmt.Sprintf("required key %q is absent", a)
		}
	}
	return m, ""
}

func signatureViolation(s procsig.Signature) string {
	if !strings.Contains(s.Name, ".") {
		return fmt.Sprintf("property name: signature %q is not qualified", s.Name)
	}
	for _, p := range s.Params {
		if p.Token < procsig.TokenInteger || p.Token > procsig.TokenNumber {
			return fmt.Sprintf("property token: %s param %q has token %d", s.Name, p.Name, p.Token)
		}
	}
	for _, r := range s.Results {
		if r.Token < procsig.TokenInteger || r.Token > procsig.TokenNumber {
			return fmt.Sprintf("property token: %s result %q has token %d", s.Name, r.Name, r.Token)
		}
	}
	return ""
}
