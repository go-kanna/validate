package gen_test

import (
	"go/types"
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

const (
	// enumField ends a struct holding the type Status.
	enumField = "Status `json:\"status\" validate:\"required\"`\n}\n"

	// enumRequest marks a struct of package api holding user.Status.
	enumRequest = "package api\n\nimport \"example.com/app/user\"\n\n//kanna:validate\ntype Request struct {\n" +
		"\tStatus user." + enumField

	// enumNest is how the output for such a struct checks the enum.
	enumNest = "x.Status.Validate()"

	// enumValidate stands in for the Validate kanna-value writes on Status.
	enumValidate = "// Code generated by kanna-value. DO NOT EDIT.\n\npackage user\n\n" +
		"func (x Status) Validate() error { return nil }\n"
)

// An enum has Validate once kanna-value writes it, which may be after this run.
func TestCommand_NestsAnEnum(t *testing.T) {
	t.Parallel()

	const (
		declared = "type Status string\n\nconst StatusActive Status = \"active\"\n"
		enum     = "//kanna:enum\n" + declared
	)
	for _, tt := range []struct {
		name  string
		files map[string]string
		want  bool
	}{
		{"of another package", map[string]string{"user/user.go": "package user\n\n" + enum, "api/api.go": enumRequest}, true},
		{"of its own package", map[string]string{
			"api/api.go": "package api\n\n" + enum + "\n//kanna:validate\ntype Request struct {\n\tStatus " + enumField,
		}, true},
		{"whose constants export data leaves out", map[string]string{
			"user/user.go": "package user\n\n//kanna:enum\ntype Status string\n\nconst statusActive Status = \"active\"\n",
			"api/api.go":   enumRequest,
		}, true},
		{"a string type with constants, not marked", map[string]string{
			"user/user.go": "package user\n\n" + declared, "api/api.go": enumRequest,
		}, false},
		{"a mark on what is no string type", map[string]string{
			"user/user.go": "package user\n\n//kanna:enum\ntype Status int\n\nconst StatusActive Status = 1\n",
			"api/api.go":   enumRequest,
		}, false},
		{"a mark on a generic type", map[string]string{
			"user/user.go": "package user\n\n//kanna:enum\ntype Status[T any] string\n\n" +
				"const StatusActive Status[int] = \"active\"\n",
			"api/api.go": strings.Replace(enumRequest, "user.Status", "user.Status[int]", 1),
		}, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := writeModule(t, tt.files)
			if code, _, stderr := run(t, filepath.Join(dir, "api")); code != cli.ExitOK || stderr != "" {
				t.Fatalf("exit %d, stderr %q", code, stderr)
			}
			got := readFile(t, filepath.Join(dir, "api", "validate_gen.go"))
			if strings.Contains(got, enumNest) != tt.want {
				t.Errorf("api/validate_gen.go holding %s = %v, want %v:\n%s", enumNest, !tt.want, tt.want, got)
			}
		})
	}
}

// What kanna-value writes after this run changes nothing of its output.
func TestCommand_StaysUpToDateOnceAnEnumIsGenerated(t *testing.T) {
	t.Parallel()

	for _, constant := range []string{"StatusActive", "statusActive"} {
		t.Run(constant, func(t *testing.T) {
			t.Parallel()

			dir := writeModule(t, map[string]string{
				"user/user.go": "package user\n\n//kanna:enum\ntype Status string\n\nconst " + constant + " Status = \"active\"\n",
				"api/api.go":   enumRequest,
			})
			api := filepath.Join(dir, "api")
			if code, _, stderr := run(t, api); code != cli.ExitOK {
				t.Fatalf("run = %d, stderr %q", code, stderr)
			}
			writeFile(t, filepath.Join(dir, "user", "value_gen.go"), enumValidate)
			if code, _, stderr := run(t, api, "-check"); code != cli.ExitOK {
				t.Errorf("-check = %d, stderr %q", code, stderr)
			}
			compiles(t, dir)
		})
	}
}

