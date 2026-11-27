package webserver

// Build preflight (spec toolchain-sets §7 items 2+3, §9 WP-8, #212):
// every op that runs the toolchain builder — the Images screen's row
// builds and Build all, the create chain's Build now, the
// mismatch/restart inline builds — refuses BEFORE the builder starts
// when the host cannot carry the build:
//
//   - Disk: the host's free space (the podman store path — home) below
//     what the op will build plus 20 % headroom refuses with a reason
//     naming both numbers and pointing at Clean intermediates. The
//     estimate covers the missing pieces only (lazy build): the base
//     budget when the base image is missing, per missing component the
//     same-id image's size where one exists else the floored budget,
//     and the assembly's own layer budget for sets.
//   - Network: a download-needing plan (any missing component or the
//     base image) probes one HTTP HEAD per distinct source host — the
//     §7 egress list's built-in construct→host mapping over the plan's
//     recipes, docker.io for the base pull — over the host's own proxy
//     environment (builds never traverse squid, so no allowlist entries
//     ever). Unreachable hosts refuse naming the uncached units, the
//     unreachable hosts and the escape. Assembly-only plans skip the
//     probe; a fully-cached plan skips the whole preflight — it builds
//     offline by construction.
//
// A plan the builder will refuse anyway (invalid recipe, underivable
// tag) preflights not at all: the builder's own loud refusal streams
// into the pane unchanged. Harnesses without a NetProbe seam skip the
// probe (the builder's own preflight still guards at build time);
// production wires webserver.NativeNetProbe.

import (
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/shredingerlabs/oc-sandbox/internal/cmdrun"
	"github.com/shredingerlabs/oc-sandbox/internal/discovery"
	"github.com/shredingerlabs/oc-sandbox/internal/identity"
)

// The §7 item-2 budgets (bytes) — mirrored from dist/scripts/
// build-images.sh: the base-build ballpark, the per-component budget
// formula (≈ base + 2× payload) floored at 1 GiB, and the assembly's
// own layer budget (base/component layers shared, budgeted by their
// own builds), floored at 1 GiB.
const (
	baseDiskBudgetBytes      = int64(2) * 1024 * 1024 * 1024
	componentDiskBudgetBytes = int64(1024) * 1024 * 1024
	setDiskBudgetBytes       = int64(1024) * 1024 * 1024

	// netProbeTimeout bounds one HEAD — the §7 "short timeout".
	netProbeTimeout = 3 * time.Second
)

// buildPlan is what one builder invocation will actually build (lazy —
// cached-skip per derived content tag): the base when missing, the
// missing components, and whether the set assembly rides the plan.
type buildPlan struct {
	BaseFile    string
	BaseMissing bool
	Missing     []discovery.Unit // valid units whose derived tag is missing, sorted by id
	Assembly    bool             // the set assembly's own build (its layer budget)
}

// empty reports a fully-cached plan: nothing to build, nothing to
// refuse, nothing to probe (§7 — fully-cached plans never refuse).
func (p buildPlan) empty() bool {
	return !p.BaseMissing && len(p.Missing) == 0 && !p.Assembly
}

// downloadNeeding reports whether the plan downloads anything (§7 item
// 3: the probe's precondition — assembly-only plans skip it).
func (p buildPlan) downloadNeeding() bool {
	return p.BaseMissing || len(p.Missing) > 0
}

// --- plan computation --------------------------------------------------------------

// builderPlanBase is the `--base` op's plan.
func (s *Server) builderPlanBase() (buildPlan, bool) {
	return s.newPlan(false)
}

// builderPlanTC is the `--tc <id>` op's plan (the builder builds the
// base first when it is missing — a component's FROM needs it).
func (s *Server) builderPlanTC(id string) (buildPlan, bool) {
	cat := discovery.Discover(s.toolchainsRoot())
	u, ok := cat.Unit(id)
	if !ok || !u.Valid {
		return buildPlan{}, false
	}
	p, ok := s.newPlan(false)
	if !ok {
		return buildPlan{}, false
	}
	s.addUnit(&p, u)
	return p, true
}

// builderPlanAll is the `--all` op's plan: base + every discovered
// component. An invalid recipe refuses the builder itself — the plan
// preflights nothing and the builder's reason streams.
func (s *Server) builderPlanAll() (buildPlan, bool) {
	cat := discovery.Discover(s.toolchainsRoot())
	p, ok := s.newPlan(false)
	if !ok {
		return buildPlan{}, false
	}
	for _, u := range cat.Units {
		if !u.Valid {
			return buildPlan{}, false
		}
		s.addUnit(&p, u)
	}
	return p, true
}

