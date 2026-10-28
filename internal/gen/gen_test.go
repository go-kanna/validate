package gen_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-kanna/internal/cli"
	"github.com/go-kanna/internal/packages"

	"github.com/go-kanna/validate/internal/gen"
)

// TestMain loads every module the tests write outside any workspace. A
// GOWORK pinned by the Makefile or the environment would otherwise put them
// outside the modules it lists, which the go command refuses to load.
func TestMain(m *testing.M) {
	// Before any test runs: t.Setenv would rule out t.Parallel.
	if err := os.Setenv("GOWORK", "off"); err != nil {
		panic(err)
	}
	os.Exit(m.Run())
}

// writeModule writes files into a fresh module and returns its directory. The
// module resolves github.com/go-kanna/validate to this checkout, which the
// generated code imports, and borrows its go.sum so that nothing is fetched.
func writeModule(t *testing.T, files map[string]string) string {
	t.Helper()

	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	sum, err := os.ReadFile(filepath.Join(root, "go.sum")) //nolint:gosec // this checkout's own go.sum
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	files["go.mod"] = "module example.com/app\n\ngo 1.27.0\n\n" +
		"require github.com/go-kanna/validate v0.0.0\n\n" +
		"replace github.com/go-kanna/validate => " + root + "\n"
	files["go.sum"] = string(sum)
	for name, content := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		writeFile(t, path, content)
	}
	return dir
}

