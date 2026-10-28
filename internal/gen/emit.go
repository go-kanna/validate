package gen

import (
	"fmt"
	"go/constant"
	"go/token"
	"go/types"
	"strconv"
	"strings"

	"github.com/go-kanna/internal/gofile"
)

// generator names the tool in the header of the file it writes.
const generator = "kanna-validate"

// formatRulesPath is the package holding the rules email, url, and uuid run.
const formatRulesPath = "github.com/go-kanna/value/rule"

// The comparison that breaks each requirement, and the error each reports.
var (
	breaking = map[token.Token]token.Token{
		token.EQL: token.NEQ, token.NEQ: token.EQL,
		token.LSS: token.GEQ, token.LEQ: token.GTR,
		token.GTR: token.LEQ, token.GEQ: token.LSS,
	}
	compareErrors = map[token.Token]string{
		token.EQL: "EqualTo", token.NEQ: "OtherThan",
		token.LSS: "LessThan", token.LEQ: "LessThanOrEqualTo",
		token.GTR: "GreaterThan", token.GEQ: "GreaterThanOrEqualTo",
	}
	lengthErrors = map[token.Token]string{token.GEQ: "TooShort", token.LEQ: "TooLong", token.EQL: "WrongLength"}
)

// emit renders the Validate methods of plans into a file of the package pkgName
// at pkgPath. declared are the top-level names the package declares outside
// the file, which nothing the file introduces may take.
func emit(pkgPath, pkgName string, plans []plan, declared []string) *gofile.File {
	f := gofile.New(generator, pkgPath, pkgName)
	f.Reserve(declared...)

	// gofile wants every top-level name the file declares reserved before it
	// hands out any other name.
	patterns, order := namePatterns(plans, declared)
	for _, expr := range order {
		f.Reserve(patterns[expr])
	}

	e := &emitter{f: f, rt: f.Import(runtimePath, "validate"), patterns: patterns}
	if len(order) > 0 {
		re := f.Import("regexp", "regexp")
		fmt.Fprint(f, "var (\n")
		for _, expr := range order {
			fmt.Fprintf(f, "%s = %s.MustCompile(%s)\n", patterns[expr], re, rawString(expr))
		}
		fmt.Fprint(f, ")\n\n")
	}
	for _, p := range plans {
		e.method(p)
	}
	return f
}

// namePatterns names a package variable for each regular expression the plans
// match, after the first field to match it, such as patternRequestZip: one
// compiled expression serves every field writing the same. It returns the names
// by expression, and the expressions in the order they first appear.
func namePatterns(plans []plan, declared []string) (map[string]string, []string) {
	taken := make(map[string]bool, len(declared))
	for _, name := range declared {
		taken[name] = true
	}

	names := make(map[string]string)
	var order []string
	var walk func(v *value, base string)
	walk = func(v *value, base string) {
		if v == nil {
			return
		}
		for _, c := range v.checks {
			if c, ok := c.(patternCheck); ok && names[c.expr] == "" {
				name := base
				for n := 2; taken[name]; n++ {
					name = base + strconv.Itoa(n)
				}
				taken[name] = true
				names[c.expr] = name
				order = append(order, c.expr)
			}
		}
		walk(v.pointee, base)
		walk(v.elem, base)
	}
	for _, p := range plans {
		for _, fp := range p.fields {
			walk(fp.value, "pattern"+p.Name+fp.name)
		}
	}
	return names, order
}

// emitter writes the methods of one file.
type emitter struct {
	f *gofile.File

	// rt is the name the file imports the runtime under.
	rt string

	// patterns are the package variables holding each regular expression.
	patterns map[string]string
}

// method writes the Validate method of one struct.
type method struct {
	*emitter

	scope gofile.Scope

	// x is the receiver, and errs the Errors collected.
	x, errs string

	// loopVars are the loop variables by name and depth, which loops of one
	// depth share.
	loopVars map[string][]string

	// err is the variable errVar names, once a check has asked for it.
	err string
}

