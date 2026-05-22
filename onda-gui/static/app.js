// Onda GUI v2 — File picker, step runners, pipeline start

let pollTimer = null;

// ── Init ──
async function init() {
  await loadInputFiles();
  setupDragDrop();
  setupDropZoneClick();
  updateStartButton();
}

// ── Load input files ──
async function loadInputFiles() {
  try {
    const res = await fetch("/cgi-bin/input");
    const data = await res.json();
    const sel = document.getElementById("select-track");
    sel.innerHTML = "<option value=\"\">— select audio file —</option>";
    data.files.forEach(f => {
      const opt = document.createElement("option");
      opt.value = f.name;
      opt.textContent = f.name;
      sel.appendChild(opt);
    });
    updateStartButton();
  } catch (e) { console.error("Error loading input:", e); }
}

// ── Drop zone click → file picker ──
function setupDropZoneClick() {
  document.getElementById("drop-zone").addEventListener("click", () => {
    document.getElementById("file-picker").click();
  });
}

// ── File picker handler ──
async function handleFilePicker(event) {
  const files = event.target.files;
  for (const file of files) {
    await uploadFile(file);
  }
  await loadInputFiles();
  event.target.value = "";
}

// ── Drag & drop ──
function setupDragDrop() {
  const zone = document.getElementById("drop-zone");
  const msg = document.getElementById("drop-msg");

  zone.addEventListener("dragover", e => {
    e.preventDefault();
    zone.classList.add("drag-over");
  });
  zone.addEventListener("dragleave", () => {
    zone.classList.remove("drag-over");
  });
  zone.addEventListener("drop", async e => {
    e.preventDefault();
    zone.classList.remove("drag-over");
    const files = e.dataTransfer.files;
    for (const file of files) {
      await uploadFile(file);
    }
    await loadInputFiles();
  });
}

// ── Upload raw POST body + X-Filename header ──
async function uploadFile(file) {
  const msg = document.getElementById("drop-msg");
  msg.textContent = "Uploading " + file.name + "...";
  try {
    const res = await fetch("/cgi-bin/upload", {
      method: "POST",
      headers: { "X-Filename": file.name },
      body: file
    });
    const data = await res.json();
    msg.textContent = data.success
      ? "Uploaded: " + file.name
      : "Upload failed: " + (data.error || "unknown");
  } catch (e) {
    msg.textContent = "Upload error";
    console.error(e);
  }
}

// ── Toggles ──
function toggleViperx() {
  document.getElementById("viperx-options").classList.toggle(
    "hidden", !document.getElementById("enable-viperx").checked
  );
}
function toggleDemucs() {
  document.getElementById("demucs-options").classList.toggle(
    "hidden", !document.getElementById("enable-demucs").checked
  );
}
function toggleRubberband() {
  document.getElementById("rubberband-options").classList.toggle(
    "hidden", !document.getElementById("enable-rubberband").checked
  );
}

// ── Pitch ──
function updatePitch() {
  const v = document.getElementById("pitch").value;
  const sign = v >= 0 ? "+" : "";
  document.getElementById("pitch-val").textContent = sign + v + " semitones";
}

// ── Start button state ──
function updateStartButton() {
  const sel = document.getElementById("select-track");
  const btn = document.getElementById("btn-start");
  btn.disabled = !sel.value;
}

// ── Keep values ──
function getViperxKeep() {
  const inst = document.getElementById("keep-instrumental").checked;
  const voc = document.getElementById("keep-vocals").checked;
  if (inst && voc) return "both";
  if (inst) return "instrumental";
  if (voc) return "vocals";
  return "";
}

function getDemucsKeep() {
  const stems = [];
  if (document.getElementById("keep-drums").checked) stems.push("drums");
  if (document.getElementById("keep-bass").checked) stems.push("bass");
  if (document.getElementById("keep-other").checked) stems.push("other");
  if (document.getElementById("keep-demucs-vocals").checked) stems.push("vocals");
  return stems.join(",");
}

