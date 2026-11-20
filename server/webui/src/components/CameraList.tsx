// Kamera ro'yxati: status nuqta, nom, kodek badge, ⭐ sevimli; katakka
// sudrash yoki bosish orqali qo'shish (client konsoli .badge/.dot uslubi).
import { useMemo } from "react";
import type { Camera } from "../lib/api";
import type { Store } from "../store";
import { t } from "../lib/i18n";

export function CameraList({
  store,
  cameras,
  onAdd,
  onHover,
}: {
  store: Store;
  cameras: Camera[];
  onAdd: (cam: Camera) => void;
  onHover?: (cam: Camera, inside: boolean) => void;
}) {
  const sorted = useMemo(
    () =>
      [...cameras].sort((a, b) => {
        // Onlayn kameralar tepada: oflaynlarni gridda ochish xato beradi,
        // shuning uchun foydalanuvchi avval ishlaydiganlarini ko'radi.
        const oa = a.status === "online" ? 0 : 1;
        const ob = b.status === "online" ? 0 : 1;
        const fa = store.isFavorite(a.id) ? 0 : 1;
        const fb = store.isFavorite(b.id) ? 0 : 1;
        return oa - ob || fa - fb || a.position - b.position || a.name.localeCompare(b.name);
      }),
    [cameras, store],
  );

  if (sorted.length === 0) return <div className="cam-list"><span className="muted">—</span></div>;

  return (
    <div className="cam-list">
      {sorted.map((cam) => (
        <div
          key={cam.id}
          className="cam-item"
          draggable
          onDragStart={(e) => {
            e.dataTransfer.setData("text/camera", cam.id);
            e.dataTransfer.effectAllowed = "copy";
          }}
          onClick={() => onAdd(cam)}
          onMouseEnter={() => onHover?.(cam, true)}
          onMouseLeave={() => onHover?.(cam, false)}
          title={cam.last_error ? `${cam.name} — ${cam.last_error}` : cam.name}
        >
          <span
            className={
              "dot " + (cam.status === "online" ? "on" : cam.status === "error" ? "dot-err off" : "off")
            }
          />
          <span className="cam-name">{cam.name}</span>
          {cam.status !== "online" && cam.last_error && (
            <span className="cam-err" title={cam.last_error}>{cam.last_error}</span>
          )}
          <span className={"badge " + (cam.codec_main === "H265" ? "h265" : "h264")}>{cam.codec_main}</span>
          {cam.transcode_mode === "server" && <span className="badge tc">TC</span>}
          {!cam.enabled && <span className="badge badge-err">{t("grid.disabled")}</span>}
          <span className="spacer" />
          <button
            className={"star" + (store.isFavorite(cam.id) ? " on" : "")}
            onClick={(e) => {
              e.stopPropagation();
              void store.toggleFavorite(cam.id);
            }}
            title={store.isFavorite(cam.id) ? t("fav.remove") : t("fav.add")}
          >
            ★
          </button>
        </div>
      ))}
    </div>
  );
}
