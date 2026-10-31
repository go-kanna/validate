package gen_test

import (
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/go-kanna/internal/deps"
)

// Code calling what the run writes does not compile before it, which is when
// the kanna command asks.
func TestDeps(t *testing.T) {
	t.Parallel()

	dir := writeModule(t, map[string]string{
		"p/p.go": "package p\n\n//kanna:validate\ntype In struct {\n\tA string `validate:\"rule=model.Email\"`\n" +
			"\tB string `validate:\"required\"`\n}\n\nvar _ = In{}.Validate()\n",
	})

	code, stdout, stderr := run(t, filepath.Join(dir, "p"), "-deps")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	var r deps.Report
	if err := json.Unmarshal([]byte(stdout), &r); err != nil {
		t.Fatal(err)
	}

	if !slices.Equal(r.Packages, []string{"example.com/app/p"}) {
		t.Errorf("Packages = %q", r.Packages)
	}
	if len(r.Writes) != 1 || !strings.HasSuffix(r.Writes[0], filepath.Join("p", "validate_gen.go")) {
		t.Errorf("Writes = %q", r.Writes)
	}
	if !slices.Equal(r.Declares, []string{"example.com/app/p.In.Validate"}) {
		t.Errorf("Declares = %q", r.Declares)
	}
	want := []deps.Ref{{Package: "model", Via: "rule=model.Email"}}
	if !slices.Equal(r.Refs, want) {
		t.Errorf("Refs = %+v, want %+v", r.Refs, want)
	}
}
