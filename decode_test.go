package validate_test

import (
	jsonv1 "encoding/json"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/go-kanna/validate"
)

// email stands in for validated.String[rules.Email]: its UnmarshalJSON rejects
// a value with a Coded error and leaves the field as it was.
type email struct{ v string }

func (e *email) UnmarshalJSON(data []byte) error {
	var s string
	if err := jsonv1.Unmarshal(data, &s); err != nil {
		return err //nolint:wrapcheck // the test expects encoding/json's own error back
	}
	if !strings.Contains(s, "@") {
		return &typeError{code: "email"}
	}
	e.v = s
	return nil
}

// role stands in for an enum that checks itself in UnmarshalText.
type role string

func (r *role) UnmarshalText(text []byte) error {
	if s := role(text); s == "admin" || s == "member" {
		*r = s
		return nil
	}
	return validate.Inclusion()
}

// period stands in for a type written for encoding/json/v2, which decodes
// itself from the decoder. It rejects a period that ends before it starts.
type period struct {
	From int `json:"from"`
	To   int `json:"to"`
}

func (p *period) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
	type plain period
	var v plain
	if err := json.UnmarshalDecode(dec, &v); err != nil {
		return err //nolint:wrapcheck // encoding/json/v2 reports it with its position
	}
	if v.To < v.From {
		return validate.Fail("backwards")
	}
	*p = period(v)
	return nil
}

// Validate stands in for `validate:"required"` on both ends.
func (p period) Validate() error {
	var errs validate.Errors
	if p.From == 0 {
		errs.Add("from", validate.Blank())
	}
	if p.To == 0 {
		errs.Add("to", validate.Blank())
	}
	return errs.Err()
}

type address struct {
	Zip email `json:"zip"`
}

type signUp struct {
	Email   email            `json:"email"`
	Name    string           `json:"name"`
	Role    role             `json:"role"`
	Stay    period           `json:"stay"`
	Address address          `json:"address"`
	Backups []email          `json:"backups"`
	Labels  map[string]email `json:"labels"`
}

// Validate stands in for what kanna-validate generates for
// `validate:"required"` on Email, Name, and Role, and for the nested Stay.
func (s signUp) Validate() error {
	var errs validate.Errors
	if s.Email.v == "" {
		errs.Add("email", validate.Blank())
	}
	if s.Name == "" {
		errs.Add("name", validate.Blank())
	}
	if s.Role == "" {
		errs.Add("role", validate.Blank())
	}
	errs.Nest("stay", s.Stay.Validate())
	return errs.Err()
}

