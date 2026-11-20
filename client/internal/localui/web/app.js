// vmsagent local console — vanilla JS (no framework, no build step).
// Screens: cameras · discovery · status & self-test · settings · event log
// (docs/protocol.md §17).
"use strict";

const SCREENS = ["cams", "discovery", "status", "settings", "log"];
const I18N = {
  loading: "loading…",
  saved: "Saved",
  saveFailed: "Could not save",
  scanDone: (n) => `${n} camera(s) found`,
  serverOnline: "server: connected",
  serverOffline: "server: disconnected",
  savedReconnect: "Saved — reconnecting…",
  joinSent: "Request sent — waiting for the server admin to approve",
  joinFailed: "Could not send the request",
  joinAlready: "Already connected to the server",
  resetDone: "Saved connection cleared — press Request access to ask again",
  resetFailed: "Could not clear the connection",
  resetConfirm: "Clear this computer's saved connection? Cameras and settings stay. You will have to request access again.",
  authRejected: "server rejected our saved credentials — the server was reset or this client was deleted. Press Clear connection, then Request access",
  err_already_enrolled: "Already connected to the server",
  err_rejected: "The server rejected this request — contact the administrator",
  err_bad_key: "Local key problem — check the agent's state directory",
  err_join_failed: "Server not reachable — check the server URL",
  err_rate_limited: "Too many attempts — wait a few minutes",
};

const $ = (sel) => document.querySelector(sel);
const $$ = (sel) => Array.from(document.querySelectorAll(sel));

function toast(msg, kind) {
  const box = document.createElement("div");
  box.className = "toast" + (kind ? " " + kind : "");
  box.textContent = msg;
  $("#toasts").appendChild(box);
  setTimeout(() => box.remove(), 4000);
}

function showScreen(name) {
  if (!SCREENS.includes(name)) name = "cams";
  $$(".screen").forEach((s) => { s.hidden = s.dataset.screen !== name; });
  $$(".nav-item").forEach((a) => a.classList.toggle("active", a.dataset.nav === name));
  if (location.hash !== "#/" + name) location.hash = "#/" + name;
  load(name);
}

let agentId = "";    // bo'sh → ulanish yo'q ("Request access" faol)
let joinStatus = ""; // §3.2: pending | approved | rejected
let authRejected = false; // §3.6: server secret'ni rad etdi (4003/4004)

async function api(path, opts) {
  const o = Object.assign({ credentials: "same-origin" }, opts || {});
  o.headers = Object.assign({}, o.headers || {});
  const res = await fetch(path, o);
  if (!res.ok) {
    const e = new Error(path + ": HTTP " + res.status);
    try { e.body = await res.json(); } catch (_) { /* JSON bo'lmasa bo'sh */ }
    throw e;
  }
  return res.status === 204 ? null : res.json();
}

async function loadStatus() {
  try {
    const st = await api("/api/ui/status");
    if (!st) return;
    const ver = st.version || "?";
    $("#txt-version").textContent = /^[0-9]/.test(ver) ? "v" + ver : ver;
    $("#txt-cams").textContent = `${st.cams_online ?? 0}/${st.cams_total ?? 0}`;
    const sa = $("#side-agent");
    if (sa) sa.textContent = st.agent_id ? st.agent_id.slice(0, 11) : "—";
    agentId = st.agent_id || "";
    joinStatus = st.join_status || "";
    authRejected = !!st.auth_rejected;
    $("#txt-publishes").textContent = String(st.publishes ?? 0);
    const dot = $("#dot-server");
    dot.className = "dot " + (st.server_online ? "dot-ok" : "dot-off");
    $("#txt-server").textContent = st.server_online ? I18N.serverOnline : I18N.serverOffline;
    renderStatusCards(st);
  } catch (e) {
    $("#dot-server").className = "dot dot-err";
    $("#txt-server").textContent = I18N.serverOffline;
  }
}

