package compat

import (
	"math"
	"strings"

	"github.com/ahmed-hashim-pro/schema-drift/schema"
)

// A writer type that no value can inhabit (a date-formatted string capped
// at 5 characters, an integer between 0.2 and 0.8) breaks nothing: the
// writer can never send one. These decide whether a type is inhabited.

func satisfiable(s *schema.Schema) bool {
	if s.Enum != nil {
		return true // the parser checked every enum value against the schema
	}
	types := s.Types
	if types == nil {
		types = schema.AllTypes
	}
	for _, t := range types {
		if typeSatisfiable(s, t) {
			return true
		}
	}
	return false
}

func typeSatisfiable(s *schema.Schema, t string) bool {
	if s.Enum != nil {
		for _, v := range s.Enum {
			if schema.TypeOf(v) == t || (t == "number" && schema.TypeOf(v) == "integer") {
				return true
			}
		}
		return false
	}
	switch t {
	case "integer":
		lo, hi := ceilp(s.Minimum), floorp(s.Maximum)
		return lo == nil || hi == nil || *lo <= *hi
	case "number":
		return true
	case "string":
		_, ok := stringOfLength(s, -1)
		return ok
	case "array":
		if s.MinItems != nil && *s.MinItems > 0 && s.Items != nil {
			return satisfiable(s.Items)
		}
		return true
	case "object":
		for _, r := range s.Required {
			if ps, ok := s.Prop(r); ok && !satisfiable(ps) {
				return false
			}
		}
		return true
	}
	return true
}

// fractionSatisfiable: is there a non-integer number the schema allows?
func fractionSatisfiable(s *schema.Schema) bool {
	if !s.AllowsFraction() {
		return false
	}
	if s.Minimum != nil && s.Maximum != nil && *s.Minimum == *s.Maximum {
		return *s.Minimum != math.Trunc(*s.Minimum)
	}
	return true
}

// stringOfLength returns a string valid for s's string keywords, of length
// n when n >= 0, else of any length s allows.
func stringOfLength(s *schema.Schema, n int) (string, bool) {
	lo, hi := 0, 64
	if s.MinLength != nil {
		lo = *s.MinLength
	}
	if s.MaxLength != nil {
		hi = *s.MaxLength
	}
	if n >= 0 {
		lo, hi = n, n
	}
	for l := lo; l <= hi && l <= lo+64; l++ {
		if str, ok := formatOfLength(s.Format, l); ok {
			return str, true
		}
	}
	return "", false
}

// formatOfLength builds a string of the format with exactly n characters.
func formatOfLength(format string, n int) (string, bool) {
	switch format {
	case "":
		return strings.Repeat("x", n), true
	case "uuid":
		return "00000000-0000-4000-8000-000000000000", n == 36
	case "date":
		return "2024-01-01", n == 10
	case "date-time":
		switch {
		case n == 20:
			return "2024-01-01T00:00:00Z", true
		case n >= 22:
			return "2024-01-01T00:00:00." + strings.Repeat("1", n-21) + "Z", true
		}
		return "", false
	case "email":
		if n < 5 {
			return "", false
		}
		return strings.Repeat("a", n-4) + "@b.c", true
	}
	return "", false
}
