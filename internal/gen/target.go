package gen

import (
	"go/token"
	"go/types"
	"path/filepath"
	"reflect"

	"github.com/go-kanna/internal/diag"
	"github.com/go-kanna/internal/directive"
	"github.com/go-kanna/internal/scan"
	"github.com/go-kanna/internal/validation"
)

// directiveKey spells the directive //kanna:validate.
const directiveKey = validation.DirectiveKey

// configKey spells the directive //kanna:config of kanna-config.
const configKey = "config"

// outputFile is the file generated in each package.
const outputFile = validation.OutputFile

// generator names the tool in the header of the file it writes.
const generator = validation.Generator

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
// rules would never run, and nothing else would say so. One marked
// //kanna:config is not: kanna-config checks them where it reads the struct.
func targets(fset *token.FileSet, structs []scan.Struct) ([]target, []diag.Diag) {
	var (
		found []target
		diags []diag.Diag
	)
	for _, s := range structs {
		d, ds := directive.Find(fset, s.Doc, directiveKey)
		diags = append(diags, ds...)
		if !d.Found {
			if hasValidateTag(s) && !readByConfig(fset, s) {
				diags = append(diags, diag.Warningf(s.Pos,
					"%s has validate tags but no //kanna:%s, so they are never checked", s.Name, directiveKey))
			}
			continue
		}

		ds = checkTarget(fset, s, d)
		hook, hookDiags := validation.Hook(fset, s)
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
				if validation.HasTag(reflect.StructTag(u.Tag(i))) {
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
		if validation.HasTag(f.Tag) {
			return true
		}
	}
	return false
}

// readByConfig reports whether s is marked //kanna:config, whose validate tags
// kanna-config checks. kanna-config reports what is wrong with the mark.
func readByConfig(fset *token.FileSet, s scan.Struct) bool {
	d, _ := directive.Find(fset, s.Doc, configKey)
	return d.Found
}
