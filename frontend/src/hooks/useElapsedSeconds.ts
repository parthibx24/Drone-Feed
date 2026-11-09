import { useEffect, useState } from "react";

/** Seconds since a Unix timestamp, updated once a second. */
export function useElapsedSeconds(startTime: number | null): number {
  const [now, setNow] = useState(() => Date.now() / 1000);

  useEffect(() => {
    if (startTime == null) return;
    setNow(Date.now() / 1000);
    const interval = setInterval(() => setNow(Date.now() / 1000), 1000);
    return () => clearInterval(interval);
  }, [startTime]);

  return startTime == null ? 0 : Math.max(0, now - startTime);
}
