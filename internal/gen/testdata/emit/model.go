// Package emit holds one of every construct kanna-validate generates. Its
// validate_gen.go is the golden file of the emit test, and the test calls the
// methods it declares.
package emit

import (
	"time"

	"github.com/go-kanna/validate"
	"github.com/go-kanna/validate/internal/gen/testdata/emit/schedule"
)

// patternRequestZip takes the name the pattern of Request.Zip would get.
var patternRequestZip = "taken"

type Role string

type Point struct{ X, Y int }

// Item validates itself by hand.
type Item struct{ SKU string }

func (i Item) Validate() error {
	if i.SKU == "" {
		return validate.Errors{{Field: "sku", Err: validate.Blank()}}
	}
	return nil
}

// Span validates itself and knows when it is empty.
type Span struct{ From, To int }

func (s Span) IsZero() bool { return s == Span{} }

func (s Span) Validate() error {
	if s.To < s.From {
		return validate.Fail("inverted")
	}
	return nil
}

// Checker is an interface whose values validate themselves.
type Checker interface{ Validate() error }

// Tree contains itself, and nothing in it validates itself.
type Tree map[string]Tree

// Since2000 names the rule of package schedule in this one too, so that a tag
// can name it either way.
type Since2000 = schedule.Since2000

//kanna:validate
type Request struct {
	Name     string           `json:"name"      validate:"required,min=1,max=50"`
	Age      int              `json:"age"       validate:"gte=18,lte=130"`
	Level    uint8            `json:"level"     validate:"oneof=1 2 3"`
	Quantity uint16           `json:"quantity"  validate:"max=1000"`
	Score    float64          `json:"score"     validate:"gt=0"`
	Role     Role             `json:"role"      validate:"required,oneof=admin member"`
	Nickname *string          `json:"nickname"  validate:"min=2"`
	Tags     []string         `json:"tags"      validate:"max=5,dive,required,max=20"`
	Matrix   [][]int          `json:"matrix"    validate:"dive,dive,gte=0"`
	Labels   map[string]Role  `json:"labels"    validate:"dive,max=10"`
	Zip      string           `json:"zip"       validate:"omitempty,pattern=^\\d{3}-\\d{4}$"`
	PostCode string           `json:"post_code" validate:"omitempty,pattern=^\\d{3}-\\d{4}$"`
	Code     Role             `json:"code"      validate:"pattern=^[A-Z]+$"`
	Password string           `json:"password"  validate:"required"`
	Confirm  string           `json:"confirm"   validate:"eqfield=Password"`
	Active   bool             `json:"active"    validate:"required"`
	When     time.Time        `json:"when"      validate:"required"`
	Point    Point            `json:"point"     validate:"required"`
	Grid     [2]Point         `json:"grid"      validate:"dive,required"`
	Address  Address          `json:"address"`
	Billing  *Address         `json:"billing"   validate:"required"`
	Items    []Item           `json:"items"     validate:"min=1"`
	ByName   map[string]*Item `json:"by_name"`
	Extra    Checker          `json:"extra"`
	Count    int              `json:"count"     validate:"required"`
	List     []string         `json:"list"      validate:"required"`
	OptAge   int              `json:"opt_age"   validate:"omitempty,gte=1"`
	OptTags  []string         `json:"opt_tags"  validate:"omitempty,min=2"`
	OptAddr  Address          `json:"opt_addr"  validate:"omitempty"`
	Span     Span             `json:"span"      validate:"omitempty"`
	Picks    *[]string        `json:"picks"     validate:"dive,required"`
	ByPoint  map[Point]Item   `json:"by_point"`
	Tree     Tree             `json:"tree"`
	DoneAt   *time.Time       `json:"done_at"   validate:"rule=schedule.Since2000"`
	Due      *time.Time       `json:"due"       validate:"rule=schedule.Deadline"`
	Shift    string           `json:"shift"     validate:"omitempty,rule=schedule.Shift"`
	Shifts   []time.Time      `json:"shifts"    validate:"dive,rule=Since2000"`
	Audit
}

func (x Request) validate(errs *validate.Errors) {
	if x.Role == "admin" && x.Age < 21 {
		errs.Add(validate.Base, validate.Fail("too_young_for_admin"))
	}
}

//kanna:validate
type Address struct {
	Zip string `json:"zip" validate:"required"`
}

//kanna:validate
type Audit struct {
	By string `json:"by" validate:"required"`
}
