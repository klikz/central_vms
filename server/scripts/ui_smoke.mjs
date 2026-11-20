// S3 qabul testi: haqiqiy Chrome (headless) orqali UI oqimi.
// Bog'liqlik YO'Q — Node 22 CDP (WebSocket built-in) ishlatiladi.
// Ishlatish: node scripts/ui_smoke.mjs <baseURL> <user> <pass>
const BASE = process.argv[2] ?? "http://127.0.0.1:8081";
const USER = process.argv[3] ?? "admin";
const PASS = process.argv[4] ?? "testpass123";
const CDP = process.env.CDP ?? "http://127.0.0.1:9222";

let ws, nextId = 1;
let diagHandler = () => {};
const netErrors = [];
const consoleLogs = [];
const pending = new Map();

function send(method, params = {}) {
  return new Promise((resolve, reject) => {
    const id = nextId++;
    pending.set(id, { resolve, reject });
    ws.send(JSON.stringify({ id, method, params }));
  });
}

async function connect() {
  const list = await (await fetch(`${CDP}/json/list`)).json();
  const page = list.find((t) => t.type === "page");
  if (!page) throw new Error("CDP page topilmadi");
  ws = new WebSocket(page.webSocketDebuggerUrl);
  await new Promise((res, rej) => {
    ws.onopen = res;
    ws.onerror = rej;
  });
  // Yagona onmessage: javoblarni yechuvchiga yetkazadi + diagnostika.
  // (Ikkinchi handler birinchini yozib qo'ysa — CDP javoblari yo'qoladi.)
  ws.onmessage = (m) => {
    const msg = JSON.parse(m.data);
    if (msg.id && pending.has(msg.id)) {
      const { resolve, reject } = pending.get(msg.id);
      pending.delete(msg.id);
      if (msg.error) reject(new Error(JSON.stringify(msg.error)));
      else resolve(msg.result);
    }
    diagHandler(msg);
  };
  await send("Runtime.enable");
  await send("Page.enable");
  await send("Log.enable").catch(() => undefined);
  await send("Network.enable").catch(() => undefined);
  // DIAGNOSTIKA: tarmoq xatolari va konsol xabarlari yig'iladi.
  diagHandler = (msg) => {
    if (msg.method === "Network.responseReceived") {
      const r = msg.params.response;
      if (r.status >= 400) netErrors.push(`${r.status} ${r.url}`);
    }
    if (msg.method === "Network.loadingFailed") {
      netErrors.push(`fail ${msg.params.errorText} (${msg.params.type})`);
    }
    if (msg.method === "Runtime.consoleAPICalled" && ["error", "warning"].includes(msg.params.type)) {
      const txt = (msg.params.args ?? []).map((a) => a.value ?? a.description ?? a.type).join(" ");
      consoleLogs.push(`${msg.params.type}: ${txt}`.slice(0, 300));
    }
  };
}

// Sahifada JS bajarish (awaitPromise bilan async ifodalar ham).
async function js(expr) {
  const r = await send("Runtime.evaluate", {
    expression: `(async () => { ${expr} })()`,
    awaitPromise: true,
    returnByValue: true,
  });
  if (r.exceptionDetails) {
    throw new Error("page JS: " + (r.exceptionDetails.exception?.description ?? JSON.stringify(r.exceptionDetails)));
  }
  return r.result.value;
}

async function waitFor(expr, timeoutMs = 15000, what = expr) {
  const t0 = Date.now();
  for (;;) {
    // DOM obyektlarini CDP orqali qaytarish mumkin emas (zanjir juda
    // uzun) — shuning uchun natija doim bool'ga aylantiriladi.
    const v = await js(`return !!(${expr});`);
    if (v) return v;
    if (Date.now() - t0 > timeoutMs) throw new Error("kutilmadi: " + what);
    await new Promise((r) => setTimeout(r, 200));
  }
}

let fails = 0;
function check(ok, name, extra = "") {
  console.log(`  ${ok ? "PASS" : "FAIL"}: ${name}${extra ? " — " + extra : ""}`);
  if (!ok) fails++;
}

