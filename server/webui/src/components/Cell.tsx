// Grid kataki: video + holat qatlami (spinner/"transcode boshlandi…"/
// xato + qayta urinish), drag&drop, ikki marta bosish → fullscreen.
import { useEffect, useRef, useState } from "react";
import { attach, release } from "../lib/player";
import { t } from "../lib/i18n";

export type CellProps = {
  index: number;
  cameraId: string | null;
  cameraName?: string;
  transcoded?: boolean;
  visible: boolean;
  kind?: "main" | "preview";
  onAssign: (index: number, cameraId: string) => void;
  onSwap: (a: number, b: number) => void;
  onClear: (index: number) => void;
  onFullscreen: (index: number) => void;
};

export function Cell(p: CellProps) {
  const videoRef = useRef<HTMLVideoElement>(null);
  const [state, setState] = useState<"idle" | "connecting" | "live" | "error">("idle");
  const [attempt, setAttempt] = useState(0);

  useEffect(() => {
    const el = videoRef.current;
    const camId = p.cameraId;
    if (!el) return;
    if (!camId || !p.visible) {
      setState("idle");
      if (camId && !p.visible) void release(camId, p.kind ? { kind: p.kind } : undefined, el);
      return;
    }
    let alive = true;
    setState("connecting");
    void attach(camId, p.kind ? { kind: p.kind } : undefined, el, (e) => {
      if (alive) setState(e.state === "live" ? "live" : e.state === "error" ? "error" : "connecting");
    }).catch(() => alive && setState("error"));
    return () => {
      alive = false;
      void release(camId, p.kind ? { kind: p.kind } : undefined, el);
    };
  }, [p.cameraId, p.visible, attempt]);

  return (
    <div
      data-idx={p.index}
      className={"cell" + (state === "live" ? " live" : "")}
      onDragOver={(e) => {
        e.preventDefault();
        e.currentTarget.classList.add("dragover");
      }}
      onDragLeave={(e) => e.currentTarget.classList.remove("dragover")}
      onDrop={(e) => {
        e.preventDefault();
        e.currentTarget.classList.remove("dragover");
        const cam = e.dataTransfer.getData("text/camera");
        const from = e.dataTransfer.getData("text/cell");
        if (from) p.onSwap(Number(from), p.index);
        else if (cam) p.onAssign(p.index, cam);
      }}
      onDoubleClick={() => p.onFullscreen(p.index)}
    >
      <video ref={videoRef} autoPlay playsInline muted />
      {p.cameraId && (
        <>
          <span className="label">{p.cameraName ?? p.cameraId.slice(0, 8)}</span>
          <span className="fav" onClick={() => p.onClear(p.index)} title="✕">
            ✕
          </span>
          {p.transcoded && <span className="badge tc">TC</span>}
        </>
      )}
      {state !== "live" && (
        <div className="overlay">
          {state === "connecting" && (
            <>
              <div className="spinner" />
              <span>{p.transcoded ? t("grid.transcoding") : t("grid.loading")}</span>
            </>
          )}
          {state === "error" && (
            <>
              <span className="errline">{t("grid.error")}</span>
              <button onClick={() => setAttempt((a) => a + 1)}>{t("grid.retry")}</button>
            </>
          )}
          {state === "idle" && !p.cameraId && <span className="muted">{t("grid.empty")}</span>}
        </div>
      )}
    </div>
  );
}
