package validate_test

import (
	jsonv1 "encoding/json"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/go-kanna/validate"
)

// typeError stands in for a Coded error from another package, such as
// *rule.Error of go-kanna/value.
type typeError struct{ code string }

func (e *typeError) Error() string          { return "type: " + e.code }
func (e *typeError) Code() string           { return e.code }
func (e *typeError) Params() map[string]any { return nil }

// equalErrors compares by field and by the text of each error, which is enough
// to tell apart the errors these tests build.
func equalErrors(a, b validate.Errors) bool {
	return slices.EqualFunc(a, b, func(x, y validate.FieldError) bool {
		return x.Field == y.Field && x.Err.Error() == y.Err.Error()
	})
}

func TestErrors_Add(t *testing.T) {
	t.Parallel()

	var errs validate.Errors
	errs.Add("name", validate.Blank())
	errs.Add("age", nil)
	errs.Add("tags[1]", validate.TooLong(20))

	want := validate.Errors{{Field: "name", Err: validate.Blank()}, {Field: "tags[1]", Err: validate.TooLong(20)}}
	if !equalErrors(errs, want) {
		t.Errorf("Errors = %v, want %v", errs, want)
	}
}

func TestErrors_Nest(t *testing.T) {
	t.Parallel()

	inner := validate.Errors{{Field: "zip", Err: validate.Blank()}, {Field: "city", Err: validate.TooLong(30)}}

	tests := []struct {
		name  string
		field string
		err   error
		want  validate.Errors
	}{
		{name: "nil", field: "address", err: nil, want: nil},
		{
			name:  "Errors move under the field",
			field: "address",
			err:   inner,
			want: validate.Errors{
				{Field: "address.zip", Err: validate.Blank()},
				{Field: "address.city", Err: validate.TooLong(30)},
			},
		},
		{
			// An embedded struct without a JSON name keeps its fields at the
			// level of the struct embedding it.
			name:  "an empty field keeps the paths",
			field: "",
			err:   inner,
			want:  inner,
		},
		{
			name:  "element of a slice",
			field: validate.Index("items", 2),
			err:   validate.Errors{{Field: "sku", Err: validate.Blank()}},
			want:  validate.Errors{{Field: "items[2].sku", Err: validate.Blank()}},
		},
		{
			name:  "a Coded error lands on the field",
			field: "email",
			err:   &typeError{code: "email"},
			want:  validate.Errors{{Field: "email", Err: &typeError{code: "email"}}},
		},
		{
			name:  "a plain error lands on the field",
			field: "address",
			err:   errors.New("lookup failed"),
			want:  validate.Errors{{Field: "address", Err: errors.New("lookup failed")}},
		},
		{
			name:  "an error without a field lands on base",
			field: "",
			err:   errors.New("lookup failed"),
			want:  validate.Errors{{Field: validate.Base, Err: errors.New("lookup failed")}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var got validate.Errors
			got.Nest(tt.field, tt.err)
			if !equalErrors(got, tt.want) {
				t.Errorf("Nest(%q) = %v, want %v", tt.field, got, tt.want)
			}
		})
	}
}

func TestPaths(t *testing.T) {
	t.Parallel()

	tests := []struct {
		got, want string
	}{
		{got: validate.Index("tags", 2), want: "tags[2]"},
		{got: validate.Key("labels", "primary"), want: "labels.primary"},
		{got: validate.Key("scores", 7), want: "scores.7"},
		{got: validate.Index(validate.Key("groups", "admin"), 0), want: "groups.admin[0]"},
	}
	for _, tt := range tests {
		if tt.got != tt.want {
			t.Errorf("path = %q, want %q", tt.got, tt.want)
		}
	}
}

func TestErrors_Err(t *testing.T) {
	t.Parallel()

	// A nil error and not an Errors holding nothing, which would compare
	// non-nil.
	if err := (validate.Errors{}).Err(); err != nil {
		t.Errorf("Err() of no errors = %v, want nil", err)
	}

	errs := validate.Errors{{Field: "name", Err: validate.Blank()}}
	got, ok := errors.AsType[validate.Errors](errs.Err())
	if !ok || !equalErrors(got, errs) {
		t.Errorf("Err() = %v, want the errors themselves", errs.Err())
	}
}

