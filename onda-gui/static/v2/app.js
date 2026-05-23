// Onda v2 — app.js v4
// Fixes: removeFromQueue deletes file, per-group play/pause/stop,
//        mute/solo working, pause→play resumes, no listener leak

(function () {
  "use strict";

  const state = {
    queue: [],
    audioElements: [],
    currentJob: null,
  };

  const $ = (s) => document.querySelector(s);
  const $$ = (s) => document.querySelectorAll(s);

  function stemEmoji(name) {
    const n = name.toLowerCase();
    if (n.includes("drums")) return "🥁";
    if (n.includes("bass")) return "🎸";
    if (n.includes("other")) return "🎹";
    if (n.includes("vocals_viperx") || n.includes("viperx_vocals")) return "🎙️";
    if (n.includes("vocals")) return "🎤";
    if (n.includes("instrumental")) return "🎼";
    return "🎵";
  }

  // ── Init ──
  function init() {
    const dz = $("#drop-zone");
    dz.addEventListener("click", () => $("#file-picker").click());
    dz.addEventListener("dragover", (e) => { e.preventDefault(); dz.classList.add("drag-over"); });
    dz.addEventListener("dragleave", () => dz.classList.remove("drag-over"));
    dz.addEventListener("drop", (e) => {
      e.preventDefault();
      dz.classList.remove("drag-over");
      handleFiles(e.dataTransfer.files);
    });

    $("#file-picker").addEventListener("change", (e) => handleFiles(e.target.files));
    $("#start-btn").addEventListener("click", startAll);
    $("#clear-queue-btn").addEventListener("click", clearQueue);
    $("#clear-results-btn").addEventListener("click", clearResults);
    $("#btn-add-more").addEventListener("click", (e) => { e.stopPropagation(); $("#file-picker").click(); });
    $("#pitch-slider").addEventListener("input", updatePitchLabel);

    // Results delegation — only attach ONCE to #results-panel (never innerHTML'd)
    const rp = $("#results-panel");
    rp.addEventListener("click", onResultsClick);
    rp.addEventListener("input", onResultsInput);

    toggleViperx();
    toggleDemucs();
    toggleRubberband();
  }

  // ── Upload ──
  async function handleFiles(fileList) {
    for (const file of fileList) {
      if (state.queue.find((q) => q.name === file.name)) continue;
      state.queue.push({ name: file.name, checked: true, status: "waiting", progress: 0 });
      try {
        const res = await fetch("/api/upload", {
          method: "POST",
          headers: { "X-Filename": file.name },
          body: file,
        });
        const data = await res.json();
        if (!data.success) toast("Upload failed: " + file.name, "error");
      } catch (e) {
        toast("Upload error: " + file.name, "error");
      }
    }
    renderQueue();
    toast("Uploaded " + fileList.length + " file(s)", "success");
  }

  async function clearQueue() {
    state.queue = [];
    state.currentJob = null;
    stopPolling();
    renderQueue();
    hideResults();
    try { await fetch("/api/clear", { method: "POST" }); } catch (e) {}
    toast("Queue cleared", "success");
  }

  async function removeFromQueue(idx) {
    const f = state.queue[idx];
    if (!f) return;
    state.queue.splice(idx, 1);
    // Also delete from /input
    try { await fetch("/api/delete?file=" + encodeURIComponent(f.name)); } catch (e) {}
    renderQueue();
  }

  // ── Pipeline Config ──
  function getPipelineFlags(forStep) {
    const flags = [];
    const vOn = forStep === "viperx" || (!forStep && $("#chk-viperx").checked);
    const dOn = forStep === "demucs" || (!forStep && $("#chk-demucs").checked);
    const rOn = forStep === "rubberband" || (!forStep && $("#chk-rubberband").checked);
    if (forStep && !vOn && !dOn && !rOn) return "";

    if (vOn) {
      flags.push("viperx=on");
      flags.push("viperx_keep=" + $("#sel-viperx-keep").value);
    }
    if (dOn) {
      flags.push("demucs=on");
      const keeps = [];
      $$("#demucs-keep-group input:checked").forEach((c) => keeps.push(c.value));
      flags.push("demucs_keep=" + (keeps.length ? keeps.join(",") : "all"));
    }
    if (rOn) {
      flags.push("rubberband=on");
      flags.push("pitch=" + $("#pitch-slider").value);
    }
    return flags.join("&");
  }

  function getCheckedFiles() {
    return state.queue.filter((f) => f.checked && f.status !== "done");
  }

  function toggleViperx() { $("#sel-viperx-keep").disabled = !$("#chk-viperx").checked; }
  function toggleDemucs() {
    const on = $("#chk-demucs").checked;
    $("#demucs-keep-group").classList.toggle("disabled", !on);
    $$("#demucs-keep-group input").forEach((c) => (c.disabled = !on));
  }
  function toggleRubberband() {
    const on = $("#chk-rubberband").checked;
    $("#rubberband-group").classList.toggle("disabled", !on);
    $("#pitch-slider").disabled = !on;
  }
  function updatePitchLabel() {
    const v = parseInt($("#pitch-slider").value);
    $("#pitch-label").textContent = v > 0 ? "+" + v + " st" : v + " st";
  }

  // ── Queue ──
  function renderQueue() {
    const panel = $("#queue-panel");
    const list = $("#queue-list");
    const addBtn = $("#btn-add-more");

    if (state.queue.length === 0) {
      panel.style.display = "none";
      addBtn.style.display = "none";
      enableStart();
      return;
    }
    panel.style.display = "";
    addBtn.style.display = "block";
    list.innerHTML = "";

    state.queue.forEach((f, i) => {
      const row = document.createElement("div");
      row.className = "queue-row";
      const pct = f.progress || 0;
      const miniBar = f.status === "processing"
        ? '<div class="queue-progress"><div class="queue-progress-fill" style="width:' + pct + '%"></div></div>'
        : "";

      row.innerHTML =
        '<input type="checkbox" ' + (f.checked ? "checked" : "") + ' data-idx="' + i + '">' +
        '<span class="queue-name">' + esc(f.name) + "</span>" +
        miniBar +
        '<span class="queue-status ' + f.status + '">' + statusLabel(f) + "</span>" +
        '<button class="queue-remove" data-idx="' + i + '" title="Remove & delete file">✕</button>';

      row.querySelector("input[type=checkbox]").addEventListener("change", function () {
        state.queue[i].checked = this.checked;
        enableStart();
      });
      row.querySelector(".queue-remove").addEventListener("click", function (e) {
        e.stopPropagation();
        removeFromQueue(i);
      });
      list.appendChild(row);
    });
    enableStart();
  }

  function statusLabel(f) {
    switch (f.status) {
      case "processing": return "Running";
      case "done": return "✓ Done";
      case "error": return "✗ Error";
      default: return "Waiting";
    }
  }

  function enableStart() {
    const anyChecked = state.queue.some((f) => f.checked);
    const hasRunning = state.queue.some((f) => f.status === "processing");
    $("#start-btn").disabled = !anyChecked || hasRunning;
  }

  // ── Pipeline ──
  async function runStep(step) {
    const files = getCheckedFiles();
    if (files.length === 0) { toast("Check at least one file", "error"); return; }
    const flags = getPipelineFlags(step);
    if (!flags) return;
    await processFiles(files, step, flags);
  }

  async function startAll() {
    const files = getCheckedFiles();
    if (files.length === 0) { toast("Check at least one file", "error"); return; }
    const flags = getPipelineFlags(null);
    if (!flags) { toast("No steps selected", "error"); return; }
    await processFiles(files, "pipeline", flags);
  }

  async function processFiles(files, mode, flags) {
    $("#start-btn").disabled = true;
    $$(".step-run").forEach((b) => (b.disabled = true));
    showProgress();

    for (const f of files) {
      f.status = "processing";
      f.progress = 0;
      state.currentJob = f.name;
      renderQueue();

      const body = flags + "&input_file=" + encodeURIComponent(f.name);
      try {
        const res = await fetch("/api/separate", {
          method: "POST",
          headers: { "Content-Type": "application/x-www-form-urlencoded" },
          body: body,
        });
        const data = await res.json();
        if (!data.success) { f.status = "error"; renderQueue(); continue; }
      } catch (e) { f.status = "error"; renderQueue(); continue; }

      await pollUntilDone(f);
    }

    state.currentJob = null;
    hideProgress();
    $$(".step-run").forEach((b) => (b.disabled = false));
    renderQueue();
    loadResults();
  }

  function pollUntilDone(f) {
    return new Promise((resolve) => {
      let attempts = 0;
      const timer = setInterval(async () => {
        attempts++;
        try {
          const res = await fetch("/api/status");
          const data = await res.json();
          if (data.status === "running") {
            f.progress = data.progress || 0;
            updateProgressBar(data);
            updateQueueTrackProgress(f);
          } else if (data.status === "done" || data.status === "error") {
            clearInterval(timer);
            f.progress = 100;
            f.status = data.status === "error" ? "error" : "done";
            updateProgressBar({ progress: 100, step: "complete" });
            renderQueue();
            resolve();
          }
          if (attempts > 900) { clearInterval(timer); f.status = "error"; renderQueue(); resolve(); }
        } catch (e) {}
      }, 1000);
    });
  }

  function updateQueueTrackProgress(f) {
    const rows = $$("#queue-list .queue-row");
    state.queue.forEach((q, i) => {
      if (q.name === f.name && rows[i]) {
        const bar = rows[i].querySelector(".queue-progress-fill");
        if (bar) bar.style.width = (q.progress || 0) + "%";
        const st = rows[i].querySelector(".queue-status");
        if (st) st.textContent = statusLabel(q);
      }
    });
  }

  function stopPolling() {}

  // ── Progress ──
  function showProgress() {
    $("#progress-bar-container").style.display = "";
    $("#progress-fill").style.width = "0%";
    $("#progress-text").textContent = "Starting...";
  }
  function hideProgress() { $("#progress-bar-container").style.display = "none"; }
  function updateProgressBar(data) {
    $("#progress-fill").style.width = (data.progress || 0) + "%";
    const step = data.step || "";
    const elapsed = data.elapsed ? " · " + fmtTime(data.elapsed) : "";
    const eta = data.eta && data.eta > 0 ? " · ETA " + fmtTime(data.eta) : "";
    $("#progress-text").textContent = (step || "Processing") + " — " + (data.progress || 0) + "%" + elapsed + eta;
  }
  function fmtTime(s) { const m = Math.floor(s / 60); return m > 0 ? m + "m " + (s % 60) + "s" : (s % 60) + "s"; }

  // ── Results ──
  async function loadResults() {
    try {
      const res = await fetch("/api/output");
      const data = await res.json();
      if (!data.files || data.files.length === 0) { hideResults(); return; }
      renderResults(data.files);
    } catch (e) { hideResults(); }
  }

  function renderResults(files) {
    $("#results-panel").style.display = "";
    $("#results-empty").style.display = "none";
    const container = $("#results-content");
    container.innerHTML = "";

    // Group by song
    const groups = {};
    files.forEach((f) => {
      const parts = f.url.replace("/output/", "").split("/");
      const song = parts[0] || "Unknown";
      if (!groups[song]) groups[song] = [];
      groups[song].push(f);
    });

    state.audioElements = [];
    let globalIdx = 0;

    Object.entries(groups).forEach(([song, stems]) => {
      const group = document.createElement("div");
      group.className = "song-group";
      group.dataset.song = song;

      // Per-group controls
      group.innerHTML =
        '<div class="song-title">' +
        '<span>🎵 ' + esc(song) + '</span>' +
        '<div class="song-actions">' +
        '<button class="btn-sm song-play" data-song="' + escAttr(song) + '">▶ Play</button>' +
        '<button class="btn-sm song-pause" data-song="' + escAttr(song) + '">⏸ Pause</button>' +
        '<button class="btn-sm song-stop" data-song="' + escAttr(song) + '">⏹ Stop</button>' +
        '<button class="btn-sm" onclick="window._ondaExportSong(\'' + escAttr(song) + '\')">⬇ Export</button>' +
        '<button class="btn-sm btn-danger" onclick="window._ondaDeleteSong(\'' + escAttr(song) + '\')">🗑 Delete</button>' +
        '</div></div>';

      stems.forEach((f) => {
        const row = document.createElement("div");
        row.className = "stem-row";
        const idx = globalIdx;
        const emoji = stemEmoji(f.name);
        row.innerHTML =
          '<button class="mute" data-idx="' + idx + '">M</button>' +
          '<button class="solo" data-idx="' + idx + '">S</button>' +
          '<span class="stem-emoji">' + emoji + '</span>' +
          '<span class="stem-name">' + esc(f.name) + '</span>' +
          '<input type="range" min="0" max="100" value="100" data-idx="' + idx + '" class="stem-vol-slider">' +
          '<span class="stem-vol">100%</span>' +
          '<a class="stem-dl" href="' + f.url + '" download title="Download">⬇</a>' +
          '<button class="stem-delete" data-idx="' + idx + '" data-file="' + escAttr(f.url) + '" title="Delete">✕</button>' +
          '<audio id="audio-' + idx + '" preload="auto" src="' + f.url + '"></audio>';

        group.appendChild(row);

        const audio = row.querySelector("audio");
        state.audioElements.push({
          name: f.name, song: song, audio: audio,
          muted: false, solo: false, url: f.url, vol: 100,
        });
        globalIdx++;
      });

      container.appendChild(group);
    });

    updateAllStemButtons();
  }

  function hideResults() {
    $("#results-panel").style.display = "none";
    $("#results-empty").style.display = "";
    state.audioElements = [];
  }

  // ── Results event delegation (attached ONCE in init) ──
  function onResultsClick(e) {
    const btn = e.target.closest("button");
    if (!btn) return;

    // Per-group controls
    if (btn.classList.contains("song-play")) { playGroup(btn.dataset.song); return; }
    if (btn.classList.contains("song-pause")) { pauseGroup(btn.dataset.song); return; }
    if (btn.classList.contains("song-stop")) { stopGroup(btn.dataset.song); return; }

    const idx = parseInt(btn.dataset.idx);
    if (isNaN(idx)) return;

    if (btn.classList.contains("mute")) toggleMute(idx);
    else if (btn.classList.contains("solo")) toggleSolo(idx);
    else if (btn.classList.contains("stem-delete")) deleteStem(idx, btn.dataset.file);
  }

  function onResultsInput(e) {
    if (e.target.classList.contains("stem-vol-slider")) {
      const idx = parseInt(e.target.dataset.idx);
      if (!isNaN(idx)) setVolume(idx, parseInt(e.target.value));
    }
  }

  async function clearResults() {
    state.audioElements = [];
    // Delete all output via API
    try {
      const res = await fetch("/api/output");
      const data = await res.json();
      if (data.files) {
        const songs = new Set();
        data.files.forEach((f) => {
          const parts = f.url.replace("/output/", "").split("/");
          if (parts[0]) songs.add(parts[0]);
        });
        for (const song of songs) {
          await fetch("/api/delete?file=" + encodeURIComponent(song));
        }
      }
    } catch (e) {}
    hideResults();
    toast("Results cleared", "success");
  }

  // ── Audio: Per-Group Controls ──
  function playGroup(song) {
    state.audioElements
      .filter((s) => s.song === song)
      .forEach((s) => {
        // Resume if paused (don't reset currentTime if > 0)
        if (s.audio.paused && s.audio.currentTime > 0) {
          // Already has position — just play
        } else {
          s.audio.currentTime = 0;
        }
        s.audio.play().catch(() => {});
      });
  }

  function pauseGroup(song) {
    state.audioElements
      .filter((s) => s.song === song)
      .forEach((s) => s.audio.pause());
  }

  function stopGroup(song) {
    state.audioElements
      .filter((s) => s.song === song)
      .forEach((s) => {
        s.audio.pause();
        s.audio.currentTime = 0;
      });
  }

  function toggleMute(idx) {
    const s = state.audioElements[idx];
    if (!s) return;
    s.muted = !s.muted;
    s.audio.volume = s.muted ? 0 : s.vol / 100;
    updateSingleStemButtons(idx);
  }

  function toggleSolo(idx) {
    const s = state.audioElements[idx];
    if (!s) return;
    const wasSolo = s.solo;
    state.audioElements.forEach((x) => (x.solo = false));
    s.solo = !wasSolo;

    state.audioElements.forEach((x, i) => {
      if (s.solo) {
        x.audio.volume = (i !== idx) ? 0 : x.vol / 100;
      } else {
        x.audio.volume = x.muted ? 0 : x.vol / 100;
      }
    });
    updateAllStemButtons();
  }

  function setVolume(idx, vol) {
    const s = state.audioElements[idx];
    if (!s) return;
    s.vol = vol;
    const isSolod = state.audioElements.some((x) => x.solo);
    if (!s.muted && (!isSolod || s.solo)) {
      s.audio.volume = vol / 100;
    }
    // Update DOM label
    const rows = $$("#results-content .stem-row");
    if (rows[idx]) {
      rows[idx].querySelector(".stem-vol").textContent = vol + "%";
    }
  }

  function updateAllStemButtons() {
    const rows = $$("#results-content .stem-row");
    state.audioElements.forEach((s, i) => {
      if (!rows[i]) return;
      rows[i].querySelector(".mute").classList.toggle("active", s.muted);
      rows[i].querySelector(".solo").classList.toggle("active", s.solo);
    });
  }

  function updateSingleStemButtons(idx) {
    const rows = $$("#results-content .stem-row");
    if (rows[idx]) {
      const s = state.audioElements[idx];
      if (!s) return;
      rows[idx].querySelector(".mute").classList.toggle("active", s.muted);
    }
  }

  async function deleteStem(idx, fileUrl) {
    const s = state.audioElements[idx];
    if (!s) return;
    try { await fetch("/api/delete?file=" + encodeURIComponent(fileUrl)); } catch (e) {}
    toast("Deleted: " + s.name, "success");
    loadResults(); // refresh UI
  }

  async function deleteSong(song) {
    try { await fetch("/api/delete?file=" + encodeURIComponent(song)); } catch (e) {}
    toast("Deleted song: " + song, "success");
    loadResults();
  }

  function exportSong(song) {
    state.audioElements
      .filter((s) => s.song === song)
      .forEach((s) => {
        const a = document.createElement("a");
        a.href = s.url;
        a.download = s.name;
        a.click();
      });
  }

  // ── Globals for inline onclick ──
  window._ondaExportSong = exportSong;
  window._ondaDeleteSong = deleteSong;
  window.runStep = runStep;
  window.startAll = startAll;

  // ── Toast ──
  function toast(msg, type) {
    let el = $("#toast");
    if (!el) {
      el = document.createElement("div");
      el.id = "toast";
      document.body.appendChild(el);
    }
    el.textContent = msg;
    el.className = "show " + (type || "");
    clearTimeout(el._timeout);
    el._timeout = setTimeout(() => el.classList.remove("show"), 2500);
  }

  function esc(s) {
    const d = document.createElement("div");
    d.textContent = s;
    return d.innerHTML;
  }
  function escAttr(s) {
    return s.replace(/&/g, "&amp;").replace(/"/g, "&quot;").replace(/</g, "&lt;").replace(/>/g, "&gt;");
  }

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", init);
  } else {
    init();
  }
})();
