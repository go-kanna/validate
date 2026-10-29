package gen

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"path/filepath"
	"slices"
	"strings"

	"github.com/go-kanna/internal/diag"
	"github.com/go-kanna/internal/directive"
	"github.com/go-kanna/internal/packages"
	"github.com/go-kanna/internal/scan"
)

// plan is what the generated Validate method of one struct does.
type plan struct {
	target

	// fields are the fields with anything to check, in declaration order.
	fields []fieldPlan
}

// fieldPlan is what the generated method checks of one field.
type fieldPlan struct {
	// name is the Go name of the field.
	name string

	// path is the name errors report the field by: its JSON name, or the Go
	// name when the json tag gives none.
	path string

	// flatten keeps what the field's own Validate reports at the level of the
	// struct, as encoding/json flattens an embedded struct without a JSON name.
	flatten bool

	value *value
}

// value is what the generated code checks of one value: a field, the value a
// pointer points to, or an element of a slice, array, or map.
type value struct {
	typ types.Type

	// required reports blank for the zero value, and omitEmpty lets it past
	// the rest. zero is how to tell it.
	required, omitEmpty bool
	zero                zeroTest

	// checks are the other rules, each reported on its own.
	checks []check

	// nest calls the value's Validate method and nests what it reports under
	// the value's path.
	nest bool

	// pointee is what the rest of a pointer's rules check: what it points to,
	// when it is not nil.
	pointee *value

	// elem checks each element of a slice or array, or each value of a map.
	elem *value
}

// zeroTest is how generated code tells the zero value of a type.
type zeroTest int

const (
	// zeroString compares a string with "".
	zeroString zeroTest = iota
	// zeroNumber compares a number with 0.
	zeroNumber
	// zeroBool tests a bool for false.
	zeroBool
	// zeroNil compares a pointer, slice, map, interface, channel, or function
	// with nil. An empty slice or map that is not nil is not zero.
	zeroNil
	// zeroMethod calls the type's own IsZero method.
	zeroMethod
	// zeroComposite compares a comparable struct or array with its zero
	// literal.
	zeroComposite
)

// planner holds what deciding the checks of a value needs besides its type.
type planner struct {
	fset *token.FileSet

	// targets are the types this run generates Validate for, which have it
	// even before it is written.
	targets map[*types.TypeName]bool

	// rewritten are the import paths of the packages whose output this run
	// rewrites, where a Validate an earlier run generated may be about to go.
	rewritten map[string]bool

	// refs is where a qualified rule= looks for its package, besides among
	// what the struct's package imports.
	refs refScope

	// marks caches what marked has read, by type.
	marks map[*types.TypeName]bool
}

// plans decides what the Validate method of each target checks. rewritten
// names the packages whose output this run rewrites, and refs where a qualified
// rule= looks besides among what the struct's package imports. A target with a
// rule that does not apply is left out, and said why.
func plans(fset *token.FileSet, targets []target, rewritten map[string]bool, refs refScope) ([]plan, []diag.Diag) {
	p := planner{
		fset:      fset,
		targets:   make(map[*types.TypeName]bool, len(targets)),
		rewritten: rewritten,
		refs:      refs,
		marks:     make(map[*types.TypeName]bool),
	}
	for _, t := range targets {
		p.targets[t.Named.Obj()] = true
	}

	var (
		found []plan
		diags []diag.Diag
	)
	for _, t := range targets {
		pl, ds := p.plan(t)
		diags = append(diags, ds...)
		if len(ds) == 0 {
			found = append(found, pl)
		}
	}
	return found, diags
}

func (p planner) plan(t target) (plan, []diag.Diag) {
	fields := make([]sibling, len(t.Fields))
	for i, f := range t.Fields {
		name, _ := jsonName(f)
		fields[i] = sibling{name: f.Name, jsonName: name, typ: f.Type}
	}

	own := t.Named.Obj().Pkg()
	refs := refScope{pkgs: append(slices.Clip(p.refs.pkgs), reachable(own)...), module: p.refs.module}
	pl := plan{target: t}
	var diags []diag.Diag
	for i, f := range t.Fields {
		if packages.Invalid(f.Type) {
			diags = append(diags, packages.NotTypeChecked(f.Pos))
			continue
		}
		at := site{pkg: own, refs: refs, fields: slices.Delete(slices.Clone(fields), i, i+1)}
		fp, errs := p.field(f, at)
		for _, err := range errs {
			diags = append(diags, fieldDiag(f, err))
		}
		if len(errs) == 0 && fp.value != nil {
			pl.fields = append(pl.fields, fp)
		}
	}
	return pl, diags
}

