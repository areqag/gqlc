package codegen

import (
	"errors"
	"fmt"
	"maps"
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

	// Sentinels PUBLISHES this backend's user-reachable refusals — it
	// carries each sentinel's error value together with the spelling a
	// consumer names it by, "<package>.<ExportedVar>", matching the
	// convention the conformance corpus's manifests already use. The
	// names must travel with the values because the only consumer that
	// needs them is one that must not import a backend to get them.
	//
	// Optional: nil publishes nothing, which is the right answer for a
	// backend whose refusals no consumer names. Publication is not free
	// — the conformance suite refuses a published name no fixture
	// witnesses — so publish a name when something is meant to hold you
	// to it.
	Sentinels map[string]error
}

// Registry resolves a driver wire key to the backend that emits for it.
// Immutable after construction and safe for concurrent reads; the zero
// value holds no backend, so every Lookup misses.
type Registry struct {
	byKey     map[string]func(pkg string) Generator
	sentinels map[string]error
}

// NewRegistry validates entries and returns the immutable Registry. It
// rejects an empty Key, a nil New, and a Key registered twice, returning
// the zero Registry and an error naming the offender. No entries yields
// an empty Registry without error.
//
// It also merges what the entries publish (see [Entry.Sentinels]),
// rejecting a name published under an empty string, a name published
// with no error value, and one name published by two entries under
// DIFFERENT error values — that last is a collision the merged map would
// otherwise resolve by whichever entry was listed later, silently
// answering a consumer with the wrong backend's refusal. Two entries
// publishing a name under the same value dedupe without complaint: two
// wire keys may share one backend package, and then they publish that
// package's names identically.
func NewRegistry(entries ...Entry) (Registry, error) {
	byKey := make(map[string]func(pkg string) Generator, len(entries))
	sentinels := make(map[string]error)
	publisher := make(map[string]string)
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
		for name, sentinel := range e.Sentinels {
			if name == "" {
				return Registry{}, fmt.Errorf("codegen: registry entry %q publishes a sentinel under an empty name", e.Key)
			}
			if sentinel == nil {
				return Registry{}, fmt.Errorf("codegen: registry entry %q publishes %q with no error value", e.Key, name)
			}
			// Identity, not errors.Is: the rule is that one name resolves
			// to one value, and errors.Is would accept an entry publishing
			// a wrapper of what another entry published, leaving the name
			// ambiguous — which is the collision this refuses.
			//nolint:errorlint // identity match on package-level sentinels is intended
			if held, seen := sentinels[name]; seen && held != sentinel {
				return Registry{}, fmt.Errorf("codegen: registry entries %q and %q publish different errors under %q",
					publisher[name], e.Key, name)
			}
			sentinels[name] = sentinel
			publisher[name] = e.Key
		}
	}
	return Registry{byKey: byKey, sentinels: sentinels}, nil
}

// Lookup resolves a driver wire key drawn from the config vocabulary.
func (r Registry) Lookup(key string) (func(pkg string) Generator, bool) {
	newGen, ok := r.byKey[key]
	return newGen, ok
}

// Sentinels returns every published name merged across the entries,
// mapped to the error value itself so a caller can match it with
// errors.Is. The result is a copy: the registry is immutable after
// construction and a consumer folding these into a map of its own must
// not be able to reach back through it.
func (r Registry) Sentinels() map[string]error {
	return maps.Clone(r.sentinels)
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
