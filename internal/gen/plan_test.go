package gen_test

import (
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/go-kanna/internal/packages"
	"github.com/go-kanna/internal/pkgtest"

	"github.com/go-kanna/validate/internal/gen"
)

// plans loads files as one package and plans its targets, as gen.Plans spells
// them.
func plans(t *testing.T, files map[string]string) (map[string][]string, []string) {
	t.Helper()

	pkg := pkgtest.Load(t, files)
	return gen.Plans(pkg.Fset, []*packages.Package{pkg})
}

const planSrc = `package test

import "time"

//kanna:validate
type Request struct {
	Name     string           ` + "`json:\"name\" validate:\"required,min=1,max=50\"`" + `
	Age      int              ` + "`json:\"age\" validate:\"gte=18\"`" + `
	Role     string           ` + "`json:\"role,omitempty\" validate:\"oneof=admin member\"`" + `
	Email    string           ` + "`json:\"email\" validate:\"omitempty,email\"`" + `
	Tags     []string         ` + "`json:\"tags\" validate:\"max=5,dive,max=20\"`" + `
	Nickname *string          ` + "`json:\"nickname\" validate:\"max=20\"`" + `
	Avatar   *string          ` + "`json:\"avatar\" validate:\"required,url\"`" + `
	When     time.Time        ` + "`json:\"when\" validate:\"required\"`" + `
	Point    Point            ` + "`json:\"point\" validate:\"required\"`" + `
	Active   bool             ` + "`json:\"active\" validate:\"required\"`" + `
	Count    int              ` + "`json:\"count\" validate:\"required\"`" + `
	Password string           ` + "`json:\"password\"`" + `
	Confirm  string           ` + "`json:\"confirm\" validate:\"eqfield=Password\"`" + `
	Secret   string           ` + "`json:\"-\" validate:\"required\"`" + `
	Plain    string           ` + "`json:\",omitempty\" validate:\"required\"`" + `
	Address  Address          ` + "`json:\"address\"`" + `
	OptAddr  Address          ` + "`json:\"opt_addr\" validate:\"omitempty\"`" + `
	Billing  *Address         ` + "`json:\"billing\" validate:\"required\"`" + `
	Items    []Item           ` + "`json:\"items\" validate:\"min=1\"`" + `
	Labels   map[string]*Item ` + "`json:\"labels\"`" + `
	Grid     [2]Item          ` + "`json:\"grid\"`" + `
	Matrix   [][]int          ` + "`json:\"matrix\" validate:\"dive,dive,gte=0\"`" + `
	Picks    *[]string        ` + "`json:\"picks\" validate:\"dive,required\"`" + `
	Checked  Checked          ` + "`json:\"checked\"`" + `
	Tree     Tree             ` + "`json:\"tree\"`" + `
	Done     *time.Time       ` + "`json:\"done\" validate:\"rule=Past\"`" + `
	Shifts   []time.Time      ` + "`json:\"shifts\" validate:\"dive,rule=Past\"`" + `
	Ignored  string           ` + "`json:\"ignored\"`" + `
	Audit
}

type Point struct{ X, Y int }

//kanna:validate
type Address struct {
	Zip string ` + "`json:\"zip\" validate:\"required,pattern=^\\\\d{3}-\\\\d{4}$\"`" + `
}

// Item validates itself by hand.
type Item struct{ SKU string }

func (Item) Validate() error { return nil }

// Checked has a Validate that the generated code cannot call.
type Checked struct{ OK bool }

func (Checked) Validate(strict bool) error { return nil }

// Tree contains itself, and nothing in it validates itself.
type Tree map[string]Tree

// Past is a rule as go-kanna/validated writes one.
type Past struct{}

func (Past) Validate(t time.Time) error { return nil }

//kanna:validate
type Audit struct {
	By string ` + "`json:\"by\" validate:\"required\"`" + `
}
`

