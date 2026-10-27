// Package gen generates Validate methods from the validate tags of structs
// marked //kanna:validate. cmd/kanna-validate runs it.
//
// The tags are written as for go-playground/validator, but nothing reads them
// at run time: each rule becomes plain Go in the generated method, reporting
// Rails' codes through the runtime at the root of this module. A rule that does
// not exist or does not fit its field stops generation instead.
package gen
