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

  // --- Standard/Advanced segmented control (skeleton: visual only —
  // persistence rides GET/POST /settings, later ticket) ---
  var seg = document.querySelector('[data-role="display-mode"]');
  seg.addEventListener("click", function (ev) {
    var btn = ev.target.closest("button[data-mode]");
    if (!btn) return;
    seg.querySelectorAll("button").forEach(function (b) {
      b.setAttribute("aria-pressed", String(b === btn));
    });
    document.querySelector('[data-role="mode-sub"]').textContent =
      btn.dataset.mode === "advanced" ? "Advanced" : "Standard";
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
      if (!btn.hasAttribute("hx-get")) {
        screenEl.innerHTML = ""; // screen stubs fill this slot (per-screen tickets)
      } // htmx-handled items swap their own fragment in
    });
  });

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

  // --- SSE hub: one stream per tab, reconnect replay via line serial ---
  function connect() {
    var es = new EventSource(base + "events");
    es.addEventListener("open", function () { /* stub: clear reconnect exhaustion UI */ });
    es.addEventListener("line", function (ev) { onLine(ev); });
    es.addEventListener("workspace", function (ev) { stubEvent("workspace", ev); });
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
  // event carries.
  function onLine(ev) {
    var stubbed = true;
    var line;
    try { line = JSON.parse(ev.data); } catch (e) { return; }
    var card = document.querySelector('[data-role="update-card"]');
    var pane = card && card.querySelector('[data-role="update-log"]');
    if (pane && updateOp !== null && line.op === updateOp) {
      stubbed = false;
      pane.hidden = false;
      pane.textContent += (pane.textContent ? "\n" : "") + line.text;
      pane.scrollTop = pane.scrollHeight;
    }
    if (stubbed && window.console && console.debug) console.debug("sse line", ev.data);
  }

  function stubEvent(kind, ev) {
    // The per-screen tickets consume these; the shell only holds the door.
    if (window.console && console.debug) console.debug("sse " + kind, ev.data);
  }

  connect();
})();
