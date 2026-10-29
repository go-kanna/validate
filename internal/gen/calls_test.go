package gen_test

import (
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-kanna/internal/cli"
	"github.com/go-kanna/internal/packages"
)

// nameSrc marks Signup, whose Validate the code below calls.
const nameSrc = "package app\n\n//kanna:validate\ntype Signup struct {\n" +
	"\tName string `json:\"name\" validate:\"required,max=50\"`\n}\n"

// checkSrc calls the Validate of Signup.
const checkSrc = "package app\n\nfunc Check(s Signup) error {\n\treturn s.Validate()\n}\n"

// compiles reports whether the packages of dir load without errors.
func compiles(t *testing.T, dir string) {
	t.Helper()

	set, err := packages.Load([]string{"./..."}, packages.Config{Dir: dir})
	if err != nil || len(set.Diags) > 0 {
		t.Errorf("the module does not compile: %v %v", err, set.Diags)
	}
}

func TestCommand_GeneratesWhatCodeAlreadyCalls(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		files map[string]string
		args  []string
	}{
		{name: "a call", files: map[string]string{"handler.go": checkSrc}},
		{
			name: "an interface, a constraint, and method expressions",
			files: map[string]string{"shapes.go": `package app

type validator interface{ Validate() error }

var (
	_ validator = Signup{}
	_ validator = (*Signup)(nil)
)

func check[T interface{ Validate() error }](T) {}

func use() {
	check(Signup{})
	_ = Signup.Validate
	_ = (*Signup).Validate
}
`},
		},
		{
			name: "a method promoted",
			files: map[string]string{
				"outer.go": "package app\n\ntype Outer struct{ Signup }\n\nfunc use() error { return Outer{}.Validate() }\n",
			},
		},
		{
			name: "another package",
			files: map[string]string{
				"api/api.go": "package api\n\nimport \"example.com/app\"\n\n" +
					"func Check(s app.Signup) error { return s.Validate() }\n",
			},
			args: []string{"./..."},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			files := map[string]string{"app.go": nameSrc}
			maps.Copy(files, tt.files)
			dir := writeModule(t, files)
			if code, stdout, stderr := run(t, dir, tt.args...); code != cli.ExitOK || stdout != "" || stderr != "" {
				t.Fatalf("exit %d, stdout %q, stderr %q; want success", code, stdout, stderr)
			}
			compiles(t, dir)
		})
	}
}

func TestCommand_Mistakes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		files map[string]string
		want  string
	}{
		{
			// Before Validate is written, go/types checks no further than
			// s.Validate.
			name: "mistakes behind calls to what is generated",
			files: map[string]string{"handler.go": "package app\n\nfunc takesInt(int) {}\n\n" +
				"func Chained(s Signup) {\n\t_ = s.Validate().Bogus()\n}\n\n" +
				"func Passed(s Signup) {\n\ttakesInt(s.Validate())\n}\n"},
			want: "./handler.go:6:19: error: s.Validate().Bogus undefined (type error has no field or method Bogus)\n" +
				"./handler.go:10:11: error: cannot use s.Validate() (value of interface type error) as int value " +
				"in argument to takesInt\n",
		},
		{
			name:  "an error not about what is generated",
			files: map[string]string{"handler.go": checkSrc + "\nvar _ int = \"x\"\n"},
			want: "./handler.go:7:13: error: cannot use \"x\" (untyped string constant) as int value " +
				"in variable declaration\n",
		},
		{
			name: "an error alongside one of generation",
			files: map[string]string{
				"app.go":     strings.Replace(nameSrc, "required,max=50", "required,bogus", 1),
				"handler.go": checkSrc + "\nvar _ int = \"x\"\n",
			},
			want: "./handler.go:7:13: error: cannot use \"x\" (untyped string constant) as int value in variable declaration\n" +
				"./app.go:5:2: error: unknown rule \"bogus\" (field Name)\n",
		},
		{
			// What rule applies to an invalid type is left to the type error.
			name:  "a field that does not type-check",
			files: map[string]string{"app.go": strings.Replace(nameSrc, "Name string", "Name strng", 1), "handler.go": checkSrc},
			want:  "./app.go:5:7: error: undefined: strng\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			files := map[string]string{"app.go": nameSrc}
			maps.Copy(files, tt.files)
			dir := writeModule(t, files)
			if code, _, stderr := run(t, dir); code != cli.ExitFailure || stderr != tt.want {
				t.Errorf("exit %d, stderr:\n%s\nwant %d and\n%s", code, stderr, cli.ExitFailure, tt.want)
			}
			if _, err := os.Stat(filepath.Join(dir, "validate_gen.go")); err == nil {
				t.Error("validate_gen.go was written")
			}
		})
	}
}

