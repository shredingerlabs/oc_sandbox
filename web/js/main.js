// oc_sandbox WebUI shell script (#150 skeleton, screens from #159):
// theme + display-mode toggles, sidebar folding, screen loading for the
// wired Settings screens, the confirm popup (ADR-0021 parity), and the
// one multiplexed SSE stream per tab (spec §9 — GET /events; event
// classes line / workspace / update / shutdown). Screens re-render
// reactively on the workspace event.

(function () {
  "use strict";

  // The page lives under /t/<token>/ — the token stays in the URL, so
  // the SSE stream resolves relative to it (EventSource cannot send
  // headers; the token-in-path choice holds).
  var base = location.pathname;
  if (!base.endsWith("/")) base += "/";

  var root = document.documentElement;
  var screenEl = document.querySelector('[data-role="screen"]');

  // --- screens wired by their tickets: nav key → fragment URL; other
  // nav targets stay stubs until their tickets land ---
  var SCREENS = {
    "set-import": "import",
    "set-config": "config",
    "set-uninstall": "uninstall",
  };
  var currentNav = null;

  function showScreen(nav) {
    currentNav = nav;
    var url = SCREENS[nav];
    if (url && window.htmx) {
      htmx.ajax("GET", url, { target: screenEl, swap: "innerHTML" });
    } else {
      screenEl.innerHTML = ""; // stub slot until the per-screen tickets land
    }
  }

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
      showScreen(btn.dataset.nav);
    });
  });

  // --- confirm popup (ADR-0021 parity): elements with data-confirm-popup
  // ask before issuing — HTMX's confirm hook drives the overlay ---
  document.body.addEventListener("htmx:confirm", function (evt) {
    var elt = evt.detail.elt;
    if (!elt || !elt.hasAttribute || !elt.hasAttribute("data-confirm-popup")) return;
    evt.preventDefault();
    openPopup(
      elt.getAttribute("data-confirm-title") || "Are you sure?",
      elt.getAttribute("data-confirm-question") || "",
      elt.getAttribute("data-confirm-label") || "Confirm",
      elt.classList.contains("danger"),
      function () { evt.detail.issueRequest(true); }
    );
  });

  function openPopup(title, question, label, danger, onConfirm) {
    var backdrop = document.createElement("div");
    backdrop.className = "popup-backdrop";
    backdrop.innerHTML =
      '<div class="popup" role="dialog" aria-modal="true">' +
      "<h3></h3>" + '<div class="q"></div>' +
      '<div class="popup-acts">' +
      '<button class="btn ghost" data-act="cancel">Cancel</button>' +
      '<button class="btn" data-act="ok"></button>' +
      "</div></div>";
    backdrop.querySelector("h3").textContent = title;
    backdrop.querySelector(".q").textContent = question;
    var ok = backdrop.querySelector('[data-act="ok"]');
    ok.textContent = label;
    if (danger) ok.classList.add("danger");
    function close() { backdrop.remove(); }
    ok.addEventListener("click", function () { close(); onConfirm(); });
    backdrop.querySelector('[data-act="cancel"]').addEventListener("click", close);
    backdrop.addEventListener("click", function (ev) { if (ev.target === backdrop) close(); });
    document.body.appendChild(backdrop);
  }

  // --- typed confirmation arms its submit button (delegated: survives
  // fragment swaps). data-arms="<button id>" data-arms-match="<text>" ---
  document.addEventListener("input", function (ev) {
    var input = ev.target.closest("[data-arms]");
    if (!input) return;
    var btn = document.getElementById(input.getAttribute("data-arms"));
    if (!btn) return;
    var match = input.getAttribute("data-arms-match") || "uninstall";
    btn.disabled = input.value.trim().toLowerCase() !== match.toLowerCase();
  });

  // --- SSE hub: one stream per tab, reconnect replay via line serial ---
  function connect() {
    var es = new EventSource(base + "events");
    es.addEventListener("open", function () { /* stub: clear reconnect exhaustion UI */ });
    es.addEventListener("line", function (ev) {
      var pane = document.querySelector('[data-role="op-log"]');
      if (!pane) return;
      try {
        var data = JSON.parse(ev.data);
        pane.textContent += data.text + "\n";
        pane.scrollTop = pane.scrollHeight;
      } catch (e) { /* malformed line: ignore */ }
    });
    es.addEventListener("workspace", function () {
      if (currentNav) showScreen(currentNav); // reactive re-render (§6)
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