// §17: status screen — server/heartbeat/camera cards + last-error banner.
function renderStatusCards(st) {
  const cards = $("#status-cards");
  if (!cards) return;
  const hb = st.last_heartbeat ? st.last_heartbeat : "—";
  const items = [
    ["Server", st.server_online ? "live" : "down", st.server_online ? "ok" : "err"],
    ["Last heartbeat", hb, ""],
    ["Cameras online", `${st.cams_online ?? 0}/${st.cams_total ?? 0}`, ""],
    ["Active streams", String(st.publishes ?? 0), ""],
    ["cams epoch", String(st.cams_epoch ?? 0), ""],
    ["FFmpeg", st.ffmpeg_ok ? "found" : "MISSING", st.ffmpeg_ok ? "ok" : "warn"],
  ];
  cards.innerHTML = items.map((it) =>
    `<div class="card ${it[2]}"><b>${esc(it[1])}</b><span>${esc(it[0])}</span></div>`).join("");
  const vb = $("#ver-banner");
  if (vb) {
    if (st.version_mismatch) {
      vb.hidden = false;
      vb.textContent = "Protocol version mismatch: server " + (st.server_version || "?") +
        " (supports " + st.proto_min + ".." + st.proto_max + ") — update the agent";
    } else {
      vb.hidden = true;
    }
  }
  const banner = $("#last-error");
  if (banner) {
    if (st.last_error) {
      banner.hidden = false;
      banner.innerHTML = "Last error: " + esc(st.last_error) +
        (st.last_error_at ? `<span class="when">${esc(st.last_error_at)}</span>` : "");
    } else {
      banner.hidden = true;
    }
  }
  updateJoinState(); // §3: Sozlamalar matni ham polling bilan yangilanadi
}

async function load(screen) {
  await loadStatus();
  if (screen === "cams") await loadCams();
  if (screen === "discovery") await loadDiscovery();
  if (screen === "settings") await loadSettings();
  if (screen === "log") {
    try {
      const r = await api("/api/ui/log?limit=200");
      $("#log-out").textContent = (r && r.lines && r.lines.length) ? r.lines.join("\n") : "log is empty";
    } catch (e) { /* older versions have no log endpoint */ }
  }
}

// --- settings & join (§3.1, §17) ---
async function loadSettings() {
  try {
    const st = await api("/api/ui/settings");
    if (st) {
      if (!$("#set-server").value) $("#set-server").value = st.server_url || "";
      if ($("#set-name") && !$("#set-name").value) $("#set-name").value = st.name || "";
      if (st.log_level) $("#set-loglevel").value = st.log_level;
    }
  } catch (e) { /* eski versiyalarda bo'lmasligi mumkin */ }
  // §3 holati: operator nima kutishini shu yerda ko'radi.
  updateJoinState();
}

// updateJoinState — §3 holat matni va tugmalar holati. Alohida funksiya:
// 5 s davrdagi status polling'i ham buni yangilaydi (avval faqat ekran
// ochilganda yangilanardi — tasdiqdan keyin matn eskirib qolardi,
// 2026-11-20 kuzatildi).
function updateJoinState() {
  const hint = $("#set-hint");
  const btn = document.querySelector('[data-act="join"]');
  const resetBtn = document.querySelector('[data-act="reset"]');
  if (!hint && !btn) return;
  // "Clear connection" identity yoki so'rov holati bo'lsa kerak (rejected
  // holda ham: eski join_key bilan qayta urinish rad etiladi).
  if (resetBtn) resetBtn.disabled = !agentId && !joinStatus;
  if (agentId && authRejected) {
    if (hint) hint.textContent = I18N.authRejected;
    if (btn) btn.disabled = true;
  } else if (agentId) {
    if (hint) hint.textContent = "connected as " + agentId;
    if (btn) btn.disabled = true;
  } else if (joinStatus === "pending") {
    if (hint) hint.textContent = "request sent — waiting for approval on the server (retried automatically)";
    if (btn) btn.disabled = false;
  } else if (joinStatus === "rejected") {
    // Eski kalit bilan qayta urinish foydasiz: avval identity tozalanadi.
    if (hint) hint.textContent = "request rejected by the server — press Clear connection, then Request access";
    if (btn) btn.disabled = true;
  } else {
    if (hint) hint.textContent = "not connected — press Request access, then approve in the server UI";
    if (btn) btn.disabled = false;
  }
}

