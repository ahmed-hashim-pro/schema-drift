package compat

import (
	"math"
	"strings"

	"github.com/ahmed-hashim-pro/schema-drift/schema"
)

// example builds a small value valid under s, if it can find one.
func example(s *schema.Schema) (any, bool) {
	if s.Enum != nil {
		return s.Enum[0], true
	}
	types := s.Types
	if types == nil {
		types = []string{"string", "integer", "boolean", "null", "object", "array", "number"}
	}
	for _, t := range types {
		v := exampleOfType(s, t)
		if schema.Valid(s, v) {
			return v, true
		}
	}
	return nil, false
}

// exampleObject is example() for the object type, kept as an *Object so a
// witness can be built by editing it.
func exampleObject(s *schema.Schema) (*schema.Object, bool) {
	o, ok := exampleOfType(s, "object").(*schema.Object)
	return o, ok && schema.Valid(s, o)
}

func exampleOfType(s *schema.Schema, t string) any {
	switch t {
	case "null":
		return nil
	case "boolean":
		return false
	case "integer", "number":
		v := 0.0
		lo, hi := s.Minimum, s.Maximum
		if t == "integer" {
			lo, hi = ceilp(lo), floorp(hi)
		}
		if lo != nil && v < *lo {
			v = *lo
		}
		if hi != nil && v > *hi {
			v = *hi
		}
		return num(v)
	case "string":
		if str, ok := stringOfLength(s, -1); ok {
			return str
		}
		return ""
	case "array":
		n := 0
		if s.MinItems != nil {
			n = *s.MinItems
		}
		arr := make([]any, n)
		if n > 0 {
			item := any(nil)
			if s.Items != nil {
				item, _ = example(s.Items)
			}
			for i := range arr {
				arr[i] = schema.Clone(item)
			}
		}
		return arr
	case "object":
		o := schema.NewObject()
		for _, r := range s.Required {
			var v any
			if ps, ok := s.Prop(r); ok {
				v, _ = example(ps)
			}
			o.Set(r, v)
		}
		return o
	}
	return nil
}

// poolOf is a handful of values of one JSON type.
func poolOf(t string) []any {
	switch t {
	case "null":
		return []any{nil}
	case "boolean":
		return []any{false, true}
	case "integer":
		return []any{num(0), num(1), num(-1), num(100)}
	case "number":
		return []any{num(0.5), num(-0.5), num(1.5)}
	case "string":
		return []any{"", "x", "abc"}
	case "array":
		return []any{[]any{}}
	case "object":
		return []any{schema.NewObject()}
	}
	return nil
}

// firstInvalid finds a value that s rejects: a witness against "any value".
func firstInvalid(s *schema.Schema) (any, bool) {
	anything := &schema.Schema{AdditionalProperties: true}
	for _, r := range Subset(anything, s, "", "") {
		if r.Proven {
			return r.Witness, true
		}
	}
	var cands []any
	for _, t := range schema.AllTypes {
		cands = append(cands, poolOf(t)...)
	}
	cands = append(cands, stringCandidates(&schema.Schema{AdditionalProperties: true}, s)...)
	cands = append(cands, numberCandidates(&schema.Schema{AdditionalProperties: true}, s)...)
	for _, v := range cands {
		if !schema.Valid(s, v) {
			return v, true
		}
	}
	return nil, false
}

func fractions(a *schema.Schema) []any {
	out := []any{num(0.5), num(-0.5)}
	for _, p := range []*float64{a.Minimum, a.Maximum} {
		if p != nil {
			out = append(out, num(*p+0.5), num(*p-0.5), num(math.Floor(*p)+0.5))
		}
	}
	return out
}

// numberCandidates are the values on each side of every bound either
// schema states.
func numberCandidates(a, b *schema.Schema) []any {
	var out []any
	add := func(f float64) { out = append(out, num(f)) }
	for _, p := range []*float64{a.Minimum, a.Maximum, b.Minimum, b.Maximum} {
		if p == nil {
			continue
		}
		add(*p)
		add(math.Floor(*p))
		add(math.Ceil(*p))
		add(math.Floor(*p) - 1)
		add(math.Ceil(*p) + 1)
		add(*p - 0.5)
		add(*p + 0.5)
	}
	add(0)
	add(-1)
	add(1e9)
	add(-1e9)
	return out
}

func stringCandidates(a, b *schema.Schema) []any {
	lengths := []int{0, 1, 3}
	for _, p := range []*int{a.MinLength, a.MaxLength, b.MinLength, b.MaxLength} {
		if p != nil {
			lengths = append(lengths, *p, *p+1)
			if *p > 0 {
				lengths = append(lengths, *p-1)
			}
		}
	}
	var out []any
	for _, f := range schema.Formats {
		out = append(out, schema.FormatExample(f))
	}
	for _, n := range lengths {
		out = append(out, strings.Repeat("x", n))
		for _, f := range []string{a.Format, b.Format} {
			if str, ok := formatOfLength(f, n); ok && f != "" {
				out = append(out, str)
			}
		}
	}
	if str, ok := stringOfLength(a, -1); ok {
		out = append(out, str)
	}
	return out
}

