import { describe, expect, it } from "vitest";
import { PreviewScheduler } from "../preview";

// Sun'iy taymerlar: soat qo'lda yurgiziladi (testlar real vaqtda uxlamaydi).
function harness(dwellMs = 600, minOnMs = 2000, closeDelayMs = 1000) {
  let clock = 0;
  let nextId = 1;
  const timers = new Map<number, { at: number; fn: () => void }>();
  const opened: string[] = [];
  const closed: string[] = [];
  const s = new PreviewScheduler(
    (c) => opened.push(c),
    (c) => closed.push(c),
    { dwellMs, minOnMs, closeDelayMs },
    () => clock,
    (fn, ms) => {
      const id = nextId++;
      timers.set(id, { at: clock + ms, fn });
      return id;
    },
    (id) => void timers.delete(id),
  );
  const advance = (ms: number) => {
    const target = clock + ms;
    const due = [...timers.entries()].filter(([, t]) => t.at <= target).sort((a, b) => a[1].at - b[1].at);
    for (const [id, t] of due) {
      if (!timers.has(id)) continue;
      timers.delete(id);
      clock = t.at;
      t.fn();
    }
    clock = target;
  };
  return { s, advance, opened, closed };
}

describe("preview debounce", () => {
  it("600 ms saqlanmasa ochilmaydi", () => {
    const h = harness();
    h.s.hover("c1", false);
    h.advance(599);
    expect(h.opened).toEqual([]);
    h.advance(1);
    expect(h.opened).toEqual(["c1"]);
  });

  it("dwell ichida kursor ketsa — ochilmaydi", () => {
    const h = harness();
    h.s.hover("c1", false);
    h.advance(300);
    h.s.leave();
    h.advance(5000);
    expect(h.opened).toEqual([]);
    expect(h.closed).toEqual([]);
  });

  it("kamida 2 s yuradi", () => {
    const h = harness();
    h.s.hover("c1", false);
    h.advance(600); // ochildi (openAt=600)
    h.advance(200); // 800
    h.s.leave(); // closeDelay=1000 → 1800, lekin minOnMs → 2600
    h.advance(1799); // 2599
    expect(h.closed).toEqual([]);
    h.advance(1); // 2600
    expect(h.closed).toEqual(["c1"]);
  });

  it("minOnMs o'tgan bo'lsa 1 s kutib yopiladi", () => {
    const h = harness();
    h.s.hover("c1", false);
    h.advance(600 + 5000);
    h.s.leave();
    h.advance(999);
    expect(h.closed).toEqual([]);
    h.advance(1);
    expect(h.closed).toEqual(["c1"]);
  });

  it("grid'da ochiq kamera preview'ni ochmaydi (dedup)", () => {
    const h = harness();
    h.s.hover("c1", true);
    h.advance(5000);
    expect(h.opened).toEqual([]);
  });

  it("boshqa kameraga o'tish: eski yopiladi, yangi dwell'dan keyin ochiladi", () => {
    const h = harness();
    h.s.hover("c1", false);
    h.advance(600);
    h.s.hover("c2", false);
    h.advance(599);
    expect(h.opened).toEqual(["c1"]);
    h.advance(1);
    expect(h.opened).toEqual(["c1", "c2"]);
    expect(h.closed).toEqual(["c1"]); // eski preview darhol yopildi
  });

  it("cancel() darhol yopadi", () => {
    const h = harness();
    h.s.hover("c1", false);
    h.advance(600);
    h.s.cancel();
    expect(h.closed).toEqual(["c1"]);
  });

  it("qayta hover — yana ochiladi (limit xatosidan keyin ham)", () => {
    const h = harness();
    h.s.hover("c1", false);
    h.advance(600);
    h.s.leave();
    h.advance(3000);
    h.s.hover("c1", false);
    h.advance(600);
    expect(h.opened).toEqual(["c1", "c1"]);
  });
});
