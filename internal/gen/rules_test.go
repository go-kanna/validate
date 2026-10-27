package gen_test

import (
	"go/types"
	"strings"
	"testing"

	"github.com/go-kanna/internal/pkgtest"

	"github.com/go-kanna/validate/internal/gen"
)

const rulesSrc = `package test

import "time"

type Role string

type Age uint8

type Fields struct {
	S     string
	Kind  Role
	I     int
	Years Age
	F     float64
	F32   float32
	B     bool
	P     *string
	Sl    []string
	M     map[string]int
	A     [3]int
	When  time.Time
	I8    int8
	U16   uint16
	R     rune
}
`

// fields loads rulesSrc and returns its package, and the type of each field of
// Fields by name.
func fields(t *testing.T) (*types.Package, map[string]types.Type) {
	t.Helper()

	pkg := pkgtest.Load(t, map[string]string{"test.go": rulesSrc}).Types
	st, ok := pkg.Scope().Lookup("Fields").Type().Underlying().(*types.Struct)
	if !ok {
		t.Fatal("Fields is not a struct")
	}
	typeOf := make(map[string]types.Type, st.NumFields())
	for f := range st.Fields() {
		typeOf[f.Name()] = f.Type()
	}
	return pkg, typeOf
}

func TestResolveRule(t *testing.T) {
	t.Parallel()

	pkg, typeOf := fields(t)

	tests := []struct {
		rule  string
		field string
		want  string
	}{
		{rule: "min=3", field: "S", want: "runes>=3"},
		{rule: "max=50", field: "Kind", want: "runes<=50"},
		{rule: "max=5", field: "Sl", want: "len<=5"},
		{rule: "len=2", field: "M", want: "len==2"},
		{rule: "min=18", field: "I", want: ">=18"},
		{rule: "max=1.5", field: "F", want: "<=1.5"},
		{rule: "max=255", field: "Years", want: "<=255"},
		{rule: "max=1e3", field: "I", want: "<=1e3"},
		{rule: "min=-128", field: "I8", want: ">=-128"},
		{rule: "max=65535", field: "U16", want: "<=65535"},
		{rule: "gt=0", field: "F32", want: ">0"},
		{rule: "gte=-10", field: "I", want: ">=-10"},
		{rule: "lt=0x10", field: "I", want: "<0x10"},
		{rule: "lte=9", field: "I", want: "<=9"},
		{rule: "eq=1", field: "I", want: "==1"},
		{rule: "ne=0", field: "F", want: "!=0"},
		{rule: "oneof=admin member", field: "Kind", want: `oneof("admin","member")`},
		{rule: "oneof=1 2", field: "I", want: "oneof(1,2)"},
		{rule: `pattern=^\d{3}-\d{4}$`, field: "S", want: `pattern(^\d{3}-\d{4}$)`},
		{rule: "email", field: "S", want: "rules.Email"},
		{rule: "url", field: "Kind", want: "rules.URL"},
		{rule: "uuid", field: "S", want: "rules.UUID"},
	}

	for _, tt := range tests {
		t.Run(tt.rule+" on "+tt.field, func(t *testing.T) {
			t.Parallel()

			got, err := gen.ResolveRule(tt.rule, typeOf[tt.field], pkg, nil)
			if err != nil {
				t.Fatalf("resolve() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("resolve() = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestResolveRule_Errors(t *testing.T) {
	t.Parallel()

	pkg, typeOf := fields(t)

	tests := []struct {
		rule     string
		field    string
		wantErr  string
		wantHint string
	}{
		{rule: "reqiured", field: "S", wantErr: `unknown rule "reqiured"`, wantHint: `did you mean "required"?`},
		{rule: "emial", field: "S", wantErr: `unknown rule "emial"`, wantHint: `did you mean "email"?`},
		{rule: "omitempy", field: "S", wantErr: `unknown rule "omitempy"`, wantHint: `did you mean "omitempty"?`},
		{rule: "xyz", field: "S", wantErr: `unknown rule "xyz"`},
		{rule: "email=x", field: "S", wantErr: `rule "email" takes no value, got "x"`},
		{rule: "min", field: "S", wantErr: `rule "min" needs a value, as in min=…`},
		{rule: "eq", field: "I", wantErr: `rule "eq" needs a value`},
		{rule: "oneof", field: "S", wantErr: `rule "oneof" needs a value`},
		{rule: "oneof= ", field: "S", wantErr: `rule "oneof" needs a value`},
		{rule: "eqfield", field: "S", wantErr: `rule "eqfield" needs a value`},
		{rule: "pattern", field: "S", wantErr: `rule "pattern" needs a value`},
		{rule: "max=fifty", field: "S", wantErr: `rule "max" expects a length, got "fifty"`},
		{rule: "min=-1", field: "S", wantErr: `rule "min" expects a length, got "-1"`},
		{rule: "max=5", field: "A", wantErr: `rule "max" cannot apply to [3]int, whose length is fixed`},
		{rule: "len=3", field: "I", wantErr: `rule "len" cannot apply to int`, wantHint: "compare a number with eq"},
		{rule: "max=fifty", field: "I", wantErr: `rule "max" expects a number, got "fifty"`},
		{rule: "gt=abc", field: "I", wantErr: `rule "gt" expects a number, got "abc"`},
		// A type of the struct's own package is named as its source names it.
		{rule: "max=300", field: "Years", wantErr: `rule "max": 300 does not fit Age`},
		{rule: "min=-1", field: "Years", wantErr: "-1 does not fit Age"},
		{rule: "max=128", field: "I8", wantErr: "128 does not fit int8"},
		{rule: "max=65536", field: "U16", wantErr: "65536 does not fit uint16"},
		{rule: "max=2147483648", field: "R", wantErr: "2147483648 does not fit rune"},
		{rule: "max=1.5", field: "I", wantErr: "1.5 does not fit int"},
		{rule: "max=1e39", field: "F32", wantErr: "1e39 does not fit float32"},
		{rule: "max=5", field: "B", wantErr: `rule "max" cannot apply to bool`},
		{rule: "max=5", field: "P", wantErr: `rule "max" cannot apply to *string`},
		{rule: "gt=0", field: "S", wantErr: `rule "gt" cannot apply to string`, wantHint: "min, max, or len"},
		{rule: "gt=0", field: "B", wantErr: `rule "gt" cannot apply to bool`},
		{rule: "oneof=1 2", field: "F", wantErr: `rule "oneof" cannot apply to float64`},
		{rule: "oneof=a b a", field: "S", wantErr: `rule "oneof" lists a twice`},
		{rule: "oneof=1 300", field: "Years", wantErr: "300 does not fit Age"},
		{rule: "pattern=[a-z", field: "S", wantErr: "invalid pattern: error parsing regexp: missing closing ]"},
		{rule: "pattern=^a$", field: "I", wantErr: `rule "pattern" cannot apply to int`},
		{rule: "email", field: "I", wantErr: `rule "email" cannot apply to int`},
		{rule: "url", field: "When", wantErr: `rule "url" cannot apply to time.Time`},
	}

	for _, tt := range tests {
		t.Run(tt.rule+" on "+tt.field, func(t *testing.T) {
			t.Parallel()

			_, err := gen.ResolveRule(tt.rule, typeOf[tt.field], pkg, nil)
			assertRuleError(t, err, tt.wantErr, tt.wantHint)
		})
	}
}

func TestResolveRule_EqField(t *testing.T) {
	t.Parallel()

	pkg, typeOf := fields(t)
	siblings := []gen.Sibling{
		gen.NewSibling("Password", "password", typeOf["S"]),
		gen.NewSibling("Age", "age", typeOf["I"]),
		gen.NewSibling("Tags", "tags", typeOf["Sl"]),
	}

	got, err := gen.ResolveRule("eqfield=Password", typeOf["S"], pkg, siblings)
	if err != nil {
		t.Fatalf("resolve() error = %v", err)
	}
	if want := "confirms Password as password"; got != want {
		t.Errorf("resolve() = %s, want %s", got, want)
	}

	tests := []struct {
		name     string
		param    string
		field    string
		siblings []gen.Sibling
		wantErr  string
		wantHint string
	}{
		{
			name: "misspelled", param: "Pasword", field: "S", siblings: siblings,
			wantErr: "eqfield=Pasword: no such field", wantHint: `did you mean "Password"?`,
		},
		{name: "unknown", param: "Nothing", field: "S", siblings: siblings, wantErr: "eqfield=Nothing: no such field"},
		{
			name: "different type", param: "Age", field: "S", siblings: siblings,
			wantErr: "compares string with int, which differ",
		},
		{name: "not comparable", param: "Tags", field: "Sl", siblings: siblings, wantErr: "cannot compare []string"},
		{name: "after dive", param: "Password", field: "S", siblings: nil, wantErr: "not to the elements after dive"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := gen.ResolveRule("eqfield="+tt.param, typeOf[tt.field], pkg, tt.siblings)
			assertRuleError(t, err, tt.wantErr, tt.wantHint)
		})
	}
}

// assertRuleError checks that err says wantErr and hints at wantHint, or
// hints at nothing when wantHint is empty.
func assertRuleError(t *testing.T, err error, wantErr, wantHint string) {
	t.Helper()

	if err == nil || !strings.Contains(err.Error(), wantErr) {
		t.Errorf("error = %v, want it to contain %q", err, wantErr)
	}
	if got := gen.Hint(err); !strings.Contains(got, wantHint) || (wantHint == "" && got != "") {
		t.Errorf("hint = %q, want %q", got, wantHint)
	}
}
