// Package bench measures the Validate methods kanna-validate generates against
// go-playground/validator, both reading the same tags of one struct. The tags
// keep to rules the two check alike, so that both do the same work.
//
// Regenerate with:
//
//	go generate ./...
package bench

//go:generate go tool kanna-validate

// CreateUserRequest is the request of the README, with len=8 in place of
// pattern=, which go-playground/validator does not have.
//
//kanna:validate
type CreateUserRequest struct {
	Name     string   `json:"name"     validate:"required,max=50"`
	Age      int      `json:"age"      validate:"gte=18,lte=130"`
	Role     string   `json:"role"     validate:"required,oneof=admin member guest"`
	Tags     []string `json:"tags"     validate:"max=5,dive,max=20"`
	Address  Address  `json:"address"  validate:"required"`
	Password string   `json:"password" validate:"required,min=12"`
	Confirm  string   `json:"confirm"  validate:"eqfield=Password"`
}

// Address is where a user lives.
//
//kanna:validate
type Address struct {
	Zip  string `json:"zip"  validate:"required,len=8"`
	City string `json:"city" validate:"required"`
}
