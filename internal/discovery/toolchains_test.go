package discovery

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/shredingerlabs/oc-sandbox/internal/cmdrun"
)

// Toolchain recipe lint/parse/discovery (spec toolchain-sets §2) — the Go
// seam beside the bash helper (dist/scripts/toolchains.sh, #195); the
// agreement test at the bottom pins both to the same behavior.

func writeRecipe(t *testing.T, dir, id, body string) string {
	t.Helper()
	path := filepath.Join(dir, id+".Dockerfile")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

const validRecipe = `# go toolchain (shipped)
USER root
RUN apt-get update \
 && apt-get install -y --no-install-recommends golang-go
USER dev
RUN printf 'export PATH=/usr/local/go/bin:$PATH\n' > ~/.profile.d/go.sh
`

func TestLintRecipeValid(t *testing.T) {
	dir := t.TempDir()
	p := writeRecipe(t, dir, "go", validRecipe)
	if reasons := LintRecipe(p); len(reasons) != 0 {
		t.Fatalf("LintRecipe(valid) = %q, want none", reasons)
	}
}

func TestLintRecipeRefusals(t *testing.T) {
	cases := []struct{ name, body, want string }{
		{"arg", "USER root\nARG CACHEBUST=1\nRUN apt-get update\nUSER dev\n", "ARG is refused"},
		{"copy", "USER root\nCOPY something /\nUSER dev\n", "COPY is refused"},
		{"workdir", "USER root\nWORKDIR /opt\nUSER dev\n", "WORKDIR is refused"},
		{"entrypoint", "USER root\nENTRYPOINT [\"/bin/bash\"]\nUSER dev\n", "ENTRYPOINT is refused"},
		{"unknown", "USER root\nLABEL maintainer=x\nUSER dev\n", "unknown directive 'LABEL'"},
		{"switchback", "USER root\nUSER dev\nUSER root\n", "switch-back"},
		{"baduser", "USER someuser\n", "only root and dev"},
		{"envpath", "ENV PATH=/opt/tool/bin:$PATH\n", "ENV PATH is refused"},
		{"envpathlegacy", "ENV PATH /opt/tool/bin\n", "ENV PATH is refused"},
		{"profilewrite", "RUN echo 'export FOO=1' >> ~/.profile\n", "write a ~/.profile.d/"},
		{"bashrcwrite", "RUN printf 'source /opt/ros/setup.bash\\n' > /home/dev/.bashrc\n", "write a ~/.profile.d/"},
		{"pippin", "RUN pip install --no-cache-dir numpy scipy\n", "pip package 'numpy' is not pinned"},
		{"pythonpippin", "RUN python3 -m pip install mpremote\n", "pip package 'mpremote' is not pinned"},
		{"npmpin", "RUN npm install -g typescript jest\n", "npm package 'typescript' is not pinned"},
		{"npmlatest", "RUN npm install -g typescript@latest\n", "npm package 'typescript@latest' is not pinned"},
		{"urllatest", "RUN curl -fsSL https://example.com/download/latest/tool.tar.gz | tar xz -C /opt\n", "floats on 'latest'"},
		{"urlapi", "RUN curl -fsSL \"$(curl -fsSL https://api.github.com/repos/cli/cli/releases/latest | jq -r .tag_name)\" -o /tmp/x\n", "releases/latest"},
		{"rustupfloat", "USER dev\nRUN curl https://sh.rustup.rs -sSf | sh -s -- -y --profile minimal --no-modify-path\n", "not pinned — pass --default-toolchain"},
		{"rustupstable", "USER dev\nRUN curl https://sh.rustup.rs -sSf | sh -s -- -y --default-toolchain stable\n", "is not a pinned toolchain"},
		{"rustupdefault", "USER dev\nRUN rustup default stable\n", "rustup default"},
		{"badextra", "# extra: relative/path\n", "not an absolute path"},
		{"rustupinitvar", "USER dev\nRUN curl -fsSL https://sh.rustup.rs -o /tmp/r.sh && sh /tmp/r.sh -y\n", "not pinned"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			p := writeRecipe(t, dir, c.name, c.body)
			reasons := LintRecipe(p)
			if len(reasons) == 0 {
				t.Fatalf("LintRecipe must refuse, got none")
			}
			if !strings.Contains(strings.Join(reasons, "\n"), c.want) {
				t.Fatalf("reasons %q do not name %q", reasons, c.want)
			}
		})
	}
}

