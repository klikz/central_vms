// Preview scheduler — REJA §16.2: kursor katakda 600 ms tursa ochiladi,
// ochilsa kamida 2 s yuradi, kursor ketgach 1 s kutib yopiladi.
// Taymerlar sozlanadi (testlarda sun'iy soat bilan tekshiriladi).
import { PREVIEW } from "./limits";

export interface PreviewPolicy {
  dwellMs: number;
  minOnMs: number;
  closeDelayMs: number;
}

type SetTimer = (fn: () => void, ms: number) => number;
type ClearTimer = (id: number) => void;

export class PreviewScheduler {
  private pendingCam: string | null = null; // ochilishi kutilayotgan
  private openCam: string | null = null; // ochiq
  private openAt = 0;
  private openTimer = 0;
  private closeTimer = 0;

  constructor(
    private onOpen: (camId: string) => void,
    private onClose: (camId: string) => void,
    private policy: PreviewPolicy = PREVIEW,
    private now: () => number = () => Date.now(),
    private setTimer: SetTimer = (fn, ms) => setTimeout(fn, ms) as unknown as number,
    private clearTimer: ClearTimer = (id) => clearTimeout(id as unknown as ReturnType<typeof setTimeout>),
  ) {}

  get current(): string | null {
    return this.openCam;
  }

  /** Kursor kamera ustiga kirdi. */
  hover(camId: string, alreadyInGrid: boolean): void {
    this.clearTimers();
    if (camId === this.openCam) return; // allaqachon ochiq
    // Bitta preview: boshqa kameraga o'tsа eski darhol yopiladi
    // (preview_max 8 ni ortiqcha egallamaslik uchun).
    if (this.openCam) this.closeNow();
    if (alreadyInGrid) {
      // Grid'da ochiq kamera uchun qo'shimcha preview sessiyasi YO'Q
      // (dedup — server tomonida ham refcount bor, lekin UI ortiqcha
      // so'rov yubormaydi).
      this.closeNow();
      return;
    }
    this.pendingCam = camId;
    const wait = this.policy.dwellMs;
    this.openTimer = this.setTimer(() => {
      this.pendingCam = null;
      this.openCam = camId;
      this.openAt = this.now();
      this.onOpen(camId);
    }, wait);
  }

  /** Kursor katakdan chiqdi. */
  leave(): void {
    if (this.pendingCam) {
      this.clearTimers();
      this.pendingCam = null;
      return;
    }
    if (!this.openCam) return;
    // Kamida minOnMs yurishi kerak — tezkor yopish isrof bo'lmasligi uchun.
    const minLeft = this.openAt + this.policy.minOnMs - this.now();
    const delay = Math.max(this.policy.closeDelayMs, minLeft);
    this.closeTimer = this.setTimer(() => this.closeNow(), delay);
  }

  /** Tanlov o'zgardi (sahifa/client almashtirildi) — darhol yop. */
  cancel(): void {
    this.clearTimers();
    this.pendingCam = null;
    this.closeNow();
  }

  private closeNow(): void {
    if (!this.openCam) return;
    const cam = this.openCam;
    this.openCam = null;
    this.onClose(cam);
  }

  private clearTimers(): void {
    if (this.openTimer) this.clearTimer(this.openTimer);
    if (this.closeTimer) this.clearTimer(this.closeTimer);
    this.openTimer = 0;
    this.closeTimer = 0;
  }
}
