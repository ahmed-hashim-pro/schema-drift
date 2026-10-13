package compat

import (
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"testing"

	"github.com/ahmed-hashim-pro/schema-drift/schema"
)

// The property tests use the validator as the oracle for the checker.
//
// Soundness: when Subset finds no reason, no event valid under the writer
// may be invalid under the reader. Thousands of random schema pairs, and
// hundreds of random writer-valid events for each, try to find one.
//
// Evidence: when Subset finds reasons, every one must carry a witness the
// validator confirms (valid under the writer, invalid under the reader). So
// no "breaking" verdict rests on the checker's word alone.

var names = []string{"a", "b", "c", "d"}

func ptrF(f float64) *float64 { return &f }
func ptrI(i int) *int         { return &i }

func randSchema(r *rand.Rand, depth int) *schema.Schema {
	s := &schema.Schema{AdditionalProperties: r.IntN(2) == 0}
	if r.IntN(7) > 0 {
		pool := []string{"null", "boolean", "integer", "number", "string", "object", "array"}
		if depth <= 0 {
			pool = pool[:5]
		}
		r.Shuffle(len(pool), func(i, j int) { pool[i], pool[j] = pool[j], pool[i] })
		s.Types = append([]string(nil), pool[:1+r.IntN(2)]...)
	}
	if r.IntN(3) == 0 {
		lo := float64(r.IntN(9) - 4)
		if r.IntN(3) == 0 {
			lo += 0.5
		}
		s.Minimum = &lo
	}
	if r.IntN(3) == 0 {
		hi := float64(r.IntN(9) - 2)
		if s.Minimum != nil && hi < *s.Minimum {
			hi = *s.Minimum + float64(r.IntN(3))
		}
		s.Maximum = &hi
	}
	if r.IntN(3) == 0 {
		s.MinLength = ptrI(r.IntN(3))
	}
	if r.IntN(3) == 0 {
		n := r.IntN(4)
		if s.MinLength != nil && n < *s.MinLength {
			n = *s.MinLength
		}
		s.MaxLength = &n
	}
	if (s.Types == nil || s.Allows("string")) && r.IntN(5) == 0 {
		s.Format = schema.Formats[r.IntN(len(schema.Formats))]
	}
	if depth > 0 && (s.Types == nil || s.Allows("object")) {
		for _, n := range names {
			if r.IntN(2) == 0 {
				s.Properties = append(s.Properties, schema.Property{Name: n, Schema: randSchema(r, depth-1)})
			}
		}
		for _, n := range names {
			_, declared := s.Prop(n)
			if r.IntN(4) == 0 && (declared || s.AdditionalProperties) {
				s.Required = append(s.Required, n)
			}
		}
	}
	if depth > 0 && (s.Types == nil || s.Allows("array")) {
		if r.IntN(2) == 0 {
			s.Items = randSchema(r, depth-1)
		}
		if r.IntN(3) == 0 {
			s.MinItems = ptrI(r.IntN(2))
		}
		if r.IntN(3) == 0 {
			s.MaxItems = ptrI(r.IntN(3) + 1)
		}
	}
	if r.IntN(8) == 0 {
		// An enum of values the rest of the schema accepts.
		var vals []any
		for i := 0; i < 8 && len(vals) < 3; i++ {
			if v, ok := randValue(r, s, 2); ok && !containsValue(vals, v) {
				vals = append(vals, v)
			}
		}
		if len(vals) > 0 {
			s.Enum = vals
		}
	}
	return s
}

func containsValue(vals []any, v any) bool {
	for _, x := range vals {
		if schema.Equal(x, v) {
			return true
		}
	}
	return false
}

// roundTrip passes a schema through JSON and the strict parser, so only
// schemas the tool would accept are tested.
func roundTrip(s *schema.Schema) (*schema.Schema, error) {
	b, err := json.Marshal(s)
	if err != nil {
		return nil, err
	}
	return schema.Parse(b)
}

