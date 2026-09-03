package gql_test

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"

	"github.com/areqag/gqlc/internal/schema/gql"
)

// body returns a one-node graph type declaration named name, the smallest thing
// a reference chain can terminate in.
func body(name string) string {
	return "CREATE GRAPH TYPE " + name + " { (a:A) }"
}

// copyOf returns a declaration of name whose source is the reference ref.
func copyOf(name, ref string) string {
	return "CREATE GRAPH TYPE " + name + " COPY OF " + ref
}

func file(content string) *fstest.MapFile { return &fstest.MapFile{Data: []byte(content)} }

// TestLoaderResolvesChains covers the supported anchorings and what a resolved
// model carries. The Name assertions are the point of several rows: COPY OF
// means the tail's element types under the ROOT's declared name, so a chain that
// renames twice must still surface the name the generation target declared.
func TestLoaderResolvesChains(t *testing.T) {
	for _, tc := range []struct {
		name  string
		fsys  fstest.MapFS
		entry string
		want  string
	}{
		{
			name: "bare name anchors at the referencing file's directory",
			fsys: fstest.MapFS{
				"root.gql":   file(copyOf("Copied", "Source")),
				"Source.gql": file(body("Source")),
			},
			entry: "root.gql",
			want:  "Copied",
		},
		{
			name: "absolute anchors at the catalogue root, not at the referencing file",
			fsys: fstest.MapFS{
				"sub/root.gql":            file(copyOf("Copied", "/schemas/base/Source")),
				"schemas/base/Source.gql": file(body("Source")),
			},
			entry: "sub/root.gql",
			want:  "Copied",
		},
		{
			name: "CURRENT_SCHEMA and ./ are the referencing file's own directory",
			fsys: fstest.MapFS{
				"sub/root.gql": file(copyOf("Copied", "CURRENT_SCHEMA/gt")),
				"sub/gt.gql":   file(body("gt")),
			},
			entry: "sub/root.gql",
			want:  "Copied",
		},
		{
			name: "a chain of two ends at the inline body and keeps the root's name",
			fsys: fstest.MapFS{
				"root.gql":   file(copyOf("Root", "Middle")),
				"Middle.gql": file(copyOf("Middle", "Tail")),
				"Tail.gql":   file(body("Tail")),
			},
			entry: "root.gql",
			want:  "Root",
		},
		{
			// The design's sharpest witness (ADR 0034 §6). The same reference text
			// escapes when its file is the root and resolves when the file is a hop
			// one directory down — so a loader that anchored the climb at the root,
			// or that re-rooted at each file, fails one of these two rows.
			name: "a climb inside a chain pops from the referencing file's directory",
			fsys: fstest.MapFS{
				"root.gql":        file(copyOf("Copied", "/sub/climber")),
				"sub/climber.gql": file(copyOf("climber", "../s/base")),
				"s/base.gql":      file(body("base")),
			},
			entry: "root.gql",
			want:  "Copied",
		},
		{
			// Two pops, resolving. The single-pop row above holds for a climb count
			// hardcoded to 1 as much as for one counted off the source, and so does
			// every escaping row (a hardcoded 1 pops from "." and errors just the
			// same) — so nothing else here can tell the two apart. Measured: pinning
			// the count to 1 survives the whole package without this row.
			name: "a climb counts its pops, so ../../ lands two directories up",
			fsys: fstest.MapFS{
				"a/b/root.gql": file(copyOf("Copied", "../../s/base")),
				"s/base.gql":   file(body("base")),
			},
			entry: "a/b/root.gql",
			want:  "Copied",
		},
		{
			// Row A of the gqlc-pyc6 falsifier ledger, and the row that REDS under
			// any segment guard spelled `token == REGULAR_IDENTIFIER`. NODE is one
			// of the 47 nonReservedWords (GQL.g4:3061-3109), so under
			// caseInsensitive lexing it arrives as the NODE token rather than as a
			// REGULAR_IDENTIFIER — while still being a perfectly ordinary path
			// segment.
			name: "a segment spelled as a non-reserved keyword resolves",
			fsys: fstest.MapFS{
				"root.gql": file(copyOf("Copied", "Node")),
				"Node.gql": file(body("Node")),
			},
			entry: "root.gql",
			want:  "Copied",
		},
		{
			// Row B: GetText returns the source bytes even for a keyword-matched
			// token, so nothing canonicalises a segment's case. Were anything to,
			// this would dangle against Source.gql instead.
			name: "a segment's case is preserved, so SOURCE is not Source",
			fsys: fstest.MapFS{
				"root.gql":   file(copyOf("Copied", "SOURCE")),
				"SOURCE.gql": file(body("SOURCE")),
				"Source.gql": file(body("Source")),
			},
			entry: "root.gql",
			want:  "Copied",
		},
		{
			// The name-drift check at loader.go:70 compares the referenced file's
			// DECLARED name against the reference's last segment, and the two sides
			// are read by different helpers: segment() refuses a delimited path
			// element outright, so a reference is always regular, while the target's
			// declaration may be delimited. Before gqlc-tzu9r the declaration side
			// carried its delimiters into the comparison, so this file resolved to
			// ErrReferenceNameMismatch reporting that Source.gql declares "`Source`"
			// — a drift diagnostic against a file whose name had not drifted.
			name: "a target may declare its name delimited and still match the reference",
			fsys: fstest.MapFS{
				"root.gql":   file(copyOf("Copied", "Source")),
				"Source.gql": file("CREATE GRAPH TYPE `Source` { (a:A) }"),
			},
			entry: "root.gql",
			want:  "Copied",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := gql.NewLoader(tc.fsys).Load(tc.entry)
			require.NoError(t, err)
			require.Equal(t, tc.want, got.Name)
			require.NotEmpty(t, got.Nodes, "the tail's element types must reach the model")
		})
	}
}

