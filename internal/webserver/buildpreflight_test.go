package webserver

// Build preflight tests (spec toolchain-sets §7/§9 WP-8, #212): every
// build op path refuses before the builder starts on a low-disk or
// offline host, fully-cached plans never refuse and assembly-only plans
// skip the probe. The fakes are the existing patterns: a scripted
// runner (podman/df answered per call) and a stubbed NetProbe seam —
// the native probe would dial real hosts.

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/shredingerlabs/oc-sandbox/internal/cmdrun"
	"github.com/shredingerlabs/oc-sandbox/internal/identity"
	"github.com/shredingerlabs/oc-sandbox/internal/registry"
)

// preflightRunner is a cmdrun.Fake pre-wired for preflight tests:
// nothing is built (no images), df answers freeSpace bytes, and every
// build-images.sh invocation is recorded and fails loudly — a preflight
// refusal must never reach the builder.
func preflightRunner(t *testing.T, freeSpace string) (*cmdrun.Fake, *[]string) {
	t.Helper()
	calls := &[]string{}
	fake := &cmdrun.Fake{Script: func(name string, args []string) (string, error) {
		rest := strings.Join(args, " ")
		switch {
		case name == "df":
			if freeSpace == "" {
				return "", fmt.Errorf("df failed") // unreadable → the disk check skips
			}
			return "Avail\n" + freeSpace + "\n", nil
		case name == "podman": // no images exist; every existence check misses
			return "", fmt.Errorf("no such image")
		case name == "bash" && len(args) > 0 && strings.HasSuffix(args[0], "build-images.sh"):
			*calls = append(*calls, rest)
			return "", fmt.Errorf("the builder must not run behind a refused preflight")
		}
		return "", nil
	}}
	return fake, calls
}

// The row build's disk preflight (§7 item 2): a low-disk host refuses
// before the builder runs — the reason names both numbers (free vs
// needed = estimate + 20 % headroom) and points at Clean intermediates.
func TestImagesRowBuildDiskPreflightRefuses(t *testing.T) {
	reg, _ := fixtureRegistry(t, "alpha")
	seedToolchains(t, reg.Home)
	run, builds := preflightRunner(t, "1073741824") // 1 GiB free
	s := depsServer(t, run, reg)

	resp := post(t, s.URL()+"images/build", "tag=go")
	body := readAll(t, resp)
	for _, want := range []string{
		"refusing to build:",
		"1.0 GiB free is below the 3.6 GiB needed",
		"3.0 GiB estimate + 20% headroom",
		"Clean intermediates",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("row-build disk refusal missing %q:\n%s", want, body)
		}
	}
	if len(*builds) != 0 {
		t.Fatalf("build-images.sh ran %v behind a refused preflight", *builds)
	}
}

// The row build's network preflight (§7 item 3): a download-needing
// plan on an offline host refuses naming the uncached units, the
// unreachable hosts and the escape ("connect and retry, or Build
// later") — and the probe saw the plan's mapped source hosts
// (docker.io for the base pull plus the recipe's apt sources).
func TestImagesRowBuildOfflinePreflightRefuses(t *testing.T) {
	reg, _ := fixtureRegistry(t, "alpha")
	seedToolchains(t, reg.Home)
	run, builds := preflightRunner(t, "")
	probed := []string{}
	s := depsServer(t, run, reg)
	s.deps.NetProbe = func(hosts []string) []string {
		probed = hosts
		return []string{"archive.ubuntu.com", "security.ubuntu.com"}
	}

	resp := post(t, s.URL()+"images/build", "tag=go")
	body := readAll(t, resp)
	for _, want := range []string{
		"refusing to build — the base image and toolchains go need downloads;",
		"cannot reach: archive.ubuntu.com, security.ubuntu.com",
		"Connect and retry, or Build later.",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("row-build offline refusal missing %q:\n%.400s", want, body)
		}
	}
	if got := strings.Join(probed, ","); got != "archive.ubuntu.com,docker.io,security.ubuntu.com" {
		t.Fatalf("probe hosts = %q, want the plan's mapped sources", got)
	}
	if len(*builds) != 0 {
		t.Fatalf("build-images.sh ran %v behind a refused preflight", *builds)
	}
}

