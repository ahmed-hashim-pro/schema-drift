// Package compat decides whether data written under one schema can be read
// under another, and proves every "no" with an example.
//
// The core question is containment: is every value valid under the writer's
// schema also valid under the reader's? The registry modes are built from
// it:
//
//	BACKWARD  consumers on the new schema can read data written with the old: old ⊆ new
//	FORWARD   consumers on the old schema can read data written with the new: new ⊆ old
//	FULL      both
//
// Containment is decided keyword by keyword over the supported subset. For
// each reason it finds, it also builds a concrete event that the writer
// accepts and the reader rejects, and checks that event with the validator.
// A "breaking" verdict therefore comes with its own evidence; the property
// tests check the "compatible" verdicts against random events.
package compat

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/ahmed-hashim-pro/schema-drift/schema"
)

// Reason is one way the writer's data can fail the reader.
type Reason struct {
	// Path locates the problem in an event: /lines/*/sku, with * for any item.
	Path    string
	Message string
	// Witness is a whole event valid under the writer and invalid under the
	// reader, when Proven. (It can be JSON null, so Proven is separate.)
	Witness any
	Proven  bool
}

// Subset returns the reasons some value valid under writer is invalid under
// reader; none means every writer-valid value is reader-valid. w and r name
// the two schemas in messages.
func Subset(writer, reader *schema.Schema, w, r string) []Reason {
	c := &checker{w: w, r: r}
	var out []Reason
	for _, n := range c.sub(writer, reader, "") {
		reason := Reason{Path: orRoot(n.path), Message: n.msg}
		if n.ok && schema.Valid(writer, n.local) && !schema.Valid(reader, n.local) {
			reason.Witness, reason.Proven = n.local, true
		}
		out = append(out, reason)
	}
	return out
}

func contains(xs []string, x string) bool {
	for _, y := range xs {
		if y == x {
			return true
		}
	}
	return false
}

func orRoot(p string) string {
	if p == "" {
		return "/"
	}
	return p
}

type checker struct{ w, r string }

// node is a reason with a witness built at its own level and wrapped by
// each parent on the way up.
type node struct {
	path  string
	msg   string
	local any
	ok    bool
}

func (c *checker) reason(path string, local any, ok bool, format string, args ...any) node {
	return node{path: path, msg: fmt.Sprintf(format, args...), local: local, ok: ok}
}

func (c *checker) sub(a, b *schema.Schema, path string) []node {
	if b.IsAny() || !satisfiable(a) {
		return nil // an empty writer schema sends nothing that could break
	}
	if a.Enum != nil {
		var out []node
		for _, v := range a.Enum {
			if !schema.Valid(b, v) {
				out = append(out, c.reason(path, v, true, "%s allows %s; %s does not", c.w, schema.Compact(v), c.r))
			}
		}
		return out
	}
	if b.Enum != nil {
		if dom, finite := domain(a); finite {
			var out []node
			for _, v := range dom {
				if !schema.Valid(b, v) {
					out = append(out, c.reason(path, v, true, "%s allows %s; %s only accepts %s", c.w, schema.Compact(v), c.r, schema.Compact(b.Enum)))
				}
			}
			return out
		}
		v, ok := search(a, b, distinct(a, len(b.Enum)+2))
		return []node{c.reason(path, v, ok, "%s only accepts %s; %s allows other values", c.r, schema.Compact(b.Enum), c.w)}
	}

	var out []node
	anyType := a.Types == nil
	types := a.Types
	if anyType {
		types = schema.AllTypes
	}
	for _, t := range types {
		if b.Allows(t) || !typeSatisfiable(a, t) {
			continue
		}
		if t == "number" && b.Allows("integer") {
			if !fractionSatisfiable(a) {
				continue
			}
			v, ok := search(a, b, fractions(a))
			out = append(out, c.reason(path, v, ok, "%s allows fractional numbers; %s only integers", c.w, c.r))
			continue
		}
		if t == "integer" && (anyType || contains(types, "number")) {
			continue // reported under "number"
		}
		v, ok := search(a, b, append([]any{exampleOfType(a, t)}, poolOf(t)...))
		out = append(out, c.reason(path, v, ok, "%s allows %s; %s does not (it allows %s)", c.w, article(t), c.r, typeList(b)))
	}
	if a.Allows("integer") && b.Allows("integer") && (typeSatisfiable(a, "integer") || fractionSatisfiable(a)) {
		out = append(out, c.numbers(a, b, path)...)
	}
	if a.Allows("string") && b.Allows("string") && typeSatisfiable(a, "string") {
		out = append(out, c.strings(a, b, path)...)
	}
	if a.Allows("array") && b.Allows("array") && typeSatisfiable(a, "array") {
		out = append(out, c.arrays(a, b, path)...)
	}
	if a.Allows("object") && b.Allows("object") && typeSatisfiable(a, "object") {
		out = append(out, c.objects(a, b, path)...)
	}
	return out
}

