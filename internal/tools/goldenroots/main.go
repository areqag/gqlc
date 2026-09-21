// Command goldenroots writes, into a SCRATCH COPY of a module of emitted
// packages, one file per package that names each of that package's entity
// decoders — `var _ = decodePerson` — so that a whole-program `unused` run over
// the copy treats them as roots (bd gqlc-ukzq).
//
// It exists because both backends emit an unexported decode<Entity> for every
// entity the schema declares, whether or not a query in the batch reads it (bd
// gqlc-m1dk, an open design question). Over the goldens that is most of what
// `unused` reports, and most of THAT is transitive: helpers only a dead decoder
// calls. Excluding findings by name cannot express it — the transitive ones
// carry ordinary helper names, toDate and agtypeInt64 among them, which are the
// names the gate exists to hold. Rooting the decoder can: what it calls is then
// used, and what nothing calls is still reported.
//
// WHAT IS ROOTED is decided by shape, not by the `decode` prefix. A record's and
// a union's decoder carry it too — decodeRecord<digest>, decodeUnion<digest> —
// and those are emitted per use, so an uncalled one is exactly the finding the
// gate is for. An entity decoder is the func
//
//	func decode<T>(one parameter) (<T>, error)
//
// with no receiver and no type parameters, where <T> is an EXPORTED STRUCT TYPE
// DEFINED in the same package. A record decoder answers an unexported alias and
// a union decoder answers `any`, so neither has such a <T>.
//
// Every number it prints is graded first: a run that rooted nothing is an error
// rather than a line reading zero, because the gate downstream of a sink that
// found nothing reports several hundred findings nobody can read, and a gate
// downstream of one that was never run reports the same.
//
// Usage:
//
//	goldenroots DIR    # DIR is the scratch copy; it is written into
package main

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// sinkFile is the file written into each package that has an entity decoder. A
// package already holding the name is refused rather than overwritten: either
// the emitter has started emitting it, or DIR is not a fresh copy.
const sinkFile = "zz_gqlc_entity_decoder_roots.go"

const usage = "usage: goldenroots DIR"

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "goldenroots: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string, out io.Writer) error {
	if len(args) != 1 {
		return errors.New(usage)
	}
	tally, err := rootModule(args[0])
	if err != nil {
		return err
	}
	if tally.decoders == 0 {
		return fmt.Errorf("no entity decoder found under %s, so nothing was rooted; either the walk "+
			"read no emitted package or the emitters stopped spelling decode<Entity> the way "+
			"entityDecoders reads it", args[0])
	}
	_, err = fmt.Fprintf(out, "rooted %d entity decoders in %d packages, %d of them named by nothing else in "+
		"their package (bd gqlc-m1dk)\n", tally.decoders, tally.packages, tally.uncalled)
	return err
}

// tally is what one run rooted. uncalled counts the decoders whose name appears
// nowhere in their package but at their own declaration — the ones `unused`
// would report, and so the size of what this exemption is hiding.
type tally struct {
	decoders, packages, uncalled int
}

// rootModule writes a sink into every directory under root that holds an entity
// decoder.
func rootModule(root string) (tally, error) {
	var t tally
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || !d.IsDir() {
			return err
		}
		pkg, roots, uncalled, err := entityDecodersIn(path)
		if err != nil || len(roots) == 0 {
			return err
		}
		if err := writeSink(filepath.Join(path, sinkFile), pkg, roots); err != nil {
			return err
		}
		t.decoders += len(roots)
		t.uncalled += uncalled
		t.packages++
		return nil
	})
	return t, err
}

// entityDecodersIn parses one directory's non-test Go files and returns its
// package name, its entity decoders in sorted order, and how many of them
// nothing else in the directory names.
func entityDecodersIn(dir string) (pkg string, roots []string, uncalled int, err error) {
	files, err := parseDir(dir)
	if err != nil || len(files) == 0 {
		return "", nil, 0, err
	}

	roots = entityDecoders(files)
	named := map[string]int{}
	for _, f := range files {
		ast.Inspect(f, func(n ast.Node) bool {
			if id, ok := n.(*ast.Ident); ok {
				named[id.Name]++
			}
			return true
		})
	}
	for _, r := range roots {
		// One mention is the declaration's own name.
		if named[r] == 1 {
			uncalled++
		}
	}
	return files[0].Name.Name, roots, uncalled, nil
}

func parseDir(dir string) ([]*ast.File, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	fset := token.NewFileSet()
	var files []*ast.File
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		if name == sinkFile {
			return nil, fmt.Errorf("%s already exists; this tool writes into a fresh scratch copy only",
				filepath.Join(dir, name))
		}
		f, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, parser.SkipObjectResolution)
		if err != nil {
			return nil, err
		}
		files = append(files, f)
	}
	return files, nil
}

// entityDecoders returns the entity decoders declared across one package's
// files: see the package comment for the shape, and for why it is not a prefix.
func entityDecoders(files []*ast.File) []string {
	structs := map[string]bool{}
	for _, f := range files {
		for _, d := range f.Decls {
			if gen, ok := d.(*ast.GenDecl); ok {
				definedStructs(gen, structs)
			}
		}
	}

	var out []string
	for _, f := range files {
		for _, d := range f.Decls {
			if fn, ok := d.(*ast.FuncDecl); ok && isEntityDecoder(fn, structs) {
				out = append(out, fn.Name.Name)
			}
		}
	}
	sort.Strings(out)
	return out
}

// definedStructs adds the exported struct types one declaration DEFINES. An
// alias is left out: a record's carrier is one.
func definedStructs(gen *ast.GenDecl, into map[string]bool) {
	for _, spec := range gen.Specs {
		ts, ok := spec.(*ast.TypeSpec)
		if !ok || ts.Assign.IsValid() || ts.TypeParams != nil || !ts.Name.IsExported() {
			continue
		}
		if _, ok := ts.Type.(*ast.StructType); ok {
			into[ts.Name.Name] = true
		}
	}
}

func isEntityDecoder(fn *ast.FuncDecl, structs map[string]bool) bool {
	if fn.Recv != nil || fn.Type.TypeParams != nil {
		return false
	}
	entity, ok := strings.CutPrefix(fn.Name.Name, "decode")
	if !ok || !structs[entity] {
		return false
	}
	return fn.Type.Params.NumFields() == 1 && answers(fn.Type.Results, entity, "error")
}

// answers reports whether a result list is exactly the named types, each
// written as a bare identifier.
func answers(results *ast.FieldList, want ...string) bool {
	if results == nil || len(results.List) != len(want) {
		return false
	}
	for i, field := range results.List {
		id, ok := field.Type.(*ast.Ident)
		if !ok || len(field.Names) > 1 || id.Name != want[i] {
			return false
		}
	}
	return true
}

func writeSink(path, pkg string, roots []string) error {
	var b strings.Builder
	fmt.Fprintf(&b, "// Written by internal/tools/goldenroots into a scratch copy. Not emitted by gqlc.\n\npackage %s\n\nvar (\n", pkg)
	for _, r := range roots {
		fmt.Fprintf(&b, "\t_ = %s\n", r)
	}
	b.WriteString(")\n")
	return os.WriteFile(path, []byte(b.String()), 0o600)
}
