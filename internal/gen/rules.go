package gen

import (
	"fmt"
	"go/constant"
	"go/token"
	"go/types"
	"maps"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/go-kanna/internal/suggest"
)

// check is a rule resolved against the type of the value it checks. Its type
// says what the generated code tests, and so which error it reports.
type check interface {
	isCheck()
}

// lengthCheck requires the length of a string, slice, or map to compare with
// bound as op says: token.GEQ for min, token.LEQ for max, and token.EQL for
// len.
type lengthCheck struct {
	op    token.Token
	bound int

	// runes counts the characters of a string rather than its bytes.
	runes bool
}

// compareCheck requires a number to compare with bound, a Go literal, as op
// says, such as token.GEQ for gte.
type compareCheck struct {
	op    token.Token
	bound string
}

// oneOfCheck accepts only values, Go literals of the type checked.
type oneOfCheck struct {
	values []string
}

// confirmCheck requires the value to equal the struct's field, whose JSON name
// is attribute.
type confirmCheck struct {
	field, attribute string
}

// patternCheck requires a string to match expr.
type patternCheck struct {
	expr string
}

// formatCheck runs rule, a type of github.com/go-kanna/validated/rules, on a
// string.
type formatCheck struct {
	rule string
}

func (lengthCheck) isCheck()  {}
func (compareCheck) isCheck() {}
func (oneOfCheck) isCheck()   {}
func (confirmCheck) isCheck() {}
func (patternCheck) isCheck() {}
func (formatCheck) isCheck()  {}

// site is where a rule is written, and so what it can refer to besides the
// value it checks.
type site struct {
	// pkg declares the struct. Messages name its types as its source does,
	// without a qualifier.
	pkg *types.Package

	// fields are the struct's other fields, for eqfield. They are nil below
	// the field's own level, where eqfield has no field to compare with.
	fields []sibling
}

// sibling is another field of the struct, which eqfield can name.
type sibling struct {
	// name is the Go name, which is how eqfield spells it.
	name string

	// jsonName is what errors call it.
	jsonName string

	typ types.Type
}

// ruleError explains why a rule does not apply where it is written.
type ruleError struct {
	msg string

	// hint, if set, suggests what to write instead.
	hint string
}

func (e *ruleError) Error() string {
	return e.msg
}

func ruleErrorf(format string, args ...any) *ruleError {
	return &ruleError{msg: fmt.Sprintf(format, args...)}
}

// resolver resolves a rule against t, the type of the value it checks.
type resolver func(r rule, t types.Type, at site) (check, error)

// resolvers holds every rule but the three that shape a level: required,
// omitempty, and dive.
var resolvers = map[string]resolver{
	"min": size(token.GEQ),
	"max": size(token.LEQ),
	"len": size(token.EQL),

	"eq":  compare(token.EQL),
	"ne":  compare(token.NEQ),
	"lt":  compare(token.LSS),
	"lte": compare(token.LEQ),
	"gt":  compare(token.GTR),
	"gte": compare(token.GEQ),

	"oneof":   oneOf,
	"eqfield": eqField,
	"pattern": pattern,

	"email": format("Email"),
	"url":   format("URL"),
	"uuid":  format("UUID"),
}

// resolve resolves r against t, the type of the value it checks, or explains
// why r does not apply there. t is taken as it is: whether the rules on a
// pointer check what it points to is for the caller to decide.
func resolve(r rule, t types.Type, at site) (check, error) {
	if res, ok := resolvers[r.name]; ok {
		return res(r, t, at)
	}

	err := ruleErrorf("unknown rule %q", r.name)
	names := append(slices.Collect(maps.Keys(resolvers)), "required", "omitempty", "dive")
	slices.Sort(names)
	if s, ok := suggest.Closest(r.name, names); ok {
		err.hint = fmt.Sprintf("did you mean %q?", s)
	}
	return nil, err
}

