export const THERMAL_FILTER_ID = "thermal-palette";

/** Blue (cold) to red (hot) colormap for the grayscale thermal feed. */
export function ThermalFilter() {
  return (
    <svg aria-hidden="true" width="0" height="0" style={{ position: "absolute" }}>
      <defs>
        <filter id={THERMAL_FILTER_ID} colorInterpolationFilters="sRGB">
          <feColorMatrix type="saturate" values="0" />
          <feComponentTransfer>
            <feFuncR type="table" tableValues="0.0 0.20 0.60 0.95 1.0" />
            <feFuncG type="table" tableValues="0.0 0.0 0.15 0.35 0.25" />
            <feFuncB type="table" tableValues="1.0 0.75 0.35 0.10 0.05" />
          </feComponentTransfer>
        </filter>
      </defs>
    </svg>
  );
}
