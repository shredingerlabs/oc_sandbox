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

  // --- screen title (Signal header: crumb above the h1) ---
  var crumbEl = document.querySelector('[data-role="crumb"]');
  var titleEl = document.querySelector('.titlebar h1');
  var SCREEN_TITLES = {
    "ws-open":       ["Workspace", "Open"],
    "ws-new":        ["Workspace", "New Workspace"],
    "set-ws":        ["Workspace › Settings", "Workspace Settings"],
    "set-vcs":       ["Workspace › Settings", "VCS Tracker"],
    "set-ai":        ["Workspace › Settings", "AI-Provider"],
    "set-import":    ["Workspace › Settings", "Import Workspace"],
    "set-config":    ["Workspace › Settings", "Export / Restore Config"],
    "set-images":    ["Workspace › Settings", "Images"],
    "set-containers": ["Workspace › Settings", "Containers"],
    "set-update":    ["Workspace › Settings", "Update"],
    "set-uninstall": ["Workspace › Settings", "Uninstall"]
  };
  function setTitle(nav) {
    var t = SCREEN_TITLES[nav];
    if (!t) return; // stubs and unknown screens keep the shell title
    crumbEl.textContent = t[0];
    titleEl.textContent = t[1];
  }
  setTitle("ws-open");

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
      var nav = btn.dataset.nav;
      currentNav = nav;
      setTitle(nav);
      // Workspace → Open (#153): the card grid loads as a fragment.
      if (nav === "ws-open" && window.htmx) {
        renderWorkspaces();
        return;
      }
      // Workspace Settings (#157): the menu entry loads the name-less
      // alias — the most recent workspace jumps in preselected.
      if (nav === "set-ws" && window.htmx) {
        renderWsSettings(null);
        return;
      }
      // Workspace → New (#156): the wizard loads as a fragment.
      if (nav === "ws-new" && window.htmx) {
        htmx.ajax("GET", base + "workspaces/new", { target: '[data-role="screen"]', swap: "innerHTML" });
        return;
      }
      // Catalog screens (#155, split per the menu): Settings → VCS
      // Tracker and AI-Provider each load their own fragment now.
      if (nav === "set-vcs" && window.htmx) {
        htmx.ajax("GET", base + "catalogs/trackers", { target: '[data-role="screen"]', swap: "innerHTML" });
        return;
      }
      if (nav === "set-ai" && window.htmx) {
        htmx.ajax("GET", base + "catalogs/providers", { target: '[data-role="screen"]', swap: "innerHTML" });
        return;
      }
      // Container center + images (#158): fragment screens with the
      // reactive workspace re-render; both show the spinner while the
      // (blocking) discovery fragment loads.
      if (screenPaths[nav]) {
        loadScreen(screenPaths[nav], screenPaths[nav] === "containers" || screenPaths[nav] === "images");
        return;
      }
      renderStub(nav);
    });
  });

  var currentNav = "ws-open"; // the Open screen is the primary (#153)

  // --- Workspace → Open screen (#153, spec §7): the card grid loads as
  // a fragment; cards re-render reactively from the SSE workspace
  // events and the display-mode switch — no polling (§6/§9).
  function renderWorkspaces() {
    if (currentNav !== "ws-open" || !window.htmx) return;
    var search = document.querySelector('[data-role="ws-search"]');
    var url = base + "workspaces";
    if (search && search.value) url += "?q=" + encodeURIComponent(search.value);
    htmx.ajax("GET", url, { target: '[data-role="screen"]', swap: "innerHTML" });
  }
  document.addEventListener("displaychange", renderWorkspaces);

  // --- Workspace Settings screen (#157, spec §7): reachable from a
  // card's settings icon and via the menu — the menu entry carries the
  // letter-tinted workspace selector dropdown (running state shown);
  // cards jump in preselected. loadScreen re-renders reactively from
  // the SSE workspace events and the display-mode switch.
  var settingsURL = null; // the loaded settings screen's URL
  function setActiveNav(nav) {
    document.querySelectorAll(".tree [data-nav]").forEach(function (b) {
      b.classList.toggle("active", b.dataset.nav === nav);
    });
  }
  function renderWsSettings(name) {
    currentNav = "set-ws";
    setActiveNav("set-ws");
    settingsURL = name ? "workspaces/" + encodeURIComponent(name) + "/settings" : "workspace-settings";
    loadScreen(settingsURL);
  }
  document.addEventListener("displaychange", function () {
    if (currentNav === "set-ws" && settingsURL) loadScreen(settingsURL);
  });

  // Catalog prefill selects (#157): picking an entry prefills the form
  // fields from the option's data-* attributes — free-form stays.
  document.addEventListener("change", function (ev) {
    var sel = ev.target;
    if (!sel.matches) return;
    if (sel.matches('[data-role="ws-selector"]')) { // the dropdown drives the screen
      if (sel.value) renderWsSettings(sel.value);
      return;
    }
    if (sel.matches('[data-role="provider-prefill"]') && sel.value) {
      var f = sel.closest("form");
      if (!f) return;
      var opt = sel.options[sel.selectedIndex];
      var nameIn = f.querySelector('[name="name"]');
      var epIn = f.querySelector('[name="endpoint"]');
      if (nameIn) nameIn.value = sel.value;
      if (epIn) epIn.value = opt.getAttribute("data-endpoint") || "";
      return;
    }
    if (sel.matches('[data-role="vcs-prefill"]') && sel.value) {
      var f = sel.closest("form");
      if (!f) return;
      var opt = sel.options[sel.selectedIndex];
      var kindIn = f.querySelector('[name="kind"]');
      var hostIn = f.querySelector('[name="host"]');
      if (kindIn) kindIn.value = opt.getAttribute("data-kind") || "";
      if (hostIn) hostIn.value = opt.getAttribute("data-host") || "";
      // The kind moved programmatically — the change event above never
      // fires, so sync the Host field's visibility with it here.
      var hostField = f.querySelector('[data-field="host"]');
      if (hostField) hostField.hidden =
        kindIn.value === "github" || kindIn.value === "gitlab" || kindIn.value === "none";
      return;
    }
    // VCS Tracker: the Host field only applies to gitlab_self / other
    // kinds — github / gitlab trackers are hostless and "none" tracks
    // nothing. The field hides (value kept); the initial hidden state
    // renders server-side (Catalog menu and Workspace settings alike).
    if (sel.matches('select[name="kind"]')) {
      var f = sel.closest("form");
      var host = f && f.querySelector('[data-field="host"]');
      if (host) host.hidden = sel.value === "github" || sel.value === "gitlab" || sel.value === "none";
      return;
    }
  });

  // Card actions: Open (Standard always web; Advanced honors the
  // configured start option), Web access (fresh pair link → new tab
  // under the click gesture), console/OpenCode host-terminal access,
  // and the Stop/Delete confirm rows running the real ops (#154).
  screenEl.addEventListener("click", function (ev) {
    var btn = ev.target.closest("button");
    if (!btn || btn.closest("[data-action]") || btn.disabled) return;
    var card = btn.closest(".card");
    if (!card) return;

    if (btn.dataset.act === "settings") {
      // #157: the card settings icon jumps into the settings screen
      // preselected.
      renderWsSettings(card.dataset.ws);
      return;
    }

    if (btn.dataset.confirm === "restart") {
      showConfirmRow(card, "restart");
      return;
    }
    if (btn.dataset.act === "confirm-restart") {
      // ADR-0033: the restart confirm runs the verified stop →
      // fresh-start replacement; the sidecar surfaces refusals.
      runWorkspaceOp(card, "restart");
      return;
    }
    if (btn.dataset.act === "mismatch-use") {
      // Use current (#115): route into the requested access — the
      // running container is untouched.
      renderWorkspaces();
      continueAccess(cardByWs(card.dataset.ws) || card, btn.dataset.variant);
      return;
    }
    if (btn.dataset.act === "mismatch-switch") {
      // Switch to <saved> (#116): the running-container replacement —
      // the adopt confirm gates it (§9).
      adoptConfirm(card, btn.dataset.variant,
        "Switch to " + btn.dataset.saved + "?",
        "The running container is stopped and restarted with the new edition — an agent may be mid-task.",
        "Switch");
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
    if (btn.dataset.act === "retry-web") {
      var rcard = cardByWs(card.dataset.ws) || card;
      openPairTab(rcard);
      pairInto(rcard, tabFor(rcard));
      return;
    }
    // First-run setup (#161, spec §11): Copy manual command copies the
    // exact wrapper; Cancel setup marks the pending session abandoned.
    // The SSE workspace push re-renders the card after each POST.
    if (btn.dataset.act === "setup-copy") {
      var cmd = btn.getAttribute("data-command");
      if (navigator.clipboard) navigator.clipboard.writeText(cmd);
      btn.textContent = "copied";
      return;
    }
    if (btn.dataset.act === "setup-cancel") {
      fetch(post(card, "setup/cancel"), { method: "POST", referrerPolicy: "no-referrer" })
        .catch(function () { renderWorkspaces(); });
      return;
    }
    if (btn.dataset.act !== "web" &&
        btn.dataset.act !== "console" && btn.dataset.act !== "opencode") return;

    var act = btn.dataset.act;
    var variant = act;

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

  // First-run setup retry (#161 owns POST …/setup/retry — #157's stub
  // replaced): works from the card's Run setup row and the workspace
  // settings screen alike — the nearest [data-ws] names the workspace;
  // the machine (re-)runs from the stored flags and the SSE workspace
  // push re-renders. Refusals (an op or session holds the workspace)
  // surface as a warning toast.
  screenEl.addEventListener("click", function (ev) {
    var btn = ev.target.closest('[data-act="setup-retry"]');
    if (!btn || btn.disabled) return;
    ev.preventDefault();
    var holder = btn.closest('[data-ws]');
    if (!holder || !holder.dataset.ws) return;
    fetch(base + "workspaces/" + encodeURIComponent(holder.dataset.ws) + "/setup/retry", {
      method: "POST",
      referrerPolicy: "no-referrer"
    }).catch(function () {
      toast("setup refused — an op or session holds this workspace", "warn");
    });
  });

  // chainStart runs the start op (queued-not-refused on the shared
  // opRunner) and continues the access chain once the workspace drains.
  // The requested access rides the POST (#154): a running container
  // whose image edition diverges from the saved one answers the #115
  // mismatch verdict instead of silently reusing — the card prompts.
  function chainStart(card, variant, then) {
    fetch(post(card, "start"), {
      method: "POST",
      body: "variant=" + encodeURIComponent(variant),
      headers: { "Content-Type": "application/x-www-form-urlencoded" },
      referrerPolicy: "no-referrer"
    })
      .then(function (r) { return r.json(); })
      .then(function (v) {
        if (v.mismatch) {
          showMismatchRow(card, variant, v);
          return;
        }
        if (v.ok === false) {
          showNote(card, v.error || "start refused");
          return;
        }
        return waitForIdle(card).then(then);
      })
      .catch(function () { showNote(card, "start failed — retry"); });
  }

  // showMismatchRow renders the mismatch prompt (#115): Use current /
  // Switch to <saved> / Cancel. The switch is the replacement the adopt
  // confirm gates; the saved edition's image is checked server-side
  // before anything stops (#117).
  function showMismatchRow(card, variant, v) {
    showRow(card,
      '<span class="note">Edition mismatch — the container runs "' + v.running_edition +
      '", the workspace is set to "' + v.saved_edition + '"</span>' +
      '<button class="btn" data-act="mismatch-use" data-variant="' + variant + '">Use current</button>' +
      '<button class="btn danger" data-act="mismatch-switch" data-variant="' + variant +
      '" data-saved="' + v.saved_edition + '">Switch to ' + v.saved_edition + '</button>' +
      '<button class="btn" data-act="cancel-confirm">Cancel</button>');
  }

  // The web-native adopt confirm (§9): both running-container
  // replacements — the edition-mismatch switch and the web re-adopt —
  // confirm first, because an agent may be mid-task inside the
  // container being replaced.
  function adoptConfirm(card, variant, title, body, okLabel) {
    showConfirm(title, body, okLabel, function () { runAdopt(card, variant); });
  }

  // runAdopt POSTs the adopt route and continues into the originally
  // requested access once the verified stop → start chain drains.
  function runAdopt(card, variant) {
    if (variant === "web" && !tabFor(card)) openPairTab(card);
    fetch(post(card, "adopt"), {
      method: "POST",
      body: "variant=" + encodeURIComponent(variant),
      headers: { "Content-Type": "application/x-www-form-urlencoded" },
      referrerPolicy: "no-referrer"
    })
      .then(function (r) { return r.json(); })
      .then(function (v) {
        if (v.ok === false) {
          showNote(card, v.error || "adopt refused");
          return;
        }
        waitForIdle(card).then(function (fresh) { continueAccess(fresh, variant); });
      })
      .catch(function () { showNote(card, "adopt failed — retry"); });
  }

  // continueAccess resolves the requested access on the (now running)
  // workspace: web as the fresh pair link in the tab opened under the
  // click gesture, console/OpenCode as the host-terminal handoff.
  function continueAccess(card, variant) {
    if (variant === "web") {
      if (!tabFor(card)) openPairTab(card);
      pairInto(card, tabFor(card));
    } else {
      accessVariant(card, variant);
    }
  }

  // runWorkspaceOp fires the confirm row's op; the refusal sidecar
  // surfaces as a note, an accepted op re-renders the card busy.
  function runWorkspaceOp(card, action) {
    fetch(post(card, action), { method: "POST", referrerPolicy: "no-referrer" })
      .then(function (r) { return r.json(); })
      .then(function (v) {
        if (v.ok === false) showNote(card, v.error || "refused");
      })
      .catch(function () { showNote(card, action + " failed — retry"); });
  }

  // waitForIdle polls (bounded) until the workspace's op drains — the
  // access chain continues only after the verified start.
  function waitForIdle(card, tries) {
    tries = tries === undefined ? 30 : tries;
    if (!tries) return Promise.resolve(cardByWs(card.dataset.ws) || card);
    return fetch(base + "workspaces", { referrerPolicy: "no-referrer" })
      .then(function (r) { return r.text(); })
      .then(function (doc) {
        if (cardBlock(doc, card.dataset.ws).indexOf("data-busy") < 0) {
          return cardByWs(card.dataset.ws) || card;
        }
        return promiseDelay(800).then(function () { return waitForIdle(card, tries - 1); });
      })
      .catch(function () { return cardByWs(card.dataset.ws) || card; });
  }

  function post(card, action) {
    return base + "workspaces/" + encodeURIComponent(card.dataset.ws) + "/" + action;
  }

  function cardByWs(ws) { return document.querySelector('.card[data-ws="' + ws + '"]'); }

  function cardBlock(doc, ws) {
    var i = doc.indexOf('data-ws="' + ws + '"');
    if (i < 0) return "";
    var end = doc.indexOf("</article>", i);
    return end < 0 ? "" : doc.slice(i, end);
  }

  function promiseDelay(ms) {
    return new Promise(function (res) { setTimeout(res, ms); });
  }

  // Access variants: console/OpenCode ride the host-terminal spawn —
  // the headless fallback renders the exact copyable command (§3).
  function accessVariant(card, variant) {
    fetch(post(card, "access"), {
      method: "POST",
      body: "variant=" + encodeURIComponent(variant),
      headers: { "Content-Type": "application/x-www-form-urlencoded" },
      referrerPolicy: "no-referrer"
    })
      .then(function (r) { return r.json(); })
      .then(function (v) {
        if (!v.opened && v.command) showCommand(card, v.command);
      })
      .catch(function () { showNote(card, "access failed — retry"); });
  }

  // Pair-link handoff (§10): a fresh one-time link per request; the tab
  // opened under the click gets it. The plain URL is the fallback when
  // pairing fails or the v2 service is mid-boot; Retry rows stay on the
  // card.
  var tabs = {};
  function tabFor(card) { return tabs[card.dataset.ws]; }

  function openPairTab(card) {
    var tab = window.open("", "_blank");
    if (tab) {
      tab.document.body.innerHTML =
        '<p style="font:14px system-ui;padding:2em">Pairing OpenCode web…</p>';
      tabs[card.dataset.ws] = tab;
    }
    return tab;
  }

  function pairInto(card, tab, tries) {
    tries = tries === undefined ? 5 : tries;
    fetch(post(card, "pair-link"), { method: "POST", referrerPolicy: "no-referrer" })
      .then(function (r) { return r.json(); })
      .then(function (v) {
        if (v.pair_link) {
          if (tab) tab.location.href = v.pair_link;
          return;
        }
        if (tries > 0) { // service mid-boot: keep trying quietly
          promiseDelay(1000).then(function () { pairInto(card, tab, tries - 1); });
          return;
        }
        if (v.plain_url && tab) {
          tab.document.body.innerHTML = '<p style="font:14px system-ui;padding:2em">OpenCode web: <a href="' +
            v.plain_url + '">' + v.plain_url + '</a></p>';
          showRetry(card);
          return;
        }
        // No published web port at all: the live container runs without
        // one (started as console/TUI) — offer the web re-adopt, the
        // replacement behind the adopt confirm (§9). The Retry row
        // stays as the non-adopting path.
        showRetry(card);
        adoptConfirm(card, "web",
          "Start OpenCode Web?",
          "The running container has no published web port. Adopting web stops and restarts it — an agent may be mid-task.",
          "Start web");
      })
      .catch(function () {
        if (tab) tab.document.body.innerHTML =
          '<p style="font:14px system-ui;padding:2em">Pairing failed.</p>';
        showRetry(card);
      });
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

  // Confirm rows (§9 confirm set): the Restart confirm-gate ask before
  // acting — Confirm posts the workspace op, Cancel restores the plain
  // card (stop/delete left the cards; Container center owns them).
  function showConfirmRow(card, kind) {
    var box = card.querySelector(".confirms");
    if (!box) return;
    var label = kind === "restart" ? "Restart this workspace?" : kind;
    box.innerHTML =
      '<span class="card-confirm" data-confirm-row="' + kind + '">' +
      '<span class="card-confirm-label">' + label + '</span>' +
      '<button class="btn danger" data-act="confirm-' + kind + '">Confirm</button>' +
      '<button class="btn ghost" data-act="cancel-confirm">Cancel</button>' +
      '</span>';
  }

  // The grid re-render keeps busy card logs and the sidebar count pill
  // in step with what streamed while the fragment was in flight.
  // The swap destroys the focused ws-search input (it sits inside the
  // swapped fragment), so keep focus in it across every swap: capture
  // value + caret before, re-focus and restore after. A response that
  // lands mid-typing echoes the request-time query — keep the live
  // value instead so keystrokes are never clobbered.
  var searchFocus = null; // {value, start, end} | null
  screenEl.addEventListener("htmx:beforeSwap", function () {
    var a = document.activeElement;
    if (a && a.matches && a.matches('[data-role="ws-search"]')) {
      searchFocus = { value: a.value, start: a.selectionStart, end: a.selectionEnd };
    } else {
      searchFocus = null;
    }
  });
  screenEl.addEventListener("htmx:afterSwap", function () {
    if (searchFocus) {
      var inp = screenEl.querySelector('[data-role="ws-search"]');
      if (inp) {
        if (inp.value !== searchFocus.value) inp.value = searchFocus.value;
        inp.focus();
        try {
          inp.setSelectionRange(searchFocus.start, searchFocus.end);
        } catch (e) {
          inp.setSelectionRange(inp.value.length, inp.value.length);
        }
      }
      searchFocus = null;
    }
    var src = screenEl.querySelector("[data-count]");
    if (src) {
      var pill = document.querySelector('[data-role="ws-count"]');
      if (pill) pill.textContent = src.getAttribute("data-count");
    }
    document.querySelectorAll(".card[data-op-id]").forEach(applyCardLog);
  });

  function applyCardLog(card) {
    var lines = logLines[card.getAttribute("data-op-id")];
    if (!lines || !lines.length) return;
    var log = card.querySelector("[data-oplog]");
    if (!log) return;
    log.hidden = false;
    log.textContent = lines.join("\n");
    log.scrollTop = log.scrollHeight;
  }

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

  function wizardForm(el) {
    var f = el.closest('[data-role="new-workspace-form"]');
    if (f) return f;
    // The pagehead source segment (signed-off prototype placement) sits
    // outside the form and rides the form="" attribute — route its
    // change events onto the wizard form all the same.
    if (el.name === "source" && el.form &&
        el.form.dataset.role === "new-workspace-form") return el.form;
    return null;
  }
  function q(form, sel) { return form.querySelector(sel); }
  // repoSourceRadio: the source radios are form-attribute-associated
  // (pagehead placement) — they are not descendants of the form, so the
  // lookup must be document-scoped.
  function repoSourceRadio() {
    return document.querySelector('[data-screen="ws-new"] [name="source"][value="repo"]');
  }
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
    var radio = repoSourceRadio();
    if (ev.target.name === "repo_url" && radio && radio.checked &&
        !form.dataset.nameManual) {
      q(form, '[name="name"]').value = deriveName(ev.target.value);
    }
  });

  function applySource(form) {
    var radio = repoSourceRadio();
    if (!radio) return;
    var repo = radio.checked;
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
      setEdition(q(form, '[data-role="wizard-edition"]'), "pkm");
    } else {
      if (row.getAttribute("data-pkm-pin") !== null) {
        row.removeAttribute("data-pkm-pin");
        setEdition(q(form, '[data-role="wizard-edition"]'), "swdev"); // §6 preselect
      }
    }
    if (row.getAttribute("data-pkm-pin") === null) applyEditionPreset(form);
  }

  // setEdition drives the .edotsel chip group: the hidden payload input
  // value + the chips' pressed state move together.
  function setEdition(holder, value) {
    if (!holder) return;
    holder.querySelector('input[name="edition"]').value = value;
    holder.querySelectorAll(".edopt").forEach(function (b) {
      b.setAttribute("aria-pressed", String(b.dataset.ed === value));
    });
  }

  // Edition chips (#156, prototype ws-new .edotsel): one chip per
  // edition; the click sets the hidden input (the confirm round-trip
  // unaffected) and fires the HIL preset rule like the old select did.
  // The Workspace-settings Advanced row (#157) uses the same chip
  // group under data-role="settings-edition" — no HIL preset coupling
  // there (the HIL row is its own checkbox).
  document.addEventListener("click", function (ev) {
    var chip = ev.target.closest(".edopt");
    if (!chip || chip.disabled) return;
    var holder = chip.closest('[data-role="wizard-edition"], [data-role="settings-edition"]');
    if (!holder) return;
    setEdition(holder, chip.dataset.ed);
    var form = holder.closest('[data-role="new-workspace-form"]');
    if (form) applyEditionPreset(form);
  });

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
  var screenPaths = { "set-containers": "containers", "set-images": "images",
    // global settings screens (#159)
    "set-import": "import", "set-config": "config", "set-uninstall": "uninstall" };
  var currentScreen = null;
  var logLines = {};   // op id → array of streamed lines
  var lastLineId = 0;  // SSE id dedupe across reconnect replay

  function loadScreen(name, spin) {
    if (spin) { // slow discovery screens answer the wait visibly
      currentScreen = name;
      screenEl.innerHTML =
        '<div class="loading"><span class="spinner" aria-hidden="true"></span>Loading…</div>';
    }
    fetch(base + name, { referrerPolicy: "no-referrer" })
      .then(function (r) { return r.text(); })
      .then(function (html) {
        currentScreen = name;
        screenEl.innerHTML = html;
        restoreLog();
        // htmx processes only its own swaps — a fragment landed through
        // raw innerHTML (the settings/config screens, SSE re-renders)
        // must be initialized explicitly or its hx-post forms submit
        // natively (full reload → back to the Open screen, §9).
        if (window.htmx) htmx.process(screenEl);
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

  // #159: confirm-gated HTMX actions carry data-confirm-popup + per-action
  // copy in data-confirm-* attributes — the htmx:confirm hook opens the
  // shared popup; the request issues only on OK.
  document.body.addEventListener("htmx:confirm", function (evt) {
    var elt = evt.detail.elt;
    if (!elt || !elt.hasAttribute || !elt.hasAttribute("data-confirm-popup")) return;
    evt.preventDefault();
    showConfirm(
      elt.getAttribute("data-confirm-title") || "Are you sure?",
      elt.getAttribute("data-confirm-question") || "",
      elt.getAttribute("data-confirm-label") || "Confirm",
      function () { evt.detail.issueRequest(true); }
    );
  });

  // #159: typed confirmations arm their submit button (delegated, so it
  // survives fragment swaps): data-arms="<button id>" gates on the exact
  // data-arms-match text (default "uninstall").
  document.addEventListener("input", function (ev) {
    var input = ev.target.closest("[data-arms]");
    if (!input) return;
    var btn = document.getElementById(input.getAttribute("data-arms"));
    if (!btn) return;
    var match = input.getAttribute("data-arms-match") || "uninstall";
    btn.disabled = input.value.trim().toLowerCase() !== match.toLowerCase();
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
          if (window.htmx) htmx.process(screenEl); // raw innerHTML swap: re-init hx-*
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
          go(tagParam(btn));
        }
      );
      return;
    }
    go(tagParam(btn));
  });

  // tagParam renders the images payload of a tagged screen button
  // (single-image build/delete); untagged buttons post bare.
  function tagParam(btn) {
    return btn.dataset.tag ? "tag=" + encodeURIComponent(btn.dataset.tag) : null;
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

  // --- SSE hub: one stream per tab, reconnect replay via line serial ---
  function connect() {
    var es = new EventSource(base + "events");
    // Crash path (§12): a hard crash pushes no shutdown — EventSource
    // reconnects on its own; a bounded watch swaps to the same
    // server-stopped screen the quit push renders once the stream
    // stays down, and a successful open clears the watch.
    var downWatch = null;
    function showServerStopped() {
      es.close();
      screenEl.innerHTML =
        '<div class="stub">Server stopped — relaunch oc-sandbox.</div>'; // §12 screen stub
    }
    es.addEventListener("open", function () {
      if (downWatch) { clearTimeout(downWatch); downWatch = null; }
    });
    es.addEventListener("error", function () {
      if (!downWatch) downWatch = setTimeout(showServerStopped, 15000);
    });
    es.addEventListener("line", function (ev) { onLine(ev); });
    es.addEventListener("workspace", function (ev) {
      // reactive re-render (#158, spec §9): fresh rows/markers after
      // every op state change — the log pane re-fills from memory
      var d = null;
      try { d = JSON.parse(ev.data); } catch (e) {}
      if (d && d.state) onCreateWorkspace(d); // the create chain's terminal event (#156)
      else if (currentNav === "ws-open") renderWorkspaces(); // #153: reactive cards
      else if (currentScreen) loadScreen(currentScreen);
    });
    es.addEventListener("update", function (ev) { onUpdate(ev); });
    es.addEventListener("shutdown", function () {
      if (downWatch) clearTimeout(downWatch);
      showServerStopped();
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
      // #153: the busy card's log streams by the same op id
      var opcard = document.querySelector('.card[data-op-id="' + line.op + '"]');
      if (opcard) {
        var oplog = opcard.querySelector("[data-oplog]");
        if (oplog) {
          oplog.hidden = false;
          oplog.textContent += line.text + "\n";
          oplog.scrollTop = oplog.scrollHeight;
        }
      }
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

  // Open Workspace is the primary screen — load it at start (#153).
  var openBtn = document.querySelector('[data-nav="ws-open"]');
  if (openBtn) openBtn.classList.add("active");
  renderWorkspaces();

  connect();
})();
