// Package protoapp is a THROWAWAY prototype (issue #69, wayfinder map #56).
// It answers: what should the Open Project center-view look like?
// Do not promote to production without a rewrite (see prototype skill).
//
// Three structurally different variants of the Open Project center-view,
// switchable with the V key (tab/arrow cycles in the floating switcher row).
// The reaction target: density, alignment, card info, click/double-click feel.
package protoapp

import "time"

// Project mirrors the fields the real TUI will read from projects.json +
// sandbox_config.json (see docs/research/tui-data-map.md on branch
// research/tui-data-map). Fake data only.
type Project struct {
	Name          string
	Path          string
	Status        string // "running" | "stopped"
	Edition       string
	Modes         []string
	StartOption   string
	AiProvider    string
	VcsTracking   string
	SetupComplete bool
	UseProxy      bool
	LastUsed      time.Time
}

func fakeProjects() []Project {
	return []Project{
		{
			Name: "swdev-core", Path: "/home/dev/oc-sandbox/swdev-core",
			Status: "running", Edition: "swdev", Modes: []string{"offline", "hil_mode"},
			StartOption: "opencode", AiProvider: "gwdg-saia", VcsTracking: "github.com",
			SetupComplete: true, LastUsed: mustTime("2026-10-07T09:14:00Z"),
		},
		{
			Name: "hil-bench", Path: "/home/dev/oc-sandbox/hil-bench",
			Status: "stopped", Edition: "hil", Modes: []string{"cbm_ui"},
			StartOption: "console", AiProvider: "none", VcsTracking: "none",
			SetupComplete: true, UseProxy: true, LastUsed: mustTime("2026-10-05T17:40:00Z"),
		},
		{
			Name: "web-rework", Path: "/home/dev/oc-sandbox/web-rework",
			Status: "stopped", Edition: "webdev", Modes: []string{},
			StartOption: "web", AiProvider: "gwdg-saia", VcsTracking: "gitlab.com",
			SetupComplete: false, LastUsed: mustTime("2026-10-02T11:05:00Z"),
		},
		{
			Name: "docs-site", Path: "/home/dev/oc-sandbox/docs-site",
			Status: "stopped", Edition: "swdev", Modes: []string{"offline"},
			StartOption: "opencode", AiProvider: "none", VcsTracking: "others",
			SetupComplete: true, LastUsed: mustTime("2026-09-20T08:00:00Z"),
		},
		{
			Name: "lab-scratch", Path: "/home/dev/oc-sandbox/lab-scratch",
			Status: "stopped", Edition: "custom", Modes: []string{},
			StartOption: "console", AiProvider: "none", VcsTracking: "none",
			SetupComplete: false, LastUsed: mustTime("2026-08-30T19:22:00Z"),
		},
	}
}

func mustTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}