// resetNow — eski ulanish identity'sini tozalash (§3.4, 1.0-draft.11).
async function resetNow(btn) {
  if (!window.confirm(I18N.resetConfirm)) return;
  const old = btn.textContent;
  btn.disabled = true;
  btn.textContent = "Clearing…";
  try {
    await api("/api/ui/reset", { method: "POST", headers: { "Content-Type": "application/json" }, body: "{}" });
    toast(I18N.resetDone, "ok");
    agentId = ""; joinStatus = ""; authRejected = false;
    setTimeout(() => { loadStatus(); loadSettings(); }, 800);
  } catch (e) {
    toast(I18N.resetFailed, "err");
  } finally {
    btn.disabled = false;
    btn.textContent = old;
  }
}

async function joinNow(btn) {
  const old = btn.textContent;
  btn.disabled = true;
  btn.textContent = "Requesting…";
  try {
    await api("/api/ui/join", {
      method: "POST", headers: { "Content-Type": "application/json" },
      body: "{}",
    });
    toast(I18N.joinSent, "ok");
    setTimeout(() => { loadStatus(); loadSettings(); }, 1500);
  } catch (e) {
    const key = e && e.body && e.body.error ? "err_" + e.body.error : null;
    const detail = e && e.body && e.body.detail ? " — " + e.body.detail : "";
    toast((key && I18N[key] ? I18N[key] : I18N.joinFailed) + detail, "err");
  } finally {
    btn.disabled = false;
    btn.textContent = old;
  }
}

// --- keyboard shortcuts (§17) ---
document.addEventListener("keydown", (ev) => {
  const typing = /^(INPUT|SELECT|TEXTAREA)$/.test(document.activeElement.tagName);
  if (ev.key === "Escape") { $("#modal").hidden = true; return; }
  if (typing) return;
  const map = { "/": () => $("#filter").focus(), "n": () => showScreen("cams"),
    "d": () => showScreen("discovery"), "s": () => showScreen("status"),
    "t": () => showScreen("settings"), "l": () => showScreen("log"),
    "?": () => toast("Shortcuts: / filter · n cameras · d discovery · s status · t settings · l log · Esc close") };
  const fn = map[ev.key];
  if (fn) { ev.preventDefault(); fn(); }
});

window.addEventListener("hashchange", () => showScreen((location.hash || "#/cams").slice(2)));

document.addEventListener("click", (ev) => {
  const btn = ev.target.closest("[data-act]");
  if (btn && btn.dataset.act === "new") { openNewCamModal(); return; }
  if (!btn) return;
  const act = btn.dataset.act;
  if (act === "save") {
    api("/api/ui/settings", { method: "PUT", headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        server_url: $("#set-server").value,
        log_level: $("#set-loglevel").value,
        name: $("#set-name") ? $("#set-name").value : "",
      }) })
      .then(() => { toast(I18N.savedReconnect, "ok"); setTimeout(loadStatus, 1200); })
      .catch(() => toast(I18N.saveFailed, "err"));
  } else if (act === "join") {
    joinNow(btn);
  } else if (act === "reset") {
    resetNow(btn);
  } else if (act === "selftest") {
    runSelftest(btn);
  } else if (act === "scan-start") {
    startScan();
  } else if (act === "scan-add") {
    addSelected();
  } else if (act === "delete-sel") {
    confirmDeleteSelected();
  }
});

