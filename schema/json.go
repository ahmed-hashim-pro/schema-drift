package schema

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"
)

// Object is a JSON object that remembers key order and refuses duplicate
// keys. encoding/json silently keeps the last of two equal keys; for a
// contract, that is a typo nobody sees, so here it is an error.
type Object struct {
	Keys   []string
	Values map[string]any
}

func (o *Object) Get(k string) (any, bool) {
	v, ok := o.Values[k]
	return v, ok
}

// MarshalJSON writes the object in its original key order.
func (o *Object) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, k := range o.Keys {
		if i > 0 {
			b.WriteByte(',')
		}
		kb, _ := json.Marshal(k)
		b.Write(kb)
		b.WriteByte(':')
		vb, err := json.Marshal(o.Values[k])
		if err != nil {
			return nil, err
		}
		b.Write(vb)
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

// Decode parses one JSON document. Objects become *Object, numbers
// json.Number, so integers and decimals stay distinguishable.
func Decode(data []byte) (any, error) {
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	v, err := decodeValue(d, "")
	if err != nil {
		return nil, err
	}
	if _, err := d.Token(); err != io.EOF {
		return nil, errors.New("unexpected data after the JSON value")
	}
	return v, nil
}

func decodeValue(d *json.Decoder, ptr string) (any, error) {
	tok, err := d.Token()
	if err != nil {
		if err == io.EOF {
			return nil, errors.New("unexpected end of JSON")
		}
		return nil, fmt.Errorf("invalid JSON: %v", err)
	}
	switch t := tok.(type) {
	case json.Delim:
		switch t {
		case '{':
			o := &Object{Values: map[string]any{}}
			for d.More() {
				kt, err := d.Token()
				if err != nil {
					return nil, fmt.Errorf("invalid JSON: %v", err)
				}
				k := kt.(string)
				if _, dup := o.Values[k]; dup {
					return nil, fmt.Errorf("%s: duplicate key %q", orRoot(ptr), k)
				}
				v, err := decodeValue(d, ptr+"/"+EscapePointer(k))
				if err != nil {
					return nil, err
				}
				o.Keys = append(o.Keys, k)
				o.Values[k] = v
			}
			if _, err := d.Token(); err != nil {
				return nil, fmt.Errorf("invalid JSON: %v", err)
			}
			return o, nil
		case '[':
			arr := []any{}
			for i := 0; d.More(); i++ {
				v, err := decodeValue(d, fmt.Sprintf("%s/%d", ptr, i))
				if err != nil {
					return nil, err
				}
				arr = append(arr, v)
			}
			if _, err := d.Token(); err != nil {
				return nil, fmt.Errorf("invalid JSON: %v", err)
			}
			return arr, nil
		}
	}
	return tok, nil // string, json.Number, bool, nil
}

func orRoot(ptr string) string {
	if ptr == "" {
		return "/"
	}
	return ptr
}

// EscapePointer escapes one JSON Pointer token (RFC 6901).
func EscapePointer(s string) string {
	return strings.NewReplacer("~", "~0", "/", "~1").Replace(s)
}

// TypeOf names a decoded value's JSON type. Numbers without a fractional
// part are "integer", as JSON Schema counts 1.0 as an integer.
func TypeOf(v any) string {
	switch x := v.(type) {
	case nil:
		return "null"
	case bool:
		return "boolean"
	case json.Number:
		if isInteger(x) {
			return "integer"
		}
		return "number"
	case float64:
		if x == math.Trunc(x) && !math.IsInf(x, 0) {
			return "integer"
		}
		return "number"
	case int:
		return "integer"
	case string:
		return "string"
	case []any:
		return "array"
	case *Object, map[string]any:
		return "object"
	}
	return "unknown"
}

func isInteger(n json.Number) bool {
	if _, err := strconv.ParseInt(string(n), 10, 64); err == nil {
		return true
	}
	f, err := n.Float64()
	return err == nil && f == math.Trunc(f) && !math.IsInf(f, 0)
}

// Num returns a numeric value as float64.
func Num(v any) (float64, bool) {
	switch x := v.(type) {
	case json.Number:
		f, err := x.Float64()
		return f, err == nil
	case float64:
		return x, true
	case int:
		return float64(x), true
	}
	return 0, false
}

// Equal compares two decoded JSON values; numbers compare by value and
// object key order does not matter.
func Equal(a, b any) bool {
	if fa, ok := Num(a); ok {
		fb, ok := Num(b)
		return ok && fa == fb
	}
	switch x := a.(type) {
	case nil:
		return b == nil
	case bool:
		y, ok := b.(bool)
		return ok && x == y
	case string:
		y, ok := b.(string)
		return ok && x == y
	case []any:
		y, ok := b.([]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for i := range x {
			if !Equal(x[i], y[i]) {
				return false
			}
		}
		return true
	case *Object:
		y, ok := b.(*Object)
		if !ok || len(x.Keys) != len(y.Keys) {
			return false
		}
		for _, k := range x.Keys {
			yv, ok := y.Values[k]
			if !ok || !Equal(x.Values[k], yv) {
				return false
			}
		}
		return true
	}
	return false
}

// Compact renders a value as one line of JSON, object keys in their order.
func Compact(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprint(v)
	}
	return string(b)
}

// NewObject builds an Object from keys and values given in order.
func NewObject(kv ...any) *Object {
	o := &Object{Values: map[string]any{}}
	for i := 0; i+1 < len(kv); i += 2 {
		o.Set(kv[i].(string), kv[i+1])
	}
	return o
}

// Set adds or replaces a key, keeping the first position of an existing key.
func (o *Object) Set(k string, v any) {
	if _, ok := o.Values[k]; !ok {
		o.Keys = append(o.Keys, k)
	}
	o.Values[k] = v
}

// Delete removes a key.
func (o *Object) Delete(k string) {
	if _, ok := o.Values[k]; !ok {
		return
	}
	delete(o.Values, k)
	for i, key := range o.Keys {
		if key == k {
			o.Keys = append(o.Keys[:i:i], o.Keys[i+1:]...)
			break
		}
	}
}

// Clone deep-copies a decoded value.
func Clone(v any) any {
	switch x := v.(type) {
	case []any:
		out := make([]any, len(x))
		for i := range x {
			out[i] = Clone(x[i])
		}
		return out
	case *Object:
		o := &Object{Keys: append([]string(nil), x.Keys...), Values: map[string]any{}}
		for k, val := range x.Values {
			o.Values[k] = Clone(val)
		}
		return o
	}
	return v
}

func sortedKeys(m map[string]bool) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
