// Boshqaruv sahifasi: kompyuterlar (ulash kodi, maxfiy kodni almashtirish,
// o'chirish), sessiyalar, foydalanuvchilar. Uslub — client konsoli bilan bir xil.
import { useCallback, useEffect, useState } from "react";
import { api, type Client, type SessionRow, type User } from "../lib/api";
import { t } from "../lib/i18n";
import { toast } from "../toast";

export function AdminPage() {
  const [clients, setClients] = useState<Client[]>([]);
  const [sessions, setSessions] = useState<SessionRow[]>([]);
  const [users, setUsers] = useState<User[]>([]);
  const [code, setCode] = useState("");

  const load = useCallback(async () => {
    setClients(await api.clients());
    setSessions(await api.sessions());
    try {
      setUsers(await api.admin.users());
    } catch {
      /* viewer */
    }
  }, []);

  useEffect(() => {
    void load();
    const id = setInterval(() => void load(), 15000);
    return () => clearInterval(id);
  }, [load]);

  return (
    <main>
      <div className="screen-head">
        <h1>{t("nav.admin")}</h1>
        <div className="actions">
          <button className="btn" onClick={() => void load()}>{t("admin.refresh")}</button>
          <button
            className="btn btn-primary"
            onClick={async () => {
              const r = await api.admin.enrollments();
              setCode(r.code);
              toast(t("admin.enroll.note"), "warn", 8000);
            }}
          >
            {t("admin.enroll")}
          </button>
        </div>
      </div>

      {code && (
        <div className="banner banner-warn">
          {t("admin.enroll")}: <span className="code">{code}</span>
          <span className="muted" style={{ marginLeft: 10 }}>{t("admin.enroll.note")}</span>
        </div>
      )}

      <div className="cards">
        <div className="card ok">
          <b>{clients.filter((c) => c.status === "online").length}</b>
          <span>{t("side.online")}</span>
        </div>
        <div className="card">
          <b>{clients.length}</b>
          <span>{t("side.clients")}</span>
        </div>
        <div className="card">
          <b>{sessions.length}</b>
          <span>{t("admin.sessions")}</span>
        </div>
        <div className="card">
          <b>{users.length}</b>
          <span>{t("admin.users")}</span>
        </div>
      </div>

      <p className="side-title">{t("admin.clients")}</p>
      <table className="grid">
        <thead>
          <tr>
            <th>{t("side.clients")}</th>
            <th>agent_id</th>
            <th>{t("admin.status")}</th>
            <th>{t("admin.version")}</th>
            <th>IP</th>
            <th></th>
          </tr>
        </thead>
        <tbody>
          {clients.map((c) => (
            <tr key={c.id}>
              <td>{c.name}</td>
              <td className="mono">{c.agent_id}</td>
              <td>
                <span className={"badge " + (c.status === "online" ? "badge-ok" : "badge-warn")}>{c.status}</span>
              </td>
              <td className="mono">{c.version ?? "—"}</td>
              <td className="mono">{c.lan_ip ?? "—"}</td>
              <td>
                <div className="row" style={{ margin: 0, gap: 6 }}>
                  <button
                    className="btn"
                    onClick={async () => {
                      const r = await api.admin.rotateSecret(c.id);
                      toast(`agent_secret: ${r.agent_secret}`, "warn", 15000);
                    }}
                  >
                    {t("admin.rotate")}
                  </button>
                  <button
                    className="btn btn-danger"
                    onClick={async () => {
                      await api.admin.deleteClient(c.id);
                      toast(t("toast.deleted"));
                      void load();
                    }}
                  >
                    {t("admin.delete")}
                  </button>
                </div>
              </td>
            </tr>
          ))}
          {clients.length === 0 && (
            <tr><td colSpan={6} className="empty">{t("admin.empty_clients")}</td></tr>
          )}
        </tbody>
      </table>

      <p className="side-title mt">{t("admin.sessions")}</p>
      <table className="grid">
        <thead>
          <tr>
            <th>id</th>
            <th>{t("admin.camera")}</th>
            <th>{t("admin.state")}</th>
            <th>{t("admin.expires")}</th>
            <th></th>
          </tr>
        </thead>
        <tbody>
          {sessions.map((x) => (
            <tr key={x.id}>
              <td className="mono">{x.id.slice(0, 12)}</td>
              <td className="mono">{x.camera_id.slice(0, 12)}</td>
              <td>
                <span className={"badge " + (x.state === "playing" ? "badge-ok" : "badge-warn")}>{x.state}</span>
              </td>
              <td className="mono">{x.expires_at}</td>
              <td>
                <button
                  className="btn btn-danger"
                  onClick={async () => {
                    await api.stopSession(x.id);
                    void load();
                  }}
                >
                  {t("admin.stop")}
                </button>
              </td>
            </tr>
          ))}
          {sessions.length === 0 && <tr><td colSpan={5} className="empty">—</td></tr>}
        </tbody>
      </table>

      {users.length > 0 && (
        <>
          <p className="side-title mt">{t("admin.users")}</p>
          <table className="grid">
            <thead>
              <tr>
                <th>{t("login.user")}</th>
                <th>{t("side.role")}</th>
                <th>{t("admin.status")}</th>
              </tr>
            </thead>
            <tbody>
              {users.map((u) => (
                <tr key={u.id}>
                  <td>{u.username}</td>
                  <td><span className="badge">{u.role}</span></td>
                  <td><span className="badge badge-ok">faol</span></td>
                </tr>
              ))}
            </tbody>
          </table>
        </>
      )}
    </main>
  );
}
