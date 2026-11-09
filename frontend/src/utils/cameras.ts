import type { CameraFeed } from "../types/drone";

const MAX_FEEDS = 2;

function suffixOf(stream: string, droneName: string): string {
  const prefix = `${droneName}_`;
  return stream.startsWith(prefix) ? stream.slice(prefix.length) : stream;
}

function isThermal(stream: string, droneName: string): boolean {
  return /(^|_)thermal($|_)|(^|_)ir($|_)/i.test(suffixOf(stream, droneName));
}

function cameraLabel(stream: string, droneName: string): string {
  const suffix = suffixOf(stream, droneName);
  if (["camera", "cam", "visible"].includes(suffix.toLowerCase())) return "Visible";
  if (isThermal(stream, droneName)) return "Thermal";
  return suffix.charAt(0).toUpperCase() + suffix.slice(1);
}

export function defaultStreams(droneName: string): string[] {
  return [`${droneName}_camera`, `${droneName}_thermal`];
}

/** Builds the two feeds shown on the dashboard, visible camera first. */
export function toCameraFeeds(streams: string[], droneName: string): CameraFeed[] {
  return streams
    .map((stream) => ({
      stream,
      label: cameraLabel(stream, droneName),
      thermal: isThermal(stream, droneName),
    }))
    .sort((a, b) => Number(a.thermal) - Number(b.thermal))
    .slice(0, MAX_FEEDS);
}
