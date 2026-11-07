package gen

import (
	"fmt"
	"go/token"

	"github.com/go-kanna/internal/packages"
	"github.com/go-kanna/internal/scan"
)

// Targets finds the targets in pkgs, naming each, with " (hook)" after the name
// of one that has a hook, and spelling each diagnostic as diag.Diag.String
// does.
func Targets(fset *token.FileSet, pkgs []*packages.Package) ([]string, []string) {
	found, diags := targets(fset, scan.Structs(pkgs))

	names := make([]string, len(found))
	for i, t := range found {
		names[i] = t.Name
		if t.hook != nil {
			names[i] += " (hook)"
		}
	}
	msgs := make([]string, len(diags))
	for i, d := range diags {
		msgs[i] = d.String()
	}
	return names, msgs
}

// Emit renders the file the targets in pkgs, which make one package, are
// generated into. declared are the top-level names the package declares
// besides.
func Emit(fset *token.FileSet, pkgs []*packages.Package, declared []string) ([]byte, error) {
	found, diags := targets(fset, scan.Structs(pkgs))
	planned, ds := plans(fset, found, map[string]bool{pkgs[0].PkgPath: true})
	if diags = append(diags, ds...); len(diags) > 0 {
		return nil, fmt.Errorf("%v", diags)
	}
	return emit(pkgs[0].PkgPath, pkgs[0].Name, planned, declared).Bytes() //nolint:wrapcheck // the test shows it as it is
}

// Plans plans the targets in pkgs, all of whose output the run rewrites. It
// spells the plan of each struct, by name, as a line per field with something
// to check, naming the path its errors go under, such as `Name(name)`, and
// `Audit(Audit,flatten)` for an embedded struct whose own errors stay at the
// level of the struct, and each diagnostic as diag.Diag.String does. What each
// field checks is package validation's to test.
func Plans(fset *token.FileSet, pkgs []*packages.Package) (map[string][]string, []string) {
	found, diags := targets(fset, scan.Structs(pkgs))
	rewritten := make(map[string]bool, len(pkgs))
	for _, pkg := range pkgs {
		rewritten[pkg.PkgPath] = true
	}
	planned, ds := plans(fset, found, rewritten)
	diags = append(diags, ds...)

	spelled := make(map[string][]string, len(planned))
	for _, p := range planned {
		lines := []string{}
		for _, f := range p.fields {
			path := f.path
			if f.flatten {
				path += ",flatten"
			}
			lines = append(lines, f.Name+"("+path+")")
		}
		spelled[p.Name] = lines
	}
	msgs := make([]string, len(diags))
	for i, d := range diags {
		msgs[i] = d.String()
	}
	return spelled, msgs
}
