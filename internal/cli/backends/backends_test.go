package backends_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/areqag/gqlc/internal/cli/backends"
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
	require.Len(t, keys, len(drivers))
}
