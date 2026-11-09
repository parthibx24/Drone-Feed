import type { DroneParams } from "../types/drone";

const stripQuotes = (value: string) => value.trim().replace(/^["']|["']$/g, "");

/** Reads the drone from `?host=&name=`; the name defaults to the host. */
export function readDroneParams(search: string): DroneParams | null {
  const params = new URLSearchParams(search);
  const rawHost = params.get("host");
  if (!rawHost) return null;
  const host = stripQuotes(rawHost);
  const rawName = params.get("name");
  return { host, name: rawName ? stripQuotes(rawName) : host };
}
