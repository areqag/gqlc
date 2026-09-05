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

// declaredJSONTags returns the wire name of every exported field of typ, in
// declaration order. A field tagged `json:"-"` is deliberately not emitted and
// is excluded; an untagged field marshals under its Go name, which is the name
// the census then demands.
func declaredJSONTags(typ reflect.Type) []string {
	var tags []string
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
		tags = append(tags, name)
	}
	return tags
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
// The census is deliberately one-directional. It demands that every declared
// field reaches the output; it does not forbid keys the struct does not declare,
// because the "kind" discriminator every variant emits is exactly such a key and
// is the point of the tagged-union encoding.
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

			for _, tag := range declaredJSONTags(typ) {
				require.Containsf(t, got, tag,
					"%s declares a field with json tag %q, but its MarshalJSON does not emit it. "+
						"The anonymous struct in %s.MarshalJSON restates the field list and has gone "+
						"short, so this field is invisible to the corpus sweep's accept digest and to "+
						"the validated goldens. Emitted: %s", name, tag, name, raw)
			}
		})
	}
}

// TestResolvedListMarshalsItsOwnNullability holds the VALUE of the one
// field gqlc-lgbjy added, which the census above cannot see.
//
// The census is a census of NAMES. Deleting `Nullable: r.Nullable` from
// the value literal while leaving the field in the anonymous struct still
// emits "nullable", now always false, and the census demands the key and
// passes. The wire's only other readers regenerate: measured, that mutant
// reddens the package and then goes ENTIRELY GREEN once the goldens and
// the sweep manifest are rebuilt with the commands their own failures
// print. So without this, a var-length binding's optionality could be
// dropped from the wire by a regeneration and nothing would say so —
// which is this bead's own defect, one level down.
//
// It is a local test rather than a second direction on the census because
// the same hole is open on four sibling variants and closing it for all
// of them is one census extension, not five local tests: bd gqlc-xpwox.
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
