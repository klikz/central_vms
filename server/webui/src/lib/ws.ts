// UI push kanali (/api/ui/ws) — avtomatik qayta ulanish bilan.
import { getAccess } from "./api";

export type UIEvent = {
  type: string;
  ts: string;
  data: any;
};

type Handler = (e: UIEvent) => void;

export class UIChannel {
  private ws: WebSocket | null = null;
  private handlers = new Set<Handler>();
  private statusHandlers = new Set<(connected: boolean) => void>();
  private retry = 0;
  private closed = false;
  private live = false;

  connect(): void {
    this.closed = false;
    if (this.live) return; // takroriy ulanish bo'lmasin
    this.live = true;
    this.open();
  }

  private setStatus(v: boolean) {
    this.statusHandlers.forEach((h) => h(v));
  }

  onStatus(h: (connected: boolean) => void): () => void {
    this.statusHandlers.add(h);
    h(this.live && this.ws?.readyState === WebSocket.OPEN);
    return () => this.statusHandlers.delete(h);
  }

  private open() {
    const tok = getAccess();
    if (!tok) return;
    const proto = location.protocol === "https:" ? "wss" : "ws";
    const url = `${proto}://${location.host}/api/ui/ws?access=${encodeURIComponent(tok)}`;
    const ws = new WebSocket(url);
    this.ws = ws;
    ws.onopen = () => {
      this.retry = 0;
      this.setStatus(true);
    };
    ws.onmessage = (m) => {
      try {
        const e = JSON.parse(m.data) as UIEvent;
        this.handlers.forEach((h) => h(e));
      } catch {
        /* buzilgan kadr */
      }
    };
    ws.onclose = () => {
      this.live = false;
      this.setStatus(false);
      if (this.closed) return;
      // Eksponensial qayta ulanish (0.5 s → 10 s gacha).
      const wait = Math.min(10000, 500 * 2 ** this.retry++);
      setTimeout(() => this.open(), wait);
    };
    ws.onerror = () => ws.close();
  }

  on(h: Handler): () => void {
    this.handlers.add(h);
    return () => this.handlers.delete(h);
  }

  close(): void {
    this.closed = true;
    this.ws?.close();
  }
}

export const uiChannel = new UIChannel();
