package validate

import (
	"cmp"
	"encoding"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"
)

// DecodeJSON decodes the JSON value r holds into v, which must be a non-nil
// pointer, and reports everything wrong with it at once.
//
// A value whose type rejects it with a Coded error, as the types of
// go-kanna/validated do, does not stop the decoding: the error is recorded at
// the value's path and decoding goes on. The rejection may come from any method
// encoding/json/v2 decodes a type with: UnmarshalJSONFrom, UnmarshalJSON, or
// UnmarshalText. Then, when v has a Validate method, what it finds is added,
// except at and under the paths decoding rejected. Those values were never set,
// and checking them would only report the absence of a value that was in fact
// sent.
//
// The result is the Errors of both, or nil. Anything else fails with
// encoding/json/v2's own error: JSON that is malformed, holds more than one
// value, or does not fit the Go types, and a rejection that is not Coded or
// that falls on a map key. Such a request cannot be read, rather than breaking
// a rule.
//
// Decoding follows encoding/json/v2, which, unlike encoding/json, matches names
// case-sensitively and rejects duplicate names.
func DecodeJSON(r io.Reader, v any) error {
	var d decoding
	err := json.UnmarshalRead(r, v, json.WithUnmarshalers(json.JoinUnmarshalers(
		// In the order encoding/json/v2 prefers the methods.
		json.UnmarshalFromFunc(d.unmarshalJSONFrom),
		json.UnmarshalFromFunc(d.unmarshalJSON),
		json.UnmarshalFromFunc(d.unmarshalText),
	)))
	if err != nil {
		return fmt.Errorf("validate: %w", err)
	}

	x, ok := v.(interface{ Validate() error })
	if !ok {
		return d.errs.Err()
	}
	var found Errors
	found.Nest("", x.Validate())
	for _, fe := range found {
		if !slices.ContainsFunc(d.rejected, func(path string) bool { return within(fe.Field, path) }) {
			d.errs = append(d.errs, fe)
		}
	}
	return d.errs.Err()
}

// decoding calls the methods types decode themselves with, as encoding/json/v2
// would, but records a Coded rejection and goes on instead of stopping.
type decoding struct {
	errs Errors

	// rejected holds the path of each of errs, with "" rather than Base for
	// the top-level value.
	rejected []string
}

func (d *decoding) unmarshalJSONFrom(dec *jsontext.Decoder, u json.UnmarshalerFrom) error {
	if !handles(dec, u) {
		return errors.ErrUnsupported
	}
	depth := dec.StackDepth()
	_, before := dec.StackIndex(depth)
	err := u.UnmarshalJSONFrom(dec)
	if dec.StackDepth() == depth {
		if _, after := dec.StackIndex(depth); after == before+1 {
			return d.record(dec, err)
		}
	}
	// It stopped partway through the value, so decoding cannot go on.
	return err //nolint:wrapcheck // encoding/json/v2 reports it with the position it occurred at
}

func (d *decoding) unmarshalJSON(dec *jsontext.Decoder, u json.Unmarshaler) error {
	if !handles(dec, u) {
		return errors.ErrUnsupported
	}
	raw, err := dec.ReadValue()
	if err != nil {
		return err //nolint:wrapcheck // encoding/json/v2 reports it with the offset it occurred at
	}
	return d.record(dec, u.UnmarshalJSON(raw))
}

func (d *decoding) unmarshalText(dec *jsontext.Decoder, u encoding.TextUnmarshaler) error {
	// encoding/json/v2 calls UnmarshalText only for a string. It decodes null
	// and rejects anything else on its own.
	if dec.PeekKind() != jsontext.KindString || !handles(dec, u) {
		return errors.ErrUnsupported
	}
	tok, err := dec.ReadToken()
	if err != nil {
		return err //nolint:wrapcheck // encoding/json/v2 reports it with the offset it occurred at
	}
	return d.record(dec, u.UnmarshalText([]byte(tok.String())))
}

// record keeps err at the path of the value dec has just read if it is Coded,
// so that decoding goes on, and hands back anything else.
func (d *decoding) record(dec *jsontext.Decoder, err error) error {
	if _, ok := errors.AsType[Coded](err); !ok {
		return err // nil, or for encoding/json/v2 to report with the value's position
	}
	path := pathOf(dec)
	d.errs.Add(cmp.Or(path, Base), err)
	d.rejected = append(d.rejected, path)
	return nil
}

// handles reports whether DecodeJSON calls the method of v itself, rather than
// leaving the value to encoding/json/v2. It leaves alone a map key, which
// cannot be skipped like a value, and time.Time, which encoding/json/v2 decodes
// on its own rather than through its methods.
func handles(dec *jsontext.Decoder, v any) bool {
	if _, ok := v.(*time.Time); ok {
		return false
	}
	// An object counts its names and values alike, so a name comes after an
	// even count.
	kind, n := dec.StackIndex(dec.StackDepth())
	return kind != jsontext.KindBeginObject || n%2 == 1
}

// pathOf spells the path to the value dec has just read with JSON names, such
// as "address.zip" or "tags[1]", or returns "" for the top-level value.
//
// A token of a JSON pointer does not tell an array index from an object name,
// so the kind of each level comes from the decoder's stack, which has one level
// per token.
func pathOf(dec *jsontext.Decoder) string {
	var path string
	level := 1
	for token := range dec.StackPointer().Tokens() {
		switch kind, _ := dec.StackIndex(level); {
		case kind == jsontext.KindBeginArray:
			path += "[" + token + "]"
		case path == "":
			path = token
		default:
			path += "." + token
		}
		level++
	}
	return path
}

// within reports whether field is path or lies under it. Every field lies
// under "", the top-level value.
func within(field, path string) bool {
	rest, ok := strings.CutPrefix(field, path)
	return ok && (path == "" || rest == "" || rest[0] == '.' || rest[0] == '[')
}
