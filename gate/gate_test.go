package gate

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const v1 = `{"type":"object","properties":{"id":{"type":"string"},"n":{"type":"integer"}},"required":["id"],"additionalProperties":false}`

// repo makes a git repository with the given files committed on main, then
// changes to it for the duration of the test (git runs in the current
// directory, as in CI).
func repo(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@example.com"}, args...)...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q", "-b", "main")
	write(t, dir, files)
	run("add", "-A")
	run("commit", "-q", "-m", "base")
	wd, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(wd) })
	return dir
}

func write(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, body := range files {
		p := filepath.Join(dir, name)
		os.MkdirAll(filepath.Dir(p), 0o755)
		if body == "" {
			os.Remove(p)
			continue
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func gateAt(t *testing.T) Result {
	t.Helper()
	base, err := Load(Git{Ref: "main"}, "contracts")
	if err != nil {
		t.Fatal(err)
	}
	head, err := Load(Disk{}, "contracts")
	if err != nil {
		t.Fatal(err)
	}
	return Gate(base, head)
}

func kinds(r Result) string {
	var out []string
	for _, p := range r.Problems {
		out = append(out, string(p.Kind)+" "+p.Topic+" "+p.Version)
	}
	return strings.Join(out, "; ")
}

func TestCompatibleAdditionPasses(t *testing.T) {
	dir := repo(t, map[string]string{"contracts/orders/v1.json": v1})
	write(t, dir, map[string]string{"contracts/orders/v2.json": strings.Replace(v1, `"n":{"type":"integer"}`, `"n":{"type":"integer"},"note":{"type":"string"}`, 1)})
	r := gateAt(t)
	if len(r.Problems) != 0 || len(r.Checked) != 1 {
		t.Fatalf("problems: %s, checked %v", kinds(r), r.Checked)
	}
}

func TestBreakingVersionIsBlocked(t *testing.T) {
	dir := repo(t, map[string]string{"contracts/orders/v1.json": v1})
	write(t, dir, map[string]string{"contracts/orders/v2.json": strings.Replace(v1, `"n":{"type":"integer"}`, `"n":{"type":"string"}`, 1)})
	r := gateAt(t)
	if kinds(r) != "incompatible orders v2" {
		t.Fatalf("got %q", kinds(r))
	}
	if f := r.Problems[0].Findings; len(f) != 1 || !f[0].Reasons[0].Proven {
		t.Fatalf("findings %+v", f)
	}
}

func TestPublishedVersionsAreImmutable(t *testing.T) {
	dir := repo(t, map[string]string{"contracts/orders/v1.json": v1, "contracts/orders/v2.json": v1})
	write(t, dir, map[string]string{
		"contracts/orders/v1.json": strings.Replace(v1, `"type":"integer"`, `"type":"number"`, 1),
		"contracts/orders/v2.json": "", // deleted
	})
	if got := kinds(gateAt(t)); got != "edited after publication orders v1; deleted after publication orders v2" {
		t.Fatalf("got %q", got)
	}
}

func TestEditsThatChangeNothingArePermitted(t *testing.T) {
	dir := repo(t, map[string]string{"contracts/orders/v1.json": v1})
	write(t, dir, map[string]string{"contracts/orders/v1.json": strings.Replace(v1, `{"type":"object",`, `{"description":"reworded","type":"object",`, 1)})
	if r := gateAt(t); len(r.Problems) != 0 {
		t.Fatalf("a description edit was blocked: %s", kinds(r))
	}
}

func TestModeIsHonouredAndChangesNoted(t *testing.T) {
	dir := repo(t, map[string]string{"contracts/orders/v1.json": v1})
	// Adding an optional field to a closed schema: backward compatible,
	// not forward (old consumers reject the new field).
	v2 := strings.Replace(v1, `"n":{"type":"integer"}`, `"n":{"type":"integer"},"note":{"type":"string"}`, 1)
	write(t, dir, map[string]string{"contracts/orders/v2.json": v2, "contracts/orders/mode": "FORWARD\n"})
	r := gateAt(t)
	if kinds(r) != "incompatible orders v2" || len(r.Notes) != 1 || !strings.Contains(r.Notes[0], "BACKWARD to FORWARD") {
		t.Fatalf("got %q, notes %v", kinds(r), r.Notes)
	}
}

func TestTransitiveChecksEveryEarlierVersion(t *testing.T) {
	a := `{"type":"object","properties":{"x":{"type":"integer"}},"additionalProperties":false}`
	b := `{"type":"object","properties":{"x":{"type":["integer","string"]}},"additionalProperties":false}`
	c := `{"type":"object","properties":{"x":{"type":"string"}},"additionalProperties":false}`
	// FORWARD: data written with v3 (strings) must be readable on v2 (it
	// is) and, transitively, on v1 (it is not).
	for _, tc := range []struct {
		mode string
		want string
	}{{"FORWARD", ""}, {"FORWARD_TRANSITIVE", "incompatible t v3"}} {
		dir := repo(t, map[string]string{"contracts/t/v1.json": a, "contracts/t/v2.json": b, "contracts/t/mode": tc.mode})
		write(t, dir, map[string]string{"contracts/t/v3.json": c})
		if got := kinds(gateAt(t)); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.mode, got, tc.want)
		}
	}
}

func TestNewTopicHistoryIsChecked(t *testing.T) {
	dir := repo(t, map[string]string{"README": "x"})
	write(t, dir, map[string]string{
		"contracts/new/v1.json": v1,
		"contracts/new/v2.json": strings.Replace(v1, `"required":["id"]`, `"required":["id","n"]`, 1),
	})
	if got := kinds(gateAt(t)); got != "incompatible new v2" {
		t.Fatalf("got %q", got)
	}
}

func TestDeletedTopic(t *testing.T) {
	dir := repo(t, map[string]string{"contracts/orders/v1.json": v1, "contracts/other/v1.json": v1})
	write(t, dir, map[string]string{"contracts/orders/v1.json": ""})
	os.Remove(filepath.Join(dir, "contracts/orders"))
	if got := kinds(gateAt(t)); got != "deleted after publication orders " {
		t.Fatalf("got %q", got)
	}
}

func TestLayoutErrors(t *testing.T) {
	for _, tc := range []struct {
		files map[string]string
		want  string
	}{
		{map[string]string{"contracts/t/v1.json": v1, "contracts/t/v3.json": v1}, "v2 is missing"},
		{map[string]string{"contracts/t/v01.json": v1}, "not a version file"},
		{map[string]string{"contracts/t/notes.md": "x"}, "not a version file"},
		{map[string]string{"contracts/t/sub/v1.json": v1}, "expected <topic>/v<N>.json"},
		{map[string]string{"contracts/t/v1.json": v1, "contracts/t/mode": "SIDEWAYS"}, "unknown compatibility mode"},
		{map[string]string{"contracts/t/v1.json": `{"type":"strng"}`}, `contracts/t/v1.json (working tree): /type`},
	} {
		dir := t.TempDir()
		write(t, dir, tc.files)
		_, err := Load(Disk{}, filepath.Join(dir, "contracts"))
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%v: got %v, want %q", tc.files, err, tc.want)
		}
	}
}

func TestBadBaseRef(t *testing.T) {
	repo(t, map[string]string{"contracts/t/v1.json": v1})
	if _, err := Load(Git{Ref: "no-such-branch"}, "contracts"); err == nil || !strings.Contains(err.Error(), "not a commit") {
		t.Fatalf("got %v", err)
	}
}
