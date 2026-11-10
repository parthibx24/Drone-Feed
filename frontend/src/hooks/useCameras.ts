import { useEffect, useState } from "react";
import { api } from "../services/api";
import type { CameraFeed } from "../types/drone";
import { defaultStreams, toCameraFeeds } from "../utils/cameras";

interface Cameras {
  go2rtcUrl: string;
  feeds: CameraFeed[];
}

/** The drone's camera feeds, discovered in go2rtc by stream prefix ("<prefix>_*"). */
export function useCameras(droneId: string, streamPrefix: string | undefined): Cameras | null {
  const [cameras, setCameras] = useState<Cameras | null>(null);

  useEffect(() => {
    if (!streamPrefix) return;
    let cancelled = false;
    api
      .cameras(droneId)
      .then((res) => {
        if (cancelled) return;
        const streams = res.streams.length ? res.streams : defaultStreams(streamPrefix);
        setCameras({ go2rtcUrl: res.go2rtc_url ?? "", feeds: toCameraFeeds(streams, streamPrefix) });
      })
      .catch((error) => console.error("Camera discovery failed:", error));
    return () => {
      cancelled = true;
    };
  }, [droneId, streamPrefix]);

  return cameras;
}
