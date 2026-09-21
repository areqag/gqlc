package main

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func parse(t *testing.T, src string) []*ast.File {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), "models.go", "package p\n"+src, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("the row's own source does not parse: %v", err)
	}
	return []*ast.File{f}
}

// TestAnEntityDecoderIsReadByShape holds the exemption to entity decoders. The
// rows that must NOT root are the ones the gate exists for: a record's and a
// union's decoder carry the `decode` prefix too, and are emitted per use.
func TestAnEntityDecoderIsReadByShape(t *testing.T) {
	const person = "type Person struct{ Name string }\n"
	for _, row := range []struct {
		name, src string
		want      []string
	}{
		{"neo4j node", person + "func decodePerson(node dbtype.Node) (Person, error) { return Person{}, nil }", []string{"decodePerson"}},
		{"age vertex", person + "func decodePerson(raw []byte) (Person, error) { return Person{}, nil }", []string{"decodePerson"}},
		{"an entity with no properties", "type Knows struct{}\nfunc decodeKnows(rel dbtype.Relationship) (Knows, error) { return Knows{}, nil }", []string{"decodeKnows"}},
		{"sorted", person + "type Blob struct{}\nfunc decodePerson(raw []byte) (Person, error) { return Person{}, nil }\nfunc decodeBlob(raw []byte) (Blob, error) { return Blob{}, nil }", []string{"decodeBlob", "decodePerson"}},

		{"a record decoder answers an unexported alias", "type record4329c440 = struct{ Zip int32 }\nfunc decodeRecord4329c440(raw []byte) (record4329c440, error) { return record4329c440{}, nil }", nil},
		{"a record decoder beside an entity", person + "type record4329c440 = struct{}\nfunc decodeRecord4329c440(v map[string]any) (record4329c440, error) { return record4329c440{}, nil }", nil},
		{"a union decoder answers any", person + "func decodeUnionbd73dd3d(raw []byte) (any, error) { return nil, nil }", nil},
		{"the struct is defined elsewhere or nowhere", "func decodePerson(raw []byte) (Person, error) { return Person{}, nil }", nil},
		{"an exported alias is not a defined struct", "type Person = struct{}\nfunc decodePerson(raw []byte) (Person, error) { return Person{}, nil }", nil},
		{"a defined non-struct", "type Person string\nfunc decodePerson(raw []byte) (Person, error) { return \"\", nil }", nil},
		{"an unexported struct", "type person struct{}\nfunc decodeperson(raw []byte) (person, error) { return person{}, nil }", nil},
		{"answers a different struct", person + "type Post struct{}\nfunc decodePerson(raw []byte) (Post, error) { return Post{}, nil }", nil},
		{"answers a pointer", person + "func decodePerson(raw []byte) (*Person, error) { return nil, nil }", nil},
		{"answers no error", person + "func decodePerson(raw []byte) Person { return Person{} }", nil},
		{"two parameters", person + "func decodePerson(raw []byte, strict bool) (Person, error) { return Person{}, nil }", nil},
		{"a method", person + "type q struct{}\nfunc (q) decodePerson(raw []byte) (Person, error) { return Person{}, nil }", nil},
		{"generic", person + "func decodePerson[T any](raw T) (Person, error) { return Person{}, nil }", nil},
		{"another verb", person + "func encodePerson(raw []byte) (Person, error) { return Person{}, nil }", nil},
	} {
		t.Run(row.name, func(t *testing.T) {
			got := entityDecoders(parse(t, row.src))
			if strings.Join(got, ",") != strings.Join(row.want, ",") {
				t.Fatalf("rooted %v, want %v", got, row.want)
			}
		})
	}
}

// goldens is the module `just check-goldens-unused` copies and roots.
const goldens = "../../../test/data/codegen/valid"

// perUseDecoder is a record's or a union's decoder as both backends name it:
// the family and the 8-hex digest of the encoding.
var perUseDecoder = regexp.MustCompile(`^decode(Record|Union)[0-9a-f]{8}$`)

// perUseFamily answers which per-use family a declaration belongs to, "" for
// neither. The name does not settle it: a schema may call an entity
// Record<8 hex>, and its decoder is then spelled as a record's is. The tool
// tells the two apart by what they answer, so this does too — a record's
// decoder answers an unexported alias and a union's answers any, where an
// entity's answers an exported name.
func perUseFamily(fn *ast.FuncDecl) string {
	m := perUseDecoder.FindStringSubmatch(fn.Name.Name)
	if m == nil || fn.Recv != nil || fn.Type.Results.NumFields() == 0 {
		return ""
	}
	if id, ok := fn.Type.Results.List[0].Type.(*ast.Ident); ok && id.IsExported() {
		return ""
	}
	return m[1]
}

// perUseDecoders maps each per-use decoder the files declare to its family.
func perUseDecoders(files []*ast.File) map[string]string {
	out := map[string]string{}
	for _, f := range files {
		for _, d := range f.Decls {
			if fn, ok := d.(*ast.FuncDecl); ok {
				if family := perUseFamily(fn); family != "" {
					out[fn.Name.Name] = family
				}
			}
		}
	}
	return out
}