// The base row build's offline refusal names the base image alone.
func TestImagesBaseBuildOfflineRefuses(t *testing.T) {
	reg, _ := fixtureRegistry(t, "alpha")
	seedToolchains(t, reg.Home)
	run, builds := preflightRunner(t, "")
	s := depsServer(t, run, reg)
	s.deps.NetProbe = func([]string) []string { return []string{"docker.io", "archive.ubuntu.com"} }

	resp := post(t, s.URL()+"images/build", "tag=base")
	body := readAll(t, resp)
	if !strings.Contains(body, "refusing to build — the base image needs downloads; cannot reach: docker.io") {
		t.Fatalf("base-build offline refusal wrong:\n%.400s", body)
	}
	if len(*builds) != 0 {
		t.Fatalf("build-images.sh ran %v behind a refused preflight", *builds)
	}
}

// Build now's Go preflight (#212): an offline host refuses BEFORE the
// builder runs — the op fails, nothing starts, the workspace stays
// stopped with the refusal in the busy pane (the builder's own
// preflight refusal test covers the streamed-script case).
func TestStartBuildNowOfflinePreflightRefuses(t *testing.T) {
	reg, roots := fixtureRegistry(t, "alpha")
	root := roots["alpha"]
	seedToolchains(t, reg.Home)
	seedToolchainsConfig(t, root, []string{"go"}, nil)
	startCalls := &syncList{}
	run := fakePodman(nil, nil, nil, nil, nil)
	base := run.Script
	run.Script = func(name string, args []string) (string, error) {
		if name == "bash" && len(args) > 0 && args[0] == "/opt/oc-sandbox/scripts/start.sh" {
			startCalls.add("start")
			return "started", nil
		}
		return base(name, args)
	}
	s := depsServer(t, run, reg)
	s.deps.NetProbe = func([]string) []string {
		return []string{"archive.ubuntu.com", "docker.io"}
	}

	v := postSidecar(t, s.URL()+"workspaces/alpha/start", "variant=web&build=1")
	if v["ok"] != true {
		t.Fatalf("start sidecar = %v, want ok:true (the refusal surfaces in the op)", v)
	}
	waitFor(t, "op to drain", func() bool { return !s.Busy() })
	if calls := run.Snapshot(); slices.ContainsFunc(calls, func(c cmdrun.Call) bool {
		return c.Name == "bash" && len(c.Args) > 0 && strings.HasSuffix(c.Args[0], "build-images.sh")
	}) {
		t.Fatal("build-images.sh ran behind a refused preflight")
	}
	if len(startCalls.snapshot()) != 0 {
		t.Fatalf("start.sh ran despite the refused build: %v", startCalls.snapshot())
	}
	if last := s.queue.LastError(registry.ContainerName(registry.ContainerIdentity(root))); !strings.Contains(last, "refusing to build") ||
		!strings.Contains(last, "Build later") {
		t.Fatalf("busy pane error = %q, want the preflight refusal", last)
	}
}

