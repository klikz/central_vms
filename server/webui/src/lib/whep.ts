// WHEP klient — native RTCPeerConnection (kutubxonasiz, REJA §4: ~120 qator).
// MediaMTX WHEP: POST offer (application/sdp) → 200 answer; Location
// resurs URL'i → yopishda DELETE. Kechikish maqsadi: ≤1.5 s (REJA §7).
export interface WhepOptions {
  url: string; // http://host:8889/<path>/whep
  token: string; // MediaMTX o'quvchi tokeni (Authorization: Bearer)
  onTrack: (stream: MediaStream) => void;
  onState?: (s: "connecting" | "connected" | "failed" | "closed") => void;
  // ICE yig'ishi uchun yuqori chegara: undan keyin SDP yuboriladi
  // (trickle MediaMTX'da ishonchsiz — to'liq SDP yuborish barqarorroq).
  gatherTimeoutMs?: number;
}

export class WhepClient {
  private pc: RTCPeerConnection | null = null;
  private resource: string | null = null;
  private opts: WhepOptions;

  constructor(opts: WhepOptions) {
    this.opts = opts;
  }

  async start(): Promise<void> {
    const pc = new RTCPeerConnection({ iceServers: [] }); // lokal tarmoq: host kandidatalar
    this.pc = pc;
    pc.addTransceiver("video", { direction: "recvonly" });
    pc.addTransceiver("audio", { direction: "recvonly" });

    const stream = new MediaStream();
    pc.ontrack = (ev) => {
      if (ev.streams[0]) {
        ev.streams[0].getTracks().forEach((t) => stream.addTrack(t));
      } else {
        stream.addTrack(ev.track);
      }
      this.opts.onTrack(stream);
    };
    pc.onconnectionstatechange = () => {
      const s = pc.connectionState;
      if (s === "connected") this.opts.onState?.("connected");
      if (s === "failed" || s === "disconnected") this.opts.onState?.("failed");
    };

    const offer = await pc.createOffer();
    await pc.setLocalDescription(offer);
    await this.waitGathering(pc);

    // Token URL query'da ham bo'lishi shart: MediaMTX auth-hook'ga
    // Authorization header'ni uzatmaydi (o'z `jwt`ini yuboradi),
    // `query` satrini esa uzatadi — tekshirilgan (v1.11.3).
    const postURL = new URL(this.opts.url);
    if (!postURL.searchParams.has("token")) postURL.searchParams.set("token", this.opts.token);
    const res = await fetch(postURL.toString(), {
      method: "POST",
      headers: {
        "Content-Type": "application/sdp",
        Authorization: "Bearer " + this.opts.token,
      },
      body: pc.localDescription?.sdp ?? "",
    });
    if (!res.ok) {
      this.opts.onState?.("failed");
      throw new Error(`WHEP ${res.status}: ${(await res.text()).slice(0, 160)}`);
    }
    const answer = await res.text();
    const loc = res.headers.get("Location");
    if (loc) {
      // Location nisbiy bo'lishi ham mumkin — to'liq URL qilamiz.
      this.resource = new URL(loc, this.opts.url).toString();
    }
    await pc.setRemoteDescription({ type: "answer", sdp: answer });
    this.opts.onState?.("connecting");
  }

  private waitGathering(pc: RTCPeerConnection): Promise<void> {
    if (pc.iceGatheringState === "complete") return Promise.resolve();
    return new Promise((resolve) => {
      const done = () => {
        if (pc.iceGatheringState === "complete") {
          pc.removeEventListener("icegatheringstatechange", done);
          clearTimeout(timer);
          resolve();
        }
      };
      const timer = setTimeout(() => {
        pc.removeEventListener("icegatheringstatechange", done);
        resolve();
      }, this.opts.gatherTimeoutMs ?? 2000);
      pc.addEventListener("icegatheringstatechange", done);
    });
  }

  async stats(): Promise<RTCStatsReport | null> {
    return this.pc ? this.pc.getStats() : null;
  }

  async stop(): Promise<void> {
    if (this.pc) {
      this.pc.close();
      this.pc = null;
    }
    if (this.resource) {
      try {
        await fetch(this.resource, {
          method: "DELETE",
          headers: { Authorization: "Bearer " + this.opts.token },
        });
      } catch {
        /* server idle stop o'zi tozalaydi */
      }
      this.resource = null;
    }
    this.opts.onState?.("closed");
  }
}

// Brauzer H.265 (HEVC) qo'llab-quvvatlashini bir marta aniqlash (REJA §16.4:
// UI birinchi ulanishda browser_caps yuboradi).
export async function detectHevc(): Promise<boolean> {
  try {
    const pc = new RTCPeerConnection({ iceServers: [] });
    const caps = await RTCRtpSender.getCapabilities("video");
    pc.close();
    const list = (caps?.codecs ?? []) as { mimeType: string }[];
    return list.some((c) => /H265|HEVC/i.test(c.mimeType));
  } catch {
    return false;
  }
}
