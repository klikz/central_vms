// Oqim hovuzi (player pool): bitta kamera+profil uchun BITTA WHEP
// sessiyasi — bir necha katak bo'lsa ham ulash qayta ishlatiladi
// (REJA §16.2 dedup). oxirgi katak yopilsa sessiya yopiladi.
import { api, ApiError, type PlayResult } from "./api";
import { WhepClient } from "./whep";
import { toast } from "../toast";
import { t } from "./i18n";

type Entry = {
  key: string;
  sess: PlayResult;
  whep: WhepClient;
  stream: MediaStream;
  videos: Set<HTMLVideoElement>;
  refreshTimer?: number;
  state: "connecting" | "live" | "error";
  listeners: Set<(e: Entry) => void>;
};

const pool = new Map<string, Entry>();

export type StreamKind = "main" | "preview";

export interface StreamOpts {
  profile?: string;
  kind?: StreamKind; // preview — server tomonida preview_max hisoblagichiga tushadi
}

function keyOf(camId: string, o?: StreamOpts) {
  return `${camId}|${o?.profile ?? ""}|${o?.kind ?? "main"}`;
}

function notify(e: Entry) {
  e.listeners.forEach((f) => f(e));
}

function limitToast(err: unknown) {
  const ae = err as ApiError;
  if (ae?.kind === "user_limit") toast(t("toast.limit_user"), "warn");
  else if (ae?.kind === "global_limit") toast(t("toast.limit_global"), "warn");
  else if (ae?.kind === "preview_limit") toast(t("toast.limit_preview"), "warn");
  else if (ae?.kind === "client_offline") toast(t("toast.agent_offline"), "err");
  else if (ae?.kind === "media_offline") toast("media server vaqtincha ishlamayapti", "err");
  else if (ae?.message) toast(ae.message, "err");
}

export function poolSize(): number {
  return pool.size;
}

export async function attach(
  camId: string,
  opts: StreamOpts | undefined,
  el: HTMLVideoElement,
  onState?: (s: Entry) => void,
): Promise<void> {
  const key = keyOf(camId, opts);
  let e = pool.get(key);
  if (e) {
    e.videos.add(el);
    el.srcObject = e.stream;
    if (onState) {
      e.listeners.add(onState);
      onState(e);
    }
    return;
  }
  e = {
    key,
    sess: undefined as unknown as PlayResult,
    whep: undefined as unknown as WhepClient,
    stream: new MediaStream(),
    videos: new Set([el]),
    state: "connecting",
    listeners: new Set(onState ? [onState] : []),
  };
  pool.set(key, e);
  try {
    const sess =
      opts?.kind === "preview"
        ? ((await api.preview(camId, true, opts.profile)) as PlayResult)
        : await api.play(camId, opts?.profile);
    e.sess = sess;
    const whep = new WhepClient({
      url: sess.whep_url,
      token: sess.token,
      onTrack: (st) => {
        e!.stream = st;
        e!.videos.forEach((v) => (v.srcObject = st));
        e!.state = "live";
        notify(e!);
      },
      onState: (s) => {
        if (s === "failed") {
          e!.state = "error";
          notify(e!);
        }
      },
    });
    e.whep = whep;
    // Transcode/publish hali tayyor bo'lmasligi mumkin — WHEP'ni
    // qisqa pauzalar bilan qayta uramiz (UI "transcode boshlandi…"
    // holatida turadi, xato ko'rsatmaydi).
    let lastErr: unknown = null;
    for (let attempt = 0; attempt < 6; attempt++) {
      try {
        await whep.start();
        lastErr = null;
        break;
      } catch (err) {
        lastErr = err;
        await new Promise((r) => setTimeout(r, 1000));
      }
    }
    if (lastErr) throw lastErr;
    // TTL yarim muddatda uzatib turamiz (token/session hayotda qolsin).
    e.refreshTimer = window.setInterval(() => {
      void api.refreshSession(sess.session_id).catch(() => void releaseByKey(key));
    }, Math.max(15, Math.floor(sess.ttl / 2)) * 1000);
  } catch (err) {
    pool.delete(key);
    e.state = "error";
    notify(e);
    limitToast(err);
    throw err;
  }
}

export async function releaseByKey(key: string) {
  const e = pool.get(key);
  if (!e) return;
  e.videos.clear();
  await teardown(e);
}

export async function release(camId: string | undefined, opts: StreamOpts | undefined, el?: HTMLVideoElement) {
  if (!camId) return;
  const key = keyOf(camId, opts);
  const e = pool.get(key);
  if (!e) return;
  if (el) e.videos.delete(el);
  if (e.videos.size > 0) return;
  await teardown(e);
}

async function teardown(e: Entry) {
  pool.delete(e.key);
  if (e.refreshTimer) clearInterval(e.refreshTimer);
  await e.whep?.stop?.().catch(() => undefined);
  if (e.sess?.session_id) await api.stopSession(e.sess.session_id).catch(() => undefined);
}

// Sahifa yashirinsa/brauzer yopilsa — barcha oqimlarni to'xtatish
// (REJA §16.2: tab yashirinsa barcha oqim to'xtaydi).
export async function releaseAll() {
  const keys = [...pool.keys()];
  await Promise.all(keys.map((k) => release(k, undefined)));
}
