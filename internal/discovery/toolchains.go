// Toolchain recipe lint, parse, and discovery (spec toolchain-sets §2) —
// the Go seam beside the bash helper dist/scripts/toolchains.sh (#195).
// One Dockerfile-text file per toolchain; allowed: RUN, ENV, USER, comments
// and '# extra: <absolute path>' lines. Everything else is a lint error:
// ARG (bakes out-of-band state invisible to the content hash), COPY,
// WORKDIR, ENTRYPOINT (the composition footer owns it), unknown directives,
// USER switch-backs (monotonic root→dev), ENV PATH (PATH additions ride
// base's ~/.profile.d/<id>.sh hook), direct ~/.profile//~/.bashrc writes,
// and floating installs (pip ==x.y.z, npm @x.y.z, version-embedded direct
// URLs — no latest segments, no release-API queries — rustup
// --default-toolchain <ver>; apt is exempt — its pin is the suite).
// 'base' is a reserved id. Discovery (list + parse + lint of recipes/ and
// custom/ + manifest read) runs live at each call; the bash helper is the
// reference behavior and the agreement test pins both together.

package discovery

import (
	"bufio"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// RecipeRecord is one parse record of a valid recipe: the directive kind
// (run / env / user / extra) and the directive's text (the '# extra:' path
// for extras).
type RecipeRecord struct {
	Kind string
	Text string
}

// lintLine is one logical recipe line: the joined text and the physical
// line it started on (every refusal names it).
type lintLine struct {
	text string
	line int
}

// lintRecipe walks the file's logical lines (backslash continuations
// joined) and returns the collected refusal reasons, parse records, and
// '# extra:' paths.
func lintRecipe(path string) (reasons []string, recs []RecipeRecord, extras []string) {
	f, err := os.Open(path)
	if err != nil {
		return []string{fmt.Sprintf("toolchains: recipe file not found: %s", path)}, nil, nil
	}
	defer f.Close()
	userState := "none" // none → root → dev, monotonic
	sc := bufio.NewScanner(f)
	var logical strings.Builder
	cont, start := false, 0
	ln := 0
	emit := func(ll lintLine) {
		reasons, recs, extras = lintOneLine(ll, userState, reasons, recs, extras)
		userState = updateUserState(ll.text, userState)
	}
	for sc.Scan() {
		raw := strings.TrimRight(sc.Text(), "\r")
		ln++
		if cont {
			logical.WriteByte(' ')
			logical.WriteString(strings.TrimLeft(raw, " \t"))
		} else {
			logical.Reset()
			logical.WriteString(raw)
			start = ln
		}
		text := logical.String()
		if strings.HasSuffix(text, "\\") {
			logical.Reset()
			logical.WriteString(strings.TrimRight(strings.TrimSuffix(text, "\\"), " \t"))
			cont = true
			continue
		}
		cont = false
		emit(lintLine{text: text, line: start})
	}
	if cont {
		emit(lintLine{text: logical.String(), line: start})
	}
	return reasons, recs, extras
}

// lintOneLine checks one logical line, appending refusals and records.
func lintOneLine(ll lintLine, userState string, reasons []string, recs []RecipeRecord, extras []string) ([]string, []RecipeRecord, []string) {
	line := strings.TrimSpace(ll.text)
	if line == "" {
		return reasons, recs, extras
	}
	if strings.HasPrefix(line, "#") {
		if m := extraRe.FindStringSubmatch(line); m != nil {
			p := strings.TrimSpace(m[1])
			if !strings.HasPrefix(p, "/") {
				reasons = append(reasons, fmt.Sprintf("line %d: '# extra: %s' is not an absolute path", ll.line, p))
			} else {
				extras = append(extras, p)
				recs = append(recs, RecipeRecord{Kind: "extra", Text: p})
			}
		}
		return reasons, recs, extras
	}
	dir := line
	rest := ""
	if i := strings.IndexAny(line, " \t"); i >= 0 {
		dir, rest = line[:i], strings.TrimLeft(line[i:], " \t")
	}
	switch strings.ToUpper(dir) {
	case "RUN":
		recs = append(recs, RecipeRecord{Kind: "run", Text: rest})
		reasons = lintRun(rest, ll.line, reasons)
	case "ENV":
		recs = append(recs, RecipeRecord{Kind: "env", Text: rest})
		reasons = lintEnv(rest, ll.line, reasons)
	case "USER":
		recs = append(recs, RecipeRecord{Kind: "user", Text: rest})
		reasons = lintUser(rest, ll.line, userState, reasons)
	case "ARG":
		reasons = append(reasons, fmt.Sprintf("line %d: ARG is refused — build args bake out-of-band state invisible to the content hash (the CACHEBUST lesson)", ll.line))
	case "COPY":
		reasons = append(reasons, fmt.Sprintf("line %d: COPY is refused — recipes carry no file content; declare author-declared files with '# extra: <absolute path>'", ll.line))
	case "WORKDIR":
		reasons = append(reasons, fmt.Sprintf("line %d: WORKDIR is refused — base owns the workdir", ll.line))
	case "ENTRYPOINT":
		reasons = append(reasons, fmt.Sprintf("line %d: ENTRYPOINT is refused — the composition footer owns it", ll.line))
	default:
		reasons = append(reasons, fmt.Sprintf("line %d: unknown directive '%s' — allowed: RUN, ENV, USER, comments and '# extra: <absolute path>'", ll.line, dir))
	}
	return reasons, recs, extras
}

var extraRe = regexp.MustCompile(`^#[[:space:]]extra:[[:space:]]*(.+)$`)

// updateUserState applies a logical USER line to the monotonic root→dev
// state (an invalid spec leaves the state untouched — the refusal already
// fired).
func updateUserState(line, state string) string {
	fields := strings.Fields(line)
	if len(fields) < 2 || !strings.EqualFold(fields[0], "USER") {
		return state
	}
	name := fields[1]
	if i := strings.IndexByte(name, ':'); i >= 0 {
		name = name[:i]
	}
	switch name {
	case "root":
		if state != "dev" {
			return "root"
		}
	case "dev":
		return "dev"
	}
	return state
}

func lintUser(spec string, ln int, state string, reasons []string) []string {
	name := spec
	if i := strings.IndexAny(name, ":\t "); i >= 0 {
		name = name[:i]
	}
	switch name {
	case "root":
		if state == "dev" {
			reasons = append(reasons, fmt.Sprintf("line %d: USER switch-back to root after dev — user switching is monotonic root→dev", ln))
		}
	case "dev":
	default:
		reasons = append(reasons, fmt.Sprintf("line %d: USER '%s' is refused — only root and dev are allowed (monotonic root→dev)", ln, spec))
	}
	return reasons
}

func lintEnv(rest string, ln int, reasons []string) []string {
	toks := strings.Fields(rest)
	var keys []string
	if len(toks) == 0 {
		return reasons
	}
	if !strings.Contains(toks[0], "=") {
		// legacy 'ENV KEY value' form — the first token is the only key
		keys = toks[:1]
	} else {
		for _, tok := range toks {
			if strings.Contains(tok, "=") {
				keys = append(keys, strings.SplitN(tok, "=", 2)[0])
			}
		}
	}
	for _, key := range keys {
		if strings.EqualFold(key, "PATH") {
			reasons = append(reasons, fmt.Sprintf("line %d: ENV PATH is refused — PATH additions ride base's ~/.profile.d/<id>.sh hook", ln))
		}
	}
	return reasons
}

var urlRe = regexp.MustCompile(`https?://[^"' ]+`)
var latestSegRe = regexp.MustCompile(`/latest(/|$)`)

func lintRun(rest string, ln int, reasons []string) []string {
	// profile/bashrc direct writes
	touched := ""
	for _, word := range strings.Fields(rest) {
		switch word {
		case "~/.profile", "~/.bashrc", "$HOME/.profile", "$HOME/.bashrc", "/home/dev/.profile", "/home/dev/.bashrc":
			touched = word
		}
	}
	if touched != "" && (strings.Contains(rest, ">>") || strings.Contains(rest, ">") || strings.Contains(rest, "tee") || strings.Contains(rest, "sed -i")) {
		reasons = append(reasons, fmt.Sprintf("line %d: direct write to %s is refused — write a ~/.profile.d/<id>.sh hook instead", ln, touched))
	}
	// direct URLs must embed the version
	for _, url := range urlRe.FindAllString(rest, -1) {
		if latestSegRe.MatchString(url) || strings.Contains(url, "/releases/latest") {
			reasons = append(reasons, fmt.Sprintf("line %d: URL '%s' floats on 'latest' — embed the exact version (no latest segments, no release-API queries)", ln, url))
		}
	}
	// rustup pins (whole RUN so the curl-piped install.sh form is covered)
	if strings.Contains(rest, "rustup") {
		if strings.Contains(rest, "rustup-init") || strings.Contains(rest, "sh.rustup.rs") {
			reasons = lintRustupInit(rest, ln, reasons)
		} else if strings.Contains(rest, "rustup default") || strings.Contains(rest, "rustup toolchain") {
			reasons = append(reasons, fmt.Sprintf("line %d: 'rustup default'/'rustup toolchain' at run time is not pinned — bake --default-toolchain <version> into the rustup install", ln))
		}
	}
	// pip / npm pins, per command segment
	for _, seg := range splitRunSegments(rest) {
		seg = strings.TrimSpace(seg)
		if seg == "" {
			continue
		}
		reasons = lintRunSegment(seg, ln, reasons)
	}
	return reasons
}

// splitRunSegments splits a RUN line into command segments on the shell
// separators &&, ||, ; and | (bash's sed-based split).
func splitRunSegments(rest string) []string {
	r := strings.NewReplacer("&&", "\n", "||", "\n", ";", "\n", "|", "\n")
	return strings.Split(r.Replace(rest), "\n")
}

func lintRunSegment(seg string, ln int, reasons []string) []string {
	fields := strings.Fields(seg)
	if len(fields) == 0 {
		return reasons
	}
	cmd := fields[0]
	if i := strings.LastIndexByte(cmd, '/'); i >= 0 {
		cmd = cmd[i+1:]
	}
	var args []string
	isPip, isNpm := false, false
	switch cmd {
	case "pip", "pip3":
		isPip, args = true, fields[1:]
	case "python3", "python":
		if len(fields) > 2 && fields[1] == "-m" && regexp.MustCompile(`^pip3?$`).MatchString(fields[2]) {
			isPip, args = true, fields[3:]
		}
	case "npm":
		isNpm, args = true, fields[1:]
	default:
		return reasons
	}
	if isPip && len(args) > 0 && args[0] == "install" {
		skipNext := false
		for _, tok := range args[1:] {
			if skipNext {
				skipNext = false
				continue
			}
			switch {
			case slices.Contains([]string{"-r", "--requirement", "-e", "--editable", "--target", "-t", "--prefix"}, tok):
				skipNext = true
			case strings.HasPrefix(tok, "-"):
			case !strings.Contains(tok, "==") && !strings.Contains(tok, "@") && !strings.Contains(tok, "://"):
				reasons = append(reasons, fmt.Sprintf("line %d: pip package '%s' is not pinned — use '%s==x.y.z'", ln, tok, tok))
			}
		}
	}
	if isNpm && len(args) > 0 && (args[0] == "install" || args[0] == "i") {
		skipNext := false
		for _, tok := range args[1:] {
			switch {
			case strings.HasPrefix(tok, "--") && strings.Contains(tok, "="):
			case strings.HasPrefix(tok, "--"):
				skipNext = true
			case strings.HasPrefix(tok, "-"):
			case skipNext:
				skipNext = false
			case !regexp.MustCompile(`@[0-9]`).MatchString(tok):
				reasons = append(reasons, fmt.Sprintf("line %d: npm package '%s' is not pinned — use '%s@x.y.z'", ln, tok, tok))
			}
		}
	}
	return reasons
}

var floatToolchainRe = regexp.MustCompile(`^(stable|nightly|beta)(-[^0-9].*)?$`)

func lintRustupInit(rest string, ln int, reasons []string) []string {
	has, val, pend := false, "", false
	for _, tok := range strings.Fields(rest) {
		switch {
		case tok == "--default-toolchain":
			has, pend = true, true
		case strings.HasPrefix(tok, "--default-toolchain="):
			has = true
			val = strings.TrimPrefix(tok, "--default-toolchain=")
			pend = false
		default:
			if pend {
				val = tok
				pend = false
			}
		}
	}
	switch {
	case !has:
		reasons = append(reasons, fmt.Sprintf("line %d: rustup install is not pinned — pass --default-toolchain <version>", ln))
	case floatToolchainRe.MatchString(val):
		reasons = append(reasons, fmt.Sprintf("line %d: rustup --default-toolchain '%s' is not a pinned toolchain — use an exact version", ln, val))
	}
	return reasons
}

// LintRecipe lints one recipe file and returns the refusal reasons —
// empty when the recipe is valid (the bash `toolchains.sh lint` rules).
func LintRecipe(path string) []string {
	reasons, _, _ := lintRecipe(path)
	return reasons
}

// ParseRecipe parses a valid recipe into its run/env/user/extra records
// (the bash `toolchains.sh parse` output); it refuses an invalid recipe —
// the component-Dockerfile generator builds fragments from these records.
func ParseRecipe(path string) ([]RecipeRecord, error) {
	reasons, recs, _ := lintRecipe(path)
	if len(reasons) > 0 {
		return nil, fmt.Errorf("%s is not a valid toolchain recipe: %s", path, strings.Join(reasons, "; "))
	}
	return recs, nil
}

// ExtrasRecipe returns a valid recipe's collected '# extra:' absolute paths
// (the bash `toolchains.sh extras` output — the dump step's declared-file
// input); it refuses an invalid recipe.
func ExtrasRecipe(path string) ([]string, error) {
	reasons, _, extras := lintRecipe(path)
	if len(reasons) > 0 {
		return nil, fmt.Errorf("%s is not a valid toolchain recipe: %s", path, strings.Join(reasons, "; "))
	}
	return extras, nil
}

// --- image refs ----------------------------------------------------------------

// RefKind classifies a podman image reference for the toolchain-sets world
// (spec toolchain-sets §6/§10).
type RefKind string

const (
	// RefUnrelated: not a sandbox-family ref (host images, dangling <none>).
	RefUnrelated RefKind = ""
	// RefLegacy: an old-world `opencode-sandbox-<edition>` ref that is no
	// current base/tc/set tag — the §10 Legacy group.
	RefLegacy RefKind = "legacy"
	// RefBase: the base image, tagged `opencode-sandbox-base[:<hash12>]`
	// (the untagged form is the old-world base).
	RefBase RefKind = "base"
	// RefToolchain: a component image `opencode-sandbox-tc-<id>[:<hash12>]`.
	RefToolchain RefKind = "tc"
	// RefSet: an assembled set image `opencode-sandbox-set[:<hash12>]`.
	RefSet RefKind = "set"
)

// ImageRef is a parsed sandbox image reference: the kind, the toolchain id
// (RefToolchain) or the old-world edition name (RefLegacy), and the
// tag/digest suffix ("" when untagged — the old-world forms).
type ImageRef struct {
	Kind RefKind
	ID   string
	Tag  string
}

// ParseImageRef parses an image reference by its last path segment's
// `opencode-sandbox-*` marker (registry prefix and tag/digest suffix
// stripped): base / `tc-<id>` / set tags are the current family tags,
// every other opencode-sandbox-* ref is Legacy (the old-world edition
// images), and refs without the marker are unrelated host images.
func ParseImageRef(ref string) ImageRef {
	i := strings.LastIndex(ref, "opencode-sandbox-")
	if i < 0 {
		return ImageRef{Kind: RefUnrelated}
	}
	suffix := ref[i+len("opencode-sandbox-"):]
	name := suffix
	tag := ""
	if j := strings.IndexAny(suffix, ":@"); j >= 0 {
		name, tag = suffix[:j], suffix[j+1:]
	}
	switch {
	case name == "base":
		return ImageRef{Kind: RefBase, Tag: tag}
	case name == "set":
		return ImageRef{Kind: RefSet, Tag: tag}
	case strings.HasPrefix(name, "tc-"):
		if id := strings.TrimPrefix(name, "tc-"); id != "" {
			return ImageRef{Kind: RefToolchain, ID: id, Tag: tag}
		}
	}
	return ImageRef{Kind: RefLegacy, ID: name, Tag: tag}
}

// ManifestBundles returns the manifest's bundle map (name → member ids,
// unordered, exactly as stored) — the legacy container_edition expansion
// mapping (spec toolchain-sets §3/§10). A missing or unparseable
// manifest returns nil (every legacy read then resolves to nothing but
// base).
func ManifestBundles(root string) map[string][]string {
	raw, err := os.ReadFile(filepath.Join(root, "manifest.json"))
	if err != nil {
		return nil
	}
	var manifest struct {
		Bundles map[string][]string `json:"bundles"`
	}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return nil
	}
	return manifest.Bundles
}

