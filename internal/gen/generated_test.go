package gen_test

import (
	"encoding/json"
	"errors"
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