func (p planner) field(f scan.Field, at site) (fieldPlan, []error) {
	path, named := jsonName(f)
	fp := fieldPlan{name: f.Name, path: path, flatten: f.Embedded && !named}

	levels, err := parseTag(f.Tag)
	if err != nil {
		return fp, []error{err}
	}
	var errs []error
	fp.value, errs = p.value(f.Type, levels, at, nil)
	return fp, errs
}

// value plans the checks of a value of type t, or returns nil when there is
// nothing to check. levels[0] holds the rules for the value itself and the
// rest those for its elements, one level per dive.
//
// enclosing are the types of the values this one was reached through without
// a dive: through a pointer, or looking for elements that validate themselves.
// A type among them contains itself, as type Tree map[string]Tree does, and is
// checked already.
func (p planner) value(t types.Type, levels []level, at site, enclosing []types.Type) (*value, []error) {
	if slices.ContainsFunc(enclosing, func(e types.Type) bool { return types.Identical(e, t) }) {
		return nil, nil
	}
	enclosing = append(slices.Clip(enclosing), t)

	var l level
	if len(levels) > 0 {
		l = levels[0]
	}
	var below []level
	if len(levels) > 1 {
		below = levels[1:]
	}

	v := &value{typ: t, required: l.required, omitEmpty: l.omitEmpty}
	if l.required || l.omitEmpty {
		zero, ok := zeroTestOf(t)
		if !ok {
			return nil, []error{cannotTellEmpty(l, t, at)}
		}
		v.zero = zero
	}

	// A pointer is only ever nil or not; every rule but required and
	// omitempty is about what it points to.
	if ptr, ok := t.Underlying().(*types.Pointer); ok {
		pointee, errs := p.value(ptr.Elem(), append([]level{{rules: l.rules}}, below...), at, enclosing)
		v.pointee = pointee
		return pruned(v), errs
	}

	var errs []error
	for _, r := range l.rules {
		c, err := resolve(r, t, at)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		v.checks = append(v.checks, c)
	}

	v.nest = p.hasValidate(t)

	elem, ok := elemOf(t)
	elemAt := site{pkg: at.pkg, refs: at.refs}
	switch {
	case len(below) > 0 && !ok:
		errs = append(errs, ruleErrorf("dive needs a slice, array, or map, not %s", at.typeString(t)))
	case len(below) > 0:
		var es []error
		v.elem, es = p.value(elem, below, elemAt, nil)
		errs = append(errs, es...)
	case ok && !v.nest:
		// The elements are validated whether or not the tag dives: a slice of
		// structs that validate themselves is checked as the structs would be.
		v.elem, _ = p.value(elem, nil, elemAt, enclosing)
	}

	return pruned(v), errs
}

// pruned returns v, or nil when it checks nothing.
func pruned(v *value) *value {
	if !v.required && len(v.checks) == 0 && !v.nest && v.pointee == nil && v.elem == nil {
		return nil
	}
	return v
}

// elemOf returns the element type of a slice or array, or the value type of a
// map.
func elemOf(t types.Type) (types.Type, bool) {
	switch u := t.Underlying().(type) {
	case *types.Slice:
		return u.Elem(), true
	case *types.Array:
		return u.Elem(), true
	case *types.Map:
		return u.Elem(), true
	}
	return nil, false
}

// zeroTestOf returns how generated code tells the zero value of t, and false
// when it cannot.
func zeroTestOf(t types.Type) (zeroTest, bool) {
	// A nil pointer or interface cannot have IsZero called on it, so these are
	// compared with nil even when what they hold has the method.
	switch t.Underlying().(type) {
	case *types.Pointer, *types.Interface, *types.Slice, *types.Map, *types.Chan, *types.Signature:
		return zeroNil, true
	}
	if hasIsZero(t) {
		return zeroMethod, true
	}

	if b, ok := t.Underlying().(*types.Basic); ok {
		switch info := b.Info(); {
		case info&types.IsString != 0:
			return zeroString, true
		case info&types.IsNumeric != 0:
			return zeroNumber, true
		case info&types.IsBoolean != 0:
			return zeroBool, true
		}
	}
	if types.Comparable(t) {
		return zeroComposite, true
	}
	return 0, false
}

// hasIsZero reports whether t, or a pointer to it, has an IsZero() bool method.
// The generated code calls it on an addressable value, so either receiver works.
func hasIsZero(t types.Type) bool {
	obj, _, _ := types.LookupFieldOrMethod(t, true, nil, "IsZero")
	fn, ok := obj.(*types.Func)
	if !ok {
		return false
	}
	sig := fn.Signature()
	return sig.Params().Len() == 0 && sig.Results().Len() == 1 &&
		types.Identical(sig.Results().At(0).Type(), types.Typ[types.Bool])
}

