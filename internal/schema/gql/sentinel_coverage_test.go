package gql_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/areqag/gqlc/internal/schema"
	"github.com/areqag/gqlc/internal/schema/gql"
)

// TestAllSentinelsAreReachable loads every file in invalid/ and every corpus
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
//
// Both halves go through the Loader, each file rooted at its own directory. For
// a file that names no other, that is the same reading Parse gives; for the four
// resolution failures it is the only reading that produces them at all, since
// Parse has no catalogue to fail against.
func TestAllSentinelsAreReachable(t *testing.T) {
	reached := make(map[string]bool, len(allSentinels))

	record := func(err error) {
		if err == nil {
			return
		}
		for name, sentinel := range allSentinels {
			if errors.Is(err, sentinel) {
				reached[name] = true
			}
		}
	}

	// invalid/ fixtures: every file there must produce a non-nil error.
	invalidDir := filepath.Join(fixtureDir, "invalid")
	invalidFiles, err := filepath.Glob(filepath.Join(invalidDir, "*.gql"))
	require.NoError(t, err)
	require.NotEmpty(t, invalidFiles, "invalid/ fixture directory must not be empty")

	for _, path := range invalidFiles {
		_, loadErr := loadFixture(path)
		record(loadErr)
	}

	// corpus unsupported entries: declared sentinel must match actual error.
	for _, entry := range corpusManifest(t) {
		if entry.outcome != unsupported {
			continue
		}
		_, loadErr := loadFixture(filepath.Join(corpusDir, entry.file))
		record(loadErr)
	}

	for name := range allSentinels {
		require.True(t, reached[name],
			"sentinel %s is in allSentinels but no file in invalid/ or corpus (unsupported) produces it", name)
	}
}

// loadFixture reads one fixture through the Loader with the file's own directory
// as the catalogue root. That anchoring is what gives the reference fixtures their
// outcomes, and it is shared with TestCorpusOutcomes so the two cannot disagree
// about what a file means.
func loadFixture(path string) (schema.Schema, error) {
	return gql.NewLoader(os.DirFS(filepath.Dir(path))).Load(filepath.Base(path))
}
