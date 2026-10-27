package gen

import (
	"go/token"
	"go/types"
	"path/filepath"

	"github.com/go-kanna/internal/diag"
	"github.com/go-kanna/internal/directive"
	"github.com/go-kanna/internal/scan"
)

// directiveKey spells the directive //kanna:validate.
const directiveKey = "validate"

// outputFile is the file generated in each package.
const outputFile = "validate_gen.go"

// runtimePath is the import path of the package the generated code calls.
const runtimePath = "github.com/go-kanna/validate"

// target is a struct marked //kanna:validate.
type target struct {
	scan.Struct

	// hook reports whether the struct declares a validate(*validate.Errors)
	// method, which the generated Validate calls last.
	hook bool
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

// hookOf reports whether s declares a validate method for the generated
// Validate to call, and says why one by that name cannot be called.
//
// Only a method declared on s itself counts. One promoted from an embedded
// struct belongs to that struct's own Validate, which the generated code calls
// already.
func hookOf(fset *token.FileSet, s scan.Struct) (bool, []diag.Diag) {
	for m := range s.Named.Methods() {
		if m.Name() != "validate" {
			continue
		}
		sig := m.Signature()
		if sig.Params().Len() == 1 && sig.Results().Len() == 0 && isErrorsPointer(sig.Params().At(0).Type()) {
			return true, nil
		}
		return false, []diag.Diag{diag.Errorf(fset.Position(m.Pos()),
			"%s.validate must be func(*validate.Errors) for the generated Validate to call it", s.Name).
			WithHints("rename it if it is not meant to be called from Validate")}
	}
	return false, nil
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

func hasValidateTag(s scan.Struct) bool {
	for _, f := range s.Fields {
		if _, ok, _ := lookup(f.Tag); ok {
			return true
		}
	}
	return false
}