func (c *checker) numbers(a, b *schema.Schema, path string) []node {
	intOnly := !a.AllowsFraction() || !b.AllowsFraction()
	var out []node
	lo, hi := a.Minimum, a.Maximum
	if intOnly {
		lo, hi = ceilp(lo), floorp(hi)
	}
	cands := numberCandidates(a, b)
	if b.Minimum != nil && (lo == nil || *lo < *b.Minimum) {
		v, ok := search(a, b, cands)
		out = append(out, c.reason(path, v, ok, "%s allows numbers below %v (%s); %s's minimum is %v", c.w, *b.Minimum, bound("minimum", lo), c.r, *b.Minimum))
	}
	if b.Maximum != nil && (hi == nil || *hi > *b.Maximum) {
		v, ok := search(a, b, cands)
		out = append(out, c.reason(path, v, ok, "%s allows numbers above %v (%s); %s's maximum is %v", c.w, *b.Maximum, bound("maximum", hi), c.r, *b.Maximum))
	}
	return out
}

func (c *checker) strings(a, b *schema.Schema, path string) []node {
	var out []node
	cands := stringCandidates(a, b)
	lo, hi := lengthRange(a)
	if b.MinLength != nil && lo < *b.MinLength {
		v, ok := search(a, b, cands)
		out = append(out, c.reason(path, v, ok, "%s allows strings shorter than %d characters; %s does not", c.w, *b.MinLength, c.r))
	}
	if b.MaxLength != nil && (hi < 0 || hi > *b.MaxLength) {
		v, ok := search(a, b, cands)
		out = append(out, c.reason(path, v, ok, "%s allows strings longer than %d characters; %s does not", c.w, *b.MaxLength, c.r))
	}
	if b.Format != "" && a.Format != b.Format {
		v, ok := search(a, b, cands)
		what := "any string"
		if a.Format != "" {
			what = a.Format + " strings"
		}
		out = append(out, c.reason(path, v, ok, "%s requires a %s; %s allows %s", c.r, b.Format, c.w, what))
	}
	return out
}

// lengthRange is the shortest and longest string the writer can produce,
// its format included (an email has at least 5 characters, a uuid exactly
// 36); -1 for no longest.
func lengthRange(a *schema.Schema) (int, int) {
	lo := 0
	if a.MinLength != nil {
		lo = *a.MinLength
	}
	for l := lo; l <= lo+64; l++ {
		if _, ok := formatOfLength(a.Format, l); ok {
			lo = l
			break
		}
	}
	hi := -1
	switch a.Format {
	case "uuid":
		hi = 36
	case "date":
		hi = 10
	}
	if a.MaxLength != nil && (hi < 0 || *a.MaxLength < hi) {
		hi = *a.MaxLength
		for hi > lo {
			if _, ok := formatOfLength(a.Format, hi); ok {
				break
			}
			hi--
		}
	}
	return lo, hi
}

func (c *checker) arrays(a, b *schema.Schema, path string) []node {
	var out []node
	if b.MinItems != nil && *b.MinItems > 0 && (a.MinItems == nil || *a.MinItems < *b.MinItems) {
		v, ok := search(a, b, arrayCandidates(a, b))
		out = append(out, c.reason(path, v, ok, "%s allows arrays with fewer than %d items; %s does not", c.w, *b.MinItems, c.r))
	}
	if b.MaxItems != nil && (a.MaxItems == nil || *a.MaxItems > *b.MaxItems) {
		v, ok := search(a, b, arrayCandidates(a, b))
		out = append(out, c.reason(path, v, ok, "%s allows arrays with more than %d items; %s does not", c.w, *b.MaxItems, c.r))
	}
	if b.Items != nil && (a.MaxItems == nil || *a.MaxItems > 0) {
		ai := a.Items
		if ai == nil {
			ai = &schema.Schema{AdditionalProperties: true}
		}
		n := 1
		if a.MinItems != nil && *a.MinItems > 1 {
			n = *a.MinItems
		}
		for _, child := range c.sub(ai, b.Items, path+"/*") {
			arr := make([]any, n)
			for i := range arr {
				arr[i] = schema.Clone(child.local)
			}
			child.local = arr
			out = append(out, child)
		}
	}
	return out
}