// TestAPerUseDecoderIsReadByWhatItAnswers holds the test below to the tool's
// own question. Its first row is an entity whose name is a record decoder's
// spelling: the tool roots it, rightly, and a reading by name alone called that
// a per-use decoder rooted.
func TestAPerUseDecoderIsReadByWhatItAnswers(t *testing.T) {
	for _, row := range []struct {
		name, src string
		want      map[string]string
	}{
		{
			"an entity named as a record decoder is spelled",
			"type Recordaaaaaaaa struct{}\nfunc decodeRecordaaaaaaaa(raw []byte) (Recordaaaaaaaa, error) { return Recordaaaaaaaa{}, nil }",
			map[string]string{},
		},
		{
			"a record decoder",
			"type record4329c440 = struct{}\nfunc decodeRecord4329c440(raw []byte) (record4329c440, error) { return record4329c440{}, nil }",
			map[string]string{"decodeRecord4329c440": "Record"},
		},
		{
			"a union decoder",
			"func decodeUnionbd73dd3d(v any) (any, error) { return v, nil }",
			map[string]string{"decodeUnionbd73dd3d": "Union"},
		},
		{
			"an entity decoder",
			"type Person struct{}\nfunc decodePerson(raw []byte) (Person, error) { return Person{}, nil }",
			map[string]string{},
		},
	} {
		t.Run(row.name, func(t *testing.T) {
			if got := perUseDecoders(parse(t, row.src)); !maps.Equal(got, row.want) {
				t.Fatalf("read %v as per-use, want %v", got, row.want)
			}
		})
	}
}

// TestNoPerUseDecoderInTheGoldensIsRooted is the same claim over what the
// emitters really write, which the rows above only imitate. It is refused as
// vacuous unless the goldens hold the families it names.
func TestNoPerUseDecoderInTheGoldensIsRooted(t *testing.T) {
	seen := map[string]int{}
	rooted := 0
	err := filepath.WalkDir(goldens, func(path string, d fs.DirEntry, err error) error {
		if err != nil || !d.IsDir() {
			return err
		}
		_, roots, _, err := entityDecodersIn(path)
		if err != nil {
			return err
		}
		rooted += len(roots)
		files, err := parseDir(path)
		if err != nil {
			return err
		}
		perUse := perUseDecoders(files)
		for _, r := range roots {
			if perUse[r] != "" {
				t.Errorf("%s: %s is a per-use decoder and was rooted, so an uncalled one is no longer reported", path, r)
			}
		}
		driver := "neo4j"
		if strings.Contains(filepath.Base(path), "age") {
			driver = "age"
		}
		for _, family := range perUse {
			seen[driver+" "+family]++
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if rooted == 0 {
		t.Fatalf("no entity decoder was read out of %s, so the loop above asserted nothing", goldens)
	}
	// No neo4j golden declares a record decoder today (no fixture with a
	// record targets that driver), so that family rests on the rows above.
	for _, family := range []string{"neo4j Union", "age Record", "age Union"} {
		if seen[family] == 0 {
			t.Errorf("the goldens declare no %s decoder, so this test did not see that family go unrooted", family)
		}
	}
}

func TestRunWritesOneSinkPerPackageAndGradesItsCount(t *testing.T) {
	root := t.TempDir()
	write := func(rel, src string) {
		t.Helper()
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("a/models.go", "package alpha\n\ntype Person struct{}\n\ntype Post struct{}\n\n"+
		"func decodePerson(raw []byte) (Person, error) { return Person{}, nil }\n\n"+
		"func decodePost(raw []byte) (Post, error) { return Post{}, nil }\n")
	write("a/queries.go", "package alpha\n\nfunc Get(raw []byte) (Person, error) { return decodePerson(raw) }\n")
	write("b/models.go", "package beta\n\nfunc decodeUnionbd73dd3d(raw []byte) (any, error) { return raw, nil }\n")

	var out bytes.Buffer
	if err := run([]string{root}, &out); err != nil {
		t.Fatal(err)
	}
	const line = "rooted 2 entity decoders in 1 packages, 1 of them named by nothing else in their package (bd gqlc-m1dk)\n"
	if out.String() != line {
		t.Errorf("printed %q, want %q", out.String(), line)
	}
	sink, err := os.ReadFile(filepath.Join(root, "a", sinkFile))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"package alpha\n", "\t_ = decodePerson\n", "\t_ = decodePost\n"} {
		if !strings.Contains(string(sink), want) {
			t.Errorf("the sink lacks %q:\n%s", want, sink)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "b", sinkFile)); err == nil {
		t.Error("a package with no entity decoder was given a sink")
	}

	if err := run([]string{root}, &out); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Errorf("a second run over the same directory returned %v, want a refusal naming the existing sink", err)
	}
	if err := run([]string{filepath.Join(root, "b")}, &out); err == nil || !strings.Contains(err.Error(), "nothing was rooted") {
		t.Errorf("a run that rooted nothing returned %v, want a refusal", err)
	}
}