// ── Build form for a given set of steps ──
function buildForm(input, viperx, demucs, rubberband) {
  const form = new URLSearchParams();
  form.append("input_file", input);
  if (viperx) {
    form.append("viperx", "on");
    form.append("viperx_keep", getViperxKeep());
  }
  if (demucs) {
    form.append("demucs", "on");
    form.append("demucs_keep", getDemucsKeep());
  }
  if (rubberband) {
    form.append("rubberband", "on");
    form.append("pitch", document.getElementById("pitch").value);
  }
  return form;
}

// ── Run a single step ──
async function runStep(step) {
  const input = document.getElementById("select-track").value;
  if (!input) { alert("Select a track first"); return; }

  const prog = document.getElementById("progress-fill");
  const area = document.getElementById("waveform-area");

  let label = "";
  const form = buildForm(
    input,
    step === "viperx",
    step === "demucs",
    step === "rubberband"
  );
  if (step === "viperx") label = "Viperx → " + getViperxKeep();
  if (step === "demucs") label = "HTDemucs → " + getDemucsKeep();
  if (step === "rubberband") label = "Rubberband ±" + document.getElementById("pitch").value;

  prog.style.width = "8%";
  area.innerHTML = "<div class=\"status-msg running\"><span class=\"spinner\"></span> " + label + "</div>";

  try {
    const res = await fetch("/cgi-bin/separate", {
      method: "POST",
      headers: { "Content-Type": "application/x-www-form-urlencoded" },
      body: form.toString()
    });
    const data = await res.json();
    if (data.success) {
      prog.style.width = "20%";
      area.innerHTML += "<div class=\"status-msg running\" style=\"margin-top:8px\"><span class=\"spinner\"></span> Processing...</div>";
      startPolling();
    } else {
      area.innerHTML = "<div class=\"status-msg error\">Failed: " + (data.error || "Unknown") + "</div>";
      prog.style.width = "0%";
    }
  } catch (e) {
    area.innerHTML = "<div class=\"status-msg error\">Connection error</div>";
    prog.style.width = "0%";
    console.error(e);
  }
}

// ── Start pipeline (all selected steps) ──
async function startAll() {
  const input = document.getElementById("select-track").value;
  if (!input) { alert("Select a track first"); return; }

  const prog = document.getElementById("progress-fill");
  const area = document.getElementById("waveform-area");

  // Disable start button
  const btn = document.querySelector(".btn-pipeline-start");
  btn.disabled = true;
  btn.innerHTML = "<span class=\"spinner\"></span> Starting...";

  const steps = [];
  if (document.getElementById("enable-viperx").checked)
    steps.push("Viperx → " + getViperxKeep());
  if (document.getElementById("enable-demucs").checked)
    steps.push("HTDemucs → " + getDemucsKeep());
  if (document.getElementById("enable-rubberband").checked)
    steps.push("Rubberband ±" + document.getElementById("pitch").value);

  if (steps.length === 0) {
    alert("Enable at least one step");
    btn.disabled = false;
    btn.innerHTML = "▶ Start Pipeline";
    return;
  }

  prog.style.width = "8%";
  area.innerHTML = "<div class=\"status-msg running\"><span class=\"spinner\"></span> " +
    steps.join(" → ") + "</div>";

  const form = buildForm(
    input,
    document.getElementById("enable-viperx").checked,
    document.getElementById("enable-demucs").checked,
    document.getElementById("enable-rubberband").checked
  );

  try {
    const res = await fetch("/cgi-bin/separate", {
      method: "POST",
      headers: { "Content-Type": "application/x-www-form-urlencoded" },
      body: form.toString()
    });
    const data = await res.json();
    if (data.success) {
      prog.style.width = "20%";
      area.innerHTML += "<div class=\"status-msg running\" style=\"margin-top:8px\"><span class=\"spinner\"></span> Processing...</div>";
      startPolling();
    } else {
      area.innerHTML = "<div class=\"status-msg error\">Failed: " + (data.error || "Unknown") + "</div>";
      prog.style.width = "0%";
      resetPipelineButton();
    }
  } catch (e) {
    area.innerHTML = "<div class=\"status-msg error\">Connection error</div>";
    prog.style.width = "0%";
    resetPipelineButton();
    console.error(e);
  }
}

