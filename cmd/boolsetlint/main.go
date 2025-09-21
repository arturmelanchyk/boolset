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
	"regexp"
	"sort"
	"strings"

	"github.com/arturmelanchyk/boolset/boolset"
)

func main() {
	flag.Parse()
	targets, err := expandTargets(flag.Args())
	if err != nil {
		if _, err := fmt.Fprintln(os.Stderr, err); err != nil {
			os.Exit(2)
		}
		os.Exit(1)
	}

	hadError := false
	totalIssues := 0
	for _, path := range targets {
		count, err := inspectPath(path)
		totalIssues += count
		if err != nil {
			if _, err := fmt.Fprintln(os.Stderr, err); err != nil {
				os.Exit(2)
			}
			hadError = true
		}
	}
	if totalIssues > 0 {
		if _, err := fmt.Fprintf(os.Stderr, "boolsetlint found %d issue(s)\n", totalIssues); err != nil {
			os.Exit(2)
		}
	}
	if hadError || totalIssues > 0 {
		os.Exit(1)
	}
}

func inspectPath(path string) (int, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	if info.IsDir() {
		return inspectDir(path)
	}
	return inspectDir(filepath.Dir(path))
}

func inspectDir(dir string) (int, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, err
	}
	hasGo := false
	for _, entry := range entries {
		if !entry.IsDir() && filepath.Ext(entry.Name()) == ".go" {
			hasGo = true
			break
		}
	}
	if !hasGo {
		return 0, nil
	}

	buildPkg, err := build.Default.ImportDir(dir, 0)
	if err != nil {
		var noGo *build.NoGoError
		if errors.As(err, &noGo) {
			return 0, nil
		}
		return 0, err
	}

	files, fileSet, err := parseFiles(dir, buildPkg.GoFiles)
	if err != nil {
		return 0, err
	}
	if len(files) == 0 {
		return 0, nil
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
		return 0, err
	}

	diagnostics := boolset.Analyze(pkgTypes, files, info)
	if len(diagnostics) == 0 {
		return 0, nil
	}

	sort.Slice(diagnostics, func(i, j int) bool {
		return fileSet.Position(diagnostics[i].Pos).Offset < fileSet.Position(diagnostics[j].Pos).Offset
	})

	for _, diag := range diagnostics {
		pos := fileSet.Position(diag.Pos)
		if _, err := fmt.Fprintf(os.Stderr, "%s:%d:%d: %s\n", pos.Filename, pos.Line, pos.Column, diag.Message); err != nil {
			os.Exit(2)
		}
	}
	return len(diagnostics), nil
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

func expandTargets(args []string) ([]string, error) {
	if len(args) == 0 {
		args = []string{"."}
	}

	seen := make(map[string]struct{})
	var targets []string
	for _, arg := range args {
		expanded, err := expandArg(arg)
		if err != nil {
			return nil, err
		}
		for _, target := range expanded {
			clean := filepath.Clean(target)
			if _, ok := seen[clean]; ok {
				continue
			}
			seen[clean] = struct{}{}
			targets = append(targets, clean)
		}
	}
	return targets, nil
}

func expandArg(arg string) ([]string, error) {
	if strings.Contains(arg, "...") {
		dirs, err := expandEllipsis(arg)
		if err != nil {
			return nil, err
		}
		if len(dirs) == 0 {
			return nil, fmt.Errorf("pattern %q matched no directories", arg)
		}
		return dirs, nil
	}
	return []string{arg}, nil
}

func expandEllipsis(pattern string) ([]string, error) {
	re, err := compilePattern(pattern)
	if err != nil {
		return nil, err
	}
	root := walkRoot(pattern)
	if _, err := os.Stat(root); err != nil {
		return nil, err
	}

	var dirs []string
	err = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info == nil || !info.IsDir() {
			return nil
		}
		candidate := normalizeForMatch(path)
		if re.MatchString(candidate) || (candidate != "." && re.MatchString(candidate+"/")) {
			dirs = append(dirs, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	sort.Strings(dirs)
	return dirs, nil
}

func walkRoot(pattern string) string {
	idx := strings.Index(pattern, "...")
	root := pattern
	if idx != -1 {
		root = pattern[:idx]
	}
	root = filepath.Clean(filepath.FromSlash(root))
	if root == "" {
		return "."
	}
	return root
}

func compilePattern(pattern string) (*regexp.Regexp, error) {
	norm := normalizeForMatch(pattern)
	var sb strings.Builder
	sb.WriteString("^")
	for i := 0; i < len(norm); {
		if strings.HasPrefix(norm[i:], "...") {
			sb.WriteString(".*")
			i += 3
			continue
		}
		switch norm[i] {
		case '*':
			sb.WriteString("[^/]*")
		case '?':
			sb.WriteString("[^/]")
		default:
			sb.WriteString(regexp.QuoteMeta(norm[i : i+1]))
		}
		i++
	}
	sb.WriteString("$")
	return regexp.Compile(sb.String())
}

func normalizeForMatch(path string) string {
	if path == "" {
		return "."
	}
	path = filepath.ToSlash(path)
	if isAbsPath(path) {
		if strings.HasSuffix(path, "/") && path != "/" {
			path = strings.TrimSuffix(path, "/")
		}
		return path
	}
	for strings.HasPrefix(path, "./") {
		path = path[2:]
	}
	if path == "" {
		return "."
	}
	if strings.HasSuffix(path, "/") {
		path = strings.TrimSuffix(path, "/")
	}
	return path
}

func isAbsPath(path string) bool {
	if filepath.IsAbs(path) {
		return true
	}
	if len(path) >= 2 && path[1] == ':' {
		return true
	}
	return strings.HasPrefix(path, "/")
}
