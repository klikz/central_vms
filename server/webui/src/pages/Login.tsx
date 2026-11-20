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
      else setErr(ae.message || t("login.network"));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="login">
      <div className="brandline">
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
      <form className="modal-box" onSubmit={submit}>
        <h2>{t("login.title")}</h2>
        <label className="field">
          <span>{t("login.user")}</span>
          <input value={user} autoFocus autoComplete="username" onChange={(e) => setUser(e.target.value)} />
        </label>
        <label className="field">
          <span>{t("login.pass")}</span>
          <input type="password" value={pass} autoComplete="current-password" onChange={(e) => setPass(e.target.value)} />
        </label>
        {err && <div className="banner banner-err">{err}</div>}
        <button className="btn btn-primary" disabled={busy || !user || !pass}>
          {busy ? t("grid.loading") : t("login.submit")}
        </button>
      </form>
    </div>
  );
}
