package gql

import (
	"errors"
	"fmt"
	"io/fs"
	"path"
	"slices"
	"strings"

	"github.com/areqag/gqlc/internal/schema"
)

// Loader reads a schema file and the graph type references it reaches, against
// a catalogue that is simply the filesystem rooted at fsys (ADR 0034). A graph
// type's catalogue identity is its file's path: the trailing segment of a
// reference names both the file — segment + ".gql" — and the graph type
// declared inside it. There is no name table and no in-file scoping, so a
// reference resolves to a file or it fails.
type Loader struct{ fsys fs.FS }

// NewLoader returns a Loader whose catalogue root is fsys. For a generation
// target that is the directory holding the configured schema file, which is why
// supporting COPY OF needed no config change at all.
func NewLoader(fsys fs.FS) *Loader { return &Loader{fsys: fsys} }

// Load reads the file at name — a root-relative slash path — and follows its
// COPY OF chain to the declaration that carries an inline body.
//
// A graph type has exactly one source and a COPY OF source holds exactly one
// reference, so chains are linear: there are no diamonds, and a cycle is the
// only way a chain fails to terminate. Hence an ordered visited list rather than
// a graph walk, and no depth limit — the chain is bounded by the files on disk.
func (l *Loader) Load(name string) (schema.Schema, error) {
	raw, err := l.parseFile(name)
	if err != nil {
		return schema.Schema{}, err
	}

	// The root file's declared name is what the resolved model carries: COPY OF
	// means the same element types under a new name. It is also the one name
	// exempt from the trailing-name rule below, being reached by the config path
	// rather than by a reference.
	declared := raw.name
	visited := []string{name}

	for raw.copyRef != nil {
		from := visited[len(visited)-1]
		ref := *raw.copyRef

		target, err := ref.target(path.Dir(from))
		if err != nil {
			return schema.Schema{}, fmt.Errorf("%s: COPY OF %s: %w", from, ref.text, err)
		}
		// Checked before the file is reopened, so a cycle is reported as a cycle
		// rather than as whatever a second read happens to do.
		if slices.Contains(visited, target) {
			return schema.Schema{}, fmt.Errorf("%w: %s", ErrReferenceCycle, strings.Join(append(visited, target), " → "))
		}

		next, err := l.parseFile(target)
		if errors.Is(err, fs.ErrNotExist) {
			return schema.Schema{}, fmt.Errorf("%s: COPY OF %s: %w: %s", from, ref.text, ErrDanglingReference, target)
		}
		if err != nil {
			return schema.Schema{}, err
		}
		// The lookup found the file by name; this catches the drift where a file
		// is renamed and the declaration inside it is not.
		if next.name != ref.name() {
			return schema.Schema{}, fmt.Errorf("%s: COPY OF %s: %w: %s declares %q", from, ref.text, ErrReferenceNameMismatch, target, next.name)
		}

		visited = append(visited, target)
		raw = next
	}

	resolved, err := raw.resolve()
	if err != nil {
		return schema.Schema{}, err
	}
	resolved.Name = declared
	return resolved, nil
}

// parseFile reads one file out of the catalogue and walks it. Errors from the
// file itself are returned unwrapped: a referenced file is parsed exactly like
// any schema file, so its sentinels are the ordinary ones and a caller matching
// on them keeps working.
func (l *Loader) parseFile(name string) (rawSchema, error) {
	f, err := l.fsys.Open(name)
	if err != nil {
		return rawSchema{}, err
	}
	defer f.Close() //nolint:errcheck // read-only.

	return parse(f)
}
