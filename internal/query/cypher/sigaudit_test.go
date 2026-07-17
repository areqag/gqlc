//go:build sigaudit_scratch

package cypher_test

import (
	"bufio"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/areqag/gqlc/internal/procsig"
)

// TestSigAudit is the Phase-1 scratch tool that produces the sig-audit
// ledger consumed by Phase 2 (docs/specs/cypher-golden-test-migration-
// sigaudit.txt). For each sig-carrying pin in mustParse whose src is
// verbatim in the TCK corpus AND has a golden on disk, it pairs the pin's
// authored sigs against the Background-declared sigs of the matching
// scenario and prints one ledger line:
//
//	<key>\t<equal|divergent>\t<goldenPath>[\t<divergence-reason>]
//
// Build-tagged out of `just test` so the ledger regenerates only on
// demand: `go test -tags sigaudit_scratch -run TestSigAudit ./internal/query/cypher/...`.
// The ledger location is fixed relative to the package dir; overwrite via
// the RunTestMain -update convention would add flag plumbing for no gain
// while the artifact lives.
func TestSigAudit(t *testing.T) {
	scenarios := harvestExecutingScenarios(t, readCoreDirs)
	if len(scenarios) == 0 {
		t.Fatal("no corpus scenarios harvested")
	}
	byQuery := make(map[string][]scenarioMeta, len(scenarios))
	for _, sc := range scenarios {
		byQuery[sc.query] = append(byQuery[sc.query], sc)
	}

	type row struct {
		key, verdict, path, reason string
	}
	var rows []row
	for key, pin := range mustParse {
		if len(pin.sigs) == 0 {
			continue
		}
		matches, ok := byQuery[pin.src]
		if !ok {
			continue
		}
		var sc scenarioMeta
		var haveGolden bool
		for _, m := range matches {
			p := goldenPath(&scenarioState{name: m.name, uri: m.uri, query: m.query})
			if _, err := os.Stat(p); err == nil {
				sc = m
				haveGolden = true
				break
			}
		}
		if !haveGolden {
			continue
		}
		path := goldenPath(&scenarioState{name: sc.name, uri: sc.uri, query: sc.query})
		verdict := "equal"
		reason := ""
		if !sigsEqual(pin.sigs, sc.sigs) {
			verdict = "divergent"
			reason = sigDivergenceReason(pin.sigs, sc.sigs)
		}
		rows = append(rows, row{key, verdict, path, reason})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].key < rows[j].key })

	ledger := filepath.Join("..", "..", "..", "docs", "specs", "cypher-golden-test-migration-sigaudit.txt")
	f, err := os.Create(ledger)
	if err != nil {
		t.Fatalf("create %s: %v", ledger, err)
	}
	w := bufio.NewWriter(f)
	for _, r := range rows {
		if r.reason == "" {
			if _, err := w.WriteString(r.key + "\t" + r.verdict + "\t" + r.path + "\n"); err != nil {
				t.Fatalf("write ledger: %v", err)
			}
			continue
		}
		if _, err := w.WriteString(r.key + "\t" + r.verdict + "\t" + r.path + "\t" + r.reason + "\n"); err != nil {
			t.Fatalf("write ledger: %v", err)
		}
	}
	if err := w.Flush(); err != nil {
		t.Fatalf("flush ledger: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close ledger: %v", err)
	}
	t.Logf("wrote %d ledger rows to %s", len(rows), ledger)
}

// sigsEqual compares two signature slices by structural equality, treating
// nil and empty-slice as equal. reflect.DeepEqual would treat nil !=
// []T{}; the ledger cares only about signature shape.
func sigsEqual(a, b []procsig.Signature) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !reflect.DeepEqual(a[i], b[i]) {
			return false
		}
	}
	return true
}

// sigDivergenceReason produces a compact tab-safe human-readable summary
// of why two sig slices differ, for the ledger's optional fourth column.
// Ledger consumers (linus-3, Phase 2) cite this text; keep it terse.
func sigDivergenceReason(pin, corpus []procsig.Signature) string {
	if len(pin) != len(corpus) {
		return "count(pin)=" + itoa(len(pin)) + " count(corpus)=" + itoa(len(corpus))
	}
	var parts []string
	for i := range pin {
		if reflect.DeepEqual(pin[i], corpus[i]) {
			continue
		}
		parts = append(parts, "sig["+itoa(i)+"]: pin="+sigString(pin[i])+" corpus="+sigString(corpus[i]))
	}
	return strings.Join(parts, "; ")
}

func sigString(s procsig.Signature) string {
	var b strings.Builder
	b.WriteString(s.Name)
	b.WriteString("(")
	for i, p := range s.Params {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString(p.Name)
	}
	b.WriteString(")::(")
	for i, r := range s.Results {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString(r.Name)
	}
	b.WriteString(")")
	return b.String()
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
