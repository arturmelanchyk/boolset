package main

import (
	"errors"
	"flag"
	"fmt"
	"go/ast"
	"go/build"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"sort"

	"github.com/arturmelanchyk/boolset/boolset"
)

func main() {
	flag.Parse()
	args := flag.Args()
	if len(args) == 0 {
		args = []string{"."}
	}

	hadError := false
	for _, path := range args {
		if err := inspectPath(path); err != nil {
			if _, err := fmt.Fprintln(os.Stderr, err); err != nil {
				os.Exit(1)
			}
			hadError = true
		}
	}
	if hadError {
		os.Exit(1)
	}
}

func inspectPath(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return inspectDir(path)
	}
	return inspectDir(filepath.Dir(path))
}

func inspectDir(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	hasGo := false
	for _, entry := range entries {
		if !entry.IsDir() && filepath.Ext(entry.Name()) == ".go" {
			hasGo = true
			break
		}
	}
	if !hasGo {
		return nil
	}

	buildPkg, err := build.Default.ImportDir(dir, 0)
	if err != nil {
		var noGo *build.NoGoError
		if errors.As(err, &noGo) {
			return nil
		}
		return err
	}

	files, fileSet, err := parseFiles(dir, buildPkg.GoFiles)
	if err != nil {
		return err
	}
	if len(files) == 0 {
		return nil
	}

	pkgName := files[0].Name.Name

	conf := types.Config{
		Importer: importer.Default(),
		Error:    func(err error) {},
	}
	info := &types.Info{
		Types:      make(map[ast.Expr]types.TypeAndValue),
		Defs:       make(map[*ast.Ident]types.Object),
		Uses:       make(map[*ast.Ident]types.Object),
		Selections: make(map[*ast.SelectorExpr]*types.Selection),
	}

	pkgTypes, err := conf.Check(pkgName, fileSet, files, info)
	if pkgTypes == nil {
		return err
	}

	diagnostics := boolset.Analyze(pkgTypes, files, info)
	if len(diagnostics) == 0 {
		return nil
	}

	sort.Slice(diagnostics, func(i, j int) bool {
		return fileSet.Position(diagnostics[i].Pos).Offset < fileSet.Position(diagnostics[j].Pos).Offset
	})

	for _, diag := range diagnostics {
		pos := fileSet.Position(diag.Pos)
		if _, err := fmt.Fprintf(os.Stderr, "%s:%d:%d: %s\n", pos.Filename, pos.Line, pos.Column, diag.Message); err != nil {
			return fmt.Errorf("failed to write to stderr: %w", err)
		}
	}
	return errors.New("boolsetlint found issues")
}

func parseFiles(dir string, names []string) ([]*ast.File, *token.FileSet, error) {
	if len(names) == 0 {
		return nil, nil, nil
	}
	fset := token.NewFileSet()
	files := make([]*ast.File, 0, len(names))
	for _, name := range names {
		path := filepath.Join(dir, name)
		file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if err != nil {
			return nil, nil, err
		}
		files = append(files, file)
	}
	return files, fset, nil
}