// --- discovery -----------------------------------------------------------------

// Unit is one discovered toolchain recipe (recipes/ shipped first, custom/
// user recipes appended, a user/shipped id collision skipped — shipped
// wins). Invalid units render visibly-with-reason.
type Unit struct {
	ID     string
	Valid  bool
	Path   string // recipe path (valid units)
	Reason string // first refusal (invalid units)
}

// Bundle is one manifest bundle: a named, unordered member list rendered in
// the canonical sorted order. Invalid bundles name why.
type Bundle struct {
	Name    string
	Valid   bool
	Members []string // sorted member ids (valid bundles)
	Reason  string   // why invalid (missing/invalid member, bad name)
}

// Catalog is one live discovery render over a toolchains dir: the units
// sorted by id, the manifest bundles sorted by name, and the warnings the
// bash discover records carry.
type Catalog struct {
	Units    []Unit
	Bundles  []Bundle
	Warnings []string
}

// Unit returns the discovered unit with this id.
func (c Catalog) Unit(id string) (Unit, bool) {
	for _, u := range c.Units {
		if u.ID == id {
			return u, true
		}
	}
	return Unit{}, false
}

// Bundle returns the discovered bundle with this name.
func (c Catalog) Bundle(name string) (Bundle, bool) {
	for _, b := range c.Bundles {
		if b.Name == name {
			return b, true
		}
	}
	return Bundle{}, false
}

