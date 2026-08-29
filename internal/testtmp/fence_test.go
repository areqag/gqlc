package testtmp_test

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const selfPath = "github.com/areqag/gqlc/internal/testtmp"

// Package enumeration and file discovery go through `go list` rather than a
// directory walk: a walk would record `open <dir>` for every directory in
// the repo — including the module root, whose entry list covers .git's
// ever-moving stat — and those records would defeat the very replay this
// package exists to protect. A child process's reads are not in the test
// log; only the parsed files' own opens are, and tracked files are
// mtime-stamped on CI.
func TestEveryTempUsingPackageRoutesTMPDIRThroughThisPackage(t *testing.T) {
	root, err := exec.Command("go", "list", "-m", "-f", "{{.Dir}}").Output()
	if err != nil {
		t.Fatalf("go list -m: %v", err)
	}
	cmd := exec.Command("go", "list", "-json=ImportPath,Dir,TestGoFiles,XTestGoFiles", "./...")
	cmd.Dir = strings.TrimSpace(string(root))
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list ./... from module root: %v", err)
	}

	type pkg struct {
		ImportPath   string
		Dir          string
		TestGoFiles  []string
		XTestGoFiles []string
	}
	dec := json.NewDecoder(strings.NewReader(string(out)))
	var violations []string
	for dec.More() {
		var p pkg
		if err := dec.Decode(&p); err != nil {
			t.Fatalf("decode go list output: %v", err)
		}
		if p.ImportPath == selfPath {
			continue
		}
		files := append(append([]string{}, p.TestGoFiles...), p.XTestGoFiles...)
		if len(files) == 0 {
			continue
		}
		fset := token.NewFileSet()
		var tempUse string
		wired := false
		for _, name := range files {
			f, err := parser.ParseFile(fset, filepath.Join(p.Dir, name), nil, 0)
			if err != nil {
				t.Fatalf("parse %s/%s: %v", p.Dir, name, err)
			}
			if use := findTempUse(fset, f); use != "" && tempUse == "" {
				tempUse = use
			}
			if testMainCallsMain(f) {
				wired = true
			}
		}
		if tempUse != "" && !wired {
			violations = append(violations,
				fmt.Sprintf("%s: %s uses a temp API but the package has no TestMain routed through %s", p.ImportPath, tempUse, selfPath))
		}
	}
	for _, v := range violations {
		t.Errorf("%s", v)
	}
	if len(violations) > 0 {
		t.Errorf("without the TMPDIR redirect these packages' test results never replay from cache on CI; add:\n\n\tfunc TestMain(m *testing.M) { testtmp.Main(m) }")
	}
}

// findTempUse reports the first call that makes the test binary's log record
// a path under the shared temp base: any X.TempDir() (testing.T, testing.B,
// suite helpers), or os.MkdirTemp / os.CreateTemp, whose leaked artifacts
// have the same effect. Returns "file:line: expr" or "".
func findTempUse(fset *token.FileSet, f *ast.File) string {
	var found string
	ast.Inspect(f, func(n ast.Node) bool {
		if found != "" {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		switch sel.Sel.Name {
		case "TempDir":
			// os.TempDir() only reads the env; the recorded open comes
			// from t.TempDir-style helpers, so skip the os package form.
			if id, ok := sel.X.(*ast.Ident); ok && id.Name == "os" {
				return true
			}
		case "MkdirTemp", "CreateTemp":
			id, ok := sel.X.(*ast.Ident)
			if !ok || id.Name != "os" {
				return true
			}
		default:
			return true
		}
		pos := fset.Position(call.Pos())
		found = fmt.Sprintf("%s:%d: %s.%s", filepath.Base(pos.Filename), pos.Line, exprString(sel.X), sel.Sel.Name)
		return false
	})
	return found
}

func exprString(e ast.Expr) string {
	switch v := e.(type) {
	case *ast.Ident:
		return v.Name
	case *ast.CallExpr:
		if sel, ok := v.Fun.(*ast.SelectorExpr); ok {
			return exprString(sel.X) + "." + sel.Sel.Name + "()"
		}
	case *ast.SelectorExpr:
		return exprString(v.X) + "." + v.Sel.Name
	}
	return "?"
}

// testMainCallsMain reports whether f declares TestMain with a call to
// <pkg>.Main where <pkg> is imported from selfPath.
func testMainCallsMain(f *ast.File) bool {
	local := ""
	for _, imp := range f.Imports {
		path := strings.Trim(imp.Path.Value, `"`)
		if path != selfPath {
			continue
		}
		if imp.Name != nil {
			local = imp.Name.Name
		} else {
			local = "testtmp"
		}
	}
	if local == "" {
		return false
	}
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv != nil || fn.Name.Name != "TestMain" {
			continue
		}
		calls := false
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			if sel, ok := n.(*ast.SelectorExpr); ok {
				if id, ok := sel.X.(*ast.Ident); ok && id.Name == local && sel.Sel.Name == "Main" {
					calls = true
				}
			}
			return true
		})
		if calls {
			return true
		}
	}
	return false
}
