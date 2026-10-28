// Package schedule declares a rule as go-kanna/validated writes one, for a
// validate tag in package emit to name with rule=.
package schedule

import (
	"time"

	"github.com/go-kanna/validate"
)

// Since2000 accepts a time in 2000 or later.
type Since2000 struct{}

func (Since2000) Validate(t time.Time) error {
	if t.Year() < 2000 {
		return validate.FailWith("too_early", map[string]any{"year": 2000})
	}
	return nil
}
