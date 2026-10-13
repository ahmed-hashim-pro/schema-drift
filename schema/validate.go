package schema

import (
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

// Violation is one way a value fails a schema, located by JSON Pointer.
type Violation struct {
	Pointer string
	Message string
}

func (v Violation) String() string { return fmt.Sprintf("%s: %s", orRoot(v.Pointer), v.Message) }

// Validate returns every violation of s by v (nil when v is valid). v is a
// value from Decode.
func Validate(s *Schema, v any) []Violation {
	var out []Violation
	validate(s, v, "", &out)
	return out
}

// Valid is Validate == nil, without collecting messages.
func Valid(s *Schema, v any) bool { return len(Validate(s, v)) == 0 }

func validate(s *Schema, v any, ptr string, out *[]Violation) {
	add := func(format string, args ...any) {
		*out = append(*out, Violation{Pointer: ptr, Message: fmt.Sprintf(format, args...)})
	}
	if s.Enum != nil {
		found := false
		for _, e := range s.Enum {
			if Equal(e, v) {
				found = true
				break
			}
		}
		if !found {
			add("%s is not one of %s", Compact(v), Compact(s.Enum))
			return
		}
	}
	t := TypeOf(v)
	if !s.Allows(t) {
		want := strings.Join(s.Types, " or ")
		add("expected %s, got %s %s", want, t, Compact(v))
		return
	}
	switch t {
	case "integer", "number":
		f, _ := Num(v)
		if s.Minimum != nil && f < *s.Minimum {
			add("%v is less than the minimum %v", Compact(v), *s.Minimum)
		}
		if s.Maximum != nil && f > *s.Maximum {
			add("%v is greater than the maximum %v", Compact(v), *s.Maximum)
		}
	case "string":
		str := v.(string)
		n := utf8.RuneCountInString(str)
		if s.MinLength != nil && n < *s.MinLength {
			add("%q is shorter than %d characters", str, *s.MinLength)
		}
		if s.MaxLength != nil && n > *s.MaxLength {
			add("%q is longer than %d characters", str, *s.MaxLength)
		}
		if s.Format != "" && !FormatOK(s.Format, str) {
			add("%q is not a valid %s", str, s.Format)
		}
	case "array":
		arr := v.([]any)
		if s.MinItems != nil && len(arr) < *s.MinItems {
			add("%d items, fewer than %d", len(arr), *s.MinItems)
		}
		if s.MaxItems != nil && len(arr) > *s.MaxItems {
			add("%d items, more than %d", len(arr), *s.MaxItems)
		}
		if s.Items != nil {
			for i, x := range arr {
				validate(s.Items, x, fmt.Sprintf("%s/%d", ptr, i), out)
			}
		}
	case "object":
		o := v.(*Object)
		for _, r := range s.Required {
			if _, ok := o.Values[r]; !ok {
				add("required property %q is missing", r)
			}
		}
		for _, k := range o.Keys {
			child := ptr + "/" + EscapePointer(k)
			if ps, ok := s.Prop(k); ok {
				validate(ps, o.Values[k], child, out)
			} else if !s.AdditionalProperties {
				*out = append(*out, Violation{Pointer: child, Message: fmt.Sprintf("property %q is not allowed", k)})
			}
		}
	}
}

var (
	uuidRE  = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
	emailRE = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)
)

// FormatOK checks a string against one of the supported formats.
func FormatOK(format, s string) bool {
	switch format {
	case "date-time":
		_, err := time.Parse(time.RFC3339Nano, s)
		return err == nil
	case "date":
		_, err := time.Parse("2006-01-02", s)
		return err == nil
	case "uuid":
		return uuidRE.MatchString(s)
	case "email":
		return emailRE.MatchString(s)
	}
	return false
}

// FormatExample is a valid string of the format.
func FormatExample(format string) string {
	switch format {
	case "date-time":
		return "2024-01-01T00:00:00Z"
	case "date":
		return "2024-01-01"
	case "uuid":
		return "00000000-0000-4000-8000-000000000000"
	case "email":
		return "a@example.com"
	}
	return ""
}
