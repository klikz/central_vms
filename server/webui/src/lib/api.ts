// API mijozlari (docs/api.md). Access token xotirada saqlanadi (localStorage
// EMAS — XSS xavfi), refresh cookie bilan avtomatik uzatiladi.
export type Role = "admin" | "viewer";

export interface User {
  id: number;
  username: string;
  role: Role;
  display_name?: string;
}

export interface Me extends User {
  limits: { user_max_sessions: number; global_max_sessions: number; preview_max: number };
}

export interface Site {
  id: number;
  name: string;
}

export interface Client {
  id: number;
  name: string;
  agent_id: string;
  status: "online" | "offline";
  version?: string;
  os?: string;
  lan_ip?: string;
  site_id?: number;
  cameras_count: number;
  online_count: number;
  max_uploads: number;
  max_transcodes: number;
  transcode_mode: string;
  last_seen_at?: string;
}

export interface Camera {
  id: string;
  client_id: number;
  name: string;
  codec_main: string;
  codec_sub: string;
  play_profile: "main" | "sub";
  transcode_mode: "auto" | "server" | "client" | "off";
  enabled: boolean;
  position: number;
  status: "online" | "offline" | "error";
  rtsp_masked?: string;
}

export interface PlayResult {
  session_id: string;
  whep_url: string;
  rtsp_url: string;
  token: string;
  ttl: number;
  codec: string;
  transcoded: boolean;
  backend: string;
  expires_at: string;
}

export interface SessionRow {
  id: string;
  camera_id: string;
  user_id: number;
  profile: "main" | "sub";
  kind: "play" | "preview";
  state: string;
  expires_at: string;
  publish_id?: string;
}

export class ApiError extends Error {
  status: number;
  kind: string;
  detail: string;
  constructor(status: number, kind: string, detail: string) {
    super(`${kind}${detail ? ": " + detail : ""}`);
    this.status = status;
    this.kind = kind;
    this.detail = detail;
  }
}

// Brauzer imkoniyatlari bir marta aniqlanadi (detectHevc) va har play'da
// yuboriladi — server transcode qarorini shu asosda qabul qiladi.
let hevcCapable: boolean | null = null;
export function setHevcCapable(v: boolean) {
  hevcCapable = v;
}
export function browserCaps() {
  return { hevc: hevcCapable === true };
}

let accessToken = "";
export const setAccess = (t: string) => {
  accessToken = t;
  window.__accessToken = t;
};
export const getAccess = () => accessToken;
// pagehide'dagi keepalive DELETE uchun (fetch header'lari qo'lda kerak).
declare global {
  interface Window {
    __accessToken?: string;
  }
}
export const hasAccess = () => accessToken !== "";

async function raw(method: string, path: string, body?: unknown): Promise<Response> {
  const headers: Record<string, string> = {};
  if (body !== undefined) headers["Content-Type"] = "application/json";
  if (accessToken) headers["Authorization"] = "Bearer " + accessToken;
  return fetch(path, {
    method,
    headers,
    credentials: "include",
    body: body === undefined ? undefined : JSON.stringify(body),
  });
}

async function parse(r: Response): Promise<any> {
  const text = await r.text();
  if (!r.ok) {
    let kind = "error";
    let detail = text.slice(0, 200);
    try {
      const j = JSON.parse(text);
      kind = j.error ?? kind;
      detail = j.detail ?? detail;
    } catch {
      /* JSON emas */
    }
    throw new ApiError(r.status, kind, detail);
  }
  return text ? JSON.parse(text) : null;
}

// So'rov: 401 bo'lsa bir marta refresh qilib qayta urinish (cookie bilan).
async function req<T = any>(method: string, path: string, body?: unknown): Promise<T> {
  let r = await raw(method, path, body);
  if (r.status === 401 && accessToken) {
    const ok = await tryRefresh();
    if (ok) r = await raw(method, path, body);
  }
  return parse(r);
}

export async function tryRefresh(): Promise<boolean> {
  try {
    const j = await parse(await fetch("/api/auth/refresh", { method: "POST", credentials: "include" }));
    if (j?.access) {
      setAccess(j.access);
      return true;
    }
  } catch {
    /* refresh cookie yaroqsiz */
  }
  return false;
}

export const api = {
  login: async (username: string, password: string) => {
    const j = await parse(await raw("POST", "/api/auth/login", { username, password }));
    setAccess(j.access);
    return j.user as User;
  },
  logout: () => req("POST", "/api/auth/logout"),
  me: () => req<Me>("GET", "/api/me"),
  sites: () => req<Site[]>("GET", "/api/sites"),
  clients: () => req<Client[]>("GET", "/api/clients"),
  cameras: (clientId: number) => req<Camera[]>("GET", `/api/clients/${clientId}/cameras`),
  camera: (id: string) => req<Camera>("GET", `/api/cameras/${id}`),
  play: (id: string, profile?: string) =>
    req<PlayResult>("POST", `/api/cameras/${id}/play`, { profile, browser: browserCaps() }),
  preview: (id: string, on: boolean, profile?: string) =>
    req<PlayResult | { ok: boolean }>("POST", `/api/cameras/${id}/preview`, {
      on,
      profile,
      browser: browserCaps(),
    }),
  refreshSession: (session_id: string, extend_s = 120) =>
    req("POST", "/api/sessions/refresh", { session_id, extend_s }),
  stopSession: (id: string) => req("DELETE", `/api/sessions/${id}`),
  sessions: () => req<SessionRow[]>("GET", "/api/sessions"),
  favorites: () => req<string[]>("GET", "/api/favorites"),
  addFavorite: (cameraId: string) => req("POST", `/api/favorites/${cameraId}`),
  delFavorite: (cameraId: string) => req("DELETE", `/api/favorites/${cameraId}`),
  reorderFavorites: (ids: string[]) => req("PUT", "/api/favorites/reorder", ids),
  layouts: () => req<any[]>("GET", "/api/layouts"),
  createLayout: (name: string, grid: number, slots: unknown) =>
    req<{ id: number }>("POST", "/api/layouts", { name, grid, slots }),
  updateLayout: (id: number, patch: unknown) => req("PUT", `/api/layouts/${id}`, patch),
  deleteLayout: (id: number) => req("DELETE", `/api/layouts/${id}`),
  setDefaultLayout: (id: number) => req("PUT", `/api/layouts/${id}/default`),
  admin: {
    users: () => req<User[]>("GET", "/api/admin/users"),
    createUser: (username: string, password: string, role: Role) =>
      req("POST", "/api/admin/users", { username, password, role }),
    patchUser: (id: number, patch: unknown) => req("PATCH", `/api/admin/users/${id}`, patch),
    enrollments: (site_name = "") => req<{ code: string; expires_at: string }>("POST", "/api/admin/enrollments", { site_name }),
    patchClient: (id: number, patch: unknown) => req("PATCH", `/api/admin/clients/${id}`, patch),
    deleteClient: (id: number) => req("DELETE", `/api/admin/clients/${id}`),
    rotateSecret: (id: number) => req<{ agent_secret: string }>("POST", `/api/admin/clients/${id}/secret-rotate`),
    patchCamera: (id: string, patch: unknown) => req("PATCH", `/api/admin/cameras/${id}`, patch),
    audit: () => req<any[]>("GET", "/api/admin/audit"),
  },
};
