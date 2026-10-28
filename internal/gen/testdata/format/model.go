// Package format uses the rules of github.com/go-kanna/value/rule, which this
// module does not require, so the emit test compares its output without
// compiling it.
package format

type ID string

//kanna:validate
type Contact struct {
	Email   string  `json:"email"   validate:"required,email"`
	Website *string `json:"website" validate:"url"`
	ID      ID      `json:"id"      validate:"uuid"`
}
