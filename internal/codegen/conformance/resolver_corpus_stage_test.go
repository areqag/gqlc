package conformance_test

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/areqag/gqlc/internal/codegen"
	"github.com/areqag/gqlc/internal/codegen/age"
	"github.com/areqag/gqlc/internal/codegen/neo4j"
	"github.com/areqag/gqlc/internal/schema/gql"
)

const resolverValidDir = "../../../test/data/resolver/valid"

// resolverCorpusList lifts one marker-delimited list out of the resolver
// corpus README. The markers are HTML comments, so they are invisible when the
// file is read as prose and unambiguous when it is read as data.
func resolverCorpusList(t *testing.T, doc, name string) []string {
	t.Helper()
	re := regexp.MustCompile(`(?s)<!-- BEGIN ` + regexp.QuoteMeta(name) + ` -->\n(.*?)<!-- END ` + regexp.QuoteMeta(name) + ` -->`)
	m := re.FindStringSubmatch(doc)
	require.Len(t, m, 2, "README has no %q block; the test and the document it pins have come apart", name)

	var out []string
	for _, line := range strings.Split(m[1], "\n") {
		if line = strings.TrimSpace(line); line != "" {
			out = append(out, line)
		}
	}
	require.NotEmpty(t, out, "%q block is empty, which would let the partition pass by asserting nothing", name)
	return out
}

// TestResolverValidCorpusStageBoundary holds the claim that
// test/data/resolver/valid/README.md makes: that directory means "valid at
// R3", and codegen refuses much of it at schema admission — before a query is
// looked at.
//
// The README states which schemas fall on each side. This admits every schema
// through both backends and requires the document to match, so a schema that
// starts or stops being codegen-admissible cannot drift away from the prose
// that explains why the corpus is shaped the way it is.
//
// Only the schema layer is pinned. Which fixtures behind an admissible schema
// codegen accepts moves whenever codegen grows a construct, and asserting it
// here would redden the resolver corpus for changes that are not about it.
func TestResolverValidCorpusStageBoundary(t *testing.T) {
	src, err := os.ReadFile(filepath.Join(resolverValidDir, "README.md"))
	require.NoError(t, err, "the corpus README is the artefact this test pins; without it there is no claim to check")
	doc := string(src)

	declaredOK := resolverCorpusList(t, doc, "codegen-admissible")
	declaredNo := resolverCorpusList(t, doc, "codegen-inadmissible")

	files, err := filepath.Glob(filepath.Join(resolverValidDir, "schemas", "*.gql"))
	require.NoError(t, err)
	require.NotEmpty(t, files)

	var actualOK, actualNo, disagree []string
	onDisk := make(map[string]struct{}, len(files))
	for _, path := range files {
		name := filepath.Base(path)
		onDisk[name] = struct{}{}

		gqlSrc, err := os.ReadFile(path)
		require.NoError(t, err)
		sch, err := gql.New().Parse(bytes.NewReader(gqlSrc))
		require.NoError(t, err, "%s: the resolver corpus must parse as a schema before any of this means anything", name)

		// An empty query batch: the only thing under test is whether the
		// schema itself is admissible, so no query can be blamed for the
		// verdict. Admitted schemas prove the empty batch is not itself
		// refused.
		in := codegen.Input{Schema: sch}
		_, ageErr := age.New(age.WithPackageName("stageprobe")).Generate(in)
		_, neoErr := neo4j.New(neo4j.WithPackageName("stageprobe")).Generate(in)

		if (ageErr == nil) != (neoErr == nil) {
			disagree = append(disagree, name)
		}
		if ageErr == nil {
			actualOK = append(actualOK, name)
		} else {
			actualNo = append(actualNo, name)
		}
	}

	// A backend split would make "codegen admits this" ambiguous, and the
	// README's single partition would be quietly reporting whichever backend
	// this test asked first.
	slices.Sort(disagree)
	require.Empty(t, disagree, "backends disagree on schema admissibility, so the README's single partition cannot be true of both")

	var listedButAbsent []string
	for _, name := range slices.Concat(declaredOK, declaredNo) {
		if _, ok := onDisk[name]; !ok {
			listedButAbsent = append(listedButAbsent, name)
		}
	}
	slices.Sort(listedButAbsent)
	require.Empty(t, listedButAbsent, "README lists schemas that are not in schemas/; a renamed or deleted schema left its declaration behind")

	// Comparing the two lists whole would report a disagreement by printing
	// both of them and leaving the reader to find the row that differs. The
	// event worth catching is one schema changing sides, so name that schema
	// and say which way it went.
	side := func(names []string) map[string]struct{} {
		m := make(map[string]struct{}, len(names))
		for _, n := range names {
			m[n] = struct{}{}
		}
		return m
	}
	inDeclaredOK, inDeclaredNo := side(declaredOK), side(declaredNo)

	var moved, undeclared []string
	for _, name := range slices.Concat(actualOK, actualNo) {
		_, sayOK := inDeclaredOK[name]
		_, sayNo := inDeclaredNo[name]
		admitted := slices.Contains(actualOK, name)
		switch {
		case !sayOK && !sayNo:
			undeclared = append(undeclared, name)
		case sayOK && sayNo:
			moved = append(moved, name+": listed in both blocks")
		case sayOK && !admitted:
			moved = append(moved, name+": README says codegen admits it, both backends refuse it")
		case sayNo && admitted:
			moved = append(moved, name+": README says codegen refuses it, both backends admit it")
		}
	}
	slices.Sort(moved)
	slices.Sort(undeclared)

	require.Empty(t, moved, "schemas changed codegen-admissibility side without the README moving with them")
	require.Empty(t, undeclared, "schemas in schemas/ are in neither README block, so the partition does not cover the directory it describes")
}
