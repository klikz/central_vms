import { useState } from "react";
import { useAuth } from "../auth";
import { ApiError } from "../lib/api";
import { t } from "../lib/i18n";

export function LoginPage() {
  const { login } = useAuth();
  const [user, setUser] = useState("");
  const [pass, setPass] = useState("");
  const [err, setErr] = useState("");
  const [busy, setBusy] = useState(false);

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setErr("");
    try {
      await login(user, pass);
    } catch (e2) {
      const ae = e2 as ApiError;
      if (ae.kind === "bad_credentials") setErr(t("login.bad_credentials"));
      else if (ae.kind === "rate_limited") setErr(t("login.rate_limited"));
      else if (ae.status === 0 || ae.kind === "error" && !ae.detail) setErr(t("login.network"));
      else setErr(ae.message);
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="login">
      <div className="login-head">
        <span className="brand big">{t("app.title")}</span>
        <span className="muted">v1.0</span>
      </div>
      <form className="card" onSubmit={submit}>
        <label>
          <span className="muted">{t("login.user")}</span>
          <input value={user} autoFocus autoComplete="username" onChange={(e) => setUser(e.target.value)} />
        </label>
        <label>
          <span className="muted">{t("login.pass")}</span>
          <input type="password" value={pass} autoComplete="current-password" onChange={(e) => setPass(e.target.value)} />
        </label>
        {err && <div className="errline">{err}</div>}
        <button className="primary" disabled={busy || !user || !pass}>
          {busy ? t("grid.loading") : t("login.submit")}
        </button>
      </form>
    </div>
  );
}