function resetPipelineButton() {
  const btn = document.querySelector(".btn-pipeline-start");
  btn.disabled = false;
  btn.innerHTML = "▶ Start Pipeline";
}

// ── Poll for results ──
function startPolling() {
  const prog = document.getElementById("progress-fill");
  let count = 0;

  if (pollTimer) clearInterval(pollTimer);

  pollTimer = setInterval(async () => {
    try {
      const res = await fetch("/cgi-bin/output");
      const data = await res.json();
      count++;
      prog.style.width = Math.min(20 + count * 4, 92) + "%";

      if (data.files && data.files.length > 0) {
        clearInterval(pollTimer);
        pollTimer = null;
        prog.style.width = "100%";
        showResults(data);
        resetPipelineButton();
      }
    } catch (e) { console.error("Poll error:", e); }
  }, 3000);

  setTimeout(() => {
    if (pollTimer) {
      clearInterval(pollTimer);
      pollTimer = null;
      document.getElementById("waveform-area").innerHTML +=
        "<div class=\"status-msg error\">Timed out</div>";
      prog.style.width = "0%";
      resetPipelineButton();
    }
  }, 900000);
}

// ── Show results ──
function showResults(data) {
  const area = document.getElementById("waveform-area");

  function stemType(name) {
    const n = name.toLowerCase();
    if (n.includes("drum")) return "drums";
    if (n.includes("bass")) return "bass";
    if (n.includes("vocal")) return n.includes("viperx") ? "vocals_viperx" : "vocals";
    if (n.includes("instrumental")) return "instrumental_viperx";
    if (n.includes("other")) return "other";
    return "other";
  }

  const emojis = { drums: "🥁", bass: "🎸", other: "🎹", vocals: "🎤", instrumental_viperx: "🎵", vocals_viperx: "🎤" };

  let html = "<div class=\"status-msg done\">✓ Pipeline complete — " +
    data.files.length + " stems</div>";

  const recent = data.files.slice(0, 10);
  recent.forEach(f => {
    const type = stemType(f.name);
    const mb = (f.size / (1024 * 1024)).toFixed(1);
    html += "<div class=\"result-item\">" +
      "<div class=\"result-icon " + type + "\">" + (emojis[type] || "🎵") + "</div>" +
      "<div class=\"result-info\">" +
        "<div class=\"result-name\">" + f.name + "</div>" +
        "<div class=\"result-meta\">" + mb + " MB · " + type.replace("_", " ") + "</div>" +
      "</div>" +
      "<a href=\"" + f.url + "\" download class=\"result-download\">Download</a>" +
    "</div>";
  });
  area.innerHTML = html;
}

// ── Export stems ──
async function exportStems() {
  try {
    const res = await fetch("/cgi-bin/output");
    const data = await res.json();

    if (!data.files || data.files.length === 0) {
      alert("No stems to export. Process a track first.");
      return;
    }

    // Download each stem file
    const recent = data.files.slice(0, 10);
    for (const f of recent) {
      const a = document.createElement("a");
      a.href = f.url;
      a.download = f.name;
      document.body.appendChild(a);
      a.click();
      document.body.removeChild(a);
      // Small delay between downloads
      await new Promise(r => setTimeout(r, 300));
    }
  } catch (e) {
    alert("Export failed: " + e.message);
    console.error(e);
  }
}

document.addEventListener("DOMContentLoaded", init);
