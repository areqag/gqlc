package config_test

import (
	"bytes"
	"fmt"
	"go/token"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/areqag/gqlc/internal/config"
	"github.com/areqag/gqlc/internal/fuzzkit"
)

// FuzzConfigDecode is the config half of target T5 of
// docs/specs/nightly-fuzzing-plan.md. On every input it checks:
//
//   - an error comes with the zero Config, names its source ("config:
//     <stream>"), and is not the loader's own entry-count invariant firing
//     (checkEntryCount's "internal:" message, which no document is known to
//     reach);
//   - a success declares at least one target, every enum field holds a member
//     of its vocabulary, every required string is non-empty, the package is a
//     Go identifier, and no two targets share an output directory;
//   - canonical round-trip: Decode(Canonical(c)) is c and Canonical of that is
//     the same bytes (Save's documented "Load(Save(c)) round-trips exactly");
//   - determinism: two decodes of the same input are reflect.DeepEqual.
//
// The plan's row asks for "err class = sentinel". This package has no
// sentinels — every error is a plain fmt.Errorf whose text echoes decoded
// input — so fuzzkit.ClassifyError would answer plain for every refusal, and
// "bug:" in its text can be the input's own. The provenance and invariant
// checks above stand in for the classification.
func FuzzConfigDecode(f *testing.F) {
	fuzzkit.SeedFiles(f, "testdata/canonical.gqlc.yaml")
	// Shapes the canonical file does not reach: the flat format that preceded
	// graph, an anchor and alias, and a null entry.
	f.Add("version: 1\nschema: s.gql\nqueries: q\n")
	f.Add("version: 1\ngraph:\n  - &t\n    schema: s.gql\n    schema_language: gql\n    queries: q\n    query_language: opencypher\n    gen: {go: {package: p, out: o, driver: apache-age-pgx-v5}}\n  - *t\n")
	f.Add("version: 1\ngraph:\n  - null\n")
	f.Fuzz(func(t *testing.T, src string) {
		fuzzkit.Skip(t, []byte(src))
		var first, second config.Config
		var err1, err2 error
		fuzzkit.Guard(t, 2*time.Second, func() {
			first, err1 = config.Decode(strings.NewReader(src))
			second, err2 = config.Decode(strings.NewReader(src))
		})
		if !reflect.DeepEqual(first, second) || fmt.Sprint(err1) != fmt.Sprint(err2) {
			t.Fatalf("property determinism: two decodes disagree:\n%#v, %v\n%#v, %v", first, err1, second, err2)
		}
		if err1 != nil {
			if msg := refusalViolation(first, err1); msg != "" {
				t.Fatal(msg)
			}
			return
		}
		if msg := configViolation(first); msg != "" {
			t.Fatal(msg)
		}
		var canon []byte
		var back config.Config
		var errBack error
		fuzzkit.Guard(t, 2*time.Second, func() {
			canon, errBack = first.Canonical()
			if errBack == nil {
				back, errBack = config.Decode(bytes.NewReader(canon))
			}
		})
		if errBack != nil {
			t.Fatalf("property round-trip: the canonical form of an accepted config does not load: %v\n%s", errBack, canon)
		}
		if !reflect.DeepEqual(first, back) {
			t.Fatalf("property round-trip: Decode(Canonical(c)) differs from c:\n%#v\n%#v\n%s", first, back, canon)
		}
		if again, err := back.Canonical(); err != nil || !bytes.Equal(again, canon) {
			t.Fatalf("property round-trip: Canonical is not a fixpoint (%v):\n%s\n%s", err, canon, again)
		}
	})
}

func refusalViolation(c config.Config, err error) string {
	if !reflect.DeepEqual(c, config.Config{}) {
		return fmt.Sprintf("property error-zero: Decode returned %#v alongside %v", c, err)
	}
	msg := err.Error()
	if !strings.HasPrefix(msg, "config: <stream>") {
		return fmt.Sprintf("property error-provenance: %q does not name its source", msg)
	}
	if strings.HasPrefix(msg, "config: <stream>: internal:") {
		return fmt.Sprintf("property entry-count: the loader's own invariant fired: %v", err)
	}
	return ""
}

func configViolation(c config.Config) string {
	if len(c.Targets) == 0 {
		return "property targets: an accepted config declares no target"
	}
	outs := map[string]int{}
	for i, tg := range c.Targets {
		switch {
		case !slices.Contains(config.SchemaLangValues(), tg.SchemaLang):
			return fmt.Sprintf("property enum: graph[%d].SchemaLang is %q", i, tg.SchemaLang)
		case !slices.Contains(config.QueryLangValues(), tg.QueryLang):
			return fmt.Sprintf("property enum: graph[%d].QueryLang is %q", i, tg.QueryLang)
		case !slices.Contains(config.DriverValues(), tg.Go.Driver):
			return fmt.Sprintf("property enum: graph[%d].Go.Driver is %q", i, tg.Go.Driver)
		case tg.SchemaPath == "" || tg.QueryDir == "" || tg.Go.Out == "" || tg.Go.Package == "":
			return fmt.Sprintf("property required: graph[%d] has an empty required field: %#v", i, tg)
		case !token.IsIdentifier(tg.Go.Package):
			return fmt.Sprintf("property package: graph[%d].Go.Package %q is not a Go identifier", i, tg.Go.Package)
		}
		clean := filepath.Clean(tg.Go.Out)
		if j, dup := outs[clean]; dup {
			return fmt.Sprintf("property out-overlap: graph[%d] and graph[%d] both write %q", j, i, clean)
		}
		outs[clean] = i
	}
	return ""
}
