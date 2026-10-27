package gen

import (
	"errors"
	"fmt"
	"go/token"
	"go/types"
	"reflect"
	"strconv"
	"strings"

	"github.com/go-kanna/internal/packages"
	"github.com/go-kanna/internal/scan"
)

// Targets finds the targets in pkgs, naming each, with " (hook)" after the name
// of one that has a hook, and spelling each diagnostic as diag.Diag.String
// does.
func Targets(fset *token.FileSet, pkgs []*packages.Package) ([]string, []string) {
	found, diags := targets(fset, scan.Structs(pkgs))

	names := make([]string, len(found))
	for i, t := range found {
		names[i] = t.Name
		if t.hook {
			names[i] += " (hook)"
		}
	}
	msgs := make([]string, len(diags))
	for i, d := range diags {
		msgs[i] = d.String()
	}
	return names, msgs
}

// Sibling is another field of the struct, for eqfield to name.
type Sibling = sibling

func NewSibling(name, jsonName string, t types.Type) Sibling {
	return sibling{name: name, jsonName: jsonName, typ: t}
}

// ResolveRule resolves written, one rule as a tag spells it, such as "max=5",
// against t, in a struct that pkg declares with fields besides, and spells out
// the check, as describe does.
func ResolveRule(written string, t types.Type, pkg *types.Package, fields []Sibling) (string, error) {
	name, param, _ := strings.Cut(written, "=")
	c, err := resolve(rule{name: name, param: param}, t, site{pkg: pkg, fields: fields})
	if err != nil {
		return "", err
	}
	return describe(c), nil
}

// Hint returns the hint of a rule's error, or "".
func Hint(err error) string {
	if e, ok := errors.AsType[*ruleError](err); ok {
		return e.hint
	}
	return ""
}

// describe spells c out, such as "runes<=50", ">=18", or "rules.Email".
func describe(c check) string {
	switch c := c.(type) {
	case lengthCheck:
		if c.runes {
			return "runes" + c.op.String() + strconv.Itoa(c.bound)
		}
		return "len" + c.op.String() + strconv.Itoa(c.bound)
	case compareCheck:
		return c.op.String() + c.bound
	case oneOfCheck:
		return "oneof(" + strings.Join(c.values, ",") + ")"
	case confirmCheck:
		return "confirms " + c.field + " as " + c.attribute
	case patternCheck:
		return "pattern(" + c.expr + ")"
	case formatCheck:
		return "rules." + c.rule
	}
	return fmt.Sprintf("%#v", c)
}

// ParseTag is parseTag with each level spelled as a tag of its own, required
// and omitempty first, such as "required,max=50".
func ParseTag(tag reflect.StructTag) ([]string, error) {
	levels, err := parseTag(tag)
	if levels == nil {
		return nil, err
	}

	spelled := make([]string, len(levels))
	for i, l := range levels {
		var parts []string
		if l.required {
			parts = append(parts, "required")
		}
		if l.omitEmpty {
			parts = append(parts, "omitempty")
		}
		for _, r := range l.rules {
			if r.param == "" {
				parts = append(parts, r.name)
			} else {
				parts = append(parts, r.name+"="+r.param)
			}
		}
		spelled[i] = strings.Join(parts, ",")
	}
	return spelled, nil
}
