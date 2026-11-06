import { useEffect, useState } from "react";
import { friendlyError } from "./errors";
import type { DirectoryFilter, DirectoryPage } from "./types";

export function useDirectory<T>(
  load: (tenant: string, filter: DirectoryFilter) => Promise<DirectoryPage<T>>,
  tenant: string,
  allowed: boolean,
  revision = 0,
) {
  const [filter, setFilter] = useState<DirectoryFilter>({ limit: 30 });
  const [cursors, setCursors] = useState<Array<string | undefined>>([]);
  const [attempt, setAttempt] = useState(0);
  const [result, setResult] = useState<{
    key: string;
    page?: DirectoryPage<T>;
    error?: string;
  }>();
  const key = JSON.stringify([tenant, filter, revision, attempt, allowed]);
  useEffect(() => {
    if (!allowed) return;
    let active = true;
    load(tenant, filter).then(
      (page) => {
        if (!active) return;
        // A deletion or status change can empty the last page. Return to the
        // beginning of the same search instead of stranding the operator.
        if (!page.items.length && filter.after) {
          setCursors([]);
          setFilter((current) => ({ ...current, after: undefined }));
        } else setResult({ key, page });
      },
      (cause) => {
        if (active) setResult({ key, error: friendlyError(cause) });
      },
    );
    return () => {
      active = false;
    };
  }, [load, tenant, filter, allowed, key]);
  const current = allowed && result?.key === key ? result : undefined;
  return {
    page: current?.page,
    error: current?.error ?? "",
    loading: allowed && !current,
    filter,
    previous: cursors.length > 0,
    search: (q: string, status: string) => {
      setCursors([]);
      setFilter({ q: q.trim(), status, limit: 30 });
      setAttempt((value) => value + 1);
    },
    retry: () => setAttempt((value) => value + 1),
    reload: () => {
      setCursors([]);
      setFilter((current) => ({ ...current, after: undefined }));
      setAttempt((value) => value + 1);
    },
    back: () => {
      setFilter((current) => ({ ...current, after: cursors.at(-1) }));
      setCursors((current) => current.slice(0, -1));
    },
    next: () => {
      if (!current?.page?.next_cursor) return;
      setCursors((value) => [...value, filter.after]);
      setFilter((value) => ({ ...value, after: current.page?.next_cursor }));
    },
  };
}
