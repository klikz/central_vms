import React, { createContext, useCallback, useContext, useEffect, useState } from "react";
import { api, tryRefresh, setAccess, type Me } from "./lib/api";
import { releaseAllKeepalive } from "./lib/player";

interface AuthState {
  me: Me | null;
  loading: boolean;
  login: (u: string, p: string) => Promise<void>;
  logout: () => Promise<void>;
  refreshMe: () => Promise<void>;
}

const Ctx = createContext<AuthState | null>(null);

export function AuthProvider({ children }: { children: React.ReactNode }) {
  const [me, setMe] = useState<Me | null>(null);
  const [loading, setLoading] = useState(true);

  const refreshMe = useCallback(async () => {
    try {
      setMe(await api.me());
    } catch {
      setMe(null);
    }
  }, []);

  // Sahifa yopilsa/qayta yuklansa — barcha sessiyalar yopilsin
  // (aks holda "arvoh" sessiyalar limitni 5 daqiqa band qiladi).
  useEffect(() => {
    const h = () => releaseAllKeepalive();
    window.addEventListener("pagehide", h);
    return () => window.removeEventListener("pagehide", h);
  }, []);

  // Sahifa ochilganda: refresh cookie bilan avtomatik kirish (REJA §9).
  useEffect(() => {
    (async () => {
      if (await tryRefresh()) await refreshMe();
      setLoading(false);
    })();
  }, [refreshMe]);

  const login = useCallback(
    async (u: string, p: string) => {
      await api.login(u, p);
      await refreshMe();
    },
    [refreshMe],
  );

  const logout = useCallback(async () => {
    await api.logout().catch(() => undefined);
    setAccess("");
    setMe(null);
  }, []);

  return <Ctx.Provider value={{ me, loading, login, logout, refreshMe }}>{children}</Ctx.Provider>;
}

export function useAuth(): AuthState {
  const v = useContext(Ctx);
  if (!v) throw new Error("AuthProvider yo'q");
  return v;
}