// place is where a value is rendered.
type place struct {
	// expr is the Go expression of the value, such as x.Name or (*x.Tags)[i].
	expr string

	// recv is the expression to call the value's methods on. For what a
	// pointer points to, it is the pointer, which Go dereferences itself.
	recv string

	// path is the Go expression of the path errors report the value under.
	path string

	// nestPath is the path the value's own Validate nests its errors under,
	// which is "" for an embedded struct that encoding/json flattens.
	nestPath string
}

func (e *emitter) method(p plan) {
	s := e.f.Scope()
	m := &method{emitter: e, scope: s, x: s.Name("x"), errs: s.Name("errs"), loopVars: make(map[string][]string)}

	fmt.Fprintf(e.f, "// Validate checks %s against the validate tags of %s. It returns the\n", m.x, p.Name)
	fmt.Fprintf(e.f, "// %s.Errors of every rule %s breaks, or nil.\n", e.rt, m.x)
	fmt.Fprintf(e.f, "func (%s %s) Validate() error {\n", m.x, p.Name)
	fmt.Fprintf(e.f, "var %s %s.Errors\n", m.errs, e.rt)

	for _, fp := range p.fields {
		expr := m.x + "." + fp.name
		path := strconv.Quote(fp.path)
		nestPath := path
		if fp.flatten {
			nestPath = `""`
		}
		fmt.Fprint(e.f, "\n")
		m.value(fp.value, place{expr: expr, recv: expr, path: path, nestPath: nestPath}, 0)
	}

	if p.hook {
		fmt.Fprintf(e.f, "\n%s.validate(&%s)\n", m.x, m.errs)
	}
	fmt.Fprintf(e.f, "\nreturn %s.Err()\n}\n\n", m.errs)
}

// value writes the checks of one value. depth counts the loops around it.
//
// A value that breaks required is reported blank and nothing else: whatever
// else an empty value breaks only says again that it is missing.
func (m *method) value(v *value, at place, depth int) {
	if v.pointee != nil {
		m.pointer(v, at, depth)
		return
	}

	rest := len(v.checks) > 0 || v.nest || v.elem != nil
	switch {
	case v.required && !rest:
		m.printf("if %s {\n%s\n}\n", m.zero(v, at), m.add(at.path, "Blank()"))
		return
	case v.required:
		m.printf("if %s {\n%s\n} else {\n", m.zero(v, at), m.add(at.path, "Blank()"))
	case v.omitEmpty:
		m.printf("if %s {\n", m.nonZero(v, at))
	}

	for _, c := range v.checks {
		m.check(c, v.typ, at)
	}

	if v.nest {
		call := fmt.Sprintf("%s.Nest(%s, %s.Validate())\n", m.errs, at.nestPath, at.recv)
		if types.IsInterface(v.typ) {
			call = fmt.Sprintf("if %s != nil {\n%s}\n", at.expr, call)
		}
		m.printf("%s", call)
	}

	if v.elem != nil {
		m.elem(v, at, depth)
	}

	if v.required || v.omitEmpty {
		m.printf("}\n")
	}
}

// pointer writes the checks of a pointer: required tests it for nil, and the
// rest checks what it points to, when there is something.
func (m *method) pointer(v *value, at place, depth int) {
	if v.required {
		m.printf("if %s == nil {\n%s\n} else {\n", at.expr, m.add(at.path, "Blank()"))
	} else {
		m.printf("if %s != nil {\n", at.expr)
	}
	pointee := at
	pointee.expr = "*" + paren(at.expr)
	pointee.recv = paren(at.expr)
	m.value(v.pointee, pointee, depth)
	m.printf("}\n")
}