func TestErrors_Error(t *testing.T) {
	t.Parallel()

	errs := validate.Errors{
		{Field: "name", Err: validate.Blank()},
		{Field: "tags[2]", Err: validate.TooLong(20)},
		{Field: "address", Err: errors.New("lookup failed")},
	}

	want := "validate: name: blank; tags[2]: too_long (count=20); address: lookup failed"
	if got := errs.Error(); got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

func TestErrors_UnwrapReachesTheRulesError(t *testing.T) {
	t.Parallel()

	errs := validate.Errors{{Field: "name", Err: validate.Blank()}, {Field: "email", Err: &typeError{code: "email"}}}

	if got, ok := errors.AsType[*typeError](errs.Err()); !ok || got.code != "email" {
		t.Errorf("errors.AsType found %v, %v, want the email error", got, ok)
	}
}

// FieldError is what go-kanna/i18n renders, through the methods it asks for.
var _ interface {
	Path() string
	Code() string
	Params() map[string]any
} = validate.FieldError{}

func TestFieldError_Path(t *testing.T) {
	t.Parallel()

	if got := (validate.FieldError{Field: "tags[1]"}).Path(); got != "tags[1]" {
		t.Errorf("Path() = %q, want %q", got, "tags[1]")
	}
}

func TestFieldError_Code(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		err        error
		wantCode   string
		wantParams map[string]any
	}{
		{name: "built-in", err: validate.TooLong(20), wantCode: "too_long", wantParams: map[string]any{"count": 20}},
		{name: "Coded from elsewhere", err: &typeError{code: "email"}, wantCode: "email"},
		{name: "not Coded", err: errors.New("lookup failed"), wantCode: "invalid"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			fe := validate.FieldError{Field: "x", Err: tt.err}
			if fe.Code() != tt.wantCode || !maps.Equal(fe.Params(), tt.wantParams) {
				t.Errorf("Code(), Params() = %q, %v, want %q, %v", fe.Code(), fe.Params(), tt.wantCode, tt.wantParams)
			}
			if !errors.Is(fe, tt.err) {
				t.Error("errors.Is does not reach Err")
			}
		})
	}
}

func TestErrors_MarshalJSON(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		errs validate.Errors
		want string
	}{
		{name: "nil", errs: nil, want: `{}`},
		{
			name: "fields in the order they first broke",
			errs: validate.Errors{
				{Field: "password", Err: validate.Blank()},
				{Field: "name", Err: validate.Blank()},
				{Field: "password", Err: validate.TooShort(12)},
			},
			want: `{"password":[{"error":"blank"},{"error":"too_short","count":12}],"name":[{"error":"blank"}]}`,
		},
		{
			name: "params in key order beside the code",
			errs: validate.Errors{{Field: "confirm", Err: validate.FailWith("between", map[string]any{"min": 1, "max": 9})}},
			want: `{"confirm":[{"error":"between","max":9,"min":1}]}`,
		},
		{
			name: "a param that is a map, in key order too",
			errs: validate.Errors{{
				Field: "x",
				Err:   validate.FailWith("odd", map[string]any{"range": map[string]int{"to": 9, "from": 1}}),
			}},
			want: `{"x":[{"error":"odd","range":{"from":1,"to":9}}]}`,
		},
		{
			name: "a param named error is left out",
			errs: validate.Errors{{Field: "x", Err: validate.FailWith("odd", map[string]any{"error": "shadow"})}},
			want: `{"x":[{"error":"odd"}]}`,
		},
		{
			name: "an error that is not Coded",
			errs: validate.Errors{{Field: "address", Err: errors.New("lookup failed")}},
			want: `{"address":[{"error":"invalid"}]}`,
		},
		{
			// A map key spells part of the path, and may be any bytes.
			name: "invalid UTF-8 in a field",
			errs: validate.Errors{{Field: "labels.\xff", Err: validate.Blank()}},
			want: `{"labels.` + "�" + `":[{"error":"blank"}]}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			v1, err := jsonv1.Marshal(tt.errs)
			if err != nil {
				t.Fatalf("encoding/json: %v", err)
			}
			v2, err := json.Marshal(tt.errs)
			if err != nil {
				t.Fatalf("encoding/json/v2: %v", err)
			}
			// What an encoder that knows only json.Marshaler gets.
			direct, err := tt.errs.MarshalJSON()
			if err != nil {
				t.Fatalf("MarshalJSON: %v", err)
			}
			if string(v1) != tt.want || string(v2) != tt.want || string(direct) != tt.want {
				t.Errorf("encoding/json gives %s, encoding/json/v2 %s, and MarshalJSON %s, want %s", v1, v2, direct, tt.want)
			}
		})
	}
}

// failingWriter refuses every write.
type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("disk full") }

func TestErrors_MarshalJSONTo_ReportsTheWriter(t *testing.T) {
	t.Parallel()

	errs := validate.Errors{{Field: "name", Err: validate.Blank()}}

	err := errs.MarshalJSONTo(jsontext.NewEncoder(failingWriter{}))
	if err == nil || !strings.Contains(err.Error(), "disk full") {
		t.Errorf("MarshalJSONTo() error = %v, want the writer's", err)
	}
}

func TestErrors_MarshalJSON_UnencodableParam(t *testing.T) {
	t.Parallel()

	errs := validate.Errors{{Field: "x", Err: validate.FailWith("odd", map[string]any{"f": func() {}})}}

	_, err := json.Marshal(errs)
	if err == nil || !strings.Contains(err.Error(), `param "f" of x`) {
		t.Errorf("Marshal() error = %v, want one naming the param and its field", err)
	}
}
