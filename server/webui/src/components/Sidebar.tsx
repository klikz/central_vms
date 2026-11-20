// Chap panel: joy sarlavhasi + client bloklari (status nuqta, online/total).
// Blok bosilganda kamera ro'yxati ko'rsatiladi (REJA §16.2).
import { useMemo } from "react";
import type { Store } from "../store";
import { t } from "../lib/i18n";

export function Sidebar({
  store,
  site,
  onSite,
  selectedClient,
  onSelectClient,
}: {
  store: Store;
  site: number | null;
  onSite: (id: number | null) => void;
  selectedClient: number | null;
  onSelectClient: (id: number | null) => void;
}) {
  const list = useMemo(
    () => store.clients.filter((c) => site === null || c.site_id === site),
    [store.clients, site],
  );

  return (
    <aside className="side">
      <div className="side-title">{t("side.sites")}</div>
      <div className="row wrap">
        <button className={site === null ? "chip on" : "chip"} onClick={() => onSite(null)}>
          {t("side.all")}
        </button>
        {store.sites.map((s) => (
          <button key={s.id} className={site === s.id ? "chip on" : "chip"} onClick={() => onSite(s.id)}>
            {s.name}
          </button>
        ))}
      </div>

      <div className="side-title mt">{t("side.clients")}</div>
      {list.map((c) => (
        <div
          key={c.id}
          className={"client-block" + (selectedClient === c.id ? " active" : "")}
          onClick={() => {
            onSelectClient(selectedClient === c.id ? null : c.id);
            if (c.id !== null) void store.loadCameras(c.id);
          }}
        >
          <div className="row">
            <span className={"dot " + (c.status === "online" ? "on" : "off")} />
            <span className="name">{c.name}</span>
          </div>
          <div className="muted">
            {c.status === "online" ? t("side.online") : t("side.offline")} · {c.online_count}/{c.cameras_count}
            {c.version ? ` · ${c.version}` : ""}
          </div>
        </div>
      ))}
      {list.length === 0 && <div className="muted">—</div>}
    </aside>
  );
}