// The mismatch switch's inline Build now rides the same preflight
// (#212): an offline host refuses before anything stops — the running
// container is never touched ("never stop without a replacement").
func TestAdoptSwitchOfflinePreflightRefuses(t *testing.T) {
	reg, roots := fixtureRegistry(t, "alpha")
	root := roots["alpha"]
	seedToolchains(t, reg.Home)
	sandboxConfigFixture(t, root)
	if err := reg.UpdateStatus(root, "running"); err != nil {
		t.Fatal(err)
	}
	id := registry.ContainerIdentity(root)
	cname := "opencode-sandbox-" + id
	running := newSyncSet(cname)
	stopCalls := &syncList{}
	run := fakePodman(nil, nil, nil, []string{"opencode-sandbox-swdev"}, nil)
	base := run.Script
	run.Script = func(name string, args []string) (string, error) {
		rest := ""
		if name == "podman" {
			rest = strings.Join(args, " ")
		}
		switch {
		case name == "podman" && strings.HasPrefix(rest, "image exists"):
			return "", fmt.Errorf("no such image") // the set image is missing — the switch must build
		case name == "podman" && strings.HasPrefix(rest, "stop "):
			stopCalls.add(args[1])
			running.set(cname, false)
			return "stopped " + cname, nil
		case name == "podman" && strings.HasPrefix(rest, "ps --format {{.Names}}\t{{.Image}}"):
			return cname + "\topencode-sandbox-web:latest", nil // runs web, saved is swdev
		case name == "podman" && strings.HasPrefix(rest, "ps --format {{.Names}}"):
			return strings.Join(running.keys(), "\n"), nil
		}
		return base(name, args)
	}
	s := depsServer(t, run, reg)
	s.deps.NetProbe = func([]string) []string {
		return []string{"docker.io", "archive.ubuntu.com", "security.ubuntu.com"}
	}

	v := postSidecar(t, s.URL()+"workspaces/alpha/adopt", "variant=web")
	if v["ok"] != true {
		t.Fatalf("adopt sidecar = %v, want ok:true (the refusal surfaces in the op)", v)
	}
	waitFor(t, "op to drain", func() bool { return !s.Busy() })
	if len(stopCalls.snapshot()) != 0 {
		t.Fatalf("the switch stopped %v behind a refused preflight", stopCalls.snapshot())
	}
	ws, err := reg.ByName("alpha")
	if err != nil {
		t.Fatal(err)
	}
	if ws.ContainerStatus != "running" {
		t.Fatalf("registry flipped despite the refused build: %q", ws.ContainerStatus)
	}
	if last := s.queue.LastError(cname); !strings.Contains(last, "refusing to build") {
		t.Fatalf("busy pane error = %q, want the preflight refusal", last)
	}
}

// The create chain's Build now rides the same preflight (#212): an
// offline host refuses at the build step — the chain fails before
// init-workspace.sh, nothing is registered, the refusal streams.
func TestCreateChainBuildNowOfflinePreflightRefuses(t *testing.T) {
	f := newCreateFixture(t, imageExistsScript(false))
	f.s.deps.NetProbe = func([]string) []string {
		return []string{"archive.ubuntu.com", "security.ubuntu.com"}
	}
	sse := sseReader(t, f.s)
	vals := f.blankForm()
	vals.Set("build", "now")
	f.confirm(t, vals)
	state, lines := waitDone(t, sse)
	if state != "failed" {
		t.Fatalf("refused build: chain state = %s, want failed", state)
	}
	if !slices.ContainsFunc(lines, func(l string) bool {
		return strings.Contains(l, "refusing to build") && strings.Contains(l, "Build later")
	}) {
		t.Errorf("refusal not streamed: %v", lines)
	}
	if calls := bashCalls(f); len(calls) != 0 {
		t.Fatalf("build-images.sh ran behind a refused preflight: %v", calls)
	}
	reg := &registry.Registry{Home: f.home}
	if _, err := reg.ByName("demo"); err == nil {
		t.Fatal("refused build must not register the workspace")
	}
}

// A fully-cached plan never refuses and never probes (§7): every row
// build rides straight to the builder (the builder skips cached tags).
func TestImagesRowBuildCachedPlanNeverRefuses(t *testing.T) {
	reg, _ := fixtureRegistry(t, "alpha")
	seedToolchains(t, reg.Home)
	run, builds := preflightRunner(t, "1000") // 1000 bytes free — would refuse any build
	probes := 0
	s := depsServer(t, run, reg)
	run.Script = func(name string, args []string) (string, error) {
		rest := strings.Join(args, " ")
		switch {
		case name == "podman" && strings.HasPrefix(rest, "image exists"):
			return "", nil // everything cached
		case name == "podman" && strings.HasPrefix(rest, "images --format"):
			return "opencode-sandbox-base:abc123def456\t1.9 GB\n" +
				"opencode-sandbox-tc-go:abc123def456\t310 MB\n", nil
		case name == "bash" && len(args) > 0 && strings.HasSuffix(args[0], "build-images.sh"):
			*builds = append(*builds, rest)
			return "skipped — already exists", nil
		}
		return "", nil
	}
	s.deps.NetProbe = func([]string) []string { probes++; return nil }

	resp := post(t, s.URL()+"images/build", "tag=go")
	if body := readAll(t, resp); strings.Contains(body, "refusing to build") {
		t.Fatalf("a fully-cached plan must never refuse:\n%.400s", body)
	}
	if probes != 0 {
		t.Fatalf("a fully-cached plan must not probe (%d probes)", probes)
	}
	waitFor(t, "builder queued", func() bool { return len(*builds) == 1 })
}

