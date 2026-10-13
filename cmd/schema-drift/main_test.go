package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ahmed-hashim-pro/schema-drift/schema"
)

const ex = "../../examples/"

func call(args ...string) (int, string, string) {
	var out, errOut bytes.Buffer
	code := run(args, &out, &errOut)
	return code, out.String(), errOut.String()
}

func TestCheck(t *testing.T) {
	code, out, _ := call("check", ex+"contracts/orders/v1.json", ex+"contracts/orders/v2.json")
	if code != 0 || !strings.HasPrefix(out, "v1 -> v2: BACKWARD\n") || !strings.Contains(out, "mode BACKWARD passes") {
		t.Fatalf("exit %d\n%s", code, out)
	}
	// Closed schema plus a new field: old consumers reject it.
	if code, out, _ := call("check", "--mode", "FORWARD", ex+"contracts/orders/v1.json", ex+"contracts/orders/v2.json"); code != 1 ||
		!strings.Contains(out, `BREAKING /customer_id: v2 may send "customer_id"; v1 does not allow it`) {
		t.Fatalf("exit %d\n%s", code, out)
	}
	code, out, _ = call("check", ex+"contracts/orders/v2.json", ex+"contracts/orders/v3.json", "--mode", "none")
	if code != 0 || !strings.Contains(out, "NONE (breaking both ways)") || !strings.Contains(out, "e.g. {") {
		t.Fatalf("exit %d\n%s", code, out)
	}
}

func TestVerify(t *testing.T) {
	code, out, _ := call("verify", ex+"contracts/orders/v2.json", ex+"events/orders-v2.ndjson")
	if code != 0 || !strings.Contains(out, "3 events, 3 valid, 0 invalid") {
		t.Fatalf("exit %d\n%s", code, out)
	}
	code, out, _ = call("verify", ex+"contracts/orders/v2.json", ex+"events/orders-v3.ndjson")
	if code != 1 || !strings.Contains(out, `line 1: /amount: expected integer, got string "12.50"`) {
		t.Fatalf("exit %d\n%s", code, out)
	}
	code, out, _ = call("verify", "--max", "1", ex+"contracts/orders/v2.json", ex+"events/orders-mixed.ndjson")
	if code != 1 || !strings.Contains(out, "... and 3 more invalid events") {
		t.Fatalf("exit %d\n%s", code, out)
	}
}

func TestInferDraftAcceptsItsSample(t *testing.T) {
	code, out, _ := call("infer", ex+"events/orders-v2.ndjson")
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	s, err := schema.Parse([]byte(out))
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(ex + "events/orders-v2.ndjson")
	for _, line := range bytes.Split(bytes.TrimSpace(data), []byte("\n")) {
		v, _ := schema.Decode(line)
		if !schema.Valid(s, v) {
			t.Fatalf("draft rejects %s", line)
		}
	}
}

func TestHistory(t *testing.T) {
	code, out, _ := call("history", ex+"contracts")
	if code != 1 || !strings.Contains(out, "3 problem(s)") || !strings.Contains(out, `v2 allows "refunded"; v1 does not`) {
		t.Fatalf("exit %d\n%s", code, out)
	}
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "orders"), 0o755)
	for _, v := range []string{"v1.json", "v2.json"} {
		b, _ := os.ReadFile(ex + "contracts/orders/" + v)
		os.WriteFile(filepath.Join(dir, "orders", v), b, 0o644)
	}
	if code, out, _ := call("history", dir); code != 0 || !strings.Contains(out, "no problems (2 version(s) checked)") {
		t.Fatalf("exit %d\n%s", code, out)
	}
}

func TestUsageErrors(t *testing.T) {
	for _, args := range [][]string{
		{},
		{"frobnicate"},
		{"check", ex + "contracts/orders/v1.json"},
		{"check", "--mode", "SIDEWAYS", ex + "contracts/orders/v1.json", ex + "contracts/orders/v2.json"},
		{"check", "--mode", "FULL_TRANSITIVE", ex + "contracts/orders/v1.json", ex + "contracts/orders/v2.json"},
		{"check", "missing.json", ex + "contracts/orders/v2.json"},
		{"verify", ex + "contracts/orders/v1.json"},
		{"infer", "missing.ndjson"},
		{"history", "no-such-dir"},
		{"gate", "extra"},
	} {
		if code, _, _ := call(args...); code != 2 {
			t.Errorf("%v: exit %d, want 2", args, code)
		}
	}
}

// The README quotes the CLI's output from docs/*.txt (written by
// scripts/docs.sh). Both must stay current: CI regenerates docs/ and fails on
// a diff, and this fails if the README no longer quotes what is there.
func TestReadmeQuotesCurrentOutput(t *testing.T) {
	readme, err := os.ReadFile("../../README.md")
	if err != nil {
		t.Fatal(err)
	}
	files, _ := filepath.Glob("../../docs/*.txt")
	if len(files) == 0 {
		t.Fatal("no docs/*.txt")
	}
	for _, f := range files {
		b, _ := os.ReadFile(f)
		if !bytes.Contains(readme, bytes.TrimRight(b, "\n")) {
			t.Errorf("README does not quote %s as it is now", filepath.Base(f))
		}
	}
}
