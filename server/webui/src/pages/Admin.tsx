// Boshqaruv sahifasi: kompyuterlar (ulash kodi, maxfiy kod almashtirish,
// o'chirish), sessiyalar, foydalanuvchilar, jurnal.
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
    <main className="main wide">
      <div className="card">
        <div className="row">
          <b>{t("admin.clients")}</b>
          <span className="spacer" />
          <button
            onClick={async () => {
              const r = await api.admin.enrollments();
              setCode(r.code);
              toast(t("admin.enroll.note"), "warn", 8000);
            }}
          >
            {t("admin.enroll")}
          </button>
        </div>
        {code && (
          <div className="code">
            <span className="muted">{t("admin.enroll")}</span> <code>{code}</code>
          </div>
        )}
        <table>
          <thead>
            <tr>
              <th>{t("side.clients")}</th>
              <th>agent_id</th>
              <th>status</th>
              <th>version</th>
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
                  <span className={"dot " + (c.status === "online" ? "on" : "off")} />
                  {c.status}
                </td>
                <td>{c.version ?? "—"}</td>
                <td className="mono">{c.lan_ip ?? "—"}</td>
                <td className="row">
                  <button
                    onClick={async () => {
                      const r = await api.admin.rotateSecret(c.id);
                      toast(`agent_secret: ${r.agent_secret}`, "warn", 15000);
                    }}
                  >
                    {t("admin.rotate")}
                  </button>
                  <button
                    onClick={async () => {
                      await api.admin.deleteClient(c.id);
                      toast(t("toast.deleted"));
                      void load();
                    }}
                  >
                    {t("admin.delete")}
                  </button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      <div className="card">
        <b>{t("admin.sessions")}</b>
        <table>
          <thead>
            <tr>
              <th>id</th>
              <th>camera</th>
              <th>holat</th>
              <th>muddat</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            {sessions.map((s) => (
              <tr key={s.id}>
                <td className="mono">{s.id.slice(0, 12)}</td>
                <td className="mono">{s.camera_id.slice(0, 12)}</td>
                <td>{s.state}</td>
                <td className="mono">{s.expires_at}</td>
                <td>
                  <button
                    onClick={async () => {
                      await api.stopSession(s.id);
                      void load();
                    }}
                  >
                    ✕
                  </button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      {users.length > 0 && (
        <div className="card">
          <b>{t("admin.users")}</b>
          <table>
            <thead>
              <tr>
                <th>login</th>
                <th>rol</th>
              </tr>
            </thead>
            <tbody>
              {users.map((u) => (
                <tr key={u.id}>
                  <td>{u.username}</td>
                  <td>{u.role}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </main>
  );
}
