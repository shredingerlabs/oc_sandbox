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
      if (btn.hasAttribute("hx-get")) return; // htmx swaps its own fragment in (Update, #160)
      // Catalogs screen (#155): Settings → VCS Tracker / AI-Provider
      // both load GET /catalogs (one fragment, both sections).
      if ((btn.dataset.nav === "set-vcs" || btn.dataset.nav === "set-ai") && window.htmx) {
        htmx.ajax("GET", base + "catalogs", { target: '[data-role="screen"]', swap: "innerHTML" });
        return;
      }
      // Container center + images (#158): fragment screens with the
      // reactive workspace re-render.
      if (screenPaths[btn.dataset.nav]) {
        loadScreen(screenPaths[btn.dataset.nav]);
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

  // onCreateWorkspace reacts to the create chain's terminal workspace
  // event ({op, state}): success swaps a fresh empty wizard (the TUI
  // resets the form too); failure wakes the form for a retry while the
  // pane stays readable.
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


  // Stub screen (#152 foundation): per-screen tickets replace it —
  // catalogs (#155) and the New Workspace wizard (#156) already do.
  // --- screens (#158): containers + images load as HTML fragments; the
  // log pane's streamed lines live in JS memory, so the reactive
  // workspace re-renders never lose build output ---
  var screenPaths = { "set-containers": "containers", "set-images": "images" };
  var currentScreen = null;
  var logLines = {};   // op id → array of streamed lines
  var lastLineId = 0;  // SSE id dedupe across reconnect replay

  function loadScreen(name) {
    fetch(base + name, { referrerPolicy: "no-referrer" })
      .then(function (r) { return r.text(); })
      .then(function (html) {
        currentScreen = name;
        screenEl.innerHTML = html;
        restoreLog();
      })
      .catch(function () { /* server may be gone; SSE shutdown covers it */ });
  }

  // restoreLog re-fills the current log pane from streamed memory.
  function restoreLog() {
    var pane = screenEl.querySelector('[data-role="log"]');
    if (!pane) return;
    var op = parseInt(pane.getAttribute("data-op"), 10) || 0;
    if (!op) return;
    var lines = logLines[op] || [];
    pane.textContent = lines.join("\n");
    if (lines.length) pane.scrollTop = pane.scrollHeight;
  }

  // --- shared confirm popup (ADR-0021 set): per-action copy, Cancel-safe ---
  var confirmEl = document.querySelector('[data-role="confirm"]');
  var confirmState = null;
  function showConfirm(title, body, okLabel, onOk) {
    confirmState = onOk;
    confirmEl.querySelector('[data-role="confirm-title"]').textContent = title;
    confirmEl.querySelector('[data-role="confirm-body"]').textContent = body;
    confirmEl.querySelector('[data-role="confirm-ok"]').textContent = okLabel;
    confirmEl.hidden = false;
    confirmEl.querySelector('[data-role="confirm-cancel"]').focus();
  }
  confirmEl.addEventListener("click", function (ev) {
    if (confirmState && ev.target.closest('[data-role="confirm-ok"]')) {
      var ok = confirmState;
      confirmState = null;
      confirmEl.hidden = true;
      ok();
    } else if (ev.target.closest('[data-role="confirm-cancel"]') || ev.target === confirmEl) {
      confirmState = null;
      confirmEl.hidden = true;
    }
  });

  // --- toasts ---
  var toastsEl = document.querySelector('[data-role="toasts"]');
  function toast(text, kind) {
    var el = document.createElement("div");
    el.className = "toast" + (kind ? " " + kind : "");
    el.textContent = text;
    toastsEl.appendChild(el);
    setTimeout(function () { el.remove(); }, 6000);
  }

  // --- container/image actions (#158): buttons carry data-action +
  // data-href (+ optional data-confirm / data-danger / data-tag).
  // Confirmed actions fire the POST only on confirm — the popup set
  // carried 1:1 (stop/delete container, build, delete image; the
  // deletes the danger ones). Confirms refuse while an op is
  // queued/running: the busy fragment comes back from the server and
  // swaps in like any other response.
  screenEl.addEventListener("click", function (ev) {
    var btn = ev.target.closest("button[data-action]");
    if (!btn || btn.disabled) return;
    ev.preventDefault();
    var href = btn.dataset.href;
    function go(form) {
      fetch(base + href, {
        method: "POST",
        body: form || null,
        headers: form ? { "Content-Type": "application/x-www-form-urlencoded" } : undefined,
        referrerPolicy: "no-referrer"
      })
        .then(function (r) { return r.text(); })
        .then(function (html) {
          screenEl.innerHTML = html;
          restoreLog();
          var refused = screenEl.querySelector('[data-toast]');
          if (refused) toast(refused.textContent, "warn");
        })
        .catch(function () { /* SSE shutdown covers a dead server */ });
    }
    if (btn.dataset.confirm) {
      showConfirm(
        btn.dataset.confirm,
        "",
        btn.dataset.danger !== undefined ? "Delete" : "Confirm",
        function () {
          go(btn.dataset.action === "img-delete"
            ? "tag=" + encodeURIComponent(btn.dataset.tag)
            : null);
        }
      );
      return;
    }
    go(null);
  });

  // --- §12 update phases: footer banner + Update card render purely
  //     from the one SSE `update` event; the run streams its log into
  //     the pane under the card; there is no in-WebUI restart.
  var bannerEl = document.querySelector('[data-role="update-banner"]');
  var updateOp = null; // the running/last update op — the log pane's filter
  function onUpdate(ev) {
    var st;
    try { st = JSON.parse(ev.data); } catch (e) { return; }
    if (st.op) updateOp = st.op;
    renderBanner(st);
    renderUpdateCard(st);
    if (st.phase === "applied") {
      toast("Update installed — restart oc-sandbox to apply.", "ok");
    } else if (st.phase === "failed") {
      toast("Update failed: " + (st.error || "see the log"), "warn");
    }
  }
  function renderBanner(st) {
    if (st.phase === "available") {
      bannerEl.textContent = "Update available: " + (st.latest || "");
      bannerEl.className = "banner";
      bannerEl.hidden = false;
    } else if (st.phase === "installing") {
      bannerEl.textContent = "Updating oc-sandbox…";
      bannerEl.className = "banner";
      bannerEl.hidden = false;
    } else if (st.phase === "failed") {
      bannerEl.textContent = "Update failed — see Settings → Update.";
      bannerEl.className = "banner danger";
      bannerEl.hidden = false;
    } else { // up-to-date / applied: the banner clears (§12)
      bannerEl.hidden = true;
    }
  }
  function renderUpdateCard(st) {
    var card = document.querySelector('[data-role="update-card"]');
    if (!card) return;
    var rowVerdict = card.querySelector('[data-role="update-verdict"]');
    var rowInstalled = card.querySelector('[data-role="update-installed"]');
    var rowLatest = card.querySelector('[data-role="update-latest"]');
    var rowError = card.querySelector('[data-role="update-error"]');
    var rowNote = card.querySelector('[data-role="update-note"]');
    var runBtn = card.querySelector('[data-role="update-run"]');
    if (st.installed && rowInstalled) rowInstalled.textContent = "Installed version: " + st.installed;
    if (st.latest && rowLatest && st.phase !== "installing") rowLatest.textContent = "Latest version: " + st.latest;
    var verdict = { "available": "Update available!", "up-to-date": "You are up to date.", "installing": "Updating…" }[st.phase] || "";
    if (rowVerdict) {
      rowVerdict.textContent = verdict;
      rowVerdict.dataset.phase = st.phase;
    }
    if (rowError) { // the failure stays visible until the next attempt
      if (st.phase === "failed") {
        rowError.textContent = "Update failed: " + (st.error || "");
        rowError.hidden = false;
      } else if (st.phase === "installing" || st.phase === "applied") {
        rowError.hidden = true; // the next attempt started / succeeded
      }
    }
    if (rowNote) rowNote.hidden = st.phase !== "applied";
    if (runBtn) {
      if (st.phase === "installing") runBtn.setAttribute("disabled", "");
      else runBtn.removeAttribute("disabled");
    }
  }

  // --- the update run behind the shared confirm popup (§12): the popup
  //     copy carries the keep-note; the POST answers at once, phases
  //     stream via /events ---
  document.addEventListener("click", function (ev) {
    var runBtn = ev.target.closest('[data-role="update-run"]');
    if (!runBtn || runBtn.hasAttribute("disabled")) return;
    ev.preventDefault();
    showConfirm(
      runBtn.dataset.confirmTitle || "Update oc-sandbox?",
      runBtn.dataset.confirmBody || "",
      "Update",
      function () {
        htmx.ajax("POST", base + "update/run", { target: '[data-role="update-card"]', swap: "outerHTML" });
      }
    );
  });

    es.addEventListener("update", function (ev) { onUpdate(ev); });

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
    es.addEventListener("line", function (ev) { onLine(ev); });
    es.addEventListener("workspace", function (ev) {
      // reactive re-render (#158, spec §9): fresh rows/markers after
      // every op state change — the log pane re-fills from memory
      var d = null;
      try { d = JSON.parse(ev.data); } catch (e) {}
      if (d && d.state) onCreateWorkspace(d); // the create chain's terminal event (#156)
      else if (currentScreen) loadScreen(currentScreen);
    });
    es.addEventListener("update", function (ev) { onUpdate(ev); });
    es.addEventListener("shutdown", function () {
      es.close();
      screenEl.innerHTML =
        '<div class="stub">Server stopped — relaunch oc-sandbox.</div>'; // §12 screen stub
    });
    return es;
  }

  // Line events stream into the embedded log pane under the Update card
  // while the update op runs (spec §12) — keyed by the op ID the update
  // event carries. #158: build lines stream the same way into the image
  // screen's log pane, buffered in JS memory across re-renders.
  function onLine(ev) {
    var id = parseInt(ev.lastEventId, 10) || 0;
    var replayed = lastLineId > 0 && id > 0 && id <= lastLineId;
    if (id > lastLineId) lastLineId = id;
    var line;
    try { line = JSON.parse(ev.data); } catch (e) { return; }
    // buffered copy for the container/image log panes (#158); replayed
    // lines are already in memory, so they are not appended twice
    if (line.op && !replayed) {
      if (!logLines[line.op]) logLines[line.op] = [];
      logLines[line.op].push(line.text);
      var pane = screenEl.querySelector('[data-role="log"]');
      if (pane && (parseInt(pane.getAttribute("data-op"), 10) || 0) === line.op) {
        pane.textContent = logLines[line.op].join("\n");
        pane.scrollTop = pane.scrollHeight;
      }
      // the create chain streams into the log pane under the wizard (#156)
      var cpane = document.querySelector('[data-role="create-log"][data-op-id="' + line.op + '"]');
      if (cpane) {
        var div = document.createElement("div");
        div.className = "logline";
        div.textContent = line.text;
        cpane.appendChild(div);
        cpane.scrollTop = cpane.scrollHeight;
      }
    }
    var card = document.querySelector('[data-role="update-card"]');
    var upane = card && card.querySelector('[data-role="update-log"]');
    if (upane && updateOp !== null && line.op === updateOp) {
      upane.hidden = false;
      upane.textContent += (upane.textContent ? "\n" : "") + line.text;
      upane.scrollTop = upane.scrollHeight;
    }
  }

  function stubEvent(kind, ev) {
    // The per-screen tickets consume these; the shell only holds the door.
    if (window.console && console.debug) console.debug("sse " + kind, ev.data);
  }

  connect();
})();
