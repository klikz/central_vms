import { NavLink, Navigate, Route, Routes } from "react-router-dom";
import { useAuth } from "./auth";
import { t } from "./lib/i18n";
import { LoginPage } from "./pages/Login";
import { MonitorPage } from "./pages/Monitor";
import { AdminPage } from "./pages/Admin";
import { Toasts } from "./components/Toasts";

function Shell({ children }: { children: React.ReactNode }) {
  const { me, logout } = useAuth();
  return (
    <div className="shell">
      <header className="topbar">
        <span className="brand">{t("app.title")}</span>
        <nav className="nav">
          <NavLink to="/monitor">{t("nav.monitor")}</NavLink>
          {me?.role === "admin" && <NavLink to="/admin">{t("nav.admin")}</NavLink>}
        </nav>
        <span className="spacer" />
        <span className="muted">{me?.display_name || me?.username}</span>
        <button onClick={() => void logout()}>{t("nav.logout")}</button>
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
