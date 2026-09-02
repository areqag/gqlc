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
	root, err := exec.CommandContext(t.Context(), "go", "list", "-m", "-f", "{{.Dir}}").Output()
	if err != nil {
		t.Fatalf("go list -m: %v", err)
	}
	cmd := exec.CommandContext(t.Context(), "go", "list",
		"-json=ImportPath,Dir,GoFiles,TestGoFiles,XTestGoFiles,TestImports,XTestImports", "./...")
	cmd.Dir = strings.TrimSpace(string(root))
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list ./... from module root: %v", err)
	}

	type pkg struct {
		ImportPath   string   `json:"ImportPath"`
		Dir          string   `json:"Dir"`
		GoFiles      []string `json:"GoFiles"`
		TestGoFiles  []string `json:"TestGoFiles"`
		XTestGoFiles []string `json:"XTestGoFiles"`
		TestImports  []string `json:"TestImports"`
		XTestImports []string `json:"XTestImports"`
	}
	dec := json.NewDecoder(strings.NewReader(string(out)))
	var pkgs []pkg
	for dec.More() {
		var p pkg
		if err := dec.Decode(&p); err != nil {
			t.Fatalf("decode go list output: %v", err)
		}
		pkgs = append(pkgs, p)
	}

	// A temp acquisition behind an imported helper names no temp API at the
	// call site, and the helper's own file is a non-test file, so neither
	// side is reached by the per-package walk below. Measured on
	// 3e8eeebd: a package calling `probehelp.TempRepo(t)` and nothing else
	// passed the fence while minting a tempdir per test, which is the state
	// the fence exists to prevent (bd gqlc-nvmz).
	//
	// So the non-test files are read first, for exported functions that
	// acquire temp, and a test file importing such a package counts as
	// using temp itself. The signature is deliberately not consulted. A
	// helper taking *testing.T is the expected shape, but a package that
	// mints a temp path for its own runtime reasons is not an exception to
	// carve out: a test exercising that function writes under the shared
	// base exactly as a test helper does, and wants the same remedy.
	//
	// This package is skipped because Main is an exported function calling
	// os.MkdirTemp, so every importer would otherwise qualify through the
	// remedy itself. Measured: removing the skip changes no verdict on the
	// tree as it stands, since an importer of testtmp is a wired package
	// and a wired package raises nothing. What it would spare is an
	// unwired package importing testtmp for some other reason, reported
	// for temp use it does not have.
	helpers := map[string]string{}
	for _, p := range pkgs {
		if p.ImportPath == selfPath {
			continue
		}
		fset := token.NewFileSet()
		for _, name := range p.GoFiles {
			f, err := parser.ParseFile(fset, filepath.Join(p.Dir, name), nil, 0)
			if err != nil {
				t.Fatalf("parse %s/%s: %v", p.Dir, name, err)
			}
			if use := findHelperTempUse(fset, f); use != "" {
				helpers[p.ImportPath] = use
				break
			}
		}
	}

	var violations []string
	for _, p := range pkgs {
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
		if tempUse == "" {
			// The package's own path leads the list because an in-package
			// test reaches its own non-test helper without importing
			// anything: go list reports no self-import for those, so the
			// loop below would not otherwise reach a helper the tests
			// beside it call.
			reach := append([]string{p.ImportPath}, p.TestImports...)
			for _, imp := range append(reach, p.XTestImports...) {
				use, ok := helpers[imp]
				if !ok {
					continue
				}
				tempUse = use
				if imp != p.ImportPath {
					tempUse = imp + "'s " + use
				}
				break
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

// findHelperTempUse reports the first exported function in a non-test file
// that acquires a temp path — the acquisition an importing test cannot be
// seen to make from its own call site. Returns "Name: file:line: expr" or "".
//
// The export check is not decoration: an unexported function cannot be the
// reason another package's tests use temp, so flagging its importers would
// be a report with no remedy behind it.
//
// One level deep, and no further: a helper whose own temp use is behind a
// second package is not found. Closing that needs the call graph, and the
// arrival this guards against is a shared testutil package calling the temp
// API itself.
func findHelperTempUse(fset *token.FileSet, f *ast.File) string {
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil || !fn.Name.IsExported() {
			continue
		}
		if use := findTempUse(fset, fn.Body); use != "" {
			return fn.Name.Name + ": " + use
		}
	}
	return ""
}

// findTempUse reports the first call that makes the test binary's log record
// a path under the shared temp base: any X.TempDir() (testing.T, testing.B,
// suite helpers), or os.MkdirTemp / os.CreateTemp, whose leaked artefacts
// have the same effect. Returns "file:line: expr" or "".
func findTempUse(fset *token.FileSet, node ast.Node) string {
	var found string
	ast.Inspect(node, func(n ast.Node) bool {
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
