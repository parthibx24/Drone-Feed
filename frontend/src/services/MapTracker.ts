import L from "leaflet";

const SATELLITE_TILES =
  "https://server.arcgisonline.com/ArcGIS/rest/services/World_Imagery/MapServer/tile/{z}/{y}/{x}";
const SATELLITE_ATTRIBUTION = "Imagery © Esri";
const MAX_ZOOM = 19;
const FOLLOW_ZOOM = 17;
const MAX_TRAIL_POINTS = 250;

/** Live drone position on a satellite map, with a rotating marker and a flight trail. */
export class MapTracker {
  private readonly map: L.Map;
  private readonly trail: L.Polyline;
  private readonly markerHtml: string;
  private readonly trailPoints: L.LatLngTuple[] = [];
  private marker: L.Marker | null = null;
  private target: L.CircleMarker | null = null;

  constructor(element: HTMLElement, markerHtml: string, trailColor: string) {
    this.markerHtml = markerHtml;
    this.map = L.map(element, { zoomControl: false }).setView([0, 0], 2);
    this.map.attributionControl.setPrefix(false);
    L.tileLayer(SATELLITE_TILES, { maxZoom: MAX_ZOOM, attribution: SATELLITE_ATTRIBUTION }).addTo(this.map);
    this.trail = L.polyline([], { color: trailColor, weight: 2 }).addTo(this.map);
  }

  update(lat: number | null | undefined, lon: number | null | undefined, heading: number): void {
    if (lat == null || lon == null || (lat === 0 && lon === 0)) return;

    if (!this.marker) {
      const icon = L.divIcon({ className: "", html: this.markerHtml, iconSize: [0, 0] });
      this.marker = L.marker([lat, lon], { icon }).addTo(this.map);
      this.map.setView([lat, lon], FOLLOW_ZOOM);
    } else {
      this.marker.setLatLng([lat, lon]);
    }
    this.marker.getElement()?.style.setProperty("--heading", `${heading}deg`);

    this.trailPoints.push([lat, lon]);
    if (this.trailPoints.length > MAX_TRAIL_POINTS) this.trailPoints.shift();
    this.trail.setLatLngs(this.trailPoints);
  }

  /** Calls handler with the clicked position (null removes it). */
  onClick(handler: ((lat: number, lon: number) => void) | null): void {
    this.map.off("click");
    if (handler) this.map.on("click", (event: L.LeafletMouseEvent) => handler(event.latlng.lat, event.latlng.lng));
    this.map.getContainer().style.cursor = handler ? "crosshair" : "";
  }

  /** Shows (or clears) a goto target ring. */
  setTarget(position: [number, number] | null): void {
    this.target?.remove();
    this.target = position
      ? L.circleMarker(position, { radius: 9, color: "#fbbf24", weight: 3, fillOpacity: 0.15 }).addTo(this.map)
      : null;
  }

  destroy(): void {
    this.map.remove();
  }
}
