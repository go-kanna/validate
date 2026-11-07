package gen

import (
	"fmt"
	"strconv"

	"github.com/go-kanna/internal/gofile"
	"github.com/go-kanna/internal/validation"
)

// emit renders the Validate methods of plans into a file of the package pkgName
// at pkgPath. declared are the top-level names the package declares outside
// the file, which nothing the file introduces may take.
func emit(pkgPath, pkgName string, plans []plan, declared []string) *gofile.File {
	f := gofile.New(validation.Generator, pkgPath, pkgName)
	f.Reserve(declared...)

	structs := make([]validation.Planned, len(plans))
	for i, p := range plans {
		fields := make([]validation.Field, len(p.fields))
		for j, fp := range p.fields {
			fields[j] = fp.Field
		}
		structs[i] = validation.Planned{Name: p.Name, Fields: fields}
	}
	w := validation.NewWriter(f, structs, declared)
	for _, p := range plans {
		method(f, w, p)
	}
	return f
}

// method writes the Validate method of one struct.
func method(f *gofile.File, w *validation.Writer, p plan) {
	s := f.Scope()
	x, errs := s.Name("x"), s.Name("errs")

	fmt.Fprintf(f, "// Validate checks %s against the validate tags of %s. It returns the\n", x, p.Name)
	fmt.Fprintf(f, "// %s.Errors of every rule %s breaks, or nil.\n", w.Runtime(), x)
	fmt.Fprintf(f, "func (%s %s) Validate() error {\n", x, p.Name)
	fmt.Fprintf(f, "var %s %s.Errors\n", errs, w.Runtime())

	body := w.Body(s, x, errs)
	for _, fp := range p.fields {
		path := strconv.Quote(fp.path)
		nestPath := path
		if fp.flatten {
			nestPath = `""`
		}
		fmt.Fprint(f, "\n")
		body.Value(fp.Value, x+"."+fp.Name, path, nestPath)
	}

	if p.hook != nil {
		fmt.Fprintf(f, "\n%s.validate(&%s)\n", x, errs)
	}
	fmt.Fprintf(f, "\nreturn %s.Err()\n}\n\n", errs)
}
