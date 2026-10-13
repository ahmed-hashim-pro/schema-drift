// Package gate checks a contracts directory: one subdirectory per topic,
// holding v1.json, v2.json, ... and optionally a "mode" file naming the
// topic's compatibility mode (BACKWARD if absent).
//
// History checks every version against the ones before it. Gate compares
// the directory with the same directory at a base git ref, the way a CI job
// sees a pull request: published versions must not change or disappear,
// and every new version must be compatible under the topic's mode.
package gate

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/ahmed-hashim-pro/schema-drift/compat"
	"github.com/ahmed-hashim-pro/schema-drift/schema"
)

type Topic struct {
	Name     string
	Mode     compat.Mode
	Versions []compat.Version
	// Raw file contents by version name, for change detection.
	Raw map[string][]byte
}

// Source lists and reads files under a directory, from disk or from git.
type Source interface {
	// Files returns paths relative to dir, slash-separated.
	Files(dir string) ([]string, error)
	Read(dir, rel string) ([]byte, error)
	Describe() string
}

type Disk struct{}

func (Disk) Describe() string { return "working tree" }

func (Disk) Files(dir string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(dir, p)
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	return out, err
}

func (Disk) Read(dir, rel string) ([]byte, error) {
	return os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
}

// Git reads the tree at a ref, from the repository containing the current directory.
type Git struct{ Ref string }

func (g Git) Describe() string { return g.Ref }

func git(args ...string) ([]byte, error) {
	var stderr bytes.Buffer
	cmd := exec.Command("git", args...)
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git %s: %s", strings.Join(args, " "), strings.TrimSpace(stderr.String()))
	}
	return out, nil
}

func (g Git) Files(dir string) ([]string, error) {
	if _, err := git("rev-parse", "--verify", "--quiet", g.Ref+"^{commit}"); err != nil {
		return nil, fmt.Errorf("base ref %q is not a commit in this repository", g.Ref)
	}
	out, err := git("ls-tree", "-r", "--name-only", g.Ref, "--", dir)
	if err != nil {
		return nil, err
	}
	prefix := strings.TrimSuffix(filepath.ToSlash(filepath.Clean(dir)), "/") + "/"
	var files []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		files = append(files, strings.TrimPrefix(line, prefix))
	}
	return files, nil
}

func (g Git) Read(dir, rel string) ([]byte, error) {
	return git("show", g.Ref+":./"+path.Join(filepath.ToSlash(dir), rel))
}

var versionFile = regexp.MustCompile(`^v([1-9][0-9]*)\.json$`)

