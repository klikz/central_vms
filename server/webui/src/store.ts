// UI ma'lumot do'koni: REST'dan boshlang'ich holat + /api/ui/ws push
// bilan jonli yangilanish. React state'ida saqlanadi (yagona manba).
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { api, type Camera, type Client, type Site } from "./lib/api";
import { uiChannel, type UIEvent } from "./lib/ws";
import { setAllClients, setClientMetrics } from "./lib/metrics";

export interface Store {
  sites: Site[];
  clients: Client[];
  cameras: Record<number, Camera[]>; // client_id → kameralar
  favorites: string[];
  connected: boolean;
  active: number; // faol sessiyalar (session_count)
  reload: () => Promise<void>;
  loadCameras: (clientId: number) => Promise<void>;
  toggleFavorite: (cameraId: string) => Promise<void>;
  isFavorite: (cameraId: string) => boolean;
}

export function useStore(): Store {
  const [sites, setSites] = useState<Site[]>([]);
  const [clients, setClients] = useState<Client[]>([]);
  const [cameras, setCameras] = useState<Record<number, Camera[]>>({});
  const [favorites, setFavorites] = useState<string[]>([]);
  const [connected, setConnected] = useState(false);
  const [active, setActive] = useState(0);
  const camsRef = useRef(cameras);
  camsRef.current = cameras;

  const reload = useCallback(async () => {
    const [s, c, f] = await Promise.all([api.sites(), api.clients(), api.favorites()]);
    setSites(s);
    setClients(c);
    setFavorites(f);
    setAllClients(c);
    // Ko'rinadigan clientlarning kameralarini yuklaymiz.
    const entries = await Promise.all(c.slice(0, 8).map(async (cl) => [cl.id, await api.cameras(cl.id)] as const));
    setCameras(Object.fromEntries(entries));
  }, []);

  const loadCameras = useCallback(async (clientId: number) => {
    const list = await api.cameras(clientId);
    setCameras((m) => ({ ...m, [clientId]: list }));
  }, []);

  const toggleFavorite = useCallback(async (cameraId: string) => {
    const has = favorites.includes(cameraId);
    if (has) await api.delFavorite(cameraId);
    else await api.addFavorite(cameraId);
    setFavorites(await api.favorites());
  }, [favorites]);

  useEffect(() => {
    void reload();
    const off = uiChannel.on((e: UIEvent) => {
      if (e.type === "client_status") {
        if (typeof e.data?.client_id === "number") {
          const cid = e.data.client_id;
          setClientMetrics(cid, {
            on: e.data.online_count ?? 0,
            total: e.data.cameras_count ?? 0,
          });
          // Agent oflayn bo'lsa uning kameralari ham oflayn (DB'da ham
          // shunday qilinadi — UI eskicha yashil ko'rsatmasin).
          if (e.data.status === "offline") {
            setCameras((m) => {
              const list = m[cid];
              if (!list) return m;
              return { ...m, [cid]: list.map((c) => ({ ...c, status: "offline" as const })) };
            });
          }
        }
        setClients((list) => {
          const found = list.some((c) => c.id === e.data.client_id);
          if (!found) {
            void reload();
            return list;
          }
          return list.map((c) =>
            c.id === e.data.client_id
              ? {
                  ...c,
                  status: e.data.status ?? c.status,
                  // Agent `hello.name` orqali nomini yuboradi — darhol ko'rsatamiz.
                  name: e.data.name ?? c.name,
                  version: e.data.version ?? c.version,
                  cameras_count: e.data.cameras_count ?? c.cameras_count,
                  online_count: e.data.online_count ?? c.online_count,
                }
              : c,
          );
        });
      } else if (e.type === "cam_status") {
        setCameras((m) => {
          const list = m[e.data.client_id];
          if (!list) return m;
          return {
            ...m,
            [e.data.client_id]: list.map((cam) =>
              cam.id === e.data.camera_id ? { ...cam, status: e.data.status, rtsp_masked: cam.rtsp_masked } : cam,
            ),
          };
        });
      } else if (e.type === "session_count") {
        setActive(e.data.active ?? 0);
      }
    });
    const offStatus = uiChannel.onStatus(setConnected);
    uiChannel.connect();
    return () => {
      off();
      offStatus();
    };
  }, [reload]);

  return useMemo(
    () => ({
      sites,
      clients,
      cameras,
      favorites,
      connected,
      active,
      reload,
      loadCameras,
      toggleFavorite,
      isFavorite: (id: string) => favorites.includes(id),
    }),
    [sites, clients, cameras, favorites, connected, active, reload, loadCameras, toggleFavorite],
  );
}
