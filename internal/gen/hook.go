package gen

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"

	"github.com/go-kanna/internal/diag"
	"github.com/go-kanna/internal/packages"
	"github.com/go-kanna/internal/scan"
	"github.com/go-kanna/internal/suggest"
)

// base spells validate.Base, the path of an error on a value as a whole.
const base = "base"

// index matches the index of an element, as validate.Index spells it.
var index = regexp.MustCompile(`^\[[0-9]+\]`)

// hookPaths reports the paths the hooks of found add or nest errors under that
// no error of their struct can be under: a field the struct does not have, or
// more after a value with nothing under it.
//
// It looks at the calls of Add and Nest on the Errors the hook is given, whose
// path is a constant, or validate.Index or validate.Key of one. A path is
// reported only when no way of reading it names something: a map key, which
// validate.Key writes as it is, may hold a dot, and so may a JSON name. Below a
// value whose Validate kanna-validate does not write, nothing is checked, as
// what it reports is up to that Validate.
func hookPaths(fset *token.FileSet, pkgs []*packages.Package, found []target) []diag.Diag {
	var diags []diag.Diag
	for _, t := range found {
		if t.hook == nil {
			continue
		}
		i := slices.IndexFunc(pkgs, func(p *packages.Package) bool { return p.PkgPath == t.PkgPath })
		if i < 0 || pkgs[i].TypesInfo == nil {
			continue
		}
		info := pkgs[i].TypesInfo
		decl, errs := hookDecl(pkgs[i].Syntax, info, t.hook)
		if errs == nil {
			continue
		}
		c := hookChecker{fset: fset, info: info, target: t, errs: errs, qualify: types.RelativeTo(t.Named.Obj().Pkg())}
		diags = append(diags, c.check(decl.Body)...)
	}
	return diags
}

// hookDecl returns the declaration of hook among files, and the variable of
// its parameter, or a nil one when it has no body or no name for it.
func hookDecl(files []*ast.File, info *types.Info, hook *types.Func) (*ast.FuncDecl, types.Object) {
	for _, f := range files {
		for _, d := range f.Decls {
			decl, ok := d.(*ast.FuncDecl)
			if !ok || decl.Body == nil || info.Defs[decl.Name] != hook {
				continue
			}
			params := decl.Type.Params.List
			if len(params) != 1 || len(params[0].Names) != 1 {
				return nil, nil
			}
			return decl, info.Defs[params[0].Names[0]]
		}
	}
	return nil, nil
}

type hookChecker struct {
	fset    *token.FileSet
	info    *types.Info
	target  target
	errs    types.Object // the hook's parameter
	qualify types.Qualifier
}

func (c hookChecker) check(body *ast.BlockStmt) []diag.Diag {
	var diags []diag.Diag
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || len(call.Args) == 0 {
			return true
		}
		method := c.errsMethod(call)
		if method == "" {
			return true
		}
		arg := call.Args[0]
		path, steps, ok := c.pathOf(arg)
		if !ok {
			return true
		}
		if path == "" && len(steps) == 0 {
			if method == "Add" {
				diags = append(diags, diag.Errorf(c.fset.Position(arg.Pos()),
					`"" is not a path of %s: Add needs a field`, c.target.Name).WithHints(c.wholeHint()))
			}
			return true
		}
		w := walk{c: c, path: path, steps: steps}
		if !w.start() {
			d := diag.Errorf(c.fset.Position(arg.Pos()), "%s is not a path of %s: %s", c.spell(arg, path), c.target.Name, w.why)
			if w.hint != "" {
				d = d.WithHints(w.hint)
			}
			diags = append(diags, d)
		}
		return true
	})
	return diags
}

// errsMethod returns "Add" or "Nest" for a call of that method on the Errors
// the hook is given, and "" for any other call.
func (c hookChecker) errsMethod(call *ast.CallExpr) string {
	sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !ok {
		return ""
	}
	recv, ok := ast.Unparen(sel.X).(*ast.Ident)
	if !ok || c.info.Uses[recv] != c.errs {
		return ""
	}
	fn, ok := c.info.Uses[sel.Sel].(*types.Func)
	if !ok {
		return ""
	}
	switch fn.FullName() {
	case "(*" + runtimePath + ".Errors).Add":
		return "Add"
	case "(*" + runtimePath + ".Errors).Nest":
		return "Nest"
	}
	return ""
}

// step is what validate.Index or validate.Key adds to a path.
type step string

const (
	indexStep step = "Index"
	keyStep   step = "Key"
)