var (
	unitIDRe     = regexp.MustCompile(`^[a-z0-9-]+$`)
	bundleNameRe = regexp.MustCompile(`^[a-z0-9-]+$`)
)

// Discover lists, parses, and lints the toolchains dir live at each call
// (the bash `toolchains.sh discover` semantics): recipes/ first, custom/
// appended — a user/shipped id collision skips the user recipe with a
// warning (shipped wins) — and the bundles-only manifest read. A missing
// or unparseable manifest invalidates all bundles with one warning; a
// member without a (valid) recipe invalidates its bundle naming the
// member; invalid recipes render visibly-with-reason.
func Discover(root string) Catalog {
	c := Catalog{}
	ids := map[string]Unit{} // discovery order → map, rendered sorted
	add := func(dir string) {
		matches, err := filepath.Glob(filepath.Join(root, dir, "*.Dockerfile"))
		if err != nil {
			return
		}
		slices.Sort(matches)
		for _, path := range matches {
			id := strings.TrimSuffix(filepath.Base(path), ".Dockerfile")
			if dir == "custom" {
				if _, seen := ids[id]; seen {
					c.Warnings = append(c.Warnings, fmt.Sprintf("user recipe custom/%s.Dockerfile collides with the shipped recipe of the same id — shipped wins, the user recipe is skipped", id))
					continue
				}
			}
			u := Unit{ID: id, Valid: true, Path: path}
			switch {
			case !unitIDRe.MatchString(id):
				u.Valid, u.Reason = false, fmt.Sprintf("invalid toolchain id '%s' — ids are lowercase [a-z0-9-]", id)
			case id == "base":
				u.Valid, u.Reason = false, "'base' is a reserved id — base is fixed, not a unit"
			default:
				if reasons := LintRecipe(path); len(reasons) > 0 {
					u.Valid, u.Reason = false, reasons[0]
				}
			}
			ids[id] = u
		}
	}
	add("recipes")
	add("custom")
	for _, id := range slices.Sorted(maps.Keys(ids)) {
		u := ids[id]
		c.Units = append(c.Units, u)
		if !u.Valid {
			c.Warnings = append(c.Warnings, fmt.Sprintf("toolchain '%s' is invalid — %s", id, u.Reason))
		}
	}
	c.readManifest(root, ids)
	return c
}