func TestPlans(t *testing.T) {
	t.Parallel()

	got, diags := plans(t, map[string]string{"test.go": planSrc})
	if len(diags) > 0 {
		t.Fatalf("diagnostics: %q", diags)
	}

	want := map[string][]string{
		"Request": {
			`Name(name): required=="" runes>=1 runes<=50`,
			`Age(age): >=18`,
			`Role(role): oneof("admin","member")`,
			`Email(email): omitempty=="" rules.Email`,
			`Tags(tags): len<=5 []{runes<=20}`,
			`Nickname(nickname): *{runes<=20}`,
			`Avatar(avatar): required==nil *{rules.URL}`,
			`When(when): required==IsZero`,
			`Point(point): required=={}`,
			`Active(active): required==false`,
			`Count(count): required==0`,
			`Confirm(confirm): confirms Password as password`,
			`Secret(Secret): required==""`,
			`Plain(Plain): required==""`,
			`Address(address): nest`,
			`OptAddr(opt_addr): omitempty=={} nest`,
			`Billing(billing): required==nil *{nest}`,
			`Items(items): len>=1 []{nest}`,
			`Labels(labels): []{*{nest}}`,
			`Grid(grid): []{nest}`,
			`Matrix(matrix): []{[]{>=0}}`,
			`Picks(picks): *{[]{required==""}}`,
			`Done(done): *{rule=test.Past}`,
			`Shifts(shifts): []{rule=test.Past}`,
			`Audit(Audit,flatten): nest`,
		},
		"Address": {`Zip(zip): required=="" pattern(^\d{3}-\d{4}$)`},
		"Audit":   {`By(by): required==""`},
	}
	for _, name := range slices.Sorted(maps.Keys(want)) {
		if !slices.Equal(got[name], want[name]) {
			t.Errorf("plan of %s:\n got  %q\n want %q", name, got[name], want[name])
		}
	}
}

func TestPlans_EarlierOutput(t *testing.T) {
	t.Parallel()

	got, diags := plans(t, map[string]string{
		"test.go": `package test

//kanna:validate
type Order struct {
	Former Former ` + "`json:\"former\"`" + `
}

// Former was a target once, and an earlier run generated its Validate.
type Former struct{ Note string }
`,
		"validate_gen.go": `// Code generated by kanna-validate. DO NOT EDIT.

package test

func (x Order) Validate() error  { return nil }

func (x Former) Validate() error { return nil }
`,
	})
	if len(diags) > 0 {
		t.Fatalf("diagnostics: %q", diags)
	}

	// The rewritten file will not declare Former.Validate, so nothing calls it.
	if lines, ok := got["Order"]; !ok || len(lines) > 0 {
		t.Errorf("plan of Order = %q, %v; want one with no fields", lines, ok)
	}
}

func TestPlans_Errors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		field string
		want  string
	}{
		{
			name:  "rule error names the field",
			field: "Age int `validate:\"emial\"`",
			want:  "test.go:5:2: error: unknown rule \"emial\" (field Age)\n  did you mean \"email\"?",
		},
		{
			name:  "unreadable tag",
			field: "Zip string `validate:\"pattern=^\\d$\"`",
			want:  `the validate tag is not a valid quoted string; write a backslash as \\ (field Zip)`,
		},
		{
			name:  "bad syntax",
			field: "Name string `validate:\"required,\"`",
			want:  "empty rule; check for a stray comma (field Name)",
		},
		{
			name:  "dive on a string",
			field: "Name string `validate:\"dive,required\"`",
			want:  "dive needs a slice, array, or map, not string (field Name)",
		},
		{
			name:  "dive through a pointer to a string",
			field: "Name *string `validate:\"dive,required\"`",
			want:  "dive needs a slice, array, or map, not string (field Name)",
		},
		{
			name:  "required on an incomparable struct",
			field: "Bag struct{ S []string } `validate:\"required\"`",
			want: "required cannot tell whether struct{S []string} is empty (field Bag)\n" +
				"  it is not comparable and has no IsZero() bool method; give it one, or use a pointer",
		},
		{
			name:  "omitempty on an incomparable element",
			field: "Bags []struct{ S []string } `validate:\"dive,omitempty,max=1\"`",
			want:  "omitempty cannot tell whether struct{S []string} is empty (field Bags)",
		},
		{
			name:  "eqfield after dive",
			field: "Tags []string `validate:\"dive,eqfield=Other\"`\n\tOther string",
			want:  "eqfield applies to the field itself, not to the elements after dive (field Tags)",
		},
		{
			name:  "rule after dive",
			field: "Tags []string `validate:\"dive,rule=Tag\"`",
			want:  "rule=Tag: no such type (field Tags)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			src := "package test\n\n//kanna:validate\ntype Target struct {\n\t" + tt.field + "\n}\n"
			got, diags := plans(t, map[string]string{"test.go": src})
			if _, ok := got["Target"]; ok {
				t.Errorf("Target was planned despite the error")
			}
			if !slices.ContainsFunc(diags, func(d string) bool { return strings.Contains(d, tt.want) }) {
				t.Errorf("diagnostics = %q, want one containing %q", diags, tt.want)
			}
		})
	}
}