// pathOf returns the constant path e starts with, and the steps validate.Index
// and validate.Key add to it, innermost first. It reports false for a path
// that is not so made.
func (c hookChecker) pathOf(e ast.Expr) (string, []step, bool) {
	if tv, ok := c.info.Types[e]; ok && tv.Value != nil && tv.Value.Kind() == constant.String {
		return constant.StringVal(tv.Value), nil, true
	}
	call, ok := ast.Unparen(e).(*ast.CallExpr)
	if !ok || len(call.Args) != 2 {
		return "", nil, false
	}
	s := step(c.runtimeFunc(call))
	if s != indexStep && s != keyStep {
		return "", nil, false
	}
	path, steps, ok := c.pathOf(call.Args[0])
	return path, append(steps, s), ok
}

// runtimeFunc returns the name of the function of the runtime call calls, or
// "".
func (c hookChecker) runtimeFunc(call *ast.CallExpr) string {
	fun := ast.Unparen(call.Fun)
	switch f := fun.(type) {
	case *ast.IndexExpr:
		fun = f.X
	case *ast.IndexListExpr:
		fun = f.X
	}
	var id *ast.Ident
	switch f := fun.(type) {
	case *ast.SelectorExpr:
		id = f.Sel
	case *ast.Ident:
		id = f
	default:
		return ""
	}
	fn, ok := c.info.Uses[id].(*types.Func)
	if !ok || fn.Pkg() == nil || fn.Pkg().Path() != runtimePath {
		return ""
	}
	return fn.Name()
}

// spell names the argument e, whose constant start is path, in a message.
func (c hookChecker) spell(e ast.Expr, path string) string {
	if tv, ok := c.info.Types[e]; ok && tv.Value != nil {
		return fmt.Sprintf("%q", path)
	}
	return types.ExprString(e)
}

func (c hookChecker) wholeHint() string {
	return "an error on " + c.target.Name + " as a whole goes under validate.Base"
}

// kind is what a path can go on with below a value.
type kind int

const (
	leaf   kind = iota // nothing
	opaque             // whatever its own Validate reports
	record             // a field, after a dot
	list               // an element, as [0]
	dict               // a key, after a dot
)

// kindOf returns what a path can go on with below a value of type t, through
// pointers, and the type of its elements for a list or a dict.
func (c hookChecker) kindOf(t types.Type) (kind, types.Type) {
	for {
		p, ok := t.Underlying().(*types.Pointer)
		if !ok {
			break
		}
		t = p.Elem()
	}
	if _, ok := types.Unalias(t).(*types.TypeParam); ok || types.IsInterface(t) || c.validatesItself(t) {
		return opaque, nil
	}
	switch u := t.Underlying().(type) {
	case *types.Struct:
		return record, nil
	case *types.Slice:
		return list, u.Elem()
	case *types.Array:
		return list, u.Elem()
	case *types.Map:
		return dict, u.Elem()
	}
	return leaf, nil
}

// validatesItself reports whether t has a Validate that kanna-validate does
// not write, which reports its errors under paths of its own.
func (c hookChecker) validatesItself(t types.Type) bool {
	obj, _, _ := types.LookupFieldOrMethod(t, true, nil, "Validate")
	fn, ok := obj.(*types.Func)
	return ok && filepath.Base(c.fset.Position(fn.Pos()).Filename) != outputFile
}

// member is a name the errors of a struct can be under.
type member struct {
	name string
	typ  types.Type
}

// members returns the names the errors of the struct t can be under, as the
// generated Validate names its fields, and whether a field it flattens reports
// under names of its own, which none of them tells.
func (c hookChecker) members(t types.Type, seen []types.Type) ([]member, bool) {
	if slices.ContainsFunc(seen, func(s types.Type) bool { return types.Identical(s, t) }) {
		return nil, false
	}
	seen = append(slices.Clip(seen), t)
	for {
		p, ok := t.Underlying().(*types.Pointer)
		if !ok {
			break
		}
		t = p.Elem()
	}
	st, ok := t.Underlying().(*types.Struct)
	if !ok {
		return nil, false
	}

	var (
		found []member
		open  bool
	)
	for i := range st.NumFields() {
		f := st.Field(i)
		if f.Name() == "_" {
			continue
		}
		tag := reflect.StructTag(st.Tag(i))
		name, named := jsonName(scan.Field{Name: f.Name(), Tag: tag})
		found = append(found, member{name: name, typ: f.Type()})
		if !f.Embedded() || named || tag.Get("json") == "-" {
			continue
		}
		switch k, _ := c.kindOf(f.Type()); k {
		case opaque:
			open = true
		case record:
			flat, o := c.members(f.Type(), seen)
			found = append(found, flat...)
			open = open || o
		case leaf, list, dict:
			// What it reports goes under its own name.
		}
	}
	return found, open
}

