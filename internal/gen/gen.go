package gen

import (
	"flag"
	"go/token"
	"go/types"
	"maps"
	"path/filepath"
	"slices"
	"strings"

	"github.com/go-kanna/internal/cli"
	"github.com/go-kanna/internal/diag"
	"github.com/go-kanna/internal/output"
	"github.com/go-kanna/internal/packages"
	"github.com/go-kanna/internal/scan"
)

// config holds what kanna-validate's own flags set.
type config struct {
	tags string
}

// Command returns kanna-validate.
func Command() cli.Command[config] {
	return cli.Command[config]{
		Name:    generator,
		Summary: "generate Validate methods from the validate tags of structs marked //kanna:" + directiveKey,
		Args:    "[packages]",
		Help: "The packages default to the one in the current directory, so that\n" +
			"//go:generate go tool " + generator + " needs no argument.",
		Flags: func(fs *flag.FlagSet, cfg *config) {
			fs.StringVar(&cfg.tags, "tags", "", "comma-separated build tags")
		},
		Run: run,
	}
}

func run(dir string, cfg config, patterns []string) ([]output.Change, []diag.Diag) {
	set, deferred, stop := load(dir, cfg.tags, patterns)
	if stop != nil {
		return nil, stop
	}
	roots := make(map[string]bool, len(set.Packages))
	for _, pkg := range set.Packages {
		roots[pkg.PkgPath] = true
	}

	// A qualified rule= may name a package of the module that nothing loaded
	// imports, as the struct's file need not, or one loaded only in part, as
	// complete explains. Listing the module takes the go command another run,
	// so it waits for such a rule. What the rule names then has to come from
	// the same load as the rest, or none of its types would be those the
	// structs know, so everything is loaded again together.
	var module map[string]string
	if quals := unresolved(set.Fset, set.Packages); len(quals) > 0 {
		var err error
		if module, err = modulePackages(dir, cfg.tags); err != nil {
			return nil, []diag.Diag{diag.Errorf(token.Position{}, "%v", err)}
		}
		if extra := unloaded(complete(typesOf(set.Packages)), module, quals); len(extra) > 0 {
			all := patterns
			if len(all) == 0 {
				all = []string{"."}
			}
			if set, deferred, stop = load(dir, cfg.tags, append(slices.Clip(all), extra...)); stop != nil {
				return nil, stop
			}
		}
	}

	changes, diags := generate(set, roots, module)
	if diag.HasErrors(diags) {
		return nil, stopped(set, roots, deferred, diags)
	}
	if rds := packages.Recheck(set, changes); len(rds) > 0 {
		return nil, slices.Concat(diags, rds, set.SetAsideWarnings())
	}
	if !slices.ContainsFunc(changes, func(c output.Change) bool { return !c.Remove }) {
		// Nothing to stop for: a go:generate line left behind does no harm. The
		// packages are named by path, as go generate ./... runs every one of
		// them in a "." of its own.
		in := slices.Sorted(maps.Keys(roots))
		if len(in) == 0 {
			in = patterns // which matched no package
		}
		diags = append(diags, diag.Warningf(token.Position{},
			"no struct is marked //kanna:%s in %s", directiveKey, strings.Join(in, ", ")))
	}
	return changes, diags
}

// load loads the packages matching patterns, and says why the run stops when
// it cannot go on with them. Code calling a Validate method does not compile
// before the run writes it, so a type error of a package loaded does not stop
// the run: it is deferred, for packages.Recheck to tell, once the output is in
// place, whether it remains.
func load(dir, tags string, patterns []string) (set packages.Set, deferred, stop []diag.Diag) {
	set, err := packages.Load(patterns, packages.Config{Dir: dir, Tags: tags, Regenerated: []string{outputFile}})
	if err != nil {
		return set, nil, []diag.Diag{diag.Errorf(token.Position{}, "%v", err)}
	}
	blocking, deferred := set.Split()
	if diag.HasErrors(blocking) {
		return set, nil, append(blocking, set.SetAsideWarnings()...)
	}
	return set, deferred, nil
}

// stopped returns what a run that cannot generate reports, leaving out the
// type errors that say only that a Validate method it writes is missing.
func stopped(set packages.Set, roots map[string]bool, deferred, diags []diag.Diag) []diag.Diag {
	pkgs := slices.DeleteFunc(slices.Clone(set.Packages), func(p *packages.Package) bool { return !roots[p.PkgPath] })
	found, _ := targets(set.Fset, scan.Structs(pkgs))
	written := make(map[string]bool, len(found))
	for _, t := range found {
		written[t.PkgPath+"."+t.Name+".Validate"] = true
	}
	return packages.Stopped(deferred, diags, set.Pending(written))
}

// generate works out the changes for the packages of set that roots names: the
// output of each package with targets, and the removal of what an earlier run
// left in a package without any. The rest of set is what a qualified rule= may
// name, and module the packages of the user's module, if the run listed them.
func generate(set packages.Set, roots map[string]bool, module map[string]string) ([]output.Change, []diag.Diag) {
	pkgs := slices.DeleteFunc(slices.Clone(set.Packages), func(p *packages.Package) bool { return !roots[p.PkgPath] })
	found, diags := targets(set.Fset, scan.Structs(pkgs))
	var local []*types.Package
	for _, p := range reachable(typesOf(set.Packages)...) {
		if _, ok := module[p.Path()]; ok || roots[p.Path()] {
			local = append(local, p)
		}
	}
	planned, ds := plans(set.Fset, found, roots, refScope{pkgs: local, module: module})
	if diags = append(diags, ds...); diag.HasErrors(diags) {
		return nil, diags
	}

	byPkg := make(map[string][]plan)
	for _, p := range planned {
		byPkg[p.PkgPath] = append(byPkg[p.PkgPath], p)
	}

	var changes []output.Change
	for _, pkg := range pkgs {
		if len(pkg.GoFiles) == 0 {
			continue
		}
		dir := filepath.Dir(pkg.GoFiles[0])
		path := filepath.Join(dir, outputFile)

		pkgPlans := byPkg[pkg.PkgPath]
		if len(pkgPlans) == 0 {
			// A package whose structs have all lost the directive keeps the
			// methods an earlier run gave them unless the file goes as well.
			if packages.GeneratedFiles(pkg)[path] {
				changes = append(changes, output.Change{Path: path, Remove: true})
			}
			continue
		}

		declared := slices.Sorted(maps.Keys(output.DeclaredNames(dir, outputFile, pkg.Name)))
		f := emit(pkg.PkgPath, pkg.Name, pkgPlans, declared)
		src, err := f.Bytes()
		if err != nil {
			return nil, append(diags, diag.Errorf(token.Position{Filename: path}, "generated code does not parse: %v", err))
		}
		changes = append(changes, output.Change{Path: path, Data: src})
		diags = append(diags, output.MissingRequires(path, f.ModuleImports())...)
	}
	return changes, diags
}
