// Kuzatuv sahifasi: chap panel + grid (avtomatik to'ldirish, pagination,
// drag&drop, klaviatura, fullscreen, Page Visibility).
import { useCallback, useEffect, useMemo, useState } from "react";
import { Sidebar } from "../components/Sidebar";
import { CameraList } from "../components/CameraList";
import { VideoGrid } from "../components/VideoGrid";
import { Cell } from "../components/Cell";
import { LayoutBar } from "../components/LayoutBar";
import { useStore } from "../store";
import { PreviewScheduler } from "../lib/preview";
import { DEFAULT_GRID, GRIDS, gridCells, keyAction } from "../lib/limits";
import { addCamera, assign as slotAssign, autoFill, clear as slotClear, pageCount, swap as slotSwap, type Slot } from "../lib/slots";
import { t } from "../lib/i18n";
import type { Camera } from "../lib/api";

export function MonitorPage() {
  const store = useStore();
  const [n, setN] = useState<number>(DEFAULT_GRID);
  const [page, setPage] = useState(0);
  const [site, setSite] = useState<number | null>(null);
  const [client, setClient] = useState<number | null>(null);
  const [manual, setManual] = useState<(string | null)[] | null>(null);
  const [full, setFull] = useState<number | null>(null);
  const [previewCam, setPreviewCam] = useState<string | null>(null);
  const scheduler = useMemo(
    () => new PreviewScheduler((c) => setPreviewCam(c), () => setPreviewCam(null)),
    [],
  );
  useEffect(() => () => scheduler.cancel(), [scheduler]);
  const [docVisible, setDocVisible] = useState(!document.hidden);

  const cams: Camera[] = useMemo(() => {
    const ids = client !== null ? [client] : store.clients.filter((c) => site === null || c.site_id === site).map((c) => c.id);
    return ids.flatMap((id) => store.cameras[id] ?? []).filter((c) => c.enabled);
  }, [store.clients, store.cameras, client, site]);

  // Ro'yxat va grid bir xil tartibda: onlayn kameralar tepada
  // (oflaynni ochib bo'lmaydi — foydalanuvchi avval ishlaydiganlarini ko'radi).
  const sorted = useMemo(
    () =>
      [...cams].sort((a, b) => {
        const oa = a.status === "online" ? 0 : 1;
        const ob = b.status === "online" ? 0 : 1;
        const fa = store.isFavorite(a.id) ? 0 : 1;
        const fb = store.isFavorite(b.id) ? 0 : 1;
        return oa - ob || fa - fb || a.position - b.position || a.name.localeCompare(b.name);
      }),
    [cams, store],
  );
  const names = useMemo(() => Object.fromEntries(cams.map((c) => [c.id, c.name])), [cams]);
  const statuses = useMemo(() => Object.fromEntries(cams.map((c) => [c.id, c.status])), [cams]);
  const perPage = gridCells(n);
  const pages = pageCount(cams.length, perPage);
  // Avto-to'ldirish FAQAT onlayn kameralar bilan: oflaynni ochib bo'lmaydi
  // (409) va katakda xato holati ko'rinadi; ular ro'yxatda qoladi va
  // istalganda qo'l bilan (drag) qo'yish mumkin.
  const auto = useMemo(
    () => autoFill(sorted.filter((c) => c.status === "online").map((c) => c.id), page, perPage),
    [sorted, page, perPage],
  );
  const slots: Slot[] = manual ?? auto;

  // Joy/client/sahifa o'zgarsa — qo'yilgan tartib bekor qilinadi va eski
  // oqimlar darhol to'xtaydi (kataklar unmount bo'ladi).
  useEffect(() => setManual(null), [page, n, client, site, cams.length]);
  useEffect(() => scheduler.cancel(), [page, n, client, site, scheduler]);
  useEffect(() => {
    if (page >= pages) setPage(0);
  }, [page, pages]);

  useEffect(() => {
    const on = () => setDocVisible(!document.hidden);
    document.addEventListener("visibilitychange", on);
    return () => document.removeEventListener("visibilitychange", on);
  }, []);

  const assign = useCallback((i: number, camId: string) => setManual((p) => slotAssign(p ?? auto, i, camId, perPage)), [auto, perPage]);
  const swap = useCallback((a: number, b: number) => setManual((p) => slotSwap(p ?? auto, a, b, perPage)), [auto, perPage]);
  const clear = useCallback((i: number) => setManual((p) => slotClear(p ?? auto, i, perPage)), [auto, perPage]);
  const addFirstEmpty = useCallback((cam: Camera) => setManual((p) => addCamera(p ?? auto, cam.id, perPage)), [auto, perPage]);
  const applyLayout = useCallback((g: number, sl: Slot[]) => {
    setN(g);
    setManual(sl);
    setPage(0);
  }, []);

  useEffect(() => {
    const h = (e: KeyboardEvent) => {
      const a = keyAction(e);
      if (!a) return;
      if (a.kind === "grid" && GRIDS.includes(a.n as 1)) setN(a.n);
      else if (a.kind === "page") setPage((p) => Math.min(pages - 1, Math.max(0, p + a.delta)));
      else if (a.kind === "esc") setFull(null);
    };
    window.addEventListener("keydown", h);
    return () => window.removeEventListener("keydown", h);
  }, [pages]);

  return (
    <main>
      <div className="screen-head">
        <h1>{t("monitor.title")}</h1>
        <div className="actions">
          {GRIDS.map((g) => (
            <button key={g} className={"chip" + (g === n ? " on" : "")} onClick={() => setN(g)}>
              {g}×{g}
            </button>
          ))}
          <span className="chip">
            <span className="chip-k">{t("monitor.page")}</span>
            <span className="mono">{page + 1}/{pages}</span>
          </span>
          <button className="btn" onClick={() => setPage((p) => Math.max(0, p - 1))}>←</button>
          <button className="btn" onClick={() => setPage((p) => Math.min(pages - 1, p + 1))}>→</button>
          <LayoutBar grid={n} slots={slots} onApply={applyLayout} />
        </div>
      </div>

      <div className="cards">
        <div className={"card" + (store.active ? " ok" : "")}>
          <b>{store.active}</b>
          <span>{t("admin.sessions")}</span>
        </div>
        <div className="card">
          <b>{cams.filter((c) => c.status === "online").length}/{cams.length}</b>
          <span>{t("side.cameras")}</span>
        </div>
        <div className="card">
          <b>{store.clients.filter((c) => c.status === "online").length}/{store.clients.length}</b>
          <span>{t("side.clients")}</span>
        </div>
        <div className={"card" + (store.connected ? " ok" : " warn")}>
          <b>{store.connected ? "ON" : "OFF"}</b>
          <span>push kanali</span>
        </div>
      </div>

      <div className="monitor">
        <Sidebar store={store} site={site} onSite={setSite} selectedClient={client} onSelectClient={setClient} />
        <div className="panel-main">
          <CameraList
            store={store}
            cameras={cams}
            onAdd={addFirstEmpty}
            onHover={(cam, inside) => (inside ? scheduler.hover(cam.id, slots.includes(cam.id)) : scheduler.leave())}
          />
          <VideoGrid
            n={n}
            active={docVisible}
            slots={slots}
            names={names}
            statuses={statuses}
            tcSet={{}}
            onAssign={assign}
            onSwap={swap}
            onClear={clear}
            onFullscreen={setFull}
          />
        </div>
      </div>

      {previewCam && (
        <div className="preview-bar">
          <span className="chip-k">preview</span>
          <Cell
            index={-1}
            kind="preview"
            cameraId={previewCam}
            cameraName={names[previewCam]}
            visible={docVisible}
            onAssign={() => undefined}
            onSwap={() => undefined}
            onClear={() => scheduler.cancel()}
            onFullscreen={() => setFull(null)}
          />
        </div>
      )}

      {full !== null && slots[full] && (
        <div className="fullscreen" onClick={() => setFull(null)}>
          <Cell
            index={full}
            cameraId={slots[full]}
            cameraName={names[slots[full] as string]}
            visible={docVisible}
            onAssign={() => undefined}
            onSwap={() => undefined}
            onClear={() => setFull(null)}
            onFullscreen={() => setFull(null)}
          />
        </div>
      )}
    </main>
  );
}
