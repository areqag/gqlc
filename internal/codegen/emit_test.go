package codegen_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/areqag/gqlc/internal/codegen"
)

// TestFinaliseIsFailClosedOnUnformattableContents witnesses the claim
// emit.go's doc comment, docs/specs/codegen-sentinel-taxonomy.md §4 and
// PR #2589's justification all rest on: a format.Source rejection returns
// (nil, err) and no raw bytes reach the caller.
//
// Until this test nothing held that behaviourally. A fail-open rewrite
// keeping a dead branch to satisfy the sentinel census survived the full
// module — 29 packages, zero failures (bd gqlc-tf8sv, measured on patrol
// gqlc-46s2b). The census that killed the naive rewrite asks whether a
// branch RETURNING the sentinel exists and goes unexecuted, which cannot
// distinguish fail-closed from fail-open.
//
// It is a direct unit test rather than a corpus fixture because §4
// declined a fixture on the ground that firing ErrFormatFailure needs
// synthetic template corruption — a test seam whose value does not pay
// for its cost. That reasoning is sound and untouched here: this drives
// the exported Finalise with contents of its own, adding no seam.
//
// Living in this package's own test binary is what keeps it off
// TestExcludedBranchesAreUnreached, which requires emit.go's return to
// execute zero times and would otherwise go red for the wrong reason.
// corpusPackages skips codegenPkgPath, and corpusPackageOf strips the
// `_test` suffix, so `codegen_test` reduces to the package under test and
// is skipped with it.
func TestFinaliseIsFailClosedOnUnformattableContents(t *testing.T) {
	// \x01 is not a legal character in Go source, so the scanner refuses
	// it. Corrupting the package clause rather than a later construct
	// keeps the refusal in go/format and out of anything downstream.
	broken := codegen.File{Path: "broken.go", Contents: []byte("package \x01")}
	valid := codegen.File{Path: "valid.go", Contents: []byte("package p\n")}

	// Both orderings, because they witness different halves and the
	// second is the one that can fail. With the broken file first the
	// loop returns at index 0, so a partial-slice implementation and a
	// fail-closed one are indistinguishable — there is nothing partial to
	// return yet. Only "failing last" reaches a state where a formatted
	// file is already in hand.
	positions := []struct {
		name  string
		files []codegen.File
	}{
		{"the failing file first", []codegen.File{broken, valid}},
		{"the failing file last", []codegen.File{valid, broken}},
	}

	for _, pos := range positions {
		t.Run(pos.name, func(t *testing.T) {
			files := append([]codegen.File(nil), pos.files...)

			got, err := codegen.Finalise(files)

			require.Error(t, err,
				"Finalise accepted contents go/format rejects; unformattable emission is always unparseable Go, so a nil error here writes a broken file into the user's tree")
			require.ErrorIs(t, err, codegen.ErrFormatFailure,
				"a format.Source rejection must surface as ErrFormatFailure, which is the sentinel the taxonomy documents and callers match on")
			require.Contains(t, err.Error(), "broken.go",
				"the error must name the offending path, which is the whole of what makes a template bug findable")
			require.NotContains(t, err.Error(), "valid.go",
				"the error names the file that failed and not a bystander, or the path it carries points a reader at the wrong template")
			require.Nil(t, got,
				"Finalise must return no files at all on failure; a partial slice is raw unformatted contents reaching a caller that only checked for a nil error")
		})
	}
}