// TestLoaderResolutionFailures covers the four ways a SUPPORTED spelling fails
// to resolve. Each asserts the sentinel rather than merely an error: two of
// these four are reachable from the same reference text, so a row pinned on
// "some error" would not tell an escape from a dangle.
func TestLoaderResolutionFailures(t *testing.T) {
	for _, tc := range []struct {
		name    string
		fsys    fstest.MapFS
		entry   string
		want    error
		message string
	}{
		{
			name:  "a climb from the root has nowhere to pop to",
			fsys:  fstest.MapFS{"root.gql": file(copyOf("Copied", "../s/gt"))},
			entry: "root.gql",
			want:  gql.ErrReferenceOutsideCatalogue,
		},
		{
			// The other half of the climber pair: identical reference text to the
			// resolving row above, red because this file is now the root.
			name:  "the climber escapes when it is loaded as the root",
			fsys:  fstest.MapFS{"climber.gql": file(copyOf("climber", "../s/base"))},
			entry: "climber.gql",
			want:  gql.ErrReferenceOutsideCatalogue,
		},
		{
			name:  "a reference to no file at all dangles",
			fsys:  fstest.MapFS{"root.gql": file(copyOf("Copied", "nowhere"))},
			entry: "root.gql",
			want:  gql.ErrDanglingReference,
			// The diagnostic points at the reference the user wrote, and names
			// the path it resolved to.
			message: "nowhere.gql",
		},
		{
			name:    "a self-copy is the one-element cycle",
			fsys:    fstest.MapFS{"self.gql": file(copyOf("self", "self"))},
			entry:   "self.gql",
			want:    gql.ErrReferenceCycle,
			message: "self.gql → self.gql",
		},
		{
			name: "a two-cycle names the whole chain in order",
			fsys: fstest.MapFS{
				"a.gql": file(copyOf("a", "b")),
				"b.gql": file(copyOf("b", "a")),
			},
			entry:   "a.gql",
			want:    gql.ErrReferenceCycle,
			message: "a.gql → b.gql → a.gql",
		},
		{
			name: "a file that does not declare the name the lookup used",
			fsys: fstest.MapFS{
				"root.gql": file(copyOf("Copied", "liar")),
				"liar.gql": file(body("NotLiar")),
			},
			entry:   "root.gql",
			want:    gql.ErrReferenceNameMismatch,
			message: `liar.gql declares "NotLiar"`,
		},
		{
			// The row above differs from its reference in more than case, and so does
			// every other pair this package owns, so a byte-exact comparison and a
			// case-folding one agree on all of them and neither is witnessed. This
			// pair differs ONLY in case, which is what separates them: the lookup is
			// case-sensitive and found Target.gql, and Target.gql declares a name that
			// merely folds to the one that found it. Replacing the comparison with
			// strings.EqualFold leaves the rest of the package green and reds this row
			// alone (bd gqlc-952q).
			name: "a file whose declaration differs from the lookup only in case",
			fsys: fstest.MapFS{
				"root.gql":   file(copyOf("Copied", "Target")),
				"Target.gql": file(body("target")),
			},
			entry:   "root.gql",
			want:    gql.ErrReferenceNameMismatch,
			message: `Target.gql declares "target"`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := gql.NewLoader(tc.fsys).Load(tc.entry)
			require.ErrorIs(t, err, tc.want)
			require.Empty(t, got.Name, "the model must be the zero value on error")
			if tc.message != "" {
				require.Contains(t, err.Error(), tc.message)
			}
		})
	}
}

