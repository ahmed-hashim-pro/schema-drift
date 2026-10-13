// Package schema is a strict subset of JSON Schema: the part a compatibility
// checker can reason about exactly.
//
// Supported keywords: type (a name or a list, so ["string","null"] is a
// nullable string), enum, properties, required, additionalProperties (a
// boolean), items (a single schema), minimum, maximum, minLength,
// maxLength, minItems, maxItems, format (date-time, date, uuid, email:
// asserted, not just annotated), plus title, description, $schema, $id and
// examples, which do not constrain anything.
//
// Everything else is an error, not ignored. A checker that skipped a
// keyword it did not understand would call changes compatible that are not.
package schema

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"sort"
	"strings"
)

// Types, most specific first. "number" includes integers.
var AllTypes = []string{"null", "boolean", "integer", "number", "string", "object", "array"}

var Formats = []string{"date-time", "date", "uuid", "email"}

type Property struct {
	Name   string
	Schema *Schema
}

type Schema struct {
	// Types lists the allowed JSON types; nil means any type.
	Types []string
	// Enum, when non-nil, is the complete list of allowed values.
	Enum []any

	Properties []Property
	Required   []string
	// AdditionalProperties: may an object carry keys not in Properties?
	// JSON Schema's default is true.
	AdditionalProperties bool

	Items *Schema

	Minimum, Maximum     *float64
	MinLength, MaxLength *int
	MinItems, MaxItems   *int
	Format               string

	Title, Description string
}

// Prop returns the schema of a declared property.
func (s *Schema) Prop(name string) (*Schema, bool) {
	for _, p := range s.Properties {
		if p.Name == name {
			return p.Schema, true
		}
	}
	return nil, false
}

func (s *Schema) IsRequired(name string) bool {
	for _, r := range s.Required {
		if r == name {
			return true
		}
	}
	return false
}

// Allows reports whether the schema admits values of JSON type t at all
// (before other keywords). "integer" is allowed by "number".
func (s *Schema) Allows(t string) bool {
	if s.Types == nil {
		return true
	}
	for _, x := range s.Types {
		if x == t || (t == "integer" && x == "number") {
			return true
		}
	}
	return false
}

// AllowsFraction: may a number with a fractional part pass?
func (s *Schema) AllowsFraction() bool { return s.Allows("number") }

// IsAny: does the schema accept every JSON value?
func (s *Schema) IsAny() bool {
	return s.Types == nil && s.Enum == nil && len(s.Properties) == 0 && len(s.Required) == 0 &&
		s.AdditionalProperties && s.Items == nil && s.Minimum == nil && s.Maximum == nil &&
		s.MinLength == nil && s.MaxLength == nil && s.MinItems == nil && s.MaxItems == nil && s.Format == ""
}

// Error locates a problem in a schema document by JSON Pointer.
type Error struct {
	Pointer string
	Message string
}

func (e *Error) Error() string { return fmt.Sprintf("%s: %s", orRoot(e.Pointer), e.Message) }

var known = map[string]bool{
	"type": true, "enum": true, "properties": true, "required": true, "additionalProperties": true,
	"items": true, "minimum": true, "maximum": true, "minLength": true, "maxLength": true,
	"minItems": true, "maxItems": true, "format": true,
	"title": true, "description": true, "$schema": true, "$id": true, "examples": true,
}

// Keywords that are real JSON Schema but outside the subset, with a hint.
var unsupported = map[string]string{
	"$ref":                  "inline the referenced schema",
	"oneOf":                 "not in the subset; a type list covers nullable fields",
	"anyOf":                 "not in the subset; a type list covers nullable fields",
	"allOf":                 "not in the subset",
	"not":                   "not in the subset",
	"if":                    "not in the subset",
	"then":                  "not in the subset",
	"else":                  "not in the subset",
	"const":                 "use a one-value enum",
	"pattern":               "not in the subset: compatibility of regular expressions is not decided here",
	"patternProperties":     "not in the subset",
	"exclusiveMinimum":      "use minimum",
	"exclusiveMaximum":      "use maximum",
	"multipleOf":            "not in the subset",
	"uniqueItems":           "not in the subset",
	"dependentRequired":     "not in the subset",
	"dependencies":          "not in the subset",
	"propertyNames":         "not in the subset",
	"minProperties":         "not in the subset",
	"maxProperties":         "not in the subset",
	"contains":              "not in the subset",
	"prefixItems":           "not in the subset",
	"unevaluatedProperties": "not in the subset",
	"default":               "defaults are not part of a contract here; drop it",
	"nullable":              `OpenAPI's nullable is not JSON Schema; use "type": ["string", "null"]`,
}

