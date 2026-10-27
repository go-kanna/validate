package gen_test

import (
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/go-kanna/internal/output"
	"github.com/go-kanna/internal/packages"
	"github.com/go-kanna/internal/pkgtest"

	"github.com/go-kanna/validate/internal/gen"
)

// TestEmit compares the output for each testdata package with its golden file.
// To accept a change in the output:
//
//	go test ./internal/gen -run TestEmit -update
//
// testdata/emit keeps its golden file as validate_gen.go, a part of the package
// that TestGenerated compiles and calls. testdata/format calls the rules of
// go-kanna/validated, which this module does not require, so its golden file
// is only compared.
func TestEmit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		dir    string
		golden string
	}{
		{dir: "testdata/emit", golden: "validate_gen.go"},
		{dir: "testdata/format", golden: "validate_gen.go.golden"},
	}

	for _, tt := range tests {
		t.Run(tt.dir, func(t *testing.T) {
			t.Parallel()

			set, err := packages.Load([]string{"./" + tt.dir}, packages.Config{Regenerated: []string{"validate_gen.go"}})
			if err != nil || len(set.Diags) > 0 {
				t.Fatalf("load: %v %v", err, set.Diags)
			}
			dir, err := filepath.Abs(tt.dir)
			if err != nil {
				t.Fatal(err)
			}
			declared := slices.Sorted(maps.Keys(output.DeclaredNames(dir, "validate_gen.go", set.Packages[0].Name)))

			got, err := gen.Emit(set.Fset, set.Packages, declared)
			if err != nil {
				t.Fatalf("emit: %v\n%s", err, got)
			}
			pkgtest.Golden(t, filepath.Join(tt.dir, tt.golden), got)
		})
	}
}

func TestEmit_KeepsClearOfDeclaredNames(t *testing.T) {
	t.Parallel()

	pkg := pkgtest.Load(t, map[string]string{"test.go": `package test

//kanna:validate
type Target struct {
	Tags  []string ` + "`validate:\"dive,max=3\"`" + `
	Quote string   "validate:\"pattern=a` + "`" + `b\""
}
`})

	got, err := gen.Emit(pkg.Fset, []*packages.Package{pkg}, []string{"x", "errs", "i", "validate", "patternTargetQuote"})
	if err != nil {
		t.Fatalf("emit: %v\n%s", err, got)
	}

	for _, want := range []string{
		"func (x2 Target) Validate() error {",
		"var errs2 validate2.Errors",
		"for i2 := range x2.Tags {",
		"patternTargetQuote2 = regexp.MustCompile(\"a`b\")",
	} {
		if !strings.Contains(string(got), want) {
			t.Errorf("output does not contain %q:\n%s", want, got)
		}
	}
}
