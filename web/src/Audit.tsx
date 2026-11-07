import { friendlyError } from "./errors";
import { useEffect, useRef, useState, type FormEvent } from "react";
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
  const [draft, setDraft] = useState({
    q: "",
    outcome: "",
    operation: "",
    from: "",
    until: "",
  });
  const [filter, setFilter] = useState<AuditFilter>({ limit: 30 });
  const [result, setResult] = useState<{
    key: string;
    page?: AuditPage;
    error?: string;
  }>();
  const [attempt, setAttempt] = useState(0);
  const [cursors, setCursors] = useState<Array<number | undefined>>([]);
  const [error, setError] = useState("");
  const [download, setDownload] = useState<{
    key: string;
    pending?: boolean;
    error?: string;
    message?: string;
  }>();
  const downloadRequest = useRef(0);
  const downloadController = useRef<AbortController>();
  const key = JSON.stringify([tenant, allowed, filter, attempt]);
  const current = allowed && result?.key === key ? result : undefined;
  const page = current?.page;
  const loading = allowed && !current;
  const exporting = download?.key === key && !!download.pending;
  useEffect(
    () => () => {
      downloadController.current?.abort();
      ++downloadRequest.current;
    },
    [tenant, allowed, filter, key],
  );
  useEffect(() => {
    if (!allowed) return;
    let active = true;
    api
      .auditPage(tenant, filter)
      .then((value) => {
        if (active) setResult({ key, page: value });
      })
      .catch((cause) => {
        if (active) setResult({ key, error: friendlyError(cause) });
      });
    return () => {
      active = false;
    };
  }, [tenant, filter, allowed, key]);
  function search(event: FormEvent) {
    event.preventDefault();
    if (exporting) return;
    setError("");
    if (Array.from(draft.q).length > 128) {
      setError("Use at most 128 characters for your search.");
      return;
    }
    const from = draft.from ? new Date(draft.from) : undefined;
    const until = draft.until ? new Date(draft.until) : undefined;
    if (
      (from && Number.isNaN(from.getTime())) ||
      (until && Number.isNaN(until.getTime())) ||
      (from && until && from >= until)
    ) {
      setError("Choose a valid start time before the end time.");
      return;
    }
    setCursors([]);
    setFilter({
      ...draft,
      from: from?.toISOString(),
      until: until?.toISOString(),
      limit: 30,
    });
    setAttempt((value) => value + 1);
  }
  async function exportEvents() {
    if (!allowed || exporting) return;
    const serial = ++downloadRequest.current;
    const controller = new AbortController();
    downloadController.current = controller;
    setDownload({ key, pending: true });
    try {
      const file = await api.exportAudit(tenant, filter, controller.signal);
      if (controller.signal.aborted || serial !== downloadRequest.current)
        return;
      const url = URL.createObjectURL(file.blob);
      const link = document.createElement("a");
      link.href = url;
      link.download = file.filename;
      document.body.append(link);
      try {
        link.click();
      } finally {
        link.remove();
        setTimeout(() => URL.revokeObjectURL(url), 0);
      }
      setDownload({ key, message: "Audit CSV downloaded." });
    } catch (cause) {
      if (!controller.signal.aborted && serial === downloadRequest.current)
        setDownload({ key, error: friendlyError(cause) });
    }
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
        action={
          <button
            className="secondary-button"
            disabled={loading || exporting}
            onClick={() => void exportEvents()}
          >
            {exporting ? "Preparing CSV…" : "Export matching events"}
          </button>
        }
      />
      <form className="audit-filters audit-range-filters" onSubmit={search}>
        <label>
          Search
          <input
            type="search"
            value={draft.q}
            onChange={(e) => setDraft((d) => ({ ...d, q: e.target.value }))}
          />
        </label>
        <label>
          Outcome
          <select
            aria-label="Outcome"
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
        <label>
          From (local time)
          <input
            type="datetime-local"
            value={draft.from}
            onChange={(e) => setDraft((d) => ({ ...d, from: e.target.value }))}
          />
        </label>
        <label>
          Until (local time)
          <input
            type="datetime-local"
            value={draft.until}
            onChange={(e) => setDraft((d) => ({ ...d, until: e.target.value }))}
          />
        </label>
        <button className="primary-button" disabled={loading || exporting}>
          Apply filters
        </button>
      </form>
      <p className="audit-range-note">
        Times use your browser’s timezone. Includes events from the start,
        before the end. Export includes all matching events at download time, up
        to 10,000 events or 10 MiB.
      </p>
      <ErrorMessage message={error} />
      <ErrorMessage message={current?.error ?? ""} />
      {current?.error ? (
        <button
          className="secondary-button"
          disabled={exporting}
          onClick={() => setAttempt((value) => value + 1)}
        >
          Retry audit query
        </button>
      ) : null}
      <ErrorMessage
        message={download?.key === key ? (download.error ?? "") : ""}
      />
      {exporting ? (
        <button
          className="secondary-button"
          onClick={() => {
            downloadController.current?.abort();
            ++downloadRequest.current;
            setDownload({ key, message: "Export cancelled." });
          }}
        >
          Cancel export
        </button>
      ) : null}
      {download?.key === key && download.message ? (
        <p role="status">{download.message}</p>
      ) : null}
      {loading ? <p role="status">Loading events…</p> : null}
      <section className="timeline">
        {page?.items.map((event) => (
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
        {page && !page.items.length ? (
          <div className="quiet-state">No events match these filters.</div>
        ) : null}
      </section>
      <div className="button-row pagination">
        <button
          className="secondary-button"
          disabled={loading || exporting || !cursors.length}
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
          disabled={loading || exporting || !page?.next_cursor}
          onClick={() => {
            setCursors((c) => [...c, filter.before]);
            setFilter((f) => ({ ...f, before: page?.next_cursor }));
          }}
        >
          Older events
        </button>
      </div>
    </>
  );
}
