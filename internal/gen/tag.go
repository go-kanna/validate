package gen

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
)

// tagKey is the struct tag holding the rules.
const tagKey = "validate"

// level holds the rules for one level of a field: the field itself, or, after
// each dive, the elements of the level before.
type level struct {
	// required reports blank for the zero value, and nothing else: whatever
	// else an empty value breaks only says again that it is missing.
	required bool

	// omitEmpty lets the zero value past the rules of the level.
	omitEmpty bool

	rules []rule
}

// rule is one rule of a validate tag, such as max=50.
type rule struct {
	name string

	// param is what follows the "=", or "" for a rule written without one.
	param string
}

// parseTag reads the validate tag in tag into its levels: the field itself
// first, then one level per dive. It returns nil when there is no validate tag.
// It checks the syntax only; whether a rule exists and fits the field is for
// the caller.
//
// Rules are separated by commas, except that pattern takes everything after
// "pattern=" as its regular expression, which may well hold a comma, as in
// \d{1,3}. That makes pattern the last rule of a tag.
func parseTag(tag reflect.StructTag) ([]level, error) {
	s, ok, err := lookup(tag)
	if !ok || err != nil {
		return nil, err
	}

	levels := []level{{}}
	for more := s != ""; more; {
		var item string
		if strings.HasPrefix(s, "pattern=") {
			item, more = s, false
		} else {
			item, s, more = strings.Cut(s, ",")
		}
		if levels, err = add(levels, item); err != nil {
			return nil, err
		}
	}

	for _, l := range levels {
		if l.required && l.omitEmpty {
			return nil, errors.New("omitempty lets through the empty value required rejects; keep one of them")
		}
	}
	if last := levels[len(levels)-1]; len(levels) > 1 && !last.required && len(last.rules) == 0 {
		return nil, errors.New("dive must be followed by the rules for the elements")
	}
	return levels, nil
}

// lookup returns the validate tag in tag and whether there is one.
//
// A tag that is there but is not a valid quoted string is an error. reflect
// treats it as absent, so without this check a field whose tag says
// `validate:"pattern=^\d+$"` would quietly go unchecked: inside a struct tag the
// value is a Go string literal, and \d is not an escape it knows.
func lookup(tag reflect.StructTag) (string, bool, error) {
	if s, ok := tag.Lookup(tagKey); ok {
		return s, true, nil
	}
	if key := tagKey + `:"`; !strings.HasPrefix(string(tag), key) && !strings.Contains(string(tag), " "+key) {
		return "", false, nil
	}
	if strings.Contains(string(tag), `\`) {
		return "", true, errors.New(`the validate tag is not a valid quoted string; write a backslash as \\`)
	}
	return "", true, errors.New("the validate tag is not a valid quoted string")
}

// add adds item, one comma-separated part of a tag, to the last of levels.
func add(levels []level, item string) ([]level, error) {
	name, param, hasParam := strings.Cut(item, "=")
	l := &levels[len(levels)-1]

	switch {
	case item == "":
		return nil, errors.New("empty rule; check for a stray comma")
	case strings.ContainsAny(name, " \t"):
		return nil, fmt.Errorf("rule %q contains a space; rules are separated by commas alone", item)
	case name == "":
		return nil, fmt.Errorf("rule %q has no name", item)
	case hasParam && param == "":
		return nil, fmt.Errorf("rule %q has no value after =", item)
	case name == "dive", name == "required", name == "omitempty":
		if hasParam {
			return nil, fmt.Errorf("%s takes no value", name)
		}
	}

	switch name {
	case "dive":
		return append(levels, level{}), nil
	case "required":
		if l.required {
			return nil, errors.New("required is given twice")
		}
		l.required = true
	case "omitempty":
		if l.omitEmpty {
			return nil, errors.New("omitempty is given twice")
		}
		if len(l.rules) > 0 {
			return nil, fmt.Errorf("omitempty must come before %s, which it lets an empty value through", l.rules[0].name)
		}
		l.omitEmpty = true
	default:
		l.rules = append(l.rules, rule{name: name, param: param})
	}
	return levels, nil
}
