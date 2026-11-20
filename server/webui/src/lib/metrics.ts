// Yuqori panel uchun jonli ko'rsatkichlar (client konsolidagi statusline).
// REST yuklanganda va WS hodisalarida yangilanadi.
export interface ClientMetrics {
  on: number;
  total: number;
}

const byClient = new Map<number, ClientMetrics>();
const subs = new Set<() => void>();

function emit() {
  subs.forEach((f) => f());
}

export function setClientMetrics(id: number, m: ClientMetrics) {
  byClient.set(id, m);
  emit();
}

export function setAllClients(list: { id: number; online_count: number; cameras_count: number }[]) {
  byClient.clear();
  list.forEach((c) => byClient.set(c.id, { on: c.online_count, total: c.cameras_count }));
  emit();
}

export function totals(): ClientMetrics {
  let on = 0;
  let total = 0;
  byClient.forEach((m) => {
    on += m.on;
    total += m.total;
  });
  return { on, total };
}

export function subscribeMetrics(f: () => void): () => void {
  subs.add(f);
  return () => subs.delete(f);
}
