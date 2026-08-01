package codegen

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// stubGenerator stands in for a backend; the registry never calls
// Generate, it only hands the constructor back.
type stubGenerator struct{}

func (stubGenerator) Generate(Input) ([]File, error) { return nil, nil }

// TestNewRegistryRejects sweeps every malformed entry list NewRegistry
// refuses. Each row is a list that must not produce a usable registry;
// the row count guards the sweep against a case silently disappearing.
func TestNewRegistryRejects(t *testing.T) {
	newStub := func(string) Generator { return stubGenerator{} }

	cases := []struct {
		name    string
		entries []Entry
		wantMsg string
	}{
		{
			name:    "empty key",
			entries: []Entry{{New: newStub}},
			wantMsg: "key must not be empty",
		},
		{
			name:    "nil constructor",
			entries: []Entry{{Key: "some-driver"}},
			wantMsg: `entry "some-driver" has no constructor`,
		},
		{
			name:    "duplicate key",
			entries: []Entry{{Key: "some-driver", New: newStub}, {Key: "some-driver", New: newStub}},
			wantMsg: `duplicate registry entry key "some-driver"`,
		},
	}
	require.Len(t, cases, 3)

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reg, err := NewRegistry(tc.entries...)
			require.ErrorContains(t, err, tc.wantMsg)
			require.Empty(t, reg.Keys(), "a rejected list must yield no registry")
		})
	}
}

// TestRegistryLookup pins the resolution contract: a registered key
// yields its own constructor, an unregistered one misses rather than
// falling back, and the zero Registry misses on everything.
func TestRegistryLookup(t *testing.T) {
	marker := &stubGenerator{}
	reg, err := NewRegistry(
		Entry{Key: "first", New: func(string) Generator { return marker }},
		Entry{Key: "second", New: func(string) Generator { return stubGenerator{} }},
	)
	require.NoError(t, err)
	require.Equal(t, []string{"first", "second"}, reg.Keys())

	newGen, ok := reg.Lookup("first")
	require.True(t, ok)
	require.Same(t, marker, newGen(""))

	_, ok = reg.Lookup("third")
	require.False(t, ok)

	_, ok = Registry{}.Lookup("first")
	require.False(t, ok)
}
