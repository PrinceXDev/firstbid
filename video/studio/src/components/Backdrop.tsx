import React from "react";
import { AbsoluteFill, useCurrentFrame, interpolate, random } from "remotion";
import { C } from "../theme";

/** Technical grid + slow particle drift + vignette. Sits under every
 *  graphics scene so the film has one continuous ground. */
export const Backdrop: React.FC<{
  /** 0 = flat, 1 = full presence. Lets a scene calm the background down. */
  intensity?: number;
  particles?: number;
  seed?: string;
}> = ({ intensity = 1, particles = 34, seed = "fb" }) => {
  const f = useCurrentFrame();
  const drift = (f * 0.14) % 80;

  return (
    <AbsoluteFill style={{ backgroundColor: C.bg }}>
      {/* deep radial lift so the centre is never dead flat */}
      <AbsoluteFill
        style={{
          background: `radial-gradient(120% 85% at 50% 38%, ${C.bgLift} 0%, ${C.bg} 62%, #030406 100%)`,
        }}
      />
      {/* drifting 80px technical grid */}
      <AbsoluteFill
        style={{
          opacity: 0.9 * intensity,
          backgroundImage: `linear-gradient(${C.grid} 1px, transparent 1px),
                            linear-gradient(90deg, ${C.grid} 1px, transparent 1px)`,
          backgroundSize: "80px 80px",
          backgroundPosition: `${drift}px ${drift * 0.5}px`,
          maskImage: "radial-gradient(105% 80% at 50% 45%, #000 30%, transparent 82%)",
          WebkitMaskImage: "radial-gradient(105% 80% at 50% 45%, #000 30%, transparent 82%)",
        }}
      />
      {/* sparse slow particles */}
      {new Array(particles).fill(0).map((_, i) => {
        const sx = random(`${seed}x${i}`);
        const sy = random(`${seed}y${i}`);
        const sp = 0.22 + random(`${seed}s${i}`) * 0.7;
        const size = 1.1 + random(`${seed}r${i}`) * 2.2;
        const y = (sy * 1200 - f * sp + 1200) % 1240 - 60;
        const tw = 0.22 + 0.5 * Math.abs(Math.sin((f * 0.02) + i));
        return (
          <div
            key={i}
            style={{
              position: "absolute",
              left: sx * 1920,
              top: y,
              width: size,
              height: size,
              borderRadius: size,
              backgroundColor: C.model,
              opacity: tw * 0.5 * intensity,
              filter: "blur(0.3px)",
            }}
          />
        );
      })}
      {/* vignette */}
      <AbsoluteFill
        style={{
          background:
            "radial-gradient(78% 62% at 50% 48%, transparent 46%, rgba(0,0,0,0.55) 100%)",
          opacity: interpolate(intensity, [0, 1], [0.55, 1]),
        }}
      />
    </AbsoluteFill>
  );
};
