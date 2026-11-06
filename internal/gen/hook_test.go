package gen_test

import (
	"testing"
)

// hookTypes declares the types the structs of the hook tests hold.
const hookTypes = `package app

import "github.com/go-kanna/validate"

type Address struct {
	Zip string ` + "`json:\"zip\"`" + `
}

type Item struct {
	SKU string ` + "`json:\"sku\"`" + `
}

// Manual validates itself by hand, under names of its own.
type Manual struct{}

func (Manual) Validate() error {
	var errs validate.Errors
	errs.Add("virtual", validate.Blank())
	return errs.Err()
}

// Wrapper is validated by the Validate of Manual, which it embeds.
type Wrapper struct {
	Manual
}
`

func TestCommand_HookPathsThatCanBe(t *testing.T) {
	t.Parallel()

	dir := writeModule(t, map[string]string{
		"types.go": hookTypes,
		"order.go": `package app

import "github.com/go-kanna/validate"

//kanna:validate
type Order struct {
	Name    string          ` + "`json:\"name\"`" + `
	Address Address         ` + "`json:\"address\"`" + `
	Items   []Item          ` + "`json:\"items\"`" + `
	Grid    [][]string      ` + "`json:\"grid\"`" + `
	Labels  map[string]Item ` + "`json:\"labels\"`" + `
	Dotted  string          ` + "`json:\"a.b\"`" + `
	Secret  string          ` + "`json:\"-\"`" + `
	Payload any             ` + "`json:\"payload\"`" + `
	Checked Manual          ` + "`json:\"checked\"`" + `
	Wrapped Wrapper         ` + "`json:\"wrapped\"`" + `
	note    string
}

const named = "name"

func (o Order) validate(errs *validate.Errors) {
	errs.Add(named, nil)
	errs.Add(validate.Base, nil)
	errs.Add("address.zip", nil)
	errs.Add("address.base", nil)
	errs.Add("items[0].sku", nil)
	errs.Add("grid[0][1]", nil)
	errs.Add("labels.a.b.sku", nil)
	errs.Add("a.b", nil)
	errs.Add("Secret", nil)
	errs.Add("note", nil)
	errs.Add("payload.anything", nil)
	errs.Add("checked.anything", nil)
	errs.Add("wrapped.virtual", nil)
	errs.Add(validate.Index("items", 1), nil)
	errs.Add(validate.Index(validate.Index("grid", 0), 1), nil)
	errs.Add(validate.Key("labels", "x"), nil)
	errs.Nest("", nil)
	errs.Nest("address", nil)
	func() {
		errs.Add("address.zip", nil)
		errs := &validate.Errors{}
		errs.Add("nothing", nil)
	}()
	var other validate.Errors
	other.Add("nothing", nil)
	other.Add(validate.Index("nothing", 0), nil)
	_ = o.note
}

// Flat reports, through the Validate of Manual, names no field of it tells.
//
//kanna:validate
type Flat struct {
	Manual
}

func (f Flat) validate(errs *validate.Errors) {
	errs.Add("virtual", nil)
}
`,
	})

	if code, _, stderr := run(t, dir); code != 0 || stderr != "" {
		t.Errorf("exit %d, stderr:\n%s", code, stderr)
	}
}

func TestCommand_HookPathsThatCannotBe(t *testing.T) {
	t.Parallel()

	dir := writeModule(t, map[string]string{
		"types.go": hookTypes,
		"order.go": `package app

import "github.com/go-kanna/validate"

//kanna:validate
type Order struct {
	Name    string          ` + "`json:\"name\"`" + `
	Age     int             ` + "`json:\"age\"`" + `
	Address Address         ` + "`json:\"address\"`" + `
	Items   []Item          ` + "`json:\"items\"`" + `
	Fixed   [2]Item         ` + "`json:\"fixed\"`" + `
	Labels  map[string]Item ` + "`json:\"labels\"`" + `
	Wrapped Wrapper         ` + "`json:\"wrapped\"`" + `
}

const typo = "nme"

func (o Order) validate(errs *validate.Errors) {
	errs.Add("aeg", nil)
	errs.Add(typo, nil)
	errs.Add("zzzzzz", nil)
	errs.Add("address.zpi", nil)
	errs.Add("age.years", nil)
	errs.Add("items.sku", nil)
	errs.Add("fixed.sku", nil)
	errs.Add("items[0].skuu", nil)
	errs.Add("labels[0]", nil)
	errs.Add("address[0]", nil)
	errs.Add("base.zip", nil)
	errs.Add("", nil)
	errs.Add(validate.Index("name", 0), nil)
	errs.Add(validate.Key("items", "x"), nil)
	errs.Nest(validate.Index("", 0), nil)
	errs.Add("virtual", nil)
}
`,
	})

	code, _, stderr := run(t, dir)
	if code != 1 {
		t.Errorf("exit %d, want 1", code)
	}
	want := `./order.go:19:11: error: "aeg" is not a path of Order: Order has no field "aeg"
  did you mean "age"?
./order.go:20:11: error: "nme" is not a path of Order: Order has no field "nme"
  did you mean "name"?
./order.go:21:11: error: "zzzzzz" is not a path of Order: Order has no field "zzzzzz"
  an error on Order as a whole goes under validate.Base
./order.go:22:11: error: "address.zpi" is not a path of Order: address has no field "zpi"
  did you mean "address.zip"?
./order.go:23:11: error: "age.years" is not a path of Order: age is int, with nothing under it
./order.go:24:11: error: "items.sku" is not a path of Order: items is a slice, whose elements go as items[0]
./order.go:25:11: error: "fixed.sku" is not a path of Order: fixed is an array, whose elements go as fixed[0]
./order.go:26:11: error: "items[0].skuu" is not a path of Order: items[0] has no field "skuu"
  did you mean "items[0].sku"?
./order.go:27:11: error: "labels[0]" is not a path of Order: labels is a map, whose keys follow a dot
./order.go:28:11: error: "address[0]" is not a path of Order: address is a struct, whose fields follow a dot
./order.go:29:11: error: "base.zip" is not a path of Order: base is the value as a whole, with nothing under it
./order.go:30:11: error: "" is not a path of Order: Add needs a field
  an error on Order as a whole goes under validate.Base
./order.go:31:11: error: validate.Index("name", 0) is not a path of Order: ` +
		`validate.Index needs a slice or an array, not string
./order.go:32:11: error: validate.Key("items", "x") is not a path of Order: validate.Key needs a map, not []Item
./order.go:33:12: error: validate.Index("", 0) is not a path of Order: ` +
		`validate.Index needs a slice or an array, not Order
./order.go:34:11: error: "virtual" is not a path of Order: Order has no field "virtual"
  an error on Order as a whole goes under validate.Base
`
	if stderr != want {
		t.Errorf("stderr:\n%s\nwant:\n%s", stderr, want)
	}
}
