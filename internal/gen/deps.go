package gen

import (
	"path/filepath"

	"github.com/go-kanna/internal/deps"
	"github.com/go-kanna/internal/packages"
	"github.com/go-kanna/internal/scan"
	"github.com/go-kanna/internal/validation"
)

// reportDeps tells what a run in dir reads and writes. A qualified rule= names a
// package the struct's file imports, by its import path.
func reportDeps(dir string, cfg config, patterns []string) (deps.Report, error) {
	set, err := packages.Load(patterns, packages.Config{Dir: dir, Tags: cfg.tags, Regenerated: []string{outputFile}})
	if err != nil {
		return deps.Report{}, err //nolint:wrapcheck // it says what could not be loaded
	}

	r := deps.Report{Packages: set.Paths()}
	found, _ := targets(set.Fset, scan.Structs(set.Packages))
	for _, t := range found {
		r.Writes = append(r.Writes, filepath.Join(filepath.Dir(t.Pos.Filename), outputFile))
		r.Declares = append(r.Declares, t.PkgPath+"."+t.Name+".Validate")
		r.Refs = append(r.Refs, validation.RuleRefs(t.Struct)...)
	}
	return r, nil
}
