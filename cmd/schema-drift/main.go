// Command schema-drift checks event contracts: whether a new schema version
// breaks the consumers or producers of the old, whether events match a
// schema, and whether a pull request changes contracts safely.
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/ahmed-hashim-pro/schema-drift/compat"
	"github.com/ahmed-hashim-pro/schema-drift/gate"
	"github.com/ahmed-hashim-pro/schema-drift/infer"
	"github.com/ahmed-hashim-pro/schema-drift/schema"
)

const version = "0.1.0"

const usage = `schema-drift: catch breaking event-schema changes before they ship

usage:
  schema-drift check   OLD.json NEW.json [--mode MODE]   what changes for consumers and producers
  schema-drift verify  SCHEMA.json EVENTS.ndjson          events against a schema, by JSON Pointer
  schema-drift infer   EVENTS.ndjson                      draft a schema from sample events
  schema-drift history [DIR]                              every topic's versions under its mode
  schema-drift gate    [--base REF] [--dir DIR]           a pull request's contract changes, for CI
  schema-drift modes                                      the compatibility modes
  schema-drift version

DIR defaults to contracts (one folder per topic: v1.json, v2.json, ..., optional mode file).
MODE is NONE, BACKWARD (default), FORWARD, FULL, or any of the last three with _TRANSITIVE.

exit codes: 0 ok; 1 a break, an invalid event or a gate problem; 2 bad input or usage`

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// usageError marks errors that are the caller's to fix.
type usageError struct{ error }

func run(args []string, out, errOut io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(errOut, usage)
		return 2
	}
	var code int
	var err error
	switch args[0] {
	case "check":
		code, err = cmdCheck(args[1:], out)
	case "verify":
		code, err = cmdVerify(args[1:], out)
	case "infer":
		code, err = cmdInfer(args[1:], out)
	case "history":
		code, err = cmdHistory(args[1:], out)
	case "gate":
		code, err = cmdGate(args[1:], out)
	case "modes":
		for _, m := range compat.Modes {
			fmt.Fprintf(out, "%-20s %s\n", m, m.Explain()+transitiveNote(m))
		}
	case "version", "--version":
		fmt.Fprintf(out, "schema-drift %s\n", version)
	case "help", "-h", "--help":
		fmt.Fprintln(out, usage)
	default:
		err = fmt.Errorf("unknown command %q\n\n%s", args[0], usage)
	}
	if err != nil {
		fmt.Fprintf(errOut, "schema-drift: %v\n", err)
		return 2
	}
	return code
}

func transitiveNote(m compat.Mode) string {
	switch {
	case m == compat.None:
		return ""
	case m.Transitive():
		return ", against every earlier version"
	default:
		return ", against the previous version"
	}
}

func flags(name string, args []string, positional int, setup func(fs *flag.FlagSet)) ([]string, error) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	setup(fs)
	// Flags may come after the positional arguments.
	var pos []string
	for len(args) > 0 {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		args = fs.Args()
		if len(args) > 0 {
			pos = append(pos, args[0])
			args = args[1:]
		}
	}
	if positional >= 0 && len(pos) != positional {
		return nil, fmt.Errorf("%s takes %d file arguments, got %d", name, positional, len(pos))
	}
	return pos, nil
}

func label(path string) string {
	return strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
}

func cmdCheck(args []string, out io.Writer) (int, error) {
	var modeName string
	pos, err := flags("check", args, 2, func(fs *flag.FlagSet) {
		fs.StringVar(&modeName, "mode", string(compat.DefaultMode), "compatibility mode")
	})
	if err != nil {
		return 0, err
	}
	mode, err := compat.ParseMode(modeName)
	if err != nil {
		return 0, err
	}
	if mode.Transitive() {
		return 0, errors.New("check compares two versions; transitive modes need a history (use history or gate)")
	}
	oldS, err := schema.Load(pos[0])
	if err != nil {
		return 0, err
	}
	newS, err := schema.Load(pos[1])
	if err != nil {
		return 0, err
	}
	on, nn := label(pos[0]), label(pos[1])
	if on == nn {
		on, nn = pos[0], pos[1]
	}
	old, new := compat.Version{Name: on, Schema: oldS}, compat.Version{Name: nn, Schema: newS}
	back, fwd := compat.Compare(old, new)
	fmt.Fprintf(out, "%s -> %s: %s\n", on, nn, compat.Verdict(back, fwd))
	printDirection(out, fmt.Sprintf("backward: consumers on %s reading events written with %s", nn, on), back)
	printDirection(out, fmt.Sprintf("forward: consumers on %s reading events written with %s", on, nn), fwd)
	ok := (!mode.Backward() || len(back) == 0) && (!mode.Forward() || len(fwd) == 0)
	verdict := "passes"
	if !ok {
		verdict = "FAILS"
	}
	fmt.Fprintf(out, "\nmode %s %s (%s)\n", mode, verdict, mode.Explain())
	if ok {
		return 0, nil
	}
	return 1, nil
}

func printDirection(out io.Writer, title string, reasons []compat.Reason) {
	fmt.Fprintf(out, "\n%s\n", title)
	if len(reasons) == 0 {
		fmt.Fprintln(out, "  compatible")
		return
	}
	printReasons(out, reasons, "  ")
}

