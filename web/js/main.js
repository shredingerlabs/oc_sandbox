// oc_sandbox WebUI shell script (#150 skeleton): theme + display-mode
// toggles, sidebar folding, and the one multiplexed SSE stream per tab
// (spec §9 — GET /events; event classes line / workspace / update /
// shutdown). Screens re-render reactively in the per-screen tickets.

(function () {
  "use strict";

  // The page lives under /t/<token>/ — the token stays in the URL, so
  // the SSE stream resolves relative to it (EventSource cannot send
  // headers; the token-in-path choice holds).
  var base = location.pathname;
  if (!base.endsWith("/")) base += "/";

  var root = document.documentElement;
  var screenEl = document.querySelector('[data-role="screen"]');

  // --- theme toggle (client preference; no ambient authority) ---
  var saved = null;
  try { saved = localStorage.getItem("oc_sandbox.theme"); } catch (e) {}
  if (saved === "bright" || saved === "dark") root.dataset.theme = saved;
  document.querySelector('[data-role="theme-toggle"]').addEventListener("click", function () {
    root.dataset.theme = root.dataset.theme === "dark" ? "bright" : "dark";
    try { localStorage.setItem("oc_sandbox.theme", root.dataset.theme); } catch (e) {}
  });

  // --- Standard/Advanced display mode (#152, spec §6): stored truth
  // lives in global_config.json via GET/POST /settings — no localStorage.
  // A switch applies immediately: data-display flips on <html> (CSS
  // hides advanced-only rows) and screens re-render on `displaychange`.
  var seg = document.querySelector('[data-role="display-mode"]');
  var modeSub = document.querySelector('[data-role="mode-sub"]');

  function applyDisplay(mode) {
    root.dataset.display = mode;
    modeSub.textContent = mode === "advanced" ? "Advanced" : "Standard";
    seg.querySelectorAll("button").forEach(function (b) {
      b.setAttribute("aria-pressed", String(b.dataset.mode === mode));
    });
    document.dispatchEvent(new CustomEvent("displaychange", { detail: { mode: mode } }));
  }

  fetch(base + "settings").then(function (r) { return r.json(); }).then(function (s) {
    applyDisplay(s.display_mode === "advanced" ? "advanced" : "standard");
  }).catch(function () { /* defaults hold: data-display="standard" */ });

  seg.addEventListener("click", function (ev) {
    var btn = ev.target.closest("button[data-mode]");
    if (!btn) return;
    var form = new FormData();
    form.append("display_mode", btn.dataset.mode);
    fetch(base + "settings", { method: "POST", body: new URLSearchParams(form) })
      .then(function (r) { if (!r.ok) throw new Error("settings POST " + r.status); return r.json(); })
      .then(function (s) { applyDisplay(s.display_mode === "advanced" ? "advanced" : "standard"); })
      .catch(function () { /* UI keeps the stored mode — never pin */ });
  });

  // --- sidebar tree folding + active state ---
  document.querySelectorAll(".treehead[data-fold]").forEach(function (head) {
    head.addEventListener("click", function () {
      var section = head.closest(".group");
      section.classList.toggle("open");
      var list = section.querySelector("ul");
      list.hidden = !section.classList.contains("open");
    });
  });
  document.querySelectorAll(".tree [data-nav]").forEach(function (btn) {
    btn.addEventListener("click", function () {
      document.querySelectorAll(".tree [data-nav]").forEach(function (b) {
        b.classList.toggle("active", b === btn);
      });
      renderStub(btn.dataset.nav);
    });
  });

  // Stub screen (#152 foundation): one Standard row, one advanced-only
  // row (stored value survives the Standard view — presets, not
  // pinning), and the presets hint. Per-screen tickets (#153,
  // #155–#157) replace this with real rows driven by the Go matrix.
  function renderStub(nav) {
    screenEl.innerHTML =
      '<div class="stub">Screen "' + nav + '" arrives with its ticket.</div>' +
      '<div class="row"><span class="label">AI-Provider</span>(none)</div>' +
      '<div class="row" data-advanced-only><span class="label">Search mode</span>searxng</div>' +
      '<div class="row hint">presets, see Advanced</div>';
  }
  renderStub(null);

  // --- SSE hub: one stream per tab, reconnect replay via line serial ---
  function connect() {
    var es = new EventSource(base + "events");
    es.addEventListener("open", function () { /* stub: clear reconnect exhaustion UI */ });
    es.addEventListener("line", function (ev) { stubEvent("line", ev); });
    es.addEventListener("workspace", function (ev) { stubEvent("workspace", ev); });
    es.addEventListener("update", function (ev) { stubEvent("update", ev); });
    es.addEventListener("shutdown", function () {
      es.close();
      screenEl.innerHTML =
        '<div class="stub">Server stopped — relaunch oc-sandbox.</div>'; // §12 screen stub
    });
    return es;
  }

  function stubEvent(kind, ev) {
    // The per-screen tickets consume these; the shell only holds the door.
    if (window.console && console.debug) console.debug("sse " + kind, ev.data);
  }

  connect();
})();