// Load reads every topic under dir from src. A directory that does not
// exist at all yields no topics (a first pull request adding contracts).
func Load(src Source, dir string) ([]Topic, error) {
	files, err := src.Files(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	byTopic := map[string][]string{}
	for _, f := range files {
		parts := strings.Split(f, "/")
		if len(parts) != 2 {
			return nil, fmt.Errorf("%s/%s (%s): expected <topic>/v<N>.json or <topic>/mode", dir, f, src.Describe())
		}
		byTopic[parts[0]] = append(byTopic[parts[0]], parts[1])
	}
	var names []string
	for t := range byTopic {
		names = append(names, t)
	}
	sort.Strings(names)
	var topics []Topic
	for _, name := range names {
		t := Topic{Name: name, Mode: compat.DefaultMode, Raw: map[string][]byte{}}
		nums := map[int]bool{}
		for _, f := range byTopic[name] {
			where := fmt.Sprintf("%s/%s/%s (%s)", dir, name, f, src.Describe())
			if f == "mode" {
				b, err := src.Read(dir, name+"/mode")
				if err != nil {
					return nil, err
				}
				if t.Mode, err = compat.ParseMode(string(b)); err != nil {
					return nil, fmt.Errorf("%s: %w", where, err)
				}
				continue
			}
			m := versionFile.FindStringSubmatch(f)
			if m == nil {
				return nil, fmt.Errorf("%s: not a version file (v1.json, v2.json, ...) or mode", where)
			}
			n, _ := strconv.Atoi(m[1])
			nums[n] = true
		}
		for n := 1; n <= len(nums); n++ {
			if !nums[n] {
				return nil, fmt.Errorf("%s/%s (%s): versions must run v1..v%d without gaps; v%d is missing", dir, name, src.Describe(), len(nums), n)
			}
		}
		for n := 1; n <= len(nums); n++ {
			v := fmt.Sprintf("v%d", n)
			b, err := src.Read(dir, name+"/"+v+".json")
			if err != nil {
				return nil, err
			}
			s, err := schema.Parse(b)
			if err != nil {
				return nil, fmt.Errorf("%s/%s/%s.json (%s): %w", dir, name, v, src.Describe(), err)
			}
			t.Versions = append(t.Versions, compat.Version{Name: v, Schema: s})
			t.Raw[v] = b
		}
		topics = append(topics, t)
	}
	return topics, nil
}

type Kind string

const (
	Incompatible Kind = "incompatible"
	Edited       Kind = "edited after publication"
	Deleted      Kind = "deleted after publication"
)

type Problem struct {
	Topic, Version string
	Kind           Kind
	Message        string
	Findings       []compat.Finding
}

// Result of a check over a whole directory.
type Result struct {
	Problems []Problem
	// Checked lists "topic vN" for every version checked for compatibility.
	Checked []string
	Notes   []string
}

// History checks every version of every topic against those before it.
func History(topics []Topic) Result {
	return Gate(nil, topics)
}

// Gate checks head against base: nothing published changes or disappears,
// and every version new in head is compatible with what came before it.
func Gate(base, head []Topic) Result {
	var r Result
	baseBy := map[string]Topic{}
	for _, t := range base {
		baseBy[t.Name] = t
	}
	headBy := map[string]Topic{}
	for _, t := range head {
		headBy[t.Name] = t
	}
	for _, b := range base {
		if _, ok := headBy[b.Name]; !ok {
			r.Problems = append(r.Problems, Problem{Topic: b.Name, Kind: Deleted,
				Message: fmt.Sprintf("topic %s and its %d versions were deleted; consumers still read it", b.Name, len(b.Versions))})
		}
	}
	for _, h := range head {
		b, existed := baseBy[h.Name]
		published := 0
		if existed {
			published = len(b.Versions)
			if b.Mode != h.Mode {
				r.Notes = append(r.Notes, fmt.Sprintf("%s: mode changed from %s to %s", h.Name, b.Mode, h.Mode))
			}
			for i, old := range b.Versions {
				if i >= len(h.Versions) {
					r.Problems = append(r.Problems, Problem{Topic: h.Name, Version: old.Name, Kind: Deleted,
						Message: fmt.Sprintf("%s %s was published and has been deleted", h.Name, old.Name)})
					continue
				}
				now := h.Versions[i]
				back, fwd := compat.Compare(old, compat.Version{Name: now.Name + " (edited)", Schema: now.Schema})
				if len(back) > 0 || len(fwd) > 0 {
					var findings []compat.Finding
					if len(back) > 0 {
						findings = append(findings, compat.Finding{Against: old.Name, Candidate: now.Name + " (edited)", Direction: compat.BackwardDir, Reasons: back})
					}
					if len(fwd) > 0 {
						findings = append(findings, compat.Finding{Against: old.Name, Candidate: now.Name + " (edited)", Direction: compat.ForwardDir, Reasons: fwd})
					}
					r.Problems = append(r.Problems, Problem{Topic: h.Name, Version: old.Name, Kind: Edited,
						Message:  fmt.Sprintf("%s %s was published and has been changed in place; publish v%d instead", h.Name, old.Name, len(h.Versions)+1),
						Findings: findings})
				}
			}
		}
		for i := published; i < len(h.Versions); i++ {
			v := h.Versions[i]
			r.Checked = append(r.Checked, h.Name+" "+v.Name)
			if f := compat.Check(h.Versions[:i], v, h.Mode); len(f) > 0 {
				r.Problems = append(r.Problems, Problem{Topic: h.Name, Version: v.Name, Kind: Incompatible,
					Message:  fmt.Sprintf("%s %s breaks %s: %s", h.Name, v.Name, h.Mode, h.Mode.Explain()),
					Findings: f})
			}
		}
	}
	return r
}
