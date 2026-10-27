package validate

import (
	"cmp"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
)

// Base is the field of an error that concerns the struct as a whole rather than
// one of its fields, as in Rails.
const Base = "base"

// FieldError is a broken rule together with the field it broke on.
type FieldError struct {
	// Field is the path to the field, spelled with JSON names, such as
	// "address.zip" or "tags[2]"; or Base.
	Field string

	// Err is the rule's error. It is usually Coded; one that is not reports the
	// code "invalid", Rails' catch-all.
	Err error
}

// Code returns the code of Err, or "invalid" when Err carries none.
func (e FieldError) Code() string {
	if c, ok := errors.AsType[Coded](e.Err); ok {
		return c.Code()
	}
	return "invalid"
}

// Params returns the params of Err, or nil when Err carries none.
func (e FieldError) Params() map[string]any {
	if c, ok := errors.AsType[Coded](e.Err); ok {
		return c.Params()
	}
	return nil
}

// Error formats the field followed by the code and params, such as
// "tags[2]: too_long (count=20)". An error that is not Coded contributes its own
// text instead.
func (e FieldError) Error() string {
	if _, ok := errors.AsType[Coded](e.Err); !ok {
		return e.Field + ": " + e.Err.Error()
	}
	return e.Field + ": " + describe(e.Code(), e.Params())
}

// Unwrap returns Err.
func (e FieldError) Unwrap() error {
	return e.Err
}

// Errors collects every rule broken while validating a value, in the order they
// were found. A generated Validate method returns it through Err.
type Errors []FieldError

// Add records err as broken on field. A nil err is ignored, so that the result
// of a rule can be passed as it is.
func (e *Errors) Add(field string, err error) {
	if err != nil {
		*e = append(*e, FieldError{Field: field, Err: err})
	}
}

// Nest records what validating the value of field reported. The fields of
// Errors move under field ("address" and "zip" make "address.zip"), and an
// empty field leaves them where they are, as for an embedded struct. Any other
// error is recorded on field itself, or on Base when field is empty. A nil err
// is ignored.
func (e *Errors) Nest(field string, err error) {
	if err == nil {
		return
	}
	nested, ok := errors.AsType[Errors](err)
	if !ok {
		e.Add(cmp.Or(field, Base), err)
		return
	}
	for _, fe := range nested {
		path := fe.Field
		if field != "" {
			path = field + "." + fe.Field
		}
		e.Add(path, fe.Err)
	}
}

// Index returns the path to element i of the slice at field, such as "tags[2]".
func Index(field string, i int) string {
	return field + "[" + strconv.Itoa(i) + "]"
}

// Key returns the path to the value under key in the map at field, such as
// "labels.primary": a map is a JSON object, so its keys are spelled as the
// names of an object are, as DecodeJSON reports them.
func Key[K comparable](field string, key K) string {
	return field + "." + fmt.Sprint(key)
}

// Err returns e, or nil when nothing broke. Returning e itself would hand the
// caller a non-nil error holding nothing.
func (e Errors) Err() error {
	if len(e) == 0 {
		return nil
	}
	return e
}

// Error lists every broken rule, such as
// "validate: name: blank; tags[2]: too_long (count=20)".
func (e Errors) Error() string {
	parts := make([]string, len(e))
	for i, fe := range e {
		parts[i] = fe.Error()
	}
	return "validate: " + strings.Join(parts, "; ")
}

// Unwrap returns each FieldError, so that errors.As finds the rule's error
// behind any of them.
func (e Errors) Unwrap() []error {
	errs := make([]error, len(e))
	for i, fe := range e {
		errs[i] = fe
	}
	return errs
}

// MarshalJSON is MarshalJSONTo for an encoder that knows only json.Marshaler.
// encoding/json itself calls MarshalJSONTo.
func (e Errors) MarshalJSON() ([]byte, error) {
	return json.Marshal(e) //nolint:wrapcheck // MarshalJSONTo already says what failed
}

// MarshalJSONTo encodes e as Rails encodes errors.details: an object keyed by
// field, in the order the fields first broke, each holding a list with the code
// under "error" and the params beside it, in key order.
//
//	{"name":[{"error":"blank"}],"tags[2]":[{"error":"too_long","count":20}]}
//
// A param named "error" would hide the code, so it is left out.
func (e Errors) MarshalJSONTo(enc *jsontext.Encoder) error {
	var fields []string
	byField := make(map[string][]FieldError)
	for _, fe := range e {
		if _, seen := byField[fe.Field]; !seen {
			fields = append(fields, fe.Field)
		}
		byField[fe.Field] = append(byField[fe.Field], fe)
	}

	w := tokens{enc: enc}
	w.write(jsontext.BeginObject)
	for _, field := range fields {
		w.write(text(field))
		w.write(jsontext.BeginArray)
		for _, fe := range byField[field] {
			w.detail(fe)
		}
		w.write(jsontext.EndArray)
	}
	w.write(jsontext.EndObject)
	return w.err
}

// tokens writes to an encoder until the first failure, which it keeps.
type tokens struct {
	enc *jsontext.Encoder
	err error
}

func (w *tokens) write(t jsontext.Token) {
	if w.err == nil {
		if err := w.enc.WriteToken(t); err != nil {
			w.err = fmt.Errorf("validate: %w", err)
		}
	}
}

// detail writes one entry of a field's list.
func (w *tokens) detail(fe FieldError) {
	w.write(jsontext.BeginObject)
	w.write(text("error"))
	w.write(text(fe.Code()))
	params := fe.Params()
	for _, k := range slices.Sorted(maps.Keys(params)) {
		if k == "error" {
			continue
		}
		w.write(text(k))
		if w.err == nil {
			if err := json.MarshalEncode(w.enc, params[k], json.Deterministic(true)); err != nil {
				w.err = fmt.Errorf("validate: param %q of %s: %w", k, fe.Field, err)
			}
		}
	}
	w.write(jsontext.EndObject)
}

// text is a JSON string holding s, with invalid UTF-8 replaced as
// encoding/json would rather than refused.
func text(s string) jsontext.Token {
	return jsontext.String(strings.ToValidUTF8(s, "\uFFFD"))
}
