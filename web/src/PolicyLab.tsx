import { useEffect, useRef, useState, type FormEvent } from "react";
import { Check, Fingerprint, X } from "lucide-react";
import { api } from "./api";
import { PageHeader, ErrorMessage } from "./components";
import { friendlyError } from "./errors";
import { useAlive } from "./routing";
import type { Account, Permission, Tenant } from "./types";

export function PolicyLab({ tenant, account, permissions }: {
  tenant: Tenant;
  account: Account;
  permissions: Permission[];
}) {
  const alive = useAlive();
  const serial = useRef(0);
  const [selection, setPermission] = useState("");
  const permission = permissions.find(item => item.key === selection)?.key ?? permissions[0]?.key ?? "";
  const question = JSON.stringify([tenant.id, account.id, permission]);
  const [pending, setPending] = useState<string | null>(null);
  const [answer, setAnswer] = useState<{ question: string; value: Awaited<ReturnType<typeof api.check>> } | null>(null);
  const [failure, setFailure] = useState<{ question: string; message: string } | null>(null);
  const result = answer?.question === question ? answer.value : null;
  const busy = pending === question;
  useEffect(() => () => { serial.current++; }, [question]);

  async function submit(event: FormEvent) {
    event.preventDefault();
    if (busy || !permission) return;
    const request = ++serial.current;
    setPending(question);
    setFailure(null);
    setAnswer(null);
    try {
      const value = await api.check({
        tenant_id: tenant.id, subject_id: account.id, permission,
        mode: "tenant_action", resource: { tenant_id: tenant.id },
      });
      if (alive.current && serial.current === request) setAnswer({ question, value });
    } catch (cause) {
      if (alive.current && serial.current === request) setFailure({ question, message: friendlyError(cause) });
    } finally {
      if (alive.current && serial.current === request) setPending(null);
    }
  }

  return <>
    <PageHeader eyebrow="Explainable decisions" title="Policy lab" detail="Ask the live authorization kernel one exact, tenant-scoped question." />
    <section className="lab-layout">
      <form className="paper-card lab-form" onSubmit={submit}>
        <label>Subject<input value={account.id} disabled /></label>
        <label>Permission<select value={permission} onChange={(event) => {
          serial.current++;
          setPermission(event.target.value);
          setPending(null);
          setAnswer(null);
          setFailure(null);
        }}>{permissions.map(item => <option key={item.key}>{item.key}</option>)}</select></label>
        <button className="primary-button" type="submit" disabled={busy || !permission}>
          <Fingerprint size={17} />{busy ? "Evaluating policy…" : "Evaluate policy"}
        </button>
        <ErrorMessage message={failure?.question === question ? failure.message : ""} />
      </form>
      <article className={result?.allowed ? "decision-card allow" : "decision-card"} aria-live="polite" aria-busy={busy}>
        <span className="eyebrow">Decision</span>
        {result ? <>
          <div className="decision-symbol">{result.allowed ? <Check /> : <X />}</div>
          <h2>{result.allowed ? "Allowed" : "Denied"}</h2>
          <p>Evaluated permission: <code>{permission}</code></p>
          <p>{result.reason.replaceAll("_", " ")}</p>
          <code>{result.effective_scope || "no effective scope"}</code>
        </> : <>
          <div className="decision-symbol neutral"><Fingerprint /></div>
          <h2>{busy ? "Evaluating question…" : "Awaiting a question"}</h2>
          <p>The result will include a stable reason and effective scope.</p>
        </>}
      </article>
    </section>
  </>;
}
