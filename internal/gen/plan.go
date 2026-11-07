package gen

import (
	"go/token"
	"go/types"

	"github.com/go-kanna/internal/diag"
	"github.com/go-kanna/internal/validation"
)

// plan is what the generated Validate method of one struct does.
type plan struct {
	target

	// fields are the fields with anything to check, in declaration order.
	fields []fieldPlan
}

// fieldPlan is what the generated method checks of one field.
type fieldPlan struct {
	validation.Field

	// path is the name errors report the field by: its JSON name, or the Go
	// name when the json tag gives none.
	path string

	// flatten keeps what the field's own Validate reports at the level of the
	// struct, as encoding/json flattens an embedded struct without a JSON name.
	flatten bool
}

// plans decides what the Validate method of each target checks. rewritten
// names the packages whose output this run rewrites. A target with a rule that
// does not apply is left out, and said why.
func plans(fset *token.FileSet, targets []target, rewritten map[string]bool) ([]plan, []diag.Diag) {
	writes := make([]*types.TypeName, len(targets))
	for i, t := range targets {
		writes[i] = t.Named.Obj()
	}
	p := validation.NewPlanner(fset, writes, rewritten)

	var (
		found []plan
		diags []diag.Diag
	)
	for _, t := range targets {
		fields, ds := p.Struct(t.Struct)
		diags = append(diags, ds...)
		if len(ds) > 0 {
			continue
		}
		pl := plan{target: t}
		for _, f := range fields {
			// encoding/json flattens an embedded struct without a JSON name, and
			// leaves out one tagged json:"-", which goes under its Go name as any
			// field tagged so does.
			path, named := validation.JSONName(f.Field)
			flatten := f.Embedded && !named && f.Tag.Get("json") != "-"
			pl.fields = append(pl.fields, fieldPlan{Field: f, path: path, flatten: flatten})
		}
		found = append(found, pl)
	}
	return found, diags
}
