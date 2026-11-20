import { describe, expect, it } from "vitest";
import { DEFAULT_GRID, GRIDS, gridCells, keyAction, pageOf, PREVIEW } from "../limits";

describe("grid va preview siyosati", () => {
  it("grid o'lchamlari 1..4, kataklar kvadrat", () => {
    expect(GRIDS).toEqual([1, 2, 3, 4]);
    expect(DEFAULT_GRID).toBe(3);
    expect(gridCells(3)).toBe(9);
    expect(gridCells(4)).toBe(16);
  });
  it("preview debounsi REJA §16.2 raqamlari", () => {
    expect(PREVIEW.dwellMs).toBe(600);
    expect(PREVIEW.minOnMs).toBe(2000);
    expect(PREVIEW.closeDelayMs).toBe(1000);
  });
  it("pagination hisobi", () => {
    expect(pageOf(0, 9)).toBe(0);
    expect(pageOf(8, 9)).toBe(0);
    expect(pageOf(9, 9)).toBe(1);
    expect(pageOf(15, 9)).toBe(1);
  });
});

describe("klaviatura xaritasi", () => {
  const ev = (key: string, target: EventTarget = new EventTarget()) =>
    keyAction({ key, target } as unknown as KeyboardEvent);

  it("1..4 grid almashtiradi", () => {
    expect(ev("2")).toEqual({ kind: "grid", n: 2 });
  });
  it("←/→ sahifa, Esc fullscreen'dan chiqish", () => {
    expect(ev("ArrowLeft")).toEqual({ kind: "page", delta: -1 });
    expect(ev("ArrowRight")).toEqual({ kind: "page", delta: 1 });
    expect(ev("Escape")).toEqual({ kind: "esc" });
  });
  it("input ichida tugmalar ishlamaydi (matn yozish buzilmasin)", () => {
    const input = { tagName: "INPUT" } as unknown as EventTarget;
    expect(keyAction({ key: "2", target: input } as unknown as KeyboardEvent)).toBeNull();
  });
  it("boshqa tugmalar — null", () => {
    expect(ev("x")).toBeNull();
    expect(ev("F5")).toBeNull();
  });
});