// run runs kanna-validate in dir and returns its exit code, stdout, and stderr.
func run(t *testing.T, dir string, args ...string) (int, string, string) {
	t.Helper()

	var stdout, stderr bytes.Buffer
	code := gen.Command().Exec(cli.Env{Out: &stdout, Err: &stderr, Dir: dir}, args)
	return code, stdout.String(), stderr.String()
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()

	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()

	b, err := os.ReadFile(path) //nolint:gosec // a file of the test's own module
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

const signupSrc = `package app

import "github.com/go-kanna/validate"

//kanna:validate
type Signup struct {
	Name string ` + "`json:\"name\" validate:\"required,max=50\"`" + `
}

func (s Signup) validate(errs *validate.Errors) {
	if s.Name == "root" {
		errs.Add("name", validate.Fail("reserved"))
	}
}
`

func TestCommand_Generates(t *testing.T) {
	t.Parallel()

	dir := writeModule(t, map[string]string{"app.go": signupSrc})
	path := filepath.Join(dir, "validate_gen.go")

	code, stdout, stderr := run(t, dir)
	if code != cli.ExitOK || stdout != "" || stderr != "" {
		t.Fatalf("run = %d, stdout %q, stderr %q; want success in silence", code, stdout, stderr)
	}
	if got := readFile(t, path); !strings.Contains(got, "func (x Signup) Validate() error {") ||
		!strings.Contains(got, "x.validate(&errs)") {
		t.Errorf("validate_gen.go does not declare Validate calling the hook:\n%s", got)
	}

	// The output compiles against the runtime it imports.
	set, err := packages.Load([]string{"./..."}, packages.Config{Dir: dir})
	if err != nil || len(set.Diags) > 0 {
		t.Errorf("generated package does not load: %v %v", err, set.Diags)
	}

	code, stdout, stderr = run(t, dir, "-check", "-v")
	if code != cli.ExitOK || stdout != "" || stderr != "" {
		t.Errorf("-check right after generating = %d, stdout %q, stderr %q; want success in silence", code, stdout, stderr)
	}
	if code, stdout, _ := run(t, dir, "-v"); code != cli.ExitOK || stdout != "unchanged ./validate_gen.go\n" {
		t.Errorf("-v again = %d, stdout %q; want the file listed as unchanged", code, stdout)
	}
}

func TestCommand_GeneratesEveryPackage(t *testing.T) {
	t.Parallel()

	other := strings.Replace(signupSrc, "package app", "package other", 1)
	dir := writeModule(t, map[string]string{"app.go": signupSrc, "other/other.go": other})

	code, stdout, stderr := run(t, dir, "-v", "./...")
	if code != cli.ExitOK {
		t.Fatalf("run = %d, stderr %q", code, stderr)
	}
	if want := "wrote ./validate_gen.go\nwrote other/validate_gen.go\n"; stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
}

func TestCommand_Check(t *testing.T) {
	t.Parallel()

	dir := writeModule(t, map[string]string{"app.go": signupSrc})
	path := filepath.Join(dir, "validate_gen.go")

	code, _, stderr := run(t, dir, "-check")
	if want := "./validate_gen.go: error: not generated yet; run go generate\n"; code != cli.ExitFailure ||
		stderr != want {
		t.Errorf("-check before generating = %d, stderr %q; want %q", code, stderr, want)
	}

	if code, _, stderr := run(t, dir); code != cli.ExitOK {
		t.Fatalf("run = %d, stderr %q", code, stderr)
	}
	before := readFile(t, path)
	writeFile(t, filepath.Join(dir, "app.go"), strings.Replace(signupSrc, "max=50", "max=40", 1))

	code, _, stderr = run(t, dir, "-check")
	if want := "./validate_gen.go: error: out of date; run go generate\n"; code != cli.ExitFailure || stderr != want {
		t.Errorf("-check after a change = %d, stderr %q; want %q", code, stderr, want)
	}
	if readFile(t, path) != before {
		t.Error("-check rewrote validate_gen.go")
	}
}

func TestCommand_RemovesLeftover(t *testing.T) {
	t.Parallel()

	dir := writeModule(t, map[string]string{"app.go": signupSrc})
	path := filepath.Join(dir, "validate_gen.go")
	if code, _, stderr := run(t, dir); code != cli.ExitOK {
		t.Fatalf("run = %d, stderr %q", code, stderr)
	}
	writeFile(t, filepath.Join(dir, "app.go"), "package app\n\ntype Signup struct{ Name string }\n")
	const nothing = "warning: no struct is marked //kanna:validate in example.com/app\n"

	code, _, stderr := run(t, dir, "-check")
	want := nothing + "./validate_gen.go: error: left over from an earlier run; run go generate\n"
	if code != cli.ExitFailure || stderr != want {
		t.Errorf("-check = %d, stderr %q; want %q", code, stderr, want)
	}

	code, stdout, stderr := run(t, dir, "-v")
	if code != cli.ExitOK || stdout != "removed ./validate_gen.go\n" || stderr != nothing {
		t.Errorf("run = %d, stdout %q, stderr %q; want the removal and a warning", code, stdout, stderr)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("validate_gen.go still exists: %v", err)
	}

	// Once the file is gone, a run says the same and succeeds the same.
	if code, _, stderr := run(t, dir); code != cli.ExitOK || stderr != nothing {
		t.Errorf("run again = %d, stderr %q; want success with the warning", code, stderr)
	}
}

// Renaming a field leaves the earlier output referring to one that is gone,
// which must not stop the run that would fix it.
func TestCommand_RegeneratesOverStaleOutput(t *testing.T) {
	t.Parallel()

	dir := writeModule(t, map[string]string{"app.go": signupSrc})
	if code, _, stderr := run(t, dir); code != cli.ExitOK {
		t.Fatalf("run = %d, stderr %q", code, stderr)
	}
	writeFile(t, filepath.Join(dir, "app.go"), strings.ReplaceAll(signupSrc, "Name", "FullName"))

	if code, _, stderr := run(t, dir); code != cli.ExitOK {
		t.Fatalf("run over stale output = %d, stderr %q", code, stderr)
	}
	if got := readFile(t, filepath.Join(dir, "validate_gen.go")); !strings.Contains(got, "x.FullName") {
		t.Errorf("validate_gen.go was not regenerated:\n%s", got)
	}
}

// Setting a stale file aside only helps with the errors it caused. Anything
// else still fails the run, which then says why the file was set aside.
func TestCommand_SaysWhatWasSetAside(t *testing.T) {
	t.Parallel()

	dir := writeModule(t, map[string]string{"app.go": signupSrc})
	if code, _, stderr := run(t, dir); code != cli.ExitOK {
		t.Fatalf("run = %d, stderr %q", code, stderr)
	}
	// Renaming the struct breaks the receivers of the generated methods, so the
	// whole file is set aside, and the unrelated error remains.
	renamed := strings.ReplaceAll(signupSrc, "Signup", "Account") + "\nvar broken int = \"x\"\n"
	writeFile(t, filepath.Join(dir, "app.go"), renamed)

	code, _, stderr := run(t, dir)
	if code != cli.ExitFailure {
		t.Fatalf("run = %d, want %d; stderr %q", code, cli.ExitFailure, stderr)
	}
	for _, want := range []string{
		`./app.go:16:18: error: cannot use "x"`,
		"./validate_gen.go: warning: set aside because it no longer compiles; " +
			"it is regenerated once the errors above are fixed",
	} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr = %q, want it to contain %q", stderr, want)
		}
	}
}

