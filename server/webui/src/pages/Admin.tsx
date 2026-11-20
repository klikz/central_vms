// Boshqaruv sahifasi: kompyuterlar (ulash kodi, maxfiy kodni almashtirish,
// o'chirish), sessiyalar, foydalanuvchilar. Uslub — client konsoli bilan bir xil.
import { useCallback, useEffect, useState } from "react";
import {
  api,
  type Client,
  type JoinRequest,
  type PreauthIP,
  type SessionRow,
  type User,
} from "../lib/api";
import { t } from "../lib/i18n";
import { toast } from "../toast";

export function AdminPage() {
  const [clients, setClients] = useState<Client[]>([]);
  const [sessions, setSessions] = useState<SessionRow[]>([]);
  const [users, setUsers] = useState<User[]>([]);
  const [joins, setJoins] = useState<JoinRequest[]>([]);
  const [preauth, setPreauth] = useState<PreauthIP[]>([]);
  const [jnames, setJnames] = useState<Record<number, string>>({});
  const [newIP, setNewIP] = useState("");
  const [newIPName, setNewIPName] = useState("");

  const load = useCallback(async () => {
    setClients(await api.clients());
    setSessions(await api.sessions());
    try {
      setUsers(await api.admin.users());
      setJoins((await api.admin.joins()).requests);
      setPreauth((await api.admin.preauth()).items);
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
        </div>
      </div>

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

      {/* §3 (1.0-draft.10): ulanish so'rovlari — mijoz faqat shu
          yerda tasdiqlangandan keyin qo'shiladi (ulanish kodi yo'q). */}
      <p className="side-title mt">{t("admin.joins")}</p>
      <table className="grid">
        <thead>
          <tr>
            <th>{t("admin.j.name")}</th>
            <th>{t("admin.j.host")}</th>
            <th>{t("admin.j.ip")}</th>
            <th>{t("admin.j.os")}</th>
            <th>{t("admin.j.when")}</th>
            <th></th>
          </tr>
        </thead>
        <tbody>
          {joins.map((j) => (
            <tr key={j.id}>
              <td>
                <input
                  className="inp"
                  value={jnames[j.id] ?? j.name ?? ""}
                  placeholder={j.hostname}
                  onChange={(e) => setJnames({ ...jnames, [j.id]: e.target.value })}
                />
              </td>
              <td className="mono">{j.hostname}</td>
              <td className="mono">{j.source_ip || j.lan_ip || "—"}</td>
              <td className="mono">{j.os || "—"}{j.agent_version ? ` · ${j.agent_version}` : ""}</td>
              <td className="mono">{(j.created_at ?? "").replace("T", " ").replace("Z", "")}</td>
              <td>
                <div className="row" style={{ margin: 0, gap: 6 }}>
                  <button
                    className="btn btn-primary"
                    onClick={async () => {
                      try {
                        const r = await api.admin.approveJoin(j.id, jnames[j.id] ?? j.name ?? "");
                        toast(`${t("toast.approved")}: ${r.agent_id}`, "info");
                        void load();
                      } catch (e) {
                        toast(e instanceof Error && e.message.includes("name_taken")
                          ? t("admin.name_taken") : t("admin.approve_failed"), "err");
                      }
                    }}
                  >
                    {t("admin.approve")}
                  </button>
                  <button
                    className="btn"
                    onClick={async () => {
                      await api.admin.rejectJoin(j.id);
                      toast(t("toast.rejected"));
                      void load();
                    }}
                  >
                    {t("admin.reject")}
                  </button>
                  <button
                    className="btn btn-danger"
                    onClick={async () => {
                      await api.admin.deleteJoin(j.id);
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
          {joins.length === 0 && (
            <tr><td colSpan={6} className="empty">{t("admin.empty_joins")}</td></tr>
          )}
        </tbody>
      </table>

      {/* IP bo'yicha oldindan ruxsat: shu manzildan kelgan so'rov
          avtomatik tasdiqlanadi (operator IP'ni shu yerdan yozadi). */}
      <p className="side-title mt">{t("admin.preauth")}</p>
      <div className="row" style={{ margin: "0 0 8px", gap: 6 }}>
        <input className="inp" placeholder="192.168.0.27 / 192.168.0.0/22"
          value={newIP} onChange={(e) => setNewIP(e.target.value)} />
        <input className="inp" placeholder={t("admin.j.name")}
          value={newIPName} onChange={(e) => setNewIPName(e.target.value)} />
        <button
          className="btn btn-primary"
          onClick={async () => {
            if (!newIP.trim()) return;
            try {
              await api.admin.addPreauth(newIP.trim(), newIPName.trim());
              setNewIP(""); setNewIPName("");
              toast(t("toast.preauth_added"));
              void load();
            } catch {
              toast(t("admin.preauth_failed"), "err");
            }
          }}
        >
          {t("admin.add")}
        </button>
      </div>
      <table className="grid">
        <tbody>
          {preauth.map((p) => (
            <tr key={p.id}>
              <td className="mono">{p.ip}</td>
              <td>{p.name || "—"}</td>
              <td className="mono muted">{(p.created_at ?? "").replace("T", " ").replace("Z", "")}</td>
              <td>
                <button
                  className="btn btn-danger"
                  onClick={async () => {
                    await api.admin.deletePreauth(p.id);
                    toast(t("toast.deleted"));
                    void load();
                  }}
                >
                  {t("admin.delete")}
                </button>
              </td>
            </tr>
          ))}
          {preauth.length === 0 && (
            <tr><td colSpan={4} className="empty">{t("admin.empty_preauth")}</td></tr>
          )}
        </tbody>
      </table>

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
