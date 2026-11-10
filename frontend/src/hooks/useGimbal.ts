import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { GimbalClient, type ZoomDirection } from "../services/GimbalClient";
import { errorText } from "../services/http";
import type { Notify } from "./useToast";

const DEFAULT_SPEED = 4;
const ZOOM_STEP = 0.1;
const MIN_ZOOM = 1;

/** Gimbal movement and camera zoom; telemetry overrides the optimistic zoom when it reports one. */
export function useGimbal(
  droneId: string,
  sendRate: (pitchDegS: number, yawDegS: number) => void,
  reportedZoom: number | null,
  notify: Notify,
) {
  const client = useMemo(() => new GimbalClient(droneId, sendRate), [droneId, sendRate]);

  useEffect(() => () => client.dispose(), [client]);
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

  const stop = useCallback(() => client.stop(), [client]);

  const center = useCallback(() => {
    client
      .center()
      .then(() => notify("Gimbal centered"))
      .catch((error) => notify(errorText(error)));
  }, [client, notify]);

  const zoomStep = useCallback(
    (direction: ZoomDirection) => {
      client.zoom(direction).catch((error) => notify(errorText(error)));
      setZoom((current) =>
        direction === "in" ? current + ZOOM_STEP : Math.max(MIN_ZOOM, current - ZOOM_STEP),
      );
    },
    [client, notify],
  );

  const zoomReset = useCallback(() => {
    client
      .zoomReset()
      .then(() => {
        setZoom(MIN_ZOOM);
        notify("Zoom reset");
      })
      .catch((error) => notify(errorText(error)));
  }, [client, notify]);

  return { speed, setSpeed, zoom, move, stop, center, zoomStep, zoomReset };
}

export type GimbalControls = ReturnType<typeof useGimbal>;
