/** Visual language: dark, premium, technical. Sampled from the Firstbid dashboard
 *  so the motion graphics and the product footage read as one system. */
export const C = {
  bg:        "#05070a",
  bgLift:    "#0a0e14",
  panel:     "#0d1219",
  panelEdge: "rgba(126,150,190,0.16)",

  ink:       "#e8eef7",
  inkDim:    "#8fa0b8",
  inkFaint:  "#56657c",

  /** the dashboard's model blue */
  model:     "#7e9cff",
  modelGlow: "rgba(126,156,255,0.40)",
  /** venue/book red and the take-green */
  book:      "#c96b7a",
  take:      "#4ec99a",
  warn:      "#e0b060",
  refuse:    "#6b7688",

  grid:      "rgba(126,156,255,0.055)",
} as const;

export const F = {
  sans: "'Inter', 'Segoe UI', system-ui, sans-serif",
  mono: "'JetBrains Mono', 'Consolas', ui-monospace, monospace",
} as const;

/** The recording is 1920x1028 with ~92px of browser chrome we crop away. */
export const SRC = { w: 1920, h: 1028, chrome: 92 } as const;
/** Footage viewport inside the 1920x1080 frame. */
export const VIEW = { x: 0, y: 48, w: 1920, h: 936 } as const;
/** A camera rect showing the whole cropped page, 1:1. */
export const FULL = { x: 0, y: SRC.chrome, w: 1920, h: 936 } as const;
