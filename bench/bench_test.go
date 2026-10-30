package bench_test

import (
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/go-kanna/validate"
	"github.com/go-playground/validator/v10"

	"github.com/go-kanna/validate/bench"
)

var requests = []struct {
	name string
	req  bench.CreateUserRequest

	// fields is what both report broken, in order.
	fields []string
}{
	{
		name: "valid",
		req: bench.CreateUserRequest{
			Name:     "Kanna",
			Age:      30,
			Role:     "member",
			Tags:     []string{"go", "rails"},
			Address:  bench.Address{Zip: "100-0001", City: "Tokyo"},
			Password: "correct horse battery",
			Confirm:  "correct horse battery",
		},
	},
	{
		// One rule broken per field: go-playground stops at the first rule a
		// field breaks, while kanna-validate reports every one.
		name: "invalid",
		req: bench.CreateUserRequest{
			Name:     "",
			Age:      17,
			Role:     "owner",
			Tags:     []string{"go", "a tag far longer than twenty"},
			Address:  bench.Address{Zip: "1000001", City: "Tokyo"},
			Password: "short",
			Confirm:  "shorter",
		},
		fields: []string{"name", "age", "role", "tags[1]", "address.zip", "password", "confirm"},
	},
}

// newPlayground returns a go-playground/validator set up to match
// kanna-validate: a required struct must not be its zero value, and a field is
// named by its JSON name.
func newPlayground() *validator.Validate {
	v := validator.New(validator.WithRequiredStructEnabled())
	v.RegisterTagNameFunc(func(f reflect.StructField) string {
		name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		return name
	})
	return v
}

func BenchmarkValidate(b *testing.B) {
	v := newPlayground()
	for _, r := range requests {
		b.Run(r.name+"/kanna-validate", func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				_ = r.req.Validate()
			}
		})
		b.Run(r.name+"/go-playground", func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				_ = v.Struct(&r.req)
			}
		})
	}
}

// TestSameFields keeps the benchmark fair: both have to find the same fields
// broken, or one of them is doing less work.
func TestSameFields(t *testing.T) {
	t.Parallel()

	v := newPlayground()
	for _, r := range requests {
		t.Run(r.name, func(t *testing.T) {
			t.Parallel()

			if got := kannaFields(t, r.req.Validate()); !slices.Equal(got, r.fields) {
				t.Errorf("kanna-validate reports %v, want %v", got, r.fields)
			}
			if got := playgroundFields(t, v.Struct(&r.req)); !slices.Equal(got, r.fields) {
				t.Errorf("go-playground reports %v, want %v", got, r.fields)
			}
		})
	}
}

func kannaFields(t *testing.T, err error) []string {
	if err == nil {
		return nil
	}
	errs, ok := errors.AsType[validate.Errors](err)
	if !ok {
		t.Fatalf("kanna-validate: %v", err)
	}
	fields := make([]string, 0, len(errs))
	for _, fe := range errs {
		fields = append(fields, fe.Field)
	}
	return fields
}

func playgroundFields(t *testing.T, err error) []string {
	if err == nil {
		return nil
	}
	errs, ok := errors.AsType[validator.ValidationErrors](err)
	if !ok {
		t.Fatalf("go-playground: %v", err)
	}
	fields := make([]string, 0, len(errs))
	for _, fe := range errs {
		// The namespace starts with the struct's name: "CreateUserRequest.address.zip".
		_, field, _ := strings.Cut(fe.Namespace(), ".")
		fields = append(fields, field)
	}
	return fields
}
