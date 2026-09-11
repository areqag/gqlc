// This file is an external test package for the same reason sealedsum_test.go
// is, and it reuses that file's `inhabitants` map on purpose: the map's key set
// is already checked against the package's own isResolvedType declarations by
// TestResolvedTypeSumIsNotClosed/declared_variants, so a ninth variant cannot
// land without extending it. The census below therefore inherits that
// completeness instead of restating a list that could silently go short — which
// would be this bead's own defect, one level up.
package resolver_test

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/areqag/gqlc/internal/resolver"
	"github.com/areqag/gqlc/internal/schema"
)

// resolvedTypeInterface is the one interface nonZero knows how to inhabit.
var resolvedTypeInterface = reflect.TypeOf((*resolver.ResolvedType)(nil)).Elem()

// declaredField is one exported field of a variant: the name it marshals
// under, and the type whose zero value the census compares the emitted value
// against.
type declaredField struct {
	tag string
	typ reflect.Type
}

// declaredJSONFields returns every exported field of typ, in declaration
// order. A field tagged `json:"-"` is deliberately not emitted and is
// excluded; an untagged field marshals under its Go name, which is the name
// the census then demands.
func declaredJSONFields(typ reflect.Type) []declaredField {
	var fields []declaredField
	for i := range typ.NumField() {
		f := typ.Field(i)
		if !f.IsExported() {
			continue
		}
		name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		if name == "-" {
			continue
		}
		if name == "" {
			name = f.Name
		}
		fields = append(fields, declaredField{tag: name, typ: f.Type})
	}
	return fields
}

// zeroWire is the JSON encoding of typ's zero value — what a field of that
// type looks like on the wire when the MarshalJSON value literal never
// assigned it.
//
// It is derived from the type rather than written down as null / "" / false,
// and that is the whole reason this is a function. For two of the field types
// in play the zero value is NOT any of those: Scalar's zero marshals to
// "bool" and Temporal's to "date", because both String() methods answer for
// their first member and both zero values ARE that member. A guard phrased as
// "the emitted value must not be empty" therefore passes a ResolvedScalar
// whose Kind was dropped from the value literal — the dropped field still
// renders as a non-empty string. Comparing against this instead catches it.
//
// Byte comparison against the census's own output is sound because
// encoding/json compacts what a Marshaler returns, so both sides are compact
// with the same escaping.
func zeroWire(t *testing.T, typ reflect.Type) string {
	t.Helper()
	raw, err := json.Marshal(reflect.Zero(typ).Interface())
	require.NoErrorf(t, err, "marshalling the zero value of %s", typ)
	return string(raw)
}

// nonZero builds a value of typ whose every leaf differs from the zero value.
//
// The non-zero part is load-bearing rather than tidy. ADR 0008's omit-when-false
// convention puts `,omitempty` on new additive axes, so a field marshalled from
// a zero value can be legitimately absent from the output. Censusing a zero
// value would read that absence as a missing field and fail a correct type.
//
// A kind it cannot fill fails the test instead of returning the zero value. A
// silent zero would be indistinguishable from a filled one at the call site and
// would reintroduce, for exactly the newest field types, the blind spot this
// census exists to close.
func nonZero(t *testing.T, typ reflect.Type) reflect.Value {
	t.Helper()

	if typ == resolvedTypeInterface {
		return reflect.ValueOf(resolver.ResolvedUnknown{})
	}

	v := reflect.New(typ).Elem()
	switch typ.Kind() {
	case reflect.Bool:
		v.SetBool(true)
	case reflect.String:
		v.SetString("census")
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		v.SetInt(1)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		v.SetUint(1)
	case reflect.Float32, reflect.Float64:
		v.SetFloat(1)
	case reflect.Slice:
		s := reflect.MakeSlice(typ, 1, 1)
		s.Index(0).Set(nonZero(t, typ.Elem()))
		v.Set(s)
	case reflect.Map:
		m := reflect.MakeMap(typ)
		m.SetMapIndex(nonZero(t, typ.Key()), nonZero(t, typ.Elem()))
		v.Set(m)
	case reflect.Pointer:
		p := reflect.New(typ.Elem())
		p.Elem().Set(nonZero(t, typ.Elem()))
		v.Set(p)
	case reflect.Struct:
		for i := range typ.NumField() {
			if typ.Field(i).IsExported() {
				v.Field(i).Set(nonZero(t, typ.Field(i).Type))
			}
		}
	default:
		require.FailNowf(t, "unfillable field type",
			"nonZero cannot build a non-zero %s (kind %s). Teach it that kind, or the census "+
				"silently stops proving anything about fields of this type.", typ, typ.Kind())
	}
	return v
}

