//go:build sigaudit_scratch

package cypher_test

import (
	"os"
	"testing"
)

// TestCensusReport reproduces §1 / §7 census numbers of the golden-test
// migration spec end-to-end from HEAD: unique corpus queries, corpus queries
// with an on-disk golden, and the mustParse 3-way bucket (in-corpus with
// golden / in-corpus without golden / not-in-corpus). Emitted with t.Logf so
// `go test -v` shows the counts alongside the -run selector; use it before
// each phase to catch a silent TCK bump before it drifts one of §1's numbers
// under the deletion arithmetic. Build-tagged out of `just test` — invoke
// via `go test -tags sigaudit_scratch -run TestCensusReport -v ./internal/query/cypher/...`.
func TestCensusReport(t *testing.T) {
	scenarios := harvestExecutingScenarios(t, readCoreDirs)
	if len(scenarios) == 0 {
		t.Fatal("no corpus scenarios harvested")
	}
	uniqueQueries := make(map[string]bool)
	queriesWithGolden := make(map[string]bool)
	byQuery := make(map[string][]scenarioMeta, len(scenarios))
	for _, sc := range scenarios {
		uniqueQueries[sc.query] = true
		byQuery[sc.query] = append(byQuery[sc.query], sc)
		path := goldenPath(&scenarioState{name: sc.name, uri: sc.uri, query: sc.query})
		if _, err := os.Stat(path); err == nil {
			queriesWithGolden[sc.query] = true
		}
	}

	inCorpusWithGolden := 0
	inCorpusNoGolden := 0
	notInCorpus := 0
	for _, pin := range mustParse {
		if _, ok := byQuery[pin.src]; !ok {
			notInCorpus++
			continue
		}
		hasGolden := false
		for _, sc := range byQuery[pin.src] {
			p := goldenPath(&scenarioState{name: sc.name, uri: sc.uri, query: sc.query})
			if _, err := os.Stat(p); err == nil {
				hasGolden = true
				break
			}
		}
		if hasGolden {
			inCorpusWithGolden++
		} else {
			inCorpusNoGolden++
		}
	}

	t.Logf("Unique corpus queries harvested: %d", len(uniqueQueries))
	t.Logf("Corpus queries with an on-disk golden: %d", len(queriesWithGolden))
	t.Logf("mustParse srcs: %d", len(mustParse))
	t.Logf("  A. in corpus AND golden exists: %d", inCorpusWithGolden)
	t.Logf("  B. in corpus BUT no golden: %d", inCorpusNoGolden)
	t.Logf("  C. NOT in corpus: %d", notInCorpus)
}