// C4.6: self-test panel — POST /api/ui/selftest, results table.
async function runSelftest(btn) {
  btn.disabled = true;
  const old = btn.textContent;
  btn.textContent = "Running…";
  try {
    const res = await api("/api/ui/selftest", { method: "POST" });
    const body = $("#selftest-body");
    if (body) {
      body.innerHTML = (res.results || []).map((r) =>
        `<tr><td>${esc(r.name)}</td><td>${r.ok ? '<span class="badge badge-ok">OK</span>' :
          '<span class="badge badge-err">FAIL</span>'}</td><td class="mono">${r.ms}</td></tr>`).join("") ||
        '<tr><td colspan="3" class="empty">No checks configured.</td></tr>';
    }
    toast("Self-test finished", "ok");
  } catch (e) {
    toast("Self-test failed", "err");
  } finally {
    btn.disabled = false;
    btn.textContent = old;
  }
}

// Show previously stored results on page open (GET).
api("/api/ui/selftest").then((res) => {
  const body = $("#selftest-body");
  if (body && res && res.results && res.results.length) {
    body.innerHTML = res.results.map((r) =>
      `<tr><td>${esc(r.name)}</td><td>${r.ok ? '<span class="badge badge-ok">OK</span>' :
        '<span class="badge badge-err">FAIL</span>'}</td><td class="mono">${r.ms}</td></tr>`).join("");
  }
}).catch(() => {});

showScreen((location.hash || "#/cams").slice(2));
setInterval(loadStatus, 5000);

// --- Cameras (C2.3): list, add, delete ---
document.addEventListener("change", (ev) => {
  if (ev.target.id === "sel-all") {
    $$("#cams-body input[data-cam]").forEach((c) => { c.checked = ev.target.checked; });
  }
});

function confirmDeleteSelected() {
  const ids = $$("#cams-body input[data-cam]:checked").map((c) => c.dataset.cam);
  if (!ids.length) { toast("Select cameras first", "warn"); return; }
  const box = $("#modal-box");
  box.innerHTML = `<h2>Delete ${ids.length} camera(s)?</h2>
    <p class="dim">Stored passwords are also removed from the keystore. This cannot be undone.</p>
    <div class="row" style="margin-top:10px">
      <button class="btn btn-danger" id="d-ok">Delete</button>
      <button class="btn" id="d-cancel">Cancel</button>
    </div>`;
  $("#modal").hidden = false;
  $("#d-ok").onclick = async () => {
    try {
      const r = await api("/api/ui/cams", { method: "DELETE", headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ ids }) });
      toast(`Deleted: ${r ? r.deleted : 0}`, "ok");
      $("#modal").hidden = true;
      await loadCams();
    } catch (e) { toast("Delete failed: " + e.message, "err"); }
  };
  $("#d-cancel").onclick = () => { $("#modal").hidden = true; };
}

function esc(v) {
  // No regex literal: scripts/jscheck.py (no node in this env) confuses quotes
  // inside regexes. split/join gives identical results.
  let s = String(v == null ? "" : v);
  s = s.split("&").join("&amp;");
  s = s.split("<").join("&lt;");
  s = s.split(">").join("&gt;");
  s = s.split(String.fromCharCode(34)).join("&quot;");
  s = s.split(String.fromCharCode(39)).join("&#39;");
  return s;
}

function badgeClass(st) {
  if (st === "online") return "badge badge-ok";
  if (st === "error") return "badge badge-err";
  if (st === "offline" || st === "muted") return "badge badge-warn";
  return "badge";
}