// Load reads and parses a schema file.
func Load(path string) (*Schema, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	s, err := Parse(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return s, nil
}

// Parse parses a schema document, rejecting anything outside the subset and
// any schema no value could satisfy.
func Parse(data []byte) (*Schema, error) {
	v, err := Decode(data)
	if err != nil {
		return nil, err
	}
	return parseNode(v, "")
}

func fail(ptr, format string, args ...any) error {
	return &Error{Pointer: ptr, Message: fmt.Sprintf(format, args...)}
}

func parseNode(v any, ptr string) (*Schema, error) {
	if b, ok := v.(bool); ok && b {
		return &Schema{AdditionalProperties: true}, nil
	}
	o, ok := v.(*Object)
	if !ok {
		return nil, fail(ptr, "a schema must be a JSON object")
	}
	s := &Schema{AdditionalProperties: true}
	for _, k := range o.Keys {
		if hint, bad := unsupported[k]; bad {
			return nil, fail(ptr, "keyword %q is not supported (%s)", k, hint)
		}
		if !known[k] {
			msg := fmt.Sprintf("unknown keyword %q", k)
			if guess := closest(k); guess != "" {
				msg += fmt.Sprintf(" (did you mean %q?)", guess)
			}
			return nil, fail(ptr, "%s", msg)
		}
	}
	at := func(k string) string { return ptr + "/" + k }

	if t, ok := o.Get("type"); ok {
		var names []any
		switch x := t.(type) {
		case string:
			names = []any{x}
		case []any:
			names = x
			if len(names) == 0 {
				return nil, fail(at("type"), "an empty type list allows nothing")
			}
		default:
			return nil, fail(at("type"), "must be a type name or a list of them")
		}
		seen := map[string]bool{}
		for _, n := range names {
			name, ok := n.(string)
			if !ok || !contains(AllTypes, name) {
				return nil, fail(at("type"), "%s is not a JSON Schema type (%s)", Compact(n), strings.Join(AllTypes, ", "))
			}
			if seen[name] {
				return nil, fail(at("type"), "%q is listed twice", name)
			}
			seen[name] = true
			s.Types = append(s.Types, name)
		}
	}
	if e, ok := o.Get("enum"); ok {
		vals, ok := e.([]any)
		if !ok || len(vals) == 0 {
			return nil, fail(at("enum"), "must be a non-empty list")
		}
		for i, x := range vals {
			for j := 0; j < i; j++ {
				if Equal(vals[j], x) {
					return nil, fail(at("enum"), "%s is listed twice", Compact(x))
				}
			}
		}
		s.Enum = vals
	}
	if p, ok := o.Get("properties"); ok {
		po, ok := p.(*Object)
		if !ok {
			return nil, fail(at("properties"), "must be an object of schemas")
		}
		for _, name := range po.Keys {
			child, err := parseNode(po.Values[name], at("properties")+"/"+EscapePointer(name))
			if err != nil {
				return nil, err
			}
			s.Properties = append(s.Properties, Property{Name: name, Schema: child})
		}
	}
	if r, ok := o.Get("required"); ok {
		list, ok := r.([]any)
		if !ok {
			return nil, fail(at("required"), "must be a list of property names")
		}
		for _, x := range list {
			name, ok := x.(string)
			if !ok {
				return nil, fail(at("required"), "%s is not a property name", Compact(x))
			}
			if contains(s.Required, name) {
				return nil, fail(at("required"), "%q is listed twice", name)
			}
			s.Required = append(s.Required, name)
		}
	}
	if a, ok := o.Get("additionalProperties"); ok {
		b, ok := a.(bool)
		if !ok {
			return nil, fail(at("additionalProperties"), "must be true or false (schemas for additional properties are not in the subset)")
		}
		s.AdditionalProperties = b
	}
	if it, ok := o.Get("items"); ok {
		if _, isList := it.([]any); isList {
			return nil, fail(at("items"), "tuple items are not in the subset; give one schema")
		}
		child, err := parseNode(it, at("items"))
		if err != nil {
			return nil, err
		}
		s.Items = child
	}
	var err error
	if s.Minimum, err = number(o, "minimum", ptr); err != nil {
		return nil, err
	}
	if s.Maximum, err = number(o, "maximum", ptr); err != nil {
		return nil, err
	}
	for _, c := range []struct {
		key string
		dst **int
	}{{"minLength", &s.MinLength}, {"maxLength", &s.MaxLength}, {"minItems", &s.MinItems}, {"maxItems", &s.MaxItems}} {
		if *c.dst, err = count(o, c.key, ptr); err != nil {
			return nil, err
		}
	}
	if f, ok := o.Get("format"); ok {
		name, ok := f.(string)
		if !ok || !contains(Formats, name) {
			return nil, fail(at("format"), "%s is not a supported format (%s)", Compact(f), strings.Join(Formats, ", "))
		}
		s.Format = name
	}
	for _, k := range []string{"title", "description"} {
		if v, ok := o.Get(k); ok {
			str, ok := v.(string)
			if !ok {
				return nil, fail(at(k), "must be a string")
			}
			if k == "title" {
				s.Title = str
			} else {
				s.Description = str
			}
		}
	}
	return s, s.check(ptr)
}

// check rejects schemas that cannot be satisfied, or say one thing twice in
// contradictory ways. Each is almost always a mistake in a contract.
func (s *Schema) check(ptr string) error {
	if s.Minimum != nil && s.Maximum != nil && *s.Minimum > *s.Maximum {
		return fail(ptr, "minimum %v is greater than maximum %v", *s.Minimum, *s.Maximum)
	}
	if s.MinLength != nil && s.MaxLength != nil && *s.MinLength > *s.MaxLength {
		return fail(ptr, "minLength %d is greater than maxLength %d", *s.MinLength, *s.MaxLength)
	}
	if s.MinItems != nil && s.MaxItems != nil && *s.MinItems > *s.MaxItems {
		return fail(ptr, "minItems %d is greater than maxItems %d", *s.MinItems, *s.MaxItems)
	}
	if s.Types != nil && !s.Allows("number") && s.Allows("integer") && s.Minimum != nil && s.Maximum != nil &&
		math.Ceil(*s.Minimum) > math.Floor(*s.Maximum) {
		return fail(ptr, "no integer lies between minimum %v and maximum %v", *s.Minimum, *s.Maximum)
	}
	for _, r := range s.Required {
		if _, ok := s.Prop(r); !ok && !s.AdditionalProperties {
			return fail(ptr+"/required", "%q is required but not declared, and additionalProperties is false: no object can satisfy this", r)
		}
	}
	if s.Enum != nil {
		// Each enum value must pass the rest of the schema, or it is dead.
		rest := *s
		rest.Enum = nil
		for _, v := range s.Enum {
			if errs := Validate(&rest, v); len(errs) > 0 {
				return fail(ptr+"/enum", "%s can never be valid: %s", Compact(v), errs[0].Message)
			}
		}
	}
	if s.Format != "" && s.Types != nil && !s.Allows("string") {
		return fail(ptr+"/format", "format %q on a schema that allows no strings", s.Format)
	}
	return nil
}

func number(o *Object, key, ptr string) (*float64, error) {
	v, ok := o.Get(key)
	if !ok {
		return nil, nil
	}
	f, ok := Num(v)
	if !ok {
		return nil, fail(ptr+"/"+key, "must be a number")
	}
	return &f, nil
}

func count(o *Object, key, ptr string) (*int, error) {
	v, ok := o.Get(key)
	if !ok {
		return nil, nil
	}
	n, isNum := v.(json.Number)
	i, err := n.Int64()
	if !isNum || err != nil || i < 0 {
		return nil, fail(ptr+"/"+key, "must be a non-negative integer")
	}
	x := int(i)
	return &x, nil
}

func contains(xs []string, x string) bool {
	for _, y := range xs {
		if y == x {
			return true
		}
	}
	return false
}

// closest suggests a known keyword within edit distance 2.
func closest(k string) string {
	best, bestD := "", 3
	var names []string
	for n := range known {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		if d := distance(strings.ToLower(k), strings.ToLower(n)); d < bestD {
			best, bestD = n, d
		}
	}
	return best
}

func distance(a, b string) int {
	prev := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur := make([]int, len(b)+1)
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev = cur
	}
	return prev[len(b)]
}

