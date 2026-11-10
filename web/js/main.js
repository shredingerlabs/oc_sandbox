// oc_sandbox WebUI shell script (#150 skeleton): theme + display-mode
// toggles, sidebar folding, and the one multiplexed SSE stream per tab
// (spec §9 — GET /events; event classes line / workspace / update /
// shutdown). #158 adds the container/image screens: fragment loader,
// confirm popups (danger styling for delete), action posts, and the
// log pane the build op streams into.

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

  // --- screens (#158): nav → fragment fetch → screen slot. The log
  // pane's streamed lines live in JS memory, so the reactive
  // workspace re-renders never lose build output. ---
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
    var op = pane.getAttribute("data-op");
    if (!op || op === "0") return;
    var lines = logLines[op] || [];
    pane.textContent = lines.join("\n");
    if (lines.length) pane.scrollTop = pane.scrollHeight;
  }

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
      if (screenPaths[btn.dataset.nav]) {
        loadScreen(screenPaths[btn.dataset.nav]);
      } else {
        screenEl.innerHTML = ""; // screen stubs fill this slot (per-screen tickets)
      }
    });
  });

  // --- confirm popup (ADR-0021 set carried 1:1, §9): stop/delete
  // container, build, delete image — danger styling for the deletes.
  // Confirms refuse while an op is queued/running: the busy toast
  // comes back from the server and swaps in like any other fragment.
  function showConfirm(message, danger) {
    return new Promise(function (resolve) {
      var overlay = document.createElement("div");
      overlay.className = "popup-overlay";
      var box = document.createElement("div");
      box.className = "popup" + (danger ? " danger" : "");
      var text = document.createElement("div");
      text.className = "popup-msg";
      text.textContent = message;
      var row = document.createElement("div");
      row.className = "popup-actions";
      var yes = document.createElement("button");
      yes.textContent = danger ? "Delete" : "Confirm";
      yes.className = danger ? "btn-danger" : "btn-primary";
      var no = document.createElement("button");
      no.textContent = "Cancel";
      no.className = "btn-muted";
      row.appendChild(yes);
      row.appendChild(no);
      box.appendChild(text);
      box.appendChild(row);
      overlay.appendChild(box);
      document.body.appendChild(overlay);
      function close(result) {
        overlay.remove();
        resolve(result);
      }
      yes.addEventListener("click", function () { close(true); });
      no.addEventListener("click", function () { close(false); });
      overlay.addEventListener("click", function (ev) {
        if (ev.target === overlay) close(false);
      });
    });
  }

  // --- actions: buttons carry data-action + data-href (+ optional
  // data-confirm / data-danger / data-tag). Confirmed actions fire the
  // POST only on confirm; every response is the fresh screen fragment.
  screenEl.addEventListener("click", function (ev) {
    var btn = ev.target.closest("button[data-action]");
    if (!btn || btn.disabled) return;
    ev.preventDefault();
    var action = btn.dataset.action;
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
        })
        .catch(function () { /* SSE shutdown covers a dead server */ });
    }
    if (btn.dataset.confirm) {
      showConfirm(btn.dataset.confirm, btn.dataset.danger !== undefined)
        .then(function (ok) {
          if (!ok) return;
          go(action === "img-delete" ? "tag=" + encodeURIComponent(btn.dataset.tag) : null);
        });
      return;
    }
    go(null);
  });

  // --- SSE hub: one stream per tab, reconnect replay via line serial ---
  function connect() {
    var es = new EventSource(base + "events");
    es.addEventListener("open", function () { /* stub: clear reconnect exhaustion UI */ });
    es.addEventListener("line", function (ev) {
      var id = parseInt(ev.lastEventId, 10) || 0;
      if (id <= lastLineId) return; // replayed line: already in memory
      lastLineId = id;
      var payload;
      try { payload = JSON.parse(ev.data); } catch (e) { return; }
      if (!payload.op) return;
      if (!logLines[payload.op]) logLines[payload.op] = [];
      logLines[payload.op].push(payload.text);
      var pane = screenEl.querySelector('[data-role="log"]');
      if (pane && pane.getAttribute("data-op") === String(payload.op)) {
        pane.textContent = logLines[payload.op].join("\n");
        pane.scrollTop = pane.scrollHeight;
      }
    });
    es.addEventListener("workspace", function (ev) {
      // reactive re-render: fresh rows/markers after every op state
      // change (the log pane re-fills from memory, restoreLog)
      if (currentScreen) loadScreen(currentScreen);
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
