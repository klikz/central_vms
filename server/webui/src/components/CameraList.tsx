// Kamera ro'yxati: nom, status, kodek badge, ⭐ sevimli; katakka sudrash
// (HTML5 drag) yoki bosish orqali birinchi bo'sh katakka qo'yish.
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
        const fa = store.isFavorite(a.id) ? 0 : 1;
        const fb = store.isFavorite(b.id) ? 0 : 1;
        return fa - fb || a.position - b.position || a.name.localeCompare(b.name);
      }),
    [cameras, store],
  );

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
          title={cam.name}
        >
          <span className={"dot " + (cam.status === "online" ? "on" : "off")} />
          <span className="cam-name">{cam.name}</span>
          <span className={"badge " + (cam.codec_main === "H265" ? "h265" : "h264")}>{cam.codec_main}</span>
          {cam.transcode_mode === "server" && <span className="badge">TC</span>}
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
      {sorted.length === 0 && <div className="muted">—</div>}
    </div>
  );
}
