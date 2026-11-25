/**
 * dji-bridge reports a DJI aircraft's flight state as the nearest
 * ArduCopter mode, because the gateway speaks MAVLink. Those names mean
 * nothing on a DJI aircraft ("GUIDED"), so the dashboard shows DJI's own.
 * The mapping mirrors dji-bridge's CustomMode (Mavic-3t-System,
 * internal/mavlink/telemetry.go).
 */
const DJI_MODE_LABELS: Record<string, string> = {
  LOITER: "NORMAL", // the pilot flying, hovering, or on the ground
  GUIDED: "AUTO FLIGHT", // take-off, fly-to, remote control by StarWatch
  AUTO: "MISSION",
  RTL: "RETURN HOME",
  LAND: "LANDING",
  BRAKE: "AVOIDING",
};

/** The mode as the operator should read it on this kind of drone. */
export function flightModeLabel(mode: string | null | undefined, autopilot: string | undefined): string | null | undefined {
  if (!mode || autopilot !== "dji") return mode;
  return DJI_MODE_LABELS[mode] ?? mode;
}
