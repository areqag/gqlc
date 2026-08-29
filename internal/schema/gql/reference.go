package gql

import (
	"path"

	"github.com/areqag/gqlc/internal/grammar/gql/gen"
)

// anchor is where a lowered graph type reference starts counting from. ISO's
// catalogue path grammar has three shapes that survive the lowering (ADR 0034
// §3.2); the rest are declined by name in lowerSchemaReference.
type anchor int

const (
	// anchorAbsolute: /gt, /schemas/base/Source — from the catalogue root.
	anchorAbsolute anchor = iota + 1
	// anchorCurrent: bare Source, ./gt, CURRENT_SCHEMA/gt — three spellings for
	// the directory of the file the reference is written in.
	anchorCurrent
	// anchorClimb: ../s/gt — climb directories from that same directory first.
	anchorClimb
)

// reference is a graph type reference reduced to the only three things
// resolution needs. It is produced during the walk, so `Parse` builds one too
// and then refuses it (ErrCopyOfSource) — the difference between Parse and Load
// is the catalogue, not the reading.
type reference struct {
	anchor anchor
	// climb is the number of directories to pop, and is non-zero only under
	// anchorClimb.
	climb int
	// segs is the directory segments followed by the graph type name, so it is
	// never empty and its last element is the name the referenced file must
	// declare.
	segs []string
	// text is the reference as the author wrote it, for diagnostics that point
	// at the source line rather than at the file that failed to exist.
	text string
}

// name is the trailing segment: the graph type the reference names, and the
// name the file it resolves to must declare (ADR 0034 §3.4).
func (r reference) name() string { return r.segs[len(r.segs)-1] }

// target resolves the reference against dir — the directory, relative to the
// catalogue root, of the file the reference was written in — and returns the
// root-relative path of the referenced file, extension included.
//
// The climb guard is the whole of ADR 0034 §3.2's hermeticity rule: a pop from
// the root has nowhere to go, and everything a generated package depends on
// lives under the schema tree. It is lexical on purpose — a correctness rule,
// not a sandbox.
func (r reference) target(dir string) (string, error) {
	base := dir
	if r.anchor == anchorAbsolute {
		base = "."
	}
	for range r.climb {
		if base == "." {
			return "", ErrReferenceOutsideCatalogue
		}
		base = path.Dir(base)
	}
	return path.Join(append([]string{base}, r.segs...)...) + ".gql", nil
}

// lowerCopyOf reduces a COPY OF source to a reference, or names why the
// spelling is declined. Every rejection here is a judgment about the spelling
// and not about any catalogue, which is why Parse and Load report them
// identically (ADR 0034 §3.3).
func lowerCopyOf(c gen.ICopyOfGraphTypeContext) (reference, error) {
	ref := c.GraphTypeReference()
	if ref.ReferenceParameterSpecification() != nil {
		return reference{}, ErrReferenceParameter
	}

	parentAndName := ref.CatalogGraphTypeParentAndName()
	name, err := segment(parentAndName.GraphTypeName().Identifier())
	if err != nil {
		return reference{}, err
	}

	parent := parentAndName.CatalogObjectParentReference()
	if parent == nil {
		return reference{anchor: anchorCurrent, segs: []string{name}, text: ref.GetText()}, nil
	}
	// Both alternatives of catalogObjectParentReference can carry an
	// (objectName PERIOD) chain — `s.gt` is the whole parent in alternative 2,
	// and `/s/o.gt` appends one to a schema reference in alternative 1 — so
	// asking for the names covers each without branching on the alternative.
	if len(parent.AllObjectName()) > 0 {
		return reference{}, ErrObjectParentReference
	}

	lowered, err := lowerSchemaReference(parent.SchemaReference())
	if err != nil {
		return reference{}, err
	}
	lowered.segs = append(lowered.segs, name)
	lowered.text = ref.GetText()
	return lowered, nil
}

