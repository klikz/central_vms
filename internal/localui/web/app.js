// vmsagent lokal UI — vanilla JS (framework yo'q, build'ga npm kerak emas).
// Ekranlar: kameralar · kashf etish · holat · sozlamalar · jurnal (docs/protocol.md §17).
"use strict";

const SCREENS = ["cams", "discovery", "status", "settings", "log"];
const I18N = {
  loading: "yuklanmoqda…",
  needPin: "PIN talab qilinadi",
  saved: "Saqlandi",
  saveFailed: "Saqlanmadi",
  scanDone: (n) => `${n} kamera topildi`,
  serverOnline: "server: ulangan",
  serverOffline: "server: uzilgan",
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
  $$(".rail-item").forEach((a) => a.classList.toggle("active", a.dataset.nav === name));
  if (location.hash !== "#/" + name) location.hash = "#/" + name;
  load(name);
}

let agentPin = ""; // yozuv amallari uchun (§10.3)

async function api(path, opts) {
  const o = Object.assign({ credentials: "same-origin" }, opts || {});
  o.headers = Object.assign({}, o.headers || {});
  if (agentPin) o.headers["X-Agent-Pin"] = agentPin;
  const res = await fetch(path, o);
  if (res.status === 401) { toast(I18N.needPin, "err"); return null; }
  if (!res.ok) throw new Error(path + ": HTTP " + res.status);
  return res.status === 204 ? null : res.json();
}

async function loadStatus() {
  try {
    const st = await api("/api/ui/status");
    if (!st) return;
    const ver = st.version || "?";
      $("#txt-version").textContent = /^[0-9]/.test(ver) ? "v" + ver : ver;
    $("#txt-cams").textContent = `kameralar: ${st.cams_online ?? 0}/${st.cams_total ?? 0}`;
    $("#txt-publishes").textContent = `publish: ${st.publishes ?? 0}`;
    const dot = $("#dot-server");
    dot.className = "dot " + (st.server_online ? "dot-ok" : "dot-off");
    $("#txt-server").textContent = st.server_online ? I18N.serverOnline : I18N.serverOffline;
  } catch (e) {
    $("#dot-server").className = "dot dot-err";
    $("#txt-server").textContent = I18N.serverOffline;
  }
}

async function load(screen) {
  await loadStatus();
  if (screen === "cams") await loadCams();
  if (screen === "discovery") await loadDiscovery();
  if (screen === "log") {
    try {
      const r = await api("/api/ui/log?limit=200");
      $("#log-out").textContent = (r && r.lines && r.lines.length) ? r.lines.join("\n") : "jurnal bo'sh";
    } catch (e) { /* eski versiyalarda log yo'q */ }
  }
}

// --- klaviatura (§17) ---
document.addEventListener("keydown", (ev) => {
  const typing = /^(INPUT|SELECT|TEXTAREA)$/.test(document.activeElement.tagName);
  if (ev.key === "Escape") { $("#modal").hidden = true; return; }
  if (typing) return;
  const map = { "/": () => $("#filter").focus(), "n": () => showScreen("cams"),
    "d": () => showScreen("discovery"), "s": () => showScreen("status"),
    "t": () => showScreen("settings"), "l": () => showScreen("log"),
    "?": () => toast("Hotkeylar: / filtr · n kameralar · d kashf etish · s holat · t sozlamalar · l jurnal · Esc yopish") };
  const fn = map[ev.key];
  if (fn) { ev.preventDefault(); fn(); }
});

window.addEventListener("hashchange", () => showScreen((location.hash || "#/cams").slice(2)));

document.addEventListener("click", (ev) => {
  const btn = ev.target.closest("[data-act]");
  if (btn && btn.dataset.act === "new") { openNewCamModal(); return; }
  if (btn && btn.dataset.act === "export") { window.location = "/api/ui/cams/export"; return; }
  if (!btn) return;
  const act = btn.dataset.act;
  if (act === "save") {
    api("/api/ui/settings", { method: "PUT", headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ server_url: $("#set-server").value, log_level: $("#set-loglevel").value }) })
      .then(() => toast(I18N.saved, "ok")).catch(() => toast(I18N.saveFailed, "err"));
  } else if (act === "selftest") {
    toast("Self-test C4.6 da qo'shiladi");
  } else if (act === "scan-start") {
    startScan();
  } else if (act === "scan-add") {
    addSelected();
  }
});

showScreen((location.hash || "#/cams").slice(2));
setInterval(loadStatus, 5000);