func TestLintRecipeAcceptedPins(t *testing.T) {
	cases := map[string]string{
		"pipok":     "RUN pip install --no-cache-dir numpy==2.2.0 scipy==1.14.1\n",
		"npmok":     "RUN npm install -g typescript@5.5.4 @scope/toolkit@1.2.3 corepack@0.28.0\n",
		"urlok":     "RUN curl -fsSL https://example.com/download/v1.2.3/tool.tar.gz -o /tmp/tool.tar.gz\n",
		"rustupok":  "USER dev\nRUN curl https://sh.rustup.rs -sSf | sh -s -- -y --profile minimal --no-modify-path --default-toolchain 1.79.0\n",
		"rustupok2": "USER dev\nRUN curl https://sh.rustup.rs -sSf | sh -s -- -y --default-toolchain 1.79.0\n",
		"aptek":     "USER root\nRUN apt-get update && apt-get install -y golang-go\nUSER dev\n",
		"extras":    "USER root\nRUN apt-get update\n# extra: /etc/apt/sources.list.d/ros2.list\n# extra: /usr/share/keyrings/ros2.gpg\nUSER dev\n",
		"envlit":    "ENV DEBIAN_FRONTEND=noninteractive\nENV OCTAVE_HOME /opt/octave\n",
	}
	for id, body := range cases {
		t.Run(id, func(t *testing.T) {
			dir := t.TempDir()
			p := writeRecipe(t, dir, id, body)
			if reasons := LintRecipe(p); len(reasons) != 0 {
				t.Fatalf("LintRecipe(%s) = %q, want none", id, reasons)
			}
		})
	}
}

func TestLintRecipeMissingFile(t *testing.T) {
	reasons := LintRecipe(filepath.Join(t.TempDir(), "nope.Dockerfile"))
	if len(reasons) != 1 || !strings.Contains(reasons[0], "not found") {
		t.Fatalf("missing-file reasons = %q, want a not-found refusal", reasons)
	}
}

func TestParseRecipeRecords(t *testing.T) {
	dir := t.TempDir()
	p := writeRecipe(t, dir, "parseok", "USER root\nRUN apt-get update \\\n && apt-get install -y octave\nENV OCTAVE_HOME=/opt/octave\n# extra: /etc/octave.conf\nUSER dev\n")
	recs, err := ParseRecipe(p)
	if err != nil {
		t.Fatal(err)
	}
	want := []RecipeRecord{
		{Kind: "user", Text: "root"},
		{Kind: "run", Text: "apt-get update && apt-get install -y octave"},
		{Kind: "env", Text: "OCTAVE_HOME=/opt/octave"},
		{Kind: "extra", Text: "/etc/octave.conf"},
		{Kind: "user", Text: "dev"},
	}
	if !reflect.DeepEqual(recs, want) {
		t.Fatalf("parse records = %+v, want %+v", recs, want)
	}
	// parse refuses invalid recipes (bash parse exits 1 before printing).
	p = writeRecipe(t, dir, "bad", "COPY x /\n")
	if _, err := ParseRecipe(p); err == nil {
		t.Fatal("parse of an invalid recipe must fail")
	}
}

func TestExtrasRecipe(t *testing.T) {
	dir := t.TempDir()
	p := writeRecipe(t, dir, "ros2", "USER root\nRUN apt-get update && apt-get install -y ros-jazzy-ros-base\n# extra: /etc/apt/sources.list.d/ros2.list\n# extra: /usr/share/keyrings/ros2.gpg\nUSER dev\n")
	extras, err := ExtrasRecipe(p)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"/etc/apt/sources.list.d/ros2.list", "/usr/share/keyrings/ros2.gpg"}
	if !reflect.DeepEqual(extras, want) {
		t.Fatalf("extras = %q, want %q", extras, want)
	}
	p = writeRecipe(t, dir, "bad", "# extra: /ok\nRUN pip install x\n")
	if _, err := ExtrasRecipe(p); err == nil {
		t.Fatal("extras of an invalid recipe must fail")
	}
}

