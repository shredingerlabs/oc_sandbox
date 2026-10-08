// Shared New Project state: one merged view, fake standard values.
// THROWAWAY prototype (issue #70). The question: does the merged
// left-menu/right-fields New Project feel right, and how should the
// Container build/stop center-view be laid out?
package protoapp

import (
	"fmt"
	"strings"
)

// field ids, shared by all designs
const (
	fName = iota
	fPath
	fURL
	fEdition
	fModes
	fStart
	fAI
	fVCS
	fProxy
	fCreate // the bottom button
)

const pathBase = "/home/dev/oc-sandbox"

type npState struct {
	source     int    // 0 blank, 1 from-url
	url        string
	name       string
	path       string // derived from name
	pathManual bool   // set when the user types in the path field
	edition    string
	modes   []string
	start   string
	ai      string
	vcs     string
	proxy   bool
	cursor  int    // caret position inside the focused text field
	fields  []int  // visible field ids, in order (per design)
	focus   int    // index into fields
	toast   string
}

func newNPState() *npState {
	s := &npState{
		name: "",
		path: pathBase,
		url:  "https://github.com/user/repo.git",
	}
	// standard values (decided defaults from configure-project extraction)
	s.edition = "swdev"
	s.modes = nil
	s.start = "opencode"
	s.ai = "gwdg-saia"
	s.vcs = "none"
	s.proxy = false
	return s
}

func (s *npState) rePath() {
	n := s.name
	n = strings.ToLower(n)
	var b []rune
	for _, r := range n {
		switch {
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9':
			b = append(b, r)
		case r == '-' || r == '_' || r == '.' || r == ' ':
			b = append(b, '-')
		}
	}
	s.path = pathBase + "/" + string(b)
}

// autofill derives the path from the name; editing the name always
// re-derives it, editing the path types it directly.
func (s *npState) autofill() {
	if !s.pathManual {
		s.rePath()
	}
}

// labels used by every design
func fieldLabel(id int) string {
	switch id {
	case fName:
		return "Project name"
	case fPath:
		return "Project path (auto)"
	case fURL:
		return "Repo URL"
	case fEdition:
		return "Edition"
	case fModes:
		return "Modes"
	case fStart:
		return "Start"
	case fAI:
		return "AI provider"
	case fVCS:
		return "VCS tracking"
	case fProxy:
		return "Use proxy"
	}
	return ""
}

func (s *npState) value(id int) string {
	switch id {
	case fName:
		return s.name
	case fPath:
		return s.path
	case fURL:
		return s.url
	case fEdition:
		return s.edition
	case fModes:
		return modesLabel(s.modes)
	case fStart:
		return s.start
	case fAI:
		return s.ai
	case fVCS:
		return s.vcs
	case fProxy:
		return boolLabel(s.proxy)
	}
	return ""
}

// nextVal cycles a select field to its next alternative; returns true if handled.
// optionList is the fixed canonical option order (selection never
// reorders the list; the selected option keeps its slot).
func optionList(id int) []string {
	switch id {
	case fEdition:
		return []string{"swdev", "webdev", "hil", "custom", "writing", "ros2"}
	case fModes:
		return []string{"none", "offline", "hil_mode", "cbm_ui"}
	case fStart:
		return []string{"opencode", "console", "web"}
	case fAI:
		return []string{"gwdg-saia", "none"}
	case fVCS:
		return []string{"none", "github.com", "gitlab.com", "own GitLab", "others"}
	case fProxy:
		return []string{"no", "yes"}
	}
	return nil
}

func (s *npState) nextVal(id int) bool {
	switch id {
	case fEdition:
		s.edition = cycle(s.edition, optionList(id))
	case fModes:
		s.modes = cycleModes(s.modes)
	case fStart:
		s.start = cycle(s.start, optionList(id))
	case fAI:
		s.ai = cycle(s.ai, optionList(id))
	case fVCS:
		s.vcs = cycle(s.vcs, optionList(id))
	case fProxy:
		s.proxy = !s.proxy
	default:
		return false
	}
	return true
}

