package gen

import (
	"reflect"
	"strings"
)

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