// TestLoaderRootNameIsExempt pins the one asymmetry in the trailing-name rule: a
// referenced file promises that its declaration matches the name that found it,
// and the root promises nothing, being reached by a config path instead. Without
// the exemption every schema file in every project would have to be named after
// its graph type.
func TestLoaderRootNameIsExempt(t *testing.T) {
	fsys := fstest.MapFS{
		"schema.gql": file(copyOf("Whatever", "Source")),
		"Source.gql": file(body("Source")),
	}

	got, err := gql.NewLoader(fsys).Load("schema.gql")
	require.NoError(t, err)
	require.Equal(t, "Whatever", got.Name)
}

// TestDeclinedSpellingsAgreeAcrossParseAndLoad is the claim ADR 0034 §3.3 makes
// about where these four fire: they are judgments about the spelling, so they
// belong to the lowering and not to the catalogue. Asserting both entry points
// on one source is what makes that checkable — a decline moved into the Loader
// would leave Parse reporting ErrCopyOfSource here, and a caller branching on
// the reason would get a different answer depending on which door it came in.
//
// Every row is also given a catalogue that WOULD satisfy it if the spelling were
// supported, so a pass cannot be explained by the file being absent.
func TestDeclinedSpellingsAgreeAcrossParseAndLoad(t *testing.T) {
	for _, tc := range []struct {
		ref  string
		want error
		also fstest.MapFS
	}{
		{ref: "$$gt", want: gql.ErrReferenceParameter, also: fstest.MapFS{"gt.gql": file(body("gt"))}},
		{ref: "$$s/gt", want: gql.ErrReferenceParameter, also: fstest.MapFS{"s/gt.gql": file(body("gt"))}},
		{ref: "HOME_SCHEMA/gt", want: gql.ErrHomeSchemaReference, also: fstest.MapFS{"gt.gql": file(body("gt"))}},
		{ref: "s.gt", want: gql.ErrObjectParentReference, also: fstest.MapFS{"s/gt.gql": file(body("gt"))}},
		{ref: "/s/o.gt", want: gql.ErrObjectParentReference, also: fstest.MapFS{"s/o/gt.gql": file(body("gt"))}},
		{ref: `"gt"`, want: gql.ErrDelimitedReferenceSegment, also: fstest.MapFS{"gt.gql": file(body("gt"))}},
		{ref: "`gt`", want: gql.ErrDelimitedReferenceSegment, also: fstest.MapFS{"gt.gql": file(body("gt"))}},
		// A directory segment, not the trailing name: simpleDirectoryPath is
		// reachable only behind a SOLIDUS or a DOUBLE_PERIOD (GQL.g4:1407-1417),
		// so a bare `dir/gt` is a syntax error and cannot witness this — the
		// leading solidus is what puts a delimited identifier in a directory
		// position at all.
		{ref: `/"dir"/gt`, want: gql.ErrDelimitedReferenceSegment, also: fstest.MapFS{"dir/gt.gql": file(body("gt"))}},
	} {
		t.Run(tc.ref, func(t *testing.T) {
			src := copyOf("Copied", tc.ref)

			_, parseErr := gql.New().Parse(strings.NewReader(src))
			require.ErrorIs(t, parseErr, tc.want, "Parse")

			fsys := fstest.MapFS{"root.gql": file(src)}
			for name, f := range tc.also {
				fsys[name] = f
			}
			_, loadErr := gql.NewLoader(fsys).Load("root.gql")
			require.ErrorIs(t, loadErr, tc.want, "Load")

			// All four wrap the class, so a caller asking only whether the source
			// was rejected keeps matching (ADR 0016's pattern).
			require.ErrorIs(t, parseErr, gql.ErrUnsupportedSource)
		})
	}
}

// TestParseRefusesEveryReachableReference pins the narrowed contract of
// ErrCopyOfSource: with no catalogue behind it, Parse refuses a lowered
// reference whatever it points at. Load is the only door that resolves one.
func TestParseRefusesEveryReachableReference(t *testing.T) {
	for _, ref := range []string{"Source", "/gt", "/a/b/gt", "./gt", "CURRENT_SCHEMA/gt", "../s/gt", "../../s/gt"} {
		t.Run(ref, func(t *testing.T) {
			_, err := gql.New().Parse(strings.NewReader(copyOf("Copied", ref)))
			require.ErrorIs(t, err, gql.ErrCopyOfSource)
		})
	}
}

// TestLoaderReportsTheReferencedFilesOwnErrors: a referenced file is parsed
// exactly like any schema file, so a defect inside it surfaces as that defect
// rather than as a resolution failure. Without this a broken hop would be
// indistinguishable from a dangling one to a caller branching on sentinels.
func TestLoaderReportsTheReferencedFilesOwnErrors(t *testing.T) {
	fsys := fstest.MapFS{
		"root.gql":   file(copyOf("Copied", "Source")),
		"Source.gql": file("CREATE GRAPH TYPE Source LIKE g"),
	}

	_, err := gql.NewLoader(fsys).Load("root.gql")
	require.ErrorIs(t, err, gql.ErrLikeGraphSource)
}
