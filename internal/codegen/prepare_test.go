package codegen

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/areqag/gqlc/internal/resolver"
)

// TestPhaseBCommitsAccessMode asserts that phaseBDerive commits the
// access mode as the prepare-side closed enum (spec §1.1). One row per
// StatementKind value; every future StatementKind addition must extend
// the table.
func TestPhaseBCommitsAccessMode(t *testing.T) {
	tests := []struct {
		name      string
		statement resolver.StatementKind
		want      accessMode
	}{
		{"read maps to accessModeRead", resolver.StatementRead, accessModeRead},
		{"write maps to accessModeWrite", resolver.StatementWrite, accessModeWrite},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q := NamedQuery{
				Name:        "Q",
				Cardinality: CardinalityExec,
				SourceText:  "MATCH (n) DELETE n",
				Validated: resolver.ValidatedQuery{
					Statement: tt.statement,
				},
			}
			out, err := phaseBDerive([]NamedQuery{q}, nil, nil)
			require.NoError(t, err)
			require.Len(t, out, 1)
			require.Equal(t, tt.want, out[0].AccessMode)
		})
	}
}

// TestPhaseBCommitsIsWrite asserts that phaseBDerive commits the
// StatementWrite axis as a preparedQuery.IsWrite bool (spec §1.2). Real
// two-value semantic axis, boolean is the honest type.
func TestPhaseBCommitsIsWrite(t *testing.T) {
	tests := []struct {
		name      string
		statement resolver.StatementKind
		want      bool
	}{
		{"read is not write", resolver.StatementRead, false},
		{"write is write", resolver.StatementWrite, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q := NamedQuery{
				Name:        "Q",
				Cardinality: CardinalityExec,
				SourceText:  "MATCH (n) DELETE n",
				Validated: resolver.ValidatedQuery{
					Statement: tt.statement,
				},
			}
			out, err := phaseBDerive([]NamedQuery{q}, nil, nil)
			require.NoError(t, err)
			require.Len(t, out, 1)
			require.Equal(t, tt.want, out[0].IsWrite)
		})
	}
}