// --- Kameralar (C2.3): ro'yxat, qo'shish, o'chirish ---
function esc(v) {
  // Regex literal ishlatmaymiz: scripts/jscheck.py (node yo'q muhit) regex ichidagi
  // tirnoqlarni adashtiradi. split/join bir xil natija beradi.
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
    body.innerHTML = '<tr><td colspan="7" class="empty">Ma\'lumot yo\'q — <kbd>n</kbd> bilan qo\'shing yoki <kbd>d</kbd> bilan kashf eting.</td></tr>';
    return;
  }
  body.innerHTML = list.map((c) => {
    const profiles = (c.main_path ? 1 : 0) + (c.sub_path ? 1 : 0);
    const codec = [c.codec_main, c.codec_sub].filter(Boolean).join(" / ") || "—";
    return `<tr>
      <td><input type="checkbox" data-cam="${esc(c.id)}" aria-label="${esc(c.name)} tanlash"></td>
      <td>${esc(c.name || "(nomsiz)")}${c.secret_ref ? `<span class="lock" title="Parol keystore da saqlangan">parol</span>` : ""}</td>
      <td class="mono">${esc(c.host)}${c.port ? ":" + esc(c.port) : ""}</td>
      <td>${profiles}</td>
      <td class="mono">${esc(codec)}</td>
      <td><span class="${badgeClass(c.status)}">${esc(c.status || "unknown")}</span></td>
      <td class="dim" title="Oqim holati C4 bosqichida to'ldiriladi">—</td>
    </tr>`;
  }).join("");
}

// --- Kashf etish (C2.18): skan, natija jadvali, checkbox, ommaviy qo'shish ---
let scanDevices = []; // oxirgi natijalar (key bo'yicha tanlash uchun)

async function loadDiscovery() {
  const body = $("#scan-body");
  if (!body) return;
  let r = null;
  try { r = await api("/api/ui/discovery/results"); } catch (e) { r = null; }
  scanDevices = (r && r.devices) || [];
  if (!scanDevices.length) {
    body.innerHTML = '<tr><td colspan="8" class="empty">Skan natijasi yo\'q — «Skan boshlash» tugmasini bosing.</td></tr>';
    return;
  }
  body.innerHTML = scanDevices.map((d) => {
    const known = d.already_known
      ? '<span class="badge badge-warn">qo\'shilgan</span>'
      : '<span class="badge badge-ok">yangi</span>';
    const check = d.already_known ? "" :
      `<input type="checkbox" class="scan-check" value="${esc(d.key)}">`;
    return `<tr>
      <td class="col-check">${check}</td>
      <td class="mono">${esc(d.ip)}:${esc(d.port)}</td>
      <td>${esc(d.model || "—")}</td>
      <td>${esc(d.name || "—")}</td>
      <td>${d.has_onvif ? "bor" : "yo'q"}</td>
      <td>${d.mac ? "bor" : "—"}</td>
      <td class="dim">—</td>
      <td>${known}</td>
    </tr>`;
  }).join("");
}

async function startScan() {
  toast("Skan boshlandi…");
  try {
    const r = await api("/api/ui/discovery/scan", { method: "POST" });
    if (r) toast(`Skan tugadi: ${r.scanned} host tekshirildi`, "ok");
    await loadDiscovery();
  } catch (e) {
    toast("Skan xatosi", "err");
  }
}

async function addSelected() {
  const keys = Array.from(document.querySelectorAll(".scan-check:checked")).map((c) => c.value);
  if (!keys.length) { toast("Hech narsa tanlanmagan", "err"); return; }
  const in_ = {
    keys: keys,
    name_pattern: ($("#name-pattern") || {}).value || "",
    password: ($("#scan-pass") || {}).value || "",
    enabled: true,
  };
  try {
    const r = await api("/api/ui/discovery/add", {
      method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(in_) });
    if (r) toast(`Qo'shildi: ${r.added}, xato: ${r.failed}`, r.failed ? "warn" : "ok");
    await loadDiscovery();
  } catch (e) {
    toast("Qo'shish xatosi", "err");
  }
}

function openNewCamModal() {
  const box = $("#modal-box");
  if (!box) return;
  box.innerHTML = `<h2>Yangi kamera</h2>
    <div class="row"><label class="field"><span>Nom</span><input id="c-name" placeholder="masalan: Ong eshik"></label>
    <label class="field"><span>Host / IP</span><input id="c-host" placeholder="192.168.10.20"></label></div>
    <div class="row"><label class="field"><span>Port</span><input id="c-port" value="554"></label>
    <label class="field"><span>User</span><input id="c-user" value="admin"></label>
    <label class="field"><span>Parol</span><input id="c-pass" type="password"></label></div>
    <div class="row" style="margin-top:10px">
      <button class="btn btn-primary" id="c-save">Saqlash</button>
      <button class="btn" id="c-cancel">Bekor qilish</button>
    </div>`;
  $("#modal").hidden = false;
  $("#c-save").onclick = async () => {
    const host = $("#c-host").value.trim();
    if (!host) { toast("Host kerak", "err"); return; }
    const pass = $("#c-pass").value;
    // Kamera yozuvida usernama alohida saqlanadi (C2.5 da qo'shiladi);
    // hozircha faqat parol keystore'ga boradi.
    const secret = pass;
    try {
      await api("/api/ui/cams", { method: "POST", headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ name: $("#c-name").value.trim(), host, port: parseInt($("#c-port").value || "554", 10), secret }) });
      toast(I18N.saved, "ok");
      $("#modal").hidden = true;
      await loadCams();
    } catch (e) { toast(I18N.saveFailed + ": " + e.message, "err"); }
  };
  $("#c-cancel").onclick = () => { $("#modal").hidden = true; };
}
