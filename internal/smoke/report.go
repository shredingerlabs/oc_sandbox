// Package smoke is the automated A-list parity harness of the sunset-gate
// checklist (issue #73, resolved there): it runs the bash TUI's own
// functions and scripts against throwaway HOMEs and checks that the Go data
// layer (internal/config, internal/registry, internal/sandboxcfg,
// internal/discovery) reads and writes the same files compatibly — no
// podman needed, so it runs in this dev container.
//
// Podman-dependent boxes stay fogged: they belong to the B-list manual
// probes executed by shredinger per #73 and are not implemented here.
package smoke

import (
	"fmt"
	"strings"
)

// Status of a checklist box.
type Status string

const (
	Pass    Status = "PASS"
	Fail    Status = "FAIL"
	Skipped Status = "SKIPPED"
)

// Item is one A-list checkbox result.
type Item struct {
	Box   string // A1…A7
	Title string
	Status
	Note string // what was verified / why failed or skipped
}

// Report is the full harness result.
type Report struct{ Items []Item }

func (r Report) count(s Status) (n int) {
	for _, it := range r.Items {
		if it.Status == s {
			n++
		}
	}
	return n
}

// Passed counts PASS boxes.
func (r Report) Passed() int { return r.count(Pass) }

// Failed counts FAIL boxes.
func (r Report) Failed() int { return r.count(Fail) }

// Skipped counts SKIPPED boxes.
func (r Report) Skipped() int { return r.count(Skipped) }

// ExitCode returns the process exit code for a harness run: 0 unless a box
// failed, then 1 (general error, CONTEXT.md exit-code policy).
func (r Report) ExitCode() int {
	if r.Failed() > 0 {
		return 1
	}
	return 0
}

// Text renders the tick-ready checklist for the sunset-gate ticket: one
// [x]/[!]/[ ] line per box in #73 order, notes underneath, summary last.
func (r Report) Text() string {
	var b strings.Builder
	fmt.Fprintln(&b, "Parity pre-check (A-list smoke, issue #73)")
	marks := map[Status]string{Pass: "[x]", Fail: "[!]", Skipped: "[ ]"}
	for _, it := range r.Items {
		fmt.Fprintf(&b, "%s %s %s\n", marks[it.Status], it.Box, it.Title)
		if it.Note != "" {
			fmt.Fprintf(&b, "      %s\n", it.Note)
		}
	}
	fmt.Fprintf(&b, "summary: %d pass, %d fail, %d skipped\n", r.Passed(), r.Failed(), r.Skipped())
	return b.String()
}
