package boolset

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
)

// Diagnostic represents a linter finding.
type Diagnostic struct {
	Pos     token.Pos
	Message string
}

// Analyze inspects the provided package AST and type info, returning any diagnostics.
func Analyze(pkg *types.Package, files []*ast.File, info *types.Info) []Diagnostic {
	if pkg == nil || len(files) == 0 || info == nil {
		return nil
	}

	v := &analyzer{
		pkg:        pkg,
		info:       info,
		results:    make(map[types.Object]*mapInfo),
		qualifier:  makeQualifier(pkg),
		boolValues: make(map[types.Object]truthState),
	}

	for _, file := range files {
		v.inspectFile(file)
	}

	var diags []Diagnostic
	for _, mi := range v.results {
		if mi.trueCount == 0 || !mi.onlyTrue {
			continue
		}
		pos := mi.pos
		if pos == token.NoPos && mi.obj != nil {
			pos = mi.obj.Pos()
		}
		if pos == token.NoPos {
			continue
		}
		key := mi.keyType
		diags = append(diags, Diagnostic{
			Pos:     pos,
			Message: fmt.Sprintf("map[%s]bool only stores true values; consider map[%s]struct{}", key, key),
		})
	}
	return diags
}

type analyzer struct {
	pkg        *types.Package
	info       *types.Info
	results    map[types.Object]*mapInfo
	qualifier  types.Qualifier
	boolValues map[types.Object]truthState
}

type mapInfo struct {
	obj       types.Object
	onlyTrue  bool
	trueCount int
	pos       token.Pos
	keyType   string
}

func (a *analyzer) inspectFile(file *ast.File) {
	var stack []ast.Node
	ast.Inspect(file, func(n ast.Node) bool {
		if n == nil {
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
			return false
		}
		stack = append(stack, n)

		switch node := n.(type) {
		case *ast.AssignStmt:
			a.handleAssign(node)
		case *ast.CompositeLit:
			a.handleComposite(node, stack)
		case *ast.ValueSpec:
			a.handleValueSpec(node)
		}
		return true
	})
}

func (a *analyzer) handleAssign(assign *ast.AssignStmt) {
	rhsLen := len(assign.Rhs)
	for i, lhs := range assign.Lhs {
		idx, ok := lhs.(*ast.IndexExpr)
		rhsExpr := exprAt(assign.Rhs, rhsLen, i)
		if rhsExpr != nil {
			a.trackVarAssignment(lhs, rhsExpr, assign.Tok)
		}
		if !ok || assign.Tok != token.ASSIGN || rhsExpr == nil {
			continue
		}
		obj := a.mapObject(idx.X)
		info := a.infoFor(obj)
		if info == nil {
			continue
		}
		info.recordAssignment(a, rhsExpr, idx.Pos())
	}
}

func (a *analyzer) handleComposite(lit *ast.CompositeLit, stack []ast.Node) {
	tv, ok := a.info.Types[lit]
	if !ok || tv.Type == nil {
		return
	}
	m, ok := tv.Type.Underlying().(*types.Map)
	if !ok || !isBool(m.Elem()) {
		return
	}

	obj := a.objectForComposite(lit, stack)
	info := a.infoFor(obj)
	if info == nil {
		return
	}

	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		info.recordAssignment(a, kv.Value, kv.Value.Pos())
	}
}

func (a *analyzer) handleValueSpec(spec *ast.ValueSpec) {
	namesLen := len(spec.Names)
	if namesLen == 0 {
		return
	}
	valuesLen := len(spec.Values)
	if valuesLen == 0 {
		return
	}
	for i, name := range spec.Names {
		rhs := exprAt(spec.Values, valuesLen, i)
		if rhs == nil {
			continue
		}
		obj := a.info.Defs[name]
		if obj == nil {
			obj = a.info.Uses[name]
		}
		if obj == nil {
			continue
		}
		a.recordVarAssignment(obj, rhs)
	}
}

func (a *analyzer) infoFor(obj types.Object) *mapInfo {
	if obj == nil {
		return nil
	}
	if pkg := obj.Pkg(); pkg != nil && pkg != a.pkg {
		return nil
	}

	typ := obj.Type()
	if typ == nil {
		return nil
	}
	m, ok := typ.Underlying().(*types.Map)
	if !ok || !isBool(m.Elem()) {
		return nil
	}

	if mi, ok := a.results[obj]; ok {
		return mi
	}
	keyType := types.TypeString(m.Key(), a.qualifier)
	mi := &mapInfo{
		obj:      obj,
		onlyTrue: true,
		pos:      obj.Pos(),
		keyType:  keyType,
	}
	a.results[obj] = mi
	return mi
}

func (a *analyzer) mapObject(expr ast.Expr) types.Object {
	switch e := expr.(type) {
	case *ast.Ident:
		if obj := a.info.Uses[e]; obj != nil {
			return obj
		}
		return a.info.Defs[e]
	case *ast.SelectorExpr:
		if sel := a.info.Selections[e]; sel != nil {
			return sel.Obj()
		}
		return a.info.Uses[e.Sel]
	case *ast.ParenExpr:
		return a.mapObject(e.X)
	default:
		return nil
	}
}