// setSelect picks a specific option for the field (used by option clicks).
func (s *npState) setSelect(id int, val string) {
	switch id {
	case fEdition:
		s.edition = val
	case fModes:
		if val == "none" {
			s.modes = nil
		} else {
			s.modes = []string{val}
		}
	case fStart:
		s.start = val
	case fAI:
		s.ai = val
	case fVCS:
		s.vcs = val
	case fProxy:
		s.proxy = val == "yes"
	}
}

var editions = []string{"opencode-sandbox-swdev", "opencode-sandbox-webdev", "opencode-sandbox-hil", "opencode-sandbox-custom", "opencode-sandbox-writing", "opencode-sandbox-ros2"}

func cycle(cur string, alts []string) string {
	for i, a := range alts {
		if a == cur {
			return alts[(i+1)%len(alts)]
		}
	}
	return alts[0]
}

func cycleModes(cur []string) []string {
	// single-select cycling keeps the prototype simple; "none" is reachable
	return []string{cycle(modesLabel(cur), optionList(fModes))}
}

// input handles a printable rune in the focused text field.
func (s *npState) input(id int, r rune) bool {
	switch id {
	case fName:
		s.name = insertRune(s.name, s.cursor, r)
		s.cursor++
		s.autofill() // path auto-updates after name entry
		return true
	case fURL:
		s.url = insertRune(s.url, s.cursor, r)
		s.cursor++
		return true
	case fPath:
		s.path = insertRune(s.path, s.cursor, r)
		s.cursor++
		s.pathManual = true // autofill stops once the path is hand-edited
		return true
	}
	return false
}

func (s *npState) backspace(id int) bool {
	switch id {
	case fName:
		s.name, s.cursor = deleteRune(s.name, s.cursor)
		s.autofill()
		return true
	case fURL:
		s.url, s.cursor = deleteRune(s.url, s.cursor)
		return true
	case fPath:
		s.path, s.cursor = deleteRune(s.path, s.cursor)
		s.pathManual = true
		return true
	}
	return false
}

func (s *npState) create() {
	s.toast = fmt.Sprintf("create %s %s (fake)", s.name, shortEdition(s.edition))
}

func insertRune(s string, i int, r rune) string {
	rs := []rune(s)
	if i > len(rs) {
		i = len(rs)
	}
	rs = append(rs[:i], append([]rune{r}, rs[i:]...)...)
	return string(rs)
}

func deleteRune(s string, i int) (string, int) {
	rs := []rune(s)
	if i > len(rs) {
		i = len(rs)
	}
	if i == 0 {
		return s, i
	}
	rs = append(rs[:i-1], rs[i:]...)
	return string(rs), i - 1
}

// fake container build/stop state, shared by designs
type ctState struct {
	building  string // edition currently "building" (fake)
	selection map[string]bool
	toast     string
}

func newCTState() *ctState {
	return &ctState{selection: map[string]bool{}}
}

func fakeRunning() []Project {
	var out []Project
	for _, p := range fakeProjects() {
		if p.Status == "running" {
			out = append(out, p)
		}
	}
	return out
}

func (c *ctState) selectedNames(running []Project) []string {
	var out []string
	for _, p := range running {
		if c.selection[p.Name] {
			out = append(out, p.Name)
		}
	}
	return out
}

func (c *ctState) allSelected(running []Project) bool {
	n := len(running)
	if n == 0 {
		return false
	}
	return len(c.selectedNames(running)) == n
}

func (c *ctState) toggleAll(running []Project) {
	if c.allSelected(running) {
		for k := range c.selection {
			delete(c.selection, k)
		}
		return
	}
	for _, p := range running {
		c.selection[p.Name] = true
	}
}

func (c *ctState) stop(running []Project) {
	names := c.selectedNames(running)
	if len(names) == 0 {
		c.toast = "nothing selected"
		return
	}
	c.toast = "stop " + strings.Join(names, ", ") + " (fake)"
	for _, n := range names {
		delete(c.selection, n)
	}
}

func (c *ctState) build(ed string) {
	c.building = ed
	c.toast = "build " + shortEdition(ed) + " (fake)"
}
