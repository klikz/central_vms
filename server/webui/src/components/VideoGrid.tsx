// Video grid: N×N katak + IntersectionObserver (ko'rinmayotgan katakda
// oqim bo'lmasligi kerak — REJA §16.2).
import { useEffect, useRef, useState } from "react";
import { Cell } from "./Cell";
import { gridCells } from "../lib/limits";

export function VideoGrid({
  n,
  slots,
  active,
  names,
  tcSet,
  onAssign,
  onSwap,
  onClear,
  onFullscreen,
}: {
  n: number;
  // active=false (tab yashirin) — barcha kataklar to'xtatiladi
  // (IntersectionObserver bilan birga, REJA §16.2).
  active: boolean;
  slots: (string | null)[];
  names: Record<string, string>;
  tcSet: Record<string, boolean>;
  onAssign: (i: number, camId: string) => void;
  onSwap: (a: number, b: number) => void;
  onClear: (i: number) => void;
  onFullscreen: (i: number) => void;
}) {
  const ref = useRef<HTMLDivElement>(null);
  const [visible, setVisible] = useState<Set<number>>(new Set());

  useEffect(() => {
    const root = ref.current;
    if (!root) return;
    const io = new IntersectionObserver(
      (entries) => {
        setVisible((prev) => {
          const next = new Set(prev);
          for (const en of entries) {
            const i = Number((en.target as HTMLElement).dataset.idx);
            if (Number.isNaN(i)) continue;
            if (en.isIntersecting) next.add(i);
            else next.delete(i);
          }
          return next;
        });
      },
      { threshold: 0.25 },
    );
    root.querySelectorAll<HTMLElement>("[data-idx]").forEach((el) => io.observe(el));
    return () => io.disconnect();
  }, [n, slots.length]);

  return (
    <div className="grid" ref={ref} style={{ gridTemplateColumns: `repeat(${n}, 1fr)` }}>
      {Array.from({ length: gridCells(n) }, (_, i) => {
        const camId = slots[i] ?? null;
        return (
          <Cell
              key={i}
              index={i}
              cameraId={camId}
              cameraName={camId ? names[camId] : undefined}
              transcoded={camId ? tcSet[camId] : false}
              visible={active && visible.has(i)}
              onAssign={onAssign}
              onSwap={onSwap}
              onClear={onClear}
              onFullscreen={onFullscreen}
          />
        );
      })}
    </div>
  );
}