// --- the native probe (§7 item 3) -------------------------------------------

// fakeProxy answers proxied HEADs and records the hosts the probe
// aimed at (the proxy request line carries the target host).
func fakeProxy(t *testing.T, fail bool) (*httptest.Server, func() []string) {
	t.Helper()
	var mu sync.Mutex
	var hosts []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hosts = append(hosts, r.Host)
		mu.Unlock()
		if fail {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	return srv, func() []string { mu.Lock(); defer mu.Unlock(); return hosts }
}

// proxyAt returns a ProxyFromEnvironment-shaped resolver pointing at
// the fake proxy — the seam that keeps the probe testable (the stdlib
// resolver snapshots the environment on first use, so env-based
// isolation is impossible).
func proxyAt(srv *httptest.Server) func(*http.Request) (*url.URL, error) {
	u, err := url.Parse(srv.URL)
	if err != nil {
		panic(err)
	}
	return func(*http.Request) (*url.URL, error) { return u, nil }
}

// The native probe's reachable path: one HEAD per distinct host over
// the proxy resolver — any response (here a 502) proves the route; a
// transport error names the host unreachable.
func TestNativeProbeRoutesThroughProxy(t *testing.T) {
	srv, seen := fakeProxy(t, false)
	unreachable := probeHosts([]string{"pypi.org", "static.rust-lang.org", "pypi.org"},
		proxyAt(srv))
	if len(unreachable) != 0 {
		t.Fatalf("unreachable = %v, want all routed", unreachable)
	}
	got := seen()
	if len(got) != 2 || !slices.Contains(got, "pypi.org") || !slices.Contains(got, "static.rust-lang.org") {
		t.Fatalf("proxy saw %v, want one HEAD per distinct host", got)
	}
}

// The unreachable path: a dead proxy makes every HEAD fail — the probe
// names the hosts instead of blocking on a timeout scale.
func TestNativeProbeUnreachableHosts(t *testing.T) {
	srv, _ := fakeProxy(t, false)
	srv.Close() // dead proxy — every dial refuses
	unreachable := probeHosts([]string{"docker.io", "registry.npmjs.org"}, proxyAt(srv))
	if !slices.Equal(unreachable, []string{"docker.io", "registry.npmjs.org"}) {
		t.Fatalf("unreachable = %v, want both hosts named", unreachable)
	}
}

// The construct→host mapping (§7 egress list): apt → the distro
// mirrors, pip → PyPI, npm → registry.npmjs.org, rustup → the rust
// hosts, a versioned URL → its own host — comment lines carry nothing,
// and unmapped constructs (the playwright browser CDN) take the
// false-green fallback.
func TestRecipeHostsMapping(t *testing.T) {
	path := filepath.Join(t.TempDir(), "unit.Dockerfile")
	recipe := `# a comment mentioning pip install must not map anything
RUN apt-get update && apt-get install -y gnupg \
    && curl -fsSL https://packages.ros.org/ros2.key | tee /tmp/k \
    && echo "deb https://labs.picotech.com/debian/ main" > /etc/apt/sources.list.d/x.list
RUN pip3 install --break-system-packages platformio==6.1.16
RUN python3 -m pip install numpy==2.2.1
USER dev
RUN npm install -g typescript@5.7.3 && npx playwright install-deps
RUN curl -fsSL https://sh.rustup.rs | sh -s -- --default-toolchain 1.90.0
RUN curl -fsSL https://downloads.arduino.cc/arduino-cli_0.35.3_Linux_64bit.tar.gz | tar xz
RUN wget -q https://cdn.playwright.dev/some-browser.zip
`
	if err := os.WriteFile(path, []byte(recipe), 0o644); err != nil {
		t.Fatal(err)
	}
	got := recipeHosts(path)
	want := []string{
		"archive.ubuntu.com", "cdn.playwright.dev", "downloads.arduino.cc",
		"files.pythonhosted.org", "labs.picotech.com", "packages.ros.org",
		"pypi.org", "registry.npmjs.org", "security.ubuntu.com",
		"sh.rustup.rs", "static.rust-lang.org",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("recipeHosts =\n%v\nwant\n%v", got, want)
	}
}

// Build all rides the same preflight (§9 WP-8): an offline host refuses
// before anything queues — the refusal names the whole download-needing
// plan.
func TestImagesBuildAllOfflinePreflightRefuses(t *testing.T) {
	reg, _ := fixtureRegistry(t, "alpha")
	seedToolchains(t, reg.Home)
	run, builds := preflightRunner(t, "")
	s := depsServer(t, run, reg)
	s.deps.NetProbe = func([]string) []string { return []string{"docker.io"} }

	resp := post(t, s.URL()+"images/build", "")
	body := readAll(t, resp)
	if !strings.Contains(body, "refusing to build — the base image and toolchains ") ||
		!strings.Contains(body, "cannot reach: docker.io") ||
		!strings.Contains(body, "Connect and retry, or Build later.") {
		t.Fatalf("Build-all offline refusal wrong:\n%.400s", body)
	}
	if len(*builds) != 0 {
		t.Fatalf("build-images.sh ran %v behind a refused preflight", *builds)
	}
}

// The estimate's same-id branch (§7 item 2): a stale-hash image of the
// same unit sizes the rebuild honestly — the refusal's estimate carries
// its real size instead of the floor.
func TestImagesRowBuildEstimateSizesFromExistingImage(t *testing.T) {
	reg, _ := fixtureRegistry(t, "alpha")
	seedToolchains(t, reg.Home)
	run, builds := preflightRunner(t, "")
	s := depsServer(t, run, reg)
	run.Script = func(name string, args []string) (string, error) {
		rest := strings.Join(args, " ")
		switch {
		case name == "df":
			return "Avail\n2791728742\n", nil // 2.6 GiB free
		case name == "podman" && strings.HasPrefix(rest, "images --format"):
			return "opencode-sandbox-base:old0000old00\t2.0 GB\n" +
				"opencode-sandbox-tc-go:stale00stale0\t536870912 B\n", nil // 512 MiB stale go
		case name == "podman" && strings.HasPrefix(rest, "image exists"):
			return "", fmt.Errorf("no such image") // the derived tags are missing
		case name == "bash" && len(args) > 0 && strings.HasSuffix(args[0], "build-images.sh"):
			*builds = append(*builds, rest)
			return "", fmt.Errorf("must not run")
		}
		return "", nil
	}

	resp := post(t, s.URL()+"images/build", "tag=go")
	body := readAll(t, resp)
	if !strings.Contains(body, "2.6 GiB free is below the 3.0 GiB needed (2.5 GiB estimate + 20% headroom)") {
		t.Fatalf("estimate did not size from the existing same-id image:\n%.300s", body)
	}
	if len(*builds) != 0 {
		t.Fatalf("build-images.sh ran %v behind a refused preflight", *builds)
	}
}

// The estimate's largest-known-payload term (§7 item 2): with no same-id
// image, the largest existing component image's size delta over the base
// sizes the rebuild honestly — floored at the flat 1 GiB budget (the
// mirror of build-images.sh's component_estimate_bytes fallback; the
// agreement test below pins the two together).
func TestImagesRowBuildEstimateUsesLargestKnownPayload(t *testing.T) {
	reg, _ := fixtureRegistry(t, "alpha")
	seedToolchains(t, reg.Home)
	run, builds := preflightRunner(t, "")
	s := depsServer(t, run, reg)
	baseTag, err := identity.BaseTag(s.baseRecipePath())
	if err != nil {
		t.Fatal(err)
	}
	run.Script = func(name string, args []string) (string, error) {
		rest := strings.Join(args, " ")
		switch {
		case name == "df":
			return "Avail\n4294967296\n", nil // 4.0 GiB free
		case name == "podman" && strings.HasPrefix(rest, "images --format"):
			// base 2 GiB + a rust image carrying a 4 GiB payload delta
			return baseTag + "\t2147483648 B\n" +
				"opencode-sandbox-tc-rust:rust0000rust0\t6442450944 B\n", nil
		case name == "podman" && strings.HasPrefix(rest, "image exists"):
			if rest == "image exists "+baseTag {
				return "", nil // the base image exists — the estimate is component-only
			}
			return "", fmt.Errorf("no such image") // the derived tags are missing
		case name == "bash" && len(args) > 0 && strings.HasSuffix(args[0], "build-images.sh"):
			*builds = append(*builds, rest)
			return "", fmt.Errorf("must not run")
		}
		return "", nil
	}

	resp := post(t, s.URL()+"images/build", "tag=go")
	body := readAll(t, resp)
	if !strings.Contains(body, "4.0 GiB free is below the 4.8 GiB needed (4.0 GiB estimate + 20% headroom)") {
		t.Fatalf("estimate did not size from the largest known payload on host:\n%.300s", body)
	}
	if len(*builds) != 0 {
		t.Fatalf("build-images.sh ran %v behind a refused preflight", *builds)
	}
}

// --- the Go ↔ bash disk-refusal agreement -----------------------------------

// buildImagesScript locates the reference builder (the repo tree runs
// `go test` from the package dir; the script sits at
// dist/scripts/build-images.sh).
func buildImagesScript(t *testing.T) string {
	t.Helper()
	for _, rel := range []string{"../../dist/scripts/build-images.sh", "../../../dist/scripts/build-images.sh"} {
		if _, err := os.Stat(rel); err == nil {
			abs, err := filepath.Abs(rel)
			if err != nil {
				t.Fatal(err)
			}
			return abs
		}
	}
	t.Skip("dist/scripts/build-images.sh not found (not the repo tree)")
	return ""
}

// refusalSentence extracts the one disk-refusal sentence from a builder
// or preflight output: the "refusing to build: … then retry" span (the
// Go side embeds it in the streamed HTML toast, the bash side prefixes
// it with "build-images: "; the network refusal's em dash after
// "refusing to build" never matches).
func refusalSentence(out string) string {
	const head, tail = "refusing to build:", "then retry"
	for _, l := range strings.Split(out, "\n") {
		i := strings.Index(l, head)
		if i < 0 {
			continue
		}
		j := strings.Index(l[i:], tail)
		if j < 0 {
			return l[i:]
		}
		return l[i : i+j+len(tail)]
	}
	return ""
}

// TestDiskRefusalAgreesWithBash pins the Go preflight's disk-refusal
// sentence to the builder's own (dist/scripts/build-images.sh, spec §7
// item 2): for the same host numbers the two must refuse with the same
// words. One fixture runs both sides over a plan that rides the
// largest-known-payload formula path — the internal/discovery agreement
// pattern, here against the builder instead of the discovery helper.
// The fixture root carries the shipped base recipe and go recipe (the
// builder lints them as shipped; the minimal fixture recipes would trip
// the base-lint's own refusal).
func TestDiskRefusalAgreesWithBash(t *testing.T) {
	script := buildImagesScript(t)
	dist := filepath.Dir(filepath.Dir(script))
	reg, _ := fixtureRegistry(t, "alpha")
	seedToolchains(t, reg.Home)

	// The Go side: the images row build behind the fakes — df answers the
	// free number, podman lists the two images and only the base tag
	// exists, the builder never runs. (baseTag is filled in below, after
	// the fixture carries the shipped base recipe.)
	var baseTag string
	run := &cmdrun.Fake{Script: func(name string, args []string) (string, error) {
		rest := strings.Join(args, " ")
		switch {
		case name == "df":
			return "Avail\n1073741824\n", nil
		case name == "podman" && strings.HasPrefix(rest, "images --format"):
			return baseTag + "\t2147483648 B\n" +
				"opencode-sandbox-tc-rust:rust0000rust0\t6442450944 B\n", nil
		case name == "podman" && strings.HasPrefix(rest, "image exists"):
			if rest == "image exists "+baseTag {
				return "", nil
			}
			return "", fmt.Errorf("no such image")
		case name == "bash":
			return "", fmt.Errorf("the Go side never launches the builder")
		}
		return "", nil
	}}
	s := depsServer(t, run, reg)

	// The fixture root carries the shipped base recipe and go recipe (the
	// builder lints them as shipped; the minimal fixture base would trip
	// the base-lint's own refusal). Written after the seeder ran — it
	// would overwrite them back.
	root := tcRootOf(reg.Home)
	for _, f := range []string{"base.Dockerfile", filepath.Join("recipes", "go.Dockerfile")} {
		raw, err := os.ReadFile(filepath.Join(dist, "toolchains", f))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, f), raw, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// The shared host numbers: 1.0 GiB free; the base image existing
	// (2 GiB — the plan's estimate is component-only) and a 6 GiB rust
	// component image on host, its delta over the base the formula's
	// largest-known-payload term; the go component image missing. Both
	// sides derive the same tags from the same fixture base recipe.
	b, err := identity.BaseTag(filepath.Join(root, "base.Dockerfile"))
	if err != nil {
		t.Fatal(err)
	}
	baseTag = b

	resp := post(t, s.URL()+"images/build", "tag=go")
	goLine := refusalSentence(readAll(t, resp))

	// The bash side: the builder itself behind a PATH-mocked podman/df/
	// curl (the tests/test-build-images.sh mock pattern).
	bin := t.TempDir()
	writeMock := func(name, body string) {
		t.Helper()
		path := filepath.Join(bin, name)
		if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeMock("podman", `#!/usr/bin/env bash
base_tag="opencode-sandbox-base:$(sha256sum "$TOOLCHAINS_ROOT/base.Dockerfile" | cut -c1-12)"
case "$1" in
  --version)
    printf 'podman version 4.9.3\n'
    exit 0
    ;;
  images)
    ref=""
    args=("$@")
    for ((i = 0; i < ${#args[@]}; i++)); do
      [[ "${args[$i]}" == "--filter" ]] && ref="${args[$((i + 1))]:-}"
    done
    ref="${ref#reference=}"
    # the glob filter lists the rust image; the per-id one lists nothing
    [[ "$ref" == 'opencode-sandbox-tc-*' ]] && printf 'opencode-sandbox-tc-rust:rust0000rust0\n'
    exit 0
    ;;
esac
case "$1 $2" in
  "image exists")
    [[ "${@: -1}" == "$base_tag" ]] && exit 0
    exit 1
    ;;
  "image inspect")
    [[ "${@: -1}" == "opencode-sandbox-tc-rust:rust0000rust0" ]] && printf '6442450944\n' || printf '2147483648\n'
    exit 0
    ;;
esac
exit 0
`)
	writeMock("df", "#!/usr/bin/env bash\nprintf 'Avail\\n1073741824\\n'\n")
	writeMock("curl", "#!/usr/bin/env bash\nexit 0\n")
	cmd := exec.Command("bash", script, "--tc", "go")
	cmd.Env = append(os.Environ(),
		"PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"),
		"TOOLCHAINS_ROOT="+root)
	out, err := cmd.CombinedOutput() // the preflight refusal exits 1
	t.Logf("bash out (%v): %s", err, out)
	bashLine := refusalSentence(string(out))
	if bashLine == "" {
		t.Fatalf("bash side did not refuse (err=%v):\n%s", err, out)
	}

	if goLine == "" {
		t.Fatal("Go side did not refuse")
	}
	if goLine != bashLine {
		t.Fatalf("disk refusal drifted:\nGo:   %s\nbash: %s", goLine, bashLine)
	}
}
