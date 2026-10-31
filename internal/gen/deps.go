package gen

import (
	"path/filepath"

	"github.com/go-kanna/internal/deps"
	"github.com/go-kanna/internal/packages"
	"github.com/go-kanna/internal/scan"
)

// reportDeps tells what a run in dir reads and writes. A qualified rule= names a
// package no import has to say anything of.
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
		for _, ref := range ruleRefs(t) {
			if qual, _, ok := splitRef(ref); ok && qual != "" {
				r.Refs = append(r.Refs, deps.Ref{Package: qual, Via: "rule=" + ref})
			}
		}
	}
	return r, nil
}