// builderPlanSet is the `--set <ids>` op's plan (missing components +
// assembly). A set whose derived tag already exists is fully cached —
// the builder skips everything (and the callers' ensure-built step
// early-returns).
func (s *Server) builderPlanSet(ids []string) (buildPlan, bool) {
	cat := discovery.Discover(s.toolchainsRoot())
	p, ok := s.newPlan(true)
	if !ok {
		return buildPlan{}, false
	}
	members, ok := discovery.MembersFor(cat, ids)
	if !ok {
		return buildPlan{}, false
	}
	for _, m := range members {
		u, _ := cat.Unit(m.ID) // MembersFor validated every member — cannot miss
		s.addUnit(&p, u)
	}
	setTag, err := identity.SetTag(p.BaseFile, members)
	if err != nil {
		return buildPlan{}, false
	}
	if s.tagExists(setTag) {
		return buildPlan{}, true // fully cached — nothing builds, nothing refuses
	}
	p.Assembly = true
	return p, true
}

// newPlan starts a plan over the toolchains seam: the base recipe's
// file and the base-image missing flag (the builder builds the base
// first whenever it is missing — every component's FROM needs it).
// ok=false when the seam is unwired or the base recipe is unreadable
// (the builder refuses with its own reason).
func (s *Server) newPlan(assembly bool) (buildPlan, bool) {
	baseFile := s.baseRecipePath()
	tag, err := identity.BaseTag(baseFile)
	if err != nil {
		return buildPlan{}, false
	}
	return buildPlan{
		BaseFile:    baseFile,
		Assembly:    assembly,
		BaseMissing: !s.tagExists(tag),
	}, true
}

// addUnit adds one unit to the plan when its derived component tag is
// missing (cached-skip per tag, the builder's lazy build).
func (s *Server) addUnit(p *buildPlan, u discovery.Unit) {
	tag, err := identity.ComponentTag(p.BaseFile, u.ID, u.Path)
	if err != nil {
		return // underivable — the builder refuses; nothing to estimate
	}
	if !s.tagExists(tag) {
		p.Missing = append(p.Missing, u)
	}
}

// tagExists is the per-tag podman existence check.
func (s *Server) tagExists(tag string) bool {
	if tag == "" {
		return false
	}
	return discovery.ImageExistsTag(s.runnerSeam(), tag)
}

// --- disk preflight (§7 item 2) -----------------------------------------------------

// diskPreflight refuses (spec §7 item 2) when the host's free space is
// below the plan's estimate + 20 % headroom: the reason names both
// numbers and points at Clean intermediates — the same sentence the
// builder itself refuses with. An unreadable free number skips the
// refusal: a false green is recoverable, a false red blocks work (the
// builder's own disk_preflight re-checks at build time).
func (s *Server) diskPreflight(p buildPlan) error {
	est := s.estimateBytes(p)
	if est == 0 {
		return nil
	}
	free, ok := s.freeSpaceBytes()
	if !ok {
		return nil
	}
	need := est + est/5 // + 20 % headroom
	if free >= need {
		return nil
	}
	return fmt.Errorf("refusing to build: %s free is below the %s needed (%s estimate + 20%% headroom) — free space or run Clean intermediates (podman image prune), then retry",
		gibStr(free), gibStr(need), gibStr(est))
}

// estimateBytes is the §7 item-2 estimate: the base budget when the
// base image is missing, per missing component the same-id image's
// size where one exists (a stale-hash tag still sizes the rebuild
// honestly) else the budget formula with the largest known payload on
// host floored at the flat budget, plus the assembly's own layer
// budget when an assembly builds.
func (s *Server) estimateBytes(p buildPlan) int64 {
	if p.empty() {
		return 0
	}
	est := int64(0)
	if p.BaseMissing {
		est += baseDiskBudgetBytes
	}
	sizes := s.hostSizes()
	largest := largestComponentDelta(sizes)
	for _, u := range p.Missing {
		if sz := sizes.component[u.ID]; sz > 0 {
			est += int64(sz)
		} else {
			est += componentEstimateBytes(largest)
		}
	}
	if p.Assembly {
		est += setDiskBudgetBytes
	}
	return est
}