// mutate changes one thing somewhere in a copy of s, the way a producer
// team edits a contract.
func mutate(r *rand.Rand, s *schema.Schema) *schema.Schema {
	c, err := roundTrip(s)
	if err != nil {
		panic(err)
	}
	node := c
	for i := 0; i < 3; i++ {
		var kids []*schema.Schema
		for _, p := range node.Properties {
			kids = append(kids, p.Schema)
		}
		if node.Items != nil {
			kids = append(kids, node.Items)
		}
		if len(kids) == 0 || r.IntN(2) == 0 {
			break
		}
		node = kids[r.IntN(len(kids))]
	}
	switch r.IntN(12) {
	case 0:
		fresh := randSchema(r, 0)
		node.Types = fresh.Types
	case 1:
		node.Minimum = nil
		if r.IntN(2) == 0 {
			node.Minimum = ptrF(float64(r.IntN(9) - 4))
		}
	case 2:
		node.Maximum = nil
		if r.IntN(2) == 0 {
			node.Maximum = ptrF(float64(r.IntN(9) - 2))
		}
	case 3:
		node.AdditionalProperties = !node.AdditionalProperties
	case 4:
		n := names[r.IntN(len(names))]
		if node.IsRequired(n) {
			var keep []string
			for _, x := range node.Required {
				if x != n {
					keep = append(keep, x)
				}
			}
			node.Required = keep
		} else {
			node.Required = append(node.Required, n)
		}
	case 5:
		n := names[r.IntN(len(names))]
		if _, ok := node.Prop(n); ok {
			var keep []schema.Property
			for _, p := range node.Properties {
				if p.Name != n {
					keep = append(keep, p)
				}
			}
			node.Properties = keep
		} else {
			node.Properties = append(node.Properties, schema.Property{Name: n, Schema: randSchema(r, 0)})
		}
	case 6:
		node.Format = ""
		if r.IntN(2) == 0 {
			node.Format = schema.Formats[r.IntN(len(schema.Formats))]
		}
	case 7:
		node.MinLength, node.MaxLength = nil, nil
		if r.IntN(2) == 0 {
			node.MaxLength = ptrI(r.IntN(4))
		}
	case 8:
		if node.Enum != nil && len(node.Enum) > 1 {
			node.Enum = node.Enum[:len(node.Enum)-1]
		} else {
			node.Enum = nil
		}
	case 9:
		if v, ok := randValue(r, node, 2); ok && node.Enum != nil && !containsValue(node.Enum, v) {
			node.Enum = append(node.Enum, v)
		}
	case 10:
		node.MinItems, node.MaxItems = nil, nil
		if r.IntN(2) == 0 {
			node.MaxItems = ptrI(r.IntN(3))
		}
	default:
		node.Items = randSchema(r, 0)
	}
	return c
}

// randValue tries to build a value valid under s.
func randValue(r *rand.Rand, s *schema.Schema, depth int) (any, bool) {
	for try := 0; try < 20; try++ {
		var v any
		if s.Enum != nil && r.IntN(4) > 0 {
			v = s.Enum[r.IntN(len(s.Enum))]
		} else {
			v = randOfType(r, s, depth)
		}
		if schema.Valid(s, v) {
			return v, true
		}
	}
	return nil, false
}

func randOfType(r *rand.Rand, s *schema.Schema, depth int) any {
	types := s.Types
	if types == nil {
		types = schema.AllTypes
	}
	t := types[r.IntN(len(types))]
	switch t {
	case "null":
		return nil
	case "boolean":
		return r.IntN(2) == 0
	case "integer", "number":
		lo, hi := -6.0, 6.0
		if s.Minimum != nil {
			lo = *s.Minimum
		}
		if s.Maximum != nil {
			hi = *s.Maximum
		}
		if r.IntN(6) == 0 { // stray far out
			lo, hi = lo-1e6, hi+1e6
		}
		f := lo + r.Float64()*(hi-lo)
		if t == "integer" || r.IntN(2) == 0 {
			f = float64(int(f))
			if r.IntN(3) == 0 && s.Minimum != nil {
				f = *s.Minimum
			}
		} else {
			f = float64(int(f*2)) / 2
		}
		return num(f)
	case "string":
		if s.Format != "" && r.IntN(5) > 0 {
			return formatVariants(s.Format, 1+r.IntN(5))[r.IntN(1)]
		}
		if r.IntN(5) == 0 {
			return schema.FormatExample(schema.Formats[r.IntN(4)])
		}
		n := r.IntN(6)
		if s.MinLength != nil && n < *s.MinLength {
			n = *s.MinLength
		}
		if s.MaxLength != nil && n > *s.MaxLength {
			n = *s.MaxLength
		}
		return variant(r.IntN(50), n)
	case "array":
		n := r.IntN(4)
		arr := []any{}
		for i := 0; i < n; i++ {
			if s.Items != nil && depth > 0 {
				v, _ := randValue(r, s.Items, depth-1)
				arr = append(arr, v)
			} else {
				arr = append(arr, randAny(r, depth-1))
			}
		}
		return arr
	default:
		o := schema.NewObject()
		for _, n := range append(append([]string(nil), names...), "e", "undeclared") {
			ps, declared := s.Prop(n)
			if !s.IsRequired(n) && r.IntN(2) == 0 {
				continue
			}
			if declared && depth > 0 {
				v, _ := randValue(r, ps, depth-1)
				o.Set(n, v)
			} else if declared {
				o.Set(n, randOfType(r, ps, 0))
			} else {
				o.Set(n, randAny(r, depth-1))
			}
		}
		return o
	}
}