// discoverTree writes a toolchains fixture tree and returns its root.
func discoverTree(t *testing.T, recipes, custom map[string]string, manifest string) string {
	t.Helper()
	root := t.TempDir()
	for _, sub := range []string{"recipes", "custom"} {
		if err := os.MkdirAll(filepath.Join(root, sub), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for id, body := range recipes {
		writeRecipe(t, filepath.Join(root, "recipes"), id, body)
	}
	for id, body := range custom {
		writeRecipe(t, filepath.Join(root, "custom"), id, body)
	}
	if manifest != "" {
		if err := os.WriteFile(filepath.Join(root, "manifest.json"), []byte(manifest), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestDiscoverUnitsSortedWithPaths(t *testing.T) {
	root := discoverTree(t, map[string]string{
		"go":  validRecipe,
		"cpp": "USER root\nRUN apt-get update && apt-get install -y build-essential\n",
	}, nil, "")
	c := Discover(root)
	var ids []string
	for _, u := range c.Units {
		ids = append(ids, u.ID)
	}
	if !reflect.DeepEqual(ids, []string{"cpp", "go"}) { // sorted by id
		t.Fatalf("unit ids = %v, want [cpp go]", ids)
	}
	if !c.Units[1].Valid || c.Units[1].Path != filepath.Join(root, "recipes", "go.Dockerfile") {
		t.Fatalf("go unit = %+v, want valid with its recipe path", c.Units[1])
	}
	if u, ok := c.Unit("go"); !ok || !u.Valid {
		t.Fatalf("Unit(go) = %+v ok=%v", u, ok)
	}
	if _, ok := c.Unit("nosuch"); ok {
		t.Fatal("unknown id must not resolve")
	}
	// no manifest → all bundles invalidated with the one warning
	if len(c.Bundles) != 0 || len(c.Warnings) != 1 || !strings.Contains(c.Warnings[0], "manifest.json is missing or unreadable") {
		t.Fatalf("warnings/bundles = %q/%+v", c.Warnings, c.Bundles)
	}
}

func TestDiscoverInvalidUnitVisibleWithReason(t *testing.T) {
	root := discoverTree(t, map[string]string{
		"copy": "USER root\nCOPY something /\nUSER dev\n",
		"base": "USER root\nRUN apt-get update\n",
		"Bad":  "RUN apt-get update\n",
	}, nil, "")
	c := Discover(root)
	for _, want := range []struct{ id, reasonPart string }{
		{"copy", "COPY is refused"},
		{"base", "reserved"},
		{"Bad", "invalid toolchain id 'Bad' — ids are lowercase [a-z0-9-]"},
	} {
		u, ok := c.Unit(want.id)
		if !ok {
			t.Fatalf("unit %s missing", want.id)
		}
		if u.Valid {
			t.Fatalf("unit %s must be invalid", want.id)
		}
		if !strings.Contains(u.Reason, want.reasonPart) {
			t.Fatalf("unit %s reason %q does not name %q", want.id, u.Reason, want.reasonPart)
		}
	}
	// each invalid unit warns (visible-with-reason, never a silent skip),
	// plus the missing-manifest warning
	if len(c.Warnings) != 4 {
		t.Fatalf("warnings = %q, want one per invalid unit + the manifest warning", c.Warnings)
	}
	if !strings.Contains(strings.Join(c.Warnings, "\n"), "toolchain 'copy' is invalid — line 2: COPY is refused") {
		t.Fatalf("warnings must name the invalid units: %q", c.Warnings)
	}
}

func TestDiscoverCollisionShippedWins(t *testing.T) {
	root := discoverTree(t,
		map[string]string{"rust": "USER dev\nRUN ~/.cargo/bin/rustup --version\n"},
		map[string]string{"rust": "USER root\nCOPY stuff /\n"},
		"")
	c := Discover(root)
	u, ok := c.Unit("rust")
	if !ok || !u.Valid || u.Path != filepath.Join(root, "recipes", "rust.Dockerfile") {
		t.Fatalf("rust unit = %+v ok=%v, want the shipped (valid) recipe", u, ok)
	}
	joined := strings.Join(c.Warnings, "\n")
	if !strings.Contains(joined, "user recipe custom/rust.Dockerfile collides with the shipped recipe of the same id — shipped wins, the user recipe is skipped") {
		t.Fatalf("collision warning missing: %q", c.Warnings)
	}
	if len(c.Units) != 1 {
		t.Fatalf("units = %+v, want only the shipped rust", c.Units)
	}
}

func TestDiscoverBundles(t *testing.T) {
	manifest := `{ "bundles": { "web": ["node-dev", "playwright"], "chat": [] } }`
	root := discoverTree(t, map[string]string{
		"node-dev":   "RUN npm install -g typescript@5.5.4\n",
		"playwright": "RUN npx playwright install chromium\n",
	}, nil, manifest)
	c := Discover(root)
	if len(c.Bundles) != 2 {
		t.Fatalf("bundles = %+v", c.Bundles)
	}
	web, ok := c.Bundle("web")
	if !ok || !web.Valid {
		t.Fatalf("web bundle = %+v ok=%v", web, ok)
	}
	// member lists render in the canonical sorted order, whatever the manifest says
	if !reflect.DeepEqual(web.Members, []string{"node-dev", "playwright"}) {
		t.Fatalf("web members = %v, want sorted [node-dev playwright]", web.Members)
	}
	chat, ok := c.Bundle("chat")
	if !ok || !chat.Valid || len(chat.Members) != 0 {
		t.Fatalf("chat (base only) bundle = %+v ok=%v", chat, ok)
	}
	if b, ok := c.Bundle("nosuch"); ok {
		t.Fatalf("unknown bundle = %+v", b)
	}
}

func TestDiscoverUnorderedManifestMembersSameBundle(t *testing.T) {
	sorted := Discover(discoverTree(t, map[string]string{
		"a": "RUN apt-get update\n", "b": "RUN apt-get update\n", "c": "RUN apt-get update\n",
	}, nil, `{ "bundles": { "set": ["c", "a", "b"] } }`)).Bundles[0].Members
	shuffled := Discover(discoverTree(t, map[string]string{
		"a": "RUN apt-get update\n", "b": "RUN apt-get update\n", "c": "RUN apt-get update\n",
	}, nil, `{ "bundles": { "set": ["b", "c", "a"] } }`)).Bundles[0].Members
	if !reflect.DeepEqual(sorted, []string{"a", "b", "c"}) || !reflect.DeepEqual(shuffled, sorted) {
		t.Fatalf("unordered member input must yield the identical canonical bundle: %v vs %v", sorted, shuffled)
	}
}

func TestDiscoverMissingMemberInvalidatesBundle(t *testing.T) {
	root := discoverTree(t, map[string]string{
		"node-dev": "RUN npm install -g typescript@5.5.4\n",
	}, nil, `{ "bundles": { "web": ["node-dev", "playwright"] } }`)
	c := Discover(root)
	web, _ := c.Bundle("web")
	if web.Valid {
		t.Fatal("bundle with a missing member must be invalid")
	}
	if !strings.Contains(web.Reason, "member 'playwright' has no recipe") {
		t.Fatalf("bundle reason = %q", web.Reason)
	}
	if !strings.Contains(strings.Join(c.Warnings, "\n"), "bundle 'web' is invalidated — member 'playwright' has no recipe") {
		t.Fatalf("warnings = %q", c.Warnings)
	}
}

func TestDiscoverInvalidMemberInvalidatesBundle(t *testing.T) {
	root := discoverTree(t, map[string]string{
		"playwright": "USER root\nCOPY browsers /\n",
	}, nil, `{ "bundles": { "web": ["playwright"] } }`)
	c := Discover(root)
	web, _ := c.Bundle("web")
	if web.Valid || !strings.Contains(web.Reason, "member 'playwright' is invalid — line 2: COPY is refused") {
		t.Fatalf("web bundle = %+v", web)
	}
}

func TestDiscoverManifestFailureInvalidatesAllBundles(t *testing.T) {
	for name, manifest := range map[string]string{
		"truncated": `{ "bundles": { "swdev": [`,
		"nobundles": `{ "nope": true }`,
		"wrongtype": `{ "bundles": "web" }`,
	} {
		t.Run(name, func(t *testing.T) {
			root := discoverTree(t, map[string]string{"cpp": "RUN apt-get update\n"}, nil, manifest)
			c := Discover(root)
			if len(c.Bundles) != 0 {
				t.Fatalf("bundles = %+v, want all invalidated away", c.Bundles)
			}
			if !strings.Contains(strings.Join(c.Warnings, "\n"), "manifest.json is missing or unreadable — all bundles are invalidated") {
				t.Fatalf("warnings = %q", c.Warnings)
			}
			// units survive the manifest failure
			if u, ok := c.Unit("cpp"); !ok || !u.Valid {
				t.Fatalf("cpp unit = %+v ok=%v", u, ok)
			}
		})
	}
	root := discoverTree(t, map[string]string{"cpp": "RUN apt-get update\n"}, nil, "")
	if c := Discover(root); len(c.Warnings) != 1 || !strings.Contains(c.Warnings[0], "manifest.json is missing") {
		t.Fatalf("missing manifest warnings = %q", c.Warnings)
	}
}

func TestDiscoverInvalidBundleName(t *testing.T) {
	root := discoverTree(t, map[string]string{"go": "RUN apt-get update\n"}, nil, `{ "bundles": { "Web": ["go"] } }`)
	c := Discover(root)
	b, _ := c.Bundle("Web")
	if b.Valid || !strings.Contains(b.Reason, "invalid bundle name 'Web'") {
		t.Fatalf("bundle = %+v", b)
	}
	if !strings.Contains(strings.Join(c.Warnings, "\n"), "bundle 'Web' is invalidated") {
		t.Fatalf("warnings = %q", c.Warnings)
	}
}

// TestParseImageRef pins the set-tag/ref parsing that replaces the
// edition-suffix read (spec toolchain-sets §6/§10): base / tc-<id> / set
// tags (any registry prefix, tag/digest suffix), and every other
// opencode-sandbox-* ref as Legacy (old-world edition images).
func TestParseImageRef(t *testing.T) {
	cases := []struct {
		ref  string
		want ImageRef
	}{
		{"opencode-sandbox-base", ImageRef{Kind: RefBase}},
		{"opencode-sandbox-base:abc123def456", ImageRef{Kind: RefBase, Tag: "abc123def456"}},
		{"opencode-sandbox-tc-rust", ImageRef{Kind: RefToolchain, ID: "rust"}},
		{"opencode-sandbox-tc-node-dev:abc123def456", ImageRef{Kind: RefToolchain, ID: "node-dev", Tag: "abc123def456"}},
		{"localhost/opencode-sandbox-tc-rust:abc123def456", ImageRef{Kind: RefToolchain, ID: "rust", Tag: "abc123def456"}},
		{"registry.io/team/opencode-sandbox-tc-go@sha256:ff", ImageRef{Kind: RefToolchain, ID: "go", Tag: "sha256:ff"}},
		{"opencode-sandbox-set:abc123def456", ImageRef{Kind: RefSet, Tag: "abc123def456"}},
		{"opencode-sandbox-set", ImageRef{Kind: RefSet}},
		{"localhost/opencode-sandbox-set:abc123def456", ImageRef{Kind: RefSet, Tag: "abc123def456"}},
		// old-world refs: the edition images and the untagged old base line
		{"opencode-sandbox-swdev", ImageRef{Kind: RefLegacy, ID: "swdev"}},
		{"localhost/opencode-sandbox-web:latest", ImageRef{Kind: RefLegacy, ID: "web", Tag: "latest"}},
		{"opencode-sandbox-settle", ImageRef{Kind: RefLegacy, ID: "settle"}},
		{"opencode-sandbox-tc-", ImageRef{Kind: RefLegacy, ID: "tc-"}},
		// non-family refs
		{"alpine:3", ImageRef{Kind: RefUnrelated}},
		{"localhost/oc-proxy:latest", ImageRef{Kind: RefUnrelated}},
	}
	for _, c := range cases {
		if got := ParseImageRef(c.ref); got != c.want {
			t.Errorf("ParseImageRef(%q) = %+v, want %+v", c.ref, got, c.want)
		}
	}
}

func TestSandboxImageTagFamily(t *testing.T) {
	// the sandbox family = every base/tc/set/Legacy opencode-sandbox-*
	// ref plus the infra images — the §10 Legacy group rides on it.
	for _, ref := range []string{
		"opencode-sandbox-base",
		"opencode-sandbox-base:abc123def456",
		"opencode-sandbox-tc-rust:abc123def456",
		"opencode-sandbox-set:abc123def456",
		"localhost/opencode-sandbox-swdev:latest",
		"oc-proxy", "localhost/oc-proxy:latest", "oc-searxng", "oc-stt",
	} {
		if !SandboxImageTag(ref) {
			t.Errorf("SandboxImageTag(%q) = false, want true", ref)
		}
	}
	for _, ref := range []string{"alpine:3", "localhost/my-oc-proxy-x:latest", "<none>:<none>"} {
		if SandboxImageTag(ref) {
			t.Errorf("SandboxImageTag(%q) = true, want false", ref)
		}
	}
}

// TestImageExistsPerTag pins per-tag existence: set/base/component tags are
// full refs checked verbatim (the derived set tag is never name-guessed).
func TestImageExistsPerTag(t *testing.T) {
	run := &cmdrun.Fake{Script: func(name string, args []string) (string, error) {
		if name == "podman" && args[0] == "image" && args[1] == "exists" {
			if args[2] == "opencode-sandbox-set:abc123def456" || args[2] == "opencode-sandbox-tc-rust:abc123def456" {
				return "", nil
			}
		}
		return "", errFake
	}}
	if !ImageExistsTag(run, "opencode-sandbox-set:abc123def456") {
		t.Fatal("existing set tag reported missing")
	}
	if !ImageExistsTag(run, "opencode-sandbox-tc-rust:abc123def456") {
		t.Fatal("existing component tag reported missing")
	}
	if ImageExistsTag(run, "opencode-sandbox-set:deadbeef0000") {
		t.Fatal("missing set tag reported existing")
	}
}

// --- Go ↔ bash agreement --------------------------------------------------------

// bashHelper locates the reference helper (the repo tree runs `go test`
// from the package dir; the helper sits at dist/scripts/toolchains.sh).
func bashHelper(t *testing.T) string {
	t.Helper()
	for _, rel := range []string{"../../dist/scripts/toolchains.sh", "../../../dist/scripts/toolchains.sh"} {
		if _, err := os.Stat(rel); err == nil {
			return rel
		}
	}
	t.Skip("dist/scripts/toolchains.sh not found (not the repo tree)")
	return ""
}

// runBashDiscover runs the bash helper's discover over root.
func runBashDiscover(t *testing.T, helper, root string) []string {
	t.Helper()
	out, err := exec.Command("bash", helper, "discover", root).Output()
	if err != nil {
		t.Fatalf("bash discover: %v", err)
	}
	var recs []string
	for _, l := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
		if l != "" {
			recs = append(recs, l)
		}
	}
	return recs
}

// renderRecords renders a Go Catalog as the bash discover records: the
// scan-time collision warnings first, then the sorted unit records each
// followed by its invalid warning, then any remaining warnings (the
// manifest failure), then the bundle records each followed by their
// invalidation warning — the bash helper's emission order.
func renderRecords(c Catalog) []string {
	var recs []string
	emitted := map[string]bool{}
	for _, w := range c.Warnings {
		if strings.HasPrefix(w, "user recipe custom/") {
			recs = append(recs, "warning\t"+w)
			emitted[w] = true
		}
	}
	for _, u := range c.Units {
		if u.Valid {
			recs = append(recs, "unit\t"+u.ID+"\tvalid\t"+u.Path)
		} else {
			recs = append(recs, "unit\t"+u.ID+"\tinvalid\t"+u.Reason)
			w := "toolchain '" + u.ID + "' is invalid — " + u.Reason
			recs = append(recs, "warning\t"+w)
			emitted[w] = true
		}
	}
	for _, w := range c.Warnings {
		if !emitted[w] && !bundleWarningRe.MatchString(w) {
			recs = append(recs, "warning\t"+w)
			emitted[w] = true
		}
	}
	for _, b := range c.Bundles {
		if b.Valid {
			recs = append(recs, "bundle\t"+b.Name+"\tvalid\t"+strings.Join(b.Members, " "))
		} else {
			recs = append(recs, "bundle\t"+b.Name+"\tinvalid\t"+b.Reason)
			recs = append(recs, "warning\tbundle '"+b.Name+"' is invalidated — "+b.Reason)
		}
	}
	return recs
}

var bundleWarningRe = regexp.MustCompile(`^bundle '.+' is invalidated — `)

// TestDiscoverAgreesWithBash pins the Go discovery to the bash reference
// over one fixture tree exercising the shared semantics: valid and invalid
// units, the reserved id, a user/shipped collision (shipped wins), a
// bundle with a missing member, and an empty (base-only) bundle.
func TestDiscoverAgreesWithBash(t *testing.T) {
	helper := bashHelper(t)
	root := discoverTree(t,
		map[string]string{
			"go":    validRecipe,
			"cpp":   "USER root\nRUN apt-get update && apt-get install -y build-essential\n",
			"copy":  "USER root\nCOPY something /\nUSER dev\n",
			"base":  "USER root\nRUN apt-get update\n",
			"rust":  "USER dev\nRUN ~/.cargo/bin/rustup --version\n",
			"pkm":   "RUN apt-get update\n",
			"empty": "",
		},
		map[string]string{"rust": "USER root\nCOPY stuff /\n"},
		`{ "bundles": { "chat": [], "pkm": ["pkm"], "web": ["node-dev", "playwright"] } }`)
	got := renderRecords(Discover(root))
	want := runBashDiscover(t, helper, root)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Go discovery disagrees with bash:\n got %q\nwant %q", got, want)
	}
}

// TestDiscoverRealCatalog pins the Go discovery to the bash reference
// over the real shipped catalog (dist/toolchains) once ticket #197 lands
// it — skipped while the catalog does not exist yet.
func TestDiscoverRealCatalog(t *testing.T) {
	helper := bashHelper(t)
	root := filepath.Join("..", "..", "dist", "toolchains")
	if _, err := os.Stat(root); err != nil {
		t.Skip("dist/toolchains not shipped yet (#197)")
	}
	got := renderRecords(Discover(root))
	want := runBashDiscover(t, helper, root)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Go discovery disagrees with bash over the real catalog:\n got %q\nwant %q", got, want)
	}
}

// TestLintAgreesWithBash compares Go and bash lint reason lines per recipe.
func TestLintAgreesWithBash(t *testing.T) {
	helper := bashHelper(t)
	dir := t.TempDir()
	files := map[string]string{
		"ok":           validRecipe,
		"arg":          "USER root\nARG CACHEBUST=1\nRUN apt-get update\nUSER dev\n",
		"envpath":      "ENV PATH=/opt/tool/bin:$PATH\n",
		"npmpin":       "RUN npm install -g typescript jest\n",
		"rustupstable": "USER dev\nRUN curl https://sh.rustup.rs -sSf | sh -s -- -y --default-toolchain stable\n",
		"profile":      "RUN echo 'export FOO=1' >> ~/.profile\n",
		"label":        "USER root\nLABEL maintainer=x\nUSER dev\n",
	}
	for id, body := range files {
		writeRecipe(t, dir, id, body)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		path := filepath.Join(dir, e.Name())
		out, err := exec.Command("bash", helper, "lint", path).CombinedOutput()
		if err != nil && len(strings.TrimSpace(string(out))) == 0 {
			t.Fatalf("bash lint %s: %v", e.Name(), err)
		}
		var want []string
		for _, l := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
			if l != "" {
				want = append(want, l)
			}
		}
		got := LintRecipe(path)
		if (len(got) == 0) != (len(want) == 0) {
			t.Errorf("lint %s: Go valid=%v, bash says valid=%v (bash: %q)", e.Name(), len(got) == 0, len(want) == 0, want)
			continue
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("lint %s disagrees:\n got %q\nwant %q", e.Name(), got, want)
		}
	}
}
