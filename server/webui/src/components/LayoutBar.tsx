// Layoutlar paneli: saqlash / yuklash / standart qilish / o'chirish.
// Slotlar serverda JSON satr sifatida saqlanadi (registry.Layout.SlotsJSON).
import { useEffect, useState } from "react";
import { api } from "../lib/api";
import { parseLayoutSlots, type Slot } from "../lib/slots";
import { t } from "../lib/i18n";
import { toast } from "../toast";

export interface LayoutRow {
  id: number;
  name: string;
  grid: number;
  slots_json: string;
  is_default: boolean;
}

export function LayoutBar({
  grid,
  slots,
  onApply,
}: {
  grid: number;
  slots: Slot[];
  onApply: (grid: number, slots: Slot[]) => void;
}) {
  const [rows, setRows] = useState<LayoutRow[]>([]);
  const [sel, setSel] = useState(0);
  const [name, setName] = useState("");

  const load = () => api.layouts().then((r) => setRows(r as LayoutRow[])).catch(() => undefined);
  useEffect(() => {
    void load();
  }, []);

  // Birinchi kirishda standart layout avtomatik qo'llaniladi (REJA §11).
  useEffect(() => {
    const d = rows.find((r) => r.is_default);
    if (!d) return;
    let parsed: unknown = [];
    try {
      parsed = JSON.parse(d.slots_json);
    } catch {
      /* buzilgan JSON */
    }
    onApply(d.grid, parseLayoutSlots(parsed, d.grid * d.grid));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [rows.length]);

  const apply = (r: LayoutRow) => {
    let parsed: unknown = [];
    try {
      parsed = JSON.parse(r.slots_json);
    } catch {
      /* buzilgan JSON */
    }
    onApply(r.grid, parseLayoutSlots(parsed, r.grid * r.grid));
    setSel(r.id);
  };

  return (
    <div className="row">
      <span className="chip-k">{t("layout.title")}</span>
      <select value={sel} onChange={(e) => {
        const r = rows.find((x) => x.id === Number(e.target.value));
        if (r) apply(r);
      }}>
        <option value={0}>—</option>
        {rows.map((r) => (
          <option key={r.id} value={r.id}>
            {r.name} ({r.grid}×{r.grid}){r.is_default ? " ★" : ""}
          </option>
        ))}
      </select>
      <input className="filter" placeholder={t("layout.name")} value={name} onChange={(e) => setName(e.target.value)} />
      <button
        onClick={async () => {
          if (!name) return;
          await api.createLayout(name, grid, slots);
          toast(t("toast.saved"));
          setName("");
          void load();
        }}
      >
        {t("layout.save")}
      </button>
      {sel > 0 && (
        <>
          <button className="btn"
            onClick={async () => {
              await api.setDefaultLayout(sel);
              toast(t("layout.default") + " — " + t("toast.saved"));
              void load();
            }}
          >
            {t("layout.default")}
          </button>
          <button className="btn btn-danger"
            onClick={async () => {
              await api.deleteLayout(sel);
              setSel(0);
              toast(t("toast.deleted"));
              void load();
            }}
          >
            {t("admin.delete")}
          </button>
        </>
      )}
    </div>
  );
}
