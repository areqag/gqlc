package age_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
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

// TestARecordPropertyGetsASiteNamedAlias pins the ergonomics layer of
// spec §2.1: the property's own site names the anonymous struct, so a
// caller declaring a variable of that type writes PlaceAddr rather than
// retyping the fields.
//
// The `=` is asserted rather than assumed, and it is the whole of what
// separates this from a bug. A DEFINED type — `type PlaceAddr struct{…}`
// — is a different Go type from the anonymous spelling the Row and
// Params structs carry, so the record's own helpers would stop accepting
// the values the rest of the package holds, and two properties declaring
// one record would no longer share a type. The alias adds a name and
// must add nothing else.
//
// RECORD<ANY> gets no site alias and is asserted here rather than left
// to the reader: it carries map[string]any, which already has a short
// spelling and no digest carrier alias either, so naming it would be the
// one place a site alias stood for something RecordEncodings does not
// know about.
func TestARecordPropertyGetsASiteNamedAlias(t *testing.T) {
	rec := graph.RecordOf([]graph.RecordField{
		{Name: "zip", Type: graph.TypeInt32, NotNull: true},
		{Name: "note", Type: graph.TypeString},
	})
	goType, ok := age.TypeMap{}.Property(rec)
	require.True(t, ok, "this backend carries %s", rec)
	loose, ok := age.TypeMap{}.Property(graph.TypeAnyRecord)
	require.True(t, ok, "this backend carries %s", graph.TypeAnyRecord)

	e := age.WiredEntity{Entity: codegen.Entity{
		Name: "Place", Kind: codegen.EntityNode,
		Fields: []codegen.EntityField{
			{PropName: "addr", Field: "Addr", GoType: goType, Width: rec},
			{PropName: "extra", Field: "Extra", GoType: loose, Width: graph.TypeAnyRecord},
		},
	}}.WithLabels("Place", age.VertexAnnotation)

	var h age.Helpers
	h.ForEntities([]age.WiredEntity{e})
	src := string(age.RenderModels("models", []age.WiredEntity{e}, h))

	// assert and not require, so that each of the three reports on its own
	// run. They are three independent claims about one emitted text, and
	// under require the first failure aborts the other two — which is not
	// a reporting nicety here but a hole in the guard: a mutation screen
	// of the alias `=` kills this test through the Contains arm and then
	// cannot observe the NotContains arm at all, so that arm would be
	// carried as screened when nothing had ever exercised it.
	assert.Contains(t, src, "type PlaceAddr = "+goType+"\n",
		"the record property has no site-named alias, so a caller naming its type retypes the struct")
	assert.NotContains(t, src, "type PlaceAddr struct",
		"the site name is a DEFINED type, so it is not assignable to the anonymous spelling "+
			"the Row and Params structs carry and the record's own helpers refuse it")
	assert.NotContains(t, src, "PlaceExtra",
		"RECORD<ANY> was given a site alias, naming a carrier RecordEncodings does not enrol")
}
