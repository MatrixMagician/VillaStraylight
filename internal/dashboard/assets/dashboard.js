// dashboard.js — the no-build vanilla-JS poll loop for the VillaStraylight control
// dashboard (D-01: no framework). It polls /api/status every ~2500ms, pauses when the
// tab is hidden (visibilitychange / D-05), renders the Health rows + header verdict
// using the project's ready/loading/down/unknown + PASS/WARN/FAIL vocabulary, and on a
// failed poll keeps last-good values dimmed while showing the single global red
// connection banner (D-11), auto-clearing on reconnect. A typed-Unknown signal renders
// as a gray "unavailable" badge, never a fabricated value (D-06/D-11).

(function () {
  "use strict";

  var POLL_MS = 2500;
  var pollTimer = null;

  var banner = document.getElementById("connection-banner");
  var verdictEl = document.getElementById("overall-verdict");
  var healthRows = document.getElementById("health-rows");
  var perfBody = document.getElementById("performance-body");
  var gpuBody = document.getElementById("gpu-body");
  var modelsBody = document.getElementById("models-body");

  // Confirm-dialog elements (the single guarded write, D-08).
  var switchDialog = document.getElementById("switch-dialog");
  var switchTitle = document.getElementById("switch-dialog-title");
  var switchFit = document.getElementById("switch-dialog-fit");
  var switchConfirm = document.getElementById("switch-confirm");
  var switchCancel = document.getElementById("switch-cancel");

  // switching holds the id of the model a switch is in-flight for (drives the row's
  // disabled "Switching…" state until polling shows the new model loaded). null = idle.
  var switching = null;
  // pendingModel is the row the open dialog would switch to (set on Switch click).
  var pendingModel = null;
  // lastFocus is the control to restore focus to when the dialog closes (a11y).
  var lastFocus = null;

  // --- Small DOM helpers --------------------------------------------------

  // mutedP returns a <p class="muted"> with the given text (empty/degraded copy).
  function mutedP(text) {
    var p = document.createElement("p");
    p.className = "muted";
    p.textContent = text;
    return p;
  }

  // metricRow renders one "label  value" line; the value is monospace tabular-nums
  // (.metric-value CSS) so the 2–3s poll never reflows the layout (Typography note).
  function metricRow(label, value) {
    var row = document.createElement("div");
    row.className = "metric-row";
    var l = document.createElement("span");
    l.className = "metric-label";
    l.textContent = label;
    var v = document.createElement("span");
    v.className = "metric-value";
    v.textContent = value;
    row.appendChild(l);
    row.appendChild(v);
    return row;
  }

  // fmtBytes renders a byte count as GiB with one decimal (tabular-friendly).
  function fmtBytes(n) {
    return (n / (1024 * 1024 * 1024)).toFixed(1) + " GiB";
  }

  // --- Vocabulary mapping -------------------------------------------------

  // healthClass maps the shared HealthState (ready/loading/down/unknown) to a
  // dot/badge variant. loading/unknown → warn/unknown (never a confident value).
  function healthClass(health) {
    switch (health) {
      case "ready": return "ready";
      case "down": return "down";
      case "loading": return "warn";
      default: return "unknown"; // "unknown" or anything unexpected → typed-Unknown
    }
  }

  // healthLabel is the human badge text for a HealthState.
  function healthLabel(health) {
    switch (health) {
      case "ready": return "ready";
      case "down": return "down";
      case "loading": return "loading";
      default: return "unavailable";
    }
  }

  // overallClass maps the worst-wins overall verdict (PASS/WARN/FAIL strings from
  // status.Aggregate) to a badge variant.
  function overallClass(overall) {
    switch ((overall || "").toUpperCase()) {
      case "PASS": return "ready";
      case "WARN": return "warn";
      case "FAIL": return "down";
      default: return "unknown";
    }
  }

  // --- Rendering ----------------------------------------------------------

  function renderVerdict(overall) {
    var cls = overallClass(overall);
    verdictEl.className = "badge badge-" + cls;
    verdictEl.textContent = overall ? overall.toUpperCase() : "unavailable";
  }

  function renderHealth(services) {
    if (!services || services.length === 0) {
      healthRows.innerHTML = '<p class="muted">No services in the generated stack.</p>';
      return;
    }
    // Build rows without innerHTML interpolation of server values (XSS-safe).
    healthRows.textContent = "";
    services.forEach(function (svc) {
      var row = document.createElement("div");
      row.className = "health-row";

      var dot = document.createElement("span");
      dot.className = "status-dot " + healthClass(svc.health);
      row.appendChild(dot);

      var name = document.createElement("span");
      name.className = "health-service";
      name.textContent = svc.service;
      row.appendChild(name);

      var badge = document.createElement("span");
      badge.className = "badge badge-" + healthClass(svc.health);
      badge.textContent = healthLabel(svc.health);
      row.appendChild(badge);

      var detail = document.createElement("span");
      detail.className = "health-detail";
      detail.textContent = "active: " + (svc.active || "unknown");
      row.appendChild(detail);

      healthRows.appendChild(row);
    });
  }

  // renderPerformance fills the Performance panel from /api/metrics (DASH-02). It
  // honors the two honesty flags: when the scrape is unavailable it shows
  // "unavailable" (never zeros, D-11); when available-but-idle it shows
  // "Idle — no active generation." (the gauges are stale snapshots, Pitfall 3 / D-10).
  // Only when generating does it present the tok/s as a live rate.
  function renderPerformance(m) {
    perfBody.textContent = "";
    if (!m || !m.available) {
      perfBody.appendChild(mutedP("Unavailable"));
      return;
    }
    // Activity unknown: /metrics returned but /slots failed AND requests_processing==0, so
    // we cannot tell idle from generating-between-requests. Render Unknown, never a
    // fabricated "Idle" (WR-01 / D-11).
    if (!m.activity_known) {
      perfBody.appendChild(mutedP("Activity unknown — slot status unavailable."));
      return;
    }
    if (m.idle) {
      perfBody.appendChild(mutedP("Idle — no active generation."));
      // Only show resting slot context when the slot count is a real reading.
      if (m.slots_known) {
        perfBody.appendChild(metricRow("active slots", String(m.active_slots || 0)));
      }
      return;
    }
    perfBody.appendChild(metricRow("generation", (m.gen_tokens_per_sec || 0).toFixed(1) + " tok/s"));
    perfBody.appendChild(metricRow("prompt", (m.prompt_tokens_per_sec || 0).toFixed(1) + " tok/s"));
    if (m.latency_ms != null) {
      perfBody.appendChild(metricRow("prompt-eval latency", m.latency_ms.toFixed(1) + " ms/tok"));
    }
    if (m.slots_known) {
      perfBody.appendChild(metricRow("active slots", String(m.active_slots || 0)));
    }
  }

  // renderGPU fills the GPU & Memory panel from /api/gpu (DASH-03), MEMORY-FIRST: a
  // used-vs-envelope bar + numeric headline is the lead; the iGPU busy% is a best-effort
  // overlay that shows the gray "Unavailable" badge + the caption when the sysfs reader
  // returns typed-Unknown (D-06) — never a fabricated number.
  function renderGPU(g) {
    gpuBody.textContent = "";
    if (!g) {
      gpuBody.appendChild(mutedP("Unavailable"));
      return;
    }

    // Memory headline (the lead). Each figure carries its own Known flag.
    if (g.mem_used_known && g.mem_envelope_known && g.mem_envelope_bytes > 0) {
      var pct = Math.max(0, Math.min(100, (g.mem_used_bytes / g.mem_envelope_bytes) * 100));
      var bar = document.createElement("div");
      bar.className = "mem-bar";
      var fill = document.createElement("div");
      fill.className = "mem-bar-fill";
      fill.style.width = pct.toFixed(1) + "%";
      bar.appendChild(fill);
      gpuBody.appendChild(bar);
      gpuBody.appendChild(metricRow("unified memory",
        fmtBytes(g.mem_used_bytes) + " / " + fmtBytes(g.mem_envelope_bytes)));
    } else if (g.mem_used_known) {
      gpuBody.appendChild(metricRow("unified memory used", fmtBytes(g.mem_used_bytes)));
      gpuBody.appendChild(metricRow("envelope", "unavailable"));
    } else {
      gpuBody.appendChild(mutedP("Unified-memory usage unavailable"));
    }

    // Busy% overlay — best-effort (D-06). Known → value; Unknown → gray badge + caption.
    var busyRow = document.createElement("div");
    busyRow.className = "metric-row";
    var busyLabel = document.createElement("span");
    busyLabel.className = "metric-label";
    busyLabel.textContent = "GPU utilization";
    busyRow.appendChild(busyLabel);
    if (g.busy_available) {
      var busyVal = document.createElement("span");
      busyVal.className = "metric-value";
      busyVal.textContent = g.busy_percent + "%";
      busyRow.appendChild(busyVal);
      gpuBody.appendChild(busyRow);
    } else {
      var badge = document.createElement("span");
      badge.className = "badge badge-unknown";
      badge.textContent = "Unavailable";
      busyRow.appendChild(badge);
      gpuBody.appendChild(busyRow);
      gpuBody.appendChild(mutedP("GPU utilization isn't reliably reported on this hardware."));
    }
  }

  // renderModels fills the Models panel from /api/models (DASH-04). Each row shows the
  // model id + quant + a badge (loaded / on disk / catalog-only) and a Switch button.
  // Fit-failing rows (fits=false) render the button DISABLED reading "Won't fit" with the
  // fit detail as an inline note (D-08 — never fire a swap the core would reject). The
  // loaded row carries the accent left-border. An empty list shows the empty-state copy.
  // All server values are rendered via textContent (XSS-safe, no innerHTML interpolation).
  function renderModels(models) {
    modelsBody.textContent = "";
    if (!models || models.length === 0) {
      renderModelsEmpty();
      return;
    }

    var list = document.createElement("div");
    list.className = "models-list";

    models.forEach(function (m) {
      var row = document.createElement("div");
      row.className = "model-row";
      if (m.loaded) { row.classList.add("model-loaded"); }

      // id + quant
      var idCol = document.createElement("div");
      idCol.className = "model-id-col";
      var name = document.createElement("span");
      name.className = "model-id";
      name.textContent = m.id;
      idCol.appendChild(name);
      if (m.quant) {
        var quant = document.createElement("span");
        quant.className = "model-quant";
        quant.textContent = m.quant;
        idCol.appendChild(quant);
      }
      row.appendChild(idCol);

      // state badge: loaded / on disk / (catalog-only → no badge)
      if (m.loaded) {
        row.appendChild(modelBadge("loaded", "model-badge-loaded"));
      } else if (m.on_disk) {
        row.appendChild(modelBadge("on disk", "model-badge-ondisk"));
      } else {
        // catalog-only: a spacer keeps the Switch column aligned.
        var spacer = document.createElement("span");
        spacer.className = "model-badge-spacer";
        row.appendChild(spacer);
      }

      // Switch action (the single sanctioned write). Disabled for the loaded row, for a
      // non-fitting target ("Won't fit", D-08), and while a switch is in flight.
      var btn = document.createElement("button");
      btn.type = "button";
      btn.className = "btn btn-primary model-switch";
      if (m.loaded) {
        btn.textContent = "Loaded";
        btn.disabled = true;
        btn.classList.remove("btn-primary");
        btn.classList.add("btn-secondary");
      } else if (!m.fits) {
        btn.textContent = "Won't fit";
        btn.disabled = true;
        btn.title = m.fit_detail || "Does not fit the usable memory envelope.";
        btn.classList.remove("btn-primary");
        btn.classList.add("btn-disabled");
      } else if (switching === m.id) {
        btn.textContent = "Switching…";
        btn.disabled = true;
      } else {
        btn.textContent = "Switch";
        btn.disabled = switching !== null; // block parallel switches
        btn.addEventListener("click", function () { openSwitchDialog(m, btn); });
      }
      row.appendChild(btn);

      // Inline fit note for non-fitting rows (the tooltip is mouse-only; show it inline
      // for keyboard/AT users too).
      list.appendChild(row);
      if (!m.fits && !m.loaded && m.fit_detail) {
        var note = document.createElement("p");
        note.className = "model-fit-note muted";
        note.textContent = m.fit_detail;
        list.appendChild(note);
      }
    });

    modelsBody.appendChild(list);
  }

  // renderModelsEmpty shows the "No models in catalog" empty state (Copywriting Contract).
  function renderModelsEmpty() {
    var h = document.createElement("p");
    h.className = "model-empty-heading";
    h.textContent = "No models in catalog";
    var b = mutedP("No models are available to switch to. Pull one with `villa model pull <id>`, then it appears here.");
    modelsBody.appendChild(h);
    modelsBody.appendChild(b);
  }

  // modelBadge builds a status badge for a model row.
  function modelBadge(text, cls) {
    var badge = document.createElement("span");
    badge.className = "badge " + cls;
    badge.textContent = text;
    return badge;
  }

  // --- Guarded switch flow (D-07/D-08) ------------------------------------

  // openSwitchDialog populates and shows the confirm dialog for a fitting model. It sets
  // the title "Switch to {id}?", the fit-verdict line, and remembers the row id; the
  // restart-warning copy is static in the markup. showModal() traps focus + Esc-cancels.
  function openSwitchDialog(m, triggerBtn) {
    pendingModel = m.id;
    lastFocus = triggerBtn || null;
    switchTitle.textContent = "Switch to " + m.id + "?";
    switchFit.textContent = m.fit_detail || "Fits the usable memory envelope.";
    if (typeof switchDialog.showModal === "function") {
      switchDialog.showModal();
      switchConfirm.focus();
    } else {
      // Fallback for a browser without <dialog>: confirm() honors the same copy.
      if (window.confirm("Switch to " + m.id + "?\n\n" + switchFit.textContent +
        "\n\nThis restarts inference — chat is briefly unavailable.")) {
        doSwitch(m.id);
      } else {
        pendingModel = null;
      }
    }
  }

  // closeSwitchDialog dismisses the dialog and restores focus (a11y).
  function closeSwitchDialog() {
    if (switchDialog.open) { switchDialog.close(); }
    if (lastFocus && typeof lastFocus.focus === "function") { lastFocus.focus(); }
  }

  // doSwitch fires the SINGLE sanctioned write: a same-origin JSON POST to
  // /api/models/switch. The same-origin guard is satisfied by the browser sending Origin/
  // Sec-Fetch-Site automatically on a same-origin fetch; we only need the JSON content
  // type. On dispatch the row enters "Switching…" and the existing polling drives the
  // downloading→restarting→ready transition (no SSE, D-07).
  function doSwitch(id) {
    switching = id;
    poll(); // immediate re-render so the row flips to "Switching…" at once
    fetch("/api/models/switch", {
      method: "POST",
      headers: { "Content-Type": "application/json", "Accept": "application/json" },
      body: JSON.stringify({ model: id })
    }).then(function (resp) {
      return resp.json().catch(function () { return null; }).then(function (res) {
        return { ok: resp.ok, res: res };
      });
    }).then(function (result) {
      var res = result.res;
      // Keep `switching` set ONLY on a genuine success the poll loop can confirm:
      // an HTTP-2xx switched/no_op result (the row flips to "loaded" once polling sees
      // it, then clearSwitchIfLoaded clears). Any other terminal result — a refusal
      // (409/422/404), a server error (500, res.refused=false/switched=false), or a
      // missing/garbled body — clears `switching` now so the row never wedges on
      // "Switching…" forever (WR-06).
      var success = result.ok && res && (res.switched || res.no_op);
      if (!success) {
        switching = null;
        poll();
      }
    }).catch(function () {
      // Network error mid-switch: clear the busy state so the user can retry; the global
      // banner (status poll) will already reflect an unreachable dashboard if relevant.
      switching = null;
      poll();
    });
  }

  // clearSwitchIfLoaded clears the in-flight switch state once the polled model list shows
  // the target as the loaded model (the loading→ready transition completed, D-07).
  function clearSwitchIfLoaded(models) {
    if (switching === null || !models) { return; }
    for (var i = 0; i < models.length; i++) {
      if (models[i].id === switching && models[i].loaded) {
        switching = null;
        return;
      }
    }
  }

  // --- Connection state ---------------------------------------------------

  function setConnected(connected) {
    if (connected) {
      banner.hidden = true;
      document.body.classList.remove("stale");
    } else {
      banner.hidden = false;
      // Keep last-good values visible but dimmed (stale-while-revalidating, D-11).
      document.body.classList.add("stale");
    }
  }

  // --- Poll ---------------------------------------------------------------

  // getJSON fetches a panel endpoint, returning the parsed JSON or null on any
  // failure. A per-panel failure is INDEPENDENT (D-11): the panel renders its own
  // typed-Unknown copy; only the /api/status poll drives the global banner.
  function getJSON(path) {
    return fetch(path, { headers: { "Accept": "application/json" } })
      .then(function (resp) {
        if (!resp.ok) { throw new Error(path + " " + resp.status); }
        return resp.json();
      })
      .catch(function () { return null; });
  }

  function poll() {
    // Health / status drives the GLOBAL connection banner (D-11).
    fetch("/api/status", { headers: { "Accept": "application/json" } })
      .then(function (resp) {
        if (!resp.ok) { throw new Error("status " + resp.status); }
        return resp.json();
      })
      .then(function (report) {
        setConnected(true);
        renderVerdict(report.overall);
        renderHealth(report.services);
      })
      .catch(function () {
        // The dashboard's own API is unreachable → global banner, keep last-good.
        setConnected(false);
      });

    // Performance + GPU degrade INDEPENDENTLY to their own typed-Unknown copy on a
    // per-panel failure; they never touch the global banner (D-11).
    getJSON("/api/metrics").then(renderPerformance);
    getJSON("/api/gpu").then(renderGPU);

    // Models drives the loading→ready transition after a switch: clear the in-flight
    // Switching… state once the target shows as loaded, then re-render the rows.
    getJSON("/api/models").then(function (models) {
      clearSwitchIfLoaded(models);
      renderModels(models);
    });
  }

  // --- Lifecycle (visibilitychange pause / D-05) --------------------------

  function startPolling() {
    if (pollTimer !== null) { return; }
    poll(); // immediate fetch on (re)start
    pollTimer = setInterval(poll, POLL_MS);
  }

  function stopPolling() {
    if (pollTimer !== null) {
      clearInterval(pollTimer);
      pollTimer = null;
    }
  }

  document.addEventListener("visibilitychange", function () {
    if (document.hidden) {
      stopPolling();
    } else {
      startPolling(); // resume + immediate re-fetch
    }
  });

  // --- Confirm-dialog wiring (the single guarded write) -------------------

  if (switchConfirm) {
    switchConfirm.addEventListener("click", function () {
      var id = pendingModel;
      pendingModel = null;
      closeSwitchDialog();
      if (id) { doSwitch(id); }
    });
  }
  if (switchCancel) {
    switchCancel.addEventListener("click", function () {
      pendingModel = null;
      closeSwitchDialog();
    });
  }
  if (switchDialog) {
    // Esc-cancel (native <dialog> "cancel" event) must NOT fire the switch.
    switchDialog.addEventListener("cancel", function () {
      pendingModel = null;
      if (lastFocus && typeof lastFocus.focus === "function") { lastFocus.focus(); }
    });
  }

  // Kick off once the DOM is ready.
  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", startPolling);
  } else {
    startPolling();
  }
})();
