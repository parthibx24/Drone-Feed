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

/** Each palette's colours from cold (left) to hot (right), for the swatch next to its name. */
const PALETTE_COLOURS: Record<string, string> = {
  white_hot: "#000, #fff",
  black_hot: "#fff, #000",
  red_hot: "#000, #aaa 70%, #f22",
  tint: "#000, #aaa 70%, #f22",
  green_hot: "#000, #2f2",
  iron_red: "#000, #2a0a5e 20%, #8c1a8c 40%, #e0401a 62%, #f7a51a 82%, #fff",
  ironbow: "#000, #2a0a5e 20%, #8c1a8c 40%, #e0401a 62%, #f7a51a 82%, #fff",
  hot_iron: "#000, #600 30%, #e33 55%, #fb3 80%, #fff",
  arctic: "#0a1a40, #2a5ab0, #a0c0e0, #f0c040, #ffe080",
  medical: "#0a0a40, #2040c0, #20c0a0, #c0e040, #f06020, #fff",
  fulgurite: "#000, #400060, #d02020, #ffa000, #ffff80",
  rainbow: "#1b1b8c, #1aa0e0, #2ec24a, #f2e02a, #f23a1a",
  rainbow1: "#1b1b8c, #1aa0e0, #2ec24a, #f2e02a, #f23a1a",
  rainbow2: "#000, #3030ff, #00e0e0, #00e000, #ff0, #f00, #fff",
};

/** A CSS gradient for the palette (a grey ramp for one this list doesn't know). */
export function paletteGradient(name: string): string {
  const key = name.toLowerCase().replace(/[\s-]+/g, "_");
  return `linear-gradient(90deg, ${PALETTE_COLOURS[key] ?? "#2a2a2a, #bbb"})`;
}
