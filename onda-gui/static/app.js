// Onda GUI — Frontend JS (nginx + CGI)

let pollTimer = null;

// ── Init ──
async function init() {
  await loadInputFiles();
  setupDragDrop();
  updateStartButton();
}

// ── Load input files into track selector ──
async function loadInputFiles() {
  try {
    const res = await fetch("/cgi-bin/input");
    const data = await res.json();
    const sel = document.getElementById("select-track");
    sel.innerHTML = "<option value=\"\">— choose a file —</option>";
    data.files.forEach(f => {
      const opt = document.createElement("option");
      opt.value = f.name;
      opt.textContent = "🎵 " + f.name;
      sel.appendChild(opt);
    });
    updateStartButton();
  } catch (e) {
    console.error("Error loading input:", e);
  }
}

// ── Drag & drop upload ──
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

// ── Upload single file ──
async function uploadFile(file) {
  const msg = document.getElementById("drop-msg");
  msg.textContent = "Uploading " + file.name + "...";
  try {
    const form = new FormData();
    form.append("file", file);

    const res = await fetch("/cgi-bin/upload", {
      method: "POST",
      body: form
    });
    const data = await res.json();
    if (data.success) {
      msg.textContent = "✅ " + data.message;
    } else {
      msg.textContent = "❌ " + (data.error || "Upload failed");
    }
  } catch (e) {
    msg.textContent = "❌ Upload error";
    console.error(e);
  }
}

// ── Toggle step options ──
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

// ── Pitch display ──
function updatePitch() {
  const v = document.getElementById("pitch").value;
  document.getElementById("pitch-val").textContent =
    (v >= 0 ? "+" : "") + v + " semitones";
}

// ── Enable START ──
function updateStartButton() {
  const sel = document.getElementById("select-track");
  const btn = document.getElementById("btn-start");
  btn.disabled = !sel.value;
}

// ── Get viperx-keep value ──
function getViperxKeep() {
  const inst = document.getElementById("keep-instrumental").checked;
  const voc = document.getElementById("keep-vocals").checked;
  if (inst && voc) return "both";
  if (inst) return "instrumental";
  if (voc) return "vocals";
  return "";
}

// ── Get demucs-keep value ──
function getDemucsKeep() {
  const stems = [];
  if (document.getElementById("keep-drums").checked) stems.push("drums");
  if (document.getElementById("keep-bass").checked) stems.push("bass");
  if (document.getElementById("keep-other").checked) stems.push("other");
  if (document.getElementById("keep-demucs-vocals").checked) stems.push("vocals");
  return stems.join(",");
}

// ── Start pipeline ──
async function startAll() {
  const input = document.getElementById("select-track").value;
  if (!input) return;

  const btn = document.getElementById("btn-start");
  btn.disabled = true;
  btn.textContent = "⏳";

  const prog = document.getElementById("progress-fill");
  const area = document.getElementById("waveform-area");

  // Show what we"re running
  const steps = [];
  if (document.getElementById("enable-viperx").checked)
    steps.push("Viperx → " + getViperxKeep());
  if (document.getElementById("enable-demucs").checked)
    steps.push("HTDemucs → " + getDemucsKeep());
  if (document.getElementById("enable-rubberband").checked)
    steps.push("Rubberband ±" + document.getElementById("pitch").value);

  prog.style.width = "10%";
  area.innerHTML = "<p class=\"status-msg running\">⏳ " + steps.join(" → ") + "</p>";

  // Build form
  const form = new URLSearchParams();
  form.append("input_file", input);

  if (document.getElementById("enable-viperx").checked) {
    form.append("viperx", "on");
    form.append("viperx_keep", getViperxKeep());
  }
  if (document.getElementById("enable-demucs").checked) {
    form.append("demucs", "on");
    form.append("demucs_keep", getDemucsKeep());
  }
  if (document.getElementById("enable-rubberband").checked) {
    form.append("rubberband", "on");
    form.append("pitch", document.getElementById("pitch").value);
  }

  try {
    const res = await fetch("/cgi-bin/separate", {
      method: "POST",
      headers: { "Content-Type": "application/x-www-form-urlencoded" },
      body: form.toString()
    });
    const data = await res.json();

    if (data.success) {
      area.innerHTML += "<p class=\"status-msg running\" style=\"margin-top:8px\">🔄 Processing... (polling for results)</p>";
      prog.style.width = "30%";
      startPolling();
    } else {
      area.innerHTML = "<p class=\"status-msg error\">❌ " +
        (data.error || "Failed to start") + "</p>";
      prog.style.width = "0%";
      resetStartButton();
    }
  } catch (e) {
    area.innerHTML = "<p class=\"status-msg error\">❌ Connection error</p>";
    prog.style.width = "0%";
    resetStartButton();
    console.error(e);
  }
}

// ── Poll for results ──
function startPolling() {
  const prog = document.getElementById("progress-fill");
  let count = 0;

  pollTimer = setInterval(async () => {
    try {
      const res = await fetch("/cgi-bin/output");
      const data = await res.json();

      count++;
      // Animate progress from 30% to 90% while waiting
      prog.style.width = Math.min(30 + count * 3, 90) + "%";

      if (data.files && data.files.length > 0) {
        clearInterval(pollTimer);
        pollTimer = null;
        prog.style.width = "100%";
        showResults(data);
        resetStartButton();
      }
    } catch (e) {
      console.error("Poll error:", e);
    }
  }, 3000); // poll every 3 seconds

  // Stop after 15 minutes
  setTimeout(() => {
    if (pollTimer) {
      clearInterval(pollTimer);
      pollTimer = null;
      document.getElementById("waveform-area").innerHTML +=
        "<p class=\"status-msg error\">⏰ Timed out waiting for results</p>";
      prog.style.width = "0%";
      resetStartButton();
    }
  }, 900000);
}

// ── Show results ──
function showResults(data) {
  const area = document.getElementById("waveform-area");

  let html = "<div class=\"status-msg done\">✅ Pipeline complete — " +
    data.files.length + " stem(s) generated:</div>";

  data.files.forEach(f => {
    const mb = (f.size / (1024 * 1024)).toFixed(1);
    html += `<div class="queue-item">
      <span>🎵</span>
      <span class="name">${f.name}</span>
      <span style="color:var(--text-dim);font-size:12px">${mb} MB</span>
      <a href="${f.url}" download style="color:var(--accent-glow);text-decoration:none;font-size:13px">⬇</a>
    </div>`;
  });
  area.innerHTML = html;
}

// ── Reset start button ──
function resetStartButton() {
  const btn = document.getElementById("btn-start");
  btn.disabled = false;
  btn.innerHTML = "▶<br><small>START</small>";
}

// ── DOM ready ──
document.addEventListener("DOMContentLoaded", init);
