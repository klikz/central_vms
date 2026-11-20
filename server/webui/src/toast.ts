// Oddiy toast tizimi (limit/xato bildirishnomalari).
import { useEffect, useState } from "react";

export type Toast = { id: number; text: string; kind: "info" | "warn" | "err" };

let seq = 0;
const items: Toast[] = [];
const subs = new Set<(t: Toast[]) => void>();

function emit() {
  subs.forEach((s) => s([...items]));
}

export function toast(text: string, kind: Toast["kind"] = "info", ttlMs = 5000) {
  const it: Toast = { id: ++seq, text, kind };
  items.push(it);
  emit();
  setTimeout(() => {
    const i = items.findIndex((x) => x.id === it.id);
    if (i >= 0) items.splice(i, 1);
    emit();
  }, ttlMs);
}

export function useToasts(): Toast[] {
  const [list, setList] = useState<Toast[]>([]);
  useEffect(() => {
    subs.add(setList);
    setList([...items]);
    return () => {
      subs.delete(setList);
    };
  }, []);
  return list;
}