// The next run of kanna-value takes away the Validate of an enum that lost its
// mark, and that run may come after this one.
func TestCommand_CallsTheValidateOfAnEnumWhileMarked(t *testing.T) {
	t.Parallel()

	const (
		declared = "type Status string\n\nconst StatusActive Status = \"active\"\n"
		byHand   = "\nfunc (x Status) Validate() error { return nil }\n"
	)
	for _, field := range []string{"user.Status", "*user.Status", "[]user.Status", "map[string]user.Status"} {
		for _, tt := range []struct {
			name  string
			files map[string]string
			want  bool
		}{
			{"generated and marked", map[string]string{
				"user/user.go": "package user\n\n//kanna:enum\n" + declared, "user/value_gen.go": enumValidate,
			}, true},
			{"generated, the mark gone", map[string]string{
				"user/user.go": "package user\n\n" + declared, "user/value_gen.go": enumValidate,
			}, false},
			{"generated by a release, the mark gone", map[string]string{
				"user/user.go":      "package user\n\n" + declared,
				"user/value_gen.go": strings.Replace(enumValidate, "kanna-value.", "kanna-value v0.0.9.", 1),
			}, false},
			{"written by hand", map[string]string{"user/user.go": "package user\n\n" + declared + byHand}, true},
			{"written by hand under the name of kanna-value's output", map[string]string{
				"user/user.go": "package user\n\n" + declared, "user/value_gen.go": "package user\n" + byHand,
			}, true},
			{"written by hand under the name of this output", map[string]string{
				"user/user.go": "package user\n\n" + declared, "user/validate_gen.go": "package user\n" + byHand,
			}, true},
			{"generated by another tool under that name", map[string]string{
				"user/user.go":      "package user\n\n" + declared,
				"user/value_gen.go": strings.Replace(enumValidate, "kanna-value.", "another-tool.", 1),
			}, true},
		} {
			t.Run(field+" "+tt.name, func(t *testing.T) {
				t.Parallel()

				tt.files["api/api.go"] = strings.Replace(enumRequest, "user.Status", field, 1)
				dir := writeModule(t, tt.files)
				if code, _, stderr := run(t, filepath.Join(dir, "api")); code != cli.ExitOK || stderr != "" {
					t.Fatalf("exit %d, stderr %q", code, stderr)
				}
				got := readFile(t, filepath.Join(dir, "api", "validate_gen.go"))
				if strings.Contains(got, ".Validate()") != tt.want {
					t.Errorf("api/validate_gen.go calling Validate = %v, want %v:\n%s", !tt.want, tt.want, got)
				}
			})
		}
	}
}

