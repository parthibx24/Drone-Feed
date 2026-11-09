import { useCallback, useEffect, useState } from "react";
import { api } from "../services/api";
import type { Recording, RecordingSort } from "../types/recording";

/** Loads recordings, reloading when the sort, the shared list version, or `reload` changes. */
export function useRecordingsList(sort: RecordingSort, listVersion: number) {
  const [items, setItems] = useState<Recording[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [reloadCount, setReloadCount] = useState(0);
  const { sortBy, order } = sort;

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    api
      .recordings({ sortBy, order })
      .then((recordings) => {
        if (cancelled) return;
        setItems(recordings);
        setError(null);
      })
      .catch((reason: Error) => {
        if (!cancelled) setError(reason.message);
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [sortBy, order, listVersion, reloadCount]);

  const reload = useCallback(() => setReloadCount((count) => count + 1), []);

  return { items, error, loading, reload };
}