func TestCommand_Failures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		files map[string]string
		want  string
	}{
		{
			name:  "rule error",
			files: map[string]string{"app.go": strings.Replace(signupSrc, "max=50", "maxx=50", 1)},
			want:  "./app.go:7:2: error: unknown rule \"maxx\" (field Name)\n  did you mean \"max\"?\n",
		},
		{
			name:  "directive with arguments",
			files: map[string]string{"app.go": strings.Replace(signupSrc, "//kanna:validate", "//kanna:validate strict", 1)},
			want:  "./app.go:5:1: error: //kanna:validate takes no arguments, got \"strict\"\n",
		},
		{
			name:  "type error elsewhere",
			files: map[string]string{"app.go": signupSrc + "\nvar broken int = \"x\"\n"},
			want:  `./app.go:16:18: error: cannot use "x"`,
		},
		{
			name:  "hand-written output",
			files: map[string]string{"app.go": signupSrc, "validate_gen.go": "package app\n"},
			want:  "./validate_gen.go: error: has no generated-code header, so it looks hand-written",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := writeModule(t, tt.files)
			code, _, stderr := run(t, dir)
			if code != cli.ExitFailure || !strings.Contains(stderr, tt.want) {
				t.Errorf("run = %d, stderr %q; want %d and %q", code, stderr, cli.ExitFailure, tt.want)
			}
		})
	}
}

func TestCommand_Warnings(t *testing.T) {
	t.Parallel()

	src := signupSrc + `
type Profile struct {
	Bio     string ` + "`validate:\"max=200\"`" + `
	Website string ` + "`validate:\"url\"`" + `
}

//kanna:validate
type Contact struct {
	Website string ` + "`json:\"website\" validate:\"url\"`" + `
}
`
	dir := writeModule(t, map[string]string{"app.go": src})

	code, _, stderr := run(t, dir)
	if code != cli.ExitOK {
		t.Fatalf("run = %d, stderr %q", code, stderr)
	}
	for _, want := range []string{
		"./app.go:16:6: warning: Profile has validate tags but no //kanna:validate, so they are never checked\n",
		"./validate_gen.go: warning: imports github.com/go-kanna/validated/rules, which go.mod does not require; " +
			"run go mod tidy\n",
	} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr = %q, want it to contain %q", stderr, want)
		}
	}
}

func TestCommand_WarnsWhenNothingIsMarked(t *testing.T) {
	t.Parallel()

	const plain = "package app\n\ntype Signup struct{ Name string }\n"
	tests := []struct {
		name  string
		files map[string]string
		args  []string
		want  string
	}{
		{
			name:  "the package in the directory",
			files: map[string]string{"app.go": plain},
			want:  "warning: no struct is marked //kanna:validate in example.com/app\n",
		},
		{
			name: "every package",
			files: map[string]string{
				"app.go":         plain,
				"other/other.go": strings.Replace(plain, "package app", "package other", 1),
			},
			args: []string{"./..."},
			want: "warning: no struct is marked //kanna:validate in example.com/app, example.com/app/other\n",
		},
		{
			name:  "patterns matching no package",
			files: map[string]string{},
			args:  []string{"./..."},
			want:  "warning: no struct is marked //kanna:validate in ./...\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := writeModule(t, tt.files)
			if code, _, stderr := run(t, dir, tt.args...); code != cli.ExitOK || stderr != tt.want {
				t.Errorf("run = %d, stderr %q; want success with %q", code, stderr, tt.want)
			}
		})
	}
}

func TestCommand_Tags(t *testing.T) {
	t.Parallel()

	tagged := "//go:build special\n\n" + signupSrc
	dir := writeModule(t, map[string]string{"app.go": tagged, "doc.go": "package app\n"})

	if code, _, stderr := run(t, dir); code != cli.ExitOK || !strings.Contains(stderr, "no struct is marked") {
		t.Errorf("run without the tag = %d, stderr %q; want success with nothing marked", code, stderr)
	}
	if code, stdout, stderr := run(t, dir, "-v", "-tags", "special"); code != cli.ExitOK ||
		stdout != "wrote ./validate_gen.go\n" {
		t.Errorf("run with the tag = %d, stdout %q, stderr %q; want the file written", code, stdout, stderr)
	}
}

func TestCommand_LoadFailure(t *testing.T) {
	t.Parallel()

	code, _, stderr := run(t, filepath.Join(t.TempDir(), "missing"))
	if code != cli.ExitFailure || stderr == "" {
		t.Errorf("run = %d, stderr %q; want a failure to load", code, stderr)
	}
}

func TestCommand_Usage(t *testing.T) {
	t.Parallel()

	code, stdout, _ := run(t, "", "-h")
	if code != cli.ExitOK {
		t.Errorf("-h = %d, want %d", code, cli.ExitOK)
	}
	for _, want := range []string{
		"kanna-validate: generate Validate methods from the validate tags of structs marked //kanna:validate",
		"kanna-validate [flags] [packages]",
		"//go:generate go tool kanna-validate needs no argument",
		"-tags string",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("usage = %q, want it to contain %q", stdout, want)
		}
	}
}
