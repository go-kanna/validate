package gen_test

import (
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/go-kanna/validate/internal/gen"
)

// validateTag is a struct tag holding s as its validate tag.
func validateTag(s string) reflect.StructTag {
	return reflect.StructTag(`json:"x" validate:` + strconv.Quote(s))
}

func TestParseTag(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		tag  reflect.StructTag
		want []string
	}{
		{name: "absent", tag: `json:"name"`, want: nil},
		{name: "empty", tag: validateTag(""), want: []string{""}},
		{name: "required", tag: validateTag("required"), want: []string{"required"}},
		{
			name: "required wherever it is",
			tag:  validateTag("max=50,required,oneof=admin member"),
			want: []string{"required,max=50,oneof=admin member"},
		},
		{name: "omitempty", tag: validateTag("omitempty,email"), want: []string{"omitempty,email"}},
		{
			name: "dive",
			tag:  validateTag("max=5,dive,omitempty,max=20"),
			want: []string{"max=5", "omitempty,max=20"},
		},
		{name: "dive twice", tag: validateTag("dive,dive,required"), want: []string{"", "", "required"}},
		{
			name: "pattern keeps its commas",
			tag:  validateTag(`required,pattern=^\d{1,3}(,\d{3})*$`),
			want: []string{`required,pattern=^\d{1,3}(,\d{3})*$`},
		},
		{name: "pattern after dive", tag: validateTag("dive,pattern=^a,b$"), want: []string{"", "pattern=^a,b$"}},
		{
			// reflect skips a malformed tag of another key.
			name: "another tag broken",
			tag:  `json:"a\d" validate:"required"`,
			want: []string{"required"},
		},
		{name: "a key ending in validate", tag: `xvalidate:"pattern=^\d$"`, want: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := gen.ParseTag(tt.tag)
			if err != nil {
				t.Fatalf("parseTag(%s) error = %v", tt.tag, err)
			}
			if !slices.Equal(got, tt.want) || (got == nil) != (tt.want == nil) {
				t.Errorf("parseTag(%s) = %q, want %q", tt.tag, got, tt.want)
			}
		})
	}
}

func TestParseTag_Errors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		tag  reflect.StructTag
		want string
	}{
		{name: "unescaped backslash", tag: `json:"zip" validate:"pattern=^\d+$"`, want: `write a backslash as \\`},
		{name: "unterminated", tag: `validate:"required`, want: "is not a valid quoted string"},
		{name: "trailing comma", tag: validateTag("required,"), want: "empty rule; check for a stray comma"},
		{name: "leading comma", tag: validateTag(",required"), want: "empty rule"},
		{name: "double comma", tag: validateTag("required,,max=5"), want: "empty rule"},
		{name: "space after comma", tag: validateTag("required, max=50"), want: `rule " max=50" contains a space`},
		{name: "no name", tag: validateTag("=5"), want: `rule "=5" has no name`},
		{name: "no value", tag: validateTag("max="), want: `rule "max=" has no value after =`},
		{name: "dive with a value", tag: validateTag("dive=1"), want: "dive takes no value"},
		{name: "dive at the end", tag: validateTag("max=5,dive"), want: "dive must be followed by the rules"},
		{name: "dive to omitempty alone", tag: validateTag("dive,omitempty"), want: "dive must be followed by the rules"},
		{name: "required with a value", tag: validateTag("required=1"), want: "required takes no value"},
		{name: "required twice", tag: validateTag("required,max=5,required"), want: "required is given twice"},
		{name: "omitempty with a value", tag: validateTag("omitempty=1"), want: "omitempty takes no value"},
		{name: "omitempty twice", tag: validateTag("omitempty,omitempty"), want: "omitempty is given twice"},
		{name: "omitempty after a rule", tag: validateTag("email,omitempty"), want: "omitempty must come before email"},
		{
			name: "omitempty and required",
			tag:  validateTag("required,omitempty"),
			want: "omitempty lets through the empty value required rejects",
		},
		{
			name: "omitempty and required after dive",
			tag:  validateTag("dive,omitempty,required"),
			want: "omitempty lets through the empty value required rejects",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := gen.ParseTag(tt.tag)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("parseTag(%s) error = %v, want it to mention %q", tt.tag, err, tt.want)
			}
		})
	}
}
