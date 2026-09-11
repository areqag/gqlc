package backends_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/areqag/gqlc/internal/cli/backends"
	"github.com/areqag/gqlc/internal/codegen/age"
	"github.com/areqag/gqlc/internal/codegen/neo4j"
	"github.com/areqag/gqlc/internal/config"
)

// TestRegistryParityWithDriverVocabulary asserts the two lists that name
// the same thing — the registry's keys and config's Driver enum — hold
// exactly the same members. The loader validates a config file against
// the enum and the pipeline resolves the accepted value through the
// registry, so a member present in one list and absent from the other is
// either a driver the loader accepts and generation cannot serve or a
// backend no config file can reach.
//
// Both directions are asserted separately so a failure names which list
// is short, and the length check closes the remaining hole: a duplicate
// on one side satisfies both Contains sweeps.
func TestRegistryParityWithDriverVocabulary(t *testing.T) {
	reg, err := backends.Registry()
	require.NoError(t, err)

	keys := reg.Keys()
	drivers := make([]string, 0, len(config.DriverValues()))
	for _, d := range config.DriverValues() {
		drivers = append(drivers, string(d))
	}
	require.NotEmpty(t, drivers, "the driver vocabulary must not be empty")

	for _, key := range keys {
		require.Contains(t, drivers, key, "registry key %q has no config.Driver const", key)
	}
	for _, driver := range drivers {
		require.Contains(t, keys, driver, "config.Driver %q has no registry entry", driver)
	}
	require.Len(t, keys, len(drivers),
		"the sweeps above both pass when config.DriverValues repeats a member; NewRegistry rejects a duplicate key, so the counts diverge only when the vocabulary carries one")
}

// TestRegistryPublishesTheBackendSentinels is the witness that the wire
// is live AT THE COMPOSITION ROOT. The pin on each map itself lives in
// the backend package, and the conformance corpus consumes the merged
// result, but neither can see this file: dropping `Sentinels:` from an
// entry below leaves both of those green in their own packages and
// silently unpublishes that backend's refusals.
//
// It asserts the merged map holds exactly the union of what the backends
// publish, rather than listing names of its own, because the names are
// pinned where the symbols are visible and a second list here would only
// drift from those.
//
// Each backend is also required to contribute something. The union alone
// would pass with one side publishing nothing, which is the failure this
// test was written for and which it could no longer see once there were
// two publishers — a registry that dropped `Sentinels:` from BOTH neo4j
// entries still merges to AGE's map, and AGE's map is most of it.
func TestRegistryPublishesTheBackendSentinels(t *testing.T) {
	reg, err := backends.Registry()
	require.NoError(t, err)

	merged := make(map[string]error)
	for name, backend := range map[string]map[string]error{"age": age.Sentinels(), "neo4j": neo4j.Sentinels()} {
		require.NotEmptyf(t, backend, "the %s backend publishes nothing, so this sweep reconciles nothing for it", name)
		for key, sentinel := range backend {
			require.NotContainsf(t, merged, key,
				"%s publishes %q and so does another backend; the registry resolves one name to one value, so this "+
					"sweep cannot say which of them the merged map answers with", name, key)
			merged[key] = sentinel
		}
	}
	require.Equal(t, merged, reg.Sentinels(),
		"the registry's merged sentinels differ from the union of what the backend entries publish")
}
