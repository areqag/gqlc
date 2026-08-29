package config_test

import (
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/areqag/gqlc/internal/config"
)

// graphSeqOf returns the graph sequence node of body, the same node the
// document scan holds past the strict decode.
func graphSeqOf(t *testing.T, body string) *yaml.Node {
	t.Helper()
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(body), &doc); err != nil {
		t.Fatalf("unmarshal node: %v", err)
	}
	root := doc.Content[0]
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value == "graph" {
			return root.Content[i+1]
		}
	}
	t.Fatalf("no graph key in %q", body)
	return nil
}

// TestEntryCountInvariant drives the §4 count check through its
// unexported helper rather than through a document: §4 establishes that
// no input reaches it — a dropped element means yaml.v3 lost one the
// scan saw and no §4.4 rule caught it — so the only way to exercise the
// message is to hand it a mismatched pair directly.
func TestEntryCountInvariant(t *testing.T) {
	body := `version: 1
graph:
  - a
  - b
  - c
`
	seq := graphSeqOf(t, body)

	t.Run("names both counts", func(t *testing.T) {
		err := config.CheckEntryCount(seq, 2)
		if err == nil {
			t.Fatal("expected a mismatch to be reported")
		}
		want := `internal: "graph" declares 3 entries but 2 decoded; the entry indices in any further message would be wrong`
		if err.Error() != want {
			t.Fatalf("checkEntryCount = %q; want %q", err.Error(), want)
		}
	})

	t.Run("agreement is silent", func(t *testing.T) {
		if err := config.CheckEntryCount(seq, 3); err != nil {
			t.Fatalf("config.CheckEntryCount(3, 3) = %v; want nil", err)
		}
	})

	// The canonical fixture runs the check for real; a load that trips it
	// fails here rather than only in the message test above.
	t.Run("does not fire for an accepted document", func(t *testing.T) {
		cfg, err := config.Load("testdata/canonical.gqlc.yaml")
		if err != nil {
			t.Fatalf("load fixture: %v", err)
		}
		if len(cfg.Targets) != 2 {
			t.Fatalf("got %d targets, want 2", len(cfg.Targets))
		}
	})
}
