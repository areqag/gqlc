package gql

import (
	"errors"
	"strings"
	"testing"
)

func TestParseRejectsUnsupportedConstructs(t *testing.T) {
	tests := []struct {
		name    string
		src     string
		wantErr error // expected sentinel, or nil for no error
	}{
		{
			name: "directed schema is accepted",
			src:  `CREATE PROPERTY GRAPH TYPE T AS { (p :Person { id :: INT }), (q :Person { id :: INT }), (p) -[:KNOWS { since :: DATE }]-> (q) }`,
		},
		{
			name:    "label implication on a node is rejected",
			src:     `CREATE PROPERTY GRAPH TYPE T AS { ( :Person => { id :: INT } ) }`,
			wantErr: ErrLabelImplication,
		},
		{
			name:    "undirected edge is rejected",
			src:     `CREATE PROPERTY GRAPH TYPE T AS { (p :Person { id :: INT }), (q :Person { id :: INT }), (p) ~[:KNOWS]~ (q) }`,
			wantErr: ErrUndirectedEdge,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := New().Parse(strings.NewReader(tt.src))

			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("got %v, want %v", err, tt.wantErr)
			}
		})
	}
}
