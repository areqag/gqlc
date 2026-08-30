package codegen_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/areqag/gqlc/internal/codegen"
)

// stubGenerator stands in for a backend; the registry never calls
// Generate, it only hands the constructor back.
type stubGenerator struct{}

func (stubGenerator) Generate(codegen.Input) ([]codegen.File, error) { return nil, nil }

// errFirst and errSecond are two distinct sentinel values, so that a row
// asserting a name published twice can distinguish the identical-value
// case from the conflicting one. Two errors carrying the SAME text, so
// that a merge comparing messages rather than identity passes the
// conflict row it should fail.
var (
	errFirst  = errors.New("a published refusal")
	errSecond = errors.New("a published refusal")
)

// TestNewRegistryRejects sweeps every malformed entry list NewRegistry
// refuses. Each row is a list that must not produce a usable registry;
// the row count guards the sweep against a case silently disappearing.
func TestNewRegistryRejects(t *testing.T) {
	newStub := func(string) codegen.Generator { return stubGenerator{} }

	cases := []struct {
		name    string
		entries []codegen.Entry
		wantMsg string
	}{
		{
			name:    "empty key",
			entries: []codegen.Entry{{New: newStub}},
			wantMsg: "key must not be empty",
		},
		{
			name:    "nil constructor",
			entries: []codegen.Entry{{Key: "some-driver"}},
			wantMsg: `entry "some-driver" has no constructor`,
		},
		{
			name:    "duplicate key",
			entries: []codegen.Entry{{Key: "some-driver", New: newStub}, {Key: "some-driver", New: newStub}},
			wantMsg: `duplicate registry entry key "some-driver"`,
		},
		{
			name: "empty sentinel name",
			entries: []codegen.Entry{{
				Key: "some-driver", New: newStub,
				Sentinels: map[string]error{"": errFirst},
			}},
			wantMsg: `entry "some-driver" publishes a sentinel under an empty name`,
		},
		{
			name: "nil sentinel value",
			entries: []codegen.Entry{{
				Key: "some-driver", New: newStub,
				Sentinels: map[string]error{"pkg.ErrThing": nil},
			}},
			wantMsg: `entry "some-driver" publishes "pkg.ErrThing" with no error value`,
		},
		{
			name: "one name, two entries, different values",
			entries: []codegen.Entry{
				{Key: "first", New: newStub, Sentinels: map[string]error{"pkg.ErrThing": errFirst}},
				{Key: "second", New: newStub, Sentinels: map[string]error{"pkg.ErrThing": errSecond}},
			},
			wantMsg: `entries "first" and "second" publish different errors under "pkg.ErrThing"`,
		},
	}
	require.Len(t, cases, 6)

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reg, err := codegen.NewRegistry(tc.entries...)
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
	reg, err := codegen.NewRegistry(
		codegen.Entry{Key: "first", New: func(string) codegen.Generator { return marker }},
		codegen.Entry{Key: "second", New: func(string) codegen.Generator { return stubGenerator{} }},
	)
	require.NoError(t, err)
	require.Equal(t, []string{"first", "second"}, reg.Keys())

	newGen, ok := reg.Lookup("first")
	require.True(t, ok)
	require.Same(t, marker, newGen(""))

	_, ok = reg.Lookup("third")
	require.False(t, ok)

	_, ok = codegen.Registry{}.Lookup("first")
	require.False(t, ok)
}

// TestRegistrySentinels pins what publication does once NewRegistry has
// accepted it: the names merge across entries, an entry publishing
// nothing is not an error, and the accessor hands back a copy.
//
// The identical-value row is the dormant case — two registry keys can
// share one backend package, as the two neo4j entries do, so both would
// publish that package's names under the same spellings. Nothing in
// production exercises it today, which is exactly why it is pinned here:
// it is the case a reader would otherwise have to guess about from the
// conflict rule alone.
func TestRegistrySentinels(t *testing.T) {
	newStub := func(string) codegen.Generator { return stubGenerator{} }

	reg, err := codegen.NewRegistry(
		codegen.Entry{Key: "first", New: newStub, Sentinels: map[string]error{
			"pkg.ErrThing": errFirst,
			"pkg.ErrOther": errSecond,
		}},
		codegen.Entry{Key: "shares-a-package", New: newStub, Sentinels: map[string]error{
			"pkg.ErrThing": errFirst,
		}},
		codegen.Entry{Key: "publishes-nothing", New: newStub},
	)
	require.NoError(t, err)

	got := reg.Sentinels()
	require.Equal(t, map[string]error{"pkg.ErrThing": errFirst, "pkg.ErrOther": errSecond}, got)
	require.Same(t, errFirst, got["pkg.ErrThing"], "the published value must be the sentinel itself, not a copy of its text")

	got["pkg.ErrThing"] = errSecond
	delete(got, "pkg.ErrOther")
	require.Equal(t, map[string]error{"pkg.ErrThing": errFirst, "pkg.ErrOther": errSecond}, reg.Sentinels(),
		"a caller mutating the returned map must not reach the registry's own")

	require.Empty(t, codegen.Registry{}.Sentinels(), "the zero Registry publishes nothing")
}
