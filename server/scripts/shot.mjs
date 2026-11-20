// Skrinshot olish: CDP orqali sahifani ochib, kerakli holatga keltirib PNG oladi.
// Ishlatish: node scripts/shot.mjs <out.png> <url> [login parol] [kutiladigan selector] [JS qatori]
const OUT = process.argv[2];
const URL1 = process.argv[3];
const USER = process.argv[4];
const PASS = process.argv[5];
const SEL = process.argv[6];
const EXTRA = process.argv[7] || "";
const CDP = process.env.CDP ?? "http://127.0.0.1:9222";

let ws, nextId = 1;
const pending = new Map();
const netErr = [];
const cons = [];

function send(method, params = {}) {
  return new Promise((resolve, reject) => {
    const id = nextId++;
    pending.set(id, { resolve, reject });
    ws.send(JSON.stringify({ id, method, params }));
  });
}
async function js(expr) {
  const r = await send("Runtime.evaluate", {
    expression: `(async () => { ${expr} })()`,
    awaitPromise: true,
    returnByValue: true,
  });
  if (r.exceptionDetails) throw new Error("JS: " + (r.exceptionDetails.exception?.description ?? ""));
  return r.result.value;
}
async function waitFor(expr, ms = 15000) {
  const t0 = Date.now();
  for (;;) {
    if (await js(`return !!(${expr});`)) return;
    if (Date.now() - t0 > ms) throw new Error("kutilmadi: " + expr);
    await new Promise((r) => setTimeout(r, 200));
  }
}

const list = await (await fetch(`${CDP}/json/list`)).json();
const page = list.find((t) => t.type === "page");
if (!page) throw new Error("CDP page topilmadi");
ws = new WebSocket(page.webSocketDebuggerUrl);
await new Promise((res, rej) => {
  ws.onopen = res;
  ws.onerror = rej;
});
// YAGONA onmessage: javoblar + diagnostika (ikkita handler biri-birini yozib qo'yadi).
ws.onmessage = (m) => {
  const msg = JSON.parse(m.data);
  if (msg.id && pending.has(msg.id)) {
    const { resolve, reject } = pending.get(msg.id);
    pending.delete(msg.id);
    if (msg.error) reject(new Error(JSON.stringify(msg.error)));
    else resolve(msg.result);
  }
  if (msg.method === "Network.responseReceived" && msg.params.response.status >= 400) {
    netErr.push(`${msg.params.response.status} ${msg.params.response.url}`);
  }
  if (msg.method === "Network.loadingFailed") netErr.push(`fail ${msg.params.errorText} (${msg.params.type})`);
  if (msg.method === "Runtime.consoleAPICalled" && ["error", "warning"].includes(msg.params.type)) {
    cons.push((msg.params.args ?? []).map((a) => a.value ?? a.description ?? a.type).join(" ").slice(0, 200));
  }
};

await send("Runtime.enable");
await send("Page.enable");
await send("Network.enable").catch(() => undefined);
await send("Emulation.setDeviceMetricsOverride", { width: 1440, height: 900, deviceScaleFactor: 1, mobile: false });

await send("Page.navigate", { url: URL1 });
await new Promise((r) => setTimeout(r, 900));

if (USER) {
  await waitFor(`document.querySelector('input[autocomplete=username]') || document.querySelector(".shell")`, 12000);
  if (await js(`return !!document.querySelector('input[autocomplete=username]');`)) {
    await js(`
      const set = (el, v) => {
        Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value").set.call(el, v);
        el.dispatchEvent(new Event("input", { bubbles: true }));
      };
      set(document.querySelector('input[autocomplete=username]'), ${JSON.stringify(USER)});
      set(document.querySelector('input[type=password]'), ${JSON.stringify(PASS)});
      document.querySelector(".modal-box button.btn-primary").click();
      return true;
    `);
    await waitFor(`document.querySelector(".shell")`);
  } else {
    console.log("allaqachon tizimga kirgan");
  }
}
if (SEL) await waitFor(`document.querySelector(${JSON.stringify(SEL)})`);
if (EXTRA) await js(EXTRA);
await new Promise((r) => setTimeout(r, 1200));

console.log("tarmoq xatolari:", netErr.slice(0, 10).join(" | ") || "—");
console.log("konsol:", cons.slice(0, 6).join(" | ") || "—");
console.log("katak holati:", JSON.stringify(await js(`const o=document.querySelector(".cell .overlay"); return o?o.innerText:"(yo'q)";`)));
console.log("video:", await js(`const v=document.querySelector(".cell video"); return v? v.videoWidth+"x"+v.videoHeight : "yo'q";`));

const shot = await send("Page.captureScreenshot", { format: "png" });
const { writeFileSync } = await import("node:fs");
writeFileSync(OUT, Buffer.from(shot.data, "base64"));
console.log("yozildi:", OUT);
ws.close();
process.exit(0);