func TestDecodeJSON(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want validate.Errors
	}{
		{
			name: "valid",
			in:   `{"email":"a@example.com","name":"kanna","role":"admin","stay":{"from":1,"to":2}}`,
			want: nil,
		},
		{
			name: "every rejection with its path, then what Validate finds",
			in: `{"email":"x","name":"","role":"root","stay":{"from":2,"to":1},"address":{"zip":"y"},` +
				`"backups":["a@example.com","z"],"labels":{"home":"w"}}`,
			want: validate.Errors{
				{Field: "email", Err: &typeError{code: "email"}},
				{Field: "role", Err: validate.Inclusion()},
				{Field: "stay", Err: validate.Fail("backwards")},
				{Field: "address.zip", Err: &typeError{code: "email"}},
				{Field: "backups[1]", Err: &typeError{code: "email"}},
				{Field: "labels.home", Err: &typeError{code: "email"}},
				{Field: "name", Err: validate.Blank()},
			},
		},
		{
			name: "Validate on fields that were never sent",
			in:   `{"name":"kanna","stay":{"to":2}}`,
			want: validate.Errors{
				{Field: "email", Err: validate.Blank()},
				{Field: "role", Err: validate.Blank()},
				{Field: "stay.from", Err: validate.Blank()},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var got signUp
			err := validate.DecodeJSON(strings.NewReader(tt.in), &got)
			if tt.want == nil {
				if err != nil {
					t.Fatalf("DecodeJSON() error = %v, want nil", err)
				}
				return
			}

			errs, ok := errors.AsType[validate.Errors](err)
			if !ok || !equalErrors(errs, tt.want) {
				t.Errorf("DecodeJSON() error = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestDecodeJSON_KeepsDecoding(t *testing.T) {
	t.Parallel()

	got := signUp{Email: email{v: "old@example.com"}}
	if err := validate.DecodeJSON(strings.NewReader(`{"email":"x","name":"kanna"}`), &got); err == nil {
		t.Fatal("DecodeJSON() error = nil, want the rejected email")
	}
	if got.Email.v != "old@example.com" {
		t.Errorf("Email = %q, want it left at %q", got.Email.v, "old@example.com")
	}
	if got.Name != "kanna" {
		t.Errorf("Name = %q, want %q decoded after the rejected email", got.Name, "kanna")
	}
}

type mailbox struct {
	Email       email `json:"email"`
	EmailBackup email `json:"emailBackup"`
}

// Validate stands in for `validate:"required"` on both.
func (m mailbox) Validate() error {
	var errs validate.Errors
	if m.Email.v == "" {
		errs.Add("email", validate.Blank())
	}
	if m.EmailBackup.v == "" {
		errs.Add("emailBackup", validate.Blank())
	}
	return errs.Err()
}

func TestDecodeJSON_KeepsWhatIsBesideARejection(t *testing.T) {
	t.Parallel()

	var got mailbox
	err := validate.DecodeJSON(strings.NewReader(`{"email":"x"}`), &got)

	// emailBackup starts like email but does not lie under it.
	want := validate.Errors{
		{Field: "email", Err: &typeError{code: "email"}},
		{Field: "emailBackup", Err: validate.Blank()},
	}
	if errs, ok := errors.AsType[validate.Errors](err); !ok || !equalErrors(errs, want) {
		t.Errorf("DecodeJSON() error = %v, want %v", err, want)
	}
}

func TestDecodeJSON_TopLevel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		v    any
		in   string
		want validate.Errors
	}{
		{
			name: "without Validate",
			v:    new(email),
			in:   `"x"`,
			want: validate.Errors{{Field: validate.Base, Err: &typeError{code: "email"}}},
		},
		{
			// Nothing was set, so whatever Validate finds is left out.
			name: "with Validate",
			v:    new(period),
			in:   `{"from":2,"to":1}`,
			want: validate.Errors{{Field: validate.Base, Err: validate.Fail("backwards")}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := validate.DecodeJSON(strings.NewReader(tt.in), tt.v)
			if errs, ok := errors.AsType[validate.Errors](err); !ok || !equalErrors(errs, tt.want) {
				t.Errorf("DecodeJSON() error = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestDecodeJSON_WithoutValidate(t *testing.T) {
	t.Parallel()

	var got address
	err := validate.DecodeJSON(strings.NewReader(`{"zip":"x"}`), &got)

	want := validate.Errors{{Field: "zip", Err: &typeError{code: "email"}}}
	if errs, ok := errors.AsType[validate.Errors](err); !ok || !equalErrors(errs, want) {
		t.Errorf("DecodeJSON() error = %v, want %v", err, want)
	}
}

func TestDecodeJSON_LeavesTimeToEncodingJSON(t *testing.T) {
	t.Parallel()

	// encoding/json/v2 sets a time to zero on null, where Time.UnmarshalJSON
	// would leave it as it was.
	got := struct {
		At time.Time `json:"at"`
	}{At: time.Unix(1, 0)}
	if err := validate.DecodeJSON(strings.NewReader(`{"at":null}`), &got); err != nil {
		t.Fatalf("DecodeJSON() error = %v, want nil", err)
	}
	if !got.At.IsZero() {
		t.Errorf("At = %v, want the zero time", got.At)
	}
}

// impatient rejects a value before it has read all of it.
type impatient struct{}

func (*impatient) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
	if _, err := dec.ReadToken(); err != nil {
		return err //nolint:wrapcheck // encoding/json/v2 reports it with its position
	}
	return validate.Invalid()
}

func TestDecodeJSON_Unreadable(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		v    any
		in   string
		as   func(error) bool
	}{
		{
			name: "malformed",
			v:    new(signUp),
			in:   `{"email":`,
			as:   isError[*jsontext.SyntacticError],
		},
		{
			name: "more than one value",
			v:    new(signUp),
			in:   `{"name":"kanna"} {}`,
			as:   isError[*jsontext.SyntacticError],
		},
		{
			name: "does not fit the Go type",
			v:    new(signUp),
			in:   `{"name":1}`,
			as:   isError[*json.SemanticError],
		},
		{
			name: "does not fit the type behind an UnmarshalJSON",
			v:    new(signUp),
			in:   `{"email":1}`,
			as:   isError[*jsonv1.UnmarshalTypeError],
		},
		{
			name: "not a string for an UnmarshalText",
			v:    new(signUp),
			in:   `{"role":1}`,
			as:   isError[*json.SemanticError],
		},
		{
			name: "a rejected map key",
			v:    new(map[role]int),
			in:   `{"root":1}`,
			as:   isError[*json.SemanticError],
		},
		{
			name: "a rejection partway through a value",
			v:    new(struct{ X impatient }),
			in:   `{"X":[1,2]}`,
			as:   isError[*json.SemanticError],
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := validate.DecodeJSON(strings.NewReader(tt.in), tt.v)
			if _, ok := errors.AsType[validate.Errors](err); ok || !tt.as(err) {
				t.Errorf("DecodeJSON() error = %v (%T), want encoding/json's own error", err, err)
			}
		})
	}
}

func isError[E error](err error) bool {
	_, ok := errors.AsType[E](err)
	return ok
}
