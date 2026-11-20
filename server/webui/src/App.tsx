import { useEffect, useState } from "react";
import { NavLink, Navigate, Route, Routes } from "react-router-dom";
import { useAuth } from "./auth";
import { t } from "./lib/i18n";
import { LoginPage } from "./pages/Login";
import { MonitorPage } from "./pages/Monitor";
import { AdminPage } from "./pages/Admin";
import { Toasts } from "./components/Toasts";
import { uiChannel } from "./lib/ws";
import { subscribeMetrics, totals } from "./lib/metrics";

const IconCam = () => (
  <svg viewBox="0 0 24 24" aria-hidden="true">
    <path d="M3 7h12l5 4v6H3z" />
    <circle cx="9" cy="13" r="2.5" />
  </svg>
);
const IconShield = () => (
  <svg viewBox="0 0 24 24" aria-hidden="true">
    <path d="M12 3l7 3v6c0 4-3 7-7 9-4-2-7-5-7-9V6z" />
  </svg>
);

// Yuqori paneldagi jonli ko'rsatkichlar (client konsolidagi statusline
// bilan bir xil: chip + dot + mono raqam).
function StatusLine() {
  const [connected, setConnected] = useState(false);
  const [sessions, setSessions] = useState(0);
  const [, force] = useState(0);

  useEffect(() => {
    const offStatus = uiChannel.onStatus(setConnected);
    const offMetrics = subscribeMetrics(() => force((x) => x + 1));
    const off = uiChannel.on((e) => {
      if (e.type === "session_count") setSessions(e.data?.active ?? 0);
    });
    uiChannel.connect();
    return () => {
      off();
      offStatus();
      offMetrics();
    };
  }, []);
  const cams = totals();

  return (
    <div className="statusline" role="status" aria-live="polite">
      <span className="chip">
        <span className={"dot " + (connected ? "on" : "off")} />
        <span>{connected ? t("status.connected") : t("status.disconnected")}</span>
      </span>
      <span className="chip">
        <span className="chip-k">{t("admin.sessions")}</span>
        <span className="mono">{sessions}</span>
      </span>
      <span className="chip">
        <span className="chip-k">{t("side.cameras")}</span>
        <span className="mono">{cams.on}/{cams.total}</span>
      </span>
    </div>
  );
}

function Shell({ children }: { children: React.ReactNode }) {
  const { me, logout } = useAuth();
  const nav = ({ isActive }: { isActive: boolean }) => "nav-item" + (isActive ? " active" : "");
  return (
    <div className="shell">
      <aside className="side">
        <div className="side-brand">
          <div className="brand-mark" aria-hidden="true">
            <svg viewBox="0 0 24 24">
              <path d="M3 7h12l5 4v6H3z" />
              <circle cx="9" cy="13" r="3" />
              <path d="M9 10v6M6 13h6" />
            </svg>
          </div>
          <div className="brand-text">
            <b>CENTRAL VMS</b>
            <span>server console</span>
          </div>
        </div>
        <nav className="side-nav" aria-label="Bo'limlar">
          <p className="nav-group">{t("nav.group.watch")}</p>
          <NavLink className={nav} to="/monitor">
            <IconCam />
            <span>{t("nav.monitor")}</span>
            <kbd>g</kbd>
          </NavLink>
          {me?.role === "admin" && (
            <>
              <p className="nav-group">{t("nav.group.manage")}</p>
              <NavLink className={nav} to="/admin">
                <IconShield />
                <span>{t("nav.admin")}</span>
                <kbd>a</kbd>
              </NavLink>
            </>
          )}
        </nav>
        <div className="side-foot">
          <div className="foot-row">
            <span>{t("side.user")}</span>
            <span className="mono">{me?.username ?? "—"}</span>
          </div>
          <div className="foot-row">
            <span>{t("side.role")}</span>
            <span className="mono">{me?.role ?? "—"}</span>
          </div>
          <div className="foot-row">
            <span>{t("side.hotkeys")}</span>
            <span className="mono">1-4 · ←→ · Esc</span>
          </div>
        </div>
      </aside>

      <header className="topbar">
        <StatusLine />
        <div className="topbar-actions">
          <button className="btn" onClick={() => void logout()}>
            {t("nav.logout")}
          </button>
        </div>
      </header>
      {children}
      <Toasts />
    </div>
  );
}

function Require({ children }: { children: React.ReactNode }) {
  const { me, loading } = useAuth();
  if (loading) return <div className="boot">{t("grid.loading")}</div>;
  if (!me) return <Navigate to="/login" replace />;
  return <Shell>{children}</Shell>;
}

export default function App() {
  const { me, loading } = useAuth();
  return (
    <Routes>
      <Route path="/login" element={loading || me ? <Navigate to="/monitor" replace /> : <LoginPage />} />
      <Route path="/monitor" element={<Require><MonitorPage /></Require>} />
      <Route path="/admin" element={<Require><AdminPage /></Require>} />
      <Route path="*" element={<Navigate to="/monitor" replace />} />
    </Routes>
  );
}
