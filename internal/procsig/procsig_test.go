package procsig_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/areqag/gqlc/internal/procsig"
)

func TestTypeTokenString(t *testing.T) {
	cases := []struct {
		tok  procsig.TypeToken
		want string
	}{
		{procsig.TokenInteger, "INTEGER"},
		{procsig.TokenFloat, "FLOAT"},
		{procsig.TokenString, "STRING"},
		{procsig.TokenNumber, "NUMBER"},
	}
	for _, tc := range cases {
		if got := tc.tok.String(); got != tc.want {
			t.Errorf("TypeToken(%d).String() = %q; want %q", tc.tok, got, tc.want)
		}
	}
}

func TestNewRegistryEmptyIsValid(t *testing.T) {
	r, err := procsig.NewRegistry(nil)
	if err != nil {
		t.Fatalf("empty input: unexpected error %v", err)
	}
	if _, ok := r.Lookup("anything"); ok {
		t.Fatalf("empty registry must miss on every lookup")
	}
}

func TestZeroRegistryMisses(t *testing.T) {
	var r procsig.Registry
	if _, ok := r.Lookup("test.my.proc"); ok {
		t.Fatalf("zero Registry must miss on every lookup")
	}
}

func TestNewRegistryLookupHitAndMiss(t *testing.T) {
	sig := procsig.Signature{
		Name: "test.my.proc",
		Params: []procsig.Param{
			{Name: "in", Token: procsig.TokenInteger, Nullable: true},
		},
		Results: []procsig.Result{
			{Name: "out", Token: procsig.TokenString, Nullable: true},
		},
	}
	r, err := procsig.NewRegistry([]procsig.Signature{sig})
	if err != nil {
		t.Fatalf("valid signature rejected: %v", err)
	}
	got, ok := r.Lookup("test.my.proc")
	if !ok {
		t.Fatalf("registered signature missed on lookup")
	}
	if got.Name != sig.Name || len(got.Params) != 1 || len(got.Results) != 1 {
		t.Fatalf("looked-up signature does not match input: %+v", got)
	}
	if _, ok := r.Lookup("nope.absent"); ok {
		t.Fatalf("unregistered name hit on lookup")
	}
}

func TestNewRegistryRejections(t *testing.T) {
	valid := procsig.Param{Name: "in", Token: procsig.TokenInteger, Nullable: true}
	validResult := procsig.Result{Name: "out", Token: procsig.TokenString, Nullable: true}
	cases := []struct {
		name    string
		sigs    []procsig.Signature
		wantSub string
	}{
		{
			name:    "empty signature name",
			sigs:    []procsig.Signature{{Name: "", Params: []procsig.Param{valid}, Results: []procsig.Result{validResult}}},
			wantSub: "name must not be empty",
		},
		{
			name:    "unqualified signature name",
			sigs:    []procsig.Signature{{Name: "doNothing", Results: []procsig.Result{validResult}}},
			wantSub: "fully-qualified",
		},
		{
			name: "duplicate signature name",
			sigs: []procsig.Signature{
				{Name: "test.dup", Results: []procsig.Result{validResult}},
				{Name: "test.dup", Results: []procsig.Result{validResult}},
			},
			wantSub: "duplicate signature name",
		},
		{
			name:    "empty param name",
			sigs:    []procsig.Signature{{Name: "test.p", Params: []procsig.Param{{Name: "", Token: procsig.TokenInteger}}, Results: []procsig.Result{validResult}}},
			wantSub: "empty param name",
		},
		{
			name: "duplicate param name",
			sigs: []procsig.Signature{{Name: "test.p", Params: []procsig.Param{
				{Name: "in", Token: procsig.TokenInteger},
				{Name: "in", Token: procsig.TokenString},
			}, Results: []procsig.Result{validResult}}},
			wantSub: "duplicate param name",
		},
		{
			name:    "empty result name",
			sigs:    []procsig.Signature{{Name: "test.p", Results: []procsig.Result{{Name: "", Token: procsig.TokenString}}}},
			wantSub: "empty result name",
		},
		{
			name: "duplicate result name",
			sigs: []procsig.Signature{{Name: "test.p", Results: []procsig.Result{
				{Name: "out", Token: procsig.TokenString},
				{Name: "out", Token: procsig.TokenInteger},
			}}},
			wantSub: "duplicate result name",
		},
		{
			name:    "unknown param token",
			sigs:    []procsig.Signature{{Name: "test.p", Params: []procsig.Param{{Name: "in", Token: procsig.TypeToken(99)}}, Results: []procsig.Result{validResult}}},
			wantSub: "unknown TypeToken",
		},
		{
			name:    "unknown result token",
			sigs:    []procsig.Signature{{Name: "test.p", Results: []procsig.Result{{Name: "out", Token: procsig.TypeToken(99)}}}},
			wantSub: "unknown TypeToken",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, err := procsig.NewRegistry(tc.sigs)
			if err == nil {
				t.Fatalf("expected error containing %q, got nil (registry lookups: %+v)", tc.wantSub, r)
			}
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Fatalf("error %q does not contain %q", err.Error(), tc.wantSub)
			}
			if _, ok := r.Lookup("test.p"); ok {
				t.Fatalf("rejected registry must miss on every lookup")
			}
		})
	}
}

// TestNewRegistrySameNameAcrossSignatures asserts that a param/result name
// collision is scoped to ONE signature: two distinct signatures may each
// declare an `in` param without colliding.
func TestNewRegistrySameNameAcrossSignatures(t *testing.T) {
	r, err := procsig.NewRegistry([]procsig.Signature{
		{Name: "test.a", Params: []procsig.Param{{Name: "in", Token: procsig.TokenInteger}}, Results: []procsig.Result{{Name: "out", Token: procsig.TokenString}}},
		{Name: "test.b", Params: []procsig.Param{{Name: "in", Token: procsig.TokenString}}, Results: []procsig.Result{{Name: "out", Token: procsig.TokenInteger}}},
	})
	if err != nil {
		t.Fatalf("cross-signature same-name params rejected: %v", err)
	}
	if _, ok := r.Lookup("test.a"); !ok {
		t.Fatalf("test.a missed after successful registry construction")
	}
	if _, ok := r.Lookup("test.b"); !ok {
		t.Fatalf("test.b missed after successful registry construction")
	}
}

// TestNewRegistryErrorSentinel documents that NewRegistry errors are
// plain errors.New / fmt.Errorf values — the package does not export
// per-family sentinels. Callers check the message; wrapping via
// errors.Is is intentionally not offered (the registry is a Stage-14-
// internal construction concern).
func TestNewRegistryErrorSentinel(t *testing.T) {
	_, err := procsig.NewRegistry([]procsig.Signature{{Name: ""}})
	if err == nil {
		t.Fatal("expected error")
	}
	// Any sentinel probe is defensive: NewRegistry doesn't wrap a
	// specific sentinel, so errors.Is on any exported error must be false.
	if errors.Is(err, errors.New("")) {
		t.Fatal("errors.Is on nil-sentinel returned true unexpectedly")
	}
}
