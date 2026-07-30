package gql

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestAllSentinelsAreReachable parses every file in invalid/ and every corpus
// file declared unsupported, collects the errors actually produced, and asserts
// that every sentinel in allSentinels is wrapped by at least one of them.
//
// Unlike TestSentinelReachability (which trusts invalidFixtures and the corpus
// manifest), this test runs the parser and reads the errors off the wire. A
// sentinel removed from errors.go drops out of allSentinels (caught by
// TestSentinelRegistryIsComplete), and a sentinel added there but produced by
// no file fails here. A sentinel that stays in allSentinels while the construct
// it guards stops being reached silently passes TestSentinelReachability (the
// maintained map still carries it) but fails here.
func TestAllSentinelsAreReachable(t *testing.T) {
	reached := make(map[string]bool, len(allSentinels))

	// invalid/ fixtures: every file there must produce a non-nil error.
	invalidDir := filepath.Join(fixtureDir, "invalid")
	invalidFiles, err := filepath.Glob(filepath.Join(invalidDir, "*.gql"))
	require.NoError(t, err)
	require.NotEmpty(t, invalidFiles, "invalid/ fixture directory must not be empty")

	for _, path := range invalidFiles {
		src, err := os.ReadFile(path)
		require.NoError(t, err)
		_, parseErr := New().Parse(bytes.NewReader(src))
		if parseErr == nil {
			continue
		}
		for name, sentinel := range allSentinels {
			if errors.Is(parseErr, sentinel) {
				reached[name] = true
			}
		}
	}

	// corpus unsupported entries: declared sentinel must match actual error.
	for _, entry := range corpusManifest(t) {
		if entry.outcome != unsupported {
			continue
		}
		src, err := os.ReadFile(filepath.Join(corpusDir, entry.file))
		require.NoError(t, err)
		_, parseErr := New().Parse(bytes.NewReader(src))
		if parseErr == nil {
			continue
		}
		for name, sentinel := range allSentinels {
			if errors.Is(parseErr, sentinel) {
				reached[name] = true
			}
		}
	}

	for name := range allSentinels {
		require.True(t, reached[name],
			"sentinel %s is in allSentinels but no file in invalid/ or corpus (unsupported) produces it", name)
	}
}