async function main() {
  await connect();
  await send("Page.navigate", { url: `${BASE}/login` });
  await waitFor(`document.querySelector('input[autocomplete=username]')`, 10000, "login form");
  check(await js(`return document.title === "Central VMS";`), "sahifa sarlavhasi (Central VMS)");

  // React-controlled input'larni to'ldirish (native setter + input event).
  await js(`
    const set = (el, v) => {
      const d = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value").set;
      d.call(el, v);
      el.dispatchEvent(new Event("input", { bubbles: true }));
    };
    set(document.querySelector('input[autocomplete=username]'), ${JSON.stringify(USER)});
    set(document.querySelector('input[type=password]'), ${JSON.stringify(PASS)});
    document.querySelector("button.primary").click();
    return true;
  `);
  await waitFor(`document.querySelector(".shell")`, 10000, "shell (login muvaffaqiyati)");
  check(true, "login → asosiy sahifa");
  check(await js(`return !!document.querySelector('a[href="/admin"]');`), "admin bo'limi ko'rinadi");
  check(
    await js(`return document.body.innerText.includes("Kuzatuv") && !document.body.innerText.includes("Loading");`),
    "interfeys o'zbekcha",
  );

  // Kamera ro'yxati (testagent cams_sync qilgan).
  await waitFor(`document.querySelectorAll(".cam-item").length > 0`, 15000, "kamera ro'yxati");
  const camCount = await js(`return document.querySelectorAll(".cam-item").length;`);
  check(camCount > 0, `kamera ro'yxati (${camCount} ta)`);

  // Kamerani gridda ochish (bosish orqali) → WHEP oqimi chiqishi kerak.
  await js(`document.querySelector(".cam-item").click(); return true;`);
  let live = false;
  try {
    await waitFor(
      `(() => { const v = document.querySelector(".grid .cell video"); return v && v.videoWidth > 0; })()`,
      20000,
      "video kadr",
    );
    live = true;
  } catch {
    live = false;
  }
  const vw = await js(`const v=document.querySelector(".grid .cell video"); return v ? v.videoWidth : 0;`);
  check(live && vw > 0, "WHEP oqimi haqiqiy kadrlar bilan ochildi", `videoWidth=${vw}`);
  if (!live) {
    const overlay = await js(`const o=document.querySelector(".grid .cell .overlay"); return o ? o.innerText : "(qatlamsiz)";`);
    console.log("    katak holati:", JSON.stringify(overlay));
    console.log("    tarmoq xatolari:", netErrors.slice(0, 6).join(" | ") || "—");
    console.log("    konsol:", consoleLogs.slice(0, 6).join(" | ") || "—");
  }

  // Sevimlilar: yulduzcha bosilsa 'on' bo'ladi.
  await js(`document.querySelector(".cam-item .star").click(); return true;`);
  await waitFor(`!!document.querySelector(".cam-item .star.on")`, 5000, "sevimli");
  check(true, "sevimlilar (⭐) ishlaydi");

  // Klaviatura: 1 → 1x1 grid (1 katak).
  await send("Input.dispatchKeyEvent", { type: "keyDown", key: "1", windowsVirtualKeyCode: 49 });
  await send("Input.dispatchKeyEvent", { type: "keyUp", key: "1", windowsVirtualKeyCode: 49 });
  await waitFor(`document.querySelectorAll(".grid .cell").length === 1`, 5000, "1x1 grid");
  check(true, "klaviatura: 1 → 1×1 grid");

  // 4×4 ga qaytamiz va katak sonini tekshiramiz.
  await send("Input.dispatchKeyEvent", { type: "keyDown", key: "4", windowsVirtualKeyCode: 52 });
  await waitFor(`document.querySelectorAll(".grid .cell").length === 16`, 5000, "4x4 grid");
  check(true, "klaviatura: 4 → 4×4 grid");

  // Layout: saqlash → standart qilish → sahifani qayta yuklab tiklash.
  const camName = await js(`const el=document.querySelector(".cam-item .cam-name"); return el ? el.textContent : "";`);
  await js(`
    const set = (el, v) => {
      const proto = el.tagName === "SELECT" ? HTMLSelectElement.prototype : HTMLInputElement.prototype;
      Object.getOwnPropertyDescriptor(proto, "value").set.call(el, v);
      el.dispatchEvent(new Event(el.tagName === "SELECT" ? "change" : "input", { bubbles: true }));
    };
    const inp = [...document.querySelectorAll("input")].find((i) => i.placeholder === "Nomi");
    if (!inp) throw new Error("layout input topilmadi");
    set(inp, "Test layout");
    const save = [...document.querySelectorAll("button")].find((b) => b.textContent === "Layoutni saqlash");
    save.click();
    return true;
  `);
  await waitFor(`[...document.querySelectorAll("select option")].some((o) => o.textContent.includes("Test layout"))`, 6000, "layout ro'yxatda");
  await js(`
    const sel = document.querySelector("select");
    const opt = [...sel.options].find((o) => o.textContent.includes("Test layout"));
    Object.getOwnPropertyDescriptor(HTMLSelectElement.prototype, "value").set.call(sel, opt.value);
    sel.dispatchEvent(new Event("change", { bubbles: true }));
    const def = [...document.querySelectorAll("button")].find((b) => b.textContent === "Standart");
    def.click();
    return true;
  `);
  await new Promise((r) => setTimeout(r, 800));
  await send("Page.navigate", { url: `${BASE}/monitor` });
  let restored = false;
  try {
    await waitFor(
      `[...document.querySelectorAll(".grid .cell .label")].some((e) => e.textContent === ${JSON.stringify(camName)})`,
      12000,
      "layout tiklandi",
    );
    restored = true;
  } catch {
    restored = false;
  }
  check(restored, "layout saqlandi va qayta yuklashda tiklandi", camName);

  // Page Visibility: tab yashirinsa oqim to'xtashi kerak. Chrome headless'da
  // haqiqiy yashirish yo'q — document.hidden/visibilityState override qilinadi
  // (ilova aynan shu signalni o'qiydi).
  await js(`
    Object.defineProperty(document, "hidden", { configurable: true, get: () => true });
    Object.defineProperty(document, "visibilityState", { configurable: true, get: () => "hidden" });
    document.dispatchEvent(new Event("visibilitychange"));
    return document.hidden === true;
  `);
  await new Promise((r) => setTimeout(r, 1500));
  const stopped = await js(
    `const v=document.querySelector(".grid .cell video"); return !v || !v.srcObject || v.srcObject.getTracks().every((t) => t.readyState === "ended");`,
  );
  check(!!stopped, "tab yashirinsa oqim to'xtadi");

  console.log(fails === 0 ? "\nS3 UI QABUL: O'TDI" : `\nS3 UI QABUL: ${fails} xato`);
  ws.close();
  process.exit(fails === 0 ? 0 : 1);
}

// Qo'riqchi: test 90 sdan oshsa — osilib qolgan so'rovlar bilan xulosa.
const watchdog = setTimeout(() => {
  console.log("xato: ui_smoke 90 sda tugamadi (osilib qoldi)");
  console.log("    tarmoq xatolari:", netErrors.slice(0, 8).join(" | ") || "—");
  console.log("    konsol:", consoleLogs.slice(0, 8).join(" | ") || "—");
  process.exit(3);
}, 90000);
watchdog.unref();

main().catch((e) => {
  console.error("xato:", e.message);
  process.exit(2);
});
