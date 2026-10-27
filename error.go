package validate

import (
	"fmt"
	"maps"
	"slices"
	"strings"
)

// Coded is an error that names the rule a value broke.
//
// The code is stable enough to key a translated message or an API response, and
// the params carry what such a message needs. *validated.Error from
// github.com/go-kanna/validated is Coded too, which is how a rule held in a type
// and one checked by a tag end up in the same Errors without either package
// importing the other.
type Coded interface {
	error
	Code() string
	Params() map[string]any
}

// Error is the Coded error the built-in rules report. A validate hook reports
// its own through Fail and FailWith.
//
// Error is used through a pointer: its params make it incomparable, so
// comparing two Error values with == would panic, while pointers compare by
// identity.
type Error struct {
	code   string
	params map[string]any
}

// Fail returns an Error with code.
func Fail(code string) *Error {
	return &Error{code: code}
}

// FailWith is Fail with params for the message. params is copied, so the caller
// may go on to change it.
func FailWith(code string, params map[string]any) *Error {
	return &Error{code: code, params: maps.Clone(params)}
}

// Code returns the code naming the broken rule.
func (e *Error) Code() string {
	return e.code
}

// Params returns a copy of the params, or nil when there are none.
func (e *Error) Params() map[string]any {
	return maps.Clone(e.params)
}

// Error formats the code and then the params in key order, such as
// "validate: too_long (count=50)".
func (e *Error) Error() string {
	return "validate: " + describe(e.code, e.params)
}

// number is what a numeric rule compares against. The count it reports keeps
// the type of the field it was checked on.
type number interface {
	~int | ~int8 | ~int16 | ~int32 | ~int64 |
		~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64 | ~uintptr |
		~float32 | ~float64
}

// The built-in rules report the errors below, each named after the Rails error
// it stands for.

// Blank reports a required value left at its zero value.
func Blank() *Error { return Fail("blank") }

// TooShort reports a length below count.
func TooShort(count int) *Error { return withCount("too_short", count) }

// TooLong reports a length above count.
func TooLong(count int) *Error { return withCount("too_long", count) }

// WrongLength reports a length other than count.
func WrongLength(count int) *Error { return withCount("wrong_length", count) }

// GreaterThan reports a number that is not greater than count.
func GreaterThan[N number](count N) *Error { return withCount("greater_than", count) }

// GreaterThanOrEqualTo reports a number below count.
func GreaterThanOrEqualTo[N number](count N) *Error {
	return withCount("greater_than_or_equal_to", count)
}

// LessThan reports a number that is not less than count.
func LessThan[N number](count N) *Error { return withCount("less_than", count) }

// LessThanOrEqualTo reports a number above count.
func LessThanOrEqualTo[N number](count N) *Error { return withCount("less_than_or_equal_to", count) }

// EqualTo reports a number other than count.
func EqualTo[N number](count N) *Error { return withCount("equal_to", count) }

// OtherThan reports a number equal to count.
func OtherThan[N number](count N) *Error { return withCount("other_than", count) }

// Inclusion reports a value outside the allowed set.
func Inclusion() *Error { return Fail("inclusion") }

// Confirmation reports a value that does not match the field it confirms, whose
// JSON name is attribute.
func Confirmation(attribute string) *Error {
	return &Error{code: "confirmation", params: map[string]any{"attribute": attribute}}
}

// Invalid reports a value in the wrong format.
func Invalid() *Error { return Fail("invalid") }

func withCount(code string, count any) *Error {
	return &Error{code: code, params: map[string]any{"count": count}}
}

// describe formats a code and its params in key order, such as
// "too_long (count=50)".
func describe(code string, params map[string]any) string {
	if len(params) == 0 {
		return code
	}

	var b strings.Builder
	b.WriteString(code)
	b.WriteString(" (")
	for i, k := range slices.Sorted(maps.Keys(params)) {
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "%s=%v", k, params[k])
	}
	b.WriteByte(')')
	return b.String()
}