// componentEstimateBytes is the §7 item-2 budget formula's component
// term: the largest known payload on host, floored at the flat budget
// (1 GiB) — the mirror of the builder's component_estimate_bytes
// fallback, pinned together by the disk-refusal agreement test.
func componentEstimateBytes(largestDelta int64) int64 {
	if largestDelta > componentDiskBudgetBytes {
		return largestDelta
	}
	return componentDiskBudgetBytes
}

// hostSizes is what the host's existing images size the estimate with:
// per-id component sizes (the same-id estimate input; the first tag per
// id, like the builder's component_estimate_bytes) and the base image's
// size (the delta term's denominator).
type hostSizes struct {
	component map[string]float64
	base      float64
}

// largestComponentDelta reports the largest known payload on host (§7
// item 2): the largest existing component image's size delta over the
// base image. The payload archive lives inside the image and the dump
// step records no host-side size marker, so the delta is the honest
// measurable proxy — it already sums the extracted payload + compressed
// archive the budget formula reads as ≈ 2× payload. 0 when no base or
// no component image exists (no known payload) — the caller floors at
// the flat budget.
func largestComponentDelta(sizes hostSizes) int64 {
	if sizes.base <= 0 || len(sizes.component) == 0 {
		return 0
	}
	var best int64
	for _, sz := range sizes.component {
		if d := int64(sz) - int64(sizes.base); d > best {
			best = d
		}
	}
	return best
}

// hostSizes reads the existing images' sizes from one podman images
// query: component sizes keyed by id (first tag per id, like the
// builder's component_estimate_bytes) and the base image's size.
func (s *Server) hostSizes() hostSizes {
	sizes := hostSizes{component: map[string]float64{}}
	for _, img := range discovery.SandboxImages(s.runnerSeam()) {
		r := discovery.ParseImageRef(img.Tag)
		if sz := parseSizeBytes(img.Size); sz > 0 {
			switch r.Kind {
			case discovery.RefToolchain:
				if _, seen := sizes.component[r.ID]; !seen {
					sizes.component[r.ID] = sz
				}
			case discovery.RefBase:
				if sizes.base == 0 {
					sizes.base = sz
				}
			}
		}
	}
	return sizes
}

// freeSpaceBytes reads the home filesystem's free bytes (`df -B1
// --output=avail` — the builder's own free_space_bytes source;
// rootless podman stores images under $HOME); ok=false when the number
// is unreadable.
func (s *Server) freeSpaceBytes() (int64, bool) {
	out, err := s.runnerSeam().Output("df", "-B1", "--output=avail", s.home())
	if err != nil {
		return 0, false
	}
	lines := cmdrun.Lines(out)
	if len(lines) == 0 {
		return 0, false
	}
	v, err := strconv.ParseInt(lines[len(lines)-1], 10, 64)
	if err != nil || v < 0 {
		return 0, false
	}
	return v, true
}

// gibStr renders a byte count as a one-decimal GiB number (the
// refusal's unit — both suites print the same shape).
func gibStr(b int64) string {
	return strconv.FormatFloat(float64(b)/1073741824, 'f', 1, 64) + " GiB"
}

// --- network probe (§7 item 3) ------------------------------------------------------

// netPreflight refuses (spec §7 item 3) an offline host building a
// download-needing plan: the plan's source hosts are probed with one
// HEAD each and the refusal names the uncached units, the unreachable
// hosts and the escape ("connect and retry, or Build later"). A
// download-free plan (fully cached, or assembly-only) never refuses.
func (s *Server) netPreflight(p buildPlan) error {
	if p.empty() || !p.downloadNeeding() {
		return nil
	}
	hosts := p.sourceHosts()
	if len(hosts) == 0 {
		return nil
	}
	unreachable := s.probeNet(hosts)
	if len(unreachable) == 0 {
		return nil
	}
	ids := make([]string, 0, len(p.Missing))
	for _, u := range p.Missing {
		ids = append(ids, u.ID)
	}
	var needs string
	switch {
	case p.BaseMissing && len(ids) > 0:
		needs = "the base image and toolchains " + strings.Join(ids, ", ") + " need downloads"
	case p.BaseMissing:
		needs = "the base image needs downloads"
	default:
		needs = "toolchains " + strings.Join(ids, ", ") + " need downloads"
	}
	return fmt.Errorf("refusing to build — %s; cannot reach: %s. Connect and retry, or Build later.",
		needs, strings.Join(unreachable, ", "))
}