// elem loops over the elements of a slice or array, or the values of a map,
// and writes v.elem for each.
//
// A map whose keys have an order is walked in it, so that its errors come out
// in the same order on every run.
func (m *method) elem(v *value, at place, depth int) {
	elem := at
	if mt, ok := v.typ.Underlying().(*types.Map); ok {
		k, val := m.loopVar("k", depth), m.loopVar("v", depth)
		if isOrdered(mt.Key()) {
			m.printf("for _, %s := range %s.Sorted(%s.Keys(%s)) {\n%s := %s[%s]\n",
				k, m.f.Import("slices", "slices"), m.f.Import("maps", "maps"), at.expr, val, paren(at.expr), k)
		} else {
			m.printf("for %s, %s := range %s {\n", k, val, at.expr)
		}
		elem.expr, elem.recv = val, val
		elem.path = fmt.Sprintf("%s.Key(%s, %s)", m.rt, at.path, k)
	} else {
		i := m.loopVar("i", depth)
		m.printf("for %s := range %s {\n", i, at.expr)
		elem.expr = paren(at.expr) + "[" + i + "]"
		elem.recv = elem.expr
		elem.path = fmt.Sprintf("%s.Index(%s, %s)", m.rt, at.path, i)
	}
	elem.nestPath = elem.path
	m.value(v.elem, elem, depth+1)
	m.printf("}\n")
}

func (m *method) check(c check, t types.Type, at place) {
	switch c := c.(type) {
	case lengthCheck:
		n := "len(" + at.expr + ")"
		if c.runes {
			n = m.f.Import("unicode/utf8", "utf8") + ".RuneCountInString(" + asString(at.expr, t) + ")"
		}
		m.printf("if %s %s %d {\n%s\n}\n", n, breaking[c.op], c.bound,
			m.add(at.path, fmt.Sprintf("%s(%d)", lengthErrors[c.op], c.bound)))
	case compareCheck:
		m.printf("if %s %s %s {\n%s\n}\n", at.expr, breaking[c.op], c.bound,
			m.add(at.path, compareErrors[c.op]+m.typeArg(c.bound, t)+"("+c.bound+")"))
	case oneOfCheck:
		conds := make([]string, len(c.values))
		for i, v := range c.values {
			conds[i] = at.expr + " != " + v
		}
		m.printf("if %s {\n%s\n}\n", strings.Join(conds, " && "), m.add(at.path, "Inclusion()"))
	case confirmCheck:
		m.printf("if %s != %s.%s {\n%s\n}\n", at.expr, m.x, c.field,
			m.add(at.path, "Confirmation("+strconv.Quote(c.attribute)+")"))
	case patternCheck:
		m.printf("if !%s.MatchString(%s) {\n%s\n}\n", m.patterns[c.expr], asString(at.expr, t), m.add(at.path, "Invalid()"))
	case formatCheck:
		m.printf("%s.Add(%s, %s.%s(%s))\n",
			m.errs, at.path, m.f.Import(formatRulesPath, "rule"), c.rule, asString(at.expr, t))
	case ruleCheck:
		if c.ctor == nil {
			m.printf("%s.Add(%s, %s.Validate(%s))\n", m.errs, at.path, m.zeroValue(c.typ), at.expr)
			return
		}
		ctor := c.ctor.Name()
		if q := m.f.Qualifier(c.ctor.Pkg()); q != "" {
			ctor = q + "." + ctor
		}
		err := m.errVar()
		m.printf("if _, %s := %s(%s); %s != nil {\n", err, ctor, at.expr, err)
		m.printf("%s.Add(%s, %s)\n", m.errs, at.path, err)
		m.printf("}\n")
	}
}

// add is the statement recording err, a call into the runtime, at path.
func (m *method) add(path, err string) string {
	return fmt.Sprintf("%s.Add(%s, %s.%s)", m.errs, path, m.rt, err)
}

