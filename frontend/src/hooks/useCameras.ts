import { useEffect, useState } from "react";
import { api } from "../services/api";
import type { CameraFeed } from "../types/drone";
import { defaultStreams, toCameraFeeds } from "../utils/cameras";

interface Cameras {
  go2rtcUrl: string;
  feeds: CameraFeed[];
}

async function discoverStreams(name: string): Promise<string[]> {
  try {
    const { cameras } = await api.cameras(name);
    if (cameras?.length) return cameras;
  } catch (error) {
    console.warn("Camera enumeration failed:", error);
  }
  return defaultStreams(name);
}

export function useCameras(name: string): Cameras | null {
  const [cameras, setCameras] = useState<Cameras | null>(null);

  useEffect(() => {
    let cancelled = false;
    Promise.all([api.config(), discoverStreams(name)])
      .then(([config, streams]) => {
        if (!cancelled) {
          setCameras({ go2rtcUrl: config.go2rtc_url ?? "", feeds: toCameraFeeds(streams, name) });
        }
      })
      .catch((error) => console.error(error));
    return () => {
      cancelled = true;
    };
  }, [name]);

  return cameras;
}