// walk tells whether a path can be read as one the errors of the target are
// under, and of the ways it cannot, keeps the one that gets furthest.
type walk struct {
	c     hookChecker
	path  string
	steps []step

	at   int    // where in the path the furthest failure is
	why  string // what is wrong there
	hint string
}

func (w *walk) start() bool {
	if w.path == "" {
		return w.end(w.c.target.Named)
	}
	return w.fields(w.c.target.Named, 0)
}

// fail keeps a failure at at, unless one got further, and returns false.
func (w *walk) fail(at int, hint, format string, args ...any) bool {
	if w.why == "" || at > w.at {
		w.at, w.why, w.hint = at, fmt.Sprintf(format, args...), hint
	}
	return false
}

// value reports whether what follows at can follow a value of type t.
func (w *walk) value(t types.Type, at int) bool {
	rest := w.path[at:]
	if rest == "" {
		return w.end(t)
	}
	reached := w.path[:at]
	switch k, elem := w.c.kindOf(t); k {
	case opaque:
		return true
	case dict:
		// A key may hold a dot, so it may run to the end of the path.
		if rest[0] == '.' {
			return true
		}
		return w.fail(at, "", "%s is a map, whose keys follow a dot", reached)
	case list:
		if m := index.FindString(rest); m != "" {
			return w.value(elem, at+len(m))
		}
		return w.fail(at, "", "%s is %s, whose elements go as %s[0]", reached, sliceOrArray(t), reached)
	case record:
		if rest[0] == '.' {
			return w.fields(t, at+1)
		}
		return w.fail(at, "", "%s is a struct, whose fields follow a dot", reached)
	case leaf:
	}
	return w.fail(at, "", "%s is %s, with nothing under it", reached, types.TypeString(t, w.c.qualify))
}

// fields reports whether the path from at can go on with a field of the
// struct t.
func (w *walk) fields(t types.Type, at int) bool {
	rest := w.path[at:]
	members, open := w.c.members(t, nil)
	for _, m := range members {
		if after, ok := strings.CutPrefix(rest, m.name); ok && endsName(after) && w.value(m.typ, at+len(m.name)) {
			return true
		}
	}
	if after, ok := strings.CutPrefix(rest, base); ok && endsName(after) {
		if after == "" && len(w.steps) == 0 {
			return true
		}
		if !open {
			return w.fail(at+len(base), "", "%s is the value as a whole, with nothing under it", w.path[:at+len(base)])
		}
	}
	if open {
		return true
	}

	name := rest[:strings.IndexAny(rest+".", ".[")]
	names := make([]string, len(members))
	for i, m := range members {
		names[i] = m.name
	}
	hint := ""
	if s, ok := suggest.Closest(name, names); ok {
		hint = fmt.Sprintf("did you mean %q?", w.path[:at]+s)
	} else if at == 0 {
		hint = w.c.wholeHint()
	}
	owner := w.c.target.Name
	if at > 0 {
		owner = w.path[:at-1]
	}
	return w.fail(at, hint, "%s has no field %q", owner, name)
}

// end reports whether the steps of the path can be taken from a value of type
// t, which the constant start of the path reaches.
func (w *walk) end(t types.Type) bool {
	for i, s := range w.steps {
		k, elem := w.c.kindOf(t)
		switch {
		case k == opaque:
			return true
		case s == indexStep && k == list, s == keyStep && k == dict:
			t = elem
			continue
		}
		at := len(w.path) + 1 + i // past where any reading of the constant fails
		if s == indexStep {
			return w.fail(at, "", "validate.Index needs a slice or an array, not %s", types.TypeString(t, w.c.qualify))
		}
		return w.fail(at, "", "validate.Key needs a map, not %s", types.TypeString(t, w.c.qualify))
	}
	return true
}

// endsName reports whether after, what follows a name in a path, lets the name
// end there.
func endsName(after string) bool {
	return after == "" || after[0] == '.' || after[0] == '['
}

func sliceOrArray(t types.Type) string {
	for {
		p, ok := t.Underlying().(*types.Pointer)
		if !ok {
			break
		}
		t = p.Elem()
	}
	if _, ok := t.Underlying().(*types.Array); ok {
		return "an array"
	}
	return "a slice"
}
