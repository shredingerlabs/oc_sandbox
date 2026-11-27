package webserver

import (
	"os"
	"strings"
)

// porting.go carries the §10 Dockerfile.custom hand-holding: the legacy
// file stops shipping and is never touched, but a host that still holds
// an uncommented stage sees a warning row (Images + settings) with the
// inline porting gist — each stage's RUN/ENV/USER lines become
// toolchains/custom/<id>.Dockerfile, one toolchain per file. The porting
// doc lives in docs/ (linked from the release notes); production serves
// dist/ only, so the gist renders fully inline — no link targets the
// unserved docs.

// portStage is one legacy Dockerfile.custom stage's porting gist: the
// edition id (the opencode-sandbox- AS name minus its prefix) and the
// stage's RUN/ENV/USER lines — the recipe-grammar input the ported
// custom/<id>.Dockerfile starts from (§2).
type portStage struct {
	ID    string
	Lines []string
}

// customPorting parses the legacy Dockerfile.custom at path: every
// uncommented stage named opencode-sandbox-<id> yields one portStage
// carrying its RUN/ENV/USER lines (backslash continuations joined,
// comment lines skipped — the shipped template stays inert by
// construction). Stages not named opencode-sandbox-<id> are boundaries
// only. Nil when the file is absent or holds no uncommented stage.
func customPorting(path string) []portStage {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var stages []portStage
	var cur *portStage
	for _, logical := range logicalPortLines(string(raw)) {
		if as, ok := portStageHeader(logical); ok {
			cur = nil
			if id, ok := strings.CutPrefix(as, "opencode-sandbox-"); ok {
				stages = append(stages, portStage{ID: id})
				cur = &stages[len(stages)-1]
			}
			continue
		}
		if cur != nil && portCarries(logical) {
			cur.Lines = append(cur.Lines, logical)
		}
	}
	return stages
}

// logicalPortLines splits the file's text into logical lines: comment
// lines and blank lines drop, backslash continuations join with single
// spaces (the gist is the candidate lines, not byte-exact source).
func logicalPortLines(raw string) []string {
	var out []string
	var acc string
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		acc += line
		if strings.HasSuffix(acc, `\`) {
			acc = strings.TrimSuffix(acc, `\`)
			acc += " "
			continue
		}
		// Whitespace normalizes (multi-space runs from joined
		// continuations collapse) — the gist is the candidate lines,
		// not byte-exact source.
		out = append(out, strings.Join(strings.Fields(acc), " "))
		acc = ""
	}
	return out
}

// portStageHeader reports whether the logical line opens a stage
// (FROM … AS <name>) and, when so, yields the AS name — case-
// insensitive like Dockerfile itself.
func portStageHeader(line string) (as string, ok bool) {
	fields := strings.Fields(line)
	if len(fields) < 4 || !strings.EqualFold(fields[0], "FROM") {
		return "", false
	}
	for i := 1; i+1 < len(fields); i++ {
		if strings.EqualFold(fields[i], "AS") {
			return fields[i+1], true
		}
	}
	return "", false
}

// portCarries reports whether the logical line is one the porting gist
// maps into the custom recipe: RUN, ENV or USER (case-insensitive).
func portCarries(line string) bool {
	first := strings.Fields(line)
	if len(first) == 0 {
		return false
	}
	switch strings.ToUpper(first[0]) {
	case "RUN", "ENV", "USER":
		return true
	}
	return false
}

// portingGistText renders the porting gist as the one-line plain text
// the settings warning shows: the copy, then per stage the mapping into
// its custom recipe file ("; "-joined — a single escaped text node).
func portingGistText(stages []portStage) string {
	var b strings.Builder
	b.WriteString("Dockerfile.custom still holds custom stages — port each to toolchain sets: ")
	for i, st := range stages {
		if i > 0 {
			b.WriteString("; ")
		}
		b.WriteString(st.ID + " → toolchains/custom/" + st.ID + ".Dockerfile: " + strings.Join(st.Lines, " ; "))
	}
	return b.String()
}