// typeArg is the type argument a numeric error needs so that the count it
// reports has t, the type of the value checked: none when the literal bound
// already has that type by default, as 18 has int.
func (m *method) typeArg(bound string, t types.Type) string {
	def := types.Typ[types.Float64]
	if constant.MakeFromLiteral(strings.TrimPrefix(bound, "-"), token.INT, 0).Kind() != constant.Unknown {
		def = types.Typ[types.Int]
	}
	if types.Identical(types.Unalias(t), def) {
		return ""
	}
	return "[" + types.TypeString(t, m.f.Qualifier) + "]"
}

// zero is the condition that holds for the zero value.
func (m *method) zero(v *value, at place) string {
	switch v.zero {
	case zeroString:
		return at.expr + ` == ""`
	case zeroNumber:
		return at.expr + " == 0"
	case zeroBool:
		return "!" + at.expr
	case zeroNil:
		return at.expr + " == nil"
	case zeroMethod:
		return at.recv + ".IsZero()"
	case zeroComposite:
		return at.expr + " == (" + types.TypeString(v.typ, m.f.Qualifier) + "{})"
	}
	panic(fmt.Sprintf("emit: unknown zero test %d", v.zero))
}

// nonZero is the condition that holds for any value but the zero one.
func (m *method) nonZero(v *value, at place) string {
	switch v.zero {
	case zeroString:
		return at.expr + ` != ""`
	case zeroNumber:
		return at.expr + " != 0"
	case zeroBool:
		return at.expr
	case zeroNil:
		return at.expr + " != nil"
	case zeroMethod:
		return "!" + at.recv + ".IsZero()"
	case zeroComposite:
		return at.expr + " != (" + types.TypeString(v.typ, m.f.Qualifier) + "{})"
	}
	panic(fmt.Sprintf("emit: unknown zero test %d", v.zero))
}

// zeroValue is the zero value of t, a type with methods, as an expression to
// call them on.
func (m *method) zeroValue(t types.Type) string {
	name := types.TypeString(t, m.f.Qualifier)
	switch u := t.Underlying().(type) {
	case *types.Struct, *types.Array:
		return name + "{}"
	case *types.Basic:
		switch info := u.Info(); {
		case info&types.IsString != 0:
			return name + `("")`
		case info&types.IsBoolean != 0:
			return name + "(false)"
		case info&types.IsNumeric != 0:
			return name + "(0)"
		}
	}
	return name + "(nil)"
}

// errVar names the variable holding what a constructor returned, which every
// check of a method shares, as each declares it in an if of its own.
func (m *method) errVar() string {
	if m.err == "" {
		m.err = m.scope.Name("err")
	}
	return m.err
}

// loopVar names the loop variable base at depth. Every loop of one depth in a
// method shares the name, so that only nested loops number theirs: i, i2, i3.
func (m *method) loopVar(base string, depth int) string {
	names := m.loopVars[base]
	for len(names) <= depth {
		names = append(names, m.scope.Name(base))
	}
	m.loopVars[base] = names
	return names[depth]
}

func (m *method) printf(format string, args ...any) {
	fmt.Fprintf(m.f, format, args...)
}

// asString converts expr, of type t, to a string where a function needs one;
// a value of a type defined on string has to be converted.
func asString(expr string, t types.Type) string {
	if types.Identical(types.Unalias(t), types.Typ[types.String]) {
		return expr
	}
	return "string(" + expr + ")"
}

// isOrdered reports whether values of t can be sorted with <.
func isOrdered(t types.Type) bool {
	b, ok := t.Underlying().(*types.Basic)
	return ok && b.Info()&types.IsOrdered != 0
}

// paren wraps a dereference, so that indexing or calling a method applies to
// what it yields.
func paren(expr string) string {
	if strings.HasPrefix(expr, "*") {
		return "(" + expr + ")"
	}
	return expr
}

// rawString quotes s as a raw string where it can, which keeps a regular
// expression as it was written in the tag.
func rawString(s string) string {
	if strconv.CanBackquote(s) {
		return "`" + s + "`"
	}
	return strconv.Quote(s)
}
