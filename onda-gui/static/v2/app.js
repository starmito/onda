// Onda v2 — app.js v0.1.0
// + Seek slider per group + waveform canvas per stem

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

  function stemColor(name) {
    const n = name.toLowerCase();
    if (n.includes("drums")) return "#ef4444";
    if (n.includes("bass")) return "#3b82f6";
    if (n.includes("other")) return "#22c55e";
    if (n.includes("vocals")) return "#f59e0b";
    if (n.includes("instrumental")) return "#8b5cf6";
    if (n.includes("viperx")) return "#ec4899";
    return "#888";
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

    const rp = $("#results-panel");
    rp.addEventListener("click", onResultsClick);
    rp.addEventListener("input", onResultsInput);

    toggleViperx();
    toggleDemucs();
  }

  // ── Upload ──
  async function handleFiles(fileList) {
    for (const file of fileList) {
      if (state.queue.find((q) => q.name === file.name)) continue;
      state.queue.push({ name: file.name, checked: true, status: "waiting", progress: 0 });
      try {
        const res = await fetch("/api/upload", {
          method: "POST", headers: { "X-Filename": file.name }, body: file,
        });
        const data = await res.json();
        if (!data.success) toast("Upload failed: " + file.name, "error");
      } catch (e) { toast("Upload error: " + file.name, "error"); }
    }
    renderQueue();
    toast("Uploaded " + fileList.length + " file(s)", "success");
  }

  async function clearQueue() {
    state.queue = []; state.currentJob = null; stopPolling();
    renderQueue(); hideResults();
    try { await fetch("/api/clear", { method: "POST" }); } catch (e) {}
    toast("Queue cleared", "success");
  }

  async function removeFromQueue(idx) {
    const f = state.queue[idx];
    if (!f) return;
    state.queue.splice(idx, 1);
    try { await fetch("/api/delete?file=" + encodeURIComponent(f.name)); } catch (e) {}
    renderQueue();
  }

  // ── Pipeline Config ──
  function getPipelineFlags(forStep) {
    const flags = [];
    const vOn = forStep === "viperx" || (!forStep && $("#chk-viperx").checked);
    const dOn = forStep === "demucs" || (!forStep && $("#chk-demucs").checked);
    if (forStep && !vOn && !dOn) return "";

    if (vOn) { flags.push("viperx=on"); flags.push("viperx_keep=" + $("#sel-viperx-keep").value); }
    if (dOn) {
      flags.push("demucs=on");
      const keeps = []; $$("#demucs-keep-group input:checked").forEach((c) => keeps.push(c.value));
      flags.push("demucs_keep=" + (keeps.length ? keeps.join(",") : "all"));
    }
    return flags.join("&");
  }

  function getCheckedFiles() { return state.queue.filter((f) => f.checked && f.status !== "done"); }

  function toggleViperx() { $("#sel-viperx-keep").disabled = !$("#chk-viperx").checked; }
  function toggleDemucs() {
    const on = $("#chk-demucs").checked;
    $("#demucs-keep-group").classList.toggle("disabled", !on);
    $$("#demucs-keep-group input").forEach((c) => (c.disabled = !on));
  }

  // ── Queue ──
  function renderQueue() {
    const panel = $("#queue-panel"), list = $("#queue-list"), addBtn = $("#btn-add-more");
    if (state.queue.length === 0) { panel.style.display = "none"; addBtn.style.display = "none"; enableStart(); return; }
    panel.style.display = ""; addBtn.style.display = "block"; list.innerHTML = "";

    state.queue.forEach((f, i) => {
      const row = document.createElement("div"); row.className = "queue-row";
      const pct = f.progress || 0;
      const miniBar = f.status === "processing"
        ? '<div class="queue-progress"><div class="queue-progress-fill" style="width:' + pct + '%"></div></div>' : "";
      row.innerHTML =
        '<input type="checkbox" ' + (f.checked ? "checked" : "") + ' data-idx="' + i + '">' +
        '<span class="queue-name">' + esc(f.name) + "</span>" + miniBar +
        '<span class="queue-status ' + f.status + '">' + statusLabel(f) + "</span>" +
        '<button class="queue-remove" data-idx="' + i + '" title="Remove & delete">✕</button>';
      row.querySelector("input[type=checkbox]").addEventListener("change", function () { state.queue[i].checked = this.checked; enableStart(); });
      row.querySelector(".queue-remove").addEventListener("click", function (e) { e.stopPropagation(); removeFromQueue(i); });
      list.appendChild(row);
    });
    enableStart();
  }

  function statusLabel(f) {
    switch (f.status) { case "processing": return "Running"; case "done": return "✓ Done"; case "error": return "✗ Error"; default: return "Waiting"; }
  }

  function enableStart() {
    const anyChecked = state.queue.some((f) => f.checked);
    const hasRunning = state.queue.some((f) => f.status === "processing");
    $("#start-btn").disabled = !anyChecked || hasRunning;
  }

  // ── Pipeline ──
  async function runStep(step) {
    const files = getCheckedFiles(); if (files.length === 0) { toast("Check at least one file", "error"); return; }
    const flags = getPipelineFlags(step); if (!flags) return;
    await processFiles(files, step, flags);
  }

  async function startAll() {
    const files = getCheckedFiles(); if (files.length === 0) { toast("Check at least one file", "error"); return; }
    const flags = getPipelineFlags(null); if (!flags) { toast("No steps selected", "error"); return; }
    await processFiles(files, "pipeline", flags);
  }

  async function processFiles(files, mode, flags) {
    $("#start-btn").disabled = true; $$(".step-run").forEach((b) => (b.disabled = true)); showProgress();
    for (const f of files) {
      f.status = "processing"; f.progress = 0; state.currentJob = f.name; renderQueue();
      const body = flags + "&input_file=" + encodeURIComponent(f.name);
      try {
        const res = await fetch("/api/separate", { method: "POST", headers: { "Content-Type": "application/x-www-form-urlencoded" }, body });
        const data = await res.json();
        if (!data.success) { f.status = "error"; renderQueue(); continue; }
      } catch (e) { f.status = "error"; renderQueue(); continue; }
      await pollUntilDone(f);
    }
    state.currentJob = null; hideProgress();
    $$(".step-run").forEach((b) => (b.disabled = false));
    renderQueue(); loadResults();
  }

  function pollUntilDone(f) {
    return new Promise((resolve) => {
      let attempts = 0;
      const timer = setInterval(async () => {
        attempts++;
        try {
          const res = await fetch("/api/status"); const data = await res.json();
          if (data.status === "running") { f.progress = data.progress || 0; updateProgressBar(data); updateQueueTrackProgress(f); }
          else if (data.status === "done" || data.status === "error") {
            clearInterval(timer); f.progress = 100; f.status = data.status === "error" ? "error" : "done";
            updateProgressBar({ progress: 100, step: "complete" }); renderQueue(); resolve();
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
        const bar = rows[i].querySelector(".queue-progress-fill"); if (bar) bar.style.width = (q.progress || 0) + "%";
        const st = rows[i].querySelector(".queue-status"); if (st) st.textContent = statusLabel(q);
      }
    });
  }

  function stopPolling() {}

  function showProgress() { $("#progress-bar-container").style.display = ""; $("#progress-fill").style.width = "0%"; $("#progress-text").textContent = "Starting..."; }
  function hideProgress() { $("#progress-bar-container").style.display = "none"; }
  function updateProgressBar(data) {
    $("#progress-fill").style.width = (data.progress || 0) + "%";
    const step = data.step || "", elapsed = data.elapsed ? " · " + fmtTime(data.elapsed) : "", eta = data.eta && data.eta > 0 ? " · ETA " + fmtTime(data.eta) : "";
    $("#progress-text").textContent = (step || "Processing") + " — " + (data.progress || 0) + "%" + elapsed + eta;
  }
  function fmtTime(s) { const m = Math.floor(s / 60); return m > 0 ? m + "m " + (s % 60) + "s" : (s % 60) + "s"; }

  // ── Results ──
  async function loadResults() {
    try {
      const res = await fetch("/api/output"); const data = await res.json();
      if (!data.files || data.files.length === 0) { hideResults(); return; }
      renderResults(data.files);
    } catch (e) { hideResults(); }
  }

  function renderResults(files) {
    $("#results-panel").style.display = ""; $("#results-empty").style.display = "none";
    const container = $("#results-content"); container.innerHTML = "";

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

      // Song header with per-group controls
      const header = document.createElement("div");
      header.className = "song-title";
      header.innerHTML =
        '<span>🎵 ' + esc(song) + '</span>' +
        '<div class="song-actions">' +
        '<button class="btn-sm song-play" data-song="' + escAttr(song) + '">▶ Play</button>' +
        '<button class="btn-sm song-pause" data-song="' + escAttr(song) + '">⏸ Pause</button>' +
        '<button class="btn-sm song-stop" data-song="' + escAttr(song) + '">⏹ Stop</button>' +
        '<button class="btn-sm" onclick="window._ondaExportSong(\'' + escAttr(song) + '\')">⬇ Export</button>' +
        '<button class="btn-sm btn-danger" onclick="window._ondaDeleteSong(\'' + escAttr(song) + '\')">🗑 Delete</button>' +
        '</div>';
      group.appendChild(header);

      // Seek slider for this group
      const seekRow = document.createElement("div");
      seekRow.className = "seek-row";
      seekRow.innerHTML =
        '<input type="range" class="seek-slider" min="0" max="1000" value="0" data-song="' + escAttr(song) + '">' +
        '<span class="seek-time" data-song="' + escAttr(song) + '">0:00 / 0:00</span>';
      group.appendChild(seekRow);

      const seekSlider = seekRow.querySelector(".seek-slider");
      const seekTime = seekRow.querySelector(".seek-time");
      let isSeeking = false;

      let isFirstStemInGroup = true;
      stems.forEach((f) => {
        const row = document.createElement("div");
        row.className = "stem-row";
        const idx = globalIdx;
        const emoji = stemEmoji(f.name);
        const color = stemColor(f.name);

        row.innerHTML =
          '<button class="tone-btn" data-idx="' + idx + '" title="Select for pitch">TONO</button>' +
          '<button class="mute" data-idx="' + idx + '">M</button>' +
          '<button class="solo" data-idx="' + idx + '">S</button>' +
          '<span class="stem-emoji">' + emoji + '</span>' +
          '<span class="stem-name">' + esc(f.name.replace(/\.[^.]+$/, '') + ' - ' + song) + '</span>' +
          '<canvas class="waveform-canvas" data-idx="' + idx + '" width="200" height="32"></canvas>' +
          '<input type="range" min="0" max="100" value="100" data-idx="' + idx + '" class="stem-vol-slider">' +
          '<span class="stem-vol">100%</span>' +
          '<a class="stem-dl" href="' + f.url + '" download title="Download">⬇</a>' +
          '<button class="stem-delete" data-idx="' + idx + '" data-file="' + escAttr(f.url) + '" title="Delete">✕</button>' +
          '<audio id="audio-' + idx + '" preload="auto" src="' + f.url + '" crossorigin="anonymous"></audio>';

        group.appendChild(row);

        const audio = row.querySelector("audio");
        const entry = {
          name: f.name, song: song, audio: audio,
          muted: false, solo: false, url: f.url, vol: 100,
          canvas: row.querySelector(".waveform-canvas"),
        };
        state.audioElements.push(entry);

        // Draw waveform async — stores entry.duration, then inits seek slider
        drawWaveform(entry).then(() => {
          if (entry.duration && entry.duration > 0) {
            initSeekSliderForGroup(song);
          }
        });

        // Sync seek slider — attach to first stem of this group
        if (isFirstStemInGroup) {
          audio.addEventListener("timeupdate", () => {
            if (isSeeking) return;
            const dur = (isFinite(audio.duration) && audio.duration > 0) ? audio.duration : (entry.duration || state.audioElements.find(s => s.song === song && s.duration)?.duration);
            if (!dur || isNaN(dur) || dur <= 0) return;
            seekSlider.max = Math.floor(dur * 1000);
            seekSlider.value = Math.floor(audio.currentTime * 1000);
            seekTime.textContent = fmtTimeSec(audio.currentTime) + " / " + fmtTimeSec(dur);
          });
          isFirstStemInGroup = false;
        }

        globalIdx++;
      });

      // Seek slider: input = seeking, change = done seeking
      seekSlider.addEventListener("input", () => {
        isSeeking = true;
        const t = parseInt(seekSlider.value) / 1000;
        seekGroup(song, t);
        seekTime.textContent = fmtTimeSec(t) + " / " + fmtTimeSec((parseInt(seekSlider.max) || 1000) / 1000);
      });
      seekSlider.addEventListener("change", () => {
        isSeeking = false;
      });

      // ── Pitch controls per song ──
      const pitchRow = document.createElement("div");
      pitchRow.className = "pitch-controls";
      pitchRow.innerHTML =
        '<span class="pitch-label">↕ Tono</span>' +
        '<input type="range" class="pitch-slider" min="-12" max="12" value="0" step="1" data-song="' + escAttr(song) + '" size="30">' +
        '<input class="pitch-val" data-song="' + escAttr(song) + '" value="0 st" size="5">' +
        '<button class="btn-sm pitch-apply" data-song="' + escAttr(song) + '">Apply</button>' +
        '<span class="pitch-status" data-song="' + escAttr(song) + '"></span>';
      group.appendChild(pitchRow);

      // Pitch slider label update
      const pitchSlider = pitchRow.querySelector(".pitch-slider");
      const pitchVal = pitchRow.querySelector(".pitch-val");
      pitchSlider.addEventListener("input", function () {
        const v = parseInt(this.value);
        pitchVal.value = (v >= 0 ? "+" : "") + v + " st";
      });
      // Editable pitch value: type number, Enter applies
      pitchVal.addEventListener("keydown", function (e) {
        if (e.key === "Enter") {
          e.preventDefault();
          const m = this.value.match(/[+-]?\d+/);
          if (m) {
            let v = parseInt(m[0]);
            v = Math.max(-12, Math.min(12, v));
            pitchSlider.value = v;
            this.value = (v >= 0 ? "+" : "") + v + " st";
            pitchSlider.dispatchEvent(new Event("input"));
          } else {
            this.value = (parseInt(pitchSlider.value) >= 0 ? "+" : "") + parseInt(pitchSlider.value) + " st";
          }
        }
      });
      pitchVal.addEventListener("blur", function () {
        const m = this.value.match(/[+-]?\d+/);
        if (m) {
          let v = parseInt(m[0]);
          v = Math.max(-12, Math.min(12, v));
          pitchSlider.value = v;
          this.value = (v >= 0 ? "+" : "") + v + " st";
        } else {
          this.value = (parseInt(pitchSlider.value) >= 0 ? "+" : "") + parseInt(pitchSlider.value) + " st";
        }
      });

      // ── Pitch-shifted results area (indented) ──
      const pitchResults = document.createElement("div");
      pitchResults.className = "pitch-results";
      pitchResults.dataset.song = song;
      pitchResults.style.display = "none";
      pitchResults.style.marginTop = "16px";
      group.appendChild(pitchResults);

      container.appendChild(group);
    });

    updateAllStemButtons();
  }

  // ── Waveform ──
  async function drawWaveform(entry) {
    const canvas = entry.canvas;
    if (!canvas) return;
    const ctx = canvas.getContext("2d");
    const color = stemColor(entry.name);

    // Draw placeholder
    const w = canvas.width, h = canvas.height;
    ctx.fillStyle = "rgba(255,255,255,0.03)";
    ctx.fillRect(0, 0, w, h);

    try {
      const res = await fetch(entry.url);
      const buf = await res.arrayBuffer();
      const actx = new (window.AudioContext || window.webkitAudioContext)();
      const audioBuf = await actx.decodeAudioData(buf);

      // Get peaks
      const channel = audioBuf.getChannelData(0);
      const step = Math.floor(channel.length / w);
      const peaks = [];
      for (let i = 0; i < w; i++) {
        let max = 0;
        for (let j = 0; j < step; j++) {
          const v = Math.abs(channel[i * step + j] || 0);
          if (v > max) max = v;
        }
        peaks.push(max);
      }
      actx.close();

      // Store duration for seek slider
      entry.duration = audioBuf.duration;

      // Draw
      ctx.clearRect(0, 0, w, h);
      ctx.fillStyle = "rgba(255,255,255,0.03)";
      ctx.fillRect(0, 0, w, h);
      const mid = h / 2;
      for (let i = 0; i < w; i++) {
        const barH = peaks[i] * mid * 0.85;
        ctx.fillStyle = color;
        ctx.globalAlpha = 0.7;
        ctx.fillRect(i, mid - barH, 1, barH * 2);
      }
      ctx.globalAlpha = 1;
    } catch (e) {
      // Draw error placeholder
      ctx.fillStyle = "rgba(255,255,255,0.05)";
      ctx.fillRect(0, 0, w, h);
    }
  }

  function hideResults() {
    $("#results-panel").style.display = "none"; $("#results-empty").style.display = "";
    state.audioElements = [];
  }

  // ── Results event delegation ──
  function onResultsClick(e) {
    const btn = e.target.closest("button");
    
    if (btn && btn.classList.contains("pitch-apply")) {
      applyPitch(btn.dataset.song);
      return;
    }

    if (!btn) return;

    if (btn.classList.contains("tone-btn")) {
      btn.classList.toggle("active");
      return;
    }

    if (btn.classList.contains("song-play")) { playGroup(btn.dataset.song); return; }
    if (btn.classList.contains("song-pause")) { pauseGroup(btn.dataset.song); return; }
    if (btn.classList.contains("song-stop")) { stopGroup(btn.dataset.song); return; }

    const row = btn.closest(".stem-row");
    const idx = parseInt(btn.dataset.idx);
    if (isNaN(idx) || !row) return;

    if (btn.classList.contains("mute")) {
      toggleMute(idx);
      row.querySelector(".mute").classList.toggle("active", state.audioElements[idx]?.muted);
    } else if (btn.classList.contains("solo")) {
      toggleSolo(idx);
      // Update ALL rows directly from DOM
      $$("#results-content .stem-row").forEach((r, i) => {
        const s = state.audioElements[i];
        if (!s) return;
        const muteBtn = r.querySelector(".mute");
        const soloBtn = r.querySelector(".solo");
        if (muteBtn) muteBtn.classList.toggle("active", s.muted);
        if (soloBtn) soloBtn.classList.toggle("active", s.solo);
      });
    } else if (btn.classList.contains("stem-delete")) {
      deleteStem(idx, btn.dataset.file);
    }
  }

  function onResultsInput(e) {
    if (e.target.classList.contains("stem-vol-slider")) {
      const idx = parseInt(e.target.dataset.idx);
      if (!isNaN(idx)) setVolume(idx, parseInt(e.target.value));
    }
  }

  async function clearResults() {
    state.audioElements = [];
    try {
      const res = await fetch("/api/output"); const data = await res.json();
      if (data.files) {
        const songs = new Set();
        data.files.forEach((f) => {
          const parts = f.url.replace("/output/", "").split("/");
          if (parts[0]) songs.add(parts[0]);
        });
        for (const song of songs) { await fetch("/api/delete?file=" + encodeURIComponent(song)); }
      }
    } catch (e) {}
    hideResults();
    toast("Results cleared", "success");
  }

  // ── Audio: Per-Group ──
  function playGroup(song) {
    state.audioElements.filter((s) => s.song === song).forEach((s) => {
      if (!(s.audio.paused && s.audio.currentTime > 0)) s.audio.currentTime = 0;
      s.audio.play().catch(() => {});
    });
  }

  function pauseGroup(song) {
    state.audioElements.filter((s) => s.song === song).forEach((s) => s.audio.pause());
  }

  function stopGroup(song) {
    state.audioElements.filter((s) => s.song === song).forEach((s) => { s.audio.pause(); s.audio.currentTime = 0; });
  }

  function seekGroup(song, time) {
    state.audioElements.filter((s) => s.song === song).forEach((s) => { s.audio.currentTime = time; });
  }

  function toggleMute(idx) {
    const s = state.audioElements[idx]; if (!s) return;
    s.muted = !s.muted;
    const anySolo = state.audioElements.some(x => x.solo);
    // If any stem has solo active, only the solo'd stem controls volume — mute is cosmetic
    if (!anySolo) {
      s.audio.volume = s.muted ? 0 : s.vol / 100;
    }
    updateSingleStemButtons(idx);
  }

  function toggleSolo(idx) {
    const s = state.audioElements[idx]; if (!s) return;
    const wasSolo = s.solo;
    state.audioElements.forEach((x) => (x.solo = false));
    s.solo = !wasSolo;
    state.audioElements.forEach((x, i) => {
      x.audio.volume = s.solo ? (i !== idx ? 0 : x.vol / 100) : (x.muted ? 0 : x.vol / 100);
    });
    updateAllStemButtons();
  }

  function setVolume(idx, vol) {
    const s = state.audioElements[idx]; if (!s) return;
    s.vol = vol;
    const isSolod = state.audioElements.some((x) => x.solo);
    if (!s.muted && (!isSolod || s.solo)) s.audio.volume = vol / 100;
    const rows = $$("#results-content .stem-row");
    if (rows[idx]) rows[idx].querySelector(".stem-vol").textContent = vol + "%";
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
      const s = state.audioElements[idx]; if (!s) return;
      rows[idx].querySelector(".mute").classList.toggle("active", s.muted);
    }
  }

  async function deleteStem(idx, fileUrl) {
    const s = state.audioElements[idx]; if (!s) return;
    try { await fetch("/api/delete?file=" + encodeURIComponent(fileUrl)); } catch (e) {}
    toast("Deleted: " + s.name, "success");
    loadResults();
  }

  async function deleteSong(song) {
    try { await fetch("/api/delete?file=" + encodeURIComponent(song)); } catch (e) {}
    toast("Deleted song: " + song, "success");
    loadResults();
  }

  function exportSong(song) {
    state.audioElements.filter((s) => s.song === song).forEach((s) => {
      const a = document.createElement("a"); a.href = s.url; a.download = s.name; a.click();
    });
  }

  async function applyPitch(song) {
    // Collect all original stems for this song (not already pitch-shifted)
    const allStems = state.audioElements.filter(s =>
      s.song === song && !s.url.match(/\(\+[-\d]+\)/)
    );

    if (allStems.length === 0) return;

    // Get pitch value from slider
    const slider = document.querySelector('.pitch-slider[data-song="' + CSS.escape(song) + '"]');
    // Read pitch from slider (authoritative), fallback to input
    let pitch = slider ? parseInt(slider.value) : 0;
    const valInput = document.querySelector('.pitch-val[data-song="' + CSS.escape(song) + '"]');
    if (valInput && valInput.value) {
      const m = valInput.value.match(/[+-]?\d+/);
      if (m) pitch = parseInt(m[0]);
    }

    // Collect checked tone stems
    const checkedIdx = new Set();
    const rows = $$("#results-content .stem-row");
    allStems.forEach((s) => {
      const idx = state.audioElements.indexOf(s);
      if (idx >= 0 && rows[idx]) {
        const tb = rows[idx].querySelector(".tone-btn");
        if (tb && tb.classList.contains("active")) checkedIdx.add(idx);
      }
    });

    const payload = {
      stems: allStems.map((s) => ({
        url: s.url,
        pitch: checkedIdx.has(state.audioElements.indexOf(s)),
      })),
      pitch: pitch,
    };

    const statusEl = document.querySelector('.pitch-status[data-song="' + CSS.escape(song) + '"]');
    if (statusEl) statusEl.textContent = "Processing...";

    try {
      const res = await fetch("/api/rubberband", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(payload),
      });
      const data = await res.json();
      if (!data.success || !data.files) {
        if (statusEl) statusEl.textContent = "Error";
        toast("Pitch shift failed", "error");
        return;
      }

      // Render pitch-shifted results in the indented area
      const pitchDiv = document.querySelector('.pitch-results[data-song="' + CSS.escape(song) + '"]');
      if (!pitchDiv) return;

      pitchDiv.style.display = "";
      pitchDiv.innerHTML = "";

      // Make a mini-player for pitch-shifted group
      const pitchAudioElements = [];
      const header = document.createElement("div");
      header.className = "song-title";  // Match main group style
      header.innerHTML =
        '<span>↕ ' + esc(song) + ' (' + (pitch >= 0 ? "+" : "") + pitch + ')</span>' +
        '<div class="song-actions">' +
        '<button class="btn-sm pitch-play">▶ Play</button>' +
        '<button class="btn-sm pitch-pause">⏸ Pause</button>' +
        '<button class="btn-sm pitch-stop">⏹ Stop</button>' +
        '<button class="btn-sm pitch-export">⬇ Export</button>' +
        '<button class="btn-sm btn-danger pitch-delete">🗑 Delete</button>' +
        '</div>';
      pitchDiv.appendChild(header);

      // Seek slider for pitch group
      const seekRow = document.createElement("div");
      seekRow.className = "seek-row";
      const pitchSliderId = "pitch-seek-" + song.replace(/[^a-zA-Z0-9]/g, "_");
      seekRow.innerHTML =
        '<input type="range" class="seek-slider" min="0" max="1000" value="0" id="' + pitchSliderId + '">' +
        '<span class="seek-time">0:00 / 0:00</span>';
      pitchDiv.appendChild(seekRow);

      const pSeek = seekRow.querySelector(".seek-slider");
      const pTime = seekRow.querySelector(".seek-time");
      let pSeeking = false;

      data.files.forEach((f, i) => {
        const row = document.createElement("div");
        row.className = "stem-row pitch-stem";
        const color = stemColor(f.name);
        const emoji = stemEmoji(f.name);
        // Display: "vocals (+2).wav" -> "vocals - Song Name (+2)"
        const baseName = f.name.replace(/\s*\([+-]?\d+\)\.[^.]+$/, '');
        const pitchMatch = f.name.match(/\(([+-]?\d+)\)/);
        const suffix = pitchMatch ? ' (' + pitchMatch[1] + ')' : '';
        const displayName = baseName + ' - ' + song + suffix;
        row.innerHTML =
          '<button class="mute" data-pitch-idx="' + i + '">M</button>' +
          '<button class="solo" data-pitch-idx="' + i + '">S</button>' +
          '<span class="stem-emoji">' + emoji + '</span>' +
          '<span class="stem-name">' + esc(displayName) + '</span>' +
          '<canvas class="waveform-canvas" width="200" height="32"></canvas>' +
          '<input type="range" min="0" max="100" value="100" data-pitch-idx="' + i + '" class="stem-vol-slider">' +
          '<span class="stem-vol">100%</span>' +
          '<a class="stem-dl" href="' + f.url + '" download>⬇</a>' +
          '<button class="stem-delete" data-file="' + escAttr(f.url) + '" title="Delete">✕</button>' +
          '<audio preload="auto" src="' + f.url + '" crossorigin="anonymous"></audio>';

        pitchDiv.appendChild(row);

        const audio = row.querySelector("audio");
        const entry = {
          name: f.name, song: song + "_pitch", audio: audio,
          muted: false, solo: false, url: f.url, vol: 100,
          canvas: row.querySelector(".waveform-canvas"),
        };
        pitchAudioElements.push(entry);

        // Placeholder waveform — no fetch to avoid competing with <audio> loading
        const pctx = entry.canvas.getContext("2d");
        pctx.fillStyle = "rgba(255,255,255,0.03)";
        pctx.fillRect(0, 0, entry.canvas.width, entry.canvas.height);

        // Timeupdate on first
        if (i === 0) {
          audio.addEventListener("timeupdate", () => {
            if (pSeeking) return;
            const dur = entry.duration || pitchAudioElements.find(e => e.duration)?.duration;
            if (!dur || dur <= 0) return;
            pSeek.max = Math.floor(dur * 1000);
            pSeek.value = Math.floor(audio.currentTime * 1000);
            pTime.textContent = fmtTimeSec(audio.currentTime) + " / " + fmtTimeSec(dur);
          });
        }

        // Mute/Solo
        row.querySelector(".mute").addEventListener("click", function () {
          entry.muted = !entry.muted;
          const anySolo = pitchAudioElements.some(x => x.solo);
          if (!anySolo) entry.audio.volume = entry.muted ? 0 : entry.vol / 100;
          this.classList.toggle("active", entry.muted);
        });
        row.querySelector(".solo").addEventListener("click", function () {
          const wasSolo = entry.solo;
          pitchAudioElements.forEach(x => x.solo = false);
          entry.solo = !wasSolo;
          pitchAudioElements.forEach((x, j) => {
            x.audio.volume = entry.solo ? (j !== i ? 0 : x.vol / 100) : (x.muted ? 0 : x.vol / 100);
          });
          $$(".pitch-stem .solo").forEach((b, j) => b.classList.toggle("active", pitchAudioElements[j]?.solo));
          $$(".pitch-stem .mute").forEach((b, j) => b.classList.toggle("active", pitchAudioElements[j]?.muted));
        });
        row.querySelector(".stem-vol-slider").addEventListener("input", function () {
          entry.vol = parseInt(this.value);
          const s = pitchAudioElements.some(x => x.solo);
          if (!entry.muted && (!s || entry.solo)) entry.audio.volume = entry.vol / 100;
          row.querySelector(".stem-vol").textContent = entry.vol + "%";
        });
        row.querySelector(".stem-delete").addEventListener("click", async function () {
          const fu = this.dataset.file;
          try { await fetch("/api/delete?file=" + encodeURIComponent(fu)); } catch(e) {}
          row.remove();
          pitchAudioElements.splice(i, 1);
        });
      });

      // Seek slider events
      pSeek.addEventListener("input", () => {
        pSeeking = true;
        const t = parseInt(pSeek.value) / 1000;
        pitchAudioElements.forEach(e => { e.audio.currentTime = t; });
        pTime.textContent = fmtTimeSec(t) + " / " + fmtTimeSec((parseInt(pSeek.max) || 1000) / 1000);
      });
      pSeek.addEventListener("change", () => { pSeeking = false; });

      // Play/Pause/Stop buttons
      header.querySelector(".pitch-play").addEventListener("click", () => {
        pitchAudioElements.forEach(e => e.audio.play().catch(()=>{}));
      });
      header.querySelector(".pitch-pause").addEventListener("click", () => {
        pitchAudioElements.forEach(e => e.audio.pause());
      });
      header.querySelector(".pitch-stop").addEventListener("click", () => {
        pitchAudioElements.forEach(e => { e.audio.pause(); e.audio.currentTime = 0; });
      });

      // Export all pitch-shifted stems
      header.querySelector(".pitch-export").addEventListener("click", () => {
        pitchAudioElements.forEach(e => {
          const a = document.createElement("a");
          a.href = e.url; a.download = e.name; a.click();
        });
      });

      // Delete all pitch-shifted stems
      header.querySelector(".pitch-delete").addEventListener("click", async () => {
        for (const e of pitchAudioElements) {
          try { await fetch("/api/delete?file=" + encodeURIComponent(e.url)); } catch (_) {}
        }
        pitchDiv.innerHTML = "";
        pitchDiv.style.display = "none";
        toast("Pitch group deleted", "success");
      });

      if (statusEl) { statusEl.textContent = "Done ✓"; statusEl.style.color = "#22c55e"; }
      toast("Pitch shift complete", "success");
    } catch (e) {
      if (statusEl) statusEl.textContent = "Error";
      toast("Pitch shift failed: " + e.message, "error");
    }
  }

  function fmtTimeSec(s) {
    const m = Math.floor(s / 60), sec = Math.floor(s % 60);
    return m + ":" + (sec < 10 ? "0" : "") + sec;
  }

  // Called after waveform loads with real duration
  function initSeekSliderForGroup(song) {
    const wfDur = state.audioElements
      .filter((s) => s.song === song)
      .reduce((max, s) => Math.max(max, s.duration || 0), 0);
    if (wfDur <= 0) return;
    // Find the seek slider for this song group
    const group = document.querySelector('.song-group[data-song="' + CSS.escape(song) + '"]');
    if (!group) return;
    const slider = group.querySelector(".seek-slider");
    const time = group.querySelector(".seek-time");
    if (slider && time) {
      slider.max = Math.floor(wfDur * 1000);
      time.textContent = "0:00 / " + fmtTimeSec(wfDur);
    }
  }

  // ── Globals ──
  window._ondaExportSong = exportSong;
  window._ondaDeleteSong = deleteSong;
  window.runStep = runStep;
  window.startAll = startAll;
  window.toggleViperx = toggleViperx;
  window.toggleDemucs = toggleDemucs;

  // ── Toast ──
  function toast(msg, type) {
    let el = $("#toast");
    if (!el) { el = document.createElement("div"); el.id = "toast"; document.body.appendChild(el); }
    el.textContent = msg; el.className = "show " + (type || "");
    clearTimeout(el._timeout); el._timeout = setTimeout(() => el.classList.remove("show"), 2500);
  }

  function esc(s) { const d = document.createElement("div"); d.textContent = s; return d.innerHTML; }
  function escAttr(s) { return s.replace(/&/g, "&amp;").replace(/"/g, "&quot;").replace(/</g, "&lt;").replace(/>/g, "&gt;"); }

  if (document.readyState === "loading") document.addEventListener("DOMContentLoaded", init);
  else init();
})();