func randAny(r *rand.Rand, depth int) any {
	return randOfType(r, &schema.Schema{AdditionalProperties: true, Types: func() []string {
		if depth <= 0 {
			return []string{"null", "boolean", "integer", "number", "string"}
		}
		return nil
	}()}, depth)
}

func describe(s *schema.Schema) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func pairs(seed uint64, n int, f func(a, b *schema.Schema)) (tested int) {
	r := rand.New(rand.NewPCG(seed, 99))
	for tested < n {
		a, err := roundTrip(randSchema(r, 2))
		if err != nil {
			continue
		}
		var b *schema.Schema
		if r.IntN(4) == 0 {
			b, err = roundTrip(randSchema(r, 2))
		} else {
			b, err = roundTrip(mutate(r, a))
		}
		if err != nil {
			continue
		}
		f(a, b)
		tested++
	}
	return tested
}

func TestSoundness(t *testing.T) {
	n := 20000
	if testing.Short() {
		n = 3000
	}
	r := rand.New(rand.NewPCG(7, 7))
	var compatible, events, failures int
	pairs(1, n, func(a, b *schema.Schema) {
		if len(Subset(a, b, "writer", "reader")) > 0 {
			return
		}
		compatible++
		for i := 0; i < 200; i++ {
			v, ok := randValue(r, a, 3)
			if !ok {
				continue
			}
			events++
			if errs := schema.Validate(b, v); len(errs) > 0 {
				failures++
				if failures <= 5 {
					t.Errorf("called compatible, but %s is valid under the writer and the reader says %v\nwriter %s\nreader %s",
						schema.Compact(v), errs, describe(a), describe(b))
				}
				return
			}
		}
	})
	t.Logf("%d pairs judged compatible, %d writer-valid events checked against the reader, %d counterexamples", compatible, events, failures)
	if compatible < n/20 {
		t.Fatalf("only %d compatible pairs: the generator is not exercising the 'yes' side", compatible)
	}
}

func TestEveryBreakIsProven(t *testing.T) {
	n := 20000
	if testing.Short() {
		n = 3000
	}
	var breaking, reasons, unproven int
	pairs(2, n, func(a, b *schema.Schema) {
		rs := Subset(a, b, "writer", "reader")
		if len(rs) == 0 {
			return
		}
		breaking++
		for _, x := range rs {
			reasons++
			if !x.Proven {
				unproven++
				if unproven <= 5 {
					t.Errorf("unproven reason at %s: %s\nwriter %s\nreader %s", x.Path, x.Message, describe(a), describe(b))
				}
			}
		}
	})
	t.Logf("%d pairs judged breaking, %d reasons, %d without a validated witness", breaking, reasons, unproven)
}

func TestSchemaIsCompatibleWithItself(t *testing.T) {
	pairs(3, 3000, func(a, _ *schema.Schema) {
		if rs := Subset(a, a, "a", "a"); len(rs) > 0 {
			t.Fatalf("%s is not compatible with itself: %s %s", describe(a), rs[0].Path, rs[0].Message)
		}
	})
}

var _ = fmt.Sprint