func (c *Catalog) readManifest(root string, units map[string]Unit) {
	raw, err := os.ReadFile(filepath.Join(root, "manifest.json"))
	if err != nil {
		c.Warnings = append(c.Warnings, "manifest.json is missing or unreadable — all bundles are invalidated")
		return
	}
	var manifest struct {
		Bundles map[string][]string `json:"bundles"`
	}
	if err := json.Unmarshal(raw, &manifest); err != nil || manifest.Bundles == nil {
		c.Warnings = append(c.Warnings, "manifest.json is missing or unreadable — all bundles are invalidated")
		return
	}
	for _, name := range slices.Sorted(maps.Keys(manifest.Bundles)) {
		members := manifest.Bundles[name]
		b := Bundle{Name: name, Valid: true}
		if !bundleNameRe.MatchString(name) {
			b.Valid = false
			b.Reason = fmt.Sprintf("invalid bundle name '%s' — names are lowercase [a-z0-9-]", name)
		} else {
			var bad []string
			for _, m := range members {
				u, ok := units[m]
				switch {
				case !ok:
					bad = append(bad, fmt.Sprintf("member '%s' has no recipe", m))
				case !u.Valid:
					bad = append(bad, fmt.Sprintf("member '%s' is invalid — %s", m, u.Reason))
				}
			}
			if len(bad) > 0 {
				b.Valid = false
				b.Reason = strings.Join(bad, "; ")
			} else {
				b.Members = slices.Sorted(slices.Values(members))
			}
		}
		c.Bundles = append(c.Bundles, b)
		if !b.Valid {
			c.Warnings = append(c.Warnings, fmt.Sprintf("bundle '%s' is invalidated — %s", name, b.Reason))
		}
	}
}
