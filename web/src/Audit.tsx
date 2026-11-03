import { friendlyError } from "./errors";
import { useEffect, useState, type FormEvent } from "react";
import { api } from "./api";
import { ErrorMessage, PageHeader } from "./components";
import type { AuditFilter, AuditPage } from "./types";
export function AuditWorkspace({
  tenant,
  allowed,
}: {
  tenant: string;
  allowed: boolean;
}) {
  const [draft, setDraft] = useState({ q: "", outcome: "", operation: "" });
  const [filter, setFilter] = useState<AuditFilter>({ limit: 30 });
  const [page, setPage] = useState<AuditPage>({ items: [] });
  const [cursors, setCursors] = useState<Array<number | undefined>>([]);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(false);
  useEffect(() => {
    if (!allowed) return;
    let active = true;
    setLoading(true);
    setError("");
    api
      .auditPage(tenant, filter)
      .then((value) => {
        if (active) setPage(value);
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
  }, [tenant, filter, allowed]);
  function search(event: FormEvent) {
    event.preventDefault();
    setCursors([]);
    setFilter({ ...draft, limit: 30 });
  }
  if (!allowed)
    return (
      <p className="quiet-state">
        Your role does not include audit read access.
      </p>
    );
  return (
    <>
      <PageHeader
        eyebrow="Append-only evidence"
        title="Audit trail"
        detail="Search administration events by actor, target, operation, or request ID."
      />
      <form className="audit-filters" onSubmit={search}>
        <label>
          Search
          <input
            type="search"
            value={draft.q}
            maxLength={128}
            onChange={(e) => setDraft((d) => ({ ...d, q: e.target.value }))}
          />
        </label>
        <label>
          Outcome
          <select
            value={draft.outcome}
            onChange={(e) =>
              setDraft((d) => ({ ...d, outcome: e.target.value }))
            }
          >
            <option value="">All outcomes</option>
            {["success", "noop", "denied", "conflict", "failed"].map((o) => (
              <option key={o}>{o}</option>
            ))}
          </select>
        </label>
        <label>
          Operation
          <input
            value={draft.operation}
            maxLength={64}
            placeholder="e.g. invitation.create"
            onChange={(e) =>
              setDraft((d) => ({ ...d, operation: e.target.value }))
            }
          />
        </label>
        <button className="primary-button" disabled={loading}>
          Apply filters
        </button>
      </form>
      <ErrorMessage message={error} />
      {loading ? <p role="status">Loading events…</p> : null}
      <section className="timeline">
        {page.items.map((event) => (
          <article key={event.id}>
            <div className="timeline-dot" />
            <time>{new Date(event.at).toLocaleString()}</time>
            <div>
              <h2>{event.operation}</h2>
              <p className="break-text">
                {event.target_type} · {event.target_id}
              </p>
              <details>
                <summary>Event details</summary>
                <dl>
                  <dt>Actor</dt>
                  <dd>
                    {event.actor_subject_id || event.host_initiator || "System"}
                  </dd>
                  <dt>Request</dt>
                  <dd>{event.request_id || "—"}</dd>
                  <dt>Reason</dt>
                  <dd>{event.reason_code || "—"}</dd>
                  <dt>Role version</dt>
                  <dd>{event.role_version || "—"}</dd>
                  <dt>Phase</dt>
                  <dd>{event.phase}</dd>
                </dl>
              </details>
            </div>
            <span
              className={event.outcome === "success" ? "pill positive" : "pill"}
            >
              {event.outcome}
            </span>
          </article>
        ))}
        {!loading && !page.items.length ? (
          <div className="quiet-state">No events match these filters.</div>
        ) : null}
      </section>
      <div className="button-row pagination">
        <button
          className="secondary-button"
          disabled={loading || !cursors.length}
          onClick={() => {
            const previous = cursors.at(-1);
            setCursors((c) => c.slice(0, -1));
            setFilter((f) => ({ ...f, before: previous }));
          }}
        >
          Newer events
        </button>
        <button
          className="secondary-button"
          disabled={loading || !page.next_cursor}
          onClick={() => {
            setCursors((c) => [...c, filter.before]);
            setFilter((f) => ({ ...f, before: page.next_cursor }));
          }}
        >
          Older events
        </button>
      </div>
    </>
  );
}
