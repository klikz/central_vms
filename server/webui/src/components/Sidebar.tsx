// Chap panel: joylar filtri + client bloklari (client konsolidagi
// .client-block uslubi: chap aksent chizig'i, mono meta satr).
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
    <div className="panel-side">
      <p className="side-title">{t("side.sites")}</p>
      <div className="row">
        <button className={"chip" + (site === null ? " on" : "")} onClick={() => onSite(null)}>
          {t("side.all")}
        </button>
        {store.sites.map((s) => (
          <button key={s.id} className={"chip" + (site === s.id ? " on" : "")} onClick={() => onSite(s.id)}>
            {s.name}
          </button>
        ))}
      </div>

      <p className="side-title mt">{t("side.clients")}</p>
      {list.map((c) => (
        <div
          key={c.id}
          className={"client-block" + (selectedClient === c.id ? " active" : "")}
          onClick={() => {
            onSelectClient(selectedClient === c.id ? null : c.id);
            void store.loadCameras(c.id);
          }}
        >
          <div className="row" style={{ marginBottom: 2 }}>
            <span className={"dot " + (c.status === "online" ? "on" : "off")} />
            <span className="name">{c.name}</span>
          </div>
          <div className="meta">
            {c.online_count}/{c.cameras_count} · {c.status === "online" ? t("side.online") : t("side.offline")}
            {c.version ? ` · ${c.version}` : ""}
          </div>
        </div>
      ))}
      {list.length === 0 && <div className="muted">—</div>}
    </div>
  );
}
