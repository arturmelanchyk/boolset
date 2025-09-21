package boolset

import (
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"sort"
	"testing"

	"golang.org/x/tools/go/analysis"
)

const diagMsg = "map[string]bool only stores \"true\" values; consider map[string]struct{}"

func TestAnalyzer(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		src      string
		wantMsgs []string
	}{
		{
			name: "only true assignments",
			src: `package p

				func f() {
					set := map[string]bool{}
					set["a"] = true
					set["b"] = true
				}
				`,
			wantMsgs: []string{diagMsg},
		},
		{
			name: "includes false",
			src: `package p

				func f() {
					set := map[string]bool{}
					set["a"] = true
					set["b"] = false
				}
				`,
			wantMsgs: nil,
		},
		{
			name: "variable assignment",
			src: `package p

				func f(b bool) {
					set := map[string]bool{}
					set["a"] = b
				}
				`,
			wantMsgs: nil,
		},
		{
			name: "composite literal",
			src: `package p

				var set = map[string]bool{
					"a": true,
					"b": true,
				}
				`,
			wantMsgs: []string{diagMsg},
		},
		{
			name: "struct field",
			src: `package p

				type S struct {
					set map[string]bool
				}
				
				func (s *S) init() {
					s.set = make(map[string]bool)
					s.set["ok"] = true
				}
				`,
			wantMsgs: []string{diagMsg},
		},
		{
			name: "struct field with both true and false",
			src: `package p

				type S struct {
					set map[string]bool
				}
				
				func (s *S) init() {
					s.set = make(map[string]bool)
					s.set["ok"] = true
				}

				func (s *S) close() {
					s.set["err"] = false
				}
				`,
			wantMsgs: nil,
		},
		{
			name: "const true variable",
			src: `package p

				const alwaysTrue = true
				
				func f() {
					set := map[string]bool{}
					set["a"] = alwaysTrue
				}
				`,
			wantMsgs: []string{diagMsg},
		},
		{
			name: "const true variable overridden by local",
			src: `package p

				const alwaysTrue = true
				
				func f() {
					set := map[string]bool{}
					set["a"] = alwaysTrue

					var alwaysFalse bool
					set["b"] = alwaysFalse
				}
				`,
			wantMsgs: nil,
		},
		{
			name: "local true variable",
			src: `package p

				func f() {
					flag := true
					set := make(map[string]bool)
					set["a"] = flag
				}
				`,
			wantMsgs: []string{diagMsg},
		},
		{
			name: "local variable reassigned false before use",
			src: `package p

				func f() {
					flag := true
					flag = false
					set := make(map[string]bool)
					set["a"] = flag
				}
				`,
			wantMsgs: nil,
		},
		{
			name: "global true variable not trusted",
			src: `package p

				var alwaysTrue = true
				
				func f() {
					set := make(map[string]bool)
					set["a"] = alwaysTrue
				}
				`,
			wantMsgs: nil,
		},
		{
			name: "tuple assignment all true",
			src: `package p

				func f() {
					set := make(map[string]bool)
					set["a"], set["b"] = true, true
				}
				`,
			wantMsgs: []string{diagMsg},
		},
		{
			name: "tuple assignment with false",
			src: `package p

				func f() {
					set := make(map[string]bool)
					set["a"], set["b"] = true, false
				}
				`,
			wantMsgs: nil,
		},
		{
			name: "constant folded true expression",
			src: `package p

				func f() {
					set := make(map[string]bool)
					set["a"] = 1 < 2
					set["b"] = !false
				}
				`,
			wantMsgs: []string{diagMsg},
		},
		{
			name: "type alias map",
			src: `package p

				type stringSet map[string]bool
				
				func f() {
					set := make(stringSet)
					set["a"] = true
				}
				`,
			wantMsgs: []string{diagMsg},
		},
		{
			name: "chained true locals",
			src: `package p

				func f() {
					flag := true
					alias := flag
					set := make(map[string]bool)
					set["a"] = alias
				}
				`,
			wantMsgs: []string{diagMsg},
		},
		{
			name: "loop assignments",
			src: `package p

				func f() {
					keys := []string{"a", "b"}
					set := make(map[string]bool)
					for _, k := range keys {
						set[k] = true
					}
				}
				`,
			wantMsgs: []string{diagMsg},
		},
		{
			name: "pointer dereference assignment not reported",
			src: `package p

				func f() {
					set := make(map[string]bool)
					setPtr := &set
					(*setPtr)["a"] = true
				}
				`,
			wantMsgs: nil,
		},
		{
			name: "assignment of function call result not reported",
			src: `package p

				func f() {
					set := make(map[string]bool)
					set["a"] = g()
				}

				func g() bool {
					return true
				}
				`,
			wantMsgs: nil,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			diags := runNewAnalyzer(t, tc.src)
			if len(diags) != len(tc.wantMsgs) {
				t.Fatalf("expected %d diagnostics, got %d", len(tc.wantMsgs), len(diags))
			}
			sort.Strings(diags)
			sort.Strings(tc.wantMsgs)
			for i, msg := range tc.wantMsgs {
				if diags[i] != msg {
					t.Fatalf("unexpected diagnostic %q, want %q", diags[i], msg)
				}
			}
		})
	}
}

func runNewAnalyzer(t *testing.T, src string) []string {
	t.Helper()

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "test.go", src, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	files := []*ast.File{file}
	info := &types.Info{
		Types:      make(map[ast.Expr]types.TypeAndValue),
		Defs:       make(map[*ast.Ident]types.Object),
		Uses:       make(map[*ast.Ident]types.Object),
		Selections: make(map[*ast.SelectorExpr]*types.Selection),
	}

	conf := types.Config{
		Importer: importer.Default(),
		Error:    func(err error) {},
	}
	pkg, err := conf.Check(file.Name.Name, fset, files, info)
	if err != nil && pkg == nil {
		t.Fatalf("type check error: %v", err)
	}

	var messages []string
	pass := &analysis.Pass{
		Analyzer:  NewAnalyzer(),
		Fset:      fset,
		Files:     files,
		Pkg:       pkg,
		TypesInfo: info,
		Report: func(diag analysis.Diagnostic) {
			messages = append(messages, diag.Message)
		},
	}

	_, err = NewAnalyzer().Run(pass)
	if err != nil {
		t.Fatalf("analyzer run error: %v", err)
	}

	return messages
}
