import { EMPTY_VALUE } from "../../utils/format";

/** "36.4 °C" (with a no-break space), or "—" while there is no reading. */
export function formatTemp(celsius: number | null): string {
  return celsius == null || !Number.isFinite(celsius) ? EMPTY_VALUE : `${celsius.toFixed(1)} °C`;
}

/** The bridge's palette names ("white_hot", "iron_red") as labels ("White hot", "Iron red"). */
export function paletteLabel(name: string): string {
  const words = name.replace(/[_-]+/g, " ").trim().toLowerCase();
  return words ? words[0].toUpperCase() + words.slice(1) : name;
}
