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
      // Catalogs screen (#155): Settings → VCS Tracker / AI-Provider
      // both load GET /catalogs (one fragment, both sections).
      if ((btn.dataset.nav === "set-vcs" || btn.dataset.nav === "set-ai") && window.htmx) {
        htmx.ajax("GET", base + "catalogs", { target: '[data-role="screen"]', swap: "innerHTML" });
        return;
      }
      // New Workspace wizard (#156): Workspace → New loads GET
      // /workspaces/new — the form + the create-chain log pane.
      if (btn.dataset.nav === "ws-new" && window.htmx) {
        htmx.ajax("GET", base + "workspaces/new", { target: '[data-role="screen"]', swap: "innerHTML" });
        return;
      }
      renderStub(btn.dataset.nav);
    });
  });

  // --- New Workspace wizard (#156, spec §7/§8): live derivation and
  // row toggles ride delegated events — HTMX swaps the fragments, so
  // nothing is bound to specific nodes here.

  // deriveName mirrors the server's newworkspace rule: the repo URL's
  // basename, .git suffix stripped.
  function deriveName(url) {
    var u = url;
    if (u.indexOf("https://") === 0) u = u.slice("https://".length);
    else if (u.indexOf("ssh://") === 0) u = u.slice("ssh://".length);
    else if (u.indexOf("git@") === 0) { var c = u.indexOf(":"); if (c >= 0) u = u.slice(c + 1); }
    if (!u) return "";
    var li = u.lastIndexOf("/");
    if (li >= 0) u = u.slice(li + 1);
    return u.replace(/\.git$/, "");
  }

  function wizardForm(el) { return el.closest('[data-role="new-workspace-form"]'); }
  function q(form, sel) { return form.querySelector(sel); }
  function setHidden(el, hidden) {
    if (!el) return;
    if (hidden) el.setAttribute("data-hidden", "1");
    else el.removeAttribute("data-hidden");
  }

  // name derivation: the workspace name derives live from the repo URL
  // in From Repo until hand-edited (the TUI's prefill rule).
  document.addEventListener("input", function (ev) {
    var form = wizardForm(ev.target);
    if (!form) return;
    if (ev.target.name === "name") form.dataset.nameManual = "1";
    if (ev.target.name === "repo_url" && q(form, '[name="source"][value="repo"]').checked &&
        !form.dataset.nameManual) {
      q(form, '[name="name"]').value = deriveName(ev.target.value);
    }
  });

  function applySource(form) {
    var repo = q(form, '[name="source"][value="repo"]').checked;
    var row = q(form, '[data-row="repo_url"]');
    if (row) {
      if (repo) row.removeAttribute("data-repo-only");
      else row.setAttribute("data-repo-only", "1");
    }
    if (repo && !form.dataset.nameManual) {
      q(form, '[name="name"]').value = deriveName(q(form, '[name="repo_url"]').value);
    }
  }

  function applyAgent(form) {
    var agentSel = q(form, '[data-role="wizard-agent"]');
    var row = q(form, '[data-row="edition"]');
    if (agentSel.value === "pkm") { // pkm pins the edition — the row hides
      row.setAttribute("data-pkm-pin", "1");
      q(form, '[data-role="wizard-edition"]').value = "pkm";
    } else {
      if (row.getAttribute("data-pkm-pin") !== null) {
        row.removeAttribute("data-pkm-pin");
        q(form, '[data-role="wizard-edition"]').value = "swdev"; // §6 preselect
      }
    }
  }

  function applyEditionPreset(form) {
    // §6: HIL presets on for embedded creations — a preset, not a pin:
    // the user can toggle it right back off in the same row.
    var ed = q(form, '[data-role="wizard-edition"]').value;
    var hil = q(form, '[name="hil"]');
    if (hil) hil.checked = ed === "embedded";
  }

  // catalog selects: the inline "New…" rows and the token input appear
  // per selection; "none" hides both (the empty default).
  function applyCatalogSelect(form, selRole, newRow, tokenRow) {
    var v = q(form, selRole).value;
    setHidden(q(form, newRow), v !== "new");
    setHidden(q(form, tokenRow), v === "none");
  }

  document.addEventListener("change", function (ev) {
    var form = wizardForm(ev.target);
    if (!form) return;
    if (ev.target.name === "source") applySource(form);
    else if (ev.target.name === "agent") applyAgent(form);
    else if (ev.target.name === "edition") applyEditionPreset(form);
    else if (ev.target.name === "vcs") applyCatalogSelect(form, '[name="vcs"]', '[data-row="vcs_new"]', '[data-row="vcs_token"]');
    else if (ev.target.name === "provider") applyCatalogSelect(form, '[name="provider"]', '[data-row="provider_new"]', '[data-row="provider_token"]');
  });

  // --- SSE consumption (per-screen tickets consume; the shell holds
  // the door): the create chain streams its steps into the log pane
  // under the wizard form (line events keyed by the op ID).
  function onCreateLine(data) {
    var pane = document.querySelector('[data-role="create-log"][data-op-id="' + data.op + '"]');
    if (!pane) return;
    var div = document.createElement("div");
    div.className = "logline";
    div.textContent = data.text;
    pane.appendChild(div);
    pane.scrollTop = pane.scrollHeight;
  }

  function onCreateWorkspace(data) {
    if (!data.op) return; // plain card re-render (Open screen ticket)
    var pane = document.querySelector('[data-role="create-log"][data-op-id="' + data.op + '"]');
    if (!pane) return;
    if (data.state === "done") {
      // success: a fresh empty wizard (the TUI resets the form too)
      htmx.ajax("GET", base + "workspaces/new", { target: "#new-workspace", swap: "outerHTML" });
    } else {
      // failure: the pane stays readable, the form wakes for a retry
      var fs = document.querySelector('[data-role="wizard-fields"]');
      if (fs) fs.disabled = false;
    }
  }

  function parseSSE(ev) {
    try { return JSON.parse(ev.data); } catch (e) { return null; }
  }


  // Stub screen (#152 foundation): per-screen tickets replace it —
  // catalogs (#155) and the New Workspace wizard (#156) already do.
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
    es.addEventListener("line", function (ev) {
      var d = parseSSE(ev);
      if (d && d.op !== undefined) onCreateLine(d);
    });
    es.addEventListener("workspace", function (ev) {
      var d = parseSSE(ev);
      if (d) onCreateWorkspace(d);
    });
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