// TestMarshalJSONEmitsEveryDeclaredField pins the invariant the corpus sweep's
// accept column rests on (gqlc-gyp5).
//
// TestCorpusSweepManifest records an accepting cell as a digest of
// json.Marshal(ValidatedQuery), and every ResolvedType writes its own
// MarshalJSON emitting a hand-written anonymous struct that restates the
// variant's fields. The struct definition and that anonymous struct are two
// separate lists of the same thing, so they can drift: a field added to the
// variant but not to its MarshalJSON is absent from the digest, and the sweep
// goes on reporting cells unchanged across a change that did move the model.
// The same bound applies to the *.validated.golden.json fixtures, which marshal
// through these same methods.
//
// The census demands the field's NAME and its VALUE, which are two separate
// drifts with one cause. A field added to the variant but not to the anonymous
// struct loses its key; a field kept in the anonymous struct but dropped from
// the value literal beneath it keeps its key and always carries the zero value.
// Neither is visible downstream, because both of the wire's other readers
// regenerate: `-update` blesses either mutant and the package goes green
// (bd gqlc-xpwox measured five such mutants, one per variant carrying a
// Nullable, all five KILLED before regeneration and SURVIVING after).
//
// The value half rests on the fill being non-zero, which is what nonZero is
// for: the variant goes in with every leaf differing from its zero value, so
// any field whose emitted value equals its type's zero encoding lost its value
// on the way out. What that does NOT catch is a value literal that hardcodes a
// non-zero constant — `Nullable: true` emits true for a mandatory list and no
// row here objects. TestResolvedListMarshalsItsOwnNullability is the shape that
// catches that one, and it costs a test per field rather than one per package.
//
// A variant whose nonZero fill happens to marshal to its zero encoding would
// fail here on unmutated code rather than pass silently. That is the intended
// direction: it means nonZero cannot distinguish that field's presence from its
// absence, so no row below it is measuring anything.
//
// The census stays one-directional in the other axis. It does not forbid keys
// the struct does not declare, because the "kind" discriminator every variant
// emits is exactly such a key and is the point of the tagged-union encoding.
func TestMarshalJSONEmitsEveryDeclaredField(t *testing.T) {
	for name, inh := range inhabitants {
		t.Run(name, func(t *testing.T) {
			typ := reflect.TypeOf(inh.value)
			require.Equalf(t, reflect.Struct, typ.Kind(),
				"%s: inhabitants holds a non-struct value form", name)

			filled := nonZero(t, typ).Interface()
			raw, err := json.Marshal(filled)
			require.NoErrorf(t, err, "%s: marshalling a fully populated value", name)

			var got map[string]json.RawMessage
			require.NoErrorf(t, json.Unmarshal(raw, &got),
				"%s: MarshalJSON emitted something that is not a JSON object: %s", name, raw)

			for _, f := range declaredJSONFields(typ) {
				require.Containsf(t, got, f.tag,
					"%s declares a field with json tag %q, but its MarshalJSON does not emit it. "+
						"The anonymous struct in %s.MarshalJSON restates the field list and has gone "+
						"short, so this field is invisible to the corpus sweep's accept digest and to "+
						"the validated goldens. Emitted: %s", name, f.tag, name, raw)

				require.NotEqualf(t, zeroWire(t, f.typ), string(got[f.tag]),
					"%s emitted the key %q carrying the ZERO value of %s, from an input whose every "+
						"field was non-zero. The anonymous struct in %s.MarshalJSON declares this "+
						"field but the value literal beneath it does not assign it, so the key is on "+
						"the wire and the value is not: every %s ever marshalled reports this field as "+
						"%s. Regenerating the validated goldens and the corpus sweep manifest hides "+
						"this — they are the wire's only other readers and both rebuild from these "+
						"same methods. Emitted: %s", name, f.tag, f.typ, name, name,
					zeroWire(t, f.typ), raw)
			}
		})
	}
}

// TestResolvedListMarshalsItsOwnNullability holds the one thing the census
// above still cannot see, now that the census reads values as well as names.
//
// The census marshals a fill whose every field is non-zero and objects to any
// field that comes back zero, so it already kills the mutant gqlc-lgbjy owed a
// guard for: deleting `Nullable: r.Nullable` from the value literal. It does
// not kill a value literal that hardcodes a non-zero constant. `Nullable: true`
// satisfies every row of the census — the fill's Nullable IS true — while
// reporting every mandatory list on the wire as nullable. Two inputs differing
// in that one field are what distinguishes a propagated value from a constant,
// and only a per-field table can supply them.
//
// It is kept for ResolvedList alone rather than replicated across the four
// sibling variants carrying a Nullable, because the census covers the dropped
// half for all eight at once (bd gqlc-xpwox) and the hardcode half has never
// been observed. If it is ever observed, the fix is this shape, per field.
func TestResolvedListMarshalsItsOwnNullability(t *testing.T) {
	elem := resolver.ResolvedEdge{EdgeKey: schema.EdgeKey{
		Source: "Person", KeyLabels: "KNOWS", Target: "Person",
	}}
	for _, tt := range []struct {
		name string
		in   resolver.ResolvedList
		want bool
	}{
		{"nullable", resolver.ResolvedList{Element: elem, Nullable: true}, true},
		{"mandatory", resolver.ResolvedList{Element: elem}, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			raw, err := json.Marshal(tt.in)
			require.NoError(t, err)

			var got struct {
				Nullable *bool `json:"nullable"`
			}
			require.NoError(t, json.Unmarshal(raw, &got))
			require.NotNilf(t, got.Nullable,
				"ResolvedList.MarshalJSON emitted no \"nullable\" key at all: %s", raw)
			require.Equalf(t, tt.want, *got.Nullable,
				"ResolvedList.MarshalJSON carried the wrong nullability onto the wire: %s", raw)
		})
	}
}
