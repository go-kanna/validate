package gen

import (
	"fmt"
	"go/token"
	"go/types"
	"maps"
	"slices"
	"strings"

	"github.com/go-kanna/internal/packages"
	"github.com/go-kanna/internal/scan"
	"github.com/go-kanna/internal/suggest"
)

// A qualified rule=, such as rule=model.CompletedAtRule, names its package as
// kanna-di's with= does: by its name, or by as much of its import path as it
// takes to tell two packages of one name apart. The struct's file need not
// import it, which Go would reject for an import that only a tag mentions; the
// generated file does.

// refScope is where a qualified rule= looks for its package.
type refScope struct {
	// pkgs are the packages it may name: those of the user's module that the
	// run loaded, and those the struct's package imports, directly or not.
	pkgs []*types.Package

	// module names every package of the module by import path once the run
	// has had to list them, so that a misspelled package can be suggested even
	// when nothing loaded it.
	module map[string]string
}

// splitRef splits ref, a type as rule= names it, into the qualifier naming its
// package, "" for the struct's own, and the type's name. The name follows the
// last dot, since an import path may hold dots of its own. It reports false
// for what cannot name a type.
func splitRef(ref string) (qual, name string, ok bool) {
	qual, name, qualified := strings.CutLast(ref, ".")
	if !qualified {
		return "", ref, token.IsIdentifier(ref)
	}
	if slices.Contains(strings.Split(qual, "/"), "") {
		return "", "", false
	}
	return qual, name, token.IsIdentifier(name)
}

// qualifies reports whether qual, as written before the type of a rule=, names
// the package at path declaring itself name.
func qualifies(qual, path, name string) bool {
	return path == qual || strings.HasSuffix(path, "/"+qual) || name == qual
}

// pick returns the one package that qual, written in ref, names in s.
func (s refScope) pick(ref, qual string) (*types.Package, error) {
	var matched []*types.Package
	for _, p := range s.pkgs {
		if qualifies(qual, p.Path(), p.Name()) &&
			!slices.ContainsFunc(matched, func(m *types.Package) bool { return m.Path() == p.Path() }) {
			matched = append(matched, p)
		}
	}

	switch len(matched) {
	case 1:
		return matched[0], nil
	case 0:
		err := ruleErrorf("rule=%s: no package %s in your module or among what the struct's package imports", ref, qual)
		names := slices.Collect(maps.Values(s.module))
		for _, p := range s.pkgs {
			names = append(names, p.Name())
		}
		slices.Sort(names)
		if name, ok := suggest.Closest(qual, slices.Compact(names)); ok {
			err.hint = fmt.Sprintf("did you mean %q?", name)
		}
		return nil, err
	}

	paths := make([]string, len(matched))
	for i, m := range matched {
		paths[i] = m.Path()
	}
	slices.Sort(paths)
	err := ruleErrorf("rule=%s: %s could be any of %s", ref, qual, strings.Join(paths, ", "))
	err.hint = "write as much of the import path as it takes to tell them apart"
	return nil, err
}

// unresolved returns the qualifiers of the rule= tags of the structs marked in
// pkgs that name no type among what is loaded: pkgs themselves, and what the
// struct's package imports. Their packages have to be looked for in the rest
// of the user's module.
func unresolved(fset *token.FileSet, pkgs []*packages.Package) []string {
	found, _ := targets(fset, scan.Structs(pkgs))
	roots := typesOf(pkgs)

	var quals []string
	for _, t := range found {
		loaded := append(slices.Clip(roots), reachable(t.Named.Obj().Pkg())...)
		for _, ref := range ruleRefs(t) {
			qual, name, ok := splitRef(ref)
			if !ok || qual == "" || slices.Contains(quals, qual) {
				continue
			}
			if !slices.ContainsFunc(loaded, func(p *types.Package) bool {
				_, isType := p.Scope().Lookup(name).(*types.TypeName)
				return isType && qualifies(qual, p.Path(), p.Name())
			}) {
				quals = append(quals, qual)
			}
		}
	}
	return quals
}

// unloaded returns, sorted, the packages of module that one of quals names and
// that are not among loaded.
func unloaded(loaded []*types.Package, module map[string]string, quals []string) []string {
	var extra []string
	for path, name := range module {
		if !slices.ContainsFunc(loaded, func(p *types.Package) bool { return p.Path() == path }) &&
			slices.ContainsFunc(quals, func(q string) bool { return qualifies(q, path, name) }) {
			extra = append(extra, path)
		}
	}
	slices.Sort(extra)
	return extra
}

// ruleRefs returns what the rule= tags of t name, as written.
func ruleRefs(t target) []string {
	var refs []string
	for _, f := range t.Fields {
		levels, _ := parseTag(f.Tag)
		for _, l := range levels {
			for _, r := range l.rules {
				if r.name == "rule" {
					refs = append(refs, r.param)
				}
			}
		}
	}
	return refs
}

// reachable returns pkgs and every package they import, directly or not, one
// per import path.
func reachable(pkgs ...*types.Package) []*types.Package {
	seen := make(map[string]bool)
	var out []*types.Package
	var walk func(p *types.Package)
	walk = func(p *types.Package) {
		if seen[p.Path()] {
			return
		}
		seen[p.Path()] = true
		out = append(out, p)
		for _, imp := range p.Imports() {
			walk(imp)
		}
	}
	for _, p := range pkgs {
		walk(p)
	}
	return out
}

// typesOf returns the types of pkgs.
func typesOf(pkgs []*packages.Package) []*types.Package {
	out := make([]*types.Package, len(pkgs))
	for i, p := range pkgs {
		out[i] = p.Types
	}
	return out
}
