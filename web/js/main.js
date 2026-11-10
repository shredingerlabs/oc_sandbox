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
      currentNav = btn.dataset.nav;
      // Workspace → Open (#153) and Catalogs screen (#155) load server
      // fragments via HTMX; the rest are per-ticket stubs.
      if (currentNav === "ws-open" && window.htmx) {
        htmx.ajax("GET", base + "workspaces", { target: '[data-role="screen"]', swap: "innerHTML" });
        return;
      }
      if ((currentNav === "set-vcs" || currentNav === "set-ai") && window.htmx) {
        htmx.ajax("GET", base + "catalogs", { target: '[data-role="screen"]', swap: "innerHTML" });
        return;
      }
      renderStub(currentNav);
    });
  });

  var currentNav = null;

  // Re-render the Open screen in place — reactive from SSE workspace
  // events and the display-mode switch (no reload, no polling, §9/§6).
  function renderWorkspaces() {
    if (currentNav !== "ws-open" || !window.htmx) return;
    var q = document.querySelector('[data-role="ws-search"]');
    var url = base + "workspaces";
    if (q && q.value) url += "?q=" + encodeURIComponent(q.value);
    htmx.ajax("GET", url, { target: '[data-role="screen"]', swap: "innerHTML" });
  }
  document.addEventListener("displaychange", renderWorkspaces);

  // Sidebar count pill rides every screen swap (§5 count pills).
  screenEl.addEventListener("htmx:afterSwap", function (ev) {
    var src = ev.target && ev.target.querySelector ? ev.target.querySelector("[data-count]") : null;
    if (src) {
      var pill = document.querySelector('[data-role="ws-count"]');
      if (pill) pill.textContent = src.getAttribute("data-count");
    }
  });

  // --- Open screen card actions (#153) ---
  // Delegated clicks: Open (Standard always web; Advanced honors the
  // configured start option), Web access (fresh pair link → new tab
  // under the click gesture), console/OpenCode host-terminal access,
  // Stop/Delete confirm rows (#154 owns the stop/delete ops).
  screenEl.addEventListener("click", function (ev) {
    var btn = ev.target.closest("button");
    if (!btn) return;
    var card = btn.closest(".card");

    if (btn.dataset.confirm === "stop" || btn.dataset.confirm === "delete") {
      showConfirmRow(card, btn.dataset.confirm);
      return;
    }
    if (btn.dataset.act === "confirm-stop" || btn.dataset.act === "confirm-delete") {
      // The stop/delete ops themselves arrive with the workspace-ops
      // ticket (#154) — the confirm rows are the UI this ticket owns.
      showNote(card, "arrives with #154");
      return;
    }
    if (btn.dataset.act === "cancel-confirm") {
      renderWorkspaces(); // restore the plain card rows
      return;
    }
    if (btn.dataset.act === "copy-command") {
      var text = btn.getAttribute("data-command");
      if (navigator.clipboard) navigator.clipboard.writeText(text);
      btn.textContent = "copied";
      return;
    }
    if (btn.dataset.act === "retry-web") { retryPair(card); return; }
    if (btn.dataset.act !== "open" && btn.dataset.act !== "web" &&
        btn.dataset.act !== "console" && btn.dataset.act !== "opencode") return;

    var act = btn.dataset.act;
    var variant = act === "open" ? (btn.dataset.variant || "web") : act;

    if (variant === "web") {
      // The new tab opens synchronously under the click gesture (§10),
      // then navigates once the fresh pair link arrives.
      openPairTab(card);
      chainStart(card, variant, function (fresh) { pairInto(fresh, tabFor(fresh)); });
    } else {
      chainStart(card, variant, function (fresh) {
        accessVariant(fresh, variant);
      });
    }
  });

  // chainStart runs the start op and then continues the access chain
  // with the fresh card node; card re-renders ride the SSE workspace
  // events.
  function chainStart(card, variant, then) {
    fetch(post(card, "start"), { method: "POST" })
      .then(function (r) { return r.text(); })
      .then(function (frag) { return swapCard(card, frag); })
      .then(then)
      .catch(function () { showNote(card, "start failed — retry"); });
  }

  function post(card, action) {
    return base + "workspaces/" + encodeURIComponent(card.dataset.ws) + "/" + action;
  }

  function accessVariant(card, variant) {
    var form = new FormData();
    form.append("variant", variant);
    fetch(post(card, "access"), { method: "POST", body: new URLSearchParams(form) })
      .then(function (r) { return r.json(); })
      .then(function (v) {
        if (!v.opened && v.command) showCommand(card, v.command);
      })
      .catch(function () { showNote(card, "access failed — retry"); });
  }

  // Pair-link handoff (§10): fresh one-time link per request; the tab
  // opened under the click gets the link, the plain URL is the fallback
  // when pairing fails or the service is mid-boot; Retry rows stay on
  // the card.
  var tabs = {};
  function tabFor(card) { return tabs[card.dataset.ws]; }

  function openPairTab(card) {
    var tab = window.open("", "_blank");
    if (tab) {
      tab.document.write('<p style="font:14px system-ui;padding:2em">Pairing OpenCode web…</p>');
      tabs[card.dataset.ws] = tab;
    }
    return tab;
  }

  function pairInto(card, tab) {
    fetch(post(card, "pair-link"), { method: "POST" })
      .then(function (r) { return r.json(); })
      .then(function (v) {
        if (v.pair_link && tab) { tab.location.href = v.pair_link; return; }
        if (v.plain_url && tab) {
          tab.document.write('<p style="font:14px system-ui;padding:2em">OpenCode web: <a href="' +
            v.plain_url + '">' + v.plain_url + '</a></p>');
        }
        showRetry(card);
      })
      .catch(function () {
        if (tab) tab.document.write('<p style="font:14px system-ui;padding:2em">Pairing failed.</p>');
        showRetry(card);
      });
  }

  function retryPair(card) {
    openPairTab(card);
    pairInto(card, tabFor(card));
  }

  function showRetry(card) {
    var acts = card.querySelector(".acts");
    if (!acts || acts.querySelector("[data-act='retry-web']")) return;
    var b = document.createElement("button");
    b.className = "btn";
    b.setAttribute("data-act", "retry-web");
    b.textContent = "Retry web access";
    acts.appendChild(b);
  }

  // Headless fallback (§3): the exact copyable command, session pending.
  function showCommand(card, command) {
    showRow(card, '<code class="cmd">' + command + '</code> ' +
      '<button class="btn" data-act="copy-command" data-command="' + command + '">Copy</button>');
  }

  function showNote(card, text) {
    showRow(card, '<span class="note">' + text + '</span>');
  }

  function showRow(card, html) {
    var log = card.querySelector("[data-oplog]");
    if (!log) return;
    log.hidden = false;
    log.innerHTML = '<div class="row note-row">' + html + '</div>' + log.innerHTML;
  }

  // Confirm rows (§9 confirm set): Stop/Delete ask before acting; the
  // ops themselves land with #154.
  function showConfirmRow(card, kind) {
    var box = card.querySelector(".confirms");
    if (!box) return;
    var label = kind === "stop" ? "Stop this workspace?" : "Delete this workspace?";
    box.innerHTML =
      '<span class="confirm" data-confirm-row="' + kind + '">' +
      '<span class="confirm-label">' + label + '</span>' +
      '<button class="btn danger" data-act="confirm-' + kind + '">Confirm</button>' +
      '<button class="btn" data-act="cancel-confirm">Cancel</button>' +
      '</span>';
  }

  // swapCard replaces the card with the server fragment, re-applying
  // the streamed log contents.
  function swapCard(card, frag) {
    var tmp = document.createElement("div");
    tmp.innerHTML = frag;
    var fresh = tmp.querySelector(".card");
    if (!fresh) return card;
    card.replaceWith(fresh);
    applyLogs(fresh);
    return fresh;
  }

  // --- SSE consumption: reactive re-render, no polling ---
  function connect() {
    var es = new EventSource(base + "events");
    es.addEventListener("open", function () { /* stub: clear reconnect exhaustion UI */ });
    es.addEventListener("line", function (ev) { appendLine(ev); });
    es.addEventListener("workspace", function (ev) { renderWorkspaces(); });
    es.addEventListener("update", function (ev) { stubEvent("update", ev); });
    es.addEventListener("shutdown", function () {
      es.close();
      screenEl.innerHTML =
        '<div class="stub">Server stopped — relaunch oc-sandbox.</div>'; // §12 screen stub
    });
    return es;
  }

  // Line events stream into the busy card's log (matched by op id, §9);
  // lines are kept per op so re-renders never lose the stream.
  var opLogs = {};
  function appendLine(ev) {
    var data;
    try { data = JSON.parse(ev.data); } catch (e) { return; }
    opLogs[data.op] = (opLogs[data.op] || "") + data.text + "\n";
    forEachCard(function (card) { applyLogs(card); });
  }

  function forEachCard(fn) {
    document.querySelectorAll(".card[data-op-id]").forEach(fn);
  }

  // applyLogs re-fills a card's log from the kept per-op lines.
  function applyLogs(card) {
    var op = card.getAttribute("data-op-id");
    var text = opLogs[op];
    if (!text) return;
    var log = card.querySelector("[data-oplog]");
    if (!log) return;
    log.hidden = false;
    log.textContent = text;
    log.scrollTop = log.scrollHeight;
  }

  // The grid re-render from a workspace event keeps busy logs visible.
  screenEl.addEventListener("htmx:afterSwap", function () {
    forEachCard(function (card) { applyLogs(card); });
  });

  function stubEvent(kind, ev) {
    // The per-screen tickets consume these; the shell only holds the door.
    if (window.console && console.debug) console.debug("sse " + kind, ev.data);
  }

  // Open Workspace is the primary screen — load it at start (#153).
  currentNav = "ws-open";
  var openBtn = document.querySelector('[data-nav="ws-open"]');
  if (openBtn) openBtn.classList.add("active");
  renderWorkspaces();

  connect();
})();
