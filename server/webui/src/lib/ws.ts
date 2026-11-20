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
  private retry = 0;
  private closed = false;
  onStatus: (connected: boolean) => void = () => {};

  connect(): void {
    this.closed = false;
    this.open();
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
      this.onStatus(true);
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
      this.onStatus(false);
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