// sourceHosts maps the plan's download needs onto their distinct source
// hosts: the base pull (docker.io) and the base recipe's own sources
// when the base image is missing, plus every missing component's
// recipe sources.
func (p buildPlan) sourceHosts() []string {
	seen := map[string]bool{}
	add := func(hosts []string) {
		for _, h := range hosts {
			if h != "" {
				seen[h] = true
			}
		}
	}
	if p.BaseMissing {
		add([]string{"docker.io"})
		add(recipeHosts(p.BaseFile))
	}
	for _, u := range p.Missing {
		add(recipeHosts(u.Path))
	}
	return slices.Sorted(maps.Keys(seen))
}

// probeNet is the §7 probe seam: the Deps.NetProbe when wired, else no
// probe (harnesses skip it — the native probe dials real hosts;
// production wires NativeNetProbe, and the builder's own preflight
// still guards at build time).
func (s *Server) probeNet(hosts []string) []string {
	if s.deps != nil && s.deps.NetProbe != nil {
		return s.deps.NetProbe(hosts)
	}
	return nil
}

// NativeNetProbe is the native §7 probe (production wiring): one HTTP
// HEAD per distinct host over the host's own proxy environment
// (http.ProxyFromEnvironment) with a short timeout. Any response
// proves the route — a redirect or an error status still answers;
// transport errors (timeout, refused, no route) mark the host
// unreachable. The HEADs go out plain-HTTP: the probe checks
// reachability, not TLS, and a proxied plain HEAD keeps the probe
// itself testable against a fake proxy.
func NativeNetProbe(hosts []string) []string {
	return probeHosts(hosts, http.ProxyFromEnvironment)
}

// probeHosts sends one HTTP HEAD per distinct host and returns the
// unreachable ones, sorted; the proxy resolver is the seam that keeps
// the probe testable (the stdlib resolver snapshots the proxy
// environment on first use, so env-based test isolation is impossible).
func probeHosts(hosts []string, proxy func(*http.Request) (*url.URL, error)) []string {
	client := &http.Client{
		Timeout:   netProbeTimeout,
		Transport: &http.Transport{Proxy: proxy},
	}
	var unreachable []string
	for _, h := range slices.Compact(slices.Sorted(slices.Values(hosts))) {
		if h == "" {
			continue
		}
		resp, err := client.Head("http://" + h + "/")
		if err != nil {
			unreachable = append(unreachable, h)
			continue
		}
		resp.Body.Close()
	}
	return unreachable
}

// preflightBuild runs the §7 preflight family for one builder op —
// disk first, then the network probe; the first refusal wins and the
// op never starts.
func (s *Server) preflightBuild(p buildPlan) error {
	if p.empty() {
		return nil // fully cached — nothing builds, nothing refuses
	}
	if err := s.diskPreflight(p); err != nil {
		return err
	}
	return s.netPreflight(p)
}

// --- construct→host mapping (§7 egress list) ----------------------------------------

var urlHostRe = regexp.MustCompile(`https?://[A-Za-z0-9.-]+`)

// recipeHosts maps one recipe file's download constructs onto their
// source hosts — the §7 egress list's built-in construct→host mapping:
// apt → the distro mirrors, pip → PyPI, npm → registry.npmjs.org,
// rustup → the rust hosts, a versioned direct URL → its own host.
// Exotic implicit sources (the playwright browser CDN, rosdep's
// runtime fetches, a vendor apt mirror the recipe only names in a
// sources.list.d entry it echoes — that one rides the URL mapping)
// take the false-green fallback: unmapped constructs probe nothing (a
// false green is recoverable; a false red blocks work). The file is
// scanned whole so backslash-continuation lines carry their
// constructs; comment lines carry nothing.
func recipeHosts(path string) []string {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	add := func(h string) {
		if h != "" {
			seen[h] = true
		}
	}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.Contains(line, "apt-get") || strings.Contains(line, "apt install") {
			add("archive.ubuntu.com")
			add("security.ubuntu.com")
		}
		if strings.Contains(line, "pip install") || strings.Contains(line, "pip3 install") ||
			strings.Contains(line, "-m pip") {
			add("pypi.org")
			add("files.pythonhosted.org")
		}
		if strings.Contains(line, "npm install") || strings.Contains(line, "npm i ") {
			add("registry.npmjs.org")
		}
		if strings.Contains(line, "rustup") {
			add("sh.rustup.rs")
			add("static.rust-lang.org")
		}
		for _, u := range urlHostRe.FindAllString(line, -1) {
			add(strings.TrimPrefix(strings.TrimPrefix(u, "https://"), "http://"))
		}
	}
	return slices.Sorted(slices.Values(keysOf(seen)))
}
