// Package user declares the request a sign-up endpoint accepts and the rules
// it has to meet. kanna-validate turns the validate tags into the Validate
// methods in validate_gen.go.
package user

import (
	"strings"

	"github.com/go-kanna/validate"
	"github.com/go-kanna/validated"
	"github.com/go-kanna/validated/rules"
)

//go:generate go tool kanna-validate

// Email is an address checked when it is built or decoded, so the tag below
// only has to say that one is required.
type Email = validated.String[rules.Email]

// CreateUserRequest is the body of a sign-up request.
//
//kanna:validate
type CreateUserRequest struct {
	Name     string   `json:"name"     validate:"required,max=50"`
	Email    Email    `json:"email"    validate:"required"`
	Age      int      `json:"age"      validate:"gte=18,lte=130"`
	Role     string   `json:"role"     validate:"required,oneof=admin member guest"`
	Tags     []string `json:"tags"     validate:"max=5,dive,max=20"`
	Website  *string  `json:"website"  validate:"url"`
	Address  Address  `json:"address"  validate:"required"`
	Password string   `json:"password" validate:"required,min=12"`
	Confirm  string   `json:"confirm"  validate:"eqfield=Password"`
}

// Address is where a user lives.
//
//kanna:validate
type Address struct {
	Zip  string `json:"zip"  validate:"required,pattern=^\\d{3}-\\d{4}$"`
	City string `json:"city" validate:"required"`
}

// validate is a hook: the generated Validate calls it after every tag has been
// checked, and what it adds to errs is reported with the rest. It is the place
// for a rule a tag cannot express, such as this one, which depends on two
// fields at once: an admin must sign up with a company address.
func (x CreateUserRequest) validate(errs *validate.Errors) {
	if x.Role == "admin" && !strings.HasSuffix(x.Email.Get(), "@example.com") {
		errs.Add("email", validate.Fail("corp_domain"))
	}
}
