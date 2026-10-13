package compat

import (
	"fmt"
	"strings"

	"github.com/ahmed-hashim-pro/schema-drift/schema"
)

// Mode is a compatibility rule between versions of one topic's schema, named
// as in Confluent Schema Registry.
type Mode string

const (
	None               Mode = "NONE"
	Backward           Mode = "BACKWARD"
	BackwardTransitive Mode = "BACKWARD_TRANSITIVE"
	Forward            Mode = "FORWARD"
	ForwardTransitive  Mode = "FORWARD_TRANSITIVE"
	Full               Mode = "FULL"
	FullTransitive     Mode = "FULL_TRANSITIVE"
)

var Modes = []Mode{None, Backward, BackwardTransitive, Forward, ForwardTransitive, Full, FullTransitive}

// DefaultMode is the registry's default.
const DefaultMode = Backward

func ParseMode(s string) (Mode, error) {
	m := Mode(strings.ToUpper(strings.TrimSpace(s)))
	for _, x := range Modes {
		if x == m {
			return m, nil
		}
	}
	names := make([]string, len(Modes))
	for i, x := range Modes {
		names[i] = string(x)
	}
	return "", fmt.Errorf("unknown compatibility mode %q (%s)", s, strings.Join(names, ", "))
}

func (m Mode) Transitive() bool { return strings.HasSuffix(string(m), "_TRANSITIVE") }
func (m Mode) Backward() bool {
	return strings.HasPrefix(string(m), "BACKWARD") || strings.HasPrefix(string(m), "FULL")
}
func (m Mode) Forward() bool {
	return strings.HasPrefix(string(m), "FORWARD") || strings.HasPrefix(string(m), "FULL")
}

// What each direction means for the people involved.
func (m Mode) Explain() string {
	switch {
	case m == None:
		return "no check"
	case m.Backward() && m.Forward():
		return "old and new consumers must read both old and new events"
	case m.Backward():
		return "consumers on the new schema must read events written with the old"
	default:
		return "consumers on the old schema must read events written with the new"
	}
}

type Version struct {
	Name   string
	Schema *schema.Schema
}

// Direction names which way data flows in a finding.
type Direction string

const (
	// BackwardDir: events written under the old schema, read under the new.
	BackwardDir Direction = "backward"
	// ForwardDir: events written under the new schema, read under the old.
	ForwardDir Direction = "forward"
)

// Finding is one failed requirement: the candidate against one earlier version.
type Finding struct {
	Against   string
	Candidate string
	Direction Direction
	Reasons   []Reason
}

// Compare returns both directions between an old and a new version.
func Compare(old, new Version) (backward, forward []Reason) {
	backward = Subset(old.Schema, new.Schema, old.Name, new.Name)
	forward = Subset(new.Schema, old.Schema, new.Name, old.Name)
	return
}

// Verdict names the strongest mode a pair satisfies.
func Verdict(backward, forward []Reason) string {
	switch {
	case len(backward) == 0 && len(forward) == 0:
		return "FULL"
	case len(backward) == 0:
		return "BACKWARD"
	case len(forward) == 0:
		return "FORWARD"
	}
	return "NONE (breaking both ways)"
}

// Check tests a candidate against the versions before it under mode: the
// latest one, or every one for a transitive mode.
func Check(prior []Version, candidate Version, mode Mode) []Finding {
	if mode == None || len(prior) == 0 {
		return nil
	}
	against := prior[len(prior)-1:]
	if mode.Transitive() {
		against = prior
	}
	var out []Finding
	for i := len(against) - 1; i >= 0; i-- {
		old := against[i]
		if mode.Backward() {
			if r := Subset(old.Schema, candidate.Schema, old.Name, candidate.Name); len(r) > 0 {
				out = append(out, Finding{Against: old.Name, Candidate: candidate.Name, Direction: BackwardDir, Reasons: r})
			}
		}
		if mode.Forward() {
			if r := Subset(candidate.Schema, old.Schema, candidate.Name, old.Name); len(r) > 0 {
				out = append(out, Finding{Against: old.Name, Candidate: candidate.Name, Direction: ForwardDir, Reasons: r})
			}
		}
	}
	return out
}