// The file rewritten will not declare the Validate of what is no target now.
func TestCommand_LeavesOutWhatAnEarlierRunGenerated(t *testing.T) {
	t.Parallel()

	dir := writeModule(t, map[string]string{
		"app.go": "package app\n\n//kanna:validate\ntype Order struct {\n\tFormer Former `json:\"former\"`\n}\n\n" +
			"type Former struct{ Note string }\n",
		"validate_gen.go": "// Code generated by kanna-validate. DO NOT EDIT.\n\npackage app\n\n" +
			"func (x Order) Validate() error { return nil }\n\nfunc (x Former) Validate() error { return nil }\n",
	})
	if code, _, stderr := run(t, dir); code != cli.ExitOK || stderr != "" {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
	if got := readFile(t, filepath.Join(dir, "validate_gen.go")); strings.Contains(got, "x.Former.Validate()") {
		t.Errorf("validate_gen.go calls the Validate it no longer declares:\n%s", got)
	}
	compiles(t, dir)
}

// Under -trimpath, where export data holds no path on disk, a run follows an
// enum as it does otherwise: before kanna-value writes its Validate, after, and
// with the mark gone before kanna-value takes the method away.
func TestCommand_FollowsAnEnumUnderTrimpath(t *testing.T) {
	t.Setenv("GOFLAGS", "-trimpath")

	const declared = "type Status string\n\nconst StatusActive Status = \"active\"\n"
	dir := writeModule(t, map[string]string{
		"user/user.go": "package user\n\n//kanna:enum\n" + declared,
		"api/api.go":   enumRequest,
	})
	api := filepath.Join(dir, "api")
	generate := func(want bool) {
		t.Helper()

		if code, _, stderr := run(t, api); code != cli.ExitOK || stderr != "" {
			t.Fatalf("exit %d, stderr %q", code, stderr)
		}
		got := readFile(t, filepath.Join(api, "validate_gen.go"))
		if strings.Contains(got, enumNest) != want {
			t.Errorf("api/validate_gen.go holding %s = %v, want %v:\n%s", enumNest, !want, want, got)
		}
	}

	generate(true)

	output := filepath.Join(dir, "user", "value_gen.go")
	writeFile(t, output, enumValidate)
	if code, _, stderr := run(t, api, "-check"); code != cli.ExitOK {
		t.Errorf("-check = %d, stderr %q", code, stderr)
	}
	generate(true)
	compiles(t, dir)

	writeFile(t, filepath.Join(dir, "user", "user.go"), "package user\n\n"+declared)
	generate(false)
	if err := os.Remove(output); err != nil {
		t.Fatal(err)
	}
	compiles(t, dir)
}

// A mark is read in the package declaring the type however it was reached, and
// whether export data holds a path on disk or not.
func TestCommand_ReadsMarksOfOtherPackages(t *testing.T) {
	const (
		address = "package user\n\n//kanna:validate\ntype Address struct {\n\tCity string `validate:\"required\"`\n}\n"
		status  = "package user\n\n//kanna:enum\ntype Status string\n\nconst StatusActive Status = \"active\"\n"
		dto     = "package dto\n\nimport \"example.com/app/user\"\n\ntype Statuses []user.Status\n"
	)
	for _, tt := range []struct {
		name  string
		files map[string]string
	}{
		{"a struct of a package imported", map[string]string{
			"user/user.go": address,
			"api/api.go": "package api\n\nimport \"example.com/app/user\"\n\n" +
				"//kanna:validate\ntype Request struct {\n\tAddress user.Address\n}\n",
		}},
		{"an enum of a package imported through another", map[string]string{
			"user/user.go": status,
			"dto/dto.go":   dto,
			"api/api.go": "package api\n\nimport \"example.com/app/dto\"\n\n" +
				"//kanna:validate\ntype Request struct {\n\tStatuses dto.Statuses\n}\n",
		}},
	} {
		for _, flags := range []string{"", "-trimpath"} {
			t.Run(tt.name+" "+flags, func(t *testing.T) {
				t.Setenv("GOFLAGS", flags)

				dir := writeModule(t, tt.files)
				if code, _, stderr := run(t, filepath.Join(dir, "api")); code != cli.ExitOK || stderr != "" {
					t.Fatalf("exit %d, stderr %q", code, stderr)
				}
				if got := readFile(t, filepath.Join(dir, "api", "validate_gen.go")); !strings.Contains(got, ".Validate()") {
					t.Errorf("api/validate_gen.go calls no Validate:\n%s", got)
				}
			})
		}
	}
}

// A struct has the Validate of what it embeds as the compiler finds it, once
// the generators have run: the output of a first run stands after they have.
func TestCommand_FindsAValidateToBePromoted(t *testing.T) {
	t.Parallel()

	const (
		enums = "package user\n\n//kanna:enum\ntype Status string\n\nconst StatusActive Status = \"active\"\n\n" +
			"//kanna:enum\ntype Kind string\n\nconst KindA Kind = \"a\"\n\n"
		output = "// Code generated by kanna-value. DO NOT EDIT.\n\npackage user\n\n" +
			"func (x Status) Validate() error { return nil }\n\nfunc (x Kind) Validate() error { return nil }\n"
		request = "package api\n\nimport \"example.com/app/user\"\n\n//kanna:validate\n" +
			"type Request struct {\n\tFilter user.Filter\n}\n"
		hand    = "type Hand struct{}\n\nfunc (Hand) Validate() error { return nil }\n\n"
		checker = "type Checker interface{ Validate() error }\n\n"
		nest    = "x.Filter.Validate()"
	)
	for _, tt := range []struct {
		name string
		decl string
		want bool
	}{
		{"embedded", "type Filter struct{ Status }\n", true},
		{"embedded by pointer", "type Filter struct{ *Status }\n", true},
		{"embedded under another name", "type Alias = Status\n\ntype Filter struct{ Alias }\n", true},
		{"embedded deeper", "type Inner struct{ Status }\n\ntype Filter struct{ Inner }\n", true},
		{"embedded in a generic struct", "type Box[T any] struct{ Status }\n\ntype Filter struct{ Box[int] }\n", true},
		{"embedded in what the type is defined as", "type Base struct{ Status }\n\ntype Filter Base\n", true},
		{"embedded unexported", "type hidden struct{ Status }\n\ntype Filter struct{ hidden }\n", true},
		{"embedded beside the type itself", "type Filter struct {\n\t*Filter\n\tStatus\n}\n", true},
		{"nothing but the type itself", "type Filter struct{ *Filter }\n", false},
		{"a struct marked", "//kanna:validate\ntype Address struct {\n\tCity string `validate:\"required\"`\n}\n\n" +
			"type Filter struct{ Address }\n", true},
		{"shallower than another", "type Inner struct{ Kind }\n\ntype Filter struct {\n\tStatus\n\tInner\n}\n", true},
		{"beside another", "type Filter struct {\n\tStatus\n\tKind\n}\n", false},
		{"beside a method written by hand", hand + "type Filter struct {\n\tStatus\n\tHand\n}\n", false},
		{"beside a method of an interface", checker + "type Filter struct {\n\tStatus\n\tChecker\n}\n", false},
		{"reached twice", "type A struct{ Status }\n\ntype B struct{ Status }\n\ntype Filter struct {\n\tA\n\tB\n}\n", false},
		{"hidden by a method", "type Filter struct{ Status }\n\nfunc (Filter) Validate() error { return nil }\n", true},
		{"hidden by a method of another shape", "type Filter struct{ Status }\n\n" +
			"func (Filter) Validate(int) error { return nil }\n", false},
		{"hidden by a field", "type Filter struct {\n\tStatus\n\tValidate bool\n}\n", false},
		{"deeper than a field", "type Flag struct{ Validate bool }\n\ntype Inner struct{ Status }\n\n" +
			"type Filter struct {\n\tFlag\n\tInner\n}\n", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := writeModule(t, map[string]string{"user/user.go": enums + tt.decl, "api/api.go": request})
			api := filepath.Join(dir, "api")
			if code, _, stderr := run(t, api); code != cli.ExitOK || stderr != "" {
				t.Fatalf("exit %d, stderr %q", code, stderr)
			}
			got := readFile(t, filepath.Join(api, "validate_gen.go"))
			if strings.Contains(got, nest) != tt.want {
				t.Errorf("api/validate_gen.go holding %s = %v, want %v:\n%s", nest, !tt.want, tt.want, got)
			}

			writeFile(t, filepath.Join(dir, "user", "value_gen.go"), output)
			if code, _, stderr := run(t, filepath.Join(dir, "user")); code != cli.ExitOK {
				t.Fatalf("user: exit %d, stderr %q", code, stderr)
			}
			if code, _, stderr := run(t, api, "-check"); code != cli.ExitOK {
				t.Errorf("-check = %d, stderr %q", code, stderr)
			}
			compiles(t, dir)
			if got := compilerFindsValidate(t, dir); got != tt.want {
				t.Errorf("the compiler finds Filter.Validate = %v, want %v", got, tt.want)
			}
		})
	}
}

