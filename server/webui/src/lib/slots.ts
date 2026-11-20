// Grid slotlari sof mantiqi (testlanadi): avtomatik to'ldirish, joylashtirish,
// almashtirish, tozalash, sahifalash, layout qo'llash.
export type Slot = string | null;

export function padSlots(slots: Slot[], size: number): Slot[] {
  const out = slots.slice(0, size);
  while (out.length < size) out.push(null);
  return out;
}

/** Joy/client tanlanganda kataklarn `position` bo'yicha to'ldirish. */
export function autoFill(camIds: string[], page: number, perPage: number): Slot[] {
  const start = page * perPage;
  return padSlots(camIds.slice(start, start + perPage), perPage);
}

export function assign(slots: Slot[], i: number, camId: string, size: number): Slot[] {
  const out = padSlots(slots, size);
  out[i] = camId;
  return out;
}

export function swap(slots: Slot[], a: number, b: number, size: number): Slot[] {
  if (a < 0 || b < 0 || a >= size || b >= size) return slots; // xavfsiz: chegaradan tashqari
  const out = padSlots(slots, size);
  const t = out[a];
  out[a] = out[b];
  out[b] = t;
  return out;
}

export function clear(slots: Slot[], i: number, size: number): Slot[] {
  const out = padSlots(slots, size);
  out[i] = null;
  return out;
}

export function firstEmpty(slots: Slot[]): number {
  return slots.findIndex((s) => !s);
}

/** Bir kamerani qo'shish: allaqachon bo'lsa — joyini o'zgartirmaymiz. */
export function addCamera(slots: Slot[], camId: string, size: number): Slot[] {
  if (slots.includes(camId)) return slots;
  const i = firstEmpty(slots);
  if (i < 0) return slots; // kataklar to'la — jim rad
  return assign(slots, i, camId, size);
}

export function pageCount(total: number, perPage: number): number {
  return Math.max(1, Math.ceil(total / perPage));
}

/** Layout'dagi slotlar (server JSONi) — noto'g'ri elementlar tashlanadi. */
export function parseLayoutSlots(raw: unknown, size: number): Slot[] {
  const list = Array.isArray(raw) ? raw : [];
  const clean = list.map((x) => (typeof x === "string" && x ? x : null));
  return padSlots(clean, size);
}
