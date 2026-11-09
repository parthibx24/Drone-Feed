import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { GimbalClient, type ZoomDirection } from "../services/GimbalClient";
import type { Notify } from "./useToast";

const DEFAULT_SPEED = 4;
const ZOOM_STEP = 0.1;
const MIN_ZOOM = 1;

/** Gimbal movement and camera zoom; telemetry overrides the optimistic zoom when it reports one. */
export function useGimbal(host: string, reportedZoom: number | null, notify: Notify) {
  const client = useMemo(() => new GimbalClient(host), [host]);
  const [speed, setSpeed] = useState(DEFAULT_SPEED);
  const [zoom, setZoom] = useState(MIN_ZOOM);
  const speedRef = useRef(speed);

  useEffect(() => {
    speedRef.current = speed;
  }, [speed]);

  useEffect(() => {
    if (reportedZoom != null) setZoom(reportedZoom);
  }, [reportedZoom]);

  const move = useCallback(
    (yaw: number, pitch: number) => client.move(yaw, pitch, speedRef.current),
    [client],
  );

  const stop = useCallback(() => {
    client.stop().catch(() => {});
  }, [client]);

  const center = useCallback(() => {
    client
      .center()
      .then(() => notify("Gimbal centered"))
      .catch((error: Error) => notify(error.message));
  }, [client, notify]);

  const zoomStep = useCallback(
    (direction: ZoomDirection) => {
      client.zoom(direction).catch(() => {});
      setZoom((current) =>
        direction === "in" ? current + ZOOM_STEP : Math.max(MIN_ZOOM, current - ZOOM_STEP),
      );
    },
    [client],
  );

  const zoomReset = useCallback(() => {
    client
      .zoomReset()
      .then(() => {
        setZoom(MIN_ZOOM);
        notify("Zoom reset");
      })
      .catch((error: Error) => notify(error.message));
  }, [client, notify]);

  return { speed, setSpeed, zoom, move, stop, center, zoomStep, zoomReset };
}

export type GimbalControls = ReturnType<typeof useGimbal>;