// MarshalJSON writes the schema back as JSON Schema, keywords in a fixed order.
func (s *Schema) MarshalJSON() ([]byte, error) {
	o := &Object{Values: map[string]any{}}
	if s.Title != "" {
		o.Set("title", s.Title)
	}
	if s.Description != "" {
		o.Set("description", s.Description)
	}
	switch len(s.Types) {
	case 0:
	case 1:
		o.Set("type", s.Types[0])
	default:
		ts := make([]any, len(s.Types))
		for i, t := range s.Types {
			ts[i] = t
		}
		o.Set("type", ts)
	}
	if s.Enum != nil {
		o.Set("enum", s.Enum)
	}
	if s.Format != "" {
		o.Set("format", s.Format)
	}
	for _, kv := range []struct {
		k string
		v *float64
	}{{"minimum", s.Minimum}, {"maximum", s.Maximum}} {
		if kv.v != nil {
			o.Set(kv.k, json.Number(fmt.Sprint(*kv.v)))
		}
	}
	for _, kv := range []struct {
		k string
		v *int
	}{{"minLength", s.MinLength}, {"maxLength", s.MaxLength}, {"minItems", s.MinItems}, {"maxItems", s.MaxItems}} {
		if kv.v != nil {
			o.Set(kv.k, *kv.v)
		}
	}
	if len(s.Properties) > 0 {
		props := &Object{Values: map[string]any{}}
		for _, p := range s.Properties {
			props.Set(p.Name, p.Schema)
		}
		o.Set("properties", props)
	}
	if len(s.Required) > 0 {
		o.Set("required", s.Required)
	}
	if !s.AdditionalProperties {
		o.Set("additionalProperties", false)
	}
	if s.Items != nil {
		o.Set("items", s.Items)
	}
	return o.MarshalJSON()
}