// compilerFindsValidate reports whether go/types finds Validate() error on the
// type Filter of package user, with everything generated.
func compilerFindsValidate(t *testing.T, dir string) bool {
	t.Helper()

	set, err := packages.Load([]string{"./user"}, packages.Config{Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	filter := set.Packages[0].Types.Scope().Lookup("Filter").Type()
	obj, _, _ := types.LookupFieldOrMethod(filter, true, nil, "Validate")
	fn, ok := obj.(*types.Func)
	return ok && fn.Signature().Params().Len() == 0
}

// What is about to go neither gives a struct Validate nor stands in the way of
// what it would otherwise collide with.
func TestCommand_FindsNoValidateToBeRemoved(t *testing.T) {
	t.Parallel()

	const (
		status   = "type Status string\n\nconst StatusActive Status = \"active\"\n\n"
		kind     = "//kanna:enum\ntype Kind string\n\nconst KindA Kind = \"a\"\n\n"
		header   = "// Code generated by kanna-value. DO NOT EDIT.\n\npackage user\n\n"
		onStatus = "func (x Status) Validate() error { return nil }\n"
		onKind   = "func (x Kind) Validate() error { return nil }\n"
		request  = "package api\n\nimport \"example.com/app/user\"\n\n//kanna:validate\n" +
			"type Request struct {\n\tFilter user.Filter\n}\n"
		nest = "x.Filter.Validate()"
	)
	for _, tt := range []struct {
		name         string
		decl         string
		output, next string // of kanna-value, before its next run and after; "" for no file
		want         bool
	}{
		{"alone", status + "type Filter struct{ Status }\n", header + onStatus, "", false},
		{
			"beside what stays", status + kind + "type Filter struct {\n\tStatus\n\tKind\n}\n",
			header + onStatus + onKind, header + onKind, true,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := writeModule(t, map[string]string{
				"user/user.go": "package user\n\n" + tt.decl, "user/value_gen.go": tt.output, "api/api.go": request,
			})
			api := filepath.Join(dir, "api")
			if code, _, stderr := run(t, api); code != cli.ExitOK || stderr != "" {
				t.Fatalf("exit %d, stderr %q", code, stderr)
			}
			got := readFile(t, filepath.Join(api, "validate_gen.go"))
			if strings.Contains(got, nest) != tt.want {
				t.Errorf("api/validate_gen.go holding %s = %v, want %v:\n%s", nest, !tt.want, tt.want, got)
			}

			output := filepath.Join(dir, "user", "value_gen.go")
			if tt.next == "" {
				if err := os.Remove(output); err != nil {
					t.Fatal(err)
				}
			} else {
				writeFile(t, output, tt.next)
			}
			if code, _, stderr := run(t, api, "-check"); code != cli.ExitOK {
				t.Errorf("-check = %d, stderr %q", code, stderr)
			}
			compiles(t, dir)
		})
	}
}

// The generated code tests what a Validate is promoted through for nil, which
// it cannot do through what another package does not export.
func TestCommand_RefusesANilItCannotTest(t *testing.T) {
	t.Parallel()

	const (
		enum    = "package user\n\n//kanna:enum\ntype Status string\n\nconst StatusActive Status = \"active\"\n\n"
		request = "package api\n\nimport \"example.com/app/user\"\n\n//kanna:validate\n" +
			"type Request struct {\n\tFilter user.Filter\n}\n"
		position = "./api.go:7:2: error: user.Filter has Validate through an embedded field that may be nil, " +
			"which package api cannot test, as "
		hint = " is not exported (field Filter)\n  give user.Filter a Validate of its own, or export "
	)
	for _, tt := range []struct {
		name    string
		decl    string
		request string
		want    string
	}{
		{"a pointer", "type status string\n\nfunc (status) Validate() error { return nil }\n\n" +
			"type Filter struct{ *status }\n", request, position + "status" + hint + "status\n"},
		{"an interface", "type checker interface{ Validate() error }\n\ntype Filter struct{ checker }\n",
			request, position + "checker" + hint + "checker\n"},
		{"a pointer below", "type hidden struct{ *Status }\n\ntype Filter struct{ hidden }\n",
			request, position + "hidden" + hint + "hidden\n"},
		{"an element", "type status string\n\nfunc (status) Validate() error { return nil }\n\n" +
			"type Filter struct{ *status }\n", strings.Replace(request, "user.Filter", "[]user.Filter", 1),
			position + "status" + hint + "status\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := writeModule(t, map[string]string{"user/user.go": enum + tt.decl, "api/api.go": tt.request})
			if code, _, stderr := run(t, filepath.Join(dir, "api")); code != cli.ExitFailure || stderr != tt.want {
				t.Errorf("exit %d, stderr:\n%s\nwant %d and\n%s", code, stderr, cli.ExitFailure, tt.want)
			}
		})
	}
}

