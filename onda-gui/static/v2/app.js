// Onda v2 — app.js v0.1.0
// + Seek slider per group + waveform canvas per stem

(function () {
  "use strict";

  const state = {
    queue: [],
    stems: [],            // Web Audio API: {name, song, url, canvas, muted, solo, vol, duration, buffer, gainNode}
    currentJob: null,
    activeGroup: null,
    audioCtx: null,
    groupOffsets: {},     // playback position per group: {song: seconds}
    rafId: null,          // requestAnimationFrame for seek slider
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
    loadVersion();
  }

  // ── Version ──
  async function loadVersion() {
    const el = document.getElementById("app-version");
    if (!el) return;
    try {
      const r = await fetch("/VERSION");
      if (r.ok) {
        const v = (await r.text()).trim();
        el.textContent = "v" + v;
      }
    } catch {
      el.textContent = "desarrollo";
    }
  }

  // ── Web Audio API ──
  function getAudioCtx() {
    if (!state.audioCtx) {
      state.audioCtx = new (window.AudioContext || window.webkitAudioContext)();
    }
    if (state.audioCtx.state === "suspended") state.audioCtx.resume();
    return state.audioCtx;
  }

  async function loadStemBuffer(url) {
    const cached = state.stems.find(s => s.url === url);
    if (cached && cached.buffer) return cached.buffer;
    const res = await fetch(url);
    if (!res.ok) throw new Error("fetch failed");
    const buf = await res.arrayBuffer();
    const ctx = getAudioCtx();
    const audioBuf = await ctx.decodeAudioData(buf);
    // Store in stem entry
    const stem = state.stems.find(s => s.url === url);
    if (stem) stem.buffer = audioBuf;
    return audioBuf;
  }

  async function preloadGroupBuffers(song) {
    const stems = state.stems.filter(s => s.song === song && !s.buffer);
    for (let i = 0; i < stems.length; i++) {
      try {
        await loadStemBuffer(stems[i].url);
        // Update mini progress (optional)
      } catch (e) {
        console.warn("Buffer load failed:", stems[i].url, e);
      }
    }
  }

  function getGroupOffset(song) {
    return state.groupOffsets[song] || 0;
  }

  function setGroupOffset(song, offset) {
    state.groupOffsets[song] = offset;
  }

  function stopGroupSources(song) {
    state.stems.filter(s => s.song === song).forEach(s => {
      if (s.sourceNode) {
        try { s.sourceNode.stop(); } catch (_) {}
        s.sourceNode.disconnect();
        s.sourceNode = null;
      }
    });
  }

  function createAndStartSources(song, offset) {
    const ctx = getAudioCtx();
    const now = ctx.currentTime;
    let maxDur = 0;

    state.stems.filter(s => s.song === song && s.buffer).forEach(s => {
      // Create nodes
      s.sourceNode = ctx.createBufferSource();
      s.sourceNode.buffer = s.buffer;
      s.gainNode = ctx.createGain();
      s.sourceNode.connect(s.gainNode);
      s.gainNode.connect(ctx.destination);

      // Apply mute/solo/volume
      updateGainForStem(s);

      // Start at offset
      const dur = s.buffer.duration - offset;
      if (dur > 0) {
        s.sourceNode.start(now, offset, dur);
        if (s.buffer.duration > maxDur) maxDur = s.buffer.duration;
      }
      s.duration = s.buffer.duration;
    });

    // Store start time on each stem for rAF loop
    state.stems.filter(s => s.song === song && s.buffer).forEach(s => {
      s._startTime = now;
    });

    return { startTime: now, totalDuration: maxDur };
  }

  function updateGainForStem(stem) {
    if (!stem.gainNode) return;
    const anySolo = state.stems.some(s => s.song === stem.song && s.solo);
    if (anySolo) {
      stem.gainNode.gain.value = stem.solo ? stem.vol / 100 : 0;
    } else {
      stem.gainNode.gain.value = stem.muted ? 0 : stem.vol / 100;
    }
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
    state.stems = [];
    state.groupOffsets = {};
    stopSeekSliderLoop();
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
          '<a class="stem-dl" href="' + f.url + '?cb=' + Date.now() + '" download title="Download">⬇</a>' +
          '<button class="stem-delete" data-idx="' + idx + '" data-file="' + escAttr(f.url) + '" title="Delete">✕</button>';

        group.appendChild(row);

        const entry = {
          name: f.name, song: song,
          muted: false, solo: false, url: f.url, vol: 100,
          canvas: row.querySelector(".waveform-canvas"),
        };
        if (entry.canvas) entry.canvas.dataset.wfState = "";
        state.audioElements.push(entry);
        // Also track in Web Audio stems array
        state.stems.push({
          name: entry.name, song: entry.song, url: entry.url,
          canvas: entry.canvas, muted: false, solo: false, vol: 100,
          duration: 0, buffer: null, gainNode: null, sourceNode: null,
        });

        // Defer waveform to avoid HTTP competition with <audio> loading
        // Will be drawn when group is activated (see activateGroup)

        // Timeupdate handled by Web Audio rAF loop (startSeekSliderLoop)

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

      // ── Pitch-shifted results area (SIBLING, not child) ──
      const pitchResults = document.createElement("div");
      pitchResults.className = "pitch-results";
      pitchResults.dataset.song = song + "_pitch";
      pitchResults.style.display = "none";
      pitchResults.style.marginTop = "16px";

      // Click group area to activate for playback
      group.addEventListener("click", function (e) {
        if (e.target.closest("button, input, a, .seek-slider, .stem-vol-slider")) return;
        activateGroup(song);
      });

      // Click pitch area to activate pitch group
      pitchResults.addEventListener("click", function (e) {
        if (e.target.closest("button, input, a, .seek-slider, .stem-vol-slider")) return;
        e.stopPropagation();
        activateGroup(song + "_pitch");
      });

      container.appendChild(group);
      container.appendChild(pitchResults);
    });

    updateAllStemButtons();
  }

  // ── Waveform from URL (for deferred pitch drawing) ──
  async function drawWaveformFromAudio(canvas, url, color) {
    if (!canvas || !url) return;
    // Skip if already loaded or currently loading
    if (canvas.dataset.wfState === "loaded" || canvas.dataset.wfState === "loading") return;
    canvas.dataset.wfState = "loading";

    const ctx = canvas.getContext("2d");
    const w = canvas.width, h = canvas.height;
    const fillColor = color || "#8b5cf6";

    // Loading indicator
    ctx.fillStyle = "rgba(255,255,255,0.03)";
    ctx.fillRect(0, 0, w, h);
    ctx.fillStyle = "rgba(255,255,255,0.15)";
    ctx.font = "10px monospace";
    ctx.textAlign = "center";
    ctx.fillText("...", w/2, h/2+4);
    ctx.textAlign = "start";

    try {
      const peaksUrl = "/api/peaks?file=" + encodeURIComponent(url) + "&n=" + w;
      const res = await fetch(peaksUrl);
      if (!res.ok) throw new Error("peaks fetch failed");
      const data = await res.json();
      const peaks = data.peaks || [];
      if (!peaks.length) throw new Error("no peaks");

      ctx.clearRect(0, 0, w, h);
      ctx.fillStyle = "rgba(255,255,255,0.03)";
      ctx.fillRect(0, 0, w, h);
      const mid = h / 2;
      for (let i = 0; i < w && i < peaks.length; i++) {
        const barH = peaks[i] * mid * 0.85;
        ctx.fillStyle = fillColor;
        ctx.globalAlpha = 0.7;
        ctx.fillRect(i, mid - barH, 1, barH * 2);
      }
      ctx.globalAlpha = 1;
      canvas.dataset.wfState = "loaded";
    } catch (e) {
      // Error indicator — visible so user knows something went wrong
      ctx.fillStyle = "rgba(255,0,0,0.08)";
      ctx.fillRect(0, 0, w, h);
      ctx.fillStyle = "rgba(255,100,100,0.5)";
      ctx.font = "8px monospace";
      ctx.textAlign = "center";
      ctx.fillText("err", w/2, h/2+3);
      ctx.textAlign = "start";
      canvas.dataset.wfState = "error";
    }
  }

  // ── Waveform ──
  async function drawWaveform(entry) {
    const canvas = entry.canvas;
    if (!canvas) return;
    const ctx = canvas.getContext("2d");
    const color = stemColor(entry.name);
    const w = canvas.width, h = canvas.height;

    // Loading indicator
    ctx.fillStyle = "rgba(255,255,255,0.03)";
    ctx.fillRect(0, 0, w, h);
    ctx.fillStyle = "rgba(255,255,255,0.15)";
    ctx.font = "10px monospace";
    ctx.textAlign = "center";
    ctx.fillText("...", w/2, h/2+4);
    ctx.textAlign = "start";

    try {
      const peaksUrl = "/api/peaks?file=" + encodeURIComponent(entry.url) + "&n=" + w;
      const res = await fetch(peaksUrl);
      if (!res.ok) throw new Error("peaks fetch failed");
      const data = await res.json();
      const peaks = data.peaks || [];
      if (!peaks.length) throw new Error("no peaks");

      // Draw peaks
      ctx.clearRect(0, 0, w, h);
      ctx.fillStyle = "rgba(255,255,255,0.03)";
      ctx.fillRect(0, 0, w, h);
      const mid = h / 2;
      for (let i = 0; i < w && i < peaks.length; i++) {
        const barH = peaks[i] * mid * 0.85;
        ctx.fillStyle = color;
        ctx.globalAlpha = 0.7;
        ctx.fillRect(i, mid - barH, 1, barH * 2);
      }
      ctx.globalAlpha = 1;
      canvas.dataset.wfState = "loaded";

      // Duration from buffer (loaded via Web Audio API)
      const stem = state.stems.find(s => s.url === entry.url);
      if (stem && stem.buffer) {
        entry.duration = stem.buffer.duration;
      }
        } catch (e) {
      ctx.fillStyle = "rgba(255,255,255,0.05)";
      ctx.fillRect(0, 0, w, h);
      canvas.dataset.wfState = "error";
    }
  }

  function hideResults() {
    $("#results-panel").style.display = "none"; $("#results-empty").style.display = "";
    state.audioElements = [];
    state.stems = [];
    state.groupOffsets = {};
    stopSeekSliderLoop();
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

    if (btn.classList.contains("song-play")) {
      activateGroup(btn.dataset.song);
      preloadGroupBuffers(btn.dataset.song).then(() => playGroup(btn.dataset.song));
      return;
    }
    if (btn.classList.contains("song-pause")) { pauseGroup(btn.dataset.song); stopSeekSliderLoop(); return; }
    if (btn.classList.contains("song-stop")) { stopGroup(btn.dataset.song); stopSeekSliderLoop(); syncSeekSliderForGroup(btn.dataset.song); return; }

    const row = btn.closest(".stem-row");
    const idx = parseInt(btn.dataset.idx);
    if (isNaN(idx) || !row) return;

    if (btn.classList.contains("mute")) {
      toggleMute(idx);
      const stem = state.stems[idx] || state.audioElements[idx];
      row.querySelector(".mute").classList.toggle("active", stem?.muted);
    } else if (btn.classList.contains("solo")) {
      toggleSolo(idx);
      // Update ALL rows directly from DOM
      $$("#results-content .stem-row").forEach((r, i) => {
        const s = state.stems[i] || state.audioElements[i];
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
    state.stems = [];
    state.groupOffsets = {};
    stopSeekSliderLoop();
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

  // ── Group Activation ──
  function activateGroup(song) {
    // Already active — do nothing
    if (state.activeGroup === song) return;
    // Stop previous group (Web Audio: save offset, stop sources)
    if (state.activeGroup) {
      pauseGroup(state.activeGroup);  // saves offset via AudioContext
      stopSeekSliderLoop();
    }
    state.activeGroup = song;
    startSeekSliderLoop();

    // Preload buffers for the group
    preloadGroupBuffers(song);

    // Highlight main groups + draw waveforms on activation
    $$(".song-group").forEach(g => {
      const isActive = g.dataset.song === song;
      g.classList.toggle("active", isActive);
      // Restore audio src + draw waveforms for main group when activated
      if (isActive && !song.endsWith("_pitch")) {
        const stems = state.audioElements.filter(s => s.song === song);
        stems.forEach(s => {
          if (s.canvas && s.canvas.dataset.wfState !== "loaded" && s.canvas.dataset.wfState !== "loading") {
            const capturedSong = song;
            s.canvas.dataset.wfState = "loading";
            drawWaveform(s).then(() => {
              if (state.activeGroup === capturedSong && s.duration && s.duration > 0) {
                initSeekSliderForGroup(capturedSong);
              }
            });
          }
        });
      }
    });
    // Sync seek slider for the newly activated group
    syncSeekSliderForGroup(song);

    // Highlight pitch groups
    $$(".pitch-results").forEach(g => {
      g.classList.toggle("active", g.dataset.song === song);
    });

    // Draw waveforms for pitch group when activated
    if (song.endsWith("_pitch")) {
      const pitchDiv = document.querySelector('.pitch-results[data-song="' + CSS.escape(song) + '"]');
      if (pitchDiv) {
        pitchDiv.querySelectorAll("canvas.waveform-canvas").forEach(canvas => {
          // Try drawing waveform — drawWaveformFromAudio handles dedup via dataset.wfState
          if (canvas.dataset.wfState !== "loaded" && canvas.dataset.wfState !== "loading") {
            const row = canvas.closest(".stem-row");
            if (row) {
                    const nameEl = row.querySelector(".stem-name");
              const sName = nameEl ? nameEl.textContent : "";
              if (true) {
                // Get URL from stem entry for waveform
                const stem = state.stems.find(s => s.canvas === canvas);
                if (stem && stem.url) {
                  drawWaveformFromAudio(canvas, stem.url, stemColor(sName));
                }
              }
            }
          }
        });
      }
    }
  }

  // ── Audio: Per-Group (Web Audio API) ──
  function playGroup(song) {
    if (!song) return;
    // Ensure group is active
    if (state.activeGroup !== song) activateGroup(song);

    stopGroupSources(song);
    const offset = getGroupOffset(song);
    const { startTime, totalDuration } = createAndStartSources(song, offset);
    if (state.stems.filter(s => s.song === song && s.buffer).length === 0) {
      // Buffers not loaded yet — load then play
      preloadGroupBuffers(song).then(() => {
        createAndStartSources(song, offset);
      });
    }
  }

  function pauseGroup(song) {
    if (!song) return;
    // Save current offset
    const stems = state.stems.filter(s => s.song === song);
    if (stems.length && stems[0].sourceNode && state.audioCtx) {
      const elapsed = state.audioCtx.currentTime - (stems[0]._startTime || state.audioCtx.currentTime);
      const offset = getGroupOffset(song) + elapsed;
      setGroupOffset(song, Math.min(offset, (stems[0].duration || 0)));
    }
    stopGroupSources(song);
  }

  function stopGroup(song) {
    if (!song) return;
    stopGroupSources(song);
    setGroupOffset(song, 0);
    // Also stop pitch group
    stopGroupSources(song + "_pitch");
    setGroupOffset(song + "_pitch", 0);
  }

  function seekGroup(song, time) {
    stopGroupSources(song);
    setGroupOffset(song, time);
    playGroup(song);
  }

  function startSeekSliderLoop() {
    if (state.rafId) return;
    function loop() {
      state.rafId = requestAnimationFrame(loop);
      if (!state.activeGroup || !state.audioCtx) return;
      const song = state.activeGroup;
      const stems = state.stems.filter(s => s.song === song && s.sourceNode);
      if (!stems.length) return;

      const elapsed = state.audioCtx.currentTime - (stems[0]._startTime || state.audioCtx.currentTime);
      const offset = getGroupOffset(song);
      const currentTime = offset + Math.max(0, elapsed);
      const dur = stems[0].duration || stems[0].buffer?.duration || 0;
      if (dur <= 0) return;

      // Main group slider
      if (!song.endsWith("_pitch")) {
        const seek = document.getElementById("seek-slider");
        const timeDisplay = document.getElementById("time-display");
        if (seek && !state.seeking) {
          seek.max = Math.floor(dur * 1000);
          seek.value = Math.floor(currentTime * 1000);
        }
        if (timeDisplay) timeDisplay.textContent = fmtTimeSec(currentTime) + " / " + fmtTimeSec(dur);
      } else {
        // Pitch slider
        const pitchDiv = document.querySelector('.pitch-results[data-song="' + CSS.escape(song) + '"]');
        if (pitchDiv) {
          const pSeek = pitchDiv.querySelector(".seek-slider");
          const pTime = pitchDiv.querySelector(".seek-time");
          if (pSeek && !state.seeking) {
            pSeek.max = Math.floor(dur * 1000);
            pSeek.value = Math.floor(currentTime * 1000);
          }
          if (pTime) pTime.textContent = fmtTimeSec(currentTime) + " / " + fmtTimeSec(dur);
        }
      }
    }
    loop();
  }

  function stopSeekSliderLoop() {
    if (state.rafId) { cancelAnimationFrame(state.rafId); state.rafId = null; }
  }

  function syncSeekSliderForGroup(song) {
    // Update seek slider and time display to reflect current position
    let currentTime = 0, duration = 0;
    if (song.endsWith("_pitch")) {
      // Find pitch audio elements (stored in DOM, not state.audioElements)
      const pitchDiv = document.querySelector('.pitch-results[data-song="' + CSS.escape(song) + '"]');
      if (pitchDiv) {
        const audios = pitchDiv.querySelectorAll("audio");
        if (audios.length) {
          currentTime = audios[0].currentTime || 0;
          duration = audios[0].duration || 0;
        }
        // Navigate from pitchDiv to find seek controls
        const pSeek = pitchDiv.querySelector(".seek-slider");
        const pTime = pitchDiv.querySelector(".seek-time");
        if (pSeek && duration > 0) {
          pSeek.max = Math.floor(duration * 1000);
          pSeek.value = Math.floor(currentTime * 1000);
        }
        if (pTime) {
          pTime.textContent = fmtTimeSec(currentTime) + " / " + fmtTimeSec(duration);
        }
      }
    } else {
      const stems = state.audioElements.filter(s => s.song === song);
      if (stems.length) {
        currentTime = getGroupOffset(song) || 0;
        duration = stems.find(s => s.duration && s.duration > 0)?.duration || stems[0].buffer?.duration || 0;
      }
      const seek = document.getElementById("seek-slider");
      const timeDisplay = document.getElementById("time-display");
      if (seek && duration > 0) {
        seek.max = Math.floor(duration * 1000);
        seek.value = Math.floor(currentTime * 1000);
      }
      if (timeDisplay) {
        timeDisplay.textContent = fmtTimeSec(currentTime) + " / " + fmtTimeSec(duration);
      }
    }
  }

  function toggleMute(idx) {
    const s = state.audioElements[idx]; if (!s) return;
    s.muted = !s.muted;
    const anySolo = state.audioElements.some(x => x.solo);
    // If any stem has solo active, only the solo'd stem controls volume — mute is cosmetic
    if (!anySolo) {
      updateGainForStem(s);
    }
    updateSingleStemButtons(idx);
  }

  function toggleSolo(idx) {
    const s = state.audioElements[idx]; if (!s) return;
    const wasSolo = s.solo;
    state.audioElements.forEach((x) => (x.solo = false));
    s.solo = !wasSolo;
    state.audioElements.forEach((x, i) => {
      updateGainForStem(x);
    });
    updateAllStemButtons();
  }

  function setVolume(idx, vol) {
    const s = state.audioElements[idx]; if (!s) return;
    s.vol = vol;
    const isSolod = state.audioElements.some((x) => x.solo);
    if (!s.muted && (!isSolod || s.solo)) updateGainForStem(s);
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
      const pitchDiv = document.querySelector('.pitch-results[data-song="' + CSS.escape(song + '_pitch') + '"]');
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
          '<a class="stem-dl" href="' + f.url + '?cb=' + Date.now() + '" download>⬇</a>' +
          '<button class="stem-delete" data-file="' + escAttr(f.url) + '" title="Delete">✕</button>';

        pitchDiv.appendChild(row);

        const entry = {
          name: f.name, song: song + "_pitch",
          muted: false, solo: false, url: f.url, vol: 100,
          canvas: row.querySelector(".waveform-canvas"), duration: 0,
        };
        pitchAudioElements.push(entry);
        // Also track in Web Audio stems for AudioContext playback
        state.stems.push({
          name: entry.name, song: entry.song, url: entry.url,
          canvas: entry.canvas, muted: false, solo: false, vol: 100,
          duration: 0, buffer: null, gainNode: null, sourceNode: null,
        });

        // Track duration for seek slider
        // Duration tracked via Web Audio buffer loading

        // Placeholder waveform — no fetch to avoid competing with <audio> loading
        entry.canvas.dataset.wfState = "";
        const pctx = entry.canvas.getContext("2d");
        pctx.fillStyle = "rgba(255,255,255,0.03)";
        pctx.fillRect(0, 0, entry.canvas.width, entry.canvas.height);

        // Timeupdate on first
        if (i === 0) {
          // Timeupdate handled by Web Audio rAF loop
        }

        // Mute/Solo
        row.querySelector(".mute").addEventListener("click", function () {
          entry.muted = !entry.muted;
          updateGainForStem(entry);
          this.classList.toggle("active", entry.muted);
        });
        row.querySelector(".solo").addEventListener("click", function () {
          const wasSolo = entry.solo;
          pitchAudioElements.forEach(x => x.solo = false);
          entry.solo = !wasSolo;
          pitchAudioElements.forEach(x => updateGainForStem(x));
          $$(".pitch-stem .solo").forEach((b, j) => b.classList.toggle("active", pitchAudioElements[j]?.solo));
          $$(".pitch-stem .mute").forEach((b, j) => b.classList.toggle("active", pitchAudioElements[j]?.muted));
        });
        row.querySelector(".stem-vol-slider").addEventListener("input", function () {
          entry.vol = parseInt(this.value);
          updateGainForStem(entry);
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
        state.seeking = true;
        const t = parseInt(pSeek.value) / 1000;
        activateGroup(song + "_pitch");
        seekGroup(song + "_pitch", t);
        pTime.textContent = fmtTimeSec(t) + " / " + fmtTimeSec((parseInt(pSeek.max) || 1000) / 1000);
      });
      pSeek.addEventListener("change", () => { state.seeking = false; });

      // Play/Pause/Stop buttons
      header.querySelector(".pitch-play").addEventListener("click", () => {
        activateGroup(song + "_pitch");
        preloadGroupBuffers(song + "_pitch").then(() => playGroup(song + "_pitch"));
      });
      header.querySelector(".pitch-pause").addEventListener("click", () => {
        pauseGroup(song + "_pitch");
        stopSeekSliderLoop();
      });
      header.querySelector(".pitch-stop").addEventListener("click", () => {
        stopGroup(song + "_pitch");
        stopSeekSliderLoop();
        syncSeekSliderForGroup(song + "_pitch");
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
        // Abort all audio connections before DOM removal
        pitchDiv.querySelectorAll("audio").forEach(a => {
          a.pause();
          a.src = "";
          a.load();
        });
        for (const e of pitchAudioElements) {
          try { await fetch("/api/delete?file=" + encodeURIComponent(e.url)); } catch (_) {}
        }
        pitchDiv.innerHTML = "";
        pitchDiv.style.display = "none";
        // Reset active group if it was this pitch
        if (state.activeGroup === song + "_pitch") state.activeGroup = null;
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
    const stems = state.stems.filter(s => s.song === song && s.duration > 0);
    const wfDur = stems.reduce((max, s) => Math.max(max, s.duration || 0), 0);
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
