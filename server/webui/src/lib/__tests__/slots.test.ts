import { describe, expect, it } from "vitest";
import {
  addCamera,
  assign,
  autoFill,
  clear,
  firstEmpty,
  pageCount,
  padSlots,
  parseLayoutSlots,
  swap,
} from "../slots";

describe("grid slotlari", () => {
  it("autoFill position tartibida sahifa bo'yicha to'ldiradi", () => {
    const cams = ["c1", "c2", "c3", "c4", "c5"];
    expect(autoFill(cams, 0, 4)).toEqual(["c1", "c2", "c3", "c4"]);
    expect(autoFill(cams, 1, 4)).toEqual(["c5", null, null, null]);
  });

  it("sahifa o'zgarsa slotlar ham o'zgaradi (eski oqimlar yopiladi)", () => {
    const cams = ["c1", "c2", "c3", "c4", "c5"];
    const p0 = autoFill(cams, 0, 4);
    const p1 = autoFill(cams, 1, 4);
    expect(p0.filter((x) => x && !p1.includes(x)).length).toBeGreaterThan(0);
  });

  it("kamroq kamera bo'lsa kataklar bo'sh qoladi", () => {
    expect(autoFill(["c1"], 0, 4)).toEqual(["c1", null, null, null]);
  });

  it("assign/clear/swap", () => {
    let s: (string | null)[] = [null, null, null];
    s = assign(s, 1, "a", 3);
    expect(s).toEqual([null, "a", null]);
    s = swap(s, 0, 1, 3);
    expect(s).toEqual(["a", null, null]);
    s = assign(s, 0, "b", 3);
    s = clear(s, 0, 3);
    expect(s).toEqual([null, null, null]);
  });

  it("swap chegaradan tashqari indeksda o'zgarish qilmaydi", () => {
    expect(swap([null, "a"], 0, 5, 2)).toEqual([null, "a"]);
  });

  it("addCamera: takrorlanmaydi, bo'sh joy topilmasa jim rad etadi", () => {
    let s: (string | null)[] = ["x", "y"];
    s = addCamera(s, "z", 2);
    expect(s).toEqual(["x", "y"]); // to'la
    s = addCamera(s, "x", 4);
    expect(s).toEqual(["x", "y"]); // allaqachon bor
    s = addCamera(["x", null, null, null], "q", 4);
    expect(s).toEqual(["x", "q", null, null]);
  });

  it("firstEmpty", () => {
    expect(firstEmpty(["a", null, "b"])).toBe(1);
    expect(firstEmpty(["a", "b"])).toBe(-1);
  });

  it("pageCount", () => {
    expect(pageCount(0, 9)).toBe(1);
    expect(pageCount(9, 9)).toBe(1);
    expect(pageCount(10, 9)).toBe(2);
    expect(pageCount(16, 16)).toBe(1);
  });

  it("padSlots uzunlikni tekislaydi", () => {
    expect(padSlots(["a"], 3)).toEqual(["a", null, null]);
    expect(padSlots(["a", "b", "c", "d"], 2)).toEqual(["a", "b"]);
  });

  it("parseLayoutSlots: noto'g'ri JSON/elementlar chidamli", () => {
    expect(parseLayoutSlots('["a","b"]', 4)).toEqual([null, null, null, null]); // string emas
    expect(parseLayoutSlots(["a", "b", 5, null, "c"], 4)).toEqual(["a", "b", null, null]);
    expect(parseLayoutSlots(null, 2)).toEqual([null, null]);
  });
});
