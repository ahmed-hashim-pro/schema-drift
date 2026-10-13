// Package infer drafts a schema from sample events: the tightest schema in
// the subset that accepts every sample, closed to properties it never saw.
// It is a starting point for a contract, not a contract: a field that was
// always present in the sample is marked required, and the sample may just
// not have shown the case where it is absent.
package infer

import (
	"github.com/ahmed-hashim-pro/schema-drift/schema"
)

type acc struct {
	seen     map[string]bool // JSON types
	strings  []string
	objects  int
	keys     []string
	present  map[string]int
	children map[string]*acc
	items    *acc
}

func newAcc() *acc {
	return &acc{seen: map[string]bool{}, present: map[string]int{}, children: map[string]*acc{}}
}

func (a *acc) add(v any) {
	t := schema.TypeOf(v)
	a.seen[t] = true
	switch t {
	case "string":
		a.strings = append(a.strings, v.(string))
	case "object":
		o := v.(*schema.Object)
		a.objects++
		for _, k := range o.Keys {
			if _, ok := a.children[k]; !ok {
				a.children[k] = newAcc()
				a.keys = append(a.keys, k)
			}
			a.present[k]++
			a.children[k].add(o.Values[k])
		}
	case "array":
		if a.items == nil {
			a.items = newAcc()
		}
		for _, x := range v.([]any) {
			a.items.add(x)
		}
	}
}

func (a *acc) schema() *schema.Schema {
	s := &schema.Schema{AdditionalProperties: true}
	for _, t := range schema.AllTypes {
		if !a.seen[t] || (t == "integer" && a.seen["number"]) {
			continue
		}
		s.Types = append(s.Types, t)
	}
	if len(s.Types) == 0 {
		s.Types = nil // nothing seen (an empty array's items): allow anything
	}
	if a.seen["string"] {
		for _, f := range schema.Formats {
			all := true
			for _, str := range a.strings {
				if !schema.FormatOK(f, str) {
					all = false
					break
				}
			}
			if all {
				s.Format = f
				break
			}
		}
	}
	if a.seen["object"] {
		s.AdditionalProperties = false
		for _, k := range a.keys {
			s.Properties = append(s.Properties, schema.Property{Name: k, Schema: a.children[k].schema()})
			if a.present[k] == a.objects {
				s.Required = append(s.Required, k)
			}
		}
	}
	if a.seen["array"] && a.items != nil && len(a.items.seen) > 0 {
		s.Items = a.items.schema()
	}
	return s
}

// Infer drafts a schema accepting every event.
func Infer(events []any) *schema.Schema {
	a := newAcc()
	for _, e := range events {
		a.add(e)
	}
	return a.schema()
}
