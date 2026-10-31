package gen_test

import (
	"encoding/json"
	"errors"
	"math"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/go-kanna/validate"
	"github.com/go-kanna/validate/internal/gen/testdata/emit"
)

// validRequest breaks no rule of emit.Request.
func validRequest() emit.Request {
	return emit.Request{
		Name:     "kanna",
		Age:      30,
		Level:    1,
		Score:    1,
		Role:     "member",
		Tags:     []string{"go"},
		Matrix:   [][]int{{0, 1}},
		Labels:   map[string]emit.Role{"a": "admin"},
		Code:     "ABC",
		Password: "secret",
		Confirm:  "secret",
		Active:   true,
		When:     time.Date(2026, 10, 27, 0, 0, 0, 0, time.UTC),
		Point:    emit.Point{X: 1},
		Grid:     [2]emit.Point{{X: 1}, {Y: 1}},
		Address:  emit.Address{Zip: "100-0001"},
		Billing:  &emit.Address{Zip: "100-0001"},
		Items:    []emit.Item{{SKU: "A1"}},
		ByName:   map[string]*emit.Item{"a": {SKU: "A1"}, "nil": nil},
		Count:    1,
		List:     []string{},
		By:       "admin",
	}
}

// checker is an emit.Checker that reports err.
type checker struct{ err error }

func (c checker) Validate() error { return c.err }

