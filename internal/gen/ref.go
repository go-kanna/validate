package gen

import (
	"go/token"
	"slices"
	"strings"
)

// A qualified rule=, such as rule=model.CompletedAt, names its package as Go
// reads model.CompletedAt in code: by the name the struct's file imports it
// under, or by the package's own name where the file imports it as _, which a
// package that only a tag names has to be. See package qualifier of
// go-kanna/internal.

// splitRef splits ref, a type as rule= names it, into the qualifier naming its
// package, "" for the struct's own, and the type's name. The name follows the
// last dot, since a path written instead of a name, which the caller reports,
// may hold dots of its own. It reports false for what cannot name a type.
func splitRef(ref string) (qual, name string, ok bool) {
	qual, name, qualified := strings.CutLast(ref, ".")
	if !qualified {
		return "", ref, token.IsIdentifier(ref)
	}
	if slices.Contains(strings.Split(qual, "/"), "") {
		return "", "", false
	}
	return qual, name, token.IsIdentifier(name)
}

// ruleRefs returns what the rule= tags of t name, as written.
func ruleRefs(t target) []string {
	var refs []string
	for _, f := range t.Fields {
		levels, _ := parseTag(f.Tag)
		for _, l := range levels {
			for _, r := range l.rules {
				if r.name == "rule" {
					refs = append(refs, r.param)
				}
			}
		}
	}
	return refs
}
