// Package schedule declares a rule type and a value type, for validate tags in
// package emit to name with rule=.
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

// Deadline is a value type as kanna-value generates one, down to the
// constructor that rule= calls.
type Deadline struct{ t time.Time }

func NewDeadline(t time.Time) (Deadline, error) {
	if t.Year() < 2000 {
		return Deadline{}, validate.FailWith("too_early", map[string]any{"year": 2000})
	}
	return Deadline{t: t}, nil
}
