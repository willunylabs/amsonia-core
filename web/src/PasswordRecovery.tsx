import { useEffect, useState, type FormEvent } from "react";
import { KeyRound, CheckCircle2, Copy } from "lucide-react";
import { api } from "./api";
import { ErrorMessage } from "./components";
import { friendlyError } from "./errors";
import { useAlive } from "./routing";
import type { PasswordRecovery, PasswordRecoveryPreview } from "./types";
export function MemberPasswordRecovery({ tenant, id, email }: {
    tenant: string;
    id: string;
    email: string;
}) {
    const alive = useAlive();
    const [password, setPassword] = useState("");
    const [result, setResult] = useState<PasswordRecovery>();
    const [busy, setBusy] = useState(false);
    const [error, setError] = useState("");
    const [copied, setCopied] = useState(false);
    async function submit(event: FormEvent) {
        event.preventDefault();
        if (busy)
            return;
        setBusy(true);
        setError("");
        setCopied(false);
        try {
            const recovery = await api.createPasswordRecovery(tenant, id, password);
            if (alive.current) {
                setResult(recovery);
                setPassword("");
            }
        }
        catch (cause) {
            if (alive.current)
                setError(friendlyError(cause));
        }
        finally {
            if (alive.current)
                setBusy(false);
        }
    }
    const link = result ? `${location.origin}${location.pathname}#reset=${encodeURIComponent(result.token)}` : "";
    return <section className="paper-card admin-form form-page">
    <span className="section-icon"><KeyRound size={22}/></span>
    <h2>Help {email} sign in again</h2>
    <p>Create a private, one-use link so this member can choose a new password. It expires after 30 minutes. Completing recovery signs them out on every device, across all organizations.</p>
    <div className="info-note">Share this link directly with the member after verifying their identity. Their membership and roles stay as they are.</div>
    <ErrorMessage message={error}/>
    {result ? <div className="recovery-result">
      <p role="status" className="success-note"><CheckCircle2 size={18}/> Recovery link ready</p>
      <label>Private recovery link<textarea readOnly rows={3} value={link} onFocus={(event) => event.currentTarget.select()}/></label>
      <p>Expires {new Date(result.expires_at).toLocaleString()}. Save it now; it is only shown here once.</p>
      <button className="secondary-button" type="button" onClick={async () => {
                try {
                    await navigator.clipboard.writeText(link);
                    if (alive.current)
                        setCopied(true);
                }
                catch {
                    if (alive.current)
                        setError("Select and copy the link above. Clipboard access is unavailable.");
                }
            }}><Copy size={15}/> {copied ? "Copied" : "Copy private link"}</button>
      <button className="text-button" type="button" onClick={() => { setResult(undefined); setCopied(false); setError(""); }}>Create a replacement link</button>
    </div> : <form className="admin-form unpadded" onSubmit={submit}>
      <label>Your administrator password<input type="password" autoComplete="current-password" value={password} onChange={(e) => setPassword(e.target.value)} maxLength={128} required disabled={busy}/></label>
      <small>Creating a new link invalidates any earlier unused recovery link for this account.</small>
      <div className="form-actions"><button className="primary-button" disabled={busy}>{busy ? "Creating link…" : "Create recovery link"}</button></div>
    </form>}
  </section>;
}
export function PasswordRecoveryLanding({ token }: {
    token: string;
}) {
    const alive = useAlive();
    const [preview, setPreview] = useState<PasswordRecoveryPreview>();
    const [password, setPassword] = useState("");
    const [confirm, setConfirm] = useState("");
    const [busy, setBusy] = useState(false);
    const [error, setError] = useState("");
    const [done, setDone] = useState(false);
    const [attempt, setAttempt] = useState(0);
    useEffect(() => {
        let active = true;
        api.previewPasswordRecovery(token).then((value) => { if (active) {
            setPreview(value);
            setError("");
        } }).catch((cause) => { if (active)
            setError(friendlyError(cause)); });
        return () => { active = false; };
    }, [token, attempt]);
    async function submit(event: FormEvent) {
        event.preventDefault();
        if (busy || !preview)
            return;
        if (password !== confirm) {
            setError("The passwords do not match.");
            return;
        }
        setBusy(true);
        setError("");
        try {
            await api.completePasswordRecovery(token, password);
            if (!alive.current)
                return;
            // Erase the secret without navigating away from the success state.
            history.replaceState(null, "", location.pathname + location.search);
            setPassword("");
            setConfirm("");
            setDone(true);
        }
        catch (cause) {
            if (alive.current)
                setError(friendlyError(cause));
        }
        finally {
            if (alive.current)
                setBusy(false);
        }
    }
    return <main className="invitation-shell recovery-shell"><section className="login-card">
    <div className="brand-lockup"><img className="brand-mark" src="/amsonia-mark-v2.svg" alt=""/><span>AMSONIA CORE</span></div>
    <span className="section-icon">{done ? <CheckCircle2 size={25}/> : <KeyRound size={25}/>}</span>
    <header><span className="folio">Account recovery</span><h1>{done ? "Your password is updated" : "Choose a new password"}</h1><p>{done ? "Your previous sessions have ended. Sign in with your new password to continue." : preview ? `${preview.tenant_name} · ${preview.email}` : "Checking your private recovery link…"}</p></header>
    <ErrorMessage message={error}/>
    {done ? <a className="primary-button" href={location.pathname + location.search}>Back to sign in</a> : preview ? <form className="admin-form unpadded" onSubmit={submit}>
      <input type="text" autoComplete="username" value={preview.email} readOnly hidden/>
      <label>New password<input type="password" autoComplete="new-password" minLength={12} maxLength={128} value={password} onChange={(e) => setPassword(e.target.value)} required disabled={busy}/></label>
      <small>Use 12–128 characters. A password manager can create and save a strong password.</small>
      <label>Confirm new password<input type="password" autoComplete="new-password" minLength={12} maxLength={128} value={confirm} onChange={(e) => setConfirm(e.target.value)} required disabled={busy}/></label>
      <div className="info-note">This link works once, until {new Date(preview.expires_at).toLocaleString()}. Updating your password signs out all your devices.</div>
      <button className="primary-button" disabled={busy}>{busy ? "Updating password…" : "Set new password"}</button>
    </form> : error ? <><p className="fine-print">Expired or used link? Ask your administrator for a new one.</p><button className="secondary-button" onClick={() => setAttempt((value) => value + 1)}>Retry link</button><a className="back-link" href={location.pathname + location.search}>Back to sign in</a></> : <p role="status">Checking link…</p>}
  </section></main>;
}
