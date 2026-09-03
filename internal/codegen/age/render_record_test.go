package age_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/areqag/gqlc/internal/codegen"
	"github.com/areqag/gqlc/internal/codegen/age"
	"github.com/areqag/gqlc/internal/graph"
)

// TestARecordPropertyRendersItsCarrierAndItsDecode is this backend's
// reproduction of the gap stage 1 leaves behind: typeMap.Property already
// admits a declared RECORD and answers it with an anonymous struct text
// (types.go, the KindRecord arm), so a schema carrying one is prepared
// and an entity field is typed from it — and nothing downstream can read
// that field back.
//
// The three assertions are the three halves of one claim, and each is a
// different failure if it is the one missing. Without the alias the
// field's type is spelled inline at every site that mentions it and no
// helper signature can name it. Without the decode helper the field has
// a type and no reader. Without the call in the entity decoder the helper
// is emitted and dead, which the emitted package's own lint fence then
// fails.
//
// Written against the rendered bytes rather than against decodeFunc,
// because decodeFunc's answer is a NAME and a name is not a helper: the
// defect this guards against is a name that no declaration answers, and
// only the file has both halves in it.
func TestARecordPropertyRendersItsCarrierAndItsDecode(t *testing.T) {
	rec := graph.RecordOf([]graph.RecordField{
		{Name: "zip", Type: graph.TypeInt32, NotNull: true},
		{Name: "note", Type: graph.TypeString},
	})
	goType, ok := age.TypeMap{}.Property(rec)
	require.True(t, ok, "this backend carries %s", rec)

	alias := codegen.RecordAliasName(rec)
	suffix := codegen.RecordHelperSuffix(rec)

	e := age.WiredEntity{Entity: codegen.Entity{
		Name: "E", Kind: codegen.EntityNode,
		Fields: []codegen.EntityField{
			{PropName: "home", Field: "Home", GoType: goType, Width: rec},
		},
	}}.WithLabels("E", age.VertexAnnotation)

	var h age.Helpers
	h.ForEntities([]age.WiredEntity{e})
	src := string(age.RenderModels("models", []age.WiredEntity{e}, h))

	require.Contains(t, src, "type "+alias+" = struct",
		"the record's carrier is not declared, so every site that names the field's type respells the struct")
	require.Contains(t, src, "func decode"+suffix+"(raw []byte) ("+alias+", error)",
		"the record's carrier has no reader, so a field typed from it can be emitted and never filled")
	require.Contains(t, src, "decode"+suffix+")",
		"the entity's record field does not read through the record's own decoder")
}
