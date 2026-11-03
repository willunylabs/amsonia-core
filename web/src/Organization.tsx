import { useEffect, useState, type FormEvent } from "react";
import { api, APIError } from "./api";
import { ErrorMessage } from "./components";
import { friendlyError } from "./errors";
import { FormPage } from "./navigation";
import { useAlive, navigate } from "./routing";
import type { OrganizationSettings } from "./types";

export function OrganizationPage({
  tenant,
  onChanged,
}: {
  tenant: string;
  onChanged: () => Promise<void>;
}) {
  const alive = useAlive();
  const [settings, setSettings] = useState<OrganizationSettings>();
  const [name, setName] = useState("");
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [attempt, setAttempt] = useState(0);
  const [error, setError] = useState("");
  const [message, setMessage] = useState("");
  const [conflict, setConflict] = useState(false);

  useEffect(() => {
    let active = true;
    setLoading(true);
    setSettings(undefined);
    setError("");
    setMessage("");
    setConflict(false);
    api
      .organization(tenant)
      .then((value) => {
        if (active) {
          setSettings(value);
          setName(value.name);
        }
      })
      .catch((cause) => {
        if (active) setError(friendlyError(cause));
      })
      .finally(() => {
        if (active) setLoading(false);
      });
    return () => {
      active = false;
    };
  }, [tenant, attempt]);

  async function save(event: FormEvent) {
    event.preventDefault();
    if (!settings?.can_manage || busy || loading || conflict) return;
    const nextName = name.trim();
    if (
      !nextName ||
      new TextEncoder().encode(nextName).length > 80 ||
      /\p{Cc}/u.test(nextName)
    ) {
      setError("Enter a short organization name without control characters.");
      return;
    }
    setBusy(true);
    setError("");
    setMessage("");
    try {
      const updated = await api.updateOrganization(
        tenant,
        nextName,
        settings.version,
      );
      if (!alive.current) return;
      setSettings(updated);
      setName(updated.name);
      setMessage("Organization settings saved.");
      await onChanged();
    } catch (cause) {
      if (!alive.current) return;
      if (cause instanceof APIError && cause.status === 409) {
        setConflict(true);
        setError(
          "Organization settings changed. Reload the latest settings before saving again. Your draft has been kept.",
        );
      } else setError(friendlyError(cause));
    } finally {
      if (alive.current) setBusy(false);
    }
  }

  return (
    <FormPage
      title="Organization settings"
      detail="Review the organization identity and update its name."
      back={{ path: "overview", label: "Back to overview" }}
    >
      {loading ? <p role="status">Loading organization settings…</p> : null}
      <ErrorMessage message={error} />
      {!settings && !loading ? (
        <button
          type="button"
          className="secondary-button"
          onClick={() => setAttempt((v) => v + 1)}
        >
          Retry settings
        </button>
      ) : null}
      {settings ? (
        <>
          <dl className="organization-metadata">
            <div>
              <dt>Organization ID</dt>
              <dd>
                <code>{settings.id}</code>
              </dd>
            </div>
            <div>
              <dt>Status</dt>
              <dd>{settings.state}</dd>
            </div>
            <div>
              <dt>Created</dt>
              <dd>{new Date(settings.created_at).toLocaleString()}</dd>
            </div>
            <div>
              <dt>Last updated</dt>
              <dd>{new Date(settings.updated_at).toLocaleString()}</dd>
            </div>
          </dl>
          <form className="admin-form" onSubmit={save}>
            <label>
              Organization name
              <input
                value={name}
                onChange={(e) => setName(e.target.value)}
                required
                maxLength={80}
                readOnly={!settings.can_manage}
                disabled={busy}
                autoComplete="organization"
              />
            </label>
            {!settings.can_manage ? (
              <p>
                Only a system administrator with active organization membership
                can change these settings.
              </p>
            ) : null}
            {message ? (
              <p role="status" className="success-note">
                {message}
              </p>
            ) : null}
            {conflict ? (
              <button
                type="button"
                className="secondary-button"
                onClick={() => setAttempt((v) => v + 1)}
              >
                Reload latest settings
              </button>
            ) : null}
            <div className="form-actions">
              <button
                type="button"
                className="secondary-button"
                onClick={() => navigate("overview")}
              >
                Cancel
              </button>
              {settings.can_manage ? (
                <button className="primary-button" disabled={busy || conflict}>
                  {busy ? "Saving…" : "Save organization"}
                </button>
              ) : null}
            </div>
          </form>
        </>
      ) : null}
    </FormPage>
  );
}
