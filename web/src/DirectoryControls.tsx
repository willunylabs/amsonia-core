import { useState, type FormEvent } from "react";
import { ErrorMessage } from "./components";

export function DirectorySearch({
  label,
  placeholder,
  statuses = [],
  onSearch,
}: {
  label: string;
  placeholder: string;
  statuses?: string[];
  onSearch: (q: string, status: string) => void;
}) {
  const [q, setQuery] = useState("");
  const [status, setStatus] = useState("");
  const [error, setError] = useState("");
  function submit(event: FormEvent) {
    event.preventDefault();
    if (Array.from(q.trim()).length > 128) {
      setError("Use at most 128 characters for your search.");
      return;
    }
    setError("");
    onSearch(q, status);
  }
  return (
    <form className="audit-filters directory-filters" onSubmit={submit}>
      <label>
        {label}
        <input
          type="search"
          value={q}
          placeholder={placeholder}
          onChange={(e) => { setQuery(e.target.value); setError(""); }}
        />
      </label>
      {statuses.length ? (
        <label>
          {label} status
          <select
            aria-label={`${label} status`}
            value={status}
            onChange={(e) => setStatus(e.target.value)}
          >
            <option value="">All statuses</option>
            {statuses.map((value) => (
              <option key={value}>{value}</option>
            ))}
          </select>
        </label>
      ) : null}
      <button className="primary-button">Search</button>
      <button
        type="button"
        className="secondary-button"
        onClick={() => {
          setQuery("");
          setStatus("");
          setError("");
          onSearch("", "");
        }}
      >
        Reset
      </button>
      <ErrorMessage message={error} />
    </form>
  );
}

export function DirectoryNavigation({
  directory,
  label,
}: {
  directory: {
    loading: boolean;
    error: string;
    page?: { total: number; next_cursor?: string };
    previous: boolean;
    back: () => void;
    next: () => void;
    retry: () => void;
  };
  label: string;
}) {
  return (
    <div className="directory-navigation" aria-label={`${label} pagination`}>
      <ErrorMessage message={directory.error} />
      {directory.error ? (
        <button className="secondary-button" onClick={directory.retry}>
          Retry {label.toLowerCase()}
        </button>
      ) : null}
      <div className="button-row pagination">
        <span role="status">
          {directory.loading
            ? `Loading ${label.toLowerCase()}…`
            : directory.page
              ? `${directory.page.total} matching ${label.toLowerCase()}`
              : ""}
        </span>
        <button
          className="secondary-button"
          disabled={directory.loading || !directory.previous}
          onClick={directory.back}
        >
          Previous {label.toLowerCase()}
        </button>
        <button
          className="secondary-button"
          disabled={directory.loading || !directory.page?.next_cursor}
          onClick={directory.next}
        >
          Next {label.toLowerCase()}
        </button>
      </div>
    </div>
  );
}