// size resolves min, max, and len: the length of a string, slice, or map, or a
// number itself.
func size(op token.Token) resolver {
	return func(r rule, t types.Type, at site) (check, error) {
		if err := needsValue(r); err != nil {
			return nil, err
		}

		switch s := shapeOf(t); {
		case s == shapeString || s == shapeSlice || s == shapeMap:
			n, err := strconv.Atoi(r.param)
			if err != nil || n < 0 {
				return nil, ruleErrorf("rule %q expects a length, got %q", r.name, r.param)
			}
			return lengthCheck{op: op, bound: n, runes: s == shapeString}, nil
		case s == shapeArray:
			return nil, ruleErrorf("rule %q cannot apply to %s, whose length is fixed", r.name, at.typeString(t))
		case s == shapeNumber && r.name == "len":
			err := ruleErrorf("rule %q cannot apply to %s", r.name, at.typeString(t))
			err.hint = "len measures strings, slices, and maps; compare a number with eq"
			return nil, err
		case s == shapeNumber:
			bound, err := numberBound(r, r.param, t, at)
			if err != nil {
				return nil, err
			}
			return compareCheck{op: op, bound: bound}, nil
		}
		return nil, cannotApply(r, t, at)
	}
}

// compare resolves eq, ne, lt, lte, gt, and gte, which compare numbers.
func compare(op token.Token) resolver {
	return func(r rule, t types.Type, at site) (check, error) {
		if err := needsValue(r); err != nil {
			return nil, err
		}

		s := shapeOf(t)
		if s == shapeNumber {
			bound, err := numberBound(r, r.param, t, at)
			if err != nil {
				return nil, err
			}
			return compareCheck{op: op, bound: bound}, nil
		}

		err := cannotApply(r, t, at)
		if s == shapeString || s == shapeSlice || s == shapeMap {
			err.hint = r.name + " compares numbers; bound a length with min, max, or len"
		}
		return nil, err
	}
}

func oneOf(r rule, t types.Type, at site) (check, error) {
	values := strings.Fields(r.param)
	if len(values) == 0 {
		return nil, ruleErrorf("rule %q needs a value, as in %s=…", r.name, r.name)
	}

	s := shapeOf(t)
	if s != shapeString && !isInteger(t) {
		return nil, cannotApply(r, t, at)
	}

	var lits []string
	for _, v := range values {
		lit := strconv.Quote(v)
		if s == shapeNumber {
			var err error
			if lit, err = numberBound(r, v, t, at); err != nil {
				return nil, err
			}
		}
		if slices.Contains(lits, lit) {
			return nil, ruleErrorf("rule %q lists %s twice", r.name, v)
		}
		lits = append(lits, lit)
	}
	return oneOfCheck{values: lits}, nil
}

func eqField(r rule, t types.Type, at site) (check, error) {
	if err := needsValue(r); err != nil {
		return nil, err
	}
	if at.fields == nil {
		return nil, ruleErrorf("eqfield applies to the field itself, not to the elements after dive")
	}

	i := slices.IndexFunc(at.fields, func(f sibling) bool { return f.name == r.param })
	if i < 0 {
		err := ruleErrorf("eqfield=%s: no such field", r.param)
		names := make([]string, len(at.fields))
		for j, f := range at.fields {
			names[j] = f.name
		}
		if s, ok := suggest.Closest(r.param, names); ok {
			err.hint = fmt.Sprintf("did you mean %q?", s)
		}
		return nil, err
	}

	other := at.fields[i]
	if !types.Identical(t, other.typ) {
		return nil, ruleErrorf("eqfield=%s compares %s with %s, which differ",
			r.param, at.typeString(t), at.typeString(other.typ))
	}
	if !types.Comparable(t) {
		return nil, ruleErrorf("eqfield=%s cannot compare %s, which is not comparable", r.param, at.typeString(t))
	}
	return confirmCheck{field: other.name, attribute: other.jsonName}, nil
}

func pattern(r rule, t types.Type, at site) (check, error) {
	if err := needsValue(r); err != nil {
		return nil, err
	}
	if shapeOf(t) != shapeString {
		return nil, cannotApply(r, t, at)
	}
	if _, err := regexp.Compile(r.param); err != nil {
		return nil, ruleErrorf("invalid pattern: %v", err)
	}
	return patternCheck{expr: r.param}, nil
}