func TestCommand_KeepsWhatCodeStillCalls(t *testing.T) {
	t.Parallel()

	const login = "\n//kanna:validate\ntype Login struct {\n\tName string `json:\"name\" validate:\"required\"`\n}\n"
	tests := []struct {
		name, app, file, src, edit string
		args                       []string
		want                       string
	}{
		{
			name: "the directive goes", app: nameSrc, file: "handler.go", src: checkSrc, edit: "//kanna:validate\n",
			want: "./app.go:3:6: warning: Signup has validate tags but no //kanna:validate, so they are never checked\n" +
				"./handler.go:4:11: error: s.Validate undefined (type Signup has no field or method Validate)\n",
		},
		{
			name: "the directive of one of two goes", app: nameSrc + login, file: "handler.go",
			src:  "package app\n\nfunc Check(s Signup, l Login) error {\n\t_ = s.Validate()\n\treturn l.Validate()\n}\n",
			edit: "//kanna:validate\ntype Login",
			want: "./app.go:8:6: warning: Login has validate tags but no //kanna:validate, so they are never checked\n" +
				"./handler.go:5:11: error: l.Validate undefined (type Login has no field or method Validate)\n",
		},
		{
			name: "a caller in another package", app: nameSrc, file: "api/api.go",
			src:  "package api\n\nimport \"example.com/app\"\n\nfunc Check(s app.Signup) error { return s.Validate() }\n",
			edit: "//kanna:validate\n", args: []string{"./..."},
			want: "./app.go:3:6: warning: Signup has validate tags but no //kanna:validate, so they are never checked\n" +
				"api/api.go:5:43: error: s.Validate undefined (type app.Signup has no field or method Validate)\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := writeModule(t, map[string]string{"app.go": tt.app, tt.file: tt.src})
			if code, _, stderr := run(t, dir, tt.args...); code != cli.ExitOK {
				t.Fatalf("first run: exit %d, stderr %q", code, stderr)
			}
			generated := readFile(t, filepath.Join(dir, "validate_gen.go"))

			with := strings.TrimPrefix(tt.edit, "//kanna:validate\n")
			writeFile(t, filepath.Join(dir, "app.go"), strings.Replace(tt.app, tt.edit, with, 1))
			if code, _, stderr := run(t, dir, tt.args...); code != cli.ExitFailure || stderr != tt.want {
				t.Errorf("exit %d, stderr:\n%s\nwant %d and\n%s", code, stderr, cli.ExitFailure, tt.want)
			}
			if got := readFile(t, filepath.Join(dir, "validate_gen.go")); got != generated {
				t.Errorf("validate_gen.go was rewritten:\n%s", got)
			}
		})
	}
}

func TestCommand_RegeneratesOutputSetAsideUnderCallers(t *testing.T) {
	t.Parallel()

	// Either change empties the last run's output, whose Validate the caller
	// keeps calling.
	tests := []struct {
		name  string
		files map[string]string
		edit  func(t *testing.T, dir string)
		want  string
	}{
		{
			// The output imports model, which moves.
			name:  "a package the output imports moved",
			files: map[string]string{"model/model.go": sinceSrc, "app.go": taskSrc("model.Since2000")},
			edit: func(t *testing.T, dir string) {
				if err := os.MkdirAll(filepath.Join(dir, "domain"), 0o750); err != nil {
					t.Fatal(err)
				}
				if err := os.Rename(filepath.Join(dir, "model"), filepath.Join(dir, "domain", "model")); err != nil {
					t.Fatal(err)
				}
				writeFile(t, filepath.Join(dir, "handler.go"),
					"package app\n\nfunc Check(t Task) error {\n\treturn t.Validate()\n}\n")
			},
			want: `"example.com/app/domain/model"`,
		},
		{
			name: "a name of the output taken by hand",
			files: map[string]string{
				"app.go": "package app\n\n//kanna:validate\ntype Signup struct {\n" +
					"\tZip string `json:\"zip\" validate:\"pattern=^[0-9]{3}-[0-9]{4}$\"`\n}\n",
			},
			edit: func(t *testing.T, dir string) {
				writeFile(t, filepath.Join(dir, "handler.go"), "package app\n\nimport \"regexp\"\n\n"+
					"var patternSignupZip = regexp.MustCompile(\"x\")\n\nfunc Check(s Signup) error { return s.Validate() }\n")
			},
			want: "patternSignupZip2",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := writeModule(t, tt.files)
			if code, _, stderr := run(t, dir); code != cli.ExitOK {
				t.Fatalf("first run: exit %d, stderr %q", code, stderr)
			}
			tt.edit(t, dir)

			if code, _, stderr := run(t, dir); code != cli.ExitOK || stderr != "" {
				t.Fatalf("exit %d, stderr %q; want success", code, stderr)
			}
			compiles(t, dir)
			if got := readFile(t, filepath.Join(dir, "validate_gen.go")); !strings.Contains(got, tt.want) {
				t.Errorf("validate_gen.go lacks %s:\n%s", tt.want, got)
			}
		})
	}
}