// cannotTellEmpty reports required or omitempty, written in l, on a type whose
// zero value generated code cannot tell.
func cannotTellEmpty(l level, t types.Type, at site) *ruleError {
	name := "omitempty"
	if l.required {
		name = "required"
	}
	err := ruleErrorf("%s cannot tell whether %s is empty", name, at.typeString(t))
	err.hint = "it is not comparable and has no IsZero() bool method; give it one, or use a pointer"
	return err
}

// hasValidate reports whether the generated code can call Validate() error on
// a value of type t.
//
// A struct of another package marked //kanna:validate has it once that
// package's own run writes it, which go generate ./... may run after this one,
// so the mark decides, and an earlier run's output there counts only for what
// is marked still.
func (p planner) hasValidate(t types.Type) bool {
	if named, ok := types.Unalias(t).(*types.Named); ok &&
		(p.targets[named.Obj()] || p.elsewhere(named.Obj()) && p.marked(named.Obj())) {
		return true
	}

	obj, _, _ := types.LookupFieldOrMethod(t, true, nil, "Validate")
	fn, ok := obj.(*types.Func)
	if !ok {
		return false
	}
	sig := fn.Signature()
	if sig.Params().Len() != 0 || sig.Results().Len() != 1 ||
		!types.Identical(sig.Results().At(0).Type(), types.Universe.Lookup("error").Type()) {
		return false
	}

	// An earlier run's output keeps only the methods of what is marked: in a
	// package this run rewrites, today's targets, and elsewhere what is marked
	// still, which the check above has found already, but for a method
	// promoted from a struct it embeds.
	if filepath.Base(p.fset.Position(fn.Pos()).Filename) != outputFile {
		return true
	}
	recvType := fn.Signature().Recv().Type()
	if ptr, isPtr := recvType.(*types.Pointer); isPtr {
		recvType = ptr.Elem()
	}
	recv, ok := types.Unalias(recvType).(*types.Named)
	return ok && (p.targets[recv.Obj()] || p.elsewhere(recv.Obj()) && p.marked(recv.Obj()))
}

// elsewhere reports whether obj is declared in a package this run does not
// rewrite.
func (p planner) elsewhere(obj *types.TypeName) bool {
	return obj.Pkg() != nil && !p.rewritten[obj.Pkg().Path()]
}

// marked reports whether the declaration of obj carries //kanna:validate,
// reading the file it is in: a package loaded from export data has no syntax.
func (p planner) marked(obj *types.TypeName) bool {
	if marked, ok := p.marks[obj]; ok {
		return marked
	}
	marked := false
	if path := p.fset.Position(obj.Pos()).Filename; path != "" {
		fset := token.NewFileSet()
		if f, err := parser.ParseFile(fset, path, nil, parser.ParseComments|parser.SkipObjectResolution); err == nil {
			marked = declMarked(fset, f, obj.Name())
		}
	}
	p.marks[obj] = marked
	return marked
}

// declMarked reports whether f declares the type name with //kanna:validate.
func declMarked(fset *token.FileSet, f *ast.File, name string) bool {
	for _, decl := range f.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.TYPE {
			continue
		}
		for _, spec := range gd.Specs {
			if ts, ok := spec.(*ast.TypeSpec); ok && ts.Name.Name == name {
				doc := ts.Doc
				if doc == nil && len(gd.Specs) == 1 {
					doc = gd.Doc
				}
				d, _ := directive.Find(fset, doc, directiveKey)
				return d.Found
			}
		}
	}
	return false
}

// jsonName returns the name encoding/json gives f, and whether its json tag
// names it; without a name there, it is the Go name.
func jsonName(f scan.Field) (string, bool) {
	tag, ok := f.Tag.Lookup("json")
	if !ok || tag == "-" {
		return f.Name, false
	}
	if name, _, _ := strings.Cut(tag, ","); name != "" {
		return name, true
	}
	return f.Name, false
}

// fieldDiag reports err at f, naming the field after the problem, as in
// `unknown rule "maxx" (field Name)`.
func fieldDiag(f scan.Field, err error) diag.Diag {
	d := diag.Errorf(f.Pos, "%v (field %s)", err, f.Name)
	if e, ok := errors.AsType[*ruleError](err); ok && e.hint != "" {
		d = d.WithHints(e.hint)
	}
	return d
}