async function loadCams() {
  const body = $("#cams-body");
  if (!body) return;
  let r = null;
  try { r = await api("/api/ui/cams"); } catch (e) { r = null; }
  const list = (r && r.cams) || [];
  if (!list.length) {
    body.innerHTML = '<tr><td colspan="7" class="empty">No cameras yet — press <kbd>n</kbd> to add one or <kbd>d</kbd> to discover.</td></tr>';
    return;
  }
  body.innerHTML = list.map((c) => {
    const profiles = (c.main_path ? 1 : 0) + (c.sub_path ? 1 : 0);
    const codec = [c.codec_main, c.codec_sub].filter(Boolean).join(" / ") || "—";
    return `<tr>
      <td><input type="checkbox" data-cam="${esc(c.id)}" aria-label="select ${esc(c.name)}"></td>
      <td>${esc(c.name || "(unnamed)")}${c.secret_ref ? '<span class="lock" title="password stored in keystore">pwd</span>' : ""}</td>
      <td class="mono">${esc(c.host)}${c.port ? ":" + esc(c.port) : ""}</td>
      <td>${profiles}</td>
      <td class="mono">${esc(codec)}</td>
      <td><span class="${badgeClass(c.status)}">${esc(c.status || "unknown")}</span></td>
      <td>${previewCell(c)}</td>
    </tr>`;
  }).join("");
}

// Preview: snapshot bir marta olinadi (qo'shish/holat o'zgarishi);
// rasmga bosish darhol yangilaydi.
function previewCell(c) {
  if (c.snapshot_at) {
    const v = encodeURIComponent(c.snapshot_at);
    return `<img class="thumb" data-snap="${esc(c.id)}" title="Click to refresh"
      src="/api/ui/cams/${encodeURIComponent(c.id)}/snapshot?v=${v}" alt="preview of ${esc(c.name || c.id)}">`;
  }
  return `<span class="noimg" data-snap="${esc(c.id)}">no image — click</span>`;
}

document.addEventListener("click", async (ev) => {
  const el = ev.target.closest("[data-snap]");
  if (!el) return;
  const id = el.dataset.snap;
  el.classList.add("busy");
  try {
    await api(`/api/ui/cams/${encodeURIComponent(id)}/snapshot`, { method: "POST" });
    toast("Snapshot updated", "ok");
  } catch (e) {
    toast("Camera unreachable — no new frame", "warn");
  }
  await loadCams();
});

// --- Discovery (C2.18): scan, results table, checkbox, bulk add ---
let scanDevices = [];      // oxirgi natijalar (jonli o'sib boradi)
let scanSel = new Set();   // tanlangan kalitlar (qayta render'da saqlanadi)
let scanPoll = null;

function discoveryVisible() {
  const el = document.querySelector('[data-screen="discovery"]');
  return el && !el.hidden;
}

function renderScanTable() {
  const body = $("#scan-body");
  if (!body) return;
  if (!scanDevices.length) {
    body.innerHTML = '<tr><td colspan="8" class="empty">Nothing found yet — press "Start scan".</td></tr>';
    return;
  }
  body.innerHTML = scanDevices.map((d) => {
    const known = d.already_known
      ? '<span class="badge badge-warn">added</span>'
      : '<span class="badge badge-ok">new</span>';
    const check = d.already_known ? "" :
      `<input type="checkbox" class="scan-check" value="${esc(d.key)}"${scanSel.has(d.key) ? " checked" : ""}>`;
    return `<tr>
      <td class="col-check">${check}</td>
      <td class="mono">${esc(d.ip)}:${esc(d.port)}</td>
      <td>${esc(d.model || "—")}</td>
      <td>${esc(d.name || "—")}</td>
      <td>${d.has_onvif ? "yes" : "no"}</td>
      <td class="mono">${d.mac ? esc(d.mac) : "—"}</td>
      <td class="dim">—</td>
      <td>${known}</td>
    </tr>`;
  }).join("");
}