func printReasons(out io.Writer, reasons []compat.Reason, indent string) {
	for _, r := range reasons {
		fmt.Fprintf(out, "%sBREAKING %s: %s\n", indent, r.Path, r.Message)
		if r.Proven {
			fmt.Fprintf(out, "%s         e.g. %s\n", indent, schema.Compact(r.Witness))
		}
	}
}

func cmdVerify(args []string, out io.Writer) (int, error) {
	var max int
	pos, err := flags("verify", args, 2, func(fs *flag.FlagSet) {
		fs.IntVar(&max, "max", 20, "stop printing after this many invalid events")
	})
	if err != nil {
		return 0, err
	}
	s, err := schema.Load(pos[0])
	if err != nil {
		return 0, err
	}
	f, err := os.Open(pos[1])
	if err != nil {
		return 0, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 16<<20)
	var total, bad, shown int
	for line := 1; sc.Scan(); line++ {
		text := bytes.TrimSpace(sc.Bytes())
		if len(text) == 0 {
			continue
		}
		total++
		var problems []string
		v, err := schema.Decode(text)
		if err != nil {
			problems = []string{err.Error()}
		} else {
			for _, x := range schema.Validate(s, v) {
				problems = append(problems, x.String())
			}
		}
		if len(problems) == 0 {
			continue
		}
		bad++
		if shown < max {
			shown++
			for _, p := range problems {
				fmt.Fprintf(out, "line %d: %s\n", line, p)
			}
		}
	}
	if err := sc.Err(); err != nil {
		return 0, err
	}
	if bad > shown {
		fmt.Fprintf(out, "... and %d more invalid events\n", bad-shown)
	}
	fmt.Fprintf(out, "%d events, %d valid, %d invalid\n", total, total-bad, bad)
	if bad > 0 {
		return 1, nil
	}
	return 0, nil
}

func cmdInfer(args []string, out io.Writer) (int, error) {
	pos, err := flags("infer", args, 1, func(*flag.FlagSet) {})
	if err != nil {
		return 0, err
	}
	data, err := os.ReadFile(pos[0])
	if err != nil {
		return 0, err
	}
	var events []any
	for i, line := range bytes.Split(data, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		v, err := schema.Decode(line)
		if err != nil {
			return 0, fmt.Errorf("%s line %d: %w", pos[0], i+1, err)
		}
		events = append(events, v)
	}
	if len(events) == 0 {
		return 0, fmt.Errorf("%s: no events", pos[0])
	}
	b, err := json.MarshalIndent(infer.Infer(events), "", "  ")
	if err != nil {
		return 0, err
	}
	fmt.Fprintf(out, "%s\n", b)
	return 0, nil
}

func cmdHistory(args []string, out io.Writer) (int, error) {
	pos, err := flags("history", args, -1, func(*flag.FlagSet) {})
	if err != nil {
		return 0, err
	}
	dir := "contracts"
	switch len(pos) {
	case 0:
	case 1:
		dir = pos[0]
	default:
		return 0, errors.New("history takes at most one directory")
	}
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		return 0, fmt.Errorf("%s is not a directory", dir)
	}
	topics, err := gate.Load(gate.Disk{}, dir)
	if err != nil {
		return 0, err
	}
	return report(out, topics, gate.History(topics)), nil
}

func cmdGate(args []string, out io.Writer) (int, error) {
	var base, dir string
	if _, err := flags("gate", args, 0, func(fs *flag.FlagSet) {
		fs.StringVar(&base, "base", "origin/main", "git ref the pull request is compared with")
		fs.StringVar(&dir, "dir", "contracts", "contracts directory")
	}); err != nil {
		return 0, err
	}
	baseTopics, err := gate.Load(gate.Git{Ref: base}, dir)
	if err != nil {
		return 0, err
	}
	headTopics, err := gate.Load(gate.Disk{}, dir)
	if err != nil {
		return 0, err
	}
	fmt.Fprintf(out, "%s: %s compared with %s\n", dir, "working tree", base)
	return report(out, headTopics, gate.Gate(baseTopics, headTopics)), nil
}

func report(out io.Writer, topics []gate.Topic, r gate.Result) int {
	for _, t := range topics {
		bad := 0
		for _, p := range r.Problems {
			if p.Topic == t.Name {
				bad++
			}
		}
		status := "ok"
		if bad > 0 {
			status = fmt.Sprintf("%d problem(s)", bad)
		}
		fmt.Fprintf(out, "%-14s %-20s v1..v%d  %s\n", t.Name, t.Mode, len(t.Versions), status)
	}
	for _, n := range r.Notes {
		fmt.Fprintf(out, "note: %s\n", n)
	}
	for _, p := range r.Problems {
		fmt.Fprintf(out, "\n%s\n", p.Message)
		for _, f := range p.Findings {
			switch f.Direction {
			case compat.BackwardDir:
				fmt.Fprintf(out, "  consumers on %s reading events written with %s:\n", f.Candidate, f.Against)
			default:
				fmt.Fprintf(out, "  consumers on %s reading events written with %s:\n", f.Against, f.Candidate)
			}
			printReasons(out, f.Reasons, "    ")
		}
	}
	if len(r.Problems) > 0 {
		fmt.Fprintf(out, "\n%d problem(s)\n", len(r.Problems))
		return 1
	}
	fmt.Fprintf(out, "\nno problems (%d version(s) checked)\n", len(r.Checked))
	return 0
}
