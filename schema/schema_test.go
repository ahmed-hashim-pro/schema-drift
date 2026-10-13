package schema

import (
	"encoding/json"
	"strings"
	"testing"
)

func mustParse(t *testing.T, src string) *Schema {
	t.Helper()
	s, err := Parse([]byte(src))
	if err != nil {
		t.Fatalf("%s: %v", src, err)
	}
	return s
}

func value(t *testing.T, src string) any {
	t.Helper()
	v, err := Decode([]byte(src))
	if err != nil {
		t.Fatalf("%s: %v", src, err)
	}
	return v
}

func TestParserFailsClosed(t *testing.T) {
	for _, tc := range []struct{ src, want string }{
		{`[]`, "must be a JSON object"},
		{`{"type":"string","minimun":1}`, `unknown keyword "minimun" (did you mean "minimum"?)`},
		{`{"$ref":"#/defs/x"}`, `"$ref" is not supported`},
		{`{"oneOf":[{"type":"string"}]}`, `"oneOf" is not supported`},
		{`{"type":"string","pattern":"^a"}`, `"pattern" is not supported`},
		{`{"type":"string","nullable":true}`, `OpenAPI's nullable`},
		{`{"type":"text"}`, `"text" is not a JSON Schema type`},
		{`{"type":[]}`, "empty type list"},
		{`{"type":["string","string"]}`, "listed twice"},
		{`{"enum":[]}`, "non-empty list"},
		{`{"enum":[1,1.0]}`, "listed twice"},
		{`{"enum":["a",3],"type":"string"}`, "3 can never be valid"},
		{`{"required":["a"],"additionalProperties":false}`, "no object can satisfy"},
		{`{"minimum":5,"maximum":1}`, "greater than maximum"},
		{`{"type":"integer","minimum":0.2,"maximum":0.8}`, "no integer lies between"},
		{`{"minLength":-1}`, "non-negative integer"},
		{`{"minLength":1.5}`, "non-negative integer"},
		{`{"format":"ipv4"}`, "not a supported format"},
		{`{"type":"integer","format":"uuid"}`, "allows no strings"},
		{`{"additionalProperties":{"type":"string"}}`, "must be true or false"},
		{`{"items":[{"type":"string"}]}`, "tuple items"},
		{`{"type":"string","type":"integer"}`, `duplicate key "type"`},
		{`{"properties":{"a":{"type":"strin"}}}`, `/properties/a/type: "strin"`},
		{`{"type":"string"} {}`, "after the JSON value"},
		{`{"type":`, "unexpected end"},
	} {
		_, err := Parse([]byte(tc.src))
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: got %v, want %q", tc.src, err, tc.want)
		}
	}
}

func TestAcceptsTheSubset(t *testing.T) {
	s := mustParse(t, `{
		"$schema": "https://json-schema.org/draft/2020-12/schema", "$id": "x", "title": "t",
		"description": "d", "examples": [{}],
		"type": "object",
		"properties": {
			"id": {"type": "string", "format": "uuid", "minLength": 36, "maxLength": 36},
			"n": {"type": ["integer", "null"], "minimum": 0, "maximum": 10},
			"tags": {"type": "array", "items": {"enum": ["a", "b"]}, "minItems": 0, "maxItems": 3},
			"any": true
		},
		"required": ["id"],
		"additionalProperties": false
	}`)
	if len(s.Properties) != 4 || s.Properties[0].Name != "id" || s.AdditionalProperties {
		t.Fatalf("%+v", s)
	}
	if !s.Properties[3].Schema.IsAny() {
		t.Fatal("true should be the any-schema")
	}
}

func TestValidatePointsAtTheProblem(t *testing.T) {
	s := mustParse(t, `{"type":"object","properties":{
		"a/b": {"type":"integer"},
		"lines": {"type":"array","items":{"type":"object","properties":{"qty":{"type":"integer","minimum":1}},"required":["qty"]}}
	},"additionalProperties":false}`)
	got := Validate(s, value(t, `{"a/b": 1.5, "lines": [{"qty": 2}, {"qty": 0}, {}], "x": 1}`))
	want := []string{
		"/a~1b: expected integer, got number 1.5",
		"/lines/1/qty: 0 is less than the minimum 1",
		`/lines/2: required property "qty" is missing`,
		`/x: property "x" is not allowed`,
	}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for i := range want {
		if got[i].String() != want[i] {
			t.Errorf("got %q, want %q", got[i].String(), want[i])
		}
	}
}

func TestIntegersAreNumbersWithoutAFraction(t *testing.T) {
	s := mustParse(t, `{"type":"integer"}`)
	for _, ok := range []string{"1", "1.0", "-3", "1e2"} {
		if !Valid(s, value(t, ok)) {
			t.Errorf("%s should be an integer", ok)
		}
	}
	for _, bad := range []string{"1.5", `"1"`, "true", "null"} {
		if Valid(s, value(t, bad)) {
			t.Errorf("%s should not be an integer", bad)
		}
	}
	if !Valid(mustParse(t, `{"type":"number"}`), value(t, "7")) {
		t.Error("an integer is a number")
	}
}

func TestNullableViaTypeList(t *testing.T) {
	s := mustParse(t, `{"type":["string","null"],"minLength":2}`)
	if !Valid(s, nil) || !Valid(s, "ab") || Valid(s, "a") || Valid(s, value(t, "1")) {
		t.Fatal("nullable string")
	}
}

func TestFormats(t *testing.T) {
	for _, tc := range []struct {
		format, ok, bad string
	}{
		{"date-time", "2026-10-01T09:14:03Z", "2026-10-01 09:14:03"},
		{"date-time", "2026-10-01T09:14:03.123+02:00", "yesterday"},
		{"date", "2026-02-28", "2026-02-30"},
		{"uuid", "6f1c2a9e-3b7d-4c55-9a43-1d2e8f7b6a01", "6f1c2a9e3b7d4c559a431d2e8f7b6a01"},
		{"email", "a@example.com", "a@b"},
	} {
		if !FormatOK(tc.format, tc.ok) || FormatOK(tc.format, tc.bad) {
			t.Errorf("%s: %q / %q", tc.format, tc.ok, tc.bad)
		}
		if !FormatOK(tc.format, FormatExample(tc.format)) {
			t.Errorf("%s: its own example is invalid", tc.format)
		}
	}
}

func TestEnumComparesByValue(t *testing.T) {
	s := mustParse(t, `{"enum":[1, {"a":[true]}]}`)
	if !Valid(s, value(t, "1.0")) || !Valid(s, value(t, `{"a":[true]}`)) || Valid(s, value(t, `{"a":[false]}`)) {
		t.Fatal("enum equality")
	}
}

func TestMarshalRoundTrips(t *testing.T) {
	src := `{"title":"t","type":["string","null"],"format":"date","minLength":10,"properties":{"b":{"type":"integer","minimum":-2.5}},"required":["b"],"additionalProperties":false,"items":{"enum":[1,"x"]}}`
	s := mustParse(t, src)
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	again := mustParse(t, string(b))
	b2, _ := json.Marshal(again)
	if string(b) != string(b2) {
		t.Fatalf("%s\n%s", b, b2)
	}
}