function setScanProgress(prog) {
  const el = $("#scan-progress");
  if (!el) return;
  const btn = document.querySelector('[data-act="scan-start"]');
  if (prog.running) {
    el.className = "scan-progress run";
    el.textContent = `scanning… ${prog.scanned}/${prog.total} · ${prog.found} found`;
    if (btn) btn.disabled = true;
  } else if (prog.total) {
    el.className = "scan-progress done";
    el.textContent = `done: ${prog.scanned}/${prog.total} · ${prog.found} found`;
    if (btn) btn.disabled = false;
  } else {
    el.className = "scan-progress";
    el.textContent = "";
    if (btn) btn.disabled = false;
  }
}

async function loadDiscovery() {
  const body = $("#scan-body");
  if (!body) return;
  let r = null;
  try { r = await api("/api/ui/discovery/results"); } catch (e) { r = null; }
  scanDevices = (r && r.devices) || [];
  renderScanTable();
  const prog = (r && r.progress) || {};
  setScanProgress(prog);
  clearTimeout(scanPoll);
  if (prog.running) {
    // skan tugaguncha ~1.2 s interval bilan yangilab boramiz
    scanPoll = setTimeout(() => { if (discoveryVisible()) loadDiscovery(); }, 1200);
  }
}

document.addEventListener("change", (ev) => {
  if (ev.target.classList && ev.target.classList.contains("scan-check")) {
    if (ev.target.checked) scanSel.add(ev.target.value); else scanSel.delete(ev.target.value);
  }
});

async function startScan() {
  scanSel.clear();
  try {
    await api("/api/ui/discovery/scan", { method: "POST" });
    toast("Scanning started — results appear as they are found", "ok");
  } catch (e) {
    toast("Scan could not start", "err");
  }
  await loadDiscovery();
}

async function addSelected() {
  const keys = Array.from(document.querySelectorAll(".scan-check:checked")).map((c) => c.value);
  if (!keys.length) { toast("Nothing selected", "err"); return; }
  const in_ = {
    keys: keys,
    name_pattern: ($("#name-pattern") || {}).value || "",
    password: ($("#scan-pass") || {}).value || "",
    enabled: true,
  };
  try {
    const r = await api("/api/ui/discovery/add", {
      method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(in_) });
    if (r) toast(`Added: ${r.added}, failed: ${r.failed}`, r.failed ? "warn" : "ok");
    await loadDiscovery();
  } catch (e) {
    toast("Add failed", "err");
  }
}

function openNewCamModal() {
  const box = $("#modal-box");
  if (!box) return;
  box.innerHTML = `<h2>Add camera</h2>
    <div class="row"><label class="field"><span>Name</span><input id="c-name" placeholder="e.g. Front door"></label>
    <label class="field"><span>Host / IP</span><input id="c-host" placeholder="192.168.10.20"></label></div>
    <div class="row"><label class="field"><span>RTSP port</span><input id="c-port" value="554"></label>
    <label class="field"><span>RTSP user</span><input id="c-user" value="admin"></label>
    <label class="field"><span>Password</span><input id="c-pass" type="password"></label></div>
    <div class="row" style="margin-top:10px">
      <button class="btn btn-primary" id="c-save">Save</button>
      <button class="btn" id="c-cancel">Cancel</button>
    </div>`;
  $("#modal").hidden = false;
  $("#c-save").onclick = async () => {
    const host = $("#c-host").value.trim();
    if (!host) { toast("Host is required", "err"); return; }
    const pass = $("#c-pass").value;
    // The camera row keeps no plaintext password — it goes to the keystore (§10.1).
    const secret = pass;
    try {
      await api("/api/ui/cams", { method: "POST", headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ name: $("#c-name").value.trim(), host, port: parseInt($("#c-port").value || "554", 10), user: $("#c-user").value.trim() || "admin", secret }) });
      toast(I18N.saved, "ok");
      $("#modal").hidden = true;
      await loadCams();
    } catch (e) { toast(I18N.saveFailed + ": " + e.message, "err"); }
  };
  $("#c-cancel").onclick = () => { $("#modal").hidden = true; };
}
