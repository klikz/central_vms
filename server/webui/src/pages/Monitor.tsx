// Kuzatuv sahifasi: chap panel + grid (avtomatik to'ldirish, pagination,
// drag&drop, klaviatura, fullscreen, Page Visibility).
import { useCallback, useEffect, useMemo, useState } from "react";
import { Sidebar } from "../components/Sidebar";
import { CameraList } from "../components/CameraList";
import { VideoGrid } from "../components/VideoGrid";
import { Cell } from "../components/Cell";
import { useStore } from "../store";
import { PreviewScheduler } from "../lib/preview";
import { DEFAULT_GRID, GRIDS, gridCells, keyAction } from "../lib/limits";
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

  const names = useMemo(() => Object.fromEntries(cams.map((c) => [c.id, c.name])), [cams]);
  const perPage = gridCells(n);
  const pages = Math.max(1, Math.ceil(cams.length / perPage));
  const auto = useMemo(() => {
    const start = page * perPage;
    return cams.slice(start, start + perPage).map((c) => c.id);
  }, [cams, page, perPage]);
  const slots: (string | null)[] = (manual ?? auto).slice(0, perPage);

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

  const assign = useCallback(
    (i: number, camId: string) => {
      setManual((prev) => {
        const arr = [...(prev ?? auto)];
        while (arr.length < perPage) arr.push(null);
        arr[i] = camId;
        return arr;
      });
    },
    [auto, perPage],
  );
  const swap = useCallback(
    (a: number, b: number) => {
      setManual((prev) => {
        const arr = [...(prev ?? auto)];
        while (arr.length < perPage) arr.push(null);
        [arr[a], arr[b]] = [arr[b], arr[a]];
        return arr;
      });
    },
    [auto, perPage],
  );
  const clear = useCallback(
    (i: number) => {
      setManual((prev) => {
        const arr = [...(prev ?? auto)];
        while (arr.length < perPage) arr.push(null);
        arr[i] = null;
        return arr;
      });
    },
    [auto, perPage],
  );
  const addFirstEmpty = useCallback(
    (cam: Camera) => {
      const i = slots.findIndex((s) => !s);
      if (i >= 0) assign(i, cam.id);
    },
    [slots, assign],
  );

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
    <>
      <Sidebar store={store} site={site} onSite={setSite} selectedClient={client} onSelectClient={setClient} />
      <main className="main">
        <div className="toolbar">
          {GRIDS.map((g) => (
            <button key={g} className={g === n ? "chip on" : "chip"} onClick={() => setN(g)}>
              {g}×{g}
            </button>
          ))}
          <span className="muted">
            {page + 1}/{pages}
          </span>
          <button onClick={() => setPage((p) => Math.max(0, p - 1))}>←</button>
          <button onClick={() => setPage((p) => Math.min(pages - 1, p + 1))}>→</button>
          <span className="spacer" />
          <span className="muted">{t("kbd.help")}</span>
          <span className="badge">
            {t("admin.sessions")}: {store.active}
          </span>
          <span className={"dot " + (store.connected ? "on" : "off")} />
        </div>

        <CameraList
          store={store}
          cameras={cams}
          onAdd={addFirstEmpty}
          onHover={(cam, inside) =>
            inside ? scheduler.hover(cam.id, slots.includes(cam.id)) : scheduler.leave()
          }
        />
        {previewCam && (
          <div className="preview-bar">
            <span className="muted">preview</span>
            <Cell
              index={-1}
              kind="preview"
              cameraId={previewCam}
              cameraName={names[previewCam]}
              visible={docVisible}
              onAssign={() => undefined}
              onSwap={() => undefined}
              onClear={() => scheduler.cancel()}
              onFullscreen={() => undefined}
            />
          </div>
        )}
        <VideoGrid
          n={n}
          active={docVisible}
          slots={slots}
          names={names}
          tcSet={{}}
          onAssign={assign}
          onSwap={swap}
          onClear={clear}
          onFullscreen={setFull}
        />
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
    </>
  );
}
