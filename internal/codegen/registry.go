package codegen

import (
	"errors"
	"fmt"
	"slices"
)

// Entry binds one driver wire key to the backend that emits for it.
// Both halves are required — [NewRegistry] rejects an entry missing
// either, so a key never reaches a lookup with no constructor behind it.
//
// New takes the generated package's name, the one construction knob
// every backend shares; the empty string leaves the backend's own
// derivation in place.
type Entry struct {
	Key string
	New func(pkg string) Generator
}

// Registry resolves a driver wire key to the backend that emits for it.
// Immutable after construction and safe for concurrent reads; the zero
// value holds no backend, so every Lookup misses.
type Registry struct {
	byKey map[string]func(pkg string) Generator
}

// NewRegistry validates entries and returns the immutable Registry. It
// rejects an empty Key, a nil New, and a Key registered twice, returning
// the zero Registry and an error naming the offender. No entries yields
// an empty Registry without error.
func NewRegistry(entries ...Entry) (Registry, error) {
	byKey := make(map[string]func(pkg string) Generator, len(entries))
	for _, e := range entries {
		if e.Key == "" {
			return Registry{}, errors.New("codegen: registry entry key must not be empty")
		}
		if e.New == nil {
			return Registry{}, fmt.Errorf("codegen: registry entry %q has no constructor", e.Key)
		}
		if _, dup := byKey[e.Key]; dup {
			return Registry{}, fmt.Errorf("codegen: duplicate registry entry key %q", e.Key)
		}
		byKey[e.Key] = e.New
	}
	return Registry{byKey: byKey}, nil
}

// Lookup resolves a driver wire key drawn from the config vocabulary.
func (r Registry) Lookup(key string) (func(pkg string) Generator, bool) {
	newGen, ok := r.byKey[key]
	return newGen, ok
}

// Keys returns every registered key, sorted.
func (r Registry) Keys() []string {
	keys := make([]string, 0, len(r.byKey))
	for k := range r.byKey {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}
