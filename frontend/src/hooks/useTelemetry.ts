import { useEffect, useState } from "react";
import { api } from "../services/api";
import { MavlinkClient } from "../services/MavlinkClient";
import type { LinkState, Telemetry } from "../types/drone";

const HEALTH_POLL_MS = 5000;
const TELEMETRY_POLL_MS = 1000;

const toLink = (ok: boolean): LinkState => (ok ? "ok" : "bad");

/** Live telemetry (WebSocket plus a fallback poll) and the agent/MAVLink link states. */
export function useTelemetry(host: string) {
  const [telemetry, setTelemetry] = useState<Telemetry>({});
  const [agentLink, setAgentLink] = useState<LinkState>("checking");
  const [mavlinkLink, setMavlinkLink] = useState<LinkState>("checking");

  useEffect(() => {
    const apply = (data: Telemetry) => {
      setTelemetry(data);
      if (data.flight_mode && data.flight_mode !== "UNKNOWN") setMavlinkLink("ok");
    };

    const pollHealth = () =>
      api
        .health(host)
        .then((health) => {
          setAgentLink(toLink(health.agent_ok !== false));
          setMavlinkLink(toLink(Boolean(health.mavlink_connected)));
        })
        .catch(() => {
          setAgentLink("bad");
          setMavlinkLink("bad");
        });

    const pollTelemetry = () =>
      api
        .telemetry(host)
        .then(apply)
        .catch(() => {
          /* offline: keep the last known values */
        });

    const client = new MavlinkClient(apply);
    client.connect(host);
    void pollHealth();
    void pollTelemetry();
    const healthTimer = setInterval(pollHealth, HEALTH_POLL_MS);
    const telemetryTimer = setInterval(pollTelemetry, TELEMETRY_POLL_MS);

    return () => {
      client.disconnect();
      clearInterval(healthTimer);
      clearInterval(telemetryTimer);
    };
  }, [host]);

  return { telemetry, agentLink, mavlinkLink };
}
