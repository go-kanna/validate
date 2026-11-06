package gen

import (
	"go/token"
	"go/types"
	"path/filepath"
	"reflect"

	"github.com/go-kanna/internal/diag"
	"github.com/go-kanna/internal/directive"
	"github.com/go-kanna/internal/scan"
)

// directiveKey spells the directive //kanna:validate.
const directiveKey = "validate"

// enumKey spells the directive //kanna:enum of kanna-value.
const enumKey = "enum"

// outputFile is the file generated in each package.
const outputFile = "validate_gen.go"

// valueOutputFile is the file kanna-value generates in each package.
const valueOutputFile = "value_gen.go"

// valueGenerator names kanna-value in the header of that file.
const valueGenerator = "kanna-value"

// runtimePath is the import path of the package the generated code calls.
const runtimePath = "github.com/go-kanna/validate"

// target is a struct marked //kanna:validate.
type target struct {
	scan.Struct

	// hook is the validate(*validate.Errors) method the struct declares, which
	// the generated Validate calls last, or nil.
	hook *types.Func
}

// targets returns the structs marked //kanna:validate that a Validate method
// can be generated for, in the order scan reports them, and says what is wrong
// with the rest.
//
// A struct carrying validate tags without the directive is warned about: its
// rules would never run, and nothing else would say so.
func targets(fset *token.FileSet, structs []scan.Struct) ([]target, []diag.Diag) {
	var (
		found []target
		diags []diag.Diag
	)
	for _, s := range structs {
		d, ds := directive.Find(fset, s.Doc, directiveKey)
		diags = append(diags, ds...)
		if !d.Found {
			if hasValidateTag(s) {
				diags = append(diags, diag.Warningf(s.Pos,
					"%s has validate tags but no //kanna:%s, so they are never checked", s.Name, directiveKey))
			}
			continue
		}

		ds = checkTarget(fset, s, d)
		hook, hookDiags := hookOf(fset, s)
		diags = append(append(diags, ds...), hookDiags...)
		if len(ds) == 0 && len(hookDiags) == 0 {
			found = append(found, target{Struct: s, hook: hook})
		}
	}
	return found, diags
}

// checkTarget says why s, marked by d, cannot have a Validate method generated
// for it.
func checkTarget(fset *token.FileSet, s scan.Struct, d directive.Directive) []diag.Diag {
	var diags []diag.Diag

	if d.Args != "" {
		diags = append(diags, diag.Errorf(d.Pos, "//kanna:%s takes no arguments, got %q", directiveKey, d.Args))
	}

	// A method on a generic type would need its type parameters, and a rule
	// written against a type parameter cannot be checked until it is
	// instantiated.
	if s.Named.TypeParams().Len() > 0 {
		diags = append(diags, diag.Errorf(s.Pos, "generic struct %s is not supported", s.Name))
	}

	for _, f := range s.Fields {
		if f.Name == "Validate" {
			diags = append(diags, diag.Errorf(f.Pos,
				"%s has a field named Validate, which the generated method would collide with", s.Name))
		}
		diags = append(diags, unnamedTags(fset, f.Name, f.Type)...)
	}

	// The Validate an earlier run generated is about to be replaced; one
	// written by hand would be declared twice.
	for m := range s.Named.Methods() {
		if pos := fset.Position(m.Pos()); m.Name() == "Validate" && filepath.Base(pos.Filename) != outputFile {
			diags = append(diags, diag.Errorf(pos, "%s already has a Validate method", s.Name).
				WithHints("remove it, or the //kanna:"+directiveKey+" on "+s.Name))
		}
	}

	return diags
}

// hookOf returns the validate method s declares for the generated Validate to
// call, or nil, and says why one by that name cannot be called.
//
// Only a method declared on s itself counts. One promoted from an embedded
// struct belongs to that struct's own Validate, which the generated code calls
// already.
func hookOf(fset *token.FileSet, s scan.Struct) (*types.Func, []diag.Diag) {
	for m := range s.Named.Methods() {
		if m.Name() != "validate" {
			continue
		}
		sig := m.Signature()
		if sig.Params().Len() == 1 && sig.Results().Len() == 0 && isErrorsPointer(sig.Params().At(0).Type()) {
			return m, nil
		}
		return nil, []diag.Diag{diag.Errorf(fset.Position(m.Pos()),
			"%s.validate must be func(*validate.Errors) for the generated Validate to call it", s.Name).
			WithHints("rename it if it is not meant to be called from Validate")}
	}
	return nil, nil
}

// isErrorsPointer reports whether t is *validate.Errors of the runtime.
func isErrorsPointer(t types.Type) bool {
	ptr, ok := t.(*types.Pointer)
	if !ok {
		return false
	}
	named, ok := types.Unalias(ptr.Elem()).(*types.Named)
	return ok && named.Obj().Name() == "Errors" && named.Obj().Pkg() != nil && named.Obj().Pkg().Path() == runtimePath
}

// unnamedTags reports the validate tags of the structs without a name that a
// field at path holds, through pointers, slices, arrays, and maps: no Validate
// is generated for one, so nothing would check them.
func unnamedTags(fset *token.FileSet, path string, t types.Type) []diag.Diag {
	for {
		switch u := types.Unalias(t).(type) {
		case *types.Pointer:
			t = u.Elem()
		case *types.Slice:
			t = u.Elem()
		case *types.Array:
			t = u.Elem()
		case *types.Map:
			t = u.Elem()
		case *types.Struct:
			var diags []diag.Diag
			for i := range u.NumFields() {
				v, at := u.Field(i), path+"."+u.Field(i).Name()
				if _, ok, _ := lookup(reflect.StructTag(u.Tag(i))); ok {
					diags = append(diags, diag.Errorf(fset.Position(v.Pos()),
						"%s has a validate tag, but a struct without a name gets no Validate, so it is never checked", at).
						WithHints("declare the struct as a type of its own, marked //kanna:"+directiveKey))
				}
				diags = append(diags, unnamedTags(fset, at, v.Type())...)
			}
			return diags
		default:
			return nil
		}
	}
}

func hasValidateTag(s scan.Struct) bool {
	for _, f := range s.Fields {
		if _, ok, _ := lookup(f.Tag); ok {
			return true
		}
	}
	return false
}