func (a *analyzer) objectForComposite(lit *ast.CompositeLit, stack []ast.Node) types.Object {
	if len(stack) < 2 {
		return nil
	}
	parent := stack[len(stack)-2]

	switch p := parent.(type) {
	case *ast.ValueSpec:
		for i, v := range p.Values {
			if v == lit {
				if i < len(p.Names) {
					if obj := a.info.Defs[p.Names[i]]; obj != nil {
						return obj
					}
					return a.info.Uses[p.Names[i]]
				}
				break
			}
		}
	case *ast.AssignStmt:
		idx := -1
		for i, v := range p.Rhs {
			if v == lit {
				idx = i
				break
			}
		}
		if idx == -1 {
			return nil
		}
		lhs := exprAt(p.Lhs, len(p.Lhs), idx)
		if lhs == nil {
			return nil
		}
		return a.objectOfAssignable(lhs)
	}
	return nil
}

func (a *analyzer) objectOfAssignable(expr ast.Expr) types.Object {
	switch e := expr.(type) {
	case *ast.Ident:
		if obj := a.info.Defs[e]; obj != nil {
			return obj
		}
		return a.info.Uses[e]
	case *ast.SelectorExpr:
		if sel := a.info.Selections[e]; sel != nil {
			return sel.Obj()
		}
		return a.info.Uses[e.Sel]
	case *ast.ParenExpr:
		return a.objectOfAssignable(e.X)
	default:
		return nil
	}
}

func (mi *mapInfo) recordAssignment(a *analyzer, rhs ast.Expr, pos token.Pos) {
	if mi == nil {
		return
	}
	if mi.pos == token.NoPos && pos.IsValid() {
		mi.pos = pos
	}
	if a.isDefinitelyTrue(rhs) {
		mi.trueCount++
		return
	}
	mi.onlyTrue = false
}

func isBool(t types.Type) bool {
	basic, ok := t.Underlying().(*types.Basic)
	return ok && basic.Kind() == types.Bool
}

func exprAt(list []ast.Expr, length, index int) ast.Expr {
	if index < length {
		return list[index]
	}
	if length == 1 {
		return list[0]
	}
	return nil
}

func makeQualifier(pkg *types.Package) types.Qualifier {
	if pkg == nil {
		return nil
	}
	return func(other *types.Package) string {
		if other == pkg {
			return ""
		}
		if other == nil {
			return ""
		}
		return other.Name()
	}
}

type truthState int

const (
	truthUnknown truthState = iota
	truthAlwaysTrue
	truthNotAlwaysTrue
)

func (a *analyzer) trackVarAssignment(lhs ast.Expr, rhs ast.Expr, tok token.Token) {
	ident, ok := lhs.(*ast.Ident)
	if !ok {
		return
	}
	if ident.Name == "_" {
		return
	}
	var obj types.Object
	if tok == token.DEFINE {
		obj = a.info.Defs[ident]
	} else {
		obj = a.info.Uses[ident]
	}
	if obj == nil {
		obj = a.info.Defs[ident]
	}
	if obj == nil {
		return
	}
	a.recordVarAssignment(obj, rhs)
}

func (a *analyzer) recordVarAssignment(obj types.Object, rhs ast.Expr) {
	v, ok := obj.(*types.Var)
	if !ok {
		return
	}
	if !isBool(v.Type()) {
		return
	}
	if !a.isLocalVar(v) {
		return
	}
	if a.boolValues == nil {
		a.boolValues = make(map[types.Object]truthState)
	}
	if a.isDefinitelyTrue(rhs) {
		if a.boolValues[obj] == truthUnknown {
			a.boolValues[obj] = truthAlwaysTrue
		}
		return
	}
	a.boolValues[obj] = truthNotAlwaysTrue
}

func (a *analyzer) isDefinitelyTrue(expr ast.Expr) bool {
	if expr == nil {
		return false
	}
	if tv, ok := a.info.Types[expr]; ok && tv.Value != nil {
		if tv.Value.Kind() == constant.Bool {
			return constant.BoolVal(tv.Value)
		}
	}
	switch e := expr.(type) {
	case *ast.Ident:
		if obj := a.info.Uses[e]; obj != nil {
			return a.objectIsDefinitelyTrue(obj)
		}
		if obj := a.info.Defs[e]; obj != nil {
			return a.objectIsDefinitelyTrue(obj)
		}
	case *ast.ParenExpr:
		return a.isDefinitelyTrue(e.X)
	}
	return false
}

func (a *analyzer) objectIsDefinitelyTrue(obj types.Object) bool {
	switch o := obj.(type) {
	case *types.Const:
		if v := o.Val(); v != nil && v.Kind() == constant.Bool {
			return constant.BoolVal(v)
		}
	case *types.Var:
		if a.boolValues != nil && a.boolValues[obj] == truthAlwaysTrue {
			return true
		}
	}
	return false
}

func (a *analyzer) isLocalVar(v *types.Var) bool {
	if v == nil {
		return false
	}
	if v.IsField() {
		return false
	}
	scope := v.Parent()
	if scope == nil {
		return false
	}
	pkg := v.Pkg()
	if pkg != nil && scope == pkg.Scope() {
		return false
	}
	// Parameters live in func scope but initial value unknown; skip them.
	if v.Pos() == token.NoPos {
		return false
	}
	return true
}
