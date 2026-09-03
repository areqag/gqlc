// Package graph holds the value vocabulary of the property-graph domain: the
// primitive types that describe graph elements — label sets and property value
// types — shared across the schema and query models.
package graph

import (
	"slices"
	"strings"
)

// LabelSet is a set of labels in source form. It is the input used to build a
// LabelSetKey; parsed models store identity as the key, not the slice.
type LabelSet []string

// LabelSetKey is the canonical, comparable form of a LabelSet, usable as a map
// key: labels sorted, deduplicated, and joined with "&".
type LabelSetKey string

// Key canonicalises the set into its map key. The original slice is left
// unmodified.
//
// The join does not quote, so the key is only an identity if no label contains
// "&" — otherwise the one-label set {A&B} keys identically to the two-label set
// {A, B}. That is a guarantee the producers give, not one enforced here: the GQL
// front end refuses such a label at the token read (gql.ErrAmpersandInLabel).
// Making one representable is bd gqlc-yd4ba.
func (ls LabelSet) Key() LabelSetKey {
	sorted := slices.Clone(ls)
	slices.Sort(sorted)
	sorted = slices.Compact(sorted)
	return LabelSetKey(strings.Join(sorted, "&"))
}

// Split returns the individual labels encoded in the key. It is the inverse of
// LabelSet.Key. The empty key yields no labels.
func (k LabelSetKey) Split() LabelSet {
	if k == "" {
		return nil
	}
	return strings.Split(string(k), "&")
}