// lowerSchemaReference lowers the schema half of a reference: everything before
// the graph type name. The returned reference carries no name yet — its caller
// appends one.
func lowerSchemaReference(sr gen.ISchemaReferenceContext) (reference, error) {
	switch {
	case sr.ReferenceParameterSpecification() != nil:
		return reference{}, ErrReferenceParameter

	case sr.AbsoluteCatalogSchemaReference() != nil:
		abs := sr.AbsoluteCatalogSchemaReference()
		// The bare-SOLIDUS alternative is the catalogue root itself, and takes
		// no directory path and no schema name.
		if abs.AbsoluteDirectoryPath() == nil {
			return reference{anchor: anchorAbsolute}, nil
		}
		segs, err := directorySegments(abs.AbsoluteDirectoryPath().SimpleDirectoryPath(), abs.SchemaName())
		if err != nil {
			return reference{}, err
		}
		return reference{anchor: anchorAbsolute, segs: segs}, nil

	case sr.RelativeCatalogSchemaReference() != nil:
		rel := sr.RelativeCatalogSchemaReference()
		if predefined := rel.PredefinedSchemaReference(); predefined != nil {
			if predefined.HOME_SCHEMA() != nil {
				return reference{}, ErrHomeSchemaReference
			}
			// CURRENT_SCHEMA and the bare PERIOD are the same referent.
			return reference{anchor: anchorCurrent}, nil
		}
		relDir := rel.RelativeDirectoryPath()
		segs, err := directorySegments(relDir.SimpleDirectoryPath(), rel.SchemaName())
		if err != nil {
			return reference{}, err
		}
		return reference{anchor: anchorClimb, climb: len(relDir.AllDOUBLE_PERIOD()), segs: segs}, nil
	}

	// An alternative added to schemaReference after this was written has no
	// justification of its own to name yet, so it reports the bare class —
	// preserving ADR 0016's property that a new alternative is rejected rather
	// than silently dropped.
	return reference{}, ErrUnsupportedSource
}

// directorySegments reads a directory path and the schema name that follows it
// into path segments. dirs is nil for the paths that name no directory at all
// (`/gt` after its SOLIDUS, `../s/gt` after its climb).
func directorySegments(dirs gen.ISimpleDirectoryPathContext, schema gen.ISchemaNameContext) ([]string, error) {
	var names []gen.IIdentifierContext
	if dirs != nil {
		for _, dir := range dirs.AllDirectoryName() {
			names = append(names, dir.Identifier())
		}
	}
	names = append(names, schema.Identifier())

	segs := make([]string, 0, len(names))
	for _, id := range names {
		seg, err := segment(id)
		if err != nil {
			return nil, err
		}
		segs = append(segs, seg)
	}
	return segs, nil
}

// segment reads one name as a path segment, refusing the delimited spellings.
//
// The test is for the two delimited terminals rather than for the accepting
// token class, and that is load-bearing: `identifier` admits `regularIdentifier`
// (GQL.g4:2963-2966), whose second arm is the 47 keyword tokens of
// `nonReservedWords`. Under caseInsensitive lexing `Source` arrives as the
// SOURCE token, so a guard spelled `token == REGULAR_IDENTIFIER` would refuse
// ADR 0034's own resolving fixtures. Neither non-delimited class can carry a
// solidus or a full stop or be empty, so refusing only the delimited ones keeps
// "every segment is one safe path element" a property of the lexer (ADR 0034
// §3.3, amended under gqlc-pyc6).
//
// GetText returns the source bytes, so nothing here canonicalises case:
// `COPY OF SOURCE` looks up SOURCE.gql.
func segment(id gen.IIdentifierContext) (string, error) {
	if id.DOUBLE_QUOTED_CHARACTER_SEQUENCE() != nil || id.ACCENT_QUOTED_CHARACTER_SEQUENCE() != nil {
		return "", ErrDelimitedReferenceSegment
	}
	return id.GetText(), nil
}
