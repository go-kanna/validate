package gen

import (
	"flag"
	"go/token"
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
	set, err := packages.Load(patterns, packages.Config{Dir: dir, Tags: cfg.tags, Regenerated: []string{outputFile}})
	if err != nil {
		return nil, []diag.Diag{diag.Errorf(token.Position{}, "%v", err)}
	}
	if diag.HasErrors(set.Diags) {
		for _, path := range set.SetAside {
			set.Diags = append(set.Diags, diag.Warningf(token.Position{Filename: path},
				"set aside because it no longer compiles; it is regenerated once the errors above are fixed"))
		}
		return nil, set.Diags
	}

	changes, diags := generate(set)
	if !diag.HasErrors(diags) && !slices.ContainsFunc(changes, func(c output.Change) bool { return !c.Remove }) {
		// Nothing to stop for: a go:generate line left behind does no harm.
		in := strings.Join(patterns, " ")
		if in == "" {
			in = "."
		}
		diags = append(diags, diag.Warningf(token.Position{}, "no struct is marked //kanna:%s in %s", directiveKey, in))
	}
	return changes, append(set.Diags, diags...)
}

// generate works out the changes for the packages of set: the output of each
// package with targets, and the removal of what an earlier run left in a
// package without any.
func generate(set packages.Set) ([]output.Change, []diag.Diag) {
	found, diags := targets(set.Fset, scan.Structs(set.Packages))
	rewritten := make(map[string]bool, len(set.Packages))
	for _, pkg := range set.Packages {
		rewritten[pkg.PkgPath] = true
	}
	planned, ds := plans(set.Fset, found, rewritten)
	if diags = append(diags, ds...); diag.HasErrors(diags) {
		return nil, diags
	}

	byPkg := make(map[string][]plan)
	for _, p := range planned {
		byPkg[p.PkgPath] = append(byPkg[p.PkgPath], p)
	}

	var changes []output.Change
	for _, pkg := range set.Packages {
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