func TestCommand_TestsANilOfItsOwnPackage(t *testing.T) {
	t.Parallel()

	dir := writeModule(t, map[string]string{"api.go": "package api\n\ntype status string\n\n" +
		"func (status) Validate() error { return nil }\n\ntype Filter struct{ *status }\n\n" +
		"//kanna:validate\ntype Request struct {\n\tFilter Filter\n}\n"})
	if code, _, stderr := run(t, dir); code != cli.ExitOK || stderr != "" {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
	const want = "if x.Filter.status != nil {"
	if got := readFile(t, filepath.Join(dir, "validate_gen.go")); !strings.Contains(got, want) {
		t.Errorf("validate_gen.go does not hold %q:\n%s", want, got)
	}
	compiles(t, dir)
}

func TestCommand_NestsAStructMarkedInAnotherPackage(t *testing.T) {
	t.Parallel()

	const (
		address = "package user\n\n//kanna:validate\ntype Address struct {\n" +
			"\tZip string `json:\"zip\" validate:\"required\"`\n}\n"
		request = "package api\n\nimport \"example.com/app/user\"\n\n//kanna:validate\ntype Request struct {\n" +
			"\tName    string       `json:\"name\" validate:\"required\"`\n\tAddress user.Address `json:\"address\"`\n}\n"
		nest = "x.Address.Validate()"
	)

	// go generate ./... may run api before user has its Validate.
	t.Run("before its package is generated", func(t *testing.T) {
		t.Parallel()

		dir := writeModule(t, map[string]string{"user/user.go": address, "api/api.go": request})
		if code, _, stderr := run(t, filepath.Join(dir, "api")); code != cli.ExitOK || stderr != "" {
			t.Fatalf("exit %d, stderr %q", code, stderr)
		}
		if got := readFile(t, filepath.Join(dir, "api", "validate_gen.go")); !strings.Contains(got, nest) {
			t.Errorf("api/validate_gen.go lacks %s:\n%s", nest, got)
		}
	})

	// user's own run has yet to remove the Validate an earlier one wrote.
	t.Run("marked no more", func(t *testing.T) {
		t.Parallel()

		dir := writeModule(t, map[string]string{"user/user.go": address, "api/api.go": request})
		if code, _, stderr := run(t, filepath.Join(dir, "user")); code != cli.ExitOK {
			t.Fatalf("user: exit %d, stderr %q", code, stderr)
		}
		writeFile(t, filepath.Join(dir, "user", "user.go"), strings.Replace(address, "//kanna:validate\n", "", 1))
		if code, _, stderr := run(t, filepath.Join(dir, "api")); code != cli.ExitOK || stderr != "" {
			t.Fatalf("api: exit %d, stderr %q", code, stderr)
		}
		if got := readFile(t, filepath.Join(dir, "api", "validate_gen.go")); strings.Contains(got, nest) {
			t.Errorf("api/validate_gen.go holds %s:\n%s", nest, got)
		}
	})
}
