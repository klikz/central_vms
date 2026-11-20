// Limitlar va preview siyosati (REJA §16.2: debounce 600 ms, min 2 s,
// yopish 1 s; grid o'lchamlari; klaviatura xaritasi).
export const PREVIEW = {
  dwellMs: 600, // kursor katakda shuncha tursa preview ochiladi
  minOnMs: 2000, // ochilganda kamida shuncha yuradi (tezkor yopish isrof)
  closeDelayMs: 1000, // kursor ketgach shuncha kutilib yopiladi
};

export const GRIDS = [1, 2, 3, 4] as const; // 1x1 … 4x4
export const DEFAULT_GRID = 3; // default 3x3 (REJA §7)

export function gridCells(n: number): number {
  return n * n;
}

export function pageOf(index: number, perPage: number): number {
  return Math.floor(index / perPage);
}

// Klaviatura: 1..4 grid, ←/→ sahifa, Esc fullscreen'dan chiqish.
export type KeyAction =
  | { kind: "grid"; n: number }
  | { kind: "page"; delta: number }
  | { kind: "esc" }
  | null;

export function keyAction(e: KeyboardEvent): KeyAction {
  // Matn maydonida tugmalar ishlamasligi kerak (instanceof o'rniga
  // tagName — test muhitida (node) DOM klasslari yo'q).
  const el = e.target as (HTMLElement | null) ?? null;
  if (el && (el.tagName === "INPUT" || el.tagName === "SELECT" || el.tagName === "TEXTAREA" || el.isContentEditable))
    return null;
  if (e.key >= "1" && e.key <= "4") return { kind: "grid", n: Number(e.key) };
  if (e.key === "ArrowLeft") return { kind: "page", delta: -1 };
  if (e.key === "ArrowRight") return { kind: "page", delta: 1 };
  if (e.key === "Escape") return { kind: "esc" };
  return null;
}