// format resolves email, url, and uuid, which run the rule of
// github.com/go-kanna/validated/rules named typeName.
func format(typeName string) resolver {
	return func(r rule, t types.Type, at site) (check, error) {
		if r.param != "" {
			return nil, ruleErrorf("rule %q takes no value, got %q", r.name, r.param)
		}
		if shapeOf(t) != shapeString {
			return nil, cannotApply(r, t, at)
		}
		return formatCheck{rule: typeName}, nil
	}
}

// numberBound checks that param, a number written in the tag, is one t can
// hold, and returns it as the Go literal to compare with. A literal that does
// not fit would only fail when the generated code is compiled, far from the tag
// that caused it.
func numberBound(r rule, param string, t types.Type, at site) (string, error) {
	digits := strings.TrimPrefix(param, "-")

	var v constant.Value
	for _, tok := range []token.Token{token.INT, token.FLOAT} {
		if v = constant.MakeFromLiteral(digits, tok, 0); v.Kind() != constant.Unknown {
			break
		}
	}
	if v.Kind() == constant.Unknown {
		return "", ruleErrorf("rule %q expects a number, got %q", r.name, param)
	}
	if digits != param {
		v = constant.UnaryOp(token.SUB, v, 0)
	}

	if b, ok := t.Underlying().(*types.Basic); !ok || !fits(v, b) {
		return "", ruleErrorf("rule %q: %s does not fit %s", r.name, param, at.typeString(t))
	}
	return param, nil
}

// fits reports whether the constant v can be held by a value of type b.
//
// int, uint, and uintptr are taken to be 64 bits wide, as on amd64, so on a
// 32-bit platform a bound beyond 32 bits still fails when the generated code is
// compiled.
func fits(v constant.Value, b *types.Basic) bool {
	if b.Info()&types.IsFloat != 0 {
		f, _ := constant.Float64Val(constant.ToFloat(v))
		if b.Kind() == types.Float32 {
			return math.Abs(f) <= math.MaxFloat32
		}
		return !math.IsInf(f, 0)
	}

	n := constant.ToInt(v)
	if n.Kind() != constant.Int {
		return false
	}
	bits := 8 * types.SizesFor("gc", "amd64").Sizeof(b)
	if b.Info()&types.IsUnsigned != 0 {
		u, exact := constant.Uint64Val(n)
		return exact && u <= math.MaxUint64>>(64-bits)
	}
	i, exact := constant.Int64Val(n)
	lo := int64(-1) << (bits - 1)
	return exact && lo <= i && i <= -(lo+1)
}

// shape is what a rule cares about in a type: what its underlying type is.
type shape int

const (
	shapeOther shape = iota
	shapeString
	shapeNumber
	shapeSlice
	shapeArray
	shapeMap
)

func shapeOf(t types.Type) shape {
	switch u := t.Underlying().(type) {
	case *types.Basic:
		switch info := u.Info(); {
		case info&types.IsString != 0:
			return shapeString
		case info&(types.IsInteger|types.IsFloat) != 0:
			return shapeNumber
		}
	case *types.Slice:
		return shapeSlice
	case *types.Array:
		return shapeArray
	case *types.Map:
		return shapeMap
	}
	return shapeOther
}

func isInteger(t types.Type) bool {
	b, ok := t.Underlying().(*types.Basic)
	return ok && b.Info()&types.IsInteger != 0
}

func needsValue(r rule) error {
	if r.param == "" {
		return ruleErrorf("rule %q needs a value, as in %s=…", r.name, r.name)
	}
	return nil
}

func cannotApply(r rule, t types.Type, at site) *ruleError {
	return ruleErrorf("rule %q cannot apply to %s", r.name, at.typeString(t))
}

// typeString spells t as the source declaring the struct would.
func (at site) typeString(t types.Type) string {
	return types.TypeString(t, func(p *types.Package) string {
		if p == at.pkg {
			return ""
		}
		return p.Name()
	})
}
