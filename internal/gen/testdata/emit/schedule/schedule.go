// Package schedule declares value types, for validate tags in package emit to
// name with rule=.
package schedule

import (
	"time"

	"github.com/go-kanna/validate"
)

// Since2000 holds a time in 2000 or later, as a value type of kanna-value.
type Since2000 struct{ t time.Time }

func (s Since2000) Time() time.Time { return s.t }

func NewSince2000(t time.Time) (Since2000, error) {
	if t.Year() < 2000 {
		return Since2000{}, validate.FailWith("too_early", map[string]any{"year": 2000})
	}
	return Since2000{t: t}, nil
}

// Deadline is a value type as kanna-value generates one, down to what rule=
// knows it by: its accessor and its constructor.
type Deadline struct{ t time.Time }

func (d Deadline) Time() time.Time { return d.t }

func NewDeadline(t time.Time) (Deadline, error) {
	if t.Year() < 2000 {
		return Deadline{}, validate.FailWith("too_early", map[string]any{"year": 2000})
	}
	return Deadline{t: t}, nil
}

// Shift is an enum as kanna-value generates one, down to what rule= knows it
// by: its constants and its constructor.
type Shift string

const (
	ShiftDay   Shift = "day"
	ShiftNight Shift = "night"
)

func NewShift(v string) (Shift, error) {
	switch s := Shift(v); s {
	case ShiftDay, ShiftNight:
		return s, nil
	}
	return "", validate.Inclusion()
}
