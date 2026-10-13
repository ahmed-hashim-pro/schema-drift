package infer

import (
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"testing"

	"github.com/ahmed-hashim-pro/schema-drift/schema"
)

func decode(t *testing.T, lines ...string) []any {
	var out []any
	for _, l := range lines {
		v, err := schema.Decode([]byte(l))
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, v)
	}
	return out
}

func TestInfersRequiredFormatsAndTypes(t *testing.T) {
	s := Infer(decode(t,
		`{"id":"6f1c2a9e-3b7d-4c55-9a43-1d2e8f7b6a01","n":1,"at":"2026-10-01T09:14:03Z","tags":["a"]}`,
		`{"id":"0b9e7d4c-58a2-4f13-b6e1-7c2d9a3f5e12","n":2.5,"at":"2026-10-01T09:15:41Z","note":null}`,
	))
	id, _ := s.Prop("id")
	n, _ := s.Prop("n")
	at, _ := s.Prop("at")
	if id.Format != "uuid" || at.Format != "date-time" || len(n.Types) != 1 || n.Types[0] != "number" {
		t.Fatalf("%+v %+v %+v", id, n, at)
	}
	if !s.IsRequired("id") || s.IsRequired("tags") || s.IsRequired("note") || s.AdditionalProperties {
		t.Fatalf("required %v, additional %v", s.Required, s.AdditionalProperties)
	}
}

// Whatever the sample, the draft accepts all of it.
func TestDraftAcceptsItsSample(t *testing.T) {
	r := rand.New(rand.NewPCG(4, 4))
	var gen func(depth int) any
	gen = func(depth int) any {
		switch k := r.IntN(8); {
		case k == 0:
			return nil
		case k == 1:
			return r.IntN(2) == 0
		case k == 2:
			v, _ := schema.Decode([]byte(fmt.Sprint(r.IntN(100) - 50)))
			return v
		case k == 3:
			v, _ := schema.Decode([]byte("2.5"))
			return v
		case k == 4:
			return []string{"x", "2026-01-01", "a@b.co", "6f1c2a9e-3b7d-4c55-9a43-1d2e8f7b6a01"}[r.IntN(4)]
		case k == 5 && depth > 0:
			arr := []any{}
			for i := r.IntN(3); i > 0; i-- {
				arr = append(arr, gen(depth-1))
			}
			return arr
		case depth > 0:
			o := schema.NewObject()
			for _, key := range []string{"a", "b", "c"} {
				if r.IntN(2) == 0 {
					o.Set(key, gen(depth-1))
				}
			}
			return o
		}
		return "leaf"
	}
	for trial := 0; trial < 2000; trial++ {
		var events []any
		for i := 1 + r.IntN(6); i > 0; i-- {
			events = append(events, gen(3))
		}
		s := Infer(events)
		b, _ := json.Marshal(s)
		if _, err := schema.Parse(b); err != nil {
			t.Fatalf("trial %d: the draft does not pass the strict parser: %v\n%s", trial, err, b)
		}
		for _, e := range events {
			if errs := schema.Validate(s, e); len(errs) > 0 {
				t.Fatalf("trial %d: draft rejects its own sample %s: %v", trial, schema.Compact(e), errs)
			}
		}
	}
}