// TestGenerated calls the methods in testdata/emit/validate_gen.go and checks
// what they report, encoded as an API would send it.
func TestGenerated(t *testing.T) {
	t.Parallel()

	nickname := "k"

	tests := []struct {
		name   string
		modify func(*emit.Request)
		want   string
	}{
		{name: "valid", modify: func(*emit.Request) {}, want: ""},
		{
			name:   "zero value",
			modify: func(r *emit.Request) { *r = emit.Request{} },
			want: `{"name":[{"error":"blank"}],` +
				`"age":[{"error":"greater_than_or_equal_to","count":18}],` +
				`"level":[{"error":"inclusion"}],` +
				`"score":[{"error":"greater_than","count":0}],` +
				`"role":[{"error":"blank"}],` +
				`"code":[{"error":"invalid"}],` +
				`"password":[{"error":"blank"}],` +
				`"active":[{"error":"blank"}],` +
				`"when":[{"error":"blank"}],` +
				`"point":[{"error":"blank"}],` +
				`"grid[0]":[{"error":"blank"}],"grid[1]":[{"error":"blank"}],` +
				`"address.zip":[{"error":"blank"}],` +
				`"billing":[{"error":"blank"}],` +
				`"items":[{"error":"too_short","count":1}],` +
				`"count":[{"error":"blank"}],` +
				`"list":[{"error":"blank"}],` +
				`"by":[{"error":"blank"}]}`,
		},
		{
			name: "lengths and bounds",
			modify: func(r *emit.Request) {
				r.Name = strings.Repeat("か", 51)
				r.Age = 131
				r.Quantity = 1001
				r.Nickname = &nickname
				r.Tags = []string{"", strings.Repeat("x", 21), "c", "d", "e", "f"}
			},
			want: `{"name":[{"error":"too_long","count":50}],` +
				`"age":[{"error":"less_than_or_equal_to","count":130}],` +
				`"quantity":[{"error":"less_than_or_equal_to","count":1000}],` +
				`"nickname":[{"error":"too_short","count":2}],` +
				`"tags":[{"error":"too_long","count":5}],` +
				`"tags[0]":[{"error":"blank"}],"tags[1]":[{"error":"too_long","count":20}]}`,
		},
		{
			name: "elements",
			modify: func(r *emit.Request) {
				r.Matrix = [][]int{{1, -1}, {-2}}
				r.Labels = map[string]emit.Role{"b": emit.Role(strings.Repeat("x", 11)), "a": emit.Role(strings.Repeat("y", 11))}
				r.Items = []emit.Item{{SKU: "A1"}, {}}
				r.ByName = map[string]*emit.Item{"z": {}, "nil": nil}
			},
			want: `{"matrix[0][1]":[{"error":"greater_than_or_equal_to","count":0}],` +
				`"matrix[1][0]":[{"error":"greater_than_or_equal_to","count":0}],` +
				`"labels.a":[{"error":"too_long","count":10}],"labels.b":[{"error":"too_long","count":10}],` +
				`"items[1].sku":[{"error":"blank"}],` +
				`"by_name.z.sku":[{"error":"blank"}]}`,
		},
		{
			name: "formats and fields",
			modify: func(r *emit.Request) {
				r.Zip = "1000001"
				r.PostCode = "100-00001"
				r.Code = "abc"
				r.Confirm = "secrets"
				r.Billing = &emit.Address{}
				r.Extra = checker{err: validate.Fail("unreachable")}
			},
			want: `{"zip":[{"error":"invalid"}],"post_code":[{"error":"invalid"}],"code":[{"error":"invalid"}],` +
				`"confirm":[{"error":"confirmation","attribute":"password"}],` +
				`"billing.zip":[{"error":"blank"}],` +
				`"extra":[{"error":"unreachable"}]}`,
		},
		{
			name: "values past omitempty",
			modify: func(r *emit.Request) {
				picks := []string{"a", ""}
				r.OptAge = -1
				r.OptTags = []string{"one"}
				r.OptAddr = emit.Address{Zip: "x"}
				r.Span = emit.Span{From: 2, To: 1}
				r.Picks = &picks
				r.ByPoint = map[emit.Point]emit.Item{{X: 1}: {}}
				r.Tree = emit.Tree{"a": {"b": nil}}
			},
			want: `{"opt_age":[{"error":"greater_than_or_equal_to","count":1}],` +
				`"opt_tags":[{"error":"too_short","count":2}],` +
				`"span":[{"error":"inverted"}],` +
				`"picks[1]":[{"error":"blank"}],` +
				`"by_point.{1 0}.sku":[{"error":"blank"}]}`,
		},
		{
			// The rules' own codes and params come through, from a rule type and
			// from the constructor of a value type or an enum alike.
			name: "rules",
			modify: func(r *emit.Request) {
				before := time.Date(1999, 12, 31, 0, 0, 0, 0, time.UTC)
				r.DoneAt = &before
				r.Due = &before
				r.Shift = "evening"
				r.Shifts = []time.Time{time.Date(2026, 10, 28, 0, 0, 0, 0, time.UTC), before}
			},
			want: `{"done_at":[{"error":"too_early","year":2000}],"due":[{"error":"too_early","year":2000}],` +
				`"shift":[{"error":"inclusion"}],"shifts[1]":[{"error":"too_early","year":2000}]}`,
		},
		{
			name:   "hook",
			modify: func(r *emit.Request) { r.Role, r.Age = "admin", 18 },
			want:   `{"base":[{"error":"too_young_for_admin"}]}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := validRequest()
			tt.modify(&r)
			err := r.Validate()

			if tt.want == "" {
				if err != nil {
					t.Fatalf("Validate() = %v, want nil", err)
				}
				return
			}
			errs, ok := errors.AsType[validate.Errors](err)
			if !ok {
				t.Fatalf("Validate() = %v, want validate.Errors", err)
			}
			got, err := json.Marshal(errs)
			if err != nil {
				t.Fatalf("Marshal() error = %v", err)
			}
			if string(got) != tt.want {
				t.Errorf("Validate() reported\n got  %s\n want %s", got, tt.want)
			}
		})
	}
}

// What a type has through an embedded pointer or interface is called only when
// that is there, as a nil pointer or interface in a field is skipped.
func TestGenerated_PromotedThroughNil(t *testing.T) {
	t.Parallel()

	var (
		valid   = emit.ByPointer{Item: &emit.Item{SKU: "A1"}}
		invalid = emit.ByPointer{Item: &emit.Item{}}
		failing = emit.ByInterface{Checker: checker{err: validate.Fail("no")}}
	)
	tests := []struct {
		name string
		in   emit.Promoted
		want []string // the fields reported
	}{
		{name: "nothing there"},
		{name: "a nil pointer behind a pointer", in: emit.Promoted{Deeper: emit.Deeper{
			Through: emit.Through{ByPointer: &emit.ByPointer{}},
		}}},
		{name: "a nil pointer in a nested value", in: emit.Promoted{
			Ref: &emit.ByPointer{}, List: []emit.ByPointer{{}}, Map: map[string]emit.ByPointer{"a": {}},
		}},
		{name: "what is there and valid", in: emit.Promoted{
			Pointer: valid, Ref: &valid, List: []emit.ByPointer{valid}, Map: map[string]emit.ByPointer{"a": valid},
			Interface: emit.ByInterface{Checker: checker{}},
			Deeper:    emit.Deeper{Through: emit.Through{ByPointer: &valid}},
		}},
		{
			name: "what is there and invalid",
			in: emit.Promoted{
				Pointer: invalid, Ref: &invalid, List: []emit.ByPointer{invalid}, Map: map[string]emit.ByPointer{"a": invalid},
				Interface: failing,
				Deeper:    emit.Deeper{Through: emit.Through{ByPointer: &invalid}},
			},
			want: []string{"pointer.sku", "interface", "deeper.sku", "ref.sku", "list[0].sku", "map.a.sku"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := tt.in.Validate()
			if tt.want == nil {
				if err != nil {
					t.Fatalf("Validate() = %v, want nil", err)
				}
				return
			}
			errs, ok := errors.AsType[validate.Errors](err)
			if !ok {
				t.Fatalf("Validate() = %v, want validate.Errors", err)
			}
			var got []string
			for _, e := range errs {
				got = append(got, e.Field)
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("Validate() reported %q, want %q", got, tt.want)
			}
		})
	}
}

func TestGenerated_CountKeepsTheFieldsType(t *testing.T) {
	t.Parallel()

	r := validRequest()
	r.Quantity, r.Score = 1001, -1

	errs, ok := errors.AsType[validate.Errors](r.Validate())
	if !ok || len(errs) != 2 {
		t.Fatalf("Validate() = %v, want the errors of quantity and score", errs)
	}
	if count, ok := errs[0].Params()["count"].(uint16); !ok || count != 1000 {
		t.Errorf("count of quantity = %#v, want uint16(1000)", errs[0].Params()["count"])
	}
	if count, ok := errs[1].Params()["count"].(float64); !ok || count != 0 {
		t.Errorf("count of score = %#v, want float64(0)", errs[1].Params()["count"])
	}
}

// NaN compares false with everything, so it meets no bound: each comparison
// rule reports it, as an infinity beyond the bound.
func TestGenerated_FloatBounds(t *testing.T) {
	t.Parallel()

	nan32 := float32(math.NaN())
	tests := []struct {
		name  string
		score float64
		ratio float32
		want  string
	}{
		{name: "NaN", score: math.NaN(), ratio: nan32,
			want: `{"score":[{"error":"greater_than","count":0}],` +
				`"ratio":[{"error":"greater_than_or_equal_to","count":0},{"error":"less_than_or_equal_to","count":1}]}`},
		{name: "infinities", score: math.Inf(-1), ratio: float32(math.Inf(1)),
			want: `{"score":[{"error":"greater_than","count":0}],"ratio":[{"error":"less_than_or_equal_to","count":1}]}`},
		{name: "the bounds", score: math.SmallestNonzeroFloat64, ratio: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := validRequest()
			r.Score, r.Ratio = tt.score, tt.ratio
			err := r.Validate()
			if tt.want == "" {
				if err != nil {
					t.Fatalf("Validate() = %v, want nil", err)
				}
				return
			}
			got, merr := json.Marshal(err)
			if merr != nil {
				t.Fatalf("Marshal() error = %v", merr)
			}
			if string(got) != tt.want {
				t.Errorf("Validate() reported\n got  %s\n want %s", got, tt.want)
			}
		})
	}
}

// FuzzGenerated_FloatBound checks gt=0 on every bit pattern of a float64: it
// is broken exactly when x > 0 does not hold.
func FuzzGenerated_FloatBound(f *testing.F) {
	seeds := []float64{
		0, math.Copysign(0, -1), 1, -1, math.NaN(), math.Inf(1), math.Inf(-1), math.SmallestNonzeroFloat64,
	}
	for _, x := range seeds {
		f.Add(math.Float64bits(x))
	}
	f.Fuzz(func(t *testing.T, bits uint64) {
		x := math.Float64frombits(bits)
		r := validRequest()
		r.Score = x

		errs, _ := errors.AsType[validate.Errors](r.Validate())
		broken := slices.ContainsFunc(errs, func(e validate.FieldError) bool { return e.Field == "score" })
		if broken != !(x > 0) {
			t.Errorf("score %v (%#x) reported %t, want %t", x, bits, broken, !(x > 0))
		}
	})
}