func (c *checker) objects(a, b *schema.Schema, path string) []node {
	var out []node
	base, baseOK := exampleObject(a)
	wrap := func(set func(o *schema.Object)) (any, bool) {
		if !baseOK {
			return nil, false
		}
		o := schema.Clone(base).(*schema.Object)
		set(o)
		return o, true
	}
	for _, r := range b.Required {
		if a.IsRequired(r) {
			continue
		}
		v, ok := wrap(func(o *schema.Object) { o.Delete(r) })
		out = append(out, c.reason(path+"/"+schema.EscapePointer(r), v, ok, "%s requires %q; %s does not, so its events may leave it out", c.r, r, c.w))
	}
	for _, p := range a.Properties {
		if !satisfiable(p.Schema) {
			continue // the writer can never send it
		}
		pp := path + "/" + schema.EscapePointer(p.Name)
		if bs, ok := b.Prop(p.Name); ok {
			for _, child := range c.sub(p.Schema, bs, pp) {
				local, cl := child.local, child.ok
				child.local, child.ok = wrap(func(o *schema.Object) { o.Set(p.Name, local) })
				child.ok = child.ok && cl
				out = append(out, child)
			}
			continue
		}
		if !b.AdditionalProperties {
			ex, exOK := example(p.Schema)
			v, ok := wrap(func(o *schema.Object) { o.Set(p.Name, ex) })
			out = append(out, c.reason(pp, v, ok && exOK, "%s may send %q; %s does not allow it (additionalProperties is false)", c.w, p.Name, c.r))
		}
	}
	if a.AdditionalProperties {
		for _, p := range b.Properties {
			if _, declared := a.Prop(p.Name); declared {
				continue
			}
			bad, found := firstInvalid(p.Schema)
			if !found && len(Subset(&schema.Schema{AdditionalProperties: true}, p.Schema, "", "")) == 0 {
				continue // constrains nothing in effect, e.g. {"minLength": 0}
			}
			v, ok := wrap(func(o *schema.Object) { o.Set(p.Name, bad) })
			out = append(out, c.reason(path+"/"+schema.EscapePointer(p.Name), v, ok && found,
				"%s allows %q with any value (additionalProperties); %s constrains it", c.w, p.Name, c.r))
		}
		if !b.AdditionalProperties {
			name := freshName(a, b)
			v, ok := wrap(func(o *schema.Object) { o.Set(name, true) })
			out = append(out, c.reason(path, v, ok, "%s allows properties it does not declare; %s does not (additionalProperties is false)", c.w, c.r))
		}
	}
	return out
}

func freshName(a, b *schema.Schema) string {
	for i := 0; ; i++ {
		name := "undeclared"
		if i > 0 {
			name += strconv.Itoa(i)
		}
		_, inA := a.Prop(name)
		_, inB := b.Prop(name)
		if !inA && !inB {
			return name
		}
	}
}

// search returns the first candidate valid under a and invalid under b.
func search(a, b *schema.Schema, cands []any) (any, bool) {
	for _, v := range cands {
		if schema.Valid(a, v) && !schema.Valid(b, v) {
			return v, true
		}
	}
	return nil, false
}

func num(f float64) json.Number { return json.Number(strconv.FormatFloat(f, 'f', -1, 64)) }

func ceilp(p *float64) *float64 {
	if p == nil {
		return nil
	}
	x := math.Ceil(*p)
	return &x
}

func floorp(p *float64) *float64 {
	if p == nil {
		return nil
	}
	x := math.Floor(*p)
	return &x
}

func bound(name string, p *float64) string {
	if p == nil {
		return "no " + name
	}
	return fmt.Sprintf("its %s is %v", name, *p)
}

func article(t string) string {
	switch t {
	case "null":
		return "null"
	case "integer", "object", "array":
		return "an " + t
	}
	return "a " + t
}

func typeList(s *schema.Schema) string {
	if s.Types == nil {
		return "any type"
	}
	return strings.Join(s.Types, ", ")
}