func arrayCandidates(a, b *schema.Schema) []any {
	item := any(nil)
	if a.Items != nil {
		item, _ = example(a.Items)
	}
	lengths := []int{0, 1}
	for _, p := range []*int{a.MinItems, a.MaxItems, b.MinItems, b.MaxItems} {
		if p != nil {
			lengths = append(lengths, *p, *p+1)
			if *p > 0 {
				lengths = append(lengths, *p-1)
			}
		}
	}
	var out []any
	for _, n := range lengths {
		arr := make([]any, n)
		for i := range arr {
			arr[i] = schema.Clone(item)
		}
		out = append(out, arr)
	}
	return out
}

// domain enumerates a schema's values when there are few enough: nulls,
// booleans, and integers in a short closed range.
func domain(s *schema.Schema) ([]any, bool) {
	if s.Types == nil {
		return nil, false
	}
	var out []any
	for _, t := range s.Types {
		switch t {
		case "null":
			out = append(out, nil)
		case "boolean":
			out = append(out, false, true)
		case "integer":
			lo, hi := ceilp(s.Minimum), floorp(s.Maximum)
			if lo == nil || hi == nil || *hi-*lo > 256 {
				return nil, false
			}
			for x := *lo; x <= *hi; x++ {
				out = append(out, num(x))
			}
		default:
			return nil, false
		}
	}
	return out, true
}

// distinct produces at least n different values valid under s (when s
// allows that many), so one of them escapes any n-2 value enum.
func distinct(s *schema.Schema, n int) []any {
	var out []any
	if ex, ok := example(s); ok {
		out = append(out, ex)
	}
	types := s.Types
	if types == nil {
		types = schema.AllTypes
	}
	for _, t := range types {
		switch t {
		case "integer", "number":
			start := 0.0
			if s.Minimum != nil {
				start = math.Ceil(*s.Minimum)
			} else if s.Maximum != nil {
				start = math.Floor(*s.Maximum) - float64(n)
			}
			for i := 0; i < n; i++ {
				out = append(out, num(start+float64(i)))
			}
			if t == "number" {
				out = append(out, fractions(s)...)
			}
		case "string":
			if s.Format != "" {
				out = append(out, formatVariants(s.Format, n)...)
				continue
			}
			length := 1
			if s.MinLength != nil && *s.MinLength > length {
				length = *s.MinLength
			}
			if s.MaxLength != nil && *s.MaxLength < length {
				length = *s.MaxLength
			}
			for i := 0; i < n; i++ {
				out = append(out, variant(i, length))
			}
		case "array":
			item := any(nil)
			if s.Items != nil {
				item, _ = example(s.Items)
			}
			for l := 0; l <= n; l++ {
				arr := make([]any, l)
				for i := range arr {
					arr[i] = schema.Clone(item)
				}
				out = append(out, arr)
			}
		case "object":
			base, ok := exampleObject(s)
			if !ok {
				continue
			}
			out = append(out, base)
			for _, p := range s.Properties {
				if v, ok := example(p.Schema); ok {
					o := schema.Clone(base).(*schema.Object)
					o.Set(p.Name, v)
					out = append(out, o)
				}
			}
			if s.AdditionalProperties {
				o := schema.Clone(base).(*schema.Object)
				o.Set("undeclared", true)
				out = append(out, o)
			}
		default:
			out = append(out, exampleOfType(s, t))
			out = append(out, poolOf(t)...)
		}
	}
	return out
}

// variant is the i-th distinct string of a given length (base 26).
func variant(i, length int) string {
	if length == 0 {
		return ""
	}
	b := []byte(strings.Repeat("a", length))
	for p := length - 1; p >= 0 && i > 0; p-- {
		b[p] = byte('a' + i%26)
		i /= 26
	}
	return string(b)
}

func formatVariants(format string, n int) []any {
	var out []any
	for i := 0; i < n; i++ {
		switch format {
		case "uuid":
			out = append(out, "00000000-0000-4000-8000-"+leftPad(i, 12))
		case "date":
			out = append(out, "2024-01-"+leftPad(1+i%28, 2))
		case "date-time":
			out = append(out, "2024-01-01T00:"+leftPad(i%60, 2)+":"+leftPad(i/60%60, 2)+"Z")
		case "email":
			out = append(out, variant(i, 3)+"@example.com")
		}
	}
	return out
}

func leftPad(i, width int) string {
	s := strings.Repeat("0", width) + itoa(i)
	return s[len(s)-width:]
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	return string(b)
}
